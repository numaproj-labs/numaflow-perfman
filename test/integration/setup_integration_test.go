//go:build integration

package integration

import (
	"strings"
	"testing"

	"numa-perfman/internal/cli"
)

// §16.3 (1): setup is idempotent on an existing disposable cluster.
func TestSetupIdempotent(t *testing.T) {
	e := shared(t)

	code, out := e.runCLI("setup", "--image", e.setupImage)
	if code != cli.ExitSuccess {
		t.Fatalf("second setup exit=%d output:\n%s", code, out)
	}
	if !strings.Contains(out, "setup complete") {
		t.Fatalf("expected setup complete message, got:\n%s", out)
	}
}
