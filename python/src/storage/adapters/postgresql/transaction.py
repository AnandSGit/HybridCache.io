"""
PostgreSQL transaction implementation.

This module provides transaction management for PostgreSQL using asyncpg
with support for all isolation levels and savepoints.
"""

from __future__ import annotations

import asyncio
import uuid
from typing import List, Optional

import asyncpg

from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.errors import (
    TransactionError,
    new_transaction_error,
    new_transaction_timeout_error,
)
from storage.interfaces import Transaction
from storage.types import (
    Command,
    ExecuteResult,
    IsolationLevel,
    Operation,
    OperationResult,
    OperationType,
    Query,
    TxOptions,
)


class PostgreSQLTransaction(Transaction):
    """PostgreSQL transaction implementation using asyncpg."""
    
    def __init__(
        self, 
        connection: asyncpg.Connection, 
        pool: asyncpg.Pool,
        options: TxOptions
    ) -> None:
        """Initialize PostgreSQL transaction.
        
        Args:
            connection: Database connection
            pool: Connection pool (for releasing connection)
            options: Transaction options
        """
        self._connection = connection
        self._pool = pool
        self._options = options
        self._transaction: Optional[asyncpg.transaction.Transaction] = None
        self._active = False
        self._id = str(uuid.uuid4())
        self._savepoints: List[str] = []
    
    async def __aenter__(self) -> PostgreSQLTransaction:
        """Async context manager entry."""
        await self._begin()
        return self
    
    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        """Async context manager exit."""
        if exc_type is not None:
            await self.rollback()
        else:
            await self.commit()
    
    async def query(self, query: Query) -> PostgreSQLResult:
        """Execute a query within the transaction.
        
        Args:
            query: The query to execute
            
        Returns:
            Query results
            
        Raises:
            QueryError: If query execution fails
            TransactionError: If transaction is not active
        """
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
        try:
            # Convert ? placeholders to $n format
            sql = self._convert_placeholders(query.sql)
            
            # Execute query with timeout
            if query.timeout:
                result = await asyncio.wait_for(
                    self._connection.fetch(sql, *query.parameters),
                    timeout=query.timeout.total_seconds()
                )
            else:
                result = await self._connection.fetch(sql, *query.parameters)
            
            return PostgreSQLResult(result, self._connection)
            
        except asyncio.TimeoutError as e:
            raise new_transaction_error("QUERY_TIMEOUT", f"Query timeout in transaction: {query.sql[:100]}...") from e
        except Exception as e:
            raise new_transaction_error("QUERY_FAILED", f"Query failed in transaction: {e}") from e
    
    async def query_one(self, query: Query) -> Optional[PostgreSQLRow]:
        """Execute a query and return a single row.
        
        Args:
            query: The query to execute
            
        Returns:
            Single row or None
            
        Raises:
            QueryError: If query execution fails
            TransactionError: If transaction is not active
        """
        result = await self.query(query)
        return await result.fetchone()
    
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
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
        try:
            # Convert ? placeholders to $n format
            sql = self._convert_placeholders(command.sql)
            
            # Execute command with timeout
            if command.timeout:
                result = await asyncio.wait_for(
                    self._connection.execute(sql, *command.parameters),
                    timeout=command.timeout.total_seconds()
                )
            else:
                result = await self._connection.execute(sql, *command.parameters)
            
            # Parse result to get rows affected
            rows_affected = self._parse_execute_result(result)
            
            return ExecuteResult(rows_affected=rows_affected)
            
        except asyncio.TimeoutError as e:
            raise new_transaction_error("COMMAND_TIMEOUT", f"Command timeout in transaction: {command.sql[:100]}...") from e
        except Exception as e:
            raise new_transaction_error("COMMAND_FAILED", f"Command failed in transaction: {e}") from e
    
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
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
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
    
    async def commit(self) -> None:
        """Commit the transaction.
        
        Raises:
            TransactionError: If commit fails
        """
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
        try:
            if self._transaction:
                await self._transaction.commit()
            self._active = False
            
            # Release connection back to pool
            await self._pool.release(self._connection)
            
        except Exception as e:
            raise new_transaction_error("COMMIT_FAILED", f"Failed to commit transaction: {e}") from e
    
    async def rollback(self) -> None:
        """Rollback the transaction.
        
        Raises:
            TransactionError: If rollback fails
        """
        if not self._active:
            return  # Already rolled back or committed
        
        try:
            if self._transaction:
                await self._transaction.rollback()
            self._active = False
            
            # Release connection back to pool
            await self._pool.release(self._connection)
            
        except Exception as e:
            raise new_transaction_error("ROLLBACK_FAILED", f"Failed to rollback transaction: {e}") from e
    
    async def savepoint(self, name: str) -> None:
        """Create a savepoint.
        
        Args:
            name: Savepoint name
            
        Raises:
            TransactionError: If savepoint creation fails
        """
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
        try:
            await self._connection.execute(f"SAVEPOINT {name}")
            self._savepoints.append(name)
        except Exception as e:
            raise new_transaction_error("SAVEPOINT_FAILED", f"Failed to create savepoint {name}: {e}") from e
    
    async def rollback_to_savepoint(self, name: str) -> None:
        """Rollback to a savepoint.
        
        Args:
            name: Savepoint name
            
        Raises:
            TransactionError: If rollback fails
        """
        if not self._active:
            raise new_transaction_error("TX_NOT_ACTIVE", "Transaction is not active")
        
        if name not in self._savepoints:
            raise new_transaction_error("SAVEPOINT_NOT_FOUND", f"Savepoint {name} not found")
        
        try:
            await self._connection.execute(f"ROLLBACK TO SAVEPOINT {name}")
            
            # Remove savepoints created after this one
            index = self._savepoints.index(name)
            self._savepoints = self._savepoints[:index + 1]
            
        except Exception as e:
            raise new_transaction_error("ROLLBACK_TO_SAVEPOINT_FAILED", f"Failed to rollback to savepoint {name}: {e}") from e
    
    def is_active(self) -> bool:
        """Check if the transaction is active.
        
        Returns:
            True if transaction is active
        """
        return self._active
    
    def get_id(self) -> str:
        """Get the transaction ID.
        
        Returns:
            Transaction ID
        """
        return self._id
    
    async def _begin(self) -> None:
        """Begin the transaction."""
        try:
            # Map isolation level to PostgreSQL syntax
            isolation_map = {
                IsolationLevel.READ_UNCOMMITTED: "READ UNCOMMITTED",
                IsolationLevel.READ_COMMITTED: "READ COMMITTED",
                IsolationLevel.REPEATABLE_READ: "REPEATABLE READ",
                IsolationLevel.SERIALIZABLE: "SERIALIZABLE",
            }
            
            isolation = isolation_map.get(self._options.isolation, "READ COMMITTED")
            
            # Begin transaction with isolation level
            self._transaction = self._connection.transaction(
                isolation=isolation,
                readonly=self._options.read_only,
            )
            
            await self._transaction.start()
            self._active = True
            
            # Set timeout if specified
            if self._options.timeout:
                # PostgreSQL doesn't have per-transaction timeouts,
                # but we could implement this with asyncio.wait_for
                pass
                
        except Exception as e:
            raise new_transaction_error("BEGIN_FAILED", f"Failed to begin transaction: {e}") from e
    
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
