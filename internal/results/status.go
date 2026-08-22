package results

// Run kind values persisted in runs.kind.
const (
	KindBenchmark  = "benchmark"
	KindValidation = "validation"
)

// Benchmark and shared lifecycle status values.
const (
	StatusCreated          = "created"
	StatusPreflight        = "preflight"
	StatusNamespaceCreated = "namespace_created"
	StatusControllerReady  = "controller_ready"
	StatusPipelineReady    = "pipeline_ready"
	StatusWarmingUp        = "warming_up"
	StatusMeasuring        = "measuring"
	StatusCollecting       = "collecting"
	StatusCompleted        = "completed"
	StatusFailed           = "failed"
	StatusInterrupted      = "interrupted"
)

// Validation-specific status values.
const (
	StatusGenerating = "generating"
	StatusSending    = "sending"
	StatusDraining   = "draining"
	StatusValidating = "validating"
	StatusPassed     = "passed"
)
