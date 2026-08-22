package report

import "testing"

func TestSourceDisplayLabel(t *testing.T) {
	if got := sourceDisplayLabel("quay.io/numaproj/numaflow:v1.8.0"); got != "v1.8.0" {
		t.Fatalf("image tag label=%q", got)
	}
	if got := sourceDisplayLabel(""); got != "source unavailable" {
		t.Fatalf("empty image ref=%q", got)
	}
	if got := sourceDisplayLabel("quay.io/numaproj/numaflow@sha256:abc123"); got != "quay.io/numaproj/numaflow@sha256:abc123" {
		t.Fatalf("digest image ref=%q", got)
	}
}
