"""
Schema support for the PostgreSQL adapter: mapping between storage data types,
Python values and PostgreSQL types, and the DDL of ``create_table`` and
``alter_table``.

Every identifier is validated and quoted, and default values and comments are
rendered as quoted literals. ``IndexDefinition.condition`` is the exception: it
is an SQL predicate (for a partial index) inserted as written, so, like
``Query.sql``, it must never contain untrusted input; it is rejected if it holds
a ``;`` outside literals and comments.
"""

from __future__ import annotations

import datetime
import decimal
import uuid
from typing import Any

from storage.adapters.postgresql.sql import (
    has_statement_separator,
    quote_identifier,
    quote_literal,
    quote_table,
    split_table_name,
)
from storage.errors import (
    InvalidSchemaError,
    ValidationError,
    new_unsupported_operation_error,
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

_DATABASE_TYPES: dict[str, DataType] = {
    **dict.fromkeys(
        ("text", "varchar", "char", "character", "character varying", "bpchar", "name"),
        DataType.STRING,
    ),
    **dict.fromkeys(
        (
            *("smallint", "int2", "integer", "int", "int4", "bigint", "int8"),
            *("smallserial", "serial2", "serial", "serial4", "bigserial", "serial8"),
        ),
        DataType.INTEGER,
    ),
    **dict.fromkeys(
        ("real", "float4", "double precision", "float8", "float"), DataType.FLOAT
    ),
    **dict.fromkeys(("numeric", "decimal"), DataType.DECIMAL),
    **dict.fromkeys(("boolean", "bool"), DataType.BOOLEAN),
    **dict.fromkeys(
        (
            "timestamp",
            "timestamptz",
            "timestamp with time zone",
            "timestamp without time zone",
        ),
        DataType.DATETIME,
    ),
    "date": DataType.DATE,
    **dict.fromkeys(
        ("time", "timetz", "time with time zone", "time without time zone"),
        DataType.TIME,
    ),
    "bytea": DataType.BINARY,
    **dict.fromkeys(("json", "jsonb"), DataType.JSON),
    "uuid": DataType.UUID,
}

_COLUMN_TYPES: dict[DataType, str] = {
    DataType.INTEGER: "INTEGER",
    DataType.FLOAT: "DOUBLE PRECISION",
    DataType.BOOLEAN: "BOOLEAN",
    DataType.DATETIME: "TIMESTAMP",
    DataType.DATE: "DATE",
    DataType.TIME: "TIME",
    DataType.BINARY: "BYTEA",
    DataType.JSON: "JSONB",
    DataType.UUID: "UUID",
    DataType.ARRAY: "TEXT[]",
}

_INDEX_METHODS: dict[IndexType, str] = {
    IndexType.BTREE: "btree",
    IndexType.HASH: "hash",
    IndexType.GIN: "gin",
    IndexType.GIST: "gist",
    IndexType.BRIN: "brin",
}


def map_database_type(db_type: str) -> DataType:
    """Map a PostgreSQL type name, as written in SQL or reported by
    ``information_schema`` (``character varying(255)``, ``integer[]``,
    ``_int4``, ``ARRAY``, ``timestamp(3) with time zone``), to a data type.

    Raises:
        ValidationError: If the type has no data type.
    """
    name = db_type.strip().lower()
    if name.endswith("[]") or (len(name) > 1 and name[0] == "_") or name == "array":
        return DataType.ARRAY
    start = name.find("(")
    end = name.find(")", start)
    if start >= 0 and end >= 0:  # drop type modifiers
        name = name[:start] + " " + name[end + 1 :]
    found = _DATABASE_TYPES.get(" ".join(name.split()))
    if found is None:
        raise ValidationError(f"unsupported PostgreSQL type: {db_type}")
    return found


def map_python_type(value: Any) -> DataType:
    """Return the data type that stores a Python value such as ``value``;
    ``None`` gives ``DataType.UNKNOWN``.

    Raises:
        ValidationError: If the type has no data type.
    """
    checks: tuple[tuple[type | tuple[type, ...], DataType], ...] = (
        (bool, DataType.BOOLEAN),  # before int: bool is an int
        (int, DataType.INTEGER),
        (float, DataType.FLOAT),
        (decimal.Decimal, DataType.DECIMAL),
        (str, DataType.STRING),
        ((bytes, bytearray, memoryview), DataType.BINARY),
        (datetime.datetime, DataType.DATETIME),  # before date: a datetime is a date
        (datetime.date, DataType.DATE),
        (datetime.time, DataType.TIME),
        (uuid.UUID, DataType.UUID),
        (dict, DataType.JSON),
        ((list, tuple), DataType.ARRAY),
    )
    if value is None:
        return DataType.UNKNOWN
    for types, data_type in checks:
        if isinstance(value, types):
            return data_type
    raise ValidationError(f"unsupported Python type: {type(value).__name__}")


def column_type(column: ColumnDefinition) -> str:
    """The PostgreSQL type of a column: ``VARCHAR(length)`` or ``TEXT`` for
    strings, ``NUMERIC(precision, scale)`` for decimals and ``TEXT`` for text and
    types without a better match (maps and unknown)."""
    data_type = column.data_type
    if data_type is DataType.STRING:
        length = _size(column.length, "length")
        return f"VARCHAR({length})" if length else "TEXT"
    if data_type is DataType.DECIMAL:
        precision = _size(column.precision, "precision")
        scale = _size(column.scale, "scale")
        if precision and scale:
            return f"NUMERIC({precision},{scale})"
        return f"NUMERIC({precision})" if precision else "NUMERIC"
    return _COLUMN_TYPES.get(data_type, "TEXT")


def _size(value: Any, what: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise InvalidSchemaError(f"column {what} must be a non-negative integer")
    return value


def column_definition(column: ColumnDefinition, *, inline_primary_key: bool) -> str:
    """Render a column for ``CREATE TABLE`` or ``ADD COLUMN``.

    ``auto_increment`` gives ``GENERATED BY DEFAULT AS IDENTITY``; the default
    value is a literal (an SQL expression such as ``now()`` cannot be given).
    """
    parts = [quote_identifier(column.name, "column"), column_type(column)]
    if column.auto_increment:
        parts.append("GENERATED BY DEFAULT AS IDENTITY")
    if not column.nullable:
        parts.append("NOT NULL")
    if column.default_value is not None:
        parts.append("DEFAULT " + quote_literal(column.default_value))
    if column.unique:
        parts.append("UNIQUE")
    if column.primary_key and inline_primary_key:
        parts.append("PRIMARY KEY")
    return " ".join(parts)


def create_table_statements(schema: TableSchema) -> list[str]:
    """Render ``CREATE TABLE`` followed by its ``CREATE INDEX`` and ``COMMENT``
    statements.

    The primary key is ``schema.primary_key`` followed by the columns flagged
    ``primary_key``. ``schema.foreign_keys`` maps a column to the column it
    references, as ``"table.column"`` or ``"schema.table.column"``.
    """
    table = quote_table(schema.name)
    definitions = [
        column_definition(column, inline_primary_key=False) for column in schema.columns
    ]
    keys = list(schema.primary_key)
    keys += [c.name for c in schema.columns if c.primary_key and c.name not in keys]
    if keys:
        definitions.append(f"PRIMARY KEY ({_quote_columns(keys)})")
    for column, reference in schema.foreign_keys.items():
        definitions.append(_foreign_key(column, reference))
    statements = [f"CREATE TABLE {table} ({', '.join(definitions)})"]
    statements += [
        create_index_statement(schema.name, index) for index in schema.indexes
    ]
    statements += [
        comment_statement(schema.name, column)
        for column in schema.columns
        if column.comment
    ]
    return statements


def alter_table_statements(table: str, change: SchemaChange) -> list[str]:
    """Render one schema change.

    ``MODIFY_COLUMN`` makes the column match the definition: its type,
    nullability, default and comment (its primary key, identity and uniqueness
    are not changed). ``DROP_INDEX`` drops the named index of the table's schema.

    Raises:
        InvalidSchemaError: If the change lacks its column or index.
        UnsupportedOperationError: For an unknown change type.
    """
    alter = f"ALTER TABLE {quote_table(table)} "
    kind = change.change_type
    if kind in (
        SchemaChangeType.ADD_COLUMN,
        SchemaChangeType.DROP_COLUMN,
        SchemaChangeType.MODIFY_COLUMN,
    ):
        column = change.column
        if column is None:
            raise InvalidSchemaError(f"{kind.value} needs a column definition")
        if kind is SchemaChangeType.ADD_COLUMN:
            statements = [
                alter
                + "ADD COLUMN "
                + column_definition(column, inline_primary_key=True)
            ]
            if column.comment:
                statements.append(comment_statement(table, column))
            return statements
        if kind is SchemaChangeType.DROP_COLUMN:
            return [alter + "DROP COLUMN " + quote_identifier(column.name, "column")]
        return [_modify_column(alter, column), comment_statement(table, column)]
    if kind in (SchemaChangeType.ADD_INDEX, SchemaChangeType.DROP_INDEX):
        index = change.index
        if index is None:
            raise InvalidSchemaError(f"{kind.value} needs an index definition")
        if kind is SchemaChangeType.ADD_INDEX:
            return [create_index_statement(table, index)]
        name = quote_identifier(index.name, "index")
        schema, _ = split_table_name(table)
        if schema is not None:  # an index lives in its table's schema
            name = f"{quote_identifier(schema, 'schema')}.{name}"
        return [f"DROP INDEX {name}"]
    raise new_unsupported_operation_error(f"schema change {kind!r}")


def _modify_column(alter: str, column: ColumnDefinition) -> str:
    prefix = f"ALTER COLUMN {quote_identifier(column.name, 'column')} "
    actions = [prefix + "TYPE " + column_type(column)]
    if column.nullable and not column.primary_key and not column.auto_increment:
        actions.append(prefix + "DROP NOT NULL")
    else:
        actions.append(prefix + "SET NOT NULL")
    if not column.auto_increment:  # identity columns keep their generated default
        if column.default_value is None:
            actions.append(prefix + "DROP DEFAULT")
        else:
            actions.append(
                prefix + "SET DEFAULT " + quote_literal(column.default_value)
            )
    return alter + ", ".join(actions)


def create_index_statement(table: str, index: IndexDefinition) -> str:
    """Render ``CREATE [UNIQUE] INDEX`` (unnamed when ``index.name`` is empty)."""
    if not index.columns:
        raise InvalidSchemaError("an index needs at least one column")
    method = _INDEX_METHODS.get(index.index_type)
    if method is None:
        raise InvalidSchemaError(f"unsupported index type {index.index_type!r}")
    parts = ["CREATE UNIQUE INDEX" if index.unique else "CREATE INDEX"]
    if index.name:
        parts.append(quote_identifier(index.name, "index"))
    parts.append(
        f"ON {quote_table(table)} USING {method} ({_quote_columns(index.columns)})"
    )
    if index.condition:
        if has_statement_separator(index.condition):
            raise InvalidSchemaError(
                "an index condition must be a single SQL expression"
            )
        parts.append(f"WHERE ({index.condition})")
    return " ".join(parts)


def comment_statement(table: str, column: ColumnDefinition) -> str:
    """Render ``COMMENT ON COLUMN``; an empty comment removes the comment."""
    comment = quote_literal(column.comment) if column.comment else "NULL"
    return (
        f"COMMENT ON COLUMN {quote_table(table)}."
        f"{quote_identifier(column.name, 'column')} IS {comment}"
    )


def _foreign_key(column: str, reference: str) -> str:
    table, dot, referenced = reference.rpartition(".")
    if not dot or not table:
        raise InvalidSchemaError(
            f"the foreign key of column {column} must reference "
            "'table.column' or 'schema.table.column'"
        )
    return (
        f"FOREIGN KEY ({quote_identifier(column, 'column')}) REFERENCES "
        f"{quote_table(table)} ({quote_identifier(referenced, 'column')})"
    )


def _quote_columns(names: list[str]) -> str:
    return ", ".join(quote_identifier(name, "column") for name in names)


LIST_TABLES_SQL = """SELECT table_name FROM information_schema.tables
WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
ORDER BY table_name"""

# $1: the schema (NULL for the current schema), $2: the table.
DESCRIBE_TABLE_SQL = """SELECT c.column_name, c.data_type, c.udt_name, c.is_nullable,
    c.column_default, c.character_maximum_length, c.numeric_precision,
    c.numeric_scale, c.is_identity,
    col_description(format('%I.%I', c.table_schema, c.table_name)::regclass,
                    c.ordinal_position::int) AS comment,
    (SELECT kcu.ordinal_position
     FROM information_schema.table_constraints tc
     JOIN information_schema.key_column_usage kcu
       ON kcu.constraint_schema = tc.constraint_schema
      AND kcu.constraint_name = tc.constraint_name
      AND kcu.table_schema = tc.table_schema
      AND kcu.table_name = tc.table_name
     WHERE tc.constraint_type = 'PRIMARY KEY'
       AND tc.table_schema = c.table_schema
       AND tc.table_name = c.table_name
       AND kcu.column_name = c.column_name) AS key_position
FROM information_schema.columns c
WHERE c.table_schema = COALESCE($1::text, current_schema()) AND c.table_name = $2
ORDER BY c.ordinal_position"""

TABLE_EXISTS_SQL = """SELECT EXISTS (SELECT 1 FROM information_schema.tables
WHERE table_schema = COALESCE($1::text, current_schema()) AND table_name = $2)"""


def describe_table(name: str, records: list[Any]) -> TableSchema:
    """Build the schema of table ``name`` from the rows of ``DESCRIBE_TABLE_SQL``.

    Columns report their type, nullability, primary key, identity (or serial),
    default expression (as SQL text), length, precision and scale (decimals)
    and comment. Indexes, uniqueness and foreign keys are not reported.
    """
    columns: list[ColumnDefinition] = []
    keys: list[tuple[int, str]] = []
    for record in records:
        data_type = _described_type(record["data_type"], record["udt_name"])
        default = record["column_default"]
        decimal_type = data_type is DataType.DECIMAL
        column = ColumnDefinition(
            name=record["column_name"],
            data_type=data_type,
            nullable=record["is_nullable"] == "YES",
            primary_key=record["key_position"] is not None,
            auto_increment=record["is_identity"] == "YES"
            or (default or "").startswith("nextval("),
            default_value=default,
            length=record["character_maximum_length"] or 0,
            precision=(record["numeric_precision"] or 0) if decimal_type else 0,
            scale=(record["numeric_scale"] or 0) if decimal_type else 0,
            comment=record["comment"] or "",
        )
        columns.append(column)
        if column.primary_key:
            keys.append((record["key_position"], column.name))
    return TableSchema(
        name=name, columns=columns, primary_key=[key for _, key in sorted(keys)]
    )


def _described_type(data_type: str, udt_name: str) -> DataType:
    for name in (data_type, udt_name):  # udt_name for USER-DEFINED and named types
        try:
            return map_database_type(name)
        except ValidationError:
            continue
    return DataType.UNKNOWN
