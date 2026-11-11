"""
Unit tests for the query and command builders.

The cases mirror internal/application/query/builder_test.go, so that the Go and
Python builders render the same SQL, parameters and specs.
"""

from __future__ import annotations

import re
import sqlite3
from collections.abc import Callable, Iterator
from typing import Any, cast

import pytest

import storage.query
from storage.errors import InvalidQueryError, QueryError, ValidationError
from storage.query import (
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
from storage.types import (
    ColumnRef,
    CommandSpec,
    CommandType,
    Condition,
    FieldValue,
    Join,
    JoinType,
    Operator,
    OrderBy,
    QuerySpec,
    QueryType,
    SortDirection,
)

# Subqueries shared by a builder and its expected spec: a spec holds the
# subquery builder itself, and builders compare by identity.
ORDERS_OVER_100 = select("user_id").from_("orders").where(greater_than("total", 100))
USER_ORDERS = (
    select("o.id")
    .from_("orders o")
    .where(equal("o.user_id", col("u.id")))
    .where(greater_than("o.total", 50))
)
HARD_BANS = (
    QueryBuilder()
    .from_("bans b")
    .where(equal("b.user_id", col("u.id")))
    .where(equal("b.kind", "hard"))
)


def complex_query() -> QueryBuilder:
    """The "users with order counts" query of the examples."""
    return (
        select("u.name", "u.email", "COUNT(o.id) as order_count")
        .from_("users u")
        .left_join("orders o", equal("o.user_id", col("u.id")))
        .where(equal("u.active", True))
        .where_or(greater_than("u.age", 18), is_not_null("u.guardian_id"))
        .group_by("u.id", "u.name", "u.email")
        .having(greater_than("COUNT(o.id)", 0))
        .order_by_desc("order_count")
        .limit(50)
        .offset(100)
    )


COMPLEX_SQL = (
    "SELECT u.name, u.email, COUNT(o.id) as order_count FROM users u "
    "LEFT JOIN orders o ON o.user_id = u.id "
    "WHERE u.active = ? AND (u.age > ? OR u.guardian_id IS NOT NULL) "
    "GROUP BY u.id, u.name, u.email HAVING COUNT(o.id) > ? "
    "ORDER BY order_count DESC LIMIT 50 OFFSET 100"
)


def complex_spec() -> QuerySpec:
    return QuerySpec(
        table="users",
        fields=["u.name", "u.email", "COUNT(o.id) as order_count"],
        joins=[Join(JoinType.LEFT, "orders o", equal("o.user_id", col("u.id")))],
        where=[
            equal("u.active", True),
            or_(greater_than("u.age", 18), is_not_null("u.guardian_id")),
        ],
        group_by=["u.id", "u.name", "u.email"],
        having=[greater_than("COUNT(o.id)", 0)],
        order_by=[OrderBy("order_count", SortDirection.DESC)],
        limit=50,
        offset=100,
    )


def valid() -> QueryBuilder:
    return QueryBuilder().from_("t")


def bogus(value: object) -> Any:
    """Pass a value of the wrong type, as untyped callers can."""
    return value


# QueryBuilder


BUILD_CASES = [
    pytest.param(
        lambda: QueryBuilder().from_("users"),
        "SELECT * FROM users",
        [],
        QuerySpec(table="users"),
        id="no fields selects star",
    ),
    pytest.param(
        lambda: select("*").from_("users"),
        "SELECT * FROM users",
        [],
        QuerySpec(table="users"),
        id="explicit star leaves spec fields empty",
    ),
    pytest.param(
        lambda: select("id", "name").select("email").from_("users"),
        "SELECT id, name, email FROM users",
        [],
        QuerySpec(table="users", fields=["id", "name", "email"]),
        id="fields accumulate in order",
    ),
    pytest.param(
        lambda: select("id").select().from_("users"),
        "SELECT id FROM users",
        [],
        QuerySpec(table="users", fields=["id"]),
        id="select without fields adds nothing",
    ),
    pytest.param(
        lambda: QueryBuilder().select_distinct("category").from_("products"),
        "SELECT DISTINCT category FROM products",
        [],
        QuerySpec(table="products", fields=["category"], distinct=True),
        id="select distinct",
    ),
    pytest.param(
        lambda: QueryBuilder().select_distinct().from_("products"),
        "SELECT DISTINCT * FROM products",
        [],
        QuerySpec(table="products", distinct=True),
        id="select distinct without fields",
    ),
    pytest.param(
        lambda: QueryBuilder().select_count().from_("users"),
        "SELECT COUNT(*) FROM users",
        [],
        QuerySpec(table="users", fields=["COUNT(*)"]),
        id="select count all",
    ),
    pytest.param(
        lambda: QueryBuilder().select_count("id").from_("users"),
        "SELECT COUNT(id) FROM users",
        [],
        QuerySpec(table="users", fields=["COUNT(id)"]),
        id="select count field",
    ),
    pytest.param(
        # examples/basic_usage.py; this used to render "SELECT *, COUNT(*)",
        # which PostgreSQL rejects with a GroupingError.
        lambda: select().select_count().from_("users"),
        "SELECT COUNT(*) FROM users",
        [],
        QuerySpec(table="users", fields=["COUNT(*)"]),
        id="select_count replaces the implicit star",
    ),
    pytest.param(
        lambda: (
            select("department").select_count().from_("users").group_by("department")
        ),
        "SELECT department, COUNT(*) FROM users GROUP BY department",
        [],
        QuerySpec(
            table="users",
            fields=["department", "COUNT(*)"],
            group_by=["department"],
        ),
        id="select_count adds to explicit fields",
    ),
    pytest.param(
        lambda: select(
            "u.*",
            "u.name AS user_name",
            "COUNT(o.id) as order_count",
            "sum(o.total) AS total",
            "MAX(o.created_at)",
            "AVG(*)",
        ).from_("users u"),
        "SELECT u.*, u.name AS user_name, COUNT(o.id) as order_count, "
        "sum(o.total) AS total, MAX(o.created_at), AVG(*) FROM users u",
        [],
        QuerySpec(
            table="users",
            fields=[
                "u.*",
                "u.name AS user_name",
                "COUNT(o.id) as order_count",
                "sum(o.total) AS total",
                "MAX(o.created_at)",
                "AVG(*)",
            ],
        ),
        id="select expressions with aliases",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users AS u"),
        "SELECT * FROM users AS u",
        [],
        QuerySpec(table="users"),
        id="table alias with AS",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("public.users pu"),
        "SELECT * FROM public.users pu",
        [],
        QuerySpec(table="public.users"),
        id="schema-qualified table with alias",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users\tu"),
        "SELECT * FROM users\tu",
        [],
        QuerySpec(table="users"),
        id="table alias after a tab",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users")
            .where(equal("active", True))
            .where(greater_than("age", 18))
            .where_and(less_than("age", 65), is_not_null("email"))
        ),
        "SELECT * FROM users "
        "WHERE active = ? AND age > ? AND age < ? AND email IS NOT NULL",
        [True, 18, 65],
        QuerySpec(
            table="users",
            where=[
                equal("active", True),
                greater_than("age", 18),
                less_than("age", 65),
                is_not_null("email"),
            ],
        ),
        id="where conditions are combined with AND",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users")
            .where(equal("active", True))
            .where_or(equal("role", "admin"), equal("role", "owner"))
        ),
        "SELECT * FROM users WHERE active = ? AND (role = ? OR role = ?)",
        [True, "admin", "owner"],
        QuerySpec(
            table="users",
            where=[
                equal("active", True),
                Condition(
                    field="",
                    operator=Operator.OR,
                    conditions=[equal("role", "admin"), equal("role", "owner")],
                ),
            ],
        ),
        id="where_or adds one parenthesised OR group",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("t")
            .where_or(equal("a", 1), equal("b", 2))
            .where_or(equal("c", 3), equal("d", 4))
        ),
        "SELECT * FROM t WHERE (a = ? OR b = ?) AND (c = ? OR d = ?)",
        [1, 2, 3, 4],
        QuerySpec(
            table="t",
            where=[
                or_(equal("a", 1), equal("b", 2)),
                or_(equal("c", 3), equal("d", 4)),
            ],
        ),
        id="each where_or call is a separate group",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("t").where_or(equal("a", 1)),
        "SELECT * FROM t WHERE (a = ?)",
        [1],
        QuerySpec(table="t", where=[or_(equal("a", 1))]),
        id="where_or with a single condition is still a group",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("t").where_or(),
        "SELECT * FROM t",
        [],
        QuerySpec(table="t"),
        id="where_or without conditions does nothing",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("t")
            .where(
                or_(
                    equal("a", 1),
                    and_(equal("b", 2), or_(equal("c", 3), is_null("d"))),
                )
            )
        ),
        "SELECT * FROM t WHERE (a = ? OR (b = ? AND (c = ? OR d IS NULL)))",
        [1, 2, 3],
        QuerySpec(
            table="t",
            where=[
                or_(
                    equal("a", 1),
                    and_(equal("b", 2), or_(equal("c", 3), is_null("d"))),
                )
            ],
        ),
        id="groups nest and are parenthesised",
    ),
    pytest.param(
        lambda: (
            select("u.name", "p.title")
            .from_("users u")
            .inner_join("posts p", equal("p.user_id", col("u.id")))
        ),
        "SELECT u.name, p.title FROM users u INNER JOIN posts p ON p.user_id = u.id",
        [],
        QuerySpec(
            table="users",
            fields=["u.name", "p.title"],
            joins=[Join(JoinType.INNER, "posts p", equal("p.user_id", col("u.id")))],
        ),
        id="inner join compares columns with col",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users u")
            .left_join("orders o", equal("o.user_id", col("u.id")))
            .right_join("teams AS tm", equal("tm.id", col("u.team_id")))
            .join("profiles pr", equal("pr.user_id", col("u.id")), JoinType.FULL)
            .join("regions r", join_type=JoinType.CROSS)
        ),
        "SELECT * FROM users u LEFT JOIN orders o ON o.user_id = u.id "
        "RIGHT JOIN teams AS tm ON tm.id = u.team_id "
        "FULL OUTER JOIN profiles pr ON pr.user_id = u.id CROSS JOIN regions r",
        [],
        QuerySpec(
            table="users",
            joins=[
                Join(JoinType.LEFT, "orders o", equal("o.user_id", col("u.id"))),
                Join(JoinType.RIGHT, "teams AS tm", equal("tm.id", col("u.team_id"))),
                Join(JoinType.FULL, "profiles pr", equal("pr.user_id", col("u.id"))),
                Join(JoinType.CROSS, "regions r"),
            ],
        ),
        id="every join type; CROSS JOIN has no ON",
    ),
    pytest.param(
        # The reviewer's reproduction: parameters used to be ['seven', 5].
        lambda: (
            select("a.y")
            .from_("a")
            .inner_join("b", equal("b.x", 5))
            .where(equal("a.y", "seven"))
        ),
        "SELECT a.y FROM a INNER JOIN b ON b.x = ? WHERE a.y = ?",
        [5, "seven"],
        QuerySpec(
            table="a",
            fields=["a.y"],
            joins=[Join(JoinType.INNER, "b", equal("b.x", 5))],
            where=[equal("a.y", "seven")],
        ),
        id="join parameters precede where parameters",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users u")
            .inner_join(
                "orders o",
                and_(equal("o.user_id", col("u.id")), greater_than("o.total", 100)),
            )
            .where(equal("u.active", True))
        ),
        "SELECT * FROM users u INNER JOIN orders o "
        "ON (o.user_id = u.id AND o.total > ?) WHERE u.active = ?",
        [100, True],
        QuerySpec(
            table="users",
            joins=[
                Join(
                    JoinType.INNER,
                    "orders o",
                    and_(equal("o.user_id", col("u.id")), greater_than("o.total", 100)),
                )
            ],
            where=[equal("u.active", True)],
        ),
        id="join group parameters precede where parameters",
    ),
    pytest.param(
        lambda: (
            select("user_id", "COUNT(*) AS orders")
            .from_("orders")
            .group_by("user_id")
            .having(greater_than("COUNT(*)", 5))
            .having(less_than("SUM(total)", 1000))
        ),
        "SELECT user_id, COUNT(*) AS orders FROM orders GROUP BY user_id "
        "HAVING COUNT(*) > ? AND SUM(total) < ?",
        [5, 1000],
        QuerySpec(
            table="orders",
            fields=["user_id", "COUNT(*) AS orders"],
            group_by=["user_id"],
            having=[greater_than("COUNT(*)", 5), less_than("SUM(total)", 1000)],
        ),
        id="having conditions accumulate with AND",
    ),
    pytest.param(
        lambda: (
            select("department", "COUNT(*) as user_count")
            .from_("users")
            .where(equal("active", True))
            .group_by("department")
            .having(greater_than("COUNT(*)", 1))
            .order_by("user_count", SortDirection.DESC)
            .limit(5)
        ),
        "SELECT department, COUNT(*) as user_count FROM users WHERE active = ? "
        "GROUP BY department HAVING COUNT(*) > ? ORDER BY user_count DESC LIMIT 5",
        [True, 1],
        QuerySpec(
            table="users",
            fields=["department", "COUNT(*) as user_count"],
            where=[equal("active", True)],
            group_by=["department"],
            having=[greater_than("COUNT(*)", 1)],
            order_by=[OrderBy("user_count", SortDirection.DESC)],
            limit=5,
        ),
        id="where parameters precede having parameters",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users")
            .order_by("name")
            .order_by("age", SortDirection.DESC)
            .order_by_desc("created_at")
            .order_by_asc("u.id")
        ),
        "SELECT * FROM users ORDER BY name ASC, age DESC, created_at DESC, u.id ASC",
        [],
        QuerySpec(
            table="users",
            order_by=[
                OrderBy("name", SortDirection.ASC),
                OrderBy("age", SortDirection.DESC),
                OrderBy("created_at", SortDirection.DESC),
                OrderBy("u.id", SortDirection.ASC),
            ],
        ),
        id="order by",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").limit(10).offset(20),
        "SELECT * FROM users LIMIT 10 OFFSET 20",
        [],
        QuerySpec(table="users", limit=10, offset=20),
        id="limit and offset",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").offset(20),
        "SELECT * FROM users OFFSET 20",
        [],
        QuerySpec(table="users", offset=20),
        id="offset without limit",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").limit(0).offset(0),
        "SELECT * FROM users LIMIT 0 OFFSET 0",
        [],
        QuerySpec(table="users", limit=0, offset=0),
        id="zero limit and offset are valid",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").page(3, 10),
        "SELECT * FROM users LIMIT 10 OFFSET 20",
        [],
        QuerySpec(table="users", limit=10, offset=20),
        id="page is 1-based",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").page(1, 25),
        "SELECT * FROM users LIMIT 25 OFFSET 0",
        [],
        QuerySpec(table="users", limit=25, offset=0),
        id="first page",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").page(2, 10).limit(5),
        "SELECT * FROM users LIMIT 5 OFFSET 10",
        [],
        QuerySpec(table="users", limit=5, offset=10),
        id="limit after page replaces the page size",
    ),
    pytest.param(
        lambda: (
            select("s.user_id")
            .from_subquery(ORDERS_OVER_100, "s")
            .where(not_equal("s.user_id", 7))
        ),
        "SELECT s.user_id FROM (SELECT user_id FROM orders WHERE total > ?) AS s "
        "WHERE s.user_id != ?",
        [100, 7],
        QuerySpec(
            fields=["s.user_id"],
            where=[not_equal("s.user_id", 7)],
            subquery=True,
        ),
        id="from_subquery binds subquery parameters first",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(ORDERS_OVER_100, "s").from_("users"),
        "SELECT * FROM users",
        [],
        QuerySpec(table="users"),
        id="from_ replaces an earlier from_subquery",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users").from_subquery(ORDERS_OVER_100, "s"),
        "SELECT * FROM (SELECT user_id FROM orders WHERE total > ?) AS s",
        [100],
        QuerySpec(subquery=True),
        id="from_subquery replaces an earlier from_",
    ),
    pytest.param(
        lambda: (
            QueryBuilder()
            .from_("users u")
            .where(equal("u.active", True))
            .where(exists(USER_ORDERS))
            .where(not_exists(HARD_BANS))
            .where(equal("u.age", 30))
        ),
        "SELECT * FROM users u WHERE u.active = ? "
        "AND EXISTS (SELECT o.id FROM orders o WHERE o.user_id = u.id AND o.total > ?) "
        "AND NOT EXISTS (SELECT * FROM bans b WHERE b.user_id = u.id AND b.kind = ?) "
        "AND u.age = ?",
        [True, 50, "hard", 30],
        QuerySpec(
            table="users",
            where=[
                equal("u.active", True),
                exists(USER_ORDERS),
                not_exists(HARD_BANS),
                equal("u.age", 30),
            ],
        ),
        id="exists subqueries bind their parameters in order",
    ),
    pytest.param(
        complex_query,
        COMPLEX_SQL,
        [True, 18, 0],
        complex_spec(),
        id="complex query",
    ),
]


@pytest.mark.parametrize(("make", "sql", "params", "spec"), BUILD_CASES)
def test_build(
    make: Callable[[], QueryBuilder], sql: str, params: list[Any], spec: QuerySpec
) -> None:
    query = make().build()

    assert query.sql == sql
    assert query.parameters == params
    assert query.query_type is QueryType.SELECT
    assert query.timeout is None
    assert query.spec == spec
    assert make().build_raw() == (sql, params)


OPERATOR_CASES = [
    pytest.param(equal("a", 1), "a = ?", [1], id="equal"),
    pytest.param(equal("a", None), "a = ?", [None], id="equal None is bound"),
    pytest.param(not_equal("a", 1), "a != ?", [1], id="not equal"),
    pytest.param(greater_than("a", 1), "a > ?", [1], id="greater than"),
    pytest.param(greater_than_or_equal("a", 1), "a >= ?", [1], id="greater or equal"),
    pytest.param(less_than("a", 1), "a < ?", [1], id="less than"),
    pytest.param(less_than_or_equal("a", 1), "a <= ?", [1], id="less or equal"),
    pytest.param(like("name", "%jo%"), "name LIKE ?", ["%jo%"], id="like"),
    pytest.param(not_like("name", "%jo%"), "name NOT LIKE ?", ["%jo%"], id="not like"),
    pytest.param(
        in_("status", "a", "b", "c"),
        "status IN (?, ?, ?)",
        ["a", "b", "c"],
        id="in",
    ),
    pytest.param(
        not_in("status", "a", "b"), "status NOT IN (?, ?)", ["a", "b"], id="not in"
    ),
    pytest.param(in_("status"), "1=0", [], id="empty in matches no rows"),
    pytest.param(not_in("status"), "1=1", [], id="empty not in matches all rows"),
    pytest.param(
        Condition("status", Operator.IN), "1=0", [], id="in without a values list"
    ),
    pytest.param(between("age", 18, 65), "age BETWEEN ? AND ?", [18, 65], id="between"),
    pytest.param(
        not_between("age", 18, 65),
        "age NOT BETWEEN ? AND ?",
        [18, 65],
        id="not between",
    ),
    pytest.param(is_null("deleted_at"), "deleted_at IS NULL", [], id="is null"),
    pytest.param(is_not_null("t.email"), "t.email IS NOT NULL", [], id="is not null"),
    pytest.param(equal("a", col("b")), "a = b", [], id="column = column"),
    pytest.param(not_equal("a", col("t.b")), "a != t.b", [], id="column != column"),
    pytest.param(greater_than("a", col("b")), "a > b", [], id="column > column"),
    pytest.param(
        greater_than_or_equal("a", col("b")), "a >= b", [], id="column >= column"
    ),
    pytest.param(less_than("a", col("b")), "a < b", [], id="column < column"),
    pytest.param(
        less_than_or_equal("a", col("b")), "a <= b", [], id="column <= column"
    ),
    pytest.param(
        Condition("a", Operator.LIKE, col("b")), "a LIKE b", [], id="column LIKE column"
    ),
    pytest.param(
        Condition("a", Operator.NOT_LIKE, col("b")),
        "a NOT LIKE b",
        [],
        id="column NOT LIKE column",
    ),
    pytest.param(
        in_("a", 1, col("t.b"), 2), "a IN (?, t.b, ?)", [1, 2], id="column in IN list"
    ),
    pytest.param(
        between("a", col("t.lo"), 10),
        "a BETWEEN t.lo AND ?",
        [10],
        id="column as BETWEEN bound",
    ),
    pytest.param(
        equal("a", "b.c"), "a = ?", ["b.c"], id="a string that looks like a column"
    ),
    pytest.param(
        greater_than("COUNT(*)", 5), "COUNT(*) > ?", [5], id="aggregate field"
    ),
    pytest.param(
        and_(equal("a", 1), is_null("b")),
        "(a = ? AND b IS NULL)",
        [1],
        id="and group",
    ),
    pytest.param(
        or_(equal("a", 1), equal("b", 2)), "(a = ? OR b = ?)", [1, 2], id="or group"
    ),
    pytest.param(
        exists(select("id").from_("t2").where(equal("x", 5))),
        "EXISTS (SELECT id FROM t2 WHERE x = ?)",
        [5],
        id="exists",
    ),
    pytest.param(
        not_exists(select("id").from_("t2").where(equal("x", 5))),
        "NOT EXISTS (SELECT id FROM t2 WHERE x = ?)",
        [5],
        id="not exists",
    ),
]


@pytest.mark.parametrize(("condition", "where_sql", "params"), OPERATOR_CASES)
def test_operator(condition: Condition, where_sql: str, params: list[Any]) -> None:
    query = QueryBuilder().from_("t").where(condition).build()

    # S608: this f-string is the expected SQL, not a query being built.
    assert query.sql == f"SELECT * FROM t WHERE {where_sql}"  # noqa: S608
    assert query.parameters == params
    assert query.spec is not None
    assert query.spec.where == [condition]


def test_operator_cases_cover_every_operator() -> None:
    covered = {cast(Condition, case.values[0]).operator for case in OPERATOR_CASES}
    assert covered == set(Operator)


QUERY_ERROR_CASES = [
    pytest.param(
        lambda: QueryBuilder(), "no table or subquery specified", id="no data source"
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users; DROP TABLE users"),
        "invalid table 'users; DROP TABLE users'",
        id="table injection",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users u x"), "invalid table", id="two aliases"
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users u.x"),
        "invalid table",
        id="qualified table alias",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users\n"),
        "invalid table",
        id="table with a trailing newline",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users\vu"),
        "invalid table",
        id="alias after a vertical tab",
    ),
    pytest.param(
        lambda: QueryBuilder().from_("users\N{NO-BREAK SPACE}u"),
        "invalid table",
        id="alias after a no-break space",
    ),
    pytest.param(
        lambda: QueryBuilder().from_(bogus(42)),
        "invalid table 42",
        id="table not a str",
    ),
    pytest.param(
        lambda: valid().select("id; DROP TABLE users"),
        "invalid select expression 'id; DROP TABLE users'",
        id="select injection",
    ),
    pytest.param(
        lambda: valid().select("lower(name)"),
        "invalid select expression",
        id="select function call",
    ),
    pytest.param(
        lambda: valid().select("1"), "invalid select expression", id="select literal"
    ),
    pytest.param(
        lambda: valid().select("id, name"),
        "invalid select expression",
        id="select comma list",
    ),
    pytest.param(
        lambda: valid().select("* AS everything"),
        "invalid select expression",
        id="alias on star",
    ),
    pytest.param(
        lambda: valid().select("name AS"),
        "invalid select expression",
        id="missing alias",
    ),
    pytest.param(
        lambda: valid().select("name n"),
        "invalid select expression",
        id="alias without AS",
    ),
    pytest.param(
        lambda: valid().select("\N{LATIN SMALL LETTER LONG S}um(total)"),
        "invalid select expression",
        id="aggregate spelled with a non-ASCII look-alike",
    ),
    pytest.param(
        lambda: valid().select(bogus(None)),
        "invalid select expression None",
        id="select expression not a str",
    ),
    pytest.param(
        lambda: valid().select_count("DISTINCT id"),
        "invalid select expression 'COUNT(DISTINCT id)'",
        id="count distinct",
    ),
    pytest.param(
        lambda: valid().select_count("*) FROM secrets --"),
        "invalid select expression",
        id="count injection",
    ),
    pytest.param(
        lambda: valid().where(equal("id = 1 OR 1=1 --", 1)),
        "invalid field 'id = 1 OR 1=1 --'",
        id="where field injection",
    ),
    pytest.param(
        lambda: valid().where(equal("", 1)), "invalid field ''", id="empty where field"
    ),
    pytest.param(
        lambda: valid().order_by_asc("name; DROP TABLE users"),
        "invalid ORDER BY field",
        id="order by injection",
    ),
    pytest.param(
        lambda: valid().order_by_desc("name DESC"),
        "invalid ORDER BY field",
        id="order by with direction text",
    ),
    pytest.param(
        lambda: valid().order_by("name", bogus("up")),
        "unknown sort direction 'up'",
        id="unknown sort direction",
    ),
    pytest.param(
        lambda: valid().group_by("id", "id) --"),
        "invalid GROUP BY field 'id) --'",
        id="group by injection",
    ),
    pytest.param(
        lambda: valid().inner_join("posts p ON 1=1 --", equal("p.id", col("t.id"))),
        "invalid table",
        id="join table injection",
    ),
    pytest.param(
        lambda: valid().inner_join("posts p", equal("p.user_id", col("t.id OR 1=1"))),
        "invalid column reference 't.id OR 1=1'",
        id="column reference injection",
    ),
    pytest.param(
        lambda: valid().join("posts p"),
        "INNER JOIN requires an ON condition",
        id="join without ON condition",
    ),
    pytest.param(
        lambda: valid().left_join("posts p", bogus(None)),
        "LEFT JOIN requires an ON condition",
        id="left join with None",
    ),
    pytest.param(
        lambda: valid().join("regions r", equal("a", 1), JoinType.CROSS),
        "CROSS JOIN does not take an ON condition",
        id="cross join with ON condition",
    ),
    pytest.param(
        lambda: valid().join("posts p", equal("p.id", col("t.id")), bogus("sideways")),
        "unknown join type 'sideways'",
        id="unknown join type",
    ),
    pytest.param(
        lambda: valid().left_join("posts p", Condition("p.id", bogus("bogus"))),
        "unknown operator 'bogus'",
        id="invalid ON condition",
    ),
    pytest.param(
        lambda: valid().having(greater_than("COUNT(*) OR 1=1", 0)),
        "invalid field",
        id="invalid having field",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(ORDERS_OVER_100, ""),
        "invalid subquery alias ''",
        id="empty subquery alias",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(ORDERS_OVER_100, "s; DROP TABLE users"),
        "invalid subquery alias",
        id="subquery alias injection",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(ORDERS_OVER_100, "a.b"),
        "invalid subquery alias",
        id="qualified subquery alias",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(QueryBuilder(), "s"),
        "invalid subquery",
        id="invalid subquery",
    ),
    pytest.param(
        lambda: QueryBuilder().from_subquery(bogus("SELECT 1; DROP TABLE t"), "s"),
        "FROM requires a QueryBuilder subquery, got str",
        id="from_subquery with SQL text",
    ),
    pytest.param(
        lambda: valid().where(exists(bogus("SELECT 1; DROP TABLE users"))),
        "EXISTS requires a QueryBuilder subquery, got str",
        id="exists with SQL text",
    ),
    pytest.param(
        lambda: valid().where(exists(bogus(None))),
        "EXISTS requires a QueryBuilder subquery, got NoneType",
        id="exists with None",
    ),
    pytest.param(
        lambda: valid().where(not_exists(QueryBuilder().from_("bad table!"))),
        "invalid subquery",
        id="not exists with invalid subquery",
    ),
    pytest.param(
        lambda: valid().where(Condition("id", bogus("bogus"), 1)),
        "unknown operator 'bogus'",
        id="unknown operator",
    ),
    pytest.param(
        lambda: valid().where(Condition("age", Operator.BETWEEN, values=[18])),
        "BETWEEN requires exactly 2 values, got 1",
        id="between with one value",
    ),
    pytest.param(
        lambda: valid().where(Condition("age", Operator.BETWEEN, value=18)),
        "BETWEEN requires exactly 2 values, got 0",
        id="between with value only",
    ),
    pytest.param(
        lambda: valid().where(Condition("age", Operator.BETWEEN, values=[1, 2, 3])),
        "BETWEEN requires exactly 2 values, got 3",
        id="between with three values",
    ),
    pytest.param(
        lambda: valid().where(Condition("age", Operator.NOT_BETWEEN, values=[18])),
        "NOT BETWEEN requires exactly 2 values, got 1",
        id="not between with one value",
    ),
    pytest.param(
        lambda: valid().where(or_()),
        "OR group requires at least one condition",
        id="empty OR group",
    ),
    pytest.param(
        lambda: valid().where(or_(equal("a", 1), and_())),
        "AND group requires at least one condition",
        id="empty nested AND group",
    ),
    pytest.param(
        lambda: valid().where_or(equal("a", 1), equal("b; --", 2)),
        "invalid field 'b; --'",
        id="invalid condition in OR group",
    ),
    pytest.param(
        lambda: valid().where(in_("id", col("x y"))),
        "invalid column reference 'x y'",
        id="invalid column in IN list",
    ),
    pytest.param(
        lambda: valid().where(between("x", col("ok"), col("bad name"))),
        "invalid column reference 'bad name'",
        id="invalid column as BETWEEN bound",
    ),
]


@pytest.mark.parametrize(("make", "message"), QUERY_ERROR_CASES)
def test_build_rejects_invalid_queries(
    make: Callable[[], QueryBuilder], message: str
) -> None:
    with pytest.raises(InvalidQueryError, match=re.escape(message)):
        make().build()
    with pytest.raises(InvalidQueryError, match=re.escape(message)):
        make().build_raw()


def test_invalid_query_error_is_a_validation_error_naming_the_identifier() -> None:
    with pytest.raises(InvalidQueryError) as excinfo:
        valid().order_by("name; DROP TABLE users").build()

    error = excinfo.value
    assert isinstance(error, QueryError)
    assert isinstance(error, ValidationError)
    assert error.code == "INVALID_QUERY"
    assert error.context == {"identifier": "name; DROP TABLE users"}


def test_invalid_subquery_keeps_its_cause() -> None:
    with pytest.raises(InvalidQueryError, match="invalid subquery") as excinfo:
        valid().where(exists(QueryBuilder().from_("bad table!"))).build()

    cause = excinfo.value.__cause__
    assert isinstance(cause, InvalidQueryError)
    assert cause.message == "invalid table 'bad table!'"
    assert "caused by: INVALID_QUERY: invalid table" in str(excinfo.value)


PAGINATION_ERROR_CASES = [
    pytest.param(lambda b: b.limit(-1), "limit must not be negative", id="limit -1"),
    pytest.param(
        lambda b: b.limit(True), "limit must be an int, got bool", id="limit True"
    ),
    pytest.param(
        lambda b: b.limit(bogus("1; DROP TABLE t")),
        "limit must be an int, got str",
        id="limit injection",
    ),
    pytest.param(
        lambda b: b.limit(bogus(1.5)), "limit must be an int, got float", id="limit 1.5"
    ),
    pytest.param(
        lambda b: b.limit(bogus(None)),
        "limit must be an int, got NoneType",
        id="limit None",
    ),
    pytest.param(lambda b: b.offset(-5), "offset must not be negative", id="offset -5"),
    pytest.param(
        lambda b: b.offset(False), "offset must be an int, got bool", id="offset False"
    ),
    pytest.param(
        lambda b: b.offset(bogus("0; DROP TABLE t")),
        "offset must be an int, got str",
        id="offset injection",
    ),
    pytest.param(
        lambda b: b.page(0, 10), "page and page size must be at least 1", id="page 0"
    ),
    pytest.param(
        lambda b: b.page(-2, 10), "page and page size must be at least 1", id="page -2"
    ),
    pytest.param(
        lambda b: b.page(1, 0), "page and page size must be at least 1", id="size 0"
    ),
    pytest.param(
        lambda b: b.page(2, -10), "page and page size must be at least 1", id="size -10"
    ),
    pytest.param(
        lambda b: b.page(True, 10), "page must be an int, got bool", id="page True"
    ),
    pytest.param(
        lambda b: b.page(1, bogus("10")), "size must be an int, got str", id="size str"
    ),
]


@pytest.mark.parametrize(("action", "message"), PAGINATION_ERROR_CASES)
def test_pagination_rejects_invalid_arguments_immediately(
    action: Callable[[QueryBuilder], object], message: str
) -> None:
    builder = valid().limit(10).offset(5)

    with pytest.raises(InvalidQueryError, match=re.escape(message)):
        action(builder)

    # The rejected argument leaves the builder as it was.
    assert builder.build().sql == "SELECT * FROM t LIMIT 10 OFFSET 5"


class _SneakyInt(int):
    """An int whose text form is not its value."""

    def __str__(self) -> str:
        return "1; DROP TABLE t"

    __repr__ = __str__

    def __format__(self, format_spec: str) -> str:
        return "1; DROP TABLE t"


def test_pagination_renders_int_subclasses_as_plain_ints() -> None:
    query = valid().limit(_SneakyInt(1)).offset(_SneakyInt(2)).build()

    assert query.sql == "SELECT * FROM t LIMIT 1 OFFSET 2"
    assert query.spec is not None
    assert type(query.spec.limit) is int
    assert type(query.spec.offset) is int

    paged = valid().page(_SneakyInt(3), _SneakyInt(4)).build()
    assert paged.sql == "SELECT * FROM t LIMIT 4 OFFSET 8"


def test_build_is_repeatable_and_does_not_store_the_implicit_star() -> None:
    builder = QueryBuilder().from_("users")
    first, second = builder.build(), builder.build()
    assert first == second
    assert first.sql == "SELECT * FROM users"

    query = builder.select("id").build()
    assert query.sql == "SELECT id FROM users"
    assert query.spec is not None
    assert query.spec.fields == ["id"]

    complex_builder = complex_query()
    assert complex_builder.build() == complex_builder.build()
    assert complex_builder.build_raw() == (COMPLEX_SQL, [True, 18, 0])


def test_built_query_is_independent_of_the_builder() -> None:
    ors = [equal("u.a", 1), equal("u.b", 2)]
    roles = ["admin", "owner"]

    def configure() -> QueryBuilder:
        return (
            select("u.id")
            .from_("users u")
            .inner_join("posts p", in_("p.kind", *roles))
            .where(in_("u.role", *roles))
            .where_or(*ors)
            .group_by("u.id")
            .having(greater_than("COUNT(*)", 1))
            .order_by_asc("u.id")
            .limit(10)
            .offset(5)
        )

    want = configure().build()

    # Changing the builder does not change an already-built query.
    builder = configure()
    query = builder.build()
    assert query == want
    (
        builder.select("u.email")
        .where(equal("x", 1))
        .where_or(equal("y", 2))
        .inner_join("tags t", equal("t.id", col("p.tag_id")))
        .group_by("u.email")
        .having(less_than("COUNT(*)", 9))
        .order_by_desc("u.email")
        .limit(1)
        .offset(2)
        .build()
    )
    assert query == want

    # Changing a built query does not change the builder.
    builder = configure()
    query = builder.build()
    spec = query.spec
    assert spec is not None
    assert spec.where[0].values is not None
    assert spec.joins[0].on is not None
    assert spec.joins[0].on.values is not None
    spec.fields[0] = "hacked"
    spec.where[0].values[0] = "hacked"
    spec.where[1].conditions[0] = equal("hacked", 0)
    spec.joins[0].on.values[0] = "hacked"
    spec.group_by[0] = "hacked"
    spec.having[0].value = "hacked"
    spec.order_by[0].field = "hacked"
    query.parameters[0] = "hacked"
    assert builder.build() == want

    # Replacing an item of the caller's list does not reach the where_or group.
    builder = configure()
    ors[0] = equal("hacked", 0)
    spec = builder.build().spec
    assert spec is not None
    assert spec.where[1].conditions[0] == equal("u.a", 1)


# Condition helpers


SUBQUERY = QueryBuilder().from_("orders")
A, B = equal("a", 1), is_null("b")

HELPER_CASES = [
    pytest.param(equal("id", 1), Condition("id", Operator.EQUAL, 1), id="equal"),
    pytest.param(
        not_equal("status", "deleted"),
        Condition("status", Operator.NOT_EQUAL, "deleted"),
        id="not_equal",
    ),
    pytest.param(
        greater_than("age", 18),
        Condition("age", Operator.GREATER_THAN, 18),
        id="greater_than",
    ),
    pytest.param(
        greater_than_or_equal("age", 18),
        Condition("age", Operator.GREATER_THAN_OR_EQUAL, 18),
        id="greater_than_or_equal",
    ),
    pytest.param(
        less_than("price", 100.0),
        Condition("price", Operator.LESS_THAN, 100.0),
        id="less_than",
    ),
    pytest.param(
        less_than_or_equal("price", 100.0),
        Condition("price", Operator.LESS_THAN_OR_EQUAL, 100.0),
        id="less_than_or_equal",
    ),
    pytest.param(
        like("name", "%john%"), Condition("name", Operator.LIKE, "%john%"), id="like"
    ),
    pytest.param(
        not_like("name", "%john%"),
        Condition("name", Operator.NOT_LIKE, "%john%"),
        id="not_like",
    ),
    pytest.param(
        in_("status", "active", "pending"),
        Condition("status", Operator.IN, values=["active", "pending"]),
        id="in_",
    ),
    pytest.param(
        in_("status"), Condition("status", Operator.IN, values=[]), id="in_ no values"
    ),
    pytest.param(
        not_in("status", "deleted"),
        Condition("status", Operator.NOT_IN, values=["deleted"]),
        id="not_in",
    ),
    pytest.param(
        between("age", 18, 65),
        Condition("age", Operator.BETWEEN, values=[18, 65]),
        id="between",
    ),
    pytest.param(
        not_between("age", 18, 65),
        Condition("age", Operator.NOT_BETWEEN, values=[18, 65]),
        id="not_between",
    ),
    pytest.param(
        is_null("deleted_at"), Condition("deleted_at", Operator.IS_NULL), id="is_null"
    ),
    pytest.param(
        is_not_null("email"), Condition("email", Operator.IS_NOT_NULL), id="is_not_null"
    ),
    pytest.param(
        exists(SUBQUERY), Condition("", Operator.EXISTS, SUBQUERY), id="exists"
    ),
    pytest.param(
        not_exists(SUBQUERY),
        Condition("", Operator.NOT_EXISTS, SUBQUERY),
        id="not_exists",
    ),
    pytest.param(and_(A, B), Condition("", Operator.AND, conditions=[A, B]), id="and_"),
    pytest.param(or_(A, B), Condition("", Operator.OR, conditions=[A, B]), id="or_"),
    pytest.param(
        equal("p.user_id", col("u.id")),
        Condition("p.user_id", Operator.EQUAL, ColumnRef("u.id")),
        id="col",
    ),
]


@pytest.mark.parametrize(("got", "want"), HELPER_CASES)
def test_condition_helpers(got: Condition, want: Condition) -> None:
    assert got == want


def test_query_package_exports_the_builder_api() -> None:
    for name in storage.query.__all__:
        assert getattr(storage.query, name) is getattr(storage.query.builder, name)
    assert {"and_", "or_", "col", "QueryBuilder", "CommandBuilder"} <= set(
        storage.query.__all__
    )


# CommandBuilder


COMMAND_CASES = [
    pytest.param(
        lambda: (
            insert("users")
            .set("name", "John Doe")
            .set("email", "john@example.com")
            .set("age", 30)
        ),
        CommandType.INSERT,
        "INSERT INTO users (name, email, age) VALUES (?, ?, ?)",
        ["John Doe", "john@example.com", 30],
        CommandSpec(
            table="users",
            values=[
                FieldValue("name", "John Doe"),
                FieldValue("email", "john@example.com"),
                FieldValue("age", 30),
            ],
        ),
        id="insert keeps set order",
    ),
    pytest.param(
        lambda: insert("users").set("name", "a").set("age", 1).set("name", "b"),
        CommandType.INSERT,
        "INSERT INTO users (name, age) VALUES (?, ?)",
        ["b", 1],
        CommandSpec(
            table="users", values=[FieldValue("name", "b"), FieldValue("age", 1)]
        ),
        id="setting a column again replaces its value in place",
    ),
    pytest.param(
        lambda: insert("users").values(
            {"name": "John", "email": "john@example.com", "age": 30}
        ),
        CommandType.INSERT,
        "INSERT INTO users (name, email, age) VALUES (?, ?, ?)",
        ["John", "john@example.com", 30],
        CommandSpec(
            table="users",
            values=[
                FieldValue("name", "John"),
                FieldValue("email", "john@example.com"),
                FieldValue("age", 30),
            ],
        ),
        id="values keeps the mapping's order",
    ),
    pytest.param(
        lambda: (
            insert("public.users")
            .set("name", "x")
            .set("zip", "1")
            .values({"name": "Alice", "age": 28})
        ),
        CommandType.INSERT,
        "INSERT INTO public.users (name, zip, age) VALUES (?, ?, ?)",
        ["Alice", "1", 28],
        CommandSpec(
            table="public.users",
            values=[
                FieldValue("name", "Alice"),
                FieldValue("zip", "1"),
                FieldValue("age", 28),
            ],
        ),
        id="values keeps the position of columns already set",
    ),
    pytest.param(
        lambda: (
            update("users").set("name", "Jane Doe").set("age", 25).where(equal("id", 1))
        ),
        CommandType.UPDATE,
        "UPDATE users SET name = ?, age = ? WHERE id = ?",
        ["Jane Doe", 25, 1],
        CommandSpec(
            table="users",
            values=[FieldValue("name", "Jane Doe"), FieldValue("age", 25)],
            where=[equal("id", 1)],
        ),
        id="update binds SET values before WHERE values",
    ),
    pytest.param(
        lambda: (
            update("users")
            .values({"name": "Jane", "email": "jane@example.com"})
            .where(equal("id", 1))
        ),
        CommandType.UPDATE,
        "UPDATE users SET name = ?, email = ? WHERE id = ?",
        ["Jane", "jane@example.com", 1],
        CommandSpec(
            table="users",
            values=[
                FieldValue("name", "Jane"),
                FieldValue("email", "jane@example.com"),
            ],
            where=[equal("id", 1)],
        ),
        id="update with values",
    ),
    pytest.param(
        lambda: (
            update("users")
            .set("active", False)
            .where(equal("department", "sales"))
            .where(less_than("last_login", "2023-01-01"))
        ),
        CommandType.UPDATE,
        "UPDATE users SET active = ? WHERE department = ? AND last_login < ?",
        [False, "sales", "2023-01-01"],
        CommandSpec(
            table="users",
            values=[FieldValue("active", False)],
            where=[equal("department", "sales"), less_than("last_login", "2023-01-01")],
        ),
        id="update with several WHERE conditions",
    ),
    pytest.param(
        lambda: (
            update("accounts")
            .set("status", "locked")
            .where(equal("active", True))
            .where(
                or_(
                    greater_than("failed_logins", 5),
                    less_than("locked_until", col("created_at")),
                )
            )
        ),
        CommandType.UPDATE,
        "UPDATE accounts SET status = ? WHERE active = ? "
        "AND (failed_logins > ? OR locked_until < created_at)",
        ["locked", True, 5],
        CommandSpec(
            table="accounts",
            values=[FieldValue("status", "locked")],
            where=[
                equal("active", True),
                or_(
                    greater_than("failed_logins", 5),
                    less_than("locked_until", col("created_at")),
                ),
            ],
        ),
        id="update with OR group and column reference",
    ),
    pytest.param(
        # The builder does not guard against an unconditional UPDATE (same as
        # Go): this statement changes every row of the table.
        lambda: update("users").set("active", False),
        CommandType.UPDATE,
        "UPDATE users SET active = ?",
        [False],
        CommandSpec(table="users", values=[FieldValue("active", False)]),
        id="update without WHERE renders an unconditional UPDATE",
    ),
    pytest.param(
        lambda: (
            delete("users")
            .where(equal("active", False))
            .where_and(less_than("last_login", "2023-01-01"), is_null("deleted_at"))
        ),
        CommandType.DELETE,
        "DELETE FROM users WHERE active = ? AND last_login < ? AND deleted_at IS NULL",
        [False, "2023-01-01"],
        CommandSpec(
            table="users",
            where=[
                equal("active", False),
                less_than("last_login", "2023-01-01"),
                is_null("deleted_at"),
            ],
        ),
        id="delete with WHERE",
    ),
    pytest.param(
        # As for UPDATE: no guard, this statement deletes every row.
        lambda: delete("sessions"),
        CommandType.DELETE,
        "DELETE FROM sessions",
        [],
        CommandSpec(table="sessions"),
        id="delete without WHERE renders an unconditional DELETE",
    ),
    pytest.param(
        lambda: delete("users").where(in_("id", 1, 2)).where(not_exists(USER_ORDERS)),
        CommandType.DELETE,
        "DELETE FROM users WHERE id IN (?, ?) AND NOT EXISTS "
        "(SELECT o.id FROM orders o WHERE o.user_id = u.id AND o.total > ?)",
        [1, 2, 50],
        CommandSpec(table="users", where=[in_("id", 1, 2), not_exists(USER_ORDERS)]),
        id="delete with IN and NOT EXISTS",
    ),
]


@pytest.mark.parametrize(
    ("make", "command_type", "sql", "params", "spec"), COMMAND_CASES
)
def test_command_build(
    make: Callable[[], CommandBuilder],
    command_type: CommandType,
    sql: str,
    params: list[Any],
    spec: CommandSpec,
) -> None:
    command = make().build()

    assert command.command_type is command_type
    assert command.sql == sql
    assert command.parameters == params
    assert command.timeout is None
    assert command.spec == spec
    assert make().build_raw() == (sql, params)


COMMAND_ERROR_CASES = [
    pytest.param(
        lambda: insert("").set("name", "x"),
        "table name is required",
        id="insert without table",
    ),
    pytest.param(
        lambda: update("").set("name", "x"),
        "table name is required",
        id="update without table",
    ),
    pytest.param(
        lambda: delete("").where(equal("id", 1)),
        "table name is required",
        id="delete without table",
    ),
    pytest.param(
        lambda: insert("users; DROP TABLE users").set("name", "x"),
        "invalid table 'users; DROP TABLE users'",
        id="table injection",
    ),
    pytest.param(
        lambda: update("users u").set("name", "x"), "invalid table", id="table alias"
    ),
    pytest.param(
        lambda: insert("users"),
        "INSERT requires at least one value",
        id="insert without values",
    ),
    pytest.param(
        lambda: update("users").where(equal("id", 1)),
        "UPDATE requires at least one value",
        id="update without values",
    ),
    pytest.param(
        lambda: insert("users").set("name", "x").where(equal("id", 1)),
        "INSERT does not support WHERE conditions",
        id="insert with WHERE",
    ),
    pytest.param(
        lambda: delete("users").set("name", "x"),
        "DELETE does not support SET values",
        id="delete with values",
    ),
    pytest.param(
        lambda: insert("users").set("name) VALUES ('x'); --", "x"),
        "invalid column",
        id="insert column injection",
    ),
    pytest.param(
        lambda: update("users").set("name = 'x', admin", True),
        "invalid column",
        id="update column injection",
    ),
    pytest.param(
        lambda: update("users").set("updated_at", col("created_at")),
        "column references are only supported in conditions",
        id="column reference as value",
    ),
    pytest.param(
        lambda: update("users").set("a", 1).where(equal("id; --", 1)),
        "invalid field",
        id="invalid where field",
    ),
    pytest.param(
        lambda: delete("users").where(Condition("id", bogus("bogus"))),
        "unknown operator 'bogus'",
        id="unknown operator",
    ),
    pytest.param(
        lambda: delete("users").where(Condition("age", Operator.BETWEEN, values=[1])),
        "BETWEEN requires exactly 2 values, got 1",
        id="between with one value",
    ),
    pytest.param(
        lambda: delete("users").where(or_()),
        "OR group requires at least one condition",
        id="empty OR group",
    ),
    pytest.param(
        lambda: CommandBuilder(CommandType.CREATE, "users"),
        "unsupported command type",
        id="unsupported command type",
    ),
]


@pytest.mark.parametrize(("make", "message"), COMMAND_ERROR_CASES)
def test_command_build_rejects_invalid_commands(
    make: Callable[[], CommandBuilder], message: str
) -> None:
    with pytest.raises(InvalidQueryError, match=re.escape(message)):
        make().build()
    with pytest.raises(InvalidQueryError, match=re.escape(message)):
        make().build_raw()


def test_built_command_is_independent_of_the_builder() -> None:
    def configure() -> CommandBuilder:
        return update("users").set("name", "Jane").set("age", 25).where(in_("id", 1, 2))

    want = configure().build()

    # Setting a column again replaces it in place inside the builder; the built
    # command must not see that.
    builder = configure()
    command = builder.build()
    assert command == want
    builder.set("name", "changed").set("email", "e").where(equal("x", 1)).build()
    assert command == want

    builder = configure()
    command = builder.build()
    assert command.spec is not None
    assert command.spec.where[0].values is not None
    command.spec.values[0].value = "hacked"
    command.spec.where[0].values[0] = "hacked"
    command.parameters[0] = "hacked"
    assert builder.build() == want


def test_command_build_is_repeatable() -> None:
    builder = (
        update("users").set("name", "Jane").where(or_(equal("id", 1), equal("id", 2)))
    )

    first, second = builder.build(), builder.build()
    assert first == second
    assert builder.build_raw() == (first.sql, first.parameters)


# The builder's SQL on a real engine: SQLite takes ``?`` placeholders as they
# are, so these check that the SQL is valid and the parameters line up with it.


@pytest.fixture
def db() -> Iterator[sqlite3.Connection]:
    connection = sqlite3.connect(":memory:")
    connection.executescript(
        """
        CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER,
                            active INTEGER);
        CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, total INTEGER);
        INSERT INTO users VALUES (1, 'ann', 30, 1), (2, 'bob', 17, 1), (3, 'cy', 45, 0);
        INSERT INTO orders VALUES (1, 1, 120), (2, 1, 80), (3, 2, 300), (4, 3, 10);
        """
    )
    yield connection
    connection.close()


SQLITE_CASES = [
    pytest.param(
        lambda: (
            select("u.name")
            .from_("users u")
            .inner_join(
                "orders o",
                and_(equal("o.user_id", col("u.id")), greater_than("o.total", 100)),
            )
            .where(equal("u.active", True))
            .order_by_asc("u.name")
        ),
        [("ann",), ("bob",)],
        id="join parameters are bound before where parameters",
    ),
    pytest.param(
        lambda: (
            select("u.name", "COUNT(o.id) AS n")
            .from_("users u")
            .left_join("orders o", equal("o.user_id", col("u.id")))
            .where_or(
                greater_than("u.age", 40),
                exists(
                    select("x.id")
                    .from_("orders x")
                    .where(equal("x.user_id", col("u.id")))
                    .where(greater_than("x.total", 200))
                ),
            )
            .group_by("u.name")
            .having(greater_than("COUNT(o.id)", 0))
            .order_by_asc("u.name")
        ),
        [("bob", 1), ("cy", 1)],
        id="OR group, EXISTS subquery, GROUP BY and HAVING",
    ),
    pytest.param(
        lambda: (
            select("s.user_id")
            .from_subquery(
                select("user_id").from_("orders").where(greater_than("total", 100)), "s"
            )
            .where(not_equal("s.user_id", 2))
        ),
        [(1,)],
        id="subquery parameters are bound first",
    ),
    pytest.param(
        lambda: select().select_count().from_("users").where(between("age", 18, 50)),
        [(2,)],
        id="select_count",
    ),
    pytest.param(
        lambda: select("name").from_("users").order_by_asc("id").page(2, 2),
        [("cy",)],
        id="page",
    ),
]


@pytest.mark.parametrize(("make", "rows"), SQLITE_CASES)
def test_query_runs_on_sqlite(
    db: sqlite3.Connection,
    make: Callable[[], QueryBuilder],
    rows: list[tuple[Any, ...]],
) -> None:
    sql, params = make().build_raw()
    assert db.execute(sql, params).fetchall() == rows


def test_commands_run_on_sqlite(db: sqlite3.Connection) -> None:
    commands = [
        insert("users").set("name", "dee").set("age", 51).set("active", 1),
        update("users")
        .set("active", 0)
        .where(greater_than("age", 40))
        .where(not_equal("name", "cy")),
        delete("users").where(in_("name", "bob", "nobody")),
    ]
    for command in commands:
        db.execute(*command.build_raw())

    sql, params = (
        select("name", "active").from_("users").order_by_asc("name").build_raw()
    )
    assert db.execute(sql, params).fetchall() == [("ann", 1), ("cy", 0), ("dee", 0)]
