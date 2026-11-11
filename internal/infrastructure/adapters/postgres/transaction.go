package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// PostgreSQLTransaction implements the domain.Transaction interface for PostgreSQL.
//
// Commit, Rollback and the savepoint methods run under the context given to
// BeginTx, bounded by TxOptions.Timeout (or Config.TxTimeout). Once that context
// is done, statements are rejected and Commit fails, rolling the transaction
// back; Rollback must still be called (or Commit attempted) to release it.
type PostgreSQLTransaction struct {
	tx     pgx.Tx
	exec   executor
	ctx    context.Context
	cancel context.CancelFunc
	id     string
	active bool
	mu     sync.RWMutex
}

func newTransaction(ctx context.Context, cancel context.CancelFunc, tx pgx.Tx, queryTimeout time.Duration) *PostgreSQLTransaction {
	return &PostgreSQLTransaction{
		tx:     tx,
		exec:   executor{db: tx, queryTimeout: queryTimeout},
		ctx:    ctx,
		cancel: cancel,
		id:     newTxID(),
		active: true,
	}
}

// Query executes a query within the transaction.
func (t *PostgreSQLTransaction) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if err := t.usable(); err != nil {
		return nil, err
	}
	return t.exec.query(ctx, query)
}

// QueryOne executes a query and returns its first row within the transaction.
func (t *PostgreSQLTransaction) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if err := t.usable(); err != nil {
		return nil, err
	}
	return t.exec.queryOne(ctx, query)
}

// Execute executes a command within the transaction.
func (t *PostgreSQLTransaction) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if err := t.usable(); err != nil {
		return domain.ExecuteResult{}, err
	}
	return t.exec.execute(ctx, command)
}

// Batch executes multiple operations within the transaction; see
// PostgreSQLStorage.Batch. A failed operation aborts the transaction.
func (t *PostgreSQLTransaction) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if err := t.usable(); err != nil {
		return nil, err
	}
	return t.exec.batch(ctx, operations)
}

// Commit commits the transaction. The transaction is over after any Commit
// attempt, successful or not.
func (t *PostgreSQLTransaction) Commit() error {
	return t.end("COMMIT", "commit failed", t.tx.Commit)
}

// Rollback rolls back the transaction. The transaction is over after any
// Rollback attempt, successful or not.
func (t *PostgreSQLTransaction) Rollback() error {
	return t.end("ROLLBACK", "rollback failed", t.tx.Rollback)
}

func (t *PostgreSQLTransaction) end(sql, msg string, finish func(context.Context) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return domain.NewTransactionClosedError()
	}
	t.active = false // pgx closes the transaction even when COMMIT/ROLLBACK fails
	defer t.cancel()

	return mapError(finish(t.ctx), opTx, msg, sql)
}

// Savepoint creates a savepoint within the transaction.
func (t *PostgreSQLTransaction) Savepoint(name string) error {
	return t.savepointCommand("SAVEPOINT ", name, "savepoint failed")
}

// RollbackToSavepoint rolls back to a savepoint.
func (t *PostgreSQLTransaction) RollbackToSavepoint(name string) error {
	return t.savepointCommand("ROLLBACK TO SAVEPOINT ", name, "rollback to savepoint failed")
}

func (t *PostgreSQLTransaction) savepointCommand(command, name, msg string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if err := t.usable(); err != nil {
		return err
	}
	if name == "" {
		return domain.NewStorageError(domain.ErrorTypeValidation, "INVALID_PARAMETERS", "savepoint name is required")
	}

	sql := command + quoteIdent(name)
	_, err := t.tx.Exec(t.ctx, sql)
	return mapError(err, opTx, msg, sql)
}

// usable reports why the transaction cannot run statements, if it cannot.
// Callers hold t.mu.
func (t *PostgreSQLTransaction) usable() error {
	if !t.active {
		return domain.NewTransactionClosedError()
	}
	if err := t.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.NewTransactionTimeoutError().WithCause(err)
		}
		return domain.NewTransactionError("TRANSACTION_FAILED", "transaction context is canceled").WithCause(err)
	}
	return nil
}

// IsActive returns whether the transaction is active
func (t *PostgreSQLTransaction) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.active
}

// ID returns the transaction ID
func (t *PostgreSQLTransaction) ID() string {
	return t.id
}

// newTxID returns a random transaction ID.
func newTxID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails: crypto/rand.Read aborts the program instead
	return "tx_" + hex.EncodeToString(b[:])
}
