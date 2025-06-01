package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		name     string
		dbType   DatabaseType
		expected string
	}{
		{"PostgreSQL", DatabaseTypePostgreSQL, "postgresql"},
		{"MySQL", DatabaseTypeMySQL, "mysql"},
		{"SQLite", DatabaseTypeSQLite, "sqlite"},
		{"Redis", DatabaseTypeRedis, "redis"},
		{"MongoDB", DatabaseTypeMongoDB, "mongodb"},
		{"CockroachDB", DatabaseTypeCockroachDB, "cockroachdb"},
		{"DynamoDB", DatabaseTypeDynamoDB, "dynamodb"},
		{"Cassandra", DatabaseTypeCassandra, "cassandra"},
		{"Unknown", DatabaseTypeUnknown, "unknown"},
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
		dataType DataType
		expected string
	}{
		{"String", DataTypeString, "string"},
		{"Integer", DataTypeInteger, "integer"},
		{"Float", DataTypeFloat, "float"},
		{"Boolean", DataTypeBoolean, "boolean"},
		{"DateTime", DataTypeDateTime, "datetime"},
		{"Date", DataTypeDate, "date"},
		{"Time", DataTypeTime, "time"},
		{"Binary", DataTypeBinary, "binary"},
		{"JSON", DataTypeJSON, "json"},
		{"UUID", DataTypeUUID, "uuid"},
		{"Array", DataTypeArray, "array"},
		{"Map", DataTypeMap, "map"},
		{"Decimal", DataTypeDecimal, "decimal"},
		{"Text", DataTypeText, "text"},
		{"Unknown", DataTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dataType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuery_Creation(t *testing.T) {
	query := Query{
		SQL:        "SELECT * FROM users WHERE id = ?",
		Parameters: []interface{}{1},
		Type:       QueryTypeSelect,
		Options: QueryOptions{
			Timeout:  30 * time.Second,
			CacheKey: "user:1",
			CacheTTL: 5 * time.Minute,
			ReadOnly: true,
		},
	}

	assert.Equal(t, "SELECT * FROM users WHERE id = ?", query.SQL)
	assert.Equal(t, []interface{}{1}, query.Parameters)
	assert.Equal(t, QueryTypeSelect, query.Type)
	assert.Equal(t, 30*time.Second, query.Options.Timeout)
	assert.Equal(t, "user:1", query.Options.CacheKey)
	assert.Equal(t, 5*time.Minute, query.Options.CacheTTL)
	assert.True(t, query.Options.ReadOnly)
}

func TestCommand_Creation(t *testing.T) {
	command := Command{
		SQL:        "INSERT INTO users (name, email) VALUES (?, ?)",
		Parameters: []interface{}{"John Doe", "john@example.com"},
		Type:       CommandTypeInsert,
		Options: CommandOptions{
			Timeout:     10 * time.Second,
			ReturnID:    true,
			ReturnCount: true,
			OnConflict:  ConflictResolutionIgnore,
		},
	}

	assert.Equal(t, "INSERT INTO users (name, email) VALUES (?, ?)", command.SQL)
	assert.Equal(t, []interface{}{"John Doe", "john@example.com"}, command.Parameters)
	assert.Equal(t, CommandTypeInsert, command.Type)
	assert.Equal(t, 10*time.Second, command.Options.Timeout)
	assert.True(t, command.Options.ReturnID)
	assert.True(t, command.Options.ReturnCount)
	assert.Equal(t, ConflictResolutionIgnore, command.Options.OnConflict)
}

func TestCondition_Creation(t *testing.T) {
	tests := []struct {
		name      string
		condition Condition
		field     string
		operator  Operator
		value     interface{}
		values    []interface{}
	}{
		{
			name: "Equal condition",
			condition: Condition{
				Field:    "id",
				Operator: OperatorEqual,
				Value:    1,
			},
			field:    "id",
			operator: OperatorEqual,
			value:    1,
		},
		{
			name: "In condition",
			condition: Condition{
				Field:    "status",
				Operator: OperatorIn,
				Values:   []interface{}{"active", "pending"},
			},
			field:    "status",
			operator: OperatorIn,
			values:   []interface{}{"active", "pending"},
		},
		{
			name: "Between condition",
			condition: Condition{
				Field:    "age",
				Operator: OperatorBetween,
				Values:   []interface{}{18, 65},
			},
			field:    "age",
			operator: OperatorBetween,
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
	opts := TxOptions{
		Isolation: IsolationLevelSerializable,
		ReadOnly:  true,
		Timeout:   30 * time.Second,
	}

	assert.Equal(t, IsolationLevelSerializable, opts.Isolation)
	assert.True(t, opts.ReadOnly)
	assert.Equal(t, 30*time.Second, opts.Timeout)
}

func TestConfig_Creation(t *testing.T) {
	config := Config{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "testuser",
		Password: "testpass",
		DSN:      "postgres://testuser:testpass@localhost:5432/testdb",
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
	info := StorageInfo{
		Name:         "PostgreSQL",
		Version:      "1.0.0",
		DatabaseType: DatabaseTypePostgreSQL,
		Features:     []string{"transactions", "joins", "schema"},
		Limits: StorageLimits{
			MaxConnections:    100,
			MaxQuerySize:      1024 * 1024,
			MaxTransactionAge: 24 * time.Hour,
			MaxBatchSize:      1000,
		},
	}

	assert.Equal(t, "PostgreSQL", info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, DatabaseTypePostgreSQL, info.DatabaseType)
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
	status := HealthStatus{
		Status:    HealthStatusHealthy,
		Message:   "All systems operational",
		Timestamp: now,
		Details: map[string]interface{}{
			"connections": 10,
			"uptime":      "24h",
		},
	}

	assert.Equal(t, HealthStatusHealthy, status.Status)
	assert.Equal(t, "All systems operational", status.Message)
	assert.Equal(t, now, status.Timestamp)
	assert.Equal(t, 10, status.Details["connections"])
	assert.Equal(t, "24h", status.Details["uptime"])
}

func TestTableSchema_Creation(t *testing.T) {
	schema := TableSchema{
		Name: "users",
		Columns: []ColumnDefinition{
			{
				Name:         "id",
				DataType:     DataTypeInteger,
				Nullable:     false,
				PrimaryKey:   true,
				AutoIncrement: true,
			},
			{
				Name:         "name",
				DataType:     DataTypeString,
				Length:       100,
				Nullable:     false,
			},
			{
				Name:         "email",
				DataType:     DataTypeString,
				Length:       255,
				Nullable:     false,
			},
		},
		Indexes: []IndexDefinition{
			{
				Name:    "idx_users_email",
				Columns: []string{"email"},
				Unique:  true,
				Type:    IndexTypeBTree,
			},
		},
	}

	assert.Equal(t, "users", schema.Name)
	assert.Len(t, schema.Columns, 3)
	assert.Equal(t, "id", schema.Columns[0].Name)
	assert.Equal(t, DataTypeInteger, schema.Columns[0].DataType)
	assert.True(t, schema.Columns[0].PrimaryKey)
	assert.True(t, schema.Columns[0].AutoIncrement)
	assert.False(t, schema.Columns[0].Nullable)
	
	assert.Equal(t, "name", schema.Columns[1].Name)
	assert.Equal(t, DataTypeString, schema.Columns[1].DataType)
	assert.Equal(t, int64(100), schema.Columns[1].Length)
	assert.False(t, schema.Columns[1].Nullable)
	
	assert.Len(t, schema.Indexes, 1)
	assert.Equal(t, "idx_users_email", schema.Indexes[0].Name)
	assert.Equal(t, []string{"email"}, schema.Indexes[0].Columns)
	assert.True(t, schema.Indexes[0].Unique)
	assert.Equal(t, IndexTypeBTree, schema.Indexes[0].Type)
}
