"""
MongoDB connection and storage implementation.

This module provides the main storage implementation for MongoDB using Motor
for high-performance async database operations.
"""

from __future__ import annotations

import asyncio
import datetime
import logging
from typing import Any, Dict, List, Optional

from motor.motor_asyncio import AsyncIOMotorClient, AsyncIOMotorDatabase
from pymongo.errors import PyMongoError

from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow
from storage.adapters.mongodb.transaction import MongoDBTransaction
from storage.errors import (
    ConnectionError,
    QueryError,
    SchemaError,
    new_connection_error,
    new_connection_timeout_error,
    new_query_error,
    new_schema_error,
    wrap_error,
)
from storage.interfaces import Storage
from storage.types import (
    Command,
    Config,
    DatabaseType,
    ExecuteResult,
    HealthStatus,
    HealthStatusType,
    Operation,
    OperationResult,
    OperationType,
    Query,
    StorageInfo,
    StorageLimits,
    TableSchema,
    TxOptions,
)

logger = logging.getLogger(__name__)


class MongoDBStorage(Storage):
    """MongoDB storage implementation using Motor."""
    
    def __init__(self, config: Config) -> None:
        """Initialize MongoDB storage.
        
        Args:
            config: Database configuration
        """
        self._config = config
        self._client: Optional[AsyncIOMotorClient] = None
        self._database: Optional[AsyncIOMotorDatabase] = None
        self._closed = False
    
    async def connect(self) -> None:
        """Connect to MongoDB.
        
        Raises:
            ConnectionError: If connection fails
        """
        try:
            # Build connection URI
            uri = self._build_connection_uri()
            
            # Create client with connection options
            self._client = AsyncIOMotorClient(
                uri,
                maxPoolSize=self._config.max_open_conns,
                minPoolSize=self._config.max_idle_conns,
                maxIdleTimeMS=int(self._config.conn_max_idle_time.total_seconds() * 1000) if self._config.conn_max_idle_time else 30000,
                connectTimeoutMS=int(self._config.connect_timeout.total_seconds() * 1000) if self._config.connect_timeout else 10000,
                socketTimeoutMS=int(self._config.query_timeout.total_seconds() * 1000) if self._config.query_timeout else 30000,
                serverSelectionTimeoutMS=10000,
            )
            
            # Get database
            self._database = self._client[self._config.database]
            
            # Test connection
            await self._client.admin.command("ping")
            
            logger.info(f"Connected to MongoDB database: {self._config.database}")
            
        except Exception as e:
            await self._cleanup()
            if isinstance(e, asyncio.TimeoutError):
                raise new_connection_timeout_error("MongoDB connection timeout") from e
            else:
                raise new_connection_error(f"Failed to connect to MongoDB: {e}") from e
    
    async def close(self) -> None:
        """Close the MongoDB connection."""
        if not self._closed:
            await self._cleanup()
            logger.info("MongoDB connection closed")
    
    async def _cleanup(self) -> None:
        """Clean up resources."""
        self._closed = True
        if self._client:
            self._client.close()
            self._client = None
            self._database = None
    
    async def ping(self) -> None:
        """Test the MongoDB connection.
        
        Raises:
            ConnectionError: If ping fails
        """
        if self._closed or not self._client:
            raise new_connection_error("MongoDB connection is closed")
        
        try:
            await self._client.admin.command("ping")
        except Exception as e:
            raise new_connection_error(f"MongoDB ping failed: {e}") from e
    
    async def health(self) -> HealthStatus:
        """Get the health status of MongoDB.
        
        Returns:
            Health status information
        """
        try:
            # Test connection
            await self.ping()
            
            # Get server status
            server_status = await self._database.command("serverStatus")
            
            return HealthStatus(
                status=HealthStatusType.HEALTHY,
                message="MongoDB is healthy",
                timestamp=datetime.datetime.now(),
                details={
                    "host": server_status.get("host", "unknown"),
                    "version": server_status.get("version", "unknown"),
                    "uptime_seconds": server_status.get("uptime", 0),
                    "connections": server_status.get("connections", {}),
                }
            )
            
        except Exception as e:
            return HealthStatus(
                status=HealthStatusType.UNHEALTHY,
                message=f"MongoDB health check failed: {e}",
                timestamp=datetime.datetime.now(),
                details={"error": str(e)}
            )
    
    async def query(self, query: Query) -> MongoDBResult:
        """Execute a query and return results.
        
        Args:
            query: Query to execute
            
        Returns:
            Query results
            
        Raises:
            QueryError: If query execution fails
        """
        if self._closed or not self._database:
            raise new_connection_error("MongoDB connection is closed")
        
        try:
            # Extract collection name and build filter
            collection_name = self._extract_collection_name(query.sql)
            if not collection_name:
                raise new_query_error("Collection name not found in query")
            
            collection = self._database[collection_name]
            filter_doc = self._build_filter(query.parameters)
            
            # Execute find operation
            cursor = collection.find(filter_doc)
            
            return MongoDBResult(cursor)
            
        except Exception as e:
            if isinstance(e, QueryError):
                raise
            raise new_query_error(f"MongoDB query failed: {e}") from e
    
    async def query_one(self, query: Query) -> MongoDBRow:
        """Execute a query and return a single row.
        
        Args:
            query: Query to execute
            
        Returns:
            Single query result
            
        Raises:
            QueryError: If query execution fails
        """
        if self._closed or not self._database:
            raise new_connection_error("MongoDB connection is closed")
        
        try:
            # Extract collection name and build filter
            collection_name = self._extract_collection_name(query.sql)
            if not collection_name:
                raise new_query_error("Collection name not found in query")
            
            collection = self._database[collection_name]
            filter_doc = self._build_filter(query.parameters)
            
            # Execute findOne operation
            document = await collection.find_one(filter_doc)
            if document is None:
                raise new_query_error("No document found")
            
            return MongoDBRow(document)
            
        except Exception as e:
            if isinstance(e, QueryError):
                raise
            raise new_query_error(f"MongoDB query_one failed: {e}") from e
    
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command and return the result.
        
        Args:
            command: Command to execute
            
        Returns:
            Execution result
            
        Raises:
            QueryError: If command execution fails
        """
        if self._closed or not self._database:
            raise new_connection_error("MongoDB connection is closed")
        
        try:
            # Route to appropriate execution method based on command type
            if command.type.value == "insert":
                return await self._execute_insert(command)
            elif command.type.value == "update":
                return await self._execute_update(command)
            elif command.type.value == "delete":
                return await self._execute_delete(command)
            else:
                raise new_query_error(f"Unsupported command type: {command.type}")
                
        except Exception as e:
            if isinstance(e, QueryError):
                raise
            raise new_query_error(f"MongoDB command execution failed: {e}") from e
    
    async def begin_tx(self, options: Optional[TxOptions] = None) -> MongoDBTransaction:
        """Begin a new transaction.
        
        Args:
            options: Transaction options
            
        Returns:
            Transaction instance
            
        Raises:
            TransactionError: If transaction creation fails
        """
        if self._closed or not self._client:
            raise new_connection_error("MongoDB connection is closed")
        
        return await MongoDBTransaction.create(self._client, self._database, options)
    
    async def batch(self, operations: List[Operation]) -> List[OperationResult]:
        """Execute multiple operations in a batch.
        
        Args:
            operations: List of operations to execute
            
        Returns:
            List of operation results
            
        Raises:
            QueryError: If batch execution fails
        """
        if self._closed or not self._database:
            raise new_connection_error("MongoDB connection is closed")
        
        results = []
        
        # Group operations by collection for bulk operations
        collection_ops: Dict[str, List[tuple[int, Operation]]] = {}
        
        for i, op in enumerate(operations):
            collection_name = self._extract_collection_name(op.query.sql)
            if not collection_name:
                results.append(OperationResult(
                    success=False,
                    error=new_query_error("Collection name not found in operation")
                ))
                continue
            
            if collection_name not in collection_ops:
                collection_ops[collection_name] = []
            collection_ops[collection_name].append((i, op))
        
        # Initialize results list
        results = [OperationResult(success=False) for _ in operations]
        
        # Execute bulk operations for each collection
        for collection_name, ops in collection_ops.items():
            try:
                collection = self._database[collection_name]
                bulk_ops = []
                
                for _, op in ops:
                    if op.type == OperationType.INSERT:
                        doc = self._build_document(op.query.parameters)
                        bulk_ops.append({"insertOne": {"document": doc}})
                    elif op.type == OperationType.UPDATE:
                        mid = len(op.query.parameters) // 2
                        filter_doc = self._build_filter(op.query.parameters[:mid])
                        update_doc = {"$set": self._build_document(op.query.parameters[mid:])}
                        bulk_ops.append({"updateOne": {"filter": filter_doc, "update": update_doc}})
                    elif op.type == OperationType.DELETE:
                        filter_doc = self._build_filter(op.query.parameters)
                        bulk_ops.append({"deleteOne": {"filter": filter_doc}})
                
                if bulk_ops:
                    await collection.bulk_write(bulk_ops)
                    
                    # Mark all operations for this collection as successful
                    for idx, _ in ops:
                        results[idx] = OperationResult(success=True)
                        
            except Exception as e:
                # Mark all operations for this collection as failed
                for idx, _ in ops:
                    results[idx] = OperationResult(
                        success=False,
                        error=new_query_error(f"Bulk operation failed: {e}")
                    )
        
        return results
    
    def info(self) -> StorageInfo:
        """Get storage information.
        
        Returns:
            Storage information
        """
        return StorageInfo(
            name="mongodb",
            version="1.0.0",
            database_type=DatabaseType.MONGODB,
            features=[
                "transactions",
                "aggregation",
                "batch",
                "schema",
                "json",
                "arrays",
                "full-text-search",
                "geospatial",
                "gridfs",
            ],
            limits=StorageLimits(
                max_connections=self._config.max_open_conns,
                max_query_size=16 * 1024 * 1024,  # 16MB BSON document limit
                max_transaction_age=datetime.timedelta(minutes=1),  # MongoDB transaction timeout
                max_batch_size=100000,  # MongoDB bulk write limit
            )
        )
    
    # Helper methods
    
    def _build_connection_uri(self) -> str:
        """Build MongoDB connection URI from config."""
        if self._config.dsn:
            return self._config.dsn
        
        # Build URI from components
        uri_parts = ["mongodb://"]
        
        # Add authentication
        if self._config.username:
            uri_parts.append(self._config.username)
            if self._config.password:
                uri_parts.append(f":{self._config.password}")
            uri_parts.append("@")
        
        # Add host and port
        uri_parts.append(self._config.host)
        if self._config.port and self._config.port != 27017:
            uri_parts.append(f":{self._config.port}")
        
        # Add database
        if self._config.database:
            uri_parts.append(f"/{self._config.database}")
        
        # Add options
        if self._config.options:
            query_params = []
            for key, value in self._config.options.items():
                query_params.append(f"{key}={value}")
            if query_params:
                uri_parts.append(f"?{'&'.join(query_params)}")
        
        return "".join(uri_parts)
    
    def _extract_collection_name(self, sql: str) -> str:
        """Extract collection name from query SQL.
        
        This is a simplified implementation - in practice, you'd have a proper parser.
        """
        if not sql:
            return ""
        
        # For now, assume the collection name is passed in a specific format
        # In a real implementation, you'd parse SQL-like syntax or use MongoDB-specific format
        return "default_collection"
    
    def _build_filter(self, params: List[Any]) -> Dict[str, Any]:
        """Build MongoDB filter document from parameters."""
        filter_doc = {}
        
        # Simple parameter mapping - in practice, you'd have sophisticated query building
        for i in range(0, len(params), 2):
            if i + 1 < len(params):
                key = params[i]
                value = params[i + 1]
                if isinstance(key, str):
                    filter_doc[key] = value
        
        return filter_doc
    
    def _build_document(self, params: List[Any]) -> Dict[str, Any]:
        """Build MongoDB document from parameters."""
        return self._build_filter(params)
    
    async def _execute_insert(self, command: Command) -> ExecuteResult:
        """Execute an insert command."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        document = self._build_document(command.parameters)
        
        result = await collection.insert_one(document)
        
        return ExecuteResult(
            rows_affected=1,
            last_insert_id=str(result.inserted_id) if result.inserted_id else None
        )
    
    async def _execute_update(self, command: Command) -> ExecuteResult:
        """Execute an update command."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        
        # Split parameters into filter and update parts
        mid = len(command.parameters) // 2
        filter_doc = self._build_filter(command.parameters[:mid])
        update_doc = {"$set": self._build_document(command.parameters[mid:])}
        
        result = await collection.update_many(filter_doc, update_doc)
        
        return ExecuteResult(
            rows_affected=result.modified_count,
            last_insert_id=None
        )
    
    async def _execute_delete(self, command: Command) -> ExecuteResult:
        """Execute a delete command."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        filter_doc = self._build_filter(command.parameters)
        
        result = await collection.delete_many(filter_doc)
        
        return ExecuteResult(
            rows_affected=result.deleted_count,
            last_insert_id=None
        )
