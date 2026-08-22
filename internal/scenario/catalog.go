package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// RunKind distinguishes benchmark and validation scenarios.
type RunKind string

const (
	RunKindBenchmark  RunKind = "benchmark"
	RunKindValidation RunKind = "validation"
)

// Unit is the canonical persisted unit for a metric.
type Unit string

const (
	UnitCores        Unit = "cores"
	UnitBytes        Unit = "bytes"
	UnitSeconds      Unit = "seconds"
	UnitMilliseconds Unit = "milliseconds"
	UnitEventsPerSec Unit = "events/second"
)

// MetricDefinition describes a Prometheus series required for a scenario.
type MetricDefinition struct {
	Name        string
	DisplayName string
	Query       string
	Unit        Unit
	Required    bool
	MinSamples  int
	// GroupBy splits one query into multiple persisted series (e.g. per pipeline vertex).
	GroupBy []string
}

// Scenario describes a benchmark or validation catalog entry.
type Scenario struct {
	ID          string
	Kind        RunKind
	Description string
	Metrics     []MetricDefinition
	NeedsISB    bool
}

var benchmarkCatalog = map[string]Scenario{
	"single-map": {
		ID: "single-map", Kind: RunKindBenchmark, Description: "Single map vertex benchmark", NeedsISB: true,
		Metrics: benchmarkPipelineMetrics("single-map"),
	},
	"two-maps": {
		ID: "two-maps", Kind: RunKindBenchmark, Description: "Two map vertices benchmark", NeedsISB: true,
		Metrics: benchmarkPipelineMetrics("two-maps"),
	},
	"simple-monovertex": {
		ID: "simple-monovertex", Kind: RunKindBenchmark, Description: "Simple MonoVertex benchmark", NeedsISB: false,
		Metrics: monovertexMetrics("simple-monovertex"),
	},
	"monovertex-generator-blackhole": {
		ID: "monovertex-generator-blackhole", Kind: RunKindBenchmark, Description: "MonoVertex generator to blackhole", NeedsISB: false,
		Metrics: monovertexMetrics("monovertex-generator-blackhole"),
	},
	"monovertex-map": {
		ID: "monovertex-map", Kind: RunKindBenchmark, Description: "MonoVertex with map UDF", NeedsISB: false,
		Metrics: monovertexMetrics("monovertex-map"),
	},
	"monovertex-mapstream": {
		ID: "monovertex-mapstream", Kind: RunKindBenchmark, Description: "MonoVertex map stream UDF", NeedsISB: false,
		Metrics: monovertexMetrics("monovertex-mapstream"),
	},
	"monovertex-batchmap": {
		ID: "monovertex-batchmap", Kind: RunKindBenchmark, Description: "MonoVertex batch map UDF", NeedsISB: false,
		Metrics: monovertexMetrics("monovertex-batchmap"),
	},
}

var validationCatalog = map[string]Scenario{
	"map": {
		ID: "map", Kind: RunKindValidation, Description: "Map correctness validation", NeedsISB: true,
		Metrics: nil,
	},
	"reduce": {
		ID: "reduce", Kind: RunKindValidation, Description: "Reduce correctness validation", NeedsISB: true,
		Metrics: nil,
	},
	"sliding-reduce": {
		ID: "sliding-reduce", Kind: RunKindValidation, Description: "Sliding reduce correctness validation", NeedsISB: true,
		Metrics: nil,
	},
	"monovertex": {
		ID: "monovertex", Kind: RunKindValidation, Description: "MonoVertex correctness validation", NeedsISB: false,
		Metrics: nil,
	},
}

func benchmarkPipelineMetrics(pipeline string) []MetricDefinition {
	perVertexPod := fmt.Sprintf(`%s-([^-]+)-.+`, pipeline)
	return []MetricDefinition{
		{
			Name: "vertex_cpu", DisplayName: "Vertex CPU", Required: true, MinSamples: 3, Unit: UnitCores,
			GroupBy: []string{"vertex"},
			Query:   fmt.Sprintf(`sum by (vertex) (label_replace(rate(container_cpu_usage_seconds_total{namespace="$namespace",pod=~"%s-.*",container="numa"}[1m]), "vertex", "$1", "pod", "%s"))`, pipeline, perVertexPod),
		},
		{
			Name: "vertex_memory", DisplayName: "Vertex memory", Required: true, MinSamples: 3, Unit: UnitBytes,
			GroupBy: []string{"vertex"},
			Query:   fmt.Sprintf(`sum by (vertex) (label_replace(container_memory_working_set_bytes{namespace="$namespace",pod=~"%s-.*",container="numa"}, "vertex", "$1", "pod", "%s"))`, pipeline, perVertexPod),
		},
		{
			Name: "forwarder_rate", DisplayName: "Forwarder rate", Required: true, MinSamples: 3, Unit: UnitEventsPerSec,
			GroupBy: []string{"vertex"},
			Query:   fmt.Sprintf(`sum by (vertex) (rate(forwarder_data_read_total{namespace="$namespace",pipeline="%s"}[1m]))`, pipeline),
		},
		{
			Name: "forwarder_latency", DisplayName: "Forwarder latency", Required: false, MinSamples: 3, Unit: UnitMilliseconds,
			GroupBy: []string{"vertex"},
			Query:   fmt.Sprintf(`histogram_quantile(0.99, sum by (le, vertex) (rate(forwarder_processing_time_bucket{namespace="$namespace",pipeline="%s"}[1m])))`, pipeline),
		},
	}
}

func monovertexMetrics(name string) []MetricDefinition {
	return []MetricDefinition{
		{
			Name: "monovertex_rate", DisplayName: "MonoVertex processing rate", Required: true, MinSamples: 3, Unit: UnitEventsPerSec,
			Query: `sum(rate(monovtx_read_total{namespace="$namespace"}[1m]))`,
		},
		{
			Name: "monovertex_cpu", DisplayName: "MonoVertex CPU", Required: true, MinSamples: 3, Unit: UnitCores,
			Query: fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="$namespace",pod=~"%s-.*",container="numa"}[1m]))`, name),
		},
	}
}

// LookupBenchmark returns a benchmark scenario by ID.
func LookupBenchmark(id string) (Scenario, error) {
	s, ok := benchmarkCatalog[id]
	if !ok {
		return Scenario{}, fmt.Errorf("unknown benchmark scenario %q", id)
	}
	return s, nil
}

// LookupValidation returns a validation scenario by ID.
func LookupValidation(id string) (Scenario, error) {
	s, ok := validationCatalog[id]
	if !ok {
		return Scenario{}, fmt.Errorf("unknown validation scenario %q", id)
	}
	return s, nil
}

// ListBenchmarks returns sorted benchmark scenario IDs.
func ListBenchmarks() []string {
	ids := make([]string, 0, len(benchmarkCatalog))
	for id := range benchmarkCatalog {
		ids = append(ids, id)
	}
	return ids
}

// ListValidations returns sorted validation scenario IDs.
func ListValidations() []string {
	ids := make([]string, 0, len(validationCatalog))
	for id := range validationCatalog {
		ids = append(ids, id)
	}
	return ids
}

// RenderInput parameters for manifest generation.
type RenderInput struct {
	ScenarioID     string
	Kind           RunKind
	Namespace      string
	UDFImage       string
	ValidationNS   string
	PostgresDBName string
	NumaflowImage  string // controller / data-plane image reference for pod labels
	RunID          string // perfman run UUID for pod labels
}

var (
	ErrForbiddenManifestContent = errors.New("manifest contains forbidden legacy content")
)

// RenderManifests produces pipeline/mono manifests for the scenario.
func RenderManifests(in RenderInput) (ManifestBundle, error) {
	if strings.TrimSpace(in.Namespace) == "" {
		return ManifestBundle{}, errors.New("namespace is required")
	}
	if strings.TrimSpace(in.UDFImage) == "" {
		return ManifestBundle{}, errors.New("udf image is required")
	}
	var bundle ManifestBundle
	var err error
	switch in.Kind {
	case RunKindBenchmark:
		bundle, err = renderBenchmark(in)
	case RunKindValidation:
		bundle, err = renderValidation(in)
	default:
		return ManifestBundle{}, fmt.Errorf("unknown kind %q", in.Kind)
	}
	if err != nil {
		return ManifestBundle{}, err
	}
	if err := ValidateManifestContent(bundle.AllDocuments()); err != nil {
		return ManifestBundle{}, err
	}
	bundle.Hash = HashManifests(bundle.AllDocuments())
	return bundle, nil
}

// ManifestBundle holds rendered YAML documents.
type ManifestBundle struct {
	ISB        string
	Pipeline   string
	MonoVertex string
	Hash       string
}

func (b ManifestBundle) AllDocuments() []string {
	var docs []string
	if b.ISB != "" {
		docs = append(docs, b.ISB)
	}
	if b.Pipeline != "" {
		docs = append(docs, b.Pipeline)
	}
	if b.MonoVertex != "" {
		docs = append(docs, b.MonoVertex)
	}
	return docs
}

// HashManifests returns a stable sha256 hex digest of concatenated manifests.
func HashManifests(docs []string) string {
	h := sha256.New()
	for _, d := range docs {
		h.Write([]byte(d))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateManifestContent enforces plan 16.2 content rules on rendered YAML.
func ValidateManifestContent(docs []string) error {
	joined := strings.ToLower(strings.Join(docs, "\n"))
	forbidden := []string{"gp3"}
	for _, f := range forbidden {
		if strings.Contains(joined, f) {
			return fmt.Errorf("%w: contains %q", ErrForbiddenManifestContent, f)
		}
	}
	// Reject legacy language-specific scenario naming in manifests.
	if strings.Contains(joined, "monovertex_java") || strings.Contains(joined, "monovertex_python") ||
		strings.Contains(joined, "pipeline-java") || strings.Contains(joined, "map_java") {
		return fmt.Errorf("%w: legacy language-specific naming", ErrForbiddenManifestContent)
	}
	for _, doc := range docs {
		if strings.Contains(doc, "udf:") || strings.Contains(doc, "udsource:") || strings.Contains(doc, "udsink:") {
			if !strings.Contains(doc, "imagePullPolicy: Never") {
				return errors.New("udf containers must use imagePullPolicy Never")
			}
		}
	}
	return nil
}
