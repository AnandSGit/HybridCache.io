// Package domain provides the core domain interfaces and types for the Database Agnostic Storage Library.
// This package contains the business logic and domain rules that are independent of any specific database implementation.
package domain

import (
	"context"
)

// Storage is the main interface for database operations
type Storage interface {
	// Connection management. Adapter.Connect returns an already-connected Storage;
	// Connect only re-verifies connectivity.
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

	// Schema operations
	CreateTable(ctx context.Context, schema TableSchema) error
	DropTable(ctx context.Context, tableName string) error
	AlterTable(ctx context.Context, tableName string, changes []SchemaChange) error

	// Metadata and introspection
	Info() StorageInfo
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, tableName string) (TableSchema, error)
}

// QueryBuilder provides a fluent API for building queries.
// Build populates both the SQL form (Query.SQL with '?' placeholders) and the
// structured form (Query.Spec) so that SQL and document adapters can execute it.
type QueryBuilder interface {
	// Selection
	Select(fields ...string) QueryBuilder
	SelectDistinct(fields ...string) QueryBuilder
	SelectCount(field string) QueryBuilder

	// Data source
	From(table string) QueryBuilder
	FromSubquery(subquery QueryBuilder, alias string) QueryBuilder

	// Filtering. Where/WhereAnd conditions are combined with AND;
	// WhereOr adds a single parenthesised OR group.
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

	// Build
	Build() (Query, error)
	BuildRaw() (string, []interface{}, error)
}

// Transaction represents a database transaction.
// Commit, Rollback and the savepoint methods run under the context passed to
// Storage.BeginTx (bounded by TxOptions.Timeout / Config.TxTimeout when set),
// mirroring database/sql semantics.
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
