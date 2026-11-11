//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// MongoDB-specific behaviour: SQL-only features are rejected rather than
// ignored, batches run each operation on its own, and there are no savepoints.

func TestMongoDB_RejectsSQLOnlyQueries(t *testing.T) {
	store := connectMongoDB(t)
	users := seedUsers(t, store)
	assert.False(t, storage.NewMongoDBAdapter().SupportsJoins())

	queries := []struct {
		name  string
		query storage.Query
	}{
		{"join", build(t, storage.NewBuilder().
			Select("u.name").
			From(users+" u").
			InnerJoin("orders o", storage.Equal("o.user_id", storage.Col("u.id"))))},
		{"group by", build(t, storage.NewBuilder().Select("active").From(users).GroupBy("active"))},
		{"exists subquery", build(t, storage.NewBuilder().From(users).Where(storage.Exists(storage.NewBuilder().From("orders"))))},
		{"comparison with a column", build(t, storage.NewBuilder().From(users).Where(storage.Equal("age", storage.Col("id"))))},
		{"raw SQL", storage.Query{SQL: "SELECT * FROM " + users}},
	}
	for _, tt := range queries {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Query(t.Context(), tt.query)
			assert.ErrorIs(t, err, storage.ErrUnsupportedQuery)
		})
	}

	commands := []struct {
		name    string
		command storage.Command
	}{
		{"delete with a column comparison", buildCommand(t, storage.Delete(users).Where(storage.Equal("age", storage.Col("id"))))},
		{"update with a column comparison", buildCommand(t, storage.Update(users).Set("active", false).Where(storage.NotEqual("age", storage.Col("id"))))},
		{"raw SQL", storage.Command{SQL: "DELETE FROM " + users}},
	}
	for _, tt := range commands {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Execute(t.Context(), tt.command)
			assert.ErrorIs(t, err, storage.ErrUnsupportedQuery)
		})
	}

	// A command that cannot be translated never runs with a broader filter.
	assert.Equal(t, fixtureUsers, allUsers(t, store, users))
}

func TestMongoDB_BatchRunsOperationsIndependently(t *testing.T) {
	store := connectMongoDB(t)
	users := seedUsers(t, store)

	duplicate := grace
	duplicate.ID, duplicate.Email = 8, "alice@example.com"
	heidi := user{ID: 9, Name: "Heidi", Email: "heidi@example.com", Age: 23, Active: true}
	results, err := store.Batch(t.Context(), []storage.Operation{
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(users, grace))},
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(users, duplicate))},
		{Type: storage.OperationTypeCommand, Command: buildCommand(t, insertUser(users, heidi))},
	})

	// Unlike PostgreSQL, a failed operation does not undo or stop the others.
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.NoError(t, results[0].Error)
	assert.ErrorIs(t, results[1].Error, storage.ErrDuplicateKey)
	assert.NoError(t, results[2].Error)
	assert.Equal(t, append(append([]user(nil), fixtureUsers...), grace, heidi), allUsers(t, store, users))
}

func TestMongoDB_Transactions(t *testing.T) {
	store := connectMongoDB(t)
	users := seedUsers(t, store)
	ctx := t.Context()

	t.Run("savepoints are unsupported", func(t *testing.T) {
		tx, err := store.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		assert.ErrorIs(t, tx.Savepoint("sp"), storage.ErrUnsupportedOperation)
		assert.ErrorIs(t, tx.RollbackToSavepoint("sp"), storage.ErrUnsupportedOperation)
		assert.True(t, tx.IsActive())
	})

	t.Run("read-only transactions reject writes", func(t *testing.T) {
		tx, err := store.BeginTx(ctx, &storage.TxOptions{ReadOnly: true})
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		assert.Equal(t, int64(6), count(t, tx, users))
		_, err = tx.Execute(ctx, buildCommand(t, insertUser(users, grace)))
		assert.ErrorIs(t, err, storage.ErrInvalidTransaction)
		require.NoError(t, tx.Commit())
		assert.Equal(t, fixtureUsers, allUsers(t, store, users))
	})
}
