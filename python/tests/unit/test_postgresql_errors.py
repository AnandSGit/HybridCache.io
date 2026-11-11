"""Unit tests for the mapping of asyncpg exceptions to storage errors."""

from __future__ import annotations

import asyncio

import asyncpg
import pytest

from storage.adapters.postgresql.errors import OpKind, map_error, translate_errors
from storage.errors import (
    ColumnNotFoundError,
    ConstraintViolationError,
    DataTruncatedError,
    DeadlockError,
    DuplicateKeyError,
    InvalidDataError,
    InvalidQueryError,
    QueryError,
    QueryFailedError,
    QueryTimeoutError,
    SchemaError,
    SchemaExistsError,
    StorageError,
    TableNotFoundError,
    TransactionError,
    TransactionFailedError,
    TransactionTimeoutError,
    is_retryable,
)

E = asyncpg.exceptions


def _pg_error(cls: type[Exception], message: str = "boom", **fields: str) -> Exception:
    """An asyncpg server error with the given error fields."""
    error = cls(message)
    for name, value in fields.items():
        setattr(error, name, value)
    return error


@pytest.mark.parametrize(
    ("error", "expected", "sqlstate"),
    [
        (
            _pg_error(E.UniqueViolationError, constraint_name="users_email_key"),
            DuplicateKeyError,
            "23505",
        ),
        (
            _pg_error(E.NotNullViolationError, column_name="email"),
            ConstraintViolationError,
            "23502",
        ),
        (
            _pg_error(E.ForeignKeyViolationError, constraint_name="fk"),
            ConstraintViolationError,
            "23503",
        ),
        (
            _pg_error(E.CheckViolationError, constraint_name="ck"),
            ConstraintViolationError,
            "23514",
        ),
        (_pg_error(E.ExclusionViolationError), ConstraintViolationError, "23P01"),
        (_pg_error(E.DeadlockDetectedError), DeadlockError, "40P01"),
        (_pg_error(E.SerializationError), TransactionFailedError, "40001"),
        (_pg_error(E.InFailedSQLTransactionError), TransactionFailedError, "25P02"),
        (_pg_error(E.UndefinedTableError), TableNotFoundError, "42P01"),
        (_pg_error(E.UndefinedColumnError), ColumnNotFoundError, "42703"),
        (_pg_error(E.DuplicateTableError), SchemaExistsError, "42P07"),
        (_pg_error(E.DuplicateColumnError), SchemaExistsError, "42701"),
        (_pg_error(E.DuplicateObjectError), SchemaExistsError, "42710"),
        (_pg_error(E.PostgresSyntaxError), InvalidQueryError, "42601"),
        (_pg_error(E.StringDataRightTruncationError), DataTruncatedError, "22001"),
        (_pg_error(E.InvalidTextRepresentationError), InvalidDataError, "22P02"),
        # asyncpg's client-side "invalid input for query argument $1" error
        (_pg_error(E.DataError), InvalidDataError, "22000"),
        (_pg_error(E.QueryCanceledError), QueryTimeoutError, "57014"),
        (_pg_error(E.InsufficientPrivilegeError), QueryFailedError, "42501"),
    ],
)
def test_server_errors_are_mapped_by_sqlstate(
    error: Exception, expected: type[StorageError], sqlstate: str
) -> None:
    mapped = map_error(error, OpKind.QUERY, "query failed", "SELECT 1")
    assert type(mapped) is expected
    assert mapped.context["sqlstate"] == sqlstate
    assert mapped.__cause__ is error


def test_duplicate_key_names_the_constraint_but_not_the_value() -> None:
    error = _pg_error(
        E.UniqueViolationError,
        'duplicate key value violates unique constraint "users_email_key"',
        constraint_name="users_email_key",
        detail="Key (email)=(ann@example.com) already exists.",
    )
    mapped = map_error(error, OpKind.QUERY, "command failed")
    assert isinstance(mapped, DuplicateKeyError)
    assert mapped.code == "DUPLICATE_KEY"
    assert mapped.context["key"] == "users_email_key"
    assert "ann@example.com" not in mapped.message


def test_constraint_violations_name_the_constraint_or_column() -> None:
    not_null = map_error(
        _pg_error(E.NotNullViolationError, column_name="email"), OpKind.QUERY, "x"
    )
    assert not_null.context["constraint"] == "email"
    check = map_error(
        _pg_error(E.CheckViolationError, constraint_name="positive_age"),
        OpKind.QUERY,
        "x",
    )
    assert check.message == "constraint violation: positive_age"


def test_missing_objects_are_named_from_the_fields_or_the_message() -> None:
    from_message = map_error(
        _pg_error(E.UndefinedTableError, 'relation "users" does not exist'),
        OpKind.QUERY,
        "x",
    )
    assert from_message.context["table"] == "users"
    from_field = map_error(
        _pg_error(E.UndefinedColumnError, "column does not exist", column_name="age"),
        OpKind.QUERY,
        "x",
    )
    assert from_field.context["column"] == "age"
    unnamed = map_error(_pg_error(E.UndefinedTableError, "no name"), OpKind.QUERY, "x")
    assert unnamed.context["table"] == ""


def test_retryable_errors() -> None:
    deadlock = map_error(_pg_error(E.DeadlockDetectedError), OpKind.QUERY, "x")
    serialization = map_error(_pg_error(E.SerializationError), OpKind.TRANSACTION, "x")
    assert is_retryable(deadlock)
    assert is_retryable(serialization)
    assert serialization.temporary
    assert isinstance(serialization, TransactionError)
    syntax = map_error(_pg_error(E.PostgresSyntaxError), OpKind.QUERY, "x")
    assert not is_retryable(syntax)


def test_query_errors_stay_query_errors_whatever_the_operation() -> None:
    for kind in OpKind:
        mapped = map_error(_pg_error(E.PostgresSyntaxError), kind, "x")
        assert isinstance(mapped, QueryError)
        assert not isinstance(mapped, TransactionError)


@pytest.mark.parametrize(
    ("kind", "expected"),
    [
        (OpKind.QUERY, QueryFailedError),
        (OpKind.TRANSACTION, TransactionFailedError),
        (OpKind.SCHEMA, SchemaError),
    ],
)
def test_unmapped_errors_fall_back_by_operation(
    kind: OpKind, expected: type[StorageError]
) -> None:
    for error in (
        _pg_error(E.InsufficientPrivilegeError),
        E.InterfaceError("cannot perform operation: another operation is in progress"),
        OSError("connection reset"),
    ):
        mapped = map_error(error, kind, "operation failed")
        assert type(mapped) is expected
        assert mapped.message == "operation failed"
        assert mapped.__cause__ is error


def test_timeouts() -> None:
    query = map_error(TimeoutError(), OpKind.QUERY, "x", "SELECT pg_sleep(9)")
    assert isinstance(query, QueryTimeoutError)
    assert query.context["query"] == "SELECT pg_sleep(9)"
    assert "pg_sleep" not in query.message
    assert is_retryable(query)
    assert isinstance(
        map_error(TimeoutError(), OpKind.TRANSACTION, "x"), TransactionTimeoutError
    )
    canceled = map_error(_pg_error(E.QueryCanceledError), OpKind.QUERY, "x", "SELECT 2")
    assert canceled.context["query"] == "SELECT 2"


def test_storage_errors_are_returned_unchanged() -> None:
    error = TableNotFoundError("table not found: t")
    assert map_error(error, OpKind.QUERY, "x") is error


def test_translate_errors_maps_and_chains() -> None:
    error = _pg_error(E.PostgresSyntaxError)
    with (
        pytest.raises(InvalidQueryError) as caught,
        translate_errors(OpKind.QUERY, "x"),
    ):
        raise error
    assert caught.value.__cause__ is error


def test_translate_errors_passes_storage_errors_and_cancellation_through() -> None:
    original = DeadlockError()
    with pytest.raises(DeadlockError) as caught, translate_errors(OpKind.QUERY, "x"):
        raise original
    assert caught.value is original
    with pytest.raises(asyncio.CancelledError), translate_errors(OpKind.QUERY, "x"):
        raise asyncio.CancelledError
