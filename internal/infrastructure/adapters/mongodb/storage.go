package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// MongoDBStorage implements the domain.Storage interface for MongoDB
type MongoDBStorage struct {
	client   *mongo.Client
	database *mongo.Database
	adapter  *Adapter
	config   domain.Config
}

// Connect establishes connection to MongoDB (already done in adapter)
func (s *MongoDBStorage) Connect(ctx context.Context) error {
	// Connection is already established in the adapter
	return s.Ping(ctx)
}

// Close closes the MongoDB connection
func (s *MongoDBStorage) Close() error {
	if s.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.client.Disconnect(ctx)
	}
	return nil
}

// Ping tests the MongoDB connection
func (s *MongoDBStorage) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, readpref.Primary())
}

// Health returns the health status of MongoDB
func (s *MongoDBStorage) Health(ctx context.Context) domain.HealthStatus {
	status := domain.HealthStatus{
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}

	// Test connection
	if err := s.Ping(ctx); err != nil {
		status.Status = domain.HealthStatusUnhealthy
		status.Message = fmt.Sprintf("MongoDB ping failed: %v", err)
		return status
	}

	// Get server status
	var serverStatus bson.M
	err := s.database.RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus)
	if err != nil {
		status.Status = domain.HealthStatusDegraded
		status.Message = fmt.Sprintf("Failed to get server status: %v", err)
		return status
	}

	status.Status = domain.HealthStatusHealthy
	status.Message = "MongoDB is healthy"

	// Add server details
	if host, ok := serverStatus["host"].(string); ok {
		status.Details["host"] = host
	}
	if version, ok := serverStatus["version"].(string); ok {
		status.Details["version"] = version
	}
	if uptime, ok := serverStatus["uptime"].(int64); ok {
		status.Details["uptime_seconds"] = uptime
	}

	return status
}

// Query executes a query and returns results
func (s *MongoDBStorage) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	// For MongoDB, we need to parse the query and determine the operation
	// This is a simplified implementation - in practice, you'd have a more sophisticated query parser

	// For now, assume the query contains collection name and filter
	// In a real implementation, you'd parse SQL-like syntax or use MongoDB-specific query format

	// Extract collection name from query (simplified)
	collectionName := s.extractCollectionName(query.SQL)
	if collectionName == "" {
		return nil, domain.NewQueryError("INVALID_QUERY", "collection name not found in query")
	}

	collection := s.database.Collection(collectionName)

	// Build filter from parameters (simplified)
	filter := s.buildFilter(query.Parameters)

	// Execute find operation
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, domain.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return &MongoDBResult{cursor: cursor}, nil
}

// QueryOne executes a query and returns a single row
func (s *MongoDBStorage) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	// Extract collection name from query
	collectionName := s.extractCollectionName(query.SQL)
	if collectionName == "" {
		return nil, domain.NewQueryError("INVALID_QUERY", "collection name not found in query")
	}

	collection := s.database.Collection(collectionName)

	// Build filter from parameters
	filter := s.buildFilter(query.Parameters)

	// Execute findOne operation
	var result bson.M
	err := collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, domain.NewDataError("NO_ROWS", "no document found")
		}
		return nil, domain.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return &MongoDBRow{document: result}, nil
}

// Execute executes a command and returns the result
func (s *MongoDBStorage) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	// Parse command type and execute appropriate MongoDB operation
	switch command.Type {
	case domain.CommandTypeInsert:
		return s.executeInsert(ctx, command)
	case domain.CommandTypeUpdate:
		return s.executeUpdate(ctx, command)
	case domain.CommandTypeDelete:
		return s.executeDelete(ctx, command)
	default:
		return domain.ExecuteResult{}, domain.NewQueryError("UNSUPPORTED_COMMAND", fmt.Sprintf("command type %v not supported", command.Type))
	}
}

// BeginTx begins a new transaction
func (s *MongoDBStorage) BeginTx(ctx context.Context, opts *domain.TxOptions) (domain.Transaction, error) {
	session, err := s.client.StartSession()
	if err != nil {
		return nil, domain.NewTransactionError("TX_START_FAILED", "failed to start MongoDB session").WithCause(err)
	}

	// Configure transaction options
	txnOpts := options.Transaction()
	if opts != nil {
		// Map isolation levels (MongoDB has different semantics)
		switch opts.Isolation {
		case domain.IsolationLevelReadCommitted:
			txnOpts.SetReadConcern(readconcern.Majority())
		case domain.IsolationLevelSerializable:
			txnOpts.SetReadConcern(readconcern.Linearizable())
		}

		if opts.ReadOnly {
			txnOpts.SetReadPreference(readpref.SecondaryPreferred())
		}
	}

	// Start transaction
	err = session.StartTransaction(txnOpts)
	if err != nil {
		session.EndSession(ctx)
		return nil, domain.NewTransactionError("TX_START_FAILED", "failed to start MongoDB transaction").WithCause(err)
	}

	return &MongoDBTransaction{
		session:  session,
		database: s.database,
		adapter:  s.adapter,
		active:   true,
	}, nil
}

// Batch executes multiple operations in a batch
func (s *MongoDBStorage) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	results := make([]domain.OperationResult, len(operations))

	// Group operations by collection for bulk operations
	collectionOps := make(map[string][]domain.Operation)
	for i, op := range operations {
		var collectionName string
		if op.Type == domain.OperationTypeQuery {
			collectionName = s.extractCollectionName(op.Query.SQL)
		} else if op.Type == domain.OperationTypeCommand {
			collectionName = s.extractCollectionName(op.Command.SQL)
		}

		if collectionName == "" {
			results[i] = domain.OperationResult{
				Index: i,
				Error: domain.NewQueryError("INVALID_OPERATION", "collection name not found"),
			}
			continue
		}
		collectionOps[collectionName] = append(collectionOps[collectionName], op)
	}

	// Execute bulk operations for each collection
	for collectionName, ops := range collectionOps {
		collection := s.database.Collection(collectionName)

		// Build bulk write operations
		var bulkOps []mongo.WriteModel
		for _, op := range ops {
			if op.Type == domain.OperationTypeCommand {
				switch op.Command.Type {
				case domain.CommandTypeInsert:
					doc := s.buildDocument(op.Command.Parameters)
					bulkOps = append(bulkOps, mongo.NewInsertOneModel().SetDocument(doc))
				case domain.CommandTypeUpdate:
					filter := s.buildFilter(op.Command.Parameters[:len(op.Command.Parameters)/2])
					update := s.buildUpdate(op.Command.Parameters[len(op.Command.Parameters)/2:])
					bulkOps = append(bulkOps, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update))
				case domain.CommandTypeDelete:
					filter := s.buildFilter(op.Command.Parameters)
					bulkOps = append(bulkOps, mongo.NewDeleteOneModel().SetFilter(filter))
				}
			}
		}

		// Execute bulk write
		if len(bulkOps) > 0 {
			_, err := collection.BulkWrite(ctx, bulkOps)
			if err != nil {
				// Mark all operations for this collection as failed
				for i, op := range operations {
					var opCollectionName string
					if op.Type == domain.OperationTypeQuery {
						opCollectionName = s.extractCollectionName(op.Query.SQL)
					} else if op.Type == domain.OperationTypeCommand {
						opCollectionName = s.extractCollectionName(op.Command.SQL)
					}

					if opCollectionName == collectionName {
						results[i] = domain.OperationResult{
							Index: i,
							Error: domain.NewQueryError("BULK_WRITE_FAILED", err.Error()).WithCause(err),
						}
					}
				}
			} else {
				// Mark all operations for this collection as successful
				for i, op := range operations {
					var opCollectionName string
					if op.Type == domain.OperationTypeQuery {
						opCollectionName = s.extractCollectionName(op.Query.SQL)
					} else if op.Type == domain.OperationTypeCommand {
						opCollectionName = s.extractCollectionName(op.Command.SQL)
					}

					if opCollectionName == collectionName {
						results[i] = domain.OperationResult{
							Index:  i,
							Result: domain.ExecuteResult{RowsAffected: 1},
						}
					}
				}
			}
		}
	}

	return results, nil
}

// CreateTable creates a collection (MongoDB equivalent of table)
func (s *MongoDBStorage) CreateTable(ctx context.Context, schema domain.TableSchema) error {
	if s.database == nil {
		return domain.NewSchemaError("DATABASE_NOT_CONNECTED", "database not connected")
	}

	// Create collection
	err := s.database.CreateCollection(ctx, schema.Name)
	if err != nil {
		return domain.NewSchemaError("CREATE_COLLECTION_FAILED", err.Error()).WithCause(err)
	}

	// Create indexes if specified
	if len(schema.Indexes) > 0 {
		collection := s.database.Collection(schema.Name)
		for _, indexDef := range schema.Indexes {
			keys := bson.D{}

			// Build index keys
			for _, column := range indexDef.Columns {
				keys = append(keys, bson.E{Key: column, Value: 1})
			}

			indexModel := mongo.IndexModel{
				Keys: keys,
			}

			// Set index options
			indexOptions := options.Index()
			if indexDef.Unique {
				indexOptions.SetUnique(true)
			}
			indexModel.Options = indexOptions

			_, err := collection.Indexes().CreateOne(ctx, indexModel)
			if err != nil {
				return domain.NewSchemaError("CREATE_INDEX_FAILED", err.Error()).WithCause(err)
			}
		}
	}

	return nil
}

// DropTable drops a collection (MongoDB equivalent of table)
func (s *MongoDBStorage) DropTable(ctx context.Context, tableName string) error {
	if s.database == nil {
		return domain.NewSchemaError("DATABASE_NOT_CONNECTED", "database not connected")
	}

	err := s.database.Collection(tableName).Drop(ctx)
	if err != nil {
		return domain.NewSchemaError("DROP_COLLECTION_FAILED", err.Error()).WithCause(err)
	}

	return nil
}

// AlterTable alters a collection (limited support in MongoDB)
func (s *MongoDBStorage) AlterTable(ctx context.Context, tableName string, changes []domain.SchemaChange) error {
	if s.database == nil {
		return domain.NewSchemaError("DATABASE_NOT_CONNECTED", "database not connected")
	}

	collection := s.database.Collection(tableName)

	for _, change := range changes {
		switch change.Type {
		case domain.SchemaChangeTypeAddIndex:
			if change.Index.Name != "" {
				keys := bson.D{}

				// Build index keys
				for _, column := range change.Index.Columns {
					keys = append(keys, bson.E{Key: column, Value: 1})
				}

				indexModel := mongo.IndexModel{
					Keys: keys,
				}

				// Set index options
				indexOptions := options.Index()
				if change.Index.Unique {
					indexOptions.SetUnique(true)
				}
				indexModel.Options = indexOptions

				_, err := collection.Indexes().CreateOne(ctx, indexModel)
				if err != nil {
					return domain.NewSchemaError("ADD_INDEX_FAILED", err.Error()).WithCause(err)
				}
			}
		case domain.SchemaChangeTypeDropIndex:
			if change.Index.Name != "" {
				_, err := collection.Indexes().DropOne(ctx, change.Index.Name)
				if err != nil {
					return domain.NewSchemaError("DROP_INDEX_FAILED", err.Error()).WithCause(err)
				}
			}
		default:
			// MongoDB doesn't support traditional column operations
			return domain.NewSchemaError("UNSUPPORTED_OPERATION", "MongoDB does not support this schema change type")
		}
	}

	return nil
}

// ListTables lists all collections in the database
func (s *MongoDBStorage) ListTables(ctx context.Context) ([]string, error) {
	if s.database == nil {
		return nil, domain.NewSchemaError("DATABASE_NOT_CONNECTED", "database not connected")
	}

	names, err := s.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, domain.NewSchemaError("LIST_COLLECTIONS_FAILED", err.Error()).WithCause(err)
	}

	return names, nil
}

// DescribeTable describes a collection structure (limited in MongoDB)
func (s *MongoDBStorage) DescribeTable(ctx context.Context, tableName string) (domain.TableSchema, error) {
	if s.database == nil {
		return domain.TableSchema{}, domain.NewSchemaError("DATABASE_NOT_CONNECTED", "database not connected")
	}

	collection := s.database.Collection(tableName)

	// Get collection info
	schema := domain.TableSchema{
		Name:    tableName,
		Columns: []domain.ColumnDefinition{}, // MongoDB is schemaless
		Indexes: []domain.IndexDefinition{},
	}

	// Get indexes
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return schema, domain.NewSchemaError("LIST_INDEXES_FAILED", err.Error()).WithCause(err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var indexDoc bson.M
		if err := cursor.Decode(&indexDoc); err != nil {
			continue
		}

		if name, ok := indexDoc["name"].(string); ok && name != "_id_" {
			indexDef := domain.IndexDefinition{
				Name:    name,
				Columns: []string{},
				Unique:  false,
			}

			// Extract key information
			if keys, ok := indexDoc["key"].(bson.M); ok {
				for key := range keys {
					indexDef.Columns = append(indexDef.Columns, key)
				}
			}

			// Check if unique
			if unique, ok := indexDoc["unique"].(bool); ok {
				indexDef.Unique = unique
			}

			schema.Indexes = append(schema.Indexes, indexDef)
		}
	}

	return schema, nil
}

// Info returns storage information
func (s *MongoDBStorage) Info() domain.StorageInfo {
	return domain.StorageInfo{
		Name:         s.adapter.Name(),
		Version:      s.adapter.Version(),
		DatabaseType: s.adapter.DatabaseType(),
		Features: []string{
			"transactions",
			"aggregation",
			"batch",
			"schema",
			"json",
			"arrays",
			"full-text-search",
			"geospatial",
			"gridfs",
		},
		Limits: domain.StorageLimits{
			MaxConnections:    s.config.MaxOpenConns,
			MaxQuerySize:      16 * 1024 * 1024, // 16MB BSON document limit
			MaxTransactionAge: 1 * time.Minute,  // MongoDB transaction timeout
			MaxBatchSize:      100000,           // MongoDB bulk write limit
		},
	}
}

// Helper methods

// extractCollectionName extracts collection name from query SQL
// This is a simplified implementation - in practice, you'd have a proper parser
func (s *MongoDBStorage) extractCollectionName(sql string) string {
	// Simple extraction - look for "FROM collection_name" pattern
	// In a real implementation, you'd use a proper SQL parser or MongoDB-specific query format
	if sql == "" {
		return ""
	}

	// For now, assume the collection name is passed as a parameter or in a specific format
	// This would be replaced with proper query parsing
	return "default_collection"
}

// buildFilter builds a MongoDB filter from parameters
func (s *MongoDBStorage) buildFilter(params []interface{}) bson.M {
	filter := bson.M{}

	// Simple parameter mapping - in practice, you'd have sophisticated query building
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
func (s *MongoDBStorage) buildDocument(params []interface{}) bson.M {
	return s.buildFilter(params) // Same logic for now
}

// buildUpdate builds a MongoDB update document from parameters
func (s *MongoDBStorage) buildUpdate(params []interface{}) bson.M {
	update := bson.M{"$set": s.buildFilter(params)}
	return update
}

// executeInsert executes an insert command
func (s *MongoDBStorage) executeInsert(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := s.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := s.database.Collection(collectionName)
	document := s.buildDocument(command.Parameters)

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

// executeUpdate executes an update command
func (s *MongoDBStorage) executeUpdate(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := s.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := s.database.Collection(collectionName)

	// Split parameters into filter and update parts
	mid := len(command.Parameters) / 2
	filter := s.buildFilter(command.Parameters[:mid])
	update := s.buildUpdate(command.Parameters[mid:])

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return domain.ExecuteResult{}, domain.NewQueryError("UPDATE_FAILED", err.Error()).WithCause(err)
	}

	return domain.ExecuteResult{
		RowsAffected: result.ModifiedCount,
	}, nil
}

// executeDelete executes a delete command
func (s *MongoDBStorage) executeDelete(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	collectionName := s.extractCollectionName(command.SQL)
	if collectionName == "" {
		return domain.ExecuteResult{}, domain.NewQueryError("INVALID_COMMAND", "collection name not found")
	}

	collection := s.database.Collection(collectionName)
	filter := s.buildFilter(command.Parameters)

	result, err := collection.DeleteMany(ctx, filter)
	if err != nil {
		return domain.ExecuteResult{}, domain.NewQueryError("DELETE_FAILED", err.Error()).WithCause(err)
	}

	return domain.ExecuteResult{
		RowsAffected: result.DeletedCount,
	}, nil
}
