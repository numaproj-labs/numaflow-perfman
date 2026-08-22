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

// MonoVertexPrimarySink handles two flows by route_tag:
//
// Flow 1 (normal-onsuccess): writes the event as processed_by="mv-primary", then
// returns ResponseOnSuccess carrying a clone with processed_by="mv-onsuccess" — the
// MonoVertex routes that clone to the onSuccess sink.
//
// Flow 2 (normal-fallback): does NOT write; returns ResponseFallback so Numaflow
// forwards the map's enriched output to the fallback sink unchanged.
//
// Flow-1 DB writes are batched per Sink() call. UPSERT on dedup_key keeps retries
// idempotent, so batch-atomic semantics are safe.
type MonoVertexPrimarySink struct {
	pool *pgxpool.Pool
}

// NewMonoVertexPrimarySink constructs the MonoVertex primary sink.
func NewMonoVertexPrimarySink(pool *pgxpool.Pool) *MonoVertexPrimarySink {
	return &MonoVertexPrimarySink{pool: pool}
}

// flow1Row is a prepared flow-1 write plus the onSuccess clone payload.
type flow1Row struct {
	event          event.Event
	dedupKey       string
	onSuccessValue []byte
}

// Sink prepares each datum, batch-writes flow-1 rows, then emits per-datum responses.
func (s *MonoVertexPrimarySink) Sink(ctx context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	// kind: 0 = fail, 1 = flow1 (flow1Idx valid), 2 = flow2 fallback.
	type prepared struct {
		datumID  string
		kind     int
		flow1Idx int
		failErr  string
	}

	var items []prepared
	var flow1Rows []flow1Row

	for datum := range datumStreamCh {
		var ev event.Event
		if err := json.Unmarshal(datum.Value(), &ev); err != nil {
			log.Printf("MonoVertexPrimarySink: failed to prepare datum %s: %v", datum.ID(), err)
			items = append(items, prepared{datumID: datum.ID(), kind: 0, failErr: err.Error()})
			continue
		}
		routeTag := derefStr(ev.RouteTag)
		switch routeTag {
		case "normal-onsuccess":
			primary := ev
			pb := "mv-primary"
			primary.ProcessedBy = &pb

			onSuccess := primary
			po := "mv-onsuccess"
			onSuccess.ProcessedBy = &po
			onSuccessValue, _ := json.Marshal(onSuccess)

			flow1Rows = append(flow1Rows, flow1Row{
				event:          primary,
				dedupKey:       event.DedupKey(primary.EventID, pb, primary.ChildIndex),
				onSuccessValue: onSuccessValue,
			})
			items = append(items, prepared{datumID: datum.ID(), kind: 1, flow1Idx: len(flow1Rows) - 1})
		case "normal-fallback":
			items = append(items, prepared{datumID: datum.ID(), kind: 2})
		default:
			log.Printf("MonoVertexPrimarySink: unexpected route_tag %q", routeTag)
			items = append(items, prepared{datumID: datum.ID(), kind: 0, failErr: "unexpected route_tag: " + routeTag})
		}
	}

	batchErr := ""
	if len(flow1Rows) > 0 {
		batchErr = s.writeBatch(ctx, flow1Rows)
	}

	responses := sinker.ResponsesBuilder()
	for _, it := range items {
		switch it.kind {
		case 0:
			responses = responses.Append(sinker.ResponseFailure(it.datumID, it.failErr))
		case 2:
			responses = responses.Append(sinker.ResponseFallback(it.datumID))
		case 1:
			if batchErr != "" {
				responses = responses.Append(sinker.ResponseFailure(it.datumID, batchErr))
			} else {
				onSuccessMsg := sinker.NewMessage(flow1Rows[it.flow1Idx].onSuccessValue)
				responses = responses.Append(sinker.ResponseOnSuccess(it.datumID, onSuccessMsg))
			}
		}
	}
	return responses
}

// writeBatch upserts all flow-1 primary rows in a single transaction. Returns an
// empty string on success or an error message on failure.
func (s *MonoVertexPrimarySink) writeBatch(ctx context.Context, rows []flow1Row) string {
	batch := &pgx.Batch{}
	for _, row := range rows {
		payload, _ := json.Marshal(row.event)
		batch.Queue(upsertSinkEventSQL,
			row.dedupKey, row.event.EventID, string(payload),
			derefStr(row.event.ProcessedBy), row.event.ChildIndex, row.event.TotalChildren)
	}
	if err := s.pool.SendBatch(ctx, batch).Close(); err != nil {
		log.Printf("MonoVertexPrimarySink: batch of %d rows failed: %v", len(rows), err)
		return err.Error()
	}
	return ""
}
