package postgres

import (
	"strconv"
	"strings"
)

// rebind rewrites '?' placeholders into PostgreSQL's positional $n form.
//
// Only '?' characters in SQL code are placeholders: string literals ('...',
// E'...'), quoted identifiers ("..."), dollar-quoted strings ($tag$...$tag$) and
// comments (-- and /* */) are copied unchanged. Because '?' is also the first
// character of some PostgreSQL operators:
//   - "?|" and "?&" (JSONB) are always left untouched;
//   - "??" is an escaped '?' and becomes a single '?' (write "data ?? 'key'" for
//     the JSONB key-exists operator);
//   - SQL that already uses $n placeholders is native PostgreSQL SQL and is
//     returned unchanged, so its '?' operators need no escaping.
//
// SQL without any '?' in code is returned unchanged. String literals are lexed
// with standard_conforming_strings semantics (the server default).
func rebind(sql string) string {
	var (
		marks  []int // offsets of '?' placeholders and "??" escapes
		native bool  // sql contains $n placeholders
	)

	n := len(sql)
	for i := 0; i < n; {
		switch c := sql[i]; {
		case c == '\'':
			i = skipStringLiteral(sql, i)
		case c == '"':
			i = skipQuoted(sql, i, '"')
		case c == '-' && i+1 < n && sql[i+1] == '-':
			i = skipLineComment(sql, i)
		case c == '/' && i+1 < n && sql[i+1] == '*':
			i = skipBlockComment(sql, i)
		case c == '$':
			switch {
			case i > 0 && isIdentByte(sql[i-1]):
				i++ // part of an identifier such as a$b
			case i+1 < n && isDigit(sql[i+1]):
				native = true
				i++
			default:
				i = skipDollarQuoted(sql, i)
			}
		case c == '?':
			if i+1 < n && (sql[i+1] == '|' || sql[i+1] == '&') {
				i += 2 // JSONB ?| and ?& operators
				continue
			}
			marks = append(marks, i)
			if i+1 < n && sql[i+1] == '?' {
				i += 2
				continue
			}
			i++
		default:
			i++
		}
	}

	if len(marks) == 0 || native {
		return sql
	}

	var b strings.Builder
	b.Grow(len(sql) + 2*len(marks))
	last, param := 0, 0
	for _, m := range marks {
		b.WriteString(sql[last:m])
		if m+1 < n && sql[m+1] == '?' {
			b.WriteByte('?')
			last = m + 2
			continue
		}
		param++
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(param))
		last = m + 1
	}
	b.WriteString(sql[last:])
	return b.String()
}

// skipStringLiteral returns the offset just past the string literal that starts
// with the quote at sql[start]. An E'...' literal treats backslash as an escape;
// in all literals a doubled quote is an escaped quote.
func skipStringLiteral(sql string, start int) int {
	escapes := start > 0 && (sql[start-1] == 'E' || sql[start-1] == 'e') &&
		(start == 1 || !isIdentByte(sql[start-2]))
	for i := start + 1; i < len(sql); i++ {
		switch sql[i] {
		case '\\':
			if escapes {
				i++
			}
		case '\'':
			if i+1 < len(sql) && sql[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(sql)
}

// skipQuoted returns the offset just past the quote-delimited token (such as a
// quoted identifier) that starts at sql[start]; a doubled quote is an escape.
func skipQuoted(sql string, start int, quote byte) int {
	for i := start + 1; i < len(sql); i++ {
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(sql)
}

func skipLineComment(sql string, start int) int {
	if end := strings.IndexByte(sql[start:], '\n'); end >= 0 {
		return start + end + 1
	}
	return len(sql)
}

// skipBlockComment skips a /* */ comment; PostgreSQL block comments nest.
func skipBlockComment(sql string, start int) int {
	depth := 0
	for i := start; i+1 < len(sql); {
		switch {
		case sql[i] == '/' && sql[i+1] == '*':
			depth++
			i += 2
		case sql[i] == '*' && sql[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(sql)
}

// skipDollarQuoted skips a $tag$...$tag$ string starting at sql[start]. If no
// dollar-quote tag starts there, it skips just the '$'.
func skipDollarQuoted(sql string, start int) int {
	end := start + 1
	for end < len(sql) && isIdentByte(sql[end]) && sql[end] != '$' {
		end++
	}
	if end >= len(sql) || sql[end] != '$' {
		return start + 1
	}
	tag := sql[start : end+1]
	if closing := strings.Index(sql[end+1:], tag); closing >= 0 {
		return end + 1 + closing + len(tag)
	}
	return len(sql)
}

// isIdentByte reports whether c can appear in an unquoted identifier (after its
// first character). Bytes of multi-byte UTF-8 sequences count as identifier
// characters, as they do in PostgreSQL.
func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 || isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'z')
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
