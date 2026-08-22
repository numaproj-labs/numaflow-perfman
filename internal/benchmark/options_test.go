package benchmark

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConfigJSONExcludesRemovedTimingOptions(t *testing.T) {
	cfg := Options{Duration: 2 * time.Minute}.ConfigJSON()
	var fields map[string]any
	if err := json.Unmarshal([]byte(cfg), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["duration"] != "2m0s" {
		t.Fatalf("duration = %v", fields["duration"])
	}
	for _, key := range []string{"repetitions", "warmup", "cooldown"} {
		if _, found := fields[key]; found {
			t.Fatalf("unexpected legacy option %q in config JSON", key)
		}
	}
}
