package results

import (
	"encoding/json"
	"fmt"
)

// normalizeJSON ensures v is stored as compact valid JSON; objects default to {}.
func normalizeJSON(raw string, emptyObject bool) (string, error) {
	if raw == "" {
		if emptyObject {
			return "{}", nil
		}
		return "", nil
	}
	if !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("invalid JSON: %q", truncate(raw, 80))
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("unmarshal JSON: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}
	return string(b), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
