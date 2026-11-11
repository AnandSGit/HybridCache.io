"""
MongoDB multi-document transactions (replica sets and sharded clusters only).

Operations of a transaction are serialised, because a session must not be used
concurrently. Results returned by ``query`` read through the transaction's
session, so consume them before ``commit`` or ``rollback``.
"""

from __future__ import annotations

import asyncio
import contextlib
import datetime
import uuid
from typing import Any

from pymongo import AsyncMongoClient, ReadPreference
from pymongo.asynchronous.client_session import AsyncClientSession
from pymongo.errors import PyMongoError
from pymongo.read_concern import ReadConcern
from pymongo.write_concern import WriteConcern

from storage.adapters.mongodb.executor import Executor, read_only_error
from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow
from storage.adapters.mongodb.support import (
    bounded,
    deadline_after,
    has_passed,
    positive_seconds,
    transaction_error,
)
from storage.errors import (
    TransactionClosedError,
    TransactionFailedError,
    TransactionTimeoutError,
    new_unsupported_operation_error,
)
from storage.interfaces import Transaction
from storage.types import (
    Command,
    ExecuteResult,
    IsolationLevel,
    Operation,
    OperationResult,
    Query,
    TxOptions,
)


def transaction_options(isolation: IsolationLevel) -> dict[str, Any]:
    """Return the ``start_transaction`` options for an isolation level.

    Reads in a transaction must use the primary, so the read preference is always
    primary (``TxOptions.read_only`` makes the transaction reject writes instead).
    Snapshot is MongoDB's strongest transaction read concern; linearizable is not
    allowed in transactions. DEFAULT and READ_UNCOMMITTED keep the server's
    default read concern.
    """
    options: dict[str, Any] = {"read_preference": ReadPreference.PRIMARY}
    if isolation is IsolationLevel.READ_COMMITTED:
        options["read_concern"] = ReadConcern("majority")
    elif isolation in (IsolationLevel.REPEATABLE_READ, IsolationLevel.SERIALIZABLE):
        options["read_concern"] = ReadConcern("snapshot")
        options["write_concern"] = WriteConcern(w="majority")
    return options


class MongoDBTransaction(Transaction):
    """A transaction on a MongoDB client session.

    ``TxOptions.timeout`` (else ``Config.tx_timeout``) bounds the transaction:
    after it, operations raise ``TransactionTimeoutError``, and so does ``commit``,
    which then aborts the transaction.
    """

    def __init__(
        self,
        session: AsyncClientSession,
        executor: Executor,
        *,
        deadline: float | None = None,
        read_only: bool = False,
    ) -> None:
        """Wrap ``session``, whose transaction has been started."""
        self._session = session
        self._executor = executor
        self._deadline = deadline
        self._read_only = read_only
        self._active = True
        # Not the session id: PyMongo reuses server sessions, and so their ids,
        # across transactions.
        self._id = str(uuid.uuid4())
        self._lock = asyncio.Lock()

    @classmethod
    async def create(
        cls,
        client: AsyncMongoClient[dict[str, Any]],
        executor: Executor,
        options: TxOptions | None = None,
        default_timeout: datetime.timedelta | None = None,
    ) -> MongoDBTransaction:
        """Start a session and a transaction on it.

        Raises:
            TransactionFailedError: If the transaction cannot be started.
        """
        options = options if options is not None else TxOptions()
        timeout = options.timeout
        if positive_seconds(timeout) is None:
            timeout = default_timeout
        deadline = deadline_after(timeout)
        try:
            session = client.start_session()
        except PyMongoError as exc:
            raise TransactionFailedError("failed to start a MongoDB session") from exc
        try:
            await session.start_transaction(**transaction_options(options.isolation))
        except PyMongoError as exc:
            with contextlib.suppress(PyMongoError):
                await session.end_session()
            raise TransactionFailedError(
                "failed to start a MongoDB transaction"
            ) from exc
        return cls(session, executor, deadline=deadline, read_only=options.read_only)

    def _check(self) -> None:
        if not self._active:
            raise TransactionClosedError()
        if has_passed(self._deadline):
            raise TransactionTimeoutError()

    async def query(self, query: Query) -> MongoDBResult:
        async with self._lock:
            self._check()
            return await self._executor.query(query, self._session, self._deadline)

    async def query_one(self, query: Query) -> MongoDBRow | None:
        async with self._lock:
            self._check()
            return await self._executor.query_one(query, self._session, self._deadline)

    async def execute(self, command: Command) -> ExecuteResult:
        async with self._lock:
            self._check()
            if self._read_only:
                raise read_only_error()
            return await self._executor.execute(command, self._session, self._deadline)

    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        async with self._lock:
            self._check()
            return await self._executor.batch(
                operations, self._session, self._deadline, read_only=self._read_only
            )

    async def commit(self) -> None:
        """Commit the transaction, which ends whether or not the commit succeeds.

        Raises:
            TransactionClosedError: If the transaction is not active.
            TransactionTimeoutError: If the transaction's deadline has passed.
            TransactionFailedError: If the commit fails (retryable when the
                server aborted the transaction, e.g. after a write conflict).
        """
        async with self._lock:
            if not self._active:
                raise TransactionClosedError()
            try:
                if has_passed(self._deadline):
                    # Ending the session below aborts the transaction.
                    raise TransactionTimeoutError()
                with bounded(self._deadline):
                    await self._session.commit_transaction()
            except PyMongoError as exc:
                message = "failed to commit the MongoDB transaction"
                raise transaction_error(exc, message) from exc
            finally:
                await self._end()

    async def rollback(self) -> None:
        """Abort the transaction; a no-op when it is no longer active.

        The abort is not bounded by the transaction's deadline, so that a
        timed-out transaction is still rolled back.
        """
        async with self._lock:
            if not self._active:
                return
            try:
                await self._session.abort_transaction()
            except PyMongoError as exc:
                message = "failed to roll back the MongoDB transaction"
                raise transaction_error(exc, message) from exc
            finally:
                await self._end()

    async def _end(self) -> None:
        self._active = False
        # Ending the session aborts a transaction still in progress; failing to
        # return the session to the pool is not actionable.
        with contextlib.suppress(PyMongoError):
            await self._session.end_session()

    async def savepoint(self, name: str) -> None:
        """Not supported: MongoDB transactions have no savepoints."""
        raise new_unsupported_operation_error("savepoint")

    async def rollback_to_savepoint(self, name: str) -> None:
        """Not supported: MongoDB transactions have no savepoints."""
        raise new_unsupported_operation_error("savepoint")

    def is_active(self) -> bool:
        return self._active

    def get_id(self) -> str:
        return self._id
