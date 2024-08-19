package collect

import (
	"time"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

type Options struct {
	step time.Duration
}

// Step returns the step size
func (o *Options) Step() time.Duration {
	return o.step
}

// DefaultOptions returns the config options.
func DefaultOptions() *Options {
	return &Options{
		step: util.Step,
	}
}

type Option func(*Options)

// WithStep sets the step size. This is mainly useful for testing queries with differing step sizes, and to
// provide the option for specific metrics to use a different step size than the config
func WithStep(step time.Duration) Option {
	return func(opts *Options) {
		opts.step = step
	}
}
