package sink

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/sinker"

	"numa-perfman/udfs/internal/event"
)

// preparedRow is a decoded, route-resolved event ready to be upserted.
type preparedRow struct {
	datumID  string
	event    event.Event
	dedupKey string
	failErr  string // non-empty means emit a failure response, skip the write
}

// MonoVertexOnSuccessSink receives two paths:
//
//	Flow 1 (normal-onsuccess): enriched event already stamped processed_by="mv-onsuccess"
//	  by the primary sink — written as-is.
//	Flow 3 (bypass-onsuccess): raw event with no enrichment — sets
//	  processed_by="mv-bypass-onsuccess" and composes the composite child index.
type MonoVertexOnSuccessSink struct {
	pool *pgxpool.Pool
}

// NewMonoVertexOnSuccessSink constructs the MonoVertex onSuccess sink.
func NewMonoVertexOnSuccessSink(pool *pgxpool.Pool) *MonoVertexOnSuccessSink {
	return &MonoVertexOnSuccessSink{pool: pool}
}

// Sink prepares, batch-writes, and responds per datum.
func (s *MonoVertexOnSuccessSink) Sink(ctx context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	return processMonoVertexSink(ctx, s.pool, "MonoVertexOnSuccessSink", datumStreamCh, func(ev event.Event) (event.Event, bool) {
		switch derefStr(ev.RouteTag) {
		case "normal-onsuccess":
			return ev, true
		case "bypass-onsuccess":
			pb := "mv-bypass-onsuccess"
			ev.ProcessedBy = &pb
			ev.ChildIndex = event.MonoVertexChildIndex(ev.TransformerChildIndex, 0)
			ev.TotalChildren = event.MonoVertexTotalChildren(ev.TransformerTotalChildren, 1)
			return ev, true
		default:
			return ev, false
		}
	})
}

// MonoVertexFallbackSink receives two paths:
//
//	Flow 2 (normal-fallback): enriched map output forwarded unchanged by the primary's
//	  ResponseFallback — sets processed_by="mv-fallback".
//	Flow 4 (bypass-fallback): raw event with no enrichment — sets
//	  processed_by="mv-bypass-fallback" and composes the composite child index.
type MonoVertexFallbackSink struct {
	pool *pgxpool.Pool
}

// NewMonoVertexFallbackSink constructs the MonoVertex fallback sink.
func NewMonoVertexFallbackSink(pool *pgxpool.Pool) *MonoVertexFallbackSink {
	return &MonoVertexFallbackSink{pool: pool}
}

// Sink prepares, batch-writes, and responds per datum.
func (s *MonoVertexFallbackSink) Sink(ctx context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	return processMonoVertexSink(ctx, s.pool, "MonoVertexFallbackSink", datumStreamCh, func(ev event.Event) (event.Event, bool) {
		switch derefStr(ev.RouteTag) {
		case "normal-fallback":
			pb := "mv-fallback"
			ev.ProcessedBy = &pb
			return ev, true
		case "bypass-fallback":
			pb := "mv-bypass-fallback"
			ev.ProcessedBy = &pb
			ev.ChildIndex = event.MonoVertexChildIndex(ev.TransformerChildIndex, 0)
			ev.TotalChildren = event.MonoVertexTotalChildren(ev.TransformerTotalChildren, 1)
			return ev, true
		default:
			return ev, false
		}
	})
}

// resolveFunc maps a decoded event to its final form, returning ok=false for an
// unexpected route_tag (which becomes a failure response).
type resolveFunc func(event.Event) (event.Event, bool)

// processMonoVertexSink is the shared decode → resolve → batch-upsert → respond loop
// used by the onSuccess and fallback sinks.
func processMonoVertexSink(ctx context.Context, pool *pgxpool.Pool, name string, datumStreamCh <-chan sinker.Datum, resolve resolveFunc) sinker.Responses {
	var rows []preparedRow

	for datum := range datumStreamCh {
		var ev event.Event
		if err := json.Unmarshal(datum.Value(), &ev); err != nil {
			log.Printf("%s: failed to prepare datum %s: %v", name, datum.ID(), err)
			rows = append(rows, preparedRow{datumID: datum.ID(), failErr: err.Error()})
			continue
		}
		final, ok := resolve(ev)
		if !ok {
			rt := derefStr(ev.RouteTag)
			log.Printf("%s: unexpected route_tag %q", name, rt)
			rows = append(rows, preparedRow{datumID: datum.ID(), failErr: "unexpected route_tag: " + rt})
			continue
		}
		dedupKey, err := dedupKeyFromEvent(final)
		if err != nil {
			rows = append(rows, preparedRow{datumID: datum.ID(), failErr: err.Error()})
			continue
		}
		rows = append(rows, preparedRow{datumID: datum.ID(), event: final, dedupKey: dedupKey})
	}

	batchErr := writeMonoVertexBatch(ctx, pool, name, rows)

	responses := sinker.ResponsesBuilder()
	for _, row := range rows {
		switch {
		case row.failErr != "":
			responses = responses.Append(sinker.ResponseFailure(row.datumID, row.failErr))
		case batchErr != "":
			responses = responses.Append(sinker.ResponseFailure(row.datumID, batchErr))
		default:
			responses = responses.Append(sinker.ResponseOK(row.datumID))
		}
	}
	return responses
}

// writeMonoVertexBatch upserts all writable rows in one transaction. Rows carrying a
// failErr are skipped. Returns an empty string on success or an error message.
func writeMonoVertexBatch(ctx context.Context, pool *pgxpool.Pool, name string, rows []preparedRow) string {
	batch := &pgx.Batch{}
	queued := 0
	for _, row := range rows {
		if row.failErr != "" {
			continue
		}
		payload, _ := json.Marshal(row.event)
		batch.Queue(upsertSinkEventSQL,
			row.dedupKey, row.event.EventID, string(payload),
			derefStr(row.event.ProcessedBy), row.event.ChildIndex, row.event.TotalChildren)
		queued++
	}
	if queued == 0 {
		return ""
	}
	if err := pool.SendBatch(ctx, batch).Close(); err != nil {
		log.Printf("%s: batch of %d rows failed: %v", name, queued, err)
		return err.Error()
	}
	return ""
}
