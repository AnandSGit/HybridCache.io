package mongodb

import (
	"testing"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Name(t *testing.T) {
	adapter := mongodb.NewAdapter()
	assert.Equal(t, "mongodb", adapter.Name())
}

func TestAdapter_Version(t *testing.T) {
	adapter := mongodb.NewAdapter()
	assert.Equal(t, "1.0.0", adapter.Version())
}

func TestAdapter_DatabaseType(t *testing.T) {
	adapter := mongodb.NewAdapter()
	assert.Equal(t, domain.DatabaseTypeMongoDB, adapter.DatabaseType())
}

func TestAdapter_ParseDSN(t *testing.T) {
	adapter := mongodb.NewAdapter()

	tests := []struct {
		name     string
		dsn      string
		expected domain.Config
		wantErr  bool
	}{
		{
			name: "basic URI",
			dsn:  "mongodb://localhost:27017/testdb",
			expected: domain.Config{
				DSN:      "mongodb://localhost:27017/testdb",
				Host:     "localhost",
				Port:     27017,
				Database: "testdb",
			},
		},
		{
			name: "URI with auth",
			dsn:  "mongodb://user:pass@localhost:27017/testdb",
			expected: domain.Config{
				DSN:      "mongodb://user:pass@localhost:27017/testdb",
				Host:     "localhost",
				Port:     27017,
				Database: "testdb",
				Username: "user",
				Password: "pass",
			},
		},
		{
			name: "URI with options",
			dsn:  "mongodb://localhost:27017/testdb?authSource=admin&ssl=true",
			expected: domain.Config{
				DSN:      "mongodb://localhost:27017/testdb?authSource=admin&ssl=true",
				Host:     "localhost",
				Port:     27017,
				Database: "testdb",
				Options: map[string]interface{}{
					"authSource": "admin",
					"ssl":        "true",
				},
			},
		},
		{
			name:    "invalid URI",
			dsn:     "://invalid-uri-format",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := adapter.ParseDSN(tt.dsn)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.DSN, config.DSN)
			assert.Equal(t, tt.expected.Host, config.Host)
			assert.Equal(t, tt.expected.Port, config.Port)
			assert.Equal(t, tt.expected.Database, config.Database)
			assert.Equal(t, tt.expected.Username, config.Username)
			assert.Equal(t, tt.expected.Password, config.Password)

			if tt.expected.Options != nil {
				assert.Equal(t, tt.expected.Options, config.Options)
			}
		})
	}
}

func TestAdapter_ValidateConfig(t *testing.T) {
	adapter := mongodb.NewAdapter()

	tests := []struct {
		name    string
		config  domain.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with DSN",
			config: domain.Config{
				DSN:          "mongodb://localhost:27017/testdb",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
		},
		{
			name: "valid config without DSN",
			config: domain.Config{
				Host:         "localhost",
				Database:     "testdb",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
		},
		{
			name: "missing host",
			config: domain.Config{
				Database:     "testdb",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name: "missing database",
			config: domain.Config{
				Host:         "localhost",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "database is required",
		},
		{
			name: "invalid max open connections",
			config: domain.Config{
				DSN:          "mongodb://localhost:27017/testdb",
				MaxOpenConns: 0,
				MaxIdleConns: 5,
			},
			wantErr: true,
			errMsg:  "max open connections must be positive",
		},
		{
			name: "negative max idle connections",
			config: domain.Config{
				DSN:          "mongodb://localhost:27017/testdb",
				MaxOpenConns: 10,
				MaxIdleConns: -1,
			},
			wantErr: true,
			errMsg:  "max idle connections cannot be negative",
		},
		{
			name: "max idle exceeds max open",
			config: domain.Config{
				DSN:          "mongodb://localhost:27017/testdb",
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

func TestAdapter_MapGoType(t *testing.T) {
	adapter := mongodb.NewAdapter()

	tests := []struct {
		name     string
		goType   interface{}
		expected domain.DataType
		wantErr  bool
	}{
		{"string", "test", domain.DataTypeString, false},
		{"int", 42, domain.DataTypeInteger, false},
		{"int64", int64(42), domain.DataTypeInteger, false},
		{"float64", 3.14, domain.DataTypeFloat, false},
		{"bool", true, domain.DataTypeBoolean, false},
		{"[]byte", []byte("test"), domain.DataTypeBinary, false},
		{"map", map[string]interface{}{"key": "value"}, domain.DataTypeJSON, false},
		{"slice", []interface{}{1, 2, 3}, domain.DataTypeJSON, false},
		{"unsupported", struct{}{}, domain.DataTypeUnknown, true},
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
	adapter := mongodb.NewAdapter()

	tests := []struct {
		name     string
		dbType   string
		expected domain.DataType
		wantErr  bool
	}{
		{"string", "string", domain.DataTypeString, false},
		{"int", "int", domain.DataTypeInteger, false},
		{"long", "long", domain.DataTypeInteger, false},
		{"double", "double", domain.DataTypeFloat, false},
		{"bool", "bool", domain.DataTypeBoolean, false},
		{"date", "date", domain.DataTypeDateTime, false},
		{"bindata", "bindata", domain.DataTypeBinary, false},
		{"object", "object", domain.DataTypeJSON, false},
		{"array", "array", domain.DataTypeArray, false},
		{"objectid", "objectid", domain.DataTypeString, false},
		{"unsupported", "unknown_type", domain.DataTypeUnknown, true},
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
	adapter := mongodb.NewAdapter()

	assert.True(t, adapter.SupportsTransactions())
	assert.True(t, adapter.SupportsJoins())
	assert.True(t, adapter.SupportsBatch())
	assert.True(t, adapter.SupportsSchema())
}
