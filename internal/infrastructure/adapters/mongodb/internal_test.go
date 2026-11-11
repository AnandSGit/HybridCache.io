package mongodb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestBuildURI(t *testing.T) {
	a := &Adapter{}

	tests := []struct {
		name     string
		config   domain.Config
		expected string
	}{
		{
			name:     "DSN is used as is",
			config:   domain.Config{DSN: "mongodb://h:1/db?replicaSet=rs0", Host: "ignored"},
			expected: "mongodb://h:1/db?replicaSet=rs0",
		},
		{
			name:     "database is not put in the path",
			config:   domain.Config{Host: "localhost", Port: 27018, Database: "app"},
			expected: "mongodb://localhost:27018/",
		},
		{
			name:     "no port",
			config:   domain.Config{Host: "localhost"},
			expected: "mongodb://localhost/",
		},
		{
			name:     "credentials are percent-encoded",
			config:   domain.Config{Host: "db", Username: "us er", Password: "p@ss:w/rd %+?#"},
			expected: "mongodb://us%20er:p%40ss%3Aw%2Frd%20%25+%3F%23@db/",
		},
		{
			name:     "username only",
			config:   domain.Config{Host: "db", Username: "x509user"},
			expected: "mongodb://x509user@db/",
		},
		{
			name:     "IPv6 with port",
			config:   domain.Config{Host: "::1", Port: 27017},
			expected: "mongodb://[::1]:27017/",
		},
		{
			name:     "bracketed IPv6 without port",
			config:   domain.Config{Host: "[fe80::1]"},
			expected: "mongodb://[fe80::1]/",
		},
		{
			name: "options are sorted and escaped",
			config: domain.Config{Host: "db", Options: map[string]interface{}{
				"replicaSet": "rs0", "authSource": "admin", "appName": "my app", "maxPoolSize": 5,
			}},
			expected: "mongodb://db/?appName=my+app&authSource=admin&maxPoolSize=5&replicaSet=rs0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri := a.buildURI(tt.config)
			assert.Equal(t, tt.expected, uri)
			// Every generated URI must be accepted by the driver's parser.
			require.NoError(t, options.Client().ApplyURI(uri).Validate())
		})
	}
}

func TestBuildURI_DriverDecodesCredentials(t *testing.T) {
	uri := (&Adapter{}).buildURI(domain.Config{
		Host: "::1", Port: 27019, Database: "app", Username: "us er+1", Password: "p@ss:w/rd %+?#",
		Options: map[string]interface{}{"appName": "my app"},
	})
	opts := options.Client().ApplyURI(uri)
	require.NoError(t, opts.Validate())
	require.NotNil(t, opts.Auth)
	assert.Equal(t, "us er+1", opts.Auth.Username)
	assert.Equal(t, "p@ss:w/rd %+?#", opts.Auth.Password)
	assert.Equal(t, "admin", opts.Auth.AuthSource, "the configured database must not become the auth database")
	assert.Equal(t, []string{"[::1]:27019"}, opts.Hosts)
	require.NotNil(t, opts.AppName)
	assert.Equal(t, "my app", *opts.AppName)
}

func TestIndexModel(t *testing.T) {
	m, err := indexModel(domain.IndexDefinition{Name: "by_email", Columns: []string{"email", "created"}, Unique: true})
	require.NoError(t, err)
	assert.Equal(t, bson.D{{Key: "email", Value: 1}, {Key: "created", Value: 1}}, m.Keys)
	var o options.IndexOptions
	for _, apply := range m.Options.List() {
		require.NoError(t, apply(&o))
	}
	require.NotNil(t, o.Name)
	assert.Equal(t, "by_email", *o.Name)
	require.NotNil(t, o.Unique)
	assert.True(t, *o.Unique)

	m, err = indexModel(domain.IndexDefinition{Columns: []string{"k"}, Type: domain.IndexTypeHash})
	require.NoError(t, err)
	assert.Equal(t, bson.D{{Key: "k", Value: "hashed"}}, m.Keys)
	o = options.IndexOptions{}
	for _, apply := range m.Options.List() {
		require.NoError(t, apply(&o))
	}
	assert.Nil(t, o.Name, "without a name the server generates one")

	_, err = indexModel(domain.IndexDefinition{Name: "empty"})
	assert.ErrorIs(t, err, domain.ErrInvalidSchema)
	_, err = indexModel(domain.IndexDefinition{Columns: []string{"k"}, Type: domain.IndexTypeGIN})
	assert.ErrorIs(t, err, domain.ErrUnsupportedOperation)
	_, err = indexModel(domain.IndexDefinition{Columns: []string{"$k"}})
	assert.ErrorIs(t, err, domain.ErrInvalidQuery)
}

func TestIntegerID(t *testing.T) {
	assert.Equal(t, int64(5), integerID(int32(5)))
	assert.Equal(t, int64(1)<<40, integerID(int64(1)<<40))
	assert.Equal(t, int64(7), integerID(7))
	assert.Equal(t, int64(0), integerID(bson.NewObjectID()), "ObjectIDs have no integer form")
	assert.Equal(t, int64(0), integerID("5"))
	assert.Equal(t, int64(0), integerID(nil))
}

func TestNestedDocument(t *testing.T) {
	assert.Equal(t, bson.D{{Key: "a", Value: 1}}, nestedDocument("a", 1))
	assert.Equal(t,
		bson.D{{Key: "a", Value: bson.D{{Key: "b", Value: bson.D{{Key: "c", Value: 1}}}}}},
		nestedDocument("a.b.c", 1))
}

func TestQueryError(t *testing.T) {
	cause := errors.New("boom")
	dup := mongo.WriteException{WriteErrors: []mongo.WriteError{{Code: 11000, Message: "E11000 duplicate key error"}}}
	transient := mongo.CommandError{Code: 112, Name: "WriteConflict", Labels: []string{"TransientTransactionError"}}

	tests := []struct {
		name      string
		err       error
		sentinel  error
		retryable bool
	}{
		{"no documents", mongo.ErrNoDocuments, domain.ErrNoRows, false},
		{"duplicate key", dup, domain.ErrDuplicateKey, false},
		{"deadline", context.DeadlineExceeded, domain.ErrQueryTimeout, true},
		{"write conflict", transient, domain.ErrTransactionFailed, true},
		{"other", cause, domain.ErrQueryFailed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := queryError(tt.err, "find on \"t\" failed")
			assert.ErrorIs(t, err, tt.sentinel)
			assert.Equal(t, tt.retryable, domain.IsRetryable(err))
			if tt.sentinel != domain.ErrNoRows {
				var se *domain.StorageError
				require.ErrorAs(t, err, &se)
				assert.Equal(t, tt.err, se.Cause, "the driver error is kept as the cause")
			}
		})
	}
}

func TestTransactionError(t *testing.T) {
	err := transactionError(context.DeadlineExceeded, "commit failed")
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)

	err = transactionError(mongo.CommandError{Code: 112, Labels: []string{"TransientTransactionError"}}, "commit failed")
	assert.ErrorIs(t, err, domain.ErrTransactionFailed)
	assert.True(t, domain.IsRetryable(err))

	err = transactionError(mongo.CommandError{Code: 64, Labels: []string{"UnknownTransactionCommitResult"}}, "commit failed")
	assert.ErrorIs(t, err, domain.ErrTransactionFailed)
	assert.False(t, domain.IsRetryable(err), "the commit may have been applied")
}

func TestExecutor_WithTimeout(t *testing.T) {
	e := &executor{queryTimeout: time.Hour}

	ctx, cancel := e.withTimeout(context.Background(), 0)
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "the configured query timeout applies by default")
	assert.WithinDuration(t, time.Now().Add(time.Hour), deadline, time.Minute)
	cancel()
	assert.Error(t, ctx.Err(), "cancel releases the context")

	ctx, cancel = e.withTimeout(context.Background(), time.Minute)
	deadline, _ = ctx.Deadline()
	assert.WithinDuration(t, time.Now().Add(time.Minute), deadline, 10*time.Second, "an explicit timeout wins")
	cancel()

	ctx, cancel = (&executor{}).withTimeout(context.Background(), 0)
	_, ok = ctx.Deadline()
	assert.False(t, ok, "zero everywhere means no timeout")
	cancel()
}

// These executor paths fail or finish before reaching the server, so a nil
// database is never touched.
func TestExecutor_WithoutServer(t *testing.T) {
	e := &executor{}
	ctx := context.Background()

	res, err := e.query(ctx, domain.Query{Spec: &domain.QuerySpec{Table: "t", Fields: []string{"a"}, Limit: intPtr(0)}})
	require.NoError(t, err)
	assert.False(t, res.Next(), "LIMIT 0 returns no rows")
	cols, err := res.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, cols)

	_, err = e.queryOne(ctx, domain.Query{Spec: &domain.QuerySpec{Table: "t", Limit: intPtr(0)}})
	assert.ErrorIs(t, err, domain.ErrNoRows)

	_, err = e.query(ctx, domain.Query{SQL: "SELECT * FROM t"})
	assert.ErrorIs(t, err, domain.ErrUnsupportedQuery)
	_, err = e.queryOne(ctx, domain.Query{SQL: "SELECT * FROM t"})
	assert.ErrorIs(t, err, domain.ErrUnsupportedQuery)
	_, err = e.execute(ctx, domain.Command{SQL: "DELETE FROM t", Type: domain.CommandTypeDelete})
	assert.ErrorIs(t, err, domain.ErrUnsupportedQuery)

	results := e.batch(ctx, []domain.Operation{
		{Type: domain.OperationTypeQuery, Query: domain.Query{SQL: "SELECT 1"}},
		{Type: domain.OperationTypeQuery, Query: domain.Query{Spec: &domain.QuerySpec{Table: "t", Limit: intPtr(0)}}},
		{Type: domain.OperationTypeCommand, Command: domain.Command{SQL: "DELETE FROM t"}},
		{Type: domain.OperationType(9)},
	}, false)
	require.Len(t, results, 4)
	for i, r := range results {
		assert.Equal(t, i, r.Index)
	}
	assert.ErrorIs(t, results[0].Error, domain.ErrUnsupportedQuery)
	assert.Nil(t, results[0].Result)
	require.NoError(t, results[1].Error)
	rows, ok := results[1].Result.(domain.Result)
	require.True(t, ok, "query operations return a domain.Result, got %T", results[1].Result)
	assert.False(t, rows.Next())
	assert.ErrorIs(t, results[2].Error, domain.ErrUnsupportedQuery)
	assert.ErrorIs(t, results[3].Error, domain.ErrInvalidQuery)

	results = e.batch(ctx, []domain.Operation{{Type: domain.OperationTypeCommand}}, true)
	assert.ErrorIs(t, results[0].Error, domain.ErrInvalidTransaction, "read-only transactions reject writes")
}

func TestTransaction_WithoutServer(t *testing.T) {
	tx := &MongoDBTransaction{}

	assert.ErrorIs(t, tx.Savepoint("sp"), domain.ErrUnsupportedOperation)
	assert.ErrorIs(t, tx.RollbackToSavepoint("sp"), domain.ErrUnsupportedOperation)

	// An ended transaction rejects everything.
	assert.False(t, tx.IsActive())
	_, err := tx.Query(context.Background(), domain.Query{})
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
	_, err = tx.QueryOne(context.Background(), domain.Query{})
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
	_, err = tx.Execute(context.Background(), domain.Command{})
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
	_, err = tx.Batch(context.Background(), nil)
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
	assert.ErrorIs(t, tx.Commit(), domain.ErrTransactionClosed)
	assert.ErrorIs(t, tx.Rollback(), domain.ErrTransactionClosed)

	// So does an active one whose BeginTx context is done.
	expired, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	tx = &MongoDBTransaction{active: true, ctx: expired}
	_, err = tx.Query(context.Background(), domain.Query{})
	assert.ErrorIs(t, err, domain.ErrTransactionTimeout)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tx = &MongoDBTransaction{active: true, ctx: canceled}
	_, err = tx.Execute(context.Background(), domain.Command{})
	assert.ErrorIs(t, err, domain.ErrTransactionClosed)
}
