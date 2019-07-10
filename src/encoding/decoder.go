package encoding

import (
	"bytes"
	"errors"
	"io"
	"math"
	"time"

	"github.com/m3db/m3/src/dbnode/encoding"
	"github.com/m3db/m3/src/dbnode/encoding/m3tsz"
	xtime "github.com/m3db/m3/src/x/time"
)

type Decoder interface {
	ReadableDecoder
	Reset(b []byte)
}

type ReadableDecoder interface {
	Next() bool
	Current() (time.Time, float64)
	Err() error
}

type decoder struct {
	tsDecoder    m3tsz.TimestampIterator
	floatDecoder m3tsz.FloatEncoderAndIterator
	bReader      *bytes.Reader
	stream       encoding.IStream

	err  error
	done bool
}

// NewDecoder creates a new decoder.
func NewDecoder() Decoder {
	return &decoder{
		bReader: bytes.NewReader(nil),
		stream:  encoding.NewIStream(nil),
	}
}

func (d *decoder) Reset(b []byte) {
	d.tsDecoder = m3tsz.NewTimestampIterator(opts, true)
	d.tsDecoder.TimeUnit = xtime.Nanosecond
	d.floatDecoder = m3tsz.FloatEncoderAndIterator{}
	d.bReader.Reset(b)
	d.stream.Reset(d.bReader)
	d.done = false
}

func (d *decoder) Next() bool {
	if d.done || d.err != nil {
		return false
	}

	bit, err := d.stream.ReadBit()
	if err == io.EOF {
		d.done = true
		return false
	}
	if err != nil {
		d.err = err
		return false
	}
	if bit != hasMoreBit {
		d.done = true
		return false
	}

	_, done, err := d.tsDecoder.ReadTimestamp(d.stream)
	if done {
		// This should never happen since we never encode the EndOfStream marker.
		d.err = errors.New("unexpected end of timestamp stream")
		return false
	}
	if err != nil {
		d.err = err
		return false
	}

	if err := d.floatDecoder.ReadFloat(d.stream); err != nil {
		d.err = err
		return false
	}

	return true
}

func (d *decoder) Current() (time.Time, float64) {
	return d.tsDecoder.PrevTime, math.Float64frombits(d.floatDecoder.PrevFloatBits)
}

func (d *decoder) Err() error {
	return d.err
}
