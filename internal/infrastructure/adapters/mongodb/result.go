package mongodb

import (
	"context"
	"fmt"
	"reflect"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDBResult implements the domain.Result interface for MongoDB
type MongoDBResult struct {
	cursor *mongo.Cursor
	err    error
}

// Next advances to the next document
func (r *MongoDBResult) Next() bool {
	if r.cursor == nil {
		return false
	}
	return r.cursor.Next(context.Background())
}

// Close closes the result cursor
func (r *MongoDBResult) Close() error {
	if r.cursor != nil {
		return r.cursor.Close(context.Background())
	}
	return nil
}

// Scan scans the current document into the provided destinations
func (r *MongoDBResult) Scan(dest ...interface{}) error {
	if r.cursor == nil {
		return domain.NewDataError("NO_CURSOR", "no cursor available")
	}

	// Get current document as BSON
	var doc bson.M
	if err := r.cursor.Decode(&doc); err != nil {
		return domain.NewDataError("DECODE_FAILED", err.Error()).WithCause(err)
	}

	// Map document fields to destinations
	if len(dest) == 1 {
		// Single destination - decode entire document
		if err := r.cursor.Decode(dest[0]); err != nil {
			return domain.NewDataError("DECODE_FAILED", err.Error()).WithCause(err)
		}
		return nil
	}

	// Multiple destinations - map by field order or names
	// This is a simplified implementation
	i := 0
	for _, value := range doc {
		if i >= len(dest) {
			break
		}
		if err := setFieldValue(reflect.ValueOf(dest[i]).Elem(), value); err != nil {
			return domain.NewDataError("FIELD_MAPPING_FAILED", err.Error()).WithCause(err)
		}
		i++
	}

	return nil
}

// ScanRow scans the current document into a struct
func (r *MongoDBResult) ScanRow(dest interface{}) error {
	if r.cursor == nil {
		return domain.NewDataError("NO_CURSOR", "no cursor available")
	}

	// Decode directly into the destination struct
	if err := r.cursor.Decode(dest); err != nil {
		return domain.NewDataError("DECODE_FAILED", err.Error()).WithCause(err)
	}

	return nil
}

// Columns returns the column names (for MongoDB, this returns document field names)
func (r *MongoDBResult) Columns() ([]string, error) {
	// For MongoDB, we can't determine columns without examining documents
	// This is a simplified implementation
	return []string{}, nil
}

// ColumnTypes returns column type information
func (r *MongoDBResult) ColumnTypes() ([]domain.ColumnType, error) {
	// For MongoDB, column types are dynamic and document-specific
	// This is a simplified implementation
	return []domain.ColumnType{}, nil
}

// RowsAffected returns the number of rows affected (not applicable for queries)
func (r *MongoDBResult) RowsAffected() (int64, error) {
	return 0, nil
}

// LastInsertID returns the last insert ID (not applicable for queries)
func (r *MongoDBResult) LastInsertID() (int64, error) {
	return 0, nil
}

// Err returns any error that occurred during iteration
func (r *MongoDBResult) Err() error {
	if r.cursor != nil {
		return r.cursor.Err()
	}
	return r.err
}

// MongoDBRow implements the domain.Row interface for MongoDB
type MongoDBRow struct {
	document bson.M
	err      error
}

// Scan scans the document into the provided destinations
func (r *MongoDBRow) Scan(dest ...interface{}) error {
	if r.document == nil {
		return domain.NewDataError("NO_DOCUMENT", "no document available")
	}

	// Map document fields to destinations
	if len(dest) == 1 {
		// Single destination - try to decode entire document
		destValue := reflect.ValueOf(dest[0])
		if destValue.Kind() != reflect.Ptr {
			return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer")
		}

		destElem := destValue.Elem()
		if destElem.Kind() == reflect.Struct {
			return r.scanToStruct(destElem)
		} else {
			// Single field - get first value
			for _, value := range r.document {
				return setFieldValue(destElem, value)
			}
		}
	}

	// Multiple destinations - map by field order
	i := 0
	for _, value := range r.document {
		if i >= len(dest) {
			break
		}
		destValue := reflect.ValueOf(dest[i])
		if destValue.Kind() != reflect.Ptr {
			return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer")
		}
		if err := setFieldValue(destValue.Elem(), value); err != nil {
			return domain.NewDataError("FIELD_MAPPING_FAILED", err.Error()).WithCause(err)
		}
		i++
	}

	return nil
}

// ScanRow scans the document into a struct
func (r *MongoDBRow) ScanRow(dest interface{}) error {
	if r.document == nil {
		return domain.NewDataError("NO_DOCUMENT", "no document available")
	}

	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer")
	}

	destElem := destValue.Elem()
	if destElem.Kind() != reflect.Struct {
		return domain.NewDataError("INVALID_DESTINATION", "destination must be a pointer to struct")
	}

	return r.scanToStruct(destElem)
}

// Err returns any error that occurred
func (r *MongoDBRow) Err() error {
	return r.err
}

// scanToStruct scans the document into a struct using field names or bson tags
func (r *MongoDBRow) scanToStruct(destValue reflect.Value) error {
	destType := destValue.Type()

	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		fieldValue := destValue.Field(i)

		if !fieldValue.CanSet() {
			continue
		}

		// Get field name from bson tag or field name
		fieldName := field.Name
		if bsonTag := field.Tag.Get("bson"); bsonTag != "" && bsonTag != "-" {
			// Parse bson tag (handle "name,omitempty" format)
			if commaIndex := len(bsonTag); commaIndex > 0 {
				if idx := findComma(bsonTag); idx != -1 {
					fieldName = bsonTag[:idx]
				} else {
					fieldName = bsonTag
				}
			}
		}

		// Get value from document
		if value, exists := r.document[fieldName]; exists {
			if err := setFieldValue(fieldValue, value); err != nil {
				return domain.NewDataError("FIELD_MAPPING_FAILED",
					fmt.Sprintf("failed to set field %s: %v", fieldName, err)).WithCause(err)
			}
		}
	}

	return nil
}

// setFieldValue sets a reflect.Value with the given interface{} value
func setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	valueReflect := reflect.ValueOf(value)
	fieldType := field.Type()

	// Handle type conversions
	if valueReflect.Type().ConvertibleTo(fieldType) {
		field.Set(valueReflect.Convert(fieldType))
		return nil
	}

	// Handle special cases
	switch fieldType.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", value))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, ok := convertToInt64(value); ok {
			field.SetInt(intVal)
		} else {
			return fmt.Errorf("cannot convert %T to int", value)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if intVal, ok := convertToInt64(value); ok && intVal >= 0 {
			field.SetUint(uint64(intVal))
		} else {
			return fmt.Errorf("cannot convert %T to uint", value)
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, ok := convertToFloat64(value); ok {
			field.SetFloat(floatVal)
		} else {
			return fmt.Errorf("cannot convert %T to float", value)
		}
	case reflect.Bool:
		if boolVal, ok := value.(bool); ok {
			field.SetBool(boolVal)
		} else {
			return fmt.Errorf("cannot convert %T to bool", value)
		}
	case reflect.Slice:
		if valueReflect.Kind() == reflect.Slice {
			field.Set(valueReflect)
		} else {
			return fmt.Errorf("cannot convert %T to slice", value)
		}
	case reflect.Map:
		if valueReflect.Kind() == reflect.Map {
			field.Set(valueReflect)
		} else {
			return fmt.Errorf("cannot convert %T to map", value)
		}
	case reflect.Interface:
		field.Set(valueReflect)
	default:
		return fmt.Errorf("unsupported field type: %v", fieldType.Kind())
	}

	return nil
}

// convertToInt64 converts various numeric types to int64
func convertToInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v <= 9223372036854775807 { // max int64
			return int64(v), true
		}
	case float32:
		return int64(v), true
	case float64:
		return int64(v), true
	}
	return 0, false
}

// convertToFloat64 converts various numeric types to float64
func convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	}
	return 0, false
}

// findComma finds the first comma in a string
func findComma(s string) int {
	for i, r := range s {
		if r == ',' {
			return i
		}
	}
	return -1
}
