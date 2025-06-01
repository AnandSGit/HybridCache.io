"""
Query and command builders for the Database Agnostic Storage Library.

This module provides fluent APIs for building SQL queries and commands
in a database-agnostic way.
"""

from __future__ import annotations

import datetime
from typing import Any, Dict, List, Optional, Union

from storage.types import (
    Command,
    CommandType,
    Condition,
    JoinType,
    Operator,
    Query,
    QueryType,
    SortDirection,
)


class QueryBuilder:
    """Fluent API for building database queries."""
    
    def __init__(self) -> None:
        """Initialize a new query builder."""
        self._select_fields: List[str] = []
        self._distinct: bool = False
        self._from_table: str = ""
        self._joins: List[str] = []
        self._where_conditions: List[Condition] = []
        self._group_by_fields: List[str] = []
        self._having_conditions: List[Condition] = []
        self._order_by_clauses: List[str] = []
        self._limit_count: Optional[int] = None
        self._offset_count: Optional[int] = None
        self._parameters: List[Any] = []
    
    def select(self, *fields: str) -> QueryBuilder:
        """Add SELECT fields.
        
        Args:
            *fields: Field names to select
            
        Returns:
            Self for method chaining
        """
        if not fields:
            self._select_fields = ["*"]
        else:
            self._select_fields.extend(fields)
        return self
    
    def select_distinct(self, *fields: str) -> QueryBuilder:
        """Add SELECT DISTINCT fields.
        
        Args:
            *fields: Field names to select
            
        Returns:
            Self for method chaining
        """
        self._distinct = True
        return self.select(*fields)
    
    def select_count(self, field: str = "") -> QueryBuilder:
        """Add SELECT COUNT.
        
        Args:
            field: Field to count, empty for COUNT(*)
            
        Returns:
            Self for method chaining
        """
        if field:
            self._select_fields.append(f"COUNT({field})")
        else:
            self._select_fields.append("COUNT(*)")
        return self
    
    def from_(self, table: str) -> QueryBuilder:
        """Set the FROM table.
        
        Args:
            table: Table name
            
        Returns:
            Self for method chaining
        """
        self._from_table = table
        return self
    
    def join(self, table: str, condition: Condition, join_type: JoinType = JoinType.INNER) -> QueryBuilder:
        """Add a JOIN clause.
        
        Args:
            table: Table to join
            condition: Join condition
            join_type: Type of join
            
        Returns:
            Self for method chaining
        """
        join_sql, join_params = self._build_condition(condition)
        self._joins.append(f"{join_type.value.upper()} JOIN {table} ON {join_sql}")
        self._parameters.extend(join_params)
        return self
    
    def inner_join(self, table: str, condition: Condition) -> QueryBuilder:
        """Add an INNER JOIN clause.
        
        Args:
            table: Table to join
            condition: Join condition
            
        Returns:
            Self for method chaining
        """
        return self.join(table, condition, JoinType.INNER)
    
    def left_join(self, table: str, condition: Condition) -> QueryBuilder:
        """Add a LEFT JOIN clause.
        
        Args:
            table: Table to join
            condition: Join condition
            
        Returns:
            Self for method chaining
        """
        return self.join(table, condition, JoinType.LEFT)
    
    def right_join(self, table: str, condition: Condition) -> QueryBuilder:
        """Add a RIGHT JOIN clause.
        
        Args:
            table: Table to join
            condition: Join condition
            
        Returns:
            Self for method chaining
        """
        return self.join(table, condition, JoinType.RIGHT)
    
    def where(self, condition: Condition) -> QueryBuilder:
        """Add a WHERE condition.
        
        Args:
            condition: WHERE condition
            
        Returns:
            Self for method chaining
        """
        self._where_conditions.append(condition)
        return self
    
    def where_and(self, *conditions: Condition) -> QueryBuilder:
        """Add multiple AND WHERE conditions.
        
        Args:
            *conditions: WHERE conditions
            
        Returns:
            Self for method chaining
        """
        self._where_conditions.extend(conditions)
        return self
    
    def group_by(self, *fields: str) -> QueryBuilder:
        """Add GROUP BY fields.
        
        Args:
            *fields: Field names to group by
            
        Returns:
            Self for method chaining
        """
        self._group_by_fields.extend(fields)
        return self
    
    def having(self, condition: Condition) -> QueryBuilder:
        """Add a HAVING condition.
        
        Args:
            condition: HAVING condition
            
        Returns:
            Self for method chaining
        """
        self._having_conditions.append(condition)
        return self
    
    def order_by(self, field: str, direction: SortDirection = SortDirection.ASC) -> QueryBuilder:
        """Add an ORDER BY clause.
        
        Args:
            field: Field to order by
            direction: Sort direction
            
        Returns:
            Self for method chaining
        """
        self._order_by_clauses.append(f"{field} {direction.value.upper()}")
        return self
    
    def order_by_desc(self, field: str) -> QueryBuilder:
        """Add an ORDER BY DESC clause.
        
        Args:
            field: Field to order by
            
        Returns:
            Self for method chaining
        """
        return self.order_by(field, SortDirection.DESC)
    
    def limit(self, count: int) -> QueryBuilder:
        """Set the LIMIT.
        
        Args:
            count: Maximum number of rows
            
        Returns:
            Self for method chaining
        """
        self._limit_count = count
        return self
    
    def offset(self, count: int) -> QueryBuilder:
        """Set the OFFSET.
        
        Args:
            count: Number of rows to skip
            
        Returns:
            Self for method chaining
        """
        self._offset_count = count
        return self
    
    def page(self, page: int, size: int) -> QueryBuilder:
        """Set pagination.
        
        Args:
            page: Page number (1-based)
            size: Page size
            
        Returns:
            Self for method chaining
        """
        self._limit_count = size
        self._offset_count = (page - 1) * size
        return self
    
    def build(self) -> Query:
        """Build the query.
        
        Returns:
            Built query
            
        Raises:
            ValueError: If query is invalid
        """
        if not self._from_table:
            raise ValueError("FROM table is required")
        
        sql_parts = []
        parameters = []
        
        # SELECT clause
        select_clause = "SELECT "
        if self._distinct:
            select_clause += "DISTINCT "
        
        if self._select_fields:
            select_clause += ", ".join(self._select_fields)
        else:
            select_clause += "*"
        
        sql_parts.append(select_clause)
        
        # FROM clause
        sql_parts.append(f"FROM {self._from_table}")
        
        # JOIN clauses
        sql_parts.extend(self._joins)
        
        # WHERE clause
        if self._where_conditions:
            where_sql, where_params = self._build_where_clause(self._where_conditions)
            sql_parts.append(f"WHERE {where_sql}")
            parameters.extend(where_params)
        
        # GROUP BY clause
        if self._group_by_fields:
            sql_parts.append(f"GROUP BY {', '.join(self._group_by_fields)}")
        
        # HAVING clause
        if self._having_conditions:
            having_sql, having_params = self._build_where_clause(self._having_conditions)
            sql_parts.append(f"HAVING {having_sql}")
            parameters.extend(having_params)
        
        # ORDER BY clause
        if self._order_by_clauses:
            sql_parts.append(f"ORDER BY {', '.join(self._order_by_clauses)}")
        
        # LIMIT clause
        if self._limit_count is not None:
            sql_parts.append(f"LIMIT {self._limit_count}")
        
        # OFFSET clause
        if self._offset_count is not None:
            sql_parts.append(f"OFFSET {self._offset_count}")
        
        # Add join parameters
        parameters.extend(self._parameters)
        
        sql = " ".join(sql_parts)
        
        return Query(
            sql=sql,
            parameters=parameters,
            query_type=QueryType.SELECT,
        )
    
    def build_raw(self) -> tuple[str, List[Any]]:
        """Build the raw SQL and parameters.
        
        Returns:
            Tuple of (SQL, parameters)
        """
        query = self.build()
        return query.sql, query.parameters
    
    def _build_where_clause(self, conditions: List[Condition]) -> tuple[str, List[Any]]:
        """Build a WHERE clause from conditions.
        
        Args:
            conditions: List of conditions
            
        Returns:
            Tuple of (SQL, parameters)
        """
        if not conditions:
            return "", []
        
        parts = []
        parameters = []
        
        for i, condition in enumerate(conditions):
            if i > 0:
                parts.append("AND")
            
            cond_sql, cond_params = self._build_condition(condition)
            parts.append(cond_sql)
            parameters.extend(cond_params)
        
        return " ".join(parts), parameters
    
    def _build_condition(self, condition: Condition) -> tuple[str, List[Any]]:
        """Build a single condition.
        
        Args:
            condition: The condition to build
            
        Returns:
            Tuple of (SQL, parameters)
        """
        field = condition.field
        operator = condition.operator
        value = condition.value
        values = condition.values or []
        
        if operator == Operator.EQUAL:
            return f"{field} = ?", [value]
        elif operator == Operator.NOT_EQUAL:
            return f"{field} != ?", [value]
        elif operator == Operator.GREATER_THAN:
            return f"{field} > ?", [value]
        elif operator == Operator.GREATER_THAN_OR_EQUAL:
            return f"{field} >= ?", [value]
        elif operator == Operator.LESS_THAN:
            return f"{field} < ?", [value]
        elif operator == Operator.LESS_THAN_OR_EQUAL:
            return f"{field} <= ?", [value]
        elif operator == Operator.LIKE:
            return f"{field} LIKE ?", [value]
        elif operator == Operator.NOT_LIKE:
            return f"{field} NOT LIKE ?", [value]
        elif operator == Operator.IN:
            if not values:
                return "1=0", []  # No values means no match
            placeholders = ", ".join("?" * len(values))
            return f"{field} IN ({placeholders})", values
        elif operator == Operator.NOT_IN:
            if not values:
                return "1=1", []  # No values means all match
            placeholders = ", ".join("?" * len(values))
            return f"{field} NOT IN ({placeholders})", values
        elif operator == Operator.BETWEEN:
            if len(values) >= 2:
                return f"{field} BETWEEN ? AND ?", values[:2]
            return f"{field} BETWEEN ? AND ?", [value, value]
        elif operator == Operator.NOT_BETWEEN:
            if len(values) >= 2:
                return f"{field} NOT BETWEEN ? AND ?", values[:2]
            return f"{field} NOT BETWEEN ? AND ?", [value, value]
        elif operator == Operator.IS_NULL:
            return f"{field} IS NULL", []
        elif operator == Operator.IS_NOT_NULL:
            return f"{field} IS NOT NULL", []
        elif operator == Operator.EXISTS:
            return f"EXISTS ({value})", []
        elif operator == Operator.NOT_EXISTS:
            return f"NOT EXISTS ({value})", []
        else:
            return f"{field} = ?", [value]


class CommandBuilder:
    """Fluent API for building database commands (INSERT, UPDATE, DELETE)."""
    
    def __init__(self, command_type: CommandType, table: str) -> None:
        """Initialize a new command builder.
        
        Args:
            command_type: Type of command
            table: Target table
        """
        self._command_type = command_type
        self._table = table
        self._values: Dict[str, Any] = {}
        self._where_conditions: List[Condition] = []
        self._parameters: List[Any] = []
    
    def set(self, field: str, value: Any) -> CommandBuilder:
        """Set a field value.
        
        Args:
            field: Field name
            value: Field value
            
        Returns:
            Self for method chaining
        """
        self._values[field] = value
        return self
    
    def values(self, values: Dict[str, Any]) -> CommandBuilder:
        """Set multiple field values.
        
        Args:
            values: Dictionary of field-value pairs
            
        Returns:
            Self for method chaining
        """
        self._values.update(values)
        return self
    
    def where(self, condition: Condition) -> CommandBuilder:
        """Add a WHERE condition (for UPDATE/DELETE).
        
        Args:
            condition: WHERE condition
            
        Returns:
            Self for method chaining
        """
        self._where_conditions.append(condition)
        return self
    
    def where_and(self, *conditions: Condition) -> CommandBuilder:
        """Add multiple AND WHERE conditions.
        
        Args:
            *conditions: WHERE conditions
            
        Returns:
            Self for method chaining
        """
        self._where_conditions.extend(conditions)
        return self
    
    def build(self) -> Command:
        """Build the command.
        
        Returns:
            Built command
            
        Raises:
            ValueError: If command is invalid
        """
        if self._command_type == CommandType.INSERT:
            return self._build_insert()
        elif self._command_type == CommandType.UPDATE:
            return self._build_update()
        elif self._command_type == CommandType.DELETE:
            return self._build_delete()
        else:
            raise ValueError(f"Unsupported command type: {self._command_type}")
    
    def build_raw(self) -> tuple[str, List[Any]]:
        """Build the raw SQL and parameters.
        
        Returns:
            Tuple of (SQL, parameters)
        """
        command = self.build()
        return command.sql, command.parameters
    
    def _build_insert(self) -> Command:
        """Build an INSERT command."""
        if not self._values:
            raise ValueError("Values are required for INSERT")
        
        fields = list(self._values.keys())
        placeholders = ["?"] * len(fields)
        parameters = list(self._values.values())
        
        sql = f"INSERT INTO {self._table} ({', '.join(fields)}) VALUES ({', '.join(placeholders)})"
        
        return Command(
            sql=sql,
            parameters=parameters,
            command_type=CommandType.INSERT,
        )
    
    def _build_update(self) -> Command:
        """Build an UPDATE command."""
        if not self._values:
            raise ValueError("Values are required for UPDATE")
        
        set_parts = []
        parameters = []
        
        for field, value in self._values.items():
            set_parts.append(f"{field} = ?")
            parameters.append(value)
        
        sql = f"UPDATE {self._table} SET {', '.join(set_parts)}"
        
        # Add WHERE clause if conditions exist
        if self._where_conditions:
            where_sql, where_params = self._build_where_clause(self._where_conditions)
            sql += f" WHERE {where_sql}"
            parameters.extend(where_params)
        
        return Command(
            sql=sql,
            parameters=parameters,
            command_type=CommandType.UPDATE,
        )
    
    def _build_delete(self) -> Command:
        """Build a DELETE command."""
        sql = f"DELETE FROM {self._table}"
        parameters = []
        
        # Add WHERE clause if conditions exist
        if self._where_conditions:
            where_sql, where_params = self._build_where_clause(self._where_conditions)
            sql += f" WHERE {where_sql}"
            parameters.extend(where_params)
        
        return Command(
            sql=sql,
            parameters=parameters,
            command_type=CommandType.DELETE,
        )
    
    def _build_where_clause(self, conditions: List[Condition]) -> tuple[str, List[Any]]:
        """Build a WHERE clause from conditions."""
        if not conditions:
            return "", []
        
        parts = []
        parameters = []
        
        for i, condition in enumerate(conditions):
            if i > 0:
                parts.append("AND")
            
            cond_sql, cond_params = self._build_condition(condition)
            parts.append(cond_sql)
            parameters.extend(cond_params)
        
        return " ".join(parts), parameters
    
    def _build_condition(self, condition: Condition) -> tuple[str, List[Any]]:
        """Build a single condition."""
        # Reuse the logic from QueryBuilder
        builder = QueryBuilder()
        return builder._build_condition(condition)


# Convenience functions for creating builders

def select(*fields: str) -> QueryBuilder:
    """Create a new SELECT query builder.
    
    Args:
        *fields: Fields to select
        
    Returns:
        New query builder
    """
    return QueryBuilder().select(*fields)


def insert(table: str) -> CommandBuilder:
    """Create a new INSERT command builder.
    
    Args:
        table: Target table
        
    Returns:
        New command builder
    """
    return CommandBuilder(CommandType.INSERT, table)


def update(table: str) -> CommandBuilder:
    """Create a new UPDATE command builder.
    
    Args:
        table: Target table
        
    Returns:
        New command builder
    """
    return CommandBuilder(CommandType.UPDATE, table)


def delete(table: str) -> CommandBuilder:
    """Create a new DELETE command builder.
    
    Args:
        table: Target table
        
    Returns:
        New command builder
    """
    return CommandBuilder(CommandType.DELETE, table)


# Condition helper functions

def equal(field: str, value: Any) -> Condition:
    """Create an equality condition."""
    return Condition(field=field, operator=Operator.EQUAL, value=value)


def not_equal(field: str, value: Any) -> Condition:
    """Create a not-equal condition."""
    return Condition(field=field, operator=Operator.NOT_EQUAL, value=value)


def greater_than(field: str, value: Any) -> Condition:
    """Create a greater-than condition."""
    return Condition(field=field, operator=Operator.GREATER_THAN, value=value)


def greater_than_or_equal(field: str, value: Any) -> Condition:
    """Create a greater-than-or-equal condition."""
    return Condition(field=field, operator=Operator.GREATER_THAN_OR_EQUAL, value=value)


def less_than(field: str, value: Any) -> Condition:
    """Create a less-than condition."""
    return Condition(field=field, operator=Operator.LESS_THAN, value=value)


def less_than_or_equal(field: str, value: Any) -> Condition:
    """Create a less-than-or-equal condition."""
    return Condition(field=field, operator=Operator.LESS_THAN_OR_EQUAL, value=value)


def like(field: str, pattern: str) -> Condition:
    """Create a LIKE condition."""
    return Condition(field=field, operator=Operator.LIKE, value=pattern)


def not_like(field: str, pattern: str) -> Condition:
    """Create a NOT LIKE condition."""
    return Condition(field=field, operator=Operator.NOT_LIKE, value=pattern)


def in_(field: str, *values: Any) -> Condition:
    """Create an IN condition."""
    return Condition(field=field, operator=Operator.IN, values=list(values))


def not_in(field: str, *values: Any) -> Condition:
    """Create a NOT IN condition."""
    return Condition(field=field, operator=Operator.NOT_IN, values=list(values))


def between(field: str, start: Any, end: Any) -> Condition:
    """Create a BETWEEN condition."""
    return Condition(field=field, operator=Operator.BETWEEN, values=[start, end])


def not_between(field: str, start: Any, end: Any) -> Condition:
    """Create a NOT BETWEEN condition."""
    return Condition(field=field, operator=Operator.NOT_BETWEEN, values=[start, end])


def is_null(field: str) -> Condition:
    """Create an IS NULL condition."""
    return Condition(field=field, operator=Operator.IS_NULL)


def is_not_null(field: str) -> Condition:
    """Create an IS NOT NULL condition."""
    return Condition(field=field, operator=Operator.IS_NOT_NULL)


def exists(subquery: str) -> Condition:
    """Create an EXISTS condition."""
    return Condition(field="", operator=Operator.EXISTS, value=subquery)


def not_exists(subquery: str) -> Condition:
    """Create a NOT EXISTS condition."""
    return Condition(field="", operator=Operator.NOT_EXISTS, value=subquery)
