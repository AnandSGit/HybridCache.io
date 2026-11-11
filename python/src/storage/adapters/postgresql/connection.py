"""
PostgreSQL storage: an asyncpg connection pool running queries, commands,
transactions, batches and schema operations.

How ``Config`` maps to asyncpg:

- ``max_open_conns`` is the pool's ``max_size`` and ``max_idle_conns`` its
  ``min_size`` (connections kept open).
- ``conn_max_idle_time`` is ``max_inactive_connection_lifetime``; ``None`` never
  closes a connection for being idle.
- ``conn_max_lifetime``: asyncpg has no connection lifetime, so the storage
  expires the pooled connections (``Pool.expire_connections``) once per
  ``conn_max_lifetime``, checked whenever it takes a connection from the pool.
  No connection older than ``conn_max_lifetime`` is handed out; one in use is
  replaced when it is given back.
- ``connect_timeout`` bounds opening each connection; the DSN, connection fields,
  ``options`` and SSL settings are described in ``dsn.connect_arguments``.
- ``query_timeout`` bounds each query and command whose own timeout is ``None``
  (taking a connection from the pool and running it), schema operations and
  ``ping``; ``tx_timeout`` is the default ``TxOptions.timeout``.

``json`` and ``jsonb`` values are encoded with ``json.dumps``, except ``str``
values, which are sent as JSON text as they are; they are decoded with
``json.loads``.
"""

from __future__ import annotations

import asyncio
import datetime
import json
from collections.abc import AsyncIterator, Sequence
from contextlib import asynccontextmanager
from typing import Any

import asyncpg

from storage.adapters.postgresql.dsn import (
    DEFAULT_PORT,
    connect_arguments,
    redact_dsn,
    validate_config,
)
from storage.adapters.postgresql.errors import OpKind, map_error, translate_errors
from storage.adapters.postgresql.executor import (
    Connection,
    Deadline,
    Executor,
    Statement,
    positive_seconds,
)
from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.adapters.postgresql.schema import (
    DESCRIBE_TABLE_SQL,
    LIST_TABLES_SQL,
    TABLE_EXISTS_SQL,
    alter_table_statements,
    create_table_statements,
    describe_table,
)
from storage.adapters.postgresql.sql import quote_table, split_table_name
from storage.adapters.postgresql.transaction import (
    PostgreSQLTransaction,
    begin_statement,
)
from storage.errors import (
    ConnectionClosedError,
    ConnectionFailedError,
    ConnectionTimeoutError,
    InvalidConfigError,
    InvalidTransactionError,
    StorageError,
    StorageTimeoutError,
    new_query_timeout_error,
    new_table_not_found_error,
)
from storage.interfaces import Row, Storage
from storage.types import (
    Command,
    Config,
    DatabaseType,
    ExecuteResult,
    HealthStatus,
    HealthStatusType,
    Operation,
    OperationResult,
    Query,
    SchemaChange,
    StorageInfo,
    StorageLimits,
    TableSchema,
    TxOptions,
)

VERSION = "1.0.0"


class PostgreSQLStorage(Storage):
    """PostgreSQL storage on an asyncpg connection pool.

    ``PostgreSQLAdapter.connect`` returns it connected; a storage created
    directly opens its pool on ``connect()``. Results are buffered, so they hold
    no connection.
    """

    def __init__(self, config: Config) -> None:
        self._config = config
        self._pool: Any = None  # asyncpg.Pool
        self._closed = False
        self._executor = Executor(config.query_timeout)
        self._lifetime: float | None = None
        self._generation_started = 0.0

    async def connect(self) -> None:
        """Open the connection pool and check that the database answers; when
        the pool is open already, only check (``ping``).

        Raises:
            InvalidConfigError: If the configuration is invalid
                (``InvalidDSNError`` for its DSN).
            ConnectionClosedError: If the storage has been closed.
            ConnectionFailedError: If the database cannot be reached.
            ConnectionTimeoutError: If connecting timed out.
        """
        if self._pool is not None or self._closed:
            await self.ping()
            return
        validate_config(self._config)
        idle = self._config.conn_max_idle_time
        pool = asyncpg.create_pool(
            min_size=self._config.max_idle_conns,
            max_size=self._config.max_open_conns,
            max_inactive_connection_lifetime=0.0
            if idle is None
            else idle.total_seconds(),
            init=_init_connection,
            **connect_arguments(self._config),
        )
        try:
            await pool
            await self._ping(pool)
        except Exception as exc:
            pool.terminate()
            raise self._connection_error(
                exc, "failed to connect to PostgreSQL"
            ) from exc
        except BaseException:
            pool.terminate()
            raise
        lifetime = self._config.conn_max_lifetime
        self._lifetime = None if lifetime is None else lifetime.total_seconds()
        self._generation_started = asyncio.get_running_loop().time()
        self._pool = pool

    async def close(self) -> None:
        """Close the pool, waiting for connections in use (open transactions)
        to be given back. Closing again does nothing."""
        self._closed = True
        pool, self._pool = self._pool, None
        if pool is not None:
            await pool.close()

    async def ping(self) -> None:
        """Check that the database answers, within ``Config.query_timeout``.

        Raises:
            ConnectionClosedError: If the storage is closed or not connected.
            ConnectionFailedError: If the database cannot be reached.
            ConnectionTimeoutError: If it did not answer in time.
        """
        pool = self._require_pool()
        try:
            await self._ping(pool)
        except Exception as exc:
            raise self._connection_error(exc, "PostgreSQL did not answer") from exc

    async def health(self) -> HealthStatus:
        """Return the health status with the pool's statistics."""
        try:
            await self.ping()
        except StorageError as exc:
            return HealthStatus(
                status=HealthStatusType.UNHEALTHY,
                message=f"PostgreSQL connection is unhealthy: {exc.message}",
                details={"error": exc.code},
            )
        pool = self._require_pool()
        return HealthStatus(
            status=HealthStatusType.HEALTHY,
            message="PostgreSQL connection is healthy",
            details={
                "pool_size": pool.get_size(),
                "pool_idle": pool.get_idle_size(),
                "pool_max_size": pool.get_max_size(),
                "database": self._config.database,
                "host": self._config.host,
                "port": self._config.port or DEFAULT_PORT,
            },
        )

    async def query(self, query: Query) -> PostgreSQLResult:
        """Execute a query and return all its rows.

        ``?`` placeholders are rewritten to ``$n`` (see ``convert_placeholders``);
        SQL with ``$n`` placeholders is sent as it is.
        """
        statement = Statement.of(query)
        deadline = self._executor.deadline(statement.timeout)
        async with self._connection(deadline, statement.sql) as conn:
            return await self._executor.fetch(conn, statement, deadline)

    async def query_one(self, query: Query) -> PostgreSQLRow | None:
        """Execute a query and return its first row, or ``None`` if it has none."""
        statement = Statement.of(query)
        deadline = self._executor.deadline(statement.timeout)
        async with self._connection(deadline, statement.sql) as conn:
            return await self._executor.fetch_one(conn, statement, deadline)

    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command. ``last_insert_id`` is always ``None`` (use
        ``INSERT ... RETURNING`` with ``query``)."""
        statement = Statement.of(command)
        deadline = self._executor.deadline(statement.timeout)
        async with self._connection(deadline, statement.sql) as conn:
            return await self._executor.execute(conn, statement, deadline)

    async def begin_tx(self, options: TxOptions | None = None) -> PostgreSQLTransaction:
        """Begin a transaction on a pooled connection and return it, active.

        Its time limit is ``options.timeout``, else ``Config.tx_timeout``, and
        includes waiting for a connection.

        Raises:
            InvalidTransactionError: If the options are invalid.
            TransactionTimeoutError: If no connection was free in time.
            TransactionError: If ``BEGIN`` fails.
        """
        options = options or TxOptions()
        begin = begin_statement(options)
        timeout = (
            self._config.tx_timeout if options.timeout is None else options.timeout
        )
        seconds = positive_seconds(
            timeout, "transaction timeout", InvalidTransactionError
        )
        pool = self._require_pool()
        at = None if seconds is None else asyncio.get_running_loop().time() + seconds
        conn = await self._acquire(pool, Deadline(at, transaction=True), begin)
        try:
            with translate_errors(
                OpKind.TRANSACTION, "begin transaction failed", begin
            ):
                async with asyncio.timeout_at(at):
                    await conn.execute(begin)
        except BaseException as exc:
            if isinstance(exc, StorageTimeoutError):
                conn.terminate()
            await pool.release(conn)
            raise
        return PostgreSQLTransaction(
            conn, pool.release, query_timeout=self._config.query_timeout, deadline=at
        )

    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        """Execute operations in order on one connection, each on its own
        (autocommit): a failed operation's result carries its error and the
        others still run. ``Transaction.batch`` is all-or-nothing instead.

        Raises:
            InvalidQueryError: Before running anything, for an invalid operation.
        """
        if not operations:
            return []
        deadline = self._executor.deadline(None)
        async with self._connection(deadline, "") as conn:
            return await self._executor.batch(
                conn, operations, stop_at_first_error=False
            )

    async def create_table(self, schema: TableSchema) -> None:
        """Create a table with its primary key, foreign keys, indexes and column
        comments, in one transaction (see ``schema.create_table_statements``).

        Raises:
            InvalidSchemaError: If the definition is invalid.
            SchemaExistsError: If the table exists.
            SchemaError: If creating it fails.
        """
        await self._run_schema(create_table_statements(schema), "create table failed")

    async def drop_table(self, table_name: str) -> None:
        """Drop a table if it exists (``"table"`` or ``"schema.table"``)."""
        statement = f"DROP TABLE IF EXISTS {quote_table(table_name)}"
        await self._run_schema([statement], "drop table failed")

    async def alter_table(self, table_name: str, changes: list[SchemaChange]) -> None:
        """Apply the changes in one transaction (see
        ``schema.alter_table_statements``).

        Raises:
            InvalidSchemaError: If a change is invalid.
            TableNotFoundError: If the table does not exist.
            SchemaError: If a change fails.
        """
        quote_table(table_name)  # validates the name
        statements = [
            statement
            for change in changes
            for statement in alter_table_statements(table_name, change)
        ]
        if statements:
            await self._run_schema(statements, "alter table failed")

    def info(self) -> StorageInfo:
        """Return information about the storage."""
        return StorageInfo(
            name="PostgreSQL",
            version=VERSION,
            database_type=DatabaseType.POSTGRESQL,
            features=["transactions", "savepoints", "joins", "batch", "schema", "json"],
            limits=StorageLimits(
                max_connections=self._config.max_open_conns,
                max_query_size=1024 * 1024 * 1024,  # PostgreSQL's 1 GB limit
                max_transaction_age=datetime.timedelta(hours=24),
                max_batch_size=10_000,
            ),
        )

    async def list_tables(self) -> list[str]:
        """Return the base tables of the current schema, sorted."""
        rows = await self._introspect(LIST_TABLES_SQL, (), "list tables failed")
        return [str(row.get("table_name")) for row in rows]

    async def describe_table(self, table_name: str) -> TableSchema:
        """Describe a table of the current schema, or of the schema named by a
        ``"schema.table"`` name (see ``schema.describe_table``).

        Raises:
            TableNotFoundError: If the table does not exist.
        """
        schema, table = split_table_name(table_name)
        rows = await self._introspect(
            DESCRIBE_TABLE_SQL, (schema, table), "describe table failed"
        )
        if not rows:  # a table can have no columns: check that it exists
            found = await self._introspect(
                TABLE_EXISTS_SQL, (schema, table), "describe table failed"
            )
            if not found[0].get("exists"):
                raise new_table_not_found_error(table_name)
        return describe_table(table_name, rows)

    def _require_pool(self) -> Any:
        if self._pool is None:
            state = "closed" if self._closed else "not connected"
            raise ConnectionClosedError(f"PostgreSQL storage is {state}")
        return self._pool

    async def _ping(self, pool: Any) -> None:
        seconds = positive_seconds(self._config.query_timeout, "query_timeout")
        async with asyncio.timeout(seconds), pool.acquire() as conn:
            await conn.fetchval("SELECT 1")

    def _connection_error(self, error: Exception, message: str) -> StorageError:
        if isinstance(error, StorageError):
            return error
        context: dict[str, Any]
        if self._config.dsn:
            context = {"dsn": redact_dsn(self._config.dsn)}
        else:
            context = {
                "host": self._config.host,
                "port": self._config.port or DEFAULT_PORT,
                "database": self._config.database,
            }
        mapped: StorageError
        if isinstance(error, TimeoutError):
            mapped = ConnectionTimeoutError(message, context=context)
        elif isinstance(error, asyncpg.ClientConfigurationError):
            mapped = InvalidConfigError("invalid connection settings", context=context)
        else:
            mapped = ConnectionFailedError(message, context=context)
        code = getattr(error, "sqlstate", None)
        if isinstance(error, asyncpg.PostgresError) and code:
            mapped.with_context("sqlstate", code)
        return mapped.with_cause(error)

    async def _acquire(self, pool: Any, deadline: Deadline, sql: str) -> Connection:
        """Take a connection from the pool before ``deadline``, first expiring
        the pool's connections when ``conn_max_lifetime`` has passed."""
        if self._lifetime is not None:
            now = asyncio.get_running_loop().time()
            if now - self._generation_started >= self._lifetime:
                self._generation_started = now
                await pool.expire_connections()
        try:
            async with asyncio.timeout_at(deadline.at):
                return await pool.acquire()
        except TimeoutError as exc:
            raise deadline.timeout_error(sql) from exc
        except Exception as exc:
            raise self._connection_error(exc, "could not get a connection") from exc

    @asynccontextmanager
    async def _connection(
        self, deadline: Deadline, sql: str
    ) -> AsyncIterator[Connection]:
        pool = self._require_pool()
        conn = await self._acquire(pool, deadline, sql)
        try:
            yield conn
        finally:
            await pool.release(conn)

    async def _run_schema(self, statements: list[str], message: str) -> None:
        """Run DDL statements in one transaction, within ``query_timeout``."""
        deadline = self._executor.deadline(None)
        async with self._connection(deadline, statements[0]) as conn:
            current = statements[0]
            try:
                async with asyncio.timeout_at(deadline.at), conn.transaction():
                    for statement in statements:
                        current = statement
                        await conn.execute(statement)
            except TimeoutError as exc:
                raise new_query_timeout_error(current) from exc
            except StorageError:
                raise
            except Exception as exc:
                raise map_error(exc, OpKind.SCHEMA, message, current) from exc

    async def _introspect(
        self, sql: str, parameters: Sequence[Any], message: str
    ) -> list[Row]:
        statement = Statement(sql, parameters, None)
        deadline = self._executor.deadline(None)
        async with self._connection(deadline, sql) as conn:
            result = await self._executor.fetch(
                conn, statement, deadline, kind=OpKind.SCHEMA, message=message
            )
        return await result.fetchall()


async def _init_connection(connection: Connection) -> None:
    """Register the JSON codecs on a new pooled connection."""
    for type_name in ("json", "jsonb"):
        await connection.set_type_codec(
            type_name, schema="pg_catalog", encoder=_encode_json, decoder=json.loads
        )


def _encode_json(value: Any) -> str:
    return value if isinstance(value, str) else json.dumps(value)
