"""
PostgreSQL adapter implementation.

This module provides the main adapter class for PostgreSQL database connections
using asyncpg for high-performance async operations.
"""

from __future__ import annotations

import re
from typing import Any, List
from urllib.parse import parse_qs, urlparse

from storage.adapters.postgresql.connection import PostgreSQLStorage
from storage.errors import ValidationError, new_validation_error
from storage.interfaces import Adapter, Storage
from storage.types import (
    Command,
    Config,
    DataType,
    DatabaseType,
    Query,
)


class PostgreSQLAdapter(Adapter):
    """PostgreSQL database adapter using asyncpg."""
    
    def name(self) -> str:
        """Get adapter name."""
        return "postgresql"
    
    def version(self) -> str:
        """Get adapter version."""
        return "1.0.0"
    
    def database_type(self) -> str:
        """Get database type."""
        return DatabaseType.POSTGRESQL.value
    
    async def connect(self, config: Config) -> Storage:
        """Create a storage connection.
        
        Args:
            config: Connection configuration
            
        Returns:
            Storage instance
            
        Raises:
            ConnectionError: If connection fails
        """
        self.validate_config(config)
        storage = PostgreSQLStorage(config)
        await storage.connect()
        return storage
    
    def parse_dsn(self, dsn: str) -> Config:
        """Parse a DSN into configuration.
        
        Args:
            dsn: Data source name
            
        Returns:
            Parsed configuration
            
        Raises:
            ValidationError: If DSN is invalid
        """
        if not dsn:
            raise new_validation_error("DSN cannot be empty")
        
        # Handle postgres:// and postgresql:// URLs
        if dsn.startswith(("postgres://", "postgresql://")):
            return self._parse_url_dsn(dsn)
        
        # Handle key=value format
        if "=" in dsn:
            return self._parse_key_value_dsn(dsn)
        
        raise new_validation_error(f"Invalid DSN format: {dsn}")
    
    def validate_config(self, config: Config) -> None:
        """Validate configuration.
        
        Args:
            config: Configuration to validate
            
        Raises:
            ValidationError: If configuration is invalid
        """
        if not config.dsn:
            if not config.host:
                raise new_validation_error("Host is required when DSN is not provided")
            if not config.database:
                raise new_validation_error("Database is required when DSN is not provided")
            if not config.username:
                raise new_validation_error("Username is required when DSN is not provided")
        
        if config.max_open_conns <= 0:
            raise new_validation_error("max_open_conns must be positive")
        
        if config.max_idle_conns < 0:
            raise new_validation_error("max_idle_conns cannot be negative")
        
        if config.max_idle_conns > config.max_open_conns:
            raise new_validation_error("max_idle_conns cannot exceed max_open_conns")
    
    def translate_query(self, query: Query) -> tuple[str, List[Any]]:
        """Translate a query to PostgreSQL-specific SQL.
        
        Args:
            query: Query to translate
            
        Returns:
            Tuple of (SQL, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        # Convert ? placeholders to $n format for PostgreSQL
        sql = self._convert_placeholders(query.sql)
        return sql, query.parameters
    
    def translate_command(self, command: Command) -> tuple[str, List[Any]]:
        """Translate a command to PostgreSQL-specific SQL.
        
        Args:
            command: Command to translate
            
        Returns:
            Tuple of (SQL, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        # Convert ? placeholders to $n format for PostgreSQL
        sql = self._convert_placeholders(command.sql)
        return sql, command.parameters
    
    def map_go_type(self, value: Any) -> DataType:
        """Map a Python type to a storage data type.
        
        Args:
            value: Python value
            
        Returns:
            Storage data type
            
        Raises:
            ValidationError: If type cannot be mapped
        """
        if isinstance(value, str):
            return DataType.STRING
        elif isinstance(value, (int, bool)):  # bool is subclass of int in Python
            if isinstance(value, bool):
                return DataType.BOOLEAN
            return DataType.INTEGER
        elif isinstance(value, float):
            return DataType.FLOAT
        elif isinstance(value, bytes):
            return DataType.BINARY
        elif isinstance(value, (list, tuple)):
            return DataType.ARRAY
        elif isinstance(value, dict):
            return DataType.JSON
        elif value is None:
            return DataType.UNKNOWN
        else:
            raise new_validation_error(f"Unsupported Python type: {type(value)}")
    
    def map_database_type(self, db_type: str) -> DataType:
        """Map a PostgreSQL type to a storage data type.
        
        Args:
            db_type: PostgreSQL type name
            
        Returns:
            Storage data type
            
        Raises:
            ValidationError: If type cannot be mapped
        """
        db_type = db_type.lower()
        
        # String types
        if db_type in ("text", "varchar", "char", "character", "character varying"):
            return DataType.STRING
        
        # Integer types
        if db_type in ("integer", "int", "int4", "bigint", "int8", "smallint", "int2"):
            return DataType.INTEGER
        
        # Float types
        if db_type in ("real", "float4", "double precision", "float8", "numeric", "decimal"):
            return DataType.FLOAT
        
        # Boolean type
        if db_type in ("boolean", "bool"):
            return DataType.BOOLEAN
        
        # Date/time types
        if db_type in ("timestamp", "timestamptz", "timestamp with time zone", 
                      "timestamp without time zone"):
            return DataType.DATETIME
        if db_type == "date":
            return DataType.DATE
        if db_type in ("time", "timetz", "time with time zone", "time without time zone"):
            return DataType.TIME
        
        # Binary type
        if db_type == "bytea":
            return DataType.BINARY
        
        # JSON types
        if db_type in ("json", "jsonb"):
            return DataType.JSON
        
        # UUID type
        if db_type == "uuid":
            return DataType.UUID
        
        # Array types
        if db_type.endswith("[]") or "array" in db_type:
            return DataType.ARRAY
        
        # Default to unknown
        raise new_validation_error(f"Unsupported PostgreSQL type: {db_type}")
    
    def supports_transactions(self) -> bool:
        """Check if adapter supports transactions."""
        return True
    
    def supports_joins(self) -> bool:
        """Check if adapter supports joins."""
        return True
    
    def supports_batch(self) -> bool:
        """Check if adapter supports batch operations."""
        return True
    
    def supports_schema(self) -> bool:
        """Check if adapter supports schema operations."""
        return True
    
    def _parse_url_dsn(self, dsn: str) -> Config:
        """Parse a URL-style DSN."""
        try:
            parsed = urlparse(dsn)
            
            config = Config()
            config.dsn = dsn
            config.host = parsed.hostname or "localhost"
            config.port = parsed.port or 5432
            config.database = parsed.path.lstrip("/") if parsed.path else ""
            config.username = parsed.username or ""
            config.password = parsed.password or ""
            
            # Parse query parameters
            if parsed.query:
                params = parse_qs(parsed.query)
                for key, values in params.items():
                    if values:
                        value = values[0]
                        if key == "sslmode":
                            config.ssl_mode = value
                        else:
                            config.options[key] = value
            
            return config
            
        except Exception as e:
            raise new_validation_error(f"Invalid URL DSN: {e}") from e
    
    def _parse_key_value_dsn(self, dsn: str) -> Config:
        """Parse a key=value style DSN."""
        config = Config()
        config.dsn = dsn
        
        # Simple key=value parser
        pairs = dsn.split()
        for pair in pairs:
            if "=" in pair:
                key, value = pair.split("=", 1)
                key = key.strip()
                value = value.strip()
                
                if key == "host":
                    config.host = value
                elif key == "port":
                    config.port = int(value)
                elif key == "dbname":
                    config.database = value
                elif key == "user":
                    config.username = value
                elif key == "password":
                    config.password = value
                elif key == "sslmode":
                    config.ssl_mode = value
                else:
                    config.options[key] = value
        
        return config
    
    def _convert_placeholders(self, sql: str) -> str:
        """Convert ? placeholders to $n format for PostgreSQL.
        
        Args:
            sql: SQL with ? placeholders
            
        Returns:
            SQL with $n placeholders
        """
        param_index = 1
        result = []
        
        for char in sql:
            if char == "?":
                result.append(f"${param_index}")
                param_index += 1
            else:
                result.append(char)
        
        return "".join(result)
