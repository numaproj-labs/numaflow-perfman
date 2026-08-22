package benchmark

import "numa-perfman/internal/results"

// State is a benchmark lifecycle status persisted in runs.status.
type State = string

const (
	StateCreated          State = results.StatusCreated
	StatePreflight        State = results.StatusPreflight
	StateNamespaceCreated State = results.StatusNamespaceCreated
	StateControllerReady  State = results.StatusControllerReady
	StatePipelineReady    State = results.StatusPipelineReady
	StateMeasuring        State = results.StatusMeasuring
	StateCollecting       State = results.StatusCollecting
	StateCompleted        State = results.StatusCompleted
	StateFailed           State = results.StatusFailed
	StateInterrupted      State = results.StatusInterrupted
)

var terminalStates = map[State]bool{
	StateCompleted:   true,
	StateFailed:      true,
	StateInterrupted: true,
}

// ValidTransition reports whether moving from -> to is allowed by the benchmark state machine.
func ValidTransition(from, to State) bool {
	if terminalStates[from] {
		return false
	}
	if to == StateFailed || to == StateInterrupted {
		return true
	}
	switch from {
	case StateCreated:
		return to == StatePreflight
	case StatePreflight:
		return to == StateNamespaceCreated
	case StateNamespaceCreated:
		return to == StateControllerReady
	case StateControllerReady:
		return to == StatePipelineReady
	case StatePipelineReady:
		return to == StateMeasuring
	case StateMeasuring:
		return to == StateCollecting
	case StateCollecting:
		return to == StateCompleted
	default:
		return false
	}
}

// Terminal reports whether s is a terminal lifecycle state.
func Terminal(s State) bool {
	return terminalStates[s]
}
