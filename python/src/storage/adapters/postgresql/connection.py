"""
PostgreSQL connection and storage implementation.

This module provides the main storage implementation for PostgreSQL using asyncpg
for high-performance async database operations.
"""

from __future__ import annotations

import asyncio
import datetime
import logging
from typing import Any, Dict, List, Optional

import asyncpg

from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.adapters.postgresql.transaction import PostgreSQLTransaction
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


class PostgreSQLStorage(Storage):
    """PostgreSQL storage implementation using asyncpg."""
    
    def __init__(self, config: Config) -> None:
        """Initialize PostgreSQL storage.
        
        Args:
            config: Database configuration
        """
        self._config = config
        self._pool: Optional[asyncpg.Pool] = None
        self._closed = False
    
    async def connect(self) -> None:
        """Establish connection to the database.
        
        Raises:
            ConnectionError: If connection fails
        """
        try:
            # Build connection parameters
            connect_kwargs = self._build_connect_kwargs()
            
            # Create connection pool
            self._pool = await asyncpg.create_pool(
                min_size=self._config.max_idle_conns,
                max_size=self._config.max_open_conns,
                max_inactive_connection_lifetime=self._config.conn_max_idle_time.total_seconds(),
                **connect_kwargs
            )
            
            logger.info("Connected to PostgreSQL database")
            
        except asyncio.TimeoutError as e:
            raise new_connection_timeout_error("Connection timeout") from e
        except Exception as e:
            raise new_connection_error("CONNECTION_FAILED", f"Failed to connect: {e}") from e
    
    async def close(self) -> None:
        """Close the database connection and cleanup resources."""
        if self._pool and not self._closed:
            await self._pool.close()
            self._closed = True
            logger.info("Closed PostgreSQL connection pool")
    
    async def ping(self) -> None:
        """Test the database connection.
        
        Raises:
            ConnectionError: If ping fails
        """
        if not self._pool or self._closed:
            raise new_connection_error("CONNECTION_CLOSED", "Connection is closed")
        
        try:
            async with self._pool.acquire() as conn:
                await conn.fetchval("SELECT 1")
        except Exception as e:
            raise new_connection_error("PING_FAILED", f"Ping failed: {e}") from e
    
    async def query(self, query: Query) -> PostgreSQLResult:
        """Execute a query.
        
        Args:
            query: The query to execute
            
        Returns:
            Query results
            
        Raises:
            QueryError: If query execution fails
        """
        if not self._pool or self._closed:
            raise new_query_error("CONNECTION_CLOSED", "Connection is closed")
        
        try:
            async with self._pool.acquire() as conn:
                # Convert ? placeholders to $n format
                sql = self._convert_placeholders(query.sql)
                
                # Execute query with timeout
                if query.timeout:
                    result = await asyncio.wait_for(
                        conn.fetch(sql, *query.parameters),
                        timeout=query.timeout.total_seconds()
                    )
                else:
                    result = await conn.fetch(sql, *query.parameters)
                
                return PostgreSQLResult(result, conn)
                
        except asyncio.TimeoutError as e:
            raise new_query_error("QUERY_TIMEOUT", f"Query timeout: {query.sql[:100]}...") from e
        except Exception as e:
            raise new_query_error("QUERY_FAILED", f"Query failed: {e}") from e
    
    async def query_one(self, query: Query) -> Optional[PostgreSQLRow]:
        """Execute a query and return a single row.
        
        Args:
            query: The query to execute
            
        Returns:
            Single row or None
            
        Raises:
            QueryError: If query execution fails
        """
        result = await self.query(query)
        return await result.fetchone()
    
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command.
        
        Args:
            command: The command to execute
            
        Returns:
            Execution result
            
        Raises:
            QueryError: If command execution fails
        """
        if not self._pool or self._closed:
            raise new_query_error("CONNECTION_CLOSED", "Connection is closed")
        
        try:
            async with self._pool.acquire() as conn:
                # Convert ? placeholders to $n format
                sql = self._convert_placeholders(command.sql)
                
                # Execute command with timeout
                if command.timeout:
                    result = await asyncio.wait_for(
                        conn.execute(sql, *command.parameters),
                        timeout=command.timeout.total_seconds()
                    )
                else:
                    result = await conn.execute(sql, *command.parameters)
                
                # Parse result to get rows affected
                rows_affected = self._parse_execute_result(result)
                
                return ExecuteResult(rows_affected=rows_affected)
                
        except asyncio.TimeoutError as e:
            raise new_query_error("COMMAND_TIMEOUT", f"Command timeout: {command.sql[:100]}...") from e
        except Exception as e:
            raise new_query_error("COMMAND_FAILED", f"Command failed: {e}") from e
    
    async def begin_tx(self, options: Optional[TxOptions] = None) -> PostgreSQLTransaction:
        """Begin a new transaction.

        Args:
            options: Transaction options

        Returns:
            New transaction

        Raises:
            TransactionError: If transaction creation fails
        """
        if not self._pool or self._closed:
            raise new_query_error("CONNECTION_CLOSED", "Connection is closed")

        conn = await self._pool.acquire()
        tx = PostgreSQLTransaction(conn, self._pool, options or TxOptions())
        await tx._begin()  # Start the transaction
        return tx
    
    async def batch(self, operations: List[Operation]) -> List[OperationResult]:
        """Execute multiple operations in a batch.
        
        Args:
            operations: List of operations to execute
            
        Returns:
            List of operation results
            
        Raises:
            QueryError: If any operation fails
        """
        results = []
        
        for i, operation in enumerate(operations):
            try:
                if operation.operation_type == OperationType.QUERY and operation.query:
                    result = await self.query(operation.query)
                    results.append(OperationResult(index=i, result=result))
                elif operation.operation_type == OperationType.COMMAND and operation.command:
                    result = await self.execute(operation.command)
                    results.append(OperationResult(index=i, result=result))
                else:
                    raise ValueError(f"Invalid operation at index {i}")
            except Exception as e:
                results.append(OperationResult(index=i, error=e))
        
        return results
    
    async def create_table(self, schema: TableSchema) -> None:
        """Create a table.
        
        Args:
            schema: Table schema definition
            
        Raises:
            SchemaError: If table creation fails
        """
        # This is a simplified implementation
        # In a full implementation, you would build the CREATE TABLE SQL
        # from the schema definition
        raise NotImplementedError("create_table not yet implemented")
    
    async def drop_table(self, table_name: str) -> None:
        """Drop a table.
        
        Args:
            table_name: Name of the table to drop
            
        Raises:
            SchemaError: If table drop fails
        """
        try:
            sql = f"DROP TABLE IF EXISTS {table_name}"
            command = Command(sql=sql, parameters=[])
            await self.execute(command)
        except Exception as e:
            raise new_schema_error("DROP_TABLE_FAILED", f"Failed to drop table {table_name}: {e}") from e
    
    async def list_tables(self) -> List[str]:
        """List all tables.
        
        Returns:
            List of table names
            
        Raises:
            SchemaError: If listing fails
        """
        try:
            sql = """
                SELECT table_name 
                FROM information_schema.tables 
                WHERE table_schema = 'public' 
                ORDER BY table_name
            """
            query = Query(sql=sql, parameters=[])
            result = await self.query(query)
            
            tables = []
            async for row in result:
                tables.append(row.get("table_name"))
            
            return tables
            
        except Exception as e:
            raise new_schema_error("LIST_TABLES_FAILED", f"Failed to list tables: {e}") from e
    
    async def describe_table(self, table_name: str) -> TableSchema:
        """Get table schema.
        
        Args:
            table_name: Name of the table
            
        Returns:
            Table schema
            
        Raises:
            SchemaError: If table doesn't exist or describe fails
        """
        # This is a simplified implementation
        # In a full implementation, you would query information_schema
        # to build the complete table schema
        raise NotImplementedError("describe_table not yet implemented")
    
    def info(self) -> StorageInfo:
        """Get storage information.
        
        Returns:
            Storage information
        """
        return StorageInfo(
            name="PostgreSQL",
            version="1.0.0",
            database_type=DatabaseType.POSTGRESQL,
            features=["transactions", "joins", "schema", "batch", "async"],
            limits=StorageLimits(
                max_connections=self._config.max_open_conns,
                max_query_size=1024 * 1024,  # 1MB
                max_transaction_age=datetime.timedelta(hours=24),
                max_batch_size=1000,
            ),
        )
    
    async def health(self) -> HealthStatus:
        """Get health status.
        
        Returns:
            Health status
        """
        try:
            await self.ping()
            return HealthStatus(
                status=HealthStatusType.HEALTHY,
                message="PostgreSQL connection is healthy",
                details={
                    "pool_size": self._pool.get_size() if self._pool else 0,
                    "pool_idle": self._pool.get_idle_size() if self._pool else 0,
                    "database": self._config.database,
                    "host": self._config.host,
                    "port": self._config.port,
                },
            )
        except Exception as e:
            return HealthStatus(
                status=HealthStatusType.UNHEALTHY,
                message=f"PostgreSQL connection is unhealthy: {e}",
                details={"error": str(e)},
            )
    
    def _build_connect_kwargs(self) -> Dict[str, Any]:
        """Build connection parameters for asyncpg."""
        kwargs = {}
        
        if self._config.dsn:
            kwargs["dsn"] = self._config.dsn
        else:
            kwargs["host"] = self._config.host
            kwargs["port"] = self._config.port
            kwargs["database"] = self._config.database
            kwargs["user"] = self._config.username
            kwargs["password"] = self._config.password
        
        # Add timeout
        kwargs["command_timeout"] = self._config.query_timeout.total_seconds()
        
        # Add SSL settings
        if self._config.ssl_mode and self._config.ssl_mode != "disable":
            kwargs["ssl"] = self._config.ssl_mode
        
        return kwargs
    
    def _convert_placeholders(self, sql: str) -> str:
        """Convert ? placeholders to $n format for PostgreSQL."""
        param_index = 1
        result = []
        
        for char in sql:
            if char == "?":
                result.append(f"${param_index}")
                param_index += 1
            else:
                result.append(char)
        
        return "".join(result)
    
    def _parse_execute_result(self, result: str) -> int:
        """Parse execute result to get rows affected."""
        # asyncpg returns strings like "INSERT 0 1" or "UPDATE 3"
        if result.startswith("INSERT"):
            parts = result.split()
            return int(parts[2]) if len(parts) >= 3 else 0
        elif result.startswith(("UPDATE", "DELETE")):
            parts = result.split()
            return int(parts[1]) if len(parts) >= 2 else 0
        else:
            return 0
