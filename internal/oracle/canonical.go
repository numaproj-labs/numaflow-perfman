package oracle

import (
	"bytes"
	"encoding/json"
	"sort"
)

// CanonicalMapEventJSON marshals a map/monovertex sink Event with stable field omission.
func CanonicalMapEventJSON(e Event) ([]byte, error) {
	return json.Marshal(e)
}

// CanonicalReduceJSON marshals reduce output with sorted category_counts keys.
func CanonicalReduceJSON(o ReduceOutput) ([]byte, error) {
	keys := make([]string, 0, len(o.CategoryCounts))
	for k := range o.CategoryCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]int, len(keys))
	for _, k := range keys {
		ordered[k] = o.CategoryCounts[k]
	}
	type row struct {
		ReduceKey      string         `json:"reduce_key"`
		WindowStart    int64          `json:"window_start"`
		WindowEnd      int64          `json:"window_end"`
		EventCount     int            `json:"event_count"`
		TotalAmount    int64          `json:"total_amount"`
		CategoryCounts map[string]int `json:"category_counts"`
		MinAmount      int            `json:"min_amount"`
		MaxAmount      int            `json:"max_amount"`
	}
	return json.Marshal(row{
		ReduceKey:      o.ReduceKey,
		WindowStart:    o.WindowStart,
		WindowEnd:      o.WindowEnd,
		EventCount:     o.EventCount,
		TotalAmount:    o.TotalAmount,
		CategoryCounts: ordered,
		MinAmount:      o.MinAmount,
		MaxAmount:      o.MaxAmount,
	})
}

// PayloadsEqual compares canonical JSON byte slices (exact equality).
func PayloadsEqual(expected, actual []byte) bool {
	return bytes.Equal(expected, actual)
}
