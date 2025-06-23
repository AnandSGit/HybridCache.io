#!/usr/bin/env python3
"""
MongoDB Adapter Example

This example demonstrates the MongoDB adapter functionality including:
- Connection management
- Basic CRUD operations
- Transaction support
- Batch operations
- Error handling
"""

import asyncio
import logging
from datetime import datetime, timedelta
from typing import Dict, Any

from storage.adapters.mongodb import MongoDBAdapter
from storage.types import Config, Command, Query, CommandType, TxOptions, IsolationLevel, Operation, OperationType

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


async def main():
    """Main example function."""
    print("🍃 MongoDB Adapter Example (Python)")
    print("====================================")
    
    # Create MongoDB adapter
    adapter = MongoDBAdapter()
    
    # Configure connection
    config = Config(
        host="localhost",
        port=27017,
        database="hybridcache_example_py",
        username="",
        password="",
        max_open_conns=25,
        max_idle_conns=5,
        conn_max_lifetime=timedelta(hours=1),
        conn_max_idle_time=timedelta(minutes=30),
        connect_timeout=timedelta(seconds=10),
        query_timeout=timedelta(seconds=30),
    )
    
    # Alternative: Use DSN
    # config = adapter.parse_dsn("mongodb://localhost:27017/hybridcache_example_py")
    
    try:
        # Connect to MongoDB
        storage = await adapter.connect(config)
        print("✅ Connected to MongoDB successfully")
        
        # Test connection
        await storage.ping()
        print("✅ MongoDB ping successful")
        
        # Get storage info
        info = storage.info()
        print(f"📊 Storage Info: {info.name} v{info.version} ({info.database_type.value})")
        print(f"🔧 Features: {info.features}")
        
        # Get health status
        health = await storage.health()
        print(f"💚 Health Status: {health.status.value} - {health.message}")
        
        # Example 1: Basic Insert Operation
        print("\n📝 Example 1: Insert Document")
        insert_command = Command(
            sql="INSERT INTO users",  # Simplified - in practice would be parsed
            parameters=["name", "John Doe", "email", "john@example.com", "age", 30],
            command_type=CommandType.INSERT
        )
        
        try:
            result = await storage.execute(insert_command)
            print(f"✅ Inserted document, rows affected: {result.rows_affected}, ID: {result.last_insert_id}")
        except Exception as e:
            print(f"❌ Insert failed: {e}")
        
        # Example 2: Query Documents
        print("\n🔍 Example 2: Query Documents")
        query = Query(
            sql="SELECT FROM users",
            parameters=["name", "John Doe"]
        )
        
        try:
            query_result = await storage.query(query)
            print("✅ Query executed successfully")
            
            # Fetch all results
            rows = await query_result.fetchall()
            print(f"📄 Found {len(rows)} documents")
            
            # Process results
            for i, row in enumerate(rows):
                print(f"  Document {i+1}: {row.to_dict()}")
            
            await query_result.close()
        except Exception as e:
            print(f"❌ Query failed: {e}")
        
        # Example 3: Query Single Document
        print("\n📄 Example 3: Query Single Document")
        single_query = Query(
            sql="SELECT FROM users",
            parameters=["email", "john@example.com"]
        )
        
        try:
            row = await storage.query_one(single_query)
            print("✅ Single document query successful")
            print(f"📄 Document: {row.to_dict()}")
        except Exception as e:
            print(f"❌ QueryOne failed: {e}")
        
        # Example 4: Update Document
        print("\n✏️ Example 4: Update Document")
        update_command = Command(
            sql="UPDATE users",
            parameters=["email", "john@example.com", "age", 31],  # filter, then update
            command_type=CommandType.UPDATE
        )
        
        try:
            update_result = await storage.execute(update_command)
            print(f"✅ Updated document, rows affected: {update_result.rows_affected}")
        except Exception as e:
            print(f"❌ Update failed: {e}")
        
        # Example 5: Transaction Operations
        print("\n🔄 Example 5: Transaction Operations")
        try:
            tx_options = TxOptions(
                isolation=IsolationLevel.READ_COMMITTED,
                read_only=False
            )
            
            tx = await storage.begin_tx(tx_options)
            
            try:
                # Insert within transaction
                tx_insert_command = Command(
                    sql="INSERT INTO users",
                    parameters=["name", "Jane Smith", "email", "jane@example.com", "age", 28],
                    command_type=CommandType.INSERT
                )
                
                tx_result = await tx.execute(tx_insert_command)
                print(f"✅ Transaction insert successful, rows affected: {tx_result.rows_affected}")
                
                # Commit transaction
                await tx.commit()
                print("✅ Transaction committed successfully")
                
            except Exception as e:
                print(f"❌ Transaction operation failed: {e}")
                await tx.rollback()
                print("🔄 Transaction rolled back")
                
        except Exception as e:
            print(f"❌ Failed to begin transaction: {e}")
        
        # Example 6: Batch Operations
        print("\n📦 Example 6: Batch Operations")
        operations = [
            Operation(
                operation_type=OperationType.INSERT,
                query=Query(
                    sql="INSERT INTO users",
                    parameters=["name", "Alice Johnson", "email", "alice@example.com", "age", 25]
                )
            ),
            Operation(
                operation_type=OperationType.INSERT,
                query=Query(
                    sql="INSERT INTO users",
                    parameters=["name", "Bob Wilson", "email", "bob@example.com", "age", 35]
                )
            ),
            Operation(
                operation_type=OperationType.UPDATE,
                query=Query(
                    sql="UPDATE users",
                    parameters=["email", "alice@example.com", "age", 26]
                )
            ),
        ]
        
        try:
            batch_results = await storage.batch(operations)
            print(f"✅ Batch operations completed, {len(batch_results)} operations processed")
            
            for i, result in enumerate(batch_results):
                if result.success:
                    print(f"  Operation {i+1}: ✅ Success")
                else:
                    print(f"  Operation {i+1}: ❌ Failed - {result.error}")
                    
        except Exception as e:
            print(f"❌ Batch operations failed: {e}")
        
        # Example 7: Delete Document
        print("\n🗑️ Example 7: Delete Document")
        delete_command = Command(
            sql="DELETE FROM users",
            parameters=["email", "bob@example.com"],
            command_type=CommandType.DELETE
        )
        
        try:
            delete_result = await storage.execute(delete_command)
            print(f"✅ Deleted document, rows affected: {delete_result.rows_affected}")
        except Exception as e:
            print(f"❌ Delete failed: {e}")
        
        # Example 8: Async Iteration
        print("\n🔄 Example 8: Async Iteration")
        try:
            async_query = Query(
                sql="SELECT FROM users",
                parameters=[]
            )
            
            result = await storage.query(async_query)
            print("✅ Starting async iteration")
            
            count = 0
            async for row in result:
                count += 1
                print(f"  Row {count}: {row.get('name', 'Unknown')} ({row.get('email', 'No email')})")
                
                # Limit output for demo
                if count >= 5:
                    break
            
            await result.close()
            print(f"✅ Async iteration completed, processed {count} rows")
            
        except Exception as e:
            print(f"❌ Async iteration failed: {e}")
        
        print("\n🎉 MongoDB adapter example completed successfully!")
        
    except Exception as e:
        logger.error(f"Example failed: {e}")
        raise
    
    finally:
        # Clean up
        try:
            await storage.close()
            print("✅ MongoDB connection closed")
        except:
            pass
    
    print("\nNote: This example uses a simplified query format.")
    print("In a production implementation, you would:")
    print("- Use proper MongoDB query builders")
    print("- Implement collection name parsing")
    print("- Add comprehensive error handling")
    print("- Use proper BSON document structures")
    print("- Implement aggregation pipeline support")
    print("- Add proper data validation and sanitization")


async def demonstrate_advanced_features():
    """Demonstrate advanced MongoDB features."""
    print("\n🚀 Advanced MongoDB Features Demo")
    print("==================================")
    
    adapter = MongoDBAdapter()
    config = Config(
        host="localhost",
        port=27017,
        database="hybridcache_advanced_py",
    )
    
    try:
        storage = await adapter.connect(config)
        
        # Example: Complex document structure
        print("\n📊 Complex Document Operations")
        
        # Insert complex document
        complex_doc_command = Command(
            sql="INSERT INTO products",
            parameters=[
                "name", "Laptop Pro",
                "price", 1299.99,
                "specs", {
                    "cpu": "Intel i7",
                    "ram": "16GB",
                    "storage": "512GB SSD"
                },
                "tags", ["electronics", "computers", "laptops"],
                "created_at", datetime.now(),
                "in_stock", True
            ],
            command_type=CommandType.INSERT
        )
        
        try:
            result = await storage.execute(complex_doc_command)
            print(f"✅ Complex document inserted, ID: {result.last_insert_id}")
        except Exception as e:
            print(f"❌ Complex document insert failed: {e}")
        
        # Example: Aggregation-like operations (simplified)
        print("\n📈 Aggregation Operations")
        
        # In a real implementation, this would use MongoDB aggregation pipeline
        agg_query = Query(
            sql="AGGREGATE products",
            parameters=["group_by", "category", "sum", "price"]
        )
        
        try:
            agg_result = await storage.query(agg_query)
            print("✅ Aggregation query executed (simplified)")
            await agg_result.close()
        except Exception as e:
            print(f"❌ Aggregation failed: {e}")
        
        await storage.close()
        
    except Exception as e:
        print(f"❌ Advanced features demo failed: {e}")


if __name__ == "__main__":
    # Run the main example
    asyncio.run(main())
    
    # Uncomment to run advanced features demo
    # asyncio.run(demonstrate_advanced_features())
