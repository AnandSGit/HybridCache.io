package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

func TestMapError_PgErrors(t *testing.T) {
	tests := []struct {
		name      string
		pgErr     *pgconn.PgError
		sentinel  error
		retryable bool
		context   map[string]interface{}
	}{
		{
			name:     "unique violation",
			pgErr:    &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key", Detail: "Key (email)=(secret@example.com) already exists."},
			sentinel: domain.ErrDuplicateKey,
			context:  map[string]interface{}{"key": "users_email_key"},
		},
		{
			name:     "foreign key violation",
			pgErr:    &pgconn.PgError{Code: "23503", ConstraintName: "orders_user_fk"},
			sentinel: domain.ErrConstraintViolation,
			context:  map[string]interface{}{"constraint": "orders_user_fk"},
		},
		{
			name:     "check violation",
			pgErr:    &pgconn.PgError{Code: "23514", ConstraintName: "age_positive"},
			sentinel: domain.ErrConstraintViolation,
			context:  map[string]interface{}{"constraint": "age_positive"},
		},
		{
			name:     "not null violation",
			pgErr:    &pgconn.PgError{Code: "23502", ColumnName: "email"},
			sentinel: domain.ErrConstraintViolation,
			context:  map[string]interface{}{"constraint": "email"},
		},
		{
			name:      "deadlock",
			pgErr:     &pgconn.PgError{Code: "40P01"},
			sentinel:  domain.ErrDeadlock,
			retryable: true,
		},
		{
			name:      "serialization failure",
			pgErr:     &pgconn.PgError{Code: "40001"},
			sentinel:  domain.ErrTransactionFailed,
			retryable: true,
		},
		{
			name:     "undefined table",
			pgErr:    &pgconn.PgError{Code: "42P01", Message: `relation "missing" does not exist`},
			sentinel: domain.ErrTableNotFound,
			context:  map[string]interface{}{"table": "missing"},
		},
		{
			name:     "undefined column",
			pgErr:    &pgconn.PgError{Code: "42703", Message: `column "nope" of relation "users" does not exist`},
			sentinel: domain.ErrColumnNotFound,
			context:  map[string]interface{}{"column": "nope"},
		},
		{
			name:      "query canceled (statement_timeout)",
			pgErr:     &pgconn.PgError{Code: "57014"},
			sentinel:  domain.ErrQueryTimeout,
			retryable: true,
			context:   map[string]interface{}{"query": "SELECT pg_sleep(1)"},
		},
		{
			name:     "duplicate table",
			pgErr:    &pgconn.PgError{Code: "42P07"},
			sentinel: domain.ErrSchemaExists,
		},
		{
			name:     "syntax error",
			pgErr:    &pgconn.PgError{Code: "42601"},
			sentinel: domain.ErrInvalidQuery,
		},
		{
			name:     "string truncation",
			pgErr:    &pgconn.PgError{Code: "22001"},
			sentinel: domain.ErrDataTruncated,
		},
		{
			name:     "invalid text representation",
			pgErr:    &pgconn.PgError{Code: "22P02"},
			sentinel: domain.ErrInvalidData,
		},
		{
			name:     "aborted transaction",
			pgErr:    &pgconn.PgError{Code: "25P02"},
			sentinel: domain.ErrTransactionFailed,
		},
		{
			name:     "other error falls back to the operation category",
			pgErr:    &pgconn.PgError{Code: "XX000"},
			sentinel: domain.ErrQueryFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := fmt.Errorf("driver: %w", tt.pgErr)
			err := mapError(wrapped, opQuery, "query failed", "SELECT pg_sleep(1)")
			require.Error(t, err)

			assert.ErrorIs(t, err, tt.sentinel)
			assert.Equal(t, tt.retryable, domain.IsRetryable(err))

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, "the PgError stays reachable as the cause")
			assert.Same(t, tt.pgErr, pgErr)

			var se *domain.StorageError
			require.ErrorAs(t, err, &se)
			assert.Equal(t, tt.pgErr.Code, se.Context["sqlstate"])
			for k, v := range tt.context {
				assert.Equal(t, v, se.Context[k], "context %q", k)
			}
			assert.NotContains(t, se.Message, "secret@example.com", "row data must not leak into the message")
			assert.Equal(t, 1, strings.Count(err.Error(), "SQLSTATE"), "cause must not be duplicated: %s", err)
		})
	}
}

func TestMapError_FallbackByKind(t *testing.T) {
	cause := errors.New("boom")
	assert.ErrorIs(t, mapError(cause, opQuery, "query failed", ""), domain.ErrQueryFailed)
	assert.ErrorIs(t, mapError(cause, opTx, "commit failed", ""), domain.ErrTransactionFailed)
	assert.ErrorIs(t, mapError(cause, opSchema, "create table failed", ""), domain.ErrInvalidSchema)

	err := mapError(cause, opTx, "commit failed", "")
	assert.True(t, domain.IsTransactionError(err))
	assert.Equal(t, "TRANSACTION_FAILED: commit failed (caused by: boom)", err.Error())
}

func TestMapError_DriverErrors(t *testing.T) {
	assert.NoError(t, mapError(nil, opQuery, "x", ""))

	noRows := mapError(pgx.ErrNoRows, opQuery, "x", "")
	assert.ErrorIs(t, noRows, domain.ErrNoRows)
	assert.ErrorIs(t, noRows, sql.ErrNoRows)

	timeout := mapError(fmt.Errorf("read: %w", context.DeadlineExceeded), opQuery, "x", "SELECT 1")
	assert.ErrorIs(t, timeout, domain.ErrQueryTimeout)
	assert.ErrorIs(t, timeout, context.DeadlineExceeded)
	assert.True(t, domain.IsTimeoutError(timeout))

	txTimeout := mapError(context.DeadlineExceeded, opTx, "commit failed", "COMMIT")
	assert.ErrorIs(t, txTimeout, domain.ErrTransactionTimeout)

	canceled := mapError(context.Canceled, opQuery, "query failed", "")
	assert.ErrorIs(t, canceled, domain.ErrQueryFailed)
	assert.ErrorIs(t, canceled, context.Canceled)

	assert.ErrorIs(t, mapError(pgx.ErrTxClosed, opTx, "x", ""), domain.ErrTransactionClosed)
	assert.ErrorIs(t, mapError(pgx.ErrTxCommitRollback, opTx, "x", ""), domain.ErrTransactionFailed)

	existing := domain.NewNoRowsError()
	assert.Same(t, existing, mapError(existing, opQuery, "x", ""))
}

func TestQuotedName(t *testing.T) {
	assert.Equal(t, "users", quotedName(`relation "users" does not exist`))
	assert.Equal(t, "", quotedName("no quotes"))
	assert.Equal(t, "", quotedName(`unterminated "quote`))
}
