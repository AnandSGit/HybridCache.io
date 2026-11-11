package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// fakeRows is an in-memory pgx.Rows.
type fakeRows struct {
	fields []pgconn.FieldDescription
	rows   [][][]byte
	pos    int
	err    error
	closed bool
	tag    pgconn.CommandTag
}

func (r *fakeRows) Close()                                       { r.closed = true }
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return r.tag }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return r.fields }
func (r *fakeRows) Values() ([]any, error)                       { return nil, errors.New("not implemented") }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }

func (r *fakeRows) Next() bool {
	if r.closed || r.err != nil || r.pos >= len(r.rows) {
		r.Close()
		return false
	}
	r.pos++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	return pgx.ScanRow(pgtype.NewMap(), r.fields, r.RawValues(), dest...)
}

func (r *fakeRows) RawValues() [][]byte {
	if r.closed || r.pos == 0 {
		return nil
	}
	return r.rows[r.pos-1]
}

func encode(t *testing.T, oid uint32, value any) []byte {
	t.Helper()
	buf, err := pgtype.NewMap().Encode(oid, pgtype.BinaryFormatCode, value, nil)
	require.NoError(t, err)
	return buf
}

func binaryField(name string, oid uint32) pgconn.FieldDescription {
	return pgconn.FieldDescription{Name: name, DataTypeOID: oid, Format: pgtype.BinaryFormatCode}
}

func textField(name string, oid uint32) pgconn.FieldDescription {
	return pgconn.FieldDescription{Name: name, DataTypeOID: oid, Format: pgtype.TextFormatCode}
}

type userRow struct {
	ID        int64
	UserName  string
	Email     *string
	Score     float64
	Active    bool
	CreatedAt time.Time
	Ignored   string `db:"-"`
	Nick      string `db:"nickname,omitempty"`
	Tags      map[string]any
}

func userFields() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{
		binaryField("id", pgtype.Int8OID),
		textField("user_name", pgtype.TextOID),
		textField("email", pgtype.TextOID),
		textField("score", pgtype.NumericOID),
		binaryField("active", pgtype.BoolOID),
		binaryField("created_at", pgtype.TimestamptzOID),
		textField("ignored", pgtype.TextOID),
		textField("nickname", pgtype.TextOID),
		textField("tags", pgtype.JSONBOID),
		textField("unmapped", pgtype.TextOID),
	}
}

func TestStructScanner_MapsAllColumns(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	values := [][]byte{
		encode(t, pgtype.Int8OID, int64(42)),
		[]byte("ada"),
		[]byte("ada@example.com"),
		[]byte("12.5"),
		encode(t, pgtype.BoolOID, true),
		encode(t, pgtype.TimestamptzOID, created),
		[]byte("must not be set"),
		[]byte("countess"),
		[]byte(`{"a": 1}`),
		[]byte("no field"),
	}

	var s structScanner
	var u userRow
	require.NoError(t, s.scan(pgtype.NewMap(), userFields(), values, &u))

	require.NotNil(t, u.Email)
	assert.Equal(t, userRow{
		ID:        42,
		UserName:  "ada",
		Email:     u.Email,
		Score:     12.5,
		Active:    true,
		CreatedAt: created,
		Nick:      "countess",
		Tags:      map[string]any{"a": float64(1)},
	}, normalizeTime(u))
	assert.Equal(t, "ada@example.com", *u.Email)
}

func normalizeTime(u userRow) userRow {
	u.CreatedAt = u.CreatedAt.UTC()
	return u
}

func TestStructScanner_NullResetsFields(t *testing.T) {
	email := "old@example.com"
	u := userRow{
		ID: 1, UserName: "stale", Email: &email, Score: 9, Active: true,
		CreatedAt: time.Now(), Nick: "stale", Tags: map[string]any{"stale": true},
	}
	values := make([][]byte, len(userFields())) // every column NULL

	var s structScanner
	require.NoError(t, s.scan(pgtype.NewMap(), userFields(), values, &u))
	assert.Equal(t, userRow{}, u, "NULL must set the zero value (nil for pointers)")

	// A JSON object replaces, rather than merges into, a previous row's map.
	u.Tags = map[string]any{"stale": true}
	values[8] = []byte(`{"fresh": 1}`)
	require.NoError(t, s.scan(pgtype.NewMap(), userFields(), values, &u))
	assert.Equal(t, map[string]any{"fresh": float64(1)}, u.Tags)
}

func TestStructScanner_Conversions(t *testing.T) {
	type target struct {
		IntAsString  string
		Numeric      float64
		WholeNumeric int64
		Small        int16
		Name         *string
		Unsigned     uint8
	}
	fields := []pgconn.FieldDescription{
		binaryField("int_as_string", pgtype.Int4OID),
		textField("numeric", pgtype.NumericOID),
		textField("whole_numeric", pgtype.NumericOID),
		binaryField("small", pgtype.Int8OID),
		textField("name", pgtype.VarcharOID),
		binaryField("unsigned", pgtype.Int2OID),
	}
	values := [][]byte{
		encode(t, pgtype.Int4OID, int32(25)),
		[]byte("1234.5678"),
		[]byte("77"),
		encode(t, pgtype.Int8OID, int64(300)),
		[]byte("bob"),
		encode(t, pgtype.Int2OID, int16(7)),
	}

	var s structScanner
	var got target
	require.NoError(t, s.scan(pgtype.NewMap(), fields, values, &got))
	assert.Equal(t, "25", got.IntAsString, "integers are formatted, never converted to a rune")
	assert.Equal(t, 1234.5678, got.Numeric)
	assert.Equal(t, int64(77), got.WholeNumeric)
	assert.Equal(t, int16(300), got.Small)
	require.NotNil(t, got.Name)
	assert.Equal(t, "bob", *got.Name)
	assert.Equal(t, uint8(7), got.Unsigned)

	failures := []struct {
		name   string
		value  []byte
		column int
	}{
		{"fractional numeric into int64", []byte("1.5"), 2},
		{"int8 overflowing int16", encode(t, pgtype.Int8OID, int64(70000)), 3},
		{"negative into uint8", encode(t, pgtype.Int2OID, int16(-1)), 5},
	}
	for _, f := range failures {
		t.Run(f.name, func(t *testing.T) {
			bad := append([][]byte{}, values...)
			bad[f.column] = f.value
			err := (&structScanner{}).scan(pgtype.NewMap(), fields, bad, &target{})
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrInvalidData)
			assert.Contains(t, err.Error(), fields[f.column].Name)
		})
	}

	type textIntoInt struct{ Name int }
	err := (&structScanner{}).scan(pgtype.NewMap(), []pgconn.FieldDescription{textField("name", pgtype.TextOID)},
		[][]byte{[]byte("abc")}, &textIntoInt{})
	require.ErrorIs(t, err, domain.ErrInvalidData)
	assert.Contains(t, err.Error(), `cannot scan column "name" into field Name`)
}

type base struct {
	ID int64
}

type Audit struct {
	CreatedBy string
}

type audit struct {
	CreatedBy string
}

type withEmbedded struct {
	base          // unexported, by value: promoted fields are settable
	*Audit        // exported pointer: allocated when a column maps into it
	Title  string `db:"headline"`
}

type withUnexportedPointer struct {
	*audit
}

func TestStructScanner_EmbeddedStructs(t *testing.T) {
	fields := []pgconn.FieldDescription{
		binaryField("id", pgtype.Int8OID),
		textField("created_by", pgtype.TextOID),
		textField("headline", pgtype.TextOID),
	}
	values := [][]byte{encode(t, pgtype.Int8OID, int64(5)), []byte("admin"), []byte("hello")}

	var got withEmbedded
	require.NoError(t, (&structScanner{}).scan(pgtype.NewMap(), fields, values, &got))
	assert.Equal(t, int64(5), got.ID)
	require.NotNil(t, got.Audit)
	assert.Equal(t, "admin", got.CreatedBy)
	assert.Equal(t, "hello", got.Title)

	// As with encoding/json, a nil embedded pointer to an unexported type
	// cannot be allocated through reflection.
	err := (&structScanner{}).scan(pgtype.NewMap(), fields, values, &withUnexportedPointer{})
	assert.ErrorIs(t, err, domain.ErrInvalidData)
	assert.Contains(t, err.Error(), "unexported")
	assert.NoError(t, (&structScanner{}).scan(pgtype.NewMap(), fields, values,
		&withUnexportedPointer{audit: &audit{}}), "an allocated pointer is fine")
}

func TestStructScanner_InvalidDestination(t *testing.T) {
	fields := []pgconn.FieldDescription{textField("a", pgtype.TextOID)}
	values := [][]byte{[]byte("x")}
	var notStruct int
	var nilPtr *userRow
	for _, dest := range []any{userRow{}, &notStruct, nilPtr, nil} {
		err := (&structScanner{}).scan(pgtype.NewMap(), fields, values, dest)
		assert.ErrorIs(t, err, domain.ErrInvalidParameters, "%T", dest)
	}

	err := (&structScanner{}).scan(pgtype.NewMap(), fields, nil, &userRow{})
	assert.ErrorIs(t, err, domain.ErrNoRows, "no current row")
}

func TestStructScanner_ReusesMappingPerType(t *testing.T) {
	type a struct{ X string }
	type b struct {
		Y string `db:"x"`
	}
	fields := []pgconn.FieldDescription{textField("x", pgtype.TextOID)}
	var s structScanner

	var first a
	require.NoError(t, s.scan(pgtype.NewMap(), fields, [][]byte{[]byte("1")}, &first))
	var second b
	require.NoError(t, s.scan(pgtype.NewMap(), fields, [][]byte{[]byte("2")}, &second))
	var third a
	require.NoError(t, s.scan(pgtype.NewMap(), fields, [][]byte{[]byte("3")}, &third))
	assert.Equal(t, "1", first.X)
	assert.Equal(t, "2", second.Y)
	assert.Equal(t, "3", third.X)
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"ID":         "id",
		"UserID":     "user_id",
		"UserName":   "user_name",
		"HTTPServer": "http_server",
		"APIKey":     "api_key",
		"Address2":   "address2",
		"Field1Name": "field1_name",
		"name":       "name",
	} {
		assert.Equal(t, want, snakeCase(in), in)
	}
}

func TestCopyValues(t *testing.T) {
	src := [][]byte{[]byte("abc"), nil, {}}
	got := copyValues(src)
	src[0][0] = 'X'
	assert.Equal(t, []byte("abc"), got[0], "copies are independent of the driver's buffer")
	assert.Nil(t, got[1], "NULL stays nil")
	assert.NotNil(t, got[2], "empty stays non-NULL")
	assert.Len(t, got[2], 0)
}

func TestBufferedResult(t *testing.T) {
	rows := &fakeRows{
		fields: []pgconn.FieldDescription{binaryField("id", pgtype.Int4OID), textField("name", pgtype.TextOID)},
		rows: [][][]byte{
			{encode(t, pgtype.Int4OID, int32(1)), []byte("one")},
			{encode(t, pgtype.Int4OID, int32(2)), nil},
		},
		tag: pgconn.NewCommandTag("SELECT 2"),
	}
	res, err := bufferRows(rows)
	require.NoError(t, err)
	assert.True(t, rows.closed, "the driver rows are closed")

	var id int
	var name *string
	assert.ErrorIs(t, res.Scan(&id, &name), domain.ErrNoRows, "Scan before Next")

	require.True(t, res.Next())
	require.NoError(t, res.Scan(&id, &name))
	assert.Equal(t, 1, id)
	require.NotNil(t, name)
	assert.Equal(t, "one", *name)

	require.True(t, res.Next())
	var row struct {
		ID   int
		Name string
	}
	require.NoError(t, res.ScanRow(&row))
	assert.Equal(t, 2, row.ID)
	assert.Equal(t, "", row.Name)

	assert.False(t, res.Next())
	assert.ErrorIs(t, res.ScanRow(&row), domain.ErrNoRows, "ScanRow after the last row")
	assert.NoError(t, res.Err())

	cols, err := res.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, cols)
	types, err := res.ColumnTypes()
	require.NoError(t, err)
	assert.Equal(t, domain.DataTypeInteger, types[0].DataType)
	assert.Equal(t, "int4", types[0].DatabaseType)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected)
	_, err = res.LastInsertID()
	assert.ErrorIs(t, err, domain.ErrUnsupportedOperation)

	require.NoError(t, res.Close())
	assert.False(t, res.Next())

	failing := &fakeRows{err: errors.New("boom")}
	_, err = bufferRows(failing)
	assert.EqualError(t, err, "boom")
}

func TestStreamingResult(t *testing.T) {
	rows := &fakeRows{
		fields: []pgconn.FieldDescription{binaryField("n", pgtype.Int4OID)},
		rows:   [][][]byte{{encode(t, pgtype.Int4OID, int32(7))}},
	}
	canceled := 0
	res := newStreamingResult(rows, func() { canceled++ }, "SELECT n")

	require.True(t, res.Next())
	var n int
	require.NoError(t, res.Scan(&n))
	assert.Equal(t, 7, n)
	var s struct{ N int }
	require.NoError(t, res.ScanRow(&s))
	assert.Equal(t, 7, s.N)
	cols, _ := res.Columns()
	assert.Equal(t, []string{"n"}, cols)
	types, _ := res.ColumnTypes()
	assert.Equal(t, domain.DataTypeInteger, types[0].DataType)
	assert.Equal(t, 0, canceled, "the query context lives while rows are read")

	assert.False(t, res.Next())
	assert.Equal(t, 1, canceled, "the context is released once the rows are exhausted")
	assert.ErrorIs(t, res.ScanRow(&s), domain.ErrNoRows)
	require.NoError(t, res.Close())
	assert.Equal(t, 1, canceled, "release is idempotent")
	assert.NoError(t, res.Err())

	var bad string
	err := newStreamingResult(&fakeRows{fields: rows.fields, rows: rows.rows}, func() {}, "").Scan(&bad, &bad)
	assert.ErrorIs(t, err, domain.ErrInvalidData)

	failed := newStreamingResult(&fakeRows{err: &pgconn.PgError{Code: "22012"}}, func() {}, "SELECT 1/0")
	assert.False(t, failed.Next())
	assert.ErrorIs(t, failed.Err(), domain.ErrInvalidData)
	_, err = failed.LastInsertID()
	assert.ErrorIs(t, err, domain.ErrUnsupportedOperation)
	affected, err := failed.RowsAffected()
	assert.NoError(t, err)
	assert.Zero(t, affected)
}

func TestRow(t *testing.T) {
	missing := &PostgreSQLRow{err: domain.NewNoRowsError().WithCause(pgx.ErrNoRows)}
	var v int
	assert.ErrorIs(t, missing.Scan(&v), domain.ErrNoRows)
	assert.ErrorIs(t, missing.ScanRow(&struct{}{}), domain.ErrNoRows)
	assert.ErrorIs(t, missing.Err(), domain.ErrNoRows)

	row := &PostgreSQLRow{
		fields:  []pgconn.FieldDescription{binaryField("id", pgtype.Int4OID), textField("full_name", pgtype.TextOID)},
		values:  [][]byte{encode(t, pgtype.Int4OID, int32(3)), []byte("Ada Lovelace")},
		typeMap: pgtype.NewMap(),
	}
	var id int
	var name string
	require.NoError(t, row.Scan(&id, &name))
	assert.Equal(t, 3, id)
	assert.Equal(t, "Ada Lovelace", name)

	var person struct {
		ID       int
		FullName string
	}
	require.NoError(t, row.ScanRow(&person), "ScanRow maps every column")
	assert.Equal(t, 3, person.ID)
	assert.Equal(t, "Ada Lovelace", person.FullName)
	assert.NoError(t, row.Err())

	assert.ErrorIs(t, row.Scan(&id), domain.ErrInvalidData, "wrong destination count")
}

func TestMapOIDToDataType(t *testing.T) {
	m := pgtype.NewMap()
	tests := map[uint32]domain.DataType{
		pgtype.BoolOID:        domain.DataTypeBoolean,
		pgtype.Int2OID:        domain.DataTypeInteger,
		pgtype.Int4OID:        domain.DataTypeInteger,
		pgtype.Int8OID:        domain.DataTypeInteger,
		pgtype.Float4OID:      domain.DataTypeFloat,
		pgtype.Float8OID:      domain.DataTypeFloat,
		pgtype.NumericOID:     domain.DataTypeDecimal,
		pgtype.TextOID:        domain.DataTypeString,
		pgtype.VarcharOID:     domain.DataTypeString,
		pgtype.BPCharOID:      domain.DataTypeString,
		pgtype.NameOID:        domain.DataTypeString,
		pgtype.ByteaOID:       domain.DataTypeBinary,
		pgtype.DateOID:        domain.DataTypeDate,
		pgtype.TimeOID:        domain.DataTypeTime,
		pgtype.TimetzOID:      domain.DataTypeTime,
		pgtype.TimestampOID:   domain.DataTypeDateTime,
		pgtype.TimestamptzOID: domain.DataTypeDateTime,
		pgtype.JSONOID:        domain.DataTypeJSON,
		pgtype.JSONBOID:       domain.DataTypeJSON,
		pgtype.UUIDOID:        domain.DataTypeUUID,
		pgtype.Int4ArrayOID:   domain.DataTypeArray,
		pgtype.TextArrayOID:   domain.DataTypeArray,
		pgtype.JSONBArrayOID:  domain.DataTypeArray,
		pgtype.IntervalOID:    domain.DataTypeUnknown,
		999999:                domain.DataTypeUnknown,
	}
	for oid, want := range tests {
		assert.Equal(t, want, mapOIDToDataType(m, oid), "oid %d", oid)
	}
}

func TestColumnTypes(t *testing.T) {
	types := columnTypes(pgtype.NewMap(), []pgconn.FieldDescription{
		{Name: "code", DataTypeOID: pgtype.VarcharOID, TypeModifier: 20 + 4},
		{Name: "price", DataTypeOID: pgtype.NumericOID, TypeModifier: (10<<16 | 2) + 4},
		{Name: "free", DataTypeOID: pgtype.NumericOID, TypeModifier: -1},
		{Name: "custom", DataTypeOID: 999999},
	})
	assert.Equal(t, domain.ColumnType{Name: "code", DatabaseType: "varchar", DataType: domain.DataTypeString, Nullable: true, Length: 20}, types[0])
	assert.Equal(t, domain.ColumnType{Name: "price", DatabaseType: "numeric", DataType: domain.DataTypeDecimal, Nullable: true, Precision: 10, Scale: 2}, types[1])
	assert.Equal(t, int64(0), types[2].Precision)
	assert.Equal(t, "oid:999999", types[3].DatabaseType)
	assert.Equal(t, domain.DataTypeUnknown, types[3].DataType)
}
