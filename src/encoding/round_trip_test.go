package encoding

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

	encoder := NewEncoder()
	for _, v := range values {
		err := encoder.Encode(v.timestamp, v.value)
		require.NoError(t, err)
	}

	encodedBytes := encoder.Bytes()
	require.Equal(t, 22, len(encodedBytes))

	decoder := NewDecoder()
	decoder.Reset(encodedBytes)

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

func TestRoundTripWithStateAndRestore(t *testing.T) {
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

	var (
		accumulated []byte
		lastState   []byte
	)
	for _, v := range values {
		encoder := NewEncoder()
		if lastState != nil {
			err := encoder.Restore(lastState)
			require.NoError(t, err)
		}
		err := encoder.Encode(v.timestamp, v.value)
		require.NoError(t, err)
		lastState = encoder.State()

		b := encoder.Bytes()
		if accumulated == nil {
			accumulated = b
		} else {
			accumulated[len(accumulated)-1] = b[0]
			if len(b) > 1 {
				accumulated = append(accumulated, b[1:]...)
			}
		}
	}

	require.Equal(t, 22, len(accumulated))

	decoder := NewDecoder()
	decoder.Reset(accumulated)

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
