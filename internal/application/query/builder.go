// Package query provides a fluent query builder for database-agnostic queries.
//
// Builders render SQL with '?' placeholders (adapters translate them to the
// database's native form) together with the equivalent structured Spec used by
// document adapters. Values are always bound as parameters unless wrapped with
// Col; identifiers (tables, columns, aliases) are validated against a strict
// pattern, and invalid input makes Build return a domain.ErrInvalidQuery error.
package query

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// Builder implements the domain.QueryBuilder interface for SELECT queries.
//
// Setters only record state (an invalid pagination or subquery argument is kept
// and reported by Build); Build validates and renders it without modifying the
// builder, so Build and BuildRaw can be called repeatedly with identical results.
type Builder struct {
	selectFields []string
	distinct     bool
	fromTable    string
	fromSubquery *Builder
	fromAlias    string
	where        []domain.Condition
	joins        []domain.Join
	groupBy      []string
	having       []domain.Condition
	orderBy      []domain.OrderBy
	limit        *int
	offset       *int
	err          error // first invalid argument passed to a setter
}

// NewBuilder creates a new query builder
func NewBuilder() domain.QueryBuilder {
	return &Builder{}
}

// Select adds fields to select. Each field is an identifier (optionally
// dot-qualified), "*", "t.*", or COUNT/SUM/AVG/MIN/MAX of an identifier or "*";
// identifiers and aggregates may be followed by "AS alias". Selecting no fields
// selects "*".
func (b *Builder) Select(fields ...string) domain.QueryBuilder {
	b.selectFields = append(b.selectFields, fields...)
	return b
}

// SelectDistinct adds fields to select and makes the query SELECT DISTINCT
func (b *Builder) SelectDistinct(fields ...string) domain.QueryBuilder {
	b.selectFields = append(b.selectFields, fields...)
	b.distinct = true
	return b
}

// SelectCount adds COUNT(field), or COUNT(*) when field is empty
func (b *Builder) SelectCount(field string) domain.QueryBuilder {
	if field == "" {
		field = "*"
	}
	b.selectFields = append(b.selectFields, "COUNT("+field+")")
	return b
}

// From sets the table to select from, optionally with an alias ("users u" or
// "users AS u"). It replaces a data source set earlier.
func (b *Builder) From(table string) domain.QueryBuilder {
	b.fromTable = table
	b.fromSubquery = nil
	b.fromAlias = ""
	return b
}

// FromSubquery sets a subquery created by NewBuilder as the data source, under
// the given alias. It replaces a data source set earlier; any other subquery
// value makes Build fail.
func (b *Builder) FromSubquery(subquery domain.QueryBuilder, alias string) domain.QueryBuilder {
	sb, ok := subquery.(*Builder)
	if !ok || sb == nil {
		b.setErr(domain.NewInvalidQueryError(fmt.Sprintf("subquery must be a non-nil *query.Builder, got %T", subquery)))
		return b
	}
	b.fromTable = ""
	b.fromSubquery = sb
	b.fromAlias = alias
	return b
}

// Where adds a condition; Where and WhereAnd conditions are combined with AND
func (b *Builder) Where(condition domain.Condition) domain.QueryBuilder {
	b.where = append(b.where, condition)
	return b
}

// WhereAnd adds conditions that are combined with AND
func (b *Builder) WhereAnd(conditions ...domain.Condition) domain.QueryBuilder {
	b.where = append(b.where, conditions...)
	return b
}

// WhereOr adds a single parenthesised group "(c1 OR c2 ...)", combined with the
// other WHERE conditions using AND. It is a no-op without conditions.
func (b *Builder) WhereOr(conditions ...domain.Condition) domain.QueryBuilder {
	if len(conditions) == 0 {
		return b
	}
	b.where = append(b.where, Or(slices.Clone(conditions)...))
	return b
}

// Having adds a HAVING condition; multiple conditions are combined with AND
func (b *Builder) Having(condition domain.Condition) domain.QueryBuilder {
	b.having = append(b.having, condition)
	return b
}

// Join adds a join. The table may carry an alias ("orders o" or "orders AS o").
// Compare columns in on with Col, e.g. Equal("o.user_id", Col("u.id")): plain
// values are bound as parameters. on is ignored for domain.JoinTypeCross, which
// has no ON clause.
func (b *Builder) Join(joinType domain.JoinType, table string, on domain.Condition) domain.QueryBuilder {
	join := domain.Join{Type: joinType, Table: table}
	if joinType != domain.JoinTypeCross {
		join.On = &on
	}
	b.joins = append(b.joins, join)
	return b
}

// LeftJoin adds a left join
func (b *Builder) LeftJoin(table string, on domain.Condition) domain.QueryBuilder {
	return b.Join(domain.JoinTypeLeft, table, on)
}

// RightJoin adds a right join
func (b *Builder) RightJoin(table string, on domain.Condition) domain.QueryBuilder {
	return b.Join(domain.JoinTypeRight, table, on)
}

// InnerJoin adds an inner join
func (b *Builder) InnerJoin(table string, on domain.Condition) domain.QueryBuilder {
	return b.Join(domain.JoinTypeInner, table, on)
}

// GroupBy adds group by fields
func (b *Builder) GroupBy(fields ...string) domain.QueryBuilder {
	b.groupBy = append(b.groupBy, fields...)
	return b
}

// OrderBy adds an order by field
func (b *Builder) OrderBy(field string, direction domain.SortDirection) domain.QueryBuilder {
	b.orderBy = append(b.orderBy, domain.OrderBy{Field: field, Direction: direction})
	return b
}

// OrderByDesc adds an order by field in descending order
func (b *Builder) OrderByDesc(field string) domain.QueryBuilder {
	return b.OrderBy(field, domain.SortDirectionDesc)
}

// OrderByAsc adds an order by field in ascending order
func (b *Builder) OrderByAsc(field string) domain.QueryBuilder {
	return b.OrderBy(field, domain.SortDirectionAsc)
}

// Limit sets the limit; a negative limit makes Build fail
func (b *Builder) Limit(limit int) domain.QueryBuilder {
	if limit < 0 {
		b.setErr(domain.NewInvalidQueryError("limit must not be negative").WithContext("limit", limit))
		return b
	}
	b.limit = &limit
	return b
}

// Offset sets the offset; a negative offset makes Build fail
func (b *Builder) Offset(offset int) domain.QueryBuilder {
	if offset < 0 {
		b.setErr(domain.NewInvalidQueryError("offset must not be negative").WithContext("offset", offset))
		return b
	}
	b.offset = &offset
	return b
}

// Page sets LIMIT size and OFFSET (page-1)*size. page is 1-based; a page or size
// below 1 makes Build fail.
func (b *Builder) Page(page, size int) domain.QueryBuilder {
	if page < 1 || size < 1 {
		b.setErr(domain.NewInvalidQueryError("page and page size must be at least 1").
			WithContext("page", page).WithContext("size", size))
		return b
	}
	if page-1 > math.MaxInt/size {
		b.setErr(domain.NewInvalidQueryError("page offset overflows int").
			WithContext("page", page).WithContext("size", size))
		return b
	}
	offset := (page - 1) * size
	b.limit = &size
	b.offset = &offset
	return b
}

func (b *Builder) setErr(err error) {
	if b.err == nil {
		b.err = err
	}
}

// Build builds the query: SQL with '?' placeholders, its parameters, and the
// structured Spec mirroring the SQL. Spec.Table is the bare table name (without
// alias) and is empty for FromSubquery, which sets Spec.Subquery instead.
func (b *Builder) Build() (domain.Query, error) {
	sql, params, spec, err := b.build()
	if err != nil {
		return domain.Query{}, err
	}
	return domain.Query{
		SQL:        sql,
		Parameters: params,
		Type:       domain.QueryTypeSelect,
		Spec:       spec,
	}, nil
}

// BuildRaw builds the raw SQL and parameters
func (b *Builder) BuildRaw() (string, []interface{}, error) {
	sql, params, _, err := b.build()
	return sql, params, err
}

func (b *Builder) build() (string, []interface{}, *domain.QuerySpec, error) {
	if b.err != nil {
		return "", nil, nil, b.err
	}

	var r renderer
	var parts []string
	spec := &domain.QuerySpec{Distinct: b.distinct}

	// SELECT clause
	for _, field := range b.selectFields {
		if err := checkIdentifier(selectPattern, "select expression", field); err != nil {
			return "", nil, nil, err
		}
	}
	selectList := "*"
	if len(b.selectFields) > 0 {
		selectList = strings.Join(b.selectFields, ", ")
	}
	if selectList != "*" {
		spec.Fields = slices.Clone(b.selectFields)
	}
	if b.distinct {
		parts = append(parts, "SELECT DISTINCT "+selectList)
	} else {
		parts = append(parts, "SELECT "+selectList)
	}

	// FROM clause
	switch {
	case b.fromSubquery != nil:
		if err := checkIdentifier(aliasPattern, "subquery alias", b.fromAlias); err != nil {
			return "", nil, nil, err
		}
		subSQL, err := r.subquery(b.fromSubquery)
		if err != nil {
			return "", nil, nil, err
		}
		parts = append(parts, "FROM "+subSQL+" AS "+b.fromAlias)
		spec.Subquery = true
	case b.fromTable != "":
		table, err := tableName(b.fromTable)
		if err != nil {
			return "", nil, nil, err
		}
		parts = append(parts, "FROM "+b.fromTable)
		spec.Table = table
	default:
		return "", nil, nil, domain.NewInvalidQueryError("no table or subquery specified")
	}

	// JOIN clauses
	for _, join := range b.joins {
		joinSQL, err := r.join(join)
		if err != nil {
			return "", nil, nil, err
		}
		parts = append(parts, joinSQL)
	}
	spec.Joins = cloneJoins(b.joins)

	// WHERE clause
	if len(b.where) > 0 {
		whereSQL, err := r.conditions(b.where, " AND ")
		if err != nil {
			return "", nil, nil, err
		}
		parts = append(parts, "WHERE "+whereSQL)
		spec.Where = cloneConditions(b.where)
	}

	// GROUP BY clause
	if len(b.groupBy) > 0 {
		for _, field := range b.groupBy {
			if err := checkIdentifier(identifierPattern, "GROUP BY field", field); err != nil {
				return "", nil, nil, err
			}
		}
		parts = append(parts, "GROUP BY "+strings.Join(b.groupBy, ", "))
		spec.GroupBy = slices.Clone(b.groupBy)
	}

	// HAVING clause
	if len(b.having) > 0 {
		havingSQL, err := r.conditions(b.having, " AND ")
		if err != nil {
			return "", nil, nil, err
		}
		parts = append(parts, "HAVING "+havingSQL)
		spec.Having = cloneConditions(b.having)
	}

	// ORDER BY clause
	if len(b.orderBy) > 0 {
		terms := make([]string, len(b.orderBy))
		for i, order := range b.orderBy {
			if err := checkIdentifier(identifierPattern, "ORDER BY field", order.Field); err != nil {
				return "", nil, nil, err
			}
			var direction string
			switch order.Direction {
			case domain.SortDirectionAsc:
				direction = "ASC"
			case domain.SortDirectionDesc:
				direction = "DESC"
			default:
				return "", nil, nil, domain.NewInvalidQueryError(fmt.Sprintf("unknown sort direction %d", order.Direction))
			}
			terms[i] = order.Field + " " + direction
		}
		parts = append(parts, "ORDER BY "+strings.Join(terms, ", "))
		spec.OrderBy = slices.Clone(b.orderBy)
	}

	// LIMIT / OFFSET clauses
	if b.limit != nil {
		limit := *b.limit
		parts = append(parts, "LIMIT "+strconv.Itoa(limit))
		spec.Limit = &limit
	}
	if b.offset != nil {
		offset := *b.offset
		parts = append(parts, "OFFSET "+strconv.Itoa(offset))
		spec.Offset = &offset
	}

	return strings.Join(parts, " "), r.params, spec, nil
}

// joinKeywords maps every supported join type to its SQL keyword.
var joinKeywords = map[domain.JoinType]string{
	domain.JoinTypeInner: "INNER JOIN",
	domain.JoinTypeLeft:  "LEFT JOIN",
	domain.JoinTypeRight: "RIGHT JOIN",
	domain.JoinTypeFull:  "FULL OUTER JOIN",
	domain.JoinTypeCross: "CROSS JOIN",
}

// join renders a JOIN clause; CROSS JOIN has no ON clause.
func (r *renderer) join(join domain.Join) (string, error) {
	keyword, ok := joinKeywords[join.Type]
	if !ok {
		return "", domain.NewInvalidQueryError(fmt.Sprintf("unknown join type %d", join.Type))
	}
	if _, err := tableName(join.Table); err != nil {
		return "", err
	}
	if join.On == nil {
		return keyword + " " + join.Table, nil
	}
	onSQL, err := r.condition(*join.On)
	if err != nil {
		return "", err
	}
	return keyword + " " + join.Table + " ON " + onSQL, nil
}

// cloneJoins deep-copies joins so that a built Spec does not share state with
// the builder.
func cloneJoins(joins []domain.Join) []domain.Join {
	if len(joins) == 0 {
		return nil
	}
	out := make([]domain.Join, len(joins))
	for i, join := range joins {
		if join.On != nil {
			on := cloneCondition(*join.On)
			join.On = &on
		}
		out[i] = join
	}
	return out
}

// CommandBuilder provides a fluent API for building commands (INSERT, UPDATE,
// DELETE). Create one with Insert, Update or Delete.
type CommandBuilder struct {
	commandType domain.CommandType
	table       string
	values      []domain.FieldValue // in first-Set order
	where       []domain.Condition
}

// Insert creates an INSERT command builder
func Insert(table string) *CommandBuilder {
	return &CommandBuilder{commandType: domain.CommandTypeInsert, table: table}
}

// Update creates an UPDATE command builder
func Update(table string) *CommandBuilder {
	return &CommandBuilder{commandType: domain.CommandTypeUpdate, table: table}
}

// Delete creates a DELETE command builder
func Delete(table string) *CommandBuilder {
	return &CommandBuilder{commandType: domain.CommandTypeDelete, table: table}
}

// Set assigns a column value for INSERT or UPDATE. Columns are rendered in the
// order they were first set; setting a column again replaces its value in place.
func (cb *CommandBuilder) Set(field string, value interface{}) *CommandBuilder {
	for i := range cb.values {
		if cb.values[i].Field == field {
			cb.values[i].Value = value
			return cb
		}
	}
	cb.values = append(cb.values, domain.FieldValue{Field: field, Value: value})
	return cb
}

// Values sets multiple column values as Set does, taking the keys in sorted
// order so that the generated SQL is deterministic.
func (cb *CommandBuilder) Values(values map[string]interface{}) *CommandBuilder {
	for _, field := range slices.Sorted(maps.Keys(values)) {
		cb.Set(field, values[field])
	}
	return cb
}

// Where adds a where condition for UPDATE or DELETE
func (cb *CommandBuilder) Where(condition domain.Condition) *CommandBuilder {
	cb.where = append(cb.where, condition)
	return cb
}

// WhereAnd adds multiple AND conditions
func (cb *CommandBuilder) WhereAnd(conditions ...domain.Condition) *CommandBuilder {
	cb.where = append(cb.where, conditions...)
	return cb
}

// Build builds the command: SQL with '?' placeholders, its parameters, and the
// structured Spec mirroring the SQL.
func (cb *CommandBuilder) Build() (domain.Command, error) {
	sql, params, err := cb.BuildRaw()
	if err != nil {
		return domain.Command{}, err
	}
	return domain.Command{
		SQL:        sql,
		Parameters: params,
		Type:       cb.commandType,
		Spec: &domain.CommandSpec{
			Table:  cb.table,
			Values: slices.Clone(cb.values),
			Where:  cloneConditions(cb.where),
		},
	}, nil
}

// BuildRaw builds the raw SQL and parameters
func (cb *CommandBuilder) BuildRaw() (string, []interface{}, error) {
	if cb.table == "" {
		return "", nil, domain.NewInvalidQueryError("table name is required")
	}
	if err := checkIdentifier(identifierPattern, "table", cb.table); err != nil {
		return "", nil, err
	}

	var r renderer
	var sql string
	switch cb.commandType {
	case domain.CommandTypeInsert:
		if len(cb.where) > 0 {
			return "", nil, domain.NewInvalidQueryError("INSERT does not support WHERE conditions")
		}
		columns, err := cb.columns(&r)
		if err != nil {
			return "", nil, err
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
		sql = "INSERT INTO " + cb.table + " (" + strings.Join(columns, ", ") + ") VALUES (" + placeholders + ")"
	case domain.CommandTypeUpdate:
		columns, err := cb.columns(&r)
		if err != nil {
			return "", nil, err
		}
		for i, column := range columns {
			columns[i] = column + " = ?"
		}
		sql = "UPDATE " + cb.table + " SET " + strings.Join(columns, ", ")
	case domain.CommandTypeDelete:
		if len(cb.values) > 0 {
			return "", nil, domain.NewInvalidQueryError("DELETE does not support SET values")
		}
		sql = "DELETE FROM " + cb.table
	default:
		return "", nil, domain.NewInvalidQueryError(fmt.Sprintf("unsupported command type %d", cb.commandType))
	}

	if len(cb.where) > 0 {
		whereSQL, err := r.conditions(cb.where, " AND ")
		if err != nil {
			return "", nil, err
		}
		sql += " WHERE " + whereSQL
	}

	return sql, r.params, nil
}

// columns validates the assigned columns and binds their values in Set order.
func (cb *CommandBuilder) columns(r *renderer) ([]string, error) {
	if len(cb.values) == 0 {
		return nil, domain.NewInvalidQueryError("at least one value is required")
	}
	columns := make([]string, len(cb.values))
	for i, fv := range cb.values {
		if err := checkIdentifier(identifierPattern, "column", fv.Field); err != nil {
			return nil, err
		}
		if _, ok := fv.Value.(domain.ColumnRef); ok {
			return nil, domain.NewInvalidQueryError("column references are only supported in conditions").
				WithContext("column", fv.Field)
		}
		columns[i] = fv.Field
		r.params = append(r.params, fv.Value)
	}
	return columns, nil
}
