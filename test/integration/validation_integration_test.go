//go:build integration

package integration

import (
	"strings"
	"testing"

	"numa-perfman/internal/cli"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/validation"
)

// §16.3 (11–12): validation duplicate reporting passes; correctness defects fail (oracle compare contract).
func TestValidationDuplicatePassCompare(t *testing.T) {
	requireIntegration(t)

	bundle, err := oracle.Generate(oracle.ScenarioMap, oracle.GenerationConfig{Seed: 1, EventCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	actual := make([]validation.PhysicalDelivery, len(bundle.Expected))
	for i, e := range bundle.Expected {
		actual[i] = validation.PhysicalDelivery{
			LogicalKey: e.LogicalKey, EventID: e.EventID,
			ProcessedBy: e.ProcessedBy, ChildIndex: e.ChildIndex, TotalChildren: e.TotalChildren,
			Payload: e.Payload, ReceiveCount: 1,
		}
	}
	actual[0].ReceiveCount = 4
	res := validation.CompareExpected(bundle.Expected, actual, validation.CompareOptions{})
	if !res.Passed {
		t.Fatalf("duplicates should pass logical compare: %+v", res)
	}
	if res.DuplicateDeliveryCount < 1 {
		t.Fatal("expected duplicate delivery reporting")
	}
}

func TestValidationDefectsFailCompare(t *testing.T) {
	requireIntegration(t)

	bundle, err := oracle.Generate(oracle.ScenarioMap, oracle.GenerationConfig{Seed: 2, EventCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	base := deliveriesFromExpected(bundle.Expected)

	corrupt := append([]validation.PhysicalDelivery(nil), base...)
	corrupt[0].Payload = []byte(`{"corrupted":true}`)
	if validation.CompareExpected(bundle.Expected, corrupt, validation.CompareOptions{}).Passed {
		t.Fatal("corrupted payload should fail")
	}
	missing := base[1:]
	if validation.CompareExpected(bundle.Expected, missing, validation.CompareOptions{}).Passed {
		t.Fatal("missing logical output should fail")
	}
	extra := append(append([]validation.PhysicalDelivery(nil), base...), validation.PhysicalDelivery{LogicalKey: "unexpected", ReceiveCount: 1})
	if validation.CompareExpected(bundle.Expected, extra, validation.CompareOptions{}).Passed {
		t.Fatal("unexpected logical output should fail")
	}
}

// §16.3 (11): end-to-end map validation records duplicates but exits success when output is correct.
func TestValidationMapRunDuplicatesAllowed(t *testing.T) {
	e := shared(t)
	requireValidationIntegration(t)
	image := requireBenchmarkImage(t)
	_ = e.loadImage(image)

	code, out := e.runCLI(
		"validation", "run",
		"--image", image,
		"--scenario", "map",
		"--events", "50",
		"--tier", "smoke",
		"--timeout", "45m",
	)
	if code != cli.ExitSuccess {
		t.Fatalf("validation map exit=%d:\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "passed") && !strings.Contains(out, "PhasePassed") {
		t.Logf("validation output:\n%s", out)
	}
}

func deliveriesFromExpected(exp []oracle.ExpectedLogicalOutput) []validation.PhysicalDelivery {
	out := make([]validation.PhysicalDelivery, len(exp))
	for i, e := range exp {
		out[i] = validation.PhysicalDelivery{
			LogicalKey: e.LogicalKey, EventID: e.EventID,
			ProcessedBy: e.ProcessedBy, ChildIndex: e.ChildIndex, TotalChildren: e.TotalChildren,
			Payload: e.Payload, ReceiveCount: 1,
		}
	}
	return out
}
