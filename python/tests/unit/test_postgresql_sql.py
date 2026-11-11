"""Unit tests for the PostgreSQL SQL helpers: placeholders, quoting, command tags."""

from __future__ import annotations

import datetime
import decimal
import uuid
from typing import Any

import pytest

from storage.adapters.postgresql.sql import (
    convert_placeholders,
    has_statement_separator,
    quote_identifier,
    quote_literal,
    quote_table,
    rows_affected,
    split_table_name,
)
from storage.errors import InvalidParametersError, InvalidSchemaError


@pytest.mark.parametrize(
    ("sql", "expected"),
    [
        (
            "SELECT * FROM t WHERE a = ? AND b = ?",
            "SELECT * FROM t WHERE a = $1 AND b = $2",
        ),
        ("INSERT INTO t (a, b) VALUES (?, ?)", "INSERT INTO t (a, b) VALUES ($1, $2)"),
        ("SELECT 1", "SELECT 1"),
        ("", ""),
        # string literals, including doubled quotes and E'' escapes
        ("SELECT 'what?' , ? FROM t", "SELECT 'what?' , $1 FROM t"),
        ("SELECT 'it''s ?' , ?", "SELECT 'it''s ?' , $1"),
        ("SELECT E'a\\'?' , ?", "SELECT E'a\\'?' , $1"),
        ("SELECT e'\\\\' , ?", "SELECT e'\\\\' , $1"),
        # a backslash does not escape in a standard string
        ("SELECT 'a\\' , ?", "SELECT 'a\\' , $1"),
        # quoted identifiers
        (
            'SELECT "col?" , "a""?" FROM t WHERE x = ?',
            'SELECT "col?" , "a""?" FROM t WHERE x = $1',
        ),
        # comments
        ("SELECT ? -- what?\n, ?", "SELECT $1 -- what?\n, $2"),
        ("SELECT ? -- trailing?", "SELECT $1 -- trailing?"),
        ("SELECT /* ? */ ?", "SELECT /* ? */ $1"),
        (
            "SELECT /* a /* nested ? */ still? */ ?",
            "SELECT /* a /* nested ? */ still? */ $1",
        ),
        # dollar quotes
        ("SELECT $$ ? $$, ?", "SELECT $$ ? $$, $1"),
        ("SELECT $tag$ ? $$ ? $tag$, ?", "SELECT $tag$ ? $$ ? $tag$, $1"),
        ("SELECT $a$ unterminated ?", "SELECT $a$ unterminated ?"),
        # JSONB operators
        (
            "SELECT data ?| array['a'] FROM t WHERE id = ?",
            "SELECT data ?| array['a'] FROM t WHERE id = $1",
        ),
        (
            "SELECT data ?& array['a'] FROM t WHERE id = ?",
            "SELECT data ?& array['a'] FROM t WHERE id = $1",
        ),
        (
            "SELECT data ?? 'k' FROM t WHERE id = ?",
            "SELECT data ? 'k' FROM t WHERE id = $1",
        ),
        ("SELECT ??? ", "SELECT ?$1 "),
        # identifiers containing '$' are not placeholders or dollar quotes
        ("SELECT a$1, ? FROM t", "SELECT a$1, $1 FROM t"),
        ("SELECT a$b$ FROM t WHERE x = ?", "SELECT a$b$ FROM t WHERE x = $1"),
        ("SELECT ? AS größe$", "SELECT $1 AS größe$"),
    ],
)
def test_convert_placeholders(sql: str, expected: str) -> None:
    assert convert_placeholders(sql) == expected


@pytest.mark.parametrize(
    "sql",
    [
        "SELECT * FROM t WHERE a = $1 AND data ? 'k'",
        "UPDATE t SET a = $2 WHERE b = $1 AND c ?| array['x']",
        "SELECT $1::jsonb ? 'k'",
    ],
)
def test_sql_with_native_placeholders_is_unchanged(sql: str) -> None:
    assert convert_placeholders(sql) == sql


def test_placeholder_like_text_inside_literals_does_not_make_sql_native() -> None:
    assert convert_placeholders("SELECT '$1', ?") == "SELECT '$1', $1"
    assert convert_placeholders("SELECT ? -- $1") == "SELECT $1 -- $1"


@pytest.mark.parametrize(
    ("sql", "expected"),
    [
        ("x > 0", False),
        ("x > 0; DROP TABLE t", True),
        ("name <> ';'", False),
        ('"a;b" IS NOT NULL', False),
        ("x > 0 -- ;", False),
        ("x > 0 /* ; */", False),
        ("x = $$;$$", False),
    ],
)
def test_has_statement_separator(sql: str, expected: bool) -> None:
    assert has_statement_separator(sql) is expected


@pytest.mark.parametrize(
    ("name", "expected"),
    [
        ("users", '"users"'),
        ("Users", '"Users"'),
        ('we"ird', '"we""ird"'),
        ("sp1; DROP TABLE victims", '"sp1; DROP TABLE victims"'),
        ('x"; DROP TABLE t; --', '"x""; DROP TABLE t; --"'),
        ("é" * 31, '"' + "é" * 31 + '"'),
    ],
)
def test_quote_identifier(name: str, expected: str) -> None:
    assert quote_identifier(name) == expected


@pytest.mark.parametrize("name", ["", "a\x00b", "a" * 64, "é" * 32])
def test_quote_identifier_rejects_invalid_names(name: str) -> None:
    with pytest.raises(InvalidSchemaError):
        quote_identifier(name)


def test_quote_identifier_raises_the_requested_error() -> None:
    with pytest.raises(InvalidParametersError, match="savepoint name is required"):
        quote_identifier("", "savepoint", InvalidParametersError)


def test_quote_table() -> None:
    assert quote_table("users") == '"users"'
    assert quote_table("app.users") == '"app"."users"'
    assert quote_table('a"b.c') == '"a""b"."c"'
    assert split_table_name("app.users") == ("app", "users")
    assert split_table_name("users") == (None, "users")


@pytest.mark.parametrize("name", ["", ".users", "app.", "a.b.c", "x\x00"])
def test_quote_table_rejects_invalid_names(name: str) -> None:
    with pytest.raises(InvalidSchemaError):
        quote_table(name)


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        (None, "NULL"),
        (True, "TRUE"),
        (False, "FALSE"),
        (42, "42"),
        (-7, "-7"),
        (1.5, "1.5"),
        (1e20, "1e+20"),
        (float("nan"), "'NaN'"),
        (float("inf"), "'Infinity'"),
        (float("-inf"), "'-Infinity'"),
        (decimal.Decimal("12.50"), "12.50"),
        (decimal.Decimal("NaN"), "'NaN'"),
        (decimal.Decimal("-Infinity"), "'-Infinity'"),
        ("plain", "'plain'"),
        ("it's", "'it''s'"),
        ("back\\slash", "E'back\\\\slash'"),
        ("'); DROP TABLE t; --", "'''); DROP TABLE t; --'"),
        ("{}", "'{}'"),
        (b"\x00\xff", "E'\\\\x00ff'"),
        (datetime.date(2024, 1, 2), "'2024-01-02'"),
        (datetime.time(3, 4, 5), "'03:04:05'"),
        (
            datetime.datetime(2024, 1, 2, 3, 4, 5, tzinfo=datetime.UTC),
            "'2024-01-02T03:04:05+00:00'",
        ),
        (
            uuid.UUID("12345678-1234-5678-1234-567812345678"),
            "'12345678-1234-5678-1234-567812345678'",
        ),
    ],
)
def test_quote_literal(value: Any, expected: str) -> None:
    assert quote_literal(value) == expected


@pytest.mark.parametrize("value", ["nul\x00", [1], {"a": 1}, datetime.timedelta(1)])
def test_quote_literal_rejects_unsupported_values(value: Any) -> None:
    with pytest.raises(InvalidSchemaError):
        quote_literal(value)


@pytest.mark.parametrize(
    ("status", "expected"),
    [
        ("INSERT 0 1", 1),
        ("INSERT 0 25", 25),
        ("UPDATE 3", 3),
        ("DELETE 0", 0),
        ("SELECT 2", 2),
        ("MERGE 1", 1),
        ("COPY 5", 5),
        ("FETCH 4", 4),
        ("CREATE TABLE", 0),
        ("BEGIN", 0),
        ("", 0),
    ],
)
def test_rows_affected(status: str, expected: int) -> None:
    assert rows_affected(status) == expected
