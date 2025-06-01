package storage

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

// Query represents a database query
type Query struct {
	SQL        string
	Parameters []interface{}
	Type       QueryType
	Options    QueryOptions
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
	Timeout     time.Duration
	CacheKey    string
	CacheTTL    time.Duration
	ReadOnly    bool
	Consistency ConsistencyLevel
}

// Command represents a database command (INSERT, UPDATE, DELETE)
type Command struct {
	SQL        string
	Parameters []interface{}
	Type       CommandType
	Options    CommandOptions
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
	Timeout     time.Duration
	ReturnID    bool
	ReturnCount bool
	OnConflict  ConflictResolution
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
	LastInsertID int64
	Error        error
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

// Condition represents a query condition
type Condition struct {
	Field    string
	Operator Operator
	Value    interface{}
	Values   []interface{} // For IN, BETWEEN operators
}

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

// ConsistencyLevel represents read consistency level
type ConsistencyLevel int

const (
	ConsistencyLevelEventual ConsistencyLevel = iota
	ConsistencyLevelStrong
	ConsistencyLevelSession
)

// ConflictResolution represents how to handle conflicts
type ConflictResolution int

const (
	ConflictResolutionError ConflictResolution = iota
	ConflictResolutionIgnore
	ConflictResolutionReplace
	ConflictResolutionUpdate
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

	// Timeouts
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
	TxTimeout      time.Duration

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

// PoolStats contains connection pool statistics
type PoolStats struct {
	OpenConnections int
	IdleConnections int
	ActiveQueries   int
	TotalQueries    int64
	TotalErrors     int64
}

// PoolHealth represents the health of the connection pool
type PoolHealth struct {
	Status           HealthStatusType
	AvailableConns   int
	MaxConns         int
	AverageWaitTime  time.Duration
	ConnectionErrors int64
}

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

// TypeConverter represents a function that converts between types
type TypeConverter func(value interface{}) (interface{}, error)

// FieldInfo represents information about a struct field
type FieldInfo struct {
	Name     string
	Type     DataType
	Tags     map[string]string
	Index    int
	Embedded bool
}

// Field represents a logging field
type Field struct {
	Key   string
	Value interface{}
}

// CacheStats contains cache statistics
type CacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Size      int64
	MaxSize   int64
	HitRatio  float64
}

// CacheHealth represents cache health
type CacheHealth struct {
	Status      HealthStatusType
	Connections int
	Memory      int64
	MaxMemory   int64
}

// Migration represents a database migration
type Migration struct {
	ID          string
	Description string
	UpSQL       string
	DownSQL     string
	Version     int64
	Checksum    string
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Migration Migration
	Applied   bool
	AppliedAt time.Time
	Error     error
}

// AppliedMigration represents an applied migration
type AppliedMigration struct {
	ID        string
	Version   int64
	AppliedAt time.Time
	Checksum  string
}

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
