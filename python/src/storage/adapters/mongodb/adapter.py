"""
MongoDB adapter implementation.

This module provides the main adapter class for MongoDB database connections
using Motor for high-performance async operations.
"""

from __future__ import annotations

import re
from typing import Any, List
from urllib.parse import parse_qs, urlparse

from storage.adapters.mongodb.connection import MongoDBStorage
from storage.errors import ValidationError, new_validation_error
from storage.interfaces import Adapter, Storage
from storage.types import (
    Command,
    Config,
    DataType,
    DatabaseType,
    Query,
)


class MongoDBAdapter(Adapter):
    """MongoDB database adapter using Motor."""
    
    def name(self) -> str:
        """Get adapter name."""
        return "mongodb"
    
    def version(self) -> str:
        """Get adapter version."""
        return "1.0.0"
    
    def database_type(self) -> str:
        """Get database type."""
        return DatabaseType.MONGODB.value
    
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
        storage = MongoDBStorage(config)
        await storage.connect()
        return storage
    
    def parse_dsn(self, dsn: str) -> Config:
        """Parse a MongoDB connection string.
        
        Args:
            dsn: MongoDB connection string
            
        Returns:
            Parsed configuration
            
        Raises:
            ValidationError: If DSN is invalid
        """
        try:
            parsed = urlparse(dsn)
            
            config = Config(
                dsn=dsn,
                host=parsed.hostname or "localhost",
                port=parsed.port or 27017,
                database=parsed.path.lstrip("/") if parsed.path else "",
                username=parsed.username or "",
                password=parsed.password or "",
            )
            
            # Parse query parameters
            if parsed.query:
                params = parse_qs(parsed.query)
                options = {}
                
                # Common MongoDB connection options
                for key, values in params.items():
                    if values:
                        value = values[0]
                        if key in ["authSource", "replicaSet", "ssl", "tls"]:
                            options[key] = value
                        elif key in ["maxPoolSize", "minPoolSize", "maxIdleTimeMS", "connectTimeoutMS", "socketTimeoutMS"]:
                            try:
                                options[key] = int(value)
                            except ValueError:
                                pass
                
                if options:
                    config.options = options
            
            return config
            
        except Exception as e:
            raise new_validation_error(f"Invalid MongoDB DSN: {e}") from e
    
    def validate_config(self, config: Config) -> None:
        """Validate the configuration.
        
        Args:
            config: Configuration to validate
            
        Raises:
            ValidationError: If configuration is invalid
        """
        if not config.dsn:
            if not config.host:
                raise new_validation_error("host is required")
            if not config.database:
                raise new_validation_error("database is required")
        
        if config.max_open_conns <= 0:
            raise new_validation_error("max_open_conns must be positive")
        
        if config.max_idle_conns < 0:
            raise new_validation_error("max_idle_conns cannot be negative")
        
        if config.max_idle_conns > config.max_open_conns:
            raise new_validation_error("max_idle_conns cannot exceed max_open_conns")
    
    def translate_query(self, query: Query) -> tuple[str, List[Any]]:
        """Translate a query to MongoDB-specific operations.
        
        Args:
            query: Query to translate
            
        Returns:
            Tuple of (operation, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        # MongoDB doesn't use SQL, so we return the query as-is for now
        # In a full implementation, this would translate SQL-like queries to MongoDB operations
        return query.sql, query.parameters
    
    def translate_command(self, command: Command) -> tuple[str, List[Any]]:
        """Translate a command to MongoDB-specific operations.
        
        Args:
            command: Command to translate
            
        Returns:
            Tuple of (operation, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        # MongoDB doesn't use SQL, so we return the command as-is for now
        # In a full implementation, this would translate SQL-like commands to MongoDB operations
        return command.sql, command.parameters
    
    def map_go_type(self, go_type: str) -> DataType:
        """Map Go types to MongoDB data types.
        
        Args:
            go_type: Go type name
            
        Returns:
            Corresponding data type
            
        Raises:
            ValidationError: If type mapping fails
        """
        type_mapping = {
            "string": DataType.STRING,
            "int": DataType.INTEGER,
            "int8": DataType.INTEGER,
            "int16": DataType.INTEGER,
            "int32": DataType.INTEGER,
            "int64": DataType.INTEGER,
            "uint": DataType.INTEGER,
            "uint8": DataType.INTEGER,
            "uint16": DataType.INTEGER,
            "uint32": DataType.INTEGER,
            "uint64": DataType.INTEGER,
            "float32": DataType.FLOAT,
            "float64": DataType.FLOAT,
            "bool": DataType.BOOLEAN,
            "time.Time": DataType.DATETIME,
            "[]byte": DataType.BINARY,
            "map[string]interface{}": DataType.JSON,
            "[]interface{}": DataType.ARRAY,
        }
        
        if go_type in type_mapping:
            return type_mapping[go_type]
        
        raise new_validation_error(f"Unsupported Go type: {go_type}")
    
    def map_database_type(self, db_type: str) -> DataType:
        """Map MongoDB BSON types to domain data types.
        
        Args:
            db_type: MongoDB BSON type name
            
        Returns:
            Corresponding data type
            
        Raises:
            ValidationError: If type mapping fails
        """
        type_mapping = {
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
            "objectid": DataType.STRING,  # ObjectID as string
        }
        
        normalized_type = db_type.lower()
        if normalized_type in type_mapping:
            return type_mapping[normalized_type]
        
        raise new_validation_error(f"Unsupported MongoDB type: {db_type}")
    
    def supports_transactions(self) -> bool:
        """Check if MongoDB supports transactions."""
        return True  # MongoDB 4.0+ supports multi-document transactions
    
    def supports_joins(self) -> bool:
        """Check if MongoDB supports joins."""
        return True  # MongoDB supports $lookup aggregation for joins
    
    def supports_batch(self) -> bool:
        """Check if MongoDB supports batch operations."""
        return True
    
    def supports_schema(self) -> bool:
        """Check if MongoDB supports schema operations."""
        return True  # MongoDB supports collections and indexes
    
    def _build_connection_uri(self, config: Config) -> str:
        """Build MongoDB connection URI from config.
        
        Args:
            config: Database configuration
            
        Returns:
            MongoDB connection URI
        """
        if config.dsn:
            return config.dsn
        
        # Build URI from components
        uri_parts = ["mongodb://"]
        
        # Add authentication
        if config.username:
            uri_parts.append(config.username)
            if config.password:
                uri_parts.append(f":{config.password}")
            uri_parts.append("@")
        
        # Add host and port
        uri_parts.append(config.host)
        if config.port and config.port != 27017:
            uri_parts.append(f":{config.port}")
        
        # Add database
        if config.database:
            uri_parts.append(f"/{config.database}")
        
        # Add options
        if config.options:
            query_params = []
            for key, value in config.options.items():
                query_params.append(f"{key}={value}")
            if query_params:
                uri_parts.append(f"?{'&'.join(query_params)}")
        
        return "".join(uri_parts)
