package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// StorageError is the structured error returned by adapters and builders. It
// matches the sentinel error for its Code with errors.Is, as well as its Cause.
type StorageError = domain.StorageError

// ErrorType is the category of a StorageError.
type ErrorType = domain.ErrorType

// Error types
const (
	ErrorTypeUnknown     = domain.ErrorTypeUnknown
	ErrorTypeConnection  = domain.ErrorTypeConnection
	ErrorTypeQuery       = domain.ErrorTypeQuery
	ErrorTypeTransaction = domain.ErrorTypeTransaction
	ErrorTypeData        = domain.ErrorTypeData
	ErrorTypeSchema      = domain.ErrorTypeSchema
	ErrorTypeAdapter     = domain.ErrorTypeAdapter
	ErrorTypeValidation  = domain.ErrorTypeValidation
	ErrorTypeTimeout     = domain.ErrorTypeTimeout
	ErrorTypePermission  = domain.ErrorTypePermission
	ErrorTypeResource    = domain.ErrorTypeResource
)

// Sentinel errors, for use with errors.Is: a StorageError matches the sentinel
// associated with its Code (for example, errors.Is(err, ErrNoRows) for code
// NO_ROWS).
var (
	// Connection errors
	ErrConnectionFailed   = domain.ErrConnectionFailed
	ErrConnectionTimeout  = domain.ErrConnectionTimeout
	ErrConnectionClosed   = domain.ErrConnectionClosed
	ErrConnectionPoolFull = domain.ErrConnectionPoolFull
	ErrInvalidDSN         = domain.ErrInvalidDSN
	ErrInvalidConfig      = domain.ErrInvalidConfig

	// Query errors
	ErrInvalidQuery      = domain.ErrInvalidQuery
	ErrQueryTimeout      = domain.ErrQueryTimeout
	ErrQueryFailed       = domain.ErrQueryFailed
	ErrInvalidParameters = domain.ErrInvalidParameters
	ErrUnsupportedQuery  = domain.ErrUnsupportedQuery

	// Transaction errors
	ErrTransactionFailed  = domain.ErrTransactionFailed
	ErrTransactionTimeout = domain.ErrTransactionTimeout
	ErrTransactionClosed  = domain.ErrTransactionClosed
	ErrInvalidTransaction = domain.ErrInvalidTransaction
	ErrDeadlock           = domain.ErrDeadlock

	// Data errors
	ErrNoRows              = domain.ErrNoRows
	ErrTooManyRows         = domain.ErrTooManyRows
	ErrInvalidData         = domain.ErrInvalidData
	ErrDataTruncated       = domain.ErrDataTruncated
	ErrConstraintViolation = domain.ErrConstraintViolation
	ErrDuplicateKey        = domain.ErrDuplicateKey

	// Schema errors
	ErrTableNotFound  = domain.ErrTableNotFound
	ErrColumnNotFound = domain.ErrColumnNotFound
	ErrIndexNotFound  = domain.ErrIndexNotFound
	ErrInvalidSchema  = domain.ErrInvalidSchema
	ErrSchemaExists   = domain.ErrSchemaExists

	// Adapter errors
	ErrAdapterNotFound    = domain.ErrAdapterNotFound
	ErrUnsupportedAdapter = domain.ErrUnsupportedAdapter
	ErrAdapterFailed      = domain.ErrAdapterFailed

	// Generic errors
	ErrNotImplemented       = domain.ErrNotImplemented
	ErrUnsupportedOperation = domain.ErrUnsupportedOperation
	ErrInternalError        = domain.ErrInternalError
)

// NewStorageError creates a StorageError. Use one of the codes of the sentinel
// errors (such as "NO_ROWS") for errors.Is to match it.
func NewStorageError(errorType ErrorType, code, message string) *StorageError {
	return domain.NewStorageError(errorType, code, message)
}

// WrapError wraps err as the cause of a new StorageError.
func WrapError(err error, errorType ErrorType, code, message string) *StorageError {
	return domain.WrapError(err, errorType, code, message)
}

// IsRetryable reports whether err (or an error it wraps) is a retryable StorageError.
func IsRetryable(err error) bool {
	return domain.IsRetryable(err)
}

// IsTemporary reports whether err (or an error it wraps) is a temporary StorageError.
func IsTemporary(err error) bool {
	return domain.IsTemporary(err)
}

// IsConnectionError reports whether err is a StorageError of type ErrorTypeConnection.
func IsConnectionError(err error) bool {
	return domain.IsConnectionError(err)
}

// IsQueryError reports whether err is a StorageError of type ErrorTypeQuery.
func IsQueryError(err error) bool {
	return domain.IsQueryError(err)
}

// IsTransactionError reports whether err is a StorageError of type ErrorTypeTransaction.
func IsTransactionError(err error) bool {
	return domain.IsTransactionError(err)
}

// IsDataError reports whether err is a StorageError of type ErrorTypeData.
func IsDataError(err error) bool {
	return domain.IsDataError(err)
}

// IsTimeoutError reports whether err is a StorageError of type ErrorTypeTimeout.
func IsTimeoutError(err error) bool {
	return domain.IsTimeoutError(err)
}
