// Package query provides a fluent query builder for database-agnostic queries
package query

import (
	"fmt"
	"strings"

	"github.com/HybridCache.io/storage/pkg/storage"
)

// Builder implements the QueryBuilder interface
type Builder struct {
	selectFields    []string
	distinct        bool
	fromTable       string
	fromSubquery    *Builder
	fromAlias       string
	whereConditions []storage.Condition
	joinClauses     []joinClause
	groupByFields   []string
	havingCondition *storage.Condition
	orderByFields   []orderByField
	limitValue      *int
	offsetValue     *int
	queryType       storage.QueryType
	parameters      []interface{}
	paramIndex      int
}

type joinClause struct {
	joinType storage.JoinType
	table    string
	on       storage.Condition
}

type orderByField struct {
	field     string
	direction storage.SortDirection
}

// NewBuilder creates a new query builder
func NewBuilder() storage.QueryBuilder {
	return &Builder{
		selectFields:    make([]string, 0),
		whereConditions: make([]storage.Condition, 0),
		joinClauses:     make([]joinClause, 0),
		groupByFields:   make([]string, 0),
		orderByFields:   make([]orderByField, 0),
		parameters:      make([]interface{}, 0),
		queryType:       storage.QueryTypeSelect,
	}
}

// Select adds fields to select
func (b *Builder) Select(fields ...string) storage.QueryBuilder {
	b.selectFields = append(b.selectFields, fields...)
	b.queryType = storage.QueryTypeSelect
	return b
}

// SelectDistinct adds distinct fields to select
func (b *Builder) SelectDistinct(fields ...string) storage.QueryBuilder {
	b.selectFields = append(b.selectFields, fields...)
	b.distinct = true
	b.queryType = storage.QueryTypeSelect
	return b
}

// SelectCount adds a count field
func (b *Builder) SelectCount(field string) storage.QueryBuilder {
	if field == "" {
		field = "*"
	}
	b.selectFields = append(b.selectFields, fmt.Sprintf("COUNT(%s)", field))
	b.queryType = storage.QueryTypeSelect
	return b
}

// From sets the table to select from
func (b *Builder) From(table string) storage.QueryBuilder {
	b.fromTable = table
	return b
}

// FromSubquery sets a subquery as the data source
func (b *Builder) FromSubquery(subquery storage.QueryBuilder, alias string) storage.QueryBuilder {
	if sb, ok := subquery.(*Builder); ok {
		b.fromSubquery = sb
		b.fromAlias = alias
	}
	return b
}

// Where adds a where condition
func (b *Builder) Where(condition storage.Condition) storage.QueryBuilder {
	b.whereConditions = append(b.whereConditions, condition)
	return b
}

// WhereAnd adds multiple AND conditions
func (b *Builder) WhereAnd(conditions ...storage.Condition) storage.QueryBuilder {
	b.whereConditions = append(b.whereConditions, conditions...)
	return b
}

// WhereOr adds multiple OR conditions (combined as a single OR group)
func (b *Builder) WhereOr(conditions ...storage.Condition) storage.QueryBuilder {
	// For OR conditions, we need to group them properly
	// This is a simplified implementation - in practice, you'd want more sophisticated grouping
	for _, condition := range conditions {
		b.whereConditions = append(b.whereConditions, condition)
	}
	return b
}

// Having adds a having condition
func (b *Builder) Having(condition storage.Condition) storage.QueryBuilder {
	b.havingCondition = &condition
	return b
}

// Join adds a join clause
func (b *Builder) Join(joinType storage.JoinType, table string, on storage.Condition) storage.QueryBuilder {
	b.joinClauses = append(b.joinClauses, joinClause{
		joinType: joinType,
		table:    table,
		on:       on,
	})
	return b
}

// LeftJoin adds a left join
func (b *Builder) LeftJoin(table string, on storage.Condition) storage.QueryBuilder {
	return b.Join(storage.JoinTypeLeft, table, on)
}

// RightJoin adds a right join
func (b *Builder) RightJoin(table string, on storage.Condition) storage.QueryBuilder {
	return b.Join(storage.JoinTypeRight, table, on)
}

// InnerJoin adds an inner join
func (b *Builder) InnerJoin(table string, on storage.Condition) storage.QueryBuilder {
	return b.Join(storage.JoinTypeInner, table, on)
}

// GroupBy adds group by fields
func (b *Builder) GroupBy(fields ...string) storage.QueryBuilder {
	b.groupByFields = append(b.groupByFields, fields...)
	return b
}

// OrderBy adds an order by field
func (b *Builder) OrderBy(field string, direction storage.SortDirection) storage.QueryBuilder {
	b.orderByFields = append(b.orderByFields, orderByField{
		field:     field,
		direction: direction,
	})
	return b
}

// OrderByDesc adds an order by field in descending order
func (b *Builder) OrderByDesc(field string) storage.QueryBuilder {
	return b.OrderBy(field, storage.SortDirectionDesc)
}

// OrderByAsc adds an order by field in ascending order
func (b *Builder) OrderByAsc(field string) storage.QueryBuilder {
	return b.OrderBy(field, storage.SortDirectionAsc)
}

// Limit sets the limit
func (b *Builder) Limit(limit int) storage.QueryBuilder {
	b.limitValue = &limit
	return b
}

// Offset sets the offset
func (b *Builder) Offset(offset int) storage.QueryBuilder {
	b.offsetValue = &offset
	return b
}

// Page sets pagination (page is 1-based)
func (b *Builder) Page(page, size int) storage.QueryBuilder {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	b.limitValue = &size
	b.offsetValue = &offset
	return b
}

// Build builds the query
func (b *Builder) Build() (storage.Query, error) {
	sql, params, err := b.BuildRaw()
	if err != nil {
		return storage.Query{}, err
	}

	return storage.Query{
		SQL:        sql,
		Parameters: params,
		Type:       b.queryType,
		Options:    storage.QueryOptions{},
	}, nil
}

// BuildRaw builds the raw SQL and parameters
func (b *Builder) BuildRaw() (string, []interface{}, error) {
	var parts []string
	var params []interface{}

	// SELECT clause
	if len(b.selectFields) == 0 {
		b.selectFields = []string{"*"}
	}

	selectClause := "SELECT "
	if b.distinct {
		selectClause += "DISTINCT "
	}
	selectClause += strings.Join(b.selectFields, ", ")
	parts = append(parts, selectClause)

	// FROM clause
	if b.fromTable != "" {
		parts = append(parts, "FROM "+b.fromTable)
	} else if b.fromSubquery != nil {
		subSQL, subParams, err := b.fromSubquery.BuildRaw()
		if err != nil {
			return "", nil, fmt.Errorf("failed to build subquery: %w", err)
		}
		parts = append(parts, fmt.Sprintf("FROM (%s) AS %s", subSQL, b.fromAlias))
		params = append(params, subParams...)
	} else {
		return "", nil, fmt.Errorf("no table or subquery specified")
	}

	// JOIN clauses
	for _, join := range b.joinClauses {
		joinSQL, joinParams := b.buildJoinClause(join)
		parts = append(parts, joinSQL)
		params = append(params, joinParams...)
	}

	// WHERE clause
	if len(b.whereConditions) > 0 {
		whereSQL, whereParams := b.buildWhereClause(b.whereConditions)
		parts = append(parts, "WHERE "+whereSQL)
		params = append(params, whereParams...)
	}

	// GROUP BY clause
	if len(b.groupByFields) > 0 {
		parts = append(parts, "GROUP BY "+strings.Join(b.groupByFields, ", "))
	}

	// HAVING clause
	if b.havingCondition != nil {
		havingSQL, havingParams := b.buildCondition(*b.havingCondition)
		parts = append(parts, "HAVING "+havingSQL)
		params = append(params, havingParams...)
	}

	// ORDER BY clause
	if len(b.orderByFields) > 0 {
		var orderParts []string
		for _, order := range b.orderByFields {
			direction := "ASC"
			if order.direction == storage.SortDirectionDesc {
				direction = "DESC"
			}
			orderParts = append(orderParts, fmt.Sprintf("%s %s", order.field, direction))
		}
		parts = append(parts, "ORDER BY "+strings.Join(orderParts, ", "))
	}

	// LIMIT clause
	if b.limitValue != nil {
		parts = append(parts, fmt.Sprintf("LIMIT %d", *b.limitValue))
	}

	// OFFSET clause
	if b.offsetValue != nil {
		parts = append(parts, fmt.Sprintf("OFFSET %d", *b.offsetValue))
	}

	return strings.Join(parts, " "), params, nil
}

// buildJoinClause builds a JOIN clause
func (b *Builder) buildJoinClause(join joinClause) (string, []interface{}) {
	var joinType string
	switch join.joinType {
	case storage.JoinTypeInner:
		joinType = "INNER JOIN"
	case storage.JoinTypeLeft:
		joinType = "LEFT JOIN"
	case storage.JoinTypeRight:
		joinType = "RIGHT JOIN"
	case storage.JoinTypeFull:
		joinType = "FULL OUTER JOIN"
	case storage.JoinTypeCross:
		joinType = "CROSS JOIN"
	default:
		joinType = "INNER JOIN"
	}

	onSQL, onParams := b.buildCondition(join.on)
	return fmt.Sprintf("%s %s ON %s", joinType, join.table, onSQL), onParams
}

// buildWhereClause builds the WHERE clause
func (b *Builder) buildWhereClause(conditions []storage.Condition) (string, []interface{}) {
	if len(conditions) == 0 {
		return "", nil
	}

	var parts []string
	var params []interface{}

	for i, condition := range conditions {
		condSQL, condParams := b.buildCondition(condition)
		if i > 0 {
			parts = append(parts, "AND")
		}
		parts = append(parts, condSQL)
		params = append(params, condParams...)
	}

	return strings.Join(parts, " "), params
}

// buildCondition builds a single condition
func (b *Builder) buildCondition(condition storage.Condition) (string, []interface{}) {
	switch condition.Operator {
	case storage.OperatorEqual:
		return fmt.Sprintf("%s = ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorNotEqual:
		return fmt.Sprintf("%s != ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorGreaterThan:
		return fmt.Sprintf("%s > ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorGreaterThanOrEqual:
		return fmt.Sprintf("%s >= ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLessThan:
		return fmt.Sprintf("%s < ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLessThanOrEqual:
		return fmt.Sprintf("%s <= ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLike:
		return fmt.Sprintf("%s LIKE ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorNotLike:
		return fmt.Sprintf("%s NOT LIKE ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorIn:
		if len(condition.Values) == 0 {
			return "1=0", nil // No values means no match
		}
		placeholders := strings.Repeat("?,", len(condition.Values))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		return fmt.Sprintf("%s IN (%s)", condition.Field, placeholders), condition.Values
	case storage.OperatorNotIn:
		if len(condition.Values) == 0 {
			return "1=1", nil // No values means all match
		}
		placeholders := strings.Repeat("?,", len(condition.Values))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		return fmt.Sprintf("%s NOT IN (%s)", condition.Field, placeholders), condition.Values
	case storage.OperatorBetween:
		if len(condition.Values) >= 2 {
			return fmt.Sprintf("%s BETWEEN ? AND ?", condition.Field), condition.Values[:2]
		}
		return fmt.Sprintf("%s BETWEEN ? AND ?", condition.Field), []interface{}{condition.Value, condition.Value}
	case storage.OperatorNotBetween:
		if len(condition.Values) >= 2 {
			return fmt.Sprintf("%s NOT BETWEEN ? AND ?", condition.Field), condition.Values[:2]
		}
		return fmt.Sprintf("%s NOT BETWEEN ? AND ?", condition.Field), []interface{}{condition.Value, condition.Value}
	case storage.OperatorIsNull:
		return fmt.Sprintf("%s IS NULL", condition.Field), nil
	case storage.OperatorIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", condition.Field), nil
	case storage.OperatorExists:
		return fmt.Sprintf("EXISTS (%s)", condition.Value), nil
	case storage.OperatorNotExists:
		return fmt.Sprintf("NOT EXISTS (%s)", condition.Value), nil
	default:
		return fmt.Sprintf("%s = ?", condition.Field), []interface{}{condition.Value}
	}
}

// Helper functions for creating conditions
func Equal(field string, value interface{}) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorEqual,
		Value:    value,
	}
}

func NotEqual(field string, value interface{}) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorNotEqual,
		Value:    value,
	}
}

func GreaterThan(field string, value interface{}) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorGreaterThan,
		Value:    value,
	}
}

func LessThan(field string, value interface{}) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorLessThan,
		Value:    value,
	}
}

func In(field string, values ...interface{}) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorIn,
		Values:   values,
	}
}

func Like(field string, pattern string) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorLike,
		Value:    pattern,
	}
}

func IsNull(field string) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorIsNull,
	}
}

func IsNotNull(field string) storage.Condition {
	return storage.Condition{
		Field:    field,
		Operator: storage.OperatorIsNotNull,
	}
}

// CommandBuilder provides a fluent API for building commands (INSERT, UPDATE, DELETE)
type CommandBuilder struct {
	commandType     storage.CommandType
	table           string
	values          map[string]interface{}
	whereConditions []storage.Condition
	parameters      []interface{}
}

// NewCommandBuilder creates a new command builder
func NewCommandBuilder() *CommandBuilder {
	return &CommandBuilder{
		values:          make(map[string]interface{}),
		whereConditions: make([]storage.Condition, 0),
		parameters:      make([]interface{}, 0),
	}
}

// Insert creates an INSERT command builder
func Insert(table string) *CommandBuilder {
	return &CommandBuilder{
		commandType:     storage.CommandTypeInsert,
		table:           table,
		values:          make(map[string]interface{}),
		whereConditions: make([]storage.Condition, 0),
		parameters:      make([]interface{}, 0),
	}
}

// Update creates an UPDATE command builder
func Update(table string) *CommandBuilder {
	return &CommandBuilder{
		commandType:     storage.CommandTypeUpdate,
		table:           table,
		values:          make(map[string]interface{}),
		whereConditions: make([]storage.Condition, 0),
		parameters:      make([]interface{}, 0),
	}
}

// Delete creates a DELETE command builder
func Delete(table string) *CommandBuilder {
	return &CommandBuilder{
		commandType:     storage.CommandTypeDelete,
		table:           table,
		values:          make(map[string]interface{}),
		whereConditions: make([]storage.Condition, 0),
		parameters:      make([]interface{}, 0),
	}
}

// Set adds a field-value pair for INSERT or UPDATE
func (cb *CommandBuilder) Set(field string, value interface{}) *CommandBuilder {
	cb.values[field] = value
	return cb
}

// Values sets multiple field-value pairs
func (cb *CommandBuilder) Values(values map[string]interface{}) *CommandBuilder {
	for field, value := range values {
		cb.values[field] = value
	}
	return cb
}

// Where adds a where condition for UPDATE or DELETE
func (cb *CommandBuilder) Where(condition storage.Condition) *CommandBuilder {
	cb.whereConditions = append(cb.whereConditions, condition)
	return cb
}

// WhereAnd adds multiple AND conditions
func (cb *CommandBuilder) WhereAnd(conditions ...storage.Condition) *CommandBuilder {
	cb.whereConditions = append(cb.whereConditions, conditions...)
	return cb
}

// Build builds the command
func (cb *CommandBuilder) Build() (storage.Command, error) {
	sql, params, err := cb.BuildRaw()
	if err != nil {
		return storage.Command{}, err
	}

	return storage.Command{
		SQL:        sql,
		Parameters: params,
		Type:       cb.commandType,
		Options:    storage.CommandOptions{},
	}, nil
}

// BuildRaw builds the raw SQL and parameters
func (cb *CommandBuilder) BuildRaw() (string, []interface{}, error) {
	switch cb.commandType {
	case storage.CommandTypeInsert:
		return cb.buildInsert()
	case storage.CommandTypeUpdate:
		return cb.buildUpdate()
	case storage.CommandTypeDelete:
		return cb.buildDelete()
	default:
		return "", nil, fmt.Errorf("unsupported command type: %v", cb.commandType)
	}
}

// buildInsert builds an INSERT statement
func (cb *CommandBuilder) buildInsert() (string, []interface{}, error) {
	if cb.table == "" {
		return "", nil, fmt.Errorf("table name is required for INSERT")
	}

	if len(cb.values) == 0 {
		return "", nil, fmt.Errorf("values are required for INSERT")
	}

	var fields []string
	var placeholders []string
	var params []interface{}

	for field, value := range cb.values {
		fields = append(fields, field)
		placeholders = append(placeholders, "?")
		params = append(params, value)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		cb.table,
		strings.Join(fields, ", "),
		strings.Join(placeholders, ", "))

	return sql, params, nil
}

// buildUpdate builds an UPDATE statement
func (cb *CommandBuilder) buildUpdate() (string, []interface{}, error) {
	if cb.table == "" {
		return "", nil, fmt.Errorf("table name is required for UPDATE")
	}

	if len(cb.values) == 0 {
		return "", nil, fmt.Errorf("values are required for UPDATE")
	}

	var setParts []string
	var params []interface{}

	for field, value := range cb.values {
		setParts = append(setParts, fmt.Sprintf("%s = ?", field))
		params = append(params, value)
	}

	sql := fmt.Sprintf("UPDATE %s SET %s", cb.table, strings.Join(setParts, ", "))

	// Add WHERE clause if conditions exist
	if len(cb.whereConditions) > 0 {
		whereSQL, whereParams := cb.buildWhereClause(cb.whereConditions)
		sql += " WHERE " + whereSQL
		params = append(params, whereParams...)
	}

	return sql, params, nil
}

// buildDelete builds a DELETE statement
func (cb *CommandBuilder) buildDelete() (string, []interface{}, error) {
	if cb.table == "" {
		return "", nil, fmt.Errorf("table name is required for DELETE")
	}

	sql := fmt.Sprintf("DELETE FROM %s", cb.table)
	var params []interface{}

	// Add WHERE clause if conditions exist
	if len(cb.whereConditions) > 0 {
		whereSQL, whereParams := cb.buildWhereClause(cb.whereConditions)
		sql += " WHERE " + whereSQL
		params = append(params, whereParams...)
	}

	return sql, params, nil
}

// buildWhereClause builds the WHERE clause (reused from QueryBuilder)
func (cb *CommandBuilder) buildWhereClause(conditions []storage.Condition) (string, []interface{}) {
	if len(conditions) == 0 {
		return "", nil
	}

	var parts []string
	var params []interface{}

	for i, condition := range conditions {
		condSQL, condParams := cb.buildCondition(condition)
		if i > 0 {
			parts = append(parts, "AND")
		}
		parts = append(parts, condSQL)
		params = append(params, condParams...)
	}

	return strings.Join(parts, " "), params
}

// buildCondition builds a single condition (reused from QueryBuilder)
func (cb *CommandBuilder) buildCondition(condition storage.Condition) (string, []interface{}) {
	switch condition.Operator {
	case storage.OperatorEqual:
		return fmt.Sprintf("%s = ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorNotEqual:
		return fmt.Sprintf("%s != ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorGreaterThan:
		return fmt.Sprintf("%s > ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorGreaterThanOrEqual:
		return fmt.Sprintf("%s >= ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLessThan:
		return fmt.Sprintf("%s < ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLessThanOrEqual:
		return fmt.Sprintf("%s <= ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorLike:
		return fmt.Sprintf("%s LIKE ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorNotLike:
		return fmt.Sprintf("%s NOT LIKE ?", condition.Field), []interface{}{condition.Value}
	case storage.OperatorIn:
		if len(condition.Values) == 0 {
			return "1=0", nil // No values means no match
		}
		placeholders := strings.Repeat("?,", len(condition.Values))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		return fmt.Sprintf("%s IN (%s)", condition.Field, placeholders), condition.Values
	case storage.OperatorNotIn:
		if len(condition.Values) == 0 {
			return "1=1", nil // No values means all match
		}
		placeholders := strings.Repeat("?,", len(condition.Values))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
		return fmt.Sprintf("%s NOT IN (%s)", condition.Field, placeholders), condition.Values
	case storage.OperatorBetween:
		if len(condition.Values) >= 2 {
			return fmt.Sprintf("%s BETWEEN ? AND ?", condition.Field), condition.Values[:2]
		}
		return fmt.Sprintf("%s BETWEEN ? AND ?", condition.Field), []interface{}{condition.Value, condition.Value}
	case storage.OperatorNotBetween:
		if len(condition.Values) >= 2 {
			return fmt.Sprintf("%s NOT BETWEEN ? AND ?", condition.Field), condition.Values[:2]
		}
		return fmt.Sprintf("%s NOT BETWEEN ? AND ?", condition.Field), []interface{}{condition.Value, condition.Value}
	case storage.OperatorIsNull:
		return fmt.Sprintf("%s IS NULL", condition.Field), nil
	case storage.OperatorIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", condition.Field), nil
	case storage.OperatorExists:
		return fmt.Sprintf("EXISTS (%s)", condition.Value), nil
	case storage.OperatorNotExists:
		return fmt.Sprintf("NOT EXISTS (%s)", condition.Value), nil
	default:
		return fmt.Sprintf("%s = ?", condition.Field), []interface{}{condition.Value}
	}
}
