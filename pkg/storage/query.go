// Package storage provides query building functionality for the Database Agnostic Storage Library.
package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/application/query"
)

// Query builder functions - re-export from internal packages

// NewBuilder creates a new query builder
func NewBuilder() QueryBuilder {
	return query.NewBuilder()
}

// Insert creates an INSERT command builder
func Insert(table string) *query.CommandBuilder {
	return query.Insert(table)
}

// Update creates an UPDATE command builder
func Update(table string) *query.CommandBuilder {
	return query.Update(table)
}

// Delete creates a DELETE command builder
func Delete(table string) *query.CommandBuilder {
	return query.Delete(table)
}

// NewCommandBuilder creates a new command builder
func NewCommandBuilder() *query.CommandBuilder {
	return query.NewCommandBuilder()
}

// CommandBuilder is re-exported from the internal query package
type CommandBuilder = query.CommandBuilder

// Condition helper functions - re-export from internal packages

// Equal creates an equality condition
func Equal(field string, value interface{}) Condition {
	return query.Equal(field, value)
}

// NotEqual creates a not-equal condition
func NotEqual(field string, value interface{}) Condition {
	return query.NotEqual(field, value)
}

// GreaterThan creates a greater-than condition
func GreaterThan(field string, value interface{}) Condition {
	return query.GreaterThan(field, value)
}

// LessThan creates a less-than condition
func LessThan(field string, value interface{}) Condition {
	return query.LessThan(field, value)
}

// In creates an IN condition
func In(field string, values ...interface{}) Condition {
	return query.In(field, values...)
}

// Like creates a LIKE condition
func Like(field string, pattern string) Condition {
	return query.Like(field, pattern)
}

// IsNull creates an IS NULL condition
func IsNull(field string) Condition {
	return query.IsNull(field)
}

// IsNotNull creates an IS NOT NULL condition
func IsNotNull(field string) Condition {
	return query.IsNotNull(field)
}
