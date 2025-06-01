package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HybridCache.io/storage/pkg/storage"
)

func TestQueryBuilder_Select(t *testing.T) {
	tests := []struct {
		name     string
		fields   []string
		expected string
	}{
		{"single field", []string{"id"}, "SELECT id FROM"},
		{"multiple fields", []string{"id", "name", "email"}, "SELECT id, name, email FROM"},
		{"no fields defaults to *", []string{}, "SELECT * FROM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBuilder()
			if len(tt.fields) > 0 {
				builder.Select(tt.fields...)
			}
			builder.From("users")
			
			query, err := builder.Build()
			require.NoError(t, err)
			assert.Contains(t, query.SQL, tt.expected)
		})
	}
}

func TestQueryBuilder_SelectDistinct(t *testing.T) {
	builder := NewBuilder()
	builder.SelectDistinct("category").From("products")
	
	query, err := builder.Build()
	require.NoError(t, err)
	assert.Contains(t, query.SQL, "SELECT DISTINCT category")
}

func TestQueryBuilder_SelectCount(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		expected string
	}{
		{"count all", "", "SELECT COUNT(*)"},
		{"count specific field", "id", "SELECT COUNT(id)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBuilder()
			builder.SelectCount(tt.field).From("users")
			
			query, err := builder.Build()
			require.NoError(t, err)
			assert.Contains(t, query.SQL, tt.expected)
		})
	}
}

func TestQueryBuilder_Where(t *testing.T) {
	builder := NewBuilder()
	builder.Select("*").
		From("users").
		Where(Equal("active", true)).
		Where(GreaterThan("age", 18))
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	assert.Contains(t, query.SQL, "WHERE")
	assert.Contains(t, query.SQL, "active = ?")
	assert.Contains(t, query.SQL, "age > ?")
	assert.Equal(t, []interface{}{true, 18}, query.Parameters)
}

func TestQueryBuilder_Join(t *testing.T) {
	builder := NewBuilder()
	builder.Select("u.name", "p.title").
		From("users u").
		InnerJoin("posts p", Equal("p.user_id", "u.id"))
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	assert.Contains(t, query.SQL, "INNER JOIN posts p ON p.user_id = ?")
	assert.Equal(t, []interface{}{"u.id"}, query.Parameters)
}

func TestQueryBuilder_OrderBy(t *testing.T) {
	builder := NewBuilder()
	builder.Select("*").
		From("users").
		OrderBy("name", storage.SortDirectionAsc).
		OrderByDesc("created_at")
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	assert.Contains(t, query.SQL, "ORDER BY name ASC, created_at DESC")
}

func TestQueryBuilder_LimitOffset(t *testing.T) {
	builder := NewBuilder()
	builder.Select("*").
		From("users").
		Limit(10).
		Offset(20)
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	assert.Contains(t, query.SQL, "LIMIT 10")
	assert.Contains(t, query.SQL, "OFFSET 20")
}

func TestQueryBuilder_Page(t *testing.T) {
	builder := NewBuilder()
	builder.Select("*").
		From("users").
		Page(3, 10) // Page 3 with 10 items per page
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	assert.Contains(t, query.SQL, "LIMIT 10")
	assert.Contains(t, query.SQL, "OFFSET 20") // (3-1) * 10 = 20
}

func TestQueryBuilder_ComplexQuery(t *testing.T) {
	builder := NewBuilder()
	builder.Select("u.name", "u.email", "COUNT(o.id) as order_count").
		From("users u").
		LeftJoin("orders o", Equal("o.user_id", "u.id")).
		Where(Equal("u.active", true)).
		Where(GreaterThan("u.age", 18)).
		GroupBy("u.id", "u.name", "u.email").
		OrderByDesc("order_count").
		Limit(50)
	
	query, err := builder.Build()
	require.NoError(t, err)
	
	expectedParts := []string{
		"SELECT u.name, u.email, COUNT(o.id) as order_count",
		"FROM users u",
		"LEFT JOIN orders o ON o.user_id = ?",
		"WHERE u.active = ? AND u.age > ?",
		"GROUP BY u.id, u.name, u.email",
		"ORDER BY order_count DESC",
		"LIMIT 50",
	}
	
	for _, part := range expectedParts {
		assert.Contains(t, query.SQL, part)
	}
	
	assert.Equal(t, []interface{}{"u.id", true, 18}, query.Parameters)
}

func TestConditionHelpers(t *testing.T) {
	tests := []struct {
		name      string
		condition storage.Condition
		field     string
		operator  storage.Operator
		value     interface{}
		values    []interface{}
	}{
		{
			name:      "Equal",
			condition: Equal("id", 1),
			field:     "id",
			operator:  storage.OperatorEqual,
			value:     1,
		},
		{
			name:      "NotEqual",
			condition: NotEqual("status", "deleted"),
			field:     "status",
			operator:  storage.OperatorNotEqual,
			value:     "deleted",
		},
		{
			name:      "GreaterThan",
			condition: GreaterThan("age", 18),
			field:     "age",
			operator:  storage.OperatorGreaterThan,
			value:     18,
		},
		{
			name:      "LessThan",
			condition: LessThan("price", 100.0),
			field:     "price",
			operator:  storage.OperatorLessThan,
			value:     100.0,
		},
		{
			name:      "In",
			condition: In("status", "active", "pending", "completed"),
			field:     "status",
			operator:  storage.OperatorIn,
			values:    []interface{}{"active", "pending", "completed"},
		},
		{
			name:      "Like",
			condition: Like("name", "%john%"),
			field:     "name",
			operator:  storage.OperatorLike,
			value:     "%john%",
		},
		{
			name:      "IsNull",
			condition: IsNull("deleted_at"),
			field:     "deleted_at",
			operator:  storage.OperatorIsNull,
		},
		{
			name:      "IsNotNull",
			condition: IsNotNull("email"),
			field:     "email",
			operator:  storage.OperatorIsNotNull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.field, tt.condition.Field)
			assert.Equal(t, tt.operator, tt.condition.Operator)
			if tt.value != nil {
				assert.Equal(t, tt.value, tt.condition.Value)
			}
			if tt.values != nil {
				assert.Equal(t, tt.values, tt.condition.Values)
			}
		})
	}
}

func TestCommandBuilder_Insert(t *testing.T) {
	cmd := Insert("users").
		Set("name", "John Doe").
		Set("email", "john@example.com").
		Set("age", 30)
	
	command, err := cmd.Build()
	require.NoError(t, err)
	
	assert.Equal(t, storage.CommandTypeInsert, command.Type)
	assert.Contains(t, command.SQL, "INSERT INTO users")
	assert.Contains(t, command.SQL, "name")
	assert.Contains(t, command.SQL, "email")
	assert.Contains(t, command.SQL, "age")
	assert.Contains(t, command.SQL, "VALUES")
	assert.Len(t, command.Parameters, 3)
	assert.Contains(t, command.Parameters, "John Doe")
	assert.Contains(t, command.Parameters, "john@example.com")
	assert.Contains(t, command.Parameters, 30)
}

func TestCommandBuilder_Update(t *testing.T) {
	cmd := Update("users").
		Set("name", "Jane Doe").
		Set("age", 25).
		Where(Equal("id", 1))
	
	command, err := cmd.Build()
	require.NoError(t, err)
	
	assert.Equal(t, storage.CommandTypeUpdate, command.Type)
	assert.Contains(t, command.SQL, "UPDATE users SET")
	assert.Contains(t, command.SQL, "name = ?")
	assert.Contains(t, command.SQL, "age = ?")
	assert.Contains(t, command.SQL, "WHERE id = ?")
	assert.Equal(t, []interface{}{"Jane Doe", 25, 1}, command.Parameters)
}

func TestCommandBuilder_Delete(t *testing.T) {
	cmd := Delete("users").
		Where(Equal("active", false)).
		Where(LessThan("last_login", "2023-01-01"))
	
	command, err := cmd.Build()
	require.NoError(t, err)
	
	assert.Equal(t, storage.CommandTypeDelete, command.Type)
	assert.Contains(t, command.SQL, "DELETE FROM users")
	assert.Contains(t, command.SQL, "WHERE active = ? AND last_login < ?")
	assert.Equal(t, []interface{}{false, "2023-01-01"}, command.Parameters)
}

func TestCommandBuilder_Values(t *testing.T) {
	values := map[string]interface{}{
		"name":  "Alice Smith",
		"email": "alice@example.com",
		"age":   28,
	}
	
	cmd := Insert("users").Values(values)
	
	command, err := cmd.Build()
	require.NoError(t, err)
	
	assert.Equal(t, storage.CommandTypeInsert, command.Type)
	assert.Contains(t, command.SQL, "INSERT INTO users")
	assert.Len(t, command.Parameters, 3)
	
	// Check that all values are present (order may vary due to map iteration)
	for _, param := range command.Parameters {
		assert.Contains(t, []interface{}{"Alice Smith", "alice@example.com", 28}, param)
	}
}

func TestCommandBuilder_Errors(t *testing.T) {
	tests := []struct {
		name    string
		builder func() *CommandBuilder
		wantErr bool
	}{
		{
			name: "Insert without table",
			builder: func() *CommandBuilder {
				return NewCommandBuilder().Set("name", "test")
			},
			wantErr: true,
		},
		{
			name: "Insert without values",
			builder: func() *CommandBuilder {
				return Insert("users")
			},
			wantErr: true,
		},
		{
			name: "Update without table",
			builder: func() *CommandBuilder {
				return NewCommandBuilder().Set("name", "test")
			},
			wantErr: true,
		},
		{
			name: "Update without values",
			builder: func() *CommandBuilder {
				return Update("users").Where(Equal("id", 1))
			},
			wantErr: true,
		},
		{
			name: "Delete without table",
			builder: func() *CommandBuilder {
				return NewCommandBuilder().Where(Equal("id", 1))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.builder().Build()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
