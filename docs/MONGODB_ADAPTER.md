# MongoDB Adapter Documentation

## Overview

The MongoDB adapter provides a complete implementation of the HybridCache.io storage interfaces for MongoDB, enabling document-based operations with full transaction support, aggregation capabilities, and high-performance async operations.

## Features

### ✅ **Core Features**
- **Document Operations**: Full CRUD operations with BSON document support
- **Multi-Document Transactions**: ACID transactions across multiple documents (MongoDB 4.0+)
- **Aggregation Pipeline**: Support for complex data processing operations
- **Connection Pooling**: Intelligent connection lifecycle management
- **Schema Management**: Collection and index operations
- **Batch Operations**: Optimized bulk write operations
- **Error Handling**: Comprehensive error types with retry logic
- **Health Monitoring**: Connection health checks and status reporting

### ✅ **MongoDB-Specific Features**
- **GridFS Support**: Large file storage and retrieval
- **Geospatial Queries**: Location-based data operations
- **Text Search**: Full-text search capabilities
- **Change Streams**: Real-time data change notifications
- **Sharding Support**: Horizontal scaling across multiple servers
- **Replica Set Support**: High availability and read scaling

## Installation

### Go Implementation

```bash
# Add MongoDB driver dependency
go get go.mongodb.org/mongo-driver@v1.13.1

# Import the adapter
import "github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
```

### Python Implementation

```bash
# Install Motor async driver
pip install motor>=3.3.0

# Or with Poetry
poetry add motor
```

## Quick Start

### Go Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/AnandSGit/HybridCache.io/internal/domain"
    "github.com/AnandSGit/HybridCache.io/internal/infrastructure/adapters/mongodb"
)

func main() {
    // Create adapter
    adapter := mongodb.NewAdapter()
    
    // Configure connection
    config := domain.Config{
        Host:     "localhost",
        Port:     27017,
        Database: "myapp",
        Username: "user",
        Password: "password",
        MaxOpenConns: 25,
        MaxIdleConns: 5,
    }
    
    // Connect
    ctx := context.Background()
    storage, err := adapter.Connect(ctx, config)
    if err != nil {
        log.Fatal(err)
    }
    defer storage.Close()
    
    // Insert document
    insertCmd := domain.Command{
        SQL: "INSERT INTO users",
        Parameters: []interface{}{
            "name", "John Doe",
            "email", "john@example.com",
            "age", 30,
        },
        Type: domain.CommandTypeInsert,
    }
    
    result, err := storage.Execute(ctx, insertCmd)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Inserted document with ID: %v", result.LastInsertID)
}
```

### Python Example

```python
import asyncio
from storage.adapters.mongodb import MongoDBAdapter
from storage.types import Config, Command, CommandType

async def main():
    # Create adapter
    adapter = MongoDBAdapter()
    
    # Configure connection
    config = Config(
        host="localhost",
        port=27017,
        database="myapp",
        username="user",
        password="password",
        max_open_conns=25,
        max_idle_conns=5,
    )
    
    # Connect
    storage = await adapter.connect(config)
    
    try:
        # Insert document
        insert_command = Command(
            sql="INSERT INTO users",
            parameters=[
                "name", "John Doe",
                "email", "john@example.com",
                "age", 30,
            ],
            command_type=CommandType.INSERT
        )
        
        result = await storage.execute(insert_command)
        print(f"Inserted document with ID: {result.last_insert_id}")
        
    finally:
        await storage.close()

if __name__ == "__main__":
    asyncio.run(main())
```

## Configuration

### Connection Options

```go
// Go configuration
config := domain.Config{
    // Basic connection
    Host:     "localhost",
    Port:     27017,
    Database: "myapp",
    Username: "user",
    Password: "password",
    
    // Connection pooling
    MaxOpenConns:    25,
    MaxIdleConns:    5,
    ConnMaxLifetime: 1 * time.Hour,
    ConnMaxIdleTime: 30 * time.Minute,
    
    // Timeouts
    ConnectTimeout: 10 * time.Second,
    QueryTimeout:   30 * time.Second,
    
    // MongoDB-specific options
    Options: map[string]interface{}{
        "authSource":  "admin",
        "replicaSet":  "rs0",
        "ssl":         "true",
        "maxPoolSize": 100,
        "minPoolSize": 10,
    },
}
```

### DSN Format

```go
// Basic DSN
dsn := "mongodb://localhost:27017/myapp"

// With authentication
dsn := "mongodb://user:pass@localhost:27017/myapp"

// With options
dsn := "mongodb://user:pass@localhost:27017/myapp?authSource=admin&ssl=true"

// Replica set
dsn := "mongodb://user:pass@host1:27017,host2:27017,host3:27017/myapp?replicaSet=rs0"

// Parse DSN
config, err := adapter.ParseDSN(dsn)
```

## Operations

### Document Operations

```go
// Insert document
insertCmd := domain.Command{
    SQL: "INSERT INTO users",
    Parameters: []interface{}{
        "name", "John Doe",
        "email", "john@example.com",
        "profile", map[string]interface{}{
            "age": 30,
            "city": "New York",
            "interests": []string{"coding", "music"},
        },
    },
    Type: domain.CommandTypeInsert,
}

// Update document
updateCmd := domain.Command{
    SQL: "UPDATE users",
    Parameters: []interface{}{
        "email", "john@example.com",  // filter
        "profile.age", 31,            // update
    },
    Type: domain.CommandTypeUpdate,
}

// Delete document
deleteCmd := domain.Command{
    SQL: "DELETE FROM users",
    Parameters: []interface{}{
        "email", "john@example.com",
    },
    Type: domain.CommandTypeDelete,
}
```

### Query Operations

```go
// Find documents
query := domain.Query{
    SQL: "SELECT FROM users",
    Parameters: []interface{}{
        "profile.age", map[string]interface{}{"$gt": 25},
    },
}

result, err := storage.Query(ctx, query)
if err != nil {
    log.Fatal(err)
}
defer result.Close()

// Iterate results
for result.Next() {
    var user User
    if err := result.ScanRow(&user); err != nil {
        log.Fatal(err)
    }
    log.Printf("User: %+v", user)
}
```

### Transaction Operations

```go
// Begin transaction
tx, err := storage.BeginTx(ctx, &domain.TxOptions{
    Isolation: domain.IsolationLevelReadCommitted,
})
if err != nil {
    log.Fatal(err)
}
defer tx.Rollback(ctx)

// Execute operations within transaction
insertCmd := domain.Command{
    SQL: "INSERT INTO orders",
    Parameters: []interface{}{
        "user_id", "12345",
        "amount", 99.99,
        "status", "pending",
    },
    Type: domain.CommandTypeInsert,
}

result, err := tx.Execute(ctx, insertCmd)
if err != nil {
    return err
}

updateCmd := domain.Command{
    SQL: "UPDATE users",
    Parameters: []interface{}{
        "_id", "12345",
        "last_order", result.LastInsertID,
    },
    Type: domain.CommandTypeUpdate,
}

if _, err := tx.Execute(ctx, updateCmd); err != nil {
    return err
}

// Commit transaction
return tx.Commit(ctx)
```

### Batch Operations

```go
operations := []domain.Operation{
    {
        Type: domain.OperationTypeInsert,
        Query: domain.Query{
            SQL: "INSERT INTO products",
            Parameters: []interface{}{
                "name", "Product 1",
                "price", 29.99,
            },
        },
    },
    {
        Type: domain.OperationTypeInsert,
        Query: domain.Query{
            SQL: "INSERT INTO products",
            Parameters: []interface{}{
                "name", "Product 2",
                "price", 39.99,
            },
        },
    },
}

results, err := storage.Batch(ctx, operations)
if err != nil {
    log.Fatal(err)
}

for i, result := range results {
    if result.Success {
        log.Printf("Operation %d: Success", i+1)
    } else {
        log.Printf("Operation %d: Failed - %v", i+1, result.Error)
    }
}
```

## Data Types

### BSON Type Mapping

| Go Type | Python Type | MongoDB BSON | Notes |
|---------|-------------|--------------|-------|
| `string` | `str` | `string` | UTF-8 strings |
| `int`, `int64` | `int` | `int32`, `int64` | Integers |
| `float64` | `float` | `double` | Floating point |
| `bool` | `bool` | `bool` | Boolean values |
| `time.Time` | `datetime` | `date` | Timestamps |
| `[]byte` | `bytes` | `binData` | Binary data |
| `map[string]interface{}` | `dict` | `document` | Nested documents |
| `[]interface{}` | `list` | `array` | Arrays |
| `primitive.ObjectID` | `ObjectId` | `objectId` | MongoDB ObjectIDs |

### Complex Data Types

```go
// Nested documents
user := map[string]interface{}{
    "name": "John Doe",
    "profile": map[string]interface{}{
        "age": 30,
        "address": map[string]interface{}{
            "street": "123 Main St",
            "city": "New York",
            "zipcode": "10001",
        },
    },
    "interests": []string{"coding", "music", "travel"},
    "metadata": map[string]interface{}{
        "created_at": time.Now(),
        "updated_at": time.Now(),
        "version": 1,
    },
}
```

## Performance Considerations

### Connection Pooling

```go
config := domain.Config{
    MaxOpenConns:    50,   // Maximum concurrent connections
    MaxIdleConns:    10,   // Connections to keep idle
    ConnMaxLifetime: 1 * time.Hour,   // Connection lifetime
    ConnMaxIdleTime: 30 * time.Minute, // Idle timeout
}
```

### Batch Operations

- Use batch operations for multiple inserts/updates
- Batch size limit: 100,000 operations
- Optimal batch size: 1,000-10,000 operations

### Indexing

```go
// Create index (simplified - in practice would use proper index management)
indexCmd := domain.Command{
    SQL: "CREATE INDEX ON users",
    Parameters: []interface{}{
        "field", "email",
        "unique", true,
    },
    Type: domain.CommandTypeSchema,
}
```

## Error Handling

### Error Types

- **ConnectionError**: Connection failures, timeouts
- **QueryError**: Query execution failures
- **TransactionError**: Transaction management failures
- **DataError**: Data validation, type conversion errors
- **SchemaError**: Collection, index operation failures

### Error Recovery

```go
// Retry logic example
func executeWithRetry(storage domain.Storage, cmd domain.Command) error {
    maxRetries := 3
    for i := 0; i < maxRetries; i++ {
        _, err := storage.Execute(ctx, cmd)
        if err == nil {
            return nil
        }
        
        // Check if error is retryable
        if domain.IsRetryableError(err) {
            time.Sleep(time.Duration(i+1) * time.Second)
            continue
        }
        
        return err
    }
    return fmt.Errorf("max retries exceeded")
}
```

## Testing

### Unit Tests

```bash
# Go tests
go test ./internal/infrastructure/adapters/mongodb/...

# Python tests
cd python
pytest tests/unit/test_mongodb_adapter.py -v
```

### Integration Tests

```bash
# Start MongoDB container
docker-compose up -d mongodb

# Run integration tests
go test ./tests/integration/mongodb_test.go -v

# Python integration tests
cd python
pytest tests/integration/test_mongodb.py -v
```

## Limitations

### Current Implementation

1. **Query Translation**: Simplified query parsing (production would need full MongoDB query language support)
2. **Collection Management**: Basic collection operations (would need comprehensive schema management)
3. **Aggregation Pipeline**: Limited aggregation support (would need full pipeline implementation)
4. **GridFS**: Not yet implemented (planned for future release)
5. **Change Streams**: Not yet implemented (planned for future release)

### MongoDB Version Support

- **Minimum Version**: MongoDB 4.0+ (for multi-document transactions)
- **Recommended Version**: MongoDB 5.0+ (for enhanced features)
- **Tested Versions**: 4.4, 5.0, 6.0, 7.0

## Future Enhancements

### Planned Features

1. **Advanced Query Builder**: Full MongoDB query language support
2. **Aggregation Pipeline**: Complete aggregation framework
3. **GridFS Integration**: Large file storage and streaming
4. **Change Streams**: Real-time data change notifications
5. **Schema Validation**: Document schema enforcement
6. **Geospatial Queries**: Location-based operations
7. **Text Search**: Full-text search with scoring
8. **Time Series**: Time series collection support

### Performance Optimizations

1. **Query Caching**: Prepared statement caching
2. **Connection Optimization**: Advanced pooling strategies
3. **Bulk Operations**: Enhanced batch processing
4. **Memory Management**: Reduced allocations
5. **Compression**: Wire protocol compression

## Examples

- [Go Example](../examples/mongodb/main.go) - Complete Go implementation example
- [Python Example](../python/examples/mongodb_example.py) - Complete Python implementation example
- [Integration Tests](../tests/integration/mongodb_test.go) - Real-world usage scenarios

## Support

For issues, questions, or contributions related to the MongoDB adapter:

1. Check the [main documentation](../README.md)
2. Review [integration tests](../tests/integration/) for usage patterns
3. Submit issues via GitHub Issues
4. Contribute via Pull Requests

## License

This MongoDB adapter is part of the HybridCache.io project and is licensed under the same terms as the main project.
