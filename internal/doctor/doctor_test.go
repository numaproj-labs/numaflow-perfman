package doctor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/doctor"
	"numa-perfman/internal/results"
)

type roInspector struct {
	mutating []string
	nodeJSON string
}

func (r *roInspector) CurrentContext(ctx context.Context) (string, error) {
	return "kind-numaflow", nil
}
func (r *roInspector) KindClusterExists(ctx context.Context, cluster string) (bool, error) {
	return true, nil
}
func (r *roInspector) NodesReadyJSON(ctx context.Context) (string, error) {
	if r.nodeJSON != "" {
		return r.nodeJSON, nil
	}
	return `{"items":[{"metadata":{"name":"n1"},"status":{"conditions":[{"type":"Ready","status":"True"}],"capacity":{"cpu":"4","memory":"8Gi","ephemeral-storage":"20Gi"},"allocatable":{"cpu":"3","memory":"8Gi","ephemeral-storage":"20Gi"}}}]}`, nil
}
func (r *roInspector) CRDExists(ctx context.Context, name string) (bool, error) {
	return true, nil
}
func (r *roInspector) DeploymentReady(ctx context.Context, ns, name string) (bool, error) {
	return true, nil
}
func (r *roInspector) StatefulSetReady(ctx context.Context, ns, name string) (bool, error) {
	return true, nil
}
func (r *roInspector) DefaultStorageClass(ctx context.Context) (string, error) {
	return "standard", nil
}
func (r *roInspector) KindNodeList(ctx context.Context, cluster string) ([]string, error) {
	return []string{"node"}, nil
}
func (r *roInspector) KindNodeReadFile(ctx context.Context, nodeContainer, path string) (string, error) {
	return `cpuManagerPolicy: static
reservedSystemCPUs: "0"
kubeReserved:
  cpu: "1"
cpuManagerPolicyOptions:
  strict-cpu-reservation: "true"
`, nil
}
func (r *roInspector) ImagePresentOnKindNode(ctx context.Context, nodeContainer, imageRef string) (bool, error) {
	return true, nil
}
func (r *roInspector) ListActiveRunNamespaces(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (r *roInspector) DeploymentReplicas(ctx context.Context, ns, name string) (int, error) {
	return 0, nil
}
func (r *roInspector) GetResourceJSON(ctx context.Context, ns, resource string) (string, error) {
	return `{"data":{"prometheus.yml":"cadvisor"}}`, nil
}
func (r *roInspector) NamespaceExists(ctx context.Context, name string) (bool, error) {
	return false, nil
}

type fakeRunner struct{}

func (fakeRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if name == "kind" && len(args) > 0 && args[0] == "version" {
		return "kind v0.32.0 go1.24.6 linux/amd64\n", "", nil
	}
	if name == "docker" && len(args) > 0 && args[0] == "info" {
		return "2\n", "", nil
	}
	return "", "", nil
}

func (fakeRunner) Start(ctx context.Context, name string, args ...string) (cluster.Process, error) {
	return nil, errors.New("not implemented")
}

type fakeHost struct {
	alivePID int
}

func (h fakeHost) ProcessAlive(pid int) bool {
	return pid == h.alivePID && h.alivePID > 0
}
func (h fakeHost) Now() time.Time { return time.Unix(1_700_000_000, 0) }
func (h fakeHost) NamespaceActive(ctx context.Context, namespace string) (bool, error) {
	return false, nil
}

func openDoctorRepo(t *testing.T) *results.Repository {
	t.Helper()
	repo, err := results.Open(context.Background(), t.TempDir()+"/doctor.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestDoctorPass(t *testing.T) {
	cfg := config.Defaults()
	cfg.UDFImage = "numaflow-perfman-udfs:abc"
	repo := openDoctorRepo(t)
	res := doctor.Run(context.Background(), doctor.Options{
		Config:     cfg,
		Host:       fakeHost{},
		Repository: repo,
	}, &roInspector{}, fakeRunner{})
	if !res.Passed() {
		t.Fatalf("expected pass, got %v", res.Error())
	}
}

func TestDoctorUIImageCompatibilityWarn(t *testing.T) {
	cfg := config.Defaults()
	cfg.UDFImage = "numaflow-perfman-udfs:abc"
	cfg.Image = "quay.io/numaproj/numaflow:v1.9.0"
	repo := openDoctorRepo(t)
	res := doctor.Run(context.Background(), doctor.Options{
		Config:     cfg,
		Host:       fakeHost{},
		Repository: repo,
	}, &roInspector{}, fakeRunner{})
	if !res.Passed() {
		t.Fatalf("warn-only compat should not block: %v", res.Error())
	}
	found := false
	for _, c := range res.Checks {
		if c.ID == "central.ui.outside_tested_range" && c.Status == doctor.StatusWarn {
			found = true
		}
	}
	if !found {
		t.Fatal("expected outside_tested_range warning")
	}
}

func TestDoctorBlocksInsufficientCapacity(t *testing.T) {
	cfg := config.Defaults()
	cfg.UDFImage = "numaflow-perfman-udfs:abc"
	inspector := &roInspector{
		nodeJSON: `{"items":[{"metadata":{"name":"n1"},"status":{"conditions":[{"type":"Ready","status":"True"}],"capacity":{"cpu":"2","memory":"4Gi","ephemeral-storage":"10Gi"},"allocatable":{"cpu":"1","memory":"4Gi","ephemeral-storage":"10Gi"}}}]}`,
	}
	repo := openDoctorRepo(t)
	res := doctor.Run(context.Background(), doctor.Options{
		Config:     cfg,
		Host:       fakeHost{},
		Repository: repo,
	}, inspector, fakeRunner{})
	if res.Passed() {
		t.Fatal("expected insufficient capacity to block doctor")
	}
}

func TestDoctorBlockersOnActiveLock(t *testing.T) {
	cfg := config.Defaults()
	cfg.UDFImage = "numaflow-perfman-udfs:abc"
	repo := openDoctorRepo(t)
	ctx := context.Background()
	if err := repo.AcquireActiveLock(ctx, results.AcquireActiveLockParams{
		LockKey: "benchmark:single-map:v1.8.0", OwnerToken: "owner", PID: 42, Host: "test",
		Command: "benchmark run", RunID: "run-1", Scenario: "single-map", ImageTag: "v1.8.0",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	res := doctor.Run(ctx, doctor.Options{
		Config:     cfg,
		Host:       fakeHost{alivePID: 42},
		Repository: repo,
	}, &roInspector{}, fakeRunner{})
	if res.Passed() {
		t.Fatal("expected failure")
	}
	if res.Error() == nil {
		t.Fatal("expected aggregate error")
	}
}

func TestDoctorDoesNotMutate(t *testing.T) {
	insp := &roInspector{}
	cfg := config.Defaults()
	cfg.UDFImage = "numaflow-perfman-udfs:abc"
	repo := openDoctorRepo(t)
	_ = doctor.Run(context.Background(), doctor.Options{
		Config:     cfg,
		Host:       fakeHost{},
		Repository: repo,
	}, insp, fakeRunner{})
	if len(insp.mutating) != 0 {
		t.Fatalf("doctor mutated cluster: %v", insp.mutating)
	}
}
