package event

import (
	"encoding/json"
	"strconv"
)

// ReduceDedupKey builds the reduce sink dedup key "{reduce_key}_{window_start}".
// Format must match the orchestrator's reduce datagen and the reduce sink.
func ReduceDedupKey(reduceKey string, windowStart int64) string {
	return reduceKey + "_" + strconv.FormatInt(windowStart, 10)
}

// ReduceEvent is the source event model for the reduce validation pipeline.
// Serializes to a 4-field JSON payload emitted by the reduce source. EventTime is
// read from the DB but excluded from the payload (json:"-") — it is carried as the
// Numaflow message event time, matching the orchestrator's ReduceSourceEvent.
type ReduceEvent struct {
	EventID   string `json:"event_id"`
	ReduceKey string `json:"reduce_key"`
	Category  string `json:"category"`
	Amount    int    `json:"amount"`
	EventTime int64  `json:"-"`
}

// ReduceOutput is the 8-field JSON emitted by the reducer and stored as-is in
// sink_events.payload for JSONB equality validation. Field order and tags match
// the orchestrator's ReduceExpectedOutput / marshalDeterministic.
type ReduceOutput struct {
	ReduceKey      string         `json:"reduce_key"`
	WindowStart    int64          `json:"window_start"`
	WindowEnd      int64          `json:"window_end"`
	EventCount     int            `json:"event_count"`
	TotalAmount    int64          `json:"total_amount"`
	CategoryCounts map[string]int `json:"category_counts"`
	MinAmount      int            `json:"min_amount"`
	MaxAmount      int            `json:"max_amount"`
}

// Marshal serializes the reduce output to JSON. Go's encoding/json marshals map
// keys in sorted order, so category_counts is deterministic — matching the
// orchestrator's marshalDeterministic, which explicitly sorts the keys.
func (o ReduceOutput) Marshal() ([]byte, error) {
	return json.Marshal(o)
}
