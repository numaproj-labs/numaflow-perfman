package namespace

import (
	"strings"
	"testing"
)

func TestRenderBenchmarkNamespaceUsesAnnotationForImageRef(t *testing.T) {
	yaml := string(renderNamespace("perf-single-map-v1-8-0", map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelScenario:  "single-map",
		LabelImageTag:  "v1-8-0",
	}, map[string]string{
		AnnotationImageRef: "quay.io/numaproj/numaflow:v1.8.0",
	}))
	labelsSection := yaml
	if idx := strings.Index(yaml, "  annotations:"); idx >= 0 {
		labelsSection = yaml[:idx]
	}
	if strings.Contains(labelsSection, "quay.io/numaproj/numaflow:v1.8.0") {
		t.Fatalf("image ref must not be a label value:\n%s", yaml)
	}
	if !strings.Contains(yaml, `perfman.numaproj.io/numaflow-image-ref: "quay.io/numaproj/numaflow:v1.8.0"`) {
		t.Fatalf("expected image ref annotation:\n%s", yaml)
	}
}
