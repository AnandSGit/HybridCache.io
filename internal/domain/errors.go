// Package domain provides the core domain errors for the Database Agnostic Storage Library.
package domain

import (
	"errors"
	"fmt"
)

// Error types for the storage package
var (
	// Connection errors
	ErrConnectionFailed   = errors.New("connection failed")
	ErrConnectionTimeout  = errors.New("connection timeout")
	ErrConnectionClosed   = errors.New("connection closed")
	ErrConnectionPoolFull = errors.New("connection pool full")
	ErrInvalidDSN         = errors.New("invalid DSN")
	ErrInvalidConfig      = errors.New("invalid configuration")

	// Query errors
	ErrInvalidQuery      = errors.New("invalid query")
	ErrQueryTimeout      = errors.New("query timeout")
	ErrQueryFailed       = errors.New("query failed")
	ErrInvalidParameters = errors.New("invalid parameters")
	ErrUnsupportedQuery  = errors.New("unsupported query")

	// Transaction errors
	ErrTransactionFailed  = errors.New("transaction failed")
	ErrTransactionTimeout = errors.New("transaction timeout")
	ErrTransactionClosed  = errors.New("transaction closed")
	ErrInvalidTransaction = errors.New("invalid transaction")
	ErrDeadlock           = errors.New("deadlock detected")

	// Data errors
	ErrNoRows              = errors.New("no rows found")
	ErrTooManyRows         = errors.New("too many rows")
	ErrInvalidData         = errors.New("invalid data")
	ErrDataTruncated       = errors.New("data truncated")
	ErrConstraintViolation = errors.New("constraint violation")
	ErrDuplicateKey        = errors.New("duplicate key")

	// Schema errors
	ErrTableNotFound  = errors.New("table not found")
	ErrColumnNotFound = errors.New("column not found")
	ErrIndexNotFound  = errors.New("index not found")
	ErrInvalidSchema  = errors.New("invalid schema")
	ErrSchemaExists   = errors.New("schema already exists")

	// Adapter errors
	ErrAdapterNotFound    = errors.New("adapter not found")
	ErrUnsupportedAdapter = errors.New("unsupported adapter")
	ErrAdapterFailed      = errors.New("adapter failed")

	// Migration errors
	ErrMigrationFailed   = errors.New("migration failed")
	ErrMigrationNotFound = errors.New("migration not found")
	ErrInvalidMigration  = errors.New("invalid migration")

	// Cache errors
	ErrCacheNotFound = errors.New("cache entry not found")
	ErrCacheFailed   = errors.New("cache operation failed")
	ErrCacheTimeout  = errors.New("cache timeout")

	// Generic errors
	ErrNotImplemented       = errors.New("not implemented")
	ErrUnsupportedOperation = errors.New("unsupported operation")
	ErrInternalError        = errors.New("internal error")
)

// ErrorType represents the category of error
type ErrorType int

const (
	ErrorTypeUnknown ErrorType = iota
	ErrorTypeConnection
	ErrorTypeQuery
	ErrorTypeTransaction
	ErrorTypeData
	ErrorTypeSchema
	ErrorTypeAdapter
	ErrorTypeMigration
	ErrorTypeCache
	ErrorTypeValidation
	ErrorTypeTimeout
	ErrorTypePermission
	ErrorTypeResource
)

func (et ErrorType) String() string {
	switch et {
	case ErrorTypeConnection:
		return "connection"
	case ErrorTypeQuery:
		return "query"
	case ErrorTypeTransaction:
		return "transaction"
	case ErrorTypeData:
		return "data"
	case ErrorTypeSchema:
		return "schema"
	case ErrorTypeAdapter:
		return "adapter"
	case ErrorTypeMigration:
		return "migration"
	case ErrorTypeCache:
		return "cache"
	case ErrorTypeValidation:
		return "validation"
	case ErrorTypeTimeout:
		return "timeout"
	case ErrorTypePermission:
		return "permission"
	case ErrorTypeResource:
		return "resource"
	default:
		return "unknown"
	}
}

// StorageError represents a structured storage error
type StorageError struct {
	Type      ErrorType              `json:"type"`
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Cause     error                  `json:"-"`
	Context   map[string]interface{} `json:"context,omitempty"`
	Retryable bool                   `json:"retryable"`
	Temporary bool                   `json:"temporary"`
}

// Error implements the error interface
func (e *StorageError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *StorageError) Unwrap() error {
	return e.Cause
}

// Is checks if the error matches the target error
func (e *StorageError) Is(target error) bool {
	if target == nil {
		return false
	}

	if se, ok := target.(*StorageError); ok {
		return e.Type == se.Type && e.Code == se.Code
	}

	return errors.Is(e.Cause, target)
}

// WithContext adds context to the error
func (e *StorageError) WithContext(key string, value interface{}) *StorageError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithCause sets the underlying cause
func (e *StorageError) WithCause(cause error) *StorageError {
	e.Cause = cause
	return e
}

// NewStorageError creates a new storage error
func NewStorageError(errorType ErrorType, code, message string) *StorageError {
	return &StorageError{
		Type:    errorType,
		Code:    code,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// Connection error constructors
func NewConnectionError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeConnection, code, message)
}

func NewConnectionTimeoutError(message string) *StorageError {
	return NewStorageError(ErrorTypeTimeout, "CONNECTION_TIMEOUT", message).
		WithRetryable(true).
		WithTemporary(true)
}

func NewConnectionPoolFullError() *StorageError {
	return NewStorageError(ErrorTypeResource, "CONNECTION_POOL_FULL", "connection pool is full").
		WithRetryable(true).
		WithTemporary(true)
}

// Query error constructors
func NewQueryError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeQuery, code, message)
}

func NewQueryTimeoutError(query string) *StorageError {
	return NewStorageError(ErrorTypeTimeout, "QUERY_TIMEOUT", "query execution timeout").
		WithContext("query", query).
		WithRetryable(true).
		WithTemporary(true)
}

func NewInvalidQueryError(message string) *StorageError {
	return NewStorageError(ErrorTypeValidation, "INVALID_QUERY", message)
}

// Transaction error constructors
func NewTransactionError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeTransaction, code, message)
}

func NewDeadlockError() *StorageError {
	return NewStorageError(ErrorTypeTransaction, "DEADLOCK", "deadlock detected").
		WithRetryable(true).
		WithTemporary(true)
}

func NewTransactionTimeoutError() *StorageError {
	return NewStorageError(ErrorTypeTimeout, "TRANSACTION_TIMEOUT", "transaction timeout").
		WithRetryable(false).
		WithTemporary(true)
}

// Data error constructors
func NewDataError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeData, code, message)
}

func NewNoRowsError() *StorageError {
	return NewStorageError(ErrorTypeData, "NO_ROWS", "no rows found")
}

func NewConstraintViolationError(constraint string) *StorageError {
	return NewStorageError(ErrorTypeData, "CONSTRAINT_VIOLATION", "constraint violation").
		WithContext("constraint", constraint)
}

func NewDuplicateKeyError(key string) *StorageError {
	return NewStorageError(ErrorTypeData, "DUPLICATE_KEY", "duplicate key violation").
		WithContext("key", key)
}

// Schema error constructors
func NewSchemaError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeSchema, code, message)
}

func NewTableNotFoundError(table string) *StorageError {
	return NewStorageError(ErrorTypeSchema, "TABLE_NOT_FOUND", "table not found").
		WithContext("table", table)
}

func NewColumnNotFoundError(column string) *StorageError {
	return NewStorageError(ErrorTypeSchema, "COLUMN_NOT_FOUND", "column not found").
		WithContext("column", column)
}

// Adapter error constructors
func NewAdapterError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeAdapter, code, message)
}

func NewUnsupportedAdapterError(adapter string) *StorageError {
	return NewStorageError(ErrorTypeAdapter, "UNSUPPORTED_ADAPTER", "unsupported adapter").
		WithContext("adapter", adapter)
}

// Migration error constructors
func NewMigrationError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeMigration, code, message)
}

func NewMigrationFailedError(migration string, cause error) *StorageError {
	return NewStorageError(ErrorTypeMigration, "MIGRATION_FAILED", "migration failed").
		WithContext("migration", migration).
		WithCause(cause)
}

// Cache error constructors
func NewCacheError(code, message string) *StorageError {
	return NewStorageError(ErrorTypeCache, code, message)
}

func NewCacheNotFoundError(key string) *StorageError {
	return NewStorageError(ErrorTypeCache, "CACHE_NOT_FOUND", "cache entry not found").
		WithContext("key", key)
}

// Helper methods for StorageError
func (e *StorageError) WithRetryable(retryable bool) *StorageError {
	e.Retryable = retryable
	return e
}

func (e *StorageError) WithTemporary(temporary bool) *StorageError {
	e.Temporary = temporary
	return e
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Retryable
	}
	return false
}

// IsTemporary checks if an error is temporary
func IsTemporary(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Temporary
	}
	return false
}

// IsConnectionError checks if an error is a connection error
func IsConnectionError(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Type == ErrorTypeConnection
	}
	return false
}

// IsQueryError checks if an error is a query error
func IsQueryError(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Type == ErrorTypeQuery
	}
	return false
}

// IsTransactionError checks if an error is a transaction error
func IsTransactionError(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Type == ErrorTypeTransaction
	}
	return false
}

// IsDataError checks if an error is a data error
func IsDataError(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Type == ErrorTypeData
	}
	return false
}

// IsTimeoutError checks if an error is a timeout error
func IsTimeoutError(err error) bool {
	if se, ok := err.(*StorageError); ok {
		return se.Type == ErrorTypeTimeout
	}
	return false
}

// WrapError wraps an existing error as a StorageError
func WrapError(err error, errorType ErrorType, code, message string) *StorageError {
	return NewStorageError(errorType, code, message).WithCause(err)
}
