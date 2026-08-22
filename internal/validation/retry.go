package validation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetryFunc performs one attempt; returning ErrInfrastructure triggers retry.
type RetryFunc func(ctx context.Context) error

// WithBoundedRetry runs fn up to attempts times with delay between infrastructure failures.
func WithBoundedRetry(ctx context.Context, attempts int, delay time.Duration, fn RetryFunc) error {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		last = err
		if !IsInfrastructure(err) {
			return err
		}
		if i+1 < attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if last == nil {
		return nil
	}
	return fmt.Errorf("%w: after %d attempts: %v", ErrInfrastructure, attempts, last)
}

// WrapInfrastructure annotates an error as retryable infrastructure failure.
func WrapInfrastructure(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrInfrastructure, err)
}
