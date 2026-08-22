package benchmark

import (
	"context"
	"fmt"
	"strings"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/diagnostics"
	"numa-perfman/internal/namespace"
	prom "numa-perfman/internal/prometheus"
	"numa-perfman/internal/scenario"
)

// ClusterAdapter wires namespace, controller, and scenario packages to Cluster.
type ClusterAdapter struct {
	Cluster       cluster.Client
	Namespaces    namespace.Manager
	KindCluster   string
	DeployTimeout time.Duration

	runID         string
	scenarioID    string
	numaflowImage string
}

// PreflightAdapter verifies UDF images on kind nodes and resolves digests when present.
type PreflightAdapter struct {
	Cluster     cluster.Client
	KindCluster string
}

// MetricsAdapter wraps the Prometheus client.
type MetricsAdapter struct {
	Client prom.Client
}

// DiagnosticsAdapter wraps diagnostics collection.
type DiagnosticsAdapter struct {
	Collector diagnostics.Collector
}

func (p PreflightAdapter) VerifyUDFImage(ctx context.Context, imageRef string) error {
	if imageRef == "" {
		return fmt.Errorf("udf image is required")
	}
	if p.KindCluster == "" {
		return nil
	}
	nodes, err := p.Cluster.KindNodeList(ctx, p.KindCluster)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no kind nodes in cluster %q", p.KindCluster)
	}
	for _, node := range nodes {
		ok, err := p.Cluster.ImagePresentOnKindNode(ctx, node, imageRef)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("udf image %q not present on node %s", imageRef, node)
		}
	}
	return nil
}

func (p PreflightAdapter) ResolveImageDigest(ctx context.Context, imageRef string) (string, error) {
	if imageRef == "" {
		return "", nil
	}
	if at := strings.LastIndex(imageRef, "@sha256:"); at >= 0 {
		return imageRef[at+1:], nil
	}
	if p.KindCluster == "" {
		return "", nil
	}
	nodes, err := p.Cluster.KindNodeList(ctx, p.KindCluster)
	if err != nil {
		return "", err
	}
	for _, node := range nodes {
		ok, err := p.Cluster.ImagePresentOnKindNode(ctx, node, imageRef)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		digest, err := p.Cluster.ImageDigestOnKindNode(ctx, node, imageRef)
		if err != nil {
			return "", err
		}
		if digest != "" {
			return digest, nil
		}
	}
	return "", nil
}

func (a ClusterAdapter) nsMgr() namespace.Manager {
	mgr := a.Namespaces
	if mgr.Cluster.Runner == nil && mgr.Cluster.Context == "" {
		mgr.Cluster = a.Cluster
	}
	return mgr
}

func (a ClusterAdapter) CreateRunNamespace(ctx context.Context, name, runID, scenarioID, imageTag, imageRef string) error {
	mgr := a.nsMgr()
	exists, err := a.Cluster.NamespaceExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		replicas, repErr := a.Cluster.DeploymentReplicas(ctx, name, controller.DeploymentName)
		if repErr == nil && replicas > 0 {
			return fmt.Errorf("namespace %q has active controller (replicas=%d)", name, replicas)
		}
		wait := a.DeployTimeout
		if wait == 0 {
			wait = 2 * time.Minute
		}
		if err := mgr.Delete(ctx, name, wait); err != nil {
			return fmt.Errorf("delete existing namespace %q: %w", name, err)
		}
	}
	return mgr.CreateBenchmark(ctx, name, runID, scenarioID, imageTag, imageRef)
}

func (a ClusterAdapter) DeleteRunNamespace(ctx context.Context, name string, wait time.Duration) error {
	return a.nsMgr().Delete(ctx, name, wait)
}

func (a ClusterAdapter) ScaleControllerToZero(ctx context.Context, name string) error {
	return a.Cluster.ScaleDeployment(ctx, name, controller.DeploymentName, 0)
}

func (a ClusterAdapter) RetainFailureNamespace(ctx context.Context, name string) error {
	return a.ScaleControllerToZero(ctx, name)
}

func (a *ClusterAdapter) BindRunContext(runID, scenarioID, numaflowImage string) {
	a.runID = runID
	a.scenarioID = scenarioID
	a.numaflowImage = numaflowImage
}

func (a *ClusterAdapter) DeployController(ctx context.Context, namespaceName, imageRef string) error {
	if err := controller.ValidateImageReference(imageRef); err != nil {
		return err
	}
	manifests, err := controller.Render(controller.RenderInput{
		Namespace:      namespaceName,
		ImageReference: imageRef,
		RunID:          a.runID,
		ScenarioID:     a.scenarioID,
	})
	if err != nil {
		return err
	}
	if !strings.Contains(manifests.Deployment, imageRef) {
		return fmt.Errorf("controller manifest image mismatch: expected %q", imageRef)
	}
	client := a.Cluster
	for _, doc := range manifests.ApplyDocuments() {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(doc)); err != nil {
			return err
		}
	}
	return nil
}

func (a ClusterAdapter) WaitControllerReady(ctx context.Context, namespaceName string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = a.DeployTimeout
	}
	return a.Cluster.WaitForCondition(ctx, namespaceName,
		"deployment/"+controller.DeploymentName,
		"condition=Available",
		timeout,
	)
}

func (a *ClusterAdapter) DeployScenario(ctx context.Context, namespaceName string, sc scenario.Scenario, udfImage string) (scenario.ManifestBundle, error) {
	bundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID:    sc.ID,
		Kind:          scenario.RunKindBenchmark,
		Namespace:     namespaceName,
		UDFImage:      udfImage,
		RunID:         a.runID,
		NumaflowImage: a.numaflowImage,
	})
	if err != nil {
		return scenario.ManifestBundle{}, err
	}
	client := a.Cluster
	if bundle.ISB != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.ISB)); err != nil {
			return scenario.ManifestBundle{}, err
		}
	}
	if bundle.Pipeline != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.Pipeline)); err != nil {
			return scenario.ManifestBundle{}, err
		}
	}
	if bundle.MonoVertex != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.MonoVertex)); err != nil {
			return scenario.ManifestBundle{}, err
		}
	}
	return bundle, nil
}

func (a ClusterAdapter) WaitScenarioReady(ctx context.Context, namespaceName string, sc scenario.Scenario, timeout time.Duration) error {
	if timeout == 0 {
		timeout = a.DeployTimeout
	}
	if sc.NeedsISB {
		// Numaflow sets phase=Running only after the pipeline's
		// VerticesHealthy condition succeeds. A separate pod-selector wait can
		// race with asynchronous vertex creation and remain blocked.
		return a.Cluster.WaitForCondition(ctx, namespaceName, "pipeline/"+sc.ID, "jsonpath={.status.phase}=Running", timeout)
	}
	return a.Cluster.WaitForCondition(ctx, namespaceName, "monovertex/"+sc.ID, "jsonpath={.status.phase}=Running", timeout)
}

// DeleteScenario removes Numaflow resources while the namespace-scoped controller
// remains available to complete their finalizers.
func (a ClusterAdapter) DeleteScenario(ctx context.Context, namespaceName string, sc scenario.Scenario, timeout time.Duration) error {
	if timeout == 0 {
		timeout = a.DeployTimeout
	}
	if sc.NeedsISB {
		pipeline := "pipeline/" + sc.ID
		if err := a.Cluster.DeleteResource(ctx, namespaceName, pipeline, true); err != nil {
			return err
		}
		if err := a.waitForResourceDeletion(ctx, namespaceName, pipeline, timeout); err != nil {
			return err
		}
		isb := "interstepbufferservice/" + sc.ID
		if err := a.Cluster.DeleteResource(ctx, namespaceName, isb, true); err != nil {
			return err
		}
		return a.waitForResourceDeletion(ctx, namespaceName, isb, timeout)
	}

	monoVertex := "monovertex/" + sc.ID
	if err := a.Cluster.DeleteResource(ctx, namespaceName, monoVertex, true); err != nil {
		return err
	}
	return a.waitForResourceDeletion(ctx, namespaceName, monoVertex, timeout)
}

func (a ClusterAdapter) waitForResourceDeletion(ctx context.Context, namespaceName, resource string, timeout time.Duration) error {
	err := a.Cluster.WaitForCondition(ctx, namespaceName, resource, "delete", timeout)
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not found") || strings.Contains(message, "no matching resources") {
		return nil
	}
	return err
}

func (a ClusterAdapter) CheckMeasurementHealth(ctx context.Context, namespaceName string) error {
	_, err := a.Cluster.ListPods(ctx, namespaceName)
	return err
}

func (m MetricsAdapter) PreflightRequired(ctx context.Context, defs []scenario.MetricDefinition, namespaceName string) error {
	var required []scenario.MetricDefinition
	for _, d := range defs {
		if d.Required {
			required = append(required, d)
		}
	}
	if len(required) == 0 {
		return nil
	}
	end := time.Now()
	start := end.Add(-30 * time.Second)
	_, err := m.Client.CollectScenarioMetrics(ctx, required, namespaceName, start, end, 10*time.Second)
	return err
}

func (m MetricsAdapter) CollectRange(ctx context.Context, defs []scenario.MetricDefinition, namespaceName string, start, end time.Time, step time.Duration) ([]prom.SeriesResult, error) {
	return m.Client.CollectScenarioMetrics(ctx, defs, namespaceName, start, end, step)
}

func (d DiagnosticsAdapter) Collect(ctx context.Context, ns string) (diagnostics.Bundle, error) {
	return d.Collector.Collect(ctx, ns)
}
