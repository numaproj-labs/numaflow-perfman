package collect

import (
	"time"

	"github.com/numaproj-labs/numaflow-perfman/util"
)

type Options struct {
	step time.Duration
}

func (o *Options) Step() time.Duration {
	return o.step
}

// DefaultOptions returns the default options.
func DefaultOptions() *Options {
	return &Options{
		step: util.Step,
	}
}

type Option func(*Options)

// WithStep sets the increment between each point in the range
func WithStep(step time.Duration) Option {
	return func(opts *Options) {
		opts.step = step
	}
}
