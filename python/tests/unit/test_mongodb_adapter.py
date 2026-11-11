"""
Unit tests of the MongoDB adapter: translation of specs, URIs and DSNs,
configuration, type mapping, rows and results, and error translation. They need
no server.
"""

from __future__ import annotations

import dataclasses
import datetime
import re
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Any, cast

import pytest
from bson import ObjectId
from bson.errors import InvalidDocument
from pymongo import AsyncMongoClient, ReadPreference
from pymongo.asynchronous.cursor import AsyncCursor
from pymongo.errors import DuplicateKeyError as DriverDuplicateKeyError
from pymongo.errors import ExecutionTimeout, InvalidURI, OperationFailure
from pymongo.uri_parser import parse_uri

from storage.adapters.mongodb import (
    MongoDBAdapter,
    MongoDBResult,
    MongoDBRow,
    MongoDBStorage,
    MongoDBTransaction,
)
from storage.adapters.mongodb.connection import build_uri, client_settings
from storage.adapters.mongodb.executor import Executor
from storage.adapters.mongodb.support import (
    deadline_after,
    driver_error,
    earliest,
    has_passed,
    positive_seconds,
    transaction_error,
)
from storage.adapters.mongodb.transaction import transaction_options
from storage.adapters.mongodb.translate import (
    QueryKind,
    build_filter,
    like_regex,
    plan_command,
    plan_query,
)
from storage.errors import (
    ConnectionClosedError,
    DataError,
    DuplicateKeyError,
    InvalidConfigError,
    InvalidDSNError,
    InvalidQueryError,
    InvalidSchemaError,
    InvalidTransactionError,
    QueryFailedError,
    QueryTimeoutError,
    StorageError,
    TransactionFailedError,
    TransactionTimeoutError,
    UnsupportedOperationError,
    UnsupportedQueryError,
    ValidationError,
    is_retryable,
)
from storage.types import (
    ColumnDefinition,
    ColumnRef,
    Command,
    CommandSpec,
    CommandType,
    Condition,
    Config,
    DatabaseType,
    DataType,
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
)


def cond(field: str, operator: Operator, value: Any = None, *values: Any) -> Condition:
    return Condition(field, operator, value, list(values) if values else None)


def group(operator: Operator, *conditions: Condition) -> Condition:
    return Condition("", operator, conditions=list(conditions))


def query(**spec: Any) -> Query:
    return Query(spec=QuerySpec(**spec))


def command(
    command_type: CommandType,
    table: str = "t",
    values: dict[str, Any] | None = None,
    where: list[Condition] | None = None,
) -> Command:
    spec = CommandSpec(
        table, [FieldValue(k, v) for k, v in (values or {}).items()], where or []
    )
    return Command(command_type=command_type, spec=spec)


ACTIVE = cond("active", Operator.EQUAL, True)


def test_adapter_identity_and_features() -> None:
    adapter = MongoDBAdapter()

    assert adapter.name() == "mongodb"
    assert adapter.version() == "1.0.0"
    assert adapter.database_type() is DatabaseType.MONGODB
    assert adapter.supports_transactions()
    assert adapter.supports_batch()
    assert adapter.supports_schema()
    assert not adapter.supports_joins(), "joins are rejected, not emulated"


@pytest.mark.parametrize(
    "cls",
    [MongoDBAdapter, MongoDBStorage, MongoDBTransaction, MongoDBResult, MongoDBRow],
)
def test_every_interface_method_is_implemented(cls: type) -> None:
    assert not getattr(cls, "__abstractmethods__", frozenset())


def test_rows_results_and_storages_can_be_created() -> None:
    assert MongoDBRow({}).keys() == []
    assert MongoDBResult().columns() == []
    assert MongoDBStorage(Config()).info().name == "mongodb"


# Filters


@pytest.mark.parametrize(
    ("conditions", "expected"),
    [
        pytest.param([], {}, id="no conditions"),
        pytest.param(
            [cond("name", Operator.EQUAL, "Ann")], {"name": {"$eq": "Ann"}}, id="equal"
        ),
        pytest.param(
            [cond("name", Operator.NOT_EQUAL, "Ann")],
            {"name": {"$ne": "Ann"}},
            id="not equal",
        ),
        pytest.param(
            [cond("age", Operator.GREATER_THAN, 18)], {"age": {"$gt": 18}}, id="gt"
        ),
        pytest.param(
            [cond("age", Operator.GREATER_THAN_OR_EQUAL, 18)],
            {"age": {"$gte": 18}},
            id="gte",
        ),
        pytest.param(
            [cond("age", Operator.LESS_THAN, 18)], {"age": {"$lt": 18}}, id="lt"
        ),
        pytest.param(
            [cond("age", Operator.LESS_THAN_OR_EQUAL, 18)],
            {"age": {"$lte": 18}},
            id="lte",
        ),
        pytest.param(
            [cond("id", Operator.IN, None, 1, 2, 3)],
            {"id": {"$in": [1, 2, 3]}},
            id="in",
        ),
        pytest.param(
            [cond("id", Operator.IN)], {"id": {"$in": []}}, id="in without values"
        ),
        pytest.param(
            [cond("id", Operator.NOT_IN, None, 1, 2)],
            {"id": {"$nin": [1, 2]}},
            id="not in",
        ),
        pytest.param(
            [cond("age", Operator.BETWEEN, None, 18, 65)],
            {"age": {"$gte": 18, "$lte": 65}},
            id="between",
        ),
        pytest.param(
            [cond("age", Operator.NOT_BETWEEN, None, 18, 65)],
            {"$or": [{"age": {"$lt": 18}}, {"age": {"$gt": 65}}]},
            id="not between",
        ),
        pytest.param(
            [cond("name", Operator.LIKE, "J_n%")],
            {"name": {"$regex": r"^J.n.*\z", "$options": "s"}},
            id="like",
        ),
        pytest.param(
            [cond("name", Operator.NOT_LIKE, "J%")],
            {"name": {"$not": {"$regex": r"^J.*\z", "$options": "s"}}},
            id="not like",
        ),
        pytest.param([cond("city", Operator.IS_NULL)], {"city": None}, id="is null"),
        pytest.param(
            [cond("city", Operator.IS_NOT_NULL)],
            {"city": {"$ne": None}},
            id="is not null",
        ),
        pytest.param(
            [
                cond("age", Operator.GREATER_THAN, 18),
                cond("age", Operator.LESS_THAN, 65),
            ],
            {"$and": [{"age": {"$gt": 18}}, {"age": {"$lt": 65}}]},
            id="several conditions are an explicit $and",
        ),
        pytest.param(
            [
                group(
                    Operator.OR,
                    group(
                        Operator.AND,
                        cond("a", Operator.EQUAL, 1),
                        cond("b", Operator.EQUAL, 2),
                    ),
                    cond("c", Operator.EQUAL, 3),
                )
            ],
            {
                "$or": [
                    {"$and": [{"a": {"$eq": 1}}, {"b": {"$eq": 2}}]},
                    {"c": {"$eq": 3}},
                ]
            },
            id="nested groups",
        ),
        pytest.param(
            [
                ACTIVE,
                group(
                    Operator.OR,
                    cond("a", Operator.EQUAL, 1),
                    cond("b", Operator.IS_NULL),
                ),
            ],
            {
                "$and": [
                    {"active": {"$eq": True}},
                    {"$or": [{"a": {"$eq": 1}}, {"b": None}]},
                ]
            },
            id="group among conditions",
        ),
        pytest.param(
            [cond("address.city", Operator.EQUAL, "Oslo")],
            {"address.city": {"$eq": "Oslo"}},
            id="dotted path",
        ),
        pytest.param(
            [cond("a", Operator.EQUAL, {"$gt": 1})],
            {"a": {"$eq": {"$gt": 1}}},
            id="operator-like values stay literal",
        ),
    ],
)
def test_build_filter(conditions: list[Condition], expected: dict[str, Any]) -> None:
    assert build_filter(conditions) == expected


@pytest.mark.parametrize(
    ("condition", "error"),
    [
        pytest.param(
            cond("a", Operator.EQUAL, ColumnRef("b")),
            UnsupportedQueryError,
            id="column reference",
        ),
        pytest.param(
            cond("a", Operator.IN, None, 1, ColumnRef("b")),
            UnsupportedQueryError,
            id="column reference in values",
        ),
        pytest.param(
            Condition("", Operator.EXISTS, object()), UnsupportedQueryError, id="exists"
        ),
        pytest.param(
            Condition("", Operator.NOT_EXISTS, object()),
            UnsupportedQueryError,
            id="not exists",
        ),
        pytest.param(group(Operator.AND), InvalidQueryError, id="empty AND group"),
        pytest.param(group(Operator.OR), InvalidQueryError, id="empty OR group"),
        pytest.param(
            group(Operator.OR, cond("$where", Operator.EQUAL, 1)),
            InvalidQueryError,
            id="bad condition in a group",
        ),
        pytest.param(
            cond("age", Operator.BETWEEN, None, 18),
            InvalidQueryError,
            id="between with one value",
        ),
        pytest.param(
            cond("age", Operator.BETWEEN, 18),
            InvalidQueryError,
            id="between without values",
        ),
        pytest.param(
            cond("name", Operator.LIKE, 5), InvalidQueryError, id="like with non-string"
        ),
        pytest.param(
            cond("name", Operator.LIKE, "abc\\"),
            InvalidQueryError,
            id="like ending in escape",
        ),
        pytest.param(
            cond("$where", Operator.EQUAL, "sleep(1000)"),
            InvalidQueryError,
            id="operator field name",
        ),
        pytest.param(cond("", Operator.EQUAL, 1), InvalidQueryError, id="empty field"),
        pytest.param(
            cond("LOWER(name)", Operator.EQUAL, "ann"),
            UnsupportedQueryError,
            id="SQL expression field",
        ),
        pytest.param(
            Condition("a", cast(Operator, "custom"), 1),
            InvalidQueryError,
            id="unknown operator",
        ),
    ],
)
def test_build_filter_rejects(condition: Condition, error: type[StorageError]) -> None:
    with pytest.raises(error):
        build_filter([condition])


@pytest.mark.parametrize(
    ("pattern", "regex", "matches", "mismatches"),
    [
        ("abc", r"^abc\z", ["abc"], ["abcd", "xabc", "abc\n", "ABC"]),
        ("a%", r"^a.*\z", ["a", "abc", "a\nb"], ["ba"]),
        ("_b_", r"^.b.\z", ["abc", "\nb\n"], ["bc", "abcd"]),
        ("a.c", r"^a\.c\z", ["a.c"], ["abc"]),
        (
            "(x|y)*+?[]{}^$",
            r"^\(x\|y\)\*\+\?\[\]\{\}\^\$\z",
            ["(x|y)*+?[]{}^$"],
            ["x"],
        ),
        ("100\\%", r"^100%\z", ["100%"], ["1000"]),
        ("a\\_b", r"^a_b\z", ["a_b"], ["axb"]),
        ("a\\\\b", r"^a\\b\z", ["a\\b"], ["ab"]),
        ("é_ü%", r"^é.ü.*\z", ["éxü", "éxüyz"], ["exu"]),
        ("a b", r"^a\ b\z", ["a b"], ["ab"]),
    ],
)
def test_like_regex(
    pattern: str, regex: str, matches: list[str], mismatches: list[str]
) -> None:
    assert like_regex(pattern) == regex
    # MongoDB's \z is \Z in Python before 3.14; DOTALL is the "s" option.
    compiled = re.compile(regex[: -len(r"\z")] + r"\Z", re.DOTALL)
    assert [s for s in matches if compiled.match(s)] == matches
    assert [s for s in mismatches if compiled.match(s)] == []


# Queries


@pytest.mark.parametrize(
    ("spec", "expected"),
    [
        pytest.param(
            {"table": "users"},
            {"collection": "users", "kind": QueryKind.FIND, "columns": ()},
            id="all fields",
        ),
        pytest.param(
            {"table": "t", "fields": ["*"]},
            {"columns": (), "projection": None},
            id="star",
        ),
        pytest.param(
            {"table": "t", "fields": ["name", "age"]},
            {"columns": ("name", "age"), "projection": {"name": 1, "age": 1, "_id": 0}},
            id="fields",
        ),
        pytest.param(
            {"table": "t", "fields": ["_id", "name"]},
            {"columns": ("_id", "name"), "projection": {"_id": 1, "name": 1}},
            id="fields with _id",
        ),
        pytest.param(
            {"table": "t", "fields": ["name", "name"]},
            {"columns": ("name", "name"), "projection": {"name": 1, "_id": 0}},
            id="repeated field",
        ),
        pytest.param(
            {"table": "t", "fields": ["COUNT(*)"], "where": [ACTIVE]},
            {
                "kind": QueryKind.COUNT,
                "columns": ("count",),
                "criteria": {"active": {"$eq": True}},
            },
            id="count star",
        ),
        pytest.param(
            {"table": "t", "fields": ["count( email )"], "where": [ACTIVE]},
            {
                "kind": QueryKind.COUNT,
                "criteria": {
                    "$and": [{"active": {"$eq": True}}, {"email": {"$ne": None}}]
                },
            },
            id="count field counts non-null values",
        ),
        pytest.param(
            {"table": "t", "fields": ["COUNT(email)"]},
            {"kind": QueryKind.COUNT, "criteria": {"email": {"$ne": None}}},
            id="count field without conditions",
        ),
        pytest.param(
            {"table": "t", "fields": ["city"], "distinct": True},
            {"kind": QueryKind.DISTINCT, "columns": ("city",), "criteria": {}},
            id="distinct",
        ),
        pytest.param(
            {
                "table": "t",
                "order_by": [OrderBy("age", SortDirection.DESC), OrderBy("name")],
                "offset": 5,
                "limit": 10,
            },
            {
                "sort": [("age", -1), ("name", 1)],
                "skip": 5,
                "limit": 10,
                "empty": False,
            },
            id="sort skip limit",
        ),
        pytest.param(
            {"table": "t", "limit": 0},
            {"empty": True},
            id="limit 0 reads nothing",
        ),
    ],
)
def test_plan_query(spec: dict[str, Any], expected: dict[str, Any]) -> None:
    plan = plan_query(query(**spec))

    assert {name: getattr(plan, name) for name in expected} == expected


@pytest.mark.parametrize(
    ("built", "error"),
    [
        pytest.param(Query(sql="SELECT * FROM t"), UnsupportedQueryError, id="raw SQL"),
        pytest.param(
            query(table="t", joins=[Join(JoinType.CROSS, "u")]),
            UnsupportedQueryError,
            id="join",
        ),
        pytest.param(
            query(table="t", group_by=["city"]), UnsupportedQueryError, id="group by"
        ),
        pytest.param(
            query(table="t", having=[cond("n", Operator.GREATER_THAN, 1)]),
            UnsupportedQueryError,
            id="having",
        ),
        pytest.param(
            query(table="", subquery=True),
            UnsupportedQueryError,
            id="subquery source",
        ),
        pytest.param(query(), InvalidQueryError, id="no table"),
        pytest.param(
            query(table="t", fields=["SUM(age)"]),
            UnsupportedQueryError,
            id="other aggregate",
        ),
        pytest.param(
            query(table="t", fields=["city", "COUNT(*)"]),
            UnsupportedQueryError,
            id="count with other fields",
        ),
        pytest.param(
            query(table="t", fields=["COUNT(DISTINCT city)"]),
            UnsupportedQueryError,
            id="count distinct",
        ),
        pytest.param(
            query(table="t", fields=["COUNT(*)"], distinct=True),
            UnsupportedQueryError,
            id="distinct count",
        ),
        pytest.param(
            query(table="t", fields=["COUNT($x)"]), InvalidQueryError, id="count $field"
        ),
        pytest.param(
            query(table="t", fields=["name AS n"]), UnsupportedQueryError, id="alias"
        ),
        pytest.param(
            query(table="t", fields=["*", "name"]),
            UnsupportedQueryError,
            id="star with fields",
        ),
        pytest.param(
            query(table="t", fields=["a", "b"], distinct=True),
            UnsupportedQueryError,
            id="distinct on two fields",
        ),
        pytest.param(
            query(table="t", distinct=True),
            UnsupportedQueryError,
            id="distinct on all fields",
        ),
        pytest.param(
            query(table="t", fields=["a"], distinct=True, order_by=[OrderBy("a")]),
            UnsupportedQueryError,
            id="distinct with order by",
        ),
        pytest.param(
            query(table="t", fields=["COUNT(*)"], limit=1),
            UnsupportedQueryError,
            id="count with limit",
        ),
        pytest.param(
            query(table="t", where=[group(Operator.OR)]),
            InvalidQueryError,
            id="bad condition",
        ),
        pytest.param(
            query(table="t", order_by=[OrderBy("$x")]),
            InvalidQueryError,
            id="bad sort field",
        ),
        pytest.param(
            query(table="t", order_by=[OrderBy("x", cast(SortDirection, 7))]),
            InvalidQueryError,
            id="bad sort direction",
        ),
        pytest.param(
            query(table="t", limit=-1), InvalidQueryError, id="negative limit"
        ),
        pytest.param(
            query(table="t", offset=-1), InvalidQueryError, id="negative offset"
        ),
    ],
)
def test_plan_query_rejects(built: Query, error: type[StorageError]) -> None:
    with pytest.raises(error):
        plan_query(built)


# Commands


def test_plan_command() -> None:
    values = {"name": "Ann", "age": 30}
    where = [cond("id", Operator.EQUAL, 1)]

    inserted = plan_command(command(CommandType.INSERT, "users", values))
    updated = plan_command(command(CommandType.UPDATE, "users", values, where))
    deleted = plan_command(command(CommandType.DELETE, "users", where=where))

    assert (inserted.collection, inserted.document) == ("users", values)
    assert list(inserted.document) == ["name", "age"], "values keep their order"
    assert updated.criteria == {"id": {"$eq": 1}}
    assert updated.update() == {"$set": {"name": "Ann", "age": 30}}
    assert deleted.criteria == {"id": {"$eq": 1}}


@pytest.mark.parametrize(
    ("built", "error"),
    [
        pytest.param(
            Command(sql="DELETE FROM t", command_type=CommandType.DELETE),
            UnsupportedQueryError,
            id="raw SQL",
        ),
        pytest.param(
            command(CommandType.DELETE, table=""), InvalidQueryError, id="no table"
        ),
        pytest.param(
            command(CommandType.INSERT), InvalidQueryError, id="insert without values"
        ),
        pytest.param(
            command(CommandType.INSERT, values={"a": 1}, where=[ACTIVE]),
            InvalidQueryError,
            id="insert with where",
        ),
        pytest.param(
            command(CommandType.UPDATE, where=[ACTIVE]),
            InvalidQueryError,
            id="update without values",
        ),
        pytest.param(
            command(CommandType.UPDATE, values={"a": 1}, where=[group(Operator.OR)]),
            InvalidQueryError,
            id="update with a bad condition cannot broaden",
        ),
        pytest.param(
            command(CommandType.DELETE, where=[cond("", Operator.EQUAL, 1)]),
            InvalidQueryError,
            id="delete with a bad condition cannot broaden",
        ),
        pytest.param(
            command(CommandType.DELETE, values={"a": 1}),
            InvalidQueryError,
            id="delete with values",
        ),
        pytest.param(
            Command(
                command_type=CommandType.UPDATE,
                spec=CommandSpec("t", [FieldValue("a", 1), FieldValue("a", 2)]),
            ),
            InvalidQueryError,
            id="field assigned twice",
        ),
        pytest.param(
            command(CommandType.INSERT, values={"$set": 1}),
            InvalidQueryError,
            id="operator field",
        ),
        pytest.param(
            command(CommandType.UPDATE, values={"a": ColumnRef("b")}),
            UnsupportedQueryError,
            id="column reference value",
        ),
        pytest.param(
            command(CommandType.CREATE, values={"a": 1}),
            UnsupportedQueryError,
            id="other command type",
        ),
    ],
)
def test_plan_command_rejects(built: Command, error: type[StorageError]) -> None:
    with pytest.raises(error):
        plan_command(built)


# Rendering (translate_query / translate_command)


@pytest.mark.parametrize(
    ("built", "expected"),
    [
        pytest.param(
            query(
                table="users",
                fields=["name"],
                where=[cond("age", Operator.GREATER_THAN_OR_EQUAL, 18)],
                order_by=[OrderBy("age", SortDirection.DESC)],
                offset=5,
                limit=10,
            ),
            'db.getCollection("users").find({"age":{"$gte":18}}, {"name":1,"_id":0})'
            '.sort({"age":-1}).skip(5).limit(10)',
            id="find",
        ),
        pytest.param(
            query(table="users", fields=["COUNT(*)"], where=[ACTIVE]),
            'db.getCollection("users").countDocuments({"active":{"$eq":true}})',
            id="count",
        ),
        pytest.param(
            query(table="users", fields=["city"], distinct=True),
            'db.getCollection("users").distinct("city", {})',
            id="distinct",
        ),
        pytest.param(
            query(table="users", limit=0),
            'db.getCollection("users").find({}) /* LIMIT 0: no documents are read */',
            id="limit 0",
        ),
        pytest.param(
            query(
                table="users", where=[cond("_id", Operator.EQUAL, ObjectId("0" * 24))]
            ),
            'db.getCollection("users").find({"_id":{"$eq":{"$oid":"'
            + "0" * 24
            + '"}}})',
            id="extended JSON values",
        ),
    ],
)
def test_translate_query(built: Query, expected: str) -> None:
    assert MongoDBAdapter().translate_query(built) == (expected, [])


@pytest.mark.parametrize(
    ("built", "expected"),
    [
        pytest.param(
            command(CommandType.INSERT, "users", {"name": "Ann", "age": 30}),
            'db.getCollection("users").insertOne({"name":"Ann","age":30})',
            id="insert",
        ),
        pytest.param(
            command(
                CommandType.UPDATE,
                "users",
                {"name": "Bob"},
                [cond("id", Operator.EQUAL, 1)],
            ),
            'db.getCollection("users").updateMany({"id":{"$eq":1}},'
            ' {"$set":{"name":"Bob"}})',
            id="update",
        ),
        pytest.param(
            command(CommandType.DELETE, "users", where=[cond("id", Operator.EQUAL, 5)]),
            'db.getCollection("users").deleteMany({"id":{"$eq":5}})',
            id="delete",
        ),
    ],
)
def test_translate_command(built: Command, expected: str) -> None:
    assert MongoDBAdapter().translate_command(built) == (expected, [])


def test_translate_rejects_values_bson_cannot_encode() -> None:
    built = query(table="t", where=[cond("tags", Operator.EQUAL, {1, 2})])

    with pytest.raises(InvalidQueryError):
        MongoDBAdapter().translate_query(built)


# URIs, DSNs and configuration


@pytest.mark.parametrize(
    ("config", "expected"),
    [
        pytest.param(
            Config(host="localhost", database="testdb"),
            "mongodb://localhost:27017/",
            id="default port, database not in the path",
        ),
        pytest.param(
            Config(host="db", port=27018, username="user", password="p@ss:w/rd"),
            "mongodb://user:p%40ss%3Aw%2Frd@db:27018/",
            id="credentials are percent-encoded",
        ),
        pytest.param(
            Config(host="db", username="us er"),
            "mongodb://us+er@db:27017/",
            id="user without password",
        ),
        pytest.param(Config(host="::1"), "mongodb://[::1]:27017/", id="IPv6"),
        pytest.param(
            Config(
                host="h",
                options={"tls": True, "authSource": "admin", "appName": "a b&c"},
            ),
            "mongodb://h:27017/?appName=a+b%26c&authSource=admin&tls=true",
            id="options are encoded and sorted",
        ),
        pytest.param(
            Config(host="h", options={"readPreferenceTags": ["dc:ny", ""]}),
            "mongodb://h:27017/?readPreferenceTags=dc%3Any&readPreferenceTags=",
            id="repeated option",
        ),
    ],
)
def test_build_uri(config: Config, expected: str) -> None:
    assert build_uri(config) == expected


def test_built_uri_round_trips_through_pymongo() -> None:
    config = Config(
        host="db.example.com",
        port=27018,
        username="app user",
        password="p@ss:w/rd%",
        options={"authSource": "admin", "tls": True},
    )

    parsed = parse_uri(build_uri(config))

    assert parsed["nodelist"] == [("db.example.com", 27018)]
    assert (parsed["username"], parsed["password"]) == ("app user", "p@ss:w/rd%")
    assert parsed["database"] is None
    assert parsed["options"] == {"authSource": "admin", "tls": True}


@pytest.mark.parametrize(
    ("dsn", "expected"),
    [
        pytest.param(
            "mongodb://localhost:27017/testdb",
            {"host": "localhost", "port": 27017, "database": "testdb", "options": {}},
            id="basic",
        ),
        pytest.param(
            "mongodb://user:p%40ss%3Aw%2Frd@db.example.com/app?authSource=admin&tls=true",
            {
                "host": "db.example.com",
                "port": 27017,
                "database": "app",
                "username": "user",
                "password": "p@ss:w/rd",
                "options": {"authSource": "admin", "tls": "true"},
            },
            id="credentials and options",
        ),
        pytest.param(
            "mongodb://h1:27017,h2:27018/db?replicaSet=rs0",
            {"host": "h1", "port": 27017, "database": "db"},
            id="replica set",
        ),
        pytest.param(
            "mongodb://127.0.0.1:57017/?replicaSet=rs0",
            {"host": "127.0.0.1", "port": 57017, "database": ""},
            id="no database",
        ),
        pytest.param(
            "mongodb://[::1]:27018/db", {"host": "::1", "port": 27018}, id="IPv6"
        ),
        pytest.param(
            "mongodb://h/db?w=1;journal=true",
            {"options": {"w": "1", "journal": "true"}},
            id="semicolon separators",
        ),
    ],
)
def test_parse_dsn(dsn: str, expected: dict[str, Any]) -> None:
    config = MongoDBAdapter().parse_dsn(dsn)

    assert config.dsn == dsn
    assert {name: getattr(config, name) for name in expected} == expected


@pytest.mark.parametrize(
    "dsn",
    [
        "",
        "invalid://uri",
        "postgresql://user:pass@host/db",
        "mongodb://",
        "mongodb://h/db?foo",
        "mongodb://h:port/db",
        "mongodb://h:99999/db",
        "mongodb://h/db?maxPoolSize=x",
        "mongodb+srv://h1,h2/db",
        "mongodb://h/d b",
        "mongodb://user:p@ss@h/db",
    ],
)
def test_parse_dsn_rejects(dsn: str) -> None:
    with pytest.raises(InvalidDSNError):
        MongoDBAdapter().parse_dsn(dsn)


def test_invalid_dsn_errors_do_not_reveal_credentials() -> None:
    # An unescaped '/' ends the host part early, so PyMongo's message quotes the
    # rest of the password as the database name.
    dsn = "mongodb://admin:s3/cr3t@h/db"
    with pytest.raises(InvalidURI, match="cr3t"):
        parse_uri(dsn)

    with pytest.raises(InvalidDSNError) as caught:
        MongoDBAdapter().parse_dsn(dsn)

    assert "cr3t" not in str(caught.value)
    assert caught.value.__cause__ is None


@pytest.mark.parametrize(
    "config",
    [
        pytest.param(Config(host="h", database="d"), id="components"),
        pytest.param(Config(dsn="mongodb://h1,h2/app?replicaSet=rs0"), id="dsn"),
        pytest.param(
            Config(dsn="mongodb://h/?replicaSet=rs0", database="app"),
            id="dsn without database",
        ),
        pytest.param(
            Config(host="h", database="d", max_open_conns=0, max_idle_conns=0),
            id="driver default pool",
        ),
    ],
)
def test_validate_config_accepts(config: Config) -> None:
    MongoDBAdapter().validate_config(config)


@pytest.mark.parametrize(
    ("config", "error", "message"),
    [
        pytest.param(
            Config(host="", database="d"),
            InvalidConfigError,
            "host is required",
            id="no host",
        ),
        pytest.param(
            Config(host="h"), InvalidConfigError, "database is required", id="no db"
        ),
        pytest.param(
            Config(dsn="mongodb://h/?replicaSet=rs0"),
            InvalidConfigError,
            "database is required",
            id="dsn without database",
        ),
        pytest.param(
            Config(dsn="mysql://h/d"), InvalidDSNError, "invalid", id="invalid dsn"
        ),
        pytest.param(
            Config(host="h", port=70000, database="d"),
            InvalidConfigError,
            "invalid MongoDB URI",
            id="invalid port",
        ),
        pytest.param(
            Config(host="h", database="d", options={"maxPoolSize": "x"}),
            InvalidConfigError,
            "invalid MongoDB URI",
            id="invalid option",
        ),
        pytest.param(
            Config(host="h", database="d", max_open_conns=-1),
            InvalidConfigError,
            "max_open_conns cannot be negative",
            id="negative max_open_conns",
        ),
        pytest.param(
            Config(host="h", database="d", max_idle_conns=-1),
            InvalidConfigError,
            "max_idle_conns cannot be negative",
            id="negative max_idle_conns",
        ),
        pytest.param(
            Config(host="h", database="d", max_open_conns=5, max_idle_conns=10),
            InvalidConfigError,
            "max_idle_conns cannot exceed max_open_conns",
            id="idle exceeds open",
        ),
    ],
)
def test_validate_config_rejects(
    config: Config, error: type[ValidationError], message: str
) -> None:
    with pytest.raises(error, match=message):
        MongoDBAdapter().validate_config(config)


@pytest.mark.parametrize(
    ("config", "database", "options"),
    [
        pytest.param(
            Config(host="h", database="app"),
            "app",
            {
                "maxPoolSize": 25,
                "minPoolSize": 5,
                "maxIdleTimeMS": 1_800_000,
                "connectTimeoutMS": 10_000,
            },
            id="defaults",
        ),
        pytest.param(
            Config(dsn="mongodb://h/fromdsn"),
            "fromdsn",
            {
                "maxPoolSize": 25,
                "minPoolSize": 5,
                "maxIdleTimeMS": 1_800_000,
                "connectTimeoutMS": 10_000,
            },
            id="database from the DSN",
        ),
        pytest.param(
            Config(dsn="mongodb://h/fromdsn", database="override"),
            "override",
            {
                "maxPoolSize": 25,
                "minPoolSize": 5,
                "maxIdleTimeMS": 1_800_000,
                "connectTimeoutMS": 10_000,
            },
            id="Config.database wins",
        ),
        pytest.param(
            Config(dsn="mongodb://h/d?maxPoolSize=2&connectTimeoutMS=500"),
            "d",
            {"maxIdleTimeMS": 1_800_000},
            id="URI options win",
        ),
        pytest.param(
            Config(host="h", database="d", options={"minPoolSize": 1}),
            "d",
            {"maxIdleTimeMS": 1_800_000, "connectTimeoutMS": 10_000},
            id="URI pool sizes win together",
        ),
        pytest.param(
            Config(
                host="h",
                database="d",
                max_open_conns=0,
                max_idle_conns=0,
                conn_max_idle_time=None,
                connect_timeout=datetime.timedelta(0),
            ),
            "d",
            {},
            id="unset values keep the driver defaults",
        ),
    ],
)
def test_client_settings(
    config: Config, database: str, options: dict[str, Any]
) -> None:
    settings = client_settings(config)

    assert settings.database == database
    assert settings.options == options
    assert settings.uri == (config.dsn or build_uri(config))


# Type mapping


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("text", DataType.STRING),
        (42, DataType.INTEGER),
        (True, DataType.BOOLEAN),
        (3.14, DataType.FLOAT),
        (datetime.datetime(2024, 1, 2, tzinfo=datetime.UTC), DataType.DATETIME),
        (b"raw", DataType.BINARY),
        (bytearray(b"raw"), DataType.BINARY),
        ({"key": "value"}, DataType.JSON),
        ([1, 2, 3], DataType.JSON),
    ],
)
def test_map_python_type(value: Any, expected: DataType) -> None:
    assert MongoDBAdapter().map_python_type(value) is expected


@pytest.mark.parametrize("value", [None, object(), {1, 2}])
def test_map_python_type_rejects(value: Any) -> None:
    with pytest.raises(ValidationError, match="unsupported Python type"):
        MongoDBAdapter().map_python_type(value)


@pytest.mark.parametrize(
    ("name", "expected"),
    [
        ("string", DataType.STRING),
        ("int", DataType.INTEGER),
        ("long", DataType.INTEGER),
        ("double", DataType.FLOAT),
        ("decimal", DataType.DECIMAL),
        ("bool", DataType.BOOLEAN),
        ("date", DataType.DATETIME),
        ("timestamp", DataType.DATETIME),
        ("binData", DataType.BINARY),
        ("object", DataType.JSON),
        ("array", DataType.ARRAY),
        ("objectId", DataType.STRING),
        ("STRING", DataType.STRING),
    ],
)
def test_map_database_type(name: str, expected: DataType) -> None:
    assert MongoDBAdapter().map_database_type(name) is expected


@pytest.mark.parametrize("name", ["null", "unknown_type", ""])
def test_map_database_type_rejects(name: str) -> None:
    with pytest.raises(ValidationError, match="unsupported MongoDB type"):
        MongoDBAdapter().map_database_type(name)


# Transactions


@pytest.mark.parametrize(
    ("isolation", "read_concern", "write_concern"),
    [
        (IsolationLevel.DEFAULT, None, None),
        (IsolationLevel.READ_UNCOMMITTED, None, None),
        (IsolationLevel.READ_COMMITTED, "majority", None),
        (IsolationLevel.REPEATABLE_READ, "snapshot", "majority"),
        (IsolationLevel.SERIALIZABLE, "snapshot", "majority"),
    ],
)
def test_transaction_options(
    isolation: IsolationLevel, read_concern: str | None, write_concern: str | None
) -> None:
    options = transaction_options(isolation)

    assert options["read_preference"] == ReadPreference.PRIMARY
    assert getattr(options.get("read_concern"), "level", None) == read_concern
    assert getattr(options.get("write_concern"), "document", {}).get("w") == (
        write_concern
    )


# Rows and results


@dataclasses.dataclass
class Person:
    name: str = ""
    age: int = 0


@dataclasses.dataclass(frozen=True)
class FrozenPerson:
    name: str = ""


DOCUMENT = {"_id": 1, "name": "Ann", "age": 30, "address": {"city": "Oslo"}}


def test_row_of_a_whole_document() -> None:
    row = MongoDBRow(DOCUMENT)

    assert row.keys() == ["_id", "name", "age", "address"]
    assert row.values() == [1, "Ann", 30, {"city": "Oslo"}]
    assert row.items() == list(DOCUMENT.items())
    assert row.to_dict() == DOCUMENT
    assert (row["name"], row.get("address.city"), row.get("nope", "-")) == (
        "Ann",
        "Oslo",
        "-",
    )
    assert ("name" in row, "nope" in row, len(row), list(row)) == (
        True,
        False,
        4,
        row.keys(),
    )
    with pytest.raises(KeyError):
        row["nope"]


def test_row_of_selected_columns() -> None:
    row = MongoDBRow(DOCUMENT, ["address.city", "name", "missing"])

    assert row.keys() == ["address.city", "name", "missing"]
    assert row.values() == ["Oslo", "Ann", None]
    assert row["missing"] is None


async def test_row_scan() -> None:
    row = MongoDBRow(DOCUMENT, ["name", "age"])
    name: list[Any] = [None]
    age: list[Any] = [None]

    await row.scan(name, age)

    assert (name, age) == (["Ann"], [30])


@pytest.mark.parametrize(
    "destinations",
    [
        pytest.param(([None],), id="too few"),
        pytest.param(([None], [None], [None]), id="too many"),
        pytest.param(([None], None), id="not a sequence"),
        pytest.param(([None], []), id="empty sequence"),
        pytest.param(([None], (None,)), id="immutable sequence"),
    ],
)
async def test_row_scan_rejects(destinations: tuple[Any, ...]) -> None:
    with pytest.raises(DataError):
        await MongoDBRow(DOCUMENT, ["name", "age"]).scan(*destinations)


async def test_row_scan_into() -> None:
    person = Person()
    plain = type("Plain", (), {"name": "", "unrelated": "kept"})()

    await MongoDBRow(DOCUMENT).scan_into(person)
    await MongoDBRow(DOCUMENT).scan_into(plain)

    assert person == Person("Ann", 30)
    assert (plain.name, plain.unrelated, hasattr(plain, "age")) == (
        "Ann",
        "kept",
        False,
    )


@pytest.mark.parametrize("target", [FrozenPerson(), Person])
async def test_row_scan_into_rejects(target: Any) -> None:
    with pytest.raises(DataError):
        await MongoDBRow(DOCUMENT).scan_into(target)


def numbered(count: int) -> list[dict[str, Any]]:
    return [{"i": i} for i in range(count)]


async def test_fetchmany_returns_every_document_once() -> None:
    result = MongoDBResult(numbered(6))

    batches = [[row.get("i") for row in await result.fetchmany(2)] for _ in range(4)]

    assert batches == [[0, 1], [2, 3], [4, 5], []]
    assert await result.fetchone() is None


async def test_result_fetch_variants() -> None:
    result = MongoDBResult(numbered(5), ["i"])

    first = await result.fetchone()
    some = await result.fetchmany(1)
    rest = [row.get("i") async for row in result]

    assert first is not None
    assert first.get("i") == 0
    assert [row.get("i") for row in some] == [1]
    assert rest == [2, 3, 4]
    assert await result.fetchall() == []
    assert await result.count() == 5
    assert result.columns() == ["i"]


async def test_result_columns_of_whole_documents() -> None:
    result = MongoDBResult([{"a": 1}, {"b": 2}])

    before = result.columns()
    await result.fetchone()
    current = result.columns()

    assert (before, current) == (["a"], ["a"])
    await result.fetchone()
    assert result.columns() == ["b"]


async def test_closed_result_has_no_rows() -> None:
    result = MongoDBResult(numbered(3))

    await result.close()
    await result.close()

    assert await result.fetchall() == []


class FakeCursor:
    """The part of AsyncCursor that MongoDBResult uses."""

    def __init__(self, documents: list[dict[str, Any]], fail_after: int = -1) -> None:
        self._documents = list(documents)
        self._fail_after = fail_after
        self.read = 0
        self.closed = False

    def __aiter__(self) -> FakeCursor:
        return self

    async def __anext__(self) -> dict[str, Any]:
        if self.read == self._fail_after:
            raise OperationFailure("cursor failed", code=43)
        if not self._documents:
            raise StopAsyncIteration
        self.read += 1
        return self._documents.pop(0)

    async def to_list(self) -> list[dict[str, Any]]:
        return [document async for document in self]

    async def close(self) -> None:
        self.closed = True


def as_cursor(fake: FakeCursor) -> AsyncCursor[dict[str, Any]]:
    return cast(AsyncCursor[dict[str, Any]], fake)


async def test_open_reads_the_first_document_and_closes_when_exhausted() -> None:
    fake = FakeCursor(numbered(3))

    result = await MongoDBResult.open(as_cursor(fake))

    assert fake.read == 1, "the query runs when the result is opened"
    assert result.columns() == ["i"]
    assert [row.get("i") for row in await result.fetchmany(10)] == [0, 1, 2]
    assert fake.closed


async def test_fetchall_of_a_cursor() -> None:
    fake = FakeCursor(numbered(4))
    result = await MongoDBResult.open(as_cursor(fake), ["i"])

    rows = await result.fetchall()

    assert [row.get("i") for row in rows] == [0, 1, 2, 3]
    assert fake.closed


async def test_materialize_holds_no_cursor() -> None:
    fake = FakeCursor(numbered(3))
    result = await MongoDBResult.open(as_cursor(fake), ["i"])

    copy = await result.materialize()

    assert fake.closed
    assert await copy.count() == 3
    assert [row.get("i") for row in await copy.fetchall()] == [0, 1, 2]


@pytest.mark.parametrize("reader", ["fetchone", "fetchall"])
async def test_failed_read_raises_a_query_error_and_closes(reader: str) -> None:
    fake = FakeCursor(numbered(5), fail_after=2)
    result = await MongoDBResult.open(as_cursor(fake))
    await result.fetchone()
    await result.fetchone()

    with pytest.raises(QueryFailedError):
        await getattr(result, reader)()

    assert fake.closed


async def test_failure_of_the_first_read_fails_open() -> None:
    with pytest.raises(QueryFailedError):
        await MongoDBResult.open(as_cursor(FakeCursor(numbered(1), fail_after=0)))


# Error translation


@pytest.mark.parametrize(
    ("error", "expected", "retryable"),
    [
        pytest.param(
            DriverDuplicateKeyError("E11000", code=11000),
            DuplicateKeyError,
            False,
            id="duplicate key",
        ),
        pytest.param(
            ExecutionTimeout("operation exceeded time limit", code=50),
            QueryTimeoutError,
            True,
            id="timeout",
        ),
        pytest.param(
            OperationFailure(
                "WriteConflict",
                code=112,
                details={"errorLabels": ["TransientTransactionError"]},
            ),
            TransactionFailedError,
            True,
            id="transient transaction error",
        ),
        pytest.param(
            OperationFailure("bad", code=2), QueryFailedError, False, id="other failure"
        ),
        pytest.param(
            InvalidDocument("cannot encode object"),
            InvalidQueryError,
            False,
            id="unencodable value",
        ),
        pytest.param(OverflowError("too big"), InvalidQueryError, False, id="int64"),
    ],
)
def test_driver_error(
    error: Exception, expected: type[StorageError], retryable: bool
) -> None:
    translated = driver_error(error, "find on 't' failed")

    assert type(translated) is expected
    assert is_retryable(translated) is retryable


@pytest.mark.parametrize(
    ("error", "expected", "retryable"),
    [
        pytest.param(
            ExecutionTimeout("timeout", code=50),
            TransactionTimeoutError,
            False,
            id="timeout",
        ),
        pytest.param(
            OperationFailure(
                "NoSuchTransaction",
                code=251,
                details={"errorLabels": ["TransientTransactionError"]},
            ),
            TransactionFailedError,
            True,
            id="transient",
        ),
        pytest.param(
            OperationFailure(
                "unknown",
                code=91,
                details={"errorLabels": ["UnknownTransactionCommitResult"]},
            ),
            TransactionFailedError,
            False,
            id="unknown commit result",
        ),
    ],
)
def test_transaction_error(
    error: OperationFailure, expected: type[StorageError], retryable: bool
) -> None:
    translated = transaction_error(error, "failed to commit")

    assert type(translated) is expected
    assert is_retryable(translated) is retryable


# Storage without a server


def unconnected_calls() -> list[Callable[[MongoDBStorage], Awaitable[Any]]]:
    find = query(table="t")
    return [
        lambda s: s.ping(),
        lambda s: s.query(find),
        lambda s: s.query_one(find),
        lambda s: s.execute(command(CommandType.DELETE)),
        lambda s: s.batch([]),
        lambda s: s.begin_tx(),
        lambda s: s.list_tables(),
        lambda s: s.drop_table("t"),
        lambda s: s.describe_table("t"),
    ]


@pytest.mark.parametrize("call", unconnected_calls())
async def test_unconnected_storage_raises_connection_closed(
    call: Callable[[MongoDBStorage], Awaitable[Any]],
) -> None:
    with pytest.raises(ConnectionClosedError):
        await call(MongoDBStorage(Config(host="h", database="d")))


async def test_unconnected_storage_is_unhealthy_and_closes() -> None:
    storage = MongoDBStorage(Config(host="h", database="d"))

    health = await storage.health()
    await storage.close()

    assert health.status is HealthStatusType.UNHEALTHY


@pytest.mark.parametrize(
    ("config", "message"),
    [
        pytest.param(
            Config(host="127.0.0.1", port=1), "database is required", id="no database"
        ),
        pytest.param(
            Config(host="127.0.0.1", port=1, database="bad name"),
            "invalid database name",
            id="invalid database name",
        ),
        pytest.param(
            Config(dsn="mongodb://127.0.0.1:1/d?maxPoolSize=1&minPoolSize=2"),
            "invalid MongoDB client options",
            id="conflicting client options",
        ),
    ],
)
async def test_connect_rejects_invalid_configs_before_any_io(
    config: Config, message: str
) -> None:
    with pytest.raises(InvalidConfigError, match=message):
        await MongoDBAdapter().connect(config)


def index_change(change_type: SchemaChangeType, **index: Any) -> SchemaChange:
    definition = IndexDefinition(**index) if index else None
    return SchemaChange(change_type, index=definition)


@pytest.mark.parametrize(
    ("changes", "error"),
    [
        pytest.param(
            [index_change(SchemaChangeType.ADD_INDEX)],
            InvalidSchemaError,
            id="add without an index",
        ),
        pytest.param(
            [index_change(SchemaChangeType.DROP_INDEX, name="", columns=["a"])],
            InvalidSchemaError,
            id="drop without a name",
        ),
        pytest.param(
            [index_change(SchemaChangeType.ADD_INDEX, name="i", columns=[])],
            InvalidSchemaError,
            id="index without columns",
        ),
        pytest.param(
            [index_change(SchemaChangeType.ADD_INDEX, name="i", columns=["$a"])],
            InvalidQueryError,
            id="index on an operator field",
        ),
        pytest.param(
            [
                index_change(
                    SchemaChangeType.ADD_INDEX,
                    name="i",
                    columns=["a"],
                    index_type=IndexType.GIST,
                )
            ],
            UnsupportedOperationError,
            id="unsupported index type",
        ),
        pytest.param(
            [
                index_change(SchemaChangeType.ADD_INDEX, name="i", columns=["a"]),
                SchemaChange(
                    SchemaChangeType.DROP_COLUMN,
                    column=ColumnDefinition("a", DataType.STRING),
                ),
            ],
            UnsupportedOperationError,
            id="column change after a valid one",
        ),
    ],
)
async def test_alter_table_validates_before_any_io(
    changes: list[SchemaChange], error: type[StorageError]
) -> None:
    with pytest.raises(error):
        await MongoDBStorage(Config(host="h", database="d")).alter_table("t", changes)


@pytest.mark.parametrize(
    ("schema", "error"),
    [
        pytest.param(TableSchema(""), InvalidSchemaError, id="no name"),
        pytest.param(
            TableSchema("t", indexes=[IndexDefinition("i", ["a"], condition="a > 1")]),
            UnsupportedOperationError,
            id="partial index",
        ),
    ],
)
async def test_create_table_validates_before_any_io(
    schema: TableSchema, error: type[StorageError]
) -> None:
    with pytest.raises(error):
        await MongoDBStorage(Config(host="h", database="d")).create_table(schema)


def test_deadlines() -> None:
    soon = deadline_after(datetime.timedelta(hours=1))
    passed = time.monotonic() - 1

    assert soon is not None
    assert deadline_after(None) is None
    assert deadline_after(datetime.timedelta(0)) is None, "zero means no timeout"
    assert positive_seconds(datetime.timedelta(seconds=-1)) is None
    assert positive_seconds(datetime.timedelta(milliseconds=1500)) == 1.5
    assert (earliest(None, None), earliest(None, soon), earliest(soon, None)) == (
        None,
        soon,
        soon,
    )
    assert earliest(soon, passed) == passed
    assert (has_passed(None), has_passed(soon), has_passed(passed)) == (
        False,
        False,
        True,
    )


def test_row_repr_shows_its_columns() -> None:
    assert repr(MongoDBRow(DOCUMENT, ["name"])) == "MongoDBRow({'name': 'Ann'})"


def test_info_lists_implemented_features_only() -> None:
    info = MongoDBStorage(Config(max_open_conns=7)).info()

    assert info.database_type is DatabaseType.MONGODB
    assert info.features == ["transactions", "batch", "schema", "json", "arrays"]
    assert info.limits is not None
    assert info.limits.max_connections == 7
    assert info.limits.max_transaction_age == datetime.timedelta(minutes=1)


@pytest.fixture
async def offline_executor() -> AsyncIterator[Executor]:
    """An executor whose server does not exist: operations that reach it fail."""
    client: AsyncMongoClient[dict[str, Any]] = AsyncMongoClient(
        "mongodb://127.0.0.1:1/", connect=False, serverSelectionTimeoutMS=1
    )
    yield Executor(client["db"], None)
    await client.close()


async def test_limit_zero_sends_nothing(offline_executor: Executor) -> None:
    empty = query(table="t", fields=["a"], limit=0)

    result = await offline_executor.query(empty)
    row = await offline_executor.query_one(empty)

    assert await result.fetchall() == []
    assert result.columns() == ["a"]
    assert row is None


async def test_batch_reports_each_operations_error_in_order(
    offline_executor: Executor,
) -> None:
    operations = [
        Operation(OperationType.QUERY),
        Operation(OperationType.QUERY, query=Query(sql="SELECT 1")),
        Operation(OperationType.COMMAND),
        Operation(OperationType.QUERY, query=query(table="t", limit=0)),
        Operation(OperationType.COMMAND, command=command(CommandType.DELETE)),
    ]

    results = await offline_executor.batch(operations, read_only=True)

    assert [r.index for r in results] == [0, 1, 2, 3, 4]
    assert [type(r.error) for r in results] == [
        InvalidQueryError,
        UnsupportedQueryError,
        InvalidQueryError,
        type(None),
        InvalidTransactionError,
    ]
    assert isinstance(results[3].result, MongoDBResult)
