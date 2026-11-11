"""
PostgreSQL adapter: the factory of ``PostgreSQLStorage`` and the PostgreSQL
specifics of the storage contract (DSN format, placeholders, type mapping).
"""

from __future__ import annotations

from typing import Any

from storage.adapters.postgresql.connection import VERSION, PostgreSQLStorage
from storage.adapters.postgresql.dsn import parse_dsn as _parse_dsn
from storage.adapters.postgresql.dsn import validate_config as _validate_config
from storage.adapters.postgresql.executor import translate_sql
from storage.adapters.postgresql.schema import map_database_type as _map_database_type
from storage.adapters.postgresql.schema import map_python_type as _map_python_type
from storage.interfaces import Adapter
from storage.types import Command, Config, DatabaseType, DataType, Query


class PostgreSQLAdapter(Adapter):
    """PostgreSQL database adapter using asyncpg."""

    def name(self) -> str:
        """Return the adapter name."""
        return "postgresql"

    def version(self) -> str:
        """Return the adapter version."""
        return VERSION

    def database_type(self) -> DatabaseType:
        """Return ``DatabaseType.POSTGRESQL``."""
        return DatabaseType.POSTGRESQL

    async def connect(self, config: Config) -> PostgreSQLStorage:
        """Validate ``config``, open a connection pool and check that the
        database answers (see ``PostgreSQLStorage`` for the settings).

        Raises:
            InvalidConfigError: If the configuration is invalid
                (``InvalidDSNError`` for its DSN).
            ConnectionFailedError: If the database cannot be reached.
            ConnectionTimeoutError: If connecting timed out.
        """
        self.validate_config(config)
        storage = PostgreSQLStorage(config)
        await storage.connect()
        return storage

    def parse_dsn(self, dsn: str) -> Config:
        """Parse a URL or ``key=value`` DSN into a ``Config`` (see
        ``storage.adapters.postgresql.dsn.parse_dsn``).

        Raises:
            InvalidDSNError: If the DSN is invalid; the message never quotes it.
        """
        return _parse_dsn(dsn)

    def validate_config(self, config: Config) -> None:
        """Validate a configuration.

        Raises:
            InvalidConfigError: If a setting is invalid (``InvalidDSNError`` for
                the DSN).
        """
        _validate_config(config)

    def translate_query(self, query: Query) -> tuple[str, list[Any]]:
        """Return the SQL with ``?`` placeholders rewritten to ``$n`` (see
        ``storage.adapters.postgresql.sql.convert_placeholders``) and the parameters.

        Raises:
            InvalidQueryError: If the SQL is empty.
        """
        return translate_sql(query.sql), list(query.parameters)

    def translate_command(self, command: Command) -> tuple[str, list[Any]]:
        """Return the command's SQL and parameters; see ``translate_query``."""
        return translate_sql(command.sql), list(command.parameters)

    def map_python_type(self, value: Any) -> DataType:
        """Return the data type that stores a Python value such as ``value``.

        Raises:
            ValidationError: If the type cannot be mapped.
        """
        return _map_python_type(value)

    def map_database_type(self, db_type: str) -> DataType:
        """Return the data type of a PostgreSQL type name.

        Raises:
            ValidationError: If the type cannot be mapped.
        """
        return _map_database_type(db_type)

    def supports_transactions(self) -> bool:
        """PostgreSQL supports transactions (and savepoints)."""
        return True

    def supports_joins(self) -> bool:
        """PostgreSQL supports joins."""
        return True

    def supports_batch(self) -> bool:
        """PostgreSQL supports batches."""
        return True

    def supports_schema(self) -> bool:
        """PostgreSQL supports schema operations."""
        return True
