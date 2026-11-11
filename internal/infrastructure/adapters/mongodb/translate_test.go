package mongodb

import (
	"regexp"
	"testing"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func toJSON(t *testing.T, doc bson.D) string {
	t.Helper()
	b, err := bson.MarshalExtJSON(doc, false, false)
	require.NoError(t, err)
	return string(b)
}

func cond(field string, op domain.Operator, value interface{}, values ...interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: op, Value: value, Values: values}
}

func group(op domain.Operator, conds ...domain.Condition) domain.Condition {
	return domain.Condition{Operator: op, Conditions: conds}
}

// fakeBuilder stands in for a subquery value; only its type matters.
type fakeBuilder struct{ domain.QueryBuilder }

func TestBuildFilter(t *testing.T) {
	tests := []struct {
		name     string
		conds    []domain.Condition
		expected string
	}{
		{"no conditions", nil, `{}`},
		{"equal", []domain.Condition{cond("name", domain.OperatorEqual, "Ann")}, `{"name":{"$eq":"Ann"}}`},
		{"not equal", []domain.Condition{cond("name", domain.OperatorNotEqual, "Ann")}, `{"name":{"$ne":"Ann"}}`},
		{"greater than", []domain.Condition{cond("age", domain.OperatorGreaterThan, 18)}, `{"age":{"$gt":18}}`},
		{"greater or equal", []domain.Condition{cond("age", domain.OperatorGreaterThanOrEqual, 18)}, `{"age":{"$gte":18}}`},
		{"less than", []domain.Condition{cond("age", domain.OperatorLessThan, 18)}, `{"age":{"$lt":18}}`},
		{"less or equal", []domain.Condition{cond("age", domain.OperatorLessThanOrEqual, 18)}, `{"age":{"$lte":18}}`},
		{"in", []domain.Condition{cond("id", domain.OperatorIn, nil, 1, 2, 3)}, `{"id":{"$in":[1,2,3]}}`},
		{"in without values matches nothing", []domain.Condition{cond("id", domain.OperatorIn, nil)}, `{"id":{"$in":[]}}`},
		{"not in", []domain.Condition{cond("id", domain.OperatorNotIn, nil, 1, 2)}, `{"id":{"$nin":[1,2]}}`},
		{"between", []domain.Condition{cond("age", domain.OperatorBetween, nil, 18, 65)}, `{"age":{"$gte":18,"$lte":65}}`},
		{
			"not between",
			[]domain.Condition{cond("age", domain.OperatorNotBetween, nil, 18, 65)},
			`{"$or":[{"age":{"$lt":18}},{"age":{"$gt":65}}]}`,
		},
		{"like", []domain.Condition{cond("name", domain.OperatorLike, "J_n%")}, `{"name":{"$regex":"^J.n.*\\z","$options":"s"}}`},
		{
			"not like",
			[]domain.Condition{cond("name", domain.OperatorNotLike, "J%")},
			`{"name":{"$not":{"$regex":"^J.*\\z","$options":"s"}}}`,
		},
		{"is null", []domain.Condition{cond("city", domain.OperatorIsNull, nil)}, `{"city":null}`},
		{"is not null", []domain.Condition{cond("city", domain.OperatorIsNotNull, nil)}, `{"city":{"$ne":null}}`},
		{
			"repeated field does not collide",
			[]domain.Condition{cond("age", domain.OperatorGreaterThan, 18), cond("age", domain.OperatorLessThan, 65)},
			`{"$and":[{"age":{"$gt":18}},{"age":{"$lt":65}}]}`,
		},
		{
			"nested AND inside OR",
			[]domain.Condition{group(domain.OperatorOr,
				group(domain.OperatorAnd, cond("a", domain.OperatorEqual, 1), cond("b", domain.OperatorEqual, 2)),
				cond("c", domain.OperatorEqual, 3),
			)},
			`{"$or":[{"$and":[{"a":{"$eq":1}},{"b":{"$eq":2}}]},{"c":{"$eq":3}}]}`,
		},
		{
			"OR group combined with a top-level condition",
			[]domain.Condition{
				cond("active", domain.OperatorEqual, true),
				group(domain.OperatorOr, cond("a", domain.OperatorEqual, 1), cond("b", domain.OperatorIsNull, nil)),
			},
			`{"$and":[{"active":{"$eq":true}},{"$or":[{"a":{"$eq":1}},{"b":null}]}]}`,
		},
		{"dotted path", []domain.Condition{cond("address.city", domain.OperatorEqual, "Oslo")}, `{"address.city":{"$eq":"Oslo"}}`},
		{
			"operator-like values are compared literally",
			[]domain.Condition{cond("a", domain.OperatorEqual, bson.M{"$gt": 1})},
			`{"a":{"$eq":{"$gt":1}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := buildFilter(tt.conds)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, toJSON(t, filter))
		})
	}
}

func TestBuildFilter_Errors(t *testing.T) {
	tests := []struct {
		name    string
		cond    domain.Condition
		wantErr error
	}{
		{"column reference", cond("a", domain.OperatorEqual, domain.ColumnRef("b")), domain.ErrUnsupportedQuery},
		{"column reference in values", cond("a", domain.OperatorIn, nil, 1, domain.ColumnRef("b")), domain.ErrUnsupportedQuery},
		{"subquery value", cond("a", domain.OperatorIn, fakeBuilder{}), domain.ErrUnsupportedQuery},
		{"exists", domain.Condition{Operator: domain.OperatorExists, Value: fakeBuilder{}}, domain.ErrUnsupportedQuery},
		{"not exists", domain.Condition{Operator: domain.OperatorNotExists, Value: fakeBuilder{}}, domain.ErrUnsupportedQuery},
		{"empty AND group", group(domain.OperatorAnd), domain.ErrInvalidQuery},
		{"empty OR group", group(domain.OperatorOr), domain.ErrInvalidQuery},
		{
			"error inside a group",
			group(domain.OperatorOr, cond("a", domain.OperatorEqual, 1), cond("b", domain.OperatorEqual, domain.ColumnRef("c"))),
			domain.ErrUnsupportedQuery,
		},
		{"between with one value", cond("age", domain.OperatorBetween, nil, 18), domain.ErrInvalidQuery},
		{"like with non-string", cond("name", domain.OperatorLike, 5), domain.ErrInvalidQuery},
		{"like ending in escape", cond("name", domain.OperatorLike, `abc\`), domain.ErrInvalidQuery},
		{"operator field name", cond("$where", domain.OperatorEqual, "sleep(1000)"), domain.ErrInvalidQuery},
		{"empty field name", cond("", domain.OperatorEqual, 1), domain.ErrInvalidQuery},
		{"SQL expression field", cond("LOWER(name)", domain.OperatorEqual, "ann"), domain.ErrUnsupportedQuery},
		{"unknown operator", cond("a", domain.Operator(99), 1), domain.ErrInvalidQuery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := buildFilter([]domain.Condition{tt.cond})
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, filter, "a failed translation must not produce a filter")
		})
	}
}

func TestLikeRegex(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
		matches  []string
		rejects  []string
	}{
		{"abc", `^abc\z`, []string{"abc"}, []string{"abcd", "xabc", "abc\n", "ABC"}},
		{"a%", `^a.*\z`, []string{"a", "abc", "a\nb"}, []string{"ba"}},
		{"_b_", `^.b.\z`, []string{"abc", "\nb\n"}, []string{"bc", "abcd"}},
		{"a.c", `^a\.c\z`, []string{"a.c"}, []string{"abc"}},
		{"(x|y)*+?[]{}^$", `^\(x\|y\)\*\+\?\[\]\{\}\^\$\z`, []string{"(x|y)*+?[]{}^$"}, []string{"x"}},
		{`100\%`, `^100%\z`, []string{"100%"}, []string{"1000"}},
		{`a\_b`, `^a_b\z`, []string{"a_b"}, []string{"axb"}},
		{`a\\b`, `^a\\b\z`, []string{`a\b`}, []string{"ab"}},
		{"é_ü%", `^é.ü.*\z`, []string{"éxü", "éxüyz"}, []string{"exu"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := likeRegex(tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, re)

			// The "s" option the adapter sends is (?s) here.
			compiled := regexp.MustCompile("(?s)" + re)
			for _, s := range tt.matches {
				assert.True(t, compiled.MatchString(s), "%q should match %q", tt.pattern, s)
			}
			for _, s := range tt.rejects {
				assert.False(t, compiled.MatchString(s), "%q should not match %q", tt.pattern, s)
			}
		})
	}
}

func TestPlanQuery(t *testing.T) {
	where := []domain.Condition{cond("active", domain.OperatorEqual, true)}

	tests := []struct {
		name       string
		spec       domain.QuerySpec
		kind       queryKind
		columns    []string
		filter     string
		projection string
	}{
		{name: "all fields", spec: domain.QuerySpec{Table: "t"}, filter: `{}`},
		{name: "star", spec: domain.QuerySpec{Table: "t", Fields: []string{"*"}}, filter: `{}`},
		{
			name:       "projection excludes _id",
			spec:       domain.QuerySpec{Table: "t", Fields: []string{"name", "age"}},
			columns:    []string{"name", "age"},
			filter:     `{}`,
			projection: `{"name":1,"age":1,"_id":0}`,
		},
		{
			name:       "projection with _id",
			spec:       domain.QuerySpec{Table: "t", Fields: []string{"_id", "name"}},
			columns:    []string{"_id", "name"},
			filter:     `{}`,
			projection: `{"_id":1,"name":1}`,
		},
		{
			name:       "repeated field is projected once",
			spec:       domain.QuerySpec{Table: "t", Fields: []string{"name", "name"}},
			columns:    []string{"name", "name"},
			filter:     `{}`,
			projection: `{"name":1,"_id":0}`,
		},
		{
			name:    "count star",
			spec:    domain.QuerySpec{Table: "t", Fields: []string{"COUNT(*)"}, Where: where},
			kind:    countQuery,
			columns: []string{"count"},
			filter:  `{"active":{"$eq":true}}`,
		},
		{
			name:    "count field skips nulls",
			spec:    domain.QuerySpec{Table: "t", Fields: []string{"count( email )"}, Where: where},
			kind:    countQuery,
			columns: []string{"count"},
			filter:  `{"$and":[{"active":{"$eq":true}},{"email":{"$ne":null}}]}`,
		},
		{
			name:    "count field without conditions",
			spec:    domain.QuerySpec{Table: "t", Fields: []string{"COUNT(email)"}},
			kind:    countQuery,
			columns: []string{"count"},
			filter:  `{"email":{"$ne":null}}`,
		},
		{
			name:    "distinct",
			spec:    domain.QuerySpec{Table: "t", Fields: []string{"city"}, Distinct: true},
			kind:    distinctQuery,
			columns: []string{"city"},
			filter:  `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			p, err := planQuery(domain.Query{Spec: &spec})
			require.NoError(t, err)
			assert.Equal(t, tt.kind, p.kind)
			assert.Equal(t, "t", p.collection)
			assert.Equal(t, tt.columns, p.columns)
			assert.Equal(t, tt.filter, toJSON(t, p.filter))
			if tt.projection == "" {
				assert.Nil(t, p.projection)
			} else {
				assert.Equal(t, tt.projection, toJSON(t, p.projection))
			}
		})
	}
}

func TestPlanQuery_Pagination(t *testing.T) {
	p, err := planQuery(domain.Query{Spec: &domain.QuerySpec{
		Table:   "t",
		OrderBy: []domain.OrderBy{{Field: "age", Direction: domain.SortDirectionDesc}, {Field: "name", Direction: domain.SortDirectionAsc}},
		Offset:  intPtr(5),
		Limit:   intPtr(10),
	}})
	require.NoError(t, err)
	assert.Equal(t, `{"age":-1,"name":1}`, toJSON(t, p.sort))
	assert.Equal(t, int64(5), p.skip)
	assert.Equal(t, int64(10), p.limit)
	assert.False(t, p.none)

	p, err = planQuery(domain.Query{Spec: &domain.QuerySpec{Table: "t", Limit: intPtr(0)}})
	require.NoError(t, err)
	assert.True(t, p.none, "SQL LIMIT 0 returns no rows, unlike MongoDB's limit(0)")
}

func TestPlanQuery_Errors(t *testing.T) {
	tests := []struct {
		name    string
		spec    *domain.QuerySpec
		wantErr error
	}{
		{"raw SQL", nil, domain.ErrUnsupportedQuery},
		{"join", &domain.QuerySpec{Table: "t", Joins: []domain.Join{{Table: "u"}}}, domain.ErrUnsupportedQuery},
		{"group by", &domain.QuerySpec{Table: "t", GroupBy: []string{"city"}}, domain.ErrUnsupportedQuery},
		{"having", &domain.QuerySpec{Table: "t", Having: []domain.Condition{cond("n", domain.OperatorGreaterThan, 1)}}, domain.ErrUnsupportedQuery},
		{"subquery source", &domain.QuerySpec{Table: "sub", Subquery: true}, domain.ErrUnsupportedQuery},
		{"no table", &domain.QuerySpec{}, domain.ErrInvalidQuery},
		{"other aggregate", &domain.QuerySpec{Table: "t", Fields: []string{"SUM(age)"}}, domain.ErrUnsupportedQuery},
		{"count with other fields", &domain.QuerySpec{Table: "t", Fields: []string{"city", "COUNT(*)"}}, domain.ErrUnsupportedQuery},
		{"count distinct", &domain.QuerySpec{Table: "t", Fields: []string{"COUNT(DISTINCT city)"}}, domain.ErrUnsupportedQuery},
		{"distinct count", &domain.QuerySpec{Table: "t", Fields: []string{"COUNT(*)"}, Distinct: true}, domain.ErrUnsupportedQuery},
		{"alias", &domain.QuerySpec{Table: "t", Fields: []string{"name AS n"}}, domain.ErrUnsupportedQuery},
		{"star with fields", &domain.QuerySpec{Table: "t", Fields: []string{"*", "name"}}, domain.ErrUnsupportedQuery},
		{"distinct on two fields", &domain.QuerySpec{Table: "t", Fields: []string{"a", "b"}, Distinct: true}, domain.ErrUnsupportedQuery},
		{"distinct on all fields", &domain.QuerySpec{Table: "t", Distinct: true}, domain.ErrUnsupportedQuery},
		{
			"distinct with order",
			&domain.QuerySpec{Table: "t", Fields: []string{"a"}, Distinct: true, OrderBy: []domain.OrderBy{{Field: "a"}}},
			domain.ErrUnsupportedQuery,
		},
		{"count with limit", &domain.QuerySpec{Table: "t", Fields: []string{"COUNT(*)"}, Limit: intPtr(1)}, domain.ErrUnsupportedQuery},
		{"bad condition", &domain.QuerySpec{Table: "t", Where: []domain.Condition{group(domain.OperatorOr)}}, domain.ErrInvalidQuery},
		{"bad sort field", &domain.QuerySpec{Table: "t", OrderBy: []domain.OrderBy{{Field: "$x"}}}, domain.ErrInvalidQuery},
		{"bad sort direction", &domain.QuerySpec{Table: "t", OrderBy: []domain.OrderBy{{Field: "x", Direction: 7}}}, domain.ErrInvalidQuery},
		{"negative limit", &domain.QuerySpec{Table: "t", Limit: intPtr(-1)}, domain.ErrInvalidQuery},
		{"negative offset", &domain.QuerySpec{Table: "t", Offset: intPtr(-1)}, domain.ErrInvalidQuery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := planQuery(domain.Query{SQL: "SELECT 1", Spec: tt.spec})
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, p)
		})
	}
}

func TestPlanCommand(t *testing.T) {
	where := []domain.Condition{cond("id", domain.OperatorEqual, 1)}
	values := []domain.FieldValue{{Field: "name", Value: "Ann"}, {Field: "age", Value: 30}}

	p, err := planCommand(domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: "t", Values: values}})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"Ann","age":30}`, toJSON(t, p.document), "insert keeps value order")

	p, err = planCommand(domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{Table: "t", Values: values, Where: where}})
	require.NoError(t, err)
	assert.Equal(t, `{"id":{"$eq":1}}`, toJSON(t, p.filter))
	assert.Equal(t, `{"$set":{"name":"Ann","age":30}}`, toJSON(t, p.update()))

	p, err = planCommand(domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{Table: "t", Where: where}})
	require.NoError(t, err)
	assert.Equal(t, `{"id":{"$eq":1}}`, toJSON(t, p.filter))

	// Like SQL, a DELETE without WHERE deletes every row.
	p, err = planCommand(domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{Table: "t"}})
	require.NoError(t, err)
	assert.Equal(t, `{}`, toJSON(t, p.filter))
}

func TestPlanCommand_Errors(t *testing.T) {
	values := []domain.FieldValue{{Field: "name", Value: "Ann"}}
	badConditions := []domain.Condition{
		cond("id", domain.OperatorEqual, domain.ColumnRef("other")),
		{Operator: domain.OperatorExists, Value: fakeBuilder{}},
		group(domain.OperatorOr),
		cond("$id", domain.OperatorEqual, 1),
		cond("id", domain.OperatorBetween, nil, 1),
	}

	// An UPDATE or DELETE whose conditions cannot be translated must fail
	// rather than run with an empty filter that matches every document.
	for _, bad := range badConditions {
		for _, typ := range []domain.CommandType{domain.CommandTypeUpdate, domain.CommandTypeDelete} {
			spec := &domain.CommandSpec{Table: "t", Where: []domain.Condition{cond("ok", domain.OperatorEqual, 1), bad}}
			if typ == domain.CommandTypeUpdate {
				spec.Values = values
			}
			p, err := planCommand(domain.Command{Type: typ, Spec: spec})
			assert.Error(t, err, "type %d with %+v", typ, bad)
			assert.Nil(t, p)
		}
	}

	tests := []struct {
		name    string
		command domain.Command
		wantErr error
	}{
		{"raw SQL", domain.Command{SQL: "DELETE FROM t", Type: domain.CommandTypeDelete}, domain.ErrUnsupportedQuery},
		{"no table", domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{}}, domain.ErrInvalidQuery},
		{"insert without values", domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: "t"}}, domain.ErrInvalidQuery},
		{
			"insert with where",
			domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: "t", Values: values, Where: []domain.Condition{cond("a", domain.OperatorEqual, 1)}}},
			domain.ErrInvalidQuery,
		},
		{
			"duplicate field",
			domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: "t", Values: append(values, values...)}},
			domain.ErrInvalidQuery,
		},
		{
			"column reference value",
			domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{Table: "t", Values: []domain.FieldValue{{Field: "a", Value: domain.ColumnRef("b")}}}},
			domain.ErrUnsupportedQuery,
		},
		{
			"operator field name",
			domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: "t", Values: []domain.FieldValue{{Field: "$set", Value: 1}}}},
			domain.ErrInvalidQuery,
		},
		{"update without values", domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{Table: "t"}}, domain.ErrInvalidQuery},
		{"delete with values", domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{Table: "t", Values: values}}, domain.ErrInvalidQuery},
		{"bulk insert", domain.Command{Type: domain.CommandTypeBulkInsert, Spec: &domain.CommandSpec{Table: "t", Values: values}}, domain.ErrUnsupportedQuery},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := planCommand(tt.command)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, p)
		})
	}
}

func TestRender_EncodingError(t *testing.T) {
	p, err := planQuery(domain.Query{Spec: &domain.QuerySpec{
		Table: "t",
		Where: []domain.Condition{cond("a", domain.OperatorEqual, make(chan int))},
	}})
	require.NoError(t, err)
	_, err = p.render()
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
}

func intPtr(i int) *int { return &i }
