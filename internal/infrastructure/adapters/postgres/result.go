package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
)

// PostgreSQLResult implements domain.Result by streaming rows from the server.
// The query's context (and timeout) stays alive until the rows are exhausted or
// the result is closed.
type PostgreSQLResult struct {
	rows    pgx.Rows
	cancel  context.CancelFunc
	sql     string
	fields  []pgconn.FieldDescription // snapshot: pgconn reuses its copy per connection
	typeMap *pgtype.Map               // private map: the connection's is not ours once released
	scanner structScanner
}

func newStreamingResult(rows pgx.Rows, cancel context.CancelFunc, sql string) *PostgreSQLResult {
	return &PostgreSQLResult{
		rows:    rows,
		cancel:  cancel,
		sql:     sql,
		fields:  copyFields(rows.FieldDescriptions()),
		typeMap: pgtype.NewMap(),
	}
}

// Next advances to the next row. When no rows remain the result releases its
// connection; check Err afterwards.
func (r *PostgreSQLResult) Next() bool {
	if r.rows.Next() {
		if len(r.fields) == 0 { // not yet described when the query returned
			r.fields = copyFields(r.rows.FieldDescriptions())
		}
		return true
	}
	r.release()
	return false
}

// Close closes the result set and releases its connection.
func (r *PostgreSQLResult) Close() error {
	r.rows.Close()
	r.release()
	return nil
}

func (r *PostgreSQLResult) release() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

// Scan copies the columns of the current row into dest, as pgx's Rows.Scan does.
func (r *PostgreSQLResult) Scan(dest ...interface{}) error {
	if err := r.rows.Scan(dest...); err != nil {
		return scanError(err)
	}
	return nil
}

// ScanRow copies the current row into the struct pointed to by dest.
func (r *PostgreSQLResult) ScanRow(dest interface{}) error {
	return r.scanner.scan(r.typeMap, r.fields, r.rows.RawValues(), dest)
}

// Columns returns the column names.
func (r *PostgreSQLResult) Columns() ([]string, error) {
	return columnNames(r.fields), nil
}

// ColumnTypes returns the column types.
func (r *PostgreSQLResult) ColumnTypes() ([]domain.ColumnType, error) {
	return columnTypes(r.typeMap, r.fields), nil
}

// RowsAffected returns the row count from the command tag (e.g. for INSERT ...
// RETURNING); it is available once all rows have been read.
func (r *PostgreSQLResult) RowsAffected() (int64, error) {
	return r.rows.CommandTag().RowsAffected(), nil
}

// LastInsertID is not supported by PostgreSQL; use RETURNING instead.
func (r *PostgreSQLResult) LastInsertID() (int64, error) {
	return 0, domain.NewUnsupportedOperationError("LastInsertID")
}

// Err returns the error, if any, that ended iteration.
func (r *PostgreSQLResult) Err() error {
	if err := r.rows.Err(); err != nil {
		return mapError(err, opQuery, "query failed", r.sql)
	}
	return nil
}

// bufferedResult is an in-memory domain.Result holding rows that were fully read
// from the server, such as the query results of a batch.
type bufferedResult struct {
	fields  []pgconn.FieldDescription
	rows    [][][]byte // raw row values; a nil value is SQL NULL
	pos     int        // index of the current row
	tag     pgconn.CommandTag
	closed  bool
	typeMap *pgtype.Map
	scanner structScanner
}

// bufferRows reads all rows (and their field descriptions) into memory and
// closes rows.
func bufferRows(rows pgx.Rows) (*bufferedResult, error) {
	defer rows.Close()

	res := &bufferedResult{pos: -1, typeMap: pgtype.NewMap()}
	res.fields = copyFields(rows.FieldDescriptions())
	for rows.Next() {
		if len(res.fields) == 0 {
			res.fields = copyFields(rows.FieldDescriptions())
		}
		res.rows = append(res.rows, copyValues(rows.RawValues()))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.tag = rows.CommandTag()
	return res, nil
}

// Next advances to the next buffered row.
func (r *bufferedResult) Next() bool {
	if r.closed || r.pos+1 >= len(r.rows) {
		r.pos = len(r.rows)
		return false
	}
	r.pos++
	return true
}

// Close releases the buffered rows.
func (r *bufferedResult) Close() error {
	r.closed = true
	r.rows = nil
	return nil
}

func (r *bufferedResult) current() ([][]byte, error) {
	if r.closed || r.pos < 0 || r.pos >= len(r.rows) {
		return nil, domain.NewDataError("NO_ROWS", "no current row: call Next before scanning")
	}
	return r.rows[r.pos], nil
}

// Scan copies the columns of the current row into dest.
func (r *bufferedResult) Scan(dest ...interface{}) error {
	values, err := r.current()
	if err != nil {
		return err
	}
	if err := pgx.ScanRow(r.typeMap, r.fields, values, dest...); err != nil {
		return scanError(err)
	}
	return nil
}

// ScanRow copies the current row into the struct pointed to by dest.
func (r *bufferedResult) ScanRow(dest interface{}) error {
	values, err := r.current()
	if err != nil {
		return err
	}
	return r.scanner.scan(r.typeMap, r.fields, values, dest)
}

// Columns returns the column names.
func (r *bufferedResult) Columns() ([]string, error) {
	return columnNames(r.fields), nil
}

// ColumnTypes returns the column types.
func (r *bufferedResult) ColumnTypes() ([]domain.ColumnType, error) {
	return columnTypes(r.typeMap, r.fields), nil
}

// RowsAffected returns the row count from the command tag.
func (r *bufferedResult) RowsAffected() (int64, error) {
	return r.tag.RowsAffected(), nil
}

// LastInsertID is not supported by PostgreSQL; use RETURNING instead.
func (r *bufferedResult) LastInsertID() (int64, error) {
	return 0, domain.NewUnsupportedOperationError("LastInsertID")
}

// Err always returns nil: the rows were read without error.
func (r *bufferedResult) Err() error {
	return nil
}

// PostgreSQLRow implements domain.Row for QueryOne. The first row of the result
// is buffered together with its field descriptions. When the query returned no
// rows, Scan, ScanRow and Err return an error matching domain.ErrNoRows.
type PostgreSQLRow struct {
	fields  []pgconn.FieldDescription
	values  [][]byte
	err     error
	typeMap *pgtype.Map
	scanner structScanner
}

// Scan copies the row's columns into dest.
func (r *PostgreSQLRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if err := pgx.ScanRow(r.typeMap, r.fields, r.values, dest...); err != nil {
		return scanError(err)
	}
	return nil
}

// ScanRow copies the row into the struct pointed to by dest.
func (r *PostgreSQLRow) ScanRow(dest interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.scanner.scan(r.typeMap, r.fields, r.values, dest)
}

// Err returns the deferred error of the row (domain.ErrNoRows if there was none).
func (r *PostgreSQLRow) Err() error {
	return r.err
}

func scanError(err error) error {
	return domain.NewDataError("INVALID_DATA", "failed to scan row").WithCause(err)
}

func copyFields(fields []pgconn.FieldDescription) []pgconn.FieldDescription {
	return append([]pgconn.FieldDescription{}, fields...)
}

// copyValues copies raw row values (only valid until the next Rows.Next) into
// a single allocation, preserving the distinction between NULL (nil) and empty.
func copyValues(values [][]byte) [][]byte {
	size := 0
	for _, v := range values {
		size += len(v)
	}
	buf := make([]byte, size)
	row := make([][]byte, len(values))
	for i, v := range values {
		if v == nil {
			continue
		}
		n := copy(buf, v)
		row[i] = buf[:n:n]
		buf = buf[n:]
	}
	return row
}

func columnNames(fields []pgconn.FieldDescription) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	return names
}

func columnTypes(m *pgtype.Map, fields []pgconn.FieldDescription) []domain.ColumnType {
	types := make([]domain.ColumnType, len(fields))
	for i, f := range fields {
		ct := domain.ColumnType{
			Name:         f.Name,
			DatabaseType: fmt.Sprintf("oid:%d", f.DataTypeOID),
			DataType:     mapOIDToDataType(m, f.DataTypeOID),
			Nullable:     true, // not reported in row descriptions
		}
		if t, ok := m.TypeForOID(f.DataTypeOID); ok {
			ct.DatabaseType = t.Name
		}
		switch f.DataTypeOID {
		case pgtype.VarcharOID, pgtype.BPCharOID:
			if f.TypeModifier > 4 {
				ct.Length = int64(f.TypeModifier - 4)
			}
		case pgtype.NumericOID:
			if f.TypeModifier >= 4 {
				mod := f.TypeModifier - 4
				ct.Precision = int64((mod >> 16) & 0xffff)
				ct.Scale = int64(((mod & 0x7ff) ^ 1024) - 1024)
			}
		}
		types[i] = ct
	}
	return types
}

// mapOIDToDataType maps a PostgreSQL type OID to a domain.DataType.
func mapOIDToDataType(m *pgtype.Map, oid uint32) domain.DataType {
	switch oid {
	case pgtype.BoolOID:
		return domain.DataTypeBoolean
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID, pgtype.OIDOID, pgtype.XIDOID, pgtype.XID8OID, pgtype.CIDOID:
		return domain.DataTypeInteger
	case pgtype.Float4OID, pgtype.Float8OID:
		return domain.DataTypeFloat
	case pgtype.NumericOID:
		return domain.DataTypeDecimal
	case pgtype.TextOID, pgtype.VarcharOID, pgtype.BPCharOID, pgtype.NameOID, pgtype.QCharOID, pgtype.XMLOID, pgtype.UnknownOID:
		return domain.DataTypeString
	case pgtype.ByteaOID:
		return domain.DataTypeBinary
	case pgtype.DateOID:
		return domain.DataTypeDate
	case pgtype.TimeOID, pgtype.TimetzOID:
		return domain.DataTypeTime
	case pgtype.TimestampOID, pgtype.TimestamptzOID:
		return domain.DataTypeDateTime
	case pgtype.JSONOID, pgtype.JSONBOID:
		return domain.DataTypeJSON
	case pgtype.UUIDOID:
		return domain.DataTypeUUID
	}
	if t, ok := m.TypeForOID(oid); ok {
		if _, isArray := t.Codec.(*pgtype.ArrayCodec); isArray {
			return domain.DataTypeArray
		}
	}
	return domain.DataTypeUnknown
}

// structScanner maps result columns onto struct fields. The mapping and the
// per-column scan plans are computed once per destination type and reused for
// the following rows.
type structScanner struct {
	typ     reflect.Type
	columns []columnTarget
}

type columnTarget struct {
	index []int  // field index path; nil if no field matches the column
	field string // field name, for error messages
	plan  pgtype.ScanPlan
}

// scan copies one row of raw values into the struct pointed to by dest. A
// column maps to the field whose db tag (the name before any comma) matches it,
// otherwise to the field whose name, or snake_case name, matches it ignoring
// case. Fields tagged db:"-" and columns without a field are skipped. NULL sets
// the field to its zero value (nil for pointers); other values are converted by
// pgx's typed scan plans, which reject lossy or invalid conversions.
func (s *structScanner) scan(m *pgtype.Map, fields []pgconn.FieldDescription, values [][]byte, dest interface{}) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return domain.NewStorageError(domain.ErrorTypeValidation, "INVALID_PARAMETERS",
			fmt.Sprintf("ScanRow destination must be a non-nil pointer to a struct, got %T", dest))
	}
	if len(values) != len(fields) {
		return domain.NewDataError("NO_ROWS", "no current row: call Next before scanning")
	}

	v = v.Elem()
	if s.typ != v.Type() || len(s.columns) != len(fields) {
		s.typ = v.Type()
		s.columns = mapColumns(s.typ, fields)
	}

	for i, value := range values {
		target := &s.columns[i]
		if target.index == nil {
			continue
		}
		field, err := fieldByIndex(v, target.index)
		if err != nil {
			return domain.NewDataError("INVALID_DATA", err.Error())
		}
		field.SetZero() // NULL, and no stale map keys or struct fields from a previous row
		if value == nil {
			continue
		}
		fd := fields[i]
		ptr := field.Addr().Interface()
		if target.plan == nil {
			target.plan = m.PlanScan(fd.DataTypeOID, fd.Format, ptr)
		}
		if err := target.plan.Scan(value, ptr); err != nil {
			return domain.NewDataError("INVALID_DATA",
				fmt.Sprintf("cannot scan column %q into field %s", fd.Name, target.field)).
				WithCause(err)
		}
	}
	return nil
}

func mapColumns(t reflect.Type, fields []pgconn.FieldDescription) []columnTarget {
	tagged := make(map[string]reflect.StructField)
	named := make(map[string]reflect.StructField)
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && (f.Type.Kind() == reflect.Struct ||
			(f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct)) {
			continue // embedded struct: its fields are promoted
		}
		tag := f.Tag.Get("db")
		if tag == "-" {
			continue
		}
		if name, _, _ := strings.Cut(tag, ","); name != "" {
			addField(tagged, strings.ToLower(name), f)
			continue
		}
		addField(named, strings.ToLower(f.Name), f)
		addField(named, snakeCase(f.Name), f)
	}

	columns := make([]columnTarget, len(fields))
	for i, fd := range fields {
		key := strings.ToLower(fd.Name)
		f, ok := tagged[key]
		if !ok {
			f, ok = named[key]
		}
		if ok {
			columns[i] = columnTarget{index: f.Index, field: f.Name}
		}
	}
	return columns
}

func addField(fields map[string]reflect.StructField, key string, f reflect.StructField) {
	if _, exists := fields[key]; !exists {
		fields[key] = f
	}
}

// fieldByIndex is reflect.Value.FieldByIndex that allocates nil embedded struct
// pointers on the way.
func fieldByIndex(v reflect.Value, index []int) (reflect.Value, error) {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}, fmt.Errorf("cannot set field of nil embedded pointer to unexported %s", v.Type().Elem())
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v, nil
}

// snakeCase converts a Go field name to snake_case: UserID -> user_id,
// HTTPServer -> http_server.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) ||
				(unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
