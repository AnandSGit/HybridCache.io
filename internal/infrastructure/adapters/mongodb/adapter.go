// Package mongodb provides MongoDB adapter for the storage library
package mongodb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Adapter implements the domain.Adapter interface for MongoDB
type Adapter struct {
	name    string
	version string
}

// NewAdapter creates a new MongoDB adapter
func NewAdapter() domain.Adapter {
	return &Adapter{
		name:    "mongodb",
		version: "1.0.0",
	}
}

// Name returns the adapter name
func (a *Adapter) Name() string {
	return a.name
}

// Version returns the adapter version
func (a *Adapter) Version() string {
	return a.version
}

// DatabaseType returns the database type
func (a *Adapter) DatabaseType() domain.DatabaseType {
	return domain.DatabaseTypeMongoDB
}

// Connect creates a new storage instance
func (a *Adapter) Connect(ctx context.Context, config domain.Config) (domain.Storage, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, domain.NewConnectionError("INVALID_CONFIG", err.Error()).WithCause(err)
	}

	uri, err := a.buildURI(config)
	if err != nil {
		return nil, domain.NewConnectionError("INVALID_URI", "failed to build MongoDB URI").WithCause(err)
	}

	clientOptions := options.Client().ApplyURI(uri)

	// Configure connection pool
	clientOptions.SetMaxPoolSize(uint64(config.MaxOpenConns))
	clientOptions.SetMinPoolSize(uint64(config.MaxIdleConns))
	clientOptions.SetMaxConnIdleTime(config.ConnMaxIdleTime)
	clientOptions.SetConnectTimeout(config.ConnectTimeout)
	clientOptions.SetSocketTimeout(config.QueryTimeout)

	// Create MongoDB client
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, domain.NewConnectionError("CONNECTION_FAILED", "failed to connect to MongoDB").WithCause(err)
	}

	// Test the connection
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		client.Disconnect(ctx)
		return nil, domain.NewConnectionError("PING_FAILED", "failed to ping MongoDB").WithCause(err)
	}

	storage := &MongoDBStorage{
		client:   client,
		database: client.Database(config.Database),
		adapter:  a,
		config:   config,
	}

	return storage, nil
}

// ParseDSN parses a MongoDB connection string
func (a *Adapter) ParseDSN(dsn string) (domain.Config, error) {
	config := domain.Config{
		DSN: dsn,
	}

	// Parse MongoDB URI
	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid MongoDB URI: %w", err)
	}

	// Extract basic connection info
	if u.Hostname() != "" {
		config.Host = u.Hostname()
	}
	if u.Port() != "" {
		if port, err := strconv.Atoi(u.Port()); err == nil {
			config.Port = port
		}
	}
	if u.User != nil {
		config.Username = u.User.Username()
		if password, ok := u.User.Password(); ok {
			config.Password = password
		}
	}

	// Extract database name from path
	if len(u.Path) > 1 {
		config.Database = strings.TrimPrefix(u.Path, "/")
	}

	// Parse query parameters
	params := u.Query()
	if len(params) > 0 {
		if config.Options == nil {
			config.Options = make(map[string]interface{})
		}
		for key, values := range params {
			if len(values) > 0 {
				config.Options[key] = values[0]
			}
		}
	}

	// Set defaults
	if config.Port == 0 {
		config.Port = 27017
	}
	if config.MaxOpenConns == 0 {
		config.MaxOpenConns = 100
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 10
	}
	if config.ConnMaxLifetime == 0 {
		config.ConnMaxLifetime = 1 * time.Hour
	}
	if config.ConnMaxIdleTime == 0 {
		config.ConnMaxIdleTime = 30 * time.Minute
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 30 * time.Second
	}

	return config, nil
}

// ValidateConfig validates the configuration
func (a *Adapter) ValidateConfig(config domain.Config) error {
	if config.DSN == "" {
		if config.Host == "" {
			return fmt.Errorf("host is required")
		}
		if config.Database == "" {
			return fmt.Errorf("database is required")
		}
	}

	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be positive")
	}

	if config.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative")
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("max idle connections cannot exceed max open connections")
	}

	return nil
}

// buildURI builds a MongoDB connection URI from config
func (a *Adapter) buildURI(config domain.Config) (string, error) {
	if config.DSN != "" {
		return config.DSN, nil
	}

	// Build URI from individual components
	var uri strings.Builder
	uri.WriteString("mongodb://")

	// Add authentication if provided
	if config.Username != "" {
		uri.WriteString(url.QueryEscape(config.Username))
		if config.Password != "" {
			uri.WriteString(":")
			uri.WriteString(url.QueryEscape(config.Password))
		}
		uri.WriteString("@")
	}

	// Add host and port
	uri.WriteString(config.Host)
	if config.Port != 0 && config.Port != 27017 {
		uri.WriteString(":")
		uri.WriteString(strconv.Itoa(config.Port))
	}

	// Add database
	if config.Database != "" {
		uri.WriteString("/")
		uri.WriteString(config.Database)
	}

	// Add options
	if len(config.Options) > 0 {
		uri.WriteString("?")
		var params []string
		for key, value := range config.Options {
			params = append(params, fmt.Sprintf("%s=%s", key, url.QueryEscape(fmt.Sprintf("%v", value))))
		}
		uri.WriteString(strings.Join(params, "&"))
	}

	return uri.String(), nil
}

// TranslateQuery translates a domain.Query to MongoDB operations
func (a *Adapter) TranslateQuery(query domain.Query) (string, []interface{}, error) {
	// MongoDB doesn't use SQL, so we'll return the query as-is for now
	// In a full implementation, this would translate SQL-like queries to MongoDB operations
	return query.SQL, query.Parameters, nil
}

// TranslateCommand translates a domain.Command to MongoDB operations
func (a *Adapter) TranslateCommand(command domain.Command) (string, []interface{}, error) {
	// MongoDB doesn't use SQL, so we'll return the command as-is for now
	// In a full implementation, this would translate SQL-like commands to MongoDB operations
	return command.SQL, command.Parameters, nil
}

// MapGoType maps Go types to MongoDB data types
func (a *Adapter) MapGoType(goType interface{}) (domain.DataType, error) {
	switch goType.(type) {
	case string:
		return domain.DataTypeString, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return domain.DataTypeInteger, nil
	case float32, float64:
		return domain.DataTypeFloat, nil
	case bool:
		return domain.DataTypeBoolean, nil
	case time.Time:
		return domain.DataTypeDateTime, nil
	case []byte:
		return domain.DataTypeBinary, nil
	case map[string]interface{}, []interface{}:
		return domain.DataTypeJSON, nil
	default:
		return domain.DataTypeUnknown, fmt.Errorf("unsupported Go type: %T", goType)
	}
}

// MapDatabaseType maps MongoDB BSON types to domain data types
func (a *Adapter) MapDatabaseType(dbType string) (domain.DataType, error) {
	switch strings.ToLower(dbType) {
	case "string":
		return domain.DataTypeString, nil
	case "int", "long", "int32", "int64":
		return domain.DataTypeInteger, nil
	case "double", "decimal":
		return domain.DataTypeFloat, nil
	case "bool", "boolean":
		return domain.DataTypeBoolean, nil
	case "date", "timestamp":
		return domain.DataTypeDateTime, nil
	case "bindata", "binary":
		return domain.DataTypeBinary, nil
	case "object", "document":
		return domain.DataTypeJSON, nil
	case "array":
		return domain.DataTypeArray, nil
	case "objectid":
		return domain.DataTypeString, nil // ObjectID as string
	default:
		return domain.DataTypeUnknown, fmt.Errorf("unsupported MongoDB type: %s", dbType)
	}
}

// SupportsTransactions returns whether MongoDB supports transactions
func (a *Adapter) SupportsTransactions() bool {
	return true // MongoDB 4.0+ supports multi-document transactions
}

// SupportsJoins returns whether MongoDB supports joins
func (a *Adapter) SupportsJoins() bool {
	return true // MongoDB supports $lookup aggregation for joins
}

// SupportsBatch returns whether MongoDB supports batch operations
func (a *Adapter) SupportsBatch() bool {
	return true
}

// SupportsSchema returns whether MongoDB supports schema operations
func (a *Adapter) SupportsSchema() bool {
	return true // MongoDB supports collections and indexes
}
