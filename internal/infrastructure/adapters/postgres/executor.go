package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// querier is the part of the pgx API shared by *pgxpool.Pool and pgx.Tx, so
// that storage and transactions run statements through the same code.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

var (
	_ querier = (*pgxpool.Pool)(nil)
	_ querier = (pgx.Tx)(nil)
)

// executor runs queries, commands and batches against a querier.
type executor struct {
	db           querier
	queryTimeout time.Duration // Config.QueryTimeout: default for a zero Options.Timeout
}

// withTimeout bounds ctx by timeout, or by the configured default when timeout
// is zero. Zero everywhere means no timeout.
func (e executor) withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout = e.effectiveTimeout(timeout); timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {}
}

func (e executor) effectiveTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return e.queryTimeout
}

func (e executor) query(ctx context.Context, q domain.Query) (domain.Result, error) {
	sql, err := translateSQL(q.SQL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := e.withTimeout(ctx, q.Options.Timeout)
	rows, err := e.db.Query(ctx, sql, q.Parameters...)
	if err != nil {
		cancel()
		return nil, mapError(err, opQuery, "query failed", sql)
	}
	return newStreamingResult(rows, cancel, sql), nil
}

func (e executor) queryOne(ctx context.Context, q domain.Query) (domain.Row, error) {
	sql, err := translateSQL(q.SQL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := e.withTimeout(ctx, q.Options.Timeout)
	defer cancel()

	rows, err := e.db.Query(ctx, sql, q.Parameters...)
	if err != nil {
		return nil, mapError(err, opQuery, "query failed", sql)
	}
	defer rows.Close()

	row := &PostgreSQLRow{typeMap: pgtype.NewMap()}
	if rows.Next() {
		row.fields = copyFields(rows.FieldDescriptions())
		row.values = copyValues(rows.RawValues())
	} else {
		row.err = domain.NewNoRowsError().WithCause(pgx.ErrNoRows)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapError(err, opQuery, "query failed", sql)
	}
	return row, nil
}

func (e executor) execute(ctx context.Context, c domain.Command) (domain.ExecuteResult, error) {
	sql, err := translateSQL(c.SQL)
	if err != nil {
		return domain.ExecuteResult{}, err
	}

	ctx, cancel := e.withTimeout(ctx, c.Options.Timeout)
	defer cancel()

	tag, err := e.db.Exec(ctx, sql, c.Parameters...)
	if err != nil {
		return domain.ExecuteResult{}, mapError(err, opQuery, "command failed", sql)
	}
	// PostgreSQL has no last-insert ID; use INSERT ... RETURNING with Query.
	return domain.ExecuteResult{RowsAffected: tag.RowsAffected()}, nil
}

// batch sends all operations in one round trip. Each query result is read into
// memory before the next operation's result is read, so it stays usable after
// batch returns.
//
// PostgreSQL runs a batch atomically: outside a transaction it executes in an
// implicit transaction, and inside one a failure aborts the transaction. So when
// an operation fails, batch returns that error (also set on the operation's
// result) and reports the operations after it as not executed. An invalid
// operation rejects the whole batch before anything is sent.
//
// The batch's timeout is the sum of its operations' timeouts (each defaulting to
// Config.QueryTimeout); the batch is unbounded if any operation is.
func (e executor) batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	results := make([]domain.OperationResult, len(operations))
	if len(operations) == 0 {
		return results, nil
	}

	b := &pgx.Batch{}
	statements := make([]string, len(operations))
	var timeout time.Duration
	bounded := true
	for i, op := range operations {
		results[i].Index = i

		var (
			sql       string
			params    []interface{}
			opTimeout time.Duration
		)
		switch op.Type {
		case domain.OperationTypeQuery:
			sql, params, opTimeout = op.Query.SQL, op.Query.Parameters, op.Query.Options.Timeout
		case domain.OperationTypeCommand:
			sql, params, opTimeout = op.Command.SQL, op.Command.Parameters, op.Command.Options.Timeout
		default:
			err := domain.NewInvalidQueryError(fmt.Sprintf("unknown operation type %d", op.Type)).
				WithContext("operation", i)
			results[i].Error = err
			return results, err
		}
		if isEmptySQL(sql) {
			err := emptySQLError().WithContext("operation", i)
			results[i].Error = err
			return results, err
		}

		statements[i] = rebind(sql)
		b.Queue(statements[i], params...)

		if opTimeout = e.effectiveTimeout(opTimeout); opTimeout > 0 {
			timeout += opTimeout
		} else {
			bounded = false
		}
	}

	cancel := context.CancelFunc(func() {})
	if bounded {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	br := e.db.SendBatch(ctx, b)
	var batchErr *domain.StorageError
	for i, op := range operations {
		if batchErr != nil {
			results[i].Error = domain.NewQueryError("QUERY_FAILED",
				fmt.Sprintf("batch operation %d was not executed because an earlier operation failed", i)).
				WithContext("operation", i).
				WithCause(batchErr)
			continue
		}

		var err error
		if op.Type == domain.OperationTypeQuery {
			var rows pgx.Rows
			if rows, err = br.Query(); err == nil {
				var buffered *bufferedResult
				if buffered, err = bufferRows(rows); err == nil {
					results[i].Result = buffered
				}
			}
		} else {
			var tag pgconn.CommandTag
			if tag, err = br.Exec(); err == nil {
				results[i].Result = domain.ExecuteResult{RowsAffected: tag.RowsAffected()}
			}
		}
		if err != nil {
			batchErr = toStorageError(err, opQuery, fmt.Sprintf("batch operation %d failed", i), statements[i]).
				WithContext("operation", i)
			results[i].Error = batchErr
		}
	}

	if err := br.Close(); err != nil && batchErr == nil {
		batchErr = toStorageError(err, opQuery, "batch failed", "")
	}
	if batchErr != nil {
		return results, batchErr
	}
	return results, nil
}

// translateSQL rejects empty SQL and rewrites '?' placeholders.
func translateSQL(sql string) (string, error) {
	if isEmptySQL(sql) {
		return "", emptySQLError()
	}
	return rebind(sql), nil
}

func isEmptySQL(sql string) bool {
	return strings.TrimSpace(sql) == ""
}

func emptySQLError() *domain.StorageError {
	return domain.NewInvalidQueryError("SQL statement is empty")
}
