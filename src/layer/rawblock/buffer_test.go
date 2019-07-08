package rawblock

import (
	"fmt"
	"testing"
	"time"

	"github.com/richardartoul/tsdb-layer/src/layer"

	"github.com/stretchr/testify/require"
)

const (
	testID = "test-id"
)

type testValue struct {
	timestamp time.Time
	value     float64
}

type bufferWriteReadTestCase struct {
	title string
	vals  []testValue
}

func TestBufferWriteRead(t *testing.T) {
	testCases := []bufferWriteReadTestCase{
		{
			title: "in order values",
			vals:  []testValue{{timestamp: time.Unix(0, 0), value: 0}, {timestamp: time.Unix(1, 0), value: 1}},
		},
		// TODO(rartoul): Not supported right now.
		// {
		// 	title: "out of order values",
		// 	vals:  []testValue{{timestamp: time.Unix(1, 0), value: 1}, {timestamp: time.Unix(0, 0), value: 0}},
		// },
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			db, cleanup := newTestFDB()
			defer cleanup()

			buffer := NewBuffer(db)
			writes := []layer.Write{}
			for _, val := range tc.vals {
				writes = append(
					writes,
					layer.Write{
						ID:        testID,
						Timestamp: val.timestamp,
						Value:     val.value})
			}
			require.NoError(t, buffer.Write(writes))

			assertReadFn := func() {
				multiDec, ok, err := buffer.Read(testID)
				require.NoError(t, err)
				require.True(t, ok)

				i := 0
				for multiDec.Next() {
					currT, currV := multiDec.Current()
					require.True(
						t,
						tc.vals[i].timestamp.Equal(currT),
						fmt.Sprintf("expected %s but got %s", tc.vals[i].timestamp.String(), currT.String()))
					require.Equal(t, tc.vals[i].value, currV)
					i++
				}
				require.NoError(t, multiDec.Err())
				require.Equal(t, len(tc.vals), i)
			}

			// Ensure reads work correctly before and after flushing.
			assertReadFn()
			require.NoError(t, buffer.Flush())
			assertReadFn()
		})
	}
}
