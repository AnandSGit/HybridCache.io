"""
Query builder package for the Database Agnostic Storage Library.

Fluent builders for SELECT queries and INSERT/UPDATE/DELETE commands, plus the
condition helpers. ``build()`` returns a ``Query``/``Command`` holding both SQL
with ``?`` placeholders and the structured spec, so the result runs on SQL and
document adapters alike.
"""

from storage.query.builder import (
    CommandBuilder,
    QueryBuilder,
    and_,
    between,
    col,
    delete,
    equal,
    exists,
    greater_than,
    greater_than_or_equal,
    in_,
    insert,
    is_not_null,
    is_null,
    less_than,
    less_than_or_equal,
    like,
    not_between,
    not_equal,
    not_exists,
    not_in,
    not_like,
    or_,
    select,
    update,
)

__all__ = [
    "CommandBuilder",
    "QueryBuilder",
    "and_",
    "between",
    "col",
    "delete",
    "equal",
    "exists",
    "greater_than",
    "greater_than_or_equal",
    "in_",
    "insert",
    "is_not_null",
    "is_null",
    "less_than",
    "less_than_or_equal",
    "like",
    "not_between",
    "not_equal",
    "not_exists",
    "not_in",
    "not_like",
    "or_",
    "select",
    "update",
]
