package oracle

// DefaultBaseEventTimeMs is 2016-01-01T00:00:00.000Z — fixed past epoch for window tests.
const DefaultBaseEventTimeMs int64 = 1451606400000

// DefaultEventSpacingMs spaces consecutive source events (reduce scenarios).
const DefaultEventSpacingMs int64 = 6

// FixedWindowSizeMs matches fixed-window reduce pipeline configuration.
const FixedWindowSizeMs int64 = 60000

// SlidingWindowLengthMs and SlidingWindowSlideMs match sliding-reduce pipeline configuration.
const (
	SlidingWindowLengthMs int64 = 60000
	SlidingWindowSlideMs  int64 = 10000
)

// GenerationConfig drives deterministic source and expected-output generation.
type GenerationConfig struct {
	Seed            uint64
	EventCount      int64
	BaseEventTimeMs int64
	SpacingMs       int64
}

func (c GenerationConfig) withDefaults() GenerationConfig {
	out := c
	if out.EventCount < 0 {
		out.EventCount = 0
	}
	if out.BaseEventTimeMs == 0 {
		out.BaseEventTimeMs = DefaultBaseEventTimeMs
	}
	if out.SpacingMs == 0 {
		out.SpacingMs = DefaultEventSpacingMs
	}
	return out
}

// SlidingWindowConfig parameterizes sliding-window assignment (for tests and scenarios).
type SlidingWindowConfig struct {
	LengthMs int64
	SlideMs  int64
}

func (c SlidingWindowConfig) withDefaults() SlidingWindowConfig {
	out := c
	if out.LengthMs == 0 {
		out.LengthMs = SlidingWindowLengthMs
	}
	if out.SlideMs == 0 {
		out.SlideMs = SlidingWindowSlideMs
	}
	return out
}
