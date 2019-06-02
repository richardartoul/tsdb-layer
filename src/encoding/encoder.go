package encoding

import (
	"time"

	"github.com/m3db/m3/src/dbnode/encoding"
	"github.com/m3db/m3/src/dbnode/encoding/m3tsz"
	xtime "github.com/m3db/m3/src/x/time"
)

var (
	// TODO(rartoul): Eliminate the need for this.
	opts = encoding.NewOptions()
)

type Encoder interface {
	Encode(timestamp time.Time, value float64) error
	// State() []byte
	// Restore(b []byte) error
	Bytes() []byte
}

type encoder struct {
	tsEncoder    m3tsz.TimestampEncoder
	floatEncoder m3tsz.FloatEncoderAndIterator
	stream       encoding.OStream

	hasWrittenFirst bool
}

// NewEncoder creates a new encoder.
func NewEncoder() Encoder {
	return &encoder{}
}

func (e *encoder) Encode(timestamp time.Time, value float64) error {
	if e.stream == nil {
		// Lazy init.
		e.stream = encoding.NewOStream(nil, false, nil)
		e.tsEncoder = m3tsz.NewTimestampEncoder(timestamp, xtime.Nanosecond, opts)
	}

	e.stream.WriteBit(hasMoreBit)

	var err error
	if !e.hasWrittenFirst {
		err = e.tsEncoder.WriteFirstTime(e.stream, timestamp, nil, xtime.Nanosecond)
	} else {
		err = e.tsEncoder.WriteNextTime(e.stream, timestamp, nil, xtime.Nanosecond)
	}
	if err != nil {
		return err
	}

	e.floatEncoder.WriteFloat(e.stream, value)
	e.hasWrittenFirst = true
	return nil
}

func (e *encoder) Bytes() []byte {
	if e.stream == nil {
		return nil
	}

	b, _ := e.stream.Rawbytes()
	return b
}
