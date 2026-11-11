package mongodb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AnandSGit/HybridCache.io/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// executor runs translated queries and commands against a database. Storage
// passes plain contexts; a transaction passes contexts that carry its session,
// so both share one implementation.
type executor struct {
	db           *mongo.Database
	queryTimeout time.Duration // used when Query/Command Options.Timeout is zero
}

// withTimeout bounds ctx by timeout, falling back to the configured query timeout.
func (e *executor) withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = e.queryTimeout
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (e *executor) query(ctx context.Context, q domain.Query) (domain.Result, error) {
	r, err := e.openResult(ctx, q)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// openResult returns a cursor-backed result for finds, and a materialised one
// for COUNT, DISTINCT and LIMIT 0. A cursor-backed result owns the query
// context until it is closed.
func (e *executor) openResult(ctx context.Context, q domain.Query) (*MongoDBResult, error) {
	plan, err := planQuery(q)
	if err != nil {
		return nil, err
	}
	if plan.none {
		return newDocumentResult(nil, plan.columns), nil
	}

	ctx, cancel := e.withTimeout(ctx, q.Options.Timeout)
	coll := e.db.Collection(plan.collection)
	if plan.kind != findQuery {
		defer cancel()
		docs, err := computedRows(ctx, coll, plan)
		if err != nil {
			return nil, err
		}
		return newDocumentResult(docs, plan.columns), nil
	}

	cursor, err := coll.Find(ctx, plan.filter, plan.findOptions())
	if err != nil {
		cancel()
		return nil, queryError(err, fmt.Sprintf("find on %q failed", plan.collection))
	}
	return newCursorResult(ctx, cancel, cursor, plan.columns), nil
}

func (e *executor) queryOne(ctx context.Context, q domain.Query) (domain.Row, error) {
	plan, err := planQuery(q)
	if err != nil {
		return nil, err
	}
	if plan.none {
		return nil, domain.NewNoRowsError()
	}

	ctx, cancel := e.withTimeout(ctx, q.Options.Timeout)
	defer cancel()
	coll := e.db.Collection(plan.collection)
	if plan.kind != findQuery {
		docs, err := computedRows(ctx, coll, plan)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			return nil, domain.NewNoRowsError()
		}
		return &MongoDBRow{document: docs[0], columns: plan.columns}, nil
	}

	doc, err := coll.FindOne(ctx, plan.filter, plan.findOneOptions()).Raw()
	if err != nil {
		return nil, queryError(err, fmt.Sprintf("find on %q failed", plan.collection))
	}
	return &MongoDBRow{document: doc, columns: plan.columns}, nil
}

// queryAll runs a query and materialises its documents, so that the result
// holds no cursor (used for batch operations).
func (e *executor) queryAll(ctx context.Context, q domain.Query) (domain.Result, error) {
	r, err := e.openResult(ctx, q)
	if err != nil {
		return nil, err
	}
	if r.cursor == nil {
		return r, nil
	}
	defer func() { _ = r.Close() }()
	var docs []bson.Raw
	for r.Next() {
		docs = append(docs, slices.Clone(r.current))
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return newDocumentResult(docs, r.columns), nil
}

// computedRows runs COUNT and DISTINCT plans, returning their rows as documents.
func computedRows(ctx context.Context, coll *mongo.Collection, plan *queryPlan) ([]bson.Raw, error) {
	if plan.kind == countQuery {
		n, err := coll.CountDocuments(ctx, plan.filter)
		if err != nil {
			return nil, queryError(err, fmt.Sprintf("count on %q failed", plan.collection))
		}
		doc, err := bson.Marshal(bson.D{{Key: countColumn, Value: n}})
		if err != nil {
			return nil, domain.NewDataError("INVALID_DATA", "cannot encode count").WithCause(err)
		}
		return []bson.Raw{doc}, nil
	}

	field := plan.columns[0]
	values, err := coll.Distinct(ctx, field, plan.filter).Raw()
	if err != nil {
		return nil, queryError(err, fmt.Sprintf("distinct on %q failed", plan.collection))
	}
	elems, err := values.Values()
	if err != nil {
		return nil, domain.NewDataError("INVALID_DATA", "malformed distinct result").WithCause(err)
	}
	docs := make([]bson.Raw, 0, len(elems))
	for _, v := range elems {
		doc, err := bson.Marshal(nestedDocument(field, v))
		if err != nil {
			return nil, domain.NewDataError("INVALID_DATA", "cannot encode distinct value").WithCause(err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// nestedDocument builds {a: {b: value}} for the path "a.b", matching the shape of
// a projected document so that rows can be scanned by field path.
func nestedDocument(path string, value interface{}) bson.D {
	keys := strings.Split(path, ".")
	doc := bson.D{{Key: keys[len(keys)-1], Value: value}}
	for i := len(keys) - 2; i >= 0; i-- {
		doc = bson.D{{Key: keys[i], Value: doc}}
	}
	return doc
}

func (e *executor) execute(ctx context.Context, c domain.Command) (domain.ExecuteResult, error) {
	plan, err := planCommand(c)
	if err != nil {
		return domain.ExecuteResult{}, err
	}

	ctx, cancel := e.withTimeout(ctx, c.Options.Timeout)
	defer cancel()
	coll := e.db.Collection(plan.collection)
	switch plan.kind {
	case domain.CommandTypeInsert:
		res, err := coll.InsertOne(ctx, plan.document)
		if err != nil {
			return domain.ExecuteResult{}, queryError(err, fmt.Sprintf("insert into %q failed", plan.collection))
		}
		return domain.ExecuteResult{RowsAffected: 1, LastInsertID: integerID(res.InsertedID)}, nil
	case domain.CommandTypeUpdate:
		res, err := coll.UpdateMany(ctx, plan.filter, plan.update())
		if err != nil {
			return domain.ExecuteResult{}, queryError(err, fmt.Sprintf("update of %q failed", plan.collection))
		}
		// SQL reports matched rows, even when the new values equal the old ones.
		return domain.ExecuteResult{RowsAffected: res.MatchedCount}, nil
	default:
		res, err := coll.DeleteMany(ctx, plan.filter)
		if err != nil {
			return domain.ExecuteResult{}, queryError(err, fmt.Sprintf("delete from %q failed", plan.collection))
		}
		return domain.ExecuteResult{RowsAffected: res.DeletedCount}, nil
	}
}

// batch runs the operations sequentially, in order, with the semantics of
// query and execute. Each result carries its own outcome: materialised rows for
// queries, an ExecuteResult for commands, or the operation's error.
func (e *executor) batch(ctx context.Context, ops []domain.Operation, readOnly bool) []domain.OperationResult {
	results := make([]domain.OperationResult, len(ops))
	for i, op := range ops {
		results[i].Index = i
		switch {
		case op.Type == domain.OperationTypeQuery:
			res, err := e.queryAll(ctx, op.Query)
			if err != nil {
				results[i].Error = err
			} else {
				results[i].Result = res
			}
		case op.Type == domain.OperationTypeCommand && readOnly:
			results[i].Error = errReadOnlyTransaction()
		case op.Type == domain.OperationTypeCommand:
			res, err := e.execute(ctx, op.Command)
			if err != nil {
				results[i].Error = err
			} else {
				results[i].Result = res
			}
		default:
			results[i].Error = domain.NewInvalidQueryError(fmt.Sprintf("unknown operation type %d", op.Type))
		}
	}
	return results
}

// integerID returns an inserted _id when it is an integer, else 0 (ObjectIDs
// and other _id types have no integer form).
func integerID(id interface{}) int64 {
	switch v := id.(type) {
	case int32:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

// queryError converts a driver error into a domain error.
func queryError(err error, message string) error {
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return domain.NewNoRowsError()
	case mongo.IsDuplicateKeyError(err):
		return domain.NewDuplicateKeyError("").WithCause(err)
	case mongo.IsTimeout(err):
		return domain.NewQueryTimeoutError(message).WithCause(err)
	case hasErrorLabel(err, "TransientTransactionError"):
		// e.g. a write conflict: the whole transaction can be retried.
		return domain.NewTransactionError("TRANSACTION_FAILED", message).
			WithCause(err).WithRetryable(true).WithTemporary(true)
	}
	return domain.NewQueryError("QUERY_FAILED", message).WithCause(err)
}

func hasErrorLabel(err error, label string) bool {
	var le mongo.LabeledError
	return errors.As(err, &le) && le.HasErrorLabel(label)
}

func hasErrorCode(err error, code int) bool {
	var se mongo.ServerError
	return errors.As(err, &se) && se.HasErrorCode(code)
}
