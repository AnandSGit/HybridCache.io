package mongodb

import (
	"context"
	"fmt"
	"sync"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDBTransaction implements the domain.Transaction interface for MongoDB
type MongoDBTransaction struct {
	session  mongo.Session
	database *mongo.Database
	adapter  *Adapter
	active   bool
	mu       sync.RWMutex
}

// Query executes a query within the transaction
func (t *MongoDBTransaction) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.active {
		return nil, domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	// Execute query within session context
	sessionCtx := mongo.NewSessionContext(ctx, t.session)

	// Extract collection name from query
	collectionName := t.extractCollectionName(query.SQL)
	if collectionName == "" {
		return nil, domain.NewQueryError("INVALID_QUERY", "collection name not found in query")
	}

	collection := t.database.Collection(collectionName)

	// Build filter from parameters
	filter := t.buildFilter(query.Parameters)

	// Execute find operation within transaction
	cursor, err := collection.Find(sessionCtx, filter)
	if err != nil {
		return nil, domain.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return &MongoDBResult{cursor: cursor}, nil
}

// QueryOne executes a query and returns a single row within the transaction
func (t *MongoDBTransaction) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.active {
		return nil, domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	// Execute query within session context
	sessionCtx := mongo.NewSessionContext(ctx, t.session)

	// Extract collection name from query
	collectionName := t.extractCollectionName(query.SQL)
	if collectionName == "" {
		return nil, domain.NewQueryError("INVALID_QUERY", "collection name not found in query")
	}

	collection := t.database.Collection(collectionName)

	// Build filter from parameters
	filter := t.buildFilter(query.Parameters)

	// Execute findOne operation within transaction
	var result bson.M
	err := collection.FindOne(sessionCtx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, domain.NewDataError("NO_ROWS", "no document found")
		}
		return nil, domain.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return &MongoDBRow{document: result}, nil
}

// Execute executes a command within the transaction
func (t *MongoDBTransaction) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.active {
		return domain.ExecuteResult{}, domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	// Execute command within session context
	sessionCtx := mongo.NewSessionContext(ctx, t.session)

	// Parse command type and execute appropriate MongoDB operation
	switch command.Type {
	case domain.CommandTypeInsert:
		return t.executeInsert(sessionCtx, command)
	case domain.CommandTypeUpdate:
		return t.executeUpdate(sessionCtx, command)
	case domain.CommandTypeDelete:
		return t.executeDelete(sessionCtx, command)
	default:
		return domain.ExecuteResult{}, domain.NewQueryError("UNSUPPORTED_COMMAND", "command type not supported in transaction")
	}
}

// Batch executes multiple operations within the transaction
func (t *MongoDBTransaction) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.active {
		return nil, domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	// Execute batch within session context
	sessionCtx := mongo.NewSessionContext(ctx, t.session)
	results := make([]domain.OperationResult, len(operations))

	// Group operations by collection for bulk operations
	collectionOps := make(map[string][]domain.Operation)
	opIndexes := make(map[string][]int)

	for i, op := range operations {
		var collectionName string
		if op.Type == domain.OperationTypeQuery {
			collectionName = t.extractCollectionName(op.Query.SQL)
		} else if op.Type == domain.OperationTypeCommand {
			collectionName = t.extractCollectionName(op.Command.SQL)
		}

		if collectionName == "" {
			results[i] = domain.OperationResult{
				Index: i,
				Error: domain.NewQueryError("INVALID_OPERATION", "collection name not found"),
			}
			continue
		}
		collectionOps[collectionName] = append(collectionOps[collectionName], op)
		opIndexes[collectionName] = append(opIndexes[collectionName], i)
	}

	// Execute bulk operations for each collection
	for collectionName, ops := range collectionOps {
		collection := t.database.Collection(collectionName)
		indexes := opIndexes[collectionName]

		// Build bulk write operations
		var bulkOps []mongo.WriteModel
		for _, op := range ops {
			if op.Type == domain.OperationTypeCommand {
				switch op.Command.Type {
				case domain.CommandTypeInsert:
					doc := t.buildDocument(op.Command.Parameters)
					bulkOps = append(bulkOps, mongo.NewInsertOneModel().SetDocument(doc))
				case domain.CommandTypeUpdate:
					filter := t.buildFilter(op.Command.Parameters[:len(op.Command.Parameters)/2])
					update := t.buildUpdate(op.Command.Parameters[len(op.Command.Parameters)/2:])
					bulkOps = append(bulkOps, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update))
				case domain.CommandTypeDelete:
					filter := t.buildFilter(op.Command.Parameters)
					bulkOps = append(bulkOps, mongo.NewDeleteOneModel().SetFilter(filter))
				}
			}
		}

		// Execute bulk write within transaction
		if len(bulkOps) > 0 {
			_, err := collection.BulkWrite(sessionCtx, bulkOps)
			if err != nil {
				// Mark all operations for this collection as failed
				for _, idx := range indexes {
					results[idx] = domain.OperationResult{
						Index: idx,
						Error: domain.NewQueryError("BULK_WRITE_FAILED", err.Error()).WithCause(err),
					}
				}
			} else {
				// Mark all operations for this collection as successful
				for _, idx := range indexes {
					results[idx] = domain.OperationResult{
						Index:  idx,
						Result: domain.ExecuteResult{RowsAffected: 1},
					}
				}
			}
		}
	}

	return results, nil
}

// Commit commits the transaction
func (t *MongoDBTransaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	ctx := context.Background()
	err := t.session.CommitTransaction(ctx)
	if err != nil {
		return domain.NewTransactionError("COMMIT_FAILED", "failed to commit MongoDB transaction").WithCause(err)
	}

	t.active = false
	t.session.EndSession(ctx)
	return nil
}

// Rollback rolls back the transaction
func (t *MongoDBTransaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return domain.NewTransactionError("TRANSACTION_CLOSED", "transaction is not active")
	}

	ctx := context.Background()
	err := t.session.AbortTransaction(ctx)
	if err != nil {
		return domain.NewTransactionError("ROLLBACK_FAILED", "failed to rollback MongoDB transaction").WithCause(err)
	}

	t.active = false
	t.session.EndSession(ctx)
	return nil
}

// Savepoint creates a savepoint (not supported in MongoDB)
func (t *MongoDBTransaction) Savepoint(name string) error {
	return domain.NewTransactionError("SAVEPOINT_NOT_SUPPORTED", "MongoDB does not support savepoints")
}

// RollbackToSavepoint rolls back to a savepoint (not supported in MongoDB)
func (t *MongoDBTransaction) RollbackToSavepoint(name string) error {
	return domain.NewTransactionError("SAVEPOINT_NOT_SUPPORTED", "MongoDB does not support savepoints")
}

// IsActive returns whether the transaction is active
func (t *MongoDBTransaction) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.active
}

// ID returns the transaction ID
func (t *MongoDBTransaction) ID() string {
	// MongoDB sessions don't have a simple string ID
	// Return a placeholder for now
	return "mongodb-transaction"
}

// Helper methods (reuse from storage.go)

// extractCollectionName extracts collection name from query SQL
func (t *MongoDBTransaction) extractCollectionName(sql string) string {
	// Simple extraction - in practice, you'd have a proper parser
	if sql == "" {
		return ""
	}
	return "default_collection"
}

// buildFilter builds a MongoDB filter from parameters
func (t *MongoDBTransaction) buildFilter(params []interface{}) bson.M {
	filter := bson.M{}

	for i := 0; i < len(params); i += 2 {
		if i+1 < len(params) {
			if key, ok := params[i].(string); ok {
				filter[key] = params[i+1]
			}
		}
	}

	return filter
}

// buildDocument builds a MongoDB document from parameters
func (t *MongoDBTransaction) buildDocument(params []interface{}) bson.M {
	return t.buildFilter(params)
}

// buildUpdate builds a MongoDB update document from parameters
func (t *MongoDBTransaction) buildUpdate(params []interface{}) bson.M {
	update := bson.M{"$set": t.buildFilter(params)}
	return update
}

// executeInsert executes an insert command within transaction
func (t *MongoDBTransaction) executeInsert(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := t.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := t.database.Collection(collectionName)
	document := t.buildDocument(command.Parameters)

	result, err := collection.InsertOne(ctx, document)
	if err != nil {
		return domain.ExecuteResult{}, domain.NewQueryError("INSERT_FAILED", err.Error()).WithCause(err)
	}

	// Convert InsertedID to int64 (MongoDB ObjectID as hash)
	var lastInsertID int64
	if result.InsertedID != nil {
		// For MongoDB, we'll use a hash of the ObjectID string representation
		lastInsertID = int64(fmt.Sprintf("%v", result.InsertedID)[0:8][0]) // Simplified conversion
	}

	return domain.ExecuteResult{
		RowsAffected: 1,
		LastInsertID: lastInsertID,
	}, nil
}

// executeUpdate executes an update command within transaction
func (t *MongoDBTransaction) executeUpdate(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := t.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := t.database.Collection(collectionName)

	mid := len(command.Parameters) / 2
	filter := t.buildFilter(command.Parameters[:mid])
	update := t.buildUpdate(command.Parameters[mid:])

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return domain.ExecuteResult{}, domain.NewQueryError("UPDATE_FAILED", err.Error()).WithCause(err)
	}

	return domain.ExecuteResult{
		RowsAffected: result.ModifiedCount,
	}, nil
}

// executeDelete executes a delete command within transaction
func (t *MongoDBTransaction) executeDelete(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := t.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := t.database.Collection(collectionName)
	filter := t.buildFilter(command.Parameters)

	result, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		return domain.ExecuteResult{}, domain.NewQueryError("DELETE_FAILED", err.Error()).WithCause(err)
	}

	return domain.ExecuteResult{
		RowsAffected: result.DeletedCount,
	}, nil
}
