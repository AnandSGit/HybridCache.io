// Basic usage example for Database Agnostic Storage Library
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/HybridCache.io/storage/pkg/adapters/postgres"
	"github.com/HybridCache.io/storage/pkg/query"
	"github.com/HybridCache.io/storage/pkg/storage"
)

// User represents a user entity
type User struct {
	ID        int       `db:"id"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	Age       int       `db:"age"`
	Active    bool      `db:"active"`
	CreatedAt time.Time `db:"created_at"`
}

func main() {
	ctx := context.Background()

	// Initialize PostgreSQL adapter
	adapter := postgres.NewAdapter()
	config := storage.Config{
		Host:            "localhost",
		Port:            5432,
		Database:        "testdb",
		Username:        "testuser",
		Password:        "testpass",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 1 * time.Hour,
		SSLMode:         "disable",
	}

	// Connect to database
	store, err := adapter.Connect(ctx, config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer store.Close()

	fmt.Println("=== Database Agnostic Storage Library - Basic Usage ===")

	// 1. Simple Query
	fmt.Println("\n1. Simple Query Example:")
	users, err := getActiveUsers(ctx, store)
	if err != nil {
		log.Printf("Error getting users: %v", err)
	} else {
		fmt.Printf("Found %d active users\n", len(users))
		for _, user := range users {
			fmt.Printf("  - %s (%s)\n", user.Name, user.Email)
		}
	}

	// 2. Insert Example
	fmt.Println("\n2. Insert Example:")
	newUser := User{
		Name:   "Example User",
		Email:  "example@test.com",
		Age:    25,
		Active: true,
	}
	
	if err := createUser(ctx, store, newUser); err != nil {
		log.Printf("Error creating user: %v", err)
	} else {
		fmt.Println("User created successfully")
	}

	// 3. Update Example
	fmt.Println("\n3. Update Example:")
	if err := updateUserAge(ctx, store, "example@test.com", 26); err != nil {
		log.Printf("Error updating user: %v", err)
	} else {
		fmt.Println("User updated successfully")
	}

	// 4. Transaction Example
	fmt.Println("\n4. Transaction Example:")
	if err := transferUserStatus(ctx, store); err != nil {
		log.Printf("Transaction failed: %v", err)
	} else {
		fmt.Println("Transaction completed successfully")
	}

	// 5. Complex Query Example
	fmt.Println("\n5. Complex Query Example:")
	if err := complexQueryExample(ctx, store); err != nil {
		log.Printf("Complex query failed: %v", err)
	} else {
		fmt.Println("Complex query executed successfully")
	}
}

// getActiveUsers demonstrates a simple SELECT query
func getActiveUsers(ctx context.Context, store storage.Storage) ([]User, error) {
	query := query.NewBuilder().
		Select("id", "name", "email", "age", "active", "created_at").
		From("users").
		Where(query.Equal("active", true)).
		OrderBy("name", storage.SortDirectionAsc).
		Limit(10)

	queryObj, err := query.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	result, err := store.Query(ctx, queryObj)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer result.Close()

	var users []User
	for result.Next() {
		var user User
		if err := result.ScanRow(&user); err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("result error: %w", err)
	}

	return users, nil
}

// createUser demonstrates an INSERT command
func createUser(ctx context.Context, store storage.Storage, user User) error {
	cmd := query.Insert("users").
		Set("name", user.Name).
		Set("email", user.Email).
		Set("age", user.Age).
		Set("active", user.Active)

	command, err := cmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build insert command: %w", err)
	}

	result, err := store.Execute(ctx, command)
	if err != nil {
		return fmt.Errorf("failed to execute insert: %w", err)
	}

	fmt.Printf("Rows affected: %d\n", result.RowsAffected)
	return nil
}

// updateUserAge demonstrates an UPDATE command
func updateUserAge(ctx context.Context, store storage.Storage, email string, newAge int) error {
	cmd := query.Update("users").
		Set("age", newAge).
		Set("updated_at", time.Now()).
		Where(query.Equal("email", email))

	command, err := cmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build update command: %w", err)
	}

	result, err := store.Execute(ctx, command)
	if err != nil {
		return fmt.Errorf("failed to execute update: %w", err)
	}

	fmt.Printf("Rows affected: %d\n", result.RowsAffected)
	return nil
}

// transferUserStatus demonstrates transaction usage
func transferUserStatus(ctx context.Context, store storage.Storage) error {
	tx, err := store.BeginTx(ctx, &storage.TxOptions{
		Isolation: storage.IsolationLevelReadCommitted,
		Timeout:   30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Deactivate one user
	deactivateCmd := query.Update("users").
		Set("active", false).
		Where(query.Equal("email", "bob.johnson@example.com"))

	command1, err := deactivateCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build deactivate command: %w", err)
	}

	result1, err := tx.Execute(ctx, command1)
	if err != nil {
		return fmt.Errorf("failed to deactivate user: %w", err)
	}

	// Activate another user
	activateCmd := query.Update("users").
		Set("active", true).
		Where(query.Equal("email", "example@test.com"))

	command2, err := activateCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build activate command: %w", err)
	}

	result2, err := tx.Execute(ctx, command2)
	if err != nil {
		return fmt.Errorf("failed to activate user: %w", err)
	}

	fmt.Printf("Deactivated %d users, activated %d users\n", 
		result1.RowsAffected, result2.RowsAffected)

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// complexQueryExample demonstrates complex queries with joins
func complexQueryExample(ctx context.Context, store storage.Storage) error {
	// Query users with their order counts
	query := query.NewBuilder().
		Select("u.name", "u.email", "COUNT(o.id) as order_count").
		From("users u").
		LeftJoin("orders o", query.Equal("o.user_id", "u.id")).
		Where(query.Equal("u.active", true)).
		GroupBy("u.id", "u.name", "u.email").
		Having(query.GreaterThan("COUNT(o.id)", 0)).
		OrderBy("order_count", storage.SortDirectionDesc).
		Limit(5)

	queryObj, err := query.Build()
	if err != nil {
		return fmt.Errorf("failed to build complex query: %w", err)
	}

	result, err := store.Query(ctx, queryObj)
	if err != nil {
		return fmt.Errorf("failed to execute complex query: %w", err)
	}
	defer result.Close()

	fmt.Println("Users with orders:")
	for result.Next() {
		var name, email string
		var orderCount int
		if err := result.Scan(&name, &email, &orderCount); err != nil {
			return fmt.Errorf("failed to scan result: %w", err)
		}
		fmt.Printf("  - %s (%s): %d orders\n", name, email, orderCount)
	}

	return result.Err()
}
