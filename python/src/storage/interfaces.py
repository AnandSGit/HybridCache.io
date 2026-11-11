"""
Core interfaces for the Database Agnostic Storage Library.

Abstract base classes that every database adapter implements, mirroring
``internal/domain/interfaces.go`` of the Go implementation.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from collections.abc import AsyncIterator
from types import TracebackType
from typing import Any, Self, final

from storage.types import (
    Command,
    Config,
    DatabaseType,
    DataType,
    ExecuteResult,
    HealthStatus,
    Operation,
    OperationResult,
    Query,
    SchemaChange,
    StorageInfo,
    TableSchema,
    TxOptions,
)


class Row(ABC):
    """A single result row."""

    @abstractmethod
    async def scan(self, *destinations: Any) -> None:
        """Scan the row's values, in column order, into ``destinations``.

        Raises:
            DataError: If scanning fails.
        """

    @abstractmethod
    async def scan_into(self, obj: Any) -> None:
        """Set the attributes of ``obj`` (e.g. a dataclass) from matching columns.

        Raises:
            DataError: If scanning fails.
        """

    @abstractmethod
    def get(self, key: str, default: Any = None) -> Any:
        """Return the value of column ``key``, or ``default`` if there is none."""

    @abstractmethod
    def keys(self) -> list[str]:
        """Return the column names."""

    @abstractmethod
    def values(self) -> list[Any]:
        """Return the column values."""

    @abstractmethod
    def items(self) -> list[tuple[str, Any]]:
        """Return the (column name, value) pairs."""


class Result(ABC):
    """Query results, consumed as an async iterator of rows or with ``fetch*``."""

    @abstractmethod
    def __aiter__(self) -> AsyncIterator[Row]:
        """Return the async iterator over the remaining rows."""

    @abstractmethod
    async def __anext__(self) -> Row:
        """Return the next row; raise ``StopAsyncIteration`` when exhausted."""

    @abstractmethod
    async def fetchone(self) -> Row | None:
        """Return the next row, or ``None`` when exhausted."""

    @abstractmethod
    async def fetchmany(self, size: int = 1000) -> list[Row]:
        """Return up to ``size`` of the remaining rows."""

    @abstractmethod
    async def fetchall(self) -> list[Row]:
        """Return all remaining rows."""

    @abstractmethod
    async def close(self) -> None:
        """Close the result set and free its resources."""

    @abstractmethod
    def columns(self) -> list[str]:
        """Return the column names."""


class Transaction(ABC):
    """A database transaction, begun by ``Storage.begin_tx``.

    ``begin_tx`` begins the transaction exactly once; entering ``async with`` does
    not begin it again. Leaving the block commits when the block completes
    normally and rolls back when it raises, unless the transaction was already
    committed or rolled back inside the block::

        async with await storage.begin_tx() as tx:
            await tx.execute(command)

    When set, ``TxOptions.timeout`` (else ``Config.tx_timeout``) bounds the
    transaction from ``begin_tx`` until it ends.
    """

    @final
    async def __aenter__(self) -> Self:
        return self

    @final
    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        if not self.is_active():
            return
        if exc_type is not None:
            await self.rollback()
            return
        try:
            await self.commit()
        except BaseException:
            # Do not leave a transaction open when its commit failed.
            if self.is_active():
                await self.rollback()
            raise

    @abstractmethod
    async def query(self, query: Query) -> Result:
        """Execute a query within the transaction.

        Raises:
            TransactionClosedError: If the transaction is not active.
            QueryError: If the query fails.
        """

    @abstractmethod
    async def query_one(self, query: Query) -> Row | None:
        """Execute a query and return its first row, or ``None`` if it has none.

        Raises:
            TransactionClosedError: If the transaction is not active.
            QueryError: If the query fails.
        """

    @abstractmethod
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command within the transaction.

        Raises:
            TransactionClosedError: If the transaction is not active.
            QueryError: If the command fails.
        """

    @abstractmethod
    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        """Execute operations in order within the transaction.

        Raises:
            TransactionClosedError: If the transaction is not active.
        """

    @abstractmethod
    async def commit(self) -> None:
        """Commit the transaction.

        Raises:
            TransactionClosedError: If the transaction is not active.
            TransactionError: If the commit fails.
        """

    @abstractmethod
    async def rollback(self) -> None:
        """Roll back the transaction; a no-op when it is no longer active.

        Raises:
            TransactionError: If the rollback fails.
        """

    @abstractmethod
    async def savepoint(self, name: str) -> None:
        """Create a savepoint.

        Raises:
            TransactionClosedError: If the transaction is not active.
            UnsupportedOperationError: If the database has no savepoints (MongoDB).
        """

    @abstractmethod
    async def rollback_to_savepoint(self, name: str) -> None:
        """Roll back to a savepoint.

        Raises:
            TransactionClosedError: If the transaction is not active.
            UnsupportedOperationError: If the database has no savepoints (MongoDB).
        """

    @abstractmethod
    def is_active(self) -> bool:
        """Return whether the transaction is still open."""

    @abstractmethod
    def get_id(self) -> str:
        """Return the transaction ID."""


class Storage(ABC):
    """Main interface for database storage operations.

    ``Adapter.connect`` returns an already-connected storage. A ``Query`` or
    ``Command`` without a timeout is bounded by ``Config.query_timeout``.
    """

    @abstractmethod
    async def connect(self) -> None:
        """Re-verify connectivity.

        Raises:
            StorageConnectionError: If the database cannot be reached.
        """

    @abstractmethod
    async def close(self) -> None:
        """Close the connections and free resources."""

    @abstractmethod
    async def ping(self) -> None:
        """Check the connection.

        Raises:
            StorageConnectionError: If the database cannot be reached.
        """

    @abstractmethod
    async def health(self) -> HealthStatus:
        """Return the health status."""

    @abstractmethod
    async def query(self, query: Query) -> Result:
        """Execute a query.

        Raises:
            QueryError: If the query fails; ``UnsupportedQueryError`` if the adapter
                cannot execute it.
        """

    @abstractmethod
    async def query_one(self, query: Query) -> Row | None:
        """Execute a query and return its first row, or ``None`` if it has none.

        Raises:
            QueryError: If the query fails.
        """

    @abstractmethod
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command.

        Raises:
            QueryError: If the command fails; ``UnsupportedQueryError`` if the
                adapter cannot execute it.
        """

    @abstractmethod
    async def begin_tx(self, options: TxOptions | None = None) -> Transaction:
        """Begin a transaction and return it, already active.

        Raises:
            TransactionError: If the transaction cannot be begun.
        """

    @abstractmethod
    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        """Execute operations in order; each result carries its error, if any."""

    @abstractmethod
    async def create_table(self, schema: TableSchema) -> None:
        """Create a table (a collection, for document databases).

        Raises:
            SchemaError: If the table cannot be created.
        """

    @abstractmethod
    async def drop_table(self, table_name: str) -> None:
        """Drop a table.

        Raises:
            SchemaError: If the table cannot be dropped.
        """

    @abstractmethod
    async def alter_table(self, table_name: str, changes: list[SchemaChange]) -> None:
        """Apply schema changes to a table.

        Raises:
            SchemaError: If a change fails.
            UnsupportedOperationError: If the adapter does not support a change.
        """

    @abstractmethod
    def info(self) -> StorageInfo:
        """Return information about the storage."""

    @abstractmethod
    async def list_tables(self) -> list[str]:
        """Return the table names.

        Raises:
            SchemaError: If listing fails.
        """

    @abstractmethod
    async def describe_table(self, table_name: str) -> TableSchema:
        """Return the schema of a table.

        Raises:
            TableNotFoundError: If the table does not exist.
            SchemaError: If describing fails.
        """


class Adapter(ABC):
    """Factory and metadata for one database type."""

    @abstractmethod
    def name(self) -> str:
        """Return the adapter name."""

    @abstractmethod
    def version(self) -> str:
        """Return the adapter version."""

    @abstractmethod
    def database_type(self) -> DatabaseType:
        """Return the database type the adapter connects to."""

    @abstractmethod
    async def connect(self, config: Config) -> Storage:
        """Validate ``config`` and return a connected storage.

        Raises:
            ValidationError: If the configuration is invalid.
            StorageConnectionError: If the connection fails.
        """

    @abstractmethod
    def parse_dsn(self, dsn: str) -> Config:
        """Parse a data source name into a configuration.

        Raises:
            InvalidDSNError: If the DSN is invalid.
        """

    @abstractmethod
    def validate_config(self, config: Config) -> None:
        """Validate a configuration.

        Raises:
            InvalidConfigError: If the configuration is invalid.
        """

    @abstractmethod
    def translate_query(self, query: Query) -> tuple[str, list[Any]]:
        """Return the native form of ``query`` and its parameters.

        Raises:
            QueryError: If the query cannot be translated.
        """

    @abstractmethod
    def translate_command(self, command: Command) -> tuple[str, list[Any]]:
        """Return the native form of ``command`` and its parameters.

        Raises:
            QueryError: If the command cannot be translated.
        """

    @abstractmethod
    def map_python_type(self, value: Any) -> DataType:
        """Return the data type that stores a Python value such as ``value``.

        Raises:
            ValidationError: If the type cannot be mapped.
        """

    @abstractmethod
    def map_database_type(self, db_type: str) -> DataType:
        """Return the data type for a database type name.

        Raises:
            ValidationError: If the type cannot be mapped.
        """

    @abstractmethod
    def supports_transactions(self) -> bool:
        """Return whether the adapter supports transactions."""

    @abstractmethod
    def supports_joins(self) -> bool:
        """Return whether the adapter supports joins."""

    @abstractmethod
    def supports_batch(self) -> bool:
        """Return whether the adapter supports batch operations."""

    @abstractmethod
    def supports_schema(self) -> bool:
        """Return whether the adapter supports schema operations."""
