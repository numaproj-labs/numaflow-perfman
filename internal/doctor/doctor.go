package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"numa-perfman/internal/central"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/namespace"
	"numa-perfman/internal/results"
	"numa-perfman/internal/setup"
)

// Options configures read-only preflight checks.
type Options struct {
	Config         config.Config
	WithValidation bool
	Now            time.Time
	HTTPClient     *http.Client
	Host           Host
	Repository     *results.Repository
}

// Inspector is the injectable read-only cluster/host surface.
type Inspector interface {
	CurrentContext(ctx context.Context) (string, error)
	KindClusterExists(ctx context.Context, cluster string) (bool, error)
	NodesReadyJSON(ctx context.Context) (string, error)
	CRDExists(ctx context.Context, name string) (bool, error)
	DeploymentReady(ctx context.Context, ns, name string) (bool, error)
	StatefulSetReady(ctx context.Context, ns, name string) (bool, error)
	DefaultStorageClass(ctx context.Context) (string, error)
	KindNodeList(ctx context.Context, cluster string) ([]string, error)
	KindNodeReadFile(ctx context.Context, nodeContainer, path string) (string, error)
	ImagePresentOnKindNode(ctx context.Context, nodeContainer, imageRef string) (bool, error)
	ListActiveRunNamespaces(ctx context.Context) ([]string, error)
	DeploymentReplicas(ctx context.Context, ns, name string) (int, error)
	GetResourceJSON(ctx context.Context, ns, resource string) (string, error)
	NamespaceExists(ctx context.Context, name string) (bool, error)
}

// Host provides host process introspection.
type Host interface {
	Now() time.Time
	ProcessAlive(pid int) bool
	NamespaceActive(ctx context.Context, namespace string) (bool, error)
}

type defaultHost struct {
	Inspector Inspector
}

func (h defaultHost) Now() time.Time { return time.Now() }

func (h defaultHost) ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (h defaultHost) NamespaceActive(ctx context.Context, ns string) (bool, error) {
	if h.Inspector == nil || ns == "" {
		return false, nil
	}
	if !namespace.IsActiveRunNamespace(ns) {
		return false, nil
	}
	return h.Inspector.NamespaceExists(ctx, ns)
}

// Examiner runs doctor checks without mutating cluster state.
type Examiner struct {
	Inspector Inspector
	Runner    cluster.Runner
	Host      Host
}

// Run executes all configured checks.
func Run(ctx context.Context, opts Options, insp Inspector, run cluster.Runner) Result {
	ex := Examiner{Inspector: insp, Runner: run, Host: defaultHost{Inspector: insp}}
	if opts.Host != nil {
		ex.Host = opts.Host
	}
	return ex.run(ctx, opts)
}

func (ex Examiner) run(ctx context.Context, opts Options) Result {
	var checks []Check
	checks = append(checks, ex.checkTools(ctx)...)
	checks = append(checks, ex.checkKindVersion(ctx)...)
	checks = append(checks, ex.checkHostCgroup(ctx)...)
	checks = append(checks, ex.checkContext(ctx, opts)...)
	checks = append(checks, ex.checkNodes(ctx)...)
	checks = append(checks, ex.checkCapacity(ctx)...)
	checks = append(checks, ex.checkStaticCPUReservation(ctx)...)
	checks = append(checks, ex.checkKubeletStaticCPU(ctx, opts.Config.Cluster)...)
	checks = append(checks, ex.checkCRDs(ctx)...)
	checks = append(checks, ex.checkCentralUI(ctx, opts)...)
	checks = append(checks, ex.checkUIImageCompatibility(opts)...)
	checks = append(checks, ex.checkPrometheus(ctx, opts)...)
	checks = append(checks, ex.checkCadvisor(ctx, opts)...)
	checks = append(checks, ex.checkDefaultStorageClass(ctx)...)
	if opts.WithValidation {
		checks = append(checks, ex.checkPostgres(ctx, opts)...)
	}
	checks = append(checks, ex.checkUDFImages(ctx, opts)...)
	checks = append(checks, ex.checkResultsDB(ctx, opts)...)
	checks = append(checks, ex.checkActiveLocks(ctx, opts)...)
	checks = append(checks, ex.checkStaleRunNamespaces(ctx)...)
	checks = append(checks, ex.checkClock(ctx, opts)...)
	return Result{Checks: checks}
}

func (ex Examiner) checkTools(ctx context.Context) []Check {
	runner := ex.Runner
	if runner == nil {
		runner = cluster.DefaultRunner
	}
	var out []Check
	for _, tool := range []string{"docker", "kind", "kubectl"} {
		if err := cluster.CheckTool(ctx, runner, tool); err != nil {
			out = append(out, fail("tools."+tool, tool+" available", err.Error(), true))
		} else {
			out = append(out, pass("tools."+tool, tool+" available", "ok"))
		}
	}
	return out
}

func (ex Examiner) checkContext(ctx context.Context, opts Options) []Check {
	ctxName, err := ex.Inspector.CurrentContext(ctx)
	if err != nil {
		return []Check{fail("context.current", "kube context", err.Error(), true)}
	}
	if opts.Config.Context != "" && ctxName != opts.Config.Context {
		return []Check{fail("context.match", "expected kube context",
			fmt.Sprintf("current %q, want %q", ctxName, opts.Config.Context), true)}
	}
	exists, err := ex.Inspector.KindClusterExists(ctx, opts.Config.Cluster)
	if err != nil {
		return []Check{fail("context.kind", "kind cluster", err.Error(), true)}
	}
	if !exists {
		return []Check{fail("context.kind", "kind cluster",
			fmt.Sprintf("cluster %q not found", opts.Config.Cluster), true)}
	}
	return []Check{pass("context.current", "kube context", ctxName)}
}

func (ex Examiner) checkNodes(ctx context.Context) []Check {
	raw, err := ex.Inspector.NodesReadyJSON(ctx)
	if err != nil {
		return []Check{fail("nodes.list", "node readiness", err.Error(), true)}
	}
	var obj struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return []Check{fail("nodes.parse", "node readiness", err.Error(), true)}
	}
	if len(obj.Items) == 0 {
		return []Check{fail("nodes.count", "node readiness", "no nodes registered", true)}
	}
	for _, node := range obj.Items {
		ready := false
		for _, cnd := range node.Status.Conditions {
			if cnd.Type == "Ready" && cnd.Status == "True" {
				ready = true
				break
			}
		}
		if !ready {
			return []Check{fail("nodes.ready", "node readiness",
				fmt.Sprintf("node %q not Ready", node.Metadata.Name), true)}
		}
	}
	return []Check{pass("nodes.ready", "node readiness", fmt.Sprintf("%d node(s) Ready", len(obj.Items)))}
}

func (ex Examiner) checkCapacity(ctx context.Context) []Check {
	raw, err := ex.Inspector.NodesReadyJSON(ctx)
	if err != nil {
		return []Check{fail("capacity.nodes", "basic capacity", err.Error(), true)}
	}
	var obj struct {
		Items []struct {
			Status struct {
				Capacity struct {
					CPU              string `json:"cpu"`
					Memory           string `json:"memory"`
					EphemeralStorage string `json:"ephemeral-storage"`
				} `json:"capacity"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return []Check{fail("capacity.parse", "basic capacity", err.Error(), true)}
	}
	const (
		minCPUCores     = 4.0
		minMemoryBytes  = int64(8 * 1024 * 1024 * 1024)
		minStorageBytes = int64(20 * 1024 * 1024 * 1024)
	)
	var totalCPU float64
	var totalMemory, totalStorage int64
	for i, node := range obj.Items {
		c := node.Status.Capacity
		if c.CPU == "" || c.Memory == "" || c.EphemeralStorage == "" {
			return []Check{fail("capacity.alloc", "basic capacity",
				fmt.Sprintf("node index %d missing capacity fields", i), true)}
		}
		cpu, err := parseCPUQuantity(c.CPU)
		if err != nil {
			return []Check{fail("capacity.cpu", "basic capacity", err.Error(), true)}
		}
		memory, err := parseBinaryQuantity(c.Memory)
		if err != nil {
			return []Check{fail("capacity.memory", "basic capacity", err.Error(), true)}
		}
		storage, err := parseBinaryQuantity(c.EphemeralStorage)
		if err != nil {
			return []Check{fail("capacity.storage", "basic capacity", err.Error(), true)}
		}
		totalCPU += cpu
		totalMemory += memory
		totalStorage += storage
	}
	if totalCPU < minCPUCores || totalMemory < minMemoryBytes || totalStorage < minStorageBytes {
		return []Check{fail("capacity.minimum", "basic capacity",
			fmt.Sprintf("cluster capacity %.2f CPU, %.1f GiB memory, %.1f GiB ephemeral storage; minimum is %.0f CPU, %.0f GiB, %.0f GiB",
				totalCPU, float64(totalMemory)/(1<<30), float64(totalStorage)/(1<<30),
				minCPUCores, float64(minMemoryBytes)/(1<<30), float64(minStorageBytes)/(1<<30)), true)}
	}
	return []Check{pass("capacity.alloc", "basic capacity",
		fmt.Sprintf("%.2f CPU, %.1f GiB memory, %.1f GiB ephemeral storage",
			totalCPU, float64(totalMemory)/(1<<30), float64(totalStorage)/(1<<30)))}
}

func parseCPUQuantity(value string) (float64, error) {
	if strings.HasSuffix(value, "m") {
		milli, err := strconv.ParseFloat(strings.TrimSuffix(value, "m"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid CPU capacity %q", value)
		}
		return milli / 1000, nil
	}
	cpu, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU capacity %q", value)
	}
	return cpu, nil
}

func parseBinaryQuantity(value string) (int64, error) {
	multipliers := []struct {
		suffix string
		value  float64
	}{
		{"Ti", 1 << 40},
		{"Gi", 1 << 30},
		{"Mi", 1 << 20},
		{"Ki", 1 << 10},
	}
	for _, multiplier := range multipliers {
		if strings.HasSuffix(value, multiplier.suffix) {
			number, err := strconv.ParseFloat(strings.TrimSuffix(value, multiplier.suffix), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid capacity %q", value)
			}
			return int64(number * multiplier.value), nil
		}
	}
	bytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid capacity %q", value)
	}
	return bytes, nil
}

func (ex Examiner) checkCRDs(ctx context.Context) []Check {
	required := []string{
		"pipelines.numaflow.numaproj.io",
		"monovertices.numaflow.numaproj.io",
		"interstepbufferservices.numaflow.numaproj.io",
		"vertices.numaflow.numaproj.io",
	}
	for _, name := range required {
		ok, err := ex.Inspector.CRDExists(ctx, name)
		if err != nil {
			return []Check{fail("crds."+name, "numaflow CRDs", err.Error(), true)}
		}
		if !ok {
			return []Check{fail("crds."+name, "numaflow CRDs", name+" missing", true)}
		}
	}
	return []Check{pass("crds.numaflow", "numaflow CRDs", "required CRDs present")}
}

func (ex Examiner) checkCentralUI(ctx context.Context, opts Options) []Check {
	ok, err := ex.Inspector.DeploymentReady(ctx, opts.Config.CentralNamespace, central.ServerDeploymentName)
	if err != nil {
		return []Check{fail("central.server", "central UI server", err.Error(), true)}
	}
	if !ok {
		return []Check{fail("central.server", "central UI server", "deployment not ready", true)}
	}
	return []Check{pass("central.server", "central UI server", "deployment ready")}
}

func (ex Examiner) checkUIImageCompatibility(opts Options) []Check {
	ui := strings.TrimSpace(opts.Config.Image)
	if ui == "" {
		return []Check{skip("central.ui_compat", "UI/controller compatibility", "image not set in config")}
	}
	var out []Check
	for _, w := range setup.CompatibilityWarnings(ui, "", opts.Config.TestedNumaflowMajorMinor) {
		out = append(out, warn("central."+w.Code, "UI/controller compatibility", w.Message))
	}
	if len(out) == 0 {
		out = append(out, pass("central.ui_compat", "UI/controller compatibility", "UI tag matches documented tested range"))
	}
	return out
}

func (ex Examiner) checkPrometheus(ctx context.Context, opts Options) []Check {
	ok, err := ex.Inspector.DeploymentReady(ctx, opts.Config.MonitoringNamespace, central.PrometheusDeploymentName)
	if err != nil {
		return []Check{fail("prometheus.ready", "prometheus", err.Error(), true)}
	}
	if !ok {
		return []Check{fail("prometheus.ready", "prometheus", "deployment not ready", true)}
	}
	return []Check{pass("prometheus.ready", "prometheus", "deployment ready")}
}

func (ex Examiner) checkCadvisor(ctx context.Context, opts Options) []Check {
	raw, err := ex.Inspector.GetResourceJSON(ctx, opts.Config.MonitoringNamespace, "configmap/prometheus-config")
	if err != nil {
		return []Check{fail("cadvisor.config", "cadvisor scrape config", err.Error(), true)}
	}
	if !strings.Contains(raw, "cadvisor") {
		return []Check{fail("cadvisor.config", "cadvisor scrape config", "prometheus config missing cadvisor job", true)}
	}
	url := opts.Config.PrometheusURL
	if url == "" {
		url = fmt.Sprintf("http://prometheus.%s.svc:9090", opts.Config.MonitoringNamespace)
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/api/v1/query?query=up{job=\"cadvisor\"}", nil)
	if err != nil {
		return []Check{warn("cadvisor.query", "cadvisor access", err.Error())}
	}
	resp, err := client.Do(req)
	if err != nil {
		return []Check{warn("cadvisor.query", "cadvisor access", err.Error())}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return []Check{warn("cadvisor.query", "cadvisor access", fmt.Sprintf("prometheus query HTTP %d", resp.StatusCode))}
	}
	if !strings.Contains(string(body), `"status":"success"`) {
		return []Check{warn("cadvisor.query", "cadvisor access", "prometheus query did not succeed")}
	}
	return []Check{pass("cadvisor.query", "cadvisor access", "prometheus can query cadvisor targets")}
}

func (ex Examiner) checkDefaultStorageClass(ctx context.Context) []Check {
	name, err := ex.Inspector.DefaultStorageClass(ctx)
	if err != nil {
		return []Check{fail("storageclass.default", "default storage class", err.Error(), true)}
	}
	if name == "" {
		return []Check{fail("storageclass.default", "default storage class", "none marked default", true)}
	}
	if name == "gp3" {
		return []Check{warn("storageclass.default", "default storage class", "gp3 default is unexpected on kind")}
	}
	return []Check{pass("storageclass.default", "default storage class", name)}
}

func (ex Examiner) checkPostgres(ctx context.Context, opts Options) []Check {
	ok, err := ex.Inspector.StatefulSetReady(ctx, opts.Config.ValidationNamespace, central.PostgresStatefulSetName)
	if err != nil {
		return []Check{fail("postgres.ready", "validation postgres", err.Error(), true)}
	}
	if !ok {
		return []Check{fail("postgres.ready", "validation postgres", "statefulset not ready", true)}
	}
	return []Check{pass("postgres.ready", "validation postgres", "statefulset ready")}
}

func (ex Examiner) checkUDFImages(ctx context.Context, opts Options) []Check {
	if strings.TrimSpace(opts.Config.UDFImage) == "" {
		return []Check{fail("udf.config", "udf image", "udf_image not configured (run setup)", true)}
	}
	nodes, err := ex.Inspector.KindNodeList(ctx, opts.Config.Cluster)
	if err != nil {
		return []Check{fail("udf.nodes", "udf image on nodes", err.Error(), true)}
	}
	for _, node := range nodes {
		ok, err := ex.Inspector.ImagePresentOnKindNode(ctx, node, opts.Config.UDFImage)
		if err != nil {
			return []Check{fail("udf.presence", "udf image on nodes", err.Error(), true)}
		}
		if !ok {
			return []Check{fail("udf.presence", "udf image on nodes",
				fmt.Sprintf("missing %q on %s", opts.Config.UDFImage, node), true)}
		}
	}
	return []Check{pass("udf.presence", "udf image on nodes", "present on all kind nodes")}
}

func (ex Examiner) checkResultsDB(ctx context.Context, opts Options) []Check {
	if err := opts.Config.EnsureResultsLayout(); err != nil {
		return []Check{fail("paths.results_db", "results database", err.Error(), true)}
	}
	if opts.Repository == nil {
		return []Check{skip("paths.results_db", "results database", "repository not available")}
	}
	if err := checkRepositoryWritable(ctx, opts.Repository); err != nil {
		return []Check{fail("paths.results_db", "results database", err.Error(), true)}
	}
	return []Check{pass("paths.results_db", "results database", opts.Config.ResultsDB+" writable")}
}

func checkRepositoryWritable(ctx context.Context, repo *results.Repository) error {
	if repo == nil {
		return errors.New("repository is required")
	}
	key := "doctor:write-check:" + uuid.NewString()
	token := uuid.NewString()
	if err := repo.AcquireActiveLock(ctx, results.AcquireActiveLockParams{
		LockKey: key, OwnerToken: token, PID: os.Getpid(), Host: "doctor", Command: "write-check",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("write check failed: %w", err)
	}
	if err := repo.ReleaseActiveLock(ctx, key, token); err != nil {
		return fmt.Errorf("write check cleanup failed: %w", err)
	}
	return nil
}

func (ex Examiner) checkActiveLocks(ctx context.Context, opts Options) []Check {
	if opts.Repository == nil {
		return []Check{skip("lock.active", "active locks", "repository not available")}
	}
	host := ex.Host
	if host == nil {
		host = defaultHost{Inspector: ex.Inspector}
	}
	locks, err := opts.Repository.ListActiveLocks(ctx)
	if err != nil {
		return []Check{fail("lock.list", "active locks", err.Error(), true)}
	}
	if len(locks) == 0 {
		return []Check{pass("lock.active", "active locks", "no active lock")}
	}
	var out []Check
	activeBenchmark := 0
	activeValidation := 0
	for _, lock := range locks {
		check := ex.checkOneActiveLock(ctx, host, lock)
		if strings.HasPrefix(lock.LockKey, "benchmark:") && check.Status == StatusFail && check.Blocker {
			activeBenchmark++
		}
		if strings.HasPrefix(lock.LockKey, "validation:") && check.Status == StatusFail && check.Blocker {
			activeValidation++
		}
		out = append(out, check)
	}
	if activeBenchmark > 1 {
		out = append(out, warn("lock.concurrent", "active locks",
			fmt.Sprintf("%d active benchmark locks", activeBenchmark)))
	}
	if activeValidation > 1 {
		out = append(out, warn("lock.concurrent", "active locks",
			fmt.Sprintf("%d active validation locks", activeValidation)))
	}
	return out
}

func (ex Examiner) checkOneActiveLock(ctx context.Context, host Host, lock results.ActiveLock) Check {
	kind := "lock"
	switch {
	case strings.HasPrefix(lock.LockKey, "validation:"):
		kind = "validation"
	case strings.HasPrefix(lock.LockKey, "benchmark:"):
		kind = "benchmark"
	}
	if host.ProcessAlive(lock.PID) {
		detail := fmt.Sprintf("lock held by pid %d key=%s", lock.PID, lock.LockKey)
		if lock.RunID != "" {
			detail = fmt.Sprintf("%s run=%s", detail, lock.RunID)
		}
		if lock.Scenario != "" {
			detail = fmt.Sprintf("%s scenario=%s tag=%s", detail, lock.Scenario, lock.ImageTag)
		}
		return fail("lock.active", kind+" lock", detail, true)
	}
	if lock.RunID != "" || lock.PID > 0 {
		nsActive := false
		if lock.Namespace != "" {
			active, err := host.NamespaceActive(ctx, lock.Namespace)
			if err != nil {
				return warn("lock.stale", kind+" lock",
					fmt.Sprintf("stale lock %s (pid %d run %s; namespace check: %v)", lock.LockKey, lock.PID, lock.RunID, err))
			}
			nsActive = active
		}
		if !nsActive {
			return warn("lock.stale", kind+" lock",
				fmt.Sprintf("stale lock %s (pid %d run %s not running)", lock.LockKey, lock.PID, lock.RunID))
		}
		return fail("lock.active", kind+" lock",
			fmt.Sprintf("lock %s references active namespace %s", lock.LockKey, lock.Namespace), true)
	}
	return pass("lock."+kind, kind+" lock", "no active lock")
}

func (ex Examiner) checkStaleRunNamespaces(ctx context.Context) []Check {
	names, err := ex.Inspector.ListActiveRunNamespaces(ctx)
	if err != nil {
		return []Check{fail("runs.list", "stale run namespaces", err.Error(), true)}
	}
	for _, ns := range names {
		if !namespace.IsActiveRunNamespace(ns) {
			continue
		}
		replicas, err := ex.Inspector.DeploymentReplicas(ctx, ns, "numaflow-controller")
		if err != nil {
			// Namespace may exist without controller during cleanup; treat as stale warning.
			return []Check{warn("runs.stale", "stale run namespaces", ns+" still present")}
		}
		if replicas > 0 {
			return []Check{fail("runs.active", "stale run namespaces",
				fmt.Sprintf("namespace %q has active controller replicas=%d", ns, replicas), true)}
		}
	}
	if len(names) > 0 {
		return []Check{warn("runs.retained", "stale run namespaces",
			fmt.Sprintf("%d retained namespace(s) with controller scaled to zero", len(names)))}
	}
	return []Check{pass("runs.active", "stale run namespaces", "no active run controllers")}
}

func (ex Examiner) checkClock(ctx context.Context, opts Options) []Check {
	host := ex.Host
	if host == nil {
		host = defaultHost{Inspector: ex.Inspector}
	}
	now := opts.Now
	if now.IsZero() {
		now = host.Now()
	}
	url := opts.Config.PrometheusURL
	if url == "" {
		return []Check{skip("clock.sync", "clock sanity", "prometheus URL not configured")}
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/api/v1/query?query=time()", nil)
	if err != nil {
		return []Check{warn("clock.sync", "clock sanity", err.Error())}
	}
	resp, err := client.Do(req)
	if err != nil {
		return []Check{warn("clock.sync", "clock sanity", err.Error())}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data struct {
			Result []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Data.Result) == 0 {
		return []Check{warn("clock.sync", "clock sanity", "unable to parse prometheus time()")}
	}
	tsStr, _ := parsed.Data.Result[0].Value[1].(string)
	var ts float64
	fmt.Sscanf(tsStr, "%f", &ts)
	promTime := time.Unix(int64(ts), 0)
	skew := now.Sub(promTime)
	if skew < 0 {
		skew = -skew
	}
	if skew > 5*time.Minute {
		return []Check{fail("clock.sync", "clock sanity",
			fmt.Sprintf("host/prometheus skew %s", skew), true)}
	}
	return []Check{pass("clock.sync", "clock sanity", fmt.Sprintf("skew %s", skew))}
}

// NamespaceLister wraps cluster list for run namespaces.
type namespaceLister struct {
	cluster.Client
}

func (n namespaceLister) ListActiveRunNamespaces(ctx context.Context) ([]string, error) {
	mgr := namespace.Manager{Cluster: n.Client}
	return mgr.ListActiveRunNamespaces(ctx)
}

// NewInspector wraps a cluster client for doctor checks.
func NewInspector(c cluster.Client) Inspector {
	return struct {
		namespaceLister
		cluster.Client
	}{namespaceLister{c}, c}
}
