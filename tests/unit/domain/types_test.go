package domain

import (
	"testing"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		name     string
		dbType   domain.DatabaseType
		expected string
	}{
		{"PostgreSQL", domain.DatabaseTypePostgreSQL, "postgresql"},
		{"MySQL", domain.DatabaseTypeMySQL, "mysql"},
		{"SQLite", domain.DatabaseTypeSQLite, "sqlite"},
		{"Redis", domain.DatabaseTypeRedis, "redis"},
		{"MongoDB", domain.DatabaseTypeMongoDB, "mongodb"},
		{"CockroachDB", domain.DatabaseTypeCockroachDB, "cockroachdb"},
		{"DynamoDB", domain.DatabaseTypeDynamoDB, "dynamodb"},
		{"Cassandra", domain.DatabaseTypeCassandra, "cassandra"},
		{"Unknown", domain.DatabaseTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dbType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDataType_String(t *testing.T) {
	tests := []struct {
		name     string
		dataType domain.DataType
		expected string
	}{
		{"String", domain.DataTypeString, "string"},
		{"Integer", domain.DataTypeInteger, "integer"},
		{"Float", domain.DataTypeFloat, "float"},
		{"Boolean", domain.DataTypeBoolean, "boolean"},
		{"DateTime", domain.DataTypeDateTime, "datetime"},
		{"Date", domain.DataTypeDate, "date"},
		{"Time", domain.DataTypeTime, "time"},
		{"Binary", domain.DataTypeBinary, "binary"},
		{"JSON", domain.DataTypeJSON, "json"},
		{"UUID", domain.DataTypeUUID, "uuid"},
		{"Array", domain.DataTypeArray, "array"},
		{"Map", domain.DataTypeMap, "map"},
		{"Decimal", domain.DataTypeDecimal, "decimal"},
		{"Text", domain.DataTypeText, "text"},
		{"Unknown", domain.DataTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dataType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuery_Creation(t *testing.T) {
	query := domain.Query{
		SQL:        "SELECT * FROM users WHERE id = ?",
		Parameters: []interface{}{1},
		Type:       domain.QueryTypeSelect,
		Options: domain.QueryOptions{
			Timeout:  30 * time.Second,
			CacheKey: "user:1",
			CacheTTL: 5 * time.Minute,
			ReadOnly: true,
		},
	}

	assert.Equal(t, "SELECT * FROM users WHERE id = ?", query.SQL)
	assert.Equal(t, []interface{}{1}, query.Parameters)
	assert.Equal(t, domain.QueryTypeSelect, query.Type)
	assert.Equal(t, 30*time.Second, query.Options.Timeout)
	assert.Equal(t, "user:1", query.Options.CacheKey)
	assert.Equal(t, 5*time.Minute, query.Options.CacheTTL)
	assert.True(t, query.Options.ReadOnly)
}

func TestCommand_Creation(t *testing.T) {
	command := domain.Command{
		SQL:        "INSERT INTO users (name, email) VALUES (?, ?)",
		Parameters: []interface{}{"John Doe", "john@example.com"},
		Type:       domain.CommandTypeInsert,
		Options: domain.CommandOptions{
			Timeout:     10 * time.Second,
			ReturnID:    true,
			ReturnCount: true,
			OnConflict:  domain.ConflictResolutionIgnore,
		},
	}

	assert.Equal(t, "INSERT INTO users (name, email) VALUES (?, ?)", command.SQL)
	assert.Equal(t, []interface{}{"John Doe", "john@example.com"}, command.Parameters)
	assert.Equal(t, domain.CommandTypeInsert, command.Type)
	assert.Equal(t, 10*time.Second, command.Options.Timeout)
	assert.True(t, command.Options.ReturnID)
	assert.True(t, command.Options.ReturnCount)
	assert.Equal(t, domain.ConflictResolutionIgnore, command.Options.OnConflict)
}

func TestCondition_Creation(t *testing.T) {
	tests := []struct {
		name      string
		condition domain.Condition
		field     string
		operator  domain.Operator
		value     interface{}
		values    []interface{}
	}{
		{
			name: "Equal condition",
			condition: domain.Condition{
				Field:    "id",
				Operator: domain.OperatorEqual,
				Value:    1,
			},
			field:    "id",
			operator: domain.OperatorEqual,
			value:    1,
		},
		{
			name: "In condition",
			condition: domain.Condition{
				Field:    "status",
				Operator: domain.OperatorIn,
				Values:   []interface{}{"active", "pending"},
			},
			field:    "status",
			operator: domain.OperatorIn,
			values:   []interface{}{"active", "pending"},
		},
		{
			name: "Between condition",
			condition: domain.Condition{
				Field:    "age",
				Operator: domain.OperatorBetween,
				Values:   []interface{}{18, 65},
			},
			field:    "age",
			operator: domain.OperatorBetween,
			values:   []interface{}{18, 65},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.field, tt.condition.Field)
			assert.Equal(t, tt.operator, tt.condition.Operator)
			if tt.value != nil {
				assert.Equal(t, tt.value, tt.condition.Value)
			}
			if tt.values != nil {
				assert.Equal(t, tt.values, tt.condition.Values)
			}
		})
	}
}

func TestTxOptions_Creation(t *testing.T) {
	opts := domain.TxOptions{
		Isolation: domain.IsolationLevelSerializable,
		ReadOnly:  true,
		Timeout:   30 * time.Second,
	}

	assert.Equal(t, domain.IsolationLevelSerializable, opts.Isolation)
	assert.True(t, opts.ReadOnly)
	assert.Equal(t, 30*time.Second, opts.Timeout)
}

func TestConfig_Creation(t *testing.T) {
	config := domain.Config{
		Host:            "localhost",
		Port:            5432,
		Database:        "testdb",
		Username:        "testuser",
		Password:        "testpass",
		DSN:             "postgres://testuser:testpass@localhost:5432/testdb",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		ConnectTimeout:  10 * time.Second,
		QueryTimeout:    30 * time.Second,
		TxTimeout:       5 * time.Minute,
		SSLMode:         "require",
		Options:         map[string]interface{}{"application_name": "storage_test"},
	}

	assert.Equal(t, "localhost", config.Host)
	assert.Equal(t, 5432, config.Port)
	assert.Equal(t, "testdb", config.Database)
	assert.Equal(t, "testuser", config.Username)
	assert.Equal(t, "testpass", config.Password)
	assert.Equal(t, "postgres://testuser:testpass@localhost:5432/testdb", config.DSN)
	assert.Equal(t, 25, config.MaxOpenConns)
	assert.Equal(t, 5, config.MaxIdleConns)
	assert.Equal(t, 1*time.Hour, config.ConnMaxLifetime)
	assert.Equal(t, 30*time.Minute, config.ConnMaxIdleTime)
	assert.Equal(t, 10*time.Second, config.ConnectTimeout)
	assert.Equal(t, 30*time.Second, config.QueryTimeout)
	assert.Equal(t, 5*time.Minute, config.TxTimeout)
	assert.Equal(t, "require", config.SSLMode)
	assert.Equal(t, "storage_test", config.Options["application_name"])
}

func TestStorageInfo_Creation(t *testing.T) {
	info := domain.StorageInfo{
		Name:         "PostgreSQL",
		Version:      "1.0.0",
		DatabaseType: domain.DatabaseTypePostgreSQL,
		Features:     []string{"transactions", "joins", "schema"},
		Limits: domain.StorageLimits{
			MaxConnections:    100,
			MaxQuerySize:      1024 * 1024,
			MaxTransactionAge: 24 * time.Hour,
			MaxBatchSize:      1000,
		},
	}

	assert.Equal(t, "PostgreSQL", info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, domain.DatabaseTypePostgreSQL, info.DatabaseType)
	assert.Contains(t, info.Features, "transactions")
	assert.Contains(t, info.Features, "joins")
	assert.Contains(t, info.Features, "schema")
	assert.Equal(t, 100, info.Limits.MaxConnections)
	assert.Equal(t, int64(1024*1024), info.Limits.MaxQuerySize)
	assert.Equal(t, 24*time.Hour, info.Limits.MaxTransactionAge)
	assert.Equal(t, 1000, info.Limits.MaxBatchSize)
}

func TestHealthStatus_Creation(t *testing.T) {
	now := time.Now()
	status := domain.HealthStatus{
		Status:    domain.HealthStatusHealthy,
		Message:   "All systems operational",
		Timestamp: now,
		Details: map[string]interface{}{
			"connections": 10,
			"uptime":      "24h",
		},
	}

	assert.Equal(t, domain.HealthStatusHealthy, status.Status)
	assert.Equal(t, "All systems operational", status.Message)
	assert.Equal(t, now, status.Timestamp)
	assert.Equal(t, 10, status.Details["connections"])
	assert.Equal(t, "24h", status.Details["uptime"])
}

func TestTableSchema_Creation(t *testing.T) {
	schema := domain.TableSchema{
		Name: "users",
		Columns: []domain.ColumnDefinition{
			{
				Name:          "id",
				DataType:      domain.DataTypeInteger,
				Nullable:      false,
				PrimaryKey:    true,
				AutoIncrement: true,
			},
			{
				Name:     "name",
				DataType: domain.DataTypeString,
				Length:   100,
				Nullable: false,
			},
			{
				Name:     "email",
				DataType: domain.DataTypeString,
				Length:   255,
				Nullable: false,
			},
		},
		Indexes: []domain.IndexDefinition{
			{
				Name:    "idx_users_email",
				Columns: []string{"email"},
				Unique:  true,
				Type:    domain.IndexTypeBTree,
			},
		},
	}

	assert.Equal(t, "users", schema.Name)
	assert.Len(t, schema.Columns, 3)
	assert.Equal(t, "id", schema.Columns[0].Name)
	assert.Equal(t, domain.DataTypeInteger, schema.Columns[0].DataType)
	assert.True(t, schema.Columns[0].PrimaryKey)
	assert.True(t, schema.Columns[0].AutoIncrement)
	assert.False(t, schema.Columns[0].Nullable)

	assert.Equal(t, "name", schema.Columns[1].Name)
	assert.Equal(t, domain.DataTypeString, schema.Columns[1].DataType)
	assert.Equal(t, int64(100), schema.Columns[1].Length)
	assert.False(t, schema.Columns[1].Nullable)

	assert.Len(t, schema.Indexes, 1)
	assert.Equal(t, "idx_users_email", schema.Indexes[0].Name)
	assert.Equal(t, []string{"email"}, schema.Indexes[0].Columns)
	assert.True(t, schema.Indexes[0].Unique)
	assert.Equal(t, domain.IndexTypeBTree, schema.Indexes[0].Type)
}
