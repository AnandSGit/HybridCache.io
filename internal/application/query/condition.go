package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// Identifiers are validated rather than quoted so that the generated SQL stays
// dialect-neutral; validation is what keeps identifiers taken from user input
// (for example an ORDER BY column) from injecting SQL.
const (
	identPart = `[A-Za-z_][A-Za-z0-9_]*`
	ident     = identPart + `(?:\.` + identPart + `)*`
	aggregate = `(?i:COUNT|SUM|AVG|MIN|MAX)\((?:\*|` + ident + `)\)`
)

var (
	// identifierPattern matches a column or table name, optionally dot-qualified.
	identifierPattern = regexp.MustCompile(`^` + ident + `$`)
	// aliasPattern matches an unqualified alias.
	aliasPattern = regexp.MustCompile(`^` + identPart + `$`)
	// tablePattern matches a table with an optional alias ("orders o", "orders AS o")
	// and captures the bare table name.
	tablePattern = regexp.MustCompile(`^(` + ident + `)(?:\s+(?:(?i:AS)\s+)?` + identPart + `)?$`)
	// selectPattern matches a select expression: *, t.*, or an identifier or
	// aggregate optionally followed by "AS alias".
	selectPattern = regexp.MustCompile(`^(?:\*|` + ident + `\.\*|(?:` + ident + `|` + aggregate + `)(?:\s+(?i:AS)\s+` + identPart + `)?)$`)
	// fieldPattern matches the field of a condition: an identifier or an aggregate
	// (aggregates are needed for HAVING).
	fieldPattern = regexp.MustCompile(`^(?:` + ident + `|` + aggregate + `)$`)
)

// checkIdentifier returns a domain.ErrInvalidQuery error unless value matches pattern.
func checkIdentifier(pattern *regexp.Regexp, kind, value string) error {
	if pattern.MatchString(value) {
		return nil
	}
	return invalidIdentifier(kind, value)
}

func invalidIdentifier(kind, value string) error {
	return domain.NewInvalidQueryError(fmt.Sprintf("invalid %s %q", kind, value)).
		WithContext("identifier", value)
}

// tableName validates a table reference with an optional alias and returns the
// bare table name.
func tableName(table string) (string, error) {
	m := tablePattern.FindStringSubmatch(table)
	if m == nil {
		return "", invalidIdentifier("table", table)
	}
	return m[1], nil
}

// operatorKeywords maps every supported operator to its SQL keyword.
var operatorKeywords = map[domain.Operator]string{
	domain.OperatorEqual:              "=",
	domain.OperatorNotEqual:           "!=",
	domain.OperatorGreaterThan:        ">",
	domain.OperatorGreaterThanOrEqual: ">=",
	domain.OperatorLessThan:           "<",
	domain.OperatorLessThanOrEqual:    "<=",
	domain.OperatorLike:               "LIKE",
	domain.OperatorNotLike:            "NOT LIKE",
	domain.OperatorIn:                 "IN",
	domain.OperatorNotIn:              "NOT IN",
	domain.OperatorBetween:            "BETWEEN",
	domain.OperatorNotBetween:         "NOT BETWEEN",
	domain.OperatorIsNull:             "IS NULL",
	domain.OperatorIsNotNull:          "IS NOT NULL",
	domain.OperatorExists:             "EXISTS",
	domain.OperatorNotExists:          "NOT EXISTS",
	domain.OperatorAnd:                "AND",
	domain.OperatorOr:                 "OR",
}

// renderer renders conditions to SQL with '?' placeholders, collecting the bound
// parameters in placeholder order. Builder and CommandBuilder share it.
type renderer struct {
	params []interface{}
}

// conditions renders conds joined by sep (" AND " or " OR ").
func (r *renderer) conditions(conds []domain.Condition, sep string) (string, error) {
	parts := make([]string, len(conds))
	for i, c := range conds {
		sql, err := r.condition(c)
		if err != nil {
			return "", err
		}
		parts[i] = sql
	}
	return strings.Join(parts, sep), nil
}

// condition renders a single condition.
func (r *renderer) condition(c domain.Condition) (string, error) {
	keyword, ok := operatorKeywords[c.Operator]
	if !ok {
		return "", domain.NewInvalidQueryError(fmt.Sprintf("unknown operator %d", c.Operator))
	}

	switch c.Operator {
	case domain.OperatorAnd, domain.OperatorOr:
		if len(c.Conditions) == 0 {
			return "", domain.NewInvalidQueryError(keyword + " group requires at least one condition")
		}
		sql, err := r.conditions(c.Conditions, " "+keyword+" ")
		if err != nil {
			return "", err
		}
		return "(" + sql + ")", nil
	case domain.OperatorExists, domain.OperatorNotExists:
		sub, ok := c.Value.(domain.QueryBuilder)
		if b, isBuilder := sub.(*Builder); !ok || (isBuilder && b == nil) {
			return "", domain.NewInvalidQueryError(fmt.Sprintf("%s requires a QueryBuilder subquery, got %T", keyword, c.Value))
		}
		sql, err := r.subquery(sub)
		if err != nil {
			return "", err
		}
		return keyword + " " + sql, nil
	}

	if err := checkIdentifier(fieldPattern, "field", c.Field); err != nil {
		return "", err
	}

	switch c.Operator {
	case domain.OperatorIsNull, domain.OperatorIsNotNull:
		return c.Field + " " + keyword, nil
	case domain.OperatorIn, domain.OperatorNotIn:
		if len(c.Values) == 0 {
			// "IN ()" is not valid SQL: an empty IN list matches no rows and an
			// empty NOT IN list matches every row.
			if c.Operator == domain.OperatorIn {
				return "1=0", nil
			}
			return "1=1", nil
		}
		operands, err := r.operands(c.Values)
		if err != nil {
			return "", err
		}
		return c.Field + " " + keyword + " (" + strings.Join(operands, ", ") + ")", nil
	case domain.OperatorBetween, domain.OperatorNotBetween:
		if len(c.Values) != 2 {
			return "", domain.NewInvalidQueryError(fmt.Sprintf("%s requires exactly 2 values, got %d", keyword, len(c.Values)))
		}
		operands, err := r.operands(c.Values)
		if err != nil {
			return "", err
		}
		return c.Field + " " + keyword + " " + operands[0] + " AND " + operands[1], nil
	default: // comparison operators
		operand, err := r.operand(c.Value)
		if err != nil {
			return "", err
		}
		return c.Field + " " + keyword + " " + operand, nil
	}
}

// operand renders a condition value: a domain.ColumnRef as its validated column
// name, anything else as a '?' placeholder bound to the value.
func (r *renderer) operand(value interface{}) (string, error) {
	if col, ok := value.(domain.ColumnRef); ok {
		if err := checkIdentifier(identifierPattern, "column reference", string(col)); err != nil {
			return "", err
		}
		return string(col), nil
	}
	r.params = append(r.params, value)
	return "?", nil
}

// operands renders each value with operand.
func (r *renderer) operands(values []interface{}) ([]string, error) {
	operands := make([]string, len(values))
	for i, v := range values {
		operand, err := r.operand(v)
		if err != nil {
			return nil, err
		}
		operands[i] = operand
	}
	return operands, nil
}

// subquery renders sub in parentheses and binds its parameters.
func (r *renderer) subquery(sub domain.QueryBuilder) (string, error) {
	sql, params, err := sub.BuildRaw()
	if err != nil {
		return "", domain.NewInvalidQueryError("invalid subquery").WithCause(err)
	}
	r.params = append(r.params, params...)
	return "(" + sql + ")", nil
}

// cloneConditions deep-copies conds so that a built Spec does not share slices
// with the builder.
func cloneConditions(conds []domain.Condition) []domain.Condition {
	if len(conds) == 0 {
		return nil
	}
	out := make([]domain.Condition, len(conds))
	for i, c := range conds {
		out[i] = cloneCondition(c)
	}
	return out
}

func cloneCondition(c domain.Condition) domain.Condition {
	c.Values = slices.Clone(c.Values)
	c.Conditions = cloneConditions(c.Conditions)
	return c
}

// Helper functions for creating conditions.
//
// Values are bound as parameters; wrap a value with Col to compare against a
// column instead (e.g. Equal("o.user_id", Col("u.id")) in a JOIN).

// Equal creates a "field = value" condition.
func Equal(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorEqual, value)
}

// NotEqual creates a "field != value" condition.
func NotEqual(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorNotEqual, value)
}

// GreaterThan creates a "field > value" condition.
func GreaterThan(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorGreaterThan, value)
}

// GreaterThanOrEqual creates a "field >= value" condition.
func GreaterThanOrEqual(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorGreaterThanOrEqual, value)
}

// LessThan creates a "field < value" condition.
func LessThan(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorLessThan, value)
}

// LessThanOrEqual creates a "field <= value" condition.
func LessThanOrEqual(field string, value interface{}) domain.Condition {
	return compare(field, domain.OperatorLessThanOrEqual, value)
}

// Like creates a "field LIKE pattern" condition.
func Like(field string, pattern string) domain.Condition {
	return compare(field, domain.OperatorLike, pattern)
}

// NotLike creates a "field NOT LIKE pattern" condition.
func NotLike(field string, pattern string) domain.Condition {
	return compare(field, domain.OperatorNotLike, pattern)
}

// In creates a "field IN (values...)" condition. With no values it matches no
// rows (rendered as 1=0).
func In(field string, values ...interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorIn, Values: values}
}

// NotIn creates a "field NOT IN (values...)" condition. With no values it matches
// every row (rendered as 1=1).
func NotIn(field string, values ...interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorNotIn, Values: values}
}

// Between creates a "field BETWEEN low AND high" condition.
func Between(field string, low, high interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorBetween, Values: []interface{}{low, high}}
}

// NotBetween creates a "field NOT BETWEEN low AND high" condition.
func NotBetween(field string, low, high interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorNotBetween, Values: []interface{}{low, high}}
}

// IsNull creates a "field IS NULL" condition.
func IsNull(field string) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorIsNull}
}

// IsNotNull creates a "field IS NOT NULL" condition.
func IsNotNull(field string) domain.Condition {
	return domain.Condition{Field: field, Operator: domain.OperatorIsNotNull}
}

// Exists creates an "EXISTS (subquery)" condition; the subquery's parameters are
// bound in place.
func Exists(subquery domain.QueryBuilder) domain.Condition {
	return domain.Condition{Operator: domain.OperatorExists, Value: subquery}
}

// NotExists creates a "NOT EXISTS (subquery)" condition.
func NotExists(subquery domain.QueryBuilder) domain.Condition {
	return domain.Condition{Operator: domain.OperatorNotExists, Value: subquery}
}

// And groups conditions as "(c1 AND c2 ...)". Building an empty group fails.
func And(conditions ...domain.Condition) domain.Condition {
	return domain.Condition{Operator: domain.OperatorAnd, Conditions: conditions}
}

// Or groups conditions as "(c1 OR c2 ...)". Building an empty group fails.
func Or(conditions ...domain.Condition) domain.Condition {
	return domain.Condition{Operator: domain.OperatorOr, Conditions: conditions}
}

// Col marks a condition value as a column reference, rendered as the (validated)
// column name instead of a bound parameter.
func Col(name string) domain.ColumnRef {
	return domain.ColumnRef(name)
}

func compare(field string, op domain.Operator, value interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: op, Value: value}
}
