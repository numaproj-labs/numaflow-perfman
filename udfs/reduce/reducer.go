// Package reduce implements the fixed-window reduce UDF for the reduce-validations
// pipeline (also used by the sliding-window pipeline — the window geometry is
// configured in the pipeline YAML, not here).
package reduce

import (
	"context"
	"encoding/json"
	"log"
	"math"

	"github.com/numaproj/numaflow-go/pkg/reducer"

	"numa-perfman/udfs/internal/event"
)

// FixedWindowReducer aggregates events within a single (key, window): count, sum,
// min, max of amount, plus per-category counts. Numaflow creates one Reduce call per
// (key, window) pair via the creator below. Output is one message per window with the
// 8-field JSON payload and a dedup-key tag of "{reduce_key}_{window_start}".
type FixedWindowReducer struct{}

// Reduce drains the input channel, accumulates aggregates, and emits one message.
func (r *FixedWindowReducer) Reduce(_ context.Context, keys []string, inputCh <-chan reducer.Datum, md reducer.Metadata) reducer.Messages {
	eventCount := 0
	var totalAmount int64
	minAmount := math.MaxInt
	maxAmount := math.MinInt
	categoryCounts := map[string]int{}

	for datum := range inputCh {
		var ev event.ReduceEvent
		if err := json.Unmarshal(datum.Value(), &ev); err != nil {
			log.Printf("FixedWindowReducer: failed to decode event: %v", err)
			continue
		}
		eventCount++
		totalAmount += int64(ev.Amount)
		if ev.Amount < minAmount {
			minAmount = ev.Amount
		}
		if ev.Amount > maxAmount {
			maxAmount = ev.Amount
		}
		categoryCounts[ev.Category]++
	}

	reduceKey := keys[0]
	windowStart := md.IntervalWindow().StartTime().UnixMilli()
	windowEnd := md.IntervalWindow().EndTime().UnixMilli()

	out := event.ReduceOutput{
		ReduceKey:      reduceKey,
		WindowStart:    windowStart,
		WindowEnd:      windowEnd,
		EventCount:     eventCount,
		TotalAmount:    totalAmount,
		MinAmount:      minAmount,
		MaxAmount:      maxAmount,
		CategoryCounts: categoryCounts,
	}
	payload, _ := out.Marshal()
	dedupKey := event.ReduceDedupKey(reduceKey, windowStart)

	return reducer.MessagesBuilder().Append(reducer.NewMessage(payload).WithKeys([]string{dedupKey}))
}

// Creator is the reducer.ReducerCreator that yields a fresh reducer per (key, window).
type Creator struct{}

// Create returns a new FixedWindowReducer instance.
func (c *Creator) Create() reducer.Reducer { return &FixedWindowReducer{} }
