// Package sink writes documents into MongoDB using bulk upserts.
package sink

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Sink wraps a *mongo.Client bound to a single database.
type Sink struct {
	client *mongo.Client
	db     *mongo.Database
}

// Open connects to MongoDB and pings the deployment to verify reachability.
func Open(ctx context.Context, uri, database string) (*Sink, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongo: %w", err)
	}
	return &Sink{client: client, db: client.Database(database)}, nil
}

// Close terminates the MongoDB connection.
func (s *Sink) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Collection returns the *mongo.Collection for a given name.
func (s *Sink) Collection(name string) *mongo.Collection {
	return s.db.Collection(name)
}

// Truncate 移除指定 collection 內所有文件。
func (s *Sink) Truncate(ctx context.Context, collection string) (int64, error) {
	res, err := s.Collection(collection).DeleteMany(ctx, bson.D{})
	if err != nil {
		return 0, fmt.Errorf("truncate %s: %w", collection, err)
	}
	return res.DeletedCount, nil
}

// BulkUpserter 收集 documents 後以 ReplaceOne(upsert=true) 批次寫入。
type BulkUpserter struct {
	coll      *mongo.Collection
	keyField  string
	batchSize int
	buf       []mongo.WriteModel
}

// NewBulkUpserter 建立批次寫入器，當 buffer 達到 batchSize 會自動 flush。
func (s *Sink) NewBulkUpserter(collection, keyField string, batchSize int) *BulkUpserter {
	if batchSize <= 0 {
		batchSize = 1000
	}
	return &BulkUpserter{
		coll:      s.Collection(collection),
		keyField:  keyField,
		batchSize: batchSize,
		buf:       make([]mongo.WriteModel, 0, batchSize),
	}
}

// Add 將一份文件加入 buffer；若超過 batchSize 會自動 flush。
func (b *BulkUpserter) Add(ctx context.Context, doc map[string]any) error {
	key, ok := doc[b.keyField]
	if !ok {
		return fmt.Errorf("document missing upsert key %q", b.keyField)
	}
	model := mongo.NewReplaceOneModel().
		SetFilter(bson.D{{Key: b.keyField, Value: key}}).
		SetReplacement(doc).
		SetUpsert(true)
	b.buf = append(b.buf, model)
	if len(b.buf) >= b.batchSize {
		return b.Flush(ctx)
	}
	return nil
}

// Flush 立刻把 buffer 內的 writes 送到 MongoDB。
func (b *BulkUpserter) Flush(ctx context.Context) error {
	if len(b.buf) == 0 {
		return nil
	}
	// ordered=false 比較快且部分失敗時仍會盡量寫入其他文件。
	_, err := b.coll.BulkWrite(ctx, b.buf, options.BulkWrite().SetOrdered(false))
	b.buf = b.buf[:0]
	if err != nil {
		return fmt.Errorf("bulk write: %w", err)
	}
	return nil
}

// BulkInserter 批次 InsertMany，讓 MongoDB 自動產生 _id。
type BulkInserter struct {
	coll      *mongo.Collection
	batchSize int
	buf       []any
}

// NewBulkInserter 建立批次插入器。
func (s *Sink) NewBulkInserter(collection string, batchSize int) *BulkInserter {
	if batchSize <= 0 {
		batchSize = 500
	}
	return &BulkInserter{
		coll:      s.Collection(collection),
		batchSize: batchSize,
		buf:       make([]any, 0, batchSize),
	}
}

// Add 將文件加入 buffer，達 batchSize 時自動 flush。
func (b *BulkInserter) Add(ctx context.Context, doc map[string]any) error {
	b.buf = append(b.buf, doc)
	if len(b.buf) >= b.batchSize {
		return b.Flush(ctx)
	}
	return nil
}

// Flush 立刻 InsertMany。
func (b *BulkInserter) Flush(ctx context.Context) error {
	if len(b.buf) == 0 {
		return nil
	}
	_, err := b.coll.InsertMany(ctx, b.buf)
	b.buf = b.buf[:0]
	if err != nil {
		return fmt.Errorf("insert many: %w", err)
	}
	return nil
}
