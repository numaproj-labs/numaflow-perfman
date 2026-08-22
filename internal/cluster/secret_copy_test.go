package cluster_test

import (
	"encoding/json"
	"testing"

	"numa-perfman/internal/cluster"
)

func TestSecretStringDataDecodesBase64(t *testing.T) {
	raw := `{"data":{"POSTGRES_USER":"bnVtYWZsb3c=","POSTGRES_PASSWORD":"bnVtYWZsb3c="}}`
	got, err := cluster.SecretStringData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["POSTGRES_USER"] != "numaflow" || got["POSTGRES_PASSWORD"] != "numaflow" {
		t.Fatalf("unexpected decode: %v", got)
	}
}

func TestRenderOpaqueSecretYAML(t *testing.T) {
	yaml, err := cluster.RenderOpaqueSecretYAML("sec", "ns", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(yaml, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["kind"] != "Secret" {
		t.Fatalf("kind %v", obj["kind"])
	}
}
