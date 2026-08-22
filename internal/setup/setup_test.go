package setup_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/kindconfig"
	"numa-perfman/internal/setup"
)

type fakeCluster struct {
	t                        *testing.T
	mutating                 []string
	clusterMissing           bool
	metricsServerApplied     bool
	metricsServerWaitedReady bool
	kindCreateArgs           []string
}

func (f *fakeCluster) record(op string) {
	f.mutating = append(f.mutating, op)
}

func (f *fakeCluster) KindClusterExists(ctx context.Context, cluster string) (bool, error) {
	return !f.clusterMissing, nil
}
func (f *fakeCluster) KindCreateCluster(ctx context.Context, cluster string, extraArgs ...string) error {
	f.record("KindCreateCluster")
	f.kindCreateArgs = append([]string(nil), extraArgs...)
	return nil
}
func (f *fakeCluster) KindInstallCA(ctx context.Context, clusterName, certPath string) error {
	if certPath != "" {
		f.record("KindInstallCA")
	}
	return nil
}
func (f *fakeCluster) ApplyCRDsFromPath(ctx context.Context, path string, strict bool) error {
	f.record("ApplyCRDsFromPath")
	docs, err := cluster.LoadYAMLDocumentsFromPath(path)
	if err != nil {
		return err
	}
	if _, err := cluster.FilterCRDDocuments(docs, strict); err != nil {
		return err
	}
	return nil
}
func (f *fakeCluster) ApplyYAML(ctx context.Context, ns string, yaml []byte) (string, error) {
	f.record("ApplyYAML:" + ns)
	body := string(yaml)
	lower := strings.ToLower(body)
	if ns == "kube-system" && strings.Contains(body, "name: metrics-server") {
		f.metricsServerApplied = true
	}
	if strings.Contains(body, "name: numaflow-controller") && strings.Contains(body, "kind: Deployment") {
		f.t.Fatalf("setup must not apply run-scoped controller")
	}
	if strings.Contains(lower, "numa-perfman") && strings.Contains(body, "kind: Deployment") {
		f.t.Fatalf("setup must not apply perfman/controller run workloads")
	}
	return "", nil
}
func (f *fakeCluster) EnsureNamespace(ctx context.Context, name string) error {
	f.record("EnsureNamespace")
	return nil
}
func (f *fakeCluster) WaitForCondition(ctx context.Context, ns, resource, condition string, timeout time.Duration) error {
	if ns == "kube-system" && resource == "deployment/metrics-server" && condition == "condition=Available" {
		f.metricsServerWaitedReady = true
	}
	return nil
}
func (f *fakeCluster) KindLoadImage(ctx context.Context, cluster, image string) error {
	f.record("KindLoadImage")
	return nil
}
func (f *fakeCluster) KindNodeList(ctx context.Context, cluster string) ([]string, error) {
	return []string{"kind-node"}, nil
}
func (f *fakeCluster) ImagePresentOnKindNode(ctx context.Context, nodeContainer, imageRef string) (bool, error) {
	return true, nil
}
func (f *fakeCluster) DefaultStorageClass(ctx context.Context) (string, error) {
	return "standard", nil
}
func (f *fakeCluster) CurrentContext(ctx context.Context) (string, error) {
	return "kind-numaflow", nil
}

type fakeRunner struct{}

func (fakeRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if name == "kind" && len(args) > 0 && args[0] == "version" {
		return "kind v0.32.0 go1.24.6 linux/amd64\n", "", nil
	}
	return "", "", nil
}

func (fakeRunner) Start(ctx context.Context, name string, args ...string) (cluster.Process, error) {
	return nil, errors.New("not implemented")
}

type fakeCmds struct{}

func (fakeCmds) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	return "", "", nil
}
func (fakeCmds) RunInDir(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	if name == "git" {
		return "deadbeef\n", "", nil
	}
	return "", "", nil
}

func writeMetricsServerManifest(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "deploy", "metrics-server-components.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-server
  namespace: kube-system
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSetupDoesNotDeployRunController(t *testing.T) {
	fc := &fakeCluster{t: t, clusterMissing: true}
	cfg := config.Defaults()
	cfg.Image = "quay.io/numaproj/numaflow:v1.8.0"
	cfg.UDFImage = "numaflow-perfman-udfs:deadbeef"
	var progress bytes.Buffer
	outDir := t.TempDir()
	deployDir := filepath.Join(outDir, "deploy")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMetricsServerManifest(t, outDir)
	crds := filepath.Join(deployDir, "numaflow.yaml")
	if err := os.WriteFile(crds, []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: pipelines.numaflow.numaproj.io
spec:
  group: numaflow.numaproj.io
  names:
    kind: Pipeline
    plural: pipelines
  scope: Namespaced
  versions: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := setup.Run(context.Background(), setup.Options{
		Config:        cfg,
		CreateCluster: true,
		RepoRoot:      outDir,
		SkipUDFBuild:  true,
		Cluster:       fc,
		Runner:        fakeRunner{},
		Progress:      &progress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.UDFImage != "numaflow-perfman-udfs:deadbeef" {
		t.Fatalf("udf image %q", got.UDFImage)
	}
	if !fc.metricsServerApplied {
		t.Fatal("setup did not apply metrics-server to kube-system")
	}
	if !fc.metricsServerWaitedReady {
		t.Fatal("setup did not wait for metrics-server to become available")
	}
	if !containsAll(fc.kindCreateArgs, "--config", "--image", kindconfig.DefaultNodeImage, "--wait", "5m") {
		t.Fatalf("unexpected kind create args: %v", fc.kindCreateArgs)
	}
	for _, want := range []string{
		"checking required tools",
		"creating kind cluster",
		"installing metrics-server",
		"applying Numaflow CRDs",
		"installing central server and waiting for it to become available",
		"installing Prometheus and waiting for it to become available",
		`loading UDF image "numaflow-perfman-udfs:deadbeef"`,
	} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("setup progress missing %q:\n%s", want, progress.String())
		}
	}
}

func TestVendoredMetricsServerManifestEnablesKindKubeletTLSWorkaround(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "metrics-server-components.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(data)
	for _, want := range []string{
		"namespace: kube-system",
		"image: registry.k8s.io/metrics-server/metrics-server:v0.9.0",
		"--kubelet-insecure-tls",
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("metrics-server manifest missing %q", want)
		}
	}
}

func containsAll(haystack []string, needles ...string) bool {
	seen := make(map[string]bool, len(haystack))
	for _, item := range haystack {
		seen[item] = true
	}
	for _, needle := range needles {
		if !seen[needle] {
			return false
		}
	}
	return true
}

func TestSetupRequiresImage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Image = ""
	_, err := setup.Run(context.Background(), setup.Options{Config: cfg})
	if err == nil {
		t.Fatal("expected error without image")
	}
}
