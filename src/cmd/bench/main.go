package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/richardartoul/tsdb-layer/src/layer"
	"github.com/richardartoul/tsdb-layer/src/layer/dircompress"
	"github.com/richardartoul/tsdb-layer/src/layer/raw"
	"github.com/richardartoul/tsdb-layer/src/layer/rawblock"
)

var (
	numSeriesFlag   = flag.Int("numSeries", 100000, "number of unique series")
	batchSizeFlag   = flag.Int("batchSize", 8, "client batch size")
	numWorkersFlag  = flag.Int("numWorkers", 100, "number of concurrent workers")
	durationFlag    = flag.Duration("duration", time.Minute, "duration to run the load test")
	layerEngineFlag = flag.String("layerEngine", "direct-compress", "layer engine to benchmark")
)

func main() {
	flag.Parse()

	tempFile, err := ioutil.TempFile("", "bench_cpu	")
	if err != nil {
		panic(err)
	}

	pprof.StartCPUProfile(tempFile)
	defer func() {
		defer pprof.StopCPUProfile()
		fmt.Println("cpu profile at:", tempFile.Name())
	}()

	var (
		numSeries   = *numSeriesFlag
		batchSize   = *batchSizeFlag
		numWorkers  = *numWorkersFlag
		duration    = *durationFlag
		layerEngine = *layerEngineFlag
	)
	fmt.Println("Running test with arguments:")
	fmt.Println("    layerEngine:", layerEngine)
	fmt.Println("    numSeries:", numSeries)
	fmt.Println("    batchSize:", batchSize)
	fmt.Println("    numWorkers:", numWorkers)
	fmt.Println("    duration:", duration)

	var layerClient layer.Layer
	switch layerEngine {
	case "direct-compress":
		layerClient = dircompress.NewLayer()
	case "raw":
		layerClient = raw.NewLayer()
	case "raw-block":
		layerClient = rawblock.NewLayer()
	default:
		log.Fatalf("invalid layer engine: ", layerEngine)
	}
	seriesIDs := make([]string, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		seriesIDs = append(seriesIDs, fmt.Sprintf("%s-%d", randomString(20), i))
	}

	var (
		wg                 sync.WaitGroup
		numWritesCompleted int64
		doneCh             = make(chan struct{})
	)
	go func() {
		time.Sleep(duration)
		close(doneCh)
	}()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			var (
				j      = 0
				batch  = make([]layer.Write, 0, batchSize)
				source = rand.NewSource(time.Now().UnixNano())
				rng    = rand.New(source)
			)
			for {
				select {
				case <-doneCh:
					return
				default:
				}
				batch = batch[:0]
				for y := 0; y < batchSize; y++ {
					idx := rng.Intn(numSeries)
					batch = append(batch, layer.Write{ID: seriesIDs[idx], Timestamp: time.Unix(0, int64(j)), Value: float64(j)})
				}
				if err := layerClient.WriteBatch(batch); err != nil {
					panic(err)
				}
				atomic.AddInt64(&numWritesCompleted, int64(batchSize))
				j++
			}
		}()
	}
	wg.Wait()

	qps := float64(numWritesCompleted) / duration.Seconds()
	fmt.Println("QPS: ", qps)
}

func randomString(len int) string {
	bytes := make([]byte, len)
	for i := 0; i < len; i++ {
		bytes[i] = byte(65 + rand.Intn(25)) //A=65 and Z = 65+25
	}
	return string(bytes)
}
