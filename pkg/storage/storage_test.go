package storage_test

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// These tests use only the public API, as an application importing
// pkg/storage would. None of them needs a database.

func TestAdapterFactories(t *testing.T) {
	tests := []struct {
		name          string
		adapter       storage.Adapter
		databaseType  storage.DatabaseType
		supportsJoins bool
	}{
		{"postgresql", storage.NewPostgreSQLAdapter(), storage.DatabaseTypePostgreSQL, true},
		{"mongodb", storage.NewMongoDBAdapter(), storage.DatabaseTypeMongoDB, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.name, tt.adapter.Name())
			assert.Equal(t, tt.databaseType, tt.adapter.DatabaseType())
			assert.Equal(t, tt.name, tt.adapter.DatabaseType().String())
			assert.NotEmpty(t, tt.adapter.Version())
			assert.True(t, tt.adapter.SupportsTransactions())
			assert.True(t, tt.adapter.SupportsBatch())
			assert.True(t, tt.adapter.SupportsSchema())
			assert.Equal(t, tt.supportsJoins, tt.adapter.SupportsJoins())
		})
	}
}

func TestAdapterConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"postgresql config without host", storage.NewPostgreSQLAdapter().ValidateConfig(storage.Config{}), storage.ErrInvalidConfig},
		{"postgresql negative pool size", storage.NewPostgreSQLAdapter().ValidateConfig(storage.Config{DSN: "host=localhost", MaxOpenConns: -1}), storage.ErrInvalidConfig},
		{"mongodb config without database", storage.NewMongoDBAdapter().ValidateConfig(storage.Config{Host: "localhost"}), storage.ErrInvalidConfig},
		{"mongodb DSN with another scheme", parseDSNError(storage.NewMongoDBAdapter(), "postgres://localhost/app"), storage.ErrInvalidDSN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			assert.ErrorIs(t, tt.err, tt.want)

			var se *storage.StorageError
			require.ErrorAs(t, tt.err, &se)
			assert.Equal(t, storage.ErrorTypeValidation, se.Type)
		})
	}
}

func parseDSNError(adapter storage.Adapter, dsn string) error {
	_, err := adapter.ParseDSN(dsn)
	return err
}

func TestAdapterParseDSN(t *testing.T) {
	pg, err := storage.NewPostgreSQLAdapter().ParseDSN("postgres://app:secret@db.example.com:6543/shop?sslmode=require")
	require.NoError(t, err)
	assert.Equal(t, "db.example.com", pg.Host)
	assert.Equal(t, 6543, pg.Port)
	assert.Equal(t, "shop", pg.Database)
	assert.Equal(t, "app", pg.Username)
	assert.Equal(t, "secret", pg.Password)
	assert.Equal(t, "require", pg.SSLMode)
	assert.NoError(t, storage.NewPostgreSQLAdapter().ValidateConfig(pg))

	mongo, err := storage.NewMongoDBAdapter().ParseDSN("mongodb://db.example.com:27018/shop?replicaSet=rs0")
	require.NoError(t, err)
	assert.Equal(t, "db.example.com", mongo.Host)
	assert.Equal(t, 27018, mongo.Port)
	assert.Equal(t, "shop", mongo.Database)
	assert.NoError(t, storage.NewMongoDBAdapter().ValidateConfig(mongo))
}

// TestSentinelErrors checks that every re-exported sentinel is the one a
// StorageError with the matching code is linked to, and no other.
func TestSentinelErrors(t *testing.T) {
	sentinels := []struct {
		code string
		err  error
	}{
		{"CONNECTION_FAILED", storage.ErrConnectionFailed},
		{"CONNECTION_TIMEOUT", storage.ErrConnectionTimeout},
		{"CONNECTION_CLOSED", storage.ErrConnectionClosed},
		{"CONNECTION_POOL_FULL", storage.ErrConnectionPoolFull},
		{"INVALID_DSN", storage.ErrInvalidDSN},
		{"INVALID_CONFIG", storage.ErrInvalidConfig},
		{"INVALID_QUERY", storage.ErrInvalidQuery},
		{"QUERY_TIMEOUT", storage.ErrQueryTimeout},
		{"QUERY_FAILED", storage.ErrQueryFailed},
		{"INVALID_PARAMETERS", storage.ErrInvalidParameters},
		{"UNSUPPORTED_QUERY", storage.ErrUnsupportedQuery},
		{"TRANSACTION_FAILED", storage.ErrTransactionFailed},
		{"TRANSACTION_TIMEOUT", storage.ErrTransactionTimeout},
		{"TRANSACTION_CLOSED", storage.ErrTransactionClosed},
		{"INVALID_TRANSACTION", storage.ErrInvalidTransaction},
		{"DEADLOCK", storage.ErrDeadlock},
		{"NO_ROWS", storage.ErrNoRows},
		{"TOO_MANY_ROWS", storage.ErrTooManyRows},
		{"INVALID_DATA", storage.ErrInvalidData},
		{"DATA_TRUNCATED", storage.ErrDataTruncated},
		{"CONSTRAINT_VIOLATION", storage.ErrConstraintViolation},
		{"DUPLICATE_KEY", storage.ErrDuplicateKey},
		{"TABLE_NOT_FOUND", storage.ErrTableNotFound},
		{"COLUMN_NOT_FOUND", storage.ErrColumnNotFound},
		{"INDEX_NOT_FOUND", storage.ErrIndexNotFound},
		{"INVALID_SCHEMA", storage.ErrInvalidSchema},
		{"SCHEMA_EXISTS", storage.ErrSchemaExists},
		{"ADAPTER_NOT_FOUND", storage.ErrAdapterNotFound},
		{"UNSUPPORTED_ADAPTER", storage.ErrUnsupportedAdapter},
		{"ADAPTER_FAILED", storage.ErrAdapterFailed},
		{"NOT_IMPLEMENTED", storage.ErrNotImplemented},
		{"UNSUPPORTED_OPERATION", storage.ErrUnsupportedOperation},
		{"INTERNAL_ERROR", storage.ErrInternalError},
	}

	for _, s := range sentinels {
		t.Run(s.code, func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", storage.NewStorageError(storage.ErrorTypeUnknown, s.code, "test"))
			assert.ErrorIs(t, err, s.err)
			for _, other := range sentinels {
				if other.code != s.code {
					assert.NotErrorIs(t, err, other.err, "matches the sentinel for %s", other.code)
				}
			}
		})
	}
}

func TestErrorTypeHelpers(t *testing.T) {
	helpers := map[string]func(error) bool{
		"IsConnectionError":  storage.IsConnectionError,
		"IsQueryError":       storage.IsQueryError,
		"IsTransactionError": storage.IsTransactionError,
		"IsDataError":        storage.IsDataError,
		"IsTimeoutError":     storage.IsTimeoutError,
	}
	tests := []struct {
		errorType storage.ErrorType
		helper    string // the only helper that reports true
	}{
		{storage.ErrorTypeConnection, "IsConnectionError"},
		{storage.ErrorTypeQuery, "IsQueryError"},
		{storage.ErrorTypeTransaction, "IsTransactionError"},
		{storage.ErrorTypeData, "IsDataError"},
		{storage.ErrorTypeTimeout, "IsTimeoutError"},
		{storage.ErrorTypeValidation, ""},
		{storage.ErrorTypeSchema, ""},
		{storage.ErrorTypeAdapter, ""},
		{storage.ErrorTypePermission, ""},
		{storage.ErrorTypeResource, ""},
		{storage.ErrorTypeUnknown, ""},
	}

	for _, tt := range tests {
		t.Run(tt.errorType.String(), func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", storage.NewStorageError(tt.errorType, "CODE", "test"))
			for name, helper := range helpers {
				assert.Equal(t, name == tt.helper, helper(err), name)
			}
		})
	}

	t.Run("non-storage errors", func(t *testing.T) {
		for name, helper := range helpers {
			assert.False(t, helper(io.EOF), name)
			assert.False(t, helper(nil), name)
		}
	})
}

func TestRetryableAndTemporary(t *testing.T) {
	retryable := fmt.Errorf("wrapped: %w", storage.NewStorageError(storage.ErrorTypeTransaction, "DEADLOCK", "deadlock").WithRetryable(true))
	temporary := storage.NewStorageError(storage.ErrorTypeTimeout, "QUERY_TIMEOUT", "timeout").WithTemporary(true)
	plain := storage.NewStorageError(storage.ErrorTypeData, "NO_ROWS", "no rows")

	assert.True(t, storage.IsRetryable(retryable))
	assert.False(t, storage.IsTemporary(retryable))
	assert.True(t, storage.IsTemporary(temporary))
	assert.False(t, storage.IsRetryable(temporary))
	assert.False(t, storage.IsRetryable(plain))
	assert.False(t, storage.IsTemporary(plain))
	assert.False(t, storage.IsRetryable(io.EOF))
	assert.False(t, storage.IsRetryable(nil))
}

func TestWrapError(t *testing.T) {
	err := storage.WrapError(io.ErrUnexpectedEOF, storage.ErrorTypeData, "INVALID_DATA", "truncated row")

	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.ErrorIs(t, err, storage.ErrInvalidData)
	assert.True(t, storage.IsDataError(err))
	assert.Equal(t, "INVALID_DATA: truncated row (caused by: unexpected EOF)", err.Error())

	var se *storage.StorageError
	require.ErrorAs(t, fmt.Errorf("wrapped: %w", err), &se)
	assert.Equal(t, "INVALID_DATA", se.Code)
	assert.Equal(t, storage.ErrorTypeData, se.Type)
	assert.Equal(t, "truncated row", se.Message)
}

func TestNewBuilder_SQLAndSpec(t *testing.T) {
	q, err := storage.NewBuilder().
		Select("id", "name").
		From("users").
		Where(storage.Equal("active", true)).
		WhereOr(storage.LessThan("age", 30), storage.Like("name", "A%")).
		OrderByDesc("age").
		Page(3, 10).
		Build()
	require.NoError(t, err)

	assert.Equal(t, "SELECT id, name FROM users WHERE active = ? AND (age < ? OR name LIKE ?) ORDER BY age DESC LIMIT 10 OFFSET 20", q.SQL)
	assert.Equal(t, []interface{}{true, 30, "A%"}, q.Parameters)
	assert.Equal(t, storage.QueryTypeSelect, q.Type)

	limit, offset := 10, 20
	assert.Equal(t, &storage.QuerySpec{
		Table:  "users",
		Fields: []string{"id", "name"},
		Where: []storage.Condition{
			{Field: "active", Operator: storage.OperatorEqual, Value: true},
			{Operator: storage.OperatorOr, Conditions: []storage.Condition{
				{Field: "age", Operator: storage.OperatorLessThan, Value: 30},
				{Field: "name", Operator: storage.OperatorLike, Value: "A%"},
			}},
		},
		OrderBy: []storage.OrderBy{{Field: "age", Direction: storage.SortDirectionDesc}},
		Limit:   &limit,
		Offset:  &offset,
	}, q.Spec)
}

func TestNewBuilder_JoinsAndSubqueries(t *testing.T) {
	paid := storage.NewBuilder().
		From("payments p").
		Where(storage.Equal("p.order_id", storage.Col("o.id"))).
		Where(storage.GreaterThan("p.amount", 0))

	q, err := storage.NewBuilder().
		Select("u.name", "o.total").
		From("users u").
		InnerJoin("orders o", storage.Equal("o.user_id", storage.Col("u.id"))).
		Where(storage.Exists(paid)).
		Where(storage.Equal("u.active", true)).
		Build()
	require.NoError(t, err)

	assert.Equal(t, "SELECT u.name, o.total FROM users u INNER JOIN orders o ON o.user_id = u.id "+
		"WHERE EXISTS (SELECT * FROM payments p WHERE p.order_id = o.id AND p.amount > ?) AND u.active = ?", q.SQL)
	assert.Equal(t, []interface{}{0, true}, q.Parameters)

	require.NotNil(t, q.Spec)
	assert.Equal(t, "users", q.Spec.Table)
	assert.Equal(t, []storage.Join{{
		Type:  storage.JoinTypeInner,
		Table: "orders o",
		On:    &storage.Condition{Field: "o.user_id", Operator: storage.OperatorEqual, Value: storage.ColumnRef("u.id")},
	}}, q.Spec.Joins)
	require.Len(t, q.Spec.Where, 2)
	assert.Equal(t, storage.OperatorExists, q.Spec.Where[0].Operator)
}

func TestNewBuilder_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		builder storage.QueryBuilder
	}{
		{"injected table", storage.NewBuilder().From("users; DROP TABLE users")},
		{"injected ORDER BY", storage.NewBuilder().From("users").OrderByAsc("name; DROP TABLE users")},
		{"injected condition field", storage.NewBuilder().From("users").Where(storage.Equal("id = 1 OR 1=1 --", 1))},
		{"missing table", storage.NewBuilder().Select("id")},
		{"page zero", storage.NewBuilder().From("users").Page(0, 10)},
		{"empty OR group", storage.NewBuilder().From("users").Where(storage.Or())},
		{"BETWEEN with one value", storage.NewBuilder().From("users").Where(storage.Condition{Field: "age", Operator: storage.OperatorBetween, Values: []interface{}{1}})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.builder.Build()
			assert.ErrorIs(t, err, storage.ErrInvalidQuery)
		})
	}
}

func TestConditionHelpers(t *testing.T) {
	sub := storage.NewBuilder().From("orders")
	tests := []struct {
		name string
		got  storage.Condition
		want storage.Condition
	}{
		{"Equal", storage.Equal("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorEqual, Value: 1}},
		{"NotEqual", storage.NotEqual("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorNotEqual, Value: 1}},
		{"GreaterThan", storage.GreaterThan("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorGreaterThan, Value: 1}},
		{"GreaterThanOrEqual", storage.GreaterThanOrEqual("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorGreaterThanOrEqual, Value: 1}},
		{"LessThan", storage.LessThan("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorLessThan, Value: 1}},
		{"LessThanOrEqual", storage.LessThanOrEqual("a", 1), storage.Condition{Field: "a", Operator: storage.OperatorLessThanOrEqual, Value: 1}},
		{"Like", storage.Like("a", "x%"), storage.Condition{Field: "a", Operator: storage.OperatorLike, Value: "x%"}},
		{"NotLike", storage.NotLike("a", "x%"), storage.Condition{Field: "a", Operator: storage.OperatorNotLike, Value: "x%"}},
		{"In", storage.In("a", 1, 2), storage.Condition{Field: "a", Operator: storage.OperatorIn, Values: []interface{}{1, 2}}},
		{"NotIn", storage.NotIn("a", 1, 2), storage.Condition{Field: "a", Operator: storage.OperatorNotIn, Values: []interface{}{1, 2}}},
		{"Between", storage.Between("a", 1, 9), storage.Condition{Field: "a", Operator: storage.OperatorBetween, Values: []interface{}{1, 9}}},
		{"NotBetween", storage.NotBetween("a", 1, 9), storage.Condition{Field: "a", Operator: storage.OperatorNotBetween, Values: []interface{}{1, 9}}},
		{"IsNull", storage.IsNull("a"), storage.Condition{Field: "a", Operator: storage.OperatorIsNull}},
		{"IsNotNull", storage.IsNotNull("a"), storage.Condition{Field: "a", Operator: storage.OperatorIsNotNull}},
		{"Exists", storage.Exists(sub), storage.Condition{Operator: storage.OperatorExists, Value: sub}},
		{"NotExists", storage.NotExists(sub), storage.Condition{Operator: storage.OperatorNotExists, Value: sub}},
		{
			"And", storage.And(storage.Equal("a", 1), storage.IsNull("b")),
			storage.Condition{Operator: storage.OperatorAnd, Conditions: []storage.Condition{
				{Field: "a", Operator: storage.OperatorEqual, Value: 1},
				{Field: "b", Operator: storage.OperatorIsNull},
			}},
		},
		{
			"Or", storage.Or(storage.Equal("a", 1), storage.IsNull("b")),
			storage.Condition{Operator: storage.OperatorOr, Conditions: []storage.Condition{
				{Field: "a", Operator: storage.OperatorEqual, Value: 1},
				{Field: "b", Operator: storage.OperatorIsNull},
			}},
		},
		{"Col", storage.Equal("a", storage.Col("t.b")), storage.Condition{Field: "a", Operator: storage.OperatorEqual, Value: storage.ColumnRef("t.b")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestCommandBuilders(t *testing.T) {
	tests := []struct {
		name    string
		builder *storage.CommandBuilder
		sql     string
		params  []interface{}
		cmdType storage.CommandType
		spec    *storage.CommandSpec
	}{
		{
			name:    "insert",
			builder: storage.Insert("users").Set("name", "Ann").Set("age", 30),
			sql:     "INSERT INTO users (name, age) VALUES (?, ?)",
			params:  []interface{}{"Ann", 30},
			cmdType: storage.CommandTypeInsert,
			spec: &storage.CommandSpec{Table: "users", Values: []storage.FieldValue{
				{Field: "name", Value: "Ann"},
				{Field: "age", Value: 30},
			}},
		},
		{
			name:    "update",
			builder: storage.Update("users").Set("age", 31).Where(storage.Equal("id", 7)),
			sql:     "UPDATE users SET age = ? WHERE id = ?",
			params:  []interface{}{31, 7},
			cmdType: storage.CommandTypeUpdate,
			spec: &storage.CommandSpec{
				Table:  "users",
				Values: []storage.FieldValue{{Field: "age", Value: 31}},
				Where:  []storage.Condition{{Field: "id", Operator: storage.OperatorEqual, Value: 7}},
			},
		},
		{
			name:    "delete",
			builder: storage.Delete("users").Where(storage.In("id", 1, 2)),
			sql:     "DELETE FROM users WHERE id IN (?, ?)",
			params:  []interface{}{1, 2},
			cmdType: storage.CommandTypeDelete,
			spec: &storage.CommandSpec{
				Table: "users",
				Where: []storage.Condition{{Field: "id", Operator: storage.OperatorIn, Values: []interface{}{1, 2}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := tt.builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.sql, cmd.SQL)
			assert.Equal(t, tt.params, cmd.Parameters)
			assert.Equal(t, tt.cmdType, cmd.Type)
			assert.Equal(t, tt.spec, cmd.Spec)
		})
	}

	t.Run("invalid column", func(t *testing.T) {
		_, err := storage.Insert("users").Set("name) VALUES ('x'); --", "y").Build()
		assert.ErrorIs(t, err, storage.ErrInvalidQuery)
	})
}

// TestTranslateBuiltQuery checks that a built query translates on both adapters
// without a server: '?' placeholders become $n for PostgreSQL, and the Spec
// becomes a find for MongoDB.
func TestTranslateBuiltQuery(t *testing.T) {
	q, err := storage.NewBuilder().
		Select("name").
		From("users").
		Where(storage.Equal("active", true)).
		Where(storage.GreaterThanOrEqual("age", 21)).
		OrderByAsc("name").
		Limit(5).
		Build()
	require.NoError(t, err)

	sql, params, err := storage.NewPostgreSQLAdapter().TranslateQuery(q)
	require.NoError(t, err)
	assert.Equal(t, "SELECT name FROM users WHERE active = $1 AND age >= $2 ORDER BY name ASC LIMIT 5", sql)
	assert.Equal(t, []interface{}{true, 21}, params)

	find, params, err := storage.NewMongoDBAdapter().TranslateQuery(q)
	require.NoError(t, err)
	assert.Equal(t, `db.getCollection("users").find({"$and":[{"active":{"$eq":true}},{"age":{"$gte":21}}]}, {"name":1,"_id":0}).sort({"name":1}).limit(5)`, find)
	assert.Nil(t, params)
}

func TestMongoDBRejectsSQLOnlyQueries(t *testing.T) {
	join, err := storage.NewBuilder().
		From("users u").
		InnerJoin("orders o", storage.Equal("o.user_id", storage.Col("u.id"))).
		Build()
	require.NoError(t, err)

	adapter := storage.NewMongoDBAdapter()
	_, _, err = adapter.TranslateQuery(join)
	assert.ErrorIs(t, err, storage.ErrUnsupportedQuery)

	_, _, err = adapter.TranslateQuery(storage.Query{SQL: "SELECT * FROM users"})
	assert.ErrorIs(t, err, storage.ErrUnsupportedQuery, "raw SQL has no Spec")

	_, _, err = adapter.TranslateCommand(storage.Command{SQL: "DELETE FROM users"})
	assert.ErrorIs(t, err, storage.ErrUnsupportedQuery, "raw SQL has no Spec")
}

func ExampleNewBuilder() {
	query, err := storage.NewBuilder().
		Select("id", "name").
		From("users").
		Where(storage.Equal("active", true)).
		WhereOr(storage.LessThan("age", 30), storage.Like("name", "A%")).
		OrderByAsc("name").
		Page(2, 20).
		Build()
	if err != nil {
		panic(err)
	}
	fmt.Println(query.SQL)
	fmt.Println(query.Parameters)
	// Output:
	// SELECT id, name FROM users WHERE active = ? AND (age < ? OR name LIKE ?) ORDER BY name ASC LIMIT 20 OFFSET 20
	// [true 30 A%]
}

func ExampleCol() {
	query, err := storage.NewBuilder().
		Select("u.name", "o.total").
		From("users u").
		LeftJoin("orders o", storage.Equal("o.user_id", storage.Col("u.id"))).
		Build()
	if err != nil {
		panic(err)
	}
	fmt.Println(query.SQL)
	fmt.Println(len(query.Parameters))
	// Output:
	// SELECT u.name, o.total FROM users u LEFT JOIN orders o ON o.user_id = u.id
	// 0
}

func ExampleInsert() {
	command, err := storage.Insert("users").
		Set("name", "Ann").
		Set("email", "ann@example.com").
		Build()
	if err != nil {
		panic(err)
	}
	fmt.Println(command.SQL)
	fmt.Println(command.Parameters)
	// Output:
	// INSERT INTO users (name, email) VALUES (?, ?)
	// [Ann ann@example.com]
}
