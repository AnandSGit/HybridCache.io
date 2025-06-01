#!/usr/bin/env python3
"""
Basic usage example for the Database Agnostic Storage Library.

This example demonstrates:
- Connecting to PostgreSQL
- Creating tables
- Basic CRUD operations
- Query building
- Error handling
- Connection management

Prerequisites:
- PostgreSQL running on localhost:5432
- Database 'testdb' with user 'testuser' and password 'testpass'
- Or run: docker-compose up -d postgres
"""

import asyncio
import logging
from dataclasses import dataclass
from typing import List, Optional

from storage.adapters.postgresql import PostgreSQLAdapter
from storage.errors import StorageError
from storage.query import delete, equal, insert, select, update
from storage.types import Config, Query, Command

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@dataclass
class User:
    """User data model."""
    id: Optional[int] = None
    name: str = ""
    email: str = ""
    age: int = 0
    active: bool = True


async def main():
    """Main example function."""
    # Create adapter and configuration
    adapter = PostgreSQLAdapter()
    config = Config(
        host="localhost",
        port=5432,
        database="testdb",
        username="testuser",
        password="testpass",
        max_open_conns=10,
        max_idle_conns=2,
    )
    
    try:
        # Connect to database
        logger.info("Connecting to PostgreSQL...")
        storage = await adapter.connect(config)
        
        # Test connection
        await storage.ping()
        logger.info("✅ Connected successfully!")
        
        # Get storage info
        info = storage.info()
        logger.info(f"Database: {info.name} v{info.version}")
        logger.info(f"Features: {', '.join(info.features)}")
        
        # Create users table
        await create_users_table(storage)
        
        # Insert sample users
        await insert_sample_users(storage)
        
        # Query users
        await query_users(storage)
        
        # Update user
        await update_user(storage)
        
        # Delete user
        await delete_user(storage)
        
        # Demonstrate transaction
        await transaction_example(storage)
        
        # Check health
        health = await storage.health()
        logger.info(f"Health: {health.status.value} - {health.message}")
        
    except StorageError as e:
        logger.error(f"Storage error: {e}")
        if e.retryable:
            logger.info("This error is retryable")
        if e.temporary:
            logger.info("This error is temporary")
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
    finally:
        # Clean up
        if 'storage' in locals():
            await storage.close()
            logger.info("Connection closed")


async def create_users_table(storage):
    """Create the users table."""
    logger.info("Creating users table...")
    
    # Drop table if exists
    try:
        await storage.drop_table("users")
        logger.info("Dropped existing users table")
    except StorageError:
        pass  # Table might not exist
    
    # Create table using raw SQL
    create_sql = """
        CREATE TABLE users (
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
    logger.info("✅ Users table created")


async def insert_sample_users(storage):
    """Insert sample users."""
    logger.info("Inserting sample users...")
    
    users = [
        User(name="Alice Johnson", email="alice@example.com", age=28),
        User(name="Bob Smith", email="bob@example.com", age=35),
        User(name="Carol Davis", email="carol@example.com", age=42),
        User(name="David Wilson", email="david@example.com", age=29),
    ]
    
    for user in users:
        # Use query builder to create INSERT command
        command = (insert("users")
                  .set("name", user.name)
                  .set("email", user.email)
                  .set("age", user.age)
                  .set("active", user.active)
                  .build())
        
        result = await storage.execute(command)
        logger.info(f"Inserted user: {user.name} (affected: {result.rows_affected})")
    
    logger.info("✅ Sample users inserted")


async def query_users(storage):
    """Query users with various examples."""
    logger.info("Querying users...")
    
    # Query all users
    query = select("id", "name", "email", "age", "active").from_("users").build()
    result = await storage.query(query)
    
    logger.info("All users:")
    async for row in result:
        logger.info(f"  {row.get('id')}: {row.get('name')} ({row.get('email')}) - Age: {row.get('age')}")
    
    await result.close()
    
    # Query with WHERE condition
    query = (select("name", "email")
            .from_("users")
            .where(equal("active", True))
            .order_by("name")
            .build())
    
    result = await storage.query(query)
    logger.info("Active users:")
    async for row in result:
        logger.info(f"  {row.get('name')} - {row.get('email')}")
    
    await result.close()
    
    # Query single user
    query = (select("*")
            .from_("users")
            .where(equal("email", "alice@example.com"))
            .build())
    
    user_row = await storage.query_one(query)
    if user_row:
        logger.info(f"Found user: {user_row.get('name')} (ID: {user_row.get('id')})")
    
    # Count users
    query = select().select_count().from_("users").build()
    result = await storage.query(query)
    count_row = await result.fetchone()
    if count_row:
        logger.info(f"Total users: {count_row.get('count')}")
    await result.close()
    
    logger.info("✅ User queries completed")


async def update_user(storage):
    """Update a user."""
    logger.info("Updating user...")
    
    # Update user's age
    command = (update("users")
              .set("age", 36)
              .set("active", True)
              .where(equal("email", "bob@example.com"))
              .build())
    
    result = await storage.execute(command)
    logger.info(f"Updated user: Bob (affected: {result.rows_affected})")
    
    # Verify update
    query = (select("name", "age")
            .from_("users")
            .where(equal("email", "bob@example.com"))
            .build())
    
    user_row = await storage.query_one(query)
    if user_row:
        logger.info(f"Verified: {user_row.get('name')} is now {user_row.get('age')} years old")
    
    logger.info("✅ User update completed")


async def delete_user(storage):
    """Delete a user."""
    logger.info("Deleting user...")
    
    # Delete user
    command = (delete("users")
              .where(equal("email", "david@example.com"))
              .build())
    
    result = await storage.execute(command)
    logger.info(f"Deleted user: David (affected: {result.rows_affected})")
    
    # Verify deletion
    query = (select("COUNT(*)")
            .from_("users")
            .where(equal("email", "david@example.com"))
            .build())
    
    result_query = await storage.query(query)
    count_row = await result_query.fetchone()
    if count_row and count_row.get('count') == 0:
        logger.info("Verified: User David was deleted")
    await result_query.close()
    
    logger.info("✅ User deletion completed")


async def transaction_example(storage):
    """Demonstrate transaction usage."""
    logger.info("Demonstrating transaction...")
    
    try:
        # Begin transaction
        async with await storage.begin_tx() as tx:
            logger.info("Transaction started")
            
            # Insert new user
            command = (insert("users")
                      .set("name", "Transaction User")
                      .set("email", "tx@example.com")
                      .set("age", 25)
                      .build())
            
            result = await tx.execute(command)
            logger.info(f"Inserted user in transaction (affected: {result.rows_affected})")
            
            # Update another user
            command = (update("users")
                      .set("age", 43)
                      .where(equal("email", "carol@example.com"))
                      .build())
            
            result = await tx.execute(command)
            logger.info(f"Updated user in transaction (affected: {result.rows_affected})")
            
            # Query within transaction
            query = select("COUNT(*)").from_("users").build()
            result_query = await tx.query(query)
            count_row = await result_query.fetchone()
            if count_row:
                logger.info(f"Users in transaction: {count_row.get('count')}")
            await result_query.close()
            
            # Transaction will auto-commit when exiting context
            logger.info("Transaction committed")
    
    except Exception as e:
        logger.error(f"Transaction failed: {e}")
        # Transaction will auto-rollback on exception
    
    logger.info("✅ Transaction example completed")


if __name__ == "__main__":
    asyncio.run(main())
