package layer

import (
	"time"

	"github.com/m3db/m3/src/dbnode/encoding"
	"github.com/m3db/m3/src/dbnode/encoding/m3tsz"
	xtime "github.com/m3db/m3/src/x/time"
)

type encoder struct {
	tsEncoder    m3tsz.TimestampEncoder
	floatEncoder m3tsz.FloatEncoderAndIterator
	ostream      encoding.OStream

	hasWrittenFirst bool
}

func (e *encoder) Encode(timestamp time.Time, value float64) error {
	if e.ostream == nil {
		e.ostream = encoding.NewOStream(nil, false, nil)
	}

	if !e.hasWrittenFirst {
		return e.tsEncoder.WriteFirstTime(e.ostream, timestamp, nil, xtime.Nanosecond)
	}
	return e.tsEncoder.WriteNextTime(e.ostream, timestamp, nil, xtime.Nanosecond)
}
