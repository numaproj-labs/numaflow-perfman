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

// MonoVertexSource reads source events for the MonoVertex pipeline. Unlike
// AdEventSource it embeds route_tag directly in the serialized payload (Event.RouteTag)
// so downstream stages can read it without re-deserializing, and also sets route-tag
// as a message header for the transformer to read cheaply.
type MonoVertexSource struct {
	*baseSource
}

// NewMonoVertexSource constructs the MonoVertex source and recovers its state.
func NewMonoVertexSource(ctx context.Context, pool *pgxpool.Pool) *MonoVertexSource {
	s := &MonoVertexSource{}
	s.baseSource = newBaseSource(ctx, pool, "MonoVertexSource", s.emit)
	return s
}

func (s *MonoVertexSource) emit(ctx context.Context, status string, limit int, messageCh chan<- sourcer.Message) int {
	if limit <= 0 {
		return 0
	}

	rows, err := s.pool.Query(ctx, `
		SELECT event_id, user_id, page_id, ad_type, event_type, event_time, ip_address, route_tag
		FROM source_events
		WHERE ack_status = $1
		ORDER BY created_at ASC
		LIMIT $2`, status, limit)
	if err != nil {
		log.Printf("MonoVertexSource: query %s events failed: %v", status, err)
		return 0
	}
	defer rows.Close()

	var batch []event.Event
	for rows.Next() {
		var ev event.Event
		var eventTime time.Time
		var routeTag string
		if err := rows.Scan(&ev.EventID, &ev.UserID, &ev.PageID, &ev.AdType, &ev.EventType,
			&eventTime, &ev.IpAddress, &routeTag); err != nil {
			log.Printf("MonoVertexSource: scan failed: %v", err)
			continue
		}
		ev.EventTime = eventTime.UnixMilli()
		ev.RouteTag = &routeTag
		batch = append(batch, ev)
	}
	if err := rows.Err(); err != nil {
		log.Printf("MonoVertexSource: row iteration error: %v", err)
	}
	if len(batch) == 0 {
		return 0
	}

	log.Printf("MonoVertexSource: sending %d %s events", len(batch), status)
	for _, ev := range batch {
		s.markPending(ev.EventID)
		// Serialize WITH route_tag in the payload.
		payload, _ := json.Marshal(ev)
		routeTag := ""
		if ev.RouteTag != nil {
			routeTag = *ev.RouteTag
		}
		msg := sourcer.NewMessage(
			payload,
			sourcer.NewOffsetWithDefaultPartitionId([]byte(ev.EventID)),
			time.UnixMilli(ev.EventTime),
		).WithHeaders(map[string]string{"route-tag": routeTag})
		messageCh <- msg
	}
	return len(batch)
}
