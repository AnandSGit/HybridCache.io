#!/usr/bin/env python3
"""
Async performance example for the Database Agnostic Storage Library.

This example demonstrates:
- High-performance async operations
- Connection pooling efficiency
- Concurrent query execution
- Batch operations
- Performance monitoring
- Memory usage optimization

Prerequisites:
- PostgreSQL running on localhost:5432
- Database 'testdb' with user 'testuser' and password 'testpass'
- Or run: docker-compose up -d postgres
"""

import asyncio
import logging
import time
from dataclasses import dataclass
from typing import List

from storage.adapters.postgresql import PostgreSQLAdapter
from storage.errors import StorageError
from storage.query import delete, equal, insert, select
from storage.types import Config, Command, Operation, OperationType

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@dataclass
class PerformanceMetrics:
    """Performance metrics tracking."""
    operation: str
    total_operations: int
    total_time: float
    avg_latency: float
    ops_per_second: float
    min_time: float
    max_time: float


async def main():
    """Main performance testing function."""
    # Create adapter and configuration
    adapter = PostgreSQLAdapter()
    config = Config(
        host="localhost",
        port=5432,
        database="testdb",
        username="testuser",
        password="testpass",
        max_open_conns=50,  # Increased for performance testing
        max_idle_conns=10,
    )
    
    try:
        # Connect to database
        logger.info("Connecting to PostgreSQL for performance testing...")
        storage = await adapter.connect(config)
        
        # Test connection
        await storage.ping()
        logger.info("✅ Connected successfully!")
        
        # Setup test environment
        await setup_performance_test(storage)
        
        # Run performance tests
        await test_single_query_performance(storage)
        await test_concurrent_query_performance(storage)
        await test_batch_insert_performance(storage)
        await test_transaction_performance(storage)
        await test_connection_pool_performance(storage)
        
        # Cleanup
        await cleanup_performance_test(storage)
        
    except StorageError as e:
        logger.error(f"Storage error: {e}")
    except Exception as e:
        logger.error(f"Unexpected error: {e}")
    finally:
        # Clean up
        if 'storage' in locals():
            await storage.close()
            logger.info("Connection closed")


async def setup_performance_test(storage):
    """Setup performance test environment."""
    logger.info("Setting up performance test environment...")
    
    # Drop and recreate test table
    try:
        await storage.drop_table("perf_test")
    except StorageError:
        pass  # Table might not exist
    
    create_sql = """
        CREATE TABLE perf_test (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            value INTEGER NOT NULL,
            data JSONB,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_name (name),
            INDEX idx_value (value)
        )
    """
    
    command = Command(sql=create_sql, parameters=[])
    await storage.execute(command)
    logger.info("✅ Performance test table created")


async def test_single_query_performance(storage):
    """Test single query performance."""
    logger.info("Testing single query performance...")
    
    # Insert test data
    logger.info("Inserting test data...")
    for i in range(1000):
        command = (insert("perf_test")
                  .set("name", f"user_{i}")
                  .set("value", i)
                  .set("data", {"index": i, "category": f"cat_{i % 10}"})
                  .build())
        await storage.execute(command)
    
    # Test simple SELECT queries
    times = []
    num_queries = 100
    
    logger.info(f"Running {num_queries} simple SELECT queries...")
    start_time = time.time()
    
    for i in range(num_queries):
        query_start = time.time()
        
        query = (select("id", "name", "value")
                .from_("perf_test")
                .where(equal("value", i % 1000))
                .build())
        
        result = await storage.query(query)
        row = await result.fetchone()
        await result.close()
        
        query_time = time.time() - query_start
        times.append(query_time)
    
    total_time = time.time() - start_time
    
    metrics = PerformanceMetrics(
        operation="Simple SELECT",
        total_operations=num_queries,
        total_time=total_time,
        avg_latency=sum(times) / len(times),
        ops_per_second=num_queries / total_time,
        min_time=min(times),
        max_time=max(times),
    )
    
    log_performance_metrics(metrics)


async def test_concurrent_query_performance(storage):
    """Test concurrent query performance."""
    logger.info("Testing concurrent query performance...")
    
    async def run_query(query_id: int) -> float:
        """Run a single query and return execution time."""
        start_time = time.time()
        
        query = (select("id", "name", "value", "data")
                .from_("perf_test")
                .where(equal("value", query_id % 1000))
                .build())
        
        result = await storage.query(query)
        rows = await result.fetchall()
        await result.close()
        
        return time.time() - start_time
    
    # Run concurrent queries
    num_concurrent = 50
    num_queries_per_task = 20
    
    logger.info(f"Running {num_concurrent} concurrent tasks with {num_queries_per_task} queries each...")
    start_time = time.time()
    
    # Create tasks for concurrent execution
    tasks = []
    for task_id in range(num_concurrent):
        task_queries = []
        for query_id in range(num_queries_per_task):
            task_queries.append(run_query(task_id * num_queries_per_task + query_id))
        tasks.extend(task_queries)
    
    # Execute all queries concurrently
    times = await asyncio.gather(*tasks)
    total_time = time.time() - start_time
    
    total_operations = num_concurrent * num_queries_per_task
    
    metrics = PerformanceMetrics(
        operation="Concurrent SELECT",
        total_operations=total_operations,
        total_time=total_time,
        avg_latency=sum(times) / len(times),
        ops_per_second=total_operations / total_time,
        min_time=min(times),
        max_time=max(times),
    )
    
    log_performance_metrics(metrics)


async def test_batch_insert_performance(storage):
    """Test batch insert performance."""
    logger.info("Testing batch insert performance...")
    
    # Clear existing data
    command = delete("perf_test").build()
    await storage.execute(command)
    
    # Create batch operations
    batch_size = 100
    num_batches = 10
    
    logger.info(f"Running {num_batches} batches of {batch_size} inserts each...")
    times = []
    
    for batch_num in range(num_batches):
        operations = []
        
        for i in range(batch_size):
            record_id = batch_num * batch_size + i
            command = (insert("perf_test")
                      .set("name", f"batch_user_{record_id}")
                      .set("value", record_id)
                      .set("data", {"batch": batch_num, "index": i})
                      .build())
            
            operations.append(Operation(
                operation_type=OperationType.COMMAND,
                command=command
            ))
        
        # Execute batch
        start_time = time.time()
        results = await storage.batch(operations)
        batch_time = time.time() - start_time
        times.append(batch_time)
        
        # Verify all operations succeeded
        failed_ops = [r for r in results if r.error is not None]
        if failed_ops:
            logger.warning(f"Batch {batch_num}: {len(failed_ops)} operations failed")
    
    total_operations = num_batches * batch_size
    total_time = sum(times)
    
    metrics = PerformanceMetrics(
        operation="Batch INSERT",
        total_operations=total_operations,
        total_time=total_time,
        avg_latency=total_time / num_batches,  # Average per batch
        ops_per_second=total_operations / total_time,
        min_time=min(times),
        max_time=max(times),
    )
    
    log_performance_metrics(metrics)


async def test_transaction_performance(storage):
    """Test transaction performance."""
    logger.info("Testing transaction performance...")
    
    num_transactions = 50
    operations_per_tx = 5
    
    logger.info(f"Running {num_transactions} transactions with {operations_per_tx} operations each...")
    times = []
    
    for tx_num in range(num_transactions):
        start_time = time.time()
        
        try:
            async with await storage.begin_tx() as tx:
                for op_num in range(operations_per_tx):
                    record_id = tx_num * operations_per_tx + op_num + 10000
                    
                    # Insert
                    command = (insert("perf_test")
                              .set("name", f"tx_user_{record_id}")
                              .set("value", record_id)
                              .build())
                    await tx.execute(command)
                    
                    # Query
                    query = (select("id")
                            .from_("perf_test")
                            .where(equal("value", record_id))
                            .build())
                    await tx.query_one(query)
            
            tx_time = time.time() - start_time
            times.append(tx_time)
            
        except Exception as e:
            logger.error(f"Transaction {tx_num} failed: {e}")
    
    total_operations = len(times) * operations_per_tx
    total_time = sum(times)
    
    metrics = PerformanceMetrics(
        operation="Transaction",
        total_operations=total_operations,
        total_time=total_time,
        avg_latency=total_time / len(times),  # Average per transaction
        ops_per_second=total_operations / total_time,
        min_time=min(times),
        max_time=max(times),
    )
    
    log_performance_metrics(metrics)


async def test_connection_pool_performance(storage):
    """Test connection pool performance."""
    logger.info("Testing connection pool performance...")
    
    async def pool_query_task(task_id: int) -> float:
        """Execute queries to test pool efficiency."""
        start_time = time.time()
        
        for i in range(10):  # 10 queries per task
            query = (select("COUNT(*)")
                    .from_("perf_test")
                    .where(equal("value", (task_id * 10 + i) % 1000))
                    .build())
            
            result = await storage.query(query)
            await result.fetchone()
            await result.close()
        
        return time.time() - start_time
    
    # Test pool under load
    num_concurrent_tasks = 100
    
    logger.info(f"Running {num_concurrent_tasks} concurrent tasks to test connection pool...")
    start_time = time.time()
    
    # Create and run concurrent tasks
    tasks = [pool_query_task(i) for i in range(num_concurrent_tasks)]
    times = await asyncio.gather(*tasks)
    
    total_time = time.time() - start_time
    total_operations = num_concurrent_tasks * 10
    
    metrics = PerformanceMetrics(
        operation="Connection Pool",
        total_operations=total_operations,
        total_time=total_time,
        avg_latency=sum(times) / len(times),
        ops_per_second=total_operations / total_time,
        min_time=min(times),
        max_time=max(times),
    )
    
    log_performance_metrics(metrics)
    
    # Check pool health
    health = await storage.health()
    logger.info(f"Pool status: {health.details.get('pool_size', 'unknown')} total, "
               f"{health.details.get('pool_idle', 'unknown')} idle")


async def cleanup_performance_test(storage):
    """Cleanup performance test environment."""
    logger.info("Cleaning up performance test environment...")
    
    try:
        await storage.drop_table("perf_test")
        logger.info("✅ Performance test table dropped")
    except StorageError as e:
        logger.warning(f"Cleanup warning: {e}")


def log_performance_metrics(metrics: PerformanceMetrics):
    """Log performance metrics in a formatted way."""
    logger.info(f"\n{'='*60}")
    logger.info(f"Performance Results: {metrics.operation}")
    logger.info(f"{'='*60}")
    logger.info(f"Total Operations: {metrics.total_operations:,}")
    logger.info(f"Total Time: {metrics.total_time:.3f}s")
    logger.info(f"Average Latency: {metrics.avg_latency*1000:.2f}ms")
    logger.info(f"Operations/Second: {metrics.ops_per_second:,.1f}")
    logger.info(f"Min Time: {metrics.min_time*1000:.2f}ms")
    logger.info(f"Max Time: {metrics.max_time*1000:.2f}ms")
    
    # Performance assessment
    if metrics.operation == "Simple SELECT":
        target_latency = 10.0  # 10ms target
        target_ops = 5000      # 5000 ops/sec target
    elif metrics.operation == "Concurrent SELECT":
        target_latency = 15.0  # 15ms target
        target_ops = 3000      # 3000 ops/sec target
    elif metrics.operation == "Batch INSERT":
        target_latency = 100.0 # 100ms per batch target
        target_ops = 500       # 500 ops/sec target
    elif metrics.operation == "Transaction":
        target_latency = 50.0  # 50ms per transaction target
        target_ops = 1000      # 1000 ops/sec target
    else:
        target_latency = 20.0
        target_ops = 2000
    
    latency_status = "✅ PASS" if metrics.avg_latency*1000 <= target_latency else "❌ FAIL"
    ops_status = "✅ PASS" if metrics.ops_per_second >= target_ops else "❌ FAIL"
    
    logger.info(f"Latency Target: {target_latency}ms - {latency_status}")
    logger.info(f"Throughput Target: {target_ops:,} ops/sec - {ops_status}")
    logger.info(f"{'='*60}\n")


if __name__ == "__main__":
    asyncio.run(main())
