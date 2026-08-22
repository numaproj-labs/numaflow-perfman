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

// AdEventSource reads pre-populated ad events from source_events and emits them with
// a "route-tag" header that the source transformer converts into a Numaflow tag.
type AdEventSource struct {
	*baseSource
}

// NewAdEventSource constructs the ad-event source and recovers its state.
func NewAdEventSource(ctx context.Context, pool *pgxpool.Pool) *AdEventSource {
	s := &AdEventSource{}
	s.baseSource = newBaseSource(ctx, pool, "AdEventSource", s.emit)
	return s
}

func (s *AdEventSource) emit(ctx context.Context, status string, limit int, messageCh chan<- sourcer.Message) int {
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
		log.Printf("AdEventSource: query %s events failed: %v", status, err)
		return 0
	}
	defer rows.Close()

	type rowData struct {
		ev       event.Event
		routeTag string
	}
	var batch []rowData
	for rows.Next() {
		var ev event.Event
		var eventTime time.Time
		var routeTag string
		if err := rows.Scan(&ev.EventID, &ev.UserID, &ev.PageID, &ev.AdType, &ev.EventType,
			&eventTime, &ev.IpAddress, &routeTag); err != nil {
			log.Printf("AdEventSource: scan failed: %v", err)
			continue
		}
		ev.EventTime = eventTime.UnixMilli()
		batch = append(batch, rowData{ev: ev, routeTag: routeTag})
	}
	if err := rows.Err(); err != nil {
		log.Printf("AdEventSource: row iteration error: %v", err)
	}
	if len(batch) == 0 {
		return 0
	}

	log.Printf("AdEventSource: sending %d %s events", len(batch), status)
	for _, r := range batch {
		s.markPending(r.ev.EventID)
		payload, _ := json.Marshal(r.ev)
		msg := sourcer.NewMessage(
			payload,
			sourcer.NewOffsetWithDefaultPartitionId([]byte(r.ev.EventID)),
			time.UnixMilli(r.ev.EventTime),
		).WithHeaders(map[string]string{"route-tag": r.routeTag})
		messageCh <- msg
	}
	return len(batch)
}
