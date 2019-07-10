package dircompress

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

func NewLayer() layer.Layer {
	fdb.MustAPIVersion(610)
	// TODO(rartoul): Make this configurable.
	db := fdb.MustOpenDefault()
	return &directCompress{
		db: db,
	}
}

type directCompress struct {
	db fdb.Database
}

type timeSeriesMetadata struct {
	State    []byte
	LastByte byte
}

func (l *directCompress) Write(id string, timestamp time.Time, value float64) error {
	// TODO: Don't allocate
	return l.WriteBatch([]layer.Write{{ID: id, Timestamp: timestamp, Value: value}})
}

func (l *directCompress) WriteBatch(writes []layer.Write) error {
	_, err := l.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		metadataFutures := make([]fdb.FutureByteSlice, 0, len(writes))
		for _, w := range writes {
			metadataKey := newTimeseriesMetadataKeyFromID(w.ID)
			metadataFuture := tr.Get(metadataKey)
			metadataFutures = append(metadataFutures, metadataFuture)
		}

		for i, f := range metadataFutures {
			var (
				w           = writes[i]
				metadataKey = newTimeseriesMetadataKeyFromID(w.ID)
				// TODO: Error handling
				metadataBytes = f.MustGet()
				metaValue     timeSeriesMetadata
				dataAppend    []byte
				enc           = encoding.NewEncoder()
			)
			if len(metadataBytes) == 0 {
				// Never written.
				enc := encoding.NewEncoder()
				if err := enc.Encode(w.Timestamp, w.Value); err != nil {
					return nil, err
				}

				metaValue = timeSeriesMetadata{
					State: enc.State(),
				}

				// TODO: Should lastByte be set here too?
				b := enc.Bytes()
				if len(b) > 1 {
					dataAppend = enc.Bytes()[:len(b)-1]
				}
			} else {
				// TODO(rartoul): Don't use JSON.
				if err := json.Unmarshal(metadataBytes, &metaValue); err != nil {
					return nil, err
				}

				// Has been written before, restore encoder state.
				if err := enc.Restore(metaValue.State); err != nil {
					return nil, err
				}

				if err := enc.Encode(w.Timestamp, w.Value); err != nil {
					return nil, err
				}

				// Ensure new state gets persisted.
				var (
					newState = enc.State()
					b        = enc.Bytes()
				)
				if len(b) == 0 {
					return nil, errors.New("encoder bytes was length zero")
				}
				if len(b) == 1 {
					// The existing last byte was modified without adding any additional bytes. The last
					// byte is always tracked by the state so there is nothing to append here.
				}
				if len(b) > 1 {
					// The last byte will be kept track of by the state, but any bytes preceding it are
					// new "complete" bytes which should be appended to the compressed stream.
					dataAppend = b[:len(b)-1]
				}
				metaValue.LastByte = b[len(b)-1]
				metaValue.State = newState
			}

			// TODO(rartoul): Don't use JSON.
			newMetadataBytes, err := json.Marshal(&metaValue)
			if err != nil {
				return nil, err
			}

			tr.Set(metadataKey, newMetadataBytes)
			// TODO(rartoul): Ensure it fits and if not split into new keys.
			dataKey := newTimeseriesDataKeyFromID(w.ID)
			tr.AppendIfFits(dataKey, dataAppend)
		}

		return nil, nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (l *directCompress) Read(id string) (encoding.ReadableDecoder, error) {
	stream, err := l.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		var (
			metadataKey    = newTimeseriesMetadataKeyFromID(id)
			dataKey        = newTimeseriesDataKeyFromID(id)
			metadataFuture = tr.Get(metadataKey)
			dataFuture     = tr.Get(dataKey)
		)

		// TODO(rartoul): Proper error handling instead of Must()
		metadataBytes := metadataFuture.MustGet()
		dataBytes := dataFuture.MustGet()

		if len(metadataBytes) == 0 {
			// Does not exist.
			return nil, nil
		}

		var metaValue timeSeriesMetadata
		if err := json.Unmarshal(metadataBytes, &metaValue); err != nil {
			return nil, err
		}
		stream := append(dataBytes, metaValue.LastByte)
		return stream, nil
	})
	if err != nil {
		return nil, err
	}

	dec := encoding.NewDecoder()
	dec.Reset(stream.([]byte))
	return dec, nil
}

func newTimeseriesDataKeyFromID(id string) fdb.KeyConvertible {
	// TODO(rartoul): This function will need to be much more intelligent to handle
	// the fact that the data may be spread across multiple values.
	return fdb.Key(fmt.Sprintf("%s-data", id))
}

func newTimeseriesMetadataKeyFromID(id string) fdb.KeyConvertible {
	return fdb.Key(fmt.Sprintf("%s-metadata", id))
}
