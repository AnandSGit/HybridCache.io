package query_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/application/query"
	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

func intPtr(v int) *int { return &v }

func condPtr(c domain.Condition) *domain.Condition { return &c }

// rawBuilder is a domain.QueryBuilder that is not a *query.Builder. Only
// BuildRaw is implemented; the embedded nil interface is never called.
type rawBuilder struct {
	domain.QueryBuilder
	sql    string
	params []interface{}
	err    error
}

func (r rawBuilder) BuildRaw() (string, []interface{}, error) { return r.sql, r.params, r.err }

// complexQuery mirrors the "users with order counts" example query.
func complexQuery() domain.QueryBuilder {
	return query.NewBuilder().
		Select("u.name", "u.email", "COUNT(o.id) as order_count").
		From("users u").
		LeftJoin("orders o", query.Equal("o.user_id", query.Col("u.id"))).
		Where(query.Equal("u.active", true)).
		WhereOr(query.GreaterThan("u.age", 18), query.IsNotNull("u.guardian_id")).
		GroupBy("u.id", "u.name", "u.email").
		Having(query.GreaterThan("COUNT(o.id)", 0)).
		OrderByDesc("order_count").
		Limit(50).
		Offset(100)
}

const complexSQL = "SELECT u.name, u.email, COUNT(o.id) as order_count FROM users u " +
	"LEFT JOIN orders o ON o.user_id = u.id " +
	"WHERE u.active = ? AND (u.age > ? OR u.guardian_id IS NOT NULL) " +
	"GROUP BY u.id, u.name, u.email HAVING COUNT(o.id) > ? " +
	"ORDER BY order_count DESC LIMIT 50 OFFSET 100"

func complexSpec() domain.QuerySpec {
	return domain.QuerySpec{
		Table:  "users",
		Fields: []string{"u.name", "u.email", "COUNT(o.id) as order_count"},
		Joins: []domain.Join{{
			Type:  domain.JoinTypeLeft,
			Table: "orders o",
			On:    condPtr(query.Equal("o.user_id", query.Col("u.id"))),
		}},
		Where: []domain.Condition{
			query.Equal("u.active", true),
			query.Or(query.GreaterThan("u.age", 18), query.IsNotNull("u.guardian_id")),
		},
		GroupBy: []string{"u.id", "u.name", "u.email"},
		Having:  []domain.Condition{query.GreaterThan("COUNT(o.id)", 0)},
		OrderBy: []domain.OrderBy{{Field: "order_count", Direction: domain.SortDirectionDesc}},
		Limit:   intPtr(50),
		Offset:  intPtr(100),
	}
}

func TestQueryBuilder_Build(t *testing.T) {
	sub := query.NewBuilder().Select("user_id").From("orders").Where(query.GreaterThan("total", 100))
	orders := query.NewBuilder().Select("o.id").From("orders o").
		Where(query.Equal("o.user_id", query.Col("u.id"))).
		Where(query.GreaterThan("o.total", 50))
	bans := query.NewBuilder().From("bans b").
		Where(query.Equal("b.user_id", query.Col("u.id"))).
		Where(query.Equal("b.kind", "hard"))

	tests := []struct {
		name       string
		build      func() domain.QueryBuilder
		wantSQL    string
		wantParams []interface{}
		wantSpec   domain.QuerySpec
	}{
		{
			name:     "no fields selects star",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users") },
			wantSQL:  "SELECT * FROM users",
			wantSpec: domain.QuerySpec{Table: "users"},
		},
		{
			name:     "explicit star leaves Spec.Fields empty",
			build:    func() domain.QueryBuilder { return query.NewBuilder().Select("*").From("users") },
			wantSQL:  "SELECT * FROM users",
			wantSpec: domain.QuerySpec{Table: "users"},
		},
		{
			name: "fields accumulate in order",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().Select("id", "name").Select("email").From("users")
			},
			wantSQL:  "SELECT id, name, email FROM users",
			wantSpec: domain.QuerySpec{Table: "users", Fields: []string{"id", "name", "email"}},
		},
		{
			name:     "select distinct",
			build:    func() domain.QueryBuilder { return query.NewBuilder().SelectDistinct("category").From("products") },
			wantSQL:  "SELECT DISTINCT category FROM products",
			wantSpec: domain.QuerySpec{Table: "products", Fields: []string{"category"}, Distinct: true},
		},
		{
			name:     "select count all",
			build:    func() domain.QueryBuilder { return query.NewBuilder().SelectCount("").From("users") },
			wantSQL:  "SELECT COUNT(*) FROM users",
			wantSpec: domain.QuerySpec{Table: "users", Fields: []string{"COUNT(*)"}},
		},
		{
			name:     "select count field",
			build:    func() domain.QueryBuilder { return query.NewBuilder().SelectCount("id").From("users") },
			wantSQL:  "SELECT COUNT(id) FROM users",
			wantSpec: domain.QuerySpec{Table: "users", Fields: []string{"COUNT(id)"}},
		},
		{
			name: "select expressions with aliases",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().
					Select("u.*", "u.name AS user_name", "COUNT(o.id) as order_count", "sum(o.total) AS total", "MAX(o.created_at)", "AVG(*)").
					From("users u")
			},
			wantSQL: "SELECT u.*, u.name AS user_name, COUNT(o.id) as order_count, sum(o.total) AS total, MAX(o.created_at), AVG(*) FROM users u",
			wantSpec: domain.QuerySpec{
				Table:  "users",
				Fields: []string{"u.*", "u.name AS user_name", "COUNT(o.id) as order_count", "sum(o.total) AS total", "MAX(o.created_at)", "AVG(*)"},
			},
		},
		{
			name:     "table alias with AS",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users AS u") },
			wantSQL:  "SELECT * FROM users AS u",
			wantSpec: domain.QuerySpec{Table: "users"},
		},
		{
			name:     "schema-qualified table with alias",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("public.users pu") },
			wantSQL:  "SELECT * FROM public.users pu",
			wantSpec: domain.QuerySpec{Table: "public.users"},
		},
		{
			name: "where conditions are combined with AND",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users").
					Where(query.Equal("active", true)).
					Where(query.GreaterThan("age", 18)).
					WhereAnd(query.LessThan("age", 65), query.IsNotNull("email"))
			},
			wantSQL:    "SELECT * FROM users WHERE active = ? AND age > ? AND age < ? AND email IS NOT NULL",
			wantParams: []interface{}{true, 18, 65},
			wantSpec: domain.QuerySpec{Table: "users", Where: []domain.Condition{
				query.Equal("active", true),
				query.GreaterThan("age", 18),
				query.LessThan("age", 65),
				query.IsNotNull("email"),
			}},
		},
		{
			name: "WhereOr adds one parenthesised OR group",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users").
					Where(query.Equal("active", true)).
					WhereOr(query.Equal("role", "admin"), query.Equal("role", "owner"))
			},
			wantSQL:    "SELECT * FROM users WHERE active = ? AND (role = ? OR role = ?)",
			wantParams: []interface{}{true, "admin", "owner"},
			wantSpec: domain.QuerySpec{Table: "users", Where: []domain.Condition{
				query.Equal("active", true),
				{Operator: domain.OperatorOr, Conditions: []domain.Condition{
					query.Equal("role", "admin"),
					query.Equal("role", "owner"),
				}},
			}},
		},
		{
			name: "each WhereOr call is a separate group",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("t").
					WhereOr(query.Equal("a", 1), query.Equal("b", 2)).
					WhereOr(query.Equal("c", 3), query.Equal("d", 4))
			},
			wantSQL:    "SELECT * FROM t WHERE (a = ? OR b = ?) AND (c = ? OR d = ?)",
			wantParams: []interface{}{1, 2, 3, 4},
			wantSpec: domain.QuerySpec{Table: "t", Where: []domain.Condition{
				query.Or(query.Equal("a", 1), query.Equal("b", 2)),
				query.Or(query.Equal("c", 3), query.Equal("d", 4)),
			}},
		},
		{
			name: "WhereOr with a single condition is still a group",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("t").WhereOr(query.Equal("a", 1))
			},
			wantSQL:    "SELECT * FROM t WHERE (a = ?)",
			wantParams: []interface{}{1},
			wantSpec:   domain.QuerySpec{Table: "t", Where: []domain.Condition{query.Or(query.Equal("a", 1))}},
		},
		{
			name:     "WhereOr without conditions is a no-op",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("t").WhereOr() },
			wantSQL:  "SELECT * FROM t",
			wantSpec: domain.QuerySpec{Table: "t"},
		},
		{
			name: "groups nest and are parenthesised",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("t").Where(query.Or(
					query.Equal("a", 1),
					query.And(query.Equal("b", 2), query.Or(query.Equal("c", 3), query.IsNull("d"))),
				))
			},
			wantSQL:    "SELECT * FROM t WHERE (a = ? OR (b = ? AND (c = ? OR d IS NULL)))",
			wantParams: []interface{}{1, 2, 3},
			wantSpec: domain.QuerySpec{Table: "t", Where: []domain.Condition{query.Or(
				query.Equal("a", 1),
				query.And(query.Equal("b", 2), query.Or(query.Equal("c", 3), query.IsNull("d"))),
			)}},
		},
		{
			name: "inner join compares columns with Col",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().Select("u.name", "p.title").From("users u").
					InnerJoin("posts p", query.Equal("p.user_id", query.Col("u.id")))
			},
			wantSQL: "SELECT u.name, p.title FROM users u INNER JOIN posts p ON p.user_id = u.id",
			wantSpec: domain.QuerySpec{
				Table:  "users",
				Fields: []string{"u.name", "p.title"},
				Joins: []domain.Join{{
					Type:  domain.JoinTypeInner,
					Table: "posts p",
					On:    condPtr(query.Equal("p.user_id", query.Col("u.id"))),
				}},
			},
		},
		{
			name: "every join type; CROSS JOIN has no ON",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users u").
					LeftJoin("orders o", query.Equal("o.user_id", query.Col("u.id"))).
					RightJoin("teams AS tm", query.Equal("tm.id", query.Col("u.team_id"))).
					Join(domain.JoinTypeFull, "profiles pr", query.Equal("pr.user_id", query.Col("u.id"))).
					Join(domain.JoinTypeCross, "regions r", query.Equal("ignored", 1))
			},
			wantSQL: "SELECT * FROM users u LEFT JOIN orders o ON o.user_id = u.id " +
				"RIGHT JOIN teams AS tm ON tm.id = u.team_id " +
				"FULL OUTER JOIN profiles pr ON pr.user_id = u.id CROSS JOIN regions r",
			wantSpec: domain.QuerySpec{
				Table: "users",
				Joins: []domain.Join{
					{Type: domain.JoinTypeLeft, Table: "orders o", On: condPtr(query.Equal("o.user_id", query.Col("u.id")))},
					{Type: domain.JoinTypeRight, Table: "teams AS tm", On: condPtr(query.Equal("tm.id", query.Col("u.team_id")))},
					{Type: domain.JoinTypeFull, Table: "profiles pr", On: condPtr(query.Equal("pr.user_id", query.Col("u.id")))},
					{Type: domain.JoinTypeCross, Table: "regions r"},
				},
			},
		},
		{
			name: "join parameters precede where parameters",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users u").
					InnerJoin("orders o", query.And(query.Equal("o.user_id", query.Col("u.id")), query.GreaterThan("o.total", 100))).
					Where(query.Equal("u.active", true))
			},
			wantSQL:    "SELECT * FROM users u INNER JOIN orders o ON (o.user_id = u.id AND o.total > ?) WHERE u.active = ?",
			wantParams: []interface{}{100, true},
			wantSpec: domain.QuerySpec{
				Table: "users",
				Joins: []domain.Join{{
					Type:  domain.JoinTypeInner,
					Table: "orders o",
					On:    condPtr(query.And(query.Equal("o.user_id", query.Col("u.id")), query.GreaterThan("o.total", 100))),
				}},
				Where: []domain.Condition{query.Equal("u.active", true)},
			},
		},
		{
			name: "having conditions accumulate with AND",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().Select("user_id", "COUNT(*) AS orders").From("orders").
					GroupBy("user_id").
					Having(query.GreaterThan("COUNT(*)", 5)).
					Having(query.LessThan("SUM(total)", 1000))
			},
			wantSQL:    "SELECT user_id, COUNT(*) AS orders FROM orders GROUP BY user_id HAVING COUNT(*) > ? AND SUM(total) < ?",
			wantParams: []interface{}{5, 1000},
			wantSpec: domain.QuerySpec{
				Table:   "orders",
				Fields:  []string{"user_id", "COUNT(*) AS orders"},
				GroupBy: []string{"user_id"},
				Having:  []domain.Condition{query.GreaterThan("COUNT(*)", 5), query.LessThan("SUM(total)", 1000)},
			},
		},
		{
			name: "order by",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users").
					OrderBy("name", domain.SortDirectionAsc).OrderByDesc("created_at").OrderByAsc("u.id")
			},
			wantSQL: "SELECT * FROM users ORDER BY name ASC, created_at DESC, u.id ASC",
			wantSpec: domain.QuerySpec{Table: "users", OrderBy: []domain.OrderBy{
				{Field: "name", Direction: domain.SortDirectionAsc},
				{Field: "created_at", Direction: domain.SortDirectionDesc},
				{Field: "u.id", Direction: domain.SortDirectionAsc},
			}},
		},
		{
			name:     "limit and offset",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users").Limit(10).Offset(20) },
			wantSQL:  "SELECT * FROM users LIMIT 10 OFFSET 20",
			wantSpec: domain.QuerySpec{Table: "users", Limit: intPtr(10), Offset: intPtr(20)},
		},
		{
			name:     "zero limit and offset are valid",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users").Limit(0).Offset(0) },
			wantSQL:  "SELECT * FROM users LIMIT 0 OFFSET 0",
			wantSpec: domain.QuerySpec{Table: "users", Limit: intPtr(0), Offset: intPtr(0)},
		},
		{
			name:     "page is 1-based",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users").Page(3, 10) },
			wantSQL:  "SELECT * FROM users LIMIT 10 OFFSET 20",
			wantSpec: domain.QuerySpec{Table: "users", Limit: intPtr(10), Offset: intPtr(20)},
		},
		{
			name:     "first page",
			build:    func() domain.QueryBuilder { return query.NewBuilder().From("users").Page(1, 25) },
			wantSQL:  "SELECT * FROM users LIMIT 25 OFFSET 0",
			wantSpec: domain.QuerySpec{Table: "users", Limit: intPtr(25), Offset: intPtr(0)},
		},
		{
			name: "from subquery binds subquery parameters first",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().Select("s.user_id").FromSubquery(sub, "s").Where(query.NotEqual("s.user_id", 7))
			},
			wantSQL:    "SELECT s.user_id FROM (SELECT user_id FROM orders WHERE total > ?) AS s WHERE s.user_id != ?",
			wantParams: []interface{}{100, 7},
			wantSpec: domain.QuerySpec{
				Fields:   []string{"s.user_id"},
				Where:    []domain.Condition{query.NotEqual("s.user_id", 7)},
				Subquery: true,
			},
		},
		{
			name:     "From replaces an earlier FromSubquery",
			build:    func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(sub, "s").From("users") },
			wantSQL:  "SELECT * FROM users",
			wantSpec: domain.QuerySpec{Table: "users"},
		},
		{
			name:       "FromSubquery replaces an earlier From",
			build:      func() domain.QueryBuilder { return query.NewBuilder().From("users").FromSubquery(sub, "s") },
			wantSQL:    "SELECT * FROM (SELECT user_id FROM orders WHERE total > ?) AS s",
			wantParams: []interface{}{100},
			wantSpec:   domain.QuerySpec{Subquery: true},
		},
		{
			name: "exists subqueries bind their parameters in order",
			build: func() domain.QueryBuilder {
				return query.NewBuilder().From("users u").
					Where(query.Equal("u.active", true)).
					Where(query.Exists(orders)).
					Where(query.NotExists(bans)).
					Where(query.Equal("u.age", 30))
			},
			wantSQL: "SELECT * FROM users u WHERE u.active = ? " +
				"AND EXISTS (SELECT o.id FROM orders o WHERE o.user_id = u.id AND o.total > ?) " +
				"AND NOT EXISTS (SELECT * FROM bans b WHERE b.user_id = u.id AND b.kind = ?) " +
				"AND u.age = ?",
			wantParams: []interface{}{true, 50, "hard", 30},
			wantSpec: domain.QuerySpec{Table: "users", Where: []domain.Condition{
				query.Equal("u.active", true),
				query.Exists(orders),
				query.NotExists(bans),
				query.Equal("u.age", 30),
			}},
		},
		{
			name:       "complex query",
			build:      complexQuery,
			wantSQL:    complexSQL,
			wantParams: []interface{}{true, 18, 0},
			wantSpec:   complexSpec(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := tt.build().Build()
			require.NoError(t, err)
			assert.Equal(t, tt.wantSQL, q.SQL)
			assert.Equal(t, tt.wantParams, q.Parameters)
			assert.Equal(t, domain.QueryTypeSelect, q.Type)
			require.NotNil(t, q.Spec)
			assert.Equal(t, tt.wantSpec, *q.Spec)

			sql, params, err := tt.build().BuildRaw()
			require.NoError(t, err)
			assert.Equal(t, tt.wantSQL, sql)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}

func TestQueryBuilder_Operators(t *testing.T) {
	tests := []struct {
		name       string
		condition  domain.Condition
		wantWhere  string
		wantParams []interface{}
	}{
		{"equal", query.Equal("a", 1), "a = ?", []interface{}{1}},
		{"equal nil", query.Equal("a", nil), "a = ?", []interface{}{nil}},
		{"not equal", query.NotEqual("a", 1), "a != ?", []interface{}{1}},
		{"greater than", query.GreaterThan("a", 1), "a > ?", []interface{}{1}},
		{"greater than or equal", query.GreaterThanOrEqual("a", 1), "a >= ?", []interface{}{1}},
		{"less than", query.LessThan("a", 1), "a < ?", []interface{}{1}},
		{"less than or equal", query.LessThanOrEqual("a", 1), "a <= ?", []interface{}{1}},
		{"like", query.Like("name", "%jo%"), "name LIKE ?", []interface{}{"%jo%"}},
		{"not like", query.NotLike("name", "%jo%"), "name NOT LIKE ?", []interface{}{"%jo%"}},
		{"in", query.In("status", "a", "b", "c"), "status IN (?, ?, ?)", []interface{}{"a", "b", "c"}},
		{"not in", query.NotIn("status", "a", "b"), "status NOT IN (?, ?)", []interface{}{"a", "b"}},
		{"empty in matches no rows", query.In("status"), "1=0", nil},
		{"empty not in matches all rows", query.NotIn("status"), "1=1", nil},
		{"between", query.Between("age", 18, 65), "age BETWEEN ? AND ?", []interface{}{18, 65}},
		{"not between", query.NotBetween("age", 18, 65), "age NOT BETWEEN ? AND ?", []interface{}{18, 65}},
		{"is null", query.IsNull("deleted_at"), "deleted_at IS NULL", nil},
		{"is not null", query.IsNotNull("t.email"), "t.email IS NOT NULL", nil},
		{"column = column", query.Equal("a", query.Col("b")), "a = b", nil},
		{"column != column", query.NotEqual("a", query.Col("t.b")), "a != t.b", nil},
		{"column > column", query.GreaterThan("a", query.Col("b")), "a > b", nil},
		{"column >= column", query.GreaterThanOrEqual("a", query.Col("b")), "a >= b", nil},
		{"column < column", query.LessThan("a", query.Col("b")), "a < b", nil},
		{"column <= column", query.LessThanOrEqual("a", query.Col("b")), "a <= b", nil},
		{"column LIKE column", domain.Condition{Field: "a", Operator: domain.OperatorLike, Value: query.Col("b")}, "a LIKE b", nil},
		{"column NOT LIKE column", domain.Condition{Field: "a", Operator: domain.OperatorNotLike, Value: query.Col("b")}, "a NOT LIKE b", nil},
		{"column in IN list", query.In("a", 1, query.Col("t.b"), 2), "a IN (?, t.b, ?)", []interface{}{1, 2}},
		{"column as BETWEEN bound", query.Between("a", query.Col("t.lo"), 10), "a BETWEEN t.lo AND ?", []interface{}{10}},
		{"and group", query.And(query.Equal("a", 1), query.IsNull("b")), "(a = ? AND b IS NULL)", []interface{}{1}},
		{"or group", query.Or(query.Equal("a", 1), query.Equal("b", 2)), "(a = ? OR b = ?)", []interface{}{1, 2}},
		{
			"exists accepts any QueryBuilder",
			query.Exists(rawBuilder{sql: "SELECT id FROM t2 WHERE x = ?", params: []interface{}{5}}),
			"EXISTS (SELECT id FROM t2 WHERE x = ?)",
			[]interface{}{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := query.NewBuilder().From("t").Where(tt.condition).Build()
			require.NoError(t, err)
			assert.Equal(t, "SELECT * FROM t WHERE "+tt.wantWhere, q.SQL)
			assert.Equal(t, tt.wantParams, q.Parameters)
			require.NotNil(t, q.Spec)
			assert.Equal(t, []domain.Condition{tt.condition}, q.Spec.Where)
		})
	}
}

func TestQueryBuilder_BuildErrors(t *testing.T) {
	valid := func() domain.QueryBuilder { return query.NewBuilder().From("t") }
	sub := query.NewBuilder().Select("id").From("orders")
	boom := errors.New("boom")

	tests := []struct {
		name    string
		build   func() domain.QueryBuilder
		wantErr string
	}{
		{"no data source", func() domain.QueryBuilder { return query.NewBuilder() }, "no table or subquery specified"},
		{"table injection", func() domain.QueryBuilder { return query.NewBuilder().From("users; DROP TABLE users") }, "invalid table"},
		{"table with two aliases", func() domain.QueryBuilder { return query.NewBuilder().From("users u x") }, "invalid table"},
		{"qualified table alias", func() domain.QueryBuilder { return query.NewBuilder().From("users u.x") }, "invalid table"},
		{"select injection", func() domain.QueryBuilder { return valid().Select("id; DROP TABLE users") }, "invalid select expression"},
		{"select function call", func() domain.QueryBuilder { return valid().Select("lower(name)") }, "invalid select expression"},
		{"select literal", func() domain.QueryBuilder { return valid().Select("1") }, "invalid select expression"},
		{"select comma list", func() domain.QueryBuilder { return valid().Select("id, name") }, "invalid select expression"},
		{"alias on star", func() domain.QueryBuilder { return valid().Select("* AS everything") }, "invalid select expression"},
		{"missing alias", func() domain.QueryBuilder { return valid().Select("name AS") }, "invalid select expression"},
		{"alias without AS", func() domain.QueryBuilder { return valid().Select("name n") }, "invalid select expression"},
		{"count distinct", func() domain.QueryBuilder { return valid().SelectCount("DISTINCT id") }, "invalid select expression"},
		{"count injection", func() domain.QueryBuilder { return valid().SelectCount("*) FROM secrets --") }, "invalid select expression"},
		{"where field injection", func() domain.QueryBuilder { return valid().Where(query.Equal("id = 1 OR 1=1 --", 1)) }, "invalid field"},
		{"empty where field", func() domain.QueryBuilder { return valid().Where(query.Equal("", 1)) }, "invalid field"},
		{"order by injection", func() domain.QueryBuilder { return valid().OrderByAsc("name; DROP TABLE users") }, "invalid ORDER BY field"},
		{"order by with direction text", func() domain.QueryBuilder { return valid().OrderByDesc("name DESC") }, "invalid ORDER BY field"},
		{"group by injection", func() domain.QueryBuilder { return valid().GroupBy("id", "id) --") }, "invalid GROUP BY field"},
		{"join table injection", func() domain.QueryBuilder {
			return valid().InnerJoin("posts p ON 1=1 --", query.Equal("p.id", query.Col("t.id")))
		}, "invalid table"},
		{"column reference injection", func() domain.QueryBuilder {
			return valid().InnerJoin("posts p", query.Equal("p.user_id", query.Col("t.id OR 1=1")))
		}, "invalid column reference"},
		{"join without ON condition", func() domain.QueryBuilder { return valid().InnerJoin("posts p", domain.Condition{}) }, "invalid field"},
		{"unknown join type", func() domain.QueryBuilder {
			return valid().Join(domain.JoinType(42), "posts p", query.Equal("p.id", query.Col("t.id")))
		}, "unknown join type 42"},
		{"invalid ON condition", func() domain.QueryBuilder {
			return valid().LeftJoin("posts p", domain.Condition{Field: "p.id", Operator: domain.Operator(99)})
		}, "unknown operator 99"},
		{"unknown sort direction", func() domain.QueryBuilder { return valid().OrderBy("name", domain.SortDirection(7)) }, "unknown sort direction 7"},
		{"invalid having field", func() domain.QueryBuilder { return valid().Having(query.GreaterThan("COUNT(*) OR 1=1", 0)) }, "invalid field"},
		{"empty subquery alias", func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(sub, "") }, "invalid subquery alias"},
		{"subquery alias injection", func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(sub, "s; DROP TABLE users") }, "invalid subquery alias"},
		{"qualified subquery alias", func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(sub, "a.b") }, "invalid subquery alias"},
		{"invalid subquery", func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(query.NewBuilder(), "s") }, "invalid subquery"},
		{"nil subquery", func() domain.QueryBuilder { return query.NewBuilder().FromSubquery(nil, "s") }, "subquery must be a non-nil *query.Builder"},
		{"typed nil subquery", func() domain.QueryBuilder {
			return query.NewBuilder().FromSubquery((*query.Builder)(nil), "s")
		}, "subquery must be a non-nil *query.Builder"},
		{"foreign subquery", func() domain.QueryBuilder {
			return query.NewBuilder().FromSubquery(rawBuilder{sql: "SELECT 1"}, "s")
		}, "subquery must be a non-nil *query.Builder"},
		{"foreign subquery is not silently ignored after From", func() domain.QueryBuilder {
			return query.NewBuilder().From("users").FromSubquery(rawBuilder{sql: "SELECT 1"}, "s")
		}, "subquery must be a non-nil *query.Builder"},
		{"exists with SQL string", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Operator: domain.OperatorExists, Value: "SELECT 1; DROP TABLE users"})
		}, "EXISTS requires a QueryBuilder subquery, got string"},
		{"exists with nil", func() domain.QueryBuilder { return valid().Where(query.Exists(nil)) }, "EXISTS requires a QueryBuilder subquery"},
		{"exists with typed nil builder", func() domain.QueryBuilder {
			return valid().Where(query.Exists((*query.Builder)(nil)))
		}, "EXISTS requires a QueryBuilder subquery"},
		{"not exists with invalid subquery", func() domain.QueryBuilder {
			return valid().Where(query.NotExists(query.NewBuilder().From("bad table!")))
		}, "invalid subquery"},
		{"exists subquery error", func() domain.QueryBuilder { return valid().Where(query.Exists(rawBuilder{err: boom})) }, "boom"},
		{"unknown operator", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Field: "id", Operator: domain.Operator(99), Value: 1})
		}, "unknown operator 99"},
		{"between with one value", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Field: "age", Operator: domain.OperatorBetween, Values: []interface{}{18}})
		}, "BETWEEN requires exactly 2 values, got 1"},
		{"between with Value only", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Field: "age", Operator: domain.OperatorBetween, Value: 18})
		}, "BETWEEN requires exactly 2 values, got 0"},
		{"between with three values", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Field: "age", Operator: domain.OperatorBetween, Values: []interface{}{1, 2, 3}})
		}, "BETWEEN requires exactly 2 values, got 3"},
		{"not between with one value", func() domain.QueryBuilder {
			return valid().Where(domain.Condition{Field: "age", Operator: domain.OperatorNotBetween, Values: []interface{}{18}})
		}, "NOT BETWEEN requires exactly 2 values, got 1"},
		{"empty OR group", func() domain.QueryBuilder { return valid().Where(query.Or()) }, "OR group requires at least one condition"},
		{"empty nested AND group", func() domain.QueryBuilder {
			return valid().Where(query.Or(query.Equal("a", 1), query.And()))
		}, "AND group requires at least one condition"},
		{"invalid condition in OR group", func() domain.QueryBuilder {
			return valid().WhereOr(query.Equal("a", 1), query.Equal("b; --", 2))
		}, "invalid field"},
		{"invalid column in IN list", func() domain.QueryBuilder { return valid().Where(query.In("id", query.Col("x y"))) }, "invalid column reference"},
		{"invalid column as BETWEEN bound", func() domain.QueryBuilder {
			return valid().Where(query.Between("x", query.Col("ok"), query.Col("bad name")))
		}, "invalid column reference"},
		{"negative limit", func() domain.QueryBuilder { return valid().Limit(-1) }, "limit must not be negative"},
		{"negative offset", func() domain.QueryBuilder { return valid().Offset(-5) }, "offset must not be negative"},
		{"page zero", func() domain.QueryBuilder { return valid().Page(0, 10) }, "page and page size must be at least 1"},
		{"negative page", func() domain.QueryBuilder { return valid().Page(-2, 10) }, "page and page size must be at least 1"},
		{"zero page size", func() domain.QueryBuilder { return valid().Page(1, 0) }, "page and page size must be at least 1"},
		{"negative page size", func() domain.QueryBuilder { return valid().Page(2, -10) }, "page and page size must be at least 1"},
		{"page offset overflow", func() domain.QueryBuilder { return valid().Page(math.MaxInt, 2) }, "page offset overflows int"},
		{"invalid argument is kept after a valid one", func() domain.QueryBuilder { return valid().Limit(-1).Limit(10) }, "limit must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := tt.build().Build()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.ErrorIs(t, err, domain.ErrInvalidQuery)
			var se *domain.StorageError
			require.ErrorAs(t, err, &se)
			assert.Equal(t, domain.ErrorTypeValidation, se.Type)
			assert.Equal(t, domain.Query{}, q)

			sql, params, err := tt.build().BuildRaw()
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrInvalidQuery)
			assert.Empty(t, sql)
			assert.Nil(t, params)
		})
	}
}

func TestQueryBuilder_SubqueryErrorKeepsCause(t *testing.T) {
	boom := errors.New("boom")
	_, err := query.NewBuilder().From("t").Where(query.Exists(rawBuilder{err: boom})).Build()
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)

	_, err = query.NewBuilder().FromSubquery(query.NewBuilder().From("t").Limit(-1), "s").Build()
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
	assert.Contains(t, err.Error(), "limit must not be negative")
}

func TestQueryBuilder_BuildIsIdempotent(t *testing.T) {
	b := query.NewBuilder().From("users")

	sql, params, err := b.BuildRaw()
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM users", sql)
	assert.Nil(t, params)

	first, err := b.Build()
	require.NoError(t, err)
	second, err := b.Build()
	require.NoError(t, err)
	assert.Equal(t, first, second)

	// The implicit "*" must not be stored by building.
	q, err := b.Select("id").Build()
	require.NoError(t, err)
	assert.Equal(t, "SELECT id FROM users", q.SQL)
	assert.Equal(t, []string{"id"}, q.Spec.Fields)

	c := complexQuery()
	first, err = c.Build()
	require.NoError(t, err)
	second, err = c.Build()
	require.NoError(t, err)
	assert.Equal(t, first, second)
	sql, params, err = c.BuildRaw()
	require.NoError(t, err)
	assert.Equal(t, first.SQL, sql)
	assert.Equal(t, first.Parameters, params)
}

func TestQueryBuilder_ConcurrentBuild(t *testing.T) {
	b := complexQuery()
	want, err := b.Build()
	require.NoError(t, err)

	var wg sync.WaitGroup
	results := make([]domain.Query, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = b.Build()
		}(i)
	}
	wg.Wait()
	for _, got := range results {
		assert.Equal(t, want, got)
	}
}

func TestQueryBuilder_SpecIsIndependentOfBuilder(t *testing.T) {
	ors := []domain.Condition{query.Equal("u.a", 1), query.Equal("u.b", 2)}
	roles := []interface{}{"admin", "owner"}
	configure := func() domain.QueryBuilder {
		return query.NewBuilder().Select("u.id").From("users u").
			InnerJoin("posts p", query.In("p.kind", roles...)).
			Where(query.In("u.role", roles...)).
			WhereOr(ors...).
			GroupBy("u.id").
			Having(query.GreaterThan("COUNT(*)", 1)).
			OrderByAsc("u.id").
			Limit(10).
			Offset(5)
	}
	want, err := configure().Build()
	require.NoError(t, err)

	b := configure()
	q, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, want, q)

	// Mutating the builder does not change an already-built query.
	b.Select("u.email").Where(query.Equal("x", 1)).WhereOr(query.Equal("y", 2)).
		InnerJoin("tags t", query.Equal("t.id", query.Col("p.tag_id"))).
		GroupBy("u.email").Having(query.LessThan("COUNT(*)", 9)).
		OrderByDesc("u.email").Limit(1).Offset(2)
	_, err = b.Build()
	require.NoError(t, err)
	assert.Equal(t, want, q)

	// Mutating a built Spec does not change the builder.
	b = configure()
	q, err = b.Build()
	require.NoError(t, err)
	q.Spec.Fields[0] = "hacked"
	q.Spec.Where[0].Values[0] = "hacked"
	q.Spec.Where[1].Conditions[0] = query.Equal("hacked", 0)
	q.Spec.Joins[0].On.Values[0] = "hacked"
	q.Spec.GroupBy[0] = "hacked"
	q.Spec.Having[0].Value = "hacked"
	q.Spec.OrderBy[0].Field = "hacked"
	*q.Spec.Limit = 99
	*q.Spec.Offset = 99
	q.Parameters[0] = "hacked"
	again, err := b.Build()
	require.NoError(t, err)
	assert.Equal(t, want, again)

	// Mutating the caller's slices after configuring does not leak into the
	// group added by WhereOr.
	b = configure()
	ors[0] = query.Equal("hacked", 0)
	q, err = b.Build()
	require.NoError(t, err)
	assert.Equal(t, query.Equal("u.a", 1), q.Spec.Where[1].Conditions[0])
}

func TestConditionHelpers(t *testing.T) {
	sub := query.NewBuilder().From("orders")
	a, b := query.Equal("a", 1), query.IsNull("b")

	tests := []struct {
		name string
		got  domain.Condition
		want domain.Condition
	}{
		{"Equal", query.Equal("id", 1), domain.Condition{Field: "id", Operator: domain.OperatorEqual, Value: 1}},
		{"NotEqual", query.NotEqual("status", "deleted"), domain.Condition{Field: "status", Operator: domain.OperatorNotEqual, Value: "deleted"}},
		{"GreaterThan", query.GreaterThan("age", 18), domain.Condition{Field: "age", Operator: domain.OperatorGreaterThan, Value: 18}},
		{"GreaterThanOrEqual", query.GreaterThanOrEqual("age", 18), domain.Condition{Field: "age", Operator: domain.OperatorGreaterThanOrEqual, Value: 18}},
		{"LessThan", query.LessThan("price", 100.0), domain.Condition{Field: "price", Operator: domain.OperatorLessThan, Value: 100.0}},
		{"LessThanOrEqual", query.LessThanOrEqual("price", 100.0), domain.Condition{Field: "price", Operator: domain.OperatorLessThanOrEqual, Value: 100.0}},
		{"Like", query.Like("name", "%john%"), domain.Condition{Field: "name", Operator: domain.OperatorLike, Value: "%john%"}},
		{"NotLike", query.NotLike("name", "%john%"), domain.Condition{Field: "name", Operator: domain.OperatorNotLike, Value: "%john%"}},
		{"In", query.In("status", "active", "pending"), domain.Condition{Field: "status", Operator: domain.OperatorIn, Values: []interface{}{"active", "pending"}}},
		{"In without values", query.In("status"), domain.Condition{Field: "status", Operator: domain.OperatorIn}},
		{"NotIn", query.NotIn("status", "deleted"), domain.Condition{Field: "status", Operator: domain.OperatorNotIn, Values: []interface{}{"deleted"}}},
		{"Between", query.Between("age", 18, 65), domain.Condition{Field: "age", Operator: domain.OperatorBetween, Values: []interface{}{18, 65}}},
		{"NotBetween", query.NotBetween("age", 18, 65), domain.Condition{Field: "age", Operator: domain.OperatorNotBetween, Values: []interface{}{18, 65}}},
		{"IsNull", query.IsNull("deleted_at"), domain.Condition{Field: "deleted_at", Operator: domain.OperatorIsNull}},
		{"IsNotNull", query.IsNotNull("email"), domain.Condition{Field: "email", Operator: domain.OperatorIsNotNull}},
		{"Exists", query.Exists(sub), domain.Condition{Operator: domain.OperatorExists, Value: sub}},
		{"NotExists", query.NotExists(sub), domain.Condition{Operator: domain.OperatorNotExists, Value: sub}},
		{"And", query.And(a, b), domain.Condition{Operator: domain.OperatorAnd, Conditions: []domain.Condition{a, b}}},
		{"Or", query.Or(a, b), domain.Condition{Operator: domain.OperatorOr, Conditions: []domain.Condition{a, b}}},
		{"Col", query.Equal("p.user_id", query.Col("u.id")), domain.Condition{Field: "p.user_id", Operator: domain.OperatorEqual, Value: domain.ColumnRef("u.id")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestCommandBuilder_Build(t *testing.T) {
	orders := query.NewBuilder().From("orders o").
		Where(query.Equal("o.user_id", query.Col("users.id"))).
		Where(query.GreaterThan("o.total", 10))

	tests := []struct {
		name       string
		build      func() *query.CommandBuilder
		wantType   domain.CommandType
		wantSQL    string
		wantParams []interface{}
		wantSpec   domain.CommandSpec
	}{
		{
			name: "insert keeps Set order",
			build: func() *query.CommandBuilder {
				return query.Insert("users").Set("name", "John Doe").Set("email", "john@example.com").Set("age", 30)
			},
			wantType:   domain.CommandTypeInsert,
			wantSQL:    "INSERT INTO users (name, email, age) VALUES (?, ?, ?)",
			wantParams: []interface{}{"John Doe", "john@example.com", 30},
			wantSpec: domain.CommandSpec{Table: "users", Values: []domain.FieldValue{
				{Field: "name", Value: "John Doe"},
				{Field: "email", Value: "john@example.com"},
				{Field: "age", Value: 30},
			}},
		},
		{
			name: "setting a column again replaces its value in place",
			build: func() *query.CommandBuilder {
				return query.Insert("users").Set("name", "a").Set("age", 1).Set("name", "b")
			},
			wantType:   domain.CommandTypeInsert,
			wantSQL:    "INSERT INTO users (name, age) VALUES (?, ?)",
			wantParams: []interface{}{"b", 1},
			wantSpec: domain.CommandSpec{Table: "users", Values: []domain.FieldValue{
				{Field: "name", Value: "b"},
				{Field: "age", Value: 1},
			}},
		},
		{
			name: "Values adds keys in sorted order",
			build: func() *query.CommandBuilder {
				return query.Insert("users").Values(map[string]interface{}{
					"name":  "Alice Smith",
					"email": "alice@example.com",
					"age":   28,
				})
			},
			wantType:   domain.CommandTypeInsert,
			wantSQL:    "INSERT INTO users (age, email, name) VALUES (?, ?, ?)",
			wantParams: []interface{}{28, "alice@example.com", "Alice Smith"},
			wantSpec: domain.CommandSpec{Table: "users", Values: []domain.FieldValue{
				{Field: "age", Value: 28},
				{Field: "email", Value: "alice@example.com"},
				{Field: "name", Value: "Alice Smith"},
			}},
		},
		{
			name: "Values keeps the position of columns already set",
			build: func() *query.CommandBuilder {
				return query.Insert("public.users").Set("name", "x").Set("zip", "1").
					Values(map[string]interface{}{"name": "Alice", "age": 28})
			},
			wantType:   domain.CommandTypeInsert,
			wantSQL:    "INSERT INTO public.users (name, zip, age) VALUES (?, ?, ?)",
			wantParams: []interface{}{"Alice", "1", 28},
			wantSpec: domain.CommandSpec{Table: "public.users", Values: []domain.FieldValue{
				{Field: "name", Value: "Alice"},
				{Field: "zip", Value: "1"},
				{Field: "age", Value: 28},
			}},
		},
		{
			name: "update binds SET values before WHERE values",
			build: func() *query.CommandBuilder {
				return query.Update("users").Set("name", "Jane Doe").Set("age", 25).Where(query.Equal("id", 1))
			},
			wantType:   domain.CommandTypeUpdate,
			wantSQL:    "UPDATE users SET name = ?, age = ? WHERE id = ?",
			wantParams: []interface{}{"Jane Doe", 25, 1},
			wantSpec: domain.CommandSpec{
				Table:  "users",
				Values: []domain.FieldValue{{Field: "name", Value: "Jane Doe"}, {Field: "age", Value: 25}},
				Where:  []domain.Condition{query.Equal("id", 1)},
			},
		},
		{
			name: "update with OR group and column reference",
			build: func() *query.CommandBuilder {
				return query.Update("accounts").Set("status", "locked").
					Where(query.Equal("active", true)).
					Where(query.Or(query.GreaterThan("failed_logins", 5), query.LessThan("locked_until", query.Col("created_at"))))
			},
			wantType:   domain.CommandTypeUpdate,
			wantSQL:    "UPDATE accounts SET status = ? WHERE active = ? AND (failed_logins > ? OR locked_until < created_at)",
			wantParams: []interface{}{"locked", true, 5},
			wantSpec: domain.CommandSpec{
				Table:  "accounts",
				Values: []domain.FieldValue{{Field: "status", Value: "locked"}},
				Where: []domain.Condition{
					query.Equal("active", true),
					query.Or(query.GreaterThan("failed_logins", 5), query.LessThan("locked_until", query.Col("created_at"))),
				},
			},
		},
		{
			name:       "update without WHERE",
			build:      func() *query.CommandBuilder { return query.Update("users").Set("active", false) },
			wantType:   domain.CommandTypeUpdate,
			wantSQL:    "UPDATE users SET active = ?",
			wantParams: []interface{}{false},
			wantSpec:   domain.CommandSpec{Table: "users", Values: []domain.FieldValue{{Field: "active", Value: false}}},
		},
		{
			name: "delete with WHERE",
			build: func() *query.CommandBuilder {
				return query.Delete("users").
					Where(query.Equal("active", false)).
					WhereAnd(query.LessThan("last_login", "2023-01-01"), query.IsNull("deleted_at"))
			},
			wantType:   domain.CommandTypeDelete,
			wantSQL:    "DELETE FROM users WHERE active = ? AND last_login < ? AND deleted_at IS NULL",
			wantParams: []interface{}{false, "2023-01-01"},
			wantSpec: domain.CommandSpec{Table: "users", Where: []domain.Condition{
				query.Equal("active", false),
				query.LessThan("last_login", "2023-01-01"),
				query.IsNull("deleted_at"),
			}},
		},
		{
			name:     "delete without WHERE",
			build:    func() *query.CommandBuilder { return query.Delete("sessions") },
			wantType: domain.CommandTypeDelete,
			wantSQL:  "DELETE FROM sessions",
			wantSpec: domain.CommandSpec{Table: "sessions"},
		},
		{
			name: "delete with IN and NOT EXISTS",
			build: func() *query.CommandBuilder {
				return query.Delete("users").Where(query.In("id", 1, 2)).Where(query.NotExists(orders))
			},
			wantType:   domain.CommandTypeDelete,
			wantSQL:    "DELETE FROM users WHERE id IN (?, ?) AND NOT EXISTS (SELECT * FROM orders o WHERE o.user_id = users.id AND o.total > ?)",
			wantParams: []interface{}{1, 2, 10},
			wantSpec: domain.CommandSpec{Table: "users", Where: []domain.Condition{
				query.In("id", 1, 2),
				query.NotExists(orders),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.build().Build()
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, cmd.Type)
			assert.Equal(t, tt.wantSQL, cmd.SQL)
			assert.Equal(t, tt.wantParams, cmd.Parameters)
			require.NotNil(t, cmd.Spec)
			assert.Equal(t, tt.wantSpec, *cmd.Spec)

			sql, params, err := tt.build().BuildRaw()
			require.NoError(t, err)
			assert.Equal(t, tt.wantSQL, sql)
			assert.Equal(t, tt.wantParams, params)
		})
	}
}

func TestCommandBuilder_DeterministicColumnOrder(t *testing.T) {
	values := make(map[string]interface{})
	var assignments []string
	var params []interface{}
	for i := 0; i < 20; i++ {
		field := fmt.Sprintf("col_%02d", i)
		values[field] = i
		assignments = append(assignments, field+" = ?")
		params = append(params, i)
	}
	wantSQL := "UPDATE t SET " + strings.Join(assignments, ", ") + " WHERE id = ?"
	params = append(params, 1)

	// Map iteration order is randomised per range loop, so repeating the build
	// would expose any order that depends on it.
	for i := 0; i < 100; i++ {
		cmd, err := query.Update("t").Values(values).Where(query.Equal("id", 1)).Build()
		require.NoError(t, err)
		require.Equal(t, wantSQL, cmd.SQL)
		require.Equal(t, params, cmd.Parameters)

		cmd, err = query.Update("users").Set("name", "Jane").Set("age", 25).Set("email", "j@example.com").
			Where(query.Equal("id", 1)).Build()
		require.NoError(t, err)
		require.Equal(t, "UPDATE users SET name = ?, age = ?, email = ? WHERE id = ?", cmd.SQL)
		require.Equal(t, []interface{}{"Jane", 25, "j@example.com", 1}, cmd.Parameters)
	}
}

func TestCommandBuilder_BuildErrors(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *query.CommandBuilder
		wantErr string
	}{
		{"insert without table", func() *query.CommandBuilder { return query.Insert("").Set("name", "x") }, "table name is required"},
		{"update without table", func() *query.CommandBuilder { return query.Update("").Set("name", "x") }, "table name is required"},
		{"delete without table", func() *query.CommandBuilder { return query.Delete("").Where(query.Equal("id", 1)) }, "table name is required"},
		{"zero-value builder", func() *query.CommandBuilder { return (&query.CommandBuilder{}).Set("name", "x") }, "table name is required"},
		{"table injection", func() *query.CommandBuilder { return query.Insert("users; DROP TABLE users").Set("name", "x") }, "invalid table"},
		{"table alias", func() *query.CommandBuilder { return query.Update("users u").Set("name", "x") }, "invalid table"},
		{"insert without values", func() *query.CommandBuilder { return query.Insert("users") }, "at least one value is required"},
		{"update without values", func() *query.CommandBuilder { return query.Update("users").Where(query.Equal("id", 1)) }, "at least one value is required"},
		{"insert with WHERE", func() *query.CommandBuilder {
			return query.Insert("users").Set("name", "x").Where(query.Equal("id", 1))
		}, "INSERT does not support WHERE conditions"},
		{"delete with values", func() *query.CommandBuilder { return query.Delete("users").Set("name", "x") }, "DELETE does not support SET values"},
		{"insert column injection", func() *query.CommandBuilder {
			return query.Insert("users").Set("name) VALUES ('x'); --", "x")
		}, "invalid column"},
		{"update column injection", func() *query.CommandBuilder {
			return query.Update("users").Set("name = 'x', admin", true)
		}, "invalid column"},
		{"column reference as value", func() *query.CommandBuilder {
			return query.Update("users").Set("updated_at", query.Col("created_at"))
		}, "column references are only supported in conditions"},
		{"invalid where field", func() *query.CommandBuilder {
			return query.Update("users").Set("a", 1).Where(query.Equal("id; --", 1))
		}, "invalid field"},
		{"unknown operator", func() *query.CommandBuilder {
			return query.Delete("users").Where(domain.Condition{Field: "id", Operator: domain.Operator(99)})
		}, "unknown operator 99"},
		{"between with one value", func() *query.CommandBuilder {
			return query.Delete("users").Where(domain.Condition{Field: "age", Operator: domain.OperatorBetween, Values: []interface{}{1}})
		}, "BETWEEN requires exactly 2 values, got 1"},
		{"empty OR group", func() *query.CommandBuilder { return query.Delete("users").Where(query.Or()) }, "OR group requires at least one condition"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.build().Build()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.ErrorIs(t, err, domain.ErrInvalidQuery)
			assert.Equal(t, domain.Command{}, cmd)

			sql, params, err := tt.build().BuildRaw()
			require.Error(t, err)
			assert.Empty(t, sql)
			assert.Nil(t, params)
		})
	}
}

func TestCommandBuilder_SpecIsIndependentOfBuilder(t *testing.T) {
	configure := func() *query.CommandBuilder {
		return query.Update("users").Set("name", "Jane").Set("age", 25).Where(query.In("id", 1, 2))
	}
	want, err := configure().Build()
	require.NoError(t, err)

	cb := configure()
	cmd, err := cb.Build()
	require.NoError(t, err)
	require.Equal(t, want, cmd)

	// Re-setting a column replaces it in place inside the builder; the built
	// command must not see that.
	cb.Set("name", "changed").Set("email", "e").Where(query.Equal("x", 1))
	_, err = cb.Build()
	require.NoError(t, err)
	assert.Equal(t, want, cmd)

	cb = configure()
	cmd, err = cb.Build()
	require.NoError(t, err)
	cmd.Spec.Values[0].Value = "hacked"
	cmd.Spec.Where[0].Values[0] = "hacked"
	again, err := cb.Build()
	require.NoError(t, err)
	assert.Equal(t, want, again)
}

func TestCommandBuilder_BuildIsIdempotent(t *testing.T) {
	cb := query.Update("users").Set("name", "Jane").Where(query.Or(query.Equal("id", 1), query.Equal("id", 2)))
	first, err := cb.Build()
	require.NoError(t, err)
	second, err := cb.Build()
	require.NoError(t, err)
	assert.Equal(t, first, second)

	sql, params, err := cb.BuildRaw()
	require.NoError(t, err)
	assert.Equal(t, first.SQL, sql)
	assert.Equal(t, first.Parameters, params)
}
