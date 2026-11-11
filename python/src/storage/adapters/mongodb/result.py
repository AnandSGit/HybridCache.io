"""
Rows and results of MongoDB queries.

A row's columns are the selected fields in select order, or, for a query that
returns whole documents, the document's fields in document order. Column names
may be dotted paths (``address.city``) into embedded documents.
"""

from __future__ import annotations

import dataclasses
from collections import deque
from collections.abc import (
    AsyncIterator,
    Awaitable,
    Callable,
    Iterable,
    Iterator,
    Mapping,
    MutableSequence,
    Sequence,
)
from typing import Any

from pymongo.asynchronous.cursor import AsyncCursor

from storage.adapters.mongodb.support import DRIVER_ERRORS, bounded, driver_error
from storage.errors import new_data_error
from storage.interfaces import Result, Row

Document = Mapping[str, Any]

_MISSING: Any = object()


def lookup(document: Document, path: str) -> Any:
    """Return the value at ``path`` (a field name or dotted path) in ``document``,
    or ``_MISSING``."""
    if path in document:
        return document[path]
    value: Any = document
    for key in path.split("."):
        if not isinstance(value, Mapping) or key not in value:
            return _MISSING
        value = value[key]
    return value


def nested_document(path: str, value: Any) -> dict[str, Any]:
    """Return ``{"a": {"b": value}}`` for the path ``a.b``: the shape of a projected
    document, so that computed rows read like found ones."""
    *parents, last = path.split(".")
    document: dict[str, Any] = {last: value}
    for key in reversed(parents):
        document = {key: document}
    return document


class MongoDBRow(Row):
    """A single document of a query result."""

    def __init__(self, document: Document, columns: Sequence[str] = ()) -> None:
        """Create a row.

        Args:
            document: The document.
            columns: The selected columns; empty for a whole document.
        """
        self._document = document
        self._columns = tuple(columns)

    async def scan(self, *destinations: Any) -> None:
        """Store the row's values, in column order, in ``destinations``.

        Python cannot assign to the caller's variables, so each destination is a
        non-empty mutable sequence, such as ``[None]``, whose first item receives
        the value.

        Raises:
            DataError: If the destinations do not match the columns.
        """
        values = self.values()
        if len(destinations) != len(values):
            raise new_data_error(
                "INVALID_DESTINATION",
                f"expected {len(values)} destinations, got {len(destinations)}",
            )
        for destination in destinations:
            if not isinstance(destination, MutableSequence) or not destination:
                raise new_data_error(
                    "INVALID_DESTINATION",
                    "a destination must be a non-empty mutable sequence, such as "
                    "[None]",
                )
        for destination, value in zip(destinations, values, strict=True):
            destination[0] = value

    async def scan_into(self, obj: Any) -> None:
        """Set the attributes of ``obj`` that match columns.

        For a dataclass instance its fields are set; for another object, the
        attributes it already has.

        Raises:
            DataError: If ``obj`` is a class or an attribute cannot be set.
        """
        if isinstance(obj, type):
            raise new_data_error("INVALID_DESTINATION", "scan_into needs an instance")
        if dataclasses.is_dataclass(obj):
            names = {f.name for f in dataclasses.fields(obj)}
            matches = [(k, v) for k, v in self.items() if k in names]
        else:
            matches = [(k, v) for k, v in self.items() if hasattr(obj, k)]
        for key, value in matches:
            try:
                setattr(obj, key, value)
            except (AttributeError, TypeError) as exc:
                raise new_data_error("SCAN_FAILED", f"cannot set {key!r}") from exc

    def get(self, key: str, default: Any = None) -> Any:
        """Return the value of the field or dotted path ``key``, or ``default``."""
        value = lookup(self._document, key)
        return default if value is _MISSING else value

    def keys(self) -> list[str]:
        """Return the column names."""
        return list(self._columns) if self._columns else list(self._document)

    def values(self) -> list[Any]:
        """Return the column values; a selected field the document lacks is None."""
        return [self.get(key) for key in self.keys()]

    def items(self) -> list[tuple[str, Any]]:
        """Return the (column name, value) pairs."""
        return [(key, self.get(key)) for key in self.keys()]

    def to_dict(self) -> dict[str, Any]:
        """Return the columns and their values as a dict."""
        return dict(self.items())

    def __getitem__(self, key: str) -> Any:
        if key not in self.keys():
            raise KeyError(key)
        return self.get(key)

    def __contains__(self, key: object) -> bool:
        return key in self.keys()

    def __len__(self) -> int:
        return len(self.keys())

    def __iter__(self) -> Iterator[str]:
        return iter(self.keys())

    def __repr__(self) -> str:
        return f"MongoDBRow({self.to_dict()!r})"


class MongoDBResult(Result):
    """The rows of a query: read through a cursor, or held in memory (COUNT,
    DISTINCT and batch results).

    A cursor is read under the query's deadline, and is closed once its rows are
    exhausted, when reading fails, or by ``close``.
    """

    def __init__(
        self,
        documents: Iterable[Document] = (),
        columns: Sequence[str] = (),
        *,
        cursor: AsyncCursor[dict[str, Any]] | None = None,
        deadline: float | None = None,
        counter: Callable[[], Awaitable[int]] | None = None,
    ) -> None:
        """Create a result.

        Args:
            documents: Documents to return before those of ``cursor``.
            columns: The selected columns; empty for whole documents.
            cursor: The cursor of the remaining documents, if any.
            deadline: The query's deadline, bounding reads from ``cursor``.
            counter: Counts the query's documents on the server (``count``).
        """
        self._buffer: deque[Document] = deque(documents)
        self._columns = tuple(columns)
        self._cursor = cursor
        self._deadline = deadline
        self._counter = counter
        self._size = len(self._buffer)
        self._current: Document | None = None

    @classmethod
    async def open(
        cls,
        cursor: AsyncCursor[dict[str, Any]],
        columns: Sequence[str] = (),
        *,
        deadline: float | None = None,
        counter: Callable[[], Awaitable[int]] | None = None,
    ) -> MongoDBResult:
        """Return a result over ``cursor``, reading its first document so that the
        query runs, and fails, here rather than on the first fetch."""
        result = cls(columns=columns, cursor=cursor, deadline=deadline, counter=counter)
        first = await result._next_document()
        if first is not None:
            result._buffer.append(first)
        return result

    def __aiter__(self) -> AsyncIterator[Row]:
        return self

    async def __anext__(self) -> MongoDBRow:
        row = await self.fetchone()
        if row is None:
            raise StopAsyncIteration
        return row

    async def fetchone(self) -> MongoDBRow | None:
        """Return the next row, or ``None`` when the rows are exhausted.

        Raises:
            QueryError: If reading fails; ``QueryTimeoutError`` after the deadline.
        """
        document = await self._next_document()
        return None if document is None else self._row(document)

    async def fetchmany(self, size: int = 1000) -> list[Row]:
        """Return up to ``size`` of the remaining rows."""
        rows: list[Row] = []
        while len(rows) < size:
            row = await self.fetchone()
            if row is None:
                break
            rows.append(row)
        return rows

    async def fetchall(self) -> list[Row]:
        """Return all remaining rows."""
        return [self._row(document) for document in await self._remaining()]

    async def close(self) -> None:
        """Close the cursor; the result then has no more rows. Idempotent."""
        cursor, self._cursor = self._cursor, None
        self._buffer.clear()
        if cursor is not None:
            await cursor.close()

    def columns(self) -> list[str]:
        """Return the selected columns; for whole documents, the fields of the
        current row (else of the next one, if any)."""
        if self._columns:
            return list(self._columns)
        document = self._current
        if document is None and self._buffer:
            document = self._buffer[0]
        return [] if document is None else list(document)

    async def count(self) -> int:
        """Return the number of rows of the query, counted on the server (with
        ``count_documents`` and the query's filter, skip and limit) for a result
        read through a cursor."""
        if self._counter is None:
            return self._size
        return await self._counter()

    async def materialize(self) -> MongoDBResult:
        """Read the remaining rows into a result that holds no cursor."""
        return MongoDBResult(await self._remaining(), self._columns)

    def _row(self, document: Document) -> MongoDBRow:
        self._current = document
        return MongoDBRow(document, self._columns)

    async def _next_document(self) -> Document | None:
        if self._buffer:
            return self._buffer.popleft()
        cursor = self._cursor
        if cursor is None:
            return None
        try:
            with bounded(self._deadline):
                document = await anext(cursor, None)
        except DRIVER_ERRORS as exc:
            await self.close()
            raise driver_error(exc, "failed to read query results") from exc
        if document is None:
            await self.close()
        return document

    async def _remaining(self) -> list[Document]:
        documents = list(self._buffer)
        self._buffer.clear()
        cursor = self._cursor
        if cursor is not None:
            try:
                with bounded(self._deadline):
                    documents.extend(await cursor.to_list())
            except DRIVER_ERRORS as exc:
                raise driver_error(exc, "failed to read query results") from exc
            finally:
                await self.close()
        return documents
