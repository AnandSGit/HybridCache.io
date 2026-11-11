package mongodb

import (
	"testing"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func rawDoc(t *testing.T, doc bson.D) bson.Raw {
	t.Helper()
	b, err := bson.Marshal(doc)
	require.NoError(t, err)
	return b
}

func errorCode(err error) string {
	if se, ok := err.(*domain.StorageError); ok {
		return se.Code
	}
	return ""
}

func TestResult_ScanFollowsSelectOrder(t *testing.T) {
	// The document's field order differs from the select order.
	docs := []bson.Raw{
		rawDoc(t, bson.D{{Key: "a", Value: "a1"}, {Key: "b", Value: int32(1)}, {Key: "c", Value: 1.5}}),
		rawDoc(t, bson.D{{Key: "b", Value: int32(2)}, {Key: "c", Value: 2.5}, {Key: "a", Value: "a2"}}),
	}
	r := newDocumentResult(docs, []string{"c", "a", "b"})

	var c float64
	var a string
	var b int
	require.True(t, r.Next())
	require.NoError(t, r.Scan(&c, &a, &b))
	assert.Equal(t, []interface{}{1.5, "a1", 1}, []interface{}{c, a, b})
	require.True(t, r.Next())
	require.NoError(t, r.Scan(&c, &a, &b))
	assert.Equal(t, []interface{}{2.5, "a2", 2}, []interface{}{c, a, b})
	assert.False(t, r.Next())
	assert.NoError(t, r.Err())
}

func TestResult_ScanDocumentOrder(t *testing.T) {
	oid := bson.NewObjectID()
	r := newDocumentResult([]bson.Raw{rawDoc(t, bson.D{
		{Key: "_id", Value: oid}, {Key: "name", Value: "Ann"}, {Key: "tags", Value: bson.A{"x"}},
	})}, nil)
	require.True(t, r.Next())

	var id, name string
	var tags interface{}
	require.NoError(t, r.Scan(&id, &name, &tags))
	assert.Equal(t, oid.Hex(), id, "ObjectIDs scan into strings as hex")
	assert.Equal(t, "Ann", name)
	assert.Equal(t, bson.A{"x"}, tags)

	err := r.Scan(&id, &name)
	assert.Equal(t, "INVALID_DESTINATION", errorCode(err), "destination count must match: %v", err)
}

func TestResult_ScanErrors(t *testing.T) {
	r := newDocumentResult([]bson.Raw{rawDoc(t, bson.D{{Key: "n", Value: "text"}})}, []string{"n", "missing"})

	var n string
	var missing = 7
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(r.Scan(&n, &missing)), "Scan before Next")
	require.True(t, r.Next())
	require.NoError(t, r.Scan(&n, &missing))
	assert.Equal(t, 0, missing, "a projected field missing from the document reads as null")

	var wrong int
	err := r.Scan(&wrong, &missing)
	assert.Equal(t, "SCAN_FAILED", errorCode(err))
	assert.Contains(t, err.Error(), `"n"`)

	assert.Error(t, r.Scan(n, &missing), "destinations must be pointers")

	assert.False(t, r.Next())
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(r.Scan(&n, &missing)), "Scan after the last document")
}

func TestResult_ScanRow(t *testing.T) {
	oid := bson.NewObjectID()
	doc := rawDoc(t, bson.D{{Key: "_id", Value: oid}, {Key: "name", Value: "Ann"}, {Key: "age", Value: int32(30)}})

	var untagged struct {
		Name string
		Age  int
	}
	r := newDocumentResult([]bson.Raw{doc}, nil)
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(r.ScanRow(&untagged)))
	require.True(t, r.Next())
	require.NoError(t, r.ScanRow(&untagged))
	assert.Equal(t, "Ann", untagged.Name)
	assert.Equal(t, 30, untagged.Age)

	var tagged struct {
		ID       string `bson:"_id"`
		FullName string `bson:"name"`
	}
	require.NoError(t, r.ScanRow(&tagged))
	assert.Equal(t, oid.Hex(), tagged.ID)
	assert.Equal(t, "Ann", tagged.FullName)

	var m bson.M
	require.NoError(t, r.ScanRow(&m))
	assert.Equal(t, "Ann", m["name"])

	var bad struct{ Name int }
	assert.Equal(t, "SCAN_FAILED", errorCode(r.ScanRow(&bad)))
}

func TestResult_Columns(t *testing.T) {
	doc := rawDoc(t, bson.D{
		{Key: "s", Value: "x"}, {Key: "i", Value: int32(1)}, {Key: "l", Value: int64(1)},
		{Key: "d", Value: 1.5}, {Key: "o", Value: bson.D{{Key: "k", Value: 1}}}, {Key: "n", Value: nil},
	})

	unprojected := newDocumentResult([]bson.Raw{doc}, nil)
	_, err := unprojected.Columns()
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(err), "columns of whole documents need a current document")
	_, err = unprojected.ColumnTypes()
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(err))
	require.True(t, unprojected.Next())
	cols, err := unprojected.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"s", "i", "l", "d", "o", "n"}, cols)

	types, err := unprojected.ColumnTypes()
	require.NoError(t, err)
	expected := []struct {
		dbType   string
		dataType domain.DataType
	}{
		{"string", domain.DataTypeString},
		{"int", domain.DataTypeInteger},
		{"long", domain.DataTypeInteger},
		{"double", domain.DataTypeFloat},
		{"object", domain.DataTypeJSON},
		{"null", domain.DataTypeUnknown},
	}
	require.Len(t, types, len(expected))
	for i, e := range expected {
		assert.Equal(t, cols[i], types[i].Name)
		assert.Equal(t, e.dbType, types[i].DatabaseType, cols[i])
		assert.Equal(t, e.dataType, types[i].DataType, cols[i])
		assert.True(t, types[i].Nullable)
	}

	projected := newDocumentResult([]bson.Raw{doc}, []string{"l", "s"})
	cols, err = projected.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"l", "s"}, cols, "projected columns are known before Next")
	types, err = projected.ColumnTypes()
	require.NoError(t, err)
	assert.Equal(t, []domain.ColumnType{{Name: "l", Nullable: true}, {Name: "s", Nullable: true}}, types)
	require.True(t, projected.Next())
	types, err = projected.ColumnTypes()
	require.NoError(t, err)
	assert.Equal(t, "long", types[0].DatabaseType)
	assert.Equal(t, "string", types[1].DatabaseType)
}

func TestResult_MetadataAndClose(t *testing.T) {
	r := newDocumentResult([]bson.Raw{rawDoc(t, bson.D{{Key: "a", Value: 1}})}, nil)

	_, err := r.RowsAffected()
	assert.Equal(t, "NOT_APPLICABLE", errorCode(err))
	assert.True(t, domain.IsDataError(err))
	_, err = r.LastInsertID()
	assert.Equal(t, "NOT_SUPPORTED", errorCode(err))
	assert.True(t, domain.IsDataError(err))

	require.NoError(t, r.Close())
	require.NoError(t, r.Close(), "Close is idempotent")
	assert.False(t, r.Next(), "a closed result has no documents")
}

func TestRow(t *testing.T) {
	row := &MongoDBRow{document: rawDoc(t, bson.D{{Key: "b", Value: "B"}, {Key: "a", Value: "A"}}), columns: []string{"a", "b"}}
	var a, b string
	require.NoError(t, row.Scan(&a, &b))
	assert.Equal(t, "A", a)
	assert.Equal(t, "B", b)

	var s struct{ A, B string }
	require.NoError(t, row.ScanRow(&s))
	assert.Equal(t, "A", s.A)
	assert.NoError(t, row.Err())

	empty := &MongoDBRow{}
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(empty.Scan(&a)))
	assert.Equal(t, "NO_CURRENT_ROW", errorCode(empty.ScanRow(&s)))
}
