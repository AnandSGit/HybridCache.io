// Example application demonstrating the Database Agnostic Storage library
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

// User represents a user in our system
type User struct {
	ID        int       `db:"id"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	Age       int       `db:"age"`
	Active    bool      `db:"active"`
	CreatedAt time.Time `db:"created_at"`
}

// Product represents a product in our system
type Product struct {
	ID          int     `db:"id"`
	Name        string  `db:"name"`
	Description string  `db:"description"`
	Price       float64 `db:"price"`
	CategoryID  int     `db:"category_id"`
	InStock     bool    `db:"in_stock"`
}

func main() {
	ctx := context.Background()

	// Get database connection string from environment or use default
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable"
	}

	// Create adapter and connect
	adapter := storage.NewPostgreSQLAdapter()
	config, err := adapter.ParseDSN(dsn)
	if err != nil {
		log.Fatalf("Failed to parse DSN: %v", err)
	}

	// Configure connection pool
	config.MaxOpenConns = 25
	config.MaxIdleConns = 5
	config.ConnMaxLifetime = 1 * time.Hour
	config.ConnMaxIdleTime = 30 * time.Minute

	store, err := adapter.Connect(ctx, config)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer store.Close()

	fmt.Println("🚀 Database Agnostic Storage Library Example")
	fmt.Println("============================================")

	// Test connection
	if err := store.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	fmt.Println("✅ Database connection successful")

	// Display storage info
	info := store.Info()
	fmt.Printf("📊 Storage Info: %s %s (%s)\n", info.Name, info.Version, info.DatabaseType.String())
	fmt.Printf("🔧 Features: %v\n", info.Features)

	// Check health
	health := store.Health(ctx)
	fmt.Printf("💚 Health Status: %v - %s\n", health.Status, health.Message)

	// Demonstrate basic CRUD operations
	fmt.Println("\n📝 Demonstrating CRUD Operations")
	fmt.Println("=================================")

	// 1. CREATE - Insert a new user
	fmt.Println("\n1. Creating a new user...")
	insertCmd := storage.Insert("users").
		Set("name", "Alice Johnson").
		Set("email", "alice.johnson@example.com").
		Set("age", 29).
		Set("active", true)

	command, err := insertCmd.Build()
	if err != nil {
		log.Fatalf("Failed to build insert command: %v", err)
	}

	result, err := store.Execute(ctx, command)
	if err != nil {
		log.Printf("⚠️  Insert failed (user might already exist): %v", err)
	} else {
		fmt.Printf("✅ User created successfully. Rows affected: %d\n", result.RowsAffected)
	}

	// 2. READ - Query users
	fmt.Println("\n2. Reading users...")
	selectQuery := storage.NewBuilder().
		Select("id", "name", "email", "age", "active").
		From("users").
		Where(storage.Equal("active", true)).
		OrderBy("name", storage.SortDirectionAsc).
		Limit(5)

	queryObj, err := selectQuery.Build()
	if err != nil {
		log.Fatalf("Failed to build select query: %v", err)
	}

	rows, err := store.Query(ctx, queryObj)
	if err != nil {
		log.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()

	fmt.Println("👥 Active Users:")
	for rows.Next() {
		var user User
		if err := rows.ScanRow(&user); err != nil {
			log.Printf("Failed to scan user: %v", err)
			continue
		}
		fmt.Printf("   - %s (%s) - Age: %d\n", user.Name, user.Email, user.Age)
	}

	// 3. UPDATE - Update a user
	fmt.Println("\n3. Updating user...")
	updateCmd := storage.Update("users").
		Set("age", 30).
		Where(storage.Equal("email", "alice.johnson@example.com"))

	updateCommand, err := updateCmd.Build()
	if err != nil {
		log.Fatalf("Failed to build update command: %v", err)
	}

	updateResult, err := store.Execute(ctx, updateCommand)
	if err != nil {
		log.Fatalf("Failed to execute update: %v", err)
	}
	fmt.Printf("✅ User updated. Rows affected: %d\n", updateResult.RowsAffected)

	// 4. Complex Query with JOIN
	fmt.Println("\n4. Complex query with JOIN...")
	complexQuery := storage.NewBuilder().
		Select("u.name", "u.email", "COUNT(o.id) as order_count").
		From("users u").
		LeftJoin("orders o", storage.Equal("o.user_id", "u.id")).
		Where(storage.Equal("u.active", true)).
		GroupBy("u.id", "u.name", "u.email").
		OrderBy("order_count", storage.SortDirectionDesc).
		Limit(10)

	complexQueryObj, err := complexQuery.Build()
	if err != nil {
		log.Fatalf("Failed to build complex query: %v", err)
	}

	complexRows, err := store.Query(ctx, complexQueryObj)
	if err != nil {
		log.Fatalf("Failed to execute complex query: %v", err)
	}
	defer complexRows.Close()

	fmt.Println("📊 Users with Order Counts:")
	for complexRows.Next() {
		var name, email string
		var orderCount int
		if err := complexRows.Scan(&name, &email, &orderCount); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}
		fmt.Printf("   - %s (%s) - Orders: %d\n", name, email, orderCount)
	}

	// 5. Transaction Example
	fmt.Println("\n5. Transaction example...")
	if err := demonstrateTransaction(ctx, store); err != nil {
		log.Printf("Transaction failed: %v", err)
	} else {
		fmt.Println("✅ Transaction completed successfully")
	}

	// 6. Batch Operations
	fmt.Println("\n6. Batch operations...")
	if err := demonstrateBatch(ctx, store); err != nil {
		log.Printf("Batch operations failed: %v", err)
	} else {
		fmt.Println("✅ Batch operations completed successfully")
	}

	// 7. Schema Operations
	fmt.Println("\n7. Schema operations...")
	if err := demonstrateSchema(ctx, store); err != nil {
		log.Printf("Schema operations failed: %v", err)
	} else {
		fmt.Println("✅ Schema operations completed successfully")
	}

	fmt.Println("\n🎉 Example completed successfully!")
}

func demonstrateTransaction(ctx context.Context, store storage.Storage) error {
	// Start a transaction
	tx, err := store.BeginTx(ctx, &storage.TxOptions{
		Isolation: storage.IsolationLevelReadCommitted,
		ReadOnly:  false,
		Timeout:   30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Ensure rollback if not committed

	// Insert a user within the transaction
	insertCmd := storage.Insert("users").
		Set("name", "Transaction User").
		Set("email", "tx.user@example.com").
		Set("age", 25).
		Set("active", true)

	command, err := insertCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build insert command: %w", err)
	}

	_, err = tx.Execute(ctx, command)
	if err != nil {
		return fmt.Errorf("failed to execute insert in transaction: %w", err)
	}

	// Query within the transaction
	selectQuery := storage.NewBuilder().
		Select("COUNT(*)").
		From("users").
		Where(storage.Equal("email", "tx.user@example.com"))

	queryObj, err := selectQuery.Build()
	if err != nil {
		return fmt.Errorf("failed to build select query: %w", err)
	}

	row, err := tx.QueryOne(ctx, queryObj)
	if err != nil {
		return fmt.Errorf("failed to execute query in transaction: %w", err)
	}

	var count int
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("failed to scan count: %w", err)
	}

	fmt.Printf("   Users with email 'tx.user@example.com': %d\n", count)

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func demonstrateBatch(ctx context.Context, store storage.Storage) error {
	// Create multiple operations
	operations := []storage.Operation{
		{
			Type: storage.OperationTypeCommand,
			Command: storage.Command{
				SQL:        "INSERT INTO users (name, email, age, active) VALUES ($1, $2, $3, $4)",
				Parameters: []interface{}{"Batch User 1", "batch1@example.com", 26, true},
				Type:       storage.CommandTypeInsert,
			},
		},
		{
			Type: storage.OperationTypeCommand,
			Command: storage.Command{
				SQL:        "INSERT INTO users (name, email, age, active) VALUES ($1, $2, $3, $4)",
				Parameters: []interface{}{"Batch User 2", "batch2@example.com", 27, true},
				Type:       storage.CommandTypeInsert,
			},
		},
		{
			Type: storage.OperationTypeQuery,
			Query: storage.Query{
				SQL:        "SELECT COUNT(*) FROM users WHERE active = $1",
				Parameters: []interface{}{true},
				Type:       storage.QueryTypeSelect,
			},
		},
	}

	// Execute batch
	results, err := store.Batch(ctx, operations)
	if err != nil {
		return fmt.Errorf("failed to execute batch: %w", err)
	}

	// Process results
	for i, result := range results {
		if result.Error != nil {
			fmt.Printf("   Operation %d failed: %v\n", i, result.Error)
		} else {
			fmt.Printf("   Operation %d succeeded\n", i)
		}
	}

	return nil
}

func demonstrateSchema(ctx context.Context, store storage.Storage) error {
	// List existing tables
	tables, err := store.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}
	fmt.Printf("   Existing tables: %v\n", tables)

	// Describe a table
	if len(tables) > 0 {
		schema, err := store.DescribeTable(ctx, tables[0])
		if err != nil {
			return fmt.Errorf("failed to describe table %s: %w", tables[0], err)
		}
		fmt.Printf("   Table '%s' has %d columns\n", schema.Name, len(schema.Columns))
	}

	return nil
}
