package runmeta

import (
	"encoding/json"
	"testing"

	"numa-perfman/internal/config"
)

func TestFromConfigIncludesResources(t *testing.T) {
	s := FromConfig(config.Config{Cluster: "numaflow", Context: "kind-numaflow"}, "1.2.3", DefaultBenchmarkResources)
	if s.Cluster != "numaflow" || s.ToolVersion != "1.2.3" {
		t.Fatalf("snapshot: %+v", s)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(s.JSON()), &raw); err != nil {
		t.Fatal(err)
	}
	res, ok := raw["resources"].(map[string]any)
	if !ok {
		t.Fatalf("resources missing: %#v", raw)
	}
	req, ok := res["requests"].(map[string]any)
	if !ok || req["cpu"] != "1" {
		t.Fatalf("requests: %#v", req)
	}
}
