package encoding

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type multiDecoderTestCase struct {
	title   string
	streams [][]testValue
}

func TestMultiDecoder(t *testing.T) {
	testCases := []multiDecoderTestCase{
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
		decs := make([]Decoder, 0, len(tc.streams))
		expected := []testValue{}
		for _, stream := range tc.streams {
			enc := NewEncoder()
			for _, v := range stream {
				enc.Encode(v.timestamp, v.value)
				expected = append(expected, v)
			}

			dec := NewDecoder()
			dec.Reset(enc.Bytes())
			decs = append(decs, dec)
		}
		sort.Slice(expected, func(i, j int) bool {
			return expected[i].timestamp.Before(expected[j].timestamp)
		})

		multiDecoder := NewMultiDecoder()
		multiDecoder.Reset(decs)

		i := 0
		for multiDecoder.Next() {
			currT, currV := multiDecoder.Current()
			require.True(
				t,
				expected[i].timestamp.Equal(currT),
				fmt.Sprintf("expected %s but got %s", expected[i].timestamp.String(), currT.String()))
			require.Equal(t, expected[i].value, currV)
			i++
		}
		require.Equal(t, len(expected), i)
	}
}
