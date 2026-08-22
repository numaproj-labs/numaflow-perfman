package sink

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/sinker"

	"numa-perfman/udfs/internal/event"
)

// upsertReduceSinkSQL differs from the map sink: on conflict it also refreshes the
// payload (a window may be re-emitted with the same dedup key under chaos/replay).
const upsertReduceSinkSQL = `
	INSERT INTO sink_events
	(dedup_key, event_id, payload, processed_by, child_index, total_children)
	VALUES ($1, $2, $3::jsonb, $4, $5, $6)
	ON CONFLICT (dedup_key)
	DO UPDATE SET
		payload = EXCLUDED.payload,
		receive_count = sink_events.receive_count + 1,
		updated_at = NOW()`

// ReduceSink writes reduce output (8-field JSON) to sink_events. The dedup key is
// derived from the payload ("{reduce_key}_{window_start}"); event_id reuses the dedup
// key as a placeholder; child_index/total_children are constant (one output per
// window). processed_by comes from PROCESSED_BY (default "fixed-window-reduce"), which
// the sliding-window pipeline overrides to "sliding-window-reduce".
type ReduceSink struct {
	pool        *pgxpool.Pool
	processedBy string
}

// NewReduceSink constructs the reduce sink, reading PROCESSED_BY from the environment.
func NewReduceSink(pool *pgxpool.Pool) *ReduceSink {
	processedBy := os.Getenv("PROCESSED_BY")
	if processedBy == "" {
		processedBy = "fixed-window-reduce"
	}
	return &ReduceSink{pool: pool, processedBy: processedBy}
}

// Sink reads the datum stream, upserts each window output, and responds per datum.
func (s *ReduceSink) Sink(ctx context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	responses := sinker.ResponsesBuilder()
	for datum := range datumStreamCh {
		var out event.ReduceOutput
		if err := json.Unmarshal(datum.Value(), &out); err != nil {
			log.Printf("ReduceSink: failed to decode datum %s: %v", datum.ID(), err)
			responses = responses.Append(sinker.ResponseFailure(datum.ID(), err.Error()))
			continue
		}
		dedupKey := event.ReduceDedupKey(out.ReduceKey, out.WindowStart)
		if _, err := s.pool.Exec(ctx, upsertReduceSinkSQL,
			dedupKey, dedupKey, string(datum.Value()), s.processedBy, "0", "1"); err != nil {
			log.Printf("ReduceSink: upsert failed for %s: %v", dedupKey, err)
			responses = responses.Append(sinker.ResponseFailure(datum.ID(), err.Error()))
			continue
		}
		responses = responses.Append(sinker.ResponseOK(datum.ID()))
	}
	return responses
}
