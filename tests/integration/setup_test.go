//go:build integration

// Package integration holds end-to-end tests that use the library only through
// pkg/storage, as an application would. Run them against the databases of
// docker-compose.yml (make dev-up) with
//
//	POSTGRES_DSN='postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable' \
//	MONGODB_URI='mongodb://localhost:27017/?replicaSet=rs0&directConnection=true' \
//	go test -tags integration ./tests/integration/
//
// The tests for a database are skipped when its variable is unset. MongoDB
// uses the database named in MONGODB_URI, or hc_go_it. Every test works on
// tables (collections) of its own and drops them afterwards.
package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// defaultMongoDatabase is used when MONGODB_URI names no database.
const defaultMongoDatabase = "hc_go_it"

// backend is a database the portable tests run against.
type backend struct {
	name         string
	databaseType storage.DatabaseType
	connect      func(t *testing.T) storage.Storage // skips the test when unconfigured
}

var backends = []backend{
	{"postgresql", storage.DatabaseTypePostgreSQL, connectPostgres},
	{"mongodb", storage.DatabaseTypeMongoDB, connectMongoDB},
}

func connectPostgres(t *testing.T) storage.Storage {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN is not set")
	}
	return connect(t, storage.NewPostgreSQLAdapter(), dsn, "")
}

func connectMongoDB(t *testing.T) storage.Storage {
	t.Helper()
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI is not set")
	}
	return connect(t, storage.NewMongoDBAdapter(), uri, defaultMongoDatabase)
}

// connect connects with the adapter's parsed DSN, keeping its default pool
// settings, and closes the storage when the test ends.
func connect(t *testing.T, adapter storage.Adapter, dsn, defaultDatabase string) storage.Storage {
	t.Helper()
	config, err := adapter.ParseDSN(dsn)
	require.NoError(t, err)
	if config.Database == "" {
		config.Database = defaultDatabase
	}
	config.ConnectTimeout = 10 * time.Second
	config.QueryTimeout = 30 * time.Second

	store, err := adapter.Connect(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// newTable returns a table name unique to this run and drops the table when
// the test ends.
func newTable(t *testing.T, store storage.Storage, prefix string) string {
	t.Helper()
	var b [4]byte
	_, _ = rand.Read(b[:])
	name := fmt.Sprintf("it_%s_%x", prefix, b)
	t.Cleanup(func() {
		if err := store.DropTable(context.Background(), name); err != nil {
			t.Errorf("drop table %s: %v", name, err)
		}
	})
	return name
}

// user is a row of the users fixture. Its untagged fields map to the columns
// by name with both adapters.
type user struct {
	ID       int
	Name     string
	Email    string
	Age      int
	Active   bool
	Nickname *string
}

func nickname(s string) *string { return &s }

// fixtureUsers is the content of a seeded users table, in id order.
var fixtureUsers = []user{
	{ID: 1, Name: "Alice", Email: "alice@example.com", Age: 34, Active: true, Nickname: nickname("Al")},
	{ID: 2, Name: "Bob", Email: "bob@example.com", Age: 27, Active: true},
	{ID: 3, Name: "Carol", Email: "carol@example.com", Age: 41, Active: false},
	{ID: 4, Name: "Dave", Email: "dave@example.com", Age: 19, Active: true, Nickname: nickname("D")},
	{ID: 5, Name: "Erin", Email: "erin@example.com", Age: 52, Active: false},
	{ID: 6, Name: "Frank", Email: "frank@example.com", Age: 30, Active: true},
}

// grace is a user that is not in the fixture.
var grace = user{ID: 7, Name: "Grace", Email: "grace@example.com", Age: 45, Active: true}

// usersSchema describes the users table; email has a unique index, which
// both databases enforce.
func usersSchema(table string) storage.TableSchema {
	return storage.TableSchema{
		Name: table,
		Columns: []storage.ColumnDefinition{
			{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true},
			{Name: "name", DataType: storage.DataTypeString, Length: 50},
			{Name: "email", DataType: storage.DataTypeString, Length: 100},
			{Name: "age", DataType: storage.DataTypeInteger},
			{Name: "active", DataType: storage.DataTypeBoolean},
			{Name: "nickname", DataType: storage.DataTypeString, Length: 50, Nullable: true},
		},
		Indexes: []storage.IndexDefinition{
			{Name: table + "_email_key", Columns: []string{"email"}, Unique: true},
		},
	}
}

// seedUsers creates a users table holding fixtureUsers and returns its name.
func seedUsers(t *testing.T, store storage.Storage) string {
	t.Helper()
	table := newTable(t, store, "users")
	require.NoError(t, store.CreateTable(t.Context(), usersSchema(table)))
	for _, u := range fixtureUsers {
		res := execute(t, store, insertUser(table, u))
		require.Equal(t, int64(1), res.RowsAffected)
	}
	return table
}

func insertUser(table string, u user) *storage.CommandBuilder {
	var nick interface{}
	if u.Nickname != nil {
		nick = *u.Nickname
	}
	return storage.Insert(table).
		Set("id", u.ID).
		Set("name", u.Name).
		Set("email", u.Email).
		Set("age", u.Age).
		Set("active", u.Active).
		Set("nickname", nick)
}

// querier is the read side shared by Storage and Transaction.
type querier interface {
	Query(ctx context.Context, query storage.Query) (storage.Result, error)
	QueryOne(ctx context.Context, query storage.Query) (storage.Row, error)
}

func build(t *testing.T, b storage.QueryBuilder) storage.Query {
	t.Helper()
	q, err := b.Build()
	require.NoError(t, err)
	return q
}

func buildCommand(t *testing.T, b *storage.CommandBuilder) storage.Command {
	t.Helper()
	c, err := b.Build()
	require.NoError(t, err)
	return c
}

func execute(t *testing.T, store storage.Storage, b *storage.CommandBuilder) storage.ExecuteResult {
	t.Helper()
	res, err := store.Execute(t.Context(), buildCommand(t, b))
	require.NoError(t, err)
	return res
}

// scanOne runs QueryOne and scans its row. An adapter may report a missing row
// from QueryOne or from Row.Scan; scanOne returns that error either way.
func scanOne(t *testing.T, q querier, query storage.Query, dest ...interface{}) error {
	t.Helper()
	row, err := q.QueryOne(t.Context(), query)
	if err != nil {
		return err
	}
	return row.Scan(dest...)
}

// ids runs a query that selects only the id column and returns the ids in
// result order.
func ids(t *testing.T, q querier, b storage.QueryBuilder) []int {
	t.Helper()
	rows, err := q.Query(t.Context(), build(t, b))
	require.NoError(t, err)
	defer rows.Close()
	return scanIDs(t, rows)
}

func scanIDs(t *testing.T, rows storage.Result) []int {
	t.Helper()
	got := []int{}
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		got = append(got, id)
	}
	require.NoError(t, rows.Err())
	return got
}

// allUsers returns the rows of a users table in id order.
func allUsers(t *testing.T, q querier, table string) []user {
	t.Helper()
	rows, err := q.Query(t.Context(), build(t, storage.NewBuilder().From(table).OrderByAsc("id")))
	require.NoError(t, err)
	defer rows.Close()

	var got []user
	for rows.Next() {
		var u user
		require.NoError(t, rows.ScanRow(&u))
		got = append(got, u)
	}
	require.NoError(t, rows.Err())
	return got
}

// count returns the number of rows matching all conditions.
func count(t *testing.T, q querier, table string, conditions ...storage.Condition) int64 {
	t.Helper()
	b := storage.NewBuilder().SelectCount("*").From(table)
	for _, c := range conditions {
		b = b.Where(c)
	}
	var n int64
	require.NoError(t, scanOne(t, q, build(t, b), &n))
	return n
}
