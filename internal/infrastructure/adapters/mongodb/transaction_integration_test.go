//go:build integration

package mongodb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every way a transaction can end must end its session, including failed commits.
func TestIntegration_TransactionsEndTheirSessions(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI is not set")
	}
	ctx := context.Background()
	a := NewAdapter()
	cfg, err := a.ParseDSN(uri)
	require.NoError(t, err)
	cfg.Database = "hc_go_mongo"
	st, err := a.Connect(ctx, cfg)
	require.NoError(t, err)
	defer st.Close()
	s, ok := st.(*MongoDBStorage)
	require.True(t, ok)

	const coll = "TestIntegration_TransactionsEndTheirSessions"
	require.NoError(t, s.DropTable(ctx, coll))
	defer s.DropTable(ctx, coll)
	insert := domain.Command{Type: domain.CommandTypeInsert, Spec: &domain.CommandSpec{
		Table: coll, Values: []domain.FieldValue{{Field: "_id", Value: 1}, {Field: "n", Value: 0}},
	}}
	update := func(n int) domain.Command {
		return domain.Command{Type: domain.CommandTypeUpdate, Spec: &domain.CommandSpec{
			Table:  coll,
			Values: []domain.FieldValue{{Field: "n", Value: n}},
			Where:  []domain.Condition{{Field: "_id", Operator: domain.OperatorEqual, Value: 1}},
		}}
	}
	_, err = s.Execute(ctx, insert)
	require.NoError(t, err)
	baseline := s.client.NumberSessionsInProgress()

	committed, err := s.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = committed.Execute(ctx, update(1))
	require.NoError(t, err)

	conflicted, err := s.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = conflicted.Execute(ctx, update(2))
	require.Error(t, err, "write conflict")
	require.Error(t, conflicted.Commit(), "the server aborted the transaction")
	require.NoError(t, committed.Commit())

	rolledBack, err := s.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = rolledBack.Execute(ctx, update(3))
	require.NoError(t, err)
	require.NoError(t, rolledBack.Rollback())

	timedOut, err := s.BeginTx(ctx, &domain.TxOptions{Timeout: 10 * time.Millisecond})
	require.NoError(t, err)
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, timedOut.Rollback())

	for _, tx := range []domain.Transaction{committed, conflicted, rolledBack, timedOut} {
		assert.False(t, tx.IsActive())
		mtx, ok := tx.(*MongoDBTransaction)
		require.True(t, ok)
		assert.Error(t, mtx.ctx.Err(), "the transaction context is released")
	}
	assert.Equal(t, baseline, s.client.NumberSessionsInProgress(), "no session is leaked")

	row, err := s.QueryOne(ctx, domain.Query{Spec: &domain.QuerySpec{Table: coll, Fields: []string{"n"}}})
	require.NoError(t, err)
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n, "only the committed update is visible")
}
