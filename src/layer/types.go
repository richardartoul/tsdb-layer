package layer

import (
	"time"

	"github.com/richardartoul/tsdb-layer/src/encoding"
)

type Write struct {
	ID        string
	Timestamp time.Time
	Value     float64
}

type Layer interface {
	Write(id string, timestamp time.Time, value float64) error
	WriteBatch(writes []Write) error
	Read(id string) (encoding.ReadableDecoder, error)
}
