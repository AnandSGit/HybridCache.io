// Package storage provides adapter functionality for the Database Agnostic Storage Library.
package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/postgres"
)

// Adapter factory functions

// NewPostgreSQLAdapter creates a new PostgreSQL adapter
func NewPostgreSQLAdapter() Adapter {
	return postgres.NewAdapter()
}
