"""
Integration tests of the MongoDB adapter against a live server.

They need ``MONGODB_URI``, a replica set for transactions (e.g.
``mongodb://127.0.0.1:57017/?replicaSet=rs0``), and are skipped without it. The
database is the URI's, else ``hc_py_mongo``; every test uses collections of its
own and drops them.
"""

from __future__ import annotations

import asyncio
import dataclasses
import datetime
import os
import time
import uuid
from collections.abc import AsyncIterator, Callable
from typing import Any

import pytest

from storage.adapters.mongodb import MongoDBAdapter, MongoDBStorage, MongoDBTransaction
from storage.errors import (
    ConnectionClosedError,
    ConnectionFailedError,
    ConnectionTimeoutError,
    DuplicateKeyError,
    IndexNotFoundError,
    InvalidQueryError,
    InvalidTransactionError,
    QueryTimeoutError,
    SchemaError,
    SchemaExistsError,
    TableNotFoundError,
    TransactionClosedError,
    TransactionFailedError,
    TransactionTimeoutError,
    UnsupportedOperationError,
    UnsupportedQueryError,
    is_connection_error,
    is_retryable,
    is_timeout_error,
)
from storage.interfaces import Result
from storage.types import (
    ColumnDefinition,
    ColumnRef,
    Command,
    CommandSpec,
    CommandType,
    Condition,
    Config,
    DataType,
    ExecuteResult,
    FieldValue,
    HealthStatusType,
    IndexDefinition,
    IndexType,
    IsolationLevel,
    Join,
    JoinType,
    Operation,
    OperationType,
    Operator,
    OrderBy,
    Query,
    QuerySpec,
    SchemaChange,
    SchemaChangeType,
    SortDirection,
    TableSchema,
    TxOptions,
)

MONGODB_URI = os.environ.get("MONGODB_URI", "")
DEFAULT_DATABASE = "hc_py_mongo"

pytestmark = [
    pytest.mark.integration,
    pytest.mark.skipif(not MONGODB_URI, reason="MONGODB_URI is not set"),
]

MICROSECOND = datetime.timedelta(microseconds=1)


def integration_config(**overrides: Any) -> Config:
    config = MongoDBAdapter().parse_dsn(MONGODB_URI)
    changes = {"database": config.database or DEFAULT_DATABASE, **overrides}
    return dataclasses.replace(config, **changes)


# Queries and commands as the query builder builds them.


def where(field: str, operator: Operator, value: Any = None, *values: Any) -> Condition:
    return Condition(field, operator, value, list(values) if values else None)


def select(
    table: str,
    *conditions: Condition,
    fields: tuple[str, ...] = (),
    distinct: bool = False,
    order_by: tuple[OrderBy, ...] = (),
    limit: int | None = None,
    offset: int | None = None,
    timeout: datetime.timedelta | None = None,
) -> Query:
    spec = QuerySpec(
        table=table,
        fields=list(fields),
        distinct=distinct,
        where=list(conditions),
        order_by=list(order_by),
        limit=limit,
        offset=offset,
    )
    return Query(spec=spec, timeout=timeout)


def count(table: str, *conditions: Condition, field: str = "*") -> Query:
    return select(table, *conditions, fields=(f"COUNT({field})",))


def insert(table: str, **values: Any) -> Command:
    spec = CommandSpec(table, [FieldValue(k, v) for k, v in values.items()])
    return Command(command_type=CommandType.INSERT, spec=spec)


def update(table: str, values: dict[str, Any], *conditions: Condition) -> Command:
    assignments = [FieldValue(k, v) for k, v in values.items()]
    spec = CommandSpec(table, assignments, list(conditions))
    return Command(command_type=CommandType.UPDATE, spec=spec)


def delete(table: str, *conditions: Condition) -> Command:
    spec = CommandSpec(table, [], list(conditions))
    return Command(command_type=CommandType.DELETE, spec=spec)


BY_ID = (OrderBy("id"),)


async def ids(
    source: MongoDBStorage | MongoDBTransaction, table: str, *conditions: Condition
) -> list[int]:
    rows = await (
        await source.query(select(table, *conditions, order_by=BY_ID))
    ).fetchall()
    return [row.get("id") for row in rows]


async def counted(source: MongoDBStorage | MongoDBTransaction, query: Query) -> int:
    row = await source.query_one(query)
    assert row is not None
    value: int = row.get("count")
    return value


@pytest.fixture
async def storage() -> AsyncIterator[MongoDBStorage]:
    connected = await MongoDBAdapter().connect(integration_config())
    yield connected
    await connected.close()


@pytest.fixture
async def tables(storage: MongoDBStorage) -> AsyncIterator[Callable[[], str]]:
    """Return a factory of fresh collection names; they are dropped afterwards."""
    names: list[str] = []

    def new() -> str:
        names.append(f"t_{uuid.uuid4().hex[:12]}")
        return names[-1]

    yield new
    await storage.connect()  # in case the test closed the storage
    for name in names:
        await storage.drop_table(name)


@pytest.fixture
def table(tables: Callable[[], str]) -> str:
    return tables()


async def seed(storage: MongoDBStorage, table: str, *documents: dict[str, Any]) -> None:
    for document in documents:
        await storage.execute(insert(table, **document))


# Connection


async def test_ping_health_close_and_reconnect(storage: MongoDBStorage) -> None:
    await storage.ping()
    health = await storage.health()
    assert health.status is HealthStatusType.HEALTHY
    assert health.details["version"].split(".")[0].isdigit()

    await storage.close()
    with pytest.raises(ConnectionClosedError):
        await storage.ping()
    assert (await storage.health()).status is HealthStatusType.UNHEALTHY

    await storage.connect()
    await storage.ping()


async def test_connect_timeout_bounds_connect() -> None:
    config = Config(
        host="127.0.0.1",
        port=1,
        database=DEFAULT_DATABASE,
        connect_timeout=datetime.timedelta(milliseconds=300),
    )
    start = time.monotonic()

    with pytest.raises(ConnectionTimeoutError) as caught:
        await MongoDBAdapter().connect(config)

    assert time.monotonic() - start < 5
    assert is_connection_error(caught.value)
    assert is_retryable(caught.value)


async def test_failed_authentication_is_a_connection_failure() -> None:
    parsed = MongoDBAdapter().parse_dsn(MONGODB_URI)
    components = Config(
        host=parsed.host,
        port=parsed.port,
        database=DEFAULT_DATABASE,
        username="hc_nobody",
        password="not-a-password",
        options=parsed.options,
    )

    with pytest.raises(ConnectionFailedError) as caught:
        await MongoDBAdapter().connect(components)

    assert not is_timeout_error(caught.value)


async def test_the_database_named_in_the_dsn_is_used(
    storage: MongoDBStorage, table: str
) -> None:
    base, _, options = MONGODB_URI.partition("?")
    if not MongoDBAdapter().parse_dsn(MONGODB_URI).database:
        base = f"{base.rstrip('/')}/{DEFAULT_DATABASE}"
    dsn = f"{base}?{options}" if options else base
    from_dsn = await MongoDBAdapter().connect(Config(dsn=dsn))
    try:
        await from_dsn.execute(insert(table, id=1))
    finally:
        await from_dsn.close()

    assert await ids(storage, table) == [1]


# Queries and commands


async def test_collections_are_isolated(
    storage: MongoDBStorage, tables: Callable[[], str]
) -> None:
    first, second = tables(), tables()
    await storage.execute(insert(first, id=1))
    await storage.execute(insert(second, id=2))

    assert await ids(storage, first) == [1]
    assert await ids(storage, second) == [2]
    assert {first, second} <= set(await storage.list_tables())


PEOPLE = (
    {"id": 1, "name": "Ann", "age": 30, "city": "Oslo", "address": {"zip": "0150"}},
    {"id": 2, "name": "Bob", "age": 25, "city": None},
    {"id": 3, "name": "Cid", "age": 35},
    {"id": 4, "name": "Dana", "age": 40, "city": "Rome"},
)


@pytest.mark.parametrize(
    ("conditions", "expected"),
    [
        pytest.param([where("name", Operator.EQUAL, "Ann")], [1], id="equal"),
        pytest.param(
            [where("name", Operator.NOT_EQUAL, "Ann")], [2, 3, 4], id="not equal"
        ),
        pytest.param([where("age", Operator.GREATER_THAN, 30)], [3, 4], id="gt"),
        pytest.param(
            [where("age", Operator.GREATER_THAN_OR_EQUAL, 30)], [1, 3, 4], id="gte"
        ),
        pytest.param([where("age", Operator.LESS_THAN, 30)], [2], id="lt"),
        pytest.param([where("age", Operator.LESS_THAN_OR_EQUAL, 30)], [1, 2], id="lte"),
        pytest.param([where("id", Operator.IN, None, 1, 3)], [1, 3], id="in"),
        pytest.param([where("id", Operator.IN)], [], id="in without values"),
        pytest.param([where("id", Operator.NOT_IN, None, 1, 3)], [2, 4], id="not in"),
        pytest.param(
            [where("age", Operator.BETWEEN, None, 30, 35)], [1, 3], id="between"
        ),
        pytest.param(
            [where("age", Operator.NOT_BETWEEN, None, 30, 35)], [2, 4], id="not between"
        ),
        pytest.param([where("name", Operator.LIKE, "_n%")], [1], id="like"),
        pytest.param([where("name", Operator.LIKE, "%a")], [4], id="like suffix"),
        pytest.param(
            [where("name", Operator.LIKE, "ann")], [], id="like is case-sensitive"
        ),
        pytest.param(
            [where("name", Operator.LIKE, "A.n")], [], id="like is not a regex"
        ),
        pytest.param(
            [where("name", Operator.NOT_LIKE, "%a")], [1, 2, 3], id="not like"
        ),
        pytest.param(
            [where("city", Operator.IS_NULL)], [2, 3], id="is null matches missing"
        ),
        pytest.param([where("city", Operator.IS_NOT_NULL)], [1, 4], id="is not null"),
        pytest.param(
            [
                Condition(
                    "",
                    Operator.AND,
                    conditions=[
                        where("age", Operator.GREATER_THAN_OR_EQUAL, 30),
                        where("city", Operator.EQUAL, "Oslo"),
                    ],
                )
            ],
            [1],
            id="and group",
        ),
        pytest.param(
            [
                Condition(
                    "",
                    Operator.OR,
                    conditions=[
                        where("name", Operator.EQUAL, "Bob"),
                        where("age", Operator.GREATER_THAN, 35),
                    ],
                )
            ],
            [2, 4],
            id="or group",
        ),
        pytest.param(
            [
                Condition(
                    "",
                    Operator.OR,
                    conditions=[
                        where("city", Operator.EQUAL, "Oslo"),
                        where("city", Operator.IS_NULL),
                    ],
                ),
                where("age", Operator.LESS_THAN, 35),
            ],
            [1, 2],
            id="group and condition",
        ),
        pytest.param(
            [
                where("age", Operator.GREATER_THAN_OR_EQUAL, 30),
                where("age", Operator.LESS_THAN_OR_EQUAL, 35),
            ],
            [1, 3],
            id="several conditions on one field",
        ),
        pytest.param(
            [where("address.zip", Operator.EQUAL, "0150")], [1], id="dotted path"
        ),
        pytest.param(
            [where("name", Operator.EQUAL, {"$ne": None})],
            [],
            id="operator-like value is a literal",
        ),
    ],
)
async def test_operators(
    storage: MongoDBStorage,
    table: str,
    conditions: list[Condition],
    expected: list[int],
) -> None:
    await seed(storage, table, *PEOPLE)

    assert await ids(storage, table, *conditions) == expected


@pytest.mark.parametrize(
    "rejected",
    [
        pytest.param(Query(sql="SELECT * FROM t"), id="raw SQL"),
        pytest.param(
            Query(spec=QuerySpec(table="t", joins=[Join(JoinType.CROSS, "u")])),
            id="join",
        ),
        pytest.param(Query(spec=QuerySpec(table="t", group_by=["id"])), id="group by"),
        pytest.param(
            Query(
                spec=QuerySpec(table="t", having=[where("n", Operator.GREATER_THAN, 1)])
            ),
            id="having",
        ),
        pytest.param(Query(spec=QuerySpec(subquery=True)), id="subquery"),
        pytest.param(
            select("t", Condition("", Operator.EXISTS, object())), id="exists"
        ),
        pytest.param(
            select("t", where("id", Operator.EQUAL, ColumnRef("other"))),
            id="column reference",
        ),
    ],
)
async def test_sql_only_queries_are_rejected(
    storage: MongoDBStorage, rejected: Query
) -> None:
    with pytest.raises(UnsupportedQueryError):
        await storage.query(rejected)
    with pytest.raises(UnsupportedQueryError):
        await storage.query_one(rejected)


async def test_rejected_commands_change_nothing(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, {"id": 1}, {"id": 2})
    raw = Command(sql="DELETE FROM people", command_type=CommandType.DELETE)
    exists = delete(table, Condition("", Operator.NOT_EXISTS, object()))
    column = update(table, {"id": 0}, where("id", Operator.EQUAL, ColumnRef("x")))

    for rejected in (raw, exists, column):
        with pytest.raises(UnsupportedQueryError):
            await storage.execute(rejected)

    assert await ids(storage, table) == [1, 2]


async def test_update_and_delete_affect_only_matches(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, *({"id": i, "balance": 10} for i in (1, 2, 3)))
    first = where("id", Operator.EQUAL, 1)

    assert (
        await storage.execute(update(table, {"balance": 20}, first))
    ).rows_affected == 1
    unchanged = await storage.execute(update(table, {"balance": 20}, first))
    assert unchanged.rows_affected == 1, "rows_affected counts matched documents"
    none = await storage.execute(
        update(table, {"balance": 0}, where("id", Operator.EQUAL, 9))
    )
    assert none.rows_affected == 0
    rows = await (await storage.query(select(table, order_by=BY_ID))).fetchall()
    assert [row.get("balance") for row in rows] == [20, 10, 10]

    assert (
        await storage.execute(delete(table, where("id", Operator.EQUAL, 2)))
    ).rows_affected == 1
    assert (
        await storage.execute(delete(table, where("id", Operator.EQUAL, 9)))
    ).rows_affected == 0
    assert await ids(storage, table) == [1, 3]


@dataclasses.dataclass
class Person:
    name: str = ""
    age: int = 0


async def test_projection_rows_and_scanning(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(
        storage, table, {"id": 1, "name": "Ann", "age": 30, "address": {"city": "Oslo"}}
    )
    projected = select(table, fields=("name", "address.city", "missing"))

    result = await storage.query(projected)
    assert result.columns() == ["name", "address.city", "missing"]
    rows = await result.fetchall()
    assert [row.items() for row in rows] == [
        [("name", "Ann"), ("address.city", "Oslo"), ("missing", None)]
    ]

    row = await storage.query_one(projected)
    assert row is not None
    name: list[Any] = [None]
    city: list[Any] = [None]
    await row.scan(name, city, [None])
    assert (name, city) == (["Ann"], ["Oslo"])

    whole = await storage.query_one(select(table))
    assert whole is not None
    assert whole.keys()[:4] == ["_id", "id", "name", "age"]
    person = Person()
    await whole.scan_into(person)
    assert person == Person("Ann", 30)


async def test_sort_skip_and_limit(storage: MongoDBStorage, table: str) -> None:
    ages = {1: 30, 2: 20, 3: 40, 4: 20, 5: 50}
    await seed(storage, table, *({"id": i, "age": age} for i, age in ages.items()))
    by_age = (OrderBy("age", SortDirection.DESC), OrderBy("id"))

    async def found(query: Query) -> list[int]:
        return [row.get("id") for row in await (await storage.query(query)).fetchall()]

    assert await found(select(table, order_by=by_age)) == [5, 3, 1, 2, 4]
    assert await found(select(table, order_by=BY_ID, offset=1, limit=2)) == [2, 3]
    assert await found(select(table, order_by=BY_ID, limit=0)) == []
    oldest = await storage.query_one(select(table, order_by=by_age))
    assert oldest is not None
    assert oldest.get("id") == 5
    last = await storage.query_one(select(table, order_by=BY_ID, offset=4))
    assert last is not None
    assert last.get("id") == 5


async def test_count(storage: MongoDBStorage, table: str) -> None:
    await seed(
        storage,
        table,
        {"id": 1, "email": "a@x"},
        {"id": 2, "email": "b@x"},
        {"id": 3, "email": None},
        {"id": 4},
    )
    later = where("id", Operator.GREATER_THAN, 1)

    result = await storage.query(count(table))
    assert result.columns() == ["count"]
    assert [dict(row.items()) for row in await result.fetchall()] == [{"count": 4}]
    assert await counted(storage, count(table, later)) == 3
    assert await counted(storage, count(table, field="email")) == 2, (
        "nulls are not counted"
    )

    found = await storage.query(select(table, later, order_by=BY_ID, offset=1, limit=1))
    assert await found.count() == 1, "count() honours the filter, skip and limit"
    assert await (await storage.query(select(table, later))).count() == 3


async def test_distinct(storage: MongoDBStorage, table: str) -> None:
    await seed(
        storage,
        table,
        {"id": 1, "city": "Oslo", "address": {"zip": "0150"}},
        {"id": 2, "city": "Oslo"},
        {"id": 3, "city": "Rome", "address": {"zip": "00100"}},
        {"id": 4, "city": None},
        {"id": 5},
    )

    cities = await (
        await storage.query(select(table, fields=("city",), distinct=True))
    ).fetchall()
    zips = await (
        await storage.query(select(table, fields=("address.zip",), distinct=True))
    ).fetchall()

    assert sorted(row.get("city") or "" for row in cities) == ["", "Oslo", "Rome"]
    assert sorted(row.get("address.zip") for row in zips) == ["00100", "0150"]
    assert {tuple(row.keys()) for row in zips} == {("address.zip",)}


async def test_query_one_returns_none_without_a_match(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, {"id": 1})

    assert (
        await storage.query_one(select(table, where("id", Operator.EQUAL, 2))) is None
    )
    assert await storage.query_one(select(table, limit=0)) is None


async def test_every_document_is_fetched_once(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, *({"id": i} for i in range(6)))
    result = await storage.query(select(table, fields=("id",), order_by=BY_ID))

    batches = [[row.get("id") for row in await result.fetchmany(2)] for _ in range(4)]

    assert batches == [[0, 1], [2, 3], [4, 5], []]
    iterated = [
        row.get("id")
        async for row in await storage.query(select(table, order_by=BY_ID))
    ]
    assert iterated == list(range(6))


async def test_last_insert_id_is_an_integer_id_or_none(
    storage: MongoDBStorage, table: str
) -> None:
    integer = await storage.execute(insert(table, _id=7, name="Ann"))
    generated = await storage.execute(insert(table, name="Bob"))

    assert integer == ExecuteResult(rows_affected=1, last_insert_id=7)
    assert generated == ExecuteResult(rows_affected=1, last_insert_id=None)


async def test_duplicate_key(storage: MongoDBStorage, table: str) -> None:
    unique = IndexDefinition("by_email", ["email"], unique=True)
    await storage.create_table(TableSchema(table, indexes=[unique]))
    await storage.execute(insert(table, email="a@x"))

    with pytest.raises(DuplicateKeyError):
        await storage.execute(insert(table, email="a@x"))


async def test_value_bson_cannot_encode(storage: MongoDBStorage, table: str) -> None:
    with pytest.raises(InvalidQueryError):
        await storage.execute(insert(table, tags={1, 2}))


# Timeouts


async def test_query_and_command_timeouts(storage: MongoDBStorage, table: str) -> None:
    await seed(storage, table, {"id": 1})
    late_query = select(table, timeout=MICROSECOND)
    late_command = dataclasses.replace(insert(table, id=2), timeout=MICROSECOND)

    with pytest.raises(QueryTimeoutError) as caught:
        await storage.query(late_query)
    assert is_timeout_error(caught.value)
    assert is_retryable(caught.value)
    with pytest.raises(QueryTimeoutError):
        await storage.query_one(late_query)
    with pytest.raises(QueryTimeoutError):
        await storage.execute(late_command)
    assert await ids(storage, table) == [1]


async def test_config_query_timeout_is_the_default(table: str) -> None:
    short = await MongoDBAdapter().connect(
        integration_config(query_timeout=MICROSECOND)
    )
    try:
        with pytest.raises(QueryTimeoutError):
            await short.query_one(count(table))
    finally:
        await short.close()


async def test_reading_a_result_is_bounded_by_its_deadline(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, *({"id": i} for i in range(250)))
    timeout = datetime.timedelta(milliseconds=300)
    result = await storage.query(select(table, fields=("id",), timeout=timeout))
    assert await result.fetchone() is not None
    await asyncio.sleep(0.4)

    read = 1

    async def read_the_rest() -> None:
        nonlocal read
        async for _ in result:
            read += 1

    with pytest.raises(QueryTimeoutError):
        await read_the_rest()

    assert 1 < read < 250, "the received batch stays readable; the next one is not"
    unbounded = await (await storage.query(select(table, fields=("id",)))).fetchall()
    assert len(unbounded) == 250


# Transactions


async def test_transaction_commits_when_the_block_succeeds(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, {"id": 1})

    async with await storage.begin_tx() as tx:
        await tx.execute(insert(table, id=2))
        assert await ids(tx, table) == [1, 2], "the transaction sees its own writes"
        assert await ids(storage, table) == [1], "others do not see uncommitted writes"

    assert not tx.is_active()
    assert await ids(storage, table) == [1, 2]
    with pytest.raises(TransactionClosedError):
        await tx.commit()
    with pytest.raises(TransactionClosedError):
        await tx.query(select(table))


async def test_transaction_rolls_back_when_the_block_raises(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, {"id": 1, "balance": 100})
    tx = await storage.begin_tx(TxOptions(isolation=IsolationLevel.READ_COMMITTED))

    async def failing_block() -> None:
        async with tx:
            await tx.execute(insert(table, id=2))
            await tx.execute(
                update(table, {"balance": 0}, where("id", Operator.EQUAL, 1))
            )
            raise RuntimeError("boom")

    with pytest.raises(RuntimeError, match="boom"):
        await failing_block()

    assert not tx.is_active()
    assert await ids(storage, table) == [1]
    assert (
        await counted(storage, count(table, where("balance", Operator.EQUAL, 100))) == 1
    )


async def test_rollback_is_a_no_op_once_the_transaction_ended(
    storage: MongoDBStorage, table: str
) -> None:
    tx = await storage.begin_tx()
    await tx.execute(insert(table, id=1))

    await tx.rollback()
    await tx.rollback()

    assert not tx.is_active()
    assert await ids(storage, table) == []
    committed = await storage.begin_tx()
    await committed.commit()
    await committed.rollback()


async def test_read_only_transactions_read_from_the_primary_and_reject_writes(
    table: str,
) -> None:
    # Reads in a transaction must use the primary, whatever the client's default
    # read preference is: PyMongo rejects a transaction read sent with another one
    # (and, without a secondary, cannot even select a server for it).
    config = integration_config()
    separator = "&" if "?" in config.dsn else "?"
    secondary = dataclasses.replace(
        config, dsn=f"{config.dsn}{separator}readPreference=secondary"
    )
    storage = await MongoDBAdapter().connect(secondary)
    try:
        await storage.execute(insert(table, id=1))
        tx = await storage.begin_tx(
            TxOptions(read_only=True, isolation=IsolationLevel.SERIALIZABLE)
        )
        assert await ids(tx, table) == [1]
        assert await counted(tx, count(table)) == 1
        with pytest.raises(InvalidTransactionError):
            await tx.execute(insert(table, id=9))
        results = await tx.batch(
            [
                Operation(OperationType.QUERY, query=count(table)),
                Operation(OperationType.COMMAND, command=insert(table, id=9)),
            ]
        )
        assert results[0].error is None
        assert isinstance(results[1].error, InvalidTransactionError)
        await tx.commit()
    finally:
        await storage.close()


@pytest.mark.parametrize("isolation", list(IsolationLevel))
async def test_every_isolation_level(
    storage: MongoDBStorage, table: str, isolation: IsolationLevel
) -> None:
    await seed(storage, table, {"id": 1}, {"id": 2})
    options = TxOptions(isolation=isolation, timeout=datetime.timedelta(seconds=30))

    async with await storage.begin_tx(options) as tx:
        assert await counted(tx, count(table)) == 2
        await tx.execute(
            update(table, {"level": isolation.value}, where("id", Operator.EQUAL, 2))
        )

    assert (
        await counted(
            storage, count(table, where("level", Operator.EQUAL, isolation.value))
        )
        == 1
    )


async def test_savepoints_are_unsupported(storage: MongoDBStorage) -> None:
    async with await storage.begin_tx() as tx:
        with pytest.raises(UnsupportedOperationError):
            await tx.savepoint("sp1")
        with pytest.raises(UnsupportedOperationError):
            await tx.rollback_to_savepoint("sp1")
        assert tx.is_active()


async def test_transaction_ids_are_unique(storage: MongoDBStorage) -> None:
    concurrent = [await storage.begin_tx(), await storage.begin_tx()]
    for tx in concurrent:
        await tx.rollback()
    sequential = []
    for _ in range(3):
        tx = await storage.begin_tx()
        await tx.rollback()
        sequential.append(tx)

    every = [tx.get_id() for tx in concurrent + sequential]
    assert all(every)
    assert len(set(every)) == len(every)


async def test_write_conflicts_are_retryable(
    storage: MongoDBStorage, table: str
) -> None:
    await seed(storage, table, {"id": 1, "balance": 100})
    first, second = await storage.begin_tx(), await storage.begin_tx()
    target = where("id", Operator.EQUAL, 1)
    try:
        await first.execute(update(table, {"balance": 101}, target))
        with pytest.raises(TransactionFailedError) as conflict:
            await second.execute(update(table, {"balance": 102}, target))
        assert is_retryable(conflict.value)

        # The server aborted the second transaction: its commit fails, and it ends.
        with pytest.raises(TransactionFailedError) as commit:
            await second.commit()
        assert is_retryable(commit.value)
        assert not second.is_active()
    finally:
        await first.rollback()
        await second.rollback()


async def test_a_timed_out_transaction_rejects_operations(
    storage: MongoDBStorage, table: str
) -> None:
    tx = await storage.begin_tx(TxOptions(timeout=datetime.timedelta(milliseconds=20)))
    await asyncio.sleep(0.05)

    with pytest.raises(TransactionTimeoutError):
        await tx.execute(insert(table, id=8))
    await tx.rollback()
    assert not tx.is_active()


async def test_commit_after_the_deadline_times_out_and_aborts(
    storage: MongoDBStorage, table: str
) -> None:
    tx = await storage.begin_tx(TxOptions(timeout=datetime.timedelta(milliseconds=300)))
    await tx.execute(insert(table, id=1))
    await asyncio.sleep(0.4)

    with pytest.raises(TransactionTimeoutError):
        await tx.commit()

    assert not tx.is_active()
    assert await ids(storage, table) == []


async def test_config_tx_timeout_is_the_default(table: str) -> None:
    short = await MongoDBAdapter().connect(
        integration_config(tx_timeout=datetime.timedelta(milliseconds=20))
    )
    try:
        tx = await short.begin_tx()
        await asyncio.sleep(0.05)
        with pytest.raises(TransactionTimeoutError):
            await tx.query(select(table))
        await tx.rollback()
    finally:
        await short.close()


async def test_batch_in_a_transaction(storage: MongoDBStorage, table: str) -> None:
    await seed(storage, table, {"id": 1})

    async with await storage.begin_tx() as tx:
        results = await tx.batch(
            [
                Operation(OperationType.COMMAND, command=insert(table, id=2)),
                Operation(OperationType.QUERY, query=count(table)),
            ]
        )

    assert results[0].error is None
    counts = results[1].result
    assert isinstance(counts, Result)
    rows = await counts.fetchall()
    assert [row.get("count") for row in rows] == [2], (
        "the query sees the batch's insert"
    )
    assert await ids(storage, table) == [1, 2]


async def test_batch_runs_in_order_and_reports_each_outcome(
    storage: MongoDBStorage, table: str
) -> None:
    operations = [
        Operation(OperationType.COMMAND, command=insert(table, id=1)),
        Operation(OperationType.QUERY, query=Query(sql="SELECT * FROM people")),
        Operation(OperationType.COMMAND, command=insert(table, id=2)),
        Operation(
            OperationType.QUERY, query=select(table, fields=("id",), order_by=BY_ID)
        ),
        Operation(
            OperationType.COMMAND, command=delete(table, where("id", Operator.EQUAL, 1))
        ),
        Operation(OperationType.QUERY),
    ]

    results = await storage.batch(operations)

    assert [r.index for r in results] == list(range(6))
    assert results[0].result == ExecuteResult(rows_affected=1)
    assert isinstance(results[1].error, UnsupportedQueryError)
    assert results[2].result == ExecuteResult(rows_affected=1)
    found = results[3].result
    assert isinstance(found, Result)
    assert [row.get("id") for row in await found.fetchall()] == [1, 2]
    assert results[4].result == ExecuteResult(rows_affected=1)
    assert isinstance(results[5].error, InvalidQueryError)
    assert await ids(storage, table) == [2]


# Schema


async def test_create_and_describe_table_keeps_index_names_and_order(
    storage: MongoDBStorage, table: str
) -> None:
    schema = TableSchema(
        table,
        columns=[ColumnDefinition("email", DataType.STRING)],
        indexes=[
            IndexDefinition("by_email", ["email"], unique=True),
            IndexDefinition("by_city_age", ["city", "age"]),
            IndexDefinition("", ["name"]),
            IndexDefinition("by_token", ["token"], index_type=IndexType.HASH),
        ],
    )

    await storage.create_table(schema)
    described = await storage.describe_table(table)

    assert described.name == table
    assert described.columns == []
    assert described.indexes == [
        IndexDefinition("by_email", ["email"], unique=True),
        IndexDefinition("by_city_age", ["city", "age"]),
        IndexDefinition("name_1", ["name"]),
        IndexDefinition("by_token", ["token"], index_type=IndexType.HASH),
    ]
    with pytest.raises(SchemaExistsError):
        await storage.create_table(schema)


async def test_alter_table_adds_and_drops_indexes(
    storage: MongoDBStorage, table: str
) -> None:
    await storage.create_table(
        TableSchema(table, indexes=[IndexDefinition("by_city_age", ["city", "age"])])
    )
    add = SchemaChange(
        SchemaChangeType.ADD_INDEX, index=IndexDefinition("by_age", ["age"])
    )
    drop = SchemaChange(
        SchemaChangeType.DROP_INDEX, index=IndexDefinition("by_city_age", [])
    )

    await storage.alter_table(table, [add, drop])

    assert [i.name for i in (await storage.describe_table(table)).indexes] == ["by_age"]
    with pytest.raises(IndexNotFoundError):
        await storage.alter_table(table, [drop])


async def test_alter_table_validates_every_change_first(
    storage: MongoDBStorage, table: str
) -> None:
    await storage.create_table(TableSchema(table))
    add = SchemaChange(
        SchemaChangeType.ADD_INDEX, index=IndexDefinition("by_age", ["age"])
    )
    column = SchemaChange(
        SchemaChangeType.ADD_COLUMN, column=ColumnDefinition("age", DataType.INTEGER)
    )

    with pytest.raises(UnsupportedOperationError):
        await storage.alter_table(table, [add, column])

    assert (await storage.describe_table(table)).indexes == []


@pytest.mark.parametrize(
    "index",
    [
        pytest.param(
            IndexDefinition("gin", ["tags"], index_type=IndexType.GIN), id="GIN"
        ),
        pytest.param(
            IndexDefinition("partial", ["age"], condition="age > 18"),
            id="partial index",
        ),
    ],
)
async def test_unsupported_indexes_create_nothing(
    storage: MongoDBStorage, table: str, index: IndexDefinition
) -> None:
    with pytest.raises(UnsupportedOperationError):
        await storage.create_table(TableSchema(table, indexes=[index]))

    assert table not in await storage.list_tables()


async def test_failed_index_creation_leaves_no_table(
    storage: MongoDBStorage, table: str
) -> None:
    hashed_unique = IndexDefinition(
        "bad", ["a"], unique=True, index_type=IndexType.HASH
    )

    with pytest.raises(SchemaError) as caught:
        await storage.create_table(TableSchema(table, indexes=[hashed_unique]))

    assert caught.value.code == "CREATE_INDEX_FAILED"
    assert table not in await storage.list_tables()


async def test_missing_tables(storage: MongoDBStorage, table: str) -> None:
    add = SchemaChange(SchemaChangeType.ADD_INDEX, index=IndexDefinition("i", ["a"]))

    with pytest.raises(TableNotFoundError):
        await storage.describe_table(table)
    with pytest.raises(TableNotFoundError):
        await storage.alter_table(table, [add])
    assert table not in await storage.list_tables(), "nothing was created implicitly"


async def test_drop_table(storage: MongoDBStorage, table: str) -> None:
    await storage.create_table(TableSchema(table))
    assert table in await storage.list_tables()

    await storage.drop_table(table)
    await storage.drop_table(table)

    assert table not in await storage.list_tables()
