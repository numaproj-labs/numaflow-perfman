package oracle_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"numa-perfman/internal/oracle"
)

func TestGoldenMapExpectedPayloads(t *testing.T) {
	t.Parallel()
	goldenPath := filepath.Join("testdata", "golden_map_expected.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	events := map[string]oracle.Event{
		"evt-1": {
			EventID: "evt-1", UserID: "user-42", PageID: "page-7",
			AdType: "banner", EventType: "impression", EventTime: 1700000000000, IpAddress: "10.1.2.3",
		},
		"evt-2": {
			EventID: "evt-2", UserID: "user-99", PageID: "page-1",
			AdType: "video", EventType: "click", EventTime: 1700000000001, IpAddress: "8.8.8.8",
		},
	}

	for dedupKey, want := range golden {
		eventID, mapName, childIdx := splitMapDedup(t, dedupKey)
		ev := events[eventID]
		outputs := oracle.ProduceMapOutputs(ev, mapName)
		var match *oracle.Event
		for i := range outputs {
			if outputs[i].ChildIndex == childIdx {
				match = &outputs[i]
				break
			}
		}
		if match == nil {
			t.Fatalf("%s: no output child %s", dedupKey, childIdx)
		}
		got, err := oracle.CanonicalMapEventJSON(*match)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s payload mismatch\n got:  %s\n want: %s", dedupKey, got, want)
		}
	}
}

func splitMapDedup(t *testing.T, key string) (eventID, mapName, childIdx string) {
	t.Helper()
	i := len(key) - 1
	for i >= 0 && key[i] != '_' {
		i--
	}
	if i <= 0 {
		t.Fatalf("bad dedup key %q", key)
	}
	childIdx = key[i+1:]
	rest := key[:i]
	j := len(rest) - 1
	for j >= 0 && rest[j] != '_' {
		j--
	}
	if j <= 0 {
		t.Fatalf("bad dedup key %q", key)
	}
	mapName = rest[j+1:]
	eventID = rest[:j]
	return
}

func TestGenerateDeterministic(t *testing.T) {
	t.Parallel()
	cfg := oracle.GenerationConfig{Seed: 42, EventCount: 100, BaseEventTimeMs: oracle.DefaultBaseEventTimeMs}
	a, err := oracle.Generate(oracle.ScenarioMap, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := oracle.Generate(oracle.ScenarioMap, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expected) != len(b.Expected) {
		t.Fatalf("expected count drift: %d vs %d", len(a.Expected), len(b.Expected))
	}
	for i := range a.Expected {
		if a.Expected[i].LogicalKey != b.Expected[i].LogicalKey {
			t.Fatalf("key drift at %d", i)
		}
		if !oracle.PayloadsEqual(a.Expected[i].Payload, b.Expected[i].Payload) {
			t.Fatalf("payload drift at %d", i)
		}
	}
}

func TestReduceGenerateWindowCount(t *testing.T) {
	t.Parallel()
	cfg := oracle.GenerationConfig{
		Seed: 1, EventCount: 1000,
		BaseEventTimeMs: oracle.DefaultBaseEventTimeMs,
		SpacingMs:       oracle.DefaultEventSpacingMs,
	}
	b, err := oracle.Generate(oracle.ScenarioReduce, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ReduceSources) != 1000 {
		t.Fatalf("sources = %d", len(b.ReduceSources))
	}
	if len(b.Expected) == 0 {
		t.Fatal("expected empty")
	}
}
