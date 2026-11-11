"""Behaviour of the storage error hierarchy (storage.errors)."""

from __future__ import annotations

import asyncio
import builtins
import datetime
import pickle
import re
from collections.abc import Callable
from pathlib import Path
from typing import assert_type

import pytest

import storage
import storage.errors as storage_errors
from storage.errors import (
    AdapterError,
    AdapterFailedError,
    AdapterNotFoundError,
    ColumnNotFoundError,
    ConnectionClosedError,
    ConnectionFailedError,
    ConnectionPoolFullError,
    ConnectionTimeoutError,
    ConstraintViolationError,
    DataError,
    DataTruncatedError,
    DeadlockError,
    DuplicateKeyError,
    IndexNotFoundError,
    InternalError,
    InvalidConfigError,
    InvalidDataError,
    InvalidDSNError,
    InvalidParametersError,
    InvalidQueryError,
    InvalidSchemaError,
    InvalidTransactionError,
    NoRowsError,
    QueryError,
    QueryFailedError,
    QueryTimeoutError,
    ResourceError,
    SchemaError,
    SchemaExistsError,
    StorageConnectionError,
    StorageError,
    StorageNotImplementedError,
    StoragePermissionError,
    StorageTimeoutError,
    TableNotFoundError,
    TooManyRowsError,
    TransactionClosedError,
    TransactionError,
    TransactionFailedError,
    TransactionTimeoutError,
    UnsupportedAdapterError,
    UnsupportedOperationError,
    UnsupportedQueryError,
    ValidationError,
    is_connection_error,
    is_data_error,
    is_query_error,
    is_retryable,
    is_schema_error,
    is_temporary,
    is_timeout_error,
    is_transaction_error,
    new_adapter_error,
    new_column_not_found_error,
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
    new_storage_error,
    new_table_not_found_error,
    new_transaction_closed_error,
    new_transaction_error,
    new_transaction_timeout_error,
    new_unsupported_adapter_error,
    new_unsupported_operation_error,
    new_unsupported_query_error,
    new_validation_error,
    wrap_error,
)
from storage.types import ErrorType

# The sentinel table of internal/domain/errors.go (codeSentinels).
GO_SENTINELS: dict[str, type[StorageError]] = {
    "CONNECTION_FAILED": ConnectionFailedError,
    "CONNECTION_TIMEOUT": ConnectionTimeoutError,
    "CONNECTION_CLOSED": ConnectionClosedError,
    "CONNECTION_POOL_FULL": ConnectionPoolFullError,
    "INVALID_DSN": InvalidDSNError,
    "INVALID_CONFIG": InvalidConfigError,
    "INVALID_QUERY": InvalidQueryError,
    "QUERY_TIMEOUT": QueryTimeoutError,
    "QUERY_FAILED": QueryFailedError,
    "INVALID_PARAMETERS": InvalidParametersError,
    "UNSUPPORTED_QUERY": UnsupportedQueryError,
    "TRANSACTION_FAILED": TransactionFailedError,
    "TRANSACTION_TIMEOUT": TransactionTimeoutError,
    "TRANSACTION_CLOSED": TransactionClosedError,
    "INVALID_TRANSACTION": InvalidTransactionError,
    "DEADLOCK": DeadlockError,
    "NO_ROWS": NoRowsError,
    "TOO_MANY_ROWS": TooManyRowsError,
    "INVALID_DATA": InvalidDataError,
    "DATA_TRUNCATED": DataTruncatedError,
    "CONSTRAINT_VIOLATION": ConstraintViolationError,
    "DUPLICATE_KEY": DuplicateKeyError,
    "TABLE_NOT_FOUND": TableNotFoundError,
    "COLUMN_NOT_FOUND": ColumnNotFoundError,
    "INDEX_NOT_FOUND": IndexNotFoundError,
    "INVALID_SCHEMA": InvalidSchemaError,
    "SCHEMA_EXISTS": SchemaExistsError,
    "ADAPTER_NOT_FOUND": AdapterNotFoundError,
    "UNSUPPORTED_ADAPTER": UnsupportedAdapterError,
    "ADAPTER_FAILED": AdapterFailedError,
    "NOT_IMPLEMENTED": StorageNotImplementedError,
    "UNSUPPORTED_OPERATION": UnsupportedOperationError,
    "INTERNAL_ERROR": InternalError,
}

CATEGORY_OF_TYPE: dict[ErrorType, type[StorageError]] = {
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

GO_ERRORS_FILE = Path(__file__).resolve().parents[3] / "internal/domain/errors.go"

PREDICATES: list[Callable[[BaseException], bool]] = [
    is_retryable,
    is_temporary,
    is_connection_error,
    is_query_error,
    is_transaction_error,
    is_data_error,
    is_schema_error,
    is_timeout_error,
]


# Codes and hierarchy


@pytest.mark.parametrize(("code", "cls"), GO_SENTINELS.items())
def test_each_go_sentinel_has_a_subclass_with_its_code(
    code: str, cls: type[StorageError]
) -> None:
    assert cls.code == code
    assert cls().code == code
    # Building an error from the code yields the class callers catch.
    assert type(new_storage_error(cls.error_type, code, "message")) is cls


def test_sentinel_table_matches_the_go_implementation() -> None:
    if not GO_ERRORS_FILE.is_file():
        pytest.skip("Go sources are not available")

    go_codes = set(re.findall(r'"([A-Z_]+)":\s+Err\w+,', GO_ERRORS_FILE.read_text()))

    assert go_codes == set(GO_SENTINELS)


def test_codes_identify_exactly_one_class() -> None:
    classes = [
        obj
        for obj in vars(storage_errors).values()
        if isinstance(obj, type) and issubclass(obj, StorageError)
    ]
    codes = [cls.code for cls in classes]

    assert len(codes) == len(set(codes))


@pytest.mark.parametrize(
    ("cls", "categories"),
    [
        (NoRowsError, (DataError,)),
        (DuplicateKeyError, (DataError,)),
        (ConstraintViolationError, (DataError,)),
        (TableNotFoundError, (SchemaError,)),
        (ColumnNotFoundError, (SchemaError,)),
        (DeadlockError, (TransactionError,)),
        (TransactionClosedError, (TransactionError,)),
        (UnsupportedQueryError, (QueryError,)),
        (UnsupportedOperationError, (AdapterError,)),
        (InvalidDSNError, (ValidationError,)),
        (InvalidConfigError, (ValidationError,)),
        (InvalidQueryError, (QueryError, ValidationError)),
        (InvalidParametersError, (QueryError, ValidationError)),
        (QueryTimeoutError, (QueryError, StorageTimeoutError)),
        (ConnectionTimeoutError, (StorageConnectionError, StorageTimeoutError)),
        (TransactionTimeoutError, (TransactionError, StorageTimeoutError)),
        (ConnectionPoolFullError, (StorageConnectionError, ResourceError)),
    ],
)
def test_errors_are_caught_by_their_categories(
    cls: type[StorageError], categories: tuple[type[StorageError], ...]
) -> None:
    for category in (*categories, StorageError):
        with pytest.raises(category):
            raise cls()


@pytest.mark.parametrize(
    ("cls", "error_type"),
    [
        (QueryTimeoutError, ErrorType.TIMEOUT),
        (ConnectionTimeoutError, ErrorType.TIMEOUT),
        (TransactionTimeoutError, ErrorType.TIMEOUT),
        (ConnectionPoolFullError, ErrorType.RESOURCE),
        (InvalidQueryError, ErrorType.VALIDATION),
        (UnsupportedQueryError, ErrorType.QUERY),
        (UnsupportedOperationError, ErrorType.ADAPTER),
        (NoRowsError, ErrorType.DATA),
    ],
)
def test_error_type_matches_the_go_constructors(
    cls: type[StorageError], error_type: ErrorType
) -> None:
    assert cls().error_type is error_type


@pytest.mark.parametrize(
    ("cls", "retryable", "temporary"),
    [
        (DeadlockError, True, True),
        (QueryTimeoutError, True, True),
        (ConnectionTimeoutError, True, True),
        (ConnectionPoolFullError, True, True),
        (TransactionTimeoutError, False, True),
        (NoRowsError, False, False),
        (DuplicateKeyError, False, False),
        (QueryFailedError, False, False),
    ],
)
def test_retryability(
    cls: type[StorageError], retryable: bool, temporary: bool
) -> None:
    error = cls()

    assert is_retryable(error) is retryable
    assert is_temporary(error) is temporary


def test_instance_overrides_leave_the_class_defaults_alone() -> None:
    assert is_retryable(DeadlockError(retryable=False)) is False
    assert is_temporary(DeadlockError(temporary=False)) is False
    assert is_retryable(NoRowsError().with_retryable(True)) is True
    assert is_temporary(NoRowsError().with_temporary(True)) is True

    assert is_retryable(DeadlockError()) is True
    assert is_temporary(DeadlockError()) is True
    assert is_retryable(NoRowsError()) is False
    assert is_temporary(NoRowsError()) is False


@pytest.mark.parametrize("name", ["MigrationError", "CacheError"])
def test_errors_of_unimplemented_features_are_gone(name: str) -> None:
    assert not hasattr(storage_errors, name)


def test_error_types_of_unimplemented_features_are_gone() -> None:
    assert {"MIGRATION", "CACHE"}.isdisjoint(ErrorType.__members__)


BUILTIN_EXCEPTIONS = sorted(
    name
    for name, obj in vars(builtins).items()
    if isinstance(obj, type) and issubclass(obj, BaseException)
)


@pytest.mark.parametrize("module", [storage_errors, storage], ids=lambda m: m.__name__)
def test_no_builtin_exception_is_shadowed(module: object) -> None:
    assert [name for name in BUILTIN_EXCEPTIONS if name in vars(module)] == []


# Construction


def test_default_and_custom_messages() -> None:
    assert NoRowsError().message == "no rows found"
    assert str(NoRowsError()) == "NO_ROWS: no rows found"
    assert (
        str(UnsupportedQueryError("joins are not supported by the MongoDB adapter"))
        == "UNSUPPORTED_QUERY: joins are not supported by the MongoDB adapter"
    )


def test_cause_is_chained_like_raise_from() -> None:
    original = ValueError("boom")

    error = QueryFailedError("insert failed", cause=original)

    assert error.cause is original
    assert error.__cause__ is original
    assert error.__suppress_context__ is True
    assert str(error) == "QUERY_FAILED: insert failed (caused by: boom)"


def test_raise_from_sets_the_cause() -> None:
    original = ValueError("boom")

    with pytest.raises(QueryFailedError) as excinfo:
        raise QueryFailedError from original

    assert excinfo.value.cause is original


def test_context_is_copied_and_extendable() -> None:
    details = {"table": "users"}

    error = TableNotFoundError(context=details).with_context("schema", "public")
    details["table"] = "changed"

    assert error.context == {"table": "users", "schema": "public"}


def test_with_methods_return_the_same_precisely_typed_error() -> None:
    error = new_deadlock_error()

    chained = (
        error.with_context("attempt", 2)
        .with_cause(ValueError("boom"))
        .with_retryable(False)
        .with_temporary(False)
    )

    assert_type(chained, DeadlockError)
    assert chained is error


def test_repr_names_the_concrete_class() -> None:
    class CustomQueryError(QueryError):
        pass

    assert repr(NoRowsError()).startswith("NoRowsError(code='NO_ROWS', ")
    assert repr(CustomQueryError("x")).startswith(
        "CustomQueryError(code='QUERY_ERROR', "
    )


def test_timestamp_is_timezone_aware() -> None:
    before = datetime.datetime.now(datetime.UTC)

    error = NoRowsError()

    # Comparing with an aware datetime would raise TypeError for a naive one.
    assert before <= error.timestamp <= datetime.datetime.now(datetime.UTC)
    assert error.timestamp.utcoffset() == datetime.timedelta(0)


def test_errors_survive_pickling() -> None:
    error = new_table_not_found_error("users").with_retryable(True)

    clone = pickle.loads(pickle.dumps(error))  # noqa: S301 - our own data

    assert type(clone) is TableNotFoundError
    assert (clone.code, clone.message, clone.context, clone.retryable) == (
        "TABLE_NOT_FOUND",
        "table not found: users",
        {"table": "users"},
        True,
    )


# Constructor helpers


@pytest.mark.parametrize(
    ("make", "cls"),
    [
        (new_no_rows_error, NoRowsError),
        (new_deadlock_error, DeadlockError),
        (new_transaction_closed_error, TransactionClosedError),
        (new_transaction_timeout_error, TransactionTimeoutError),
        (new_connection_pool_full_error, ConnectionPoolFullError),
        (lambda: new_connection_timeout_error("dial timeout"), ConnectionTimeoutError),
        (lambda: new_query_timeout_error("SELECT 1"), QueryTimeoutError),
        (lambda: new_invalid_query_error("no table"), InvalidQueryError),
        (lambda: new_unsupported_query_error("joins"), UnsupportedQueryError),
        (lambda: new_constraint_violation_error("age_check"), ConstraintViolationError),
        (lambda: new_duplicate_key_error("users_email_key"), DuplicateKeyError),
        (lambda: new_table_not_found_error("users"), TableNotFoundError),
        (lambda: new_column_not_found_error("email"), ColumnNotFoundError),
        (lambda: new_unsupported_adapter_error("oracle"), UnsupportedAdapterError),
        (
            lambda: new_unsupported_operation_error("savepoint"),
            UnsupportedOperationError,
        ),
        (lambda: new_validation_error("bad"), ValidationError),
    ],
)
def test_helpers_build_the_dedicated_class(
    make: Callable[[], StorageError], cls: type[StorageError]
) -> None:
    assert type(make()) is cls


def test_helpers_are_precisely_typed() -> None:
    assert_type(new_no_rows_error(), NoRowsError)
    assert_type(new_query_timeout_error("SELECT 1"), QueryTimeoutError)
    assert_type(new_unsupported_operation_error("savepoint"), UnsupportedOperationError)
    assert_type(new_validation_error("bad"), ValidationError)
    assert_type(new_query_error("QUERY_FAILED", "failed"), QueryError)


def test_new_validation_error() -> None:
    error = new_validation_error("max_open_conns must be positive")

    assert isinstance(error, ValidationError)
    assert str(error) == "VALIDATION_ERROR: max_open_conns must be positive"


def test_identifiers_are_in_the_message_and_the_context() -> None:
    error = new_table_not_found_error("users")

    assert "users" in str(error)
    assert error.context == {"table": "users"}


def test_query_text_and_key_values_stay_out_of_messages() -> None:
    sql = "SELECT * FROM users WHERE ssn = '123-45-6789'"

    timeout = new_query_timeout_error(sql)
    duplicate = new_duplicate_key_error("email=alice@example.com")

    assert "123-45-6789" not in str(timeout)
    assert timeout.context == {"query": sql}
    assert "alice@example.com" not in str(duplicate)
    assert duplicate.context == {"key": "email=alice@example.com"}


@pytest.mark.parametrize(
    ("make", "category", "known_code", "dedicated"),
    [
        (
            new_connection_error,
            StorageConnectionError,
            "CONNECTION_CLOSED",
            ConnectionClosedError,
        ),
        (new_query_error, QueryError, "UNSUPPORTED_QUERY", UnsupportedQueryError),
        (
            new_transaction_error,
            TransactionError,
            "TRANSACTION_CLOSED",
            TransactionClosedError,
        ),
        (new_data_error, DataError, "NO_ROWS", NoRowsError),
        (new_schema_error, SchemaError, "TABLE_NOT_FOUND", TableNotFoundError),
        (
            new_adapter_error,
            AdapterError,
            "UNSUPPORTED_OPERATION",
            UnsupportedOperationError,
        ),
    ],
)
def test_code_helpers_build_the_class_of_the_code(
    make: Callable[[str, str], StorageError],
    category: type[StorageError],
    known_code: str,
    dedicated: type[StorageError],
) -> None:
    known = make(known_code, "known")
    custom = make("CUSTOM_CODE", "custom")

    assert type(known) is dedicated
    assert known.message == "known"
    assert type(custom) is category
    assert custom.code == "CUSTOM_CODE"


def test_code_helpers_keep_codes_of_other_categories_in_their_own() -> None:
    error = new_query_error("NO_ROWS", "nothing")  # NoRowsError is a DataError

    assert type(error) is QueryError
    assert error.code == "NO_ROWS"


@pytest.mark.parametrize("error_type", list(ErrorType))
def test_new_storage_error_uses_the_category_of_the_error_type(
    error_type: ErrorType,
) -> None:
    error = new_storage_error(error_type, "CUSTOM_CODE", "message")

    assert type(error) is CATEGORY_OF_TYPE[error_type]
    assert error.error_type is error_type
    assert error.code == "CUSTOM_CODE"


def test_wrap_error_chains_the_cause_and_resolves_the_code() -> None:
    original = TimeoutError("timed out")

    error = wrap_error(
        original, ErrorType.TIMEOUT, "QUERY_TIMEOUT", "statement timeout"
    )

    assert type(error) is QueryTimeoutError
    assert error.cause is original
    assert error.message == "statement timeout"
    assert is_retryable(error)


# Predicates


@pytest.mark.parametrize("predicate", PREDICATES, ids=lambda p: p.__name__)
@pytest.mark.parametrize(
    "error",
    [ValueError("x"), asyncio.CancelledError(), KeyboardInterrupt()],
    ids=lambda e: type(e).__name__,
)
def test_predicates_accept_any_exception(
    predicate: Callable[[BaseException], bool], error: BaseException
) -> None:
    assert predicate(error) is False


@pytest.mark.parametrize(
    ("predicate", "error"),
    [
        (is_connection_error, ConnectionClosedError()),
        (is_connection_error, ConnectionTimeoutError()),
        (is_query_error, QueryTimeoutError()),
        (is_transaction_error, DeadlockError()),
        (is_data_error, NoRowsError()),
        (is_schema_error, TableNotFoundError()),
        (is_timeout_error, QueryTimeoutError()),
        (is_timeout_error, TransactionTimeoutError()),
    ],
)
def test_predicates_recognise_their_category(
    predicate: Callable[[BaseException], bool], error: StorageError
) -> None:
    assert predicate(error) is True


def test_predicates_look_through_explicit_causes() -> None:
    wrapper = RuntimeError("giving up")
    wrapper.__cause__ = DeadlockError()

    assert is_retryable(wrapper)
    assert is_transaction_error(wrapper)


def test_predicates_ignore_the_implicit_context() -> None:
    wrapper = RuntimeError("while handling another error")
    wrapper.__context__ = DeadlockError()

    assert not is_retryable(wrapper)


def test_the_first_storage_error_in_the_chain_decides() -> None:
    # Like Go's errors.As: the outer storage error is found first.
    assert not is_retryable(QueryFailedError(cause=DeadlockError()))


def test_cause_cycles_terminate() -> None:
    first, second = ValueError("first"), ValueError("second")
    first.__cause__, second.__cause__ = second, first

    assert not is_retryable(first)
