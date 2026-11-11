"""
MongoDB adapter: configuration, query translation and type mapping.
"""

from __future__ import annotations

import datetime
from typing import Any
from urllib.parse import parse_qsl

from storage.adapters.mongodb.connection import (
    ADAPTER_NAME,
    ADAPTER_VERSION,
    MongoDBStorage,
    client_settings,
    parse_uri,
)
from storage.adapters.mongodb.translate import (
    plan_command,
    plan_query,
    render_command,
    render_query,
)
from storage.errors import new_validation_error
from storage.interfaces import Adapter
from storage.types import Command, Config, DatabaseType, DataType, Query

# MongoDB type names (the $type aliases, lowercased, plus a few common synonyms).
_DATABASE_TYPES = {
    "string": DataType.STRING,
    "int": DataType.INTEGER,
    "long": DataType.INTEGER,
    "int32": DataType.INTEGER,
    "int64": DataType.INTEGER,
    "double": DataType.FLOAT,
    "decimal": DataType.DECIMAL,
    "bool": DataType.BOOLEAN,
    "boolean": DataType.BOOLEAN,
    "date": DataType.DATETIME,
    "timestamp": DataType.DATETIME,
    "bindata": DataType.BINARY,
    "binary": DataType.BINARY,
    "object": DataType.JSON,
    "document": DataType.JSON,
    "array": DataType.ARRAY,
    "objectid": DataType.STRING,  # ObjectId as string
}

# Checked in order: bool is an int subclass.
_PYTHON_TYPES: tuple[tuple[type | tuple[type, ...], DataType], ...] = (
    (bool, DataType.BOOLEAN),
    (int, DataType.INTEGER),
    (float, DataType.FLOAT),
    (str, DataType.STRING),
    (datetime.datetime, DataType.DATETIME),
    ((bytes, bytearray), DataType.BINARY),
    ((dict, list), DataType.JSON),
)


class MongoDBAdapter(Adapter):
    """Adapter for MongoDB 4.4+; transactions need a replica set or a sharded
    cluster."""

    def name(self) -> str:
        return ADAPTER_NAME

    def version(self) -> str:
        return ADAPTER_VERSION

    def database_type(self) -> DatabaseType:
        return DatabaseType.MONGODB

    async def connect(self, config: Config) -> MongoDBStorage:
        """Validate ``config`` and return a connected storage.

        The connection URI is ``config.dsn``, else built from the host, port
        (default 27017), credentials and options; the database is
        ``config.database``, else the one named in the DSN.

        Raises:
            ValidationError: If the configuration is invalid.
            ConnectionTimeoutError: If the server cannot be reached within
                ``Config.connect_timeout``.
            ConnectionFailedError: If the connection fails otherwise.
        """
        storage = MongoDBStorage(config)
        await storage.connect()  # validates config first
        return storage

    def parse_dsn(self, dsn: str) -> Config:
        """Parse a MongoDB URI; the DSN is kept for ``connect``.

        ``host`` and ``port`` are those of the first host (for ``mongodb+srv://``,
        the SRV name and no port), and ``options`` holds the URI's options as
        written.

        Raises:
            InvalidDSNError: If the DSN is not a valid MongoDB URI.
        """
        parsed = parse_uri(dsn)
        if parsed["fqdn"]:
            host, port = parsed["fqdn"], None
        else:
            host, port = parsed["nodelist"][0]
        query = dsn.partition("?")[2]
        separator = ";" if ";" in query and "&" not in query else "&"
        options: dict[str, Any] = {}
        for key, value in parse_qsl(query, keep_blank_values=True, separator=separator):
            options.setdefault(key, value)
        return Config(
            host=host,
            port=port,
            database=parsed["database"] or "",
            username=parsed["username"] or "",
            password=parsed["password"] or "",
            dsn=dsn,
            options=options,
        )

    def validate_config(self, config: Config) -> None:
        """Validate a configuration, including its URI.

        Raises:
            InvalidDSNError: If ``config.dsn`` is invalid.
            InvalidConfigError: If the configuration is otherwise invalid.
        """
        client_settings(config)

    def translate_query(self, query: Query) -> tuple[str, list[Any]]:
        """Render the MongoDB operation a builder-produced query runs as (for
        logging and debugging); values are inlined, so no parameters are
        returned.

        Raises:
            UnsupportedQueryError: For raw SQL and SQL-only features.
            InvalidQueryError: If the query is malformed.
        """
        return render_query(plan_query(query)), []

    def translate_command(self, command: Command) -> tuple[str, list[Any]]:
        """Render the MongoDB operation a builder-produced command runs as (see
        ``translate_query``)."""
        return render_command(plan_command(command)), []

    def map_python_type(self, value: Any) -> DataType:
        """Return the data type that stores a Python value such as ``value``.

        Raises:
            ValidationError: If the value's type is not supported.
        """
        for python_type, data_type in _PYTHON_TYPES:
            if isinstance(value, python_type):
                return data_type
        raise new_validation_error(f"unsupported Python type: {type(value).__name__}")

    def map_database_type(self, db_type: str) -> DataType:
        """Return the data type of a MongoDB type name (case-insensitive).

        Raises:
            ValidationError: If the type name is not supported.
        """
        data_type = _DATABASE_TYPES.get(db_type.lower())
        if data_type is None:
            raise new_validation_error(f"unsupported MongoDB type: {db_type}")
        return data_type

    def supports_transactions(self) -> bool:
        return True

    def supports_joins(self) -> bool:
        """False: queries with joins raise ``UnsupportedQueryError``."""
        return False

    def supports_batch(self) -> bool:
        return True

    def supports_schema(self) -> bool:
        return True
