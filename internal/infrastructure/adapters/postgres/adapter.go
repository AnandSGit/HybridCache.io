// Package postgres provides PostgreSQL adapter for the storage library
package postgres

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Adapter implements the domain.Adapter interface for PostgreSQL
type Adapter struct {
	name    string
	version string
}

// NewAdapter creates a new PostgreSQL adapter
func NewAdapter() storage.Adapter {
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
func (a *Adapter) DatabaseType() storage.DatabaseType {
	return storage.DatabaseTypePostgreSQL
}

// Connect creates a new storage instance
func (a *Adapter) Connect(ctx context.Context, config storage.Config) (storage.Storage, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, storage.NewConnectionError("INVALID_CONFIG", err.Error()).WithCause(err)
	}

	dsn, err := a.buildDSN(config)
	if err != nil {
		return nil, storage.NewConnectionError("INVALID_DSN", "failed to build DSN").WithCause(err)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, storage.NewConnectionError("INVALID_DSN", "failed to parse DSN").WithCause(err)
	}

	// Configure connection pool
	poolConfig.MaxConns = int32(config.MaxOpenConns)
	poolConfig.MinConns = int32(config.MaxIdleConns)
	poolConfig.MaxConnLifetime = config.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = config.ConnMaxIdleTime

	// Configure connection timeouts
	if config.ConnectTimeout > 0 {
		poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, storage.NewConnectionError("CONNECTION_FAILED", "failed to create connection pool").WithCause(err)
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, storage.NewConnectionError("CONNECTION_FAILED", "failed to ping database").WithCause(err)
	}

	return &PostgreSQLStorage{
		pool:    pool,
		config:  config,
		adapter: a,
	}, nil
}

// ParseDSN parses a PostgreSQL DSN into a Config
func (a *Adapter) ParseDSN(dsn string) (storage.Config, error) {
	config := storage.Config{
		Options: make(map[string]interface{}),
	}

	// Handle postgres:// URLs
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return config, fmt.Errorf("invalid DSN URL: %w", err)
		}

		config.Host = u.Hostname()
		if u.Port() != "" {
			if port, err := strconv.Atoi(u.Port()); err == nil {
				config.Port = port
			}
		}

		if u.User != nil {
			config.Username = u.User.Username()
			if password, ok := u.User.Password(); ok {
				config.Password = password
			}
		}

		config.Database = strings.TrimPrefix(u.Path, "/")

		// Parse query parameters
		for key, values := range u.Query() {
			if len(values) > 0 {
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
		}
	} else {
		// Handle key=value format
		config.DSN = dsn
	}

	return config, nil
}

// ValidateConfig validates the configuration
func (a *Adapter) ValidateConfig(config storage.Config) error {
	if config.DSN == "" {
		if config.Host == "" {
			return fmt.Errorf("host is required")
		}
		if config.Database == "" {
			return fmt.Errorf("database is required")
		}
		if config.Username == "" {
			return fmt.Errorf("username is required")
		}
	}

	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be positive")
	}

	if config.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative")
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("max idle connections cannot exceed max open connections")
	}

	return nil
}

// TranslateQuery translates a storage.Query to PostgreSQL SQL
func (a *Adapter) TranslateQuery(query storage.Query) (string, []interface{}, error) {
	// PostgreSQL uses $1, $2, etc. for parameters
	sql := query.SQL
	params := query.Parameters

	// Convert ? placeholders to $n format
	paramIndex := 1
	var result strings.Builder
	for _, char := range sql {
		if char == '?' {
			result.WriteString(fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		} else {
			result.WriteRune(char)
		}
	}

	return result.String(), params, nil
}

// TranslateCommand translates a storage.Command to PostgreSQL SQL
func (a *Adapter) TranslateCommand(command storage.Command) (string, []interface{}, error) {
	return a.TranslateQuery(storage.Query{
		SQL:        command.SQL,
		Parameters: command.Parameters,
	})
}

// MapGoType maps a Go type to a storage.DataType
func (a *Adapter) MapGoType(goType interface{}) (storage.DataType, error) {
	switch goType.(type) {
	case string:
		return storage.DataTypeString, nil
	case int, int8, int16, int32, int64:
		return storage.DataTypeInteger, nil
	case uint, uint8, uint16, uint32, uint64:
		return storage.DataTypeInteger, nil
	case float32, float64:
		return storage.DataTypeFloat, nil
	case bool:
		return storage.DataTypeBoolean, nil
	case time.Time:
		return storage.DataTypeDateTime, nil
	case []byte:
		return storage.DataTypeBinary, nil
	default:
		return storage.DataTypeUnknown, fmt.Errorf("unsupported Go type: %T", goType)
	}
}

// MapDatabaseType maps a PostgreSQL type to a storage.DataType
func (a *Adapter) MapDatabaseType(dbType string) (storage.DataType, error) {
	switch strings.ToLower(dbType) {
	case "text", "varchar", "char", "character", "character varying":
		return storage.DataTypeString, nil
	case "integer", "int", "int4", "bigint", "int8", "smallint", "int2":
		return storage.DataTypeInteger, nil
	case "real", "float4", "double precision", "float8", "numeric", "decimal":
		return storage.DataTypeFloat, nil
	case "boolean", "bool":
		return storage.DataTypeBoolean, nil
	case "timestamp", "timestamptz", "timestamp with time zone", "timestamp without time zone":
		return storage.DataTypeDateTime, nil
	case "date":
		return storage.DataTypeDate, nil
	case "time", "timetz", "time with time zone", "time without time zone":
		return storage.DataTypeTime, nil
	case "bytea":
		return storage.DataTypeBinary, nil
	case "json", "jsonb":
		return storage.DataTypeJSON, nil
	case "uuid":
		return storage.DataTypeUUID, nil
	case "array":
		return storage.DataTypeArray, nil
	default:
		return storage.DataTypeUnknown, fmt.Errorf("unsupported PostgreSQL type: %s", dbType)
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

// buildDSN builds a PostgreSQL DSN from the config
func (a *Adapter) buildDSN(config storage.Config) (string, error) {
	if config.DSN != "" {
		return config.DSN, nil
	}

	var parts []string

	// Host and port
	if config.Port > 0 {
		parts = append(parts, fmt.Sprintf("host=%s port=%d", config.Host, config.Port))
	} else {
		parts = append(parts, fmt.Sprintf("host=%s", config.Host))
	}

	// Database
	parts = append(parts, fmt.Sprintf("dbname=%s", config.Database))

	// User and password
	parts = append(parts, fmt.Sprintf("user=%s", config.Username))
	if config.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", config.Password))
	}

	// SSL mode
	if config.SSLMode != "" {
		parts = append(parts, fmt.Sprintf("sslmode=%s", config.SSLMode))
	} else {
		parts = append(parts, "sslmode=prefer")
	}

	// SSL certificates
	if config.SSLCert != "" {
		parts = append(parts, fmt.Sprintf("sslcert=%s", config.SSLCert))
	}
	if config.SSLKey != "" {
		parts = append(parts, fmt.Sprintf("sslkey=%s", config.SSLKey))
	}
	if config.SSLRootCA != "" {
		parts = append(parts, fmt.Sprintf("sslrootcert=%s", config.SSLRootCA))
	}

	// Timeouts
	if config.ConnectTimeout > 0 {
		parts = append(parts, fmt.Sprintf("connect_timeout=%d", int(config.ConnectTimeout.Seconds())))
	}

	// Additional options
	for key, value := range config.Options {
		if strValue, ok := value.(string); ok {
			parts = append(parts, fmt.Sprintf("%s=%s", key, strValue))
		}
	}

	return strings.Join(parts, " "), nil
}

// PostgreSQLStorage implements the storage.Storage interface for PostgreSQL
type PostgreSQLStorage struct {
	pool    *pgxpool.Pool
	config  storage.Config
	adapter *Adapter
}

// Connect establishes the connection (already done in constructor)
func (s *PostgreSQLStorage) Connect(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close closes the connection pool
func (s *PostgreSQLStorage) Close() error {
	s.pool.Close()
	return nil
}

// Ping tests the connection
func (s *PostgreSQLStorage) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Health returns the health status
func (s *PostgreSQLStorage) Health(ctx context.Context) storage.HealthStatus {
	status := storage.HealthStatus{
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}

	if err := s.pool.Ping(ctx); err != nil {
		status.Status = storage.HealthStatusUnhealthy
		status.Message = fmt.Sprintf("ping failed: %v", err)
		return status
	}

	stats := s.pool.Stat()
	status.Status = storage.HealthStatusHealthy
	status.Message = "connection pool healthy"
	status.Details["total_connections"] = stats.TotalConns()
	status.Details["idle_connections"] = stats.IdleConns()
	status.Details["acquired_connections"] = stats.AcquiredConns()

	return status
}

// Info returns storage information
func (s *PostgreSQLStorage) Info() storage.StorageInfo {
	return storage.StorageInfo{
		Name:         s.adapter.Name(),
		Version:      s.adapter.Version(),
		DatabaseType: s.adapter.DatabaseType(),
		Features: []string{
			"transactions",
			"joins",
			"batch",
			"schema",
			"json",
			"arrays",
			"full-text-search",
		},
		Limits: storage.StorageLimits{
			MaxConnections:    s.config.MaxOpenConns,
			MaxQuerySize:      1024 * 1024 * 1024, // 1GB
			MaxTransactionAge: 24 * time.Hour,
			MaxBatchSize:      10000,
		},
	}
}

// Query executes a query and returns results
func (s *PostgreSQLStorage) Query(ctx context.Context, query storage.Query) (storage.Result, error) {
	sql, params, err := s.adapter.TranslateQuery(query)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}

	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return &PostgreSQLResult{rows: rows}, nil
}

// QueryOne executes a query and returns a single row
func (s *PostgreSQLStorage) QueryOne(ctx context.Context, query storage.Query) (storage.Row, error) {
	sql, params, err := s.adapter.TranslateQuery(query)
	if err != nil {
		return nil, storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}

	row := s.pool.QueryRow(ctx, sql, params...)
	return &PostgreSQLRow{row: row}, nil
}

// Execute executes a command and returns the result
func (s *PostgreSQLStorage) Execute(ctx context.Context, command storage.Command) (storage.ExecuteResult, error) {
	sql, params, err := s.adapter.TranslateCommand(command)
	if err != nil {
		return storage.ExecuteResult{}, storage.NewQueryError("COMMAND_TRANSLATION_FAILED", err.Error()).WithCause(err)
	}

	result, err := s.pool.Exec(ctx, sql, params...)
	if err != nil {
		return storage.ExecuteResult{}, storage.NewQueryError("COMMAND_EXECUTION_FAILED", err.Error()).WithCause(err)
	}

	return storage.ExecuteResult{
		RowsAffected: result.RowsAffected(),
		LastInsertID: 0, // PostgreSQL doesn't support LastInsertID
	}, nil
}

// BeginTx starts a new transaction
func (s *PostgreSQLStorage) BeginTx(ctx context.Context, opts *storage.TxOptions) (storage.Transaction, error) {
	var pgxOpts pgx.TxOptions

	if opts != nil {
		switch opts.Isolation {
		case storage.IsolationLevelReadUncommitted:
			pgxOpts.IsoLevel = pgx.ReadUncommitted
		case storage.IsolationLevelReadCommitted:
			pgxOpts.IsoLevel = pgx.ReadCommitted
		case storage.IsolationLevelRepeatableRead:
			pgxOpts.IsoLevel = pgx.RepeatableRead
		case storage.IsolationLevelSerializable:
			pgxOpts.IsoLevel = pgx.Serializable
		default:
			pgxOpts.IsoLevel = pgx.ReadCommitted
		}

		if opts.ReadOnly {
			pgxOpts.AccessMode = pgx.ReadOnly
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgxOpts)
	if err != nil {
		return nil, storage.NewTransactionError("TRANSACTION_BEGIN_FAILED", err.Error()).WithCause(err)
	}

	return &PostgreSQLTransaction{
		tx:      tx,
		adapter: s.adapter,
		id:      fmt.Sprintf("tx_%d", time.Now().UnixNano()),
		active:  true,
	}, nil
}

// Batch executes multiple operations in a batch
func (s *PostgreSQLStorage) Batch(ctx context.Context, operations []storage.Operation) ([]storage.OperationResult, error) {
	results := make([]storage.OperationResult, len(operations))

	batch := &pgx.Batch{}

	// Add all operations to the batch
	for i, op := range operations {
		switch op.Type {
		case storage.OperationTypeQuery:
			sql, params, err := s.adapter.TranslateQuery(op.Query)
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("QUERY_TRANSLATION_FAILED", err.Error()).WithCause(err),
				}
				continue
			}
			batch.Queue(sql, params...)
		case storage.OperationTypeCommand:
			sql, params, err := s.adapter.TranslateCommand(op.Command)
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("COMMAND_TRANSLATION_FAILED", err.Error()).WithCause(err),
				}
				continue
			}
			batch.Queue(sql, params...)
		}
	}

	// Execute the batch
	batchResults := s.pool.SendBatch(ctx, batch)
	defer batchResults.Close()

	// Process results
	for i, op := range operations {
		if results[i].Error != nil {
			continue // Skip operations that failed translation
		}

		switch op.Type {
		case storage.OperationTypeQuery:
			rows, err := batchResults.Query()
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("BATCH_QUERY_FAILED", err.Error()).WithCause(err),
				}
			} else {
				results[i] = storage.OperationResult{
					Index:  i,
					Result: &PostgreSQLResult{rows: rows},
				}
			}
		case storage.OperationTypeCommand:
			cmdTag, err := batchResults.Exec()
			if err != nil {
				results[i] = storage.OperationResult{
					Index: i,
					Error: storage.NewQueryError("BATCH_COMMAND_FAILED", err.Error()).WithCause(err),
				}
			} else {
				results[i] = storage.OperationResult{
					Index: i,
					Result: storage.ExecuteResult{
						RowsAffected: cmdTag.RowsAffected(),
						LastInsertID: 0,
					},
				}
			}
		}
	}

	return results, nil
}

// Schema operations
func (s *PostgreSQLStorage) CreateTable(ctx context.Context, schema storage.TableSchema) error {
	sql := s.buildCreateTableSQL(schema)
	_, err := s.pool.Exec(ctx, sql)
	if err != nil {
		return storage.NewSchemaError("CREATE_TABLE_FAILED", err.Error()).WithCause(err)
	}
	return nil
}

func (s *PostgreSQLStorage) DropTable(ctx context.Context, tableName string) error {
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	_, err := s.pool.Exec(ctx, sql)
	if err != nil {
		return storage.NewSchemaError("DROP_TABLE_FAILED", err.Error()).WithCause(err)
	}
	return nil
}

func (s *PostgreSQLStorage) AlterTable(ctx context.Context, tableName string, changes []storage.SchemaChange) error {
	for _, change := range changes {
		sql := s.buildAlterTableSQL(tableName, change)
		if sql != "" {
			_, err := s.pool.Exec(ctx, sql)
			if err != nil {
				return storage.NewSchemaError("ALTER_TABLE_FAILED", err.Error()).WithCause(err)
			}
		}
	}
	return nil
}

func (s *PostgreSQLStorage) ListTables(ctx context.Context) ([]string, error) {
	sql := `SELECT table_name FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`

	rows, err := s.pool.Query(ctx, sql)
	if err != nil {
		return nil, storage.NewSchemaError("LIST_TABLES_FAILED", err.Error()).WithCause(err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, storage.NewSchemaError("SCAN_TABLE_NAME_FAILED", err.Error()).WithCause(err)
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}

func (s *PostgreSQLStorage) DescribeTable(ctx context.Context, tableName string) (storage.TableSchema, error) {
	schema := storage.TableSchema{
		Name:    tableName,
		Columns: []storage.ColumnDefinition{},
	}

	sql := `SELECT column_name, data_type, is_nullable, column_default, character_maximum_length
			FROM information_schema.columns
			WHERE table_name = $1 AND table_schema = 'public'
			ORDER BY ordinal_position`

	rows, err := s.pool.Query(ctx, sql, tableName)
	if err != nil {
		return schema, storage.NewSchemaError("DESCRIBE_TABLE_FAILED", err.Error()).WithCause(err)
	}
	defer rows.Close()

	for rows.Next() {
		var columnName, dataType, isNullable string
		var columnDefault *string
		var maxLength *int64

		if err := rows.Scan(&columnName, &dataType, &isNullable, &columnDefault, &maxLength); err != nil {
			return schema, storage.NewSchemaError("SCAN_COLUMN_FAILED", err.Error()).WithCause(err)
		}

		storageType, _ := s.adapter.MapDatabaseType(dataType)

		column := storage.ColumnDefinition{
			Name:     columnName,
			DataType: storageType,
			Nullable: isNullable == "YES",
		}

		if maxLength != nil {
			column.Length = *maxLength
		}

		if columnDefault != nil {
			column.DefaultValue = *columnDefault
		}

		schema.Columns = append(schema.Columns, column)
	}

	return schema, nil
}

// Helper methods for schema operations
func (s *PostgreSQLStorage) buildCreateTableSQL(schema storage.TableSchema) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (", schema.Name))

	var columnDefs []string
	for _, col := range schema.Columns {
		colDef := s.buildColumnDefinition(col)
		columnDefs = append(columnDefs, colDef)
	}

	parts = append(parts, strings.Join(columnDefs, ", "))
	parts = append(parts, ")")

	return strings.Join(parts, "")
}

func (s *PostgreSQLStorage) buildColumnDefinition(col storage.ColumnDefinition) string {
	var parts []string
	parts = append(parts, col.Name)

	// Map storage type to PostgreSQL type
	pgType := s.mapStorageTypeToPostgreSQL(col.DataType, col.Length)
	parts = append(parts, pgType)

	if !col.Nullable {
		parts = append(parts, "NOT NULL")
	}

	if col.DefaultValue != nil {
		parts = append(parts, fmt.Sprintf("DEFAULT %v", col.DefaultValue))
	}

	if col.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}

	if col.AutoIncrement {
		parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
	}

	return strings.Join(parts, " ")
}

func (s *PostgreSQLStorage) buildAlterTableSQL(tableName string, change storage.SchemaChange) string {
	switch change.Type {
	case storage.SchemaChangeTypeAddColumn:
		colDef := s.buildColumnDefinition(change.Column)
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableName, colDef)
	case storage.SchemaChangeTypeDropColumn:
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, change.Column.Name)
	case storage.SchemaChangeTypeModifyColumn:
		colDef := s.buildColumnDefinition(change.Column)
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", tableName, colDef)
	default:
		return ""
	}
}

func (s *PostgreSQLStorage) mapStorageTypeToPostgreSQL(dataType storage.DataType, length int64) string {
	switch dataType {
	case storage.DataTypeString:
		if length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", length)
		}
		return "TEXT"
	case storage.DataTypeInteger:
		return "INTEGER"
	case storage.DataTypeFloat:
		return "DOUBLE PRECISION"
	case storage.DataTypeBoolean:
		return "BOOLEAN"
	case storage.DataTypeDateTime:
		return "TIMESTAMP"
	case storage.DataTypeDate:
		return "DATE"
	case storage.DataTypeTime:
		return "TIME"
	case storage.DataTypeBinary:
		return "BYTEA"
	case storage.DataTypeJSON:
		return "JSONB"
	case storage.DataTypeUUID:
		return "UUID"
	case storage.DataTypeArray:
		return "TEXT[]"
	case storage.DataTypeDecimal:
		return "DECIMAL"
	case storage.DataTypeText:
		return "TEXT"
	default:
		return "TEXT"
	}
}
