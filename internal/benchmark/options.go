package benchmark

import (
	"encoding/json"
	"fmt"
	"time"

	"numa-perfman/internal/controller"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

// Options configures one benchmark run.
type Options struct {
	Scenario string
	ImageRef string
	UDFImage string

	Duration time.Duration

	PrometheusStep      time.Duration
	HealthPollInterval  time.Duration
	DeployTimeout       time.Duration
	NamespaceDeleteWait time.Duration

	Command string
}

func (o Options) withDefaults() Options {
	out := o
	if out.Duration == 0 {
		out.Duration = 15 * time.Minute
	}
	if out.PrometheusStep == 0 {
		out.PrometheusStep = 10 * time.Second
	}
	if out.HealthPollInterval == 0 {
		out.HealthPollInterval = 15 * time.Second
	}
	if out.DeployTimeout == 0 {
		out.DeployTimeout = 10 * time.Minute
	}
	if out.NamespaceDeleteWait == 0 {
		out.NamespaceDeleteWait = 5 * time.Minute
	}
	return out
}

// Validate checks required fields and image reference shape.
func (o Options) Validate() error {
	if o.Scenario == "" {
		return fmt.Errorf("%w: scenario is required", ErrConfiguration)
	}
	if o.ImageRef == "" {
		return fmt.Errorf("%w: image reference is required", ErrConfiguration)
	}
	if err := controller.ValidateImageReference(o.ImageRef); err != nil {
		return fmt.Errorf("%w: %v", ErrConfiguration, err)
	}
	if o.UDFImage == "" {
		return fmt.Errorf("%w: udf image is required", ErrConfiguration)
	}
	if _, err := scenario.LookupBenchmark(o.Scenario); err != nil {
		return fmt.Errorf("%w: %v", ErrConfiguration, err)
	}
	return nil
}

// ConfigJSON serializes benchmark options for persistence.
func (o Options) ConfigJSON() string {
	cfg := map[string]any{
		"duration":  o.Duration.String(),
		"udf_image": o.UDFImage,
		"resources": runmeta.DefaultBenchmarkResources,
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// RequiredMetricNames lists metric names that must be present for a successful run.
func RequiredMetricNames(defs []scenario.MetricDefinition) []string {
	var names []string
	for _, d := range defs {
		if d.Required {
			names = append(names, d.Name)
		}
	}
	return names
}
