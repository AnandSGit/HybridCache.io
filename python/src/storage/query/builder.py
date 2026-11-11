"""
Query and command builders for the Database Agnostic Storage Library.

Mirrors ``internal/application/query`` of the Go implementation. ``build()``
renders SQL with ``?`` placeholders (SQL adapters translate them to the
database's native form), the parameters in placeholder order, and the
equivalent structured spec (``QuerySpec`` / ``CommandSpec``) that document
adapters execute.

Values are always bound as parameters unless wrapped with ``col``. Identifiers
(tables, columns, aliases) are validated against a strict grammar rather than
quoted, so the SQL stays dialect-neutral; invalid input raises
``InvalidQueryError``.
"""

from __future__ import annotations

import dataclasses
import re
from collections.abc import Iterable, Mapping
from typing import Any, Final, Self

from storage.errors import InvalidQueryError
from storage.types import (
    ColumnRef,
    Command,
    CommandSpec,
    CommandType,
    Condition,
    FieldValue,
    Join,
    JoinType,
    Operator,
    OrderBy,
    Query,
    QuerySpec,
    QueryType,
    SortDirection,
)

# Identifier grammar, the same as the Go builder's. Validation is what keeps
# identifiers taken from user input (for example an ORDER BY column) from
# injecting SQL. Patterns are applied with fullmatch (``$`` would accept a
# trailing newline), whitespace is Go's ``\s`` class, and re.ASCII keeps the
# case-insensitive keywords from matching non-ASCII look-alikes.
_IDENT_PART = r"[A-Za-z_][A-Za-z0-9_]*"
_IDENT = rf"{_IDENT_PART}(?:\.{_IDENT_PART})*"
_AGGREGATE = rf"(?i:COUNT|SUM|AVG|MIN|MAX)\((?:\*|{_IDENT})\)"
_SPACE = r"[\t\n\f\r ]+"

# A column or table name, optionally dot-qualified.
_IDENTIFIER: Final = re.compile(_IDENT, re.ASCII)
# An unqualified alias.
_ALIAS: Final = re.compile(_IDENT_PART, re.ASCII)
# A table with an optional alias ("orders o", "orders AS o"); group 1 is the
# bare table name.
_TABLE: Final = re.compile(
    rf"({_IDENT})(?:{_SPACE}(?:(?i:AS){_SPACE})?{_IDENT_PART})?", re.ASCII
)
# A select expression: *, t.*, or an identifier or aggregate optionally
# followed by "AS alias".
_SELECT: Final = re.compile(
    rf"\*|{_IDENT}\.\*"
    rf"|(?:{_IDENT}|{_AGGREGATE})(?:{_SPACE}(?i:AS){_SPACE}{_IDENT_PART})?",
    re.ASCII,
)
# The field of a condition: an identifier or an aggregate (for HAVING).
_FIELD: Final = re.compile(rf"{_IDENT}|{_AGGREGATE}", re.ASCII)

_OPERATOR_KEYWORDS: Final[Mapping[Operator, str]] = {
    Operator.EQUAL: "=",
    Operator.NOT_EQUAL: "!=",
    Operator.GREATER_THAN: ">",
    Operator.GREATER_THAN_OR_EQUAL: ">=",
    Operator.LESS_THAN: "<",
    Operator.LESS_THAN_OR_EQUAL: "<=",
    Operator.LIKE: "LIKE",
    Operator.NOT_LIKE: "NOT LIKE",
    Operator.IN: "IN",
    Operator.NOT_IN: "NOT IN",
    Operator.BETWEEN: "BETWEEN",
    Operator.NOT_BETWEEN: "NOT BETWEEN",
    Operator.IS_NULL: "IS NULL",
    Operator.IS_NOT_NULL: "IS NOT NULL",
    Operator.EXISTS: "EXISTS",
    Operator.NOT_EXISTS: "NOT EXISTS",
    Operator.AND: "AND",
    Operator.OR: "OR",
}

_JOIN_KEYWORDS: Final[Mapping[JoinType, str]] = {
    JoinType.INNER: "INNER JOIN",
    JoinType.LEFT: "LEFT JOIN",
    JoinType.RIGHT: "RIGHT JOIN",
    JoinType.FULL: "FULL OUTER JOIN",
    JoinType.CROSS: "CROSS JOIN",
}

_SORT_KEYWORDS: Final[Mapping[SortDirection, str]] = {
    SortDirection.ASC: "ASC",
    SortDirection.DESC: "DESC",
}


def _invalid_identifier(kind: str, value: object) -> InvalidQueryError:
    return InvalidQueryError(f"invalid {kind} {value!r}", context={"identifier": value})


def _check(pattern: re.Pattern[str], kind: str, value: object) -> str:
    """Return ``value`` if it is a string matching ``pattern``, else raise."""
    if isinstance(value, str) and pattern.fullmatch(value):
        return value
    raise _invalid_identifier(kind, value)


def _table_name(table: object) -> str:
    """Validate a table with an optional alias and return the bare table name."""
    match = _TABLE.fullmatch(table) if isinstance(table, str) else None
    if match is None:
        raise _invalid_identifier("table", table)
    return match.group(1)


def _as_int(name: str, value: object) -> int:
    """Return ``value`` as a plain int; bools and non-ints are rejected."""
    if isinstance(value, bool) or not isinstance(value, int):
        raise InvalidQueryError(
            f"{name} must be an int, got {type(value).__name__}", context={name: value}
        )
    return int(value)


def _order_term(order: OrderBy) -> str:
    field = _check(_IDENTIFIER, "ORDER BY field", order.field)
    direction = _SORT_KEYWORDS.get(order.direction)
    if direction is None:
        raise InvalidQueryError(f"unknown sort direction {order.direction!r}")
    return f"{field} {direction}"


def _clone_condition(condition: Condition) -> Condition:
    """Copy ``condition`` so that a built spec shares no lists with the builder."""
    return dataclasses.replace(
        condition,
        values=None if condition.values is None else list(condition.values),
        conditions=_clone_conditions(condition.conditions),
    )


def _clone_conditions(conditions: Iterable[Condition]) -> list[Condition]:
    return [_clone_condition(condition) for condition in conditions]


def _clone_join(join: Join) -> Join:
    on = None if join.on is None else _clone_condition(join.on)
    return dataclasses.replace(join, on=on)


class _Renderer:
    """Renders conditions with ``?`` placeholders, collecting the bound parameters
    in placeholder order. Shared by ``QueryBuilder`` and ``CommandBuilder``."""

    def __init__(self) -> None:
        self.params: list[Any] = []

    def conditions(self, conditions: Iterable[Condition], separator: str) -> str:
        return separator.join([self.condition(condition) for condition in conditions])

    def condition(self, condition: Condition) -> str:
        op = condition.operator
        keyword = _OPERATOR_KEYWORDS.get(op)
        if keyword is None:
            raise InvalidQueryError(f"unknown operator {op!r}")

        if op in (Operator.AND, Operator.OR):
            if not condition.conditions:
                raise InvalidQueryError(
                    f"{keyword} group requires at least one condition"
                )
            return f"({self.conditions(condition.conditions, f' {keyword} ')})"
        if op in (Operator.EXISTS, Operator.NOT_EXISTS):
            return f"{keyword} {self.subquery(condition.value, keyword)}"

        field = _check(_FIELD, "field", condition.field)
        if op in (Operator.IS_NULL, Operator.IS_NOT_NULL):
            return f"{field} {keyword}"
        if op in (Operator.IN, Operator.NOT_IN):
            if not condition.values:
                # "IN ()" is not valid SQL: an empty IN list matches no rows and
                # an empty NOT IN list matches every row.
                return "1=0" if op is Operator.IN else "1=1"
            return f"{field} {keyword} ({', '.join(self.operands(condition.values))})"
        if op in (Operator.BETWEEN, Operator.NOT_BETWEEN):
            values = condition.values or []
            if len(values) != 2:
                raise InvalidQueryError(
                    f"{keyword} requires exactly 2 values, got {len(values)}"
                )
            low, high = self.operands(values)
            return f"{field} {keyword} {low} AND {high}"
        # Comparison operators.
        return f"{field} {keyword} {self.operand(condition.value)}"

    def operand(self, value: Any) -> str:
        """Render a value: a ``ColumnRef`` as its validated column name, anything
        else as a ``?`` placeholder bound to the value."""
        if isinstance(value, ColumnRef):
            return _check(_IDENTIFIER, "column reference", value.name)
        self.params.append(value)
        return "?"

    def operands(self, values: Iterable[Any]) -> list[str]:
        return [self.operand(value) for value in values]

    def subquery(self, subquery: object, usage: str) -> str:
        """Render ``subquery`` in parentheses and bind its parameters in place."""
        if not isinstance(subquery, QueryBuilder):
            kind = type(subquery).__name__
            raise InvalidQueryError(
                f"{usage} requires a QueryBuilder subquery, got {kind}"
            )
        try:
            sql, params = subquery.build_raw()
        except InvalidQueryError as err:
            raise InvalidQueryError("invalid subquery") from err
        self.params.extend(params)
        return f"({sql})"

    def join(self, join: Join) -> str:
        """Render a JOIN clause; CROSS JOIN is the only join without ON."""
        keyword = _JOIN_KEYWORDS.get(join.join_type)
        if keyword is None:
            raise InvalidQueryError(f"unknown join type {join.join_type!r}")
        _table_name(join.table)
        if join.join_type is JoinType.CROSS:
            if join.on is not None:
                raise InvalidQueryError("CROSS JOIN does not take an ON condition")
            return f"{keyword} {join.table}"
        if join.on is None:
            raise InvalidQueryError(f"{keyword} requires an ON condition")
        return f"{keyword} {join.table} ON {self.condition(join.on)}"


class QueryBuilder:
    """Fluent builder for SELECT queries.

    Setters record state and return the builder; only ``limit``, ``offset`` and
    ``page`` validate their arguments immediately. ``build()`` validates and
    renders the state without modifying it, so it can be called repeatedly. The
    ``Query`` it returns has its own lists and condition objects (values are not
    copied), so changing the builder later does not change it.
    """

    def __init__(self) -> None:
        """Initialize an empty query builder."""
        self._fields: list[str] = []
        self._distinct = False
        self._table = ""
        self._subquery: QueryBuilder | None = None
        self._alias = ""
        self._joins: list[Join] = []
        self._where: list[Condition] = []
        self._group_by: list[str] = []
        self._having: list[Condition] = []
        self._order_by: list[OrderBy] = []
        self._limit: int | None = None
        self._offset: int | None = None

    def select(self, *fields: str) -> Self:
        """Add select expressions.

        Each is an identifier (optionally dot-qualified), ``*``, ``t.*``, or
        COUNT/SUM/AVG/MIN/MAX of an identifier or ``*``; identifiers and
        aggregates may be followed by ``AS alias``. A query without select
        expressions selects ``*``.
        """
        self._fields.extend(fields)
        return self

    def select_distinct(self, *fields: str) -> Self:
        """Add select expressions and make the query SELECT DISTINCT."""
        self._distinct = True
        return self.select(*fields)

    def select_count(self, field: str = "") -> Self:
        """Add ``COUNT(field)``, or ``COUNT(*)`` when ``field`` is empty.

        Like any select expression it replaces the implicit ``*``.
        """
        return self.select(f"COUNT({field or '*'})")

    def from_(self, table: str) -> Self:
        """Set the table, optionally with an alias ("users u" or "users AS u").

        It replaces a data source set earlier.
        """
        self._table = table
        self._subquery = None
        self._alias = ""
        return self

    def from_subquery(self, subquery: QueryBuilder, alias: str) -> Self:
        """Set a subquery as the data source, under ``alias``.

        Its parameters are bound before those of the outer query. It replaces a
        data source set earlier.
        """
        self._table = ""
        self._subquery = subquery
        self._alias = alias
        return self

    def join(
        self,
        table: str,
        condition: Condition | None = None,
        join_type: JoinType = JoinType.INNER,
    ) -> Self:
        """Add a join; ``table`` may carry an alias ("orders o", "orders AS o").

        Compare columns with ``col``, e.g. ``equal("o.user_id", col("u.id"))``:
        plain values are bound as parameters. ``condition`` is required, except
        for ``JoinType.CROSS``, which takes none.
        """
        self._joins.append(Join(join_type=join_type, table=table, on=condition))
        return self

    def inner_join(self, table: str, condition: Condition) -> Self:
        """Add an INNER JOIN."""
        return self.join(table, condition, JoinType.INNER)

    def left_join(self, table: str, condition: Condition) -> Self:
        """Add a LEFT JOIN."""
        return self.join(table, condition, JoinType.LEFT)

    def right_join(self, table: str, condition: Condition) -> Self:
        """Add a RIGHT JOIN."""
        return self.join(table, condition, JoinType.RIGHT)

    def where(self, condition: Condition) -> Self:
        """Add a WHERE condition; all WHERE conditions are combined with AND."""
        self._where.append(condition)
        return self

    def where_and(self, *conditions: Condition) -> Self:
        """Add WHERE conditions combined with AND."""
        self._where.extend(conditions)
        return self

    def where_or(self, *conditions: Condition) -> Self:
        """Add one parenthesised group ``(c1 OR c2 ...)``.

        The group is combined with the other WHERE conditions using AND. Without
        conditions this does nothing.
        """
        if conditions:
            self._where.append(or_(*conditions))
        return self

    def having(self, condition: Condition) -> Self:
        """Add a HAVING condition; HAVING conditions are combined with AND."""
        self._having.append(condition)
        return self

    def group_by(self, *fields: str) -> Self:
        """Add GROUP BY fields."""
        self._group_by.extend(fields)
        return self

    def order_by(
        self, field: str, direction: SortDirection = SortDirection.ASC
    ) -> Self:
        """Add an ORDER BY term."""
        self._order_by.append(OrderBy(field=field, direction=direction))
        return self

    def order_by_asc(self, field: str) -> Self:
        """Add an ascending ORDER BY term."""
        return self.order_by(field, SortDirection.ASC)

    def order_by_desc(self, field: str) -> Self:
        """Add a descending ORDER BY term."""
        return self.order_by(field, SortDirection.DESC)

    def limit(self, count: int) -> Self:
        """Set the LIMIT.

        Raises:
            InvalidQueryError: If ``count`` is not an int (bools included) or is
                negative.
        """
        limit = _as_int("limit", count)
        if limit < 0:
            raise InvalidQueryError(
                "limit must not be negative", context={"limit": limit}
            )
        self._limit = limit
        return self

    def offset(self, count: int) -> Self:
        """Set the OFFSET.

        Raises:
            InvalidQueryError: If ``count`` is not an int (bools included) or is
                negative.
        """
        offset = _as_int("offset", count)
        if offset < 0:
            raise InvalidQueryError(
                "offset must not be negative", context={"offset": offset}
            )
        self._offset = offset
        return self

    def page(self, page: int, size: int) -> Self:
        """Set LIMIT ``size`` and OFFSET ``(page - 1) * size``; ``page`` is 1-based.

        Raises:
            InvalidQueryError: If ``page`` or ``size`` is not an int (bools
                included) or is below 1.
        """
        number, page_size = _as_int("page", page), _as_int("size", size)
        if number < 1 or page_size < 1:
            raise InvalidQueryError(
                "page and page size must be at least 1",
                context={"page": number, "size": page_size},
            )
        self._limit = page_size
        self._offset = (number - 1) * page_size
        return self

    def build(self) -> Query:
        """Build the query: SQL, its parameters and the spec mirroring the SQL.

        ``spec.table`` is the bare table name (without alias); it is empty for
        ``from_subquery``, which sets ``spec.subquery`` instead.

        Raises:
            InvalidQueryError: If there is no data source, or an identifier,
                condition, join or subquery is invalid.
        """
        renderer = _Renderer()
        sql, spec = self._render(renderer)
        return Query(
            sql=sql,
            parameters=renderer.params,
            query_type=QueryType.SELECT,
            spec=spec,
        )

    def build_raw(self) -> tuple[str, list[Any]]:
        """Build the SQL and its parameters; raises like ``build()``."""
        query = self.build()
        return query.sql, query.parameters

    def _render(self, r: _Renderer) -> tuple[str, QuerySpec]:
        """Render the clauses in SQL order, so ``r`` binds parameters in
        placeholder order."""
        for field in self._fields:
            _check(_SELECT, "select expression", field)
        select_list = ", ".join(self._fields) or "*"
        distinct = "DISTINCT " if self._distinct else ""
        parts = [f"SELECT {distinct}{select_list}"]

        table = ""
        if self._subquery is not None:
            alias = _check(_ALIAS, "subquery alias", self._alias)
            parts.append(f"FROM {r.subquery(self._subquery, 'FROM')} AS {alias}")
        elif self._table:
            table = _table_name(self._table)
            parts.append(f"FROM {self._table}")
        else:
            raise InvalidQueryError("no table or subquery specified")

        for join in self._joins:
            parts.append(r.join(join))
        if self._where:
            parts.append(f"WHERE {r.conditions(self._where, ' AND ')}")
        if self._group_by:
            for field in self._group_by:
                _check(_IDENTIFIER, "GROUP BY field", field)
            parts.append(f"GROUP BY {', '.join(self._group_by)}")
        if self._having:
            parts.append(f"HAVING {r.conditions(self._having, ' AND ')}")
        if self._order_by:
            terms = [_order_term(order) for order in self._order_by]
            parts.append(f"ORDER BY {', '.join(terms)}")
        if self._limit is not None:
            parts.append(f"LIMIT {self._limit}")
        if self._offset is not None:
            parts.append(f"OFFSET {self._offset}")

        spec = QuerySpec(
            table=table,
            fields=[] if select_list == "*" else list(self._fields),
            distinct=self._distinct,
            where=_clone_conditions(self._where),
            order_by=[dataclasses.replace(order) for order in self._order_by],
            limit=self._limit,
            offset=self._offset,
            joins=[_clone_join(join) for join in self._joins],
            group_by=list(self._group_by),
            having=_clone_conditions(self._having),
            subquery=self._subquery is not None,
        )
        return " ".join(parts), spec


class CommandBuilder:
    """Fluent builder for INSERT, UPDATE and DELETE commands.

    Create one with ``insert``, ``update`` or ``delete``. As with
    ``QueryBuilder``, ``build()`` does not modify the builder and changing the
    builder later does not change the ``Command`` it returned. UPDATE and DELETE
    without WHERE are allowed and affect every row.
    """

    def __init__(self, command_type: CommandType, table: str) -> None:
        """Initialize a builder for a ``command_type`` command on ``table``."""
        self._command_type = command_type
        self._table = table
        self._values: dict[str, Any] = {}
        self._where: list[Condition] = []

    def set(self, field: str, value: Any) -> Self:
        """Assign a column value (INSERT and UPDATE).

        Columns are rendered in the order they were first set; setting a column
        again replaces its value in place.
        """
        self._values[field] = value
        return self

    def values(self, values: Mapping[str, Any]) -> Self:
        """Assign several column values, as ``set`` does, in iteration order."""
        self._values.update(values)
        return self

    def where(self, condition: Condition) -> Self:
        """Add a WHERE condition (UPDATE and DELETE), combined with AND."""
        self._where.append(condition)
        return self

    def where_and(self, *conditions: Condition) -> Self:
        """Add WHERE conditions combined with AND."""
        self._where.extend(conditions)
        return self

    def build(self) -> Command:
        """Build the command: SQL, its parameters and the spec mirroring the SQL.

        Raises:
            InvalidQueryError: If the table, a column or a condition is invalid,
                INSERT/UPDATE has no values, INSERT has WHERE conditions, DELETE
                has values, or the command type is not INSERT/UPDATE/DELETE.
        """
        renderer = _Renderer()
        sql = self._render(renderer)
        return Command(
            sql=sql,
            parameters=renderer.params,
            command_type=self._command_type,
            spec=CommandSpec(
                table=self._table,
                values=[
                    FieldValue(field, value) for field, value in self._values.items()
                ],
                where=_clone_conditions(self._where),
            ),
        )

    def build_raw(self) -> tuple[str, list[Any]]:
        """Build the SQL and its parameters; raises like ``build()``."""
        command = self.build()
        return command.sql, command.parameters

    def _render(self, r: _Renderer) -> str:
        if not self._table:
            raise InvalidQueryError("table name is required")
        table = _check(_IDENTIFIER, "table", self._table)

        # The identifiers are validated and every value is bound (S608).
        if self._command_type is CommandType.INSERT:
            if self._where:
                raise InvalidQueryError("INSERT does not support WHERE conditions")
            columns = self._columns(r)
            placeholders = ", ".join(["?"] * len(columns))
            sql = f"INSERT INTO {table} ({', '.join(columns)}) VALUES ({placeholders})"  # noqa: S608
        elif self._command_type is CommandType.UPDATE:
            assignments = ", ".join(f"{column} = ?" for column in self._columns(r))
            sql = f"UPDATE {table} SET {assignments}"  # noqa: S608
        elif self._command_type is CommandType.DELETE:
            if self._values:
                raise InvalidQueryError("DELETE does not support SET values")
            sql = f"DELETE FROM {table}"  # noqa: S608
        else:
            raise InvalidQueryError(f"unsupported command type {self._command_type!r}")

        if self._where:
            sql += f" WHERE {r.conditions(self._where, ' AND ')}"
        return sql

    def _columns(self, r: _Renderer) -> list[str]:
        """Validate the assigned columns and bind their values in order."""
        if not self._values:
            raise InvalidQueryError(
                f"{self._command_type.name} requires at least one value"
            )
        columns = []
        for field, value in self._values.items():
            columns.append(_check(_IDENTIFIER, "column", field))
            if isinstance(value, ColumnRef):
                raise InvalidQueryError(
                    "column references are only supported in conditions",
                    context={"column": field},
                )
            r.params.append(value)
        return columns


# Builders


def select(*fields: str) -> QueryBuilder:
    """Create a SELECT query builder (see ``QueryBuilder.select``)."""
    return QueryBuilder().select(*fields)


def insert(table: str) -> CommandBuilder:
    """Create an INSERT command builder."""
    return CommandBuilder(CommandType.INSERT, table)


def update(table: str) -> CommandBuilder:
    """Create an UPDATE command builder."""
    return CommandBuilder(CommandType.UPDATE, table)


def delete(table: str) -> CommandBuilder:
    """Create a DELETE command builder."""
    return CommandBuilder(CommandType.DELETE, table)


# Conditions. Values are bound as parameters; wrap a value with ``col`` to
# compare against a column instead, e.g. ``equal("o.user_id", col("u.id"))``.


def equal(field: str, value: Any) -> Condition:
    """Create a ``field = value`` condition."""
    return Condition(field=field, operator=Operator.EQUAL, value=value)


def not_equal(field: str, value: Any) -> Condition:
    """Create a ``field != value`` condition."""
    return Condition(field=field, operator=Operator.NOT_EQUAL, value=value)


def greater_than(field: str, value: Any) -> Condition:
    """Create a ``field > value`` condition."""
    return Condition(field=field, operator=Operator.GREATER_THAN, value=value)


def greater_than_or_equal(field: str, value: Any) -> Condition:
    """Create a ``field >= value`` condition."""
    return Condition(field=field, operator=Operator.GREATER_THAN_OR_EQUAL, value=value)


def less_than(field: str, value: Any) -> Condition:
    """Create a ``field < value`` condition."""
    return Condition(field=field, operator=Operator.LESS_THAN, value=value)


def less_than_or_equal(field: str, value: Any) -> Condition:
    """Create a ``field <= value`` condition."""
    return Condition(field=field, operator=Operator.LESS_THAN_OR_EQUAL, value=value)


def like(field: str, pattern: str) -> Condition:
    """Create a ``field LIKE pattern`` condition."""
    return Condition(field=field, operator=Operator.LIKE, value=pattern)


def not_like(field: str, pattern: str) -> Condition:
    """Create a ``field NOT LIKE pattern`` condition."""
    return Condition(field=field, operator=Operator.NOT_LIKE, value=pattern)


def in_(field: str, *values: Any) -> Condition:
    """Create a ``field IN (values...)`` condition; without values it matches no
    rows (rendered as ``1=0``)."""
    return Condition(field=field, operator=Operator.IN, values=list(values))


def not_in(field: str, *values: Any) -> Condition:
    """Create a ``field NOT IN (values...)`` condition; without values it matches
    every row (rendered as ``1=1``)."""
    return Condition(field=field, operator=Operator.NOT_IN, values=list(values))


def between(field: str, start: Any, end: Any) -> Condition:
    """Create a ``field BETWEEN start AND end`` condition."""
    return Condition(field=field, operator=Operator.BETWEEN, values=[start, end])


def not_between(field: str, start: Any, end: Any) -> Condition:
    """Create a ``field NOT BETWEEN start AND end`` condition."""
    return Condition(field=field, operator=Operator.NOT_BETWEEN, values=[start, end])


def is_null(field: str) -> Condition:
    """Create a ``field IS NULL`` condition."""
    return Condition(field=field, operator=Operator.IS_NULL)


def is_not_null(field: str) -> Condition:
    """Create a ``field IS NOT NULL`` condition."""
    return Condition(field=field, operator=Operator.IS_NOT_NULL)


def exists(subquery: QueryBuilder) -> Condition:
    """Create an ``EXISTS (subquery)`` condition; the subquery's parameters are
    bound in place."""
    return Condition(field="", operator=Operator.EXISTS, value=subquery)


def not_exists(subquery: QueryBuilder) -> Condition:
    """Create a ``NOT EXISTS (subquery)`` condition."""
    return Condition(field="", operator=Operator.NOT_EXISTS, value=subquery)


def and_(*conditions: Condition) -> Condition:
    """Group conditions as ``(c1 AND c2 ...)``; building an empty group fails."""
    return Condition(field="", operator=Operator.AND, conditions=list(conditions))


def or_(*conditions: Condition) -> Condition:
    """Group conditions as ``(c1 OR c2 ...)``; building an empty group fails."""
    return Condition(field="", operator=Operator.OR, conditions=list(conditions))


def col(name: str) -> ColumnRef:
    """Mark a condition value as a column reference, rendered as the (validated)
    column name instead of a bound parameter."""
    return ColumnRef(name)
