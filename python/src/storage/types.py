"""
Type definitions for the Database Agnostic Storage Library.

These mirror ``internal/domain/types.go`` of the Go implementation with Pythonic
naming. SQL adapters execute ``Query.sql`` / ``Command.sql`` with their
``parameters``; document adapters (MongoDB) cannot execute SQL text and execute the
structured ``spec`` that the query builder always populates.
"""

from __future__ import annotations

import dataclasses
import datetime
from dataclasses import dataclass, field
from enum import Enum
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from storage.interfaces import Result


class DatabaseType(Enum):
    """Supported database types."""

    POSTGRESQL = "postgresql"
    MYSQL = "mysql"
    SQLITE = "sqlite"
    REDIS = "redis"
    MONGODB = "mongodb"
    COCKROACHDB = "cockroachdb"
    DYNAMODB = "dynamodb"
    CASSANDRA = "cassandra"
    UNKNOWN = "unknown"


class DataType(Enum):
    """Universal data types supported across databases."""

    STRING = "string"
    INTEGER = "integer"
    FLOAT = "float"
    BOOLEAN = "boolean"
    DATETIME = "datetime"
    DATE = "date"
    TIME = "time"
    BINARY = "binary"
    JSON = "json"
    UUID = "uuid"
    ARRAY = "array"
    MAP = "map"
    DECIMAL = "decimal"
    TEXT = "text"
    UNKNOWN = "unknown"


class QueryType(Enum):
    """Types of database queries."""

    SELECT = "select"
    INSERT = "insert"
    UPDATE = "update"
    DELETE = "delete"
    CREATE = "create"
    ALTER = "alter"
    DROP = "drop"
    UNKNOWN = "unknown"


class CommandType(Enum):
    """Types of database commands."""

    INSERT = "insert"
    UPDATE = "update"
    DELETE = "delete"
    CREATE = "create"
    ALTER = "alter"
    DROP = "drop"
    UNKNOWN = "unknown"


class OperationType(Enum):
    """Types of batch operations."""

    QUERY = "query"
    COMMAND = "command"


class IsolationLevel(Enum):
    """Transaction isolation levels. ``DEFAULT`` uses the database's own default."""

    DEFAULT = "default"
    READ_UNCOMMITTED = "read_uncommitted"
    READ_COMMITTED = "read_committed"
    REPEATABLE_READ = "repeatable_read"
    SERIALIZABLE = "serializable"


class SortDirection(Enum):
    """Sort directions for ORDER BY clauses."""

    ASC = "asc"
    DESC = "desc"


class Operator(Enum):
    """Operators for conditions.

    ``AND`` and ``OR`` combine the operands in ``Condition.conditions``;
    ``EXISTS`` and ``NOT_EXISTS`` take a query builder (the subquery) as value.
    """

    EQUAL = "equal"
    NOT_EQUAL = "not_equal"
    GREATER_THAN = "greater_than"
    GREATER_THAN_OR_EQUAL = "greater_than_or_equal"
    LESS_THAN = "less_than"
    LESS_THAN_OR_EQUAL = "less_than_or_equal"
    LIKE = "like"
    NOT_LIKE = "not_like"
    IN = "in"
    NOT_IN = "not_in"
    BETWEEN = "between"
    NOT_BETWEEN = "not_between"
    IS_NULL = "is_null"
    IS_NOT_NULL = "is_not_null"
    EXISTS = "exists"
    NOT_EXISTS = "not_exists"
    AND = "and"
    OR = "or"


class JoinType(Enum):
    """Types of SQL joins."""

    INNER = "inner"
    LEFT = "left"
    RIGHT = "right"
    FULL = "full"
    CROSS = "cross"


class SchemaChangeType(Enum):
    """Types of schema changes."""

    ADD_COLUMN = "add_column"
    DROP_COLUMN = "drop_column"
    MODIFY_COLUMN = "modify_column"
    ADD_INDEX = "add_index"
    DROP_INDEX = "drop_index"


class IndexType(Enum):
    """Types of database indexes."""

    BTREE = "btree"
    HASH = "hash"
    GIN = "gin"
    GIST = "gist"
    BRIN = "brin"


class HealthStatusType(Enum):
    """Health status types."""

    HEALTHY = "healthy"
    DEGRADED = "degraded"
    UNHEALTHY = "unhealthy"
    UNKNOWN = "unknown"


class ErrorType(Enum):
    """Categories of storage errors."""

    CONNECTION = "connection"
    QUERY = "query"
    TRANSACTION = "transaction"
    DATA = "data"
    SCHEMA = "schema"
    ADAPTER = "adapter"
    VALIDATION = "validation"
    TIMEOUT = "timeout"
    PERMISSION = "permission"
    RESOURCE = "resource"
    UNKNOWN = "unknown"


@dataclass(frozen=True)
class ColumnRef:
    """Marks a condition value as a column reference, never a bound parameter.

    Example: ``Condition("a.id", Operator.EQUAL, ColumnRef("b.a_id"))`` renders
    ``a.id = b.a_id`` (as in ``JOIN b ON a.id = b.a_id``).
    """

    name: str


@dataclass
class Condition:
    """A condition in a WHERE, HAVING or JOIN ... ON clause.

    ``value`` is a bound parameter, unless it is a ``ColumnRef`` (rendered as a
    column) or the operator is ``EXISTS``/``NOT_EXISTS`` (``value`` is a query
    builder). ``values`` holds the operands of ``IN``/``NOT_IN`` and
    ``BETWEEN``/``NOT_BETWEEN``. For ``AND``/``OR`` the operands are in
    ``conditions`` and ``field`` is unused.
    """

    field: str
    operator: Operator
    value: Any = None
    values: list[Any] | None = None
    # dataclasses.field, because the ``field`` attribute above shadows the bare
    # name inside this class body for type checkers.
    conditions: list[Condition] = dataclasses.field(default_factory=list)


@dataclass
class OrderBy:
    """A single ORDER BY term."""

    field: str
    direction: SortDirection = SortDirection.ASC


@dataclass
class Join:
    """A single JOIN clause; ``on`` is ``None`` only for ``JoinType.CROSS``."""

    join_type: JoinType
    table: str
    on: Condition | None = None


@dataclass
class QuerySpec:
    """The structured, database-neutral form of a SELECT built by the query builder.

    ``where`` and ``having`` are combined with AND. ``table`` is the bare table name
    and is empty when the data source is a subquery (``subquery`` is then true).
    ``joins``, ``group_by``, ``having`` and ``subquery`` are SQL-only features:
    document adapters raise ``UnsupportedQueryError`` for a spec that uses them
    instead of ignoring them.
    """

    table: str = ""
    fields: list[str] = field(default_factory=list)  # empty: all fields
    distinct: bool = False
    where: list[Condition] = field(default_factory=list)
    order_by: list[OrderBy] = field(default_factory=list)
    limit: int | None = None
    offset: int | None = None
    joins: list[Join] = field(default_factory=list)
    group_by: list[str] = field(default_factory=list)
    having: list[Condition] = field(default_factory=list)
    subquery: bool = False


@dataclass
class FieldValue:
    """A single column assignment of an INSERT or UPDATE."""

    field: str
    value: Any


@dataclass
class CommandSpec:
    """The structured form of an INSERT/UPDATE/DELETE built by the command builder.

    ``values`` are the ordered column assignments (INSERT/UPDATE); ``where`` is
    combined with AND (UPDATE/DELETE).
    """

    table: str
    values: list[FieldValue] = field(default_factory=list)
    where: list[Condition] = field(default_factory=list)


@dataclass
class Query:
    """A database query.

    SQL adapters execute ``sql`` with ``parameters`` (the builder emits ``?``
    placeholders). Document adapters execute ``spec`` and reject raw SQL. A
    ``timeout`` of ``None`` falls back to ``Config.query_timeout``.
    """

    sql: str = ""
    parameters: list[Any] = field(default_factory=list)
    query_type: QueryType = QueryType.UNKNOWN
    timeout: datetime.timedelta | None = None
    spec: QuerySpec | None = None


@dataclass
class Command:
    """A database command (INSERT, UPDATE, DELETE, DDL); see ``Query``."""

    sql: str = ""
    parameters: list[Any] = field(default_factory=list)
    command_type: CommandType = CommandType.UNKNOWN
    timeout: datetime.timedelta | None = None
    spec: CommandSpec | None = None


@dataclass
class Operation:
    """A batch operation: ``query`` for QUERY, ``command`` for COMMAND."""

    operation_type: OperationType
    query: Query | None = None
    command: Command | None = None


@dataclass
class ExecuteResult:
    """Result of executing a command.

    ``last_insert_id`` is ``None`` when the database has no integer insert IDs
    (PostgreSQL, MongoDB).
    """

    rows_affected: int
    last_insert_id: int | None = None


@dataclass
class OperationResult:
    """Result of one batch operation: a ``Result`` or an ``ExecuteResult``, or the
    error that operation raised."""

    index: int
    result: Result | ExecuteResult | None = None
    error: Exception | None = None


@dataclass
class ColumnType:
    """Information about a column type."""

    name: str
    database_type: str
    data_type: DataType
    nullable: bool = True
    length: int | None = None
    precision: int | None = None
    scale: int | None = None


@dataclass
class ColumnDefinition:
    """Definition of a database column."""

    name: str
    data_type: DataType
    nullable: bool = True
    primary_key: bool = False
    auto_increment: bool = False
    unique: bool = False
    default_value: Any = None
    length: int = 0
    precision: int = 0
    scale: int = 0
    comment: str = ""


@dataclass
class IndexDefinition:
    """Definition of a database index."""

    name: str
    columns: list[str]
    unique: bool = False
    index_type: IndexType = IndexType.BTREE
    condition: str | None = None


@dataclass
class SchemaChange:
    """A schema change operation, as passed to ``Storage.alter_table``."""

    change_type: SchemaChangeType
    column: ColumnDefinition | None = None
    index: IndexDefinition | None = None


@dataclass
class TableSchema:
    """Schema definition for a database table."""

    name: str
    columns: list[ColumnDefinition] = field(default_factory=list)
    indexes: list[IndexDefinition] = field(default_factory=list)
    primary_key: list[str] = field(default_factory=list)
    foreign_keys: dict[str, str] = field(default_factory=dict)


@dataclass
class TxOptions:
    """Transaction options. A ``timeout`` of ``None`` falls back to
    ``Config.tx_timeout``."""

    isolation: IsolationLevel = IsolationLevel.DEFAULT
    read_only: bool = False
    timeout: datetime.timedelta | None = None


@dataclass
class StorageLimits:
    """Storage system limits."""

    max_connections: int
    max_query_size: int
    max_transaction_age: datetime.timedelta
    max_batch_size: int


@dataclass
class StorageInfo:
    """Information about a storage system."""

    name: str
    version: str
    database_type: DatabaseType
    features: list[str] = field(default_factory=list)
    limits: StorageLimits | None = None


@dataclass
class HealthStatus:
    """Health status of a storage system; ``timestamp`` is timezone-aware (UTC)."""

    status: HealthStatusType
    message: str
    timestamp: datetime.datetime = field(
        default_factory=lambda: datetime.datetime.now(datetime.UTC)
    )
    details: dict[str, Any] = field(default_factory=dict)


@dataclass
class Config:
    """Configuration for database connections.

    ``port`` of ``None`` means the adapter's default port (5432 for PostgreSQL,
    27017 for MongoDB). A timeout or lifetime of ``None`` means no limit.
    ``password`` and ``dsn`` (which may embed the password) are left out of
    ``repr()``.
    """

    # Connection details
    host: str = "localhost"
    port: int | None = None
    database: str = ""
    username: str = ""
    password: str = field(default="", repr=False)
    dsn: str = field(default="", repr=False)

    # Connection pool settings
    max_open_conns: int = 25
    max_idle_conns: int = 5
    conn_max_lifetime: datetime.timedelta | None = datetime.timedelta(hours=1)
    conn_max_idle_time: datetime.timedelta | None = datetime.timedelta(minutes=30)

    # Timeouts: query_timeout is the default for Query/Command.timeout,
    # tx_timeout the default for TxOptions.timeout.
    connect_timeout: datetime.timedelta | None = datetime.timedelta(seconds=10)
    query_timeout: datetime.timedelta | None = datetime.timedelta(seconds=30)
    tx_timeout: datetime.timedelta | None = datetime.timedelta(minutes=5)

    # SSL/TLS settings
    ssl_mode: str = "prefer"
    ssl_cert: str = ""
    ssl_key: str = ""
    ssl_root_ca: str = ""

    # Additional, adapter-specific options
    options: dict[str, Any] = field(default_factory=dict)
