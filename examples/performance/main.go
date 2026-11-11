// Command performance measures query, batch and transaction throughput on
// PostgreSQL.
//
// It connects to POSTGRES_DSN (default: the database of docker-compose.yml),
// works on its own perf_users table, and drops it when it finishes:
//
//	go run ./examples/performance
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

const (
	defaultDSN = "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable"
	table      = "perf_users"
	seedRows   = 1000
	iterations = 1000 // queries per query benchmark; the batch and transaction benchmarks run a tenth
)

func main() {
	if err := run(context.Background(), iterations); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, n int) error {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	adapter := storage.NewPostgreSQLAdapter()
	config, err := adapter.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse POSTGRES_DSN: %w", err)
	}
	config.MaxOpenConns = 20 // enough for the concurrent benchmarks
	config.QueryTimeout = 30 * time.Second

	store, err := adapter.Connect(ctx, config)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer store.Close()

	if err := setup(ctx, store); err != nil {
		return err
	}
	defer drop(store)

	info := store.Info()
	fmt.Printf("%s, pool of %d connections, %d rows in %s\n", info.DatabaseType, info.Limits.MaxConnections, seedRows, table)

	benchmarks := []struct {
		name string
		run  func(context.Context, storage.Storage, int) error
	}{
		{"Sequential queries", sequentialQueries},
		{"Concurrent queries", concurrentQueries},
		{"Batch insert", batchInsert},
		{"Transactions", transactions},
		{"Query latency under load", latencyUnderLoad},
	}
	for i, b := range benchmarks {
		fmt.Printf("\n%d. %s\n", i+1, b.name)
		if err := b.run(ctx, store, n); err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
	}
	return nil
}

// setup creates the table, replacing any left by an interrupted run, and
// fills it in one batch.
func setup(ctx context.Context, store storage.Storage) error {
	drop(store)
	err := store.CreateTable(ctx, storage.TableSchema{
		Name: table,
		Columns: []storage.ColumnDefinition{
			{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", DataType: storage.DataTypeString, Length: 100},
			{Name: "email", DataType: storage.DataTypeString, Length: 255},
			{Name: "age", DataType: storage.DataTypeInteger},
			{Name: "active", DataType: storage.DataTypeBoolean},
		},
		Indexes: []storage.IndexDefinition{{Name: table + "_active_idx", Columns: []string{"active"}}},
	})
	if err != nil {
		return fmt.Errorf("create table %s: %w", table, err)
	}

	operations := make([]storage.Operation, seedRows)
	for i := range operations {
		command, err := insertUser(fmt.Sprintf("seed%d", i), i).Build()
		if err != nil {
			return err
		}
		operations[i] = storage.Operation{Type: storage.OperationTypeCommand, Command: command}
	}
	if _, err := store.Batch(ctx, operations); err != nil {
		return fmt.Errorf("seed %s: %w", table, err)
	}
	return nil
}

func drop(store storage.Storage) {
	if err := store.DropTable(context.Background(), table); err != nil {
		log.Printf("drop table %s: %v", table, err)
	}
}

func insertUser(key string, i int) *storage.CommandBuilder {
	return storage.Insert(table).
		Set("name", "User "+key).
		Set("email", key+"@example.com").
		Set("age", 20+i%50).
		Set("active", i%4 != 0)
}

// readAll runs a query and reads every row, returning the number of rows.
func readAll(ctx context.Context, store storage.Storage, query storage.Query) (int, error) {
	rows, err := store.Query(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func pageQuery(page int) (storage.Query, error) {
	return storage.NewBuilder().
		Select("id", "name").
		From(table).
		Where(storage.Equal("active", true)).
		WhereOr(storage.LessThan("age", 30), storage.GreaterThan("age", 60)).
		OrderByAsc("id").
		Page(page, 10).
		Build()
}

func report(operations int, elapsed time.Duration, unit string) {
	fmt.Printf("   %d %s in %v (%.0f %s/s)\n",
		operations, unit, elapsed.Round(time.Millisecond), float64(operations)/elapsed.Seconds(), unit)
}

func reportAverage(operations int, elapsed time.Duration) {
	fmt.Printf("   average latency %v\n", (elapsed / time.Duration(operations)).Round(time.Microsecond))
}

func sequentialQueries(ctx context.Context, store storage.Storage, n int) error {
	start := time.Now()
	for i := 0; i < n; i++ {
		query, err := pageQuery(i%10 + 1)
		if err != nil {
			return err
		}
		if _, err := readAll(ctx, store, query); err != nil {
			return err
		}
	}
	elapsed := time.Since(start)
	report(n, elapsed, "queries")
	reportAverage(n, elapsed)
	return nil
}

func concurrentQueries(ctx context.Context, store storage.Storage, n int) error {
	const workers = 10
	query, err := pageQuery(1)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var failed atomic.Int64
	var firstErr error
	var once sync.Once
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n/workers; i++ {
				if _, err := readAll(ctx, store, query); err != nil {
					failed.Add(1)
					once.Do(func() { firstErr = err })
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return fmt.Errorf("%d queries failed, the first with: %w", failed.Load(), firstErr)
	}
	report(n/workers*workers, time.Since(start), "queries")
	return nil
}

func batchInsert(ctx context.Context, store storage.Storage, n int) error {
	operations := make([]storage.Operation, n/10)
	for i := range operations {
		command, err := insertUser(fmt.Sprintf("batch%d", i), i).Build()
		if err != nil {
			return err
		}
		operations[i] = storage.Operation{Type: storage.OperationTypeCommand, Command: command}
	}

	start := time.Now()
	if _, err := store.Batch(ctx, operations); err != nil {
		return err
	}
	report(len(operations), time.Since(start), "inserts")
	return nil
}

func transactions(ctx context.Context, store storage.Storage, n int) error {
	count := n / 10
	start := time.Now()
	for i := 0; i < count; i++ {
		if err := transferOnce(ctx, store, i); err != nil {
			return err
		}
	}
	elapsed := time.Since(start)
	report(count, elapsed, "transactions")
	reportAverage(count, elapsed)
	return nil
}

// transferOnce inserts a user and deactivates another in one transaction.
func transferOnce(ctx context.Context, store storage.Storage, i int) error {
	tx, err := store.BeginTx(ctx, &storage.TxOptions{Isolation: storage.IsolationLevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // does nothing once the transaction is committed

	insert, err := insertUser(fmt.Sprintf("tx%d", i), i).Build()
	if err != nil {
		return err
	}
	update, err := storage.Update(table).Set("active", false).Where(storage.Equal("id", i+1)).Build()
	if err != nil {
		return err
	}
	for _, command := range []storage.Command{insert, update} {
		if _, err := tx.Execute(ctx, command); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func latencyUnderLoad(ctx context.Context, store storage.Storage, n int) error {
	const workers = 20
	query, err := storage.NewBuilder().Select("id", "name").From(table).Where(storage.Equal("id", 1)).Build()
	if err != nil {
		return err
	}

	var mu sync.Mutex
	var latencies []time.Duration
	var firstErr error
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n/workers; i++ {
				queryStart := time.Now()
				_, err := readAll(ctx, store, query)
				latency := time.Since(queryStart)

				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				latencies = append(latencies, latency)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	elapsed := time.Since(start)

	slices.Sort(latencies)
	percentile := func(p int) time.Duration {
		return latencies[(len(latencies)-1)*p/100].Round(time.Microsecond)
	}
	report(len(latencies), elapsed, "queries")
	fmt.Printf("   latency with %d workers: p50 %v, p95 %v, p99 %v, max %v\n",
		workers, percentile(50), percentile(95), percentile(99), percentile(100))
	return nil
}
