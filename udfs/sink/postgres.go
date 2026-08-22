// Package sink implements the Numaflow UDSink handlers for the validation pipelines:
// the map-validations sink, the reduce sink, and the three MonoVertex sinks.
package sink

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/sinker"

	"numa-perfman/udfs/internal/event"
)

// upsertSinkEventSQL is the idempotent insert used by all sinks. The dedup_key
// primary key makes retries safe: a conflicting row only bumps receive_count.
const upsertSinkEventSQL = `
	INSERT INTO sink_events
	(dedup_key, event_id, payload, processed_by, child_index, total_children)
	VALUES ($1, $2, $3::jsonb, $4, $5, $6)
	ON CONFLICT (dedup_key)
	DO UPDATE SET
		receive_count = sink_events.receive_count + 1,
		updated_at = NOW()`

// PostgresSink writes enriched map-validation events to postgres, deduplicating on
// dedup_key. Implements sinker.Sinker.
type PostgresSink struct {
	pool *pgxpool.Pool
}

// NewPostgresSink constructs the map-validations sink.
func NewPostgresSink(pool *pgxpool.Pool) *PostgresSink {
	return &PostgresSink{pool: pool}
}

// Sink reads the datum stream, upserts each event, and returns a response per datum.
func (s *PostgresSink) Sink(ctx context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	responses := sinker.ResponsesBuilder()
	for datum := range datumStreamCh {
		var ev event.Event
		if err := json.Unmarshal(datum.Value(), &ev); err != nil {
			log.Printf("PostgresSink: failed to decode datum %s: %v", datum.ID(), err)
			responses = responses.Append(sinker.ResponseFailure(datum.ID(), err.Error()))
			continue
		}
		dedupKey, err := dedupKeyFromEvent(ev)
		if err != nil {
			log.Printf("PostgresSink: %v", err)
			responses = responses.Append(sinker.ResponseFailure(datum.ID(), err.Error()))
			continue
		}
		if _, err := s.pool.Exec(ctx, upsertSinkEventSQL,
			dedupKey, ev.EventID, string(datum.Value()),
			derefStr(ev.ProcessedBy), ev.ChildIndex, ev.TotalChildren); err != nil {
			log.Printf("PostgresSink: upsert failed for %s: %v", dedupKey, err)
			responses = responses.Append(sinker.ResponseFailure(datum.ID(), err.Error()))
			continue
		}
		responses = responses.Append(sinker.ResponseOK(datum.ID()))
	}
	return responses
}

// dedupKeyFromEvent builds the composite dedup key, requiring processed_by to be set.
func dedupKeyFromEvent(ev event.Event) (string, error) {
	if ev.ProcessedBy == nil {
		return "", fmt.Errorf("processed_by is required for event %s", ev.EventID)
	}
	return event.DedupKey(ev.EventID, *ev.ProcessedBy, ev.ChildIndex), nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
