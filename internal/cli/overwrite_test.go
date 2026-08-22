package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/results"
)

func TestBenchmarkOverwriteConfirmer(t *testing.T) {
	existing := results.Run{
		ID:        "0f9fad5b-d9cb-469f-a165-70867728950e",
		Scenario:  "single-map",
		ImageRef:  "quay.io/numaproj/numaflow:v1.8.0",
		Status:    results.StatusCompleted,
		CreatedAt: time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	}

	var output bytes.Buffer
	confirmed, err := (benchmarkOverwriteConfirmer{
		input: strings.NewReader("yes\n"), output: &output,
	}).ConfirmOverwrite(context.Background(), existing)
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%v err=%v", confirmed, err)
	}
	if !strings.Contains(output.String(), existing.ImageRef) {
		t.Fatalf("prompt does not identify existing result: %q", output.String())
	}

	confirmed, err = (benchmarkOverwriteConfirmer{
		input: strings.NewReader("\n"), output: &output,
	}).ConfirmOverwrite(context.Background(), existing)
	if err != nil || confirmed {
		t.Fatalf("empty confirmation = %v, %v", confirmed, err)
	}
}
