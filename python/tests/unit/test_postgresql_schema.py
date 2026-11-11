"""Unit tests for PostgreSQL type mapping and DDL rendering."""

from __future__ import annotations

import datetime
import decimal
import uuid
from typing import Any

import pytest

from storage.adapters.postgresql import PostgreSQLAdapter
from storage.adapters.postgresql.schema import (
    alter_table_statements,
    column_type,
    create_table_statements,
    describe_table,
    map_database_type,
    map_python_type,
)
from storage.errors import (
    InvalidSchemaError,
    UnsupportedOperationError,
    ValidationError,
)
from storage.types import (
    ColumnDefinition,
    DataType,
    IndexDefinition,
    IndexType,
    SchemaChange,
    SchemaChangeType,
    TableSchema,
)


@pytest.mark.parametrize(
    ("db_type", "expected"),
    [
        ("text", DataType.STRING),
        ("character varying(255)", DataType.STRING),
        ("VARCHAR(10)", DataType.STRING),
        ("bpchar", DataType.STRING),
        ("integer", DataType.INTEGER),
        ("bigserial", DataType.INTEGER),
        ("int8", DataType.INTEGER),
        ("double precision", DataType.FLOAT),
        ("real", DataType.FLOAT),
        ("numeric", DataType.DECIMAL),
        ("numeric(10,2)", DataType.DECIMAL),
        ("decimal", DataType.DECIMAL),
        ("boolean", DataType.BOOLEAN),
        ("timestamp(3) with time zone", DataType.DATETIME),
        ("timestamp without time zone", DataType.DATETIME),
        ("timestamptz", DataType.DATETIME),
        ("date", DataType.DATE),
        ("time with time zone", DataType.TIME),
        ("bytea", DataType.BINARY),
        ("jsonb", DataType.JSON),
        ("uuid", DataType.UUID),
        ("integer[]", DataType.ARRAY),
        ("_int4", DataType.ARRAY),
        ("ARRAY", DataType.ARRAY),
        ("  Integer  ", DataType.INTEGER),
    ],
)
def test_map_database_type(db_type: str, expected: DataType) -> None:
    assert map_database_type(db_type) is expected
    assert PostgreSQLAdapter().map_database_type(db_type) is expected


@pytest.mark.parametrize("db_type", ["interval", "inet", "USER-DEFINED", "", "_"])
def test_map_database_type_rejects_unknown_types(db_type: str) -> None:
    with pytest.raises(ValidationError):
        map_database_type(db_type)


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        (None, DataType.UNKNOWN),
        (True, DataType.BOOLEAN),
        (1, DataType.INTEGER),
        (1.5, DataType.FLOAT),
        (decimal.Decimal("1.5"), DataType.DECIMAL),
        ("s", DataType.STRING),
        (b"b", DataType.BINARY),
        (bytearray(b"b"), DataType.BINARY),
        (datetime.datetime(2024, 1, 1, tzinfo=datetime.UTC), DataType.DATETIME),
        (datetime.date(2024, 1, 1), DataType.DATE),
        (datetime.time(1, 2), DataType.TIME),
        (uuid.uuid4(), DataType.UUID),
        ({"a": 1}, DataType.JSON),
        ([1, 2], DataType.ARRAY),
        ((1, 2), DataType.ARRAY),
    ],
)
def test_map_python_type(value: Any, expected: DataType) -> None:
    assert map_python_type(value) is expected
    assert PostgreSQLAdapter().map_python_type(value) is expected


@pytest.mark.parametrize("value", [object(), datetime.timedelta(1), {1, 2}])
def test_map_python_type_rejects_unknown_types(value: Any) -> None:
    with pytest.raises(ValidationError):
        map_python_type(value)


@pytest.mark.parametrize(
    ("column", "expected"),
    [
        (ColumnDefinition("c", DataType.STRING), "TEXT"),
        (ColumnDefinition("c", DataType.STRING, length=255), "VARCHAR(255)"),
        (ColumnDefinition("c", DataType.TEXT), "TEXT"),
        (ColumnDefinition("c", DataType.INTEGER), "INTEGER"),
        (ColumnDefinition("c", DataType.FLOAT), "DOUBLE PRECISION"),
        (ColumnDefinition("c", DataType.DECIMAL), "NUMERIC"),
        (ColumnDefinition("c", DataType.DECIMAL, precision=10), "NUMERIC(10)"),
        (
            ColumnDefinition("c", DataType.DECIMAL, precision=10, scale=2),
            "NUMERIC(10,2)",
        ),
        (ColumnDefinition("c", DataType.BOOLEAN), "BOOLEAN"),
        (ColumnDefinition("c", DataType.DATETIME), "TIMESTAMP"),
        (ColumnDefinition("c", DataType.DATE), "DATE"),
        (ColumnDefinition("c", DataType.TIME), "TIME"),
        (ColumnDefinition("c", DataType.BINARY), "BYTEA"),
        (ColumnDefinition("c", DataType.JSON), "JSONB"),
        (ColumnDefinition("c", DataType.UUID), "UUID"),
        (ColumnDefinition("c", DataType.ARRAY), "TEXT[]"),
        (ColumnDefinition("c", DataType.MAP), "TEXT"),
        (ColumnDefinition("c", DataType.UNKNOWN), "TEXT"),
    ],
)
def test_column_type(column: ColumnDefinition, expected: str) -> None:
    assert column_type(column) == expected


@pytest.mark.parametrize(
    "column",
    [
        ColumnDefinition("c", DataType.STRING, length=-1),
        ColumnDefinition("c", DataType.STRING, length="10) ; DROP TABLE t; --"),  # type: ignore[arg-type]
        ColumnDefinition("c", DataType.DECIMAL, precision=True),
    ],
)
def test_column_type_rejects_invalid_sizes(column: ColumnDefinition) -> None:
    with pytest.raises(InvalidSchemaError):
        column_type(column)


def test_create_table() -> None:
    schema = TableSchema(
        name="app.orders",
        columns=[
            ColumnDefinition(
                "id", DataType.INTEGER, nullable=False, auto_increment=True
            ),
            ColumnDefinition(
                "code", DataType.STRING, length=20, nullable=False, unique=True
            ),
            ColumnDefinition(
                "status", DataType.STRING, default_value="new", comment="it's new"
            ),
            ColumnDefinition(
                "total", DataType.DECIMAL, precision=10, scale=2, default_value=0
            ),
            ColumnDefinition("user_id", DataType.INTEGER),
        ],
        indexes=[
            IndexDefinition("orders_status_idx", ["status"]),
            IndexDefinition(
                "orders_open_idx",
                ["user_id", "code"],
                unique=True,
                condition="status <> 'closed'",
            ),
            IndexDefinition("orders_code_idx", ["code"], index_type=IndexType.HASH),
            IndexDefinition("", ["total"], index_type=IndexType.BRIN),
        ],
        primary_key=["id"],
        foreign_keys={"user_id": "app.users.id"},
    )
    assert create_table_statements(schema) == [
        'CREATE TABLE "app"."orders" ('
        '"id" INTEGER GENERATED BY DEFAULT AS IDENTITY NOT NULL, '
        '"code" VARCHAR(20) NOT NULL UNIQUE, '
        "\"status\" TEXT DEFAULT 'new', "
        '"total" NUMERIC(10,2) DEFAULT 0, '
        '"user_id" INTEGER, '
        'PRIMARY KEY ("id"), '
        'FOREIGN KEY ("user_id") REFERENCES "app"."users" ("id"))',
        'CREATE INDEX "orders_status_idx" ON "app"."orders" USING btree ("status")',
        'CREATE UNIQUE INDEX "orders_open_idx" ON "app"."orders" USING btree '
        '("user_id", "code") WHERE (status <> \'closed\')',
        'CREATE INDEX "orders_code_idx" ON "app"."orders" USING hash ("code")',
        'CREATE INDEX ON "app"."orders" USING brin ("total")',
        'COMMENT ON COLUMN "app"."orders"."status" IS \'it\'\'s new\'',
    ]


def test_create_table_primary_key_from_columns_and_schema() -> None:
    schema = TableSchema(
        name="t",
        columns=[
            ColumnDefinition("a", DataType.INTEGER, primary_key=True),
            ColumnDefinition("b", DataType.INTEGER, primary_key=True),
        ],
        primary_key=["b"],
    )
    assert create_table_statements(schema) == [
        'CREATE TABLE "t" ("a" INTEGER, "b" INTEGER, PRIMARY KEY ("b", "a"))'
    ]


def test_create_table_quotes_hostile_names_and_defaults() -> None:
    schema = TableSchema(
        name='t"; DROP TABLE victims; --',
        columns=[
            ColumnDefinition(
                'c"x', DataType.STRING, default_value="'); DROP TABLE victims; --"
            )
        ],
    )
    assert create_table_statements(schema) == [
        'CREATE TABLE "t""; DROP TABLE victims; --" '
        "(\"c\"\"x\" TEXT DEFAULT '''); DROP TABLE victims; --')"
    ]


@pytest.mark.parametrize(
    "schema",
    [
        TableSchema(name=""),
        TableSchema(name="a.b.c"),
        TableSchema(name="t", columns=[ColumnDefinition("", DataType.INTEGER)]),
        TableSchema(
            name="t",
            columns=[ColumnDefinition("c", DataType.INTEGER, default_value=[1])],
        ),
        TableSchema(name="t", foreign_keys={"c": "users"}),
        TableSchema(name="t", foreign_keys={"c": ".id"}),
        TableSchema(name="t", indexes=[IndexDefinition("i", [])]),
        TableSchema(
            name="t", indexes=[IndexDefinition("i", ["c"], condition="x; DROP TABLE t")]
        ),
        TableSchema(
            name="t",
            indexes=[IndexDefinition("i", ["c"], index_type="spatial")],  # type: ignore[arg-type]
        ),
    ],
)
def test_create_table_rejects_invalid_schemas(schema: TableSchema) -> None:
    with pytest.raises(InvalidSchemaError):
        create_table_statements(schema)


def _change(kind: SchemaChangeType, **parts: Any) -> SchemaChange:
    return SchemaChange(kind, **parts)


@pytest.mark.parametrize(
    ("table", "change", "expected"),
    [
        (
            "t",
            _change(
                SchemaChangeType.ADD_COLUMN,
                column=ColumnDefinition(
                    "age",
                    DataType.INTEGER,
                    nullable=False,
                    default_value=0,
                    comment="years",
                ),
            ),
            [
                'ALTER TABLE "t" ADD COLUMN "age" INTEGER NOT NULL DEFAULT 0',
                'COMMENT ON COLUMN "t"."age" IS \'years\'',
            ],
        ),
        (
            "t",
            _change(
                SchemaChangeType.ADD_COLUMN,
                column=ColumnDefinition("id", DataType.INTEGER, primary_key=True),
            ),
            ['ALTER TABLE "t" ADD COLUMN "id" INTEGER PRIMARY KEY'],
        ),
        (
            "s.t",
            _change(
                SchemaChangeType.DROP_COLUMN,
                column=ColumnDefinition("age", DataType.INTEGER),
            ),
            ['ALTER TABLE "s"."t" DROP COLUMN "age"'],
        ),
        (
            "t",
            _change(
                SchemaChangeType.MODIFY_COLUMN,
                column=ColumnDefinition(
                    "name", DataType.STRING, length=50, nullable=False
                ),
            ),
            [
                'ALTER TABLE "t" ALTER COLUMN "name" TYPE VARCHAR(50), '
                'ALTER COLUMN "name" SET NOT NULL, ALTER COLUMN "name" DROP DEFAULT',
                'COMMENT ON COLUMN "t"."name" IS NULL',
            ],
        ),
        (
            "t",
            _change(
                SchemaChangeType.MODIFY_COLUMN,
                column=ColumnDefinition(
                    "n", DataType.INTEGER, default_value=1, comment="c"
                ),
            ),
            [
                'ALTER TABLE "t" ALTER COLUMN "n" TYPE INTEGER, '
                'ALTER COLUMN "n" DROP NOT NULL, ALTER COLUMN "n" SET DEFAULT 1',
                'COMMENT ON COLUMN "t"."n" IS \'c\'',
            ],
        ),
        (
            "t",
            _change(
                SchemaChangeType.MODIFY_COLUMN,
                column=ColumnDefinition("id", DataType.INTEGER, auto_increment=True),
            ),
            [
                'ALTER TABLE "t" ALTER COLUMN "id" TYPE INTEGER, '
                'ALTER COLUMN "id" SET NOT NULL',
                'COMMENT ON COLUMN "t"."id" IS NULL',
            ],
        ),
        (
            "t",
            _change(
                SchemaChangeType.ADD_INDEX,
                index=IndexDefinition("t_a", ["a"], unique=True),
            ),
            ['CREATE UNIQUE INDEX "t_a" ON "t" USING btree ("a")'],
        ),
        (
            "s.t",
            _change(SchemaChangeType.DROP_INDEX, index=IndexDefinition("t_a", [])),
            ['DROP INDEX "s"."t_a"'],
        ),
        (
            "t",
            _change(SchemaChangeType.DROP_INDEX, index=IndexDefinition("t_a", [])),
            ['DROP INDEX "t_a"'],
        ),
    ],
)
def test_alter_table(table: str, change: SchemaChange, expected: list[str]) -> None:
    assert alter_table_statements(table, change) == expected


@pytest.mark.parametrize(
    "change",
    [
        _change(SchemaChangeType.ADD_COLUMN),
        _change(SchemaChangeType.DROP_COLUMN),
        _change(SchemaChangeType.MODIFY_COLUMN),
        _change(SchemaChangeType.ADD_INDEX),
        _change(SchemaChangeType.DROP_INDEX),
        _change(SchemaChangeType.DROP_INDEX, index=IndexDefinition("", [])),
    ],
)
def test_alter_table_rejects_incomplete_changes(change: SchemaChange) -> None:
    with pytest.raises(InvalidSchemaError):
        alter_table_statements("t", change)


def test_alter_table_rejects_unknown_change_types() -> None:
    change = SchemaChange("rename_table")  # type: ignore[arg-type]
    with pytest.raises(UnsupportedOperationError):
        alter_table_statements("t", change)


def test_alter_table_quotes_hostile_names() -> None:
    change = _change(
        SchemaChangeType.DROP_COLUMN,
        column=ColumnDefinition('x"; DROP TABLE t; --', DataType.INTEGER),
    )
    assert alter_table_statements("t", change) == [
        'ALTER TABLE "t" DROP COLUMN "x""; DROP TABLE t; --"'
    ]


def test_describe_table_builds_columns_and_primary_key() -> None:
    def record(name: str, data_type: str, **values: Any) -> dict[str, Any]:
        base: dict[str, Any] = {
            "column_name": name,
            "data_type": data_type,
            "udt_name": data_type,
            "is_nullable": "YES",
            "column_default": None,
            "character_maximum_length": None,
            "numeric_precision": None,
            "numeric_scale": None,
            "is_identity": "NO",
            "comment": None,
            "key_position": None,
        }
        base.update(values)
        return base

    schema = describe_table(
        "t",
        [
            record(
                "b",
                "integer",
                is_nullable="NO",
                is_identity="YES",
                key_position=2,
                numeric_precision=32,
            ),
            record(
                "a",
                "integer",
                is_nullable="NO",
                key_position=1,
                column_default="nextval('t_a_seq'::regclass)",
            ),
            record("price", "numeric", numeric_precision=10, numeric_scale=2),
            record(
                "name",
                "character varying",
                character_maximum_length=40,
                column_default="'x'::character varying",
                comment="the name",
            ),
            record("tags", "ARRAY", udt_name="_text"),
            record("mood", "USER-DEFINED", udt_name="mood"),
        ],
    )
    b, a, price, name, tags, mood = schema.columns
    assert schema.name == "t"
    assert schema.primary_key == ["a", "b"]
    assert (b.primary_key, b.auto_increment, b.nullable, b.precision) == (
        True,
        True,
        False,
        0,
    )
    assert (a.auto_increment, a.default_value) == (True, "nextval('t_a_seq'::regclass)")
    assert (price.data_type, price.precision, price.scale) == (DataType.DECIMAL, 10, 2)
    assert (name.data_type, name.length, name.comment) == (
        DataType.STRING,
        40,
        "the name",
    )
    assert name.default_value == "'x'::character varying"
    assert tags.data_type is DataType.ARRAY
    assert mood.data_type is DataType.UNKNOWN
    assert schema.indexes == []
    assert schema.foreign_keys == {}
