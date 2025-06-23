// Package main demonstrates MongoDB adapter usage
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
)

func main() {
	fmt.Println("🍃 MongoDB Adapter Example")
	fmt.Println("==========================")

	// Create MongoDB adapter
	adapter := mongodb.NewAdapter()

	// Configure connection
	config := domain.Config{
		Host:            "localhost",
		Port:            27017,
		Database:        "hybridcache_example",
		Username:        "",
		Password:        "",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1 * time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		ConnectTimeout:  10 * time.Second,
		QueryTimeout:    30 * time.Second,
	}

	// Alternative: Use DSN
	// config, err := adapter.ParseDSN("mongodb://localhost:27017/hybridcache_example")
	// if err != nil {
	//     log.Fatal("Failed to parse DSN:", err)
	// }

	// Connect to MongoDB
	ctx := context.Background()
	storage, err := adapter.Connect(ctx, config)
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}
	defer storage.Close()

	fmt.Println("✅ Connected to MongoDB successfully")

	// Test connection
	if err := storage.Ping(ctx); err != nil {
		log.Fatal("Failed to ping MongoDB:", err)
	}
	fmt.Println("✅ MongoDB ping successful")

	// Get storage info
	info := storage.Info()
	fmt.Printf("📊 Storage Info: %s v%s (%s)\n", info.Name, info.Version, info.DatabaseType)
	fmt.Printf("🔧 Features: %v\n", info.Features)

	// Get health status
	health := storage.Health(ctx)
	fmt.Printf("💚 Health Status: %v - %s\n", health.Status, health.Message)

	// Example 1: Basic Insert Operation
	fmt.Println("\n📝 Example 1: Insert Document")
	insertCommand := domain.Command{
		SQL:        "INSERT INTO users", // Simplified - in practice would be parsed
		Parameters: []interface{}{"name", "John Doe", "email", "john@example.com", "age", 30},
		Type:       domain.CommandTypeInsert,
	}

	result, err := storage.Execute(ctx, insertCommand)
	if err != nil {
		log.Printf("Insert failed: %v", err)
	} else {
		fmt.Printf("✅ Inserted document, rows affected: %d, ID: %v\n", result.RowsAffected, result.LastInsertID)
	}

	// Example 2: Query Documents
	fmt.Println("\n🔍 Example 2: Query Documents")
	query := domain.Query{
		SQL:        "SELECT FROM users",
		Parameters: []interface{}{"name", "John Doe"},
	}

	queryResult, err := storage.Query(ctx, query)
	if err != nil {
		log.Printf("Query failed: %v", err)
	} else {
		fmt.Println("✅ Query executed successfully")
		// Note: In a real implementation, you'd iterate over results
		queryResult.Close()
	}

	// Example 3: Query Single Document
	fmt.Println("\n📄 Example 3: Query Single Document")
	singleQuery := domain.Query{
		SQL:        "SELECT FROM users",
		Parameters: []interface{}{"email", "john@example.com"},
	}

	row, err := storage.QueryOne(ctx, singleQuery)
	if err != nil {
		log.Printf("QueryOne failed: %v", err)
	} else {
		fmt.Println("✅ Single document query successful")
		// Note: In a real implementation, you'd scan the row data
		_ = row
	}

	// Example 4: Update Document
	fmt.Println("\n✏️ Example 4: Update Document")
	updateCommand := domain.Command{
		SQL:        "UPDATE users",
		Parameters: []interface{}{"email", "john@example.com", "age", 31}, // filter, then update
		Type:       domain.CommandTypeUpdate,
	}

	updateResult, err := storage.Execute(ctx, updateCommand)
	if err != nil {
		log.Printf("Update failed: %v", err)
	} else {
		fmt.Printf("✅ Updated document, rows affected: %d\n", updateResult.RowsAffected)
	}

	// Example 5: Transaction Operations
	fmt.Println("\n🔄 Example 5: Transaction Operations")
	tx, err := storage.BeginTx(ctx, &domain.TxOptions{
		Isolation: domain.IsolationLevelReadCommitted,
		ReadOnly:  false,
	})
	if err != nil {
		log.Printf("Failed to begin transaction: %v", err)
	} else {
		// Insert within transaction
		txInsertCommand := domain.Command{
			SQL:        "INSERT INTO users",
			Parameters: []interface{}{"name", "Jane Smith", "email", "jane@example.com", "age", 28},
			Type:       domain.CommandTypeInsert,
		}

		txResult, err := tx.Execute(ctx, txInsertCommand)
		if err != nil {
			log.Printf("Transaction insert failed: %v", err)
			tx.Rollback()
		} else {
			fmt.Printf("✅ Transaction insert successful, rows affected: %d\n", txResult.RowsAffected)

			// Commit transaction
			if err := tx.Commit(); err != nil {
				log.Printf("Transaction commit failed: %v", err)
			} else {
				fmt.Println("✅ Transaction committed successfully")
			}
		}
	}

	// Example 6: Batch Operations
	fmt.Println("\n📦 Example 6: Batch Operations")
	operations := []domain.Operation{
		{
			Type: domain.OperationTypeCommand,
			Command: domain.Command{
				SQL:        "INSERT INTO users",
				Parameters: []interface{}{"name", "Alice Johnson", "email", "alice@example.com", "age", 25},
				Type:       domain.CommandTypeInsert,
			},
		},
		{
			Type: domain.OperationTypeCommand,
			Command: domain.Command{
				SQL:        "INSERT INTO users",
				Parameters: []interface{}{"name", "Bob Wilson", "email", "bob@example.com", "age", 35},
				Type:       domain.CommandTypeInsert,
			},
		},
		{
			Type: domain.OperationTypeCommand,
			Command: domain.Command{
				SQL:        "UPDATE users",
				Parameters: []interface{}{"email", "alice@example.com", "age", 26},
				Type:       domain.CommandTypeUpdate,
			},
		},
	}

	batchResults, err := storage.Batch(ctx, operations)
	if err != nil {
		log.Printf("Batch operations failed: %v", err)
	} else {
		fmt.Printf("✅ Batch operations completed, %d operations processed\n", len(batchResults))
		for i, result := range batchResults {
			if result.Error == nil {
				fmt.Printf("  Operation %d: ✅ Success\n", i+1)
			} else {
				fmt.Printf("  Operation %d: ❌ Failed - %v\n", i+1, result.Error)
			}
		}
	}

	// Example 7: Delete Document
	fmt.Println("\n🗑️ Example 7: Delete Document")
	deleteCommand := domain.Command{
		SQL:        "DELETE FROM users",
		Parameters: []interface{}{"email", "bob@example.com"},
		Type:       domain.CommandTypeDelete,
	}

	deleteResult, err := storage.Execute(ctx, deleteCommand)
	if err != nil {
		log.Printf("Delete failed: %v", err)
	} else {
		fmt.Printf("✅ Deleted document, rows affected: %d\n", deleteResult.RowsAffected)
	}

	fmt.Println("\n🎉 MongoDB adapter example completed successfully!")
	fmt.Println("\nNote: This example uses a simplified query format.")
	fmt.Println("In a production implementation, you would:")
	fmt.Println("- Use proper MongoDB query builders")
	fmt.Println("- Implement collection name parsing")
	fmt.Println("- Add comprehensive error handling")
	fmt.Println("- Use proper BSON document structures")
	fmt.Println("- Implement aggregation pipeline support")
}

// User represents a user document structure
type User struct {
	ID    string `bson:"_id,omitempty" json:"id"`
	Name  string `bson:"name" json:"name"`
	Email string `bson:"email" json:"email"`
	Age   int    `bson:"age" json:"age"`
}

// UserActivity represents user activity tracking
type UserActivity struct {
	ID        string                 `bson:"_id,omitempty" json:"id"`
	UserID    string                 `bson:"user_id" json:"user_id"`
	Action    string                 `bson:"action" json:"action"`
	Timestamp time.Time              `bson:"timestamp" json:"timestamp"`
	Metadata  map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
}
