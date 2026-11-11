package mongodb

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// MongoDB server error codes.
const (
	codeIndexNotFound   = 27
	codeNamespaceExists = 48
)

// MongoDBStorage implements the domain.Storage interface for MongoDB
type MongoDBStorage struct {
	client   *mongo.Client
	database *mongo.Database
	adapter  *Adapter
	config   domain.Config
	exec     *executor
}

func newStorage(client *mongo.Client, database string, adapter *Adapter, config domain.Config) *MongoDBStorage {
	db := client.Database(database)
	return &MongoDBStorage{
		client:   client,
		database: db,
		adapter:  adapter,
		config:   config,
		exec:     &executor{db: db, queryTimeout: config.QueryTimeout},
	}
}

// Connect re-verifies connectivity; Adapter.Connect returns a connected storage.
func (s *MongoDBStorage) Connect(ctx context.Context) error {
	return s.Ping(ctx)
}

// Close closes the MongoDB connection
func (s *MongoDBStorage) Close() error {
	if s.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.client.Disconnect(ctx)
	}
	return nil
}

// Ping tests the MongoDB connection
func (s *MongoDBStorage) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx, readpref.Primary()); err != nil {
		return domain.NewConnectionError("CONNECTION_FAILED", "failed to ping MongoDB").WithCause(err)
	}
	return nil
}

// Health pings the server and reports its version. Both commands are available
// to any user, unlike serverStatus.
func (s *MongoDBStorage) Health(ctx context.Context) domain.HealthStatus {
	status := domain.HealthStatus{
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}

	if err := s.Ping(ctx); err != nil {
		status.Status = domain.HealthStatusUnhealthy
		status.Message = fmt.Sprintf("MongoDB ping failed: %v", err)
		return status
	}

	var info struct {
		Version string `bson:"version"`
	}
	if err := s.database.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&info); err != nil {
		status.Status = domain.HealthStatusDegraded
		status.Message = fmt.Sprintf("MongoDB is reachable but buildInfo failed: %v", err)
		return status
	}
	status.Details["version"] = info.Version
	status.Status = domain.HealthStatusHealthy
	status.Message = "MongoDB is healthy"
	return status
}

// Query executes a builder-produced query (Query.Spec); raw SQL is rejected.
func (s *MongoDBStorage) Query(ctx context.Context, query domain.Query) (domain.Result, error) {
	return s.exec.query(ctx, query)
}

// QueryOne executes a query and returns its first row, or an error matching
// domain.ErrNoRows when there is none.
func (s *MongoDBStorage) QueryOne(ctx context.Context, query domain.Query) (domain.Row, error) {
	return s.exec.queryOne(ctx, query)
}

// Execute executes a builder-produced command (Command.Spec); raw SQL is rejected.
func (s *MongoDBStorage) Execute(ctx context.Context, command domain.Command) (domain.ExecuteResult, error) {
	return s.exec.execute(ctx, command)
}

// BeginTx starts a multi-document transaction (requires a replica set or
// sharded cluster). Reads in a transaction must use the primary, so ReadOnly
// does not change the read preference; it makes the transaction reject writes.
func (s *MongoDBStorage) BeginTx(ctx context.Context, opts *domain.TxOptions) (domain.Transaction, error) {
	var o domain.TxOptions
	if opts != nil {
		o = *opts
	}

	txnOpts := options.Transaction().SetReadPreference(readpref.Primary())
	switch o.Isolation {
	case domain.IsolationLevelReadCommitted:
		txnOpts.SetReadConcern(readconcern.Majority())
	case domain.IsolationLevelRepeatableRead, domain.IsolationLevelSerializable:
		// Snapshot is MongoDB's strongest transaction read concern; linearizable
		// is not allowed in transactions.
		txnOpts.SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority())
	}
	// Default and ReadUncommitted keep the server default read concern (local).

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = s.config.TxTimeout
	}
	var txCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		txCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		txCtx, cancel = context.WithCancel(ctx)
	}

	session, err := s.client.StartSession()
	if err != nil {
		cancel()
		return nil, domain.NewTransactionError("TRANSACTION_FAILED", "failed to start MongoDB session").WithCause(err)
	}
	if err := session.StartTransaction(txnOpts); err != nil {
		session.EndSession(txCtx)
		cancel()
		return nil, domain.NewTransactionError("TRANSACTION_FAILED", "failed to start MongoDB transaction").WithCause(err)
	}

	return &MongoDBTransaction{
		session:  session,
		exec:     s.exec,
		ctx:      txCtx,
		cancel:   cancel,
		id:       rand.Text(),
		readOnly: o.ReadOnly,
		active:   true,
	}, nil
}

// Batch executes the operations sequentially in order; each OperationResult
// carries that operation's result (materialised rows for queries) or error.
func (s *MongoDBStorage) Batch(ctx context.Context, operations []domain.Operation) ([]domain.OperationResult, error) {
	return s.exec.batch(ctx, operations, false), nil
}

// CreateTable creates a collection and its indexes. Columns and constraints
// are not enforced: MongoDB collections are schemaless.
func (s *MongoDBStorage) CreateTable(ctx context.Context, schema domain.TableSchema) error {
	if schema.Name == "" {
		return domain.NewSchemaError("INVALID_SCHEMA", "table name is required")
	}
	models := make([]mongo.IndexModel, 0, len(schema.Indexes))
	for _, def := range schema.Indexes {
		model, err := indexModel(def)
		if err != nil {
			return err
		}
		models = append(models, model)
	}

	// The server reports NamespaceExists only when the existing collection's
	// options differ, so check first: like CREATE TABLE, creating an existing
	// table is an error.
	exists, err := s.collectionExists(ctx, schema.Name)
	if err != nil {
		return err
	}
	if exists {
		return errSchemaExists(schema.Name)
	}
	if err := s.database.CreateCollection(ctx, schema.Name); err != nil {
		if hasErrorCode(err, codeNamespaceExists) {
			return errSchemaExists(schema.Name).WithCause(err)
		}
		return domain.NewSchemaError("CREATE_COLLECTION_FAILED", "failed to create collection").
			WithContext("table", schema.Name).WithCause(err)
	}
	if len(models) == 0 {
		return nil
	}
	coll := s.database.Collection(schema.Name)
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		// Leave no half-created table behind.
		_ = coll.Drop(ctx)
		return domain.NewSchemaError("CREATE_INDEX_FAILED", "failed to create indexes").
			WithContext("table", schema.Name).WithCause(err)
	}
	return nil
}

// DropTable drops a collection; dropping a missing collection is not an error.
func (s *MongoDBStorage) DropTable(ctx context.Context, tableName string) error {
	if err := s.database.Collection(tableName).Drop(ctx); err != nil {
		return domain.NewSchemaError("DROP_COLLECTION_FAILED", "failed to drop collection").
			WithContext("table", tableName).WithCause(err)
	}
	return nil
}

// AlterTable adds or drops indexes; MongoDB has no column or constraint changes.
// All changes are validated before any is applied.
func (s *MongoDBStorage) AlterTable(ctx context.Context, tableName string, changes []domain.SchemaChange) error {
	models := make([]mongo.IndexModel, len(changes))
	for i, change := range changes {
		switch change.Type {
		case domain.SchemaChangeTypeAddIndex:
			model, err := indexModel(change.Index)
			if err != nil {
				return err
			}
			models[i] = model
		case domain.SchemaChangeTypeDropIndex:
			if change.Index.Name == "" {
				return domain.NewSchemaError("INVALID_SCHEMA", "index name is required to drop an index")
			}
		default:
			return domain.NewUnsupportedOperationError(fmt.Sprintf("schema change type %d", change.Type))
		}
	}
	if err := s.requireCollection(ctx, tableName); err != nil {
		return err
	}

	indexes := s.database.Collection(tableName).Indexes()
	for i, change := range changes {
		if change.Type == domain.SchemaChangeTypeAddIndex {
			if _, err := indexes.CreateOne(ctx, models[i]); err != nil {
				return domain.NewSchemaError("ADD_INDEX_FAILED", "failed to create index").
					WithContext("table", tableName).WithCause(err)
			}
			continue
		}
		if err := indexes.DropOne(ctx, change.Index.Name); err != nil {
			if hasErrorCode(err, codeIndexNotFound) {
				return domain.NewSchemaError("INDEX_NOT_FOUND", "index not found").
					WithContext("index", change.Index.Name).WithCause(err)
			}
			return domain.NewSchemaError("DROP_INDEX_FAILED", "failed to drop index").
				WithContext("index", change.Index.Name).WithCause(err)
		}
	}
	return nil
}

// indexModel translates an index definition. Without a Name the server
// generates one (e.g. "email_1").
func indexModel(def domain.IndexDefinition) (mongo.IndexModel, error) {
	if len(def.Columns) == 0 {
		return mongo.IndexModel{}, domain.NewSchemaError("INVALID_SCHEMA", "index must have at least one column").
			WithContext("index", def.Name)
	}
	var key interface{}
	switch def.Type {
	case domain.IndexTypeBTree:
		key = 1
	case domain.IndexTypeHash:
		key = "hashed"
	default:
		return mongo.IndexModel{}, domain.NewUnsupportedOperationError(fmt.Sprintf("index type %d", def.Type))
	}
	keys := make(bson.D, 0, len(def.Columns))
	for _, c := range def.Columns {
		if err := checkField(c); err != nil {
			return mongo.IndexModel{}, err
		}
		keys = append(keys, bson.E{Key: c, Value: key})
	}
	opts := options.Index()
	if def.Name != "" {
		opts.SetName(def.Name)
	}
	if def.Unique {
		opts.SetUnique(true)
	}
	return mongo.IndexModel{Keys: keys, Options: opts}, nil
}

// ListTables lists all collections in the database
func (s *MongoDBStorage) ListTables(ctx context.Context) ([]string, error) {
	names, err := s.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, domain.NewSchemaError("LIST_COLLECTIONS_FAILED", "failed to list collections").WithCause(err)
	}
	return names, nil
}

// DescribeTable describes a collection's indexes (other than the _id index).
// Collections are schemaless, so no columns are reported.
func (s *MongoDBStorage) DescribeTable(ctx context.Context, tableName string) (domain.TableSchema, error) {
	if err := s.requireCollection(ctx, tableName); err != nil {
		return domain.TableSchema{}, err
	}
	specs, err := s.database.Collection(tableName).Indexes().ListSpecifications(ctx)
	if err != nil {
		return domain.TableSchema{}, domain.NewSchemaError("LIST_INDEXES_FAILED", "failed to list indexes").
			WithContext("table", tableName).WithCause(err)
	}

	schema := domain.TableSchema{
		Name:    tableName,
		Columns: []domain.ColumnDefinition{},
		Indexes: []domain.IndexDefinition{},
	}
	for _, spec := range specs {
		if spec.Name == "_id_" {
			continue
		}
		keys, err := spec.KeysDocument.Elements()
		if err != nil {
			return domain.TableSchema{}, domain.NewSchemaError("LIST_INDEXES_FAILED", "malformed index keys").
				WithContext("index", spec.Name).WithCause(err)
		}
		def := domain.IndexDefinition{
			Name:    spec.Name,
			Columns: make([]string, 0, len(keys)),
			Unique:  spec.Unique != nil && *spec.Unique,
		}
		for _, k := range keys {
			def.Columns = append(def.Columns, k.Key())
			if kind, ok := k.Value().StringValueOK(); ok && kind == "hashed" {
				def.Type = domain.IndexTypeHash
			}
		}
		schema.Indexes = append(schema.Indexes, def)
	}
	return schema, nil
}

func (s *MongoDBStorage) collectionExists(ctx context.Context, name string) (bool, error) {
	names, err := s.database.ListCollectionNames(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return false, domain.NewSchemaError("LIST_COLLECTIONS_FAILED", "failed to list collections").WithCause(err)
	}
	return len(names) > 0, nil
}

// requireCollection returns a TableNotFound error when the collection does not
// exist (MongoDB would otherwise create it implicitly or report nothing).
func (s *MongoDBStorage) requireCollection(ctx context.Context, name string) error {
	exists, err := s.collectionExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return domain.NewTableNotFoundError(name)
	}
	return nil
}

func errSchemaExists(table string) *domain.StorageError {
	return domain.NewSchemaError("SCHEMA_EXISTS", "collection already exists").WithContext("table", table)
}

// Info returns storage information
func (s *MongoDBStorage) Info() domain.StorageInfo {
	return domain.StorageInfo{
		Name:         s.adapter.Name(),
		Version:      s.adapter.Version(),
		DatabaseType: s.adapter.DatabaseType(),
		Features: []string{
			"transactions",
			"batch",
			"schema",
			"json",
			"arrays",
		},
		Limits: domain.StorageLimits{
			MaxConnections:    s.config.MaxOpenConns,
			MaxQuerySize:      16 * 1024 * 1024, // 16MB BSON document limit
			MaxTransactionAge: 1 * time.Minute,  // default transactionLifetimeLimitSeconds
			MaxBatchSize:      100000,           // maxWriteBatchSize
		},
	}
}
