package encoding

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mergeStreamsTestCase struct {
	title   string
	streams [][]testValue
}

func TestMergeStreams(t *testing.T) {
	testCases := []mergeStreamsTestCase{
		{
			title: "Merge two in order streams",
			streams: [][]testValue{
				[]testValue{{timestamp: time.Unix(0, 0), value: 0}},
				[]testValue{{timestamp: time.Unix(1, 0), value: 1}},
			},
		},
		{
			title: "Merge two out of order streams",
			streams: [][]testValue{
				[]testValue{{timestamp: time.Unix(1, 0), value: 1}},
				[]testValue{{timestamp: time.Unix(0, 0), value: 0}},
			},
		},
		{
			title: "Merge multiple streams",
			streams: [][]testValue{
				[]testValue{{timestamp: time.Unix(10, 0), value: 10}, {timestamp: time.Unix(11, 0), value: 11}},
				[]testValue{{timestamp: time.Unix(7, 0), value: 7}},
				[]testValue{{timestamp: time.Unix(8, 0), value: 8}, {timestamp: time.Unix(9, 0), value: 9}},
				[]testValue{{timestamp: time.Unix(1, 0), value: 1}, {timestamp: time.Unix(3, 0), value: 3}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			streams := make([][]byte, 0, len(tc.streams))
			expected := []testValue{}
			for _, stream := range tc.streams {
				enc := NewEncoder()
				for _, v := range stream {
					enc.Encode(v.timestamp, v.value)
					expected = append(expected, v)
				}

				streams = append(streams, enc.Bytes())
			}
			sort.Slice(expected, func(i, j int) bool {
				return expected[i].timestamp.Before(expected[j].timestamp)
			})

			merged, err := MergeStreams(streams...)
			require.NoError(t, err)
			decoder := NewDecoder()
			decoder.Reset(merged)

			i := 0
			for decoder.Next() {
				currT, currV := decoder.Current()
				require.True(
					t,
					expected[i].timestamp.Equal(currT),
					fmt.Sprintf("expected %s but got %s", expected[i].timestamp.String(), currT.String()))
				require.Equal(t, expected[i].value, currV)
				i++
			}
			require.NoError(t, decoder.Err())
			require.Equal(t, len(expected), i)
		})
	}
}
