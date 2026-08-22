package oracle_test

import (
	"testing"

	"numa-perfman/internal/oracle"
)

func TestSlidingWindowBoundaries(t *testing.T) {
	t.Parallel()
	cfg := oracle.SlidingWindowConfig{LengthMs: 60_000, SlideMs: 10_000}
	base := oracle.DefaultBaseEventTimeMs

	// Exactly on slide boundary (aligned to 10s grid from base).
	onSlide := base + 20_000
	starts := oracle.AssignSlidingWindows(onSlide, cfg)
	if len(starts) == 0 {
		t.Fatal("expected windows on slide boundary")
	}
	if starts[0] != onSlide {
		t.Fatalf("first window start on boundary = %d want %d", starts[0], onSlide)
	}

	before := onSlide - 1
	after := onSlide + 1
	beforeStarts := oracle.AssignSlidingWindows(before, cfg)
	afterStarts := oracle.AssignSlidingWindows(after, cfg)
	if containsWindow(beforeStarts, onSlide) {
		t.Fatalf("event 1ms before boundary must not belong to window starting at %d", onSlide)
	}
	if !containsWindow(starts, onSlide) || !containsWindow(afterStarts, onSlide) {
		t.Fatalf("events on and 1ms after boundary must belong to window starting at %d", onSlide)
	}
	previousOldest := onSlide - cfg.LengthMs
	if !containsWindow(beforeStarts, previousOldest) {
		t.Fatalf("event 1ms before boundary must still belong to window starting at %d", previousOldest)
	}
	if containsWindow(starts, previousOldest) || containsWindow(afterStarts, previousOldest) {
		t.Fatalf("events on and after boundary must not belong to window ending at %d", onSlide)
	}

	// Window end: event at start+length-1 is inside; at start+length is outside top window anchored at start.
	ws := (base / cfg.SlideMs) * cfg.SlideMs
	atEnd := ws + cfg.LengthMs - 1
	if len(oracle.AssignSlidingWindows(atEnd, cfg)) == 0 {
		t.Fatalf("event at window end-1 should belong to windows")
	}
	pastEnd := ws + cfg.LengthMs
	startsAtPastEnd := oracle.AssignSlidingWindows(pastEnd, cfg)
	for _, s := range startsAtPastEnd {
		if s == ws {
			t.Fatalf("event at %d should not belong to prior window [%d,%d)", pastEnd, ws, ws+cfg.LengthMs)
		}
	}

	// Pre-base / negative timestamps: algorithm still defined (may yield fewer overlaps).
	preBase := base - 5_000
	if n := len(oracle.AssignSlidingWindows(preBase, cfg)); n == 0 {
		t.Fatalf("pre-base event should belong to at least one window, got 0")
	}
	negative := int64(-1)
	if len(oracle.AssignSlidingWindows(negative, cfg)) != 0 {
		t.Fatalf("negative timestamp should not belong to windows")
	}

	// Divisible length/slide => 6 overlapping windows for interior events.
	interior := base + 30_000
	if got := oracle.OverlappingWindowCount(interior, cfg); got != 6 {
		t.Fatalf("interior overlap count = %d want 6", got)
	}

	nonDivCfg := oracle.SlidingWindowConfig{LengthMs: 55_000, SlideMs: 10_000}
	if got := oracle.OverlappingWindowCount(interior, nonDivCfg); got < 5 || got > 6 {
		t.Fatalf("non-divisible length overlap = %d", got)
	}
}

func containsWindow(starts []int64, want int64) bool {
	for _, start := range starts {
		if start == want {
			return true
		}
	}
	return false
}

func TestSlidingReduceDiffersFromFixed(t *testing.T) {
	t.Parallel()
	cfg := oracle.GenerationConfig{Seed: 99, EventCount: 50, BaseEventTimeMs: oracle.DefaultBaseEventTimeMs}
	fixed, _ := oracle.Generate(oracle.ScenarioReduce, cfg)
	slide, _ := oracle.Generate(oracle.ScenarioSlidingReduce, cfg)
	if len(fixed.Expected) >= len(slide.Expected) {
		t.Fatalf("sliding should produce more logical outputs: fixed=%d slide=%d", len(fixed.Expected), len(slide.Expected))
	}
}
