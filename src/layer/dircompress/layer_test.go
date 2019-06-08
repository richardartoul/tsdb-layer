package dircompress

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testValue struct {
	timestamp time.Time
	value     float64
}

// TODO(rartoul): This probably needs some kind of property test.
func TestRoundTripSimple(t *testing.T) {
	tsID := "test-id-1"
	values := []testValue{
		{
			timestamp: time.Unix(0, 1),
			value:     -1,
		},
		{
			timestamp: time.Unix(0, 2),
			value:     0,
		},
		{
			timestamp: time.Unix(0, 3),
			value:     1,
		},
	}

	layer := NewLayer()
	for _, v := range values {
		err := layer.Write(tsID, v.timestamp, v.value)
		require.NoError(t, err)
	}

	decoder, err := layer.Read(tsID)
	require.NoError(t, err)

	i := 0
	for decoder.Next() {
		currT, currV := decoder.Current()
		require.Equal(t, values[i].timestamp, currT)
		require.Equal(t, values[i].value, currV)
		i++
	}
	require.NoError(t, decoder.Err())
	require.Equal(t, len(values), i)
}
