"""
Query builder package for the Database Agnostic Storage Library.

This package provides a fluent API for building database queries and commands
in a database-agnostic way.
"""

from storage.query.builder import (
    QueryBuilder,
    CommandBuilder,
    # Condition helpers
    equal,
    not_equal,
    greater_than,
    greater_than_or_equal,
    less_than,
    less_than_or_equal,
    like,
    not_like,
    in_,
    not_in,
    between,
    not_between,
    is_null,
    is_not_null,
    exists,
    not_exists,
    # Builder functions
    select,
    insert,
    update,
    delete,
)

__all__ = [
    "QueryBuilder",
    "CommandBuilder",
    # Condition helpers
    "equal",
    "not_equal", 
    "greater_than",
    "greater_than_or_equal",
    "less_than",
    "less_than_or_equal",
    "like",
    "not_like",
    "in_",
    "not_in",
    "between",
    "not_between",
    "is_null",
    "is_not_null",
    "exists",
    "not_exists",
    # Builder functions
    "select",
    "insert",
    "update",
    "delete",
]
