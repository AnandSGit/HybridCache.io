"""
Unit tests for MongoDB adapter.

Tests the MongoDB adapter functionality without requiring a real database connection.
"""

import pytest

from storage.adapters.mongodb import MongoDBAdapter
from storage.errors import ValidationError
from storage.types import Config, DataType, DatabaseType


class TestMongoDBAdapter:
    """Test MongoDB adapter functionality."""
    
    def test_adapter_info(self):
        """Test adapter information."""
        adapter = MongoDBAdapter()
        
        assert adapter.name() == "mongodb"
        assert adapter.version() == "1.0.0"
        assert adapter.database_type() == DatabaseType.MONGODB.value
    
    def test_feature_support(self):
        """Test feature support flags."""
        adapter = MongoDBAdapter()
        
        assert adapter.supports_transactions() is True
        assert adapter.supports_joins() is True
        assert adapter.supports_batch() is True
        assert adapter.supports_schema() is True
    
    def test_parse_dsn_basic(self):
        """Test parsing basic MongoDB DSN."""
        adapter = MongoDBAdapter()
        
        dsn = "mongodb://localhost:27017/testdb"
        config = adapter.parse_dsn(dsn)
        
        assert config.dsn == dsn
        assert config.host == "localhost"
        assert config.port == 27017
        assert config.database == "testdb"
        assert config.username == ""
        assert config.password == ""
    
    def test_parse_dsn_with_auth(self):
        """Test parsing MongoDB DSN with authentication."""
        adapter = MongoDBAdapter()
        
        dsn = "mongodb://user:pass@localhost:27017/testdb"
        config = adapter.parse_dsn(dsn)
        
        assert config.dsn == dsn
        assert config.host == "localhost"
        assert config.port == 27017
        assert config.database == "testdb"
        assert config.username == "user"
        assert config.password == "pass"
    
    def test_parse_dsn_with_options(self):
        """Test parsing MongoDB DSN with options."""
        adapter = MongoDBAdapter()
        
        dsn = "mongodb://localhost:27017/testdb?authSource=admin&ssl=true"
        config = adapter.parse_dsn(dsn)
        
        assert config.dsn == dsn
        assert config.host == "localhost"
        assert config.port == 27017
        assert config.database == "testdb"
        assert config.options is not None
        assert config.options.get("authSource") == "admin"
        assert config.options.get("ssl") == "true"
    
    def test_parse_dsn_default_port(self):
        """Test parsing MongoDB DSN with default port."""
        adapter = MongoDBAdapter()
        
        dsn = "mongodb://localhost/testdb"
        config = adapter.parse_dsn(dsn)
        
        assert config.host == "localhost"
        assert config.port == 27017  # Default MongoDB port
        assert config.database == "testdb"
    
    def test_parse_dsn_invalid(self):
        """Test parsing invalid MongoDB DSN."""
        adapter = MongoDBAdapter()
        
        with pytest.raises(ValidationError):
            adapter.parse_dsn("invalid://uri")
    
    def test_validate_config_valid_dsn(self):
        """Test validating config with valid DSN."""
        adapter = MongoDBAdapter()
        
        config = Config(
            dsn="mongodb://localhost:27017/testdb",
            max_open_conns=10,
            max_idle_conns=5
        )
        
        # Should not raise any exception
        adapter.validate_config(config)
    
    def test_validate_config_valid_components(self):
        """Test validating config with valid components."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            database="testdb",
            max_open_conns=10,
            max_idle_conns=5
        )
        
        # Should not raise any exception
        adapter.validate_config(config)
    
    def test_validate_config_missing_host(self):
        """Test validating config with missing host."""
        adapter = MongoDBAdapter()
        
        config = Config(
            database="testdb",
            max_open_conns=10,
            max_idle_conns=5
        )
        
        with pytest.raises(ValidationError, match="host is required"):
            adapter.validate_config(config)
    
    def test_validate_config_missing_database(self):
        """Test validating config with missing database."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            max_open_conns=10,
            max_idle_conns=5
        )
        
        with pytest.raises(ValidationError, match="database is required"):
            adapter.validate_config(config)
    
    def test_validate_config_invalid_max_open_conns(self):
        """Test validating config with invalid max_open_conns."""
        adapter = MongoDBAdapter()
        
        config = Config(
            dsn="mongodb://localhost:27017/testdb",
            max_open_conns=0,
            max_idle_conns=5
        )
        
        with pytest.raises(ValidationError, match="max_open_conns must be positive"):
            adapter.validate_config(config)
    
    def test_validate_config_negative_max_idle_conns(self):
        """Test validating config with negative max_idle_conns."""
        adapter = MongoDBAdapter()
        
        config = Config(
            dsn="mongodb://localhost:27017/testdb",
            max_open_conns=10,
            max_idle_conns=-1
        )
        
        with pytest.raises(ValidationError, match="max_idle_conns cannot be negative"):
            adapter.validate_config(config)
    
    def test_validate_config_max_idle_exceeds_max_open(self):
        """Test validating config where max_idle_conns exceeds max_open_conns."""
        adapter = MongoDBAdapter()
        
        config = Config(
            dsn="mongodb://localhost:27017/testdb",
            max_open_conns=5,
            max_idle_conns=10
        )
        
        with pytest.raises(ValidationError, match="max_idle_conns cannot exceed max_open_conns"):
            adapter.validate_config(config)
    
    def test_map_go_type(self):
        """Test mapping Go types to data types."""
        adapter = MongoDBAdapter()
        
        test_cases = [
            ("string", DataType.STRING),
            ("int", DataType.INTEGER),
            ("int64", DataType.INTEGER),
            ("float64", DataType.FLOAT),
            ("bool", DataType.BOOLEAN),
            ("time.Time", DataType.DATETIME),
            ("[]byte", DataType.BINARY),
            ("map[string]interface{}", DataType.JSON),
            ("[]interface{}", DataType.ARRAY),
        ]
        
        for go_type, expected_data_type in test_cases:
            result = adapter.map_go_type(go_type)
            assert result == expected_data_type
    
    def test_map_go_type_unsupported(self):
        """Test mapping unsupported Go type."""
        adapter = MongoDBAdapter()
        
        with pytest.raises(ValidationError, match="Unsupported Go type"):
            adapter.map_go_type("unsupported_type")
    
    def test_map_database_type(self):
        """Test mapping MongoDB BSON types to data types."""
        adapter = MongoDBAdapter()
        
        test_cases = [
            ("string", DataType.STRING),
            ("int", DataType.INTEGER),
            ("long", DataType.INTEGER),
            ("double", DataType.FLOAT),
            ("decimal", DataType.DECIMAL),
            ("bool", DataType.BOOLEAN),
            ("boolean", DataType.BOOLEAN),
            ("date", DataType.DATETIME),
            ("timestamp", DataType.DATETIME),
            ("bindata", DataType.BINARY),
            ("binary", DataType.BINARY),
            ("object", DataType.JSON),
            ("document", DataType.JSON),
            ("array", DataType.ARRAY),
            ("objectid", DataType.STRING),
        ]
        
        for db_type, expected_data_type in test_cases:
            result = adapter.map_database_type(db_type)
            assert result == expected_data_type
    
    def test_map_database_type_case_insensitive(self):
        """Test mapping database types is case insensitive."""
        adapter = MongoDBAdapter()
        
        # Test uppercase
        result = adapter.map_database_type("STRING")
        assert result == DataType.STRING
        
        # Test mixed case
        result = adapter.map_database_type("Boolean")
        assert result == DataType.BOOLEAN
    
    def test_map_database_type_unsupported(self):
        """Test mapping unsupported database type."""
        adapter = MongoDBAdapter()
        
        with pytest.raises(ValidationError, match="Unsupported MongoDB type"):
            adapter.map_database_type("unsupported_type")
    
    def test_build_connection_uri_from_dsn(self):
        """Test building connection URI when DSN is provided."""
        adapter = MongoDBAdapter()
        
        config = Config(dsn="mongodb://localhost:27017/testdb")
        uri = adapter._build_connection_uri(config)
        
        assert uri == "mongodb://localhost:27017/testdb"
    
    def test_build_connection_uri_from_components(self):
        """Test building connection URI from individual components."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            port=27017,
            database="testdb"
        )
        uri = adapter._build_connection_uri(config)
        
        assert uri == "mongodb://localhost:27017/testdb"
    
    def test_build_connection_uri_with_auth(self):
        """Test building connection URI with authentication."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            port=27017,
            database="testdb",
            username="user",
            password="pass"
        )
        uri = adapter._build_connection_uri(config)
        
        assert uri == "mongodb://user:pass@localhost:27017/testdb"
    
    def test_build_connection_uri_with_options(self):
        """Test building connection URI with options."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            port=27017,
            database="testdb",
            options={"authSource": "admin", "ssl": "true"}
        )
        uri = adapter._build_connection_uri(config)
        
        assert "mongodb://localhost:27017/testdb?" in uri
        assert "authSource=admin" in uri
        assert "ssl=true" in uri
    
    def test_build_connection_uri_default_port(self):
        """Test building connection URI with default port."""
        adapter = MongoDBAdapter()
        
        config = Config(
            host="localhost",
            database="testdb"
        )
        uri = adapter._build_connection_uri(config)
        
        # Should not include port when it's the default
        assert uri == "mongodb://localhost/testdb"
