// Performance example for Database Agnostic Storage Library
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/HybridCache.io/storage/pkg/adapters/postgres"
	"github.com/HybridCache.io/storage/pkg/query"
	"github.com/HybridCache.io/storage/pkg/storage"
)

func main() {
	ctx := context.Background()

	// Initialize PostgreSQL adapter with optimized settings
	adapter := postgres.NewAdapter()
	config := storage.Config{
		Host:            "localhost",
		Port:            5432,
		Database:        "testdb",
		Username:        "testuser",
		Password:        "testpass",
		MaxOpenConns:    50,  // Higher for performance testing
		MaxIdleConns:    25,  // Higher for performance testing
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		SSLMode:         "disable",
	}

	store, err := adapter.Connect(ctx, config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer store.Close()

	fmt.Println("=== Database Agnostic Storage Library - Performance Testing ===")

	// 1. Single Query Performance
	fmt.Println("\n1. Single Query Performance:")
	singleQueryPerformance(ctx, store)

	// 2. Concurrent Query Performance
	fmt.Println("\n2. Concurrent Query Performance:")
	concurrentQueryPerformance(ctx, store)

	// 3. Batch Operation Performance
	fmt.Println("\n3. Batch Operation Performance:")
	batchOperationPerformance(ctx, store)

	// 4. Transaction Performance
	fmt.Println("\n4. Transaction Performance:")
	transactionPerformance(ctx, store)

	// 5. Connection Pool Performance
	fmt.Println("\n5. Connection Pool Performance:")
	connectionPoolPerformance(ctx, store)
}

// singleQueryPerformance measures single query execution time
func singleQueryPerformance(ctx context.Context, store storage.Storage) {
	iterations := 1000
	
	query := query.NewBuilder().
		Select("id", "name", "email").
		From("users").
		Where(query.Equal("active", true)).
		Limit(10)

	queryObj, err := query.Build()
	if err != nil {
		log.Printf("Failed to build query: %v", err)
		return
	}

	start := time.Now()
	
	for i := 0; i < iterations; i++ {
		result, err := store.Query(ctx, queryObj)
		if err != nil {
			log.Printf("Query failed: %v", err)
			continue
		}
		
		// Consume results
		for result.Next() {
			var id int
			var name, email string
			result.Scan(&id, &name, &email)
		}
		result.Close()
	}
	
	duration := time.Since(start)
	avgLatency := duration / time.Duration(iterations)
	qps := float64(iterations) / duration.Seconds()
	
	fmt.Printf("  Executed %d queries in %v\n", iterations, duration)
	fmt.Printf("  Average latency: %v\n", avgLatency)
	fmt.Printf("  Queries per second: %.2f\n", qps)
}

// concurrentQueryPerformance measures concurrent query performance
func concurrentQueryPerformance(ctx context.Context, store storage.Storage) {
	workers := 10
	queriesPerWorker := 100
	totalQueries := workers * queriesPerWorker
	
	query := query.NewBuilder().
		Select("COUNT(*)").
		From("users").
		Where(query.Equal("active", true))

	queryObj, err := query.Build()
	if err != nil {
		log.Printf("Failed to build query: %v", err)
		return
	}

	var wg sync.WaitGroup
	start := time.Now()
	
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for i := 0; i < queriesPerWorker; i++ {
				result, err := store.Query(ctx, queryObj)
				if err != nil {
					log.Printf("Query failed: %v", err)
					continue
				}
				
				if result.Next() {
					var count int
					result.Scan(&count)
				}
				result.Close()
			}
		}()
	}
	
	wg.Wait()
	duration := time.Since(start)
	qps := float64(totalQueries) / duration.Seconds()
	
	fmt.Printf("  Executed %d concurrent queries with %d workers in %v\n", 
		totalQueries, workers, duration)
	fmt.Printf("  Queries per second: %.2f\n", qps)
}

// batchOperationPerformance measures batch operation performance
func batchOperationPerformance(ctx context.Context, store storage.Storage) {
	batchSize := 100
	
	// Create batch operations
	operations := make([]storage.Operation, batchSize)
	for i := 0; i < batchSize; i++ {
		cmd := query.Insert("users").
			Set("name", fmt.Sprintf("Batch User %d", i)).
			Set("email", fmt.Sprintf("batch%d@example.com", i)).
			Set("age", 25+i%40).
			Set("active", true)
		
		command, err := cmd.Build()
		if err != nil {
			log.Printf("Failed to build command: %v", err)
			continue
		}
		
		operations[i] = storage.Operation{
			Type:    storage.OperationTypeCommand,
			Command: command,
		}
	}
	
	start := time.Now()
	results, err := store.Batch(ctx, operations)
	duration := time.Since(start)
	
	if err != nil {
		log.Printf("Batch operation failed: %v", err)
		return
	}
	
	successCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		}
	}
	
	opsPerSecond := float64(batchSize) / duration.Seconds()
	
	fmt.Printf("  Executed batch of %d operations in %v\n", batchSize, duration)
	fmt.Printf("  Successful operations: %d/%d\n", successCount, batchSize)
	fmt.Printf("  Operations per second: %.2f\n", opsPerSecond)
}

// transactionPerformance measures transaction performance
func transactionPerformance(ctx context.Context, store storage.Storage) {
	iterations := 100
	
	start := time.Now()
	
	for i := 0; i < iterations; i++ {
		tx, err := store.BeginTx(ctx, &storage.TxOptions{
			Isolation: storage.IsolationLevelReadCommitted,
		})
		if err != nil {
			log.Printf("Failed to begin transaction: %v", err)
			continue
		}
		
		// Insert operation within transaction
		cmd := query.Insert("users").
			Set("name", fmt.Sprintf("TX User %d", i)).
			Set("email", fmt.Sprintf("tx%d@example.com", i)).
			Set("age", 30).
			Set("active", true)
		
		command, err := cmd.Build()
		if err != nil {
			tx.Rollback()
			continue
		}
		
		_, err = tx.Execute(ctx, command)
		if err != nil {
			tx.Rollback()
			continue
		}
		
		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit transaction: %v", err)
		}
	}
	
	duration := time.Since(start)
	txPerSecond := float64(iterations) / duration.Seconds()
	
	fmt.Printf("  Executed %d transactions in %v\n", iterations, duration)
	fmt.Printf("  Transactions per second: %.2f\n", txPerSecond)
}

// connectionPoolPerformance measures connection pool efficiency
func connectionPoolPerformance(ctx context.Context, store storage.Storage) {
	workers := 20
	queriesPerWorker := 50
	
	query := query.NewBuilder().
		Select("id").
		From("users").
		Limit(1)

	queryObj, err := query.Build()
	if err != nil {
		log.Printf("Failed to build query: %v", err)
		return
	}

	var wg sync.WaitGroup
	connectionAcquisitionTimes := make([]time.Duration, workers*queriesPerWorker)
	var mutex sync.Mutex
	index := 0
	
	start := time.Now()
	
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for i := 0; i < queriesPerWorker; i++ {
				queryStart := time.Now()
				result, err := store.Query(ctx, queryObj)
				queryDuration := time.Since(queryStart)
				
				mutex.Lock()
				connectionAcquisitionTimes[index] = queryDuration
				index++
				mutex.Unlock()
				
				if err != nil {
					log.Printf("Query failed: %v", err)
					continue
				}
				
				if result.Next() {
					var id int
					result.Scan(&id)
				}
				result.Close()
			}
		}()
	}
	
	wg.Wait()
	totalDuration := time.Since(start)
	
	// Calculate statistics
	var totalAcquisitionTime time.Duration
	var maxAcquisitionTime time.Duration
	var minAcquisitionTime time.Duration = time.Hour // Initialize to a large value
	
	for _, duration := range connectionAcquisitionTimes {
		totalAcquisitionTime += duration
		if duration > maxAcquisitionTime {
			maxAcquisitionTime = duration
		}
		if duration < minAcquisitionTime {
			minAcquisitionTime = duration
		}
	}
	
	avgAcquisitionTime := totalAcquisitionTime / time.Duration(len(connectionAcquisitionTimes))
	
	fmt.Printf("  Executed %d queries with %d workers in %v\n", 
		workers*queriesPerWorker, workers, totalDuration)
	fmt.Printf("  Average query time: %v\n", avgAcquisitionTime)
	fmt.Printf("  Min query time: %v\n", minAcquisitionTime)
	fmt.Printf("  Max query time: %v\n", maxAcquisitionTime)
	
	// Display storage info
	info := store.Info()
	fmt.Printf("  Storage: %s %s\n", info.Name, info.Version)
	fmt.Printf("  Max connections: %d\n", info.Limits.MaxConnections)
}
