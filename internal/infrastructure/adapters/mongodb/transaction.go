package mongodb

import (
	"context"
	"errors"
	"sync"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MongoDBTransaction implements domain.Transaction on a MongoDB session.
// Operations are serialised because a session must not be used concurrently;
// results returned by Query read through the transaction, so consume them
// before Commit or Rollback.
type MongoDBTransaction struct {
	mu       sync.Mutex
	session  *mongo.Session
	exec     *executor
	ctx      context.Context    // BeginTx context, bounded by the transaction timeout
	cancel   context.CancelFunc // releases ctx when the transaction ends
	id       string
	readOnly bool
	active   bool
}

// sessionContext checks that the transaction can run an operation and returns
// ctx carrying the transaction's session.
func (t *MongoDBTransaction) sessionContext(ctx context.Context) (context.Context, error) {
	if !t.active {
		return nil, domain.NewTransactionClosedError()
	}
	if err := t.ctx.Err(); err != nil {
		// Like database/sql, a transaction is done once its BeginTx context is.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, domain.NewTransactionTimeoutError().WithCause(err)
		}
		return nil, domain.NewTransactionClosedError().WithCause(err)
	}
	return mongo.NewSessionContext(ctx, t.session), nil
}

// Query executes a query within the transaction.
func (t *MongoDBTransaction) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sctx, err := t.sessionContext(ctx)
	if err != nil {
		return nil, err
	}
	return t.exec.query(sctx, query)
}

// QueryOne executes a query within the transaction and returns its first row.
func (t *MongoDBTransaction) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sctx, err := t.sessionContext(ctx)
	if err != nil {
		return nil, err
	}
	return t.exec.queryOne(sctx, query)
}

// Execute executes a command within the transaction.
func (t *MongoDBTransaction) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sctx, err := t.sessionContext(ctx)
	if err != nil {
		return domain.ExecuteResult{}, err
	}
	if t.readOnly {
		return domain.ExecuteResult{}, errReadOnlyTransaction()
	}
	return t.exec.execute(sctx, command)
}

// Batch executes the operations sequentially within the transaction.
func (t *MongoDBTransaction) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sctx, err := t.sessionContext(ctx)
	if err != nil {
		return nil, err
	}
	return t.exec.batch(sctx, operations, t.readOnly), nil
}

// Commit commits the transaction under the BeginTx context. The transaction
// ends whether or not the commit succeeds.
func (t *MongoDBTransaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return domain.NewTransactionClosedError()
	}
	defer t.end()
	if err := t.session.CommitTransaction(t.ctx); err != nil {
		return transactionError(err, "failed to commit MongoDB transaction")
	}
	return nil
}

// Rollback aborts the transaction under the BeginTx context. The transaction
// ends whether or not the abort succeeds.
func (t *MongoDBTransaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return domain.NewTransactionClosedError()
	}
	defer t.end()
	if err := t.session.AbortTransaction(t.ctx); err != nil {
		return transactionError(err, "failed to roll back MongoDB transaction")
	}
	return nil
}

// end marks the transaction inactive, ends its session and releases its context.
func (t *MongoDBTransaction) end() {
	t.active = false
	t.session.EndSession(t.ctx)
	t.cancel()
}

// Savepoint is not supported: MongoDB transactions have no savepoints.
func (t *MongoDBTransaction) Savepoint(name string) error {
	return domain.NewUnsupportedOperationError("savepoint")
}

// RollbackToSavepoint is not supported: MongoDB transactions have no savepoints.
func (t *MongoDBTransaction) RollbackToSavepoint(name string) error {
	return domain.NewUnsupportedOperationError("savepoint")
}

// IsActive reports whether the transaction has not yet been committed or rolled back.
func (t *MongoDBTransaction) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

// ID returns the transaction's unique identifier.
func (t *MongoDBTransaction) ID() string {
	return t.id
}

func errReadOnlyTransaction() error {
	return domain.NewTransactionError("INVALID_TRANSACTION", "cannot execute a write command in a read-only transaction")
}

func transactionError(err error, message string) error {
	if mongo.IsTimeout(err) {
		return domain.NewTransactionTimeoutError().WithCause(err)
	}
	se := domain.NewTransactionError("TRANSACTION_FAILED", message).WithCause(err)
	// Only a transient error guarantees the transaction was aborted, so that
	// retrying it cannot apply it twice (unlike UnknownTransactionCommitResult).
	if hasErrorLabel(err, "TransientTransactionError") {
		se.WithRetryable(true).WithTemporary(true)
	}
	return se
}
