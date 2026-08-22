package runmeta_test

import (
	"testing"

	"numa-perfman/internal/runmeta"
)

func TestImageTagSegment(t *testing.T) {
	if got := runmeta.ImageTagSegment("quay.io/numaproj/numaflow:v1.8.0"); got != "v1.8.0" {
		t.Fatalf("tag=%q", got)
	}
	if got := runmeta.ImageTagSegment("quay.io/numaproj/numaflow@sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"); got != "sha256-abcdef012345" {
		t.Fatalf("digest tag=%q", got)
	}
}

func TestDisplayImageTag(t *testing.T) {
	if got := runmeta.DisplayImageTag("quay.io/numaproj/numaflow@sha256:abc123"); got != "quay.io/numaproj/numaflow@sha256:abc123" {
		t.Fatalf("digest display=%q", got)
	}
}
