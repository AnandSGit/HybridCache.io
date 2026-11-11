package storage

import (
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
	"github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/postgres"
)

// Adapter factory functions

// NewPostgreSQLAdapter creates a PostgreSQL adapter. It runs the SQL of queries
// and commands, translating '?' placeholders, so raw SQL works as well as the
// query builder.
func NewPostgreSQLAdapter() Adapter {
	return postgres.NewAdapter()
}

// NewMongoDBAdapter creates a MongoDB adapter. It runs queries and commands made
// with the query builder (their Spec); raw SQL and SQL-only features such as
// joins, GROUP BY and subqueries return an error matching ErrUnsupportedQuery.
// Transactions require a replica set or sharded cluster.
func NewMongoDBAdapter() Adapter {
	return mongodb.NewAdapter()
}
