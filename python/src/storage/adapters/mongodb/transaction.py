"""
MongoDB transaction implementation.

This module provides transaction management for MongoDB using Motor
with support for multi-document transactions and session management.
"""

from __future__ import annotations

import asyncio
from typing import Any, Dict, List, Optional

from motor.motor_asyncio import AsyncIOMotorClient, AsyncIOMotorClientSession, AsyncIOMotorDatabase
from pymongo import ReadPreference
from pymongo.read_concern import ReadConcern
from pymongo.write_concern import WriteConcern

from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow
from storage.errors import (
    TransactionError,
    new_transaction_error,
    new_transaction_timeout_error,
    new_query_error,
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


class MongoDBTransaction(Transaction):
    """MongoDB transaction implementation using Motor sessions."""
    
    def __init__(
        self,
        session: AsyncIOMotorClientSession,
        database: AsyncIOMotorDatabase,
        options: Optional[TxOptions] = None
    ) -> None:
        """Initialize MongoDB transaction.
        
        Args:
            session: Motor client session
            database: MongoDB database
            options: Transaction options
        """
        self._session = session
        self._database = database
        self._options = options
        self._active = True
    
    @classmethod
    async def create(
        cls,
        client: AsyncIOMotorClient,
        database: AsyncIOMotorDatabase,
        options: Optional[TxOptions] = None
    ) -> MongoDBTransaction:
        """Create a new MongoDB transaction.
        
        Args:
            client: MongoDB client
            database: MongoDB database
            options: Transaction options
            
        Returns:
            New transaction instance
            
        Raises:
            TransactionError: If transaction creation fails
        """
        try:
            # Start session
            session = await client.start_session()
            
            # Configure transaction options
            txn_options = {}
            
            if options:
                # Map isolation levels to MongoDB read concerns
                if options.isolation == IsolationLevel.READ_COMMITTED:
                    txn_options["read_concern"] = ReadConcern("majority")
                elif options.isolation == IsolationLevel.SERIALIZABLE:
                    txn_options["read_concern"] = ReadConcern("linearizable")
                
                # Set read preference for read-only transactions
                if options.read_only:
                    txn_options["read_preference"] = ReadPreference.SECONDARY_PREFERRED
                
                # Set write concern
                txn_options["write_concern"] = WriteConcern(w="majority")
            
            # Start transaction
            await session.start_transaction(**txn_options)
            
            return cls(session, database, options)
            
        except Exception as e:
            if 'session' in locals():
                await session.end_session()
            raise new_transaction_error(f"Failed to create MongoDB transaction: {e}") from e
    
    async def query(self, query: Query) -> MongoDBResult:
        """Execute a query within the transaction.
        
        Args:
            query: Query to execute
            
        Returns:
            Query results
            
        Raises:
            TransactionError: If transaction is not active
            QueryError: If query execution fails
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
        try:
            # Extract collection name and build filter
            collection_name = self._extract_collection_name(query.sql)
            if not collection_name:
                raise new_query_error("Collection name not found in query")
            
            collection = self._database[collection_name]
            filter_doc = self._build_filter(query.parameters)
            
            # Execute find operation within session
            cursor = collection.find(filter_doc, session=self._session)
            
            return MongoDBResult(cursor)
            
        except Exception as e:
            if isinstance(e, (TransactionError, new_query_error().__class__)):
                raise
            raise new_query_error(f"MongoDB transaction query failed: {e}") from e
    
    async def query_one(self, query: Query) -> MongoDBRow:
        """Execute a query and return a single row within the transaction.
        
        Args:
            query: Query to execute
            
        Returns:
            Single query result
            
        Raises:
            TransactionError: If transaction is not active
            QueryError: If query execution fails
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
        try:
            # Extract collection name and build filter
            collection_name = self._extract_collection_name(query.sql)
            if not collection_name:
                raise new_query_error("Collection name not found in query")
            
            collection = self._database[collection_name]
            filter_doc = self._build_filter(query.parameters)
            
            # Execute findOne operation within session
            document = await collection.find_one(filter_doc, session=self._session)
            if document is None:
                raise new_query_error("No document found")
            
            return MongoDBRow(document)
            
        except Exception as e:
            if isinstance(e, (TransactionError, new_query_error().__class__)):
                raise
            raise new_query_error(f"MongoDB transaction query_one failed: {e}") from e
    
    async def execute(self, command: Command) -> ExecuteResult:
        """Execute a command within the transaction.
        
        Args:
            command: Command to execute
            
        Returns:
            Execution result
            
        Raises:
            TransactionError: If transaction is not active
            QueryError: If command execution fails
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
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
            if isinstance(e, (TransactionError, new_query_error().__class__)):
                raise
            raise new_query_error(f"MongoDB transaction command failed: {e}") from e
    
    async def batch(self, operations: List[Operation]) -> List[OperationResult]:
        """Execute multiple operations within the transaction.
        
        Args:
            operations: List of operations to execute
            
        Returns:
            List of operation results
            
        Raises:
            TransactionError: If transaction is not active
            QueryError: If batch execution fails
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
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
        
        # Execute bulk operations for each collection within session
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
                    await collection.bulk_write(bulk_ops, session=self._session)
                    
                    # Mark all operations for this collection as successful
                    for idx, _ in ops:
                        results[idx] = OperationResult(success=True)
                        
            except Exception as e:
                # Mark all operations for this collection as failed
                for idx, _ in ops:
                    results[idx] = OperationResult(
                        success=False,
                        error=new_query_error(f"Transaction batch operation failed: {e}")
                    )
        
        return results
    
    async def commit(self) -> None:
        """Commit the transaction.
        
        Raises:
            TransactionError: If commit fails or transaction is not active
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
        try:
            await self._session.commit_transaction()
            self._active = False
            await self._session.end_session()
        except Exception as e:
            self._active = False
            await self._session.end_session()
            raise new_transaction_error(f"Failed to commit MongoDB transaction: {e}") from e
    
    async def rollback(self) -> None:
        """Rollback the transaction.
        
        Raises:
            TransactionError: If rollback fails or transaction is not active
        """
        if not self._active:
            raise new_transaction_error("Transaction is not active")
        
        try:
            await self._session.abort_transaction()
            self._active = False
            await self._session.end_session()
        except Exception as e:
            self._active = False
            await self._session.end_session()
            raise new_transaction_error(f"Failed to rollback MongoDB transaction: {e}") from e
    
    # Helper methods
    
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
        """Execute an insert command within transaction."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        document = self._build_document(command.parameters)
        
        result = await collection.insert_one(document, session=self._session)
        
        return ExecuteResult(
            rows_affected=1,
            last_insert_id=str(result.inserted_id) if result.inserted_id else None
        )
    
    async def _execute_update(self, command: Command) -> ExecuteResult:
        """Execute an update command within transaction."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        
        # Split parameters into filter and update parts
        mid = len(command.parameters) // 2
        filter_doc = self._build_filter(command.parameters[:mid])
        update_doc = {"$set": self._build_document(command.parameters[mid:])}
        
        result = await collection.update_many(filter_doc, update_doc, session=self._session)
        
        return ExecuteResult(
            rows_affected=result.modified_count,
            last_insert_id=None
        )
    
    async def _execute_delete(self, command: Command) -> ExecuteResult:
        """Execute a delete command within transaction."""
        collection_name = self._extract_collection_name(command.sql)
        if not collection_name:
            raise new_query_error("Collection name not found in command")
        
        collection = self._database[collection_name]
        filter_doc = self._build_filter(command.parameters)
        
        result = await collection.delete_many(filter_doc, session=self._session)
        
        return ExecuteResult(
            rows_affected=result.deleted_count,
            last_insert_id=None
        )
