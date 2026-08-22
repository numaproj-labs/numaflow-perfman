package oracle_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"numa-perfman/internal/oracle"
)

const goldenEventCount = int64(30)

func TestGoldenReduceExpectedPayloads(t *testing.T) {
	t.Parallel()
	assertScenarioGolden(t, "golden_reduce_expected.json", oracle.ScenarioReduce, 42, goldenEventCount)
}

func TestGoldenSlidingReduceExpectedPayloads(t *testing.T) {
	t.Parallel()
	assertScenarioGolden(t, "golden_sliding_reduce_expected.json", oracle.ScenarioSlidingReduce, 42, goldenEventCount)
}

func TestGoldenMonoVertexExpectedPayloads(t *testing.T) {
	t.Parallel()
	assertScenarioGolden(t, "golden_monovertex_expected.json", oracle.ScenarioMonoVertex, 42, 10)
}

func assertScenarioGolden(t *testing.T, file string, scen oracle.Scenario, seed uint64, count int64) {
	t.Helper()
	goldenPath := filepath.Join("testdata", file)
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]string
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	bundle, err := oracle.Generate(scen, oracle.GenerationConfig{
		Seed: seed, EventCount: count,
		BaseEventTimeMs: oracle.DefaultBaseEventTimeMs,
		SpacingMs:       oracle.DefaultEventSpacingMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(bundle.Expected))
	for _, e := range bundle.Expected {
		got[e.LogicalKey] = string(e.Payload)
	}
	if len(got) != len(golden) {
		t.Fatalf("expected count %d golden keys %d", len(got), len(golden))
	}
	for key, want := range golden {
		g, ok := got[key]
		if !ok {
			t.Fatalf("oracle missing golden key %q", key)
		}
		if g != want {
			t.Errorf("%s payload mismatch\n got:  %s\n want: %s", key, g, want)
		}
	}
}

func TestWriteGoldenFixtures(t *testing.T) {
	if os.Getenv("WRITE_GOLDEN") == "" {
		t.Skip("set WRITE_GOLDEN=1 to regenerate oracle testdata")
	}
	writeGolden(t, "golden_reduce_expected.json", oracle.ScenarioReduce, 42, goldenEventCount)
	writeGolden(t, "golden_sliding_reduce_expected.json", oracle.ScenarioSlidingReduce, 42, goldenEventCount)
	writeGolden(t, "golden_monovertex_expected.json", oracle.ScenarioMonoVertex, 42, 10)
}

func writeGolden(t *testing.T, name string, scen oracle.Scenario, seed uint64, count int64) {
	t.Helper()
	b, err := oracle.Generate(scen, oracle.GenerationConfig{
		Seed: seed, EventCount: count,
		BaseEventTimeMs: oracle.DefaultBaseEventTimeMs,
		SpacingMs:       oracle.DefaultEventSpacingMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]string, len(b.Expected))
	for _, e := range b.Expected {
		m[e.LogicalKey] = string(e.Payload)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name)
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
