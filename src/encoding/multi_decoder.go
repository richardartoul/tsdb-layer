package encoding

import (
	"container/heap"
	"time"
)

type MultiDecoder interface {
	ReadableDecoder
	Reset(decs []Decoder)
}

type decState struct {
	dec Decoder
}

type multiDecoder struct {
	decs      []decState
	currEntry heapEntry
	heap      minHeap
	err       error
}

func NewMultiDecoder() *multiDecoder {
	return &multiDecoder{}
}

func (m *multiDecoder) Next() bool {
	if m.err != nil {
		return false
	}
	if m.heap.Len() == 0 {
		return false
	}
	m.currEntry = heap.Pop(&m.heap).(heapEntry)
	dec := m.decs[m.currEntry.decIdx].dec
	if dec.Next() {
		t, v := dec.Current()
		heap.Push(&m.heap, heapEntry{t: t, v: v, decIdx: m.currEntry.decIdx})
	} else {
		if dec.Err() != nil {
			m.err = dec.Err()
		}
	}
	return true
}

func (m *multiDecoder) Current() (time.Time, float64) {
	return m.currEntry.t, m.currEntry.v
}

func (m *multiDecoder) Err() error {
	return nil
}

func (m *multiDecoder) Reset(decs []Decoder) {
	m.err = nil
	for i := range m.decs {
		m.decs[i] = decState{}
	}
	m.decs = m.decs[:0]
	for _, dec := range decs {
		m.decs = append(m.decs, decState{dec: dec})
	}

	m.heap.vals = m.heap.vals[:0]
	for i, dec := range m.decs {
		if dec.dec.Next() {
			t, v := dec.dec.Current()
			m.heap.vals = append(m.heap.vals, heapEntry{t: t, v: v, decIdx: i})
		} else {
			if dec.dec.Err() != nil {
				m.err = dec.dec.Err()
			}
		}
	}
	heap.Init(&m.heap)
}

type minHeap struct {
	vals []heapEntry
}

type heapEntry struct {
	t      time.Time
	v      float64
	decIdx int
}

func (h *minHeap) Push(x interface{}) {
	h.vals = append(h.vals, x.(heapEntry))
}

func (h *minHeap) Pop() interface{} {
	lastIdx := len(h.vals) - 1
	x := h.vals[lastIdx]
	h.vals = h.vals[:lastIdx]
	return x
}

func (h *minHeap) Len() int {
	if h == nil {
		return 0
	}
	return len(h.vals)
}

func (h *minHeap) Less(i, j int) bool {
	return h.vals[i].t.Before(h.vals[j].t)
}

func (h *minHeap) Swap(i, j int) {
	h.vals[i], h.vals[j] = h.vals[j], h.vals[i]
}
