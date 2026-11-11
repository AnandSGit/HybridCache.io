"""
PostgreSQL transactions.

``PostgreSQLStorage.begin_tx`` takes a connection from the pool and runs
``BEGIN`` once, with the isolation level and access mode of ``TxOptions``; the
transaction gives the connection back to the pool when it ends. Leaving
``async with`` commits or rolls back (see ``storage.interfaces.Transaction``).

When ``TxOptions.timeout`` (else ``Config.tx_timeout``) is set, the transaction
must end within it: each statement is bounded by the time left, and once it is
used up statements and ``commit`` raise ``TransactionTimeoutError`` and the
transaction is rolled back by closing its connection.
"""

from __future__ import annotations

import asyncio
import datetime
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager

from storage.adapters.postgresql.errors import OpKind, translate_errors
from storage.adapters.postgresql.executor import Connection, Executor, Statement
from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.adapters.postgresql.sql import quote_identifier
from storage.errors import (
    InvalidParametersError,
    InvalidTransactionError,
    StorageTimeoutError,
    TransactionClosedError,
    TransactionFailedError,
    TransactionTimeoutError,
)
from storage.interfaces import Transaction
from storage.types import (
    Command,
    ExecuteResult,
    IsolationLevel,
    Operation,
    OperationResult,
    Query,
    TxOptions,
)

_ISOLATION_CLAUSES = {
    IsolationLevel.DEFAULT: "",
    IsolationLevel.READ_UNCOMMITTED: " ISOLATION LEVEL READ UNCOMMITTED",
    IsolationLevel.READ_COMMITTED: " ISOLATION LEVEL READ COMMITTED",
    IsolationLevel.REPEATABLE_READ: " ISOLATION LEVEL REPEATABLE READ",
    IsolationLevel.SERIALIZABLE: " ISOLATION LEVEL SERIALIZABLE",
}


def begin_statement(options: TxOptions) -> str:
    """The ``BEGIN`` statement for ``options``; ``IsolationLevel.DEFAULT`` uses
    the session's default isolation level.

    Raises:
        InvalidTransactionError: For an unknown isolation level.
    """
    clause = _ISOLATION_CLAUSES.get(options.isolation)
    if clause is None:
        raise InvalidTransactionError(f"unknown isolation level {options.isolation!r}")
    return "BEGIN" + clause + (" READ ONLY" if options.read_only else "")


class PostgreSQLTransaction(Transaction):
    """A transaction on one pooled connection; created by ``begin_tx``.

    Operations on one transaction run one at a time. A failed query or command
    raises the same error as outside a transaction (a ``QueryError``, or a data
    or schema error), not a ``TransactionError``. PostgreSQL aborts the
    transaction on any error: later statements raise ``TransactionFailedError``,
    and so does ``commit``, as PostgreSQL rolls the transaction back.
    """

    def __init__(
        self,
        connection: Connection,
        release: Callable[[Connection], Awaitable[None]],
        *,
        query_timeout: datetime.timedelta | None,
        deadline: float | None,
    ) -> None:
        """Wrap ``connection``, on which ``BEGIN`` has run; ``release`` gives it
        back to its pool. ``deadline`` (event-loop time) is when the
        transaction times out."""
        self._connection: Connection = connection
        self._release = release
        self._deadline = deadline
        self._executor = Executor(query_timeout, deadline)
        self._active = True
        self._id = "tx_" + uuid.uuid4().hex
        self._lock = asyncio.Lock()

    async def query(self, query: Query) -> PostgreSQLResult:
        """Execute a query within the transaction."""
        statement = Statement.of(query)
        async with self._using() as conn:
            deadline = self._executor.deadline(statement.timeout)
            return await self._executor.fetch(conn, statement, deadline)

    async def query_one(self, query: Query) -> PostgreSQLRow | None:
        """Execute a query and return its first row, or ``None``."""
        statement = Statement.of(query)
        async with self._using() as conn:
            deadline = self._executor.deadline(statement.timeout)
            return await self._executor.fetch_one(conn, statement, deadline)

    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command within the transaction."""
        statement = Statement.of(command)
        async with self._using() as conn:
            deadline = self._executor.deadline(statement.timeout)
            return await self._executor.execute(conn, statement, deadline)

    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        """Execute operations in order within the transaction.

        The first failed operation aborts the transaction: its result carries its
        error, the operations after it are not run (their results carry a
        ``QueryFailedError``), and ``commit`` raises ``TransactionFailedError``.

        Raises:
            TransactionClosedError: If the transaction is not active.
            InvalidQueryError: Before running anything, for an invalid operation.
        """
        async with self._using() as conn:
            return await self._executor.batch(
                conn, operations, stop_at_first_error=True
            )

    async def commit(self) -> None:
        """Commit the transaction; it is over afterwards, whatever the outcome.

        Raises:
            TransactionClosedError: If the transaction is not active.
            TransactionFailedError: If PostgreSQL rolled the transaction back
                because a statement had failed, or the commit failed (a
                serialization failure is retryable).
            TransactionTimeoutError: If the transaction's time is up; it is
                rolled back.
        """
        async with self._lock:
            self._check_active()
            if self._expired():
                await self._end(terminate=True)
                raise TransactionTimeoutError()
            try:
                status = await self._control("COMMIT", "commit failed")
            except BaseException as exc:
                await self._end(terminate=isinstance(exc, StorageTimeoutError))
                raise
            await self._end()
        if status == "ROLLBACK":
            raise TransactionFailedError(
                "transaction was rolled back because an earlier statement failed"
            )

    async def rollback(self) -> None:
        """Roll back the transaction; a no-op when it is no longer active.

        Once the transaction's time is up, it is rolled back by closing its
        connection instead.

        Raises:
            TransactionFailedError: If the rollback fails; the connection is
                closed, which also rolls the transaction back.
        """
        async with self._lock:
            if not self._active:
                return
            if self._expired():
                await self._end(terminate=True)
                return
            try:
                await self._control("ROLLBACK", "rollback failed")
            except TransactionTimeoutError:
                await self._end(terminate=True)
                return
            except BaseException:
                await self._end(terminate=True)
                raise
            await self._end()

    async def savepoint(self, name: str) -> None:
        """Create a savepoint; ``name`` is quoted as an identifier.

        Raises:
            InvalidParametersError: If ``name`` is empty, contains NUL or is
                longer than 63 bytes.
        """
        await self._savepoint_command("SAVEPOINT", name, "savepoint failed")

    async def rollback_to_savepoint(self, name: str) -> None:
        """Roll back to a savepoint, which stays defined.

        Raises:
            InvalidParametersError: For an invalid name.
            TransactionFailedError: If the savepoint does not exist (PostgreSQL
                then aborts the transaction).
        """
        await self._savepoint_command(
            "ROLLBACK TO SAVEPOINT", name, "rollback to savepoint failed"
        )

    def is_active(self) -> bool:
        """Whether the transaction is still open."""
        return self._active

    def get_id(self) -> str:
        """Return the transaction ID."""
        return self._id

    async def _savepoint_command(self, command: str, name: str, message: str) -> None:
        async with self._using():
            savepoint = quote_identifier(name, "savepoint", InvalidParametersError)
            await self._control(f"{command} {savepoint}", message)

    @asynccontextmanager
    async def _using(self) -> AsyncIterator[Connection]:
        """Hold the transaction for one operation; a timeout ends it."""
        async with self._lock:
            self._check_active()
            if self._expired():
                await self._end(terminate=True)
                raise TransactionTimeoutError()
            try:
                yield self._connection
            except TransactionTimeoutError:
                await self._end(terminate=True)
                raise

    async def _control(self, sql: str, message: str) -> str:
        """Run a transaction control statement, bounded by the deadline."""
        with translate_errors(OpKind.TRANSACTION, message, sql):
            async with asyncio.timeout_at(self._deadline):
                status: str = await self._connection.execute(sql)
        return status

    def _check_active(self) -> None:
        if not self._active:
            raise TransactionClosedError()

    def _expired(self) -> bool:
        return (
            self._deadline is not None
            and asyncio.get_running_loop().time() >= self._deadline
        )

    async def _end(self, *, terminate: bool = False) -> None:
        """End the transaction and give its connection back to the pool; closing
        the connection (``terminate``) rolls back whatever is still open."""
        self._active = False
        connection, self._connection = self._connection, None
        if connection is None:
            return
        if terminate:
            connection.terminate()
        await self._release(connection)
