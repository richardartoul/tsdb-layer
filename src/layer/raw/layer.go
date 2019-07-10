package raw

import (
	"errors"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

func NewLayer() layer.Layer {
	fdb.MustAPIVersion(610)
	// TODO(rartoul): Make this configurable.
	db := fdb.MustOpenDefault()
	return &raw{
		db: db,
	}
}

type raw struct {
	db fdb.Database
}

func (l *raw) Write(id string, timestamp time.Time, value float64) error {
	// TODO: Don't allocate
	return l.WriteBatch([]layer.Write{{ID: id, Timestamp: timestamp, Value: value}})
}

func (l *raw) WriteBatch(writes []layer.Write) error {
	_, err := l.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		for _, w := range writes {
			key := tuple.Tuple{w.ID, w.Timestamp.UnixNano()}
			tr.Set(key, tuple.Tuple{w.Value}.Pack())
		}
		return nil, nil
	})

	return err
}

func (l *raw) Read(id string) (encoding.ReadableDecoder, error) {
	return nil, errors.New("not-implemented")
}
