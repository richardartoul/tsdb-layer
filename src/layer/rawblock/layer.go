package rawblock

import (
	"encoding/binary"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

func NewLayer() layer.Layer {
	fdb.MustAPIVersion(610)
	// TODO(rartoul): Make this configurable.
	db := fdb.MustOpenDefault()
	cl := NewCommitlog(db, NewCommitlogOptions())
	if err := cl.Open(); err != nil {
		// TODO(rartoul): Clean this up
		panic(err)
	}
	buffer := NewBuffer(db)

	// TODO(rartoul): Fix this and integrate the lifecycle with commitlog
	// truncation.
	go func() {
		for {
			time.Sleep(10 * time.Second)
			if err := buffer.Flush(); err != nil {
				log.Printf("error flushing buffer: %v", err)
			}
		}
	}()

	return &rawBlock{
		db:        db,
		cl:        cl,
		buffer:    buffer,
		bytesPool: newBytesPool(1024, 16000, 4096),
	}
}

type rawBlock struct {
	db        fdb.Database
	cl        Commitlog
	buffer    Buffer
	bytesPool *bytesPool
}

func (l *rawBlock) Write(id string, timestamp time.Time, value float64) error {
	// TODO: Don't allocate
	return l.WriteBatch([]layer.Write{{ID: id, Timestamp: timestamp, Value: value}})
}

func (l *rawBlock) WriteBatch(writes []layer.Write) error {
	if err := l.buffer.Write(writes); err != nil {
		return err
	}

	b := l.bytesPool.Get()
	for _, w := range writes {
		b = encodeWrite(b, w)
	}
	err := l.cl.Write(b)
	l.bytesPool.Put(b)

	return err
}

func (l *rawBlock) Read(id string) (encoding.Decoder, error) {
	return nil, errors.New("not-implemented")
}

// TODO(rartoul): Bucketized would be more efficient
type bytesPool struct {
	sync.Mutex
	pool             [][]byte
	size             int
	maxCapacity      int
	defaultAllocSize int
}

func newBytesPool(size, maxCapacity, defaultAllocSize int) *bytesPool {
	return &bytesPool{
		defaultAllocSize: defaultAllocSize,
		size:             size,
		maxCapacity:      maxCapacity,
	}
}

func (p *bytesPool) Get() []byte {
	p.Lock()
	var b []byte
	if len(p.pool) == 0 {
		b = make([]byte, 0, p.defaultAllocSize)
	} else {
		b = p.pool[len(p.pool)-1]
		p.pool = p.pool[:len(p.pool)-1]
	}
	p.Unlock()
	return b
}

func (p *bytesPool) Put(b []byte) {
	p.Lock()
	if len(p.pool) >= p.size || cap(b) > p.maxCapacity {
		p.Unlock()
		return
	}
	p.pool = append(p.pool, b[:0])
	p.Unlock()
}

// TODO: This needs to be length prefixed and all that other nice stuff so it can actually be decoded
func encodeWrite(b []byte, w layer.Write) []byte {
	b = append(b, w.ID...)
	binary.PutVarint(b, w.Timestamp.UnixNano())
	binary.PutUvarint(b, math.Float64bits(w.Value))
	return b
}
