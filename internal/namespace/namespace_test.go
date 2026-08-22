package namespace_test

import (
	"testing"

	"github.com/google/uuid"
	"numa-perfman/internal/namespace"
)

func TestNameDeterministic(t *testing.T) {
	runID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000").String()
	n1, err := namespace.Name(namespace.KindBenchmark, runID)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := namespace.Name(namespace.KindBenchmark, runID)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != n2 {
		t.Fatalf("%q != %q", n1, n2)
	}
	if n1 != "numaflow-perf-550e8400" {
		t.Fatalf("name %q", n1)
	}
	vn, err := namespace.Name(namespace.KindValidation, runID)
	if err != nil {
		t.Fatal(err)
	}
	if vn != "numaflow-validation-550e8400" {
		t.Fatalf("validation name %q", vn)
	}
}

func TestIsActiveRunNamespace(t *testing.T) {
	if !namespace.IsActiveRunNamespace("perf-single-map-v1.8.0") {
		t.Fatal("expected perf prefix active")
	}
	if !namespace.IsActiveRunNamespace("numaflow-perf-abc") {
		t.Fatal("expected legacy active")
	}
	if !namespace.IsActiveRunNamespace("validation-map-v1-8-0") {
		t.Fatal("expected validation prefix active")
	}
	if !namespace.IsActiveRunNamespace("numaflow-validation-deadbeef") {
		t.Fatal("expected legacy validation active")
	}
	if namespace.IsActiveRunNamespace("default") {
		t.Fatal("default should not match")
	}
}
