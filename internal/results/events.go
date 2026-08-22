package results

import (
	"context"
	"fmt"
	"strings"
)

// ListEvents returns run events ordered by timestamp ascending.
func (r *Repository) ListEvents(ctx context.Context, runID string) ([]RunEvent, error) {
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, timestamp, level, phase, message, detail_json
		FROM run_events WHERE run_id = ? ORDER BY timestamp ASC, id ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []RunEvent
	for rows.Next() {
		var ev RunEvent
		var ts string
		if err := rows.Scan(&ev.ID, &ev.RunID, &ts, &ev.Level, &ev.Phase, &ev.Message, &ev.DetailJSON); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		t, err := parseTime(ts)
		if err != nil {
			return nil, fmt.Errorf("parse event timestamp: %w", err)
		}
		ev.Timestamp = t
		out = append(out, ev)
	}
	return out, rows.Err()
}

// FormatEventsLog renders persisted events as newline-terminated log lines.
func FormatEventsLog(events []RunEvent) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(ev.Timestamp.UTC().Format(timeLayout))
		b.WriteByte('\t')
		b.WriteString(ev.Level)
		if ev.Phase != "" {
			b.WriteByte('\t')
			b.WriteString(ev.Phase)
		}
		b.WriteByte('\t')
		b.WriteString(ev.Message)
		b.WriteByte('\n')
	}
	return b.String()
}
