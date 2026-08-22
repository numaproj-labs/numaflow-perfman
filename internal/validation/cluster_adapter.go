package validation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/central"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/oracle"
	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

// ClusterAdapter wires namespace, controller, and scenario rendering for validation runs.
type ClusterAdapter struct {
	Cluster              cluster.Client
	Namespaces           namespace.Manager
	ValidationPostgresNS string
	DeployTimeout        time.Duration
	NamespaceDeleteWait  time.Duration

	boundRunID         string
	boundNumaflowImage string
	boundScenarioID    string
	boundImageTag      string
}

// BindRunID sets run context used for namespace and manifest labels.
func (a *ClusterAdapter) BindRunID(runID uuid.UUID, numaflowImage, scenarioID string) {
	a.boundRunID = runID.String()
	a.boundNumaflowImage = numaflowImage
	a.boundScenarioID = scenarioID
	a.boundImageTag = runmeta.ImageTagSegment(numaflowImage)
}

func (a *ClusterAdapter) nsMgr() namespace.Manager {
	mgr := a.Namespaces
	if mgr.Cluster.Runner == nil && mgr.Cluster.Context == "" {
		mgr.Cluster = a.Cluster
	}
	return mgr
}

func (a *ClusterAdapter) deployTimeout(fallback time.Duration) time.Duration {
	if fallback > 0 {
		return fallback
	}
	if a.DeployTimeout > 0 {
		return a.DeployTimeout
	}
	return 90 * time.Minute
}

func (a *ClusterAdapter) validationNS() string {
	if a.ValidationPostgresNS != "" {
		return a.ValidationPostgresNS
	}
	return "validation-system"
}

func (a *ClusterAdapter) CreateNamespace(ctx context.Context, name string) error {
	if a.boundRunID == "" {
		return fmt.Errorf("cluster adapter run id not bound")
	}
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
		if err := a.clearRetainedScenarioFinalizers(ctx, name); err != nil {
			return fmt.Errorf("clear retained namespace finalizers %q: %w", name, err)
		}
		wait := a.NamespaceDeleteWait
		if wait == 0 {
			wait = 2 * time.Minute
		}
		if err := mgr.Delete(ctx, name, wait); err != nil {
			return fmt.Errorf("delete existing namespace %q: %w", name, err)
		}
	}
	if a.boundScenarioID != "" && a.boundImageTag != "" {
		if err := mgr.CreateValidation(ctx, name, a.boundRunID, a.boundScenarioID, a.boundImageTag, a.boundNumaflowImage); err != nil {
			return err
		}
	} else if err := mgr.Create(ctx, name, a.boundRunID, namespace.KindValidation); err != nil {
		return err
	}
	return mgr.WaitReady(ctx, name, a.deployTimeout(0))
}

func (a *ClusterAdapter) DeleteNamespace(ctx context.Context, name string) error {
	wait := a.NamespaceDeleteWait
	if wait == 0 {
		wait = 2 * time.Minute
	}
	return a.nsMgr().Delete(ctx, name, wait)
}

func (a *ClusterAdapter) RetainNamespace(ctx context.Context, name string) error {
	return a.nsMgr().RetainFailure(ctx, name)
}

func (a *ClusterAdapter) clearRetainedScenarioFinalizers(ctx context.Context, namespaceName string) error {
	if a.boundScenarioID == "" {
		return nil
	}
	cat, err := scenario.LookupValidation(a.boundScenarioID)
	if err != nil {
		return err
	}
	var resources []string
	if cat.NeedsISB {
		resources = append(resources,
			"pipeline/"+cat.ID,
			"interstepbufferservice/"+cat.ID,
		)
	} else {
		resources = append(resources, "monovertex/"+cat.ID)
	}
	for _, resource := range resources {
		if err := a.Cluster.ClearResourceFinalizers(ctx, namespaceName, resource, true); err != nil {
			return fmt.Errorf("%s: %w", resource, err)
		}
	}
	return nil
}

func (a *ClusterAdapter) ScaleControllerToZero(ctx context.Context, ns string) error {
	return a.Cluster.ScaleDeployment(ctx, ns, controller.DeploymentName, 0)
}

func (a *ClusterAdapter) DeployController(ctx context.Context, namespaceName, imageRef string) error {
	if err := controller.ValidateImageReference(imageRef); err != nil {
		return err
	}
	manifests, err := controller.Render(controller.RenderInput{
		Namespace:      namespaceName,
		ImageReference: imageRef,
		RunID:          a.boundRunID,
		ScenarioID:     a.boundScenarioID,
	})
	if err != nil {
		return err
	}
	if !strings.Contains(manifests.Deployment, imageRef) {
		return fmt.Errorf("controller manifest image mismatch: expected %q", imageRef)
	}
	if !strings.Contains(manifests.Deployment, controller.ImagePullPolicy) {
		return fmt.Errorf("controller must use %s pull policy", controller.ImagePullPolicy)
	}
	client := a.Cluster
	for _, doc := range manifests.ApplyDocuments() {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(doc)); err != nil {
			return err
		}
	}
	return nil
}

func (a *ClusterAdapter) WaitControllerReady(ctx context.Context, namespaceName string, timeout time.Duration) error {
	timeout = a.deployTimeout(timeout)
	return a.Cluster.WaitForCondition(ctx, namespaceName,
		"deployment/"+controller.DeploymentName,
		"condition=Available",
		timeout,
	)
}

func (a *ClusterAdapter) DeployScenario(ctx context.Context, namespaceName string, sc oracle.Scenario, dbName, udfImage string) error {
	if udfImage == "" {
		return fmt.Errorf("udf image is required")
	}
	if err := a.ensurePostgresCredentialsSecret(ctx, namespaceName); err != nil {
		return err
	}
	bundle, err := scenario.RenderManifests(scenario.RenderInput{
		ScenarioID:     string(sc),
		Kind:           scenario.RunKindValidation,
		Namespace:      namespaceName,
		UDFImage:       udfImage,
		ValidationNS:   a.validationNS(),
		PostgresDBName: dbName,
		RunID:          a.boundRunID,
		NumaflowImage:  a.boundNumaflowImage,
	})
	if err != nil {
		return err
	}
	client := a.Cluster
	if bundle.ISB != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.ISB)); err != nil {
			return err
		}
	}
	if bundle.Pipeline != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.Pipeline)); err != nil {
			return err
		}
	}
	if bundle.MonoVertex != "" {
		if _, err := client.ApplyYAML(ctx, namespaceName, []byte(bundle.MonoVertex)); err != nil {
			return err
		}
	}
	return nil
}

// ensurePostgresCredentialsSecret copies central validation Postgres credentials into the run namespace so UDF pods can use secretKeyRef.
func (a *ClusterAdapter) ensurePostgresCredentialsSecret(ctx context.Context, runNS string) error {
	srcNS := a.validationNS()
	return a.Cluster.CopySecretToNamespace(ctx, srcNS, runNS, central.PostgresSecretName)
}

func (a *ClusterAdapter) WaitScenarioReady(ctx context.Context, namespaceName string, sc oracle.Scenario, timeout time.Duration) error {
	timeout = a.deployTimeout(timeout)
	cat, err := scenario.LookupValidation(string(sc))
	if err != nil {
		return err
	}
	if cat.NeedsISB {
		// Numaflow sets phase=Running only after the pipeline's
		// VerticesHealthy condition succeeds. A separate pod-selector wait can
		// race with asynchronous vertex creation and remain blocked.
		return a.Cluster.WaitForCondition(ctx, namespaceName, "pipeline/"+cat.ID, "jsonpath={.status.phase}=Running", timeout)
	}
	return a.Cluster.WaitForCondition(ctx, namespaceName, "monovertex/monovertex", "jsonpath={.status.phase}=Running", timeout)
}

// DeleteScenario removes Numaflow resources while the namespace-scoped controller
// remains available to complete their finalizers.
func (a *ClusterAdapter) DeleteScenario(ctx context.Context, namespaceName string, sc oracle.Scenario, timeout time.Duration) error {
	timeout = a.deployTimeout(timeout)
	cat, err := scenario.LookupValidation(string(sc))
	if err != nil {
		return err
	}
	if cat.NeedsISB {
		pipeline := "pipeline/" + cat.ID
		if err := a.Cluster.DeleteResource(ctx, namespaceName, pipeline, true); err != nil {
			return err
		}
		if err := a.waitForResourceDeletion(ctx, namespaceName, pipeline, timeout); err != nil {
			return err
		}
		isb := "interstepbufferservice/" + cat.ID
		if err := a.Cluster.DeleteResource(ctx, namespaceName, isb, true); err != nil {
			return err
		}
		return a.waitForResourceDeletion(ctx, namespaceName, isb, timeout)
	}

	monoVertex := "monovertex/" + cat.ID
	if err := a.Cluster.DeleteResource(ctx, namespaceName, monoVertex, true); err != nil {
		return err
	}
	return a.waitForResourceDeletion(ctx, namespaceName, monoVertex, timeout)
}

func (a *ClusterAdapter) waitForResourceDeletion(ctx context.Context, namespaceName, resource string, timeout time.Duration) error {
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
