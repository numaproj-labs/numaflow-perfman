package source

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/sourcer"

	"numa-perfman/udfs/internal/event"
)

// ReduceSource reads pre-populated reduce events from source_events and emits them
// with message key = reduce_key (for keyed reduce partitioning) and event time from
// the DB. Unlike AdEventSource it sets no route-tag header (single downstream vertex).
type ReduceSource struct {
	*baseSource
}

// NewReduceSource constructs the reduce source and recovers its state.
func NewReduceSource(ctx context.Context, pool *pgxpool.Pool) *ReduceSource {
	s := &ReduceSource{}
	s.baseSource = newBaseSource(ctx, pool, "ReduceSource", s.emit)
	return s
}

func (s *ReduceSource) emit(ctx context.Context, status string, limit int, messageCh chan<- sourcer.Message) int {
	if limit <= 0 {
		return 0
	}

	rows, err := s.pool.Query(ctx, `
		SELECT event_id, reduce_key, category, amount, event_time
		FROM source_events
		WHERE ack_status = $1
		ORDER BY created_at ASC
		LIMIT $2`, status, limit)
	if err != nil {
		log.Printf("ReduceSource: query %s events failed: %v", status, err)
		return 0
	}
	defer rows.Close()

	var batch []event.ReduceEvent
	for rows.Next() {
		var ev event.ReduceEvent
		if err := rows.Scan(&ev.EventID, &ev.ReduceKey, &ev.Category, &ev.Amount, &ev.EventTime); err != nil {
			log.Printf("ReduceSource: scan failed: %v", err)
			continue
		}
		batch = append(batch, ev)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ReduceSource: row iteration error: %v", err)
	}
	if len(batch) == 0 {
		return 0
	}

	log.Printf("ReduceSource: sending %d %s events", len(batch), status)
	for _, ev := range batch {
		s.markPending(ev.EventID)
		// EventTime is excluded from the payload (json:"-"); it travels as the
		// Numaflow message event time so the reducer's window boundaries match.
		payload, _ := json.Marshal(ev)
		msg := sourcer.NewMessage(
			payload,
			sourcer.NewOffsetWithDefaultPartitionId([]byte(ev.EventID)),
			time.UnixMilli(ev.EventTime),
		).WithKeys([]string{ev.ReduceKey})
		messageCh <- msg
	}
	return len(batch)
}
