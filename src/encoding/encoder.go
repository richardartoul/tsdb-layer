package encoding

import (
	"encoding/json"
	"fmt"
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
	TSEncoder       m3tsz.TimestampEncoder
	FloatEncoder    m3tsz.FloatEncoderAndIterator
	LastByte        byte
	BitPos          int
	HasWrittenFirst bool
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
		TSEncoder:       e.tsEncoder,
		FloatEncoder:    e.floatEncoder,
		HasWrittenFirst: e.hasWrittenFirst,
		LastByte:        lastByte,
		BitPos:          bitPos,
	}
	// Prevent JSON marshaling error.
	marshalState.TSEncoder.Options = nil

	// TODO(rartoul): Replace this with something efficient / performant.
	marshaled, err := json.Marshal(&marshalState)
	if err != nil {
		// TODO(rartoul): Remove this once there is a better encoding scheme.
		panic(err)
	}

	return marshaled
}

func (e *encoder) Restore(b []byte) error {
	if b == nil {
		return fmt.Errorf("cannot restore from nil state")
	}

	marshalState := marshalState{}
	if err := json.Unmarshal(b, &marshalState); err != nil {
		return err
	}

	e.tsEncoder = marshalState.TSEncoder
	e.tsEncoder.Options = opts
	e.floatEncoder = marshalState.FloatEncoder
	e.hasWrittenFirst = marshalState.HasWrittenFirst

	if e.stream == nil {
		e.stream = NewOStream()
	}
	// TODO(rartoul): Fix this non-sense.
	e.stream.(*ostream).buf = []byte{marshalState.LastByte}
	e.stream.(*ostream).pos = marshalState.BitPos

	return nil
}

func (e *encoder) Bytes() []byte {
	if e.stream == nil {
		return nil
	}

	b, _ := e.stream.Rawbytes()
	return b
}
