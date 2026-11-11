"""
MongoDB storage on PyMongo's native asyncio API (``pymongo.AsyncMongoClient``).

Queries and commands must be built with the query builder: their ``spec`` is
translated into MongoDB operations (see ``translate``), and raw SQL is rejected.
"""

from __future__ import annotations

import contextlib
import datetime
from dataclasses import dataclass
from typing import Any
from urllib.parse import quote_plus, urlencode

from pymongo import ASCENDING, HASHED, AsyncMongoClient, IndexModel, uri_parser
from pymongo.asynchronous.database import AsyncDatabase
from pymongo.errors import CollectionInvalid, InvalidName, PyMongoError

from storage.adapters.mongodb.executor import Executor
from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow
from storage.adapters.mongodb.support import (
    bounded,
    deadline_after,
    has_error_code,
    positive_seconds,
)
from storage.adapters.mongodb.transaction import MongoDBTransaction
from storage.adapters.mongodb.translate import check_field
from storage.errors import (
    ConnectionClosedError,
    ConnectionFailedError,
    ConnectionTimeoutError,
    IndexNotFoundError,
    InvalidConfigError,
    InvalidDSNError,
    InvalidSchemaError,
    SchemaError,
    SchemaExistsError,
    StorageError,
    ValidationError,
    new_table_not_found_error,
    new_unsupported_operation_error,
)
from storage.interfaces import Storage
from storage.types import (
    Command,
    Config,
    DatabaseType,
    ExecuteResult,
    HealthStatus,
    HealthStatusType,
    IndexDefinition,
    IndexType,
    Operation,
    OperationResult,
    Query,
    SchemaChange,
    SchemaChangeType,
    StorageInfo,
    StorageLimits,
    TableSchema,
    TxOptions,
)

ADAPTER_NAME = "mongodb"
ADAPTER_VERSION = "1.0.0"
DEFAULT_PORT = 27017

# MongoDB server error codes.
_INDEX_NOT_FOUND = 27
_NAMESPACE_EXISTS = 48

_INDEX_KEYS: dict[IndexType, int | str] = {
    IndexType.BTREE: ASCENDING,
    IndexType.HASH: HASHED,
}


@dataclass(frozen=True)
class ClientSettings:
    """How to create the client for a configuration."""

    uri: str
    database: str
    options: dict[str, Any]  # AsyncMongoClient keyword arguments


def parse_uri(
    uri: str, error: type[ValidationError] = InvalidDSNError
) -> dict[str, Any]:
    """Parse and validate a MongoDB URI with ``pymongo.uri_parser.parse_uri``.

    A ``mongodb+srv://`` URI is resolved through DNS, as PyMongo does.

    Raises:
        ValidationError: ``error``, if the URI is invalid.
    """
    try:
        return uri_parser.parse_uri(uri)
    except (PyMongoError, ValueError) as exc:
        if "@" in uri:
            # PyMongo's message may quote the URI's credentials.
            raise error("invalid MongoDB URI") from None
        raise error("invalid MongoDB URI") from exc


def build_uri(config: Config) -> str:
    """Build a connection URI from the components of ``config``.

    Credentials and options are percent-encoded. The database is not put in the
    path, where it would also select the authentication database: it is selected
    on the client instead.
    """
    host = config.host
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"  # IPv6 literal
    port = config.port if config.port is not None else DEFAULT_PORT
    userinfo = ""
    if config.username:
        userinfo = quote_plus(config.username)
        if config.password:
            userinfo += ":" + quote_plus(config.password)
        userinfo += "@"
    uri = f"mongodb://{userinfo}{host}:{port}/"
    pairs = [
        (key, _option_text(item))
        for key, value in sorted(config.options.items())
        for item in (value if isinstance(value, list) else [value])
    ]
    return f"{uri}?{urlencode(pairs)}" if pairs else uri


def _option_text(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def client_settings(config: Config) -> ClientSettings:
    """Validate ``config`` and return how to create its client.

    The URI is ``config.dsn``, else built from the components. Options that the
    URI sets take precedence over the pool and connect-timeout fields of
    ``config``; the pool sizes count as one setting, so that they stay
    consistent.

    Raises:
        InvalidDSNError: If ``config.dsn`` is invalid.
        InvalidConfigError: If the configuration is otherwise invalid.
    """
    if not config.dsn and not config.host:
        raise InvalidConfigError("host is required")
    if config.dsn:
        uri, parsed = config.dsn, parse_uri(config.dsn)
    else:
        uri = build_uri(config)
        parsed = parse_uri(uri, InvalidConfigError)
    database = config.database or parsed["database"] or ""
    if not database:
        raise InvalidConfigError("database is required")
    if config.max_open_conns < 0:
        raise InvalidConfigError("max_open_conns cannot be negative")
    if config.max_idle_conns < 0:
        raise InvalidConfigError("max_idle_conns cannot be negative")
    if 0 < config.max_open_conns < config.max_idle_conns:
        raise InvalidConfigError("max_idle_conns cannot exceed max_open_conns")

    explicit = {name.lower() for name in parsed["options"]}
    options: dict[str, Any] = {}
    if not explicit & {"maxpoolsize", "minpoolsize"}:
        # Zero keeps the driver's default.
        if config.max_open_conns:
            options["maxPoolSize"] = config.max_open_conns
        if config.max_idle_conns:
            options["minPoolSize"] = config.max_idle_conns
    idle = positive_seconds(config.conn_max_idle_time)
    if idle is not None and "maxidletimems" not in explicit:
        options["maxIdleTimeMS"] = round(idle * 1000)
    connect = positive_seconds(config.connect_timeout)
    if connect is not None and "connecttimeoutms" not in explicit:
        options["connectTimeoutMS"] = round(connect * 1000)
    return ClientSettings(uri, database, options)


class MongoDBStorage(Storage):
    """MongoDB storage. ``MongoDBAdapter.connect`` returns a connected one."""

    def __init__(self, config: Config) -> None:
        self._config = config
        self._client: AsyncMongoClient[dict[str, Any]] | None = None
        self._executor: Executor | None = None

    async def connect(self) -> None:
        """Connect, or re-verify the connection; possible again after ``close``.

        Raises:
            ValidationError: If the configuration is invalid.
            ConnectionTimeoutError: If the server cannot be reached within
                ``Config.connect_timeout``.
            ConnectionFailedError: If the connection fails otherwise.
        """
        if self._client is not None:
            await self.ping()
            return
        settings = client_settings(self._config)
        try:
            client: AsyncMongoClient[dict[str, Any]] = AsyncMongoClient(
                settings.uri, **settings.options
            )
        except (PyMongoError, ValueError, TypeError) as exc:
            raise InvalidConfigError("invalid MongoDB client options") from exc
        try:
            database = client[settings.database]
        except InvalidName as exc:
            await client.close()
            message = f"invalid database name {settings.database!r}"
            raise InvalidConfigError(message) from exc
        try:
            await self._ping(client)
        except BaseException:
            await client.close()
            raise
        self._client = client
        self._executor = Executor(database, self._config.query_timeout)

    async def close(self) -> None:
        """Close the client; idempotent."""
        client, self._client, self._executor = self._client, None, None
        if client is not None:
            await client.close()

    async def _ping(self, client: AsyncMongoClient[dict[str, Any]]) -> None:
        try:
            with bounded(deadline_after(self._config.connect_timeout)):
                await client.admin.command("ping")
        except PyMongoError as exc:
            if exc.timeout:
                raise ConnectionTimeoutError("timed out connecting to MongoDB") from exc
            raise ConnectionFailedError("failed to ping MongoDB") from exc

    def _connected(self) -> tuple[AsyncMongoClient[dict[str, Any]], Executor]:
        if self._client is None or self._executor is None:
            raise ConnectionClosedError("the MongoDB storage is not connected")
        return self._client, self._executor

    def _database(self) -> AsyncDatabase[dict[str, Any]]:
        return self._connected()[1].database

    async def ping(self) -> None:
        """Ping the server, within ``Config.connect_timeout``."""
        await self._ping(self._connected()[0])

    async def health(self) -> HealthStatus:
        """Ping the server and report its version (both need no privileges)."""
        try:
            await self.ping()
        except StorageError as exc:
            return HealthStatus(
                HealthStatusType.UNHEALTHY, f"MongoDB ping failed: {exc}"
            )
        try:
            with bounded(deadline_after(self._config.connect_timeout)):
                build = await self._database().command("buildInfo")
        except PyMongoError as exc:
            message = f"MongoDB is reachable but buildInfo failed: {exc}"
            return HealthStatus(HealthStatusType.DEGRADED, message)
        return HealthStatus(
            HealthStatusType.HEALTHY,
            "MongoDB is healthy",
            details={"version": build.get("version", "")},
        )

    async def query(self, query: Query) -> MongoDBResult:
        return await self._connected()[1].query(query)

    async def query_one(self, query: Query) -> MongoDBRow | None:
        return await self._connected()[1].query_one(query)

    async def execute(self, command: Command) -> ExecuteResult:
        return await self._connected()[1].execute(command)

    async def begin_tx(self, options: TxOptions | None = None) -> MongoDBTransaction:
        """Begin a multi-document transaction (needs a replica set or a sharded
        cluster)."""
        client, executor = self._connected()
        return await MongoDBTransaction.create(
            client, executor, options, self._config.tx_timeout
        )

    async def batch(self, operations: list[Operation]) -> list[OperationResult]:
        """Execute the operations sequentially, in order; each result carries its
        rows (read into memory), ``ExecuteResult`` or error."""
        return await self._connected()[1].batch(operations)

    async def create_table(self, schema: TableSchema) -> None:
        """Create a collection and its indexes, in order and with their names.

        Columns and constraints are not enforced: collections are schemaless.

        Raises:
            SchemaExistsError: If the collection exists.
            UnsupportedOperationError: For index types other than BTREE and HASH,
                and for partial indexes.
        """
        if not schema.name:
            raise InvalidSchemaError("table name is required")
        models = [index_model(definition) for definition in schema.indexes]
        database = self._database()
        context = {"table": schema.name}
        try:
            await database.create_collection(schema.name)
        except PyMongoError as exc:
            if isinstance(exc, CollectionInvalid) or has_error_code(
                exc, _NAMESPACE_EXISTS
            ):
                message = f"collection {schema.name!r} already exists"
                raise SchemaExistsError(message, context=context) from exc
            message = f"failed to create collection {schema.name!r}"
            raise SchemaError(
                message, code="CREATE_COLLECTION_FAILED", context=context
            ) from exc
        if not models:
            return
        try:
            await database[schema.name].create_indexes(models)
        except PyMongoError as exc:
            # Leave no half-created table behind.
            with contextlib.suppress(PyMongoError):
                await database.drop_collection(schema.name)
            message = f"failed to create the indexes of {schema.name!r}"
            raise SchemaError(
                message, code="CREATE_INDEX_FAILED", context=context
            ) from exc

    async def drop_table(self, table_name: str) -> None:
        """Drop a collection; dropping a missing collection is not an error."""
        try:
            await self._database().drop_collection(table_name)
        except PyMongoError as exc:
            raise SchemaError(
                f"failed to drop collection {table_name!r}",
                code="DROP_COLLECTION_FAILED",
                context={"table": table_name},
            ) from exc

    async def alter_table(self, table_name: str, changes: list[SchemaChange]) -> None:
        """Add or drop indexes, in order; every change is validated first.

        Raises:
            TableNotFoundError: If the collection does not exist.
            IndexNotFoundError: If an index to drop does not exist.
            UnsupportedOperationError: For column changes, which schemaless
                collections do not have.
        """
        # An index to create, or the name of an index to drop.
        steps: list[IndexModel | str] = []
        for change in changes:
            if change.change_type is SchemaChangeType.ADD_INDEX:
                if change.index is None:
                    raise InvalidSchemaError("ADD_INDEX needs an index definition")
                steps.append(index_model(change.index))
            elif change.change_type is SchemaChangeType.DROP_INDEX:
                if change.index is None or not change.index.name:
                    raise InvalidSchemaError("index name is required to drop an index")
                steps.append(change.index.name)
            else:
                operation = f"schema change {change.change_type.value}"
                raise new_unsupported_operation_error(operation)
        await self._require_collection(table_name)

        collection = self._database()[table_name]
        for step in steps:
            if isinstance(step, IndexModel):
                try:
                    await collection.create_indexes([step])
                except PyMongoError as exc:
                    raise SchemaError(
                        f"failed to create an index on {table_name!r}",
                        code="ADD_INDEX_FAILED",
                        context={"table": table_name},
                    ) from exc
                continue
            context = {"table": table_name, "index": step}
            try:
                await collection.drop_index(step)
            except PyMongoError as exc:
                if has_error_code(exc, _INDEX_NOT_FOUND):
                    message = f"index not found: {step}"
                    raise IndexNotFoundError(message, context=context) from exc
                raise SchemaError(
                    f"failed to drop index {step!r}",
                    code="DROP_INDEX_FAILED",
                    context=context,
                ) from exc

    def info(self) -> StorageInfo:
        """Return the adapter's identity, features and limits."""
        return StorageInfo(
            name=ADAPTER_NAME,
            version=ADAPTER_VERSION,
            database_type=DatabaseType.MONGODB,
            features=["transactions", "batch", "schema", "json", "arrays"],
            limits=StorageLimits(
                max_connections=self._config.max_open_conns,
                max_query_size=16 * 1024 * 1024,  # the BSON document size limit
                # The default transactionLifetimeLimitSeconds.
                max_transaction_age=datetime.timedelta(minutes=1),
                max_batch_size=100_000,  # maxWriteBatchSize
            ),
        )

    async def list_tables(self) -> list[str]:
        """Return the collection names, sorted."""
        try:
            return sorted(await self._database().list_collection_names())
        except PyMongoError as exc:
            raise SchemaError(
                "failed to list collections", code="LIST_COLLECTIONS_FAILED"
            ) from exc

    async def describe_table(self, table_name: str) -> TableSchema:
        """Describe a collection's indexes, other than the ``_id`` index, with
        their keys in order. Collections are schemaless: no columns are reported.

        Raises:
            TableNotFoundError: If the collection does not exist.
        """
        await self._require_collection(table_name)
        try:
            information = await self._database()[table_name].index_information()
        except PyMongoError as exc:
            raise SchemaError(
                f"failed to list the indexes of {table_name!r}",
                code="LIST_INDEXES_FAILED",
                context={"table": table_name},
            ) from exc
        indexes = [
            IndexDefinition(
                name=name,
                columns=[key for key, _ in spec["key"]],
                unique=bool(spec.get("unique", False)),
                index_type=(
                    IndexType.HASH
                    if any(kind == HASHED for _, kind in spec["key"])
                    else IndexType.BTREE
                ),
            )
            for name, spec in information.items()
            if name != "_id_"
        ]
        return TableSchema(name=table_name, indexes=indexes)

    async def _require_collection(self, name: str) -> None:
        """Raise ``TableNotFoundError`` unless the collection exists (MongoDB would
        otherwise create it implicitly, or report nothing)."""
        try:
            names = await self._database().list_collection_names(filter={"name": name})
        except PyMongoError as exc:
            raise SchemaError(
                "failed to list collections", code="LIST_COLLECTIONS_FAILED"
            ) from exc
        if not names:
            raise new_table_not_found_error(name)


def index_model(definition: IndexDefinition) -> IndexModel:
    """Translate an index definition; without a name, the server names it (e.g.
    ``email_1``).

    Raises:
        InvalidSchemaError: If the index has no columns.
        UnsupportedOperationError: For index types other than BTREE and HASH, and
            for partial indexes (whose condition is SQL).
    """
    if not definition.columns:
        raise InvalidSchemaError(
            f"index {definition.name!r} must have at least one column",
            context={"index": definition.name},
        )
    key = _INDEX_KEYS.get(definition.index_type)
    if key is None:
        operation = f"index type {definition.index_type.value}"
        raise new_unsupported_operation_error(operation)
    if definition.condition:
        raise new_unsupported_operation_error("partial index conditions")
    for column in definition.columns:
        check_field(column)
    options: dict[str, Any] = {}
    if definition.name:
        options["name"] = definition.name
    if definition.unique:
        options["unique"] = True
    return IndexModel([(column, key) for column in definition.columns], **options)
