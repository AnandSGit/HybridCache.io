//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// PostgreSQL-specific behaviour: SQL-only query features, raw SQL, atomic
// batches and savepoints.

// seedOrders creates an orders table: Alice (1) has 2 orders, Bob (2) has 1
// and Dave (4) has 3; the other users have none.
func seedOrders(t *testing.T, store storage.Storage) string {
	t.Helper()
	table := newTable(t, store, "orders")
	require.NoError(t, store.CreateTable(t.Context(), storage.TableSchema{
		Name: table,
		Columns: []storage.ColumnDefinition{
			{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true},
			{Name: "user_id", DataType: storage.DataTypeInteger},
			{Name: "total", DataType: storage.DataTypeInteger},
		},
	}))
	for _, o := range []struct{ id, userID, total int }{
		{1, 1, 120}, {2, 1, 80}, {3, 2, 40}, {4, 4, 300}, {5, 4, 15}, {6, 4, 60},
	} {
		execute(t, store, storage.Insert(table).Set("id", o.id).Set("user_id", o.userID).Set("total", o.total))
	}
	return table
}

func TestPostgreSQL_Joins(t *testing.T) {
	store := connectPostgres(t)
	users, orders := seedUsers(t, store), seedOrders(t, store)

	t.Run("inner join on a column reference", func(t *testing.T) {
		rows, err := store.Query(t.Context(), build(t, storage.NewBuilder().
			Select("u.name", "o.total").
			From(users+" u").
			InnerJoin(orders+" o", storage.Equal("o.user_id", storage.Col("u.id"))).
			Where(storage.GreaterThan("o.total", 50)).
			OrderByAsc("o.id")))
		require.NoError(t, err)
		defer rows.Close()

		type line struct {
			name  string
			total int
		}
		var got []line
		for rows.Next() {
			var l line
			require.NoError(t, rows.Scan(&l.name, &l.total))
			got = append(got, l)
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, []line{{"Alice", 120}, {"Alice", 80}, {"Dave", 300}, {"Dave", 60}}, got)
	})

	t.Run("left join with group by and having", func(t *testing.T) {
		rows, err := store.Query(t.Context(), build(t, storage.NewBuilder().
			Select("u.name", "COUNT(o.id) AS order_count").
			From(users+" u").
			LeftJoin(orders+" o", storage.Equal("o.user_id", storage.Col("u.id"))).
			GroupBy("u.id", "u.name").
			Having(storage.LessThan("COUNT(o.id)", 2)).
			OrderByDesc("order_count").
			OrderByAsc("u.name")))
		require.NoError(t, err)
		defer rows.Close()

		got := map[string]int64{}
		var names []string
		for rows.Next() {
			var name string
			var n int64
			require.NoError(t, rows.Scan(&name, &n))
			got[name] = n
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, []string{"Bob", "Carol", "Erin", "Frank"}, names)
		assert.Equal(t, map[string]int64{"Bob": 1, "Carol": 0, "Erin": 0, "Frank": 0}, got)
	})

	t.Run("exists and not exists subqueries", func(t *testing.T) {
		names := func(cond storage.Condition) []string {
			rows, err := store.Query(t.Context(), build(t, storage.NewBuilder().
				Select("u.name").From(users+" u").Where(cond).OrderByAsc("u.name")))
			require.NoError(t, err)
			defer rows.Close()
			var got []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				got = append(got, name)
			}
			require.NoError(t, rows.Err())
			return got
		}
		ordersOf := func() storage.QueryBuilder {
			return storage.NewBuilder().From(orders + " o").Where(storage.Equal("o.user_id", storage.Col("u.id")))
		}

		assert.Equal(t, []string{"Alice", "Dave"}, names(storage.Exists(ordersOf().Where(storage.GreaterThan("o.total", 100)))))
		assert.Equal(t, []string{"Carol", "Erin", "Frank"}, names(storage.NotExists(ordersOf())))
	})

	t.Run("select from a subquery", func(t *testing.T) {
		older := storage.NewBuilder().Select("id", "age").From(users).Where(storage.GreaterThan("age", 30))
		assert.Equal(t, []int{5, 3, 1}, ids(t, store, storage.NewBuilder().
			Select("s.id").FromSubquery(older, "s").OrderByDesc("s.age")))
	})
}

func TestPostgreSQL_RawSQL(t *testing.T) {
	store := connectPostgres(t)
	users := seedUsers(t, store)

	// Raw SQL may use '?' placeholders, which the adapter rewrites to $n.
	res, err := store.Execute(t.Context(), storage.Command{
		SQL:        "UPDATE " + users + " SET age = age + ? WHERE active = ?",
		Parameters: []interface{}{1, false},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.RowsAffected)

	var total int64
	require.NoError(t, scanOne(t, store, storage.Query{
		SQL:        "SELECT SUM(age) FROM " + users + " WHERE active = ?",
		Parameters: []interface{}{false},
	}, &total))
	assert.Equal(t, int64(41+52+2), total)
}

func TestPostgreSQL_BatchIsAtomic(t *testing.T) {
	store := connectPostgres(t)
	users := seedUsers(t, store)

	duplicate := grace
	duplicate.ID, duplicate.Email = 8, "alice@example.com"
	results, err := store.Batch(t.Context(), []storage.Operation{
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(users, grace))},
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(users, duplicate))},
		{Type: storage.OperationTypeQuery, Query: build(t, storage.NewBuilder().SelectCount("*").From(users))},
	})

	// The batch runs in one implicit transaction: the failure undoes the first
	// insert and the operations after it do not run.
	assert.ErrorIs(t, err, storage.ErrDuplicateKey)
	require.Len(t, results, 3)
	assert.ErrorIs(t, results[1].Error, storage.ErrDuplicateKey)
	assert.Error(t, results[2].Error)
	assert.Nil(t, results[2].Result)
	assert.Equal(t, fixtureUsers, allUsers(t, store, users))
}

func TestPostgreSQL_Savepoints(t *testing.T) {
	store := connectPostgres(t)
	users := seedUsers(t, store)
	ctx := t.Context()

	tx, err := store.BeginTx(ctx, &storage.TxOptions{Isolation: storage.IsolationLevelSerializable})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	_, err = tx.Execute(ctx, buildCommand(t, storage.Update(users).Set("age", 35).Where(storage.Equal("id", 1))))
	require.NoError(t, err)
	require.NoError(t, tx.Savepoint("before_delete"))
	_, err = tx.Execute(ctx, buildCommand(t, storage.Delete(users).Where(storage.Equal("active", false))))
	require.NoError(t, err)
	assert.Equal(t, int64(4), count(t, tx, users))

	require.NoError(t, tx.RollbackToSavepoint("before_delete"))
	require.NoError(t, tx.Commit())

	want := append([]user(nil), fixtureUsers...)
	want[0].Age = 35
	assert.Equal(t, want, allUsers(t, store, users))
}

func TestPostgreSQL_DescribeTable(t *testing.T) {
	store := connectPostgres(t)
	users := seedUsers(t, store)

	schema, err := store.DescribeTable(t.Context(), users)
	require.NoError(t, err)
	assert.Equal(t, []storage.ColumnDefinition{
		{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true},
		{Name: "name", DataType: storage.DataTypeString, Length: 50},
		{Name: "email", DataType: storage.DataTypeString, Length: 100},
		{Name: "age", DataType: storage.DataTypeInteger},
		{Name: "active", DataType: storage.DataTypeBoolean},
		{Name: "nickname", DataType: storage.DataTypeString, Length: 50, Nullable: true},
	}, schema.Columns)
}
