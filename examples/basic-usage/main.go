// Command basic-usage walks through the main features of the library on
// PostgreSQL: schema management, the query and command builders, joins,
// transactions and batches.
//
// It connects to POSTGRES_DSN (default: the database of docker-compose.yml),
// works on its own example_users and example_orders tables, and drops them when
// it finishes:
//
//	go run ./examples/basic-usage
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AnandSGit/HybridCache.io/pkg/storage"
)

const (
	defaultDSN  = "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable"
	usersTable  = "example_users"
	ordersTable = "example_orders"
)

// User is a row of the users table. ScanRow maps columns to fields by db tag.
type User struct {
	ID     int    `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Age    int    `db:"age"`
	Active bool   `db:"active"`
}

var users = []User{
	{ID: 1, Name: "Alice Johnson", Email: "alice@example.com", Age: 29, Active: true},
	{ID: 2, Name: "Bob Smith", Email: "bob@example.com", Age: 35, Active: true},
	{ID: 3, Name: "Carol White", Email: "carol@example.com", Age: 42, Active: false},
	{ID: 4, Name: "Dan Brown", Email: "dan@example.com", Age: 23, Active: true},
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	adapter := storage.NewPostgreSQLAdapter()
	config, err := adapter.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse POSTGRES_DSN: %w", err)
	}
	config.QueryTimeout = 10 * time.Second // the default timeout of each query and command

	store, err := adapter.Connect(ctx, config)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer store.Close()

	info := store.Info()
	health := store.Health(ctx)
	fmt.Printf("Connected to %s (adapter %s): %s\n", info.DatabaseType, info.Version, health.Message)
	fmt.Printf("Features: %v\n", info.Features)

	if err := createTables(ctx, store); err != nil {
		return err
	}
	defer dropTables(store)

	steps := []struct {
		name string
		run  func(context.Context, storage.Storage) error
	}{
		{"Insert rows", insertRows},
		{"Query with the builder", queryUsers},
		{"Find one row", findUsers},
		{"Update", updateUser},
		{"Join with aggregates", ordersPerUser},
		{"Transaction: commit", commitTransaction},
		{"Transaction: rollback", rollbackTransaction},
		{"Batch", batch},
		{"Delete", deleteInactiveUsers},
		{"Describe a table", describeTable},
	}
	for i, step := range steps {
		fmt.Printf("\n%d. %s\n", i+1, step.name)
		if err := step.run(ctx, store); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	fmt.Println("\nDone.")
	return nil
}

// createTables creates the example tables, replacing any left by an
// interrupted run.
func createTables(ctx context.Context, store storage.Storage) error {
	dropTables(store)

	schemas := []storage.TableSchema{
		{
			Name: usersTable,
			Columns: []storage.ColumnDefinition{
				{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true},
				{Name: "name", DataType: storage.DataTypeString, Length: 100},
				{Name: "email", DataType: storage.DataTypeString, Length: 255},
				{Name: "age", DataType: storage.DataTypeInteger},
				{Name: "active", DataType: storage.DataTypeBoolean, DefaultValue: true},
			},
			Indexes: []storage.IndexDefinition{
				{Name: usersTable + "_email_key", Columns: []string{"email"}, Unique: true},
			},
		},
		{
			Name: ordersTable,
			Columns: []storage.ColumnDefinition{
				{Name: "id", DataType: storage.DataTypeInteger, PrimaryKey: true, AutoIncrement: true},
				{Name: "user_id", DataType: storage.DataTypeInteger},
				{Name: "total", DataType: storage.DataTypeDecimal, Precision: 10, Scale: 2},
			},
			Constraints: []storage.ConstraintDefinition{{
				Name:       ordersTable + "_user_fk",
				Type:       storage.ConstraintTypeForeignKey,
				Columns:    []string{"user_id"},
				RefTable:   usersTable,
				RefColumns: []string{"id"},
				OnDelete:   storage.ReferentialActionCascade,
			}},
		},
	}
	for _, schema := range schemas {
		if err := store.CreateTable(ctx, schema); err != nil {
			return fmt.Errorf("create table %s: %w", schema.Name, err)
		}
	}
	fmt.Printf("Created tables %s and %s\n", usersTable, ordersTable)
	return nil
}

func dropTables(store storage.Storage) {
	for _, table := range []string{ordersTable, usersTable} {
		if err := store.DropTable(context.Background(), table); err != nil {
			log.Printf("drop table %s: %v", table, err)
		}
	}
}

func insertUser(u User) *storage.CommandBuilder {
	return storage.Insert(usersTable).
		Set("id", u.ID).
		Set("name", u.Name).
		Set("email", u.Email).
		Set("age", u.Age).
		Set("active", u.Active)
}

func execute(ctx context.Context, store storage.Storage, b *storage.CommandBuilder) (int64, error) {
	command, err := b.Build()
	if err != nil {
		return 0, err
	}
	result, err := store.Execute(ctx, command)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected, nil
}

func insertRows(ctx context.Context, store storage.Storage) error {
	for _, u := range users {
		if _, err := execute(ctx, store, insertUser(u)); err != nil {
			return err
		}
	}
	orders := []struct {
		userID int
		total  float64
	}{{1, 25.50}, {1, 74.25}, {2, 12.00}, {4, 99.99}, {4, 5.00}, {4, 42.10}}
	for _, o := range orders {
		// id is an identity column, so the database assigns it.
		if _, err := execute(ctx, store, storage.Insert(ordersTable).Set("user_id", o.userID).Set("total", o.total)); err != nil {
			return err
		}
	}
	fmt.Printf("   Inserted %d users and %d orders\n", len(users), len(orders))

	// The unique index on email rejects a second user with the same address.
	_, err := execute(ctx, store, insertUser(User{ID: 5, Name: "Alice Again", Email: "alice@example.com", Age: 30}))
	switch {
	case err == nil:
		return errors.New("a duplicate email was accepted")
	case !errors.Is(err, storage.ErrDuplicateKey):
		return err
	}
	fmt.Println("   A duplicate email is rejected with storage.ErrDuplicateKey")
	return nil
}

func queryUsers(ctx context.Context, store storage.Storage) error {
	query, err := storage.NewBuilder().
		Select("id", "name", "email", "age", "active").
		From(usersTable).
		Where(storage.Equal("active", true)).
		WhereOr(storage.LessThan("age", 30), storage.Like("email", "bob@%")).
		OrderByAsc("name").
		Page(1, 10).
		Build()
	if err != nil {
		return err
	}
	fmt.Printf("   %s %v\n", query.SQL, query.Parameters)

	rows, err := store.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u User
		if err := rows.ScanRow(&u); err != nil {
			return err
		}
		fmt.Printf("   - %-13s %-18s age %d\n", u.Name, u.Email, u.Age)
	}
	return rows.Err()
}

func findUsers(ctx context.Context, store storage.Storage) error {
	for _, email := range []string{"carol@example.com", "nobody@example.com"} {
		query, err := storage.NewBuilder().
			Select("name", "age").
			From(usersTable).
			Where(storage.Equal("email", email)).
			Build()
		if err != nil {
			return err
		}

		var name string
		var age int
		row, err := store.QueryOne(ctx, query)
		if err == nil {
			err = row.Scan(&name, &age)
		}
		switch {
		case errors.Is(err, storage.ErrNoRows):
			fmt.Printf("   %s: not found\n", email)
		case err != nil:
			return err
		default:
			fmt.Printf("   %s: %s, age %d\n", email, name, age)
		}
	}
	return nil
}

func updateUser(ctx context.Context, store storage.Storage) error {
	n, err := execute(ctx, store, storage.Update(usersTable).
		Set("age", 30).
		Where(storage.Equal("email", "alice@example.com")))
	if err != nil {
		return err
	}
	fmt.Printf("   Updated %d row(s)\n", n)
	return nil
}

func ordersPerUser(ctx context.Context, store storage.Storage) error {
	// Col compares with a column; a plain value would be bound as a parameter.
	query, err := storage.NewBuilder().
		Select("u.name", "COUNT(o.id) AS order_count", "SUM(o.total) AS spent").
		From(usersTable+" u").
		LeftJoin(ordersTable+" o", storage.Equal("o.user_id", storage.Col("u.id"))).
		GroupBy("u.id", "u.name").
		Having(storage.GreaterThan("COUNT(o.id)", 0)).
		OrderByDesc("order_count").
		Build()
	if err != nil {
		return err
	}

	rows, err := store.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var orders int64
		var spent float64
		if err := rows.Scan(&name, &orders, &spent); err != nil {
			return err
		}
		fmt.Printf("   - %-13s %d order(s), %.2f in total\n", name, orders, spent)
	}
	return rows.Err()
}

func commitTransaction(ctx context.Context, store storage.Storage) error {
	tx, err := store.BeginTx(ctx, &storage.TxOptions{
		Isolation: storage.IsolationLevelReadCommitted,
		Timeout:   30 * time.Second,
	})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // does nothing once the transaction is committed

	// Move Dan's large orders to Bob and deactivate Dan, atomically.
	changes := []*storage.CommandBuilder{
		storage.Update(ordersTable).
			Set("user_id", 2).
			Where(storage.Equal("user_id", 4)).
			Where(storage.GreaterThan("total", 50)),
		storage.Update(usersTable).Set("active", false).Where(storage.Equal("id", 4)),
	}
	for _, change := range changes {
		command, err := change.Build()
		if err != nil {
			return err
		}
		result, err := tx.Execute(ctx, command)
		if err != nil {
			return err
		}
		fmt.Printf("   %s -> %d row(s)\n", command.SQL, result.RowsAffected)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Println("   Committed")
	return nil
}

func rollbackTransaction(ctx context.Context, store storage.Storage) error {
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	command, err := storage.Delete(ordersTable).Build()
	if err != nil {
		return err
	}
	result, err := tx.Execute(ctx, command)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Rollback(); err != nil {
		return err
	}

	n, err := countRows(ctx, store, ordersTable)
	if err != nil {
		return err
	}
	fmt.Printf("   Deleted %d orders in a transaction and rolled it back: %d orders remain\n", result.RowsAffected, n)
	return nil
}

func countRows(ctx context.Context, store storage.Storage, table string) (int64, error) {
	query, err := storage.NewBuilder().SelectCount("*").From(table).Build()
	if err != nil {
		return 0, err
	}
	var n int64
	row, err := store.QueryOne(ctx, query)
	if err == nil {
		err = row.Scan(&n)
	}
	return n, err
}

func batch(ctx context.Context, store storage.Storage) error {
	var operations []storage.Operation
	for _, u := range []User{
		{ID: 5, Name: "Eve Adams", Email: "eve@example.com", Age: 31, Active: true},
		{ID: 6, Name: "Frank Moore", Email: "frank@example.com", Age: 48, Active: true},
	} {
		command, err := insertUser(u).Build()
		if err != nil {
			return err
		}
		operations = append(operations, storage.Operation{Type: storage.OperationTypeCommand, Command: command})
	}
	query, err := storage.NewBuilder().SelectCount("*").From(usersTable).Build()
	if err != nil {
		return err
	}
	operations = append(operations, storage.Operation{Type: storage.OperationTypeQuery, Query: query})

	// PostgreSQL sends the batch in one round trip and runs it atomically: if an
	// operation fails, Batch returns its error and nothing is applied.
	results, err := store.Batch(ctx, operations)
	if err != nil {
		return err
	}
	for _, r := range results {
		switch result := r.Result.(type) {
		case storage.ExecuteResult:
			fmt.Printf("   Operation %d: %d row(s) affected\n", r.Index, result.RowsAffected)
		case storage.Result:
			var n int64
			if result.Next() {
				if err := result.Scan(&n); err != nil {
					return err
				}
			}
			if err := result.Close(); err != nil {
				return err
			}
			fmt.Printf("   Operation %d: %d users\n", r.Index, n)
		}
	}
	return nil
}

func deleteInactiveUsers(ctx context.Context, store storage.Storage) error {
	n, err := execute(ctx, store, storage.Delete(usersTable).Where(storage.Equal("active", false)))
	if err != nil {
		return err
	}
	orders, err := countRows(ctx, store, ordersTable)
	if err != nil {
		return err
	}
	fmt.Printf("   Deleted %d inactive user(s); ON DELETE CASCADE removed their orders, %d remain\n", n, orders)
	return nil
}

func describeTable(ctx context.Context, store storage.Storage) error {
	schema, err := store.DescribeTable(ctx, usersTable)
	if err != nil {
		return err
	}
	for _, c := range schema.Columns {
		fmt.Printf("   - %-6s %-8s nullable=%-5t primary key=%t\n", c.Name, c.DataType, c.Nullable, c.PrimaryKey)
	}
	return nil
}
