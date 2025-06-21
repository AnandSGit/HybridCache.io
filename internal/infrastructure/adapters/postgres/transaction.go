package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/HybridCache.io/storage/pkg/storage"
)

// PostgreSQLTransaction implements the storage.Transaction interface for PostgreSQL
type PostgreSQLTransaction struct {
	tx      pgx.Tx
	adapter *Adapter
	id      string
	active  bool
	mu      sync.RWMutex
}

// Query executes a query within the transaction
func (t *PostgreSQLTransaction) Query(ctx context.Context, query storage.Query) (storage.Result, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return nil, storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	sql, params, err := t.adapter.TranslateQuery(query)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}
	
	rows, err := t.tx.Query(ctx, sql, params...)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}
	
	return &PostgreSQLResult{rows: rows}, nil
}

// QueryOne executes a query and returns a single row within the transaction
func (t *PostgreSQLTransaction) QueryOne(ctx context.Context, query storage.Query) (storage.Row, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return nil, storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	sql, params, err := t.adapter.TranslateQuery(query)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}
	
	row := t.tx.QueryRow(ctx, sql, params...)
	return &PostgreSQLRow{row: row}, nil
}

// Execute executes a command within the transaction
func (t *PostgreSQLTransaction) Execute(ctx context.Context, command storage.Command) (storage.ExecuteResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return storage.ExecuteResult{}, storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	sql, params, err := t.adapter.TranslateCommand(command)
	if err != nil {
		return storage.ExecuteResult{}, storage.NewQueryError("COMMAND_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}
	
	result, err := t.tx.Exec(ctx, sql, params...)
	if err != nil {
		return storage.ExecuteResult{}, storage.NewQueryError("COMMAND_EXECUTION_FAILED", err.Error()).WithCause(err)
	}
	
	return storage.ExecuteResult{
		RowsAffected: result.RowsAffected(),
		LastInsertID: 0, // PostgreSQL doesn't support LastInsertID
	}, nil
}

// Batch executes multiple operations within the transaction
func (t *PostgreSQLTransaction) Batch(ctx context.Context, operations []storage.Operation) ([]storage.OperationResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return nil, storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	results := make([]storage.OperationResult, len(operations))
	
	batch := &pgx.Batch{}
	
	// Add all operations to the batch
	for i, op := range operations {
		switch op.Type {
		case storage.OperationTypeQuery:
			sql, params, err := t.adapter.TranslateQuery(op.Query)
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err),
				}
				continue
			}
			batch.Queue(sql, params...)
		case storage.OperationTypeCommand:
			sql, params, err := t.adapter.TranslateCommand(op.Command)
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("COMMAND_TRANSLATION_FAILED", err.Error()).WithCause(err),
				}
				continue
			}
			batch.Queue(sql, params...)
		}
	}
	
	// Execute the batch
	batchResults := t.tx.SendBatch(ctx, batch)
	defer batchResults.Close()
	
	// Process results
	for i, op := range operations {
		if results[i].Error != nil {
			continue // Skip operations that failed translation
		}
		
		switch op.Type {
		case storage.OperationTypeQuery:
			rows, err := batchResults.Query()
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("BATCH_QUERY_FAILED", err.Error()).WithCause(err),
				}
			} else {
				results[i] = storage.OperationResult{
					Index:  i,
					Result: &PostgreSQLResult{rows: rows},
				}
			}
		case storage.OperationTypeCommand:
			cmdTag, err := batchResults.Exec()
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("BATCH_COMMAND_FAILED", err.Error()).WithCause(err),
				}
			} else {
				results[i] = storage.OperationResult{
					Index: i,
					Result: storage.ExecuteResult{
						RowsAffected: cmdTag.RowsAffected(),
						LastInsertID: 0,
					},
				}
			}
		}
	}
	
	return results, nil
}

// Commit commits the transaction
func (t *PostgreSQLTransaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if !t.active {
		return storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	err := t.tx.Commit(context.Background())
	if err != nil {
		return storage.NewTransactionError("COMMIT_FAILED", err.Error()).WithCause(err)
	}
	
	t.active = false
	return nil
}

// Rollback rolls back the transaction
func (t *PostgreSQLTransaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if !t.active {
		return storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	err := t.tx.Rollback(context.Background())
	if err != nil {
		return storage.NewTransactionError("ROLLBACK_FAILED", err.Error()).WithCause(err)
	}
	
	t.active = false
	return nil
}

// Savepoint creates a savepoint within the transaction
func (t *PostgreSQLTransaction) Savepoint(name string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	sql := fmt.Sprintf("SAVEPOINT %s", name)
	_, err := t.tx.Exec(context.Background(), sql)
	if err != nil {
		return storage.NewTransactionError("SAVEPOINT_FAILED", err.Error()).WithCause(err)
	}
	
	return nil
}

// RollbackToSavepoint rolls back to a savepoint
func (t *PostgreSQLTransaction) RollbackToSavepoint(name string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.active {
		return storage.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}
	
	sql := fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name)
	_, err := t.tx.Exec(context.Background(), sql)
	if err != nil {
		return storage.NewTransactionError("ROLLBACK_TO_SAVEPOINT_FAILED", err.Error()).WithCause(err)
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
