"""
Statement execution shared by ``PostgreSQLStorage`` and ``PostgreSQLTransaction``.

An ``Executor`` runs queries, commands and batches on a connection-like object
(an asyncpg connection or pool connection proxy), bounding each statement by
its timeout and mapping driver errors to storage errors.
"""

from __future__ import annotations

import asyncio
import datetime
from collections.abc import Awaitable, Callable, Sequence
from dataclasses import dataclass
from typing import Any, TypeVar

from storage.adapters.postgresql.errors import OpKind, translate_errors
from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.adapters.postgresql.sql import convert_placeholders, rows_affected
from storage.errors import (
    InvalidQueryError,
    QueryFailedError,
    StorageError,
    TransactionTimeoutError,
    new_query_timeout_error,
)
from storage.types import (
    Command,
    ExecuteResult,
    Operation,
    OperationResult,
    OperationType,
    Query,
)

T = TypeVar("T")

# An asyncpg Connection or PoolConnectionProxy (asyncpg ships no type hints).
Connection = Any


def translate_sql(sql: str) -> str:
    """Reject empty SQL and rewrite ``?`` placeholders (see
    ``convert_placeholders``).

    Raises:
        InvalidQueryError: If ``sql`` is empty.
    """
    if not sql.strip():
        raise InvalidQueryError("SQL statement is empty")
    return convert_placeholders(sql)


def positive_seconds(
    timeout: datetime.timedelta | None,
    what: str,
    error: type[StorageError] = InvalidQueryError,
) -> float | None:
    """``timeout`` in seconds (``None``: no limit).

    Raises:
        error: If ``timeout`` is zero or negative.
    """
    if timeout is None:
        return None
    seconds = timeout.total_seconds()
    if seconds <= 0:
        raise error(f"{what} must be positive (None means no limit)")
    return seconds


@dataclass(frozen=True)
class Statement:
    """A translated query or command."""

    sql: str
    parameters: Sequence[Any]
    timeout: datetime.timedelta | None

    @classmethod
    def of(cls, item: Query | Command) -> Statement:
        """Translate a query or command (``InvalidQueryError`` for empty SQL)."""
        return cls(translate_sql(item.sql), item.parameters, item.timeout)


@dataclass(frozen=True)
class Deadline:
    """When a statement must be done, in event-loop time (``None``: no limit).

    ``transaction`` is true when the transaction's deadline, not the statement's
    own timeout, is the binding one.
    """

    at: float | None
    transaction: bool = False

    def timeout_error(self, sql: str) -> StorageError:
        """The error for a statement that ran out of time."""
        if self.transaction:
            return TransactionTimeoutError()
        return new_query_timeout_error(sql)


class Executor:
    """Runs statements on a connection.

    ``query_timeout`` bounds a statement whose own timeout is ``None``
    (``Config.query_timeout``); ``transaction_deadline`` (event-loop time)
    additionally bounds every statement of a transaction.
    """

    def __init__(
        self,
        query_timeout: datetime.timedelta | None,
        transaction_deadline: float | None = None,
    ) -> None:
        self._query_timeout = query_timeout
        self._transaction_deadline = transaction_deadline

    def deadline(self, timeout: datetime.timedelta | None) -> Deadline:
        """The deadline of a statement with ``timeout`` that starts now.

        Raises:
            InvalidQueryError: If ``timeout`` is zero or negative.
        """
        seconds = positive_seconds(
            self._query_timeout if timeout is None else timeout, "timeout"
        )
        at = None if seconds is None else asyncio.get_running_loop().time() + seconds
        limit = self._transaction_deadline
        if limit is not None and (at is None or limit <= at):
            return Deadline(limit, transaction=True)
        return Deadline(at)

    async def fetch(
        self,
        conn: Connection,
        statement: Statement,
        deadline: Deadline,
        *,
        kind: OpKind = OpKind.QUERY,
        message: str = "query failed",
    ) -> PostgreSQLResult:
        """Run a query and return all its rows; ``kind`` and ``message`` select
        the error for failures without a specific mapping."""
        records: list[Any] = await self._run(
            lambda: conn.fetch(statement.sql, *statement.parameters),
            statement,
            deadline,
            message,
            kind,
        )
        return PostgreSQLResult(records)

    async def fetch_one(
        self, conn: Connection, statement: Statement, deadline: Deadline
    ) -> PostgreSQLRow | None:
        """Run a query and return its first row, or ``None``."""
        record: Any = await self._run(
            lambda: conn.fetchrow(statement.sql, *statement.parameters),
            statement,
            deadline,
            "query failed",
        )
        return None if record is None else PostgreSQLRow(record)

    async def execute(
        self, conn: Connection, statement: Statement, deadline: Deadline
    ) -> ExecuteResult:
        """Run a command. Without parameters it may hold several statements; the
        row count is that of the last one."""
        status: str = await self._run(
            lambda: conn.execute(statement.sql, *statement.parameters),
            statement,
            deadline,
            "command failed",
        )
        # PostgreSQL has no integer insert IDs; use INSERT ... RETURNING instead.
        return ExecuteResult(rows_affected=rows_affected(status))

    async def batch(
        self,
        conn: Connection,
        operations: Sequence[Operation],
        *,
        stop_at_first_error: bool,
    ) -> list[OperationResult]:
        """Run ``operations`` in order, each with its own timeout.

        A failed operation's result carries its error. With
        ``stop_at_first_error`` (inside a transaction, which PostgreSQL aborts
        on an error) the operations after it are not run; their results carry a
        ``QueryFailedError`` caused by that error. A ``TransactionTimeoutError``
        is raised, not recorded.

        Raises:
            InvalidQueryError: Before running anything, if an operation is
                invalid (no query or command, empty SQL or a timeout that is
                not positive).
        """
        statements = [_operation_statement(i, op) for i, op in enumerate(operations)]
        results: list[OperationResult] = []
        failure: tuple[int, StorageError] | None = None
        for index, (kind, statement) in enumerate(statements):
            if failure is not None:
                failed, cause = failure
                results.append(
                    OperationResult(
                        index=index,
                        error=QueryFailedError(
                            f"batch operation {index} was not run because "
                            f"operation {failed} failed",
                            cause=cause,
                            context={"operation": index},
                        ),
                    )
                )
                continue
            deadline = self.deadline(statement.timeout)
            try:
                result: PostgreSQLResult | ExecuteResult
                if kind is OperationType.QUERY:
                    result = await self.fetch(conn, statement, deadline)
                else:
                    result = await self.execute(conn, statement, deadline)
            except TransactionTimeoutError:
                raise
            except StorageError as exc:
                results.append(
                    OperationResult(
                        index=index, error=exc.with_context("operation", index)
                    )
                )
                if stop_at_first_error:
                    failure = (index, exc)
                continue
            results.append(OperationResult(index=index, result=result))
        return results

    async def _run(
        self,
        operation: Callable[[], Awaitable[T]],
        statement: Statement,
        deadline: Deadline,
        message: str,
        kind: OpKind = OpKind.QUERY,
    ) -> T:
        with translate_errors(kind, message, statement.sql):
            try:
                async with asyncio.timeout_at(deadline.at):
                    return await operation()
            except TimeoutError as exc:
                raise deadline.timeout_error(statement.sql) from exc


def _operation_statement(
    index: int, operation: Operation
) -> tuple[OperationType, Statement]:
    item: Query | Command | None
    if operation.operation_type is OperationType.QUERY:
        item = operation.query
    elif operation.operation_type is OperationType.COMMAND:
        item = operation.command
    else:
        raise InvalidQueryError(
            f"batch operation {index} has an unknown type", context={"operation": index}
        )
    if item is None:
        raise InvalidQueryError(
            f"batch operation {index} has no {operation.operation_type.value}",
            context={"operation": index},
        )
    try:
        positive_seconds(item.timeout, "timeout")
        return operation.operation_type, Statement.of(item)
    except InvalidQueryError as exc:
        exc.with_context("operation", index)
        raise
