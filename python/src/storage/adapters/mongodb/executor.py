"""
Execution of translated queries and commands.

``MongoDBStorage`` runs operations without a session and ``MongoDBTransaction``
with its own, so both share this one implementation.
"""

from __future__ import annotations

import datetime
from typing import Any

from pymongo.asynchronous.client_session import AsyncClientSession
from pymongo.asynchronous.collection import AsyncCollection
from pymongo.asynchronous.database import AsyncDatabase

from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow, nested_document
from storage.adapters.mongodb.support import (
    DRIVER_ERRORS,
    bounded,
    deadline_after,
    driver_error,
    earliest,
    positive_seconds,
)
from storage.adapters.mongodb.translate import (
    COUNT_COLUMN,
    QueryKind,
    QueryPlan,
    plan_command,
    plan_query,
)
from storage.errors import InvalidQueryError, InvalidTransactionError, StorageError
from storage.interfaces import Result
from storage.types import (
    Command,
    CommandType,
    ExecuteResult,
    Operation,
    OperationResult,
    OperationType,
    Query,
)

_FAILURES = {
    CommandType.INSERT: "insert into {!r} failed",
    CommandType.UPDATE: "update of {!r} failed",
    CommandType.DELETE: "delete from {!r} failed",
}


def read_only_error() -> InvalidTransactionError:
    """Return the error for a write in a read-only transaction."""
    return InvalidTransactionError(
        "cannot execute a write command in a read-only transaction"
    )


def integer_id(value: Any) -> int | None:
    """Return an inserted ``_id`` that is an integer; ObjectIds and other ``_id``
    types have no integer form."""
    if isinstance(value, int) and not isinstance(value, bool):
        return int(value)
    return None


class Executor:
    """Runs queries and commands against one database.

    Each operation is bounded by its own timeout, else the configured query
    timeout, and by the deadline of the enclosing transaction, if any.
    """

    def __init__(
        self,
        database: AsyncDatabase[dict[str, Any]],
        query_timeout: datetime.timedelta | None,
    ) -> None:
        self.database = database
        self._query_timeout = query_timeout

    def _deadline(
        self, timeout: datetime.timedelta | None, outer: float | None
    ) -> float | None:
        if positive_seconds(timeout) is None:
            timeout = self._query_timeout
        return earliest(deadline_after(timeout), outer)

    async def query(
        self,
        query: Query,
        session: AsyncClientSession | None = None,
        deadline: float | None = None,
    ) -> MongoDBResult:
        """Execute a query; finds return a cursor-backed result.

        Args:
            query: The query.
            session: The transaction's session, if any.
            deadline: The transaction's deadline, if any.
        """
        plan = plan_query(query)
        if plan.empty:
            return MongoDBResult(columns=plan.columns)
        own_deadline = self._deadline(query.timeout, deadline)
        collection = self.database[plan.collection]
        if plan.kind is not QueryKind.FIND:
            documents = await self._computed(collection, plan, session, own_deadline)
            return MongoDBResult(documents, plan.columns)

        # count_documents rejects a skip or limit of 0.
        pagination = {"skip": plan.skip, "limit": plan.limit}
        count_options = {key: value for key, value in pagination.items() if value}

        async def count() -> int:
            try:
                with bounded(self._deadline(query.timeout, deadline)):
                    return await collection.count_documents(
                        plan.criteria, session=session, **count_options
                    )
            except DRIVER_ERRORS as exc:
                message = f"count on {plan.collection!r} failed"
                raise driver_error(exc, message) from exc

        cursor = collection.find(
            plan.criteria,
            projection=plan.projection,
            sort=plan.sort or None,
            skip=plan.skip,
            limit=plan.limit,
            session=session,
        )
        return await MongoDBResult.open(
            cursor, plan.columns, deadline=own_deadline, counter=count
        )

    async def query_all(
        self,
        query: Query,
        session: AsyncClientSession | None = None,
        deadline: float | None = None,
    ) -> MongoDBResult:
        """Execute a query and read all its rows into a result without a cursor."""
        result = await self.query(query, session, deadline)
        return await result.materialize()

    async def query_one(
        self,
        query: Query,
        session: AsyncClientSession | None = None,
        deadline: float | None = None,
    ) -> MongoDBRow | None:
        """Execute a query and return its first row, or ``None`` if it has none."""
        plan = plan_query(query)
        if plan.empty:
            return None
        own_deadline = self._deadline(query.timeout, deadline)
        collection = self.database[plan.collection]
        if plan.kind is not QueryKind.FIND:
            documents = await self._computed(collection, plan, session, own_deadline)
            return MongoDBRow(documents[0], plan.columns) if documents else None
        try:
            with bounded(own_deadline):
                document = await collection.find_one(
                    plan.criteria,
                    projection=plan.projection,
                    sort=plan.sort or None,
                    skip=plan.skip,
                    session=session,
                )
        except DRIVER_ERRORS as exc:
            raise driver_error(exc, f"find on {plan.collection!r} failed") from exc
        return None if document is None else MongoDBRow(document, plan.columns)

    async def _computed(
        self,
        collection: AsyncCollection[dict[str, Any]],
        plan: QueryPlan,
        session: AsyncClientSession | None,
        deadline: float | None,
    ) -> list[dict[str, Any]]:
        """Run a COUNT or DISTINCT plan and return its rows as documents."""
        try:
            with bounded(deadline):
                if plan.kind is QueryKind.COUNT:
                    count = await collection.count_documents(
                        plan.criteria, session=session
                    )
                    return [{COUNT_COLUMN: count}]
                values = await collection.distinct(
                    plan.columns[0], plan.criteria, session=session
                )
        except DRIVER_ERRORS as exc:
            message = f"{plan.kind.value} on {plan.collection!r} failed"
            raise driver_error(exc, message) from exc
        return [nested_document(plan.columns[0], value) for value in values]

    async def execute(
        self,
        command: Command,
        session: AsyncClientSession | None = None,
        deadline: float | None = None,
    ) -> ExecuteResult:
        """Execute an INSERT (``insert_one``), UPDATE (``update_many`` with
        ``$set``) or DELETE (``delete_many``).

        ``rows_affected`` of an UPDATE counts the matched documents, as SQL does,
        even when the new values equal the old ones.
        """
        plan = plan_command(command)
        collection = self.database[plan.collection]
        try:
            with bounded(self._deadline(command.timeout, deadline)):
                if plan.command_type is CommandType.INSERT:
                    inserted = await collection.insert_one(
                        plan.document, session=session
                    )
                    return ExecuteResult(
                        rows_affected=1,
                        last_insert_id=integer_id(inserted.inserted_id),
                    )
                if plan.command_type is CommandType.UPDATE:
                    updated = await collection.update_many(
                        plan.criteria, plan.update(), session=session
                    )
                    return ExecuteResult(rows_affected=updated.matched_count)
                deleted = await collection.delete_many(plan.criteria, session=session)
                return ExecuteResult(rows_affected=deleted.deleted_count)
        except DRIVER_ERRORS as exc:
            message = _FAILURES[plan.command_type].format(plan.collection)
            raise driver_error(exc, message) from exc

    async def batch(
        self,
        operations: list[Operation],
        session: AsyncClientSession | None = None,
        deadline: float | None = None,
        *,
        read_only: bool = False,
    ) -> list[OperationResult]:
        """Execute the operations sequentially, in order, as ``query_all`` and
        ``execute`` do; each result carries its rows, ``ExecuteResult`` or error."""
        results = []
        for index, operation in enumerate(operations):
            outcome = OperationResult(index=index)
            try:
                outcome.result = await self._run(
                    operation, session, deadline, read_only
                )
            except StorageError as exc:
                outcome.error = exc
            results.append(outcome)
        return results

    async def _run(
        self,
        operation: Operation,
        session: AsyncClientSession | None,
        deadline: float | None,
        read_only: bool,
    ) -> Result | ExecuteResult:
        kind = operation.operation_type
        if kind is OperationType.QUERY and operation.query is not None:
            return await self.query_all(operation.query, session, deadline)
        if kind is OperationType.COMMAND and operation.command is not None:
            if read_only:
                raise read_only_error()
            return await self.execute(operation.command, session, deadline)
        raise InvalidQueryError(f"{kind.value} operation has no {kind.value}")
