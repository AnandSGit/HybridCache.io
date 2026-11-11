package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// fakeTx is a pgx.Tx whose statements go to a fakeQuerier. Methods the
// transaction does not use panic through the nil embedded interface.
type fakeTx struct {
	pgx.Tx
	q           *fakeQuerier
	commitErr   error
	rollbackErr error
	endCtx      context.Context
	ended       int
}

func (tx *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.q.Exec(ctx, sql, args...)
}

func (tx *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.q.Query(ctx, sql, args...)
}

func (tx *fakeTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return tx.q.SendBatch(ctx, b)
}

func (tx *fakeTx) Commit(ctx context.Context) error {
	tx.endCtx, tx.ended = ctx, tx.ended+1
	return tx.commitErr
}

func (tx *fakeTx) Rollback(ctx context.Context) error {
	tx.endCtx, tx.ended = ctx, tx.ended+1
	return tx.rollbackErr
}

func newFakeTransaction(timeout time.Duration) (*PostgreSQLTransaction, *fakeTx) {
	ftx := &fakeTx{q: &fakeQuerier{rows: &fakeRows{}}}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return newTransaction(ctx, cancel, ftx, 0), ftx
}

func TestTransaction_CommitUsesTransactionContext(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Minute)
	require.True(t, tx.IsActive())

	require.NoError(t, tx.Commit())
	assert.InDelta(t, time.Minute, deadlineIn(t, ftx.endCtx), float64(time.Second), "COMMIT runs under the BeginTx context")
	assert.ErrorIs(t, ftx.endCtx.Err(), context.Canceled, "the context is released when the transaction ends")
	assert.False(t, tx.IsActive())

	assert.ErrorIs(t, tx.Commit(), domain.ErrTransactionClosed)
	assert.ErrorIs(t, tx.Rollback(), domain.ErrTransactionClosed)
	assert.Equal(t, 1, ftx.ended)
}

func TestTransaction_InactiveAfterFailedCommit(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Minute)
	ftx.commitErr = pgx.ErrTxCommitRollback

	err := tx.Commit()
	assert.ErrorIs(t, err, domain.ErrTransactionFailed)
	assert.False(t, tx.IsActive(), "pgx closes the transaction even when COMMIT fails")
	assert.ErrorIs(t, tx.Rollback(), domain.ErrTransactionClosed)

	_, err = tx.Execute(context.Background(), domain.Command{SQL: "DELETE FROM t"})
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
}

func TestTransaction_RollbackFailure(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Minute)
	ftx.rollbackErr = errors.New("conn lost")
	assert.ErrorIs(t, tx.Rollback(), domain.ErrTransactionFailed)
	assert.False(t, tx.IsActive())
}

func TestTransaction_TimeoutRejectsStatements(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Millisecond)
	<-tx.ctx.Done()

	_, err := tx.Query(context.Background(), domain.Query{SQL: "SELECT 1"})
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
	_, err = tx.QueryOne(context.Background(), domain.Query{SQL: "SELECT 1"})
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
	_, err = tx.Execute(context.Background(), domain.Command{SQL: "DELETE FROM t"})
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
	_, err = tx.Batch(context.Background(), []domain.Operation{{Type: domain.OperationTypeCommand, Command: domain.Command{SQL: "DELETE FROM t"}}})
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
	assert.ErrorIs(t, tx.Savepoint("sp"), domain.ErrTransactionTimeout)
	assert.Nil(t, ftx.q.ctx, "nothing is sent after the transaction timed out")

	ftx.commitErr = context.DeadlineExceeded // what pgx returns for an expired context
	assert.ErrorIs(t, tx.Commit(), domain.ErrTransactionTimeout)
	assert.False(t, tx.IsActive())
}

func TestTransaction_CanceledContext(t *testing.T) {
	ftx := &fakeTx{q: &fakeQuerier{}}
	ctx, cancel := context.WithCancel(context.Background())
	tx := newTransaction(ctx, cancel, ftx, 0)
	cancel()
	_, err := tx.Execute(context.Background(), domain.Command{SQL: "DELETE FROM t"})
	assert.ErrorIs(t, err, domain.ErrTransactionFailed)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTransaction_Statements(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Minute)
	ctx := context.Background()

	_, err := tx.Execute(ctx, domain.Command{SQL: "UPDATE t SET a = ? WHERE id = ?", Parameters: []any{1, 2}})
	require.NoError(t, err)
	assert.Equal(t, "UPDATE t SET a = $1 WHERE id = $2", ftx.q.sql)

	res, err := tx.Query(ctx, domain.Query{SQL: "SELECT ?", Parameters: []any{1}})
	require.NoError(t, err)
	require.NoError(t, res.Close())

	row, err := tx.QueryOne(ctx, domain.Query{SQL: "SELECT 1"})
	require.NoError(t, err)
	assert.ErrorIs(t, row.Err(), domain.ErrNoRows)

	ftx.q.batch = &fakeBatchResults{results: []any{pgconn.NewCommandTag("DELETE 3")}}
	results, err := tx.Batch(ctx, []domain.Operation{{Type: domain.OperationTypeCommand, Command: domain.Command{SQL: "DELETE FROM t"}}})
	require.NoError(t, err)
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 3}, results[0].Result)
}

func TestTransaction_Savepoints(t *testing.T) {
	tx, ftx := newFakeTransaction(time.Minute)

	require.NoError(t, tx.Savepoint(`sp "1"; COMMIT`))
	assert.Equal(t, `SAVEPOINT "sp ""1""; COMMIT"`, ftx.q.sql)
	assert.InDelta(t, time.Minute, deadlineIn(t, ftx.q.ctx), float64(time.Second), "savepoints run under the transaction context")

	require.NoError(t, tx.RollbackToSavepoint("sp"))
	assert.Equal(t, `ROLLBACK TO SAVEPOINT "sp"`, ftx.q.sql)

	assert.ErrorIs(t, tx.Savepoint(""), domain.ErrInvalidParameters)

	ftx.q.execErr = &pgconn.PgError{Code: "3B001"}
	assert.ErrorIs(t, tx.RollbackToSavepoint("missing"), domain.ErrTransactionFailed)
}

func TestNewTxID(t *testing.T) {
	pattern := regexp.MustCompile(`^tx_[0-9a-f]{32}$`)
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := newTxID()
		require.Regexp(t, pattern, id)
		require.False(t, seen[id], "duplicate transaction ID %s", id)
		seen[id] = true
	}

	tx, _ := newFakeTransaction(time.Minute)
	assert.Regexp(t, pattern, tx.ID())
}
