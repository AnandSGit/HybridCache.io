// Package domain provides the core domain interfaces and types for the Database Agnostic Storage Library.
// This package contains the business logic and domain rules that are independent of any specific database implementation.
package domain

import (
	"context"
	"time"
)

// Storage is the main interface for database operations
type Storage interface {
	// Connection management
	Connect(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error
	Health(ctx context.Context) HealthStatus

	// Query operations
	Query(ctx context.Context, query Query) (Result, error)
	QueryOne(ctx context.Context, query Query) (Row, error)
	Execute(ctx context.Context, command Command) (ExecuteResult, error)

	// Transaction support
	BeginTx(ctx context.Context, opts *TxOptions) (Transaction, error)

	// Batch operations
	Batch(ctx context.Context, operations []Operation) ([]OperationResult, error)

	// Schema operations (for SQL databases)
	CreateTable(ctx context.Context, schema TableSchema) error
	DropTable(ctx context.Context, tableName string) error
	AlterTable(ctx context.Context, tableName string, changes []SchemaChange) error

	// Metadata and introspection
	Info() StorageInfo
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, tableName string) (TableSchema, error)
}

// QueryBuilder provides a fluent API for building queries
type QueryBuilder interface {
	// Selection
	Select(fields ...string) QueryBuilder
	SelectDistinct(fields ...string) QueryBuilder
	SelectCount(field string) QueryBuilder

	// Data source
	From(table string) QueryBuilder
	FromSubquery(subquery QueryBuilder, alias string) QueryBuilder

	// Filtering
	Where(condition Condition) QueryBuilder
	WhereAnd(conditions ...Condition) QueryBuilder
	WhereOr(conditions ...Condition) QueryBuilder
	Having(condition Condition) QueryBuilder

	// Joins
	Join(joinType JoinType, table string, on Condition) QueryBuilder
	LeftJoin(table string, on Condition) QueryBuilder
	RightJoin(table string, on Condition) QueryBuilder
	InnerJoin(table string, on Condition) QueryBuilder

	// Grouping and ordering
	GroupBy(fields ...string) QueryBuilder
	OrderBy(field string, direction SortDirection) QueryBuilder
	OrderByDesc(field string) QueryBuilder
	OrderByAsc(field string) QueryBuilder

	// Pagination
	Limit(limit int) QueryBuilder
	Offset(offset int) QueryBuilder
	Page(page, size int) QueryBuilder

	// Build and execute
	Build() (Query, error)
	BuildRaw() (string, []interface{}, error)
}

// Transaction represents a database transaction
type Transaction interface {
	// Query operations within transaction
	Query(ctx context.Context, query Query) (Result, error)
	QueryOne(ctx context.Context, query Query) (Row, error)
	Execute(ctx context.Context, command Command) (ExecuteResult, error)

	// Batch operations within transaction
	Batch(ctx context.Context, operations []Operation) ([]OperationResult, error)

	// Transaction control
	Commit() error
	Rollback() error
	Savepoint(name string) error
	RollbackToSavepoint(name string) error

	// Transaction state
	IsActive() bool
	ID() string
}

// ConnectionPool manages database connections
type ConnectionPool interface {
	// Connection management
	Get(ctx context.Context) (Connection, error)
	Put(conn Connection) error
	Close() error

	// Pool statistics
	Stats() PoolStats
	Health() PoolHealth

	// Configuration
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
	SetConnMaxLifetime(d time.Duration)
	SetConnMaxIdleTime(d time.Duration)
}

// Connection represents a single database connection
type Connection interface {
	// Basic operations
	Query(ctx context.Context, query string, args ...interface{}) (Result, error)
	Execute(ctx context.Context, command string, args ...interface{}) (ExecuteResult, error)

	// Transaction support
	Begin(ctx context.Context) (Transaction, error)

	// Connection state
	IsValid() bool
	LastUsed() time.Time
	Close() error
}

// Adapter defines the interface that database-specific adapters must implement
type Adapter interface {
	// Adapter identification
	Name() string
	Version() string
	DatabaseType() DatabaseType

	// Connection management
	Connect(ctx context.Context, config Config) (Storage, error)
	ParseDSN(dsn string) (Config, error)
	ValidateConfig(config Config) error

	// Query translation
	TranslateQuery(query Query) (string, []interface{}, error)
	TranslateCommand(command Command) (string, []interface{}, error)

	// Type mapping
	MapGoType(goType interface{}) (DataType, error)
	MapDatabaseType(dbType string) (DataType, error)

	// Feature support
	SupportsTransactions() bool
	SupportsJoins() bool
	SupportsBatch() bool
	SupportsSchema() bool
}

// ResultMapper handles mapping between database results and Go types
type ResultMapper interface {
	// Mapping operations
	MapRow(row Row, dest interface{}) error
	MapRows(result Result, dest interface{}) error
	MapValue(value interface{}, dest interface{}) error

	// Type conversion
	ConvertType(value interface{}, targetType DataType) (interface{}, error)
	RegisterConverter(sourceType, targetType DataType, converter TypeConverter)

	// Reflection utilities
	GetStructFields(structType interface{}) ([]FieldInfo, error)
	GetFieldTags(field FieldInfo) map[string]string
}

// MetricsCollector defines the interface for collecting storage metrics
type MetricsCollector interface {
	// Query metrics
	RecordQueryDuration(operation string, duration time.Duration)
	RecordQueryError(operation string, errorType string)
	IncrementQueryCount(operation string)

	// Connection metrics
	RecordConnectionAcquired()
	RecordConnectionReleased()
	RecordConnectionError(errorType string)

	// Transaction metrics
	RecordTransactionStarted()
	RecordTransactionCommitted()
	RecordTransactionRolledBack()

	// Custom metrics
	RecordCustomMetric(name string, value float64, tags map[string]string)
}

// Logger defines the interface for storage logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
}

// CacheProvider defines the interface for query result caching
type CacheProvider interface {
	// Cache operations
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error

	// Cache statistics
	Stats() CacheStats
	Health() CacheHealth
}

// MigrationManager handles database schema migrations
type MigrationManager interface {
	// Migration operations
	Apply(ctx context.Context, migrations []Migration) error
	Rollback(ctx context.Context, steps int) error
	Status(ctx context.Context) ([]MigrationStatus, error)

	// Migration discovery
	LoadMigrations(path string) ([]Migration, error)
	ValidateMigrations(migrations []Migration) error

	// Migration history
	GetAppliedMigrations(ctx context.Context) ([]AppliedMigration, error)
	MarkMigrationApplied(ctx context.Context, migration Migration) error
}
