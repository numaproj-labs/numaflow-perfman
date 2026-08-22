package benchmark

import "errors"

// ErrInfrastructure marks Kubernetes or other runtime infrastructure failures (exit class 4).
var ErrInfrastructure = errors.New("benchmark infrastructure error")

// ErrMetricsIncomplete marks missing or insufficient required Prometheus series (exit class 5).
var ErrMetricsIncomplete = errors.New("benchmark metrics incomplete")

// ErrConfiguration marks invalid options or scenario configuration (exit class 2).
var ErrConfiguration = errors.New("benchmark configuration error")

// ErrLockHeld marks an active global test lock (exit class 10).
var ErrLockHeld = errors.New("another benchmark is active")

// ErrOverwriteDeclined marks a benchmark replacement declined by the caller.
var ErrOverwriteDeclined = errors.New("benchmark result overwrite declined")

// IsInfrastructure reports whether err is an infrastructure failure.
func IsInfrastructure(err error) bool {
	return errors.Is(err, ErrInfrastructure)
}

// IsMetricsIncomplete reports whether err is a metrics collection failure.
func IsMetricsIncomplete(err error) bool {
	return errors.Is(err, ErrMetricsIncomplete)
}

// WrapInfrastructure annotates err as an infrastructure failure.
func WrapInfrastructure(err error) error {
	if err == nil {
		return nil
	}
	return fmtWrap(ErrInfrastructure, err)
}

// WrapMetricsIncomplete annotates err as a metrics failure.
func WrapMetricsIncomplete(err error) error {
	if err == nil {
		return nil
	}
	return fmtWrap(ErrMetricsIncomplete, err)
}

type wrapError struct {
	class error
	cause error
}

func (e *wrapError) Error() string { return e.class.Error() + ": " + e.cause.Error() }
func (e *wrapError) Unwrap() error { return e.cause }
func (e *wrapError) Is(target error) bool {
	return errors.Is(e.class, target)
}

func fmtWrap(class, cause error) error {
	return &wrapError{class: class, cause: cause}
}
