package domain

import (
	"errors"
	"testing"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestStorageError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *domain.StorageError
		expected string
	}{
		{
			name: "error without cause",
			err: &domain.StorageError{
				Type:    domain.ErrorTypeConnection,
				Code:    "CONNECTION_FAILED",
				Message: "failed to connect to database",
			},
			expected: "CONNECTION_FAILED: failed to connect to database",
		},
		{
			name: "error with cause",
			err: &domain.StorageError{
				Type:    domain.ErrorTypeQuery,
				Code:    "QUERY_FAILED",
				Message: "domain.query execution failed",
				Cause:   errors.New("syntax error"),
			},
			expected: "QUERY_FAILED: domain.query execution failed (caused by: syntax error)",
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
	err := &domain.StorageError{
		Type:    domain.ErrorTypeConnection,
		Code:    "CONNECTION_FAILED",
		Message: "connection failed",
		Cause:   cause,
	}

	unwrapped := err.Unwrap()
	assert.Equal(t, cause, unwrapped)
}

func TestStorageError_Is(t *testing.T) {
	cause := errors.New("underlying error")
	err := &domain.StorageError{
		Type:    domain.ErrorTypeConnection,
		Code:    "CONNECTION_FAILED",
		Message: "connection failed",
		Cause:   cause,
	}

	// Test matching StorageError
	target := &domain.StorageError{
		Type: domain.ErrorTypeConnection,
		Code: "CONNECTION_FAILED",
	}
	assert.True(t, err.Is(target))

	// Test non-matching StorageError
	nonMatching := &domain.StorageError{
		Type: domain.ErrorTypeQuery,
		Code: "QUERY_FAILED",
	}
	assert.False(t, err.Is(nonMatching))

	// Test matching underlying error
	assert.True(t, err.Is(cause))

	// Test nil target
	assert.False(t, err.Is(nil))
}

func TestStorageError_WithContext(t *testing.T) {
	err := &domain.StorageError{
		Type:    domain.ErrorTypeQuery,
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
	err := &domain.StorageError{
		Type:    domain.ErrorTypeQuery,
		Code:    "QUERY_FAILED",
		Message: "query failed",
	}

	result := err.WithCause(cause)

	assert.Equal(t, cause, result.Cause)
	assert.Same(t, err, result) // Should return the same instance
}

func TestNewStorageError(t *testing.T) {
	err := domain.NewStorageError(domain.ErrorTypeConnection, "CONNECTION_FAILED", "connection failed")

	assert.Equal(t, domain.ErrorTypeConnection, err.Type)
	assert.Equal(t, "CONNECTION_FAILED", err.Code)
	assert.Equal(t, "connection failed", err.Message)
	assert.NotNil(t, err.Context)
	assert.False(t, err.Retryable)
	assert.False(t, err.Temporary)
}

func TestConnectionErrorConstructors(t *testing.T) {
	tests := []struct {
		name      string
		create    func() *domain.StorageError
		code      string
		errType   domain.ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewConnectionError",
			create:  func() *domain.StorageError { return domain.NewConnectionError("CONN_FAILED", "connection failed") },
			code:    "CONN_FAILED",
			errType: domain.ErrorTypeConnection,
		},
		{
			name:      "NewConnectionTimeoutError",
			create:    func() *domain.StorageError { return domain.NewConnectionTimeoutError("timeout occurred") },
			code:      "CONNECTION_TIMEOUT",
			errType:   domain.ErrorTypeTimeout,
			retryable: true,
			temporary: true,
		},
		{
			name:      "NewConnectionPoolFullError",
			create:    func() *domain.StorageError { return domain.NewConnectionPoolFullError() },
			code:      "CONNECTION_POOL_FULL",
			errType:   domain.ErrorTypeResource,
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
		name      string
		create    func() *domain.StorageError
		code      string
		errType   domain.ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewQueryError",
			create:  func() *domain.StorageError { return domain.NewQueryError("QUERY_FAILED", "query failed") },
			code:    "QUERY_FAILED",
			errType: domain.ErrorTypeQuery,
		},
		{
			name:      "NewQueryTimeoutError",
			create:    func() *domain.StorageError { return domain.NewQueryTimeoutError("SELECT * FROM users") },
			code:      "QUERY_TIMEOUT",
			errType:   domain.ErrorTypeTimeout,
			retryable: true,
			temporary: true,
		},
		{
			name:    "NewInvalidQueryError",
			create:  func() *domain.StorageError { return domain.NewInvalidQueryError("invalid syntax") },
			code:    "INVALID_QUERY",
			errType: domain.ErrorTypeValidation,
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
		name      string
		create    func() *domain.StorageError
		code      string
		errType   domain.ErrorType
		retryable bool
		temporary bool
	}{
		{
			name:    "NewTransactionError",
			create:  func() *domain.StorageError { return domain.NewTransactionError("TX_FAILED", "transaction failed") },
			code:    "TX_FAILED",
			errType: domain.ErrorTypeTransaction,
		},
		{
			name:      "NewDeadlockError",
			create:    func() *domain.StorageError { return domain.NewDeadlockError() },
			code:      "DEADLOCK",
			errType:   domain.ErrorTypeTransaction,
			retryable: true,
			temporary: true,
		},
		{
			name:      "NewTransactionTimeoutError",
			create:    func() *domain.StorageError { return domain.NewTransactionTimeoutError() },
			code:      "TRANSACTION_TIMEOUT",
			errType:   domain.ErrorTypeTimeout,
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
		create  func() *domain.StorageError
		code    string
		errType domain.ErrorType
		context map[string]interface{}
	}{
		{
			name:    "NewDataError",
			create:  func() *domain.StorageError { return domain.NewDataError("DATA_FAILED", "data operation failed") },
			code:    "DATA_FAILED",
			errType: domain.ErrorTypeData,
		},
		{
			name:    "NewNoRowsError",
			create:  func() *domain.StorageError { return domain.NewNoRowsError() },
			code:    "NO_ROWS",
			errType: domain.ErrorTypeData,
		},
		{
			name:    "NewConstraintViolationError",
			create:  func() *domain.StorageError { return domain.NewConstraintViolationError("unique_email") },
			code:    "CONSTRAINT_VIOLATION",
			errType: domain.ErrorTypeData,
			context: map[string]interface{}{"constraint": "unique_email"},
		},
		{
			name:    "NewDuplicateKeyError",
			create:  func() *domain.StorageError { return domain.NewDuplicateKeyError("email") },
			code:    "DUPLICATE_KEY",
			errType: domain.ErrorTypeData,
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
		create  func() *domain.StorageError
		code    string
		errType domain.ErrorType
		context map[string]interface{}
	}{
		{
			name:    "NewSchemaError",
			create:  func() *domain.StorageError { return domain.NewSchemaError("SCHEMA_FAILED", "schema operation failed") },
			code:    "SCHEMA_FAILED",
			errType: domain.ErrorTypeSchema,
		},
		{
			name:    "NewTableNotFoundError",
			create:  func() *domain.StorageError { return domain.NewTableNotFoundError("users") },
			code:    "TABLE_NOT_FOUND",
			errType: domain.ErrorTypeSchema,
			context: map[string]interface{}{"table": "users"},
		},
		{
			name:    "NewColumnNotFoundError",
			create:  func() *domain.StorageError { return domain.NewColumnNotFoundError("email") },
			code:    "COLUMN_NOT_FOUND",
			errType: domain.ErrorTypeSchema,
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
			err:      domain.NewConnectionTimeoutError("timeout").WithRetryable(true),
			checker:  domain.IsRetryable,
			expected: true,
		},
		{
			name:     "IsRetryable - non-retryable error",
			err:      domain.NewQueryError("SYNTAX_ERROR", "syntax error"),
			checker:  domain.IsRetryable,
			expected: false,
		},
		{
			name:     "IsTemporary - temporary error",
			err:      domain.NewConnectionTimeoutError("timeout").WithTemporary(true),
			checker:  domain.IsTemporary,
			expected: true,
		},
		{
			name:     "IsTemporary - permanent error",
			err:      domain.NewQueryError("SYNTAX_ERROR", "syntax error"),
			checker:  domain.IsTemporary,
			expected: false,
		},
		{
			name:     "IsConnectionError - connection error",
			err:      domain.NewConnectionError("CONN_FAILED", "connection failed"),
			checker:  domain.IsConnectionError,
			expected: true,
		},
		{
			name:     "IsConnectionError - non-connection error",
			err:      domain.NewQueryError("QUERY_FAILED", "query failed"),
			checker:  domain.IsConnectionError,
			expected: false,
		},
		{
			name:     "IsQueryError - query error",
			err:      domain.NewQueryError("QUERY_FAILED", "query failed"),
			checker:  domain.IsQueryError,
			expected: true,
		},
		{
			name:     "IsQueryError - non-query error",
			err:      domain.NewConnectionError("CONN_FAILED", "connection failed"),
			checker:  domain.IsQueryError,
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
	wrappedErr := domain.WrapError(originalErr, domain.ErrorTypeQuery, "QUERY_FAILED", "query execution failed")

	assert.Equal(t, domain.ErrorTypeQuery, wrappedErr.Type)
	assert.Equal(t, "QUERY_FAILED", wrappedErr.Code)
	assert.Equal(t, "query execution failed", wrappedErr.Message)
	assert.Equal(t, originalErr, wrappedErr.Cause)
	assert.True(t, wrappedErr.Is(originalErr))
}

func TestErrorType_String(t *testing.T) {
	tests := []struct {
		name     string
		errType  domain.ErrorType
		expected string
	}{
		{"Connection", domain.ErrorTypeConnection, "connection"},
		{"Query", domain.ErrorTypeQuery, "query"},
		{"Transaction", domain.ErrorTypeTransaction, "transaction"},
		{"Data", domain.ErrorTypeData, "data"},
		{"Schema", domain.ErrorTypeSchema, "schema"},
		{"Adapter", domain.ErrorTypeAdapter, "adapter"},
		{"Migration", domain.ErrorTypeMigration, "migration"},
		{"Cache", domain.ErrorTypeCache, "cache"},
		{"Validation", domain.ErrorTypeValidation, "validation"},
		{"Timeout", domain.ErrorTypeTimeout, "timeout"},
		{"Permission", domain.ErrorTypePermission, "permission"},
		{"Resource", domain.ErrorTypeResource, "resource"},
		{"Unknown", domain.ErrorTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.errType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}
