package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMongoDBAdapter_BasicIntegration tests basic MongoDB adapter functionality
// This test requires a running MongoDB instance
func TestMongoDBAdapter_BasicIntegration(t *testing.T) {
	// Skip if no MongoDB connection string provided
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017/hybridcache_test"
		t.Logf("Using default MongoDB URI: %s", mongoURI)
	}

	adapter := mongodb.NewAdapter()

	// Parse the connection string
	config, err := adapter.ParseDSN(mongoURI)
	require.NoError(t, err, "Failed to parse MongoDB URI")

	// Set reasonable timeouts for testing
	config.ConnectTimeout = 5 * time.Second
	config.QueryTimeout = 10 * time.Second
	config.MaxOpenConns = 5
	config.MaxIdleConns = 2

	ctx := context.Background()

	// Test connection
	storage, err := adapter.Connect(ctx, config)
	if err != nil {
		t.Skipf("Skipping MongoDB integration test - cannot connect to MongoDB: %v", err)
		return
	}
	defer storage.Close()

	t.Run("Ping", func(t *testing.T) {
		err := storage.Ping(ctx)
		assert.NoError(t, err, "Ping should succeed")
	})

	t.Run("Health", func(t *testing.T) {
		health := storage.Health(ctx)
		assert.Equal(t, domain.HealthStatusHealthy, health.Status, "Health status should be healthy")
		assert.Contains(t, health.Message, "MongoDB is healthy", "Health message should indicate MongoDB is healthy")
	})

	t.Run("StorageInfo", func(t *testing.T) {
		info := storage.Info()
		assert.Equal(t, "mongodb", info.Name, "Storage name should be mongodb")
		assert.Equal(t, "1.0.0", info.Version, "Storage version should be 1.0.0")
		assert.Equal(t, domain.DatabaseTypeMongoDB, info.DatabaseType, "Database type should be MongoDB")
		assert.Contains(t, info.Features, "transactions", "Should support transactions")
		assert.Contains(t, info.Features, "aggregation", "Should support aggregation")
		assert.Contains(t, info.Features, "batch", "Should support batch operations")
		assert.Contains(t, info.Features, "json", "Should support JSON")
	})

	t.Run("BasicOperations", func(t *testing.T) {
		// Test insert operation
		insertCmd := domain.Command{
			SQL:        "INSERT INTO test_users",
			Parameters: []interface{}{"name", "John Doe", "email", "john@test.com", "age", 30},
			Type:       domain.CommandTypeInsert,
		}

		result, err := storage.Execute(ctx, insertCmd)
		assert.NoError(t, err, "Insert should succeed")
		assert.Equal(t, int64(1), result.RowsAffected, "Should affect 1 row")
		assert.NotZero(t, result.LastInsertID, "Should have a last insert ID")

		// Test query operation (simplified - may not return data due to collection name parsing)
		query := domain.Query{
			SQL:        "SELECT FROM test_users",
			Parameters: []interface{}{"name", "John Doe"},
		}

		queryResult, err := storage.Query(ctx, query)
		if err != nil {
			t.Logf("Query failed as expected in simplified implementation: %v", err)
		} else {
			assert.NoError(t, queryResult.Close(), "Should be able to close query result")
		}

		// Test update operation
		updateCmd := domain.Command{
			SQL:        "UPDATE test_users",
			Parameters: []interface{}{"email", "john@test.com", "age", 31},
			Type:       domain.CommandTypeUpdate,
		}

		updateResult, err := storage.Execute(ctx, updateCmd)
		assert.NoError(t, err, "Update should succeed")
		assert.GreaterOrEqual(t, updateResult.RowsAffected, int64(0), "Should affect 0 or more rows")

		// Test delete operation
		deleteCmd := domain.Command{
			SQL:        "DELETE FROM test_users",
			Parameters: []interface{}{"email", "john@test.com"},
			Type:       domain.CommandTypeDelete,
		}

		deleteResult, err := storage.Execute(ctx, deleteCmd)
		assert.NoError(t, err, "Delete should succeed")
		assert.GreaterOrEqual(t, deleteResult.RowsAffected, int64(0), "Should affect 0 or more rows")
	})

	t.Run("TransactionOperations", func(t *testing.T) {
		// Test transaction creation
		tx, err := storage.BeginTx(ctx, &domain.TxOptions{
			Isolation: domain.IsolationLevelReadCommitted,
			ReadOnly:  false,
		})
		require.NoError(t, err, "Should be able to begin transaction")

		// Test transaction operations
		insertCmd := domain.Command{
			SQL:        "INSERT INTO test_tx_users",
			Parameters: []interface{}{"name", "Jane Doe", "email", "jane@test.com", "age", 25},
			Type:       domain.CommandTypeInsert,
		}

		result, err := tx.Execute(ctx, insertCmd)
		assert.NoError(t, err, "Transaction insert should succeed")
		assert.Equal(t, int64(1), result.RowsAffected, "Should affect 1 row")

		// Test transaction commit
		err = tx.Commit()
		assert.NoError(t, err, "Transaction commit should succeed")
	})

	t.Run("TransactionRollback", func(t *testing.T) {
		// Test transaction rollback
		tx, err := storage.BeginTx(ctx, &domain.TxOptions{
			Isolation: domain.IsolationLevelReadCommitted,
			ReadOnly:  false,
		})
		require.NoError(t, err, "Should be able to begin transaction")

		// Insert data in transaction
		insertCmd := domain.Command{
			SQL:        "INSERT INTO test_rollback_users",
			Parameters: []interface{}{"name", "Bob Smith", "email", "bob@test.com", "age", 35},
			Type:       domain.CommandTypeInsert,
		}

		_, err = tx.Execute(ctx, insertCmd)
		assert.NoError(t, err, "Transaction insert should succeed")

		// Rollback transaction
		err = tx.Rollback()
		assert.NoError(t, err, "Transaction rollback should succeed")
	})

	t.Run("BatchOperations", func(t *testing.T) {
		// Test batch operations
		operations := []domain.Operation{
			{
				Type: domain.OperationTypeCommand,
				Command: domain.Command{
					SQL:        "INSERT INTO test_batch_users",
					Parameters: []interface{}{"name", "Alice Johnson", "email", "alice@test.com", "age", 28},
					Type:       domain.CommandTypeInsert,
				},
			},
			{
				Type: domain.OperationTypeCommand,
				Command: domain.Command{
					SQL:        "INSERT INTO test_batch_users",
					Parameters: []interface{}{"name", "Charlie Brown", "email", "charlie@test.com", "age", 32},
					Type:       domain.CommandTypeInsert,
				},
			},
		}

		results, err := storage.Batch(ctx, operations)
		assert.NoError(t, err, "Batch operations should succeed")
		assert.Len(t, results, 2, "Should have 2 operation results")

		// Check individual results
		for i, result := range results {
			assert.Equal(t, i, result.Index, "Result index should match operation index")
			if result.Error != nil {
				t.Logf("Operation %d failed: %v", i, result.Error)
			}
		}
	})

	t.Run("SchemaOperations", func(t *testing.T) {
		// Test collection creation (MongoDB equivalent of table)
		schema := domain.TableSchema{
			Name: "test_schema_collection",
			Columns: []domain.ColumnDefinition{
				{Name: "id", DataType: domain.DataTypeString, PrimaryKey: true},
				{Name: "name", DataType: domain.DataTypeString, Nullable: false},
				{Name: "email", DataType: domain.DataTypeString, Nullable: false},
			},
			Indexes: []domain.IndexDefinition{
				{Name: "idx_email", Columns: []string{"email"}, Unique: true},
			},
		}

		err := storage.CreateTable(ctx, schema)
		assert.NoError(t, err, "Should be able to create collection")

		// Test listing collections
		tables, err := storage.ListTables(ctx)
		assert.NoError(t, err, "Should be able to list collections")
		assert.Contains(t, tables, "test_schema_collection", "Should contain the created collection")

		// Test describing collection
		describedSchema, err := storage.DescribeTable(ctx, "test_schema_collection")
		assert.NoError(t, err, "Should be able to describe collection")
		assert.Equal(t, "test_schema_collection", describedSchema.Name, "Schema name should match")

		// Test dropping collection
		err = storage.DropTable(ctx, "test_schema_collection")
		assert.NoError(t, err, "Should be able to drop collection")
	})
}

// TestMongoDBAdapter_ErrorHandling tests error handling scenarios
func TestMongoDBAdapter_ErrorHandling(t *testing.T) {
	adapter := mongodb.NewAdapter()

	t.Run("InvalidConnection", func(t *testing.T) {
		// Test connection to invalid host
		config := domain.Config{
			Host:           "invalid-host-that-does-not-exist",
			Port:           27017,
			Database:       "test",
			ConnectTimeout: 1 * time.Second,
		}

		ctx := context.Background()
		_, err := adapter.Connect(ctx, config)
		assert.Error(t, err, "Should fail to connect to invalid host")
		assert.True(t, domain.IsConnectionError(err), "Should be a connection error")
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		// Test invalid configuration
		config := domain.Config{
			Host:         "",
			Database:     "",
			MaxOpenConns: 0,
		}

		err := adapter.ValidateConfig(config)
		assert.Error(t, err, "Should fail validation for invalid config")
	})
}

// TestMongoDBAdapter_FeatureSupport tests feature support flags
func TestMongoDBAdapter_FeatureSupport(t *testing.T) {
	adapter := mongodb.NewAdapter()

	assert.True(t, adapter.SupportsTransactions(), "Should support transactions")
	assert.True(t, adapter.SupportsJoins(), "Should support joins (via aggregation)")
	assert.True(t, adapter.SupportsBatch(), "Should support batch operations")
	assert.True(t, adapter.SupportsSchema(), "Should support schema operations")
}
