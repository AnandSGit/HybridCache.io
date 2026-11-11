package mongodb

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// bsonTypeNames maps BSON types to the aliases MongoDB uses for $type.
var bsonTypeNames = map[bson.Type]string{
	bson.TypeDouble:           "double",
	bson.TypeString:           "string",
	bson.TypeEmbeddedDocument: "object",
	bson.TypeArray:            "array",
	bson.TypeBinary:           "binData",
	bson.TypeUndefined:        "undefined",
	bson.TypeObjectID:         "objectId",
	bson.TypeBoolean:          "bool",
	bson.TypeDateTime:         "date",
	bson.TypeNull:             "null",
	bson.TypeRegex:            "regex",
	bson.TypeDBPointer:        "dbPointer",
	bson.TypeJavaScript:       "javascript",
	bson.TypeSymbol:           "symbol",
	bson.TypeCodeWithScope:    "javascriptWithScope",
	bson.TypeInt32:            "int",
	bson.TypeTimestamp:        "timestamp",
	bson.TypeInt64:            "long",
	bson.TypeDecimal128:       "decimal",
	bson.TypeMinKey:           "minKey",
	bson.TypeMaxKey:           "maxKey",
}

// MongoDBResult implements domain.Result over a driver cursor, or over
// materialised documents (COUNT, DISTINCT and batch results).
type MongoDBResult struct {
	cursor  *mongo.Cursor
	ctx     context.Context    // query context, bounded by the query timeout
	cancel  context.CancelFunc // releases ctx on Close
	docs    []bson.Raw         // materialised documents when cursor is nil
	pos     int
	current bson.Raw
	columns []string // projected columns in select order; nil means document order
	err     error
	closed  bool
}

func newCursorResult(ctx context.Context, cancel context.CancelFunc, cursor *mongo.Cursor, columns []string) *MongoDBResult {
	return &MongoDBResult{cursor: cursor, ctx: ctx, cancel: cancel, columns: columns}
}

func newDocumentResult(docs []bson.Raw, columns []string) *MongoDBResult {
	return &MongoDBResult{docs: docs, columns: columns}
}

// Next advances to the next document. As with database/sql, the result closes
// itself once the documents are exhausted or iteration fails.
func (r *MongoDBResult) Next() bool {
	r.current = nil
	if r.closed {
		return false
	}
	if r.cursor == nil {
		if r.pos < len(r.docs) {
			r.current = r.docs[r.pos]
			r.pos++
			return true
		}
		r.finish()
		return false
	}
	if r.cursor.Next(r.ctx) {
		r.current = r.cursor.Current
		return true
	}
	if err := r.cursor.Err(); err != nil {
		r.err = queryError(err, "failed to read query results")
	}
	r.finish()
	return false
}

// finish closes the result at the end of iteration, keeping the first error.
func (r *MongoDBResult) finish() {
	if err := r.Close(); err != nil && r.err == nil {
		r.err = err
	}
}

// Close closes the cursor and releases the query context. It is idempotent.
func (r *MongoDBResult) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.current = nil
	var err error
	if r.cursor != nil {
		if cerr := r.cursor.Close(r.ctx); cerr != nil {
			err = queryError(cerr, "failed to close cursor")
		}
	}
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

// Scan copies the columns of the current document into dest, in select order
// for projected queries and in document order otherwise.
func (r *MongoDBResult) Scan(dest ...interface{}) error {
	if r.current == nil {
		return errNoCurrentDocument()
	}
	return scanColumns(r.current, r.columns, dest)
}

// ScanRow decodes the current document into dest (a pointer to a struct, map or bson.D).
func (r *MongoDBResult) ScanRow(dest interface{}) error {
	if r.current == nil {
		return errNoCurrentDocument()
	}
	return decodeDocument(r.current, dest)
}

// Columns returns the projected fields, or the current document's field names
// when the query returns whole documents.
func (r *MongoDBResult) Columns() ([]string, error) {
	if r.columns != nil {
		return slices.Clone(r.columns), nil
	}
	if r.current == nil {
		return nil, errNoCurrentDocument()
	}
	cols, err := documentColumns(r.current, nil)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	return names, nil
}

// ColumnTypes returns the columns with the BSON types of the current document's
// values. Before Next, a projected result reports its columns with unknown types.
func (r *MongoDBResult) ColumnTypes() ([]domain.ColumnType, error) {
	if r.current == nil {
		if r.columns == nil {
			return nil, errNoCurrentDocument()
		}
		types := make([]domain.ColumnType, len(r.columns))
		for i, name := range r.columns {
			types[i] = domain.ColumnType{Name: name, Nullable: true}
		}
		return types, nil
	}
	cols, err := documentColumns(r.current, r.columns)
	if err != nil {
		return nil, err
	}
	types := make([]domain.ColumnType, len(cols))
	for i, c := range cols {
		typeName := bsonTypeNames[c.value.Type]
		dataType, err := mapDatabaseType(typeName)
		if err != nil {
			dataType = domain.DataTypeUnknown
		}
		types[i] = domain.ColumnType{Name: c.name, DatabaseType: typeName, DataType: dataType, Nullable: true}
	}
	return types, nil
}

// RowsAffected is not applicable to query results.
func (r *MongoDBResult) RowsAffected() (int64, error) {
	return 0, domain.NewDataError("NOT_APPLICABLE", "rows affected not applicable for query results")
}

// LastInsertID is not applicable to query results.
func (r *MongoDBResult) LastInsertID() (int64, error) {
	return 0, domain.NewDataError("NOT_SUPPORTED", "last insert ID not supported for MongoDB query results")
}

// Err returns the error, if any, that ended iteration.
func (r *MongoDBResult) Err() error {
	return r.err
}

// MongoDBRow implements domain.Row for a single document.
type MongoDBRow struct {
	document bson.Raw
	columns  []string
}

// Scan copies the document's columns into dest (see MongoDBResult.Scan).
func (r *MongoDBRow) Scan(dest ...interface{}) error {
	if r.document == nil {
		return errNoCurrentDocument()
	}
	return scanColumns(r.document, r.columns, dest)
}

// ScanRow decodes the document into dest (a pointer to a struct, map or bson.D).
func (r *MongoDBRow) ScanRow(dest interface{}) error {
	if r.document == nil {
		return errNoCurrentDocument()
	}
	return decodeDocument(r.document, dest)
}

// Err returns nil: QueryOne reports failures, including no rows, directly.
func (r *MongoDBRow) Err() error {
	return nil
}

func errNoCurrentDocument() error {
	return domain.NewDataError("NO_CURRENT_ROW", "no current document: Next must be called and return true first")
}

type column struct {
	name  string
	value bson.RawValue
}

// documentColumns returns the projected fields of doc in select order, or all of
// its elements in document order when fields is nil. A projected field that is
// missing from the document reads as null.
func documentColumns(doc bson.Raw, fields []string) ([]column, error) {
	if fields != nil {
		cols := make([]column, len(fields))
		for i, f := range fields {
			v, err := doc.LookupErr(strings.Split(f, ".")...)
			if err != nil {
				v = bson.RawValue{Type: bson.TypeNull}
			}
			cols[i] = column{name: f, value: v}
		}
		return cols, nil
	}
	elems, err := doc.Elements()
	if err != nil {
		return nil, domain.NewDataError("INVALID_DATA", "malformed document").WithCause(err)
	}
	cols := make([]column, len(elems))
	for i, e := range elems {
		cols[i] = column{name: e.Key(), value: e.Value()}
	}
	return cols, nil
}

func scanColumns(doc bson.Raw, fields []string, dest []interface{}) error {
	cols, err := documentColumns(doc, fields)
	if err != nil {
		return err
	}
	if len(dest) != len(cols) {
		return domain.NewDataError("INVALID_DESTINATION",
			fmt.Sprintf("expected %d destination arguments in Scan, got %d", len(cols), len(dest)))
	}
	for i, d := range dest {
		if err := decodeValue(cols[i].value, d); err != nil {
			return domain.NewDataError("SCAN_FAILED", fmt.Sprintf("cannot scan field %q", cols[i].name)).WithCause(err)
		}
	}
	return nil
}

// decodeValue decodes v into dest with the driver's rules, additionally
// accepting an ObjectID for a *string (as its hex form).
func decodeValue(v bson.RawValue, dest interface{}) error {
	if s, ok := dest.(*string); ok && v.Type == bson.TypeObjectID {
		*s = v.ObjectID().Hex()
		return nil
	}
	return v.Unmarshal(dest)
}

// decodeDocument decodes doc with the driver's struct mapping (bson tags, else
// lowercased field names); ObjectIDs decode into strings as hex.
func decodeDocument(doc bson.Raw, dest interface{}) error {
	dec := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(doc)))
	dec.ObjectIDAsHexString()
	if err := dec.Decode(dest); err != nil {
		return domain.NewDataError("SCAN_FAILED", "cannot decode document").WithCause(err)
	}
	return nil
}
