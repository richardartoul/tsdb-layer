package layer

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func BenchmarkLayer(b *testing.B) {
	testCases := []struct {
		numSeries          int
		numWritesPerWorker int
		numWorkers         int
	}{
		{
			numSeries:          100000,
			numWritesPerWorker: 100,
			numWorkers:         1000,
		},
	}

	layer := NewLayer()
	for _, tc := range testCases {
		b.Run(fmt.Sprintf("%+v", tc), func(b *testing.B) {
			seriesIDs := make([]string, 0, tc.numSeries)
			for i := 0; i < tc.numSeries; i++ {
				seriesIDs = append(seriesIDs, fmt.Sprintf("some-long-ts-id-%d", i))
			}

			var (
				start = time.Now()
				wg    sync.WaitGroup
			)
			for i := 0; i < tc.numWorkers; i++ {
				wg.Add(1)
				go func() {
					source := rand.NewSource(time.Now().UnixNano())
					rng := rand.New(source)
					for j := 0; j < tc.numWritesPerWorker; j++ {
						idx := rng.Intn(tc.numSeries)
						if err := layer.Write(seriesIDs[idx], time.Unix(0, int64(j)), float64(j)); err != nil {
							panic(err)
						}
					}
					wg.Done()
				}()
			}
			wg.Wait()

			var (
				numWrites = tc.numWorkers * tc.numWritesPerWorker
				duration  = time.Now().Sub(start)
				qps       = float64(numWrites) / float64((time.Second * duration))
			)
			fmt.Println("Duration: ", duration)
			fmt.Println("QPS: ", qps)
		})
	}
}
