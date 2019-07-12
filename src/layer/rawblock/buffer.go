package rawblock

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

const (
	bufferKeyPrefix    = "b-"
	metadataKeyPostfix = "-meta"
	tsChunkKeyPrefix   = "-chunk-"

	targetChunkSize = 4096
	flushBatchSize  = 128
)

type tsMetadata struct {
	Chunks []chunkMetadata
}

func newTSMetadata() tsMetadata {
	return tsMetadata{}
}

type chunkMetadata struct {
	Key       []byte
	First     time.Time
	Last      time.Time
	SizeBytes int
}

func newChunkMetadata(key []byte, first, last time.Time, sizeBytes int) chunkMetadata {
	return chunkMetadata{
		Key:       key,
		First:     first,
		Last:      last,
		SizeBytes: sizeBytes,
	}
}

type Buffer interface {
	Write(writes []layer.Write) error
	Read(id string) (encoding.MultiDecoder, bool, error)
	Flush() error
}

// TODO(rartoul): This entire thing needs to be refactored to support creating
// new encoders (not just during flush) so that encoders can be split when:
// 1. An existing encoder gets too big (so we don't end up with huge streams
//    that later need to be broken up during flush into smaller streams)
// 2. An out-of-order write comes in.
type buffer struct {
	sync.Mutex
	db       fdb.Database
	encoders map[string][]encoding.Encoder
}

func NewBuffer(db fdb.Database) Buffer {
	return &buffer{
		db:       db,
		encoders: map[string][]encoding.Encoder{},
	}
}

// TODO(rartoul): This should split up writes into a new encoder once the existing
// encoder has reached a certain size so that a given stream cant grow too large
// inbetween flushes (which is an issue because fdb has maximum sizes for a given
// value).
// TODO(rartoul): Should have per-write error handling.
func (b *buffer) Write(writes []layer.Write) error {
	b.Lock()
	defer b.Unlock()

	for _, w := range writes {
		encoders, ok := b.encoders[w.ID]
		if !ok {
			encoders = []encoding.Encoder{encoding.NewEncoder()}
			b.encoders[w.ID] = encoders
		}

		enc := encoders[len(encoders)-1]
		lastT, _, hasWrittenAnyValues := enc.LastEncoded()
		if hasWrittenAnyValues {
			if w.Timestamp.Before(lastT) {
				// TODO(rartoul): Remove this restriction with multiple encoders.
				return fmt.Errorf(
					"cannot write data out of order, series: %s, prevTimestamp: %s, currTimestamp: %s",
					w.ID, lastT.String(), w.Timestamp.String())
			}
			if w.Timestamp.Equal(lastT) {
				return fmt.Errorf(
					"cannot upsert existing values, series: %s, currTimestamp: %s",
					w.ID, lastT.String())
			}
		}

		if err := enc.Encode(w.Timestamp, w.Value); err != nil {
			return err
		}
	}

	return nil
}

func (b *buffer) Read(id string) (encoding.MultiDecoder, bool, error) {
	var decoders []encoding.Decoder
	_, err := b.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		metadataKey := metadataKey(id)
		metaBytes, err := tr.Get(metadataKey).Get()
		if err != nil {
			return nil, err
		}
		if metaBytes == nil {
			return nil, nil
		}

		var metadata tsMetadata
		if err := json.Unmarshal(metaBytes, &metadata); err != nil {
			return nil, err
		}

		for _, chunk := range metadata.Chunks {
			chunkBytes, err := tr.Get(fdb.Key(chunk.Key)).Get()
			if err != nil {
				return nil, err
			}
			dec := encoding.NewDecoder()
			dec.Reset(chunkBytes)
			decoders = append(decoders, dec)
		}
		return nil, nil
	})
	if err != nil {
		return nil, false, err
	}

	encoders, ok := b.encoders[id]
	if ok {
		decoders = append(decoders, encodersToDecoders(encoders)...)
	}

	if len(decoders) == 0 {
		return nil, false, nil
	}

	multiDec := encoding.NewMultiDecoder()
	multiDec.Reset(decoders)
	return multiDec, true, nil
}

func encodersToDecoders(encs []encoding.Encoder) []encoding.Decoder {
	decs := make([]encoding.Decoder, 0, len(encs))
	for _, enc := range encs {
		dec := encoding.NewDecoder()
		dec.Reset(enc.Bytes())
		decs = append(decs, dec)
	}
	return decs
}

// TODO(rartoul): Instead of performing one transaction per series it would be more efficient
// to collect "batches" of series and then write them all together in one fdb transaction.
func (b *buffer) Flush() error {
	// Manually control locking so map can be iterated while still being concurrently
	// accessed.
	b.Lock()

	var pendingFlush []toFlush
	for seriesID, encoders := range b.encoders {
		if len(encoders) == 0 {
			continue
		}

		// Append a new encoder to the list of existing encoders. Only the last encoder
		// in the list is ever written to so this effectively renders all previous
		// encoders immutable which can be taken advantage of to flush them without
		// holding a lock on the entire map.
		encoders = append(encoders, encoding.NewEncoder())
		encodersToFlush := encoders[:len(encoders)-1]
		b.encoders[seriesID] = encoders

		var streams [][]byte
		for _, enc := range encodersToFlush {
			streams = append(streams, enc.Bytes())
		}
		pendingFlush = append(pendingFlush, toFlush{
			id:      seriesID,
			streams: streams,
		})

		if len(pendingFlush) < flushBatchSize {
			continue
		}

		b.Unlock()
		if err := b.flush(pendingFlush); err != nil {
			return err
		}
		pendingFlush = pendingFlush[:0]

		// Hold the lock for the next iteration.
		b.Lock()
	}
	b.Unlock()
	if err := b.flush(pendingFlush); err != nil {
		return err
	}
	return nil
}

type toFlush struct {
	id      string
	streams [][]byte
}

func (b *buffer) flush(toFlush []toFlush) error {
	if len(toFlush) == 0 {
		return nil
	}

	_, err := b.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		var metadataFutures []fdb.FutureByteSlice
		// Start parallel fetches for each metadata.
		for _, series := range toFlush {
			metadataKey := metadataKey(series.id)
			metadataFuture := tr.Get(metadataKey)
			metadataFutures = append(metadataFutures, metadataFuture)
		}

		for i, series := range toFlush {
			metaBytes, err := metadataFutures[i].Get()
			if err != nil {
				return nil, err
			}

			var metadata tsMetadata
			if metaBytes == nil {
				metadata = newTSMetadata()
			} else {
				// TODO(rartoul): Don't use JSON.
				if err := json.Unmarshal(metaBytes, &metadata); err != nil {
					return nil, err
				}
			}

			stream, err := encoding.MergeStreams(series.streams...)
			if err != nil {
				return nil, err
			}

			var newChunkKey fdb.Key
			if len(metadata.Chunks) == 0 {
				newChunkKey = tsChunkKey(series.id, 0)
				metadata.Chunks = append(metadata.Chunks, newChunkMetadata(
					newChunkKey,
					time.Unix(0, 0), // TODO(rartoul): Fill this in.
					time.Unix(0, 0), // TODO(rartoul): Fill this in.
					len(stream),
				))
			} else {
				lastChunkIdx := len(metadata.Chunks) - 1
				lastChunk := metadata.Chunks[lastChunkIdx]
				// TODO(rartoul): Make compaction/merge logic more intelligent.
				if lastChunk.SizeBytes+len(stream) <= targetChunkSize {
					// Merge with last chunk.
					newChunkKey = fdb.Key(lastChunk.Key)
					// TODO(rartoul): This is inefficient because it forces a synchronous wait
					// on a read from fdb. This should be refactored so that all of the chunks
					// that need to be read can be fetched in parallel similar to how the metadata
					// futures are fetched in parallel above.
					existingStream, err := tr.Get(newChunkKey).Get()
					if err != nil {
						return nil, err
					}
					stream, err = encoding.MergeStreams(existingStream, stream)
					if err != nil {
						return nil, err
					}
					// TODO(rartoul): Update first and last properties here as well.
					metadata.Chunks[lastChunkIdx].SizeBytes = len(stream)
				} else {
					// Insert new chunk.
					newChunkKey = tsChunkKey(series.id, lastChunkIdx)
					metadata.Chunks = append(metadata.Chunks, newChunkMetadata(
						newChunkKey,
						time.Unix(0, 0), // TODO(rartoul): Fill this in.
						time.Unix(0, 0), // TODO(rartoul): Fill this in.
						len(stream),
					))
				}
			}

			newMetadataBytes, err := json.Marshal(metadata)
			if err != nil {
				return nil, err
			}

			metadataKey := metadataKey(series.id)
			tr.Set(metadataKey, newMetadataBytes)
			tr.Set(newChunkKey, stream)
		}
		return nil, nil
	})
	if err != nil {
		return err
	}

	b.Lock()
	defer b.Unlock()
	for _, series := range toFlush {
		encoders, ok := b.encoders[series.id]
		if !ok {
			return fmt.Errorf("flushed series %s which does not exist in encoders", series.id)
		}

		// Now that all of the immutable encoders have been flushed, they can be removed
		// from the list of existing encoders because they can now be read from FDB directly.
		//
		// TODO(rartoul): This logic works right now because the only thing that can
		// trigger creating a new encoder for an existing series is a flush and because flushing
		// is single-threaded. Once there is support for out-of-order writes, this logic will need
		// to change since there will be no way to determine if all of the encoder except the last
		// have been flushed yet (or could just force out of order writes to merge on demand?).
		b.encoders[series.id] = encoders[len(encoders)-1:]
	}
	return nil
}

func metadataKey(id string) fdb.Key {
	// TODO(rartoul): Not sure if this is ideal key structure/
	return tuple.Tuple{bufferKeyPrefix, id, metadataKeyPostfix}.Pack()
}

func tsChunkKey(id string, chunkNum int) fdb.Key {
	return tuple.Tuple{bufferKeyPrefix, id, tsChunkKeyPrefix, chunkNum}.Pack()
}
