package rawblock

import (
	"encoding/json"
	"errors"
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
)

type tsMetadata struct {
	chunks []chunkMetadata
}

func newTSMetadata() tsMetadata {
	return tsMetadata{}
}

type chunkMetadata struct {
	key       []byte
	first     time.Time
	last      time.Time
	sizeBytes int
}

func newChunkMetadata(key []byte, first, last time.Time, sizeBytes int) chunkMetadata {
	return chunkMetadata{
		key:       key,
		first:     first,
		last:      last,
		sizeBytes: sizeBytes,
	}
}

type Buffer interface {
	Write(writes []layer.Write) error
	Read(id string) (encoding.MultiDecoder, bool, error)
	Flush() error
}

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
	encoders, ok := b.encoders[id]
	if !ok {
		return nil, false, nil
	}

	decs := encodersToDecoders(encoders)
	multiDec := encoding.NewMultiDecoder()
	multiDec.Reset(decs)
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

func (b *buffer) Flush() error {
	b.Lock()
	for seriesID, encoders := range b.encoders {
		encoders = append(encoders, encoding.NewEncoder())
		encodersToFlush := encoders[:len(encoders)-1]
		b.encoders[seriesID] = encoders
		b.Unlock()
		if len(encodersToFlush) == 0 {
			b.Lock()
			continue
		}

		var (
			stream []byte
			err    error
		)
		if len(encodersToFlush) > 1 {
			streams := make([][]byte, 0, len(encodersToFlush))
			for _, enc := range encodersToFlush {
				streams = append(streams, enc.Bytes())
			}
			stream, err = encoding.MergeStreams(stream)
		} else {
			stream = encodersToFlush[0].Bytes()
		}
		if err != nil {
			return err
		}

		// Write to fdb.
		_, err = b.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
			metadataKey := metadataKey(seriesID)
			metaBytes, err := tr.Get(metadataKey).Get()
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

			var newChunkKey fdb.Key
			if len(metadata.chunks) == 0 {
				newChunkKey = tsChunkKey(seriesID, 0)
				metadata.chunks = append(metadata.chunks, newChunkMetadata(
					newChunkKey,
					time.Unix(0, 0), // TODO(rartoul): Fill this in.
					time.Unix(0, 0), // TODO(rartoul): Fill this in.
					len(stream),
				))
			} else {
				lastChunkIdx := len(metadata.chunks) - 1
				lastChunk := metadata.chunks[lastChunkIdx]
				// TODO(rartoul): Make compaction/merge logic more intelligent.
				if lastChunk.sizeBytes+len(stream) <= targetChunkSize {
					// Merge with last chunk.
					newChunkKey = fdb.Key(lastChunk.key)
					existingStream, err := tr.Get(newChunkKey).Get()
					if err != nil {
						return nil, err
					}
					stream, err = encoding.MergeStreams(existingStream, stream)
					if err != nil {
						return nil, err
					}
					// TODO(rartoul): Update first and last properties here as well.
					metadata.chunks[lastChunkIdx].sizeBytes = len(stream)
				} else {
					// Insert new chunk.
					newChunkKey = tsChunkKey(seriesID, lastChunkIdx)
					metadata.chunks = append(metadata.chunks, newChunkMetadata(
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
			tr.Set(metadataKey, newMetadataBytes)
			tr.Set(newChunkKey, stream)
			return nil, nil
		})
		if err != nil {
			return err
		}

		b.Lock()
		encoders, ok := b.encoders[seriesID]
		if !ok {
			return errors.New("could not retrieve encoders for recently flushed series")
		}
		// TODO(rartoul): This logic works right now because the only thing that can
		// trigger creating a new encoder for an existing series is a flush. Once there
		// is support for out-of-order writes, this logic will need to change.
		b.encoders[seriesID] = encoders[len(encoders)-1:]

		// Hold the lock for the next iteration.
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
