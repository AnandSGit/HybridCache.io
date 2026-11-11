package mongodb

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// countExpr matches the select expression emitted by QueryBuilder.SelectCount.
var countExpr = regexp.MustCompile(`(?i)^COUNT\(\s*([^()\s]+)\s*\)$`)

// countColumn names the single column of a COUNT result.
const countColumn = "count"

// comparisonOps maps single-value comparison operators to MongoDB query operators.
var comparisonOps = map[domain.Operator]string{
	domain.OperatorEqual:              "$eq",
	domain.OperatorNotEqual:           "$ne",
	domain.OperatorGreaterThan:        "$gt",
	domain.OperatorGreaterThanOrEqual: "$gte",
	domain.OperatorLessThan:           "$lt",
	domain.OperatorLessThanOrEqual:    "$lte",
	domain.OperatorIn:                 "$in",
	domain.OperatorNotIn:              "$nin",
}

type queryKind int

const (
	findQuery queryKind = iota
	countQuery
	distinctQuery
)

// queryPlan is the MongoDB form of a domain.QuerySpec.
type queryPlan struct {
	kind       queryKind
	collection string
	filter     bson.D
	columns    []string // result columns in select order; nil means whole documents
	projection bson.D
	sort       bson.D
	skip       int64
	limit      int64 // 0 means no limit
	none       bool  // LIMIT 0: the result is empty and nothing is sent to the server
}

// commandPlan is the MongoDB form of a domain.CommandSpec.
type commandPlan struct {
	kind       domain.CommandType
	collection string
	document   bson.D // insert: the new document; update: the values to $set
	filter     bson.D // update and delete
}

func unsupported(feature string) error {
	return domain.NewUnsupportedQueryError(feature + " is not supported by the MongoDB adapter")
}

// planQuery translates a builder-produced query. MongoDB cannot execute SQL text,
// so a query without a Spec is rejected rather than parsed.
func planQuery(q domain.Query) (*queryPlan, error) {
	spec := q.Spec
	if spec == nil {
		return nil, domain.NewUnsupportedQueryError(
			"MongoDB requires a query built with the query builder (Query.Spec); raw SQL cannot be executed")
	}
	switch {
	case len(spec.Joins) > 0:
		return nil, unsupported("JOIN")
	case len(spec.GroupBy) > 0:
		return nil, unsupported("GROUP BY")
	case len(spec.Having) > 0:
		return nil, unsupported("HAVING")
	case spec.Subquery:
		return nil, unsupported("selecting from a subquery")
	case spec.Table == "":
		return nil, domain.NewInvalidQueryError("query has no table")
	}

	filter, err := buildFilter(spec.Where)
	if err != nil {
		return nil, err
	}
	p := &queryPlan{collection: spec.Table, filter: filter}
	if err := p.selectFields(spec); err != nil {
		return nil, err
	}
	if p.kind != findQuery && (len(spec.OrderBy) > 0 || spec.Limit != nil || spec.Offset != nil) {
		return nil, unsupported("ORDER BY, LIMIT or OFFSET combined with COUNT or DISTINCT")
	}
	if err := p.paginate(spec); err != nil {
		return nil, err
	}
	return p, nil
}

// selectFields sets the plan kind, result columns and projection from the select list.
func (p *queryPlan) selectFields(spec *domain.QuerySpec) error {
	fields := spec.Fields
	if len(fields) == 1 && fields[0] == "*" {
		fields = nil
	}
	if len(fields) == 1 {
		if m := countExpr.FindStringSubmatch(fields[0]); m != nil {
			if spec.Distinct {
				return unsupported("DISTINCT COUNT")
			}
			p.kind, p.columns = countQuery, []string{countColumn}
			if m[1] != "*" {
				// COUNT(field) counts the documents whose field is not null.
				if err := checkField(m[1]); err != nil {
					return err
				}
				p.filter = and(p.filter, bson.D{{Key: m[1], Value: bson.D{{Key: "$ne", Value: nil}}}})
			}
			return nil
		}
	}
	for _, f := range fields {
		if err := checkField(f); err != nil {
			return err
		}
	}
	if spec.Distinct {
		if len(fields) != 1 {
			return unsupported("DISTINCT on other than exactly one field")
		}
		p.kind, p.columns = distinctQuery, fields
		return nil
	}
	if len(fields) == 0 {
		return nil
	}

	p.columns = fields
	p.projection = make(bson.D, 0, len(fields)+1)
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !seen[f] {
			seen[f] = true
			p.projection = append(p.projection, bson.E{Key: f, Value: 1})
		}
	}
	if !seen["_id"] {
		// Like SQL, return only the selected columns.
		p.projection = append(p.projection, bson.E{Key: "_id", Value: 0})
	}
	return nil
}

func (p *queryPlan) paginate(spec *domain.QuerySpec) error {
	for _, o := range spec.OrderBy {
		if err := checkField(o.Field); err != nil {
			return err
		}
		var dir int
		switch o.Direction {
		case domain.SortDirectionAsc:
			dir = 1
		case domain.SortDirectionDesc:
			dir = -1
		default:
			return domain.NewInvalidQueryError(fmt.Sprintf("unknown sort direction %d", o.Direction))
		}
		p.sort = append(p.sort, bson.E{Key: o.Field, Value: dir})
	}
	if spec.Offset != nil {
		if *spec.Offset < 0 {
			return domain.NewInvalidQueryError("OFFSET must not be negative")
		}
		p.skip = int64(*spec.Offset)
	}
	if spec.Limit != nil {
		if *spec.Limit < 0 {
			return domain.NewInvalidQueryError("LIMIT must not be negative")
		}
		// MongoDB treats limit 0 as "no limit"; SQL LIMIT 0 returns no rows.
		p.limit = int64(*spec.Limit)
		p.none = *spec.Limit == 0
	}
	return nil
}

func (p *queryPlan) findOptions() *options.FindOptionsBuilder {
	o := options.Find()
	if p.projection != nil {
		o.SetProjection(p.projection)
	}
	if p.sort != nil {
		o.SetSort(p.sort)
	}
	if p.skip > 0 {
		o.SetSkip(p.skip)
	}
	if p.limit > 0 {
		o.SetLimit(p.limit)
	}
	return o
}

func (p *queryPlan) findOneOptions() *options.FindOneOptionsBuilder {
	o := options.FindOne()
	if p.projection != nil {
		o.SetProjection(p.projection)
	}
	if p.sort != nil {
		o.SetSort(p.sort)
	}
	if p.skip > 0 {
		o.SetSkip(p.skip)
	}
	return o
}

// planCommand translates a builder-produced command. Translation failures are
// returned as errors, so an UPDATE or DELETE can never broaden to every document.
func planCommand(c domain.Command) (*commandPlan, error) {
	spec := c.Spec
	if spec == nil {
		return nil, domain.NewUnsupportedQueryError(
			"MongoDB requires a command built with the command builder (Command.Spec); raw SQL cannot be executed")
	}
	if spec.Table == "" {
		return nil, domain.NewInvalidQueryError("command has no table")
	}

	p := &commandPlan{kind: c.Type, collection: spec.Table}
	var err error
	switch c.Type {
	case domain.CommandTypeInsert:
		if len(spec.Where) > 0 {
			return nil, domain.NewInvalidQueryError("INSERT does not take WHERE conditions")
		}
		p.document, err = buildDocument(spec.Values)
	case domain.CommandTypeUpdate:
		if p.document, err = buildDocument(spec.Values); err == nil {
			p.filter, err = buildFilter(spec.Where)
		}
	case domain.CommandTypeDelete:
		if len(spec.Values) > 0 {
			return nil, domain.NewInvalidQueryError("DELETE does not take values")
		}
		p.filter, err = buildFilter(spec.Where)
	default:
		return nil, unsupported(fmt.Sprintf("command type %d", c.Type))
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func buildDocument(values []domain.FieldValue) (bson.D, error) {
	if len(values) == 0 {
		return nil, domain.NewInvalidQueryError("command has no values")
	}
	doc := make(bson.D, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if err := checkField(v.Field); err != nil {
			return nil, err
		}
		if err := checkValue(v.Value); err != nil {
			return nil, err
		}
		if seen[v.Field] {
			return nil, domain.NewInvalidQueryError(fmt.Sprintf("field %q is assigned more than once", v.Field))
		}
		seen[v.Field] = true
		doc = append(doc, bson.E{Key: v.Field, Value: v.Value})
	}
	return doc, nil
}

// buildFilter translates AND-combined conditions into a filter document. Several
// conditions become an explicit $and so that repeated fields cannot collide.
func buildFilter(conds []domain.Condition) (bson.D, error) {
	parts, err := translateAll(conds)
	if err != nil {
		return nil, err
	}
	switch len(parts) {
	case 0:
		return bson.D{}, nil
	case 1:
		return parts[0], nil
	}
	return bson.D{{Key: "$and", Value: parts}}, nil
}

func translateAll(conds []domain.Condition) ([]bson.D, error) {
	parts := make([]bson.D, 0, len(conds))
	for _, c := range conds {
		f, err := translateCondition(c)
		if err != nil {
			return nil, err
		}
		parts = append(parts, f)
	}
	return parts, nil
}

func translateCondition(c domain.Condition) (bson.D, error) {
	switch c.Operator {
	case domain.OperatorAnd, domain.OperatorOr:
		if len(c.Conditions) == 0 {
			return nil, domain.NewInvalidQueryError("empty AND/OR condition group")
		}
		parts, err := translateAll(c.Conditions)
		if err != nil {
			return nil, err
		}
		op := "$and"
		if c.Operator == domain.OperatorOr {
			op = "$or"
		}
		return bson.D{{Key: op, Value: parts}}, nil
	case domain.OperatorExists, domain.OperatorNotExists:
		return nil, unsupported("EXISTS subqueries")
	}

	if err := checkField(c.Field); err != nil {
		return nil, err
	}
	if err := checkValue(c.Value); err != nil {
		return nil, err
	}
	for _, v := range c.Values {
		if err := checkValue(v); err != nil {
			return nil, err
		}
	}

	f := c.Field
	if op, ok := comparisonOps[c.Operator]; ok {
		if c.Operator == domain.OperatorIn || c.Operator == domain.OperatorNotIn {
			return fieldOp(f, op, append(bson.A{}, c.Values...)), nil
		}
		return fieldOp(f, op, c.Value), nil
	}
	switch c.Operator {
	case domain.OperatorBetween, domain.OperatorNotBetween:
		if len(c.Values) != 2 {
			return nil, domain.NewInvalidQueryError(fmt.Sprintf("BETWEEN on %q needs exactly two values", f))
		}
		lo, hi := c.Values[0], c.Values[1]
		if c.Operator == domain.OperatorBetween {
			return bson.D{{Key: f, Value: bson.D{{Key: "$gte", Value: lo}, {Key: "$lte", Value: hi}}}}, nil
		}
		return bson.D{{Key: "$or", Value: bson.A{fieldOp(f, "$lt", lo), fieldOp(f, "$gt", hi)}}}, nil
	case domain.OperatorLike, domain.OperatorNotLike:
		pattern, ok := c.Value.(string)
		if !ok {
			return nil, domain.NewInvalidQueryError(fmt.Sprintf("LIKE on %q needs a string pattern", f))
		}
		re, err := likeRegex(pattern)
		if err != nil {
			return nil, err
		}
		// "s" lets '.' match newlines, as SQL wildcards do.
		match := bson.D{{Key: "$regex", Value: re}, {Key: "$options", Value: "s"}}
		if c.Operator == domain.OperatorLike {
			return bson.D{{Key: f, Value: match}}, nil
		}
		return fieldOp(f, "$not", match), nil
	case domain.OperatorIsNull:
		// Matches null and missing fields, the document equivalent of SQL NULL.
		return bson.D{{Key: f, Value: nil}}, nil
	case domain.OperatorIsNotNull:
		return fieldOp(f, "$ne", nil), nil
	}
	return nil, domain.NewInvalidQueryError(fmt.Sprintf("unknown operator %d", c.Operator))
}

func fieldOp(field, op string, value interface{}) bson.D {
	return bson.D{{Key: field, Value: bson.D{{Key: op, Value: value}}}}
}

func and(filter, extra bson.D) bson.D {
	if len(filter) == 0 {
		return extra
	}
	return bson.D{{Key: "$and", Value: bson.A{filter, extra}}}
}

// likeRegex converts a SQL LIKE pattern into an anchored regular expression:
// '%' matches any sequence, '_' any single character, and '\' escapes the next
// character (PostgreSQL's default escape). Everything else matches literally.
func likeRegex(pattern string) (string, error) {
	var b strings.Builder
	b.WriteString("^")
	escaped := false
	for _, r := range pattern {
		switch {
		case escaped:
			b.WriteString(regexp.QuoteMeta(string(r)))
			escaped = false
		case r == '\\':
			escaped = true
		case r == '%':
			b.WriteString(".*")
		case r == '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	if escaped {
		return "", domain.NewInvalidQueryError("LIKE pattern must not end with the escape character")
	}
	// \z rather than $, which would also match before a trailing newline.
	b.WriteString(`\z`)
	return b.String(), nil
}

// checkField accepts plain (optionally dotted) field paths. SQL expressions such
// as functions, aliases and wildcards have no MongoDB translation, and '$'
// prefixes would be interpreted as operators.
func checkField(name string) error {
	switch {
	case name == "":
		return domain.NewInvalidQueryError("empty field name")
	case strings.HasPrefix(name, "$"):
		return domain.NewInvalidQueryError(fmt.Sprintf("field name %q must not start with '$'", name))
	case strings.ContainsAny(name, " \t\r\n()*,'\"\x00"):
		return domain.NewUnsupportedQueryError(fmt.Sprintf(
			"expression %q is not supported by the MongoDB adapter: only plain field paths are", name))
	}
	return nil
}

func checkValue(v interface{}) error {
	switch v.(type) {
	case domain.ColumnRef:
		return unsupported("comparing with a column reference")
	case domain.QueryBuilder:
		return unsupported("subquery values")
	}
	return nil
}

// render returns a deterministic mongosh-style rendering of the plan.
func (p *queryPlan) render() (string, error) {
	filter, err := extJSON(p.filter)
	if err != nil {
		return "", err
	}
	coll := "db.getCollection(" + strconv.Quote(p.collection) + ")"
	switch p.kind {
	case countQuery:
		return coll + ".countDocuments(" + filter + ")", nil
	case distinctQuery:
		return coll + ".distinct(" + strconv.Quote(p.columns[0]) + ", " + filter + ")", nil
	}

	var b strings.Builder
	b.WriteString(coll + ".find(" + filter)
	if p.projection != nil {
		proj, err := extJSON(p.projection)
		if err != nil {
			return "", err
		}
		b.WriteString(", " + proj)
	}
	b.WriteString(")")
	if p.sort != nil {
		sort, err := extJSON(p.sort)
		if err != nil {
			return "", err
		}
		b.WriteString(".sort(" + sort + ")")
	}
	if p.skip > 0 {
		fmt.Fprintf(&b, ".skip(%d)", p.skip)
	}
	if p.limit > 0 {
		fmt.Fprintf(&b, ".limit(%d)", p.limit)
	}
	if p.none {
		b.WriteString(" /* LIMIT 0: no documents are read */")
	}
	return b.String(), nil
}

// render returns a deterministic mongosh-style rendering of the plan.
func (p *commandPlan) render() (string, error) {
	coll := "db.getCollection(" + strconv.Quote(p.collection) + ")"
	switch p.kind {
	case domain.CommandTypeInsert:
		doc, err := extJSON(p.document)
		if err != nil {
			return "", err
		}
		return coll + ".insertOne(" + doc + ")", nil
	case domain.CommandTypeUpdate:
		filter, err := extJSON(p.filter)
		if err != nil {
			return "", err
		}
		update, err := extJSON(p.update())
		if err != nil {
			return "", err
		}
		return coll + ".updateMany(" + filter + ", " + update + ")", nil
	}
	filter, err := extJSON(p.filter)
	if err != nil {
		return "", err
	}
	return coll + ".deleteMany(" + filter + ")", nil
}

func (p *commandPlan) update() bson.D {
	return bson.D{{Key: "$set", Value: p.document}}
}

func extJSON(doc bson.D) (string, error) {
	b, err := bson.MarshalExtJSON(doc, false, false)
	if err != nil {
		return "", domain.NewInvalidQueryError("value cannot be encoded as BSON: " + err.Error()).WithCause(err)
	}
	return string(b), nil
}
