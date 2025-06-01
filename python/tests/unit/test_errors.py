"""
Unit tests for storage errors.

Tests the comprehensive error hierarchy with structured error types,
context information, and retry logic capabilities.
"""

import datetime

import pytest

from storage.errors import (
    AdapterError,
    CacheError,
    ConnectionError,
    DataError,
    MigrationError,
    PermissionError,
    QueryError,
    ResourceError,
    SchemaError,
    StorageError,
    TimeoutError,
    TransactionError,
    ValidationError,
    is_connection_error,
    is_data_error,
    is_query_error,
    is_retryable,
    is_schema_error,
    is_temporary,
    is_timeout_error,
    is_transaction_error,
    new_connection_error,
    new_connection_pool_full_error,
    new_connection_timeout_error,
    new_constraint_violation_error,
    new_data_error,
    new_deadlock_error,
    new_duplicate_key_error,
    new_invalid_query_error,
    new_no_rows_error,
    new_query_error,
    new_query_timeout_error,
    new_schema_error,
    new_table_not_found_error,
    new_transaction_error,
    new_transaction_timeout_error,
    wrap_error,
)
from storage.types import ErrorType


class TestStorageError:
    """Test base StorageError class."""
    
    def test_basic_creation(self):
        """Test basic StorageError creation."""
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="TEST_ERROR",
            message="Test error message"
        )
        
        assert error.error_type == ErrorType.CONNECTION
        assert error.code == "TEST_ERROR"
        assert error.message == "Test error message"
        assert error.cause is None
        assert error.context == {}
        assert error.retryable is False
        assert error.temporary is False
        assert isinstance(error.timestamp, datetime.datetime)
    
    def test_creation_with_all_params(self):
        """Test StorageError creation with all parameters."""
        cause = ValueError("Original error")
        context = {"table": "users", "operation": "SELECT"}
        
        error = StorageError(
            error_type=ErrorType.QUERY,
            code="QUERY_FAILED",
            message="Query execution failed",
            cause=cause,
            context=context,
            retryable=True,
            temporary=True
        )
        
        assert error.error_type == ErrorType.QUERY
        assert error.code == "QUERY_FAILED"
        assert error.message == "Query execution failed"
        assert error.cause == cause
        assert error.context == context
        assert error.retryable is True
        assert error.temporary is True
    
    def test_string_representation(self):
        """Test string representation of StorageError."""
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="CONN_FAILED",
            message="Connection failed"
        )
        
        assert str(error) == "CONN_FAILED: Connection failed"
    
    def test_string_representation_with_cause(self):
        """Test string representation with cause."""
        cause = ValueError("Original error")
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="CONN_FAILED",
            message="Connection failed",
            cause=cause
        )
        
        assert str(error) == "CONN_FAILED: Connection failed (caused by: Original error)"
    
    def test_repr_representation(self):
        """Test repr representation of StorageError."""
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="CONN_FAILED",
            message="Connection failed",
            retryable=True,
            temporary=False
        )
        
        repr_str = repr(error)
        assert "StorageError" in repr_str
        assert "type=connection" in repr_str
        assert "code='CONN_FAILED'" in repr_str
        assert "message='Connection failed'" in repr_str
        assert "retryable=True" in repr_str
        assert "temporary=False" in repr_str
    
    def test_with_context(self):
        """Test adding context to error."""
        error = StorageError(
            error_type=ErrorType.QUERY,
            code="QUERY_FAILED",
            message="Query failed"
        )
        
        result = error.with_context("table", "users")
        
        assert result is error  # Should return same instance
        assert error.context["table"] == "users"
        
        # Chain multiple context additions
        error.with_context("operation", "SELECT").with_context("timeout", 30)
        
        assert error.context["operation"] == "SELECT"
        assert error.context["timeout"] == 30
    
    def test_with_cause(self):
        """Test setting cause of error."""
        cause = ValueError("Original error")
        error = StorageError(
            error_type=ErrorType.QUERY,
            code="QUERY_FAILED",
            message="Query failed"
        )
        
        result = error.with_cause(cause)
        
        assert result is error  # Should return same instance
        assert error.cause == cause
    
    def test_with_retryable(self):
        """Test setting retryable flag."""
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="CONN_FAILED",
            message="Connection failed"
        )
        
        result = error.with_retryable(True)
        
        assert result is error  # Should return same instance
        assert error.retryable is True
    
    def test_with_temporary(self):
        """Test setting temporary flag."""
        error = StorageError(
            error_type=ErrorType.CONNECTION,
            code="CONN_FAILED",
            message="Connection failed"
        )
        
        result = error.with_temporary(True)
        
        assert result is error  # Should return same instance
        assert error.temporary is True


class TestSpecificErrors:
    """Test specific error types."""
    
    def test_connection_error(self):
        """Test ConnectionError."""
        error = ConnectionError("CONN_FAILED", "Connection failed")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.CONNECTION
        assert error.code == "CONN_FAILED"
        assert error.message == "Connection failed"
    
    def test_query_error(self):
        """Test QueryError."""
        error = QueryError("QUERY_FAILED", "Query failed")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.QUERY
        assert error.code == "QUERY_FAILED"
        assert error.message == "Query failed"
    
    def test_transaction_error(self):
        """Test TransactionError."""
        error = TransactionError("TX_FAILED", "Transaction failed")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.TRANSACTION
        assert error.code == "TX_FAILED"
        assert error.message == "Transaction failed"
    
    def test_data_error(self):
        """Test DataError."""
        error = DataError("DATA_INVALID", "Invalid data")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.DATA
        assert error.code == "DATA_INVALID"
        assert error.message == "Invalid data"
    
    def test_schema_error(self):
        """Test SchemaError."""
        error = SchemaError("SCHEMA_INVALID", "Invalid schema")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.SCHEMA
        assert error.code == "SCHEMA_INVALID"
        assert error.message == "Invalid schema"
    
    def test_timeout_error(self):
        """Test TimeoutError."""
        error = TimeoutError("TIMEOUT", "Operation timed out")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.TIMEOUT
        assert error.code == "TIMEOUT"
        assert error.message == "Operation timed out"
    
    def test_validation_error(self):
        """Test ValidationError."""
        error = ValidationError("VALIDATION_FAILED", "Validation failed")
        
        assert isinstance(error, StorageError)
        assert error.error_type == ErrorType.VALIDATION
        assert error.code == "VALIDATION_FAILED"
        assert error.message == "Validation failed"


class TestErrorConstructors:
    """Test convenience error constructor functions."""
    
    def test_new_connection_error(self):
        """Test new_connection_error constructor."""
        error = new_connection_error("CONN_FAILED", "Connection failed")
        
        assert isinstance(error, ConnectionError)
        assert error.code == "CONN_FAILED"
        assert error.message == "Connection failed"
    
    def test_new_connection_timeout_error(self):
        """Test new_connection_timeout_error constructor."""
        error = new_connection_timeout_error("Connection timed out")
        
        assert isinstance(error, ConnectionError)
        assert error.code == "CONNECTION_TIMEOUT"
        assert error.message == "Connection timed out"
        assert error.retryable is True
        assert error.temporary is True
    
    def test_new_connection_pool_full_error(self):
        """Test new_connection_pool_full_error constructor."""
        error = new_connection_pool_full_error()
        
        assert isinstance(error, ConnectionError)
        assert error.code == "CONNECTION_POOL_FULL"
        assert error.message == "Connection pool is full"
        assert error.retryable is True
        assert error.temporary is True
    
    def test_new_query_error(self):
        """Test new_query_error constructor."""
        error = new_query_error("QUERY_FAILED", "Query failed")
        
        assert isinstance(error, QueryError)
        assert error.code == "QUERY_FAILED"
        assert error.message == "Query failed"
    
    def test_new_query_timeout_error(self):
        """Test new_query_timeout_error constructor."""
        sql = "SELECT * FROM users WHERE name = 'test'"
        error = new_query_timeout_error(sql)
        
        assert isinstance(error, QueryError)
        assert error.code == "QUERY_TIMEOUT"
        assert "Query timed out" in error.message
        assert error.context["sql"] == sql
        assert error.retryable is True
        assert error.temporary is True
    
    def test_new_invalid_query_error(self):
        """Test new_invalid_query_error constructor."""
        error = new_invalid_query_error("Invalid SQL syntax")
        
        assert isinstance(error, QueryError)
        assert error.code == "INVALID_QUERY"
        assert error.message == "Invalid SQL syntax"
    
    def test_new_transaction_error(self):
        """Test new_transaction_error constructor."""
        error = new_transaction_error("TX_FAILED", "Transaction failed")
        
        assert isinstance(error, TransactionError)
        assert error.code == "TX_FAILED"
        assert error.message == "Transaction failed"
    
    def test_new_deadlock_error(self):
        """Test new_deadlock_error constructor."""
        error = new_deadlock_error()
        
        assert isinstance(error, TransactionError)
        assert error.code == "DEADLOCK"
        assert error.message == "Transaction deadlock detected"
        assert error.retryable is True
        assert error.temporary is True
    
    def test_new_transaction_timeout_error(self):
        """Test new_transaction_timeout_error constructor."""
        error = new_transaction_timeout_error()
        
        assert isinstance(error, TransactionError)
        assert error.code == "TRANSACTION_TIMEOUT"
        assert error.message == "Transaction timed out"
        assert error.temporary is True
    
    def test_new_data_error(self):
        """Test new_data_error constructor."""
        error = new_data_error("DATA_INVALID", "Invalid data")
        
        assert isinstance(error, DataError)
        assert error.code == "DATA_INVALID"
        assert error.message == "Invalid data"
    
    def test_new_no_rows_error(self):
        """Test new_no_rows_error constructor."""
        error = new_no_rows_error()
        
        assert isinstance(error, DataError)
        assert error.code == "NO_ROWS"
        assert error.message == "No rows found"
    
    def test_new_constraint_violation_error(self):
        """Test new_constraint_violation_error constructor."""
        error = new_constraint_violation_error("unique_email")
        
        assert isinstance(error, DataError)
        assert error.code == "CONSTRAINT_VIOLATION"
        assert "unique_email" in error.message
        assert error.context["constraint"] == "unique_email"
    
    def test_new_duplicate_key_error(self):
        """Test new_duplicate_key_error constructor."""
        error = new_duplicate_key_error("email")
        
        assert isinstance(error, DataError)
        assert error.code == "DUPLICATE_KEY"
        assert "email" in error.message
        assert error.context["key"] == "email"
    
    def test_new_schema_error(self):
        """Test new_schema_error constructor."""
        error = new_schema_error("SCHEMA_INVALID", "Invalid schema")
        
        assert isinstance(error, SchemaError)
        assert error.code == "SCHEMA_INVALID"
        assert error.message == "Invalid schema"
    
    def test_new_table_not_found_error(self):
        """Test new_table_not_found_error constructor."""
        error = new_table_not_found_error("users")
        
        assert isinstance(error, SchemaError)
        assert error.code == "TABLE_NOT_FOUND"
        assert "users" in error.message
        assert error.context["table"] == "users"


class TestErrorHelpers:
    """Test error helper functions."""
    
    def test_wrap_error(self):
        """Test wrap_error function."""
        original = ValueError("Original error")
        wrapped = wrap_error(
            original,
            ErrorType.QUERY,
            "QUERY_FAILED",
            "Query execution failed"
        )
        
        assert isinstance(wrapped, StorageError)
        assert wrapped.error_type == ErrorType.QUERY
        assert wrapped.code == "QUERY_FAILED"
        assert wrapped.message == "Query execution failed"
        assert wrapped.cause == original
    
    def test_is_retryable(self):
        """Test is_retryable function."""
        retryable_error = StorageError(
            ErrorType.CONNECTION,
            "CONN_TIMEOUT",
            "Connection timeout",
            retryable=True
        )
        non_retryable_error = StorageError(
            ErrorType.VALIDATION,
            "INVALID_DATA",
            "Invalid data"
        )
        regular_error = ValueError("Regular error")
        
        assert is_retryable(retryable_error) is True
        assert is_retryable(non_retryable_error) is False
        assert is_retryable(regular_error) is False
    
    def test_is_temporary(self):
        """Test is_temporary function."""
        temporary_error = StorageError(
            ErrorType.CONNECTION,
            "CONN_TIMEOUT",
            "Connection timeout",
            temporary=True
        )
        permanent_error = StorageError(
            ErrorType.VALIDATION,
            "INVALID_DATA",
            "Invalid data"
        )
        regular_error = ValueError("Regular error")
        
        assert is_temporary(temporary_error) is True
        assert is_temporary(permanent_error) is False
        assert is_temporary(regular_error) is False
    
    def test_is_connection_error(self):
        """Test is_connection_error function."""
        conn_error = ConnectionError("CONN_FAILED", "Connection failed")
        query_error = QueryError("QUERY_FAILED", "Query failed")
        regular_error = ValueError("Regular error")
        
        assert is_connection_error(conn_error) is True
        assert is_connection_error(query_error) is False
        assert is_connection_error(regular_error) is False
    
    def test_is_query_error(self):
        """Test is_query_error function."""
        query_error = QueryError("QUERY_FAILED", "Query failed")
        conn_error = ConnectionError("CONN_FAILED", "Connection failed")
        regular_error = ValueError("Regular error")
        
        assert is_query_error(query_error) is True
        assert is_query_error(conn_error) is False
        assert is_query_error(regular_error) is False
    
    def test_is_transaction_error(self):
        """Test is_transaction_error function."""
        tx_error = TransactionError("TX_FAILED", "Transaction failed")
        query_error = QueryError("QUERY_FAILED", "Query failed")
        regular_error = ValueError("Regular error")
        
        assert is_transaction_error(tx_error) is True
        assert is_transaction_error(query_error) is False
        assert is_transaction_error(regular_error) is False
    
    def test_is_data_error(self):
        """Test is_data_error function."""
        data_error = DataError("DATA_INVALID", "Invalid data")
        query_error = QueryError("QUERY_FAILED", "Query failed")
        regular_error = ValueError("Regular error")
        
        assert is_data_error(data_error) is True
        assert is_data_error(query_error) is False
        assert is_data_error(regular_error) is False
    
    def test_is_schema_error(self):
        """Test is_schema_error function."""
        schema_error = SchemaError("SCHEMA_INVALID", "Invalid schema")
        query_error = QueryError("QUERY_FAILED", "Query failed")
        regular_error = ValueError("Regular error")
        
        assert is_schema_error(schema_error) is True
        assert is_schema_error(query_error) is False
        assert is_schema_error(regular_error) is False
    
    def test_is_timeout_error(self):
        """Test is_timeout_error function."""
        timeout_error = TimeoutError("TIMEOUT", "Operation timed out")
        query_error = QueryError("QUERY_FAILED", "Query failed")
        regular_error = ValueError("Regular error")
        
        assert is_timeout_error(timeout_error) is True
        assert is_timeout_error(query_error) is False
        assert is_timeout_error(regular_error) is False
