"""
Type definitions for the Database Agnostic Storage Library.

This module contains all the data types, enums, and dataclasses used throughout
the library, providing strong typing and clear interfaces.
"""

from __future__ import annotations

import datetime
from dataclasses import dataclass, field
from enum import Enum, auto
from typing import Any, Dict, List, Optional, Union
from uuid import UUID


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
    """Transaction isolation levels."""
    
    READ_UNCOMMITTED = "read_uncommitted"
    READ_COMMITTED = "read_committed"
    REPEATABLE_READ = "repeatable_read"
    SERIALIZABLE = "serializable"


class SortDirection(Enum):
    """Sort directions for ORDER BY clauses."""
    
    ASC = "asc"
    DESC = "desc"


class Operator(Enum):
    """Comparison operators for WHERE conditions."""
    
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


class JoinType(Enum):
    """Types of SQL joins."""
    
    INNER = "inner"
    LEFT = "left"
    RIGHT = "right"
    FULL = "full"
    CROSS = "cross"


class ConflictResolution(Enum):
    """Conflict resolution strategies for INSERT/UPDATE."""
    
    IGNORE = "ignore"
    REPLACE = "replace"
    UPDATE = "update"
    FAIL = "fail"


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
    """Types of storage errors."""
    
    CONNECTION = "connection"
    QUERY = "query"
    TRANSACTION = "transaction"
    DATA = "data"
    SCHEMA = "schema"
    ADAPTER = "adapter"
    MIGRATION = "migration"
    CACHE = "cache"
    VALIDATION = "validation"
    TIMEOUT = "timeout"
    PERMISSION = "permission"
    RESOURCE = "resource"
    UNKNOWN = "unknown"


@dataclass
class Condition:
    """Represents a WHERE condition in a query."""
    
    field: str
    operator: Operator
    value: Any = None
    values: Optional[List[Any]] = None


@dataclass
class Query:
    """Represents a database query."""
    
    sql: str
    parameters: List[Any] = field(default_factory=list)
    query_type: QueryType = QueryType.UNKNOWN
    timeout: Optional[datetime.timedelta] = None
    cache_key: Optional[str] = None
    cache_ttl: Optional[datetime.timedelta] = None
    read_only: bool = False


@dataclass
class Command:
    """Represents a database command (INSERT, UPDATE, DELETE)."""
    
    sql: str
    parameters: List[Any] = field(default_factory=list)
    command_type: CommandType = CommandType.UNKNOWN
    timeout: Optional[datetime.timedelta] = None
    return_id: bool = False
    return_count: bool = True
    on_conflict: ConflictResolution = ConflictResolution.FAIL


@dataclass
class Operation:
    """Represents a batch operation."""
    
    operation_type: OperationType
    query: Optional[Query] = None
    command: Optional[Command] = None


@dataclass
class ExecuteResult:
    """Result of executing a command."""
    
    rows_affected: int
    last_insert_id: Optional[int] = None


@dataclass
class OperationResult:
    """Result of a batch operation."""
    
    index: int
    result: Optional[Union[ExecuteResult, Any]] = None
    error: Optional[Exception] = None


@dataclass
class ColumnType:
    """Information about a column type."""
    
    name: str
    database_type: str
    data_type: DataType
    nullable: bool = True
    length: Optional[int] = None
    precision: Optional[int] = None
    scale: Optional[int] = None


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
    columns: List[str]
    unique: bool = False
    index_type: IndexType = IndexType.BTREE
    condition: Optional[str] = None


@dataclass
class SchemaChange:
    """Represents a schema change operation."""
    
    change_type: SchemaChangeType
    column: Optional[ColumnDefinition] = None
    index: Optional[IndexDefinition] = None


@dataclass
class TableSchema:
    """Schema definition for a database table."""
    
    name: str
    columns: List[ColumnDefinition] = field(default_factory=list)
    indexes: List[IndexDefinition] = field(default_factory=list)
    primary_key: List[str] = field(default_factory=list)
    foreign_keys: Dict[str, str] = field(default_factory=dict)


@dataclass
class TxOptions:
    """Transaction options."""
    
    isolation: IsolationLevel = IsolationLevel.READ_COMMITTED
    read_only: bool = False
    timeout: Optional[datetime.timedelta] = None


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
    features: List[str] = field(default_factory=list)
    limits: Optional[StorageLimits] = None


@dataclass
class HealthStatus:
    """Health status of a storage system."""
    
    status: HealthStatusType
    message: str
    timestamp: datetime.datetime = field(default_factory=datetime.datetime.now)
    details: Dict[str, Any] = field(default_factory=dict)


@dataclass
class Config:
    """Configuration for database connections."""
    
    # Connection details
    host: str = "localhost"
    port: int = 5432
    database: str = ""
    username: str = ""
    password: str = ""
    dsn: str = ""
    
    # Connection pool settings
    max_open_conns: int = 25
    max_idle_conns: int = 5
    conn_max_lifetime: datetime.timedelta = datetime.timedelta(hours=1)
    conn_max_idle_time: datetime.timedelta = datetime.timedelta(minutes=30)
    
    # Timeout settings
    connect_timeout: datetime.timedelta = datetime.timedelta(seconds=10)
    query_timeout: datetime.timedelta = datetime.timedelta(seconds=30)
    tx_timeout: datetime.timedelta = datetime.timedelta(minutes=5)
    
    # SSL settings
    ssl_mode: str = "prefer"
    ssl_cert: str = ""
    ssl_key: str = ""
    ssl_root_ca: str = ""
    
    # Additional options
    options: Dict[str, Any] = field(default_factory=dict)


# Type aliases for commonly used types
ParameterValue = Union[str, int, float, bool, datetime.datetime, datetime.date, 
                      datetime.time, bytes, UUID, None]
Parameters = List[ParameterValue]
Row = Dict[str, Any]
Rows = List[Row]
