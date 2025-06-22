package postgres

import (
	"fmt"
	"reflect"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/jackc/pgx/v5"
)

// PostgreSQLResult implements the domain.Result interface for PostgreSQL
type PostgreSQLResult struct {
	rows pgx.Rows
	err  error
}

// Next advances to the next row
func (r *PostgreSQLResult) Next() bool {
	if r.rows == nil {
		return false
	}
	return r.rows.Next()
}

// Close closes the result set
func (r *PostgreSQLResult) Close() error {
	if r.rows != nil {
		r.rows.Close()
	}
	return nil
}

// Scan scans the current row into the provided destinations
func (r *PostgreSQLResult) Scan(dest ...interface{}) error {
	if r.rows == nil {
		return domain.NewDataError("NO_ROWS", "no rows available")
	}
	return r.rows.Scan(dest...)
}

// ScanRow scans the current row into a struct
func (r *PostgreSQLResult) ScanRow(dest interface{}) error {
	if r.rows == nil {
		return domain.NewDataError("NO_ROWS", "no rows available")
	}

	// Use reflection to scan into struct fields
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer")
	}

	destValue = destValue.Elem()
	if destValue.Kind() != reflect.Struct {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer to struct")
	}

	// Get field descriptions
	fieldDescriptions := r.rows.FieldDescriptions()
	values := make([]interface{}, len(fieldDescriptions))
	valuePtrs := make([]interface{}, len(fieldDescriptions))

	// Create pointers to values
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Scan the row
	if err := r.rows.Scan(valuePtrs...); err != nil {
		return domain.NewDataError("SCAN_FAILED", err.Error()).WithCause(err)
	}

	// Map values to struct fields
	destType := destValue.Type()
	for i, fieldDesc := range fieldDescriptions {
		fieldName := string(fieldDesc.Name)

		// Find matching struct field (by name or db tag)
		for j := 0; j < destType.NumField(); j++ {
			field := destType.Field(j)
			structField := destValue.Field(j)

			if !structField.CanSet() {
				continue
			}

			// Check field name or db tag
			dbTag := field.Tag.Get("db")
			if dbTag == fieldName || (dbTag == "" && field.Name == fieldName) {
				if values[i] != nil {
					if err := setFieldValue(structField, values[i]); err != nil {
						return domain.NewDataError("FIELD_MAPPING_FAILED", err.Error()).WithCause(err)
					}
				}
				break
			}
		}
	}

	return nil
}

// Columns returns the column names
func (r *PostgreSQLResult) Columns() ([]string, error) {
	if r.rows == nil {
		return nil, domain.NewDataError("NO_ROWS", "no rows available")
	}

	fieldDescriptions := r.rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, desc := range fieldDescriptions {
		columns[i] = string(desc.Name)
	}
	return columns, nil
}

// ColumnTypes returns the column types
func (r *PostgreSQLResult) ColumnTypes() ([]domain.ColumnType, error) {
	if r.rows == nil {
		return nil, domain.NewDataError("NO_ROWS", "no rows available")
	}

	fieldDescriptions := r.rows.FieldDescriptions()
	columnTypes := make([]domain.ColumnType, len(fieldDescriptions))

	for i, desc := range fieldDescriptions {
		columnTypes[i] = domain.ColumnType{
			Name:         string(desc.Name),
			DatabaseType: fmt.Sprintf("oid:%d", desc.DataTypeOID),
			DataType:     mapOIDToDataType(desc.DataTypeOID),
			Nullable:     true, // PostgreSQL doesn't provide this info in field descriptions
		}
	}

	return columnTypes, nil
}

// RowsAffected returns the number of rows affected (not applicable for SELECT)
func (r *PostgreSQLResult) RowsAffected() (int64, error) {
	return 0, domain.NewDataError("NOT_APPLICABLE", "rows affected not applicable for query results")
}

// LastInsertID returns the last insert ID (not supported by PostgreSQL)
func (r *PostgreSQLResult) LastInsertID() (int64, error) {
	return 0, domain.NewDataError("NOT_SUPPORTED", "last insert ID not supported by PostgreSQL")
}

// Err returns any error that occurred during iteration
func (r *PostgreSQLResult) Err() error {
	if r.rows != nil {
		return r.rows.Err()
	}
	return r.err
}

// PostgreSQLRow implements the domain.Row interface for PostgreSQL
type PostgreSQLRow struct {
	row pgx.Row
	err error
}

// Scan scans the row into the provided destinations
func (r *PostgreSQLRow) Scan(dest ...interface{}) error {
	if r.row == nil {
		return domain.NewDataError("NO_ROW", "no row available")
	}
	return r.row.Scan(dest...)
}

// ScanRow scans the row into a struct
func (r *PostgreSQLRow) ScanRow(dest interface{}) error {
	if r.row == nil {
		return domain.NewDataError("NO_ROW", "no row available")
	}

	// For single row, we need to use a different approach
	// This is a simplified implementation - in practice, you'd want more sophisticated mapping
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer")
	}

	destValue = destValue.Elem()
	if destValue.Kind() != reflect.Struct {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer to struct")
	}

	// For simplicity, we'll scan into the first field
	// In a real implementation, you'd want proper field mapping
	if destValue.NumField() > 0 {
		firstField := destValue.Field(0)
		if firstField.CanSet() {
			var value interface{}
			if err := r.row.Scan(&value); err != nil {
				return domain.NewDataError("SCAN_FAILED", err.Error()).WithCause(err)
			}
			if err := setFieldValue(firstField, value); err != nil {
				return domain.NewDataError("FIELD_MAPPING_FAILED", err.Error()).WithCause(err)
			}
		}
	}

	return nil
}

// Err returns any error that occurred
func (r *PostgreSQLRow) Err() error {
	return r.err
}

// Helper functions
func mapOIDToDataType(oid uint32) domain.DataType {
	// This is a simplified mapping - in practice, you'd want a complete OID to DataType mapping
	switch oid {
	case 23: // INT4
		return domain.DataTypeInteger
	case 25: // TEXT
		return domain.DataTypeString
	case 16: // BOOL
		return domain.DataTypeBoolean
	case 1114: // TIMESTAMP
		return domain.DataTypeDateTime
	case 1082: // DATE
		return domain.DataTypeDate
	case 1083: // TIME
		return domain.DataTypeTime
	case 17: // BYTEA
		return domain.DataTypeBinary
	case 114: // JSON
		return domain.DataTypeJSON
	case 3802: // JSONB
		return domain.DataTypeJSON
	case 2950: // UUID
		return domain.DataTypeUUID
	default:
		return domain.DataTypeString
	}
}

func setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	valueReflect := reflect.ValueOf(value)

	// Handle type conversions
	if field.Type() == valueReflect.Type() {
		field.Set(valueReflect)
		return nil
	}

	// Try to convert
	if valueReflect.Type().ConvertibleTo(field.Type()) {
		field.Set(valueReflect.Convert(field.Type()))
		return nil
	}

	// Handle string conversions
	if field.Kind() == reflect.String {
		field.SetString(valueReflect.String())
		return nil
	}

	// Handle numeric conversions
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if valueReflect.Kind() >= reflect.Int && valueReflect.Kind() <= reflect.Int64 {
			field.SetInt(valueReflect.Int())
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if valueReflect.Kind() >= reflect.Uint && valueReflect.Kind() <= reflect.Uint64 {
			field.SetUint(valueReflect.Uint())
			return nil
		}
	case reflect.Float32, reflect.Float64:
		if valueReflect.Kind() >= reflect.Float32 && valueReflect.Kind() <= reflect.Float64 {
			field.SetFloat(valueReflect.Float())
			return nil
		}
	case reflect.Bool:
		if valueReflect.Kind() == reflect.Bool {
			field.SetBool(valueReflect.Bool())
			return nil
		}
	}

	return domain.NewDataError("TYPE_CONVERSION_FAILED", "cannot convert value to field type")
}
