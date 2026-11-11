package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
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
	assert.Equal(t, domain.DatabaseTypePostgreSQL, adapter.DatabaseType())
}

func TestAdapter_ParseDSN(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		dsn      string
		expected domain.Config
		wantErr  bool
	}{
		{
			name: "postgres URL",
			dsn:  "postgres://user:pass@localhost:5432/dbname?sslmode=require",
			expected: domain.Config{
				Host:     "localhost",
				Port:     5432,
				Database: "dbname",
				Username: "user",
				Password: "pass",
				SSLMode:  "require",
				Options:  map[string]interface{}{},
			},
		},
		{
			name: "postgresql URL with options",
			dsn:  "postgresql://user:p%40ss@localhost:5432/dbname?connect_timeout=5&pool_max_conns=7",
			expected: domain.Config{
				Host:           "localhost",
				Port:           5432,
				Database:       "dbname",
				Username:       "user",
				Password:       "p@ss",
				ConnectTimeout: 5 * time.Second,
				Options:        map[string]interface{}{"pool_max_conns": "7"},
			},
		},
		{
			name: "key=value format",
			dsn:  "host=localhost port=5432 dbname=test user=postgres password=secret",
			expected: domain.Config{
				DSN:     "host=localhost port=5432 dbname=test user=postgres password=secret",
				Options: map[string]interface{}{},
			},
		},
		{
			name:    "invalid URL",
			dsn:     "postgres://user:pass@[::1/db",
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
			assert.Equal(t, tt.expected, config)
		})
	}
}

func TestAdapter_ValidateConfig(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name    string
		config  domain.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:   "valid config with DSN",
			config: domain.Config{DSN: "postgres://user:pass@localhost:5432/dbname", MaxOpenConns: 10, MaxIdleConns: 5},
		},
		{
			name:   "valid config without DSN",
			config: domain.Config{Host: "localhost", Database: "testdb", Username: "testuser", MaxOpenConns: 10, MaxIdleConns: 5},
		},
		{
			name:   "zero pool settings use the driver defaults",
			config: domain.Config{Host: "localhost", Database: "testdb", Username: "testuser"},
		},
		{
			name:   "idle connections without max open connections",
			config: domain.Config{DSN: "host=localhost", MaxIdleConns: 2},
		},
		{
			name:    "missing host",
			config:  domain.Config{Database: "testdb", Username: "testuser"},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name:    "missing database",
			config:  domain.Config{Host: "localhost", Username: "testuser"},
			wantErr: true,
			errMsg:  "database is required",
		},
		{
			name:    "missing username",
			config:  domain.Config{Host: "localhost", Database: "testdb"},
			wantErr: true,
			errMsg:  "username is required",
		},
		{
			name:    "negative max open connections",
			config:  domain.Config{DSN: "host=localhost", MaxOpenConns: -1},
			wantErr: true,
			errMsg:  "max open connections cannot be negative",
		},
		{
			name:    "too many max open connections",
			config:  domain.Config{DSN: "host=localhost", MaxOpenConns: 1 << 40},
			wantErr: true,
			errMsg:  "max open connections is too large",
		},
		{
			name:    "negative max idle connections",
			config:  domain.Config{DSN: "host=localhost", MaxOpenConns: 10, MaxIdleConns: -1},
			wantErr: true,
			errMsg:  "max idle connections cannot be negative",
		},
		{
			name:    "max idle > max open",
			config:  domain.Config{DSN: "host=localhost", MaxOpenConns: 5, MaxIdleConns: 10},
			wantErr: true,
			errMsg:  "max idle connections cannot exceed max open connections",
		},
		{
			name:    "negative lifetime",
			config:  domain.Config{DSN: "host=localhost", ConnMaxLifetime: -time.Second},
			wantErr: true,
			errMsg:  "connection max lifetime cannot be negative",
		},
		{
			name:    "negative query timeout",
			config:  domain.Config{DSN: "host=localhost", QueryTimeout: -time.Second},
			wantErr: true,
			errMsg:  "query timeout cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.ValidateConfig(tt.config)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
			assert.ErrorIs(t, err, domain.ErrInvalidConfig)
		})
	}
}

// ParseDSN leaves the pool settings at zero, so Connect(ParseDSN(dsn)) must validate.
func TestAdapter_ValidateConfig_AcceptsParsedDSN(t *testing.T) {
	adapter := NewAdapter()
	config, err := adapter.ParseDSN("postgres://user:pass@localhost:5432/db?sslmode=disable")
	require.NoError(t, err)
	assert.NoError(t, adapter.ValidateConfig(config))
}

func TestBuildDSN_EscapesValues(t *testing.T) {
	passwords := []string{
		"x sslmode=disable",
		`it's`,
		`back\slash`,
		`'; host=evil`,
		"",
		`trailing\`,
	}
	for _, password := range passwords {
		t.Run(password, func(t *testing.T) {
			dsn, err := buildDSN(domain.Config{
				Host:     "db.example.com",
				Port:     5433,
				Database: "my db",
				Username: "o'brien",
				Password: password,
				SSLMode:  "require",
			})
			require.NoError(t, err)

			parsed, err := pgconn.ParseConfig(dsn)
			require.NoError(t, err)
			assert.Equal(t, password, parsed.Password)
			assert.Equal(t, "db.example.com", parsed.Host)
			assert.Equal(t, uint16(5433), parsed.Port)
			assert.Equal(t, "my db", parsed.Database)
			assert.Equal(t, "o'brien", parsed.User)
			assert.NotNil(t, parsed.TLSConfig, "sslmode=require must not be overridden by the password")
		})
	}
}

func TestBuildDSN_Options(t *testing.T) {
	config := domain.Config{
		Host:     "localhost",
		Database: "db",
		Username: "u",
		Options: map[string]interface{}{
			"search_path":      "app, public",
			"application_name": "svc",
			"pool_max_conns":   7,
		},
	}

	first, err := buildDSN(config)
	require.NoError(t, err)
	assert.Equal(t,
		`host='localhost' dbname='db' user='u' sslmode='prefer' application_name='svc' pool_max_conns='7' search_path='app, public'`,
		first, "options are appended in sorted order")
	for i := 0; i < 20; i++ {
		again, err := buildDSN(config)
		require.NoError(t, err)
		require.Equal(t, first, again, "DSN must be deterministic")
	}

	_, err = buildDSN(domain.Config{Host: "h", Options: map[string]interface{}{"bad key": "v"}})
	assert.ErrorIs(t, err, domain.ErrInvalidConfig)
	_, err = buildDSN(domain.Config{Host: "h", Options: map[string]interface{}{"k": []string{"v"}}})
	assert.ErrorIs(t, err, domain.ErrInvalidConfig)
	_, err = buildDSN(domain.Config{Host: "h", Password: "a\x00b"})
	assert.ErrorIs(t, err, domain.ErrInvalidConfig)

	dsn, err := buildDSN(domain.Config{DSN: "host=raw"})
	require.NoError(t, err)
	assert.Equal(t, "host=raw", dsn, "an explicit DSN is used as is")
}

func TestBuildPoolConfig(t *testing.T) {
	base := domain.Config{Host: "localhost", Database: "db", Username: "u"}

	t.Run("zero values keep the pgxpool defaults", func(t *testing.T) {
		defaults, err := pgxpool.ParseConfig("host=localhost dbname=db user=u")
		require.NoError(t, err)

		pc, err := buildPoolConfig(base)
		require.NoError(t, err)
		assert.Equal(t, time.Hour, pc.MaxConnLifetime)
		assert.Equal(t, 30*time.Minute, pc.MaxConnIdleTime)
		assert.Equal(t, defaults.MaxConns, pc.MaxConns)
		assert.Equal(t, int32(0), pc.MinConns)
	})

	t.Run("set values override", func(t *testing.T) {
		config := base
		config.MaxOpenConns = 12
		config.MaxIdleConns = 3
		config.ConnMaxLifetime = 5 * time.Minute
		config.ConnMaxIdleTime = time.Minute
		config.ConnectTimeout = 1500 * time.Millisecond

		pc, err := buildPoolConfig(config)
		require.NoError(t, err)
		assert.Equal(t, int32(12), pc.MaxConns)
		assert.Equal(t, int32(3), pc.MinConns)
		assert.Equal(t, 5*time.Minute, pc.MaxConnLifetime)
		assert.Equal(t, time.Minute, pc.MaxConnIdleTime)
		assert.Equal(t, 1500*time.Millisecond, pc.ConnConfig.ConnectTimeout)
	})

	t.Run("DSN pool settings are kept unless the Config field is set", func(t *testing.T) {
		pc, err := buildPoolConfig(domain.Config{DSN: "host=localhost pool_max_conns=9 pool_max_conn_lifetime=2h"})
		require.NoError(t, err)
		assert.Equal(t, int32(9), pc.MaxConns)
		assert.Equal(t, 2*time.Hour, pc.MaxConnLifetime)

		pc, err = buildPoolConfig(domain.Config{DSN: "host=localhost pool_max_conns=9", MaxOpenConns: 4})
		require.NoError(t, err)
		assert.Equal(t, int32(4), pc.MaxConns)
	})

	t.Run("idle connections cannot exceed the effective pool size", func(t *testing.T) {
		_, err := buildPoolConfig(domain.Config{DSN: "host=localhost pool_max_conns=2", MaxIdleConns: 3})
		assert.ErrorIs(t, err, domain.ErrInvalidConfig)
	})

	t.Run("unparsable DSN", func(t *testing.T) {
		_, err := buildPoolConfig(domain.Config{DSN: "host='unterminated"})
		assert.ErrorIs(t, err, domain.ErrInvalidDSN)
	})
}

func TestAdapter_Connect_InvalidConfig(t *testing.T) {
	_, err := NewAdapter().Connect(t.Context(), domain.Config{Database: "db"})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidConfig)
	assert.Equal(t, 1, strings.Count(err.Error(), "host is required"), "message must not repeat the cause: %s", err)
}

func TestAdapter_Connect_Unreachable(t *testing.T) {
	// Port 1 on localhost refuses connections.
	_, err := NewAdapter().Connect(t.Context(), domain.Config{
		Host: "127.0.0.1", Port: 1, Database: "db", Username: "u", SSLMode: "disable",
		ConnectTimeout: 2 * time.Second,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrConnectionFailed)
	assert.True(t, domain.IsConnectionError(err))
}

func TestConnectionError(t *testing.T) {
	refused := errors.New("connection refused")
	err := connectionError(refused, "failed to connect")
	assert.ErrorIs(t, err, domain.ErrConnectionFailed)
	assert.ErrorIs(t, err, refused)

	err = connectionError(fmt.Errorf("dial: %w", context.DeadlineExceeded), "failed to connect")
	assert.ErrorIs(t, err, domain.ErrConnectionTimeout)
	assert.True(t, domain.IsRetryable(err))
}

func TestAdapter_TranslateQuery(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		query    domain.Query
		expected string
		params   []interface{}
	}{
		{
			name:     "simple query with parameters",
			query:    domain.Query{SQL: "SELECT * FROM users WHERE id = ? AND name = ?", Parameters: []interface{}{1, "John"}},
			expected: "SELECT * FROM users WHERE id = $1 AND name = $2",
			params:   []interface{}{1, "John"},
		},
		{
			name:     "query without parameters",
			query:    domain.Query{SQL: "SELECT * FROM users", Parameters: []interface{}{}},
			expected: "SELECT * FROM users",
			params:   []interface{}{},
		},
		{
			name:     "query with multiple placeholders",
			query:    domain.Query{SQL: "INSERT INTO users (name, email, age) VALUES (?, ?, ?)", Parameters: []interface{}{"John", "john@example.com", 30}},
			expected: "INSERT INTO users (name, email, age) VALUES ($1, $2, $3)",
			params:   []interface{}{"John", "john@example.com", 30},
		},
		{
			name:     "question mark inside a literal is not a placeholder",
			query:    domain.Query{SQL: "SELECT * FROM t WHERE a = ? AND b = 'why?'", Parameters: []interface{}{1}},
			expected: "SELECT * FROM t WHERE a = $1 AND b = 'why?'",
			params:   []interface{}{1},
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

	_, _, err := adapter.TranslateQuery(domain.Query{SQL: "  "})
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)

	sql, params, err := adapter.TranslateCommand(domain.Command{SQL: "DELETE FROM t WHERE id = ?", Parameters: []interface{}{7}})
	require.NoError(t, err)
	assert.Equal(t, "DELETE FROM t WHERE id = $1", sql)
	assert.Equal(t, []interface{}{7}, params)
}

func TestAdapter_MapGoType(t *testing.T) {
	adapter := NewAdapter()

	tests := []struct {
		name     string
		goType   interface{}
		expected domain.DataType
		wantErr  bool
	}{
		{"string", "hello", domain.DataTypeString, false},
		{"int", 42, domain.DataTypeInteger, false},
		{"int32", int32(42), domain.DataTypeInteger, false},
		{"int64", int64(42), domain.DataTypeInteger, false},
		{"uint", uint(42), domain.DataTypeInteger, false},
		{"float32", float32(3.14), domain.DataTypeFloat, false},
		{"float64", 3.14, domain.DataTypeFloat, false},
		{"bool", true, domain.DataTypeBoolean, false},
		{"time", time.Time{}, domain.DataTypeDateTime, false},
		{"[]byte", []byte("data"), domain.DataTypeBinary, false},
		{"unsupported", make(chan int), domain.DataTypeUnknown, true},
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
		dbType   string
		expected domain.DataType
	}{
		{"text", domain.DataTypeString},
		{"varchar", domain.DataTypeString},
		{"character varying(255)", domain.DataTypeString},
		{"bpchar", domain.DataTypeString},
		{"CHARACTER", domain.DataTypeString},
		{"integer", domain.DataTypeInteger},
		{"bigint", domain.DataTypeInteger},
		{"int2", domain.DataTypeInteger},
		{"serial", domain.DataTypeInteger},
		{"real", domain.DataTypeFloat},
		{"double precision", domain.DataTypeFloat},
		{"numeric", domain.DataTypeDecimal},
		{"numeric(10,2)", domain.DataTypeDecimal},
		{"decimal", domain.DataTypeDecimal},
		{"boolean", domain.DataTypeBoolean},
		{"timestamp", domain.DataTypeDateTime},
		{"timestamptz", domain.DataTypeDateTime},
		{"timestamp(3) with time zone", domain.DataTypeDateTime},
		{"timestamp without time zone", domain.DataTypeDateTime},
		{"date", domain.DataTypeDate},
		{"time", domain.DataTypeTime},
		{"time with time zone", domain.DataTypeTime},
		{"bytea", domain.DataTypeBinary},
		{"json", domain.DataTypeJSON},
		{"jsonb", domain.DataTypeJSON},
		{"uuid", domain.DataTypeUUID},
		{"array", domain.DataTypeArray},
		{"ARRAY", domain.DataTypeArray},
		{"integer[]", domain.DataTypeArray},
		{"_int4", domain.DataTypeArray},
	}

	for _, tt := range tests {
		t.Run(tt.dbType, func(t *testing.T) {
			dataType, err := adapter.MapDatabaseType(tt.dbType)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, dataType)
		})
	}

	dataType, err := adapter.MapDatabaseType("unknown_type")
	assert.Error(t, err)
	assert.Equal(t, domain.DataTypeUnknown, dataType)
}

func TestAdapter_FeatureSupport(t *testing.T) {
	adapter := NewAdapter()

	assert.True(t, adapter.SupportsTransactions())
	assert.True(t, adapter.SupportsJoins())
	assert.True(t, adapter.SupportsBatch())
	assert.True(t, adapter.SupportsSchema())
}

func TestStorage_InfoFeatures(t *testing.T) {
	s := newStorage(nil, domain.Config{}, NewAdapter().(*Adapter), 8)
	info := s.Info()
	assert.Equal(t, []string{"transactions", "joins", "batch", "schema", "json"}, info.Features)
	assert.Equal(t, 8, info.Limits.MaxConnections)
	assert.Equal(t, domain.DatabaseTypePostgreSQL, info.DatabaseType)
}
