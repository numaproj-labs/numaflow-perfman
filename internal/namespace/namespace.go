package namespace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/cluster"
)

const (
	LabelManagedBy = "perfman.numaproj.io/managed-by"
	LabelRunID     = "perfman.numaproj.io/run-id"
	LabelRunKind   = "perfman.numaproj.io/run-kind"
	LabelScenario  = "perfman.numaproj.io/scenario"
	LabelImageTag  = "perfman.numaproj.io/image-tag"
	AnnotationImageRef = "perfman.numaproj.io/numaflow-image-ref"
	ManagedByValue = "numaflow-perfman"
)

// Kind distinguishes benchmark vs validation namespaces.
type Kind string

const (
	KindBenchmark  Kind = "benchmark"
	KindValidation Kind = "validation"
)

// Manager handles run namespace lifecycle.
type Manager struct {
	Cluster cluster.Client
}

// Name generates a deterministic safe namespace name from run ID prefix and UUID segment.
// Prefer BenchmarkKey / ValidationKey for new runs; this remains for legacy UUID namespaces.
func Name(kind Kind, runID string) (string, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return "", fmt.Errorf("run id must be a UUID: %w", err)
	}
	short := strings.ReplaceAll(id.String(), "-", "")[:8]
	switch kind {
	case KindBenchmark:
		return PrefixBenchmarkLegacy + short, nil
	case KindValidation:
		return PrefixValidationLegacy + short, nil
	default:
		return "", fmt.Errorf("unknown run kind %q", kind)
	}
}

func (m Manager) client() cluster.Client {
	return m.Cluster
}

// Create applies a Namespace manifest with perfman labels.
func (m Manager) Create(ctx context.Context, name, runID string, kind Kind) error {
	labels := map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelRunID:     runID,
		LabelRunKind:   string(kind),
	}
	yaml := renderNamespace(name, labels, nil)
	_, err := m.client().ApplyYAML(ctx, "", yaml)
	return err
}

// CreateBenchmark applies a benchmark namespace manifest with scenario and image metadata.
func (m Manager) CreateBenchmark(ctx context.Context, name, runID, scenarioID, imageTag, imageRef string) error {
	return m.createLabeled(ctx, name, runID, KindBenchmark, scenarioID, imageTag, imageRef)
}

// CreateValidation applies a validation namespace manifest with scenario and image metadata.
func (m Manager) CreateValidation(ctx context.Context, name, runID, scenarioID, imageTag, imageRef string) error {
	return m.createLabeled(ctx, name, runID, KindValidation, scenarioID, imageTag, imageRef)
}

func (m Manager) createLabeled(ctx context.Context, name, runID string, kind Kind, scenarioID, imageTag, imageRef string) error {
	labels := map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelRunID:     runID,
		LabelRunKind:   string(kind),
		LabelScenario:  scenarioID,
		LabelImageTag:  imageTag,
	}
	annotations := map[string]string{
		AnnotationImageRef: imageRef,
	}
	yaml := renderNamespace(name, labels, annotations)
	_, err := m.client().ApplyYAML(ctx, "", yaml)
	return err
}

// Delete removes the namespace and optionally waits for removal.
func (m Manager) Delete(ctx context.Context, name string, wait time.Duration) error {
	yaml := []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + name + "\n")
	_, err := m.client().DeleteYAML(ctx, "", yaml, true)
	if err != nil {
		return err
	}
	if wait <= 0 {
		return nil
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		exists, err := m.client().NamespaceExists(ctx, name)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("namespace %q still exists after %s", name, wait)
}

// WaitReady waits until namespace phase is Active.
func (m Manager) WaitReady(ctx context.Context, name string, timeout time.Duration) error {
	return m.client().WaitForCondition(ctx, "", "namespace/"+name, "jsonpath={.status.phase}=Active", timeout)
}

// ListActiveRunNamespaces returns managed namespaces that still exist.
func (m Manager) ListActiveRunNamespaces(ctx context.Context) ([]string, error) {
	return m.client().ListNamespacesByLabel(ctx, LabelManagedBy+"="+ManagedByValue)
}

// IsActiveRunNamespace reports whether name matches perf harness run prefixes.
func IsActiveRunNamespace(name string) bool {
	return strings.HasPrefix(name, PrefixBenchmark) ||
		strings.HasPrefix(name, PrefixBenchmarkLegacy) ||
		strings.HasPrefix(name, PrefixValidation) ||
		strings.HasPrefix(name, PrefixValidationLegacy)
}

// RetainFailure scales the namespace-scoped controller deployment to zero for retained failures.
func (m Manager) RetainFailure(ctx context.Context, ns string) error {
	return m.client().ScaleDeployment(ctx, ns, "numaflow-controller", 0)
}

func renderNamespace(name string, labels, annotations map[string]string) []byte {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ")
	b.WriteString(name)
	b.WriteString("\n")
	if len(labels) > 0 {
		b.WriteString("  labels:\n")
		for k, v := range labels {
			b.WriteString("    ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	if len(annotations) > 0 {
		b.WriteString("  annotations:\n")
		for k, v := range annotations {
			b.WriteString("    ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(yamlQuote(v))
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func yamlQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	return string(b)
}
