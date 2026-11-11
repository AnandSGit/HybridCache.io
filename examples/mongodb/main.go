// Command mongodb shows the library on MongoDB: the same query and command
// builders and Storage API as on PostgreSQL, run against a collection.
//
// It connects to MONGODB_URI (default: the replica set of docker-compose.yml;
// transactions need a replica set), uses the database named in the URI or
// hybridcache_example, works on its own example_users collection, and drops it
// when it finishes:
//
//	go run ./examples/mongodb
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
	defaultURI      = "mongodb://localhost:27017/?replicaSet=rs0&directConnection=true"
	defaultDatabase = "hybridcache_example"
	usersCollection = "example_users"
)

// User is a document of the users collection. ScanRow decodes fields by bson
// tag (untagged fields match their lowercased names).
type User struct {
	ID     int    `bson:"id"`
	Name   string `bson:"name"`
	Email  string `bson:"email"`
	Age    int    `bson:"age"`
	Active bool   `bson:"active"`
}

var users = []User{
	{ID: 1, Name: "John Doe", Email: "john@example.com", Age: 30, Active: true},
	{ID: 2, Name: "Jane Smith", Email: "jane@example.com", Age: 28, Active: true},
	{ID: 3, Name: "Alice Johnson", Email: "alice@example.com", Age: 25, Active: false},
	{ID: 4, Name: "Bob Wilson", Email: "bob@example.com", Age: 35, Active: true},
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		uri = defaultURI
	}

	adapter := storage.NewMongoDBAdapter()
	config, err := adapter.ParseDSN(uri)
	if err != nil {
		return fmt.Errorf("parse MONGODB_URI: %w", err)
	}
	if config.Database == "" {
		config.Database = defaultDatabase
	}
	config.ConnectTimeout = 10 * time.Second
	config.QueryTimeout = 10 * time.Second // the default timeout of each query and command

	store, err := adapter.Connect(ctx, config)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer store.Close()

	info := store.Info()
	health := store.Health(ctx)
	fmt.Printf("Connected to %s %v, database %s: %s\n", info.DatabaseType, health.Details["version"], config.Database, health.Message)
	fmt.Printf("Features: %v\n", info.Features)

	if err := createCollection(ctx, store); err != nil {
		return err
	}
	defer dropCollection(store)

	steps := []struct {
		name string
		run  func(context.Context, storage.Storage) error
	}{
		{"Insert documents", insertDocuments},
		{"Query with the builder", func(ctx context.Context, store storage.Storage) error {
			return queryUsers(ctx, store, adapter)
		}},
		{"Count", countActive},
		{"Find one document", findUsers},
		{"Update and delete", updateAndDelete},
		{"Transaction: commit", commitTransaction},
		{"Transaction: rollback", rollbackTransaction},
		{"Batch", batch},
		{"SQL-only features", sqlOnlyFeatures},
		{"Describe the collection", describeCollection},
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

// createCollection creates the example collection with a unique index on
// email, replacing any collection left by an interrupted run. Collections are
// schemaless: only the indexes of the schema are created.
func createCollection(ctx context.Context, store storage.Storage) error {
	dropCollection(store)
	err := store.CreateTable(ctx, storage.TableSchema{
		Name: usersCollection,
		Indexes: []storage.IndexDefinition{
			{Name: "email_unique", Columns: []string{"email"}, Unique: true},
			{Name: "age_active", Columns: []string{"age", "active"}},
		},
	})
	if err != nil {
		return fmt.Errorf("create collection %s: %w", usersCollection, err)
	}
	fmt.Printf("Created collection %s\n", usersCollection)
	return nil
}

func dropCollection(store storage.Storage) {
	if err := store.DropTable(context.Background(), usersCollection); err != nil {
		log.Printf("drop collection %s: %v", usersCollection, err)
	}
}

func insertUser(u User) *storage.CommandBuilder {
	return storage.Insert(usersCollection).
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

func insertDocuments(ctx context.Context, store storage.Storage) error {
	for _, u := range users {
		if _, err := execute(ctx, store, insertUser(u)); err != nil {
			return err
		}
	}
	fmt.Printf("   Inserted %d documents\n", len(users))

	_, err := execute(ctx, store, insertUser(User{ID: 5, Name: "John Again", Email: "john@example.com", Age: 40}))
	switch {
	case err == nil:
		return errors.New("a duplicate email was accepted")
	case !errors.Is(err, storage.ErrDuplicateKey):
		return err
	}
	fmt.Println("   A duplicate email is rejected with storage.ErrDuplicateKey")
	return nil
}

func queryUsers(ctx context.Context, store storage.Storage, adapter storage.Adapter) error {
	query, err := storage.NewBuilder().
		Select("name", "email", "age").
		From(usersCollection).
		Where(storage.Equal("active", true)).
		WhereOr(storage.LessThan("age", 30), storage.Like("name", "B%")).
		OrderByDesc("age").
		Page(1, 10).
		Build()
	if err != nil {
		return err
	}
	// TranslateQuery shows the MongoDB operation the query runs as.
	operation, _, err := adapter.TranslateQuery(query)
	if err != nil {
		return err
	}
	fmt.Printf("   %s\n", operation)

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
		fmt.Printf("   - %-11s %-17s age %d\n", u.Name, u.Email, u.Age)
	}
	return rows.Err()
}

func countActive(ctx context.Context, store storage.Storage) error {
	query, err := storage.NewBuilder().SelectCount("*").From(usersCollection).Where(storage.Equal("active", true)).Build()
	if err != nil {
		return err
	}
	var n int64
	row, err := store.QueryOne(ctx, query)
	if err == nil {
		err = row.Scan(&n)
	}
	if err != nil {
		return err
	}
	fmt.Printf("   %d active users\n", n)
	return nil
}

func findUsers(ctx context.Context, store storage.Storage) error {
	for _, email := range []string{"jane@example.com", "nobody@example.com"} {
		query, err := storage.NewBuilder().
			Select("name", "age").
			From(usersCollection).
			Where(storage.Equal("email", email)).
			Build()
		if err != nil {
			return err
		}

		// MongoDB reports a missing document from QueryOne, PostgreSQL from
		// Row.Scan: checking both keeps the code portable.
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

func updateAndDelete(ctx context.Context, store storage.Storage) error {
	n, err := execute(ctx, store, storage.Update(usersCollection).
		Set("age", 31).
		Where(storage.Equal("email", "john@example.com")))
	if err != nil {
		return err
	}
	fmt.Printf("   Updated %d document(s)\n", n)

	n, err = execute(ctx, store, storage.Delete(usersCollection).Where(storage.Equal("active", false)))
	if err != nil {
		return err
	}
	fmt.Printf("   Deleted %d inactive document(s)\n", n)
	return nil
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

	changes := []*storage.CommandBuilder{
		insertUser(User{ID: 6, Name: "Carol King", Email: "carol@example.com", Age: 33, Active: true}),
		storage.Update(usersCollection).Set("active", false).Where(storage.Equal("email", "bob@example.com")),
	}
	for _, change := range changes {
		command, err := change.Build()
		if err != nil {
			return err
		}
		if _, err := tx.Execute(ctx, command); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Println("   Inserted Carol and deactivated Bob in one committed transaction")
	return nil
}

func rollbackTransaction(ctx context.Context, store storage.Storage) error {
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	command, err := storage.Delete(usersCollection).Build()
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

	query, err := storage.NewBuilder().SelectCount("*").From(usersCollection).Build()
	if err != nil {
		return err
	}
	var n int64
	row, err := store.QueryOne(ctx, query)
	if err == nil {
		err = row.Scan(&n)
	}
	if err != nil {
		return err
	}
	fmt.Printf("   Deleted %d documents in a transaction and rolled it back: %d remain\n", result.RowsAffected, n)
	return nil
}

func batch(ctx context.Context, store storage.Storage) error {
	var operations []storage.Operation
	for _, u := range []User{
		{ID: 7, Name: "Dave Lee", Email: "dave@example.com", Age: 41, Active: true},
		{ID: 8, Name: "Dave Again", Email: "dave@example.com", Age: 42, Active: true},
	} {
		command, err := insertUser(u).Build()
		if err != nil {
			return err
		}
		operations = append(operations, storage.Operation{Type: storage.OperationTypeCommand, Command: command})
	}

	// MongoDB runs the operations one after another; each result carries its
	// own error, and a failure does not undo the others.
	results, err := store.Batch(ctx, operations)
	if err != nil {
		return err
	}
	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("   Operation %d failed: %v\n", r.Index, r.Error)
			continue
		}
		if result, ok := r.Result.(storage.ExecuteResult); ok {
			fmt.Printf("   Operation %d: %d document(s) inserted\n", r.Index, result.RowsAffected)
		}
	}
	return nil
}

func sqlOnlyFeatures(ctx context.Context, store storage.Storage) error {
	query, err := storage.NewBuilder().
		From(usersCollection+" u").
		InnerJoin("orders o", storage.Equal("o.user_id", storage.Col("u.id"))).
		Build()
	if err != nil {
		return err
	}
	_, err = store.Query(ctx, query)
	switch {
	case err == nil:
		return errors.New("a join was accepted")
	case !errors.Is(err, storage.ErrUnsupportedQuery):
		return err
	}
	fmt.Printf("   A join is rejected with storage.ErrUnsupportedQuery: %v\n", err)
	return nil
}

func describeCollection(ctx context.Context, store storage.Storage) error {
	schema, err := store.DescribeTable(ctx, usersCollection)
	if err != nil {
		return err
	}
	for _, index := range schema.Indexes {
		fmt.Printf("   - index %s on %v (unique: %t)\n", index.Name, index.Columns, index.Unique)
	}
	return nil
}
