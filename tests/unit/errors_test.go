package storage

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStorageError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *StorageError
		expected string
	}{
		{
			name: "error without cause",
			err: &StorageError{
				Type:    ErrorTypeConnection,
				Code:    "CONNECTION_FAILED",
				Message: "failed to connect to database",
			},
			expected: "CONNECTION_FAILED: failed to connect to database",
		},
		{
			name: "error with cause",
			err: &StorageError{
				Type:    ErrorTypeQuery,
				Code:    "QUERY_FAILED",
				Message: "query execution failed",
				Cause:   errors.New("syntax error"),
			},
			expected: "QUERY_FAILED: query execution failed (caused by: syntax error)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStorageError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &StorageError{
		Type:    ErrorTypeConnection,
		Code:    "CONNECTION_FAILED",
		Message: "connection failed",
		Cause:   cause,
	}

	unwrapped := err.Unwrap()
	assert.Equal(t, cause, unwrapped)
}

func TestStorageError_Is(t *testing.T) {
	cause := errors.New("underlying error")
	err := &StorageError{
		Type:    ErrorTypeConnection,
		Code:    "CONNECTION_FAILED",
		Message: "connection failed",
		Cause:   cause,
	}

	// Test matching StorageError
	target := &StorageError{
		Type: ErrorTypeConnection,
		Code: "CONNECTION_FAILED",
	}
	assert.True(t, err.Is(target))

	// Test non-matching StorageError
	nonMatching := &StorageError{
		Type: ErrorTypeQuery,
		Code: "QUERY_FAILED",
	}
	assert.False(t, err.Is(nonMatching))

	// Test matching underlying error
	assert.True(t, err.Is(cause))

	// Test nil target
	assert.False(t, err.Is(nil))
}

func TestStorageError_WithContext(t *testing.T) {
	err := &StorageError{
		Type:    ErrorTypeQuery,
		Code:    "QUERY_FAILED",
		Message: "query failed",
		Context: make(map[string]interface{}),
	}

	result := err.WithContext("table", "users").WithContext("operation", "SELECT")

	assert.Equal(t, "users", result.Context["table"])
	assert.Equal(t, "SELECT", result.Context["operation"])
	assert.Same(t, err, result) // Should return the same instance
}

func TestStorageError_WithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := &StorageError{
		Type:    ErrorTypeQuery,
		Code:    "QUERY_FAILED",
		Message: "query failed",
	}

	result := err.WithCause(cause)

	assert.Equal(t, cause, result.Cause)
	assert.Same(t, err, result) // Should return the same instance
}

func TestNewStorageError(t *testing.T) {
	err := NewStorageError(ErrorTypeConnection, "CONNECTION_FAILED", "connection failed")

	assert.Equal(t, ErrorTypeConnection, err.Type)
	assert.Equal(t, "CONNECTION_FAILED", err.Code)
	assert.Equal(t, "connection failed", err.Message)
	assert.NotNil(t, err.Context)
	assert.False(t, err.Retryable)
	assert.False(t, err.Temporary)
}

func TestConnectionErrorConstructors(t *testing.T) {
	tests := []struct {
		name     string
		create   func() *StorageError
		code     string
		errType  ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewConnectionError",
			create:  func() *StorageError { return NewConnectionError("CONN_FAILED", "connection failed") },
			code:    "CONN_FAILED",
			errType: ErrorTypeConnection,
		},
		{
			name:      "NewConnectionTimeoutError",
			create:    func() *StorageError { return NewConnectionTimeoutError("timeout occurred") },
			code:      "CONNECTION_TIMEOUT",
			errType:   ErrorTypeTimeout,
			retryable: true,
			temporary: true,
		},
		{
			name:      "NewConnectionPoolFullError",
			create:    func() *StorageError { return NewConnectionPoolFullError() },
			code:      "CONNECTION_POOL_FULL",
			errType:   ErrorTypeResource,
			retryable: true,
			temporary: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.errType, err.Type)
			assert.Equal(t, tt.retryable, err.Retryable)
			assert.Equal(t, tt.temporary, err.Temporary)
		})
	}
}

func TestQueryErrorConstructors(t *testing.T) {
	tests := []struct {
		name     string
		create   func() *StorageError
		code     string
		errType  ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewQueryError",
			create:  func() *StorageError { return NewQueryError("QUERY_FAILED", "query failed") },
			code:    "QUERY_FAILED",
			errType: ErrorTypeQuery,
		},
		{
			name:      "NewQueryTimeoutError",
			create:    func() *StorageError { return NewQueryTimeoutError("SELECT * FROM users") },
			code:      "QUERY_TIMEOUT",
			errType:   ErrorTypeTimeout,
			retryable: true,
			temporary: true,
		},
		{
			name:    "NewInvalidQueryError",
			create:  func() *StorageError { return NewInvalidQueryError("invalid syntax") },
			code:    "INVALID_QUERY",
			errType: ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.errType, err.Type)
			assert.Equal(t, tt.retryable, err.Retryable)
			assert.Equal(t, tt.temporary, err.Temporary)
		})
	}
}

func TestTransactionErrorConstructors(t *testing.T) {
	tests := []struct {
		name     string
		create   func() *StorageError
		code     string
		errType  ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewTransactionError",
			create:  func() *StorageError { return NewTransactionError("TX_FAILED", "transaction failed") },
			code:    "TX_FAILED",
			errType: ErrorTypeTransaction,
		},
		{
			name:      "NewDeadlockError",
			create:    func() *StorageError { return NewDeadlockError() },
			code:      "DEADLOCK",
			errType:   ErrorTypeTransaction,
			retryable: true,
			temporary: true,
		},
		{
			name:      "NewTransactionTimeoutError",
			create:    func() *StorageError { return NewTransactionTimeoutError() },
			code:      "TRANSACTION_TIMEOUT",
			errType:   ErrorTypeTimeout,
			retryable: false,
			temporary: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.errType, err.Type)
			assert.Equal(t, tt.retryable, err.Retryable)
			assert.Equal(t, tt.temporary, err.Temporary)
		})
	}
}

func TestDataErrorConstructors(t *testing.T) {
	tests := []struct {
		name    string
		create  func() *StorageError
		code    string
		errType ErrorType
		context map[string]interface{}
	}{
		{
			name:    "NewDataError",
			create:  func() *StorageError { return NewDataError("DATA_FAILED", "data operation failed") },
			code:    "DATA_FAILED",
			errType: ErrorTypeData,
		},
		{
			name:    "NewNoRowsError",
			create:  func() *StorageError { return NewNoRowsError() },
			code:    "NO_ROWS",
			errType: ErrorTypeData,
		},
		{
			name:    "NewConstraintViolationError",
			create:  func() *StorageError { return NewConstraintViolationError("unique_email") },
			code:    "CONSTRAINT_VIOLATION",
			errType: ErrorTypeData,
			context: map[string]interface{}{"constraint": "unique_email"},
		},
		{
			name:    "NewDuplicateKeyError",
			create:  func() *StorageError { return NewDuplicateKeyError("email") },
			code:    "DUPLICATE_KEY",
			errType: ErrorTypeData,
			context: map[string]interface{}{"key": "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.errType, err.Type)
			if tt.context != nil {
				for key, value := range tt.context {
					assert.Equal(t, value, err.Context[key])
				}
			}
		})
	}
}

func TestSchemaErrorConstructors(t *testing.T) {
	tests := []struct {
		name    string
		create  func() *StorageError
		code    string
		errType ErrorType
		context map[string]interface{}
	}{
		{
			name:    "NewSchemaError",
			create:  func() *StorageError { return NewSchemaError("SCHEMA_FAILED", "schema operation failed") },
			code:    "SCHEMA_FAILED",
			errType: ErrorTypeSchema,
		},
		{
			name:    "NewTableNotFoundError",
			create:  func() *StorageError { return NewTableNotFoundError("users") },
			code:    "TABLE_NOT_FOUND",
			errType: ErrorTypeSchema,
			context: map[string]interface{}{"table": "users"},
		},
		{
			name:    "NewColumnNotFoundError",
			create:  func() *StorageError { return NewColumnNotFoundError("email") },
			code:    "COLUMN_NOT_FOUND",
			errType: ErrorTypeSchema,
			context: map[string]interface{}{"column": "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.create()
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.errType, err.Type)
			if tt.context != nil {
				for key, value := range tt.context {
					assert.Equal(t, value, err.Context[key])
				}
			}
		})
	}
}

func TestErrorTypeHelpers(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		checker  func(error) bool
		expected bool
	}{
		{
			name:     "IsRetryable - retryable error",
			err:      NewConnectionTimeoutError("timeout").WithRetryable(true),
			checker:  IsRetryable,
			expected: true,
		},
		{
			name:     "IsRetryable - non-retryable error",
			err:      NewQueryError("SYNTAX_ERROR", "syntax error"),
			checker:  IsRetryable,
			expected: false,
		},
		{
			name:     "IsTemporary - temporary error",
			err:      NewConnectionTimeoutError("timeout").WithTemporary(true),
			checker:  IsTemporary,
			expected: true,
		},
		{
			name:     "IsTemporary - permanent error",
			err:      NewQueryError("SYNTAX_ERROR", "syntax error"),
			checker:  IsTemporary,
			expected: false,
		},
		{
			name:     "IsConnectionError - connection error",
			err:      NewConnectionError("CONN_FAILED", "connection failed"),
			checker:  IsConnectionError,
			expected: true,
		},
		{
			name:     "IsConnectionError - non-connection error",
			err:      NewQueryError("QUERY_FAILED", "query failed"),
			checker:  IsConnectionError,
			expected: false,
		},
		{
			name:     "IsQueryError - query error",
			err:      NewQueryError("QUERY_FAILED", "query failed"),
			checker:  IsQueryError,
			expected: true,
		},
		{
			name:     "IsQueryError - non-query error",
			err:      NewConnectionError("CONN_FAILED", "connection failed"),
			checker:  IsQueryError,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.checker(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWrapError(t *testing.T) {
	originalErr := errors.New("original error")
	wrappedErr := WrapError(originalErr, ErrorTypeQuery, "QUERY_FAILED", "query execution failed")

	assert.Equal(t, ErrorTypeQuery, wrappedErr.Type)
	assert.Equal(t, "QUERY_FAILED", wrappedErr.Code)
	assert.Equal(t, "query execution failed", wrappedErr.Message)
	assert.Equal(t, originalErr, wrappedErr.Cause)
	assert.True(t, wrappedErr.Is(originalErr))
}

func TestErrorType_String(t *testing.T) {
	tests := []struct {
		name     string
		errType  ErrorType
		expected string
	}{
		{"Connection", ErrorTypeConnection, "connection"},
		{"Query", ErrorTypeQuery, "query"},
		{"Transaction", ErrorTypeTransaction, "transaction"},
		{"Data", ErrorTypeData, "data"},
		{"Schema", ErrorTypeSchema, "schema"},
		{"Adapter", ErrorTypeAdapter, "adapter"},
		{"Migration", ErrorTypeMigration, "migration"},
		{"Cache", ErrorTypeCache, "cache"},
		{"Validation", ErrorTypeValidation, "validation"},
		{"Timeout", ErrorTypeTimeout, "timeout"},
		{"Permission", ErrorTypePermission, "permission"},
		{"Resource", ErrorTypeResource, "resource"},
		{"Unknown", ErrorTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.errType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}
