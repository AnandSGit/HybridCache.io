// Package mongodb provides MongoDB adapter for the storage library
package mongodb

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// databaseTypes maps MongoDB type names (the $type aliases, lowercased, plus a
// few common synonyms) to domain data types.
var databaseTypes = map[string]domain.DataType{
	"string":    domain.DataTypeString,
	"int":       domain.DataTypeInteger,
	"long":      domain.DataTypeInteger,
	"int32":     domain.DataTypeInteger,
	"int64":     domain.DataTypeInteger,
	"double":    domain.DataTypeFloat,
	"decimal":   domain.DataTypeDecimal,
	"bool":      domain.DataTypeBoolean,
	"boolean":   domain.DataTypeBoolean,
	"date":      domain.DataTypeDateTime,
	"timestamp": domain.DataTypeDateTime,
	"bindata":   domain.DataTypeBinary,
	"binary":    domain.DataTypeBinary,
	"object":    domain.DataTypeJSON,
	"document":  domain.DataTypeJSON,
	"array":     domain.DataTypeArray,
	"objectid":  domain.DataTypeString, // ObjectID as string
}

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

// Connect creates a connected storage instance. Pool and timeout settings are
// applied only when set, so that URI options and driver defaults stay in effect.
func (a *Adapter) Connect(ctx context.Context, config domain.Config) (domain.Storage, error) {
	if err := a.ValidateConfig(config); err != nil {
		return nil, err
	}
	database := config.Database
	if database == "" {
		database = databaseFromDSN(config.DSN)
	}

	clientOptions := options.Client().ApplyURI(a.buildURI(config))
	if config.MaxOpenConns > 0 {
		clientOptions.SetMaxPoolSize(uint64(config.MaxOpenConns))
	}
	if config.MaxIdleConns > 0 {
		clientOptions.SetMinPoolSize(uint64(config.MaxIdleConns))
	}
	if config.ConnMaxIdleTime > 0 {
		clientOptions.SetMaxConnIdleTime(config.ConnMaxIdleTime)
	}
	if config.ConnectTimeout > 0 {
		clientOptions.SetConnectTimeout(config.ConnectTimeout)
	}

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, domain.NewConnectionError("CONNECTION_FAILED", "failed to connect to MongoDB").WithCause(err)
	}

	pingCtx := ctx
	if config.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		pingCtx, cancel = context.WithTimeout(ctx, config.ConnectTimeout)
		defer cancel()
	}
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		if mongo.IsTimeout(err) {
			return nil, domain.NewConnectionTimeoutError("timed out connecting to MongoDB").WithCause(err)
		}
		return nil, domain.NewConnectionError("CONNECTION_FAILED", "failed to ping MongoDB").WithCause(err)
	}

	return newStorage(client, database, a, config), nil
}

// ParseDSN parses a MongoDB connection string. The DSN is kept for Connect;
// pool settings are left zero so that the driver defaults apply.
func (a *Adapter) ParseDSN(dsn string) (domain.Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return domain.Config{}, invalidDSN("invalid MongoDB URI").WithCause(err)
	}
	if u.Scheme != "mongodb" && u.Scheme != "mongodb+srv" {
		return domain.Config{}, invalidDSN(`MongoDB URI scheme must be "mongodb" or "mongodb+srv"`)
	}

	config := domain.Config{
		DSN:      dsn,
		Host:     u.Hostname(),
		Database: strings.TrimPrefix(u.Path, "/"),
	}
	if port, err := strconv.Atoi(u.Port()); err == nil {
		config.Port = port
	} else if u.Scheme == "mongodb" {
		config.Port = 27017
	}
	if u.User != nil {
		config.Username = u.User.Username()
		config.Password, _ = u.User.Password()
	}
	if params := u.Query(); len(params) > 0 {
		config.Options = make(map[string]interface{}, len(params))
		for key, values := range params {
			config.Options[key] = values[0]
		}
	}
	return config, nil
}

// ValidateConfig validates the configuration. Zero pool sizes select the
// driver defaults.
func (a *Adapter) ValidateConfig(config domain.Config) error {
	switch {
	case config.DSN == "" && config.Host == "":
		return invalidConfig("host is required")
	case config.Database == "" && databaseFromDSN(config.DSN) == "":
		return invalidConfig("database is required")
	case config.MaxOpenConns < 0:
		return invalidConfig("max open connections cannot be negative")
	case config.MaxIdleConns < 0:
		return invalidConfig("max idle connections cannot be negative")
	case config.MaxOpenConns > 0 && config.MaxIdleConns > config.MaxOpenConns:
		return invalidConfig("max idle connections cannot exceed max open connections")
	}
	return nil
}

func invalidConfig(message string) *domain.StorageError {
	return domain.NewStorageError(domain.ErrorTypeValidation, "INVALID_CONFIG", message)
}

func invalidDSN(message string) *domain.StorageError {
	return domain.NewStorageError(domain.ErrorTypeValidation, "INVALID_DSN", message)
}

// databaseFromDSN returns the database named in a MongoDB URI's path, if any.
func databaseFromDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// buildURI builds a MongoDB connection URI from config. The database is not put
// in the path, where it would also become the authentication database; Connect
// selects it with client.Database instead.
func (a *Adapter) buildURI(config domain.Config) string {
	if config.DSN != "" {
		return config.DSN
	}

	host := strings.TrimSuffix(strings.TrimPrefix(config.Host, "["), "]")
	switch {
	case config.Port > 0:
		host = net.JoinHostPort(host, strconv.Itoa(config.Port))
	case strings.Contains(host, ":"):
		host = "[" + host + "]" // IPv6 literal
	}
	u := url.URL{Scheme: "mongodb", Host: host, Path: "/"}
	if config.Username != "" {
		if config.Password != "" {
			u.User = url.UserPassword(config.Username, config.Password)
		} else {
			u.User = url.User(config.Username)
		}
	}
	if len(config.Options) > 0 {
		query := make(url.Values, len(config.Options))
		for key, value := range config.Options {
			query.Set(key, fmt.Sprint(value))
		}
		u.RawQuery = query.Encode() // sorted by key
	}
	return u.String()
}

// TranslateQuery renders the MongoDB operation a builder-produced query runs as
// (for logging and debugging; parameters are inlined, so none are returned).
// Raw SQL cannot be translated.
func (a *Adapter) TranslateQuery(query domain.Query) (string, []interface{}, error) {
	plan, err := planQuery(query)
	if err != nil {
		return "", nil, err
	}
	s, err := plan.render()
	return s, nil, err
}

// TranslateCommand renders the MongoDB operation a builder-produced command runs
// as (see TranslateQuery).
func (a *Adapter) TranslateCommand(command domain.Command) (string, []interface{}, error) {
	plan, err := planCommand(command)
	if err != nil {
		return "", nil, err
	}
	s, err := plan.render()
	return s, nil, err
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

// MapDatabaseType maps MongoDB BSON type names to domain data types
func (a *Adapter) MapDatabaseType(dbType string) (domain.DataType, error) {
	return mapDatabaseType(dbType)
}

func mapDatabaseType(dbType string) (domain.DataType, error) {
	if dt, ok := databaseTypes[strings.ToLower(dbType)]; ok {
		return dt, nil
	}
	return domain.DataTypeUnknown, fmt.Errorf("unsupported MongoDB type: %s", dbType)
}

// SupportsTransactions returns whether MongoDB supports transactions
func (a *Adapter) SupportsTransactions() bool {
	return true // MongoDB 4.0+ supports multi-document transactions
}

// SupportsJoins reports false: queries with joins are rejected as unsupported.
func (a *Adapter) SupportsJoins() bool {
	return false
}

// SupportsBatch returns whether MongoDB supports batch operations
func (a *Adapter) SupportsBatch() bool {
	return true
}

// SupportsSchema returns whether MongoDB supports schema operations
func (a *Adapter) SupportsSchema() bool {
	return true // MongoDB supports collections and indexes
}
