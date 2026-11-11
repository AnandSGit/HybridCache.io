// Package storage is the public API of HybridCache.io, a database-agnostic
// storage library. It is the package applications import: it re-exports the
// library's interfaces, types, constants and errors, and provides the query
// builder and the adapter factories.
//
// Connect through an adapter, then build queries and commands with NewBuilder,
// Insert, Update and Delete and run them on the returned Storage:
//
//	adapter := storage.NewPostgreSQLAdapter() // or storage.NewMongoDBAdapter()
//	config, err := adapter.ParseDSN(os.Getenv("POSTGRES_DSN"))
//	if err != nil {
//		return err
//	}
//	store, err := adapter.Connect(ctx, config)
//	if err != nil {
//		return err
//	}
//	defer store.Close()
//
//	query, err := storage.NewBuilder().
//		Select("id", "name").
//		From("users").
//		Where(storage.Equal("active", true)).
//		OrderByAsc("name").
//		Page(1, 20).
//		Build()
//	if err != nil {
//		return err
//	}
//	rows, err := store.Query(ctx, query)
//
// Builders produce both SQL (with '?' placeholders) and a structured Spec, so
// the same query runs on SQL and document databases. An adapter reports what
// it cannot run, such as a join on MongoDB, with an error matching
// ErrUnsupportedQuery.
//
// Errors are *StorageError values that match the sentinel errors (ErrNoRows,
// ErrDuplicateKey, ...) with errors.Is. Depending on the adapter, a query
// without rows is reported by QueryOne itself or by the returned Row, so check
// both:
//
//	row, err := store.QueryOne(ctx, query)
//	if err == nil {
//		err = row.Scan(&id, &name)
//	}
//	if errors.Is(err, storage.ErrNoRows) {
//		// not found
//	}
package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// Core interfaces
type (
	Storage      = domain.Storage
	Adapter      = domain.Adapter
	QueryBuilder = domain.QueryBuilder
	Transaction  = domain.Transaction
	Result       = domain.Result
	Row          = domain.Row
)

// Queries, commands and their results
type (
	Query           = domain.Query
	QueryType       = domain.QueryType
	QueryOptions    = domain.QueryOptions
	QuerySpec       = domain.QuerySpec
	OrderBy         = domain.OrderBy
	Join            = domain.Join
	Command         = domain.Command
	CommandType     = domain.CommandType
	CommandOptions  = domain.CommandOptions
	CommandSpec     = domain.CommandSpec
	FieldValue      = domain.FieldValue
	Condition       = domain.Condition
	ColumnRef       = domain.ColumnRef
	Operator        = domain.Operator
	JoinType        = domain.JoinType
	SortDirection   = domain.SortDirection
	ExecuteResult   = domain.ExecuteResult
	Operation       = domain.Operation
	OperationType   = domain.OperationType
	OperationResult = domain.OperationResult
)

// Configuration, transactions and metadata
type (
	DatabaseType     = domain.DatabaseType
	DataType         = domain.DataType
	Config           = domain.Config
	TxOptions        = domain.TxOptions
	IsolationLevel   = domain.IsolationLevel
	StorageInfo      = domain.StorageInfo
	StorageLimits    = domain.StorageLimits
	HealthStatus     = domain.HealthStatus
	HealthStatusType = domain.HealthStatusType
	ColumnType       = domain.ColumnType
)

// Schema
type (
	TableSchema          = domain.TableSchema
	ColumnDefinition     = domain.ColumnDefinition
	IndexDefinition      = domain.IndexDefinition
	IndexType            = domain.IndexType
	ConstraintDefinition = domain.ConstraintDefinition
	ConstraintType       = domain.ConstraintType
	ReferentialAction    = domain.ReferentialAction
	SchemaChange         = domain.SchemaChange
	SchemaChangeType     = domain.SchemaChangeType
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
	DataTypeUnknown  = domain.DataTypeUnknown
	DataTypeString   = domain.DataTypeString
	DataTypeInteger  = domain.DataTypeInteger
	DataTypeFloat    = domain.DataTypeFloat
	DataTypeBoolean  = domain.DataTypeBoolean
	DataTypeDateTime = domain.DataTypeDateTime
	DataTypeDate     = domain.DataTypeDate
	DataTypeTime     = domain.DataTypeTime
	DataTypeBinary   = domain.DataTypeBinary
	DataTypeJSON     = domain.DataTypeJSON
	DataTypeUUID     = domain.DataTypeUUID
	DataTypeArray    = domain.DataTypeArray
	DataTypeMap      = domain.DataTypeMap
	DataTypeDecimal  = domain.DataTypeDecimal
	DataTypeText     = domain.DataTypeText

	// Query types
	QueryTypeSelect = domain.QueryTypeSelect
	QueryTypeInsert = domain.QueryTypeInsert
	QueryTypeUpdate = domain.QueryTypeUpdate
	QueryTypeDelete = domain.QueryTypeDelete
	QueryTypeCreate = domain.QueryTypeCreate
	QueryTypeDrop   = domain.QueryTypeDrop
	QueryTypeAlter  = domain.QueryTypeAlter
	QueryTypeCustom = domain.QueryTypeCustom

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
	OperatorBetween            = domain.OperatorBetween
	OperatorNotBetween         = domain.OperatorNotBetween
	OperatorIsNull             = domain.OperatorIsNull
	OperatorIsNotNull          = domain.OperatorIsNotNull
	OperatorExists             = domain.OperatorExists
	OperatorNotExists          = domain.OperatorNotExists
	OperatorAnd                = domain.OperatorAnd
	OperatorOr                 = domain.OperatorOr

	// Join types
	JoinTypeInner = domain.JoinTypeInner
	JoinTypeLeft  = domain.JoinTypeLeft
	JoinTypeRight = domain.JoinTypeRight
	JoinTypeFull  = domain.JoinTypeFull
	JoinTypeCross = domain.JoinTypeCross

	// Sort directions
	SortDirectionAsc  = domain.SortDirectionAsc
	SortDirectionDesc = domain.SortDirectionDesc

	// Isolation levels
	IsolationLevelDefault         = domain.IsolationLevelDefault
	IsolationLevelReadUncommitted = domain.IsolationLevelReadUncommitted
	IsolationLevelReadCommitted   = domain.IsolationLevelReadCommitted
	IsolationLevelRepeatableRead  = domain.IsolationLevelRepeatableRead
	IsolationLevelSerializable    = domain.IsolationLevelSerializable

	// Health status types
	HealthStatusHealthy   = domain.HealthStatusHealthy
	HealthStatusDegraded  = domain.HealthStatusDegraded
	HealthStatusUnhealthy = domain.HealthStatusUnhealthy
	HealthStatusUnknown   = domain.HealthStatusUnknown

	// Index types
	IndexTypeBTree = domain.IndexTypeBTree
	IndexTypeHash  = domain.IndexTypeHash
	IndexTypeGIN   = domain.IndexTypeGIN
	IndexTypeGiST  = domain.IndexTypeGiST

	// Constraint types
	ConstraintTypePrimaryKey = domain.ConstraintTypePrimaryKey
	ConstraintTypeForeignKey = domain.ConstraintTypeForeignKey
	ConstraintTypeUnique     = domain.ConstraintTypeUnique
	ConstraintTypeCheck      = domain.ConstraintTypeCheck

	// Referential actions
	ReferentialActionNoAction   = domain.ReferentialActionNoAction
	ReferentialActionRestrict   = domain.ReferentialActionRestrict
	ReferentialActionCascade    = domain.ReferentialActionCascade
	ReferentialActionSetNull    = domain.ReferentialActionSetNull
	ReferentialActionSetDefault = domain.ReferentialActionSetDefault

	// Schema change types
	SchemaChangeTypeAddColumn      = domain.SchemaChangeTypeAddColumn
	SchemaChangeTypeDropColumn     = domain.SchemaChangeTypeDropColumn
	SchemaChangeTypeModifyColumn   = domain.SchemaChangeTypeModifyColumn
	SchemaChangeTypeAddIndex       = domain.SchemaChangeTypeAddIndex
	SchemaChangeTypeDropIndex      = domain.SchemaChangeTypeDropIndex
	SchemaChangeTypeAddConstraint  = domain.SchemaChangeTypeAddConstraint
	SchemaChangeTypeDropConstraint = domain.SchemaChangeTypeDropConstraint
)
