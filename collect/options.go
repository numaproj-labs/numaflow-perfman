package collect

import "time"

const DefaultStep = 15 * time.Second

type Options struct {
	step time.Duration
}

func (o *Options) Step() time.Duration {
	return o.step
}

// DefaultOptions returns the default options.
func DefaultOptions() *Options {
	return &Options{
		step: DefaultStep,
	}
}

type Option func(*Options)

// WithStep sets the increment between each point in the range. It defines how often a new value is produced
func WithStep(step time.Duration) Option {
	return func(opts *Options) {
		opts.step = step
	}
}
