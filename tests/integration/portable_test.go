//go:build integration

package integration

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// portableCases run unchanged against every backend, each on a freshly seeded
// users table: the same application code must give the same results on
// PostgreSQL and MongoDB.
var portableCases = []struct {
	name string
	run  func(t *testing.T, store storage.Storage, table string)
}{
	{"filters", testFilters},
	{"where, where-or, order by and pagination", testPagination},
	{"count", testCount},
	{"scan rows into a struct", testScanRow},
	{"query one", testQueryOne},
	{"query one without a match reports ErrNoRows", testQueryOneNoRows},
	{"duplicate key", testDuplicateKey},
	{"update changes only matching rows", testUpdate},
	{"delete removes only matching rows", testDelete},
	{"transaction commit", testCommit},
	{"transaction rollback", testRollback},
	{"batch", testBatch},
	{"schema", testSchema},
}

func TestPortable(t *testing.T) {
	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()
			store := b.connect(t)

			require.NoError(t, store.Ping(t.Context()))
			assert.Equal(t, storage.HealthStatusHealthy, store.Health(t.Context()).Status)
			info := store.Info()
			assert.Equal(t, b.databaseType, info.DatabaseType)
			assert.Equal(t, b.name, info.Name)
			assert.Contains(t, info.Features, "transactions")

			for _, c := range portableCases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					c.run(t, store, seedUsers(t, store))
				})
			}
		})
	}
}

func testFilters(t *testing.T, store storage.Storage, table string) {
	tests := []struct {
		name  string
		where []storage.Condition
		want  []int
	}{
		{"equal", []storage.Condition{storage.Equal("active", true)}, []int{1, 2, 4, 6}},
		{"not equal", []storage.Condition{storage.NotEqual("active", true)}, []int{3, 5}},
		{"greater than", []storage.Condition{storage.GreaterThan("age", 34)}, []int{3, 5}},
		{"greater than or equal", []storage.Condition{storage.GreaterThanOrEqual("age", 34)}, []int{1, 3, 5}},
		{"less than", []storage.Condition{storage.LessThan("age", 27)}, []int{4}},
		{"less than or equal", []storage.Condition{storage.LessThanOrEqual("age", 27)}, []int{2, 4}},
		{"like is case-sensitive", []storage.Condition{storage.Like("name", "%a%")}, []int{3, 4, 6}},
		{"like with a single-character wildcard", []storage.Condition{storage.Like("name", "_a%")}, []int{3, 4}},
		{"not like", []storage.Condition{storage.NotLike("name", "%a%")}, []int{1, 2, 5}},
		{"in", []storage.Condition{storage.In("name", "Bob", "Erin", "Zed")}, []int{2, 5}},
		{"not in", []storage.Condition{storage.NotIn("id", 1, 2, 3)}, []int{4, 5, 6}},
		{"empty in matches no rows", []storage.Condition{storage.In("id")}, []int{}},
		{"empty not in matches every row", []storage.Condition{storage.NotIn("id")}, []int{1, 2, 3, 4, 5, 6}},
		{"between is inclusive", []storage.Condition{storage.Between("age", 27, 34)}, []int{1, 2, 6}},
		{"not between", []storage.Condition{storage.NotBetween("age", 27, 34)}, []int{3, 4, 5}},
		{"is null", []storage.Condition{storage.IsNull("nickname")}, []int{2, 3, 5, 6}},
		{"is not null", []storage.Condition{storage.IsNotNull("nickname")}, []int{1, 4}},
		{"conditions are combined with AND", []storage.Condition{storage.Equal("active", true), storage.LessThan("age", 30)}, []int{2, 4}},
		{"one field in two conditions", []storage.Condition{storage.GreaterThan("age", 20), storage.LessThan("age", 40)}, []int{1, 2, 6}},
		{"nested groups", []storage.Condition{storage.Or(
			storage.Equal("name", "Alice"),
			storage.And(storage.Equal("active", false), storage.LessThan("age", 50)),
		)}, []int{1, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := storage.NewBuilder().Select("id").From(table).OrderByAsc("id")
			for _, c := range tt.where {
				b = b.Where(c)
			}
			assert.Equal(t, tt.want, ids(t, store, b))
		})
	}
}

func testPagination(t *testing.T, store storage.Storage, table string) {
	page := func(n int) storage.QueryBuilder {
		return storage.NewBuilder().
			Select("id").
			From(table).
			Where(storage.Equal("active", true)).
			WhereOr(storage.LessThan("age", 30), storage.Like("name", "F%")).
			OrderByDesc("age").
			Page(n, 2)
	}
	// Active users younger than 30 or named F...: Frank (30), Bob (27), Dave (19).
	assert.Equal(t, []int{6, 2}, ids(t, store, page(1)))
	assert.Equal(t, []int{4}, ids(t, store, page(2)))
	assert.Equal(t, []int{}, ids(t, store, page(3)))

	all := storage.NewBuilder().Select("id").From(table).OrderByAsc("id")
	assert.Equal(t, []int{5, 6}, ids(t, store, all.Offset(4)))
	assert.Equal(t, []int{}, ids(t, store, storage.NewBuilder().Select("id").From(table).Limit(0)),
		"LIMIT 0 returns no rows")
}

func testCount(t *testing.T, store storage.Storage, table string) {
	assert.Equal(t, int64(6), count(t, store, table))
	assert.Equal(t, int64(4), count(t, store, table, storage.Equal("active", true)))
	assert.Equal(t, int64(0), count(t, store, table, storage.GreaterThan("age", 100)))

	// COUNT(column) does not count NULLs.
	var n int64
	require.NoError(t, scanOne(t, store, build(t, storage.NewBuilder().SelectCount("nickname").From(table)), &n))
	assert.Equal(t, int64(2), n)
}

func testScanRow(t *testing.T, store storage.Storage, table string) {
	assert.Equal(t, fixtureUsers, allUsers(t, store, table))

	// Positional Scan follows the select list.
	rows, err := store.Query(t.Context(), build(t, storage.NewBuilder().
		Select("email", "id", "nickname").
		From(table).
		Where(storage.In("id", 1, 2)).
		OrderByAsc("id")))
	require.NoError(t, err)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"email", "id", "nickname"}, columns)

	type scanned struct {
		email    string
		id       int
		nickname *string
	}
	var got []scanned
	for rows.Next() {
		var s scanned
		require.NoError(t, rows.Scan(&s.email, &s.id, &s.nickname))
		got = append(got, s)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []scanned{
		{"alice@example.com", 1, nickname("Al")},
		{"bob@example.com", 2, nil},
	}, got)
}

func testQueryOne(t *testing.T, store storage.Storage, table string) {
	var name string
	var age int
	q := build(t, storage.NewBuilder().Select("name", "age").From(table).Where(storage.Equal("email", "dave@example.com")))
	require.NoError(t, scanOne(t, store, q, &name, &age))
	assert.Equal(t, "Dave", name)
	assert.Equal(t, 19, age)

	// The first row in query order.
	q = build(t, storage.NewBuilder().From(table).Where(storage.Equal("active", false)).OrderByDesc("age"))
	row, err := store.QueryOne(t.Context(), q)
	require.NoError(t, err)
	var u user
	require.NoError(t, row.ScanRow(&u))
	assert.Equal(t, fixtureUsers[4], u) // Erin, the oldest inactive user
}

func testQueryOneNoRows(t *testing.T, store storage.Storage, table string) {
	var name string
	q := build(t, storage.NewBuilder().Select("name").From(table).Where(storage.Equal("email", "nobody@example.com")))
	err := scanOne(t, store, q, &name)
	assert.ErrorIs(t, err, storage.ErrNoRows)
	assert.True(t, storage.IsDataError(err))
	assert.Empty(t, name)
}

func testDuplicateKey(t *testing.T, store storage.Storage, table string) {
	duplicate := fixtureUsers[0]
	duplicate.ID, duplicate.Name = 7, "Alice Again"
	_, err := store.Execute(t.Context(), buildCommand(t, insertUser(table, duplicate)))
	assert.ErrorIs(t, err, storage.ErrDuplicateKey)
	assert.True(t, storage.IsDataError(err))
	assert.Equal(t, fixtureUsers, allUsers(t, store, table))
}

func testUpdate(t *testing.T, store storage.Storage, table string) {
	res := execute(t, store, storage.Update(table).
		Set("age", 35).
		Set("nickname", "Ally").
		Where(storage.Equal("email", "alice@example.com")))
	assert.Equal(t, int64(1), res.RowsAffected)

	res = execute(t, store, storage.Update(table).Set("active", true).Where(storage.Equal("active", false)))
	assert.Equal(t, int64(2), res.RowsAffected)

	res = execute(t, store, storage.Update(table).Set("age", 99).Where(storage.Equal("email", "nobody@example.com")))
	assert.Equal(t, int64(0), res.RowsAffected)

	want := slices.Clone(fixtureUsers)
	want[0].Age, want[0].Nickname = 35, nickname("Ally")
	want[2].Active, want[4].Active = true, true
	assert.Equal(t, want, allUsers(t, store, table))
}

func testDelete(t *testing.T, store storage.Storage, table string) {
	res := execute(t, store, storage.Delete(table).Where(storage.LessThan("age", 30)))
	assert.Equal(t, int64(2), res.RowsAffected) // Bob (27) and Dave (19)

	res = execute(t, store, storage.Delete(table).Where(storage.Equal("name", "Nobody")))
	assert.Equal(t, int64(0), res.RowsAffected)

	assert.Equal(t, []int{1, 3, 5, 6}, ids(t, store, storage.NewBuilder().Select("id").From(table).OrderByAsc("id")))
}

func testCommit(t *testing.T, store storage.Storage, table string) {
	ctx := t.Context()
	tx, err := store.BeginTx(ctx, &storage.TxOptions{Isolation: storage.IsolationLevelReadCommitted, Timeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() }) // no-op after a commit
	assert.True(t, tx.IsActive())
	assert.NotEmpty(t, tx.ID())

	res, err := tx.Execute(ctx, buildCommand(t, insertUser(table, grace)))
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.RowsAffected)
	res, err = tx.Execute(ctx, buildCommand(t, storage.Update(table).Set("age", 35).Where(storage.Equal("id", 1))))
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.RowsAffected)

	assert.Equal(t, int64(7), count(t, tx, table), "a transaction sees its own writes")
	assert.Equal(t, int64(6), count(t, store, table), "uncommitted writes are not visible outside the transaction")

	require.NoError(t, tx.Commit())
	assert.False(t, tx.IsActive())

	want := append(slices.Clone(fixtureUsers), grace)
	want[0].Age = 35
	assert.Equal(t, want, allUsers(t, store, table))

	assert.ErrorIs(t, tx.Commit(), storage.ErrTransactionClosed)
	_, err = tx.Execute(ctx, buildCommand(t, storage.Delete(table).Where(storage.Equal("id", 7))))
	assert.ErrorIs(t, err, storage.ErrTransactionClosed)
	assert.Equal(t, int64(7), count(t, store, table))
}

func testRollback(t *testing.T, store storage.Storage, table string) {
	ctx := t.Context()
	tx, err := store.BeginTx(ctx, nil)
	require.NoError(t, err)
	other, err := store.BeginTx(ctx, nil)
	require.NoError(t, err)
	assert.NotEqual(t, tx.ID(), other.ID())
	require.NoError(t, other.Rollback())

	_, err = tx.Execute(ctx, buildCommand(t, insertUser(table, grace)))
	require.NoError(t, err)
	res, err := tx.Execute(ctx, buildCommand(t, storage.Delete(table).Where(storage.Equal("active", false))))
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.RowsAffected)
	assert.Equal(t, int64(5), count(t, tx, table))

	require.NoError(t, tx.Rollback())
	assert.False(t, tx.IsActive())
	assert.Equal(t, fixtureUsers, allUsers(t, store, table))
	assert.ErrorIs(t, tx.Rollback(), storage.ErrTransactionClosed)
}

func testBatch(t *testing.T, store storage.Storage, table string) {
	operations := []storage.Operation{
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(table, grace))},
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, storage.Update(table).Set("active", false).Where(storage.Equal("id", 1)))},
		{Type: storage.OperationTypeQuery, Query: build(t, storage.NewBuilder().Select("id").From(table).Where(storage.Equal("active", true)).OrderByAsc("id"))},
	}

	results, err := store.Batch(t.Context(), operations)
	require.NoError(t, err)
	require.Len(t, results, len(operations))
	for i, r := range results {
		assert.Equal(t, i, r.Index)
		assert.NoError(t, r.Error, "operation %d", i)
	}
	assert.Equal(t, storage.ExecuteResult{RowsAffected: 1}, results[0].Result)
	assert.Equal(t, storage.ExecuteResult{RowsAffected: 1}, results[1].Result)

	// Operations run in order, and query rows stay readable after Batch returns.
	rows, ok := results[2].Result.(storage.Result)
	require.True(t, ok, "query result is %T", results[2].Result)
	defer rows.Close()
	assert.Equal(t, []int{2, 4, 6, 7}, scanIDs(t, rows))
}

func testSchema(t *testing.T, store storage.Storage, table string) {
	ctx := t.Context()
	assert.ErrorIs(t, store.CreateTable(ctx, usersSchema(table)), storage.ErrSchemaExists)

	tables, err := store.ListTables(ctx)
	require.NoError(t, err)
	assert.Contains(t, tables, table)

	schema, err := store.DescribeTable(ctx, table)
	require.NoError(t, err)
	assert.Equal(t, table, schema.Name)

	require.NoError(t, store.DropTable(ctx, table))
	tables, err = store.ListTables(ctx)
	require.NoError(t, err)
	assert.NotContains(t, tables, table)

	_, err = store.DescribeTable(ctx, table)
	assert.ErrorIs(t, err, storage.ErrTableNotFound)
	assert.NoError(t, store.DropTable(ctx, table), "dropping a missing table is not an error")
}
