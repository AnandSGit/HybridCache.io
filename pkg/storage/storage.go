// Package storage provides the public API for the Database Agnostic Storage Library.
// This package exposes the core interfaces and types needed by client applications.
package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// Re-export core domain types and interfaces for public use

// Core interfaces
type (
	Storage          = domain.Storage
	Adapter          = domain.Adapter
	QueryBuilder     = domain.QueryBuilder
	Transaction      = domain.Transaction
	Result           = domain.Result
	Row              = domain.Row
	Connection       = domain.Connection
	HealthChecker    = domain.HealthChecker
	MetricsCollector = domain.MetricsCollector
	Logger           = domain.Logger
	CacheProvider    = domain.CacheProvider
)

// Core types
type (
	DatabaseType        = domain.DatabaseType
	DataType           = domain.DataType
	Query              = domain.Query
	Command            = domain.Command
	Config             = domain.Config
	StorageInfo        = domain.StorageInfo
	HealthStatus       = domain.HealthStatus
	TableSchema        = domain.TableSchema
	ColumnDefinition   = domain.ColumnDefinition
	IndexDefinition    = domain.IndexDefinition
	TxOptions          = domain.TxOptions
	IsolationLevel     = domain.IsolationLevel
	SortDirection      = domain.SortDirection
	QueryType          = domain.QueryType
	CommandType        = domain.CommandType
	OperationType      = domain.OperationType
	Operation          = domain.Operation
	OperationResult    = domain.OperationResult
	ExecuteResult      = domain.ExecuteResult
	ColumnType         = domain.ColumnType
	Condition          = domain.Condition
	Operator           = domain.Operator
	JoinType           = domain.JoinType
	ConflictResolution = domain.ConflictResolution
	SchemaChange       = domain.SchemaChange
	SchemaChangeType   = domain.SchemaChangeType
	IndexType          = domain.IndexType
	HealthStatusType   = domain.HealthStatusType
	ErrorType          = domain.ErrorType
	Field              = domain.Field
	CacheStats         = domain.CacheStats
	CacheHealth        = domain.CacheHealth
	StorageLimits      = domain.StorageLimits
	PoolStats          = domain.PoolStats
)

// Constants
const (
	// Database types
	DatabaseTypeUnknown     = domain.DatabaseTypeUnknown
	DatabaseTypePostgreSQL  = domain.DatabaseTypePostgreSQL
	DatabaseTypeMySQL       = domain.DatabaseTypeMySQL
	DatabaseTypeSQLite      = domain.DatabaseTypeSQLite
	DatabaseTypeRedis       = domain.DatabaseTypeRedis
	DatabaseTypeMongoDB     = domain.DatabaseTypeMongoDB
	DatabaseTypeCockroachDB = domain.DatabaseTypeCockroachDB
	DatabaseTypeDynamoDB    = domain.DatabaseTypeDynamoDB
	DatabaseTypeCassandra   = domain.DatabaseTypeCassandra

	// Data types
	DataTypeString    = domain.DataTypeString
	DataTypeInt       = domain.DataTypeInt
	DataTypeInt64     = domain.DataTypeInt64
	DataTypeFloat64   = domain.DataTypeFloat64
	DataTypeBool      = domain.DataTypeBool
	DataTypeTime      = domain.DataTypeTime
	DataTypeBytes     = domain.DataTypeBytes
	DataTypeJSON      = domain.DataTypeJSON
	DataTypeUUID      = domain.DataTypeUUID
	DataTypeDecimal   = domain.DataTypeDecimal
	DataTypeArray     = domain.DataTypeArray
	DataTypeMap       = domain.DataTypeMap
	DataTypeUnknown   = domain.DataTypeUnknown

	// Query types
	QueryTypeSelect = domain.QueryTypeSelect
	QueryTypeInsert = domain.QueryTypeInsert
	QueryTypeUpdate = domain.QueryTypeUpdate
	QueryTypeDelete = domain.QueryTypeDelete
	QueryTypeCount  = domain.QueryTypeCount
	QueryTypeExists = domain.QueryTypeExists

	// Command types
	CommandTypeInsert     = domain.CommandTypeInsert
	CommandTypeUpdate     = domain.CommandTypeUpdate
	CommandTypeDelete     = domain.CommandTypeDelete
	CommandTypeBulkInsert = domain.CommandTypeBulkInsert
	CommandTypeBulkUpdate = domain.CommandTypeBulkUpdate
	CommandTypeBulkDelete = domain.CommandTypeBulkDelete

	// Operation types
	OperationTypeQuery   = domain.OperationTypeQuery
	OperationTypeCommand = domain.OperationTypeCommand

	// Sort directions
	SortDirectionAsc  = domain.SortDirectionAsc
	SortDirectionDesc = domain.SortDirectionDesc

	// Isolation levels
	IsolationLevelDefault         = domain.IsolationLevelDefault
	IsolationLevelReadUncommitted = domain.IsolationLevelReadUncommitted
	IsolationLevelReadCommitted   = domain.IsolationLevelReadCommitted
	IsolationLevelRepeatableRead  = domain.IsolationLevelRepeatableRead
	IsolationLevelSerializable    = domain.IsolationLevelSerializable

	// Join types
	JoinTypeInner = domain.JoinTypeInner
	JoinTypeLeft  = domain.JoinTypeLeft
	JoinTypeRight = domain.JoinTypeRight
	JoinTypeFull  = domain.JoinTypeFull
	JoinTypeCross = domain.JoinTypeCross

	// Operators
	OperatorEqual              = domain.OperatorEqual
	OperatorNotEqual           = domain.OperatorNotEqual
	OperatorGreaterThan        = domain.OperatorGreaterThan
	OperatorGreaterThanOrEqual = domain.OperatorGreaterThanOrEqual
	OperatorLessThan           = domain.OperatorLessThan
	OperatorLessThanOrEqual    = domain.OperatorLessThanOrEqual
	OperatorLike               = domain.OperatorLike
	OperatorNotLike            = domain.OperatorNotLike
	OperatorIn                 = domain.OperatorIn
	OperatorNotIn              = domain.OperatorNotIn
	OperatorIsNull             = domain.OperatorIsNull
	OperatorIsNotNull          = domain.OperatorIsNotNull
	OperatorBetween            = domain.OperatorBetween
	OperatorNotBetween         = domain.OperatorNotBetween

	// Conflict resolution
	ConflictResolutionIgnore  = domain.ConflictResolutionIgnore
	ConflictResolutionReplace = domain.ConflictResolutionReplace
	ConflictResolutionUpdate  = domain.ConflictResolutionUpdate
	ConflictResolutionError   = domain.ConflictResolutionError

	// Schema change types
	SchemaChangeTypeAddColumn    = domain.SchemaChangeTypeAddColumn
	SchemaChangeTypeDropColumn   = domain.SchemaChangeTypeDropColumn
	SchemaChangeTypeModifyColumn = domain.SchemaChangeTypeModifyColumn
	SchemaChangeTypeAddIndex     = domain.SchemaChangeTypeAddIndex
	SchemaChangeTypeDropIndex    = domain.SchemaChangeTypeDropIndex

	// Index types
	IndexTypePrimary = domain.IndexTypePrimary
	IndexTypeUnique  = domain.IndexTypeUnique
	IndexTypeRegular = domain.IndexTypeRegular
	IndexTypePartial = domain.IndexTypePartial

	// Health status types
	HealthStatusHealthy   = domain.HealthStatusHealthy
	HealthStatusDegraded  = domain.HealthStatusDegraded
	HealthStatusUnhealthy = domain.HealthStatusUnhealthy
	HealthStatusUnknown   = domain.HealthStatusUnknown

	// Error types
	ErrorTypeConnection  = domain.ErrorTypeConnection
	ErrorTypeQuery       = domain.ErrorTypeQuery
	ErrorTypeTransaction = domain.ErrorTypeTransaction
	ErrorTypeData        = domain.ErrorTypeData
	ErrorTypeSchema      = domain.ErrorTypeSchema
	ErrorTypeTimeout     = domain.ErrorTypeTimeout
	ErrorTypeValidation  = domain.ErrorTypeValidation
	ErrorTypeUnknown     = domain.ErrorTypeUnknown
)

// Error types
type (
	StorageError     = domain.StorageError
	ConnectionError  = domain.ConnectionError
	QueryError       = domain.QueryError
	TransactionError = domain.TransactionError
	DataError        = domain.DataError
	SchemaError      = domain.SchemaError
	TimeoutError     = domain.TimeoutError
	ValidationError  = domain.ValidationError
)

// Error constructors
var (
	NewStorageError     = domain.NewStorageError
	NewConnectionError  = domain.NewConnectionError
	NewQueryError       = domain.NewQueryError
	NewTransactionError = domain.NewTransactionError
	NewDataError        = domain.NewDataError
	NewSchemaError      = domain.NewSchemaError
	NewTimeoutError     = domain.NewTimeoutError
	NewValidationError  = domain.NewValidationError
)
