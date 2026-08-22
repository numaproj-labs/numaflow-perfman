package cli

import (
	"errors"
	"testing"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/validation"
)

func TestExitCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want int
	}{
		{nil, ExitSuccess},
		{errUsage, ExitInvalidUsage},
		{errInterruptedSIGINT, ExitInterruptedSIGINT},
		{errInterruptedSIGTERM, ExitTerminatedSIGTERM},
		{validation.ErrCorrectness, ExitTestFailure},
		{benchmark.ErrLockHeld, ExitLockHeld},
		{benchmark.ErrMetricsIncomplete, ExitMetricsIncomplete},
		{benchmark.ErrInfrastructure, ExitInfrastructure},
		{errPreflight, ExitPreflightSetup},
		{errReport, ExitReportFailure},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Fatalf("%v -> %d want %d", c.err, got, c.want)
		}
	}
	if ExitCode(errors.Join(benchmark.ErrMetricsIncomplete, errors.New("x"))) != ExitMetricsIncomplete {
		t.Fatal("joined metrics error")
	}
}

// ErrUsageForTest exposes errUsage to external tests.
func ErrUsageForTest() error { return errUsage }
