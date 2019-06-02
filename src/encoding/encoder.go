package encoding

import (
	"encoding/json"
	"time"
	"unsafe"

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
	State() []byte
	Restore(b []byte) error
	Bytes() []byte
}

type marshalState struct {
	tsEncoder       m3tsz.TimestampEncoder
	floatEncoder    m3tsz.FloatEncoderAndIterator
	lastByte        byte
	bitPos          int
	hasWrittenFirst bool
}

type encoder struct {
	tsEncoder    m3tsz.TimestampEncoder
	floatEncoder m3tsz.FloatEncoderAndIterator
	stream       OStream

	hasWrittenFirst bool
}

// NewEncoder creates a new encoder.
func NewEncoder() Encoder {
	return &encoder{}
}

func (e *encoder) Encode(timestamp time.Time, value float64) error {
	if e.stream == nil {
		// Lazy init.
		e.stream = NewOStream()
		e.tsEncoder = m3tsz.NewTimestampEncoder(timestamp, xtime.Nanosecond, opts)
	}

	e.stream.WriteBit(hasMoreBit)

	var (
		// Unsafe insanity to temporarily avoid having to fork upstream.
		encodingStream = *(*encoding.OStream)(unsafe.Pointer(&e.stream))
		err            error
	)
	if !e.hasWrittenFirst {
		err = e.tsEncoder.WriteFirstTime(encodingStream, timestamp, nil, xtime.Nanosecond)
	} else {
		err = e.tsEncoder.WriteNextTime(encodingStream, timestamp, nil, xtime.Nanosecond)
	}
	if err != nil {
		return err
	}

	e.floatEncoder.WriteFloat(encodingStream, value)
	e.hasWrittenFirst = true
	return nil
}

func (e *encoder) State() []byte {
	var (
		raw, bitPos = e.stream.Rawbytes()
		lastByte    byte
	)
	if len(raw) > 0 {
		lastByte = raw[len(raw)-1]
	}

	marshalState := marshalState{
		tsEncoder:       e.tsEncoder,
		floatEncoder:    e.floatEncoder,
		hasWrittenFirst: e.hasWrittenFirst,
		lastByte:        lastByte,
		bitPos:          bitPos,
	}

	// TODO(rartoul): Replace this with something efficient / performant.
	marshaled, err := json.Marshal(&marshalState)
	if err != nil {
		// TODO(rartoul): Remove this once there is a better encoding scheme.
		panic(err)
	}

	return marshaled
}

func (e *encoder) Restore(b []byte) error {
	marshalState := marshalState{}
	if err := json.Unmarshal(b, &marshalState); err != nil {
		return err
	}

	e.tsEncoder = marshalState.tsEncoder
	e.floatEncoder = marshalState.floatEncoder
	e.hasWrittenFirst = marshalState.hasWrittenFirst

	return nil
}

func (e *encoder) Bytes() []byte {
	if e.stream == nil {
		return nil
	}

	b, _ := e.stream.Rawbytes()
	return b
}
