"""
Core interfaces for the Database Agnostic Storage Library.

This module defines the abstract interfaces that all database adapters must implement,
providing a unified API across different database types while maintaining async support.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any, AsyncContextManager, AsyncIterator, Dict, List, Optional, Type

from storage.types import (
    Command,
    Config,
    DataType,
    ExecuteResult,
    HealthStatus,
    Operation,
    OperationResult,
    Query,
    StorageInfo,
    TableSchema,
    TxOptions,
)


class Row(ABC):
    """Interface for a single database row result."""
    
    @abstractmethod
    async def scan(self, *destinations: Any) -> None:
        """Scan the row into the provided destinations.
        
        Args:
            *destinations: Variables to scan the row values into
            
        Raises:
            DataError: If scanning fails
        """
        ...
    
    @abstractmethod
    async def scan_into(self, obj: Any) -> None:
        """Scan the row into a dataclass or object.
        
        Args:
            obj: Object to scan the row into
            
        Raises:
            DataError: If scanning fails
        """
        ...
    
    @abstractmethod
    def get(self, key: str, default: Any = None) -> Any:
        """Get a value by column name.
        
        Args:
            key: Column name
            default: Default value if key not found
            
        Returns:
            The column value or default
        """
        ...
    
    @abstractmethod
    def keys(self) -> List[str]:
        """Get all column names.
        
        Returns:
            List of column names
        """
        ...
    
    @abstractmethod
    def values(self) -> List[Any]:
        """Get all column values.
        
        Returns:
            List of column values
        """
        ...
    
    @abstractmethod
    def items(self) -> List[tuple[str, Any]]:
        """Get all column name-value pairs.
        
        Returns:
            List of (name, value) tuples
        """
        ...


class Result(ABC):
    """Interface for database query results."""
    
    @abstractmethod
    def __aiter__(self) -> AsyncIterator[Row]:
        """Async iterator over result rows."""
        ...
    
    @abstractmethod
    async def __anext__(self) -> Row:
        """Get the next row."""
        ...
    
    @abstractmethod
    async def fetchone(self) -> Optional[Row]:
        """Fetch a single row.
        
        Returns:
            The next row or None if no more rows
        """
        ...
    
    @abstractmethod
    async def fetchmany(self, size: int = 1000) -> List[Row]:
        """Fetch multiple rows.
        
        Args:
            size: Maximum number of rows to fetch
            
        Returns:
            List of rows
        """
        ...
    
    @abstractmethod
    async def fetchall(self) -> List[Row]:
        """Fetch all remaining rows.
        
        Returns:
            List of all rows
        """
        ...
    
    @abstractmethod
    async def close(self) -> None:
        """Close the result set and free resources."""
        ...
    
    @abstractmethod
    def columns(self) -> List[str]:
        """Get column names.
        
        Returns:
            List of column names
        """
        ...


class Transaction(ABC):
    """Interface for database transactions."""
    
    @abstractmethod
    async def query(self, query: Query) -> Result:
        """Execute a query within the transaction.
        
        Args:
            query: The query to execute
            
        Returns:
            Query results
            
        Raises:
            QueryError: If query execution fails
            TransactionError: If transaction is not active
        """
        ...
    
    @abstractmethod
    async def query_one(self, query: Query) -> Optional[Row]:
        """Execute a query and return a single row.
        
        Args:
            query: The query to execute
            
        Returns:
            Single row or None
            
        Raises:
            QueryError: If query execution fails
            TransactionError: If transaction is not active
        """
        ...
    
    @abstractmethod
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command within the transaction.
        
        Args:
            command: The command to execute
            
        Returns:
            Execution result
            
        Raises:
            QueryError: If command execution fails
            TransactionError: If transaction is not active
        """
        ...
    
    @abstractmethod
    async def batch(self, operations: List[Operation]) -> List[OperationResult]:
        """Execute multiple operations within the transaction.
        
        Args:
            operations: List of operations to execute
            
        Returns:
            List of operation results
            
        Raises:
            QueryError: If any operation fails
            TransactionError: If transaction is not active
        """
        ...
    
    @abstractmethod
    async def commit(self) -> None:
        """Commit the transaction.
        
        Raises:
            TransactionError: If commit fails
        """
        ...
    
    @abstractmethod
    async def rollback(self) -> None:
        """Rollback the transaction.
        
        Raises:
            TransactionError: If rollback fails
        """
        ...
    
    @abstractmethod
    async def savepoint(self, name: str) -> None:
        """Create a savepoint.
        
        Args:
            name: Savepoint name
            
        Raises:
            TransactionError: If savepoint creation fails
        """
        ...
    
    @abstractmethod
    async def rollback_to_savepoint(self, name: str) -> None:
        """Rollback to a savepoint.
        
        Args:
            name: Savepoint name
            
        Raises:
            TransactionError: If rollback fails
        """
        ...
    
    @abstractmethod
    def is_active(self) -> bool:
        """Check if the transaction is active.
        
        Returns:
            True if transaction is active
        """
        ...
    
    @abstractmethod
    def get_id(self) -> str:
        """Get the transaction ID.
        
        Returns:
            Transaction ID
        """
        ...


class Storage(ABC):
    """Main interface for database storage operations."""
    
    @abstractmethod
    async def connect(self) -> None:
        """Establish connection to the database.
        
        Raises:
            ConnectionError: If connection fails
        """
        ...
    
    @abstractmethod
    async def close(self) -> None:
        """Close the database connection and cleanup resources."""
        ...
    
    @abstractmethod
    async def ping(self) -> None:
        """Test the database connection.
        
        Raises:
            ConnectionError: If ping fails
        """
        ...
    
    @abstractmethod
    async def query(self, query: Query) -> Result:
        """Execute a query.
        
        Args:
            query: The query to execute
            
        Returns:
            Query results
            
        Raises:
            QueryError: If query execution fails
        """
        ...
    
    @abstractmethod
    async def query_one(self, query: Query) -> Optional[Row]:
        """Execute a query and return a single row.
        
        Args:
            query: The query to execute
            
        Returns:
            Single row or None
            
        Raises:
            QueryError: If query execution fails
        """
        ...
    
    @abstractmethod
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command.
        
        Args:
            command: The command to execute
            
        Returns:
            Execution result
            
        Raises:
            QueryError: If command execution fails
        """
        ...
    
    @abstractmethod
    async def begin_tx(self, options: Optional[TxOptions] = None) -> Transaction:
        """Begin a new transaction.
        
        Args:
            options: Transaction options
            
        Returns:
            New transaction
            
        Raises:
            TransactionError: If transaction creation fails
        """
        ...
    
    @abstractmethod
    async def batch(self, operations: List[Operation]) -> List[OperationResult]:
        """Execute multiple operations in a batch.
        
        Args:
            operations: List of operations to execute
            
        Returns:
            List of operation results
            
        Raises:
            QueryError: If any operation fails
        """
        ...
    
    @abstractmethod
    async def create_table(self, schema: TableSchema) -> None:
        """Create a table.
        
        Args:
            schema: Table schema definition
            
        Raises:
            SchemaError: If table creation fails
        """
        ...
    
    @abstractmethod
    async def drop_table(self, table_name: str) -> None:
        """Drop a table.
        
        Args:
            table_name: Name of the table to drop
            
        Raises:
            SchemaError: If table drop fails
        """
        ...
    
    @abstractmethod
    async def list_tables(self) -> List[str]:
        """List all tables.
        
        Returns:
            List of table names
            
        Raises:
            SchemaError: If listing fails
        """
        ...
    
    @abstractmethod
    async def describe_table(self, table_name: str) -> TableSchema:
        """Get table schema.
        
        Args:
            table_name: Name of the table
            
        Returns:
            Table schema
            
        Raises:
            SchemaError: If table doesn't exist or describe fails
        """
        ...
    
    @abstractmethod
    def info(self) -> StorageInfo:
        """Get storage information.
        
        Returns:
            Storage information
        """
        ...
    
    @abstractmethod
    async def health(self) -> HealthStatus:
        """Get health status.
        
        Returns:
            Health status
        """
        ...


class Adapter(ABC):
    """Interface for database adapters."""
    
    @abstractmethod
    def name(self) -> str:
        """Get adapter name.
        
        Returns:
            Adapter name
        """
        ...
    
    @abstractmethod
    def version(self) -> str:
        """Get adapter version.
        
        Returns:
            Adapter version
        """
        ...
    
    @abstractmethod
    def database_type(self) -> str:
        """Get database type.
        
        Returns:
            Database type
        """
        ...
    
    @abstractmethod
    async def connect(self, config: Config) -> Storage:
        """Create a storage connection.
        
        Args:
            config: Connection configuration
            
        Returns:
            Storage instance
            
        Raises:
            ConnectionError: If connection fails
        """
        ...
    
    @abstractmethod
    def parse_dsn(self, dsn: str) -> Config:
        """Parse a DSN into configuration.
        
        Args:
            dsn: Data source name
            
        Returns:
            Parsed configuration
            
        Raises:
            ValidationError: If DSN is invalid
        """
        ...
    
    @abstractmethod
    def validate_config(self, config: Config) -> None:
        """Validate configuration.
        
        Args:
            config: Configuration to validate
            
        Raises:
            ValidationError: If configuration is invalid
        """
        ...
    
    @abstractmethod
    def translate_query(self, query: Query) -> tuple[str, List[Any]]:
        """Translate a query to database-specific SQL.
        
        Args:
            query: Query to translate
            
        Returns:
            Tuple of (SQL, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        ...
    
    @abstractmethod
    def translate_command(self, command: Command) -> tuple[str, List[Any]]:
        """Translate a command to database-specific SQL.
        
        Args:
            command: Command to translate
            
        Returns:
            Tuple of (SQL, parameters)
            
        Raises:
            QueryError: If translation fails
        """
        ...
    
    @abstractmethod
    def map_go_type(self, value: Any) -> DataType:
        """Map a Python type to a storage data type.
        
        Args:
            value: Python value
            
        Returns:
            Storage data type
            
        Raises:
            ValidationError: If type cannot be mapped
        """
        ...
    
    @abstractmethod
    def map_database_type(self, db_type: str) -> DataType:
        """Map a database type to a storage data type.
        
        Args:
            db_type: Database type name
            
        Returns:
            Storage data type
            
        Raises:
            ValidationError: If type cannot be mapped
        """
        ...
    
    @abstractmethod
    def supports_transactions(self) -> bool:
        """Check if adapter supports transactions."""
        ...
    
    @abstractmethod
    def supports_joins(self) -> bool:
        """Check if adapter supports joins."""
        ...
    
    @abstractmethod
    def supports_batch(self) -> bool:
        """Check if adapter supports batch operations."""
        ...
    
    @abstractmethod
    def supports_schema(self) -> bool:
        """Check if adapter supports schema operations."""
        ...


class HealthChecker(ABC):
    """Interface for health checking."""
    
    @abstractmethod
    async def check_health(self) -> HealthStatus:
        """Check system health.
        
        Returns:
            Health status
        """
        ...
