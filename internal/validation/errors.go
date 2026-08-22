package validation

import "errors"

// ErrInfrastructure marks failures that should retry (DB/K8s unreachable).
var ErrInfrastructure = errors.New("validation infrastructure error")

// ErrCorrectness marks output mismatch failures (non-retryable for the same inputs).
var ErrCorrectness = errors.New("validation correctness failure")

// IsInfrastructure reports whether err (or its chain) is an infrastructure failure.
func IsInfrastructure(err error) bool {
	return errors.Is(err, ErrInfrastructure)
}
