// Command udfs is the single-binary entry point for the numaflow validation UDFs.
// A mode flag selects which Numaflow server (or the one-shot validator) to run,
// mirroring the Kotlin Main.kt dispatcher.
package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/batchmapper"
	sdkmapper "github.com/numaproj/numaflow-go/pkg/mapper"
	"github.com/numaproj/numaflow-go/pkg/mapstreamer"
	"github.com/numaproj/numaflow-go/pkg/reducer"
	"github.com/numaproj/numaflow-go/pkg/sinker"
	"github.com/numaproj/numaflow-go/pkg/sourcer"
	"github.com/numaproj/numaflow-go/pkg/sourcetransformer"

	"numa-perfman/udfs/benchmark"
	"numa-perfman/udfs/internal/db"
	udfmapper "numa-perfman/udfs/mapper"
	"numa-perfman/udfs/reduce"
	"numa-perfman/udfs/sink"
	"numa-perfman/udfs/source"
	"numa-perfman/udfs/transformer"
	"numa-perfman/udfs/validator"
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	ctx := context.Background()

	switch os.Args[1] {
	// ── map-validations ──
	case "--source":
		runSourceServer(source.NewAdEventSource(ctx, mustPool(ctx)))
	case "--sourcetransformer":
		start(sourcetransformer.NewServer(transformer.NewSourceTransformer()))
	case "--unarymap":
		start(sdkmapper.NewServer(udfmapper.NewUnaryMap()))
	case "--batchmap":
		start(batchmapper.NewServer(udfmapper.NewBatchMap()))
	case "--streammap":
		start(mapstreamer.NewServer(udfmapper.NewStreamMap()))
	case "--sink":
		start(sinker.NewServer(sink.NewPostgresSink(mustPool(ctx))))
	case "--validator":
		runValidator(ctx)

	// ── reduce-validations (also serves sliding-window) ──
	case "--reduce-source":
		runSourceServer(source.NewReduceSource(ctx, mustPool(ctx)))
	case "--reduce":
		start(reducer.NewServer(&reduce.Creator{}))
	case "--reduce-sink":
		start(sinker.NewServer(sink.NewReduceSink(mustPool(ctx))))

	// ── monovertex-validations ──
	case "--monovertex-source":
		runSourceServer(source.NewMonoVertexSource(ctx, mustPool(ctx)))
	case "--monovertex-transformer":
		start(sourcetransformer.NewServer(transformer.NewMonoVertexTransformer()))
	case "--monovertex-map":
		start(sdkmapper.NewServer(udfmapper.NewMonoVertexMap()))
	case "--monovertex-sink":
		start(sinker.NewServer(sink.NewMonoVertexPrimarySink(mustPool(ctx))))
	case "--monovertex-onsuccess-sink":
		start(sinker.NewServer(sink.NewMonoVertexOnSuccessSink(mustPool(ctx))))
	case "--monovertex-fallback-sink":
		start(sinker.NewServer(sink.NewMonoVertexFallbackSink(mustPool(ctx))))

	// ── benchmark fixtures ──
	case "--benchmark-map":
		start(sdkmapper.NewServer(benchmark.ForwardMap{}))
	case "--benchmark-batchmap":
		start(batchmapper.NewServer(benchmark.ForwardBatchMap{}))
	case "--benchmark-streammap":
		start(mapstreamer.NewServer(benchmark.ForwardStreamMap{}))
	case "--benchmark-transformer":
		start(sourcetransformer.NewServer(benchmark.PassThroughTransformer{}))
	case "--benchmark-blackhole-sink":
		start(sinker.NewServer(benchmark.BlackholeSink{}))

	case "--help", "-h":
		usageAndExit()
	default:
		log.Printf("Unknown flag: %s", os.Args[1])
		usageAndExit()
	}
}

// server is the common shape of every Numaflow NewServer result.
type server interface {
	Start(ctx context.Context) error
}

func start(s server) {
	if err := s.Start(context.Background()); err != nil {
		log.Fatalf("server exited with error: %v", err)
	}
}

// runSourceServer starts a sourcer.Server for the given Sourcer implementation.
func runSourceServer(src sourcer.Sourcer) {
	start(sourcer.NewServer(src))
}

func mustPool(ctx context.Context) *pgxpool.Pool {
	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("failed to create postgres pool: %v", err)
	}
	return pool
}

func runValidator(ctx context.Context) {
	pool := mustPool(ctx)
	defer pool.Close()

	sampleLimit := 100
	if v := os.Getenv("VALIDATION_SAMPLE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			sampleLimit = n
		}
	}

	result, err := validator.New(pool, sampleLimit).Validate(ctx)
	if err != nil {
		log.Fatalf("validation error: %v", err)
	}

	if result.Status == "PASS" {
		log.Printf("Validation PASSED! Total: %d source -> %d expected -> %d actual",
			result.TotalSourceEvents, result.TotalExpectedSinkEvents, result.TotalActualSinkEvents)
		os.Exit(0)
	}
	log.Printf("Validation FAILED! Missing: %d, Corrupted: %d, Extra: %d",
		result.MissingCount, result.CorruptedCount, result.ExtraCount)
	if len(result.SampleMissingKeys) > 0 {
		log.Printf("Sample missing: %v", firstN(result.SampleMissingKeys, 5))
	}
	if len(result.SampleCorruptedKeys) > 0 {
		log.Printf("Sample corrupted: %v", firstN(result.SampleCorruptedKeys, 5))
	}
	os.Exit(1)
}

func firstN(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func usageAndExit() {
	log.Print(`Usage: udfs <flag>

Flags:
  --source                     Run the Ad Event Source
  --sourcetransformer          Run the Source Transformer UDF
  --unarymap                   Run the Unary Map UDF
  --batchmap                   Run the Batch Map UDF
  --streammap                  Run the Stream Map UDF
  --sink                       Run the PostgreSQL Sink
  --validator                  Run the Validator (one-shot)
  --reduce-source              Run the Reduce Source
  --reduce                     Run the Fixed Window Reduce UDF
  --reduce-sink                Run the Reduce Sink
  --monovertex-source          Run the MonoVertex Source
  --monovertex-transformer     Run the MonoVertex Transformer
  --monovertex-map             Run the MonoVertex Map UDF
  --monovertex-sink            Run the MonoVertex Primary Sink
  --monovertex-onsuccess-sink  Run the MonoVertex OnSuccess Sink
  --monovertex-fallback-sink   Run the MonoVertex Fallback Sink
  --benchmark-map              Benchmark passthrough map
  --benchmark-batchmap         Benchmark passthrough batch map
  --benchmark-streammap        Benchmark passthrough stream map
  --benchmark-transformer      Benchmark passthrough source transformer
  --benchmark-blackhole-sink   Benchmark sink that discards input
  --help, -h                   Show this help message`)
	os.Exit(1)
}
