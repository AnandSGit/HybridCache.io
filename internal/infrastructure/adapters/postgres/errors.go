package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// opKind selects the fallback error category for errors that have no more
// specific mapping.
type opKind int

const (
	opQuery  opKind = iota // queries, commands, batches and introspection
	opTx                   // BEGIN, COMMIT, ROLLBACK and savepoints
	opSchema               // DDL
)

// PostgreSQL SQLSTATE codes mapped to domain errors.
const (
	sqlStateUniqueViolation      = "23505"
	sqlStateNotNullViolation     = "23502"
	sqlStateSerializationFailure = "40001"
	sqlStateDeadlockDetected     = "40P01"
	sqlStateUndefinedTable       = "42P01"
	sqlStateUndefinedColumn      = "42703"
	sqlStateSyntaxError          = "42601"
	sqlStateDuplicateTable       = "42P07"
	sqlStateDuplicateColumn      = "42701"
	sqlStateDuplicateObject      = "42710"
	sqlStateStringTruncation     = "22001"
	sqlStateInFailedTransaction  = "25P02"
	sqlStateQueryCanceled        = "57014"
)

// mapError converts an error from pgx into a *domain.StorageError (nil stays
// nil). msg describes the failed operation; sql is the statement involved
// (recorded only for timeouts). The original error is kept as the cause, so
// errors.As still finds a *pgconn.PgError.
func mapError(err error, kind opKind, msg, sql string) error {
	if err == nil {
		return nil
	}
	return toStorageError(err, kind, msg, sql)
}

// toStorageError is mapError for a non-nil err.
func toStorageError(err error, kind opKind, msg, sql string) *domain.StorageError {
	var se *domain.StorageError
	if errors.As(err, &se) {
		return se
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return mapPgError(pgErr, kind, msg, sql).WithCause(err)
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err):
		if kind == opTx {
			return domain.NewTransactionTimeoutError().WithCause(err)
		}
		return domain.NewQueryTimeoutError(sql).WithCause(err)
	case errors.Is(err, pgx.ErrNoRows):
		return domain.NewNoRowsError().WithCause(err)
	case errors.Is(err, pgx.ErrTxClosed):
		return domain.NewTransactionClosedError().WithCause(err)
	case errors.Is(err, pgx.ErrTxCommitRollback):
		return domain.NewTransactionError("TRANSACTION_FAILED",
			"transaction was rolled back because an earlier statement failed").WithCause(err)
	}

	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return domain.NewConnectionError("CONNECTION_FAILED", "failed to connect to PostgreSQL").WithCause(err)
	}

	return fallbackError(kind, msg).WithCause(err)
}

func mapPgError(pgErr *pgconn.PgError, kind opKind, msg, sql string) *domain.StorageError {
	var mapped *domain.StorageError
	switch code := pgErr.Code; {
	case code == sqlStateUniqueViolation:
		mapped = domain.NewDuplicateKeyError(pgErr.ConstraintName)
	case code == sqlStateNotNullViolation:
		mapped = domain.NewConstraintViolationError(pgErr.ColumnName)
	case strings.HasPrefix(code, "23"): // foreign key, check, exclusion, ...
		mapped = domain.NewConstraintViolationError(pgErr.ConstraintName)
	case code == sqlStateDeadlockDetected:
		mapped = domain.NewDeadlockError()
	case code == sqlStateSerializationFailure:
		mapped = domain.NewTransactionError("TRANSACTION_FAILED",
			"could not serialize access due to concurrent transactions").
			WithRetryable(true).
			WithTemporary(true)
	case code == sqlStateInFailedTransaction:
		mapped = domain.NewTransactionError("TRANSACTION_FAILED",
			"transaction is aborted; roll it back before issuing more statements")
	case code == sqlStateUndefinedTable:
		mapped = domain.NewTableNotFoundError(firstNonEmpty(pgErr.TableName, quotedName(pgErr.Message)))
	case code == sqlStateUndefinedColumn:
		mapped = domain.NewColumnNotFoundError(firstNonEmpty(pgErr.ColumnName, quotedName(pgErr.Message)))
	case code == sqlStateDuplicateTable || code == sqlStateDuplicateColumn || code == sqlStateDuplicateObject:
		mapped = domain.NewSchemaError("SCHEMA_EXISTS", "object already exists")
	case code == sqlStateSyntaxError:
		mapped = domain.NewInvalidQueryError("syntax error in SQL statement")
	case code == sqlStateStringTruncation:
		mapped = domain.NewDataError("DATA_TRUNCATED", "value too long for column")
	case strings.HasPrefix(code, "22"): // data exception
		mapped = domain.NewDataError("INVALID_DATA", "invalid data for column type")
	case code == sqlStateQueryCanceled:
		mapped = domain.NewQueryTimeoutError(sql)
	default:
		mapped = fallbackError(kind, msg)
	}
	return mapped.WithContext("sqlstate", pgErr.Code)
}

func fallbackError(kind opKind, msg string) *domain.StorageError {
	switch kind {
	case opTx:
		return domain.NewTransactionError("TRANSACTION_FAILED", msg)
	case opSchema:
		return domain.NewSchemaError("INVALID_SCHEMA", msg)
	default:
		return domain.NewQueryError("QUERY_FAILED", msg)
	}
}

// quotedName returns the first double-quoted name in a server message such as
// `relation "users" does not exist`.
func quotedName(msg string) string {
	start := strings.IndexByte(msg, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(msg[start+1:], '"')
	if end < 0 {
		return ""
	}
	return msg[start+1 : start+1+end]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
