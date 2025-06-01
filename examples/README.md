# Database Agnostic Storage Library - Examples

This directory contains comprehensive examples demonstrating the capabilities of the Database Agnostic Storage Library.

## Prerequisites

Before running the examples, ensure you have:

1. **Go 1.19+** installed
2. **Docker and Docker Compose** for running test databases
3. **Database dependencies** downloaded

### Setup

1. **Start the test databases:**
   ```bash
   docker-compose up -d postgres redis
   ```

2. **Wait for databases to be ready:**
   ```bash
   # Check PostgreSQL
   docker-compose exec postgres pg_isready -U testuser -d testdb
   
   # Check Redis
   docker-compose exec redis redis-cli ping
   ```

3. **Download Go dependencies:**
   ```bash
   go mod download
   ```

## Examples Overview

### 1. Basic Usage (`basic-usage/`)

Demonstrates fundamental operations:
- **Connection Management**: Establishing and configuring database connections
- **CRUD Operations**: Create, Read, Update, Delete operations
- **Query Builder**: Using the fluent query builder API
- **Transactions**: Basic transaction usage
- **Error Handling**: Proper error handling patterns

**Run the example:**
```bash
cd examples/basic-usage
go run main.go
```

**Key Features Demonstrated:**
- Simple SELECT queries with conditions
- INSERT operations with the command builder
- UPDATE operations with WHERE clauses
- Transaction management with commit/rollback
- Complex queries with JOINs and aggregations

### 2. Performance Testing (`performance/`)

Demonstrates performance optimization and benchmarking:
- **Single Query Performance**: Measuring individual query latency
- **Concurrent Operations**: Testing with multiple goroutines
- **Batch Operations**: Bulk operations for efficiency
- **Transaction Performance**: Transaction throughput testing
- **Connection Pool Efficiency**: Pool utilization metrics

**Run the example:**
```bash
cd examples/performance
go run main.go
```

**Performance Metrics:**
- Queries per second (QPS)
- Average latency (95th percentile)
- Connection acquisition time
- Transaction throughput
- Batch operation efficiency

## Example Code Patterns

### Basic Query Pattern

```go
// Build query
query := query.NewBuilder().
    Select("id", "name", "email").
    From("users").
    Where(query.Equal("active", true)).
    OrderBy("name", storage.SortDirectionAsc).
    Limit(10)

// Execute query
queryObj, err := query.Build()
if err != nil {
    return err
}

result, err := store.Query(ctx, queryObj)
if err != nil {
    return err
}
defer result.Close()

// Process results
for result.Next() {
    var user User
    if err := result.ScanRow(&user); err != nil {
        return err
    }
    // Process user...
}
```

### Command Pattern

```go
// Build command
cmd := query.Insert("users").
    Set("name", "John Doe").
    Set("email", "john@example.com").
    Set("age", 30).
    Set("active", true)

// Execute command
command, err := cmd.Build()
if err != nil {
    return err
}

result, err := store.Execute(ctx, command)
if err != nil {
    return err
}

fmt.Printf("Rows affected: %d\n", result.RowsAffected)
```

### Transaction Pattern

```go
// Begin transaction
tx, err := store.BeginTx(ctx, &storage.TxOptions{
    Isolation: storage.IsolationLevelReadCommitted,
    Timeout:   30 * time.Second,
})
if err != nil {
    return err
}
defer tx.Rollback() // Ensure rollback if not committed

// Execute operations within transaction
result1, err := tx.Execute(ctx, command1)
if err != nil {
    return err
}

result2, err := tx.Execute(ctx, command2)
if err != nil {
    return err
}

// Commit transaction
if err := tx.Commit(); err != nil {
    return err
}
```

### Batch Operations Pattern

```go
// Create batch operations
operations := []storage.Operation{
    {
        Type:    storage.OperationTypeCommand,
        Command: insertCommand1,
    },
    {
        Type:    storage.OperationTypeCommand,
        Command: insertCommand2,
    },
    {
        Type:  storage.OperationTypeQuery,
        Query: selectQuery,
    },
}

// Execute batch
results, err := store.Batch(ctx, operations)
if err != nil {
    return err
}

// Process results
for i, result := range results {
    if result.Error != nil {
        log.Printf("Operation %d failed: %v", i, result.Error)
    } else {
        log.Printf("Operation %d succeeded", i)
    }
}
```

## Configuration Examples

### Development Configuration

```go
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
```

### Production Configuration

```go
config := storage.Config{
    Host:            "prod-db.example.com",
    Port:            5432,
    Database:        "proddb",
    Username:        "produser",
    Password:        os.Getenv("DB_PASSWORD"),
    MaxOpenConns:    50,
    MaxIdleConns:    25,
    ConnMaxLifetime: 1 * time.Hour,
    ConnMaxIdleTime: 30 * time.Minute,
    ConnectTimeout:  10 * time.Second,
    QueryTimeout:    30 * time.Second,
    SSLMode:         "require",
    SSLCert:         "/path/to/client-cert.pem",
    SSLKey:          "/path/to/client-key.pem",
    SSLRootCA:       "/path/to/ca-cert.pem",
}
```

## Error Handling Best Practices

### Structured Error Handling

```go
result, err := store.Execute(ctx, command)
if err != nil {
    // Check for specific error types
    if storage.IsConnectionError(err) {
        // Handle connection errors (retry logic)
        return handleConnectionError(err)
    }
    
    if storage.IsDataError(err) {
        // Handle data validation errors
        return handleDataError(err)
    }
    
    if storage.IsRetryable(err) {
        // Implement retry logic
        return retryOperation(ctx, store, command)
    }
    
    // Handle other errors
    return fmt.Errorf("operation failed: %w", err)
}
```

### Retry Logic Example

```go
func executeWithRetry(ctx context.Context, store storage.Storage, command storage.Command, maxRetries int) error {
    for attempt := 0; attempt <= maxRetries; attempt++ {
        result, err := store.Execute(ctx, command)
        if err == nil {
            return nil
        }
        
        if !storage.IsRetryable(err) {
            return err
        }
        
        if attempt < maxRetries {
            backoff := time.Duration(attempt+1) * time.Second
            time.Sleep(backoff)
        }
    }
    
    return fmt.Errorf("operation failed after %d retries", maxRetries)
}
```

## Running All Examples

To run all examples in sequence:

```bash
# Start databases
make dev-up

# Run basic usage example
cd examples/basic-usage && go run main.go

# Run performance example
cd ../performance && go run main.go

# Stop databases
make dev-down
```

## Troubleshooting

### Common Issues

1. **Connection Refused**
   - Ensure databases are running: `docker-compose ps`
   - Check port availability: `netstat -an | grep 5432`

2. **Permission Denied**
   - Verify database credentials in docker-compose.yml
   - Check user permissions in database

3. **Timeout Errors**
   - Increase connection timeout in configuration
   - Check network connectivity to database

4. **Memory Issues**
   - Reduce batch sizes in performance examples
   - Adjust connection pool settings

### Debug Mode

Enable debug logging by setting environment variable:
```bash
export STORAGE_DEBUG=true
go run main.go
```

## Next Steps

After running the examples:

1. **Explore the API Documentation**: See `docs/api/` for detailed interface documentation
2. **Run Integration Tests**: Use `make test-integration` to run comprehensive tests
3. **Performance Tuning**: Adjust connection pool settings based on your workload
4. **Production Deployment**: Review `docs/guides/production.md` for deployment guidelines

## Contributing

To add new examples:

1. Create a new directory under `examples/`
2. Include a `main.go` file with clear documentation
3. Add a section to this README
4. Ensure the example follows established patterns
5. Test with the provided database setup
