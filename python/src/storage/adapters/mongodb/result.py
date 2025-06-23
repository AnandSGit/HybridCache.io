"""
MongoDB result handling implementation.

This module provides result and row implementations for MongoDB query results
using Motor cursors and documents.
"""

from __future__ import annotations

import dataclasses
from typing import Any, AsyncIterator, Dict, List, Optional

from motor.motor_asyncio import AsyncIOMotorCursor

from storage.errors import DataError, new_data_error
from storage.interfaces import Result, Row


class MongoDBRow(Row):
    """MongoDB row implementation using BSON documents."""
    
    def __init__(self, document: Dict[str, Any]) -> None:
        """Initialize MongoDB row.
        
        Args:
            document: MongoDB document
        """
        self._document = document
    
    def get(self, key: str, default: Any = None) -> Any:
        """Get a field value by key.
        
        Args:
            key: Field name
            default: Default value if key not found
            
        Returns:
            Field value
        """
        return self._document.get(key, default)
    
    def keys(self) -> List[str]:
        """Get all field names.
        
        Returns:
            List of field names
        """
        return list(self._document.keys())
    
    def values(self) -> List[Any]:
        """Get all field values.
        
        Returns:
            List of field values
        """
        return list(self._document.values())
    
    def items(self) -> List[tuple[str, Any]]:
        """Get all field name-value pairs.
        
        Returns:
            List of (name, value) tuples
        """
        return list(self._document.items())
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert row to dictionary.
        
        Returns:
            Dictionary representation
        """
        return self._document.copy()
    
    def __getitem__(self, key: str) -> Any:
        """Get field value by key.
        
        Args:
            key: Field name
            
        Returns:
            Field value
            
        Raises:
            KeyError: If key not found
        """
        return self._document[key]
    
    def __contains__(self, key: str) -> bool:
        """Check if field exists.
        
        Args:
            key: Field name
            
        Returns:
            True if field exists
        """
        return key in self._document
    
    def __len__(self) -> int:
        """Get number of fields.
        
        Returns:
            Number of fields
        """
        return len(self._document)
    
    def __iter__(self):
        """Iterate over field names."""
        return iter(self._document)
    
    def __repr__(self) -> str:
        """String representation."""
        return f"MongoDBRow({self._document})"


class MongoDBResult(Result):
    """MongoDB result implementation using Motor cursors."""
    
    def __init__(self, cursor: AsyncIOMotorCursor) -> None:
        """Initialize MongoDB result.
        
        Args:
            cursor: Motor cursor
        """
        self._cursor = cursor
        self._closed = False
    
    def __aiter__(self) -> AsyncIterator[MongoDBRow]:
        """Async iterator over result rows."""
        return self
    
    async def __anext__(self) -> MongoDBRow:
        """Get the next row.
        
        Returns:
            Next row
            
        Raises:
            StopAsyncIteration: When no more rows
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise StopAsyncIteration
        
        try:
            document = await self._cursor.next()
            if document is None:
                raise StopAsyncIteration
            return MongoDBRow(document)
        except StopAsyncIteration:
            raise
        except Exception as e:
            raise new_data_error(f"Error reading MongoDB result: {e}") from e
    
    async def fetchone(self) -> Optional[MongoDBRow]:
        """Fetch the next row.
        
        Returns:
            Next row or None if no more rows
            
        Raises:
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise new_data_error("Result cursor is closed")
        
        try:
            document = await self._cursor.next()
            if document is None:
                return None
            return MongoDBRow(document)
        except StopAsyncIteration:
            return None
        except Exception as e:
            raise new_data_error(f"Error fetching MongoDB row: {e}") from e
    
    async def fetchmany(self, size: int = 100) -> List[MongoDBRow]:
        """Fetch multiple rows.
        
        Args:
            size: Maximum number of rows to fetch
            
        Returns:
            List of rows
            
        Raises:
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise new_data_error("Result cursor is closed")
        
        try:
            rows = []
            async for document in self._cursor:
                if len(rows) >= size:
                    break
                rows.append(MongoDBRow(document))
            return rows
        except Exception as e:
            raise new_data_error(f"Error fetching MongoDB rows: {e}") from e
    
    async def fetchall(self) -> List[MongoDBRow]:
        """Fetch all remaining rows.
        
        Returns:
            List of all rows
            
        Raises:
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise new_data_error("Result cursor is closed")
        
        try:
            rows = []
            async for document in self._cursor:
                rows.append(MongoDBRow(document))
            return rows
        except Exception as e:
            raise new_data_error(f"Error fetching all MongoDB rows: {e}") from e
    
    async def close(self) -> None:
        """Close the result cursor."""
        if not self._closed:
            self._closed = True
            # Motor cursors are automatically closed when exhausted or garbage collected
            # But we can explicitly close if needed
            if hasattr(self._cursor, 'close'):
                await self._cursor.close()
    
    async def count(self) -> int:
        """Get the total count of documents.
        
        Note: This may not be accurate for all cursor types.
        
        Returns:
            Estimated document count
            
        Raises:
            DataError: If count operation fails
        """
        if self._closed:
            raise new_data_error("Result cursor is closed")
        
        try:
            # For Motor cursors, we need to use the collection's count method
            # This is an approximation since we can't get exact count from cursor
            collection = self._cursor.collection
            return await collection.estimated_document_count()
        except Exception as e:
            raise new_data_error(f"Error getting MongoDB result count: {e}") from e
    
    def __repr__(self) -> str:
        """String representation."""
        status = "closed" if self._closed else "open"
        return f"MongoDBResult(cursor={self._cursor}, status={status})"


class MongoDBBatchResult:
    """Result for MongoDB batch operations."""
    
    def __init__(self, results: List[Any]) -> None:
        """Initialize batch result.
        
        Args:
            results: List of individual operation results
        """
        self._results = results
    
    @property
    def inserted_count(self) -> int:
        """Get number of inserted documents."""
        count = 0
        for result in self._results:
            if hasattr(result, 'inserted_count'):
                count += result.inserted_count
            elif hasattr(result, 'inserted_id'):
                count += 1
        return count
    
    @property
    def modified_count(self) -> int:
        """Get number of modified documents."""
        count = 0
        for result in self._results:
            if hasattr(result, 'modified_count'):
                count += result.modified_count
        return count
    
    @property
    def deleted_count(self) -> int:
        """Get number of deleted documents."""
        count = 0
        for result in self._results:
            if hasattr(result, 'deleted_count'):
                count += result.deleted_count
        return count
    
    @property
    def upserted_count(self) -> int:
        """Get number of upserted documents."""
        count = 0
        for result in self._results:
            if hasattr(result, 'upserted_count'):
                count += result.upserted_count
        return count
    
    @property
    def matched_count(self) -> int:
        """Get number of matched documents."""
        count = 0
        for result in self._results:
            if hasattr(result, 'matched_count'):
                count += result.matched_count
        return count
    
    def __repr__(self) -> str:
        """String representation."""
        return (f"MongoDBBatchResult("
                f"inserted={self.inserted_count}, "
                f"modified={self.modified_count}, "
                f"deleted={self.deleted_count}, "
                f"upserted={self.upserted_count})")


class MongoDBAggregateResult(Result):
    """Result for MongoDB aggregation operations."""
    
    def __init__(self, cursor: AsyncIOMotorCursor, pipeline: List[Dict[str, Any]]) -> None:
        """Initialize aggregation result.
        
        Args:
            cursor: Motor cursor for aggregation results
            pipeline: Aggregation pipeline used
        """
        self._cursor = cursor
        self._pipeline = pipeline
        self._closed = False
    
    @property
    def pipeline(self) -> List[Dict[str, Any]]:
        """Get the aggregation pipeline.
        
        Returns:
            Aggregation pipeline
        """
        return self._pipeline.copy()
    
    def __aiter__(self) -> AsyncIterator[MongoDBRow]:
        """Async iterator over aggregation results."""
        return self
    
    async def __anext__(self) -> MongoDBRow:
        """Get the next aggregation result.
        
        Returns:
            Next result row
            
        Raises:
            StopAsyncIteration: When no more results
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise StopAsyncIteration
        
        try:
            document = await self._cursor.next()
            if document is None:
                raise StopAsyncIteration
            return MongoDBRow(document)
        except StopAsyncIteration:
            raise
        except Exception as e:
            raise new_data_error(f"Error reading MongoDB aggregation result: {e}") from e
    
    async def fetchall(self) -> List[MongoDBRow]:
        """Fetch all aggregation results.
        
        Returns:
            List of all result rows
            
        Raises:
            DataError: If cursor is closed or error occurs
        """
        if self._closed:
            raise new_data_error("Aggregation cursor is closed")
        
        try:
            rows = []
            async for document in self._cursor:
                rows.append(MongoDBRow(document))
            return rows
        except Exception as e:
            raise new_data_error(f"Error fetching aggregation results: {e}") from e
    
    async def close(self) -> None:
        """Close the aggregation cursor."""
        if not self._closed:
            self._closed = True
            if hasattr(self._cursor, 'close'):
                await self._cursor.close()
    
    def __repr__(self) -> str:
        """String representation."""
        status = "closed" if self._closed else "open"
        return f"MongoDBAggregateResult(pipeline_stages={len(self._pipeline)}, status={status})"
