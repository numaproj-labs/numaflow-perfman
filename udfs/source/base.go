// Package source implements the Numaflow UDSource handlers for the validation
// pipelines: the ad-event source (map-validations), the reduce source, and the
// MonoVertex source.
//
// All three share the same coordination protocol with the orchestrator (see
// SOURCE_CONTRACT.md): events are pre-populated in source_events, the source emits
// them, tracks ACK/NACK in postgres, and sets run_config.source_completed = TRUE
// once every event is ACKed. That shared lifecycle lives in baseSource; each
// concrete source only supplies how to fetch rows and turn them into messages.
package source

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/numaproj/numaflow-go/pkg/sourcer"
)

// emitFunc sends the events of the given ack_status (up to limit) to the observer
// and returns how many were sent. It also records them as pending.
type emitFunc func(ctx context.Context, status string, limit int, messageCh chan<- sourcer.Message) int

// baseSource holds the shared ACK/NACK lifecycle state and logic for all three
// sources. The concrete source embeds it and wires emit to its own row→message
// conversion.
type baseSource struct {
	pool *pgxpool.Pool
	name string
	emit emitFunc

	pending         sync.Map // event_id -> struct{}
	pendingCount    atomic.Int64
	totalEvents     atomic.Int64
	sourceCompleted atomic.Bool
	dbAckedCount    atomic.Int64

	stopRefresh chan struct{}
}

func newBaseSource(ctx context.Context, pool *pgxpool.Pool, name string, emit emitFunc) *baseSource {
	b := &baseSource{
		pool:        pool,
		name:        name,
		emit:        emit,
		stopRefresh: make(chan struct{}),
	}
	b.recoverState(ctx)

	// Periodically refresh the ACKed count from the DB (every 3s), matching the
	// Kotlin countRefreshScheduler.
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.refreshAckedCount(ctx)
			case <-b.stopRefresh:
				return
			}
		}
	}()

	log.Printf("%s initialized: totalEvents=%d, ackedCount=%d, sourceCompleted=%t",
		name, b.totalEvents.Load(), b.dbAckedCount.Load(), b.sourceCompleted.Load())
	return b
}

// recoverState reads run_config on startup to determine total_events and whether
// the source has already completed (the pipeline may have restarted).
func (b *baseSource) recoverState(ctx context.Context) {
	var total int64
	var completed bool
	err := b.pool.QueryRow(ctx,
		"SELECT total_events, source_completed FROM run_config LIMIT 1").Scan(&total, &completed)
	if err != nil {
		log.Fatalf("%s: run_config must be initialized by the orchestrator before start: %v", b.name, err)
	}
	b.totalEvents.Store(total)
	b.sourceCompleted.Store(completed)

	b.refreshAckedCount(ctx)

	if completed {
		log.Printf("%s already completed on startup. Returning empty from Read().", b.name)
	} else {
		log.Printf("%s resuming: %d/%d events already ACKed.", b.name, b.dbAckedCount.Load(), total)
	}
}

func (b *baseSource) refreshAckedCount(ctx context.Context) {
	var count int64
	if err := b.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM source_events WHERE ack_status = 'ACKED'").Scan(&count); err != nil {
		log.Printf("%s: failed to refresh ACKed count: %v", b.name, err)
		return
	}
	b.dbAckedCount.Store(count)
}

// Read emits NACKED events first (highest priority), then PENDING events, up to the
// requested count. Mirrors the Kotlin read() ordering.
func (b *baseSource) Read(ctx context.Context, req sourcer.ReadRequest, messageCh chan<- sourcer.Message) {
	if b.sourceCompleted.Load() {
		// Already done — sleep to avoid busy-waiting.
		timeout := max(req.TimeOut(), 5*time.Second)
		select {
		case <-time.After(timeout):
		case <-ctx.Done():
		}
		return
	}

	requested := int(req.Count())
	sent := 0
	sent += b.emit(ctx, "NACKED", requested-sent, messageCh)
	if sent < requested {
		sent += b.emit(ctx, "PENDING", requested-sent, messageCh)
	}

	b.checkAndMarkCompleted(ctx)
}

func (b *baseSource) Ack(ctx context.Context, req sourcer.AckRequest) {
	eventIDs := offsetsToIDs(req.Offsets())
	if len(eventIDs) == 0 {
		return
	}
	if _, err := b.pool.Exec(ctx,
		"UPDATE source_events SET ack_status = 'ACKED' WHERE event_id = ANY($1)", eventIDs); err != nil {
		log.Printf("%s: ack update failed: %v", b.name, err)
	}
	for _, id := range eventIDs {
		if _, loaded := b.pending.LoadAndDelete(id); loaded {
			b.pendingCount.Add(-1)
		}
	}
	b.checkAndMarkCompleted(ctx)
}

func (b *baseSource) Nack(ctx context.Context, req sourcer.NackRequest) {
	eventIDs := offsetsToIDs(req.Offsets())
	if len(eventIDs) == 0 {
		return
	}
	log.Printf("%s: nack received for %d events", b.name, len(eventIDs))
	if _, err := b.pool.Exec(ctx,
		"UPDATE source_events SET ack_status = 'NACKED' WHERE event_id = ANY($1)", eventIDs); err != nil {
		log.Printf("%s: nack update failed: %v", b.name, err)
	}
	for _, id := range eventIDs {
		if _, loaded := b.pending.LoadAndDelete(id); loaded {
			b.pendingCount.Add(-1)
		}
	}
}

// Pending returns the number of un-ACKed events plus events needing retry, or 0
// once the source has completed.
func (b *baseSource) Pending(ctx context.Context) int64 {
	if b.sourceCompleted.Load() {
		return 0
	}
	remaining := max(b.totalEvents.Load()-b.dbAckedCount.Load(), 0)
	return remaining + b.countEventsNeedingRetry(ctx)
}

// ActivePartitions returns the default partitions — these sources are not
// partitioned, so watermark publishing uses the platform default.
func (b *baseSource) ActivePartitions(ctx context.Context) []int32 {
	return sourcer.DefaultPartitions()
}

// TotalPartitions returns nil: these sources don't report a partition count.
func (b *baseSource) TotalPartitions(ctx context.Context) *int32 {
	return nil
}

// checkAndMarkCompleted sets run_config.source_completed = TRUE once every event is
// ACKed and nothing is in flight or awaiting retry.
func (b *baseSource) checkAndMarkCompleted(ctx context.Context) {
	if b.sourceCompleted.Load() {
		return
	}
	acked := b.dbAckedCount.Load()
	target := b.totalEvents.Load()
	if target <= 0 || acked < target {
		return
	}
	if b.pendingCount.Load() > 0 {
		return
	}
	if b.countEventsNeedingRetry(ctx) > 0 {
		return
	}

	log.Printf("%s: all %d events ACKed. Setting source_completed = TRUE.", b.name, acked)
	if _, err := b.pool.Exec(ctx, "UPDATE run_config SET source_completed = TRUE"); err != nil {
		log.Printf("%s: failed to set source_completed: %v", b.name, err)
		return
	}
	b.sourceCompleted.Store(true)
}

func (b *baseSource) countEventsNeedingRetry(ctx context.Context) int64 {
	var count int64
	if err := b.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM source_events WHERE ack_status IN ('PENDING', 'NACKED')").Scan(&count); err != nil {
		log.Printf("%s: failed to count events needing retry: %v", b.name, err)
		return 0
	}
	return count
}

// markPending records an event_id as in-flight.
func (b *baseSource) markPending(eventID string) {
	if _, loaded := b.pending.LoadOrStore(eventID, struct{}{}); !loaded {
		b.pendingCount.Add(1)
	}
}

func offsetsToIDs(offsets []sourcer.Offset) []string {
	ids := make([]string, 0, len(offsets))
	for _, o := range offsets {
		ids = append(ids, string(o.Value()))
	}
	return ids
}
