package mongodb_test

import (
	"context"
	"errors"
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
			name: "URI with escaped credentials",
			dsn:  "mongodb://us%40er:p%3Ass%20w@localhost/testdb",
			expected: domain.Config{
				DSN:      "mongodb://us%40er:p%3Ass%20w@localhost/testdb",
				Host:     "localhost",
				Port:     27017,
				Database: "testdb",
				Username: "us@er",
				Password: "p:ss w",
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
			name: "URI without database",
			dsn:  "mongodb://127.0.0.1:57017/?replicaSet=rs0",
			expected: domain.Config{
				DSN:     "mongodb://127.0.0.1:57017/?replicaSet=rs0",
				Host:    "127.0.0.1",
				Port:    57017,
				Options: map[string]interface{}{"replicaSet": "rs0"},
			},
		},
		{
			name: "SRV URI has no port",
			dsn:  "mongodb+srv://cluster.example.com/testdb",
			expected: domain.Config{
				DSN:      "mongodb+srv://cluster.example.com/testdb",
				Host:     "cluster.example.com",
				Database: "testdb",
			},
		},
		{
			name:    "invalid URI",
			dsn:     "://invalid-uri-format",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			dsn:     "postgres://localhost:5432/testdb",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := adapter.ParseDSN(tt.dsn)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, domain.ErrInvalidDSN)
				return
			}

			require.NoError(t, err)
			// Only the DSN's own settings are filled in: pool sizes and timeouts
			// stay zero so that the driver defaults apply.
			assert.Equal(t, tt.expected, config)
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
			name:   "zero pool sizes select driver defaults",
			config: domain.Config{Host: "localhost", Database: "testdb"},
		},
		{
			name:   "idle connections without a max open limit",
			config: domain.Config{Host: "localhost", Database: "testdb", MaxIdleConns: 5},
		},
		{
			name:   "DSN without database but database configured",
			config: domain.Config{DSN: "mongodb://localhost/?replicaSet=rs0", Database: "testdb"},
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
			name:    "DSN without database",
			config:  domain.Config{DSN: "mongodb://localhost:27017/"},
			wantErr: true,
			errMsg:  "database is required",
		},
		{
			name: "negative max open connections",
			config: domain.Config{
				DSN:          "mongodb://localhost:27017/testdb",
				MaxOpenConns: -1,
			},
			wantErr: true,
			errMsg:  "max open connections cannot be negative",
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
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.ErrorIs(t, err, domain.ErrInvalidConfig)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAdapter_ConnectRejectsInvalidConfig(t *testing.T) {
	adapter := mongodb.NewAdapter()
	_, err := adapter.Connect(context.Background(), domain.Config{Host: "localhost"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidConfig)
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
		{"decimal", "decimal", domain.DataTypeDecimal, false},
		{"bool", "bool", domain.DataTypeBoolean, false},
		{"date", "date", domain.DataTypeDateTime, false},
		{"bindata", "bindata", domain.DataTypeBinary, false},
		{"binData alias is case-insensitive", "binData", domain.DataTypeBinary, false},
		{"object", "object", domain.DataTypeJSON, false},
		{"array", "array", domain.DataTypeArray, false},
		{"objectid", "objectid", domain.DataTypeString, false},
		{"objectId alias", "objectId", domain.DataTypeString, false},
		{"null", "null", domain.DataTypeUnknown, true},
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
	assert.False(t, adapter.SupportsJoins(), "queries with joins are rejected")
	assert.True(t, adapter.SupportsBatch())
	assert.True(t, adapter.SupportsSchema())
}

func intPtr(i int) *int { return &i }

func TestAdapter_TranslateQuery(t *testing.T) {
	adapter := mongodb.NewAdapter()

	tests := []struct {
		name     string
		spec     *domain.QuerySpec
		expected string
	}{
		{
			name:     "all documents",
			spec:     &domain.QuerySpec{Table: "users"},
			expected: `db.getCollection("users").find({})`,
		},
		{
			name: "projection, filter, sort and pagination",
			spec: &domain.QuerySpec{
				Table:  "users",
				Fields: []string{"name", "age"},
				Where: []domain.Condition{
					{Field: "age", Operator: domain.OperatorGreaterThanOrEqual, Value: 21},
					{Field: "name", Operator: domain.OperatorLike, Value: "J%"},
				},
				OrderBy: []domain.OrderBy{{Field: "age", Direction: domain.SortDirectionDesc}, {Field: "name"}},
				Offset:  intPtr(20),
				Limit:   intPtr(10),
			},
			expected: `db.getCollection("users").find(` +
				`{"$and":[{"age":{"$gte":21}},{"name":{"$regex":"^J.*\\z","$options":"s"}}]}, ` +
				`{"name":1,"age":1,"_id":0}).sort({"age":-1,"name":1}).skip(20).limit(10)`,
		},
		{
			name:     "count",
			spec:     &domain.QuerySpec{Table: "users", Fields: []string{"COUNT(*)"}},
			expected: `db.getCollection("users").countDocuments({})`,
		},
		{
			name: "distinct",
			spec: &domain.QuerySpec{
				Table: "users", Fields: []string{"city"}, Distinct: true,
				Where: []domain.Condition{{Field: "active", Operator: domain.OperatorEqual, Value: true}},
			},
			expected: `db.getCollection("users").distinct("city", {"active":{"$eq":true}})`,
		},
		{
			name:     "limit 0",
			spec:     &domain.QuerySpec{Table: "users", Limit: intPtr(0)},
			expected: `db.getCollection("users").find({}) /* LIMIT 0: no documents are read */`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// SQL text is ignored: the rendering comes from the spec alone.
			s, params, err := adapter.TranslateQuery(domain.Query{SQL: "SELECT ignored", Spec: tt.spec})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s)
			assert.Nil(t, params)
		})
	}
}

func TestAdapter_TranslateCommand(t *testing.T) {
	adapter := mongodb.NewAdapter()
	where := []domain.Condition{{Field: "id", Operator: domain.OperatorEqual, Value: 1}}

	tests := []struct {
		name     string
		command  domain.Command
		expected string
	}{
		{
			name: "insert keeps value order",
			command: domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{
				Table:  "users",
				Values: []domain.FieldValue{{Field: "name", Value: "Ann"}, {Field: "age", Value: 30}},
			}},
			expected: `db.getCollection("users").insertOne({"name":"Ann","age":30})`,
		},
		{
			name: "update",
			command: domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{
				Table: "users", Values: []domain.FieldValue{{Field: "age", Value: 31}}, Where: where,
			}},
			expected: `db.getCollection("users").updateMany({"id":{"$eq":1}}, {"$set":{"age":31}})`,
		},
		{
			name: "delete",
			command: domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{
				Table: "users", Where: where,
			}},
			expected: `db.getCollection("users").deleteMany({"id":{"$eq":1}})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, params, err := adapter.TranslateCommand(tt.command)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s)
			assert.Nil(t, params)
		})
	}
}

func TestAdapter_TranslateRejectsRawSQL(t *testing.T) {
	adapter := mongodb.NewAdapter()

	_, _, err := adapter.TranslateQuery(domain.Query{SQL: "SELECT * FROM users WHERE id = ?", Parameters: []interface{}{1}})
	assert.True(t, errors.Is(err, domain.ErrUnsupportedQuery), "got %v", err)

	_, _, err = adapter.TranslateCommand(domain.Command{SQL: "DELETE FROM users", Type: domain.CommandTypeDelete})
	assert.True(t, errors.Is(err, domain.ErrUnsupportedQuery), "got %v", err)
}
