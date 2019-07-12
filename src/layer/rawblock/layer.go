package rawblock

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/richardartoul/tsdb-layer/src/encoding"
	"github.com/richardartoul/tsdb-layer/src/layer"
)

const (
	persistLoopInterval = 100 * time.Millisecond
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

	l := &rawBlock{
		db:        db,
		cl:        cl,
		buffer:    buffer,
		bytesPool: newBytesPool(1024, 16000, 4096),
	}
	go l.startPersistLoop()
	return l
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

func (l *rawBlock) Read(id string) (encoding.ReadableDecoder, error) {
	decoder, _, err := l.buffer.Read(id)
	return decoder, err
}

// TODO(rartoul): Add clean shutdown logic.
func (l *rawBlock) startPersistLoop() {
	for {
		// Prevent excessive activity when there are no incoming writes.
		time.Sleep(persistLoopInterval)

		truncToken, err := l.cl.WaitForRotation()
		if err != nil {
			log.Printf("error waiting for commitlog rotation: %v", err)
			continue
		}
		start := time.Now()
		if err := l.buffer.Flush(); err != nil {
			log.Printf("error flushing buffer: %v", err)
			continue
		}
		fmt.Println("flush took: ", time.Now().Sub(start))
		if err := l.cl.Truncate(truncToken); err != nil {
			log.Printf("error truncating commitlog: %v", err)
			continue
		}
	}
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
