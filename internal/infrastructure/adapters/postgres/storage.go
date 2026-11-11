package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// PostgreSQLStorage implements the domain.Storage interface for PostgreSQL
type PostgreSQLStorage struct {
	pool     *pgxpool.Pool
	config   domain.Config
	adapter  *Adapter
	exec     executor
	maxConns int
}

func newStorage(pool *pgxpool.Pool, config domain.Config, adapter *Adapter, maxConns int) *PostgreSQLStorage {
	return &PostgreSQLStorage{
		pool:     pool,
		config:   config,
		adapter:  adapter,
		exec:     executor{db: pool, queryTimeout: config.QueryTimeout},
		maxConns: maxConns,
	}
}

// Connect re-verifies connectivity (Adapter.Connect returns a connected storage).
func (s *PostgreSQLStorage) Connect(ctx context.Context) error {
	return s.Ping(ctx)
}

// Close closes the connection pool, waiting for acquired connections to be released.
func (s *PostgreSQLStorage) Close() error {
	s.pool.Close()
	return nil
}

// Ping tests the connection
func (s *PostgreSQLStorage) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return connectionError(err, "failed to ping PostgreSQL")
	}
	return nil
}

// Health returns the health status
func (s *PostgreSQLStorage) Health(ctx context.Context) domain.HealthStatus {
	status := domain.HealthStatus{
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}

	if err := s.pool.Ping(ctx); err != nil {
		status.Status = domain.HealthStatusUnhealthy
		status.Message = fmt.Sprintf("ping failed: %v", err)
		return status
	}

	stats := s.pool.Stat()
	status.Status = domain.HealthStatusHealthy
	status.Message = "connection pool healthy"
	status.Details["total_connections"] = stats.TotalConns()
	status.Details["idle_connections"] = stats.IdleConns()
	status.Details["acquired_connections"] = stats.AcquiredConns()
	status.Details["max_connections"] = stats.MaxConns()

	return status
}

// Info returns storage information
func (s *PostgreSQLStorage) Info() domain.StorageInfo {
	return domain.StorageInfo{
		Name:         s.adapter.Name(),
		Version:      s.adapter.Version(),
		DatabaseType: s.adapter.DatabaseType(),
		Features: []string{
			"transactions",
			"joins",
			"batch",
			"schema",
			"json",
		},
		Limits: domain.StorageLimits{
			MaxConnections:    s.maxConns,
			MaxQuerySize:      1024 * 1024 * 1024, // 1GB
			MaxTransactionAge: 24 * time.Hour,
			MaxBatchSize:      10000,
		},
	}
}

// Query executes a query. Rows are streamed: close the result to release its
// connection. The query timeout (Options.Timeout, else Config.QueryTimeout)
// covers reading the rows.
func (s *PostgreSQLStorage) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	return s.exec.query(ctx, query)
}

// QueryOne executes a query and returns its first row. If there is no row, the
// returned Row's Scan and ScanRow return an error matching domain.ErrNoRows.
func (s *PostgreSQLStorage) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	return s.exec.queryOne(ctx, query)
}

// Execute executes a command and returns the result
func (s *PostgreSQLStorage) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	return s.exec.execute(ctx, command)
}

// BeginTx starts a new transaction. ctx is used until the transaction ends and
// is bounded by opts.Timeout (or Config.TxTimeout) when set.
func (s *PostgreSQLStorage) BeginTx(ctx context.Context, opts *domain.TxOptions) (domain.Transaction, error) {
	var pgxOpts pgx.TxOptions
	timeout := s.config.TxTimeout
	if opts != nil {
		switch opts.Isolation {
		case domain.IsolationLevelReadUncommitted:
			pgxOpts.IsoLevel = pgx.ReadUncommitted
		case domain.IsolationLevelReadCommitted:
			pgxOpts.IsoLevel = pgx.ReadCommitted
		case domain.IsolationLevelRepeatableRead:
			pgxOpts.IsoLevel = pgx.RepeatableRead
		case domain.IsolationLevelSerializable:
			pgxOpts.IsoLevel = pgx.Serializable
		}
		if opts.ReadOnly {
			pgxOpts.AccessMode = pgx.ReadOnly
		}
		if opts.Timeout > 0 {
			timeout = opts.Timeout
		}
	}

	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	tx, err := s.pool.BeginTx(ctx, pgxOpts)
	if err != nil {
		cancel()
		return nil, mapError(err, opTx, "begin transaction failed", "BEGIN")
	}
	return newTransaction(ctx, cancel, tx, s.config.QueryTimeout), nil
}

// Batch sends multiple operations in one round trip and runs them atomically:
// if one fails, Batch returns its error and none of the operations take effect.
// Query results are buffered and remain readable after Batch returns.
func (s *PostgreSQLStorage) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	return s.exec.batch(ctx, operations)
}

// CreateTable creates a table with its constraints and indexes in one transaction.
func (s *PostgreSQLStorage) CreateTable(ctx context.Context, schema domain.TableSchema) error {
	statements, err := createTableStatements(schema)
	if err != nil {
		return err
	}
	return s.execSchema(ctx, statements, "create table failed")
}

// DropTable drops a table if it exists.
func (s *PostgreSQLStorage) DropTable(ctx context.Context, tableName string) error {
	if tableName == "" {
		return invalidSchema("table name is required")
	}
	return s.execSchema(ctx, []string{"DROP TABLE IF EXISTS " + quoteTable(tableName)}, "drop table failed")
}

// AlterTable applies the changes in one transaction.
func (s *PostgreSQLStorage) AlterTable(ctx context.Context, tableName string, changes []domain.SchemaChange) error {
	if tableName == "" {
		return invalidSchema("table name is required")
	}
	var statements []string
	for _, change := range changes {
		stmts, err := alterTableStatements(tableName, change)
		if err != nil {
			return err
		}
		statements = append(statements, stmts...)
	}
	if len(statements) == 0 {
		return nil
	}
	return s.execSchema(ctx, statements, "alter table failed")
}

// execSchema runs DDL statements in a single transaction.
func (s *PostgreSQLStorage) execSchema(ctx context.Context, statements []string, msg string) error {
	var failed string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, stmt := range statements {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				failed = stmt
				return err
			}
		}
		return nil
	})
	return mapError(err, opSchema, msg, failed)
}

// ListTables lists the base tables of the current schema.
func (s *PostgreSQLStorage) ListTables(ctx context.Context) ([]string, error) {
	const sql = `SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
		ORDER BY table_name`

	rows, err := s.pool.Query(ctx, sql)
	if err != nil {
		return nil, mapError(err, opQuery, "list tables failed", sql)
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, mapError(err, opQuery, "list tables failed", sql)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err, opQuery, "list tables failed", sql)
	}
	return tables, nil
}

const describeTableSQL = `SELECT c.column_name, c.data_type, c.udt_name, c.is_nullable, c.column_default,
		c.character_maximum_length, c.numeric_precision, c.numeric_scale, c.is_identity,
		EXISTS (
			SELECT 1
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON kcu.constraint_schema = tc.constraint_schema
				AND kcu.constraint_name = tc.constraint_name
				AND kcu.table_schema = tc.table_schema
				AND kcu.table_name = tc.table_name
			WHERE tc.constraint_type = 'PRIMARY KEY'
				AND tc.table_schema = c.table_schema
				AND tc.table_name = c.table_name
				AND kcu.column_name = c.column_name
		) AS primary_key
	FROM information_schema.columns c
	WHERE c.table_schema = COALESCE(NULLIF($1::text, ''), current_schema()) AND c.table_name = $2
	ORDER BY c.ordinal_position`

const tableExistsSQL = `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = COALESCE(NULLIF($1::text, ''), current_schema()) AND table_name = $2
	)`

// DescribeTable describes the columns of a table in the current schema (or the
// schema named by a "schema.table" name). Indexes and constraints are not
// reported.
func (s *PostgreSQLStorage) DescribeTable(ctx context.Context, tableName string) (domain.TableSchema, error) {
	schema := domain.TableSchema{Name: tableName, Columns: []domain.ColumnDefinition{}}

	namespace, table := "", tableName
	if dot := strings.LastIndexByte(tableName, '.'); dot >= 0 {
		namespace, table = tableName[:dot], tableName[dot+1:]
	}

	rows, err := s.pool.Query(ctx, describeTableSQL, namespace, table)
	if err != nil {
		return schema, mapError(err, opQuery, "describe table failed", describeTableSQL)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name, dataType, udtName, isNullable, isIdentity string
			columnDefault                                   *string
			maxLength, precision, scale                     *int64
			primaryKey                                      bool
		)
		if err := rows.Scan(&name, &dataType, &udtName, &isNullable, &columnDefault,
			&maxLength, &precision, &scale, &isIdentity, &primaryKey); err != nil {
			return schema, mapError(err, opQuery, "describe table failed", describeTableSQL)
		}

		dt, err := s.adapter.MapDatabaseType(dataType)
		if err != nil {
			dt, _ = s.adapter.MapDatabaseType(udtName) // USER-DEFINED and other named types
		}

		column := domain.ColumnDefinition{
			Name:          name,
			DataType:      dt,
			Nullable:      isNullable == "YES",
			AutoIncrement: isIdentity == "YES" || (columnDefault != nil && strings.HasPrefix(*columnDefault, "nextval(")),
			PrimaryKey:    primaryKey,
		}
		if maxLength != nil {
			column.Length = *maxLength
		}
		if dt == domain.DataTypeDecimal {
			if precision != nil {
				column.Precision = *precision
			}
			if scale != nil {
				column.Scale = *scale
			}
		}
		if columnDefault != nil {
			column.DefaultValue = *columnDefault // the default's SQL expression
		}
		schema.Columns = append(schema.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return schema, mapError(err, opQuery, "describe table failed", describeTableSQL)
	}

	if len(schema.Columns) == 0 { // a table can have no columns: check it exists
		var exists bool
		if err := s.pool.QueryRow(ctx, tableExistsSQL, namespace, table).Scan(&exists); err != nil {
			return schema, mapError(err, opQuery, "describe table failed", tableExistsSQL)
		}
		if !exists {
			return schema, domain.NewTableNotFoundError(tableName)
		}
	}
	return schema, nil
}
