"""
Unit tests for storage types.

Tests all data types, enums, and dataclasses used throughout the library.
"""

import datetime
from dataclasses import fields

import pytest

from storage.types import (
    Command,
    CommandType,
    Condition,
    Config,
    DataType,
    DatabaseType,
    ErrorType,
    ExecuteResult,
    HealthStatus,
    HealthStatusType,
    IsolationLevel,
    Operator,
    Query,
    QueryType,
    SortDirection,
    StorageInfo,
    TableSchema,
    TxOptions,
)


class TestEnums:
    """Test enum types."""
    
    def test_database_type_values(self):
        """Test DatabaseType enum values."""
        assert DatabaseType.POSTGRESQL.value == "postgresql"
        assert DatabaseType.MYSQL.value == "mysql"
        assert DatabaseType.SQLITE.value == "sqlite"
        assert DatabaseType.REDIS.value == "redis"
        assert DatabaseType.MONGODB.value == "mongodb"
        assert DatabaseType.UNKNOWN.value == "unknown"
    
    def test_data_type_values(self):
        """Test DataType enum values."""
        assert DataType.STRING.value == "string"
        assert DataType.INTEGER.value == "integer"
        assert DataType.FLOAT.value == "float"
        assert DataType.BOOLEAN.value == "boolean"
        assert DataType.DATETIME.value == "datetime"
        assert DataType.JSON.value == "json"
        assert DataType.UUID.value == "uuid"
        assert DataType.UNKNOWN.value == "unknown"
    
    def test_query_type_values(self):
        """Test QueryType enum values."""
        assert QueryType.SELECT.value == "select"
        assert QueryType.INSERT.value == "insert"
        assert QueryType.UPDATE.value == "update"
        assert QueryType.DELETE.value == "delete"
        assert QueryType.UNKNOWN.value == "unknown"
    
    def test_command_type_values(self):
        """Test CommandType enum values."""
        assert CommandType.INSERT.value == "insert"
        assert CommandType.UPDATE.value == "update"
        assert CommandType.DELETE.value == "delete"
        assert CommandType.UNKNOWN.value == "unknown"
    
    def test_isolation_level_values(self):
        """Test IsolationLevel enum values."""
        assert IsolationLevel.READ_UNCOMMITTED.value == "read_uncommitted"
        assert IsolationLevel.READ_COMMITTED.value == "read_committed"
        assert IsolationLevel.REPEATABLE_READ.value == "repeatable_read"
        assert IsolationLevel.SERIALIZABLE.value == "serializable"
    
    def test_operator_values(self):
        """Test Operator enum values."""
        assert Operator.EQUAL.value == "equal"
        assert Operator.NOT_EQUAL.value == "not_equal"
        assert Operator.GREATER_THAN.value == "greater_than"
        assert Operator.LESS_THAN.value == "less_than"
        assert Operator.LIKE.value == "like"
        assert Operator.IN.value == "in"
        assert Operator.IS_NULL.value == "is_null"
    
    def test_sort_direction_values(self):
        """Test SortDirection enum values."""
        assert SortDirection.ASC.value == "asc"
        assert SortDirection.DESC.value == "desc"
    
    def test_health_status_type_values(self):
        """Test HealthStatusType enum values."""
        assert HealthStatusType.HEALTHY.value == "healthy"
        assert HealthStatusType.DEGRADED.value == "degraded"
        assert HealthStatusType.UNHEALTHY.value == "unhealthy"
        assert HealthStatusType.UNKNOWN.value == "unknown"
    
    def test_error_type_values(self):
        """Test ErrorType enum values."""
        assert ErrorType.CONNECTION.value == "connection"
        assert ErrorType.QUERY.value == "query"
        assert ErrorType.TRANSACTION.value == "transaction"
        assert ErrorType.DATA.value == "data"
        assert ErrorType.SCHEMA.value == "schema"
        assert ErrorType.TIMEOUT.value == "timeout"
        assert ErrorType.VALIDATION.value == "validation"
        assert ErrorType.UNKNOWN.value == "unknown"


class TestDataClasses:
    """Test dataclass types."""
    
    def test_condition_creation(self):
        """Test Condition dataclass creation."""
        condition = Condition(
            field="name",
            operator=Operator.EQUAL,
            value="test"
        )
        
        assert condition.field == "name"
        assert condition.operator == Operator.EQUAL
        assert condition.value == "test"
        assert condition.values is None
    
    def test_condition_with_values(self):
        """Test Condition with multiple values."""
        condition = Condition(
            field="status",
            operator=Operator.IN,
            values=["active", "pending"]
        )
        
        assert condition.field == "status"
        assert condition.operator == Operator.IN
        assert condition.values == ["active", "pending"]
    
    def test_query_creation(self):
        """Test Query dataclass creation."""
        query = Query(
            sql="SELECT * FROM users WHERE id = ?",
            parameters=[1],
            query_type=QueryType.SELECT
        )
        
        assert query.sql == "SELECT * FROM users WHERE id = ?"
        assert query.parameters == [1]
        assert query.query_type == QueryType.SELECT
        assert query.timeout is None
        assert query.cache_key is None
        assert query.read_only is False
    
    def test_query_with_timeout(self):
        """Test Query with timeout."""
        timeout = datetime.timedelta(seconds=30)
        query = Query(
            sql="SELECT * FROM users",
            parameters=[],
            timeout=timeout
        )
        
        assert query.timeout == timeout
    
    def test_command_creation(self):
        """Test Command dataclass creation."""
        command = Command(
            sql="INSERT INTO users (name, email) VALUES (?, ?)",
            parameters=["John", "john@example.com"],
            command_type=CommandType.INSERT
        )
        
        assert command.sql == "INSERT INTO users (name, email) VALUES (?, ?)"
        assert command.parameters == ["John", "john@example.com"]
        assert command.command_type == CommandType.INSERT
        assert command.return_id is False
        assert command.return_count is True
    
    def test_execute_result_creation(self):
        """Test ExecuteResult dataclass creation."""
        result = ExecuteResult(
            rows_affected=5,
            last_insert_id=123
        )
        
        assert result.rows_affected == 5
        assert result.last_insert_id == 123
    
    def test_config_creation(self):
        """Test Config dataclass creation."""
        config = Config(
            host="localhost",
            port=5432,
            database="testdb",
            username="testuser",
            password="testpass"
        )
        
        assert config.host == "localhost"
        assert config.port == 5432
        assert config.database == "testdb"
        assert config.username == "testuser"
        assert config.password == "testpass"
        assert config.max_open_conns == 25  # default value
        assert config.max_idle_conns == 5   # default value
    
    def test_config_with_dsn(self):
        """Test Config with DSN."""
        dsn = "postgresql://user:pass@localhost:5432/dbname"
        config = Config(dsn=dsn)
        
        assert config.dsn == dsn
    
    def test_tx_options_creation(self):
        """Test TxOptions dataclass creation."""
        timeout = datetime.timedelta(minutes=5)
        options = TxOptions(
            isolation=IsolationLevel.SERIALIZABLE,
            read_only=True,
            timeout=timeout
        )
        
        assert options.isolation == IsolationLevel.SERIALIZABLE
        assert options.read_only is True
        assert options.timeout == timeout
    
    def test_tx_options_defaults(self):
        """Test TxOptions default values."""
        options = TxOptions()
        
        assert options.isolation == IsolationLevel.READ_COMMITTED
        assert options.read_only is False
        assert options.timeout is None
    
    def test_storage_info_creation(self):
        """Test StorageInfo dataclass creation."""
        info = StorageInfo(
            name="PostgreSQL",
            version="1.0.0",
            database_type=DatabaseType.POSTGRESQL,
            features=["transactions", "joins"]
        )
        
        assert info.name == "PostgreSQL"
        assert info.version == "1.0.0"
        assert info.database_type == DatabaseType.POSTGRESQL
        assert info.features == ["transactions", "joins"]
        assert info.limits is None
    
    def test_health_status_creation(self):
        """Test HealthStatus dataclass creation."""
        now = datetime.datetime.now()
        status = HealthStatus(
            status=HealthStatusType.HEALTHY,
            message="All systems operational",
            timestamp=now,
            details={"connections": 10}
        )
        
        assert status.status == HealthStatusType.HEALTHY
        assert status.message == "All systems operational"
        assert status.timestamp == now
        assert status.details == {"connections": 10}
    
    def test_health_status_default_timestamp(self):
        """Test HealthStatus with default timestamp."""
        status = HealthStatus(
            status=HealthStatusType.HEALTHY,
            message="OK"
        )
        
        assert status.status == HealthStatusType.HEALTHY
        assert status.message == "OK"
        assert isinstance(status.timestamp, datetime.datetime)
        assert status.details == {}
    
    def test_table_schema_creation(self):
        """Test TableSchema dataclass creation."""
        schema = TableSchema(
            name="users",
            columns=[],
            indexes=[],
            primary_key=["id"],
            foreign_keys={"department_id": "departments.id"}
        )
        
        assert schema.name == "users"
        assert schema.columns == []
        assert schema.indexes == []
        assert schema.primary_key == ["id"]
        assert schema.foreign_keys == {"department_id": "departments.id"}


class TestDataClassFields:
    """Test that dataclasses have expected fields."""
    
    def test_query_fields(self):
        """Test Query dataclass fields."""
        field_names = {field.name for field in fields(Query)}
        expected_fields = {
            "sql", "parameters", "query_type", "timeout", 
            "cache_key", "cache_ttl", "read_only"
        }
        assert field_names == expected_fields
    
    def test_command_fields(self):
        """Test Command dataclass fields."""
        field_names = {field.name for field in fields(Command)}
        expected_fields = {
            "sql", "parameters", "command_type", "timeout",
            "return_id", "return_count", "on_conflict"
        }
        assert field_names == expected_fields
    
    def test_config_fields(self):
        """Test Config dataclass fields."""
        field_names = {field.name for field in fields(Config)}
        expected_fields = {
            "host", "port", "database", "username", "password", "dsn",
            "max_open_conns", "max_idle_conns", "conn_max_lifetime", "conn_max_idle_time",
            "connect_timeout", "query_timeout", "tx_timeout",
            "ssl_mode", "ssl_cert", "ssl_key", "ssl_root_ca", "options"
        }
        assert field_names == expected_fields


class TestTypeValidation:
    """Test type validation and constraints."""
    
    def test_condition_field_required(self):
        """Test that Condition requires field."""
        # This should work
        condition = Condition(field="test", operator=Operator.EQUAL)
        assert condition.field == "test"
    
    def test_query_sql_required(self):
        """Test that Query requires SQL."""
        # This should work
        query = Query(sql="SELECT 1")
        assert query.sql == "SELECT 1"
    
    def test_command_sql_required(self):
        """Test that Command requires SQL."""
        # This should work
        command = Command(sql="INSERT INTO test VALUES (?)")
        assert command.sql == "INSERT INTO test VALUES (?)"
    
    def test_execute_result_rows_affected_required(self):
        """Test that ExecuteResult requires rows_affected."""
        # This should work
        result = ExecuteResult(rows_affected=1)
        assert result.rows_affected == 1
