package namespace_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/namespace"
)

func TestBenchmarkKey(t *testing.T) {
	key, err := namespace.ParseBenchmarkKey("single-map", "quay.io/numaproj/numaflow:v1.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if key.Namespace != "perf-single-map-v1-8-0" {
		t.Fatalf("namespace=%q", key.Namespace)
	}
	if key.LockFileName != "perf-single-map-v1-8-0.lock" {
		t.Fatalf("lock=%q", key.LockFileName)
	}
}

func TestBenchmarkKeyDigest(t *testing.T) {
	key, err := namespace.ParseBenchmarkKey("single-map", "quay.io/numaproj/numaflow@sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key.Namespace, "perf-single-map-sha256-abcdef012345") {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestBenchmarkKeySanitizesSpecialChars(t *testing.T) {
	key, err := namespace.ParseBenchmarkKey("single-map", "registry.io/numaflow:Local_Test")
	if err != nil {
		t.Fatal(err)
	}
	if key.Namespace != "perf-single-map-local-test" {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestBenchmarkKeyMaxLength(t *testing.T) {
	longTag := "release-" + strings.Repeat("a", 50) + "-final"
	key, err := namespace.ParseBenchmarkKey("single-map", "quay.io/numaproj/numaflow:"+longTag)
	if err != nil {
		t.Fatal(err)
	}
	if len(key.Namespace) > 63 {
		t.Fatalf("namespace length %d: %q", len(key.Namespace), key.Namespace)
	}
	if !strings.HasPrefix(key.Namespace, "perf-single-map-") {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestBenchmarkKeyInvalidScenario(t *testing.T) {
	_, err := namespace.ParseBenchmarkKey("not-a-scenario", "quay.io/numaproj/numaflow:v1.8.0")
	if err == nil {
		t.Fatal("expected error")
	}
}
