//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"numa-perfman/internal/central"
	"numa-perfman/internal/cli"
	"numa-perfman/internal/config"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/kindconfig"
	"numa-perfman/internal/results"
)

const (
	envIntegration      = "PERFHARNESS_INTEGRATION"
	envIntegrationFull  = "PERFHARNESS_INTEGRATION_FULL"
	envIntegrationValid = "PERFHARNESS_INTEGRATION_VALIDATION"
	envImage            = "PERFHARNESS_IMAGE"
	envImageAlt         = "IMAGE"
	envBenchmarkImage   = "PERFHARNESS_BENCHMARK_IMAGE"
	envBenchmarkImageB  = "PERFHARNESS_BENCHMARK_IMAGE_B"
	envExternalImage    = "PERFHARNESS_EXTERNAL_IMAGE"
)

var (
	sharedOnce sync.Once
	sharedEnv  *env
	sharedErr  error
)

// TestMain tears down the disposable kind cluster created for this package run.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedEnv != nil {
		sharedEnv.destroyCluster()
	}
	os.Exit(code)
}

type env struct {
	root       string
	cluster    string
	context    string
	workDir    string
	setupImage string
	benchImg   string
	benchImgB  string
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv(envIntegration) != "1" {
		t.Skip("set PERFHARNESS_INTEGRATION=1 to opt in (creates a disposable kind cluster; never touches the default numaflow cluster)")
	}
}

func setupImageFromEnv() string {
	return firstNonEmpty(os.Getenv(envImage), os.Getenv(envImageAlt), config.DefaultNumaflowImage)
}

func requireBenchmarkImage(t *testing.T) string {
	t.Helper()
	v := os.Getenv(envBenchmarkImage)
	if v == "" {
		t.Skipf("%s is required (complete Numaflow controller OCI reference for benchmark/validation runs)", envBenchmarkImage)
	}
	return v
}

func optionalBenchmarkImageB(t *testing.T) (string, bool) {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(envBenchmarkImageB))
	if v == "" {
		return "", false
	}
	return v, true
}

func requireExternalImage(t *testing.T) string {
	t.Helper()
	v := os.Getenv(envExternalImage)
	if v == "" {
		t.Skipf("%s is required to verify pull-if-absent behavior against an external registry", envExternalImage)
	}
	return v
}

func requireValidationIntegration(t *testing.T) {
	t.Helper()
	requireIntegration(t)
	if os.Getenv(envIntegrationValid) != "1" {
		t.Skipf("set %s=1 to run validation scenarios against Postgres (setup --with-validation)", envIntegrationValid)
	}
}

func requireFullAcceptance(t *testing.T) {
	t.Helper()
	requireIntegration(t)
	if os.Getenv(envIntegrationFull) != "1" {
		t.Skipf("set %s=1 for the long §16.4 acceptance suite (single-map per image, all validations, compare reports)", envIntegrationFull)
	}
}

func shared(t *testing.T) *env {
	t.Helper()
	requireIntegration(t)
	sharedOnce.Do(func() {
		root, err := findRepoRoot()
		if err != nil {
			sharedErr = err
			return
		}
		sharedEnv, sharedErr = newEnv(root, setupImageFromEnv(), os.Getenv(envBenchmarkImage))
		if sharedErr != nil {
			return
		}
		sharedErr = sharedEnv.bootstrap()
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	return sharedEnv
}

func newEnv(root, setupImage, benchImage string) (*env, error) {
	if setupImage == "" {
		setupImage = config.DefaultNumaflowImage
	}
	workDir, err := os.MkdirTemp("", "perfman-int-*")
	if err != nil {
		return nil, err
	}
	suffix := randomSuffix(4)
	cluster := "perfh-int-" + suffix
	return &env{
		root:       root,
		cluster:    cluster,
		context:    "kind-" + cluster,
		workDir:    workDir,
		setupImage: setupImage,
		benchImg:   benchImage,
		benchImgB:  os.Getenv(envBenchmarkImageB),
	}, nil
}

func (e *env) destroyCluster() {
	if e == nil {
		return
	}
	_, _ = runCmd(e.root, "", "kind", "delete", "cluster", "--name", e.cluster)
	_ = os.RemoveAll(e.workDir)
}

func (e *env) bootstrap() error {
	if err := ensureCLI(e.root); err != nil {
		return err
	}
	kindCfg := filepath.Join(e.root, kindconfig.DefaultConfigPath)
	if _, err := runCmd(e.root, "", "kind", "create", "cluster",
		"--name", e.cluster,
		"--config", kindCfg,
		"--image", kindconfig.DefaultNodeImage,
		"--wait", "5m",
	); err != nil {
		return fmt.Errorf("kind create: %w", err)
	}
	withValidation := os.Getenv(envIntegrationValid) == "1" || os.Getenv(envIntegrationFull) == "1"
	setupArgs := []string{"setup", "--create-cluster", "--image", e.setupImage}
	if withValidation {
		setupArgs = append(setupArgs, "--with-validation")
	}
	code, out := e.runCLI(setupArgs...)
	if code != cli.ExitSuccess {
		return fmt.Errorf("setup failed (exit %d): %s", code, out)
	}
	doctorArgs := []string{"doctor"}
	if withValidation {
		doctorArgs = append(doctorArgs, "--with-validation")
	}
	code, out = e.runCLI(doctorArgs...)
	if code != cli.ExitSuccess {
		return fmt.Errorf("doctor after setup failed (exit %d): %s", code, out)
	}
	return nil
}

func (e *env) runCLI(args ...string) (int, string) {
	bin := filepath.Join(e.root, "bin", "perfman")
	global := []string{
		"--cluster", e.cluster,
		"--context", e.context,
	}
	cmd := exec.Command(bin, append(global, args...)...)
	cmd.Dir = e.workDir
	cmd.Env = append(os.Environ(), e.configEnv()...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
			buf.WriteString(err.Error())
		}
	}
	return code, buf.String()
}

func (e *env) configEnv() []string {
	return []string{
		"PERFHARNESS_CLUSTER=" + e.cluster,
		"PERFHARNESS_CONTEXT=" + e.context,
		"PERFHARNESS_RESULTS_DB=" + filepath.Join(e.workDir, "results", "perfman.db"),
	}
}

func (e *env) kubectl(args ...string) (string, error) {
	base := []string{"--context", e.context}
	if len(args) > 0 {
		base = append(base, args...)
	}
	out, errOut, err := runCmdOutput(e.root, e.workDir, "kubectl", base...)
	if err != nil {
		return out + errOut, err
	}
	return out, nil
}

func (e *env) openResults(ctx context.Context) (*results.Repository, error) {
	dbPath := filepath.Join(e.workDir, "results", "perfman.db")
	return results.Open(ctx, dbPath)
}

func (e *env) loadImage(image string) error {
	_, err := runCmd(e.root, e.workDir, "kind", "load", "docker-image", "--name", e.cluster, image)
	return err
}

func (e *env) imageOnNodes(image string) (bool, error) {
	out, err := e.kubectl("get", "nodes", "-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return false, err
	}
	node := strings.TrimSpace(out)
	if node == "" {
		return false, fmt.Errorf("no kind nodes")
	}
	stdout, _, err := runCmdOutput(e.root, e.workDir, "docker", "exec", node, "crictl", "images")
	if err != nil {
		return false, err
	}
	ref := strings.TrimSpace(image)
	repo := ref
	if i := strings.LastIndex(ref, ":"); i > 0 {
		repo = ref[:i]
	}
	return strings.Contains(stdout, ref) || strings.Contains(stdout, repo), nil
}

func (e *env) serverCanListPipelinesIn(ns string) (bool, string, error) {
	sa := fmt.Sprintf("system:serviceaccount:%s:%s", config.Defaults().CentralNamespace, central.ServerServiceAccountName)
	out, err := e.kubectl(
		"auth", "can-i", "list", "pipelines.numaflow.numaproj.io",
		"--as="+sa,
		"-n", ns,
	)
	if err != nil {
		return false, out, err
	}
	return strings.TrimSpace(out) == "yes", out, nil
}

func (e *env) controllerPullPolicyIn(ns string) (string, error) {
	out, err := e.kubectl(
		"get", "deployment", controller.DeploymentName, "-n", ns,
		"-o", "jsonpath={.spec.template.spec.containers[0].imagePullPolicy}",
	)
	return strings.TrimSpace(out), err
}

func (e *env) controllerReplicasIn(ns string) (int, error) {
	out, err := e.kubectl(
		"get", "deployment", controller.DeploymentName, "-n", ns,
		"-o", "jsonpath={.spec.replicas}",
	)
	if err != nil {
		return -1, err
	}
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n, nil
}

func ensureCLI(root string) error {
	bin := filepath.Join(root, "bin", "perfman")
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		return nil
	}
	_, err := runCmd(root, "", "go", "build", "-o", bin, "./cmd/perfman")
	return err
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func runCmd(dir, workDir, name string, args ...string) (string, error) {
	out, errOut, err := runCmdOutput(dir, workDir, name, args...)
	if err != nil {
		return out + errOut, err
	}
	return out, nil
}

func runCmdOutput(dir, workDir, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func benchShortFlags() []string {
	return []string{"--duration", "2m", "--yes"}
}

func waitForNoActiveRunNamespaces(t *testing.T, e *env) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := e.kubectl("get", "ns", "-l", "perfman.numaproj.io/managed-by=numaflow-perfman", "-o", "name")
		if err != nil {
			t.Fatalf("list run namespaces: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatal("active perfman run namespaces still present")
}
