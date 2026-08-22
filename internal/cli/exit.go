package cli

import (
	"context"
	"errors"
	"os"
	"syscall"

	"numa-perfman/internal/benchmark"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/validation"
)

const (
	ExitSuccess           = 0
	ExitTestFailure       = 1
	ExitInvalidUsage      = 2
	ExitPreflightSetup    = 3
	ExitInfrastructure    = 4
	ExitMetricsIncomplete = 5
	ExitReportFailure     = 6
	ExitLockHeld          = 10
	ExitInterruptedSIGINT = 130
	ExitTerminatedSIGTERM = 143
)

// ExitCode maps an error to the documented perfman exit status.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if errors.Is(err, errUsage) {
		return ExitInvalidUsage
	}
	if errors.Is(err, errInterruptedSIGINT) {
		return ExitInterruptedSIGINT
	}
	if errors.Is(err, errInterruptedSIGTERM) {
		return ExitTerminatedSIGTERM
	}
	if errors.Is(err, validation.ErrCorrectness) {
		return ExitTestFailure
	}
	if errors.Is(err, benchmark.ErrOverwriteDeclined) {
		return ExitSuccess
	}
	if errors.Is(err, benchmark.ErrLockHeld) {
		return ExitLockHeld
	}
	if errors.Is(err, benchmark.ErrMetricsIncomplete) {
		return ExitMetricsIncomplete
	}
	if errors.Is(err, benchmark.ErrInfrastructure) || validation.IsInfrastructure(err) {
		return ExitInfrastructure
	}
	if errors.Is(err, benchmark.ErrConfiguration) {
		return ExitInvalidUsage
	}
	if errors.Is(err, controller.ErrInvalidImageRef) {
		return ExitInvalidUsage
	}
	if errors.Is(err, errPreflight) {
		return ExitPreflightSetup
	}
	if errors.Is(err, errReport) {
		return ExitReportFailure
	}
	if errors.Is(err, errBenchmarkFailed) {
		return ExitTestFailure
	}
	if errors.Is(err, context.Canceled) {
		return ExitInterruptedSIGINT
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ExitInfrastructure
	}
	return ExitInfrastructure
}

var (
	errUsage              = errors.New("invalid command or configuration")
	errInterruptedSIGINT  = errors.New("interrupted by SIGINT")
	errInterruptedSIGTERM = errors.New("terminated by SIGTERM")
	errReport             = errors.New("report generation failed")
	errBenchmarkFailed    = errors.New("benchmark run did not complete successfully")
	errPreflight          = errors.New("preflight or setup failure")
)

func signalExitCode(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return ExitTerminatedSIGTERM
	}
	return ExitInterruptedSIGINT
}
