package rawblock

import (
	"fmt"
	"sync"

	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

type Buffer interface {
	Write(writes []layer.Write) error
	Read(id string) (encoding.MultiDecoder, bool, error)
}

type buffer struct {
	sync.Mutex
	encoders map[string][]encoding.Encoder
}

func NewBuffer() Buffer {
	return &buffer{
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
