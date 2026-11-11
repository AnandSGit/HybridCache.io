"""
Error handling for the Database Agnostic Storage Library.

Mirrors ``internal/domain/errors.go``. Go matches errors by code through sentinels
(``errors.Is(err, ErrNoRows)``); Python uses an exception hierarchy instead. Every
Go sentinel has a subclass carrying the same ``code`` string, so callers write
``except NoRowsError``. Category classes (``QueryError``, ``DataError``, ...) group
them, and a few errors belong to two categories: ``QueryTimeoutError`` is both a
``QueryError`` and a ``StorageTimeoutError``.

The ``new_*_error`` helpers mirror the Go constructors. Helpers taking a ``code``
return the dedicated subclass when the code has one within the requested category,
so ``new_query_error("UNSUPPORTED_QUERY", ...)`` is an ``UnsupportedQueryError``.
"""

from __future__ import annotations

import datetime
from collections.abc import Iterator, Mapping
from typing import Any, ClassVar, Final, Self, TypeVar

from storage.types import ErrorType

_E = TypeVar("_E", bound="StorageError")


class StorageError(Exception):
    """Base class of all storage errors.

    Attributes:
        code: Stable, machine-readable code (the Go sentinel's code for the
            dedicated subclasses).
        error_type: The error category.
        message: Human-readable message.
        context: Extra details, such as the table involved.
        retryable: Whether retrying the operation may succeed.
        temporary: Whether the condition is expected to clear by itself.
        timestamp: When the error was created (timezone-aware, UTC).
    """

    code: str = "STORAGE_ERROR"
    error_type: ErrorType = ErrorType.UNKNOWN
    retryable: bool = False
    temporary: bool = False
    default_message: ClassVar[str] = "storage error"

    def __init__(
        self,
        message: str | None = None,
        *,
        code: str | None = None,
        cause: BaseException | None = None,
        context: Mapping[str, Any] | None = None,
        retryable: bool | None = None,
        temporary: bool | None = None,
    ) -> None:
        """Create an error; unset arguments keep the class defaults.

        Args:
            message: Human-readable message (default: the class's message).
            code: Code override, only for codes without a dedicated subclass.
            cause: The underlying exception; stored as ``__cause__``.
            context: Extra details.
            retryable: Override of the class's retryability.
            temporary: Override of the class's temporariness.
        """
        self.message = self.default_message if message is None else message
        super().__init__(self.message)
        if code is not None:
            self.code = code
        if retryable is not None:
            self.retryable = retryable
        if temporary is not None:
            self.temporary = temporary
        self.context: dict[str, Any] = dict(context or {})
        self.timestamp = datetime.datetime.now(datetime.UTC)
        if cause is not None:
            self.__cause__ = cause

    @property
    def cause(self) -> BaseException | None:
        """The underlying exception (``__cause__``), if any."""
        return self.__cause__

    def __str__(self) -> str:
        if self.cause is not None:
            return f"{self.code}: {self.message} (caused by: {self.cause})"
        return f"{self.code}: {self.message}"

    def __repr__(self) -> str:
        return (
            f"{type(self).__name__}(code={self.code!r}, message={self.message!r}, "
            f"type={self.error_type.value}, retryable={self.retryable}, "
            f"temporary={self.temporary})"
        )

    def with_context(self, key: str, value: Any) -> Self:
        """Add a context entry and return this error."""
        self.context[key] = value
        return self

    def with_cause(self, cause: BaseException) -> Self:
        """Set the underlying exception and return this error."""
        self.__cause__ = cause
        return self

    def with_retryable(self, retryable: bool) -> Self:
        """Set whether the error is retryable and return this error."""
        self.retryable = retryable
        return self

    def with_temporary(self, temporary: bool) -> Self:
        """Set whether the error is temporary and return this error."""
        self.temporary = temporary
        return self


# Categories


class StorageConnectionError(StorageError):
    """Error related to database connections."""

    code = "CONNECTION_ERROR"
    error_type = ErrorType.CONNECTION
    default_message = "connection error"


class QueryError(StorageError):
    """Error related to query or command execution."""

    code = "QUERY_ERROR"
    error_type = ErrorType.QUERY
    default_message = "query error"


class TransactionError(StorageError):
    """Error related to transaction management."""

    code = "TRANSACTION_ERROR"
    error_type = ErrorType.TRANSACTION
    default_message = "transaction error"


class DataError(StorageError):
    """Error related to data."""

    code = "DATA_ERROR"
    error_type = ErrorType.DATA
    default_message = "data error"


class SchemaError(StorageError):
    """Error related to schema operations."""

    code = "SCHEMA_ERROR"
    error_type = ErrorType.SCHEMA
    default_message = "schema error"


class AdapterError(StorageError):
    """Error related to database adapters."""

    code = "ADAPTER_ERROR"
    error_type = ErrorType.ADAPTER
    default_message = "adapter error"


class ValidationError(StorageError):
    """Invalid input: configuration, DSN, query or parameters."""

    code = "VALIDATION_ERROR"
    error_type = ErrorType.VALIDATION
    default_message = "validation error"


class StorageTimeoutError(StorageError):
    """An operation timed out."""

    code = "TIMEOUT_ERROR"
    error_type = ErrorType.TIMEOUT
    default_message = "timeout"


class StoragePermissionError(StorageError):
    """The database refused the operation for lack of privileges."""

    code = "PERMISSION_ERROR"
    error_type = ErrorType.PERMISSION
    default_message = "permission denied"


class ResourceError(StorageError):
    """A resource limit was reached."""

    code = "RESOURCE_ERROR"
    error_type = ErrorType.RESOURCE
    default_message = "resource exhausted"


# Connection errors


class ConnectionFailedError(StorageConnectionError):
    code = "CONNECTION_FAILED"
    default_message = "connection failed"


class ConnectionTimeoutError(StorageConnectionError, StorageTimeoutError):
    code = "CONNECTION_TIMEOUT"
    error_type = ErrorType.TIMEOUT
    retryable = True
    temporary = True
    default_message = "connection timeout"


class ConnectionClosedError(StorageConnectionError):
    code = "CONNECTION_CLOSED"
    default_message = "connection closed"


class ConnectionPoolFullError(StorageConnectionError, ResourceError):
    code = "CONNECTION_POOL_FULL"
    error_type = ErrorType.RESOURCE
    retryable = True
    temporary = True
    default_message = "connection pool is full"


class InvalidDSNError(ValidationError):
    code = "INVALID_DSN"
    default_message = "invalid DSN"


class InvalidConfigError(ValidationError):
    code = "INVALID_CONFIG"
    default_message = "invalid configuration"


# Query errors


class InvalidQueryError(QueryError, ValidationError):
    code = "INVALID_QUERY"
    error_type = ErrorType.VALIDATION
    default_message = "invalid query"


class QueryTimeoutError(QueryError, StorageTimeoutError):
    code = "QUERY_TIMEOUT"
    error_type = ErrorType.TIMEOUT
    retryable = True
    temporary = True
    default_message = "query execution timeout"


class QueryFailedError(QueryError):
    code = "QUERY_FAILED"
    default_message = "query failed"


class InvalidParametersError(QueryError, ValidationError):
    code = "INVALID_PARAMETERS"
    error_type = ErrorType.VALIDATION
    default_message = "invalid parameters"


class UnsupportedQueryError(QueryError):
    """The adapter cannot execute this query, e.g. raw SQL or a SQL-only feature
    (joins, GROUP BY, HAVING, subqueries) on a document database."""

    code = "UNSUPPORTED_QUERY"
    default_message = "unsupported query"


# Transaction errors


class TransactionFailedError(TransactionError):
    code = "TRANSACTION_FAILED"
    default_message = "transaction failed"


class TransactionTimeoutError(TransactionError, StorageTimeoutError):
    code = "TRANSACTION_TIMEOUT"
    error_type = ErrorType.TIMEOUT
    temporary = True
    default_message = "transaction timeout"


class TransactionClosedError(TransactionError):
    code = "TRANSACTION_CLOSED"
    default_message = "transaction is not active"


class InvalidTransactionError(TransactionError):
    code = "INVALID_TRANSACTION"
    default_message = "invalid transaction"


class DeadlockError(TransactionError):
    code = "DEADLOCK"
    retryable = True
    temporary = True
    default_message = "deadlock detected"


# Data errors


class NoRowsError(DataError):
    code = "NO_ROWS"
    default_message = "no rows found"


class TooManyRowsError(DataError):
    code = "TOO_MANY_ROWS"
    default_message = "too many rows"


class InvalidDataError(DataError):
    code = "INVALID_DATA"
    default_message = "invalid data"


class DataTruncatedError(DataError):
    code = "DATA_TRUNCATED"
    default_message = "data truncated"


class ConstraintViolationError(DataError):
    code = "CONSTRAINT_VIOLATION"
    default_message = "constraint violation"


class DuplicateKeyError(DataError):
    code = "DUPLICATE_KEY"
    default_message = "duplicate key violation"


# Schema errors


class TableNotFoundError(SchemaError):
    code = "TABLE_NOT_FOUND"
    default_message = "table not found"


class ColumnNotFoundError(SchemaError):
    code = "COLUMN_NOT_FOUND"
    default_message = "column not found"


class IndexNotFoundError(SchemaError):
    code = "INDEX_NOT_FOUND"
    default_message = "index not found"


class InvalidSchemaError(SchemaError):
    code = "INVALID_SCHEMA"
    default_message = "invalid schema"


class SchemaExistsError(SchemaError):
    code = "SCHEMA_EXISTS"
    default_message = "schema already exists"


# Adapter errors


class AdapterNotFoundError(AdapterError):
    code = "ADAPTER_NOT_FOUND"
    default_message = "adapter not found"


class UnsupportedAdapterError(AdapterError):
    code = "UNSUPPORTED_ADAPTER"
    default_message = "unsupported adapter"


class AdapterFailedError(AdapterError):
    code = "ADAPTER_FAILED"
    default_message = "adapter failed"


class UnsupportedOperationError(AdapterError):
    """The adapter does not support this operation, e.g. savepoints on MongoDB."""

    code = "UNSUPPORTED_OPERATION"
    default_message = "operation not supported by this adapter"


# Generic errors


class StorageNotImplementedError(StorageError):
    code = "NOT_IMPLEMENTED"
    default_message = "not implemented"


class InternalError(StorageError):
    code = "INTERNAL_ERROR"
    default_message = "internal error"


# The dedicated subclass of every Go sentinel code (Go's codeSentinels).
_BY_CODE: Final[Mapping[str, type[StorageError]]] = {
    cls.code: cls
    for cls in (
        ConnectionFailedError,
        ConnectionTimeoutError,
        ConnectionClosedError,
        ConnectionPoolFullError,
        InvalidDSNError,
        InvalidConfigError,
        InvalidQueryError,
        QueryTimeoutError,
        QueryFailedError,
        InvalidParametersError,
        UnsupportedQueryError,
        TransactionFailedError,
        TransactionTimeoutError,
        TransactionClosedError,
        InvalidTransactionError,
        DeadlockError,
        NoRowsError,
        TooManyRowsError,
        InvalidDataError,
        DataTruncatedError,
        ConstraintViolationError,
        DuplicateKeyError,
        TableNotFoundError,
        ColumnNotFoundError,
        IndexNotFoundError,
        InvalidSchemaError,
        SchemaExistsError,
        AdapterNotFoundError,
        UnsupportedAdapterError,
        AdapterFailedError,
        StorageNotImplementedError,
        UnsupportedOperationError,
        InternalError,
    )
}

_BY_TYPE: Final[Mapping[ErrorType, type[StorageError]]] = {
    ErrorType.CONNECTION: StorageConnectionError,
    ErrorType.QUERY: QueryError,
    ErrorType.TRANSACTION: TransactionError,
    ErrorType.DATA: DataError,
    ErrorType.SCHEMA: SchemaError,
    ErrorType.ADAPTER: AdapterError,
    ErrorType.VALIDATION: ValidationError,
    ErrorType.TIMEOUT: StorageTimeoutError,
    ErrorType.PERMISSION: StoragePermissionError,
    ErrorType.RESOURCE: ResourceError,
    ErrorType.UNKNOWN: StorageError,
}


def _new(category: type[_E], code: str, message: str) -> _E:
    """Create an error of ``category`` with ``code``, using the code's dedicated
    subclass when it belongs to ``category``."""
    cls: type[_E] = category
    dedicated = _BY_CODE.get(code)
    if dedicated is not None and issubclass(dedicated, category):
        cls = dedicated
    return cls(message, code=code)


# Constructors mirroring the Go ones


def new_storage_error(error_type: ErrorType, code: str, message: str) -> StorageError:
    """Create an error of the category ``error_type``."""
    return _new(_BY_TYPE[error_type], code, message)


def new_connection_error(code: str, message: str) -> StorageConnectionError:
    """Create a connection error."""
    return _new(StorageConnectionError, code, message)


def new_connection_timeout_error(message: str) -> ConnectionTimeoutError:
    """Create a connection timeout error (retryable, temporary)."""
    return ConnectionTimeoutError(message)


def new_connection_pool_full_error() -> ConnectionPoolFullError:
    """Create a connection pool full error (retryable, temporary)."""
    return ConnectionPoolFullError()


def new_query_error(code: str, message: str) -> QueryError:
    """Create a query error."""
    return _new(QueryError, code, message)


def new_query_timeout_error(query: str) -> QueryTimeoutError:
    """Create a query timeout error (retryable, temporary).

    The query text goes into ``context["query"]``, not into the message.
    """
    return QueryTimeoutError(context={"query": query})


def new_invalid_query_error(message: str) -> InvalidQueryError:
    """Create an invalid query error."""
    return InvalidQueryError(message)


def new_unsupported_query_error(message: str) -> UnsupportedQueryError:
    """Create an error for a query the adapter cannot execute."""
    return UnsupportedQueryError(message)


def new_transaction_error(code: str, message: str) -> TransactionError:
    """Create a transaction error."""
    return _new(TransactionError, code, message)


def new_deadlock_error() -> DeadlockError:
    """Create a deadlock error (retryable, temporary)."""
    return DeadlockError()


def new_transaction_closed_error() -> TransactionClosedError:
    """Create the error for using a transaction that is no longer active."""
    return TransactionClosedError()


def new_transaction_timeout_error() -> TransactionTimeoutError:
    """Create a transaction timeout error (temporary, not retryable)."""
    return TransactionTimeoutError()


def new_data_error(code: str, message: str) -> DataError:
    """Create a data error."""
    return _new(DataError, code, message)


def new_no_rows_error() -> NoRowsError:
    """Create a no rows found error."""
    return NoRowsError()


def new_constraint_violation_error(constraint: str) -> ConstraintViolationError:
    """Create a constraint violation error for the named constraint."""
    return ConstraintViolationError(
        f"constraint violation: {constraint}", context={"constraint": constraint}
    )


def new_duplicate_key_error(key: str) -> DuplicateKeyError:
    """Create a duplicate key error.

    The key may contain row data, so it goes into ``context["key"]`` only.
    """
    return DuplicateKeyError(context={"key": key})


def new_schema_error(code: str, message: str) -> SchemaError:
    """Create a schema error."""
    return _new(SchemaError, code, message)


def new_table_not_found_error(table: str) -> TableNotFoundError:
    """Create a table not found error."""
    return TableNotFoundError(f"table not found: {table}", context={"table": table})


def new_column_not_found_error(column: str) -> ColumnNotFoundError:
    """Create a column not found error."""
    return ColumnNotFoundError(
        f"column not found: {column}", context={"column": column}
    )


def new_adapter_error(code: str, message: str) -> AdapterError:
    """Create an adapter error."""
    return _new(AdapterError, code, message)


def new_unsupported_adapter_error(adapter: str) -> UnsupportedAdapterError:
    """Create an unsupported adapter error."""
    return UnsupportedAdapterError(
        f"unsupported adapter: {adapter}", context={"adapter": adapter}
    )


def new_unsupported_operation_error(operation: str) -> UnsupportedOperationError:
    """Create an error for an operation the adapter does not support."""
    return UnsupportedOperationError(
        f"operation not supported by this adapter: {operation}",
        context={"operation": operation},
    )


def new_validation_error(message: str) -> ValidationError:
    """Create a validation error."""
    return ValidationError(message)


def wrap_error(
    error: BaseException, error_type: ErrorType, code: str, message: str
) -> StorageError:
    """Wrap ``error`` in a storage error of the category ``error_type``."""
    return new_storage_error(error_type, code, message).with_cause(error)


# Predicates. Like Go's errors.As, they look through explicit causes
# (``raise ... from ...``) for the first storage error and work on any exception.


def _cause_chain(error: BaseException) -> Iterator[BaseException]:
    seen: set[int] = set()
    current: BaseException | None = error
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        yield current
        current = current.__cause__


def _first_storage_error(error: BaseException) -> StorageError | None:
    for exc in _cause_chain(error):
        if isinstance(exc, StorageError):
            return exc
    return None


def is_retryable(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is retryable."""
    found = _first_storage_error(error)
    return found is not None and found.retryable


def is_temporary(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is temporary."""
    found = _first_storage_error(error)
    return found is not None and found.temporary


def is_connection_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a connection error."""
    return isinstance(_first_storage_error(error), StorageConnectionError)


def is_query_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a query error."""
    return isinstance(_first_storage_error(error), QueryError)


def is_transaction_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a transaction error."""
    return isinstance(_first_storage_error(error), TransactionError)


def is_data_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a data error."""
    return isinstance(_first_storage_error(error), DataError)


def is_schema_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a schema error."""
    return isinstance(_first_storage_error(error), SchemaError)


def is_timeout_error(error: BaseException) -> bool:
    """Whether ``error`` (or the storage error it wraps) is a timeout error."""
    return isinstance(_first_storage_error(error), StorageTimeoutError)
