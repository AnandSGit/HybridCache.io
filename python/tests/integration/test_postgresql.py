"""
Integration tests for PostgreSQL adapter.

These tests require a running PostgreSQL instance and test the complete
adapter functionality with real database operations.
"""

import os
import pytest
import pytest_asyncio
from testcontainers.postgres import PostgresContainer

from storage.adapters.postgresql import PostgreSQLAdapter
from storage.errors import StorageError
from storage.query import delete, equal, insert, select, update
from storage.types import Config, Command, Query


@pytest.fixture(scope="session")
def postgres_container():
    """Start PostgreSQL container for testing."""
    # Check if we should use external database
    if os.getenv("DATABASE_URL"):
        yield None
        return
    
    with PostgresContainer("postgres:15-alpine") as postgres:
        yield postgres


@pytest.fixture(scope="session")
async def postgres_config(postgres_container):
    """Create PostgreSQL configuration."""
    if os.getenv("DATABASE_URL"):
        # Use external database URL
        adapter = PostgreSQLAdapter()
        return adapter.parse_dsn(os.getenv("DATABASE_URL"))
    
    # Use container
    return Config(
        host=postgres_container.get_container_host_ip(),
        port=postgres_container.get_exposed_port(5432),
        database=postgres_container.dbname,
        username=postgres_container.username,
        password=postgres_container.password,
    )


@pytest.fixture
async def storage(postgres_config):
    """Create storage instance for testing."""
    adapter = PostgreSQLAdapter()
    storage = await adapter.connect(postgres_config)
    
    # Create test table
    create_sql = """
        CREATE TABLE IF NOT EXISTS test_users (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            email VARCHAR(255) UNIQUE NOT NULL,
            age INTEGER DEFAULT 0,
            active BOOLEAN DEFAULT true,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    """
    
    command = Command(sql=create_sql, parameters=[])
    await storage.execute(command)
    
    # Clean up any existing data
    cleanup_command = delete("test_users").build()
    await storage.execute(cleanup_command)
    
    yield storage
    
    # Cleanup
    await storage.close()


@pytest.mark.integration
class TestPostgreSQLAdapter:
    """Test PostgreSQL adapter functionality."""
    
    def test_adapter_info(self):
        """Test adapter information."""
        adapter = PostgreSQLAdapter()
        
        assert adapter.name() == "postgresql"
        assert adapter.version() == "1.0.0"
        assert adapter.database_type() == "postgresql"
        assert adapter.supports_transactions() is True
        assert adapter.supports_joins() is True
        assert adapter.supports_batch() is True
        assert adapter.supports_schema() is True
    
    async def test_connection_and_ping(self, storage):
        """Test connection and ping functionality."""
        # Test ping
        await storage.ping()
        
        # Test storage info
        info = storage.info()
        assert info.name == "PostgreSQL"
        assert info.database_type.value == "postgresql"
        assert "transactions" in info.features
        assert "async" in info.features
    
    async def test_health_check(self, storage):
        """Test health check functionality."""
        health = await storage.health()
        assert health.status.value == "healthy"
        assert "PostgreSQL connection is healthy" in health.message
        assert "pool_size" in health.details
    
    async def test_basic_crud_operations(self, storage):
        """Test basic CRUD operations."""
        # Insert
        command = (insert("test_users")
                  .set("name", "John Doe")
                  .set("email", "john@example.com")
                  .set("age", 30)
                  .build())
        
        result = await storage.execute(command)
        assert result.rows_affected == 1
        
        # Select
        query = (select("id", "name", "email", "age")
                .from_("test_users")
                .where(equal("email", "john@example.com"))
                .build())
        
        user_row = await storage.query_one(query)
        assert user_row is not None
        assert user_row.get("name") == "John Doe"
        assert user_row.get("email") == "john@example.com"
        assert user_row.get("age") == 30
        
        user_id = user_row.get("id")
        
        # Update
        command = (update("test_users")
                  .set("age", 31)
                  .where(equal("id", user_id))
                  .build())
        
        result = await storage.execute(command)
        assert result.rows_affected == 1
        
        # Verify update
        query = (select("age")
                .from_("test_users")
                .where(equal("id", user_id))
                .build())
        
        updated_row = await storage.query_one(query)
        assert updated_row.get("age") == 31
        
        # Delete
        command = (delete("test_users")
                  .where(equal("id", user_id))
                  .build())
        
        result = await storage.execute(command)
        assert result.rows_affected == 1
        
        # Verify deletion
        query = (select("COUNT(*)")
                .from_("test_users")
                .where(equal("id", user_id))
                .build())
        
        count_result = await storage.query(query)
        count_row = await count_result.fetchone()
        assert count_row.get("count") == 0
        await count_result.close()
    
    async def test_query_iteration(self, storage):
        """Test query result iteration."""
        # Insert test data
        users = [
            ("Alice", "alice@example.com", 25),
            ("Bob", "bob@example.com", 30),
            ("Carol", "carol@example.com", 35),
        ]
        
        for name, email, age in users:
            command = (insert("test_users")
                      .set("name", name)
                      .set("email", email)
                      .set("age", age)
                      .build())
            await storage.execute(command)
        
        # Query all users
        query = (select("name", "email", "age")
                .from_("test_users")
                .order_by("name")
                .build())
        
        result = await storage.query(query)
        
        # Test async iteration
        names = []
        async for row in result:
            names.append(row.get("name"))
        
        assert names == ["Alice", "Bob", "Carol"]
        await result.close()
        
        # Test fetchall
        result = await storage.query(query)
        all_rows = await result.fetchall()
        assert len(all_rows) == 3
        assert all_rows[0].get("name") == "Alice"
        await result.close()
        
        # Test fetchmany
        result = await storage.query(query)
        some_rows = await result.fetchmany(2)
        assert len(some_rows) == 2
        assert some_rows[0].get("name") == "Alice"
        assert some_rows[1].get("name") == "Bob"
        await result.close()
    
    async def test_transaction_commit(self, storage):
        """Test transaction commit functionality."""
        async with await storage.begin_tx() as tx:
            # Insert user in transaction
            command = (insert("test_users")
                      .set("name", "Transaction User")
                      .set("email", "tx@example.com")
                      .set("age", 25)
                      .build())
            
            result = await tx.execute(command)
            assert result.rows_affected == 1
            
            # Query within transaction
            query = (select("name")
                    .from_("test_users")
                    .where(equal("email", "tx@example.com"))
                    .build())
            
            user_row = await tx.query_one(query)
            assert user_row.get("name") == "Transaction User"
        
        # Verify commit - user should exist outside transaction
        query = (select("name")
                .from_("test_users")
                .where(equal("email", "tx@example.com"))
                .build())
        
        user_row = await storage.query_one(query)
        assert user_row.get("name") == "Transaction User"
    
    async def test_transaction_rollback(self, storage):
        """Test transaction rollback functionality."""
        try:
            async with await storage.begin_tx() as tx:
                # Insert user in transaction
                command = (insert("test_users")
                          .set("name", "Rollback User")
                          .set("email", "rollback@example.com")
                          .set("age", 25)
                          .build())
                
                result = await tx.execute(command)
                assert result.rows_affected == 1
                
                # Force rollback by raising exception
                raise ValueError("Force rollback")
        
        except ValueError:
            pass  # Expected
        
        # Verify rollback - user should not exist
        query = (select("COUNT(*)")
                .from_("test_users")
                .where(equal("email", "rollback@example.com"))
                .build())
        
        result = await storage.query(query)
        count_row = await result.fetchone()
        assert count_row.get("count") == 0
        await result.close()
    
    async def test_batch_operations(self, storage):
        """Test batch operations."""
        from storage.types import Operation, OperationType
        
        # Create batch operations
        operations = [
            Operation(
                operation_type=OperationType.COMMAND,
                command=insert("test_users").set("name", "Batch1").set("email", "batch1@example.com").build()
            ),
            Operation(
                operation_type=OperationType.COMMAND,
                command=insert("test_users").set("name", "Batch2").set("email", "batch2@example.com").build()
            ),
            Operation(
                operation_type=OperationType.QUERY,
                query=select("COUNT(*)").from_("test_users").build()
            ),
        ]
        
        results = await storage.batch(operations)
        
        assert len(results) == 3
        assert results[0].error is None
        assert results[1].error is None
        assert results[2].error is None
        
        # Verify batch insert worked
        query = (select("COUNT(*)")
                .from_("test_users")
                .where(equal("name", "Batch1"))
                .build())
        
        result = await storage.query(query)
        count_row = await result.fetchone()
        assert count_row.get("count") == 1
        await result.close()
    
    async def test_error_handling(self, storage):
        """Test error handling."""
        # Test invalid SQL
        with pytest.raises(StorageError):
            query = Query(sql="INVALID SQL", parameters=[])
            await storage.query(query)
        
        # Test constraint violation
        command = (insert("test_users")
                  .set("name", "Duplicate")
                  .set("email", "duplicate@example.com")
                  .build())
        
        await storage.execute(command)
        
        # Try to insert duplicate email
        with pytest.raises(StorageError):
            await storage.execute(command)
    
    async def test_table_operations(self, storage):
        """Test table operations."""
        # List tables
        tables = await storage.list_tables()
        assert "test_users" in tables
        
        # Drop table
        await storage.drop_table("test_users")
        
        # Verify table was dropped
        tables = await storage.list_tables()
        assert "test_users" not in tables
