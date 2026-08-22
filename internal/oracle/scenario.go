package oracle

// Scenario identifies a validation correctness scenario.
type Scenario string

const (
	ScenarioMap           Scenario = "map"
	ScenarioReduce        Scenario = "reduce"
	ScenarioSlidingReduce Scenario = "sliding-reduce"
	ScenarioMonoVertex    Scenario = "monovertex"
)

func (s Scenario) Valid() bool {
	switch s {
	case ScenarioMap, ScenarioReduce, ScenarioSlidingReduce, ScenarioMonoVertex:
		return true
	default:
		return false
	}
}
