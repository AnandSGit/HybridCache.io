package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HybridCache.io/storage/pkg/storage"
)

func TestAdapter_Name(t *testing.T) {
	adapter := NewAdapter()
	assert.Equal(t, "postgresql", adapter.Name())
}

func TestAdapter_Version(t *testing.T) {
	adapter := NewAdapter()
	assert.Equal(t, "1.0.0", adapter.Version())
}

func TestAdapter_DatabaseType(t *testing.T) {
	adapter := NewAdapter()
	assert.Equal(t, storage.DatabaseTypePostgreSQL, adapter.DatabaseType())
}

func TestAdapter_ParseDSN(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		dsn      string
		expected storage.Config
		wantErr  bool
	}{
		{
			name: "postgres URL",
			dsn:  "postgres://user:pass@localhost:5432/dbname?sslmode=require",
			expected: storage.Config{
				Host:     "localhost",
				Port:     5432,
				Database: "dbname",
				Username: "user",
				Password: "pass",
				SSLMode:  "require",
				Options:  map[string]interface{}{},
			},
			wantErr: false,
		},
		{
			name: "postgresql URL",
			dsn:  "postgresql://user:pass@localhost:5432/dbname",
			expected: storage.Config{
				Host:     "localhost",
				Port:     5432,
				Database: "dbname",
				Username: "user",
				Password: "pass",
				Options:  map[string]interface{}{},
			},
			wantErr: false,
		},
		{
			name: "key=value format",
			dsn:  "host=localhost port=5432 dbname=test user=postgres password=secret",
			expected: storage.Config{
				DSN:     "host=localhost port=5432 dbname=test user=postgres password=secret",
				Options: map[string]interface{}{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := adapter.ParseDSN(tt.dsn)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.Host, config.Host)
				assert.Equal(t, tt.expected.Port, config.Port)
				assert.Equal(t, tt.expected.Database, config.Database)
				assert.Equal(t, tt.expected.Username, config.Username)
				assert.Equal(t, tt.expected.Password, config.Password)
				assert.Equal(t, tt.expected.SSLMode, config.SSLMode)
				assert.NotNil(t, config.Options)
			}
		})
	}
}

func TestAdapter_ValidateConfig(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name    string
		config  storage.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with DSN",
			config: storage.Config{
				DSN:          "postgres://user:pass@localhost:5432/dbname",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: false,
		},
		{
			name: "valid config without DSN",
			config: storage.Config{
				Host:         "localhost",
				Database:     "testdb",
				Username:     "testuser",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: storage.Config{
				Database:     "testdb",
				Username:     "testuser",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name: "missing database",
			config: storage.Config{
				Host:         "localhost",
				Username:     "testuser",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "database is required",
		},
		{
			name: "missing username",
			config: storage.Config{
				Host:         "localhost",
				Database:     "testdb",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "username is required",
		},
		{
			name: "invalid max open connections",
			config: storage.Config{
				DSN:          "postgres://user:pass@localhost:5432/dbname",
				MaxOpenConns: 0,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "max open connections must be positive",
		},
		{
			name: "negative max idle connections",
			config: storage.Config{
				DSN:          "postgres://user:pass@localhost:5432/dbname",
				MaxOpenConns: 10,
				MaxIdleConns: -1,
			},
			wantErr: true,
			errMsg:  "max idle connections cannot be negative",
		},
		{
			name: "max idle > max open",
			config: storage.Config{
				DSN:          "postgres://user:pass@localhost:5432/dbname",
				MaxOpenConns: 5,
				MaxIdleConns: 10,
			},
			wantErr: true,
			errMsg:  "max idle connections cannot exceed max open connections",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.ValidateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAdapter_TranslateQuery(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		query    storage.Query
		expected string
		params   []interface{}
	}{
		{
			name: "simple query with parameters",
			query: storage.Query{
				SQL:        "SELECT * FROM users WHERE id = ? AND name = ?",
				Parameters: []interface{}{1, "John"},
			},
			expected: "SELECT * FROM users WHERE id = $1 AND name = $2",
			params:   []interface{}{1, "John"},
		},
		{
			name: "query without parameters",
			query: storage.Query{
				SQL:        "SELECT * FROM users",
				Parameters: []interface{}{},
			},
			expected: "SELECT * FROM users",
			params:   []interface{}{},
		},
		{
			name: "query with multiple placeholders",
			query: storage.Query{
				SQL:        "INSERT INTO users (name, email, age) VALUES (?, ?, ?)",
				Parameters: []interface{}{"John", "john@example.com", 30},
			},
			expected: "INSERT INTO users (name, email, age) VALUES ($1, $2, $3)",
			params:   []interface{}{"John", "john@example.com", 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := adapter.TranslateQuery(tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, sql)
			assert.Equal(t, tt.params, params)
		})
	}
}

func TestAdapter_MapGoType(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		goType   interface{}
		expected storage.DataType
		wantErr  bool
	}{
		{"string", "hello", storage.DataTypeString, false},
		{"int", 42, storage.DataTypeInteger, false},
		{"int32", int32(42), storage.DataTypeInteger, false},
		{"int64", int64(42), storage.DataTypeInteger, false},
		{"float32", float32(3.14), storage.DataTypeFloat, false},
		{"float64", 3.14, storage.DataTypeFloat, false},
		{"bool", true, storage.DataTypeBoolean, false},
		{"[]byte", []byte("data"), storage.DataTypeBinary, false},
		{"unsupported", make(chan int), storage.DataTypeUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataType, err := adapter.MapGoType(tt.goType)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, dataType)
			}
		})
	}
}

func TestAdapter_MapDatabaseType(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		dbType   string
		expected storage.DataType
		wantErr  bool
	}{
		{"text", "text", storage.DataTypeString, false},
		{"varchar", "varchar", storage.DataTypeString, false},
		{"integer", "integer", storage.DataTypeInteger, false},
		{"bigint", "bigint", storage.DataTypeInteger, false},
		{"boolean", "boolean", storage.DataTypeBoolean, false},
		{"timestamp", "timestamp", storage.DataTypeDateTime, false},
		{"date", "date", storage.DataTypeDate, false},
		{"time", "time", storage.DataTypeTime, false},
		{"bytea", "bytea", storage.DataTypeBinary, false},
		{"json", "json", storage.DataTypeJSON, false},
		{"jsonb", "jsonb", storage.DataTypeJSON, false},
		{"uuid", "uuid", storage.DataTypeUUID, false},
		{"array", "array", storage.DataTypeArray, false},
		{"unsupported", "unknown_type", storage.DataTypeUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataType, err := adapter.MapDatabaseType(tt.dbType)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, dataType)
			}
		})
	}
}

func TestAdapter_FeatureSupport(t *testing.T) {
	adapter := NewAdapter()

	assert.True(t, adapter.SupportsTransactions())
	assert.True(t, adapter.SupportsJoins())
	assert.True(t, adapter.SupportsBatch())
	assert.True(t, adapter.SupportsSchema())
}
