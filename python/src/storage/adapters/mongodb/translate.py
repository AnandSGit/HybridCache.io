"""
Translation of builder-produced queries and commands into MongoDB operations.

MongoDB cannot execute SQL text, so only the structured ``Query.spec`` /
``Command.spec`` that the query builder populates is translated. Raw SQL and the
SQL-only features of a spec (joins, GROUP BY, HAVING, subqueries) raise
``UnsupportedQueryError``; they are never parsed or ignored. The translation table
is the one of the Go adapter (``internal/infrastructure/adapters/mongodb``).
"""

from __future__ import annotations

import enum
import json
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Any

import bson
from bson import json_util
from bson.errors import BSONError

from storage.errors import InvalidQueryError, UnsupportedQueryError
from storage.types import (
    ColumnRef,
    Command,
    CommandType,
    Condition,
    FieldValue,
    Operator,
    Query,
    QuerySpec,
    SortDirection,
)

#: The single column of a COUNT result.
COUNT_COLUMN = "count"

# The select expression emitted by the query builder's select_count.
_COUNT_EXPRESSION = re.compile(r"^COUNT\(\s*([^()\s]+)\s*\)$", re.IGNORECASE)

# Characters that make a field name a SQL expression rather than a field path.
_EXPRESSION_CHARACTERS = frozenset(" \t\r\n()*,'\"\x00")

_COMPARISONS = {
    Operator.EQUAL: "$eq",
    Operator.NOT_EQUAL: "$ne",
    Operator.GREATER_THAN: "$gt",
    Operator.GREATER_THAN_OR_EQUAL: "$gte",
    Operator.LESS_THAN: "$lt",
    Operator.LESS_THAN_OR_EQUAL: "$lte",
}
_MEMBERSHIPS = {Operator.IN: "$in", Operator.NOT_IN: "$nin"}
_SORT_ORDERS = {SortDirection.ASC: 1, SortDirection.DESC: -1}


class QueryKind(enum.Enum):
    """The MongoDB operation a query runs as."""

    FIND = "find"
    COUNT = "count"
    DISTINCT = "distinct"


@dataclass
class QueryPlan:
    """The MongoDB form of a ``QuerySpec``."""

    collection: str
    criteria: dict[str, Any]
    kind: QueryKind = QueryKind.FIND
    # Result columns in select order; empty means whole documents.
    columns: tuple[str, ...] = ()
    projection: dict[str, int] | None = None
    sort: list[tuple[str, int]] = field(default_factory=list)
    skip: int = 0
    limit: int = 0  # 0: no limit
    empty: bool = False  # LIMIT 0: the result is empty and nothing is sent


@dataclass
class CommandPlan:
    """The MongoDB form of a ``CommandSpec``."""

    command_type: CommandType
    collection: str
    # INSERT: the new document; UPDATE: the values to $set.
    document: dict[str, Any] = field(default_factory=dict)
    criteria: dict[str, Any] = field(default_factory=dict)  # UPDATE and DELETE

    def update(self) -> dict[str, Any]:
        """Return the update document of an UPDATE."""
        return {"$set": self.document}


def _unsupported(feature: str) -> UnsupportedQueryError:
    return UnsupportedQueryError(f"{feature} is not supported by the MongoDB adapter")


def plan_query(query: Query) -> QueryPlan:
    """Translate a builder-produced query.

    Raises:
        UnsupportedQueryError: For raw SQL (no ``spec``) and SQL-only features.
        InvalidQueryError: If the spec is malformed.
    """
    spec = query.spec
    if spec is None:
        raise UnsupportedQueryError(
            "MongoDB requires a query built with the query builder (Query.spec); "
            "raw SQL cannot be executed"
        )
    if spec.joins:
        raise _unsupported("JOIN")
    if spec.group_by:
        raise _unsupported("GROUP BY")
    if spec.having:
        raise _unsupported("HAVING")
    if spec.subquery:
        raise _unsupported("selecting from a subquery")
    if not spec.table:
        raise InvalidQueryError("query has no table")

    plan = QueryPlan(collection=spec.table, criteria=build_filter(spec.where))
    _select(plan, spec)
    if plan.kind is not QueryKind.FIND and (
        spec.order_by or spec.limit is not None or spec.offset is not None
    ):
        raise _unsupported("ORDER BY, LIMIT or OFFSET combined with COUNT or DISTINCT")
    _paginate(plan, spec)
    return plan


def _select(plan: QueryPlan, spec: QuerySpec) -> None:
    """Set the plan's kind, result columns and projection from the select list."""
    fields = [] if spec.fields == ["*"] else list(spec.fields)
    if len(fields) == 1 and (match := _COUNT_EXPRESSION.match(fields[0])):
        if spec.distinct:
            raise _unsupported("DISTINCT COUNT")
        plan.kind, plan.columns = QueryKind.COUNT, (COUNT_COLUMN,)
        counted = match.group(1)
        if counted != "*":
            # COUNT(field) counts the documents whose field is not null.
            check_field(counted)
            plan.criteria = _and(plan.criteria, {counted: {"$ne": None}})
        return
    for name in fields:
        check_field(name)
    if spec.distinct:
        if len(fields) != 1:
            raise _unsupported("DISTINCT on other than exactly one field")
        plan.kind, plan.columns = QueryKind.DISTINCT, (fields[0],)
        return
    if not fields:
        return
    plan.columns = tuple(fields)
    plan.projection = dict.fromkeys(fields, 1)
    if "_id" not in plan.projection:
        # Like SQL, return only the selected columns.
        plan.projection["_id"] = 0


def _paginate(plan: QueryPlan, spec: QuerySpec) -> None:
    for term in spec.order_by:
        check_field(term.field)
        order = _SORT_ORDERS.get(term.direction)
        if order is None:
            raise InvalidQueryError(f"unknown sort direction {term.direction!r}")
        plan.sort.append((term.field, order))
    if spec.offset is not None:
        if spec.offset < 0:
            raise InvalidQueryError("OFFSET must not be negative")
        plan.skip = spec.offset
    if spec.limit is not None:
        if spec.limit < 0:
            raise InvalidQueryError("LIMIT must not be negative")
        # MongoDB treats limit 0 as "no limit"; SQL LIMIT 0 returns no rows.
        plan.limit = spec.limit
        plan.empty = spec.limit == 0


def plan_command(command: Command) -> CommandPlan:
    """Translate a builder-produced command.

    Translation failures raise, so that an UPDATE or DELETE can never broaden to
    every document.

    Raises:
        UnsupportedQueryError: For raw SQL (no ``spec``) and other command types
            than INSERT, UPDATE and DELETE.
        InvalidQueryError: If the spec is malformed.
    """
    spec = command.spec
    if spec is None:
        raise UnsupportedQueryError(
            "MongoDB requires a command built with the command builder "
            "(Command.spec); raw SQL cannot be executed"
        )
    if not spec.table:
        raise InvalidQueryError("command has no table")

    plan = CommandPlan(command.command_type, spec.table)
    if command.command_type is CommandType.INSERT:
        if spec.where:
            raise InvalidQueryError("INSERT does not take WHERE conditions")
        plan.document = build_document(spec.values)
    elif command.command_type is CommandType.UPDATE:
        plan.document = build_document(spec.values)
        plan.criteria = build_filter(spec.where)
    elif command.command_type is CommandType.DELETE:
        if spec.values:
            raise InvalidQueryError("DELETE does not take values")
        plan.criteria = build_filter(spec.where)
    else:
        raise _unsupported(f"command type {command.command_type.value!r}")
    return plan


def build_document(values: list[FieldValue]) -> dict[str, Any]:
    """Return the document of the ordered column assignments ``values``."""
    if not values:
        raise InvalidQueryError("command has no values")
    document: dict[str, Any] = {}
    for item in values:
        check_field(item.field)
        _check_value(item.value)
        if item.field in document:
            raise InvalidQueryError(f"field {item.field!r} is assigned more than once")
        document[item.field] = item.value
    return document


def build_filter(conditions: list[Condition]) -> dict[str, Any]:
    """Translate AND-combined conditions into a filter document.

    Several conditions become an explicit ``$and``, so that repeated fields cannot
    collide.
    """
    parts = [translate_condition(condition) for condition in conditions]
    if not parts:
        return {}
    if len(parts) == 1:
        return parts[0]
    return {"$and": parts}


def translate_condition(condition: Condition) -> dict[str, Any]:
    """Translate one condition into a filter document."""
    operator = condition.operator
    if operator in (Operator.AND, Operator.OR):
        if not condition.conditions:
            raise InvalidQueryError("empty AND/OR condition group")
        group = "$and" if operator is Operator.AND else "$or"
        return {group: [translate_condition(c) for c in condition.conditions]}
    if operator in (Operator.EXISTS, Operator.NOT_EXISTS):
        raise _unsupported("EXISTS subqueries")

    name = condition.field
    check_field(name)
    values = condition.values or []
    for value in (condition.value, *values):
        _check_value(value)

    if operator in _COMPARISONS:
        return {name: {_COMPARISONS[operator]: condition.value}}
    if operator in _MEMBERSHIPS:
        return {name: {_MEMBERSHIPS[operator]: list(values)}}
    if operator in (Operator.BETWEEN, Operator.NOT_BETWEEN):
        if len(values) != 2:
            raise InvalidQueryError(f"BETWEEN on {name!r} needs exactly two values")
        low, high = values
        if operator is Operator.BETWEEN:
            return {name: {"$gte": low, "$lte": high}}
        return {"$or": [{name: {"$lt": low}}, {name: {"$gt": high}}]}
    if operator in (Operator.LIKE, Operator.NOT_LIKE):
        if not isinstance(condition.value, str):
            raise InvalidQueryError(f"LIKE on {name!r} needs a string pattern")
        # "s" lets "." match newlines, as SQL wildcards do.
        match = {"$regex": like_regex(condition.value), "$options": "s"}
        return {name: match} if operator is Operator.LIKE else {name: {"$not": match}}
    if operator is Operator.IS_NULL:
        # Matches null and missing fields, the document equivalent of SQL NULL.
        return {name: None}
    if operator is Operator.IS_NOT_NULL:
        return {name: {"$ne": None}}
    raise InvalidQueryError(f"unknown operator {operator!r}")


def like_regex(pattern: str) -> str:
    """Convert a SQL LIKE pattern into an anchored regular expression.

    ``%`` matches any sequence, ``_`` any single character, and ``\\`` escapes the
    next character (PostgreSQL's default escape); everything else matches
    literally.
    """
    parts = ["^"]
    escaped = False
    for char in pattern:
        if escaped:
            parts.append(re.escape(char))
            escaped = False
        elif char == "\\":
            escaped = True
        elif char == "%":
            parts.append(".*")
        elif char == "_":
            parts.append(".")
        else:
            parts.append(re.escape(char))
    if escaped:
        raise InvalidQueryError("LIKE pattern must not end with the escape character")
    # \z rather than $, which would also match before a trailing newline.
    parts.append(r"\z")
    return "".join(parts)


def check_field(name: str) -> None:
    """Accept plain, optionally dotted, field paths.

    SQL expressions such as functions, aliases and wildcards have no MongoDB
    translation, and a ``$`` prefix would be interpreted as an operator.
    """
    if not name:
        raise InvalidQueryError("empty field name")
    if name.startswith("$"):
        raise InvalidQueryError(f"field name {name!r} must not start with '$'")
    if not _EXPRESSION_CHARACTERS.isdisjoint(name):
        raise UnsupportedQueryError(
            f"expression {name!r} is not supported by the MongoDB adapter: "
            "only plain field paths are"
        )


def _check_value(value: Any) -> None:
    if isinstance(value, ColumnRef):
        raise _unsupported("comparing with a column reference")


def _and(criteria: dict[str, Any], extra: dict[str, Any]) -> dict[str, Any]:
    return {"$and": [criteria, extra]} if criteria else extra


def render_query(plan: QueryPlan) -> str:
    """Return a deterministic, mongosh-style rendering of a query plan."""
    collection = _collection(plan.collection)
    criteria = _json(plan.criteria)
    if plan.kind is QueryKind.COUNT:
        return f"{collection}.countDocuments({criteria})"
    if plan.kind is QueryKind.DISTINCT:
        return f"{collection}.distinct({json.dumps(plan.columns[0])}, {criteria})"
    text = f"{collection}.find({criteria}"
    if plan.projection is not None:
        text += f", {_json(plan.projection)}"
    text += ")"
    if plan.sort:
        text += f".sort({_json(dict(plan.sort))})"
    if plan.skip:
        text += f".skip({plan.skip})"
    if plan.limit:
        text += f".limit({plan.limit})"
    if plan.empty:
        text += " /* LIMIT 0: no documents are read */"
    return text


def render_command(plan: CommandPlan) -> str:
    """Return a deterministic, mongosh-style rendering of a command plan."""
    collection = _collection(plan.collection)
    if plan.command_type is CommandType.INSERT:
        return f"{collection}.insertOne({_json(plan.document)})"
    if plan.command_type is CommandType.UPDATE:
        return (
            f"{collection}.updateMany({_json(plan.criteria)}, {_json(plan.update())})"
        )
    return f"{collection}.deleteMany({_json(plan.criteria)})"


def _collection(name: str) -> str:
    return f"db.getCollection({json.dumps(name, ensure_ascii=False)})"


def _json(document: Mapping[str, Any]) -> str:
    try:
        bson.encode(document)
    except (BSONError, OverflowError, ValueError) as exc:
        raise InvalidQueryError("value cannot be encoded as BSON") from exc
    return json_util.dumps(document, separators=(",", ":"), ensure_ascii=False)
