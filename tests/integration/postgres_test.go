//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/HybridCache.io/storage/pkg/adapters/postgres"
	"github.com/HybridCache.io/storage/pkg/query"
	"github.com/HybridCache.io/storage/pkg/storage"
)

type PostgreSQLIntegrationSuite struct {
	suite.Suite
	store   storage.Storage
	adapter storage.Adapter
}

func (suite *PostgreSQLIntegrationSuite) SetupSuite() {
	// Get DSN from environment or use default
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable"
	}

	// Create adapter and connect
	suite.adapter = postgres.NewAdapter()
	config, err := suite.adapter.ParseDSN(dsn)
	require.NoError(suite.T(), err)

	// Configure for testing
	config.MaxOpenConns = 10
	config.MaxIdleConns = 5
	config.ConnMaxLifetime = 1 * time.Hour

	suite.store, err = suite.adapter.Connect(context.Background(), config)
	require.NoError(suite.T(), err)

	// Test connection
	err = suite.store.Ping(context.Background())
	require.NoError(suite.T(), err)
}

func (suite *PostgreSQLIntegrationSuite) TearDownSuite() {
	if suite.store != nil {
		suite.store.Close()
	}
}

func (suite *PostgreSQLIntegrationSuite) SetupTest() {
	// Clean up test data before each test
	ctx := context.Background()
	
	// Delete test users
	deleteCmd := query.Delete("users").
		Where(query.Like("email", "%@test.example%"))
	
	command, err := deleteCmd.Build()
	if err == nil {
		suite.store.Execute(ctx, command)
	}
}

func (suite *PostgreSQLIntegrationSuite) TestBasicCRUD() {
	ctx := context.Background()

	// Test INSERT
	insertCmd := query.Insert("users").
		Set("name", "Integration Test User").
		Set("email", "integration@test.example").
		Set("age", 30).
		Set("active", true)

	command, err := insertCmd.Build()
	require.NoError(suite.T(), err)

	result, err := suite.store.Execute(ctx, command)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), int64(1), result.RowsAffected)

	// Test SELECT
	selectQuery := query.NewBuilder().
		Select("id", "name", "email", "age", "active").
		From("users").
		Where(query.Equal("email", "integration@test.example"))

	queryObj, err := selectQuery.Build()
	require.NoError(suite.T(), err)

	rows, err := suite.store.Query(ctx, queryObj)
	require.NoError(suite.T(), err)
	defer rows.Close()

	assert.True(suite.T(), rows.Next())
	
	var id int
	var name, email string
	var age int
	var active bool
	
	err = rows.Scan(&id, &name, &email, &age, &active)
	require.NoError(suite.T(), err)
	
	assert.Equal(suite.T(), "Integration Test User", name)
	assert.Equal(suite.T(), "integration@test.example", email)
	assert.Equal(suite.T(), 30, age)
	assert.True(suite.T(), active)

	// Test UPDATE
	updateCmd := query.Update("users").
		Set("age", 31).
		Where(query.Equal("email", "integration@test.example"))

	updateCommand, err := updateCmd.Build()
	require.NoError(suite.T(), err)

	updateResult, err := suite.store.Execute(ctx, updateCommand)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), int64(1), updateResult.RowsAffected)

	// Verify update
	row, err := suite.store.QueryOne(ctx, queryObj)
	require.NoError(suite.T(), err)
	
	err = row.Scan(&id, &name, &email, &age, &active)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 31, age)

	// Test DELETE
	deleteCmd := query.Delete("users").
		Where(query.Equal("email", "integration@test.example"))

	deleteCommand, err := deleteCmd.Build()
	require.NoError(suite.T(), err)

	deleteResult, err := suite.store.Execute(ctx, deleteCommand)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), int64(1), deleteResult.RowsAffected)
}

func (suite *PostgreSQLIntegrationSuite) TestTransactions() {
	ctx := context.Background()

	// Begin transaction
	tx, err := suite.store.BeginTx(ctx, &storage.TxOptions{
		Isolation: storage.IsolationLevelReadCommitted,
		Timeout:   30 * time.Second,
	})
	require.NoError(suite.T(), err)

	// Insert user within transaction
	insertCmd := query.Insert("users").
		Set("name", "Transaction Test User").
		Set("email", "transaction@test.example").
		Set("age", 25).
		Set("active", true)

	command, err := insertCmd.Build()
	require.NoError(suite.T(), err)

	result, err := tx.Execute(ctx, command)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), int64(1), result.RowsAffected)

	// Query within transaction
	selectQuery := query.NewBuilder().
		Select("COUNT(*)").
		From("users").
		Where(query.Equal("email", "transaction@test.example"))

	queryObj, err := selectQuery.Build()
	require.NoError(suite.T(), err)

	row, err := tx.QueryOne(ctx, queryObj)
	require.NoError(suite.T(), err)

	var count int
	err = row.Scan(&count)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 1, count)

	// Rollback transaction
	err = tx.Rollback()
	require.NoError(suite.T(), err)

	// Verify rollback - user should not exist
	row, err = suite.store.QueryOne(ctx, queryObj)
	require.NoError(suite.T(), err)

	err = row.Scan(&count)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 0, count)
}

func (suite *PostgreSQLIntegrationSuite) TestBatchOperations() {
	ctx := context.Background()

	// Create batch operations
	operations := []storage.Operation{
		{
			Type: storage.OperationTypeCommand,
			Command: storage.Command{
				SQL:        "INSERT INTO users (name, email, age, active) VALUES ($1, $2, $3, $4)",
				Parameters: []interface{}{"Batch User 1", "batch1@test.example", 25, true},
				Type:       storage.CommandTypeInsert,
			},
		},
		{
			Type: storage.OperationTypeCommand,
			Command: storage.Command{
				SQL:        "INSERT INTO users (name, email, age, active) VALUES ($1, $2, $3, $4)",
				Parameters: []interface{}{"Batch User 2", "batch2@test.example", 26, true},
				Type:       storage.CommandTypeInsert,
			},
		},
		{
			Type: storage.OperationTypeQuery,
			Query: storage.Query{
				SQL:        "SELECT COUNT(*) FROM users WHERE email LIKE $1",
				Parameters: []interface{}{"%@test.example"},
				Type:       storage.QueryTypeSelect,
			},
		},
	}

	// Execute batch
	results, err := suite.store.Batch(ctx, operations)
	require.NoError(suite.T(), err)
	require.Len(suite.T(), results, 3)

	// Check results
	for i, result := range results[:2] {
		assert.NoError(suite.T(), result.Error, "Operation %d should succeed", i)
		if execResult, ok := result.Result.(storage.ExecuteResult); ok {
			assert.Equal(suite.T(), int64(1), execResult.RowsAffected)
		}
	}

	// Check query result
	assert.NoError(suite.T(), results[2].Error)
}

func (suite *PostgreSQLIntegrationSuite) TestComplexQueries() {
	ctx := context.Background()

	// Test JOIN query
	joinQuery := query.NewBuilder().
		Select("u.name", "u.email", "COUNT(o.id) as order_count").
		From("users u").
		LeftJoin("orders o", query.Equal("o.user_id", "u.id")).
		Where(query.Equal("u.active", true)).
		GroupBy("u.id", "u.name", "u.email").
		OrderBy("order_count", storage.SortDirectionDesc).
		Limit(5)

	queryObj, err := joinQuery.Build()
	require.NoError(suite.T(), err)

	result, err := suite.store.Query(ctx, queryObj)
	require.NoError(suite.T(), err)
	defer result.Close()

	// Should have results
	hasResults := false
	for result.Next() {
		hasResults = true
		var name, email string
		var orderCount int
		err := result.Scan(&name, &email, &orderCount)
		require.NoError(suite.T(), err)
		
		assert.NotEmpty(suite.T(), name)
		assert.NotEmpty(suite.T(), email)
		assert.GreaterOrEqual(suite.T(), orderCount, 0)
	}
	
	assert.True(suite.T(), hasResults, "Should have at least one result")
	require.NoError(suite.T(), result.Err())
}

func (suite *PostgreSQLIntegrationSuite) TestSchemaOperations() {
	ctx := context.Background()

	// Test ListTables
	tables, err := suite.store.ListTables(ctx)
	require.NoError(suite.T(), err)
	assert.Contains(suite.T(), tables, "users")
	assert.Contains(suite.T(), tables, "products")

	// Test DescribeTable
	schema, err := suite.store.DescribeTable(ctx, "users")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "users", schema.Name)
	assert.NotEmpty(suite.T(), schema.Columns)

	// Check for expected columns
	columnNames := make([]string, len(schema.Columns))
	for i, col := range schema.Columns {
		columnNames[i] = col.Name
	}
	
	assert.Contains(suite.T(), columnNames, "id")
	assert.Contains(suite.T(), columnNames, "name")
	assert.Contains(suite.T(), columnNames, "email")
}

func (suite *PostgreSQLIntegrationSuite) TestHealthAndInfo() {
	ctx := context.Background()

	// Test Health
	health := suite.store.Health(ctx)
	assert.Equal(suite.T(), storage.HealthStatusHealthy, health.Status)
	assert.NotEmpty(suite.T(), health.Message)

	// Test Info
	info := suite.store.Info()
	assert.Equal(suite.T(), "postgresql", info.Name)
	assert.Equal(suite.T(), storage.DatabaseTypePostgreSQL, info.DatabaseType)
	assert.NotEmpty(suite.T(), info.Features)
	assert.Contains(suite.T(), info.Features, "transactions")
	assert.Contains(suite.T(), info.Features, "joins")
}

func TestPostgreSQLIntegrationSuite(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	suite.Run(t, new(PostgreSQLIntegrationSuite))
}
