//go:build integration

package mongodb_test

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDatabase is reserved for these tests; collections are per test.
const testDatabase = "hc_go_mongo"

func testConfig(t *testing.T) domain.Config {
	t.Helper()
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI is not set")
	}
	cfg, err := mongodb.NewAdapter().ParseDSN(uri)
	require.NoError(t, err)
	cfg.Database = testDatabase
	cfg.ConnectTimeout = 10 * time.Second
	cfg.QueryTimeout = 30 * time.Second
	return cfg
}

func connect(t *testing.T, cfg domain.Config) domain.Storage {
	t.Helper()
	s, err := mongodb.NewAdapter().Connect(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func openStorage(t *testing.T) domain.Storage {
	t.Helper()
	return connect(t, testConfig(t))
}

var nonWord = regexp.MustCompile(`\W+`)

// collection returns a collection name unique to the test and drops it afterwards.
func collection(t *testing.T, s domain.Storage, suffix string) string {
	t.Helper()
	name := nonWord.ReplaceAllString(t.Name(), "_") + "_" + suffix
	require.NoError(t, s.DropTable(context.Background(), name))
	t.Cleanup(func() { _ = s.DropTable(context.Background(), name) })
	return name
}

func val(field string, value interface{}) domain.FieldValue {
	return domain.FieldValue{Field: field, Value: value}
}

func where(field string, op domain.Operator, value interface{}, values ...interface{}) domain.Condition {
	return domain.Condition{Field: field, Operator: op, Value: value, Values: values}
}

func selectQuery(table string, fields []string, conds ...domain.Condition) domain.Query {
	return domain.Query{Type: domain.QueryTypeSelect, Spec: &domain.QuerySpec{Table: table, Fields: fields, Where: conds}}
}

func insertCommand(table string, values ...domain.FieldValue) domain.Command {
	return domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{Table: table, Values: values}}
}

func updateCommand(table string, values []domain.FieldValue, conds ...domain.Condition) domain.Command {
	return domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{Table: table, Values: values, Where: conds}}
}

func deleteCommand(table string, conds ...domain.Condition) domain.Command {
	return domain.Command{Type: domain.CommandTypeDelete, Spec: &domain.CommandSpec{Table: table, Where: conds}}
}

func countQuery(table string, conds ...domain.Condition) domain.Query {
	return selectQuery(table, []string{"COUNT(*)"}, conds...)
}

type querier interface {
	Query(ctx context.Context, query domain.Query) (domain.Result, error)
	QueryOne(ctx context.Context, query domain.Query) (domain.Row, error)
	Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error)
}

func mustExec(t *testing.T, q querier, cmd domain.Command) domain.ExecuteResult {
	t.Helper()
	res, err := q.Execute(context.Background(), cmd)
	require.NoError(t, err)
	return res
}

// ids returns the "id" field of the matching documents in ascending id order.
func ids(t *testing.T, q querier, table string, conds ...domain.Condition) []int {
	t.Helper()
	query := selectQuery(table, []string{"id"}, conds...)
	query.Spec.OrderBy = []domain.OrderBy{{Field: "id"}}
	res, err := q.Query(context.Background(), query)
	require.NoError(t, err)
	defer res.Close()
	out := []int{}
	for res.Next() {
		var id int
		require.NoError(t, res.Scan(&id))
		out = append(out, id)
	}
	require.NoError(t, res.Err())
	return out
}

// rowsOf returns the materialised rows of a successful batch query operation.
func rowsOf(t *testing.T, r domain.OperationResult) domain.Result {
	t.Helper()
	require.NoError(t, r.Error)
	rows, ok := r.Result.(domain.Result)
	require.True(t, ok, "operation %d returned %T", r.Index, r.Result)
	return rows
}

func count(t *testing.T, q querier, table string, conds ...domain.Condition) int64 {
	t.Helper()
	row, err := q.QueryOne(context.Background(), countQuery(table, conds...))
	require.NoError(t, err)
	var n int64
	require.NoError(t, row.Scan(&n))
	return n
}

func TestIntegration_CollectionsAreIsolated(t *testing.T) {
	s := openStorage(t)
	a, b := collection(t, s, "a"), collection(t, s, "b")

	mustExec(t, s, insertCommand(a, val("id", 1)))
	mustExec(t, s, insertCommand(a, val("id", 2)))
	mustExec(t, s, insertCommand(b, val("id", 3)))

	assert.Equal(t, []int{1, 2}, ids(t, s, a))
	assert.Equal(t, []int{3}, ids(t, s, b))

	res := mustExec(t, s, deleteCommand(b))
	assert.Equal(t, int64(1), res.RowsAffected)
	assert.Equal(t, []int{1, 2}, ids(t, s, a), "deleting from one collection leaves the other alone")
}

func TestIntegration_Operators(t *testing.T) {
	s := openStorage(t)
	people := collection(t, s, "people")

	mustExec(t, s, insertCommand(people, val("id", 1), val("name", "Ann"), val("age", 25), val("city", "Oslo"),
		val("note", "100%"), val("address", map[string]interface{}{"zip": "0150"})))
	mustExec(t, s, insertCommand(people, val("id", 2), val("name", "Bob"), val("age", 35), val("city", "Bergen"), val("note", "a.c")))
	mustExec(t, s, insertCommand(people, val("id", 3), val("name", "bob"), val("age", 45), val("city", nil), val("note", "abc")))
	mustExec(t, s, insertCommand(people, val("id", 4), val("name", "Cara"), val("age", 55), val("note", "line\nbreak")))

	or := func(conds ...domain.Condition) domain.Condition {
		return domain.Condition{Operator: domain.OperatorOr, Conditions: conds}
	}
	and := func(conds ...domain.Condition) domain.Condition {
		return domain.Condition{Operator: domain.OperatorAnd, Conditions: conds}
	}

	tests := []struct {
		name     string
		conds    []domain.Condition
		expected []int
	}{
		{"equal", []domain.Condition{where("name", domain.OperatorEqual, "Bob")}, []int{2}},
		{"not equal", []domain.Condition{where("name", domain.OperatorNotEqual, "Bob")}, []int{1, 3, 4}},
		{"greater than", []domain.Condition{where("age", domain.OperatorGreaterThan, 35)}, []int{3, 4}},
		{"greater or equal", []domain.Condition{where("age", domain.OperatorGreaterThanOrEqual, 35)}, []int{2, 3, 4}},
		{"less than", []domain.Condition{where("age", domain.OperatorLessThan, 35)}, []int{1}},
		{"less or equal", []domain.Condition{where("age", domain.OperatorLessThanOrEqual, 35)}, []int{1, 2}},
		{"in", []domain.Condition{where("id", domain.OperatorIn, nil, 1, 3)}, []int{1, 3}},
		{"in nothing", []domain.Condition{where("id", domain.OperatorIn, nil)}, []int{}},
		{"not in", []domain.Condition{where("id", domain.OperatorNotIn, nil, 1, 3)}, []int{2, 4}},
		{"between", []domain.Condition{where("age", domain.OperatorBetween, nil, 30, 50)}, []int{2, 3}},
		{"not between", []domain.Condition{where("age", domain.OperatorNotBetween, nil, 30, 50)}, []int{1, 4}},
		{"like prefix is case-sensitive", []domain.Condition{where("name", domain.OperatorLike, "B%")}, []int{2}},
		{"like single character", []domain.Condition{where("name", domain.OperatorLike, "_ob")}, []int{2, 3}},
		{"like suffix", []domain.Condition{where("name", domain.OperatorLike, "%a")}, []int{4}},
		{"like escaped percent", []domain.Condition{where("note", domain.OperatorLike, `100\%`)}, []int{1}},
		{"like dot is literal", []domain.Condition{where("note", domain.OperatorLike, "a.c")}, []int{2}},
		{"like spans newlines", []domain.Condition{where("note", domain.OperatorLike, "line%")}, []int{4}},
		{"like is anchored", []domain.Condition{where("note", domain.OperatorLike, "b%")}, []int{}},
		{"not like", []domain.Condition{where("name", domain.OperatorNotLike, "B%")}, []int{1, 3, 4}},
		{"is null matches null and missing", []domain.Condition{where("city", domain.OperatorIsNull, nil)}, []int{3, 4}},
		{"is not null", []domain.Condition{where("city", domain.OperatorIsNotNull, nil)}, []int{1, 2}},
		{
			"same field twice",
			[]domain.Condition{where("age", domain.OperatorGreaterThan, 30), where("age", domain.OperatorLessThan, 50)},
			[]int{2, 3},
		},
		{
			"nested OR and AND",
			[]domain.Condition{or(
				where("name", domain.OperatorEqual, "Ann"),
				and(where("age", domain.OperatorGreaterThanOrEqual, 45), where("city", domain.OperatorIsNull, nil)),
			)},
			[]int{1, 3, 4},
		},
		{"dotted path", []domain.Condition{where("address.zip", domain.OperatorEqual, "0150")}, []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ids(t, s, people, tt.conds...))
		})
	}
}

func TestIntegration_UpdateAndDeleteAffectOnlyMatches(t *testing.T) {
	s := openStorage(t)
	users := collection(t, s, "users")
	for i := 1; i <= 3; i++ {
		mustExec(t, s, insertCommand(users, val("id", i), val("age", 20+i)))
	}
	age := func(id int) int {
		q := selectQuery(users, []string{"age"}, where("id", domain.OperatorEqual, id))
		row, err := s.QueryOne(context.Background(), q)
		require.NoError(t, err)
		var a int
		require.NoError(t, row.Scan(&a))
		return a
	}

	set99 := updateCommand(users, []domain.FieldValue{val("age", 99)}, where("id", domain.OperatorEqual, 2))
	assert.Equal(t, int64(1), mustExec(t, s, set99).RowsAffected)
	assert.Equal(t, int64(1), mustExec(t, s, set99).RowsAffected, "matched rows count even when unchanged")
	assert.Equal(t, []int{21, 99, 23}, []int{age(1), age(2), age(3)})

	none := updateCommand(users, []domain.FieldValue{val("age", 0)}, where("id", domain.OperatorEqual, 42))
	assert.Equal(t, int64(0), mustExec(t, s, none).RowsAffected)

	res := mustExec(t, s, deleteCommand(users, where("id", domain.OperatorIn, nil, 1)))
	assert.Equal(t, int64(1), res.RowsAffected)
	assert.Equal(t, []int{2, 3}, ids(t, s, users))

	// Untranslatable conditions fail instead of matching every document.
	bad := where("age", domain.OperatorEqual, domain.ColumnRef("id"))
	_, err := s.Execute(context.Background(), deleteCommand(users, where("id", domain.OperatorEqual, 3), bad))
	assert.ErrorIs(t, err, domain.ErrUnsupportedQuery)
	_, err = s.Execute(context.Background(), updateCommand(users, []domain.FieldValue{val("age", 0)}, bad))
	assert.ErrorIs(t, err, domain.ErrUnsupportedQuery)
	assert.Equal(t, []int{2, 3}, ids(t, s, users))
	assert.Equal(t, []int{99, 23}, []int{age(2), age(3)})
}

func TestIntegration_ScanOrder(t *testing.T) {
	s := openStorage(t)
	rows := collection(t, s, "rows")
	const n = 40
	for i := 1; i <= n; i++ {
		mustExec(t, s, insertCommand(rows, val("id", i), val("a", "a"+string(rune('A'+i%26))), val("b", i*10), val("c", float64(i)/2)))
	}

	q := selectQuery(rows, []string{"c", "a", "id", "b"})
	q.Spec.OrderBy = []domain.OrderBy{{Field: "id"}}
	res, err := s.Query(context.Background(), q)
	require.NoError(t, err)
	cols, err := res.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "a", "id", "b"}, cols)
	seen := 0
	for res.Next() {
		var c float64
		var a string
		var id, b int
		require.NoError(t, res.Scan(&c, &a, &id, &b))
		seen++
		require.Equal(t, seen, id)
		require.Equal(t, "a"+string(rune('A'+id%26)), a)
		require.Equal(t, id*10, b)
		require.Equal(t, float64(id)/2, c)
		types, err := res.ColumnTypes()
		require.NoError(t, err)
		require.Equal(t, []string{"double", "string", "int", "int"},
			[]string{types[0].DatabaseType, types[1].DatabaseType, types[2].DatabaseType, types[3].DatabaseType})
	}
	require.NoError(t, res.Err())
	assert.Equal(t, n, seen)
	require.NoError(t, res.Close())

	// Whole documents scan in document order: _id first, then insertion order.
	res, err = s.Query(context.Background(), selectQuery(rows, nil, where("id", domain.OperatorEqual, 7)))
	require.NoError(t, err)
	defer res.Close()
	require.True(t, res.Next())
	cols, err = res.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"_id", "id", "a", "b", "c"}, cols)
	var oid, a string
	var id, b int
	var c float64
	require.NoError(t, res.Scan(&oid, &id, &a, &b, &c))
	assert.Regexp(t, `^[0-9a-f]{24}$`, oid)
	assert.Equal(t, []interface{}{7, "aH", 70, 3.5}, []interface{}{id, a, b, c})
	assert.False(t, res.Next())

	row, err := s.QueryOne(context.Background(), selectQuery(rows, []string{"b", "a"}, where("id", domain.OperatorEqual, 3)))
	require.NoError(t, err)
	require.NoError(t, row.Scan(&b, &a))
	assert.Equal(t, 30, b)
	assert.Equal(t, "aD", a)

	_, err = s.QueryOne(context.Background(), selectQuery(rows, nil, where("id", domain.OperatorEqual, 999)))
	assert.ErrorIs(t, err, domain.ErrNoRows)
}

func TestIntegration_ScanRowUntaggedStruct(t *testing.T) {
	s := openStorage(t)
	people := collection(t, s, "people")
	mustExec(t, s, insertCommand(people, val("name", "Ann"), val("age", 30), val("email", "ann@example.com")))

	type person struct {
		ID    string `bson:"_id"`
		Name  string
		Age   int
		Email string
	}

	res, err := s.Query(context.Background(), selectQuery(people, nil))
	require.NoError(t, err)
	defer res.Close()
	require.True(t, res.Next())
	var p person
	require.NoError(t, res.ScanRow(&p))
	assert.Regexp(t, `^[0-9a-f]{24}$`, p.ID)
	assert.Equal(t, person{ID: p.ID, Name: "Ann", Age: 30, Email: "ann@example.com"}, p)

	row, err := s.QueryOne(context.Background(), selectQuery(people, []string{"name", "age"}))
	require.NoError(t, err)
	var projected person
	require.NoError(t, row.ScanRow(&projected))
	assert.Equal(t, person{Name: "Ann", Age: 30}, projected, "only the selected fields are returned")
}

func TestIntegration_Count(t *testing.T) {
	s := openStorage(t)
	users := collection(t, s, "users")
	mustExec(t, s, insertCommand(users, val("id", 1), val("email", "a@x"), val("active", true)))
	mustExec(t, s, insertCommand(users, val("id", 2), val("email", "b@x"), val("active", false)))
	mustExec(t, s, insertCommand(users, val("id", 3), val("email", nil), val("active", true)))
	mustExec(t, s, insertCommand(users, val("id", 4), val("active", true)))

	assert.Equal(t, int64(4), count(t, s, users))
	assert.Equal(t, int64(3), count(t, s, users, where("active", domain.OperatorEqual, true)))

	res, err := s.Query(context.Background(), selectQuery(users, []string{"COUNT(email)"}))
	require.NoError(t, err)
	defer res.Close()
	cols, err := res.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"count"}, cols)
	require.True(t, res.Next())
	var n int64
	require.NoError(t, res.Scan(&n))
	assert.Equal(t, int64(2), n, "COUNT(field) skips null and missing values")
	assert.False(t, res.Next())

	assert.Equal(t, int64(0), count(t, s, collection(t, s, "empty")))
}

func TestIntegration_SortSkipLimit(t *testing.T) {
	s := openStorage(t)
	users := collection(t, s, "users")
	ages := []int{30, 20, 30, 10, 20, 30}
	names := []string{"f", "e", "d", "c", "b", "a"}
	for i := range ages {
		mustExec(t, s, insertCommand(users, val("id", i+1), val("age", ages[i]), val("name", names[i])))
	}

	page := func(offset, limit *int) []int {
		q := selectQuery(users, []string{"id"})
		q.Spec.OrderBy = []domain.OrderBy{
			{Field: "age", Direction: domain.SortDirectionDesc},
			{Field: "name", Direction: domain.SortDirectionAsc},
		}
		q.Spec.Offset, q.Spec.Limit = offset, limit
		res, err := s.Query(context.Background(), q)
		require.NoError(t, err)
		defer res.Close()
		out := []int{}
		for res.Next() {
			var id int
			require.NoError(t, res.Scan(&id))
			out = append(out, id)
		}
		require.NoError(t, res.Err())
		return out
	}

	// age desc, then name asc: (30,a)=6 (30,d)=3 (30,f)=1 (20,b)=5 (20,e)=2 (10,c)=4
	assert.Equal(t, []int{6, 3, 1, 5, 2, 4}, page(nil, nil))
	assert.Equal(t, []int{3, 1, 5}, page(intPtr(1), intPtr(3)))
	assert.Equal(t, []int{2, 4}, page(intPtr(4), nil))
	assert.Equal(t, []int{}, page(nil, intPtr(0)), "LIMIT 0 returns no rows")

	q := selectQuery(users, []string{"id"})
	q.Spec.OrderBy = []domain.OrderBy{{Field: "age", Direction: domain.SortDirectionDesc}, {Field: "name"}}
	q.Spec.Offset = intPtr(2)
	row, err := s.QueryOne(context.Background(), q)
	require.NoError(t, err)
	var id int
	require.NoError(t, row.Scan(&id))
	assert.Equal(t, 1, id, "QueryOne applies sort and offset")
}

func TestIntegration_Distinct(t *testing.T) {
	s := openStorage(t)
	users := collection(t, s, "users")
	cities := []string{"Oslo", "Bergen", "Oslo", "Tromsø", "Bergen"}
	for i, city := range cities {
		mustExec(t, s, insertCommand(users, val("id", i), val("active", i != 3),
			val("home", map[string]interface{}{"city": city})))
	}

	q := selectQuery(users, []string{"home.city"}, where("active", domain.OperatorEqual, true))
	q.Spec.Distinct = true
	res, err := s.Query(context.Background(), q)
	require.NoError(t, err)
	defer res.Close()
	var got []string
	for res.Next() {
		var city string
		require.NoError(t, res.Scan(&city))
		got = append(got, city)
	}
	require.NoError(t, res.Err())
	assert.ElementsMatch(t, []string{"Oslo", "Bergen"}, got)
}

func TestIntegration_LastInsertID(t *testing.T) {
	s := openStorage(t)
	items := collection(t, s, "items")

	res := mustExec(t, s, insertCommand(items, val("_id", 5), val("name", "five")))
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1, LastInsertID: 5}, res)
	res = mustExec(t, s, insertCommand(items, val("name", "generated ObjectID")))
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1, LastInsertID: 0}, res)
	res = mustExec(t, s, insertCommand(items, val("_id", "k"), val("name", "string id")))
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1, LastInsertID: 0}, res)

	_, err := s.Execute(context.Background(), insertCommand(items, val("_id", 5)))
	assert.ErrorIs(t, err, domain.ErrDuplicateKey)
}

func TestIntegration_Transactions(t *testing.T) {
	s := openStorage(t)
	accounts := collection(t, s, "accounts")
	mustExec(t, s, insertCommand(accounts, val("id", 1), val("balance", 100)))
	ctx := context.Background()

	t.Run("commit", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		mustExec(t, tx, insertCommand(accounts, val("id", 2), val("balance", 50)))
		assert.Equal(t, []int{1, 2}, ids(t, tx, accounts), "the transaction sees its own writes")
		assert.Equal(t, []int{1}, ids(t, s, accounts), "others do not see uncommitted writes")
		require.NoError(t, tx.Commit())
		assert.False(t, tx.IsActive())
		assert.Equal(t, []int{1, 2}, ids(t, s, accounts))
		assert.ErrorIs(t, tx.Commit(), domain.ErrTransactionClosed)
		_, err = tx.Query(ctx, selectQuery(accounts, nil))
		assert.ErrorIs(t, err, domain.ErrTransactionClosed)
	})

	t.Run("rollback", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, &domain.TxOptions{Isolation: domain.IsolationLevelReadCommitted})
		require.NoError(t, err)
		mustExec(t, tx, insertCommand(accounts, val("id", 3)))
		mustExec(t, tx, updateCommand(accounts, []domain.FieldValue{val("balance", 0)}, where("id", domain.OperatorEqual, 1)))
		require.NoError(t, tx.Rollback())
		assert.False(t, tx.IsActive())
		assert.ErrorIs(t, tx.Rollback(), domain.ErrTransactionClosed)
		assert.Equal(t, []int{1, 2}, ids(t, s, accounts))
		assert.Equal(t, int64(1), count(t, s, accounts, where("balance", domain.OperatorEqual, 100)))
	})

	t.Run("read-only reads from the primary and rejects writes", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, &domain.TxOptions{ReadOnly: true, Isolation: domain.IsolationLevelSerializable})
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, ids(t, tx, accounts))
		assert.Equal(t, int64(2), count(t, tx, accounts))
		_, err = tx.Execute(ctx, insertCommand(accounts, val("id", 9)))
		assert.ErrorIs(t, err, domain.ErrInvalidTransaction)
		results, err := tx.Batch(ctx, []domain.Operation{
			{Type: domain.OperationTypeQuery, Query: countQuery(accounts)},
			{Type: domain.OperationTypeCommand, Command: insertCommand(accounts, val("id", 9))},
		})
		require.NoError(t, err)
		require.NoError(t, results[0].Error)
		assert.ErrorIs(t, results[1].Error, domain.ErrInvalidTransaction)
		require.NoError(t, tx.Commit())
		assert.Equal(t, []int{1, 2}, ids(t, s, accounts))
	})

	t.Run("every isolation level", func(t *testing.T) {
		levels := []domain.IsolationLevel{
			domain.IsolationLevelDefault, domain.IsolationLevelReadUncommitted, domain.IsolationLevelReadCommitted,
			domain.IsolationLevelRepeatableRead, domain.IsolationLevelSerializable,
		}
		for _, level := range levels {
			tx, err := s.BeginTx(ctx, &domain.TxOptions{Isolation: level, Timeout: 30 * time.Second})
			require.NoError(t, err)
			assert.Equal(t, int64(2), count(t, tx, accounts), "isolation %d", level)
			mustExec(t, tx, updateCommand(accounts, []domain.FieldValue{val("level", int(level))}, where("id", domain.OperatorEqual, 2)))
			require.NoError(t, tx.Commit(), "isolation %d", level)
		}
		assert.Equal(t, int64(1), count(t, s, accounts, where("level", domain.OperatorEqual, int(domain.IsolationLevelSerializable))))
	})

	t.Run("savepoints are unsupported", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		assert.ErrorIs(t, tx.Savepoint("sp1"), domain.ErrUnsupportedOperation)
		assert.ErrorIs(t, tx.RollbackToSavepoint("sp1"), domain.ErrUnsupportedOperation)
		assert.True(t, tx.IsActive())
	})

	t.Run("ids are unique", func(t *testing.T) {
		tx1, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx1.Rollback() }()
		tx2, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()
		assert.NotEmpty(t, tx1.ID())
		assert.NotEqual(t, tx1.ID(), tx2.ID())
	})

	t.Run("write conflicts are retryable", func(t *testing.T) {
		tx1, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx1.Rollback() }()
		tx2, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		bump := func(v int) domain.Command {
			return updateCommand(accounts, []domain.FieldValue{val("balance", v)}, where("id", domain.OperatorEqual, 1))
		}
		mustExec(t, tx1, bump(101))
		_, err = tx2.Execute(ctx, bump(102))
		require.Error(t, err)
		assert.True(t, domain.IsRetryable(err), "got %v", err)
		assert.ErrorIs(t, err, domain.ErrTransactionFailed)

		// The server aborted tx2, so its commit fails, and the transaction ends anyway.
		err = tx2.Commit()
		assert.ErrorIs(t, err, domain.ErrTransactionFailed)
		assert.True(t, domain.IsRetryable(err), "got %v", err)
		assert.False(t, tx2.IsActive())
	})

	t.Run("a timed-out transaction rejects operations", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, &domain.TxOptions{Timeout: 20 * time.Millisecond})
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		_, err = tx.Execute(ctx, insertCommand(accounts, val("id", 8)))
		assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
		assert.NoError(t, tx.Rollback(), "nothing was sent, so the abort is local")
		assert.False(t, tx.IsActive())
	})

	t.Run("Config.TxTimeout is the default", func(t *testing.T) {
		cfg := testConfig(t)
		cfg.TxTimeout = 20 * time.Millisecond
		short := connect(t, cfg)
		tx, err := short.BeginTx(ctx, nil)
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		_, err = tx.Query(ctx, selectQuery(accounts, nil))
		assert.ErrorIs(t, err, domain.ErrTransactionTimeout)
		assert.NoError(t, tx.Rollback())
	})

	t.Run("batch", func(t *testing.T) {
		tx, err := s.BeginTx(ctx, nil)
		require.NoError(t, err)
		results, err := tx.Batch(ctx, []domain.Operation{
			{Type: domain.OperationTypeCommand, Command: insertCommand(accounts, val("id", 4))},
			{Type: domain.OperationTypeQuery, Query: countQuery(accounts)},
		})
		require.NoError(t, err)
		require.NoError(t, results[0].Error)
		rows := rowsOf(t, results[1])
		require.True(t, rows.Next())
		var n int64
		require.NoError(t, rows.Scan(&n))
		assert.Equal(t, int64(3), n, "the batch query sees the batch's earlier insert")
		require.NoError(t, tx.Commit())
		assert.Equal(t, []int{1, 2, 4}, ids(t, s, accounts))
	})
}

func TestIntegration_Batch(t *testing.T) {
	s := openStorage(t)
	items := collection(t, s, "items")
	ctx := context.Background()

	results, err := s.Batch(ctx, []domain.Operation{
		{Type: domain.OperationTypeCommand, Command: insertCommand(items, val("_id", 1), val("name", "one"))},
		{Type: domain.OperationTypeCommand, Command: insertCommand(items, val("_id", 1), val("name", "duplicate"))},
		{Type: domain.OperationTypeCommand, Command: insertCommand(items, val("_id", 2), val("name", "two"))},
		{Type: domain.OperationTypeQuery, Query: func() domain.Query {
			q := selectQuery(items, []string{"name", "_id"})
			q.Spec.OrderBy = []domain.OrderBy{{Field: "_id"}}
			return q
		}()},
		{Type: domain.OperationTypeCommand, Command: updateCommand(items, []domain.FieldValue{val("name", "uno")}, where("_id", domain.OperatorEqual, 1))},
		{Type: domain.OperationTypeQuery, Query: domain.Query{SQL: "SELECT * FROM items"}},
		{Type: domain.OperationTypeCommand, Command: deleteCommand(items, where("_id", domain.OperatorEqual, 2))},
		{Type: domain.OperationTypeQuery, Query: countQuery(items)},
		{Type: domain.OperationType(42)},
	})
	require.NoError(t, err)
	require.Len(t, results, 9)
	for i, r := range results {
		assert.Equal(t, i, r.Index)
	}

	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1, LastInsertID: 1}, results[0].Result)
	assert.ErrorIs(t, results[1].Error, domain.ErrDuplicateKey, "only the failing operation reports the error")
	assert.Nil(t, results[1].Result)
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1, LastInsertID: 2}, results[2].Result)

	// Query results are materialised, in input order: the update comes later.
	rows := rowsOf(t, results[3])
	var names []string
	for rows.Next() {
		var name string
		var id int
		require.NoError(t, rows.Scan(&name, &id))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"one", "two"}, names)

	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1}, results[4].Result)
	assert.ErrorIs(t, results[5].Error, domain.ErrUnsupportedQuery)
	assert.Equal(t, domain.ExecuteResult{RowsAffected: 1}, results[6].Result)
	countRows := rowsOf(t, results[7])
	require.True(t, countRows.Next())
	var n int64
	require.NoError(t, countRows.Scan(&n))
	assert.Equal(t, int64(1), n)
	assert.ErrorIs(t, results[8].Error, domain.ErrInvalidQuery)

	row, err := s.QueryOne(ctx, selectQuery(items, []string{"name"}, where("_id", domain.OperatorEqual, 1)))
	require.NoError(t, err)
	var name string
	require.NoError(t, row.Scan(&name))
	assert.Equal(t, "uno", name)
}

func indexNamed(schema domain.TableSchema, name string) (domain.IndexDefinition, bool) {
	for _, idx := range schema.Indexes {
		if idx.Name == name {
			return idx, true
		}
	}
	return domain.IndexDefinition{}, false
}

func TestIntegration_Schema(t *testing.T) {
	s := openStorage(t)
	ctx := context.Background()
	users := collection(t, s, "users")

	require.NoError(t, s.CreateTable(ctx, domain.TableSchema{Name: users, Indexes: []domain.IndexDefinition{
		{Name: "by_email", Columns: []string{"email"}, Unique: true},
		{Name: "by_city_age_name", Columns: []string{"city", "age", "name"}},
	}}))
	assert.ErrorIs(t, s.CreateTable(ctx, domain.TableSchema{Name: users}), domain.ErrSchemaExists)

	tables, err := s.ListTables(ctx)
	require.NoError(t, err)
	assert.Contains(t, tables, users)

	schema, err := s.DescribeTable(ctx, users)
	require.NoError(t, err)
	assert.Equal(t, users, schema.Name)
	assert.Len(t, schema.Indexes, 2, "the _id index is not reported")
	email, ok := indexNamed(schema, "by_email")
	require.True(t, ok, "indexes keep the names they were created with: %+v", schema.Indexes)
	assert.Equal(t, domain.IndexDefinition{Name: "by_email", Columns: []string{"email"}, Unique: true}, email)
	compound, ok := indexNamed(schema, "by_city_age_name")
	require.True(t, ok)
	assert.Equal(t, []string{"city", "age", "name"}, compound.Columns, "compound key order is preserved")
	assert.False(t, compound.Unique)

	mustExec(t, s, insertCommand(users, val("email", "a@x")))
	_, err = s.Execute(ctx, insertCommand(users, val("email", "a@x")))
	assert.ErrorIs(t, err, domain.ErrDuplicateKey)

	require.NoError(t, s.AlterTable(ctx, users, []domain.SchemaChange{
		{Type: domain.SchemaChangeTypeAddIndex, Index: domain.IndexDefinition{Name: "by_name", Columns: []string{"name"}}},
		{Type: domain.SchemaChangeTypeAddIndex, Index: domain.IndexDefinition{Columns: []string{"age"}}},
		{Type: domain.SchemaChangeTypeAddIndex, Index: domain.IndexDefinition{Name: "by_hash", Columns: []string{"token"}, Type: domain.IndexTypeHash}},
	}))
	schema, err = s.DescribeTable(ctx, users)
	require.NoError(t, err)
	_, ok = indexNamed(schema, "by_name")
	assert.True(t, ok)
	_, ok = indexNamed(schema, "age_1")
	assert.True(t, ok, "an unnamed index gets the server's default name")
	hashed, ok := indexNamed(schema, "by_hash")
	require.True(t, ok)
	assert.Equal(t, domain.IndexTypeHash, hashed.Type)

	drop := func(name string) error {
		return s.AlterTable(ctx, users, []domain.SchemaChange{{Type: domain.SchemaChangeTypeDropIndex, Index: domain.IndexDefinition{Name: name}}})
	}
	require.NoError(t, drop("by_name"))
	schema, err = s.DescribeTable(ctx, users)
	require.NoError(t, err)
	_, ok = indexNamed(schema, "by_name")
	assert.False(t, ok, "indexes can be dropped by the name they were created with")
	assert.ErrorIs(t, drop("by_name"), domain.ErrIndexNotFound)
	assert.ErrorIs(t, drop(""), domain.ErrInvalidSchema)

	err = s.AlterTable(ctx, users, []domain.SchemaChange{
		{Type: domain.SchemaChangeTypeAddIndex, Index: domain.IndexDefinition{Name: "never_created", Columns: []string{"x"}}},
		{Type: domain.SchemaChangeTypeAddColumn, Column: domain.ColumnDefinition{Name: "x"}},
	})
	assert.ErrorIs(t, err, domain.ErrUnsupportedOperation)
	schema, err = s.DescribeTable(ctx, users)
	require.NoError(t, err)
	_, ok = indexNamed(schema, "never_created")
	assert.False(t, ok, "changes are validated before any is applied")

	missing := collection(t, s, "missing")
	_, err = s.DescribeTable(ctx, missing)
	assert.ErrorIs(t, err, domain.ErrTableNotFound)
	err = s.AlterTable(ctx, missing, []domain.SchemaChange{
		{Type: domain.SchemaChangeTypeAddIndex, Index: domain.IndexDefinition{Name: "i", Columns: []string{"x"}}},
	})
	assert.ErrorIs(t, err, domain.ErrTableNotFound)
	tables, err = s.ListTables(ctx)
	require.NoError(t, err)
	assert.NotContains(t, tables, missing, "AlterTable must not create the collection implicitly")

	// Hashed indexes cannot be unique: CreateTable fails without leaving the collection behind.
	invalid := collection(t, s, "invalid")
	err = s.CreateTable(ctx, domain.TableSchema{Name: invalid, Indexes: []domain.IndexDefinition{
		{Name: "bad", Columns: []string{"x"}, Type: domain.IndexTypeHash, Unique: true},
	}})
	require.Error(t, err)
	tables, err = s.ListTables(ctx)
	require.NoError(t, err)
	assert.NotContains(t, tables, invalid)

	require.NoError(t, s.DropTable(ctx, users))
	require.NoError(t, s.DropTable(ctx, users), "dropping a missing collection is not an error")
	_, err = s.DescribeTable(ctx, users)
	assert.ErrorIs(t, err, domain.ErrTableNotFound)
}

func TestIntegration_HealthAndInfo(t *testing.T) {
	s := openStorage(t)
	ctx := context.Background()
	require.NoError(t, s.Connect(ctx))
	require.NoError(t, s.Ping(ctx))

	health := s.Health(ctx)
	assert.Equal(t, domain.HealthStatusHealthy, health.Status, health.Message)
	assert.Equal(t, "MongoDB is healthy", health.Message)
	assert.NotEmpty(t, health.Details["version"])

	info := s.Info()
	assert.Equal(t, "mongodb", info.Name)
	assert.Equal(t, domain.DatabaseTypeMongoDB, info.DatabaseType)
	assert.Subset(t, []string{"transactions", "batch", "schema", "json", "arrays"}, info.Features)
	assert.NotContains(t, info.Features, "gridfs")

	require.NoError(t, s.Close())
	assert.ErrorIs(t, s.Ping(ctx), domain.ErrConnectionFailed)
	assert.Equal(t, domain.HealthStatusUnhealthy, s.Health(ctx).Status)
}

func TestIntegration_Timeouts(t *testing.T) {
	s := openStorage(t)
	items := collection(t, s, "items")
	ctx := context.Background()
	for i := 0; i < 250; i++ {
		mustExec(t, s, insertCommand(items, val("id", i)))
	}

	q := selectQuery(items, nil)
	q.Options.Timeout = time.Nanosecond
	_, err := s.Query(ctx, q)
	assert.ErrorIs(t, err, domain.ErrQueryTimeout)
	assert.True(t, domain.IsTimeoutError(err))

	cmd := insertCommand(items, val("id", -1))
	cmd.Options.Timeout = time.Nanosecond
	_, err = s.Execute(ctx, cmd)
	assert.ErrorIs(t, err, domain.ErrQueryTimeout)

	cfg := testConfig(t)
	cfg.QueryTimeout = time.Nanosecond
	short := connect(t, cfg)
	_, err = short.QueryOne(ctx, countQuery(items))
	assert.ErrorIs(t, err, domain.ErrQueryTimeout, "Config.QueryTimeout is the default")

	// Iteration runs under the query's context: once it is canceled, fetching
	// the next batch fails instead of silently continuing.
	qctx, cancel := context.WithCancel(ctx)
	res, err := s.Query(qctx, selectQuery(items, []string{"id"}))
	require.NoError(t, err)
	require.True(t, res.Next())
	cancel()
	n := 1
	for res.Next() {
		n++
	}
	assert.Less(t, n, 250, "only the first batch is available after cancellation")
	assert.Error(t, res.Err())
	assert.NoError(t, res.Close())

	// Without cancellation every batch is read.
	res, err = s.Query(ctx, selectQuery(items, []string{"id"}))
	require.NoError(t, err)
	n = 0
	for res.Next() {
		n++
	}
	require.NoError(t, res.Err())
	assert.Equal(t, 250, n)
	assert.NoError(t, res.Close())
}

func TestIntegration_ConnectTimeout(t *testing.T) {
	testConfig(t) // skips without MONGODB_URI
	start := time.Now()
	_, err := mongodb.NewAdapter().Connect(context.Background(), domain.Config{
		Host: "127.0.0.1", Port: 1, Database: testDatabase, ConnectTimeout: 300 * time.Millisecond,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrConnectionTimeout)
	assert.Less(t, time.Since(start), 5*time.Second, "ConnectTimeout bounds Connect")
}
