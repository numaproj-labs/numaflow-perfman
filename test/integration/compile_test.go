package integration

import "testing"

// TestCompileWithoutIntegrationTag ensures this package is included in default
// go test ./... runs. Integration scenarios live in files with //go:build integration.
func TestCompileWithoutIntegrationTag(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	t.Log("integration E2E tests require: go test -tags=integration ./test/integration and PERFHARNESS_INTEGRATION=1")
}
