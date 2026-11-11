// Package postgres provides PostgreSQL adapter for the storage library
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// Adapter implements the domain.Adapter interface for PostgreSQL
type Adapter struct {
	name    string
	version string
}

// NewAdapter creates a new PostgreSQL adapter
func NewAdapter() domain.Adapter {
	return &Adapter{
		name:    "postgresql",
		version: "1.0.0",
	}
}

// Name returns the adapter name
func (a *Adapter) Name() string {
	return a.name
}

// Version returns the adapter version
func (a *Adapter) Version() string {
	return a.version
}

// DatabaseType returns the database type
func (a *Adapter) DatabaseType() domain.DatabaseType {
	return domain.DatabaseTypePostgreSQL
}

// Connect creates a connection pool and verifies that the database is reachable.
func (a *Adapter) Connect(ctx context.Context, config domain.Config) (domain.Storage, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}

	poolConfig, err := buildPoolConfig(config)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, connectionError(err, "failed to create connection pool")
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, connectionError(err, "failed to connect to PostgreSQL")
	}

	return newStorage(pool, config, a, int(poolConfig.MaxConns)), nil
}

// buildPoolConfig builds the pgxpool configuration. Pool settings from the DSN
// (or pgx's defaults) are overridden only by Config fields that are set (> 0).
func buildPoolConfig(config domain.Config) (*pgxpool.Config, error) {
	dsn, err := buildDSN(config)
	if err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, domain.NewConnectionError("INVALID_DSN", "failed to parse PostgreSQL connection string").WithCause(err)
	}

	// ValidateConfig bounds both counts to int32.
	if n := config.MaxOpenConns; n > 0 && n <= math.MaxInt32 {
		poolConfig.MaxConns = int32(n)
	}
	if n := config.MaxIdleConns; n > 0 && n <= math.MaxInt32 {
		poolConfig.MinConns = int32(n)
	}
	if config.ConnMaxLifetime > 0 {
		poolConfig.MaxConnLifetime = config.ConnMaxLifetime
	}
	if config.ConnMaxIdleTime > 0 {
		poolConfig.MaxConnIdleTime = config.ConnMaxIdleTime
	}
	if config.ConnectTimeout > 0 {
		poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout
	}

	if poolConfig.MinConns > poolConfig.MaxConns {
		return nil, invalidConfig(fmt.Sprintf("max idle connections (%d) cannot exceed the pool size (%d)",
			poolConfig.MinConns, poolConfig.MaxConns))
	}
	return poolConfig, nil
}

// connectionError maps a failure to reach the database.
func connectionError(err error, msg string) error {
	if errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
		return domain.NewConnectionTimeoutError(msg).WithCause(err)
	}
	return domain.NewConnectionError("CONNECTION_FAILED", msg).WithCause(err)
}

func invalidConfig(msg string) *domain.StorageError {
	return domain.NewStorageError(domain.ErrorTypeValidation, "INVALID_CONFIG", msg)
}

// ParseDSN parses a PostgreSQL DSN into a Config. A postgres:// or
// postgresql:// URL is split into the Config fields (unknown query parameters
// go to Options); a key=value DSN is kept in Config.DSN.
func (a *Adapter) ParseDSN(dsn string) (domain.Config, error) {
	config := domain.Config{
		Options: make(map[string]interface{}),
	}

	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		config.DSN = dsn
		return config, nil
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, domain.NewConnectionError("INVALID_DSN", "invalid PostgreSQL connection URL").WithCause(err)
	}

	config.Host = u.Hostname()
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil {
			return config, domain.NewConnectionError("INVALID_DSN", "invalid port in PostgreSQL connection URL").WithCause(err)
		}
		config.Port = port
	}

	if u.User != nil {
		config.Username = u.User.Username()
		if password, ok := u.User.Password(); ok {
			config.Password = password
		}
	}

	config.Database = strings.TrimPrefix(u.Path, "/")

	for key, values := range u.Query() {
		if len(values) == 0 {
			continue
		}
		switch key {
		case "sslmode":
			config.SSLMode = values[0]
		case "connect_timeout":
			if timeout, err := strconv.Atoi(values[0]); err == nil {
				config.ConnectTimeout = time.Duration(timeout) * time.Second
			}
		default:
			config.Options[key] = values[0]
		}
	}

	return config, nil
}

// ValidateConfig validates the configuration. Zero pool settings and timeouts
// mean "use the default"; negative values are invalid.
func (a *Adapter) ValidateConfig(config domain.Config) error {
	if config.DSN == "" {
		if config.Host == "" {
			return invalidConfig("host is required")
		}
		if config.Database == "" {
			return invalidConfig("database is required")
		}
		if config.Username == "" {
			return invalidConfig("username is required")
		}
	}

	switch {
	case config.MaxOpenConns < 0:
		return invalidConfig("max open connections cannot be negative")
	case config.MaxOpenConns > math.MaxInt32:
		return invalidConfig("max open connections is too large")
	case config.MaxIdleConns < 0:
		return invalidConfig("max idle connections cannot be negative")
	case config.MaxIdleConns > math.MaxInt32:
		return invalidConfig("max idle connections is too large")
	case config.MaxOpenConns > 0 && config.MaxIdleConns > config.MaxOpenConns:
		return invalidConfig("max idle connections cannot exceed max open connections")
	}

	for _, d := range []struct {
		name  string
		value time.Duration
	}{
		{"connection max lifetime", config.ConnMaxLifetime},
		{"connection max idle time", config.ConnMaxIdleTime},
		{"connect timeout", config.ConnectTimeout},
		{"query timeout", config.QueryTimeout},
		{"transaction timeout", config.TxTimeout},
	} {
		if d.value < 0 {
			return invalidConfig(d.name + " cannot be negative")
		}
	}

	return nil
}

// TranslateQuery converts the query's '?' placeholders to PostgreSQL's $n form;
// see rebind for the rules. SQL using $n placeholders is returned unchanged.
func (a *Adapter) TranslateQuery(query domain.Query) (string, []interface{}, error) {
	sql, err := translateSQL(query.SQL)
	if err != nil {
		return "", nil, err
	}
	return sql, query.Parameters, nil
}

// TranslateCommand translates a domain.Command to PostgreSQL SQL
func (a *Adapter) TranslateCommand(command domain.Command) (string, []interface{}, error) {
	return a.TranslateQuery(domain.Query{
		SQL:        command.SQL,
		Parameters: command.Parameters,
	})
}

// MapGoType maps a Go type to a domain.DataType
func (a *Adapter) MapGoType(goType interface{}) (domain.DataType, error) {
	switch goType.(type) {
	case string:
		return domain.DataTypeString, nil
	case int, int8, int16, int32, int64:
		return domain.DataTypeInteger, nil
	case uint, uint8, uint16, uint32, uint64:
		return domain.DataTypeInteger, nil
	case float32, float64:
		return domain.DataTypeFloat, nil
	case bool:
		return domain.DataTypeBoolean, nil
	case time.Time:
		return domain.DataTypeDateTime, nil
	case []byte:
		return domain.DataTypeBinary, nil
	default:
		return domain.DataTypeUnknown, fmt.Errorf("unsupported Go type: %T", goType)
	}
}

// MapDatabaseType maps a PostgreSQL type name, as written in SQL or reported by
// information_schema (e.g. "character varying(255)", "integer[]", "_int4",
// "ARRAY", "timestamp(3) with time zone"), to a domain.DataType.
func (a *Adapter) MapDatabaseType(dbType string) (domain.DataType, error) {
	t := strings.ToLower(strings.TrimSpace(dbType))
	if strings.HasSuffix(t, "[]") || (len(t) > 1 && t[0] == '_') || t == "array" {
		return domain.DataTypeArray, nil
	}
	if open := strings.IndexByte(t, '('); open >= 0 { // drop type modifiers
		if end := strings.IndexByte(t[open:], ')'); end >= 0 {
			t = t[:open] + " " + t[open+end+1:]
		}
	}
	t = strings.Join(strings.Fields(t), " ")

	switch t {
	case "text", "varchar", "char", "character", "character varying", "bpchar", "name":
		return domain.DataTypeString, nil
	case "smallint", "int2", "integer", "int", "int4", "bigint", "int8",
		"smallserial", "serial2", "serial", "serial4", "bigserial", "serial8":
		return domain.DataTypeInteger, nil
	case "real", "float4", "double precision", "float8", "float":
		return domain.DataTypeFloat, nil
	case "numeric", "decimal":
		return domain.DataTypeDecimal, nil
	case "boolean", "bool":
		return domain.DataTypeBoolean, nil
	case "timestamp", "timestamptz", "timestamp with time zone", "timestamp without time zone":
		return domain.DataTypeDateTime, nil
	case "date":
		return domain.DataTypeDate, nil
	case "time", "timetz", "time with time zone", "time without time zone":
		return domain.DataTypeTime, nil
	case "bytea":
		return domain.DataTypeBinary, nil
	case "json", "jsonb":
		return domain.DataTypeJSON, nil
	case "uuid":
		return domain.DataTypeUUID, nil
	default:
		return domain.DataTypeUnknown, fmt.Errorf("unsupported PostgreSQL type: %s", dbType)
	}
}

// SupportsTransactions returns true if the adapter supports transactions
func (a *Adapter) SupportsTransactions() bool {
	return true
}

// SupportsJoins returns true if the adapter supports joins
func (a *Adapter) SupportsJoins() bool {
	return true
}

// SupportsBatch returns true if the adapter supports batch operations
func (a *Adapter) SupportsBatch() bool {
	return true
}

// SupportsSchema returns true if the adapter supports schema operations
func (a *Adapter) SupportsSchema() bool {
	return true
}

// buildDSN builds a key=value DSN from the config. Every value is quoted and
// escaped (libpq rules), so values with spaces, quotes or backslashes cannot
// break the DSN or inject parameters. Options are appended in sorted key order.
func buildDSN(config domain.Config) (string, error) {
	if config.DSN != "" {
		return config.DSN, nil
	}

	var b strings.Builder
	add := func(key, value string) {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(key)
		b.WriteString("='")
		b.WriteString(strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value))
		b.WriteByte('\'')
	}

	add("host", config.Host)
	if config.Port > 0 {
		add("port", strconv.Itoa(config.Port))
	}
	add("dbname", config.Database)
	add("user", config.Username)
	if config.Password != "" {
		add("password", config.Password)
	}
	if config.SSLMode != "" {
		add("sslmode", config.SSLMode)
	} else {
		add("sslmode", "prefer")
	}
	if config.SSLCert != "" {
		add("sslcert", config.SSLCert)
	}
	if config.SSLKey != "" {
		add("sslkey", config.SSLKey)
	}
	if config.SSLRootCA != "" {
		add("sslrootcert", config.SSLRootCA)
	}
	// ConnectTimeout is applied to the pool config directly (sub-second precision).

	keys := make([]string, 0, len(config.Options))
	for key := range config.Options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !validOptionKey(key) {
			return "", invalidConfig(fmt.Sprintf("invalid connection option name %q", key))
		}
		value, err := optionValue(config.Options[key])
		if err != nil {
			return "", invalidConfig(fmt.Sprintf("connection option %q: %v", key, err))
		}
		add(key, value)
	}

	if strings.IndexByte(b.String(), 0) >= 0 {
		return "", invalidConfig("connection settings cannot contain NUL bytes")
	}
	return b.String(), nil
}

// validOptionKey reports whether key is a plausible connection parameter or
// run-time setting name (letters, digits, '_', '.', '-').
func validOptionKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if !(r == '_' || r == '.' || r == '-' || (r >= '0' && r <= '9') || (r|0x20 >= 'a' && r|0x20 <= 'z')) {
			return false
		}
	}
	return true
}

// optionValue formats an Options value for the DSN.
func optionValue(v interface{}) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case time.Duration:
		return x.String(), nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(x), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}
