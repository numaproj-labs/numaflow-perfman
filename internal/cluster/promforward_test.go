package cluster_test

import (
	"testing"

	"numa-perfman/internal/cluster"
)

func TestNumaflowServerUIURL(t *testing.T) {
	if got := cluster.NumaflowServerUIURL(); got != "https://127.0.0.1:8443" {
		t.Fatalf("NumaflowServerUIURL() = %q", got)
	}
	if cluster.NumaflowServerLocalPort != 8443 {
		t.Fatalf("NumaflowServerLocalPort = %d", cluster.NumaflowServerLocalPort)
	}
}
