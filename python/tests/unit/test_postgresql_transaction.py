"""Unit tests for PostgreSQL transactions and the storage's use of its pool,
with a fake pool and connection (no database)."""

from __future__ import annotations

import asyncio
import datetime
from typing import Any

import asyncpg
import pytest

from storage.adapters.postgresql import PostgreSQLStorage
from storage.adapters.postgresql.transaction import (
    PostgreSQLTransaction,
    begin_statement,
)
from storage.errors import (
    ConnectionClosedError,
    DuplicateKeyError,
    InvalidParametersError,
    InvalidQueryError,
    InvalidTransactionError,
    QueryFailedError,
    QueryTimeoutError,
    TransactionClosedError,
    TransactionFailedError,
    TransactionTimeoutError,
    is_retryable,
)
from storage.types import (
    Command,
    Config,
    HealthStatusType,
    IsolationLevel,
    Operation,
    OperationType,
    Query,
    TxOptions,
)

MS = datetime.timedelta(milliseconds=1)


class FakeConnection:
    """Records statements; ``fail`` maps SQL to the exception it raises,
    ``status`` to its command tag and ``delay`` to seconds it takes."""

    def __init__(
        self,
        *,
        fail: dict[str, BaseException] | None = None,
        status: dict[str, str] | None = None,
        delay: dict[str, float] | None = None,
    ) -> None:
        self.statements: list[str] = []
        self.terminated = False
        self.running = 0
        self.max_running = 0
        self._fail = fail or {}
        self._status = status or {}
        self._delay = delay or {}

    async def _run(self, sql: str) -> str:
        self.statements.append(sql)
        self.running += 1
        self.max_running = max(self.max_running, self.running)
        try:
            await asyncio.sleep(self._delay.get(sql, 0))
            if sql in self._fail:
                raise self._fail[sql]
            return self._status.get(sql, "OK 1")
        finally:
            self.running -= 1

    async def execute(self, sql: str, *args: Any) -> str:
        return await self._run(sql)

    async def fetch(self, sql: str, *args: Any) -> list[Any]:
        await self._run(sql)
        return []

    async def fetchrow(self, sql: str, *args: Any) -> Any:
        await self._run(sql)
        return None

    def terminate(self) -> None:
        self.terminated = True


class FakePool:
    def __init__(self, connection: FakeConnection, acquire_delay: float = 0) -> None:
        self.connection = connection
        self.acquire_delay = acquire_delay
        self.released: list[FakeConnection] = []

    async def acquire(self) -> FakeConnection:
        await asyncio.sleep(self.acquire_delay)
        return self.connection

    async def release(self, connection: FakeConnection) -> None:
        self.released.append(connection)


def _storage(pool: FakePool, **config: Any) -> PostgreSQLStorage:
    storage = PostgreSQLStorage(Config(**config))
    storage._pool = pool  # an open storage on a fake pool
    return storage


def _transaction(
    connection: FakeConnection,
    pool: FakePool,
    deadline_in: float | None = None,
    query_timeout: datetime.timedelta | None = None,
) -> PostgreSQLTransaction:
    deadline = None
    if deadline_in is not None:
        deadline = asyncio.get_running_loop().time() + deadline_in
    return PostgreSQLTransaction(
        connection, pool.release, query_timeout=query_timeout, deadline=deadline
    )


def _insert(value: str) -> Operation:
    return Operation(
        OperationType.COMMAND, command=Command(sql=value, parameters=[value])
    )


@pytest.mark.parametrize(
    ("options", "expected"),
    [
        (TxOptions(), "BEGIN"),
        (
            TxOptions(IsolationLevel.READ_UNCOMMITTED),
            "BEGIN ISOLATION LEVEL READ UNCOMMITTED",
        ),
        (
            TxOptions(IsolationLevel.READ_COMMITTED),
            "BEGIN ISOLATION LEVEL READ COMMITTED",
        ),
        (
            TxOptions(IsolationLevel.REPEATABLE_READ),
            "BEGIN ISOLATION LEVEL REPEATABLE READ",
        ),
        (
            TxOptions(IsolationLevel.SERIALIZABLE, read_only=True),
            "BEGIN ISOLATION LEVEL SERIALIZABLE READ ONLY",
        ),
        (TxOptions(read_only=True), "BEGIN READ ONLY"),
    ],
)
def test_begin_statement(options: TxOptions, expected: str) -> None:
    assert begin_statement(options) == expected


def test_begin_statement_rejects_unknown_isolation_levels() -> None:
    with pytest.raises(InvalidTransactionError):
        begin_statement(TxOptions(isolation="chaos"))  # type: ignore[arg-type]


async def test_async_with_begins_once_and_commits() -> None:
    connection = FakeConnection()
    pool = FakePool(connection)
    storage = _storage(pool)
    async with await storage.begin_tx() as tx:
        await tx.execute(Command(sql="INSERT 1"))
    assert connection.statements == ["BEGIN", "INSERT 1", "COMMIT"]
    assert not tx.is_active()
    assert pool.released == [connection]
    assert tx.get_id().startswith("tx_")


async def test_async_with_rolls_back_on_exception() -> None:
    connection = FakeConnection()
    pool = FakePool(connection)
    with pytest.raises(ValueError, match="boom"):
        async with await _storage(pool).begin_tx():
            raise ValueError("boom")
    assert connection.statements == ["BEGIN", "ROLLBACK"]
    assert pool.released == [connection]


async def test_failed_begin_gives_the_connection_back() -> None:
    connection = FakeConnection(fail={"BEGIN": asyncpg.exceptions.InterfaceError("x")})
    pool = FakePool(connection)
    with pytest.raises(TransactionFailedError):
        await _storage(pool).begin_tx()
    assert pool.released == [connection]
    assert not connection.terminated


async def test_begin_that_times_out_closes_the_connection() -> None:
    connection = FakeConnection(delay={"BEGIN": 1})
    pool = FakePool(connection)
    with pytest.raises(TransactionTimeoutError):
        await _storage(pool).begin_tx(TxOptions(timeout=50 * MS))
    assert pool.released == [connection]
    assert connection.terminated


async def test_waiting_for_a_connection_counts_against_the_transaction_timeout() -> (
    None
):
    pool = FakePool(FakeConnection(), acquire_delay=1)
    with pytest.raises(TransactionTimeoutError):
        await _storage(pool).begin_tx(TxOptions(timeout=50 * MS))


async def test_invalid_timeouts_are_rejected() -> None:
    storage = _storage(FakePool(FakeConnection()))
    with pytest.raises(InvalidTransactionError):
        await storage.begin_tx(TxOptions(timeout=datetime.timedelta(0)))
    with pytest.raises(InvalidQueryError):
        await storage.query(Query(sql="SELECT 1", timeout=-MS))


async def test_commit_reports_a_rollback_by_postgresql() -> None:
    connection = FakeConnection(status={"COMMIT": "ROLLBACK"})
    pool = FakePool(connection)
    tx = _transaction(connection, pool)
    with pytest.raises(TransactionFailedError, match="rolled back"):
        await tx.commit()
    assert not tx.is_active()
    assert pool.released == [connection]


async def test_a_failed_commit_ends_the_transaction() -> None:
    error = asyncpg.exceptions.SerializationError("could not serialize")
    connection = FakeConnection(fail={"COMMIT": error})
    pool = FakePool(connection)
    tx = _transaction(connection, pool)
    with pytest.raises(TransactionFailedError) as caught:
        await tx.commit()
    assert is_retryable(caught.value)
    assert caught.value.__cause__ is error
    assert not tx.is_active()
    assert pool.released == [connection]
    await tx.rollback()  # a no-op now
    assert connection.statements == ["COMMIT"]


async def test_a_failed_rollback_ends_the_transaction_and_closes_the_connection() -> (
    None
):
    connection = FakeConnection(fail={"ROLLBACK": OSError("connection reset")})
    pool = FakePool(connection)
    tx = _transaction(connection, pool)
    with pytest.raises(TransactionFailedError):
        await tx.rollback()
    assert not tx.is_active()
    assert connection.terminated
    assert pool.released == [connection]


async def test_an_ended_transaction_refuses_work_and_rollback_is_a_no_op() -> None:
    connection = FakeConnection()
    pool = FakePool(connection)
    tx = _transaction(connection, pool)
    await tx.commit()
    await tx.rollback()
    for attempt in (
        tx.commit(),
        tx.query(Query(sql="SELECT 1")),
        tx.query_one(Query(sql="SELECT 1")),
        tx.execute(Command(sql="DELETE")),
        tx.batch([]),
        tx.savepoint("sp"),
        tx.rollback_to_savepoint("sp"),
    ):
        with pytest.raises(TransactionClosedError):
            await attempt
    assert connection.statements == ["COMMIT"]
    assert pool.released == [connection]


async def test_savepoint_names_are_quoted() -> None:
    connection = FakeConnection()
    tx = _transaction(connection, FakePool(connection))
    await tx.savepoint("sp1; DROP TABLE victims")
    await tx.rollback_to_savepoint('a"b')
    assert connection.statements == [
        'SAVEPOINT "sp1; DROP TABLE victims"',
        'ROLLBACK TO SAVEPOINT "a""b"',
    ]
    for name in ("", "x\x00", "n" * 64):
        with pytest.raises(InvalidParametersError):
            await tx.savepoint(name)
    assert tx.is_active()


async def test_an_expired_transaction_is_rolled_back_by_closing_its_connection() -> (
    None
):
    connection = FakeConnection()
    pool = FakePool(connection)
    tx = _transaction(connection, pool, deadline_in=-1)
    with pytest.raises(TransactionTimeoutError):
        await tx.execute(Command(sql="INSERT 1"))
    assert not tx.is_active()
    assert connection.terminated
    assert pool.released == [connection]
    assert connection.statements == []


async def test_commit_after_the_deadline_fails() -> None:
    connection = FakeConnection()
    pool = FakePool(connection)
    tx = _transaction(connection, pool, deadline_in=-1)
    with pytest.raises(TransactionTimeoutError):
        await tx.commit()
    assert connection.terminated
    assert connection.statements == []


async def test_rollback_after_the_deadline_closes_the_connection_quietly() -> None:
    connection = FakeConnection()
    pool = FakePool(connection)
    tx = _transaction(connection, pool, deadline_in=-1)
    await tx.rollback()
    assert connection.terminated
    assert pool.released == [connection]


async def test_a_statement_cut_off_by_the_deadline_ends_the_transaction() -> None:
    connection = FakeConnection(delay={"SELECT pg_sleep(1)": 1})
    pool = FakePool(connection)
    tx = _transaction(connection, pool, deadline_in=0.05)
    with pytest.raises(TransactionTimeoutError):
        await tx.query(Query(sql="SELECT pg_sleep(1)"))
    assert not tx.is_active()
    assert connection.terminated


async def test_a_statement_timeout_inside_a_transaction_keeps_it_open() -> None:
    connection = FakeConnection(delay={"SELECT pg_sleep(1)": 1})
    tx = _transaction(connection, FakePool(connection), deadline_in=10)
    with pytest.raises(QueryTimeoutError) as caught:
        await tx.query(Query(sql="SELECT pg_sleep(1)", timeout=50 * MS))
    assert caught.value.context["query"] == "SELECT pg_sleep(1)"
    assert tx.is_active()
    assert not connection.terminated


async def test_commit_or_rollback_cut_off_by_the_deadline() -> None:
    connection = FakeConnection(delay={"COMMIT": 1})
    tx = _transaction(connection, FakePool(connection), deadline_in=0.05)
    with pytest.raises(TransactionTimeoutError):
        await tx.commit()
    assert connection.terminated
    slow_rollback = FakeConnection(delay={"ROLLBACK": 1})
    tx = _transaction(slow_rollback, FakePool(slow_rollback), deadline_in=0.05)
    await tx.rollback()  # closing the connection rolled it back
    assert slow_rollback.terminated
    assert not tx.is_active()


async def test_transaction_operations_run_one_at_a_time() -> None:
    connection = FakeConnection(delay={"A": 0.02, "B": 0.02})
    tx = _transaction(connection, FakePool(connection))
    await asyncio.gather(
        tx.execute(Command(sql="A")),
        tx.execute(Command(sql="B")),
        tx.query(Query(sql="A")),
    )
    assert connection.max_running == 1


async def test_transaction_batch_stops_at_the_first_failure() -> None:
    duplicate = asyncpg.exceptions.UniqueViolationError("duplicate")
    connection = FakeConnection(fail={"second": duplicate})
    tx = _transaction(connection, FakePool(connection))
    results = await tx.batch([_insert("first"), _insert("second"), _insert("third")])
    assert [r.index for r in results] == [0, 1, 2]
    assert results[0].error is None
    assert isinstance(results[1].error, DuplicateKeyError)
    assert results[1].error.context["operation"] == 1
    skipped = results[2].error
    assert isinstance(skipped, QueryFailedError)
    assert skipped.__cause__ is results[1].error
    assert connection.statements == ["first", "second"]
    assert tx.is_active()


async def test_storage_batch_runs_every_operation() -> None:
    duplicate = asyncpg.exceptions.UniqueViolationError("duplicate")
    connection = FakeConnection(fail={"second": duplicate})
    pool = FakePool(connection)
    results = await _storage(pool).batch(
        [
            _insert("first"),
            _insert("second"),
            Operation(OperationType.QUERY, query=Query(sql="third")),
        ]
    )
    assert results[0].error is None
    assert isinstance(results[1].error, DuplicateKeyError)
    assert results[2].error is None
    assert connection.statements == ["first", "second", "third"]
    assert pool.released == [connection]
    assert await _storage(pool).batch([]) == []


@pytest.mark.parametrize(
    "operation",
    [
        Operation(OperationType.QUERY),
        Operation(OperationType.COMMAND),
        Operation(OperationType.QUERY, query=Query(sql=" ")),
        Operation(OperationType.QUERY, query=Query(sql="SELECT 1", timeout=-MS)),
        Operation("other"),  # type: ignore[arg-type]
    ],
)
async def test_invalid_batch_operations_run_nothing(operation: Operation) -> None:
    connection = FakeConnection()
    tx = _transaction(connection, FakePool(connection))
    with pytest.raises(InvalidQueryError) as caught:
        await tx.batch([_insert("first"), operation])
    assert caught.value.context["operation"] == 1
    assert connection.statements == []


async def test_waiting_for_a_connection_counts_against_the_query_timeout() -> None:
    pool = FakePool(FakeConnection(), acquire_delay=1)
    with pytest.raises(QueryTimeoutError) as caught:
        await _storage(pool).query(Query(sql="SELECT 1", timeout=50 * MS))
    assert caught.value.context["query"] == "SELECT 1"


async def test_query_timeout_applies_by_default() -> None:
    connection = FakeConnection(delay={"SELECT 1": 1})
    storage = _storage(FakePool(connection), query_timeout=50 * MS)
    with pytest.raises(QueryTimeoutError):
        await storage.execute(Command(sql="SELECT 1"))
    with pytest.raises(QueryTimeoutError):
        await storage.query_one(Query(sql="SELECT 1"))


async def test_a_storage_that_is_not_connected_or_closed_refuses_work() -> None:
    storage = PostgreSQLStorage(Config(host="db", database="d", username="u"))
    with pytest.raises(ConnectionClosedError, match="not connected"):
        await storage.query(Query(sql="SELECT 1"))
    health = await storage.health()
    assert health.status is HealthStatusType.UNHEALTHY
    assert health.details == {"error": "CONNECTION_CLOSED"}
    await storage.close()
    await storage.close()
    for attempt in (
        storage.connect(),
        storage.ping(),
        storage.begin_tx(),
        storage.execute(Command(sql="SELECT 1")),
        storage.list_tables(),
    ):
        with pytest.raises(ConnectionClosedError, match="closed"):
            await attempt
