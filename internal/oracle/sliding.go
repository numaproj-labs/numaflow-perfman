package oracle

// AssignSlidingWindows returns window start times for eventTime using Numaflow's sliding
// assignment (truncate to slide, walk backward while eventTime ∈ [start, start+length)).
func AssignSlidingWindows(eventTime int64, cfg SlidingWindowConfig) []int64 {
	cfg = cfg.withDefaults()
	slide := cfg.SlideMs
	length := cfg.LengthMs

	ws := (eventTime / slide) * slide
	we := ws + length
	var starts []int64
	for ws <= eventTime && eventTime < we {
		starts = append(starts, ws)
		ws -= slide
		we -= slide
	}
	return starts
}

// OverlappingWindowCount returns how many windows contain eventTime for the given config.
func OverlappingWindowCount(eventTime int64, cfg SlidingWindowConfig) int {
	return len(AssignSlidingWindows(eventTime, cfg))
}
