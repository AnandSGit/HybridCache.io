package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRebind(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"no placeholders", "SELECT 1", "SELECT 1"},
		{"placeholders", "SELECT * FROM t WHERE a = ? AND b IN (?, ?)", "SELECT * FROM t WHERE a = $1 AND b IN ($2, $3)"},
		{"adjacent punctuation", "VALUES (?,?)", "VALUES ($1,$2)"},
		{"single-quoted literal", "SELECT '?' , ?", "SELECT '?' , $1"},
		{"doubled quote inside literal", "SELECT 'it''s ?', ?", "SELECT 'it''s ?', $1"},
		{"backslash is literal in standard strings", `SELECT 'a\', ?`, `SELECT 'a\', $1`},
		{"escape string", `SELECT E'it\'s ?', ?`, `SELECT E'it\'s ?', $1`},
		{"lowercase escape string", `SELECT e'\\?', ?`, `SELECT e'\\?', $1`},
		{"identifier ending in E is not an escape string", `SELECT typE'?' , ?`, `SELECT typE'?' , $1`},
		{"quoted identifier", `SELECT "col?" FROM t WHERE x = ?`, `SELECT "col?" FROM t WHERE x = $1`},
		{"doubled quote in identifier", `SELECT "a""?" FROM t WHERE x = ?`, `SELECT "a""?" FROM t WHERE x = $1`},
		{"line comment", "SELECT ? -- why?\n, ?", "SELECT $1 -- why?\n, $2"},
		{"line comment at end", "SELECT ? -- trailing ?", "SELECT $1 -- trailing ?"},
		{"block comment", "SELECT /* ? */ ?", "SELECT /* ? */ $1"},
		{"nested block comment", "SELECT /* a /* ? */ still ? */ ?", "SELECT /* a /* ? */ still ? */ $1"},
		{"dollar quoted", "SELECT $$ ? $$, ?", "SELECT $$ ? $$, $1"},
		{"tagged dollar quote", "SELECT $fn$ it's ? $$ ? $fn$, ?", "SELECT $fn$ it's ? $$ ? $fn$, $1"},
		{"dollar inside identifier", "SELECT a$b$c FROM t WHERE x = ?", "SELECT a$b$c FROM t WHERE x = $1"},
		{"lone dollar", "SELECT $ ?", "SELECT $ $1"},
		{"jsonb ?| and ?& operators", "SELECT * FROM t WHERE d ?| ? AND d ?& ?", "SELECT * FROM t WHERE d ?| $1 AND d ?& $2"},
		{"escaped jsonb ? operator", "SELECT * FROM t WHERE d ?? ? AND x = ?", "SELECT * FROM t WHERE d ? $1 AND x = $2"},
		{"escaped ?| still works", "SELECT d ??| ?", "SELECT d ?| $1"},
		{"native $n SQL is unchanged", "SELECT * FROM t WHERE d ? $1 AND x = $2", "SELECT * FROM t WHERE d ? $1 AND x = $2"},
		{"native $n SQL keeps ??", "SELECT d ?? $1", "SELECT d ?? $1"},
		{"$n inside literal is not native", "SELECT '$1', ?", "SELECT '$1', $1"},
		{"unterminated literal", "SELECT ?, 'open ?", "SELECT $1, 'open ?"},
		{"unterminated comment", "SELECT ? /* open ?", "SELECT $1 /* open ?"},
		{"unicode", "SELECT 'héllo ?', ? AS ünï", "SELECT 'héllo ?', $1 AS ünï"},
		{"many placeholders", "?,?,?,?,?,?,?,?,?,?,?", "$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rebind(tt.sql))
		})
	}
}

func TestRebind_UnchangedWithoutPlaceholders(t *testing.T) {
	for _, sql := range []string{
		"SELECT 'x?' -- ?\n/* ? */ FROM \"t?\"",
		"INSERT INTO t VALUES ($1, $2)",
		"SELECT $tag$?$tag$",
	} {
		assert.Equal(t, sql, rebind(sql))
	}
}
