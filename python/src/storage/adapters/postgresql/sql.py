"""
SQL text helpers for the PostgreSQL adapter.

``convert_placeholders`` rewrites the ``?`` placeholders that the query builder
emits into PostgreSQL's ``$n`` form. The ``quote_*`` helpers render identifiers
and literals for statements the adapter builds itself (DDL, savepoints), and
``rows_affected`` reads the row count from a command tag.
"""

from __future__ import annotations

import datetime
import decimal
import math
import uuid
from collections.abc import Iterator
from typing import Any

from storage.errors import InvalidSchemaError, StorageError

# NAMEDATALEN - 1: PostgreSQL silently truncates longer identifiers, so two long
# names could address the same object. They are rejected instead.
MAX_IDENTIFIER_BYTES = 63


def convert_placeholders(sql: str) -> str:
    """Rewrite ``?`` placeholders into PostgreSQL's positional ``$n`` form.

    Only a ``?`` in SQL code is a placeholder: string literals (``'...'``,
    ``E'...'``), quoted identifiers (``"..."``), dollar-quoted strings
    (``$tag$...$tag$``) and comments (``--`` and nested ``/* */``) are copied
    unchanged. Because ``?`` also starts some PostgreSQL operators:

    - ``?|`` and ``?&`` (JSONB) are left untouched;
    - ``??`` is an escaped ``?`` and becomes a single ``?``, so write
      ``data ?? 'key'`` for the JSONB key-exists operator;
    - SQL that already uses ``$n`` placeholders is native PostgreSQL SQL and is
      returned unchanged, so its ``?`` operators need no escaping.

    String literals are read with ``standard_conforming_strings`` on (the
    server default).
    """
    marks: list[int] = []  # offsets of '?' placeholders and '??' escapes
    skip_to = 0
    for i, char in _code(sql):
        if i < skip_to:
            continue
        if char == "$" and _is_digit(sql, i + 1) and not _ident_before(sql, i):
            return sql  # native $n placeholders
        if char != "?":
            continue
        following = sql[i + 1 : i + 2]
        if following in ("|", "&"):
            skip_to = i + 2
            continue
        marks.append(i)
        if following == "?":
            skip_to = i + 2

    if not marks:
        return sql
    parts: list[str] = []
    last = 0
    number = 0
    for mark in marks:
        parts.append(sql[last:mark])
        if sql.startswith("??", mark):
            parts.append("?")
            last = mark + 2
        else:
            number += 1
            parts.append(f"${number}")
            last = mark + 1
    parts.append(sql[last:])
    return "".join(parts)


def has_statement_separator(sql: str) -> bool:
    """Whether ``sql`` has a ``;`` in SQL code (outside literals and comments)."""
    return any(char == ";" for _, char in _code(sql))


def _code(sql: str) -> Iterator[tuple[int, str]]:
    """Yield the offset and character of every character of SQL code in ``sql``,
    skipping string literals, quoted identifiers, dollar-quoted strings and
    comments."""
    i = 0
    n = len(sql)
    while i < n:
        char = sql[i]
        if char == "'":
            i = _skip_string_literal(sql, i)
        elif char == '"':
            i = _skip_quoted(sql, i, '"')
        elif char == "-" and sql.startswith("-", i + 1):
            end = sql.find("\n", i)
            i = n if end < 0 else end + 1
        elif char == "/" and sql.startswith("*", i + 1):
            i = _skip_block_comment(sql, i)
        elif char == "$" and not _ident_before(sql, i) and not _is_digit(sql, i + 1):
            end = _skip_dollar_quoted(sql, i)
            if end == i + 1:  # a lone '$', not a dollar quote
                yield i, char
            i = end
        else:
            yield i, char
            i += 1


def _skip_string_literal(sql: str, start: int) -> int:
    """Return the offset just past the string literal whose quote is at
    ``start``. In an ``E'...'`` literal a backslash escapes the next character;
    in every literal a doubled quote is an escaped quote."""
    escapes = start > 0 and sql[start - 1] in "Ee" and not _ident_before(sql, start - 1)
    i = start + 1
    n = len(sql)
    while i < n:
        char = sql[i]
        if char == "\\" and escapes:
            i += 2
            continue
        if char == "'":
            if sql.startswith("'", i + 1):
                i += 2
                continue
            return i + 1
        i += 1
    return n


def _skip_quoted(sql: str, start: int, quote: str) -> int:
    """Return the offset just past the quoted token (such as a quoted identifier)
    starting at ``start``; a doubled quote is an escaped quote."""
    i = start + 1
    n = len(sql)
    while i < n:
        if sql[i] == quote:
            if sql.startswith(quote, i + 1):
                i += 2
                continue
            return i + 1
        i += 1
    return n


def _skip_block_comment(sql: str, start: int) -> int:
    """Skip a ``/* */`` comment; PostgreSQL block comments nest."""
    depth = 0
    i = start
    n = len(sql)
    while i + 1 < n:
        pair = sql[i : i + 2]
        if pair == "/*":
            depth += 1
            i += 2
        elif pair == "*/":
            depth -= 1
            i += 2
            if depth == 0:
                return i
        else:
            i += 1
    return n


def _skip_dollar_quoted(sql: str, start: int) -> int:
    """Skip the ``$tag$...$tag$`` string starting at ``start``. If no dollar-quote
    tag starts there, skip just the ``$``."""
    end = start + 1
    n = len(sql)
    while end < n and _is_ident_char(sql[end]) and sql[end] != "$":
        end += 1
    if end >= n or sql[end] != "$":
        return start + 1
    tag = sql[start : end + 1]
    closing = sql.find(tag, end + 1)
    return n if closing < 0 else closing + len(tag)


def _is_ident_char(char: str) -> bool:
    """Whether ``char`` can appear in an unquoted identifier after its first
    character (non-ASCII letters included, as in PostgreSQL)."""
    return char == "_" or char == "$" or not char.isascii() or char.isalnum()


def _ident_before(sql: str, i: int) -> bool:
    return i > 0 and _is_ident_char(sql[i - 1])


def _is_digit(sql: str, i: int) -> bool:
    return i < len(sql) and "0" <= sql[i] <= "9"


def quote_identifier(
    name: str,
    what: str = "identifier",
    error: type[StorageError] = InvalidSchemaError,
) -> str:
    """Quote ``name`` as a single SQL identifier, doubling embedded quotes.

    Raises:
        error (``InvalidSchemaError`` by default): If ``name`` is empty, contains
            a NUL character or is longer than 63 bytes.
    """
    if not name:
        raise error(f"{what} name is required")
    if "\x00" in name:
        raise error(f"{what} name cannot contain NUL characters")
    if len(name.encode()) > MAX_IDENTIFIER_BYTES:
        raise error(f"{what} name is longer than {MAX_IDENTIFIER_BYTES} bytes")
    return '"' + name.replace('"', '""') + '"'


def split_table_name(name: str) -> tuple[str | None, str]:
    """Split ``"table"`` or ``"schema.table"`` into (schema or ``None``, table).

    Raises:
        InvalidSchemaError: If the name is empty or has more than one dot.
    """
    parts = name.split(".")
    if len(parts) > 2:
        raise InvalidSchemaError("table name must be 'table' or 'schema.table'")
    for part in parts:
        quote_identifier(part, "table")  # validates
    if len(parts) == 2:
        return parts[0], parts[1]
    return None, parts[0]


def quote_table(name: str) -> str:
    """Quote ``"table"`` or ``"schema.table"`` (``"schema"."table"``)."""
    schema, table = split_table_name(name)
    quoted = quote_identifier(table, "table")
    return (
        quoted if schema is None else f"{quote_identifier(schema, 'schema')}.{quoted}"
    )


def quote_literal(value: Any) -> str:
    """Render ``value`` as an SQL literal for statements that cannot take bound
    parameters, such as ``DEFAULT`` clauses and ``COMMENT ON``.

    Supported: ``None``, ``bool``, ``int``, ``float``, ``Decimal``, ``str``,
    ``bytes``, ``datetime``/``date``/``time`` and ``UUID``. Strings are quoted so
    that the result is safe whatever the server's ``standard_conforming_strings``
    setting.

    Raises:
        InvalidSchemaError: For other types and for strings containing NUL.
    """
    if value is None:
        return "NULL"
    if isinstance(value, bool):
        return "TRUE" if value else "FALSE"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        if math.isnan(value):
            return "'NaN'"
        if math.isinf(value):
            return "'Infinity'" if value > 0 else "'-Infinity'"
        return repr(value)
    if isinstance(value, decimal.Decimal):
        if value.is_nan():
            return "'NaN'"
        if value.is_infinite():
            return "'Infinity'" if value > 0 else "'-Infinity'"
        return str(value)
    if isinstance(value, str):
        return _quote_string(value)
    if isinstance(value, bytes | bytearray | memoryview):
        return _quote_string("\\x" + bytes(value).hex())  # bytea hex format
    if isinstance(value, datetime.date | datetime.time):  # datetime is a date
        return _quote_string(value.isoformat())
    if isinstance(value, uuid.UUID):
        return _quote_string(str(value))
    raise InvalidSchemaError(f"unsupported literal type {type(value).__name__}")


def _quote_string(text: str) -> str:
    if "\x00" in text:
        raise InvalidSchemaError("string literals cannot contain NUL characters")
    text = text.replace("'", "''")
    if "\\" in text:
        return "E'" + text.replace("\\", "\\\\") + "'"
    return "'" + text + "'"


def rows_affected(status: str) -> int:
    """Return the row count of a command tag such as ``INSERT 0 1``, ``UPDATE 3``,
    ``SELECT 2``, ``MERGE 1`` or ``COPY 5``; 0 for tags without one
    (``CREATE TABLE``)."""
    count = status.rpartition(" ")[2]
    return int(count) if count.isascii() and count.isdigit() else 0
