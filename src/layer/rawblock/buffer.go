package rawblock

import (
	"sync"

	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

type Buffer interface {
	Write(writes []layer.Write) error
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
		if err := enc.Encode(w.Timestamp, w.Value); err != nil {
			return err
		}
	}

	return nil
}
