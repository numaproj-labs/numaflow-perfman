package setup_test

import (
	"testing"

	"numa-perfman/internal/setup"
)

func TestParseImageMajorMinor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ref   string
		ok    bool
		major int
		minor int
		raw   string
	}{
		{"quay.io/numaproj/numaflow:v1.8.0", true, 1, 8, "1.8"},
		{"numaflow:local", false, 0, 0, ""},
		{"quay.io/numaproj/numaflow@sha256:abc", false, 0, 0, ""},
		{"localhost/numaflow:1.7.2", true, 1, 7, "1.7"},
	}
	for _, c := range cases {
		got, ok := setup.ParseImageMajorMinor(c.ref)
		if ok != c.ok {
			t.Fatalf("%q ok=%v want %v", c.ref, ok, c.ok)
		}
		if !ok {
			continue
		}
		if got.Major != c.major || got.Minor != c.minor || got.Raw != c.raw {
			t.Fatalf("%q = %+v want %d.%d raw %q", c.ref, got, c.major, c.minor, c.raw)
		}
	}
}

func TestCompatibilityWarnings(t *testing.T) {
	t.Parallel()
	w := setup.CompatibilityWarnings(
		"quay.io/numaproj/numaflow:v1.9.0",
		"quay.io/numaproj/numaflow:v1.8.0",
		setup.DefaultTestedMajorMinor,
	)
	if len(w) < 2 {
		t.Fatalf("expected UI range + mismatch warnings, got %d", len(w))
	}
	aligned := setup.CompatibilityWarnings(
		"quay.io/numaproj/numaflow:v1.8.0",
		"quay.io/numaproj/numaflow:v1.8.0",
		setup.DefaultTestedMajorMinor,
	)
	if len(aligned) != 0 {
		t.Fatalf("aligned tags should not warn: %+v", aligned)
	}
}
