"""
Database Agnostic Storage Library for Python

A comprehensive, high-performance database abstraction library that provides
a unified interface across multiple database types while maintaining the
unique strengths of each database system.

This library provides:
- Universal async interface for SQL and NoSQL databases
- High-performance connection pooling and query execution
- Type-safe operations with automatic data mapping
- Extensible architecture with plugin-based adapters
- Production-ready monitoring, metrics, and error handling
- Clean architecture following SOLID principles
"""

from storage.errors import (
    StorageError,
    ConnectionError,
    QueryError,
    TransactionError,
    DataError,
    SchemaError,
    TimeoutError,
    ValidationError,
)
from storage.interfaces import (
    Storage,
    Adapter,
    Result,
    Row,
    Transaction,
    HealthChecker,
)
from storage.types import (
    DatabaseType,
    DataType,
    Query,
    Command,
    Config,
    StorageInfo,
    HealthStatus,
    TableSchema,
    ColumnDefinition,
    IndexDefinition,
    TxOptions,
    IsolationLevel,
    SortDirection,
    QueryType,
    CommandType,
    OperationType,
    Operation,
    OperationResult,
    ExecuteResult,
    ColumnType,
    Condition,
    Operator,
    JoinType,
    ConflictResolution,
    SchemaChange,
    SchemaChangeType,
    IndexType,
    HealthStatusType,
    ErrorType,
)

__version__ = "1.0.0"
__author__ = "HybridCache.io Team"
__email__ = "support@hybridcache.io"
__license__ = "MIT"

__all__ = [
    # Core interfaces
    "Storage",
    "Adapter", 
    "Result",
    "Row",
    "Transaction",
    "HealthChecker",
    # Types
    "DatabaseType",
    "DataType",
    "Query",
    "Command",
    "Config",
    "StorageInfo",
    "HealthStatus",
    "TableSchema",
    "ColumnDefinition",
    "IndexDefinition",
    "TxOptions",
    "IsolationLevel",
    "SortDirection",
    "QueryType",
    "CommandType",
    "OperationType",
    "Operation",
    "OperationResult",
    "ExecuteResult",
    "ColumnType",
    "Condition",
    "Operator",
    "JoinType",
    "ConflictResolution",
    "SchemaChange",
    "SchemaChangeType",
    "IndexType",
    "HealthStatusType",
    "ErrorType",
    # Errors
    "StorageError",
    "ConnectionError",
    "QueryError",
    "TransactionError",
    "DataError",
    "SchemaError",
    "TimeoutError",
    "ValidationError",
]
