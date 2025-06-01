"""
PostgreSQL result handling implementation.

This module provides result and row implementations for PostgreSQL query results
using asyncpg records.
"""

from __future__ import annotations

import dataclasses
from typing import Any, AsyncIterator, List, Optional

import asyncpg

from storage.errors import DataError, new_data_error
from storage.interfaces import Result, Row


class PostgreSQLRow(Row):
    """PostgreSQL row implementation using asyncpg.Record."""
    
    def __init__(self, record: asyncpg.Record) -> None:
        """Initialize PostgreSQL row.
        
        Args:
            record: asyncpg record
        """
        self._record = record
    
    async def scan(self, *destinations: Any) -> None:
        """Scan the row into the provided destinations.
        
        Args:
            *destinations: Variables to scan the row values into
            
        Raises:
            DataError: If scanning fails
        """
        try:
            if len(destinations) != len(self._record):
                raise new_data_error(
                    "SCAN_MISMATCH",
                    f"Expected {len(destinations)} values, got {len(self._record)}"
                )
            
            for i, dest in enumerate(destinations):
                # This is a simplified implementation
                # In practice, you would need to handle type conversion
                # and assignment to the destination variables
                if hasattr(dest, '__setitem__'):
                    dest[0] = self._record[i]
                else:
                    # For simple variables, we can't modify them directly
                    # This would need a different approach in practice
                    pass
                    
        except Exception as e:
            raise new_data_error("SCAN_FAILED", f"Failed to scan row: {e}") from e
    
    async def scan_into(self, obj: Any) -> None:
        """Scan the row into a dataclass or object.
        
        Args:
            obj: Object to scan the row into
            
        Raises:
            DataError: If scanning fails
        """
        try:
            if dataclasses.is_dataclass(obj):
                # Handle dataclass objects
                field_names = [field.name for field in dataclasses.fields(obj)]
                for field_name in field_names:
                    if field_name in self._record:
                        setattr(obj, field_name, self._record[field_name])
            else:
                # Handle regular objects with attributes
                for key in self._record.keys():
                    if hasattr(obj, key):
                        setattr(obj, key, self._record[key])
                        
        except Exception as e:
            raise new_data_error("SCAN_INTO_FAILED", f"Failed to scan into object: {e}") from e
    
    def get(self, key: str, default: Any = None) -> Any:
        """Get a value by column name.
        
        Args:
            key: Column name
            default: Default value if key not found
            
        Returns:
            The column value or default
        """
        try:
            return self._record[key]
        except KeyError:
            return default
    
    def keys(self) -> List[str]:
        """Get all column names.
        
        Returns:
            List of column names
        """
        return list(self._record.keys())
    
    def values(self) -> List[Any]:
        """Get all column values.
        
        Returns:
            List of column values
        """
        return list(self._record.values())
    
    def items(self) -> List[tuple[str, Any]]:
        """Get all column name-value pairs.
        
        Returns:
            List of (name, value) tuples
        """
        return list(self._record.items())
    
    def __getitem__(self, key: str) -> Any:
        """Get a value by column name."""
        return self._record[key]
    
    def __contains__(self, key: str) -> bool:
        """Check if column exists."""
        return key in self._record
    
    def __len__(self) -> int:
        """Get number of columns."""
        return len(self._record)
    
    def __iter__(self):
        """Iterate over column values."""
        return iter(self._record)


class PostgreSQLResult(Result):
    """PostgreSQL result implementation using asyncpg records."""
    
    def __init__(self, records: List[asyncpg.Record], connection: asyncpg.Connection) -> None:
        """Initialize PostgreSQL result.
        
        Args:
            records: List of asyncpg records
            connection: Database connection (for column info)
        """
        self._records = records
        self._connection = connection
        self._index = 0
        self._closed = False
    
    def __aiter__(self) -> AsyncIterator[PostgreSQLRow]:
        """Async iterator over result rows."""
        return self
    
    async def __anext__(self) -> PostgreSQLRow:
        """Get the next row."""
        if self._closed:
            raise StopAsyncIteration
        
        if self._index >= len(self._records):
            raise StopAsyncIteration
        
        row = PostgreSQLRow(self._records[self._index])
        self._index += 1
        return row
    
    async def fetchone(self) -> Optional[PostgreSQLRow]:
        """Fetch a single row.
        
        Returns:
            The next row or None if no more rows
        """
        if self._closed or self._index >= len(self._records):
            return None
        
        row = PostgreSQLRow(self._records[self._index])
        self._index += 1
        return row
    
    async def fetchmany(self, size: int = 1000) -> List[PostgreSQLRow]:
        """Fetch multiple rows.
        
        Args:
            size: Maximum number of rows to fetch
            
        Returns:
            List of rows
        """
        if self._closed:
            return []
        
        end_index = min(self._index + size, len(self._records))
        rows = [
            PostgreSQLRow(record) 
            for record in self._records[self._index:end_index]
        ]
        self._index = end_index
        return rows
    
    async def fetchall(self) -> List[PostgreSQLRow]:
        """Fetch all remaining rows.
        
        Returns:
            List of all rows
        """
        if self._closed:
            return []
        
        rows = [
            PostgreSQLRow(record) 
            for record in self._records[self._index:]
        ]
        self._index = len(self._records)
        return rows
    
    async def close(self) -> None:
        """Close the result set and free resources."""
        self._closed = True
        self._records.clear()
    
    def columns(self) -> List[str]:
        """Get column names.
        
        Returns:
            List of column names
        """
        if not self._records:
            return []
        return list(self._records[0].keys())
    
    def __len__(self) -> int:
        """Get number of rows."""
        return len(self._records)
    
    def __bool__(self) -> bool:
        """Check if result has rows."""
        return len(self._records) > 0
