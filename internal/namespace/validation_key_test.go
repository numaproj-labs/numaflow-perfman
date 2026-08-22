package namespace_test

import (
	"strings"
	"testing"

	"numa-perfman/internal/namespace"
)

func TestValidationKey(t *testing.T) {
	key, err := namespace.ParseValidationKey("map", "quay.io/numaproj/numaflow:v1.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if key.Namespace != "validation-map-v1-8-0" {
		t.Fatalf("namespace=%q", key.Namespace)
	}
	if key.LockFileName != "validation-map-v1-8-0.lock" {
		t.Fatalf("lock=%q", key.LockFileName)
	}
}

func TestValidationKeySlidingReduce(t *testing.T) {
	key, err := namespace.ParseValidationKey("sliding-reduce", "quay.io/numaproj/numaflow:v1.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if key.Namespace != "validation-sliding-reduce-v1-8-0" {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestValidationKeyDigest(t *testing.T) {
	key, err := namespace.ParseValidationKey("map", "quay.io/numaproj/numaflow@sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key.Namespace, "validation-map-sha256-abcdef012345") {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestValidationKeyMaxLength(t *testing.T) {
	longTag := "release-" + strings.Repeat("a", 50) + "-final"
	key, err := namespace.ParseValidationKey("sliding-reduce", "quay.io/numaproj/numaflow:"+longTag)
	if err != nil {
		t.Fatal(err)
	}
	if len(key.Namespace) > 63 {
		t.Fatalf("namespace length %d: %q", len(key.Namespace), key.Namespace)
	}
	if !strings.HasPrefix(key.Namespace, "validation-") {
		t.Fatalf("namespace=%q", key.Namespace)
	}
}

func TestValidationKeyInvalidScenario(t *testing.T) {
	_, err := namespace.ParseValidationKey("not-a-scenario", "quay.io/numaproj/numaflow:v1.8.0")
	if err == nil {
		t.Fatal("expected error")
	}
}
