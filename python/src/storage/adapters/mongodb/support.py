"""
Operation deadlines and PyMongo error translation for the MongoDB adapter.

A deadline is a ``time.monotonic()`` value, or ``None`` for no limit. Operations
run under it with PyMongo's client-side operation timeout (``pymongo.timeout``).
"""

from __future__ import annotations

import contextlib
import datetime
import time
from collections.abc import Iterator

import pymongo
from bson.errors import BSONError
from pymongo.errors import DuplicateKeyError as DriverDuplicateKeyError
from pymongo.errors import OperationFailure, PyMongoError

from storage.errors import (
    DuplicateKeyError,
    InvalidQueryError,
    QueryFailedError,
    QueryTimeoutError,
    StorageError,
    TransactionFailedError,
    TransactionTimeoutError,
)

#: What a driver call can raise: PyMongo's errors, and bson's for a value it cannot
#: encode (``ValueError`` e.g. for a ``uuid.UUID`` without a configured UUID
#: representation, ``OverflowError`` for an integer wider than 64 bits).
DRIVER_ERRORS = (PyMongoError, BSONError, OverflowError, ValueError)

# pymongo.timeout(0) means "no timeout", so a deadline that has passed becomes the
# smallest timeout instead: operations that need the server then fail at once,
# while documents already received stay readable.
_PASSED = 1e-6


def positive_seconds(value: datetime.timedelta | None) -> float | None:
    """Return ``value`` in seconds, or ``None`` when it is unset or not positive."""
    if value is None or value <= datetime.timedelta(0):
        return None
    return value.total_seconds()


def deadline_after(timeout: datetime.timedelta | None) -> float | None:
    """Return the deadline ``timeout`` from now, or ``None`` for no timeout."""
    seconds = positive_seconds(timeout)
    return None if seconds is None else time.monotonic() + seconds


def earliest(first: float | None, second: float | None) -> float | None:
    """Return the earlier of two deadlines."""
    if first is None:
        return second
    if second is None:
        return first
    return min(first, second)


def has_passed(deadline: float | None) -> bool:
    """Return whether ``deadline`` has passed."""
    return deadline is not None and time.monotonic() >= deadline


@contextlib.contextmanager
def bounded(deadline: float | None) -> Iterator[None]:
    """Run the PyMongo operations of the block under ``deadline``."""
    if deadline is None:
        yield
        return
    with pymongo.timeout(max(deadline - time.monotonic(), _PASSED)):
        yield


def has_error_code(error: BaseException, code: int) -> bool:
    """Return whether ``error`` is a server error with ``code``."""
    return isinstance(error, OperationFailure) and error.code == code


def driver_error(error: Exception, message: str) -> StorageError:
    """Translate an exception of ``DRIVER_ERRORS`` raised by a query or command."""
    if isinstance(error, DriverDuplicateKeyError):
        return DuplicateKeyError(message)
    if isinstance(error, PyMongoError):
        if error.timeout:
            return QueryTimeoutError(message)
        if error.has_error_label("TransientTransactionError"):
            # E.g. a write conflict: the whole transaction can be retried.
            return TransactionFailedError(message, retryable=True, temporary=True)
        return QueryFailedError(message)
    return InvalidQueryError(f"{message}: a value cannot be encoded as BSON")


def transaction_error(error: PyMongoError, message: str) -> StorageError:
    """Translate an exception raised by committing or aborting a transaction."""
    if error.timeout:
        return TransactionTimeoutError()
    # Only a transient error guarantees that the transaction was aborted, so that
    # retrying it cannot apply it twice (unlike UnknownTransactionCommitResult).
    transient = error.has_error_label("TransientTransactionError")
    return TransactionFailedError(message, retryable=transient, temporary=transient)
