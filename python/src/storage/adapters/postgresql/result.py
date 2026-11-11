"""
Rows and results of the PostgreSQL adapter.

Results are buffered: every row is read before the query returns, so a result
holds no connection and stays readable after its connection went back to the
pool or its transaction ended.
"""

from __future__ import annotations

import dataclasses
from collections.abc import Iterator
from typing import Any, Self

from storage.errors import InvalidDataError
from storage.interfaces import Result, Row


class PostgreSQLRow(Row):
    """A row, wrapping an ``asyncpg.Record``."""

    def __init__(self, record: Any) -> None:
        self._record = record

    async def scan(self, *destinations: Any) -> None:
        """Copy the row's values, in column order, into ``destinations``.

        Python cannot assign to the caller's variables, so each destination is
        a ``list`` whose contents are replaced by the column's value::

            user_id, name = [], []
            await row.scan(user_id, name)  # user_id == [1], name == ["Ann"]

        Raises:
            InvalidDataError: If the number of destinations is not the number
                of columns or a destination is not a list; no destination is
                changed then.
        """
        if len(destinations) != len(self._record):
            raise InvalidDataError(
                f"scan needs {len(self._record)} destinations, got {len(destinations)}"
            )
        for index, destination in enumerate(destinations):
            if not isinstance(destination, list):
                raise InvalidDataError(
                    f"scan destination {index} must be a list, "
                    f"not {type(destination).__name__}"
                )
        for destination, value in zip(destinations, self._record.values(), strict=True):
            destination[:] = [value]

    async def scan_into(self, obj: Any) -> None:
        """Set the attributes of ``obj`` that have a column of the same name: the
        fields of a dataclass instance, else the attributes ``obj`` already has.

        Raises:
            InvalidDataError: If ``obj`` is a class or an attribute cannot be set
                (e.g. a frozen dataclass).
        """
        if isinstance(obj, type):
            raise InvalidDataError("scan_into needs an instance, not a class")
        if dataclasses.is_dataclass(obj):
            names = [field.name for field in dataclasses.fields(obj)]
        else:
            names = [key for key in self._record.keys() if hasattr(obj, key)]
        try:
            for name in names:
                if name in self._record:
                    setattr(obj, name, self._record[name])
        except (AttributeError, TypeError) as exc:  # frozen dataclasses included
            raise InvalidDataError(
                f"cannot set the attributes of {type(obj).__name__}"
            ) from exc

    def get(self, key: str, default: Any = None) -> Any:
        """Return the value of column ``key``, or ``default`` if there is none."""
        return self._record.get(key, default)

    def keys(self) -> list[str]:
        """Return the column names."""
        return list(self._record.keys())

    def values(self) -> list[Any]:
        """Return the column values."""
        return list(self._record.values())

    def items(self) -> list[tuple[str, Any]]:
        """Return the (column name, value) pairs."""
        return list(self._record.items())

    def __getitem__(self, key: str | int) -> Any:
        """Return a value by column name or position."""
        return self._record[key]

    def __contains__(self, key: object) -> bool:
        """Whether the row has a column named ``key``."""
        return key in self._record

    def __len__(self) -> int:
        """Return the number of columns."""
        return len(self._record)

    def __iter__(self) -> Iterator[Any]:
        """Iterate over the column values."""
        return iter(self._record)


class PostgreSQLResult(Result):
    """The buffered rows of a query."""

    def __init__(self, records: list[Any]) -> None:
        self._records = records
        self._index = 0

    def __aiter__(self) -> Self:
        return self

    async def __anext__(self) -> PostgreSQLRow:
        row = await self.fetchone()
        if row is None:
            raise StopAsyncIteration
        return row

    async def fetchone(self) -> PostgreSQLRow | None:
        """Return the next row, or ``None`` when exhausted."""
        if self._index >= len(self._records):
            return None
        self._index += 1
        return PostgreSQLRow(self._records[self._index - 1])

    async def fetchmany(self, size: int = 1000) -> list[Row]:
        """Return up to ``size`` of the remaining rows."""
        end = min(self._index + max(size, 0), len(self._records))
        rows: list[Row] = [
            PostgreSQLRow(record) for record in self._records[self._index : end]
        ]
        self._index = end
        return rows

    async def fetchall(self) -> list[Row]:
        """Return all remaining rows."""
        return await self.fetchmany(len(self._records))

    async def close(self) -> None:
        """Discard the remaining rows."""
        self._records = []
        self._index = 0

    def columns(self) -> list[str]:
        """Return the column names (empty when the result has no rows)."""
        return list(self._records[0].keys()) if self._records else []

    def __len__(self) -> int:
        """Return the number of rows."""
        return len(self._records)

    def __bool__(self) -> bool:
        """Whether the result has rows."""
        return bool(self._records)
