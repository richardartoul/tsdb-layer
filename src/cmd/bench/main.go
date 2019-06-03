package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/richardartoul/tsdb-layer/src/layer"
)

func main() {
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

	layer := layer.NewLayer()
	for _, tc := range testCases {
		fmt.Printf("%+v\n", tc)
		seriesIDs := make([]string, 0, tc.numSeries)
		for i := 0; i < tc.numSeries; i++ {
			seriesIDs = append(seriesIDs, fmt.Sprintf("some-long-ts-id-new-%d", i))
		}

		var (
			start = time.Now()
			wg    sync.WaitGroup
		)
		for i := 0; i < tc.numWorkers; i++ {
			source := rand.NewSource(time.Now().UnixNano())
			rng := rand.New(source)
			wg.Add(1)
			go func() {
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
			qps       = float64(numWrites) / duration.Seconds()
		)
		fmt.Println("Duration: ", duration)
		fmt.Println("QPS: ", qps)
	}
}
