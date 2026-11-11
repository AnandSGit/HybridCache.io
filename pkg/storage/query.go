package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/application/query"
)

// NewBuilder creates a SELECT query builder. Build returns a Query holding both
// the SQL (with '?' placeholders) and the structured Spec, so the query runs on
// any adapter. Invalid input (such as an identifier that is not a plain column
// or table name) makes Build return an error matching ErrInvalidQuery.
func NewBuilder() QueryBuilder {
	return query.NewBuilder()
}

// CommandBuilder builds INSERT, UPDATE and DELETE commands. Create one with
// Insert, Update or Delete.
type CommandBuilder = query.CommandBuilder

// Insert creates an INSERT command builder
func Insert(table string) *CommandBuilder {
	return query.Insert(table)
}

// Update creates an UPDATE command builder
func Update(table string) *CommandBuilder {
	return query.Update(table)
}

// Delete creates a DELETE command builder
func Delete(table string) *CommandBuilder {
	return query.Delete(table)
}

// Condition helpers. Values are bound as parameters; wrap a value with Col to
// compare against a column instead (for example in a JOIN condition).

// Equal creates a "field = value" condition
func Equal(field string, value interface{}) Condition {
	return query.Equal(field, value)
}

// NotEqual creates a "field != value" condition
func NotEqual(field string, value interface{}) Condition {
	return query.NotEqual(field, value)
}

// GreaterThan creates a "field > value" condition
func GreaterThan(field string, value interface{}) Condition {
	return query.GreaterThan(field, value)
}

// GreaterThanOrEqual creates a "field >= value" condition
func GreaterThanOrEqual(field string, value interface{}) Condition {
	return query.GreaterThanOrEqual(field, value)
}

// LessThan creates a "field < value" condition
func LessThan(field string, value interface{}) Condition {
	return query.LessThan(field, value)
}

// LessThanOrEqual creates a "field <= value" condition
func LessThanOrEqual(field string, value interface{}) Condition {
	return query.LessThanOrEqual(field, value)
}

// Like creates a "field LIKE pattern" condition ('%' matches any sequence,
// '_' any single character)
func Like(field string, pattern string) Condition {
	return query.Like(field, pattern)
}

// NotLike creates a "field NOT LIKE pattern" condition
func NotLike(field string, pattern string) Condition {
	return query.NotLike(field, pattern)
}

// In creates a "field IN (values...)" condition; with no values it matches no rows
func In(field string, values ...interface{}) Condition {
	return query.In(field, values...)
}

// NotIn creates a "field NOT IN (values...)" condition; with no values it
// matches every row
func NotIn(field string, values ...interface{}) Condition {
	return query.NotIn(field, values...)
}

// Between creates a "field BETWEEN low AND high" condition (inclusive)
func Between(field string, low, high interface{}) Condition {
	return query.Between(field, low, high)
}

// NotBetween creates a "field NOT BETWEEN low AND high" condition
func NotBetween(field string, low, high interface{}) Condition {
	return query.NotBetween(field, low, high)
}

// IsNull creates a "field IS NULL" condition
func IsNull(field string) Condition {
	return query.IsNull(field)
}

// IsNotNull creates a "field IS NOT NULL" condition
func IsNotNull(field string) Condition {
	return query.IsNotNull(field)
}

// Exists creates an "EXISTS (subquery)" condition; the subquery's parameters
// are bound in place
func Exists(subquery QueryBuilder) Condition {
	return query.Exists(subquery)
}

// NotExists creates a "NOT EXISTS (subquery)" condition
func NotExists(subquery QueryBuilder) Condition {
	return query.NotExists(subquery)
}

// And groups conditions as "(c1 AND c2 ...)"
func And(conditions ...Condition) Condition {
	return query.And(conditions...)
}

// Or groups conditions as "(c1 OR c2 ...)"
func Or(conditions ...Condition) Condition {
	return query.Or(conditions...)
}

// Col marks a condition value as a column reference rather than a bound
// parameter, for example Equal("o.user_id", Col("u.id")) in a JOIN.
func Col(name string) ColumnRef {
	return query.Col(name)
}
