"""
Mapping of asyncpg exceptions to the storage error hierarchy.

Server errors are mapped by SQLSTATE, as in the Go adapter
(``internal/infrastructure/adapters/postgres/errors.go``). The original
exception is kept as ``__cause__``, and the SQLSTATE is recorded in
``context["sqlstate"]``.
"""

from __future__ import annotations

import enum
import re
from collections.abc import Iterator
from contextlib import contextmanager

import asyncpg

from storage.errors import (
    DataTruncatedError,
    DeadlockError,
    InvalidDataError,
    InvalidQueryError,
    QueryFailedError,
    SchemaError,
    SchemaExistsError,
    StorageError,
    TransactionFailedError,
    TransactionTimeoutError,
    new_column_not_found_error,
    new_constraint_violation_error,
    new_duplicate_key_error,
    new_query_timeout_error,
    new_table_not_found_error,
)


class OpKind(enum.Enum):
    """The kind of operation that failed; selects the fallback error class."""

    QUERY = "query"  # queries, commands and batches
    TRANSACTION = "transaction"  # BEGIN, COMMIT, ROLLBACK and savepoints
    SCHEMA = "schema"  # DDL and schema introspection


# SQLSTATE codes with a dedicated mapping
UNIQUE_VIOLATION = "23505"
NOT_NULL_VIOLATION = "23502"
SERIALIZATION_FAILURE = "40001"
DEADLOCK_DETECTED = "40P01"
IN_FAILED_TRANSACTION = "25P02"
UNDEFINED_TABLE = "42P01"
UNDEFINED_COLUMN = "42703"
DUPLICATE_OBJECTS = frozenset({"42P07", "42701", "42710"})  # table, column, object
SYNTAX_ERROR = "42601"
STRING_TRUNCATION = "22001"
QUERY_CANCELED = "57014"

_QUOTED_NAME = re.compile(r'"([^"]*)"')


def map_error(
    error: BaseException, kind: OpKind, message: str, sql: str = ""
) -> StorageError:
    """Return the storage error for ``error``, with ``error`` as its cause.

    ``message`` describes the failed operation and is used when there is no more
    specific mapping; ``sql`` is the statement involved, recorded only for
    timeouts (in ``context["query"]``). Storage errors are returned unchanged.
    """
    if isinstance(error, StorageError):
        return error
    mapped: StorageError
    if isinstance(error, asyncpg.PostgresError):
        mapped = _map_postgres_error(error, kind, message, sql)
    elif isinstance(error, TimeoutError):
        mapped = (
            TransactionTimeoutError()
            if kind is OpKind.TRANSACTION
            else new_query_timeout_error(sql)
        )
    else:
        mapped = _fallback(kind, message)
    return mapped.with_cause(error)


@contextmanager
def translate_errors(kind: OpKind, message: str, sql: str = "") -> Iterator[None]:
    """Re-raise exceptions other than storage errors as storage errors (see
    ``map_error``). ``asyncio.CancelledError`` passes through unchanged."""
    try:
        yield
    except StorageError:
        raise
    except Exception as exc:
        raise map_error(exc, kind, message, sql) from exc


def _map_postgres_error(
    error: BaseException, kind: OpKind, message: str, sql: str
) -> StorageError:
    code = str(getattr(error, "sqlstate", "") or "")
    mapped: StorageError
    if code == UNIQUE_VIOLATION:
        mapped = new_duplicate_key_error(_field(error, "constraint_name"))
    elif code == NOT_NULL_VIOLATION:
        mapped = new_constraint_violation_error(_field(error, "column_name"))
    elif code.startswith("23"):  # foreign key, check, exclusion, ...
        mapped = new_constraint_violation_error(_field(error, "constraint_name"))
    elif code == DEADLOCK_DETECTED:
        mapped = DeadlockError()
    elif code == SERIALIZATION_FAILURE:
        mapped = TransactionFailedError(
            "could not serialize access due to concurrent transactions",
            retryable=True,
            temporary=True,
        )
    elif code == IN_FAILED_TRANSACTION:
        mapped = TransactionFailedError(
            "transaction is aborted; roll it back before issuing more statements"
        )
    elif code == UNDEFINED_TABLE:
        mapped = new_table_not_found_error(_name(error, "table_name"))
    elif code == UNDEFINED_COLUMN:
        mapped = new_column_not_found_error(_name(error, "column_name"))
    elif code in DUPLICATE_OBJECTS:
        mapped = SchemaExistsError("object already exists")
    elif code == SYNTAX_ERROR:
        mapped = InvalidQueryError("syntax error in SQL statement")
    elif code == STRING_TRUNCATION:
        mapped = DataTruncatedError("value too long for column")
    elif code.startswith("22"):  # data exception, including invalid parameters
        mapped = InvalidDataError("invalid data for column type")
    elif code == QUERY_CANCELED:
        mapped = new_query_timeout_error(sql)
    else:
        mapped = _fallback(kind, message)
    if code:
        mapped.with_context("sqlstate", code)
    return mapped


def _fallback(kind: OpKind, message: str) -> StorageError:
    if kind is OpKind.TRANSACTION:
        return TransactionFailedError(message)
    if kind is OpKind.SCHEMA:
        return SchemaError(message)
    return QueryFailedError(message)


def _field(error: BaseException, name: str) -> str:
    value = getattr(error, name, None)
    return value if isinstance(value, str) else ""


def _name(error: BaseException, field: str) -> str:
    """The object named by ``field``, else the first double-quoted name in the
    server message, as in ``relation "users" does not exist``."""
    value = _field(error, field)
    if value:
        return value
    text = _field(error, "message") or (str(error.args[0]) if error.args else "")
    found = _QUOTED_NAME.search(text)
    return found.group(1) if found else ""
