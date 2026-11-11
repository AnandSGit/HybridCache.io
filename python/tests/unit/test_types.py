"""Behaviour of the contract types (storage.types) and interfaces."""

from __future__ import annotations

import asyncio
import dataclasses
import datetime
import enum
from collections.abc import Callable
from typing import Any

import pytest

import storage
import storage.interfaces as storage_interfaces
import storage.types as storage_types
from storage.errors import TransactionFailedError
from storage.interfaces import Adapter, Result, Row, Storage, Transaction
from storage.types import (
    ColumnRef,
    Command,
    CommandSpec,
    Condition,
    Config,
    ExecuteResult,
    HealthStatus,
    HealthStatusType,
    IsolationLevel,
    Join,
    JoinType,
    Operation,
    OperationResult,
    Operator,
    OrderBy,
    Query,
    QuerySpec,
    QueryType,
    SortDirection,
    TableSchema,
    TxOptions,
)

# Config


def test_config_defaults() -> None:
    config = Config()

    assert config.host == "localhost"
    assert config.port is None  # the adapter supplies 5432 / 27017
    assert config.connect_timeout == datetime.timedelta(seconds=10)
    assert config.query_timeout == datetime.timedelta(seconds=30)
    assert config.tx_timeout == datetime.timedelta(minutes=5)
    assert config.max_idle_conns <= config.max_open_conns


def test_config_repr_redacts_password_and_dsn() -> None:
    config = Config(
        host="db.internal",
        username="app",
        password="hunter2",
        dsn="postgresql://app:hunter2@db.internal/app",
    )

    text = repr(config)

    assert "hunter2" not in text
    assert "postgresql://" not in text
    assert "host='db.internal'" in text
    assert "username='app'" in text


def test_config_redaction_does_not_affect_equality() -> None:
    assert Config(password="a") != Config(password="b")
    assert Config(dsn="postgresql://a") != Config(dsn="postgresql://b")


@pytest.mark.parametrize(
    ("make", "attribute"),
    [
        (Config, "options"),
        (Query, "parameters"),
        (Command, "parameters"),
        (QuerySpec, "where"),
        (lambda: CommandSpec("users"), "values"),
        (lambda: Condition("", Operator.AND), "conditions"),
        (lambda: TableSchema("users"), "columns"),
        (lambda: HealthStatus(HealthStatusType.HEALTHY, "ok"), "details"),
    ],
)
def test_mutable_defaults_are_not_shared(
    make: Callable[[], Any], attribute: str
) -> None:
    assert getattr(make(), attribute) is not getattr(make(), attribute)


# Queries and commands


def test_query_can_carry_only_a_spec() -> None:
    spec = QuerySpec(
        table="users",
        where=[Condition("age", Operator.GREATER_THAN, 30)],
        order_by=[OrderBy("name")],
        limit=10,
    )

    query = Query(spec=spec)

    assert query.sql == ""
    assert query.parameters == []
    assert query.query_type is QueryType.UNKNOWN
    assert query.timeout is None  # falls back to Config.query_timeout
    assert query.spec is spec


def test_raw_sql_has_no_spec() -> None:
    assert Query("SELECT 1").spec is None
    assert Command("DELETE FROM users").spec is None


def test_query_spec_uses_no_sql_only_feature_by_default() -> None:
    spec = QuerySpec(table="users")

    assert spec.fields == []  # all fields
    assert spec.joins == []
    assert spec.group_by == []
    assert spec.having == []
    assert spec.subquery is False


@pytest.mark.parametrize(
    ("make", "option"),
    [
        (Query, "cache_key"),
        (Query, "cache_ttl"),
        (Query, "read_only"),
        (Command, "return_id"),
        (Command, "return_count"),
        (Command, "on_conflict"),
    ],
)
def test_unimplemented_options_are_rejected(
    make: type[Query] | type[Command], option: str
) -> None:
    kwargs: dict[str, Any] = {option: True}

    with pytest.raises(TypeError, match=option):
        make("SELECT 1", **kwargs)


def test_column_ref_is_a_frozen_value_distinct_from_strings() -> None:
    ref = ColumnRef("b.a_id")
    literal: object = "b.a_id"

    assert ref == ColumnRef("b.a_id")
    assert hash(ref) == hash(ColumnRef("b.a_id"))
    assert ref != literal
    assert not isinstance(ref, str)
    with pytest.raises(dataclasses.FrozenInstanceError):
        ref.name = "other"  # type: ignore[misc]


def test_defaults_of_join_order_by_and_execute_result() -> None:
    assert Join(JoinType.CROSS, "b").on is None
    assert OrderBy("name").direction is SortDirection.ASC
    assert ExecuteResult(rows_affected=3).last_insert_id is None


def test_tx_options_default_to_the_database_defaults() -> None:
    options = TxOptions()

    assert options.isolation is IsolationLevel.DEFAULT
    assert options.read_only is False
    assert options.timeout is None  # falls back to Config.tx_timeout


def test_health_status_timestamp_is_timezone_aware() -> None:
    before = datetime.datetime.now(datetime.UTC)

    status = HealthStatus(HealthStatusType.HEALTHY, "ok")

    # Comparing with an aware datetime would raise TypeError for a naive one.
    assert before <= status.timestamp <= datetime.datetime.now(datetime.UTC)
    assert status.timestamp.utcoffset() == datetime.timedelta(0)


ENUMS = [
    obj
    for obj in vars(storage_types).values()
    if isinstance(obj, type)
    and issubclass(obj, enum.Enum)
    and obj.__module__ == storage_types.__name__
]


@pytest.mark.parametrize("enum_class", ENUMS, ids=lambda cls: cls.__name__)
def test_enum_values_are_unique(enum_class: type[enum.Enum]) -> None:
    # A duplicated value would silently make a member an alias of another.
    assert len(enum_class.__members__) == len(list(enum_class))


@pytest.mark.parametrize(
    ("module", "name"),
    [
        (storage_types, "Row"),
        (storage_types, "Rows"),
        (storage_types, "ParameterValue"),
        (storage_types, "Parameters"),
        (storage_types, "ConflictResolution"),
        (storage_interfaces, "HealthChecker"),
    ],
)
def test_removed_names_are_gone(module: object, name: str) -> None:
    assert not hasattr(module, name)


def test_package_exports_the_contract() -> None:
    assert [name for name in storage.__all__ if not hasattr(storage, name)] == []
    assert storage.Row is storage_interfaces.Row


# Interfaces


@pytest.mark.parametrize("interface", [Row, Result, Transaction, Storage, Adapter])
def test_interfaces_cannot_be_instantiated(interface: type[Any]) -> None:
    with pytest.raises(TypeError, match="abstract"):
        interface()


@pytest.mark.parametrize(
    ("interface", "methods"),
    [
        (
            Storage,
            {
                "connect",
                "close",
                "ping",
                "health",
                "query",
                "query_one",
                "execute",
                "begin_tx",
                "batch",
                "create_table",
                "drop_table",
                "alter_table",
                "info",
                "list_tables",
                "describe_table",
            },
        ),
        (
            Transaction,
            {
                "query",
                "query_one",
                "execute",
                "batch",
                "commit",
                "rollback",
                "savepoint",
                "rollback_to_savepoint",
                "is_active",
                "get_id",
            },
        ),
        (
            Adapter,
            {
                "name",
                "version",
                "database_type",
                "connect",
                "parse_dsn",
                "validate_config",
                "translate_query",
                "translate_command",
                "map_python_type",
                "map_database_type",
                "supports_transactions",
                "supports_joins",
                "supports_batch",
                "supports_schema",
            },
        ),
    ],
)
def test_interfaces_mirror_the_go_contract(
    interface: type[Any], methods: set[str]
) -> None:
    assert interface.__abstractmethods__ == methods


# Transaction context manager


class FakeTransaction(Transaction):
    """Records control calls; commit and rollback end the transaction."""

    def __init__(
        self,
        commit_error: BaseException | None = None,
        failed_commit_ends: bool = False,
    ) -> None:
        self.calls: list[str] = []
        self.active = False
        self.commit_error = commit_error
        self.failed_commit_ends = failed_commit_ends

    async def begin(self) -> None:
        self.calls.append("begin")
        self.active = True

    async def commit(self) -> None:
        self.calls.append("commit")
        if self.commit_error is not None:
            self.active = not self.failed_commit_ends
            raise self.commit_error
        self.active = False

    async def rollback(self) -> None:
        self.calls.append("rollback")
        self.active = False

    def is_active(self) -> bool:
        return self.active

    def get_id(self) -> str:
        return "tx-1"

    async def query(self, query: Query) -> Result:
        raise NotImplementedError

    async def query_one(self, query: Query) -> Row | None:
        raise NotImplementedError

    async def execute(self, command: Command) -> ExecuteResult:
        raise NotImplementedError

    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        raise NotImplementedError

    async def savepoint(self, name: str) -> None:
        raise NotImplementedError

    async def rollback_to_savepoint(self, name: str) -> None:
        raise NotImplementedError


async def begin_tx(
    commit_error: BaseException | None = None, failed_commit_ends: bool = False
) -> FakeTransaction:
    """Like Storage.begin_tx: the transaction is begun exactly once, here."""
    tx = FakeTransaction(commit_error, failed_commit_ends)
    await tx.begin()
    return tx


async def raise_inside(tx: Transaction, error: BaseException) -> None:
    async with tx:
        raise error


async def complete_inside(tx: Transaction) -> None:
    async with tx:
        pass


async def test_entering_does_not_begin_again_and_success_commits() -> None:
    async with await begin_tx() as tx:
        assert tx.calls == ["begin"]

    assert tx.calls == ["begin", "commit"]
    assert not tx.is_active()


async def test_entering_returns_the_transaction() -> None:
    tx = await begin_tx()

    async with tx as entered:
        assert entered is tx


async def test_exception_rolls_back_and_propagates() -> None:
    tx = await begin_tx()

    with pytest.raises(LookupError):
        await raise_inside(tx, LookupError("missing"))

    assert tx.calls == ["begin", "rollback"]


async def test_cancellation_rolls_back() -> None:
    tx = await begin_tx()

    with pytest.raises(asyncio.CancelledError):
        await raise_inside(tx, asyncio.CancelledError())

    assert tx.calls == ["begin", "rollback"]


@pytest.mark.parametrize("action", ["commit", "rollback"])
async def test_ending_inside_the_block_is_not_repeated_on_exit(action: str) -> None:
    async with await begin_tx() as tx:
        await getattr(tx, action)()

    assert tx.calls == ["begin", action]


@pytest.mark.parametrize(
    ("failed_commit_ends", "calls"),
    [
        (False, ["begin", "commit", "rollback"]),
        # e.g. PostgreSQL ends a transaction whose COMMIT failed
        (True, ["begin", "commit"]),
    ],
)
async def test_failed_commit_leaves_no_open_transaction_and_raises_its_error(
    failed_commit_ends: bool, calls: list[str]
) -> None:
    error = TransactionFailedError("serialization failure")
    tx = await begin_tx(commit_error=error, failed_commit_ends=failed_commit_ends)

    with pytest.raises(TransactionFailedError) as excinfo:
        await complete_inside(tx)

    assert excinfo.value is error
    assert tx.calls == calls
    assert not tx.is_active()


def test_context_manager_is_implemented_once_for_all_adapters() -> None:
    assert getattr(Transaction.__aenter__, "__final__", False) is True
    assert getattr(Transaction.__aexit__, "__final__", False) is True
