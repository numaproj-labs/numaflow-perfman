package validation

// Phase is a validation run lifecycle phase.
type Phase string

const (
	PhaseCreated          Phase = "created"
	PhaseGenerating       Phase = "generating"
	PhaseNamespaceCreated Phase = "namespace_created"
	PhaseControllerReady  Phase = "controller_ready"
	PhasePipelineReady    Phase = "pipeline_ready"
	PhaseSending          Phase = "sending"
	PhaseDraining         Phase = "draining"
	PhaseValidating       Phase = "validating"
	PhasePassed           Phase = "passed"
	PhaseFailed           Phase = "failed"
)

var terminalPhases = map[Phase]bool{
	PhasePassed: true,
	PhaseFailed: true,
}

// ValidTransition reports whether moving from -> to is allowed.
func ValidTransition(from, to Phase) bool {
	if terminalPhases[from] {
		return false
	}
	switch from {
	case PhaseCreated:
		return to == PhaseGenerating || to == PhaseFailed
	case PhaseGenerating:
		return to == PhaseNamespaceCreated || to == PhaseFailed
	case PhaseNamespaceCreated:
		return to == PhaseControllerReady || to == PhaseFailed
	case PhaseControllerReady:
		return to == PhasePipelineReady || to == PhaseFailed
	case PhasePipelineReady:
		return to == PhaseSending || to == PhaseFailed
	case PhaseSending:
		return to == PhaseDraining || to == PhaseFailed
	case PhaseDraining:
		return to == PhaseValidating || to == PhaseFailed
	case PhaseValidating:
		return to == PhasePassed || to == PhaseFailed
	default:
		return false
	}
}

func (p Phase) Terminal() bool {
	return terminalPhases[p]
}
