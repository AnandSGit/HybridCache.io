package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// fakeQuerier records the statements it receives.
type fakeQuerier struct {
	rows     pgx.Rows
	queryErr error
	tag      pgconn.CommandTag
	execErr  error
	batch    *fakeBatchResults

	ctx       context.Context
	sql       string
	args      []any
	queued    []string
	sentBatch bool
}

func (q *fakeQuerier) record(ctx context.Context, sql string, args []any) {
	q.ctx, q.sql, q.args = ctx, sql, args
}

func (q *fakeQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	q.record(ctx, sql, args)
	return q.tag, q.execErr
}

func (q *fakeQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.record(ctx, sql, args)
	return q.rows, q.queryErr
}

func (q *fakeQuerier) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	q.ctx, q.sentBatch = ctx, true
	for _, qq := range b.QueuedQueries {
		q.queued = append(q.queued, qq.SQL)
	}
	return q.batch
}

// fakeBatchResults replays one scripted result per queued statement: a
// *fakeRows for a query, a pgconn.CommandTag for a command, or an error.
type fakeBatchResults struct {
	results  []any
	pos      int
	closeErr error
	closed   bool
}

func (b *fakeBatchResults) next() any {
	r := b.results[b.pos]
	b.pos++
	return r
}

func (b *fakeBatchResults) Exec() (pgconn.CommandTag, error) {
	switch r := b.next().(type) {
	case error:
		return pgconn.CommandTag{}, r
	case pgconn.CommandTag:
		return r, nil
	default:
		panic("Exec on a query result")
	}
}

func (b *fakeBatchResults) Query() (pgx.Rows, error) {
	switch r := b.next().(type) {
	case error:
		return &fakeRows{err: r, closed: true}, r
	case *fakeRows:
		return r, nil
	default:
		panic("Query on a command result")
	}
}

func (b *fakeBatchResults) QueryRow() pgx.Row { panic("not used") }
func (b *fakeBatchResults) Tx() pgx.Tx        { return nil }
func (b *fakeBatchResults) Close() error {
	b.closed = true
	return b.closeErr
}

func deadlineIn(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "expected a deadline")
	return time.Until(deadline)
}

func TestExecutor_Timeouts(t *testing.T) {
	ctx := context.Background()

	t.Run("Options.Timeout wins", func(t *testing.T) {
		q := &fakeQuerier{}
		e := executor{db: q, queryTimeout: time.Hour}
		_, err := e.execute(ctx, domain.Command{SQL: "DELETE FROM t", Options: domain.CommandOptions{Timeout: time.Minute}})
		require.NoError(t, err)
		assert.InDelta(t, time.Minute, deadlineIn(t, q.ctx), float64(time.Second))
		assert.ErrorIs(t, q.ctx.Err(), context.Canceled, "the timeout is released when Execute returns")
	})

	t.Run("Config.QueryTimeout is the default", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{}}
		e := executor{db: q, queryTimeout: 2 * time.Minute}
		_, err := e.queryOne(ctx, domain.Query{SQL: "SELECT 1"})
		require.NoError(t, err)
		assert.InDelta(t, 2*time.Minute, deadlineIn(t, q.ctx), float64(time.Second))
	})

	t.Run("zero everywhere means no timeout", func(t *testing.T) {
		q := &fakeQuerier{}
		e := executor{db: q}
		_, err := e.execute(ctx, domain.Command{SQL: "DELETE FROM t"})
		require.NoError(t, err)
		_, ok := q.ctx.Deadline()
		assert.False(t, ok)
	})

	t.Run("a streaming query keeps its context until Close", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{rows: [][][]byte{{}}}}
		e := executor{db: q, queryTimeout: time.Minute}
		res, err := e.query(ctx, domain.Query{SQL: "SELECT ?", Parameters: []any{1}})
		require.NoError(t, err)
		assert.Equal(t, "SELECT $1", q.sql)
		assert.Equal(t, []any{1}, q.args)
		assert.NoError(t, q.ctx.Err(), "rows are still being read")
		require.NoError(t, res.Close())
		assert.ErrorIs(t, q.ctx.Err(), context.Canceled)
	})
}

func TestExecutor_Errors(t *testing.T) {
	ctx := context.Background()
	dup := &pgconn.PgError{Code: "23505", ConstraintName: "t_pkey"}

	q := &fakeQuerier{execErr: dup}
	_, err := executor{db: q}.execute(ctx, domain.Command{SQL: "INSERT INTO t VALUES (?)", Parameters: []any{1}})
	assert.ErrorIs(t, err, domain.ErrDuplicateKey)
	assert.Equal(t, "INSERT INTO t VALUES ($1)", q.sql)

	q = &fakeQuerier{queryErr: &pgconn.PgError{Code: "42P01", Message: `relation "t" does not exist`}}
	_, err = executor{db: q, queryTimeout: time.Minute}.query(ctx, domain.Query{SQL: "SELECT * FROM t"})
	assert.ErrorIs(t, err, domain.ErrTableNotFound)
	assert.ErrorIs(t, q.ctx.Err(), context.Canceled, "the timeout is released on error")

	_, err = executor{db: q}.queryOne(ctx, domain.Query{SQL: "SELECT * FROM t"})
	assert.ErrorIs(t, err, domain.ErrTableNotFound)

	untouched := &fakeQuerier{}
	_, err = executor{db: untouched}.query(ctx, domain.Query{})
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
	_, err = executor{db: untouched}.queryOne(ctx, domain.Query{SQL: " "})
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
	_, err = executor{db: untouched}.execute(ctx, domain.Command{})
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
	assert.Nil(t, untouched.ctx, "nothing is sent for empty SQL")
}

func TestExecutor_QueryOne(t *testing.T) {
	ctx := context.Background()
	fields := []pgconn.FieldDescription{binaryField("id", pgtype.Int4OID), textField("name", pgtype.TextOID)}

	rows := &fakeRows{fields: fields, rows: [][][]byte{
		{encode(t, pgtype.Int4OID, int32(1)), []byte("first")},
		{encode(t, pgtype.Int4OID, int32(2)), []byte("second")},
	}}
	row, err := executor{db: &fakeQuerier{rows: rows}}.queryOne(ctx, domain.Query{SQL: "SELECT id, name FROM t"})
	require.NoError(t, err)
	assert.True(t, rows.closed)
	var got struct {
		ID   int
		Name string
	}
	require.NoError(t, row.ScanRow(&got))
	assert.Equal(t, 1, got.ID)
	assert.Equal(t, "first", got.Name)

	row, err = executor{db: &fakeQuerier{rows: &fakeRows{fields: fields}}}.queryOne(ctx, domain.Query{SQL: "SELECT 1"})
	require.NoError(t, err, "no rows is reported by the Row, like database/sql")
	var id int
	assert.ErrorIs(t, row.Scan(&id), domain.ErrNoRows)
	assert.ErrorIs(t, row.Scan(&id), pgx.ErrNoRows, "the pgx cause is kept")

	_, err = executor{db: &fakeQuerier{rows: &fakeRows{err: &pgconn.PgError{Code: "22012"}}}}.queryOne(ctx, domain.Query{SQL: "SELECT 1/0"})
	assert.ErrorIs(t, err, domain.ErrInvalidData)
}

func TestExecutor_Batch(t *testing.T) {
	ctx := context.Background()
	query := func(sql string) domain.Operation {
		return domain.Operation{Type: domain.OperationTypeQuery, Query: domain.Query{SQL: sql}}
	}
	command := func(sql string) domain.Operation {
		return domain.Operation{Type: domain.OperationTypeCommand, Command: domain.Command{SQL: sql}}
	}
	fields := []pgconn.FieldDescription{binaryField("n", pgtype.Int4OID)}

	t.Run("query results are readable after Batch returns", func(t *testing.T) {
		rows := &fakeRows{fields: fields, rows: [][][]byte{{encode(t, pgtype.Int4OID, int32(5))}}}
		br := &fakeBatchResults{results: []any{pgconn.NewCommandTag("INSERT 0 2"), rows}}
		q := &fakeQuerier{batch: br}
		results, err := executor{db: q}.batch(ctx, []domain.Operation{
			command("INSERT INTO t VALUES (?), (?)"),
			query("SELECT n FROM t WHERE n > ?"),
		})
		require.NoError(t, err)
		assert.True(t, br.closed)
		assert.True(t, rows.closed, "rows are read before the next result")
		assert.Equal(t, []string{"INSERT INTO t VALUES ($1), ($2)", "SELECT n FROM t WHERE n > $1"}, q.queued)

		assert.Equal(t, 0, results[0].Index)
		assert.Equal(t, domain.ExecuteResult{RowsAffected: 2}, results[0].Result)
		res := results[1].Result.(domain.Result)
		require.True(t, res.Next())
		var n int
		require.NoError(t, res.Scan(&n))
		assert.Equal(t, 5, n)
		assert.False(t, res.Next())
	})

	t.Run("a failed operation fails the batch", func(t *testing.T) {
		dup := &pgconn.PgError{Code: "23505", ConstraintName: "t_pkey"}
		br := &fakeBatchResults{results: []any{pgconn.NewCommandTag("INSERT 0 1"), dup}}
		results, err := executor{db: &fakeQuerier{batch: br}}.batch(ctx, []domain.Operation{
			command("INSERT INTO t VALUES (1)"),
			command("INSERT INTO t VALUES (1)"),
			query("SELECT 1"),
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrDuplicateKey)
		assert.NoError(t, results[0].Error)
		assert.ErrorIs(t, results[1].Error, domain.ErrDuplicateKey)
		assert.Same(t, err, results[1].Error)
		assert.ErrorIs(t, results[2].Error, domain.ErrDuplicateKey, "later operations report the failure as cause")
		assert.Contains(t, results[2].Error.Error(), "not executed")
		assert.Equal(t, 2, br.pos, "nothing is read after the failure")
		assert.True(t, br.closed)
	})

	t.Run("a failed query result", func(t *testing.T) {
		br := &fakeBatchResults{results: []any{&fakeRows{err: &pgconn.PgError{Code: "22012"}}}}
		results, err := executor{db: &fakeQuerier{batch: br}}.batch(ctx, []domain.Operation{query("SELECT 1/0")})
		assert.ErrorIs(t, err, domain.ErrInvalidData)
		assert.ErrorIs(t, results[0].Error, domain.ErrInvalidData)
		assert.Nil(t, results[0].Result)
	})

	t.Run("a Close error is reported", func(t *testing.T) {
		br := &fakeBatchResults{results: []any{pgconn.NewCommandTag("DELETE 0")}, closeErr: errors.New("conn lost")}
		_, err := executor{db: &fakeQuerier{batch: br}}.batch(ctx, []domain.Operation{command("DELETE FROM t")})
		assert.ErrorIs(t, err, domain.ErrQueryFailed)
	})

	t.Run("invalid operations reject the batch before sending", func(t *testing.T) {
		for name, ops := range map[string][]domain.Operation{
			"unknown type": {command("DELETE FROM t"), {Type: 42}},
			"empty SQL":    {command("DELETE FROM t"), query("")},
		} {
			q := &fakeQuerier{}
			results, err := executor{db: q}.batch(ctx, ops)
			assert.ErrorIs(t, err, domain.ErrInvalidQuery, name)
			assert.ErrorIs(t, results[1].Error, domain.ErrInvalidQuery, name)
			assert.False(t, q.sentBatch, name)
		}
	})

	t.Run("no operations", func(t *testing.T) {
		q := &fakeQuerier{}
		results, err := executor{db: q}.batch(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, results)
		assert.False(t, q.sentBatch)
	})

	t.Run("timeout is the sum of the operation timeouts", func(t *testing.T) {
		br := &fakeBatchResults{results: []any{pgconn.NewCommandTag("DELETE 0"), pgconn.NewCommandTag("DELETE 0")}}
		q := &fakeQuerier{batch: br}
		ops := []domain.Operation{command("DELETE FROM a"), command("DELETE FROM b")}
		ops[0].Command.Options.Timeout = time.Minute
		_, err := executor{db: q, queryTimeout: 2 * time.Minute}.batch(ctx, ops)
		require.NoError(t, err)
		assert.InDelta(t, 3*time.Minute, deadlineIn(t, q.ctx), float64(time.Second))

		br = &fakeBatchResults{results: []any{pgconn.NewCommandTag("DELETE 0"), pgconn.NewCommandTag("DELETE 0")}}
		q = &fakeQuerier{batch: br}
		_, err = executor{db: q}.batch(ctx, ops)
		require.NoError(t, err)
		_, bounded := q.ctx.Deadline()
		assert.False(t, bounded, "an operation without a timeout makes the batch unbounded")
	})
}
