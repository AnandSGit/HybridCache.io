"""
Unit tests for query builder.

Tests the fluent API for building SQL queries and commands
in a database-agnostic way.
"""

import pytest

from storage.query.builder import (
    CommandBuilder,
    QueryBuilder,
    between,
    delete,
    equal,
    greater_than,
    in_,
    insert,
    is_not_null,
    is_null,
    less_than,
    like,
    not_equal,
    select,
    update,
)
from storage.types import (
    CommandType,
    JoinType,
    Operator,
    QueryType,
    SortDirection,
)


class TestQueryBuilder:
    """Test QueryBuilder class."""
    
    def test_simple_select(self):
        """Test simple SELECT query."""
        query = select("id", "name").from_("users").build()
        
        assert query.sql == "SELECT id, name FROM users"
        assert query.parameters == []
        assert query.query_type == QueryType.SELECT
    
    def test_select_all(self):
        """Test SELECT * query."""
        query = select().from_("users").build()
        
        assert query.sql == "SELECT * FROM users"
        assert query.parameters == []
    
    def test_select_distinct(self):
        """Test SELECT DISTINCT query."""
        query = QueryBuilder().select_distinct("name").from_("users").build()
        
        assert query.sql == "SELECT DISTINCT name FROM users"
        assert query.parameters == []
    
    def test_select_count(self):
        """Test SELECT COUNT query."""
        query = QueryBuilder().select_count().from_("users").build()
        
        assert query.sql == "SELECT COUNT(*) FROM users"
        assert query.parameters == []
    
    def test_select_count_field(self):
        """Test SELECT COUNT(field) query."""
        query = QueryBuilder().select_count("id").from_("users").build()
        
        assert query.sql == "SELECT COUNT(id) FROM users"
        assert query.parameters == []
    
    def test_where_condition(self):
        """Test WHERE condition."""
        condition = equal("name", "John")
        query = select("*").from_("users").where(condition).build()
        
        assert query.sql == "SELECT * FROM users WHERE name = ?"
        assert query.parameters == ["John"]
    
    def test_multiple_where_conditions(self):
        """Test multiple WHERE conditions."""
        condition1 = equal("name", "John")
        condition2 = greater_than("age", 18)
        query = (select("*")
                .from_("users")
                .where(condition1)
                .where(condition2)
                .build())
        
        assert query.sql == "SELECT * FROM users WHERE name = ? AND age > ?"
        assert query.parameters == ["John", 18]
    
    def test_where_and(self):
        """Test where_and method."""
        condition1 = equal("name", "John")
        condition2 = greater_than("age", 18)
        condition3 = is_not_null("email")
        query = (select("*")
                .from_("users")
                .where_and(condition1, condition2, condition3)
                .build())
        
        assert query.sql == "SELECT * FROM users WHERE name = ? AND age > ? AND email IS NOT NULL"
        assert query.parameters == ["John", 18]
    
    def test_order_by(self):
        """Test ORDER BY clause."""
        query = (select("*")
                .from_("users")
                .order_by("name")
                .build())
        
        assert query.sql == "SELECT * FROM users ORDER BY name ASC"
        assert query.parameters == []
    
    def test_order_by_desc(self):
        """Test ORDER BY DESC clause."""
        query = (select("*")
                .from_("users")
                .order_by("name", SortDirection.DESC)
                .build())
        
        assert query.sql == "SELECT * FROM users ORDER BY name DESC"
        assert query.parameters == []
    
    def test_order_by_desc_method(self):
        """Test order_by_desc method."""
        query = (select("*")
                .from_("users")
                .order_by_desc("created_at")
                .build())
        
        assert query.sql == "SELECT * FROM users ORDER BY created_at DESC"
        assert query.parameters == []
    
    def test_multiple_order_by(self):
        """Test multiple ORDER BY clauses."""
        query = (select("*")
                .from_("users")
                .order_by("name")
                .order_by_desc("created_at")
                .build())
        
        assert query.sql == "SELECT * FROM users ORDER BY name ASC, created_at DESC"
        assert query.parameters == []
    
    def test_limit(self):
        """Test LIMIT clause."""
        query = (select("*")
                .from_("users")
                .limit(10)
                .build())
        
        assert query.sql == "SELECT * FROM users LIMIT 10"
        assert query.parameters == []
    
    def test_offset(self):
        """Test OFFSET clause."""
        query = (select("*")
                .from_("users")
                .offset(20)
                .build())
        
        assert query.sql == "SELECT * FROM users OFFSET 20"
        assert query.parameters == []
    
    def test_limit_offset(self):
        """Test LIMIT and OFFSET clauses."""
        query = (select("*")
                .from_("users")
                .limit(10)
                .offset(20)
                .build())
        
        assert query.sql == "SELECT * FROM users LIMIT 10 OFFSET 20"
        assert query.parameters == []
    
    def test_page(self):
        """Test pagination helper."""
        query = (select("*")
                .from_("users")
                .page(3, 10)  # Page 3, size 10
                .build())
        
        assert query.sql == "SELECT * FROM users LIMIT 10 OFFSET 20"
        assert query.parameters == []
    
    def test_group_by(self):
        """Test GROUP BY clause."""
        query = (select("department", "COUNT(*)")
                .from_("users")
                .group_by("department")
                .build())
        
        assert query.sql == "SELECT department, COUNT(*) FROM users GROUP BY department"
        assert query.parameters == []
    
    def test_multiple_group_by(self):
        """Test multiple GROUP BY fields."""
        query = (select("department", "role", "COUNT(*)")
                .from_("users")
                .group_by("department", "role")
                .build())
        
        assert query.sql == "SELECT department, role, COUNT(*) FROM users GROUP BY department, role"
        assert query.parameters == []
    
    def test_having(self):
        """Test HAVING clause."""
        condition = greater_than("COUNT(*)", 5)
        query = (select("department", "COUNT(*)")
                .from_("users")
                .group_by("department")
                .having(condition)
                .build())
        
        assert query.sql == "SELECT department, COUNT(*) FROM users GROUP BY department HAVING COUNT(*) > ?"
        assert query.parameters == [5]
    
    def test_inner_join(self):
        """Test INNER JOIN."""
        condition = equal("users.department_id", "departments.id")
        query = (select("users.name", "departments.name")
                .from_("users")
                .inner_join("departments", condition)
                .build())
        
        expected_sql = "SELECT users.name, departments.name FROM users INNER JOIN departments ON users.department_id = ?"
        assert query.sql == expected_sql
        assert query.parameters == ["departments.id"]
    
    def test_left_join(self):
        """Test LEFT JOIN."""
        condition = equal("users.department_id", "departments.id")
        query = (select("users.name", "departments.name")
                .from_("users")
                .left_join("departments", condition)
                .build())
        
        expected_sql = "SELECT users.name, departments.name FROM users LEFT JOIN departments ON users.department_id = ?"
        assert query.sql == expected_sql
        assert query.parameters == ["departments.id"]
    
    def test_complex_query(self):
        """Test complex query with multiple clauses."""
        where_condition = equal("active", True)
        having_condition = greater_than("COUNT(*)", 1)
        
        query = (select("department", "COUNT(*) as user_count")
                .from_("users")
                .where(where_condition)
                .group_by("department")
                .having(having_condition)
                .order_by("user_count", SortDirection.DESC)
                .limit(5)
                .build())
        
        expected_sql = ("SELECT department, COUNT(*) as user_count FROM users "
                       "WHERE active = ? GROUP BY department HAVING COUNT(*) > ? "
                       "ORDER BY user_count DESC LIMIT 5")
        assert query.sql == expected_sql
        assert query.parameters == [True, 1]
    
    def test_build_raw(self):
        """Test build_raw method."""
        sql, params = (select("*")
                      .from_("users")
                      .where(equal("name", "John"))
                      .build_raw())
        
        assert sql == "SELECT * FROM users WHERE name = ?"
        assert params == ["John"]
    
    def test_missing_from_table(self):
        """Test error when FROM table is missing."""
        with pytest.raises(ValueError, match="FROM table is required"):
            select("*").build()


class TestCommandBuilder:
    """Test CommandBuilder class."""
    
    def test_insert_command(self):
        """Test INSERT command."""
        command = (insert("users")
                  .set("name", "John")
                  .set("email", "john@example.com")
                  .build())
        
        assert command.sql == "INSERT INTO users (name, email) VALUES (?, ?)"
        assert command.parameters == ["John", "john@example.com"]
        assert command.command_type == CommandType.INSERT
    
    def test_insert_with_values(self):
        """Test INSERT with values method."""
        values = {"name": "John", "email": "john@example.com", "age": 30}
        command = insert("users").values(values).build()
        
        # Order might vary, so check components
        assert "INSERT INTO users" in command.sql
        assert "VALUES" in command.sql
        assert command.command_type == CommandType.INSERT
        assert len(command.parameters) == 3
        assert "John" in command.parameters
        assert "john@example.com" in command.parameters
        assert 30 in command.parameters
    
    def test_update_command(self):
        """Test UPDATE command."""
        command = (update("users")
                  .set("name", "Jane")
                  .set("email", "jane@example.com")
                  .where(equal("id", 1))
                  .build())
        
        assert command.sql == "UPDATE users SET name = ?, email = ? WHERE id = ?"
        assert command.parameters == ["Jane", "jane@example.com", 1]
        assert command.command_type == CommandType.UPDATE
    
    def test_update_with_values(self):
        """Test UPDATE with values method."""
        values = {"name": "Jane", "email": "jane@example.com"}
        command = (update("users")
                  .values(values)
                  .where(equal("id", 1))
                  .build())
        
        # Order might vary, so check components
        assert "UPDATE users SET" in command.sql
        assert "WHERE id = ?" in command.sql
        assert command.command_type == CommandType.UPDATE
        assert 1 in command.parameters
    
    def test_delete_command(self):
        """Test DELETE command."""
        command = (delete("users")
                  .where(equal("id", 1))
                  .build())
        
        assert command.sql == "DELETE FROM users WHERE id = ?"
        assert command.parameters == [1]
        assert command.command_type == CommandType.DELETE
    
    def test_delete_without_where(self):
        """Test DELETE without WHERE clause."""
        command = delete("users").build()
        
        assert command.sql == "DELETE FROM users"
        assert command.parameters == []
        assert command.command_type == CommandType.DELETE
    
    def test_update_multiple_where(self):
        """Test UPDATE with multiple WHERE conditions."""
        command = (update("users")
                  .set("active", False)
                  .where(equal("department", "sales"))
                  .where(less_than("last_login", "2023-01-01"))
                  .build())
        
        expected_sql = "UPDATE users SET active = ? WHERE department = ? AND last_login < ?"
        assert command.sql == expected_sql
        assert command.parameters == [False, "sales", "2023-01-01"]
    
    def test_command_build_raw(self):
        """Test command build_raw method."""
        sql, params = (insert("users")
                      .set("name", "John")
                      .build_raw())
        
        assert sql == "INSERT INTO users (name) VALUES (?)"
        assert params == ["John"]
    
    def test_insert_without_values(self):
        """Test INSERT without values raises error."""
        with pytest.raises(ValueError, match="Values are required for INSERT"):
            insert("users").build()
    
    def test_update_without_values(self):
        """Test UPDATE without values raises error."""
        with pytest.raises(ValueError, match="Values are required for UPDATE"):
            update("users").where(equal("id", 1)).build()


class TestConditionHelpers:
    """Test condition helper functions."""
    
    def test_equal_condition(self):
        """Test equal condition helper."""
        condition = equal("name", "John")
        
        assert condition.field == "name"
        assert condition.operator == Operator.EQUAL
        assert condition.value == "John"
    
    def test_not_equal_condition(self):
        """Test not_equal condition helper."""
        condition = not_equal("status", "inactive")
        
        assert condition.field == "status"
        assert condition.operator == Operator.NOT_EQUAL
        assert condition.value == "inactive"
    
    def test_greater_than_condition(self):
        """Test greater_than condition helper."""
        condition = greater_than("age", 18)
        
        assert condition.field == "age"
        assert condition.operator == Operator.GREATER_THAN
        assert condition.value == 18
    
    def test_like_condition(self):
        """Test like condition helper."""
        condition = like("name", "John%")
        
        assert condition.field == "name"
        assert condition.operator == Operator.LIKE
        assert condition.value == "John%"
    
    def test_in_condition(self):
        """Test in_ condition helper."""
        condition = in_("status", "active", "pending", "approved")
        
        assert condition.field == "status"
        assert condition.operator == Operator.IN
        assert condition.values == ["active", "pending", "approved"]
    
    def test_between_condition(self):
        """Test between condition helper."""
        condition = between("age", 18, 65)
        
        assert condition.field == "age"
        assert condition.operator == Operator.BETWEEN
        assert condition.values == [18, 65]
    
    def test_is_null_condition(self):
        """Test is_null condition helper."""
        condition = is_null("deleted_at")
        
        assert condition.field == "deleted_at"
        assert condition.operator == Operator.IS_NULL
        assert condition.value is None
    
    def test_is_not_null_condition(self):
        """Test is_not_null condition helper."""
        condition = is_not_null("email")
        
        assert condition.field == "email"
        assert condition.operator == Operator.IS_NOT_NULL
        assert condition.value is None


class TestConditionBuilding:
    """Test condition building in queries."""
    
    def test_in_condition_empty_values(self):
        """Test IN condition with empty values."""
        builder = QueryBuilder()
        condition = in_("status")  # No values
        sql, params = builder._build_condition(condition)
        
        assert sql == "1=0"  # No match condition
        assert params == []
    
    def test_not_in_condition_empty_values(self):
        """Test NOT IN condition with empty values."""
        from storage.query.builder import not_in
        builder = QueryBuilder()
        condition = not_in("status")  # No values
        sql, params = builder._build_condition(condition)
        
        assert sql == "1=1"  # All match condition
        assert params == []
    
    def test_between_condition_building(self):
        """Test BETWEEN condition building."""
        builder = QueryBuilder()
        condition = between("age", 18, 65)
        sql, params = builder._build_condition(condition)
        
        assert sql == "age BETWEEN ? AND ?"
        assert params == [18, 65]
