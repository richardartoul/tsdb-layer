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

type roundTripTestCase struct {
	title string
	vals  []testValue
}

// TODO(rartoul): This probably needs some kind of property test.
func TestRoundTripSimple(t *testing.T) {
	testCases := []roundTripTestCase{
		{
			title: "simple in order",
			vals: []testValue{
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
			},
		},
		{
			title: "simple out of order",
			vals: []testValue{
				{
					timestamp: time.Unix(0, 3),
					value:     -1,
				},
				{
					timestamp: time.Unix(0, 2),
					value:     0,
				},
				{
					timestamp: time.Unix(0, 1),
					value:     1,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			encoder := NewEncoder()
			// TODO(rartoul): This should probably be its own test.
			_, _, ok := encoder.LastEncoded()
			require.False(t, ok)

			for _, v := range tc.vals {
				err := encoder.Encode(v.timestamp, v.value)
				require.NoError(t, err)

				// TODO(rartoul): This should probably be its own test.
				lastEncodedT, lastEncodedV, ok := encoder.LastEncoded()
				require.True(t, ok)
				require.True(t, v.timestamp.Equal(lastEncodedT))
				require.Equal(t, v.value, lastEncodedV)
			}

			encodedBytes := encoder.Bytes()
			require.Equal(t, 22, len(encodedBytes))

			decoder := NewDecoder()
			decoder.Reset(encodedBytes)

			i := 0
			for decoder.Next() {
				currT, currV := decoder.Current()
				require.Equal(t, tc.vals[i].timestamp, currT)
				require.Equal(t, tc.vals[i].value, currV)
				i++
			}
			require.NoError(t, decoder.Err())
			require.Equal(t, len(tc.vals), i)
		})
	}

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
