"""
Error handling for the Database Agnostic Storage Library.

This module provides a comprehensive error hierarchy with structured error types,
context information, and retry logic capabilities.
"""

from __future__ import annotations

import datetime
from typing import Any, Dict, Optional

from storage.types import ErrorType


class StorageError(Exception):
    """Base exception for all storage-related errors."""
    
    def __init__(
        self,
        error_type: ErrorType,
        code: str,
        message: str,
        cause: Optional[Exception] = None,
        context: Optional[Dict[str, Any]] = None,
        retryable: bool = False,
        temporary: bool = False,
    ) -> None:
        """Initialize a storage error.
        
        Args:
            error_type: The type of error
            code: Error code for programmatic handling
            message: Human-readable error message
            cause: The underlying exception that caused this error
            context: Additional context information
            retryable: Whether this error can be retried
            temporary: Whether this is a temporary error
        """
        super().__init__(message)
        self.error_type = error_type
        self.code = code
        self.message = message
        self.cause = cause
        self.context = context or {}
        self.retryable = retryable
        self.temporary = temporary
        self.timestamp = datetime.datetime.now()
    
    def __str__(self) -> str:
        """Return string representation of the error."""
        if self.cause:
            return f"{self.code}: {self.message} (caused by: {self.cause})"
        return f"{self.code}: {self.message}"
    
    def __repr__(self) -> str:
        """Return detailed representation of the error."""
        return (
            f"StorageError(type={self.error_type.value}, code={self.code!r}, "
            f"message={self.message!r}, retryable={self.retryable}, "
            f"temporary={self.temporary})"
        )
    
    def with_context(self, key: str, value: Any) -> StorageError:
        """Add context information to the error.
        
        Args:
            key: Context key
            value: Context value
            
        Returns:
            Self for method chaining
        """
        self.context[key] = value
        return self
    
    def with_cause(self, cause: Exception) -> StorageError:
        """Set the underlying cause of this error.
        
        Args:
            cause: The underlying exception
            
        Returns:
            Self for method chaining
        """
        self.cause = cause
        return self
    
    def with_retryable(self, retryable: bool) -> StorageError:
        """Set whether this error is retryable.
        
        Args:
            retryable: Whether the error can be retried
            
        Returns:
            Self for method chaining
        """
        self.retryable = retryable
        return self
    
    def with_temporary(self, temporary: bool) -> StorageError:
        """Set whether this error is temporary.
        
        Args:
            temporary: Whether the error is temporary
            
        Returns:
            Self for method chaining
        """
        self.temporary = temporary
        return self


class ConnectionError(StorageError):
    """Error related to database connections."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.CONNECTION, code, message, **kwargs)


class QueryError(StorageError):
    """Error related to query execution."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.QUERY, code, message, **kwargs)


class TransactionError(StorageError):
    """Error related to transaction management."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.TRANSACTION, code, message, **kwargs)


class DataError(StorageError):
    """Error related to data operations."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.DATA, code, message, **kwargs)


class SchemaError(StorageError):
    """Error related to schema operations."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.SCHEMA, code, message, **kwargs)


class TimeoutError(StorageError):
    """Error related to operation timeouts."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.TIMEOUT, code, message, **kwargs)


class ValidationError(StorageError):
    """Error related to data validation."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.VALIDATION, code, message, **kwargs)


class AdapterError(StorageError):
    """Error related to database adapters."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.ADAPTER, code, message, **kwargs)


class MigrationError(StorageError):
    """Error related to database migrations."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.MIGRATION, code, message, **kwargs)


class CacheError(StorageError):
    """Error related to caching operations."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.CACHE, code, message, **kwargs)


class PermissionError(StorageError):
    """Error related to permissions."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.PERMISSION, code, message, **kwargs)


class ResourceError(StorageError):
    """Error related to resource limitations."""
    
    def __init__(self, code: str, message: str, **kwargs: Any) -> None:
        super().__init__(ErrorType.RESOURCE, code, message, **kwargs)


# Convenience functions for creating common errors

def new_connection_error(code: str, message: str) -> ConnectionError:
    """Create a new connection error."""
    return ConnectionError(code, message)


def new_connection_timeout_error(message: str) -> ConnectionError:
    """Create a connection timeout error."""
    return ConnectionError("CONNECTION_TIMEOUT", message).with_retryable(True).with_temporary(True)


def new_connection_pool_full_error() -> ConnectionError:
    """Create a connection pool full error."""
    return ConnectionError(
        "CONNECTION_POOL_FULL", 
        "Connection pool is full"
    ).with_retryable(True).with_temporary(True)


def new_query_error(code: str, message: str) -> QueryError:
    """Create a new query error."""
    return QueryError(code, message)


def new_query_timeout_error(sql: str) -> QueryError:
    """Create a query timeout error."""
    return QueryError(
        "QUERY_TIMEOUT", 
        f"Query timed out: {sql[:100]}..."
    ).with_context("sql", sql).with_retryable(True).with_temporary(True)


def new_invalid_query_error(message: str) -> QueryError:
    """Create an invalid query error."""
    return QueryError("INVALID_QUERY", message)


def new_transaction_error(code: str, message: str) -> TransactionError:
    """Create a new transaction error."""
    return TransactionError(code, message)


def new_deadlock_error() -> TransactionError:
    """Create a deadlock error."""
    return TransactionError(
        "DEADLOCK", 
        "Transaction deadlock detected"
    ).with_retryable(True).with_temporary(True)


def new_transaction_timeout_error() -> TransactionError:
    """Create a transaction timeout error."""
    return TransactionError(
        "TRANSACTION_TIMEOUT", 
        "Transaction timed out"
    ).with_temporary(True)


def new_data_error(code: str, message: str) -> DataError:
    """Create a new data error."""
    return DataError(code, message)


def new_no_rows_error() -> DataError:
    """Create a no rows found error."""
    return DataError("NO_ROWS", "No rows found")


def new_constraint_violation_error(constraint: str) -> DataError:
    """Create a constraint violation error."""
    return DataError(
        "CONSTRAINT_VIOLATION", 
        f"Constraint violation: {constraint}"
    ).with_context("constraint", constraint)


def new_duplicate_key_error(key: str) -> DataError:
    """Create a duplicate key error."""
    return DataError(
        "DUPLICATE_KEY", 
        f"Duplicate key: {key}"
    ).with_context("key", key)


def new_schema_error(code: str, message: str) -> SchemaError:
    """Create a new schema error."""
    return SchemaError(code, message)


def new_table_not_found_error(table: str) -> SchemaError:
    """Create a table not found error."""
    return SchemaError(
        "TABLE_NOT_FOUND", 
        f"Table not found: {table}"
    ).with_context("table", table)


def new_column_not_found_error(column: str) -> SchemaError:
    """Create a column not found error."""
    return SchemaError(
        "COLUMN_NOT_FOUND", 
        f"Column not found: {column}"
    ).with_context("column", column)


def wrap_error(
    error: Exception, 
    error_type: ErrorType, 
    code: str, 
    message: str
) -> StorageError:
    """Wrap an existing exception in a StorageError.
    
    Args:
        error: The original exception
        error_type: The storage error type
        code: Error code
        message: Error message
        
    Returns:
        A new StorageError wrapping the original exception
    """
    return StorageError(error_type, code, message, cause=error)


# Error checking functions

def is_retryable(error: Exception) -> bool:
    """Check if an error is retryable."""
    if isinstance(error, StorageError):
        return error.retryable
    return False


def is_temporary(error: Exception) -> bool:
    """Check if an error is temporary."""
    if isinstance(error, StorageError):
        return error.temporary
    return False


def is_connection_error(error: Exception) -> bool:
    """Check if an error is a connection error."""
    return isinstance(error, ConnectionError)


def is_query_error(error: Exception) -> bool:
    """Check if an error is a query error."""
    return isinstance(error, QueryError)


def is_transaction_error(error: Exception) -> bool:
    """Check if an error is a transaction error."""
    return isinstance(error, TransactionError)


def is_data_error(error: Exception) -> bool:
    """Check if an error is a data error."""
    return isinstance(error, DataError)


def is_schema_error(error: Exception) -> bool:
    """Check if an error is a schema error."""
    return isinstance(error, SchemaError)


def is_timeout_error(error: Exception) -> bool:
    """Check if an error is a timeout error."""
    return isinstance(error, TimeoutError)
