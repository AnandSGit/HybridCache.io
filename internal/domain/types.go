// Package domain provides the core domain types for the Database Agnostic Storage Library.
package domain

import (
	"database/sql/driver"
	"time"
)

// DatabaseType represents the type of database
type DatabaseType int

const (
	DatabaseTypeUnknown DatabaseType = iota
	DatabaseTypePostgreSQL
	DatabaseTypeMySQL
	DatabaseTypeSQLite
	DatabaseTypeRedis
	DatabaseTypeMongoDB
	DatabaseTypeCockroachDB
	DatabaseTypeDynamoDB
	DatabaseTypeCassandra
)

func (dt DatabaseType) String() string {
	switch dt {
	case DatabaseTypePostgreSQL:
		return "postgresql"
	case DatabaseTypeMySQL:
		return "mysql"
	case DatabaseTypeSQLite:
		return "sqlite"
	case DatabaseTypeRedis:
		return "redis"
	case DatabaseTypeMongoDB:
		return "mongodb"
	case DatabaseTypeCockroachDB:
		return "cockroachdb"
	case DatabaseTypeDynamoDB:
		return "dynamodb"
	case DatabaseTypeCassandra:
		return "cassandra"
	default:
		return "unknown"
	}
}

// DataType represents a database-agnostic data type
type DataType int

const (
	DataTypeUnknown DataType = iota
	DataTypeString
	DataTypeInteger
	DataTypeFloat
	DataTypeBoolean
	DataTypeDateTime
	DataTypeDate
	DataTypeTime
	DataTypeBinary
	DataTypeJSON
	DataTypeUUID
	DataTypeArray
	DataTypeMap
	DataTypeDecimal
	DataTypeText
)

func (dt DataType) String() string {
	switch dt {
	case DataTypeString:
		return "string"
	case DataTypeInteger:
		return "integer"
	case DataTypeFloat:
		return "float"
	case DataTypeBoolean:
		return "boolean"
	case DataTypeDateTime:
		return "datetime"
	case DataTypeDate:
		return "date"
	case DataTypeTime:
		return "time"
	case DataTypeBinary:
		return "binary"
	case DataTypeJSON:
		return "json"
	case DataTypeUUID:
		return "uuid"
	case DataTypeArray:
		return "array"
	case DataTypeMap:
		return "map"
	case DataTypeDecimal:
		return "decimal"
	case DataTypeText:
		return "text"
	default:
		return "unknown"
	}
}

// Query represents a database query.
//
// SQL adapters execute SQL/Parameters (raw SQL may use '?' or the database's native
// placeholders). Document adapters such as MongoDB cannot execute SQL text and require
// Spec, which the query builder always populates.
type Query struct {
	SQL        string
	Parameters []interface{}
	Type       QueryType
	Options    QueryOptions
	Spec       *QuerySpec
}

// QueryType represents the type of query
type QueryType int

const (
	QueryTypeSelect QueryType = iota
	QueryTypeInsert
	QueryTypeUpdate
	QueryTypeDelete
	QueryTypeCreate
	QueryTypeDrop
	QueryTypeAlter
	QueryTypeCustom
)

// QueryOptions contains query execution options
type QueryOptions struct {
	// Timeout bounds the query; when zero, Config.QueryTimeout applies (if set).
	Timeout time.Duration
}

// QuerySpec is the structured, database-neutral form of a SELECT produced by the
// query builder.
type QuerySpec struct {
	Table    string
	Fields   []string // select expressions as given; empty means all fields
	Distinct bool
	Where    []Condition // combined with AND
	OrderBy  []OrderBy
	Limit    *int
	Offset   *int

	// SQL-only features. Document adapters must reject a spec that uses them
	// with ErrUnsupportedQuery instead of silently ignoring them.
	Joins    []Join
	GroupBy  []string
	Having   []Condition // combined with AND
	Subquery bool        // the data source is a subquery (FromSubquery)
}

// OrderBy is a single ORDER BY term
type OrderBy struct {
	Field     string
	Direction SortDirection
}

// Join is a single JOIN clause
type Join struct {
	Type  JoinType
	Table string
	On    *Condition // nil for CROSS JOIN
}

// Command represents a database command (INSERT, UPDATE, DELETE).
// As with Query, Spec carries the structured form required by document adapters.
type Command struct {
	SQL        string
	Parameters []interface{}
	Type       CommandType
	Options    CommandOptions
	Spec       *CommandSpec
}

// CommandType represents the type of command
type CommandType int

const (
	CommandTypeInsert CommandType = iota
	CommandTypeUpdate
	CommandTypeDelete
	CommandTypeBulkInsert
	CommandTypeBulkUpdate
	CommandTypeBulkDelete
)

// CommandOptions contains command execution options
type CommandOptions struct {
	// Timeout bounds the command; when zero, Config.QueryTimeout applies (if set).
	Timeout time.Duration
}

// CommandSpec is the structured form of an INSERT/UPDATE/DELETE produced by the
// command builder.
type CommandSpec struct {
	Table  string
	Values []FieldValue // ordered column assignments (INSERT/UPDATE)
	Where  []Condition  // combined with AND (UPDATE/DELETE)
}

// FieldValue is a single column assignment
type FieldValue struct {
	Field string
	Value interface{}
}

// Result represents query results
type Result interface {
	// Navigation
	Next() bool
	Close() error

	// Data access
	Scan(dest ...interface{}) error
	ScanRow(dest interface{}) error
	Columns() ([]string, error)
	ColumnTypes() ([]ColumnType, error)

	// Metadata
	RowsAffected() (int64, error)
	LastInsertID() (int64, error)
	Err() error
}

// Row represents a single row result
type Row interface {
	Scan(dest ...interface{}) error
	ScanRow(dest interface{}) error
	Err() error
}

// ExecuteResult represents the result of a command execution
type ExecuteResult struct {
	RowsAffected int64
	LastInsertID int64 // 0 when the database has no integer insert IDs (PostgreSQL, MongoDB)
}

// Operation represents a batch operation
type Operation struct {
	Type    OperationType
	Query   Query
	Command Command
}

// OperationType represents the type of operation
type OperationType int

const (
	OperationTypeQuery OperationType = iota
	OperationTypeCommand
)

// OperationResult represents the result of a batch operation
type OperationResult struct {
	Index  int
	Result interface{} // Result or ExecuteResult
	Error  error
}

// Condition represents a query condition.
//
// Value semantics:
//   - a ColumnRef value is rendered as a column reference, not a bound parameter
//     (e.g. JOIN ... ON a.id = b.a_id);
//   - for OperatorExists/OperatorNotExists, Value must be a QueryBuilder (the subquery);
//   - for OperatorAnd/OperatorOr, the operands are in Conditions and Field is unused.
type Condition struct {
	Field      string
	Operator   Operator
	Value      interface{}
	Values     []interface{} // For IN, BETWEEN operators
	Conditions []Condition   // For AND, OR groups
}

// ColumnRef marks a condition value as a column reference rather than a literal.
type ColumnRef string

// Operator represents a comparison operator
type Operator int

const (
	OperatorEqual Operator = iota
	OperatorNotEqual
	OperatorGreaterThan
	OperatorGreaterThanOrEqual
	OperatorLessThan
	OperatorLessThanOrEqual
	OperatorLike
	OperatorNotLike
	OperatorIn
	OperatorNotIn
	OperatorBetween
	OperatorNotBetween
	OperatorIsNull
	OperatorIsNotNull
	OperatorExists
	OperatorNotExists
	OperatorAnd
	OperatorOr
)

// JoinType represents the type of join
type JoinType int

const (
	JoinTypeInner JoinType = iota
	JoinTypeLeft
	JoinTypeRight
	JoinTypeFull
	JoinTypeCross
)

// SortDirection represents the sort direction
type SortDirection int

const (
	SortDirectionAsc SortDirection = iota
	SortDirectionDesc
)

// TxOptions represents transaction options
type TxOptions struct {
	Isolation IsolationLevel
	ReadOnly  bool
	Timeout   time.Duration
}

// IsolationLevel represents transaction isolation level
type IsolationLevel int

const (
	IsolationLevelDefault IsolationLevel = iota
	IsolationLevelReadUncommitted
	IsolationLevelReadCommitted
	IsolationLevelRepeatableRead
	IsolationLevelSerializable
)

// Config represents database configuration
type Config struct {
	// Connection details
	Host     string
	Port     int
	Database string
	Username string
	Password string
	DSN      string

	// Connection pool settings
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Timeouts (zero means no timeout)
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration // default for QueryOptions/CommandOptions.Timeout
	TxTimeout      time.Duration // default for TxOptions.Timeout

	// SSL/TLS settings
	SSLMode   string
	SSLCert   string
	SSLKey    string
	SSLRootCA string

	// Additional options
	Options map[string]interface{}
}

// StorageInfo contains information about the storage instance
type StorageInfo struct {
	Name         string
	Version      string
	DatabaseType DatabaseType
	Features     []string
	Limits       StorageLimits
}

// StorageLimits contains storage limitations
type StorageLimits struct {
	MaxConnections    int
	MaxQuerySize      int64
	MaxTransactionAge time.Duration
	MaxBatchSize      int
}

// HealthStatus represents the health status of the storage
type HealthStatus struct {
	Status    HealthStatusType
	Message   string
	Timestamp time.Time
	Details   map[string]interface{}
}

// HealthStatusType represents the type of health status
type HealthStatusType int

const (
	HealthStatusHealthy HealthStatusType = iota
	HealthStatusDegraded
	HealthStatusUnhealthy
	HealthStatusUnknown
)

// ColumnType represents a database column type
type ColumnType struct {
	Name         string
	DatabaseType string
	DataType     DataType
	Nullable     bool
	Length       int64
	Precision    int64
	Scale        int64
}

// TableSchema represents a database table schema
type TableSchema struct {
	Name        string
	Columns     []ColumnDefinition
	Indexes     []IndexDefinition
	Constraints []ConstraintDefinition
}

// ColumnDefinition represents a column definition
type ColumnDefinition struct {
	Name          string
	DataType      DataType
	Length        int64
	Precision     int64
	Scale         int64
	Nullable      bool
	DefaultValue  interface{}
	AutoIncrement bool
	PrimaryKey    bool
}

// IndexDefinition represents an index definition
type IndexDefinition struct {
	Name    string
	Columns []string
	Unique  bool
	Type    IndexType
}

// IndexType represents the type of index
type IndexType int

const (
	IndexTypeBTree IndexType = iota
	IndexTypeHash
	IndexTypeGIN
	IndexTypeGiST
)

// ConstraintDefinition represents a constraint definition
type ConstraintDefinition struct {
	Name       string
	Type       ConstraintType
	Columns    []string
	RefTable   string
	RefColumns []string
	OnUpdate   ReferentialAction
	OnDelete   ReferentialAction
}

// ConstraintType represents the type of constraint
type ConstraintType int

const (
	ConstraintTypePrimaryKey ConstraintType = iota
	ConstraintTypeForeignKey
	ConstraintTypeUnique
	ConstraintTypeCheck
)

// ReferentialAction represents referential actions
type ReferentialAction int

const (
	ReferentialActionNoAction ReferentialAction = iota
	ReferentialActionRestrict
	ReferentialActionCascade
	ReferentialActionSetNull
	ReferentialActionSetDefault
)

// SchemaChange represents a schema change operation
type SchemaChange struct {
	Type       SchemaChangeType
	Column     ColumnDefinition
	Index      IndexDefinition
	Constraint ConstraintDefinition
}

// SchemaChangeType represents the type of schema change
type SchemaChangeType int

const (
	SchemaChangeTypeAddColumn SchemaChangeType = iota
	SchemaChangeTypeDropColumn
	SchemaChangeTypeModifyColumn
	SchemaChangeTypeAddIndex
	SchemaChangeTypeDropIndex
	SchemaChangeTypeAddConstraint
	SchemaChangeTypeDropConstraint
)

// Value implements driver.Valuer for DataType
func (dt DataType) Value() (driver.Value, error) {
	return dt.String(), nil
}

// Value implements driver.Valuer for DatabaseType
func (dbt DatabaseType) Value() (driver.Value, error) {
	return dbt.String(), nil
}

// Ensure types implement driver.Valuer interface where appropriate
var (
	_ driver.Valuer = DataType(0)
	_ driver.Valuer = DatabaseType(0)
)
