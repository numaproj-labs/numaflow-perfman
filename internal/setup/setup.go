package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"numa-perfman/internal/central"
	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
	"numa-perfman/internal/controller"
	"numa-perfman/internal/host"
	"numa-perfman/internal/kindconfig"
)

const (
	defaultCRDsPath     = "deploy/numaflow.yaml"
	defaultMetricsPath  = "deploy/metrics-server-components.yaml"
	defaultKindConfig   = kindconfig.DefaultConfigPath
	defaultUDFImageBase = "numaflow-perfman-udfs"
	metricsNamespace    = "kube-system"
	metricsDeployment   = "metrics-server"
)

// Options controls idempotent cluster bootstrap for long-lived components.
type Options struct {
	Config config.Config

	CreateCluster  bool
	KindConfigPath string
	KindNodeImage  string
	CACertPath     string
	CRDsPath       string
	WithValidation bool
	RepoRoot       string
	SkipUDFBuild   bool
	UDFDockerfile  string
	Progress       io.Writer

	Runner  cluster.Runner
	Client  cluster.Client
	Cluster ClusterOps // optional override for tests
}

// ClusterOps is the injectable surface setup uses for cluster mutations.
type ClusterOps interface {
	KindClusterExists(ctx context.Context, cluster string) (bool, error)
	KindCreateCluster(ctx context.Context, cluster string, extraArgs ...string) error
	KindInstallCA(ctx context.Context, clusterName, certPath string) error
	ApplyCRDsFromPath(ctx context.Context, path string, strict bool) error
	ApplyYAML(ctx context.Context, ns string, yaml []byte) (string, error)
	EnsureNamespace(ctx context.Context, name string) error
	WaitForCondition(ctx context.Context, ns, resource, condition string, timeout time.Duration) error
	KindLoadImage(ctx context.Context, cluster, image string) error
	KindNodeList(ctx context.Context, cluster string) ([]string, error)
	ImagePresentOnKindNode(ctx context.Context, nodeContainer, imageRef string) (bool, error)
	DefaultStorageClass(ctx context.Context) (string, error)
	CurrentContext(ctx context.Context) (string, error)
}

// Commands runs host commands such as docker build and git rev-parse.
type Commands interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
	RunInDir(ctx context.Context, dir, name string, args ...string) (stdout, stderr string, err error)
}

type execCommands struct {
	runner cluster.Runner
}

func (e execCommands) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	return e.runner.Run(ctx, name, args...)
}

func (e execCommands) RunInDir(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	return cluster.RunWithDir(ctx, e.runner, dir, name, args...)
}

// Orchestrator performs setup steps using injectable dependencies.
type Orchestrator struct {
	Cluster  ClusterOps
	Commands Commands
	runner   cluster.Runner
	progress io.Writer
}

// Run executes setup and returns the resolved configuration snapshot.
func Run(ctx context.Context, opts Options) (config.Config, error) {
	o := newOrchestrator(opts)
	return o.run(ctx, opts)
}

func newOrchestrator(opts Options) Orchestrator {
	runner := opts.Runner
	client := opts.Client
	if client.Runner != nil && runner == nil {
		runner = client.Runner
	}
	if runner == nil {
		runner = cluster.DefaultRunner
	}
	client.Runner = runner
	var ops ClusterOps = client
	if opts.Cluster != nil {
		ops = opts.Cluster
	}
	cmds := Commands(execCommands{runner: runner})
	progress := opts.Progress
	if progress == nil {
		progress = os.Stderr
	}
	return Orchestrator{Cluster: ops, Commands: cmds, runner: runner, progress: progress}
}

func (o Orchestrator) run(ctx context.Context, opts Options) (config.Config, error) {
	cfg := opts.Config
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.Image) == "" {
		return cfg, errors.New("image is required for setup")
	}
	if err := controller.ValidateImageReference(cfg.Image); err != nil {
		return cfg, err
	}
	for _, w := range CompatibilityWarnings(cfg.Image, "", cfg.TestedNumaflowMajorMinor) {
		o.progressf("warning [%s]: %s", w.Code, w.Message)
	}
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return cfg, err
		}
		repoRoot = wd
	}
	o.progressf("checking required tools")
	for _, tool := range []string{"docker", "kind", "kubectl"} {
		o.progressf("checking %s availability", tool)
		if err := cluster.CheckTool(ctx, o.runner, tool); err != nil {
			return cfg, err
		}
	}
	if opts.CreateCluster {
		if err := o.warnKindVersion(ctx); err != nil {
			return cfg, err
		}
		if err := o.warnHostCgroup(); err != nil {
			return cfg, err
		}
		o.progressf("checking whether kind cluster %q exists", cfg.Cluster)
		exists, err := o.Cluster.KindClusterExists(ctx, cfg.Cluster)
		if err != nil {
			return cfg, err
		}
		if !exists {
			kindCfg := opts.KindConfigPath
			if kindCfg == "" {
				kindCfg = filepath.Join(repoRoot, defaultKindConfig)
			}
			nodeImage := strings.TrimSpace(opts.KindNodeImage)
			if nodeImage == "" {
				nodeImage = kindconfig.DefaultNodeImage
			}
			o.progressf("creating kind cluster %q using %s (node image %s)", cfg.Cluster, kindCfg, nodeImage)
			createArgs := []string{"--config", kindCfg, "--image", nodeImage, "--wait", "5m"}
			if err := o.Cluster.KindCreateCluster(ctx, cfg.Cluster, createArgs...); err != nil {
				return cfg, err
			}
		}
	}
	o.progressf("checking Kubernetes connectivity")
	if _, err := o.Cluster.CurrentContext(ctx); err != nil {
		return cfg, fmt.Errorf("cluster connectivity: %w", err)
	}
	if opts.CACertPath != "" {
		o.progressf("installing CA certificate into kind cluster %q", cfg.Cluster)
	}
	if err := o.Cluster.KindInstallCA(ctx, cfg.Cluster, opts.CACertPath); err != nil {
		return cfg, err
	}
	metricsPath := filepath.Join(repoRoot, defaultMetricsPath)
	o.progressf("installing metrics-server from %s", metricsPath)
	if err := o.applyMetricsServer(ctx, metricsPath); err != nil {
		return cfg, err
	}
	crds := opts.CRDsPath
	if crds == "" {
		crds = filepath.Join(repoRoot, defaultCRDsPath)
	}
	strictCRDs := strings.TrimSpace(opts.CRDsPath) != ""
	o.progressf("applying Numaflow CRDs from %s", crds)
	if err := o.Cluster.ApplyCRDsFromPath(ctx, crds, strictCRDs); err != nil {
		return cfg, err
	}
	for _, ns := range []string{cfg.CentralNamespace, cfg.MonitoringNamespace} {
		o.progressf("ensuring namespace %q", ns)
		if err := o.Cluster.EnsureNamespace(ctx, ns); err != nil {
			return cfg, err
		}
	}
	if opts.WithValidation {
		o.progressf("ensuring validation namespace %q", cfg.ValidationNamespace)
		if err := o.Cluster.EnsureNamespace(ctx, cfg.ValidationNamespace); err != nil {
			return cfg, err
		}
	}
	o.progressf("detecting the default storage class")
	defaultSC, err := o.Cluster.DefaultStorageClass(ctx)
	if err != nil {
		return cfg, err
	}
	o.progressf("installing central server and waiting for it to become available")
	if err := o.applyCentral(ctx, cfg); err != nil {
		return cfg, err
	}
	o.progressf("installing Prometheus and waiting for it to become available")
	if err := o.applyPrometheus(ctx, cfg); err != nil {
		return cfg, err
	}
	if opts.WithValidation {
		o.progressf("installing validation Postgres and waiting for it to become ready")
		if err := o.applyValidationPostgres(ctx, cfg, defaultSC); err != nil {
			return cfg, err
		}
	}
	o.progressf("preparing the UDF image")
	udfImage, err := o.resolveUDFImage(ctx, cfg, opts, repoRoot)
	if err != nil {
		return cfg, err
	}
	cfg.UDFImage = udfImage
	o.progressf("verifying the UDF image on kind nodes")
	if err := o.verifyUDFOnNodes(ctx, cfg); err != nil {
		return cfg, err
	}
	// Keep an empty URL as the explicit "auto-managed host port-forward" setting.
	// Cluster service DNS is not reachable by the host CLI.
	return cfg, nil
}

func (o Orchestrator) progressf(format string, args ...any) {
	fmt.Fprintf(o.progress, "perfman setup: "+format+"\n", args...)
}

func (o Orchestrator) warnKindVersion(ctx context.Context) error {
	runner := o.runner
	if runner == nil {
		runner = cluster.DefaultRunner
	}
	out, _, err := runner.Run(ctx, "kind", "version")
	if err != nil {
		o.progressf("warning: unable to read kind version: %v", err)
		return nil
	}
	version, err := kindconfig.ParseKindVersion(out)
	if err != nil {
		o.progressf("warning: %v", err)
		return nil
	}
	if !kindconfig.VersionAtLeast(version, kindconfig.MinKindVersion) {
		o.progressf("warning: kind %s is older than recommended %s (static CPU Manager needs Kubernetes 1.35+)", version, kindconfig.MinKindVersion)
	}
	return nil
}

func (o Orchestrator) warnHostCgroup() error {
	report, err := host.CheckCgroup()
	if err != nil {
		o.progressf("warning: unable to inspect host cgroup: %v", err)
		return nil
	}
	if !report.Ready() {
		o.progressf("warning: host cgroup may not support static CPU Manager (filesystem=%s cpuset=%v); see static-qos.md", report.FilesystemType, report.HasCPUSet)
	}
	return nil
}

func (o Orchestrator) applyMetricsServer(ctx context.Context, path string) error {
	manifest, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read metrics-server manifest %q: %w", path, err)
	}
	o.progressf("applying metrics-server manifests")
	if _, err := o.Cluster.ApplyYAML(ctx, metricsNamespace, manifest); err != nil {
		return fmt.Errorf("apply metrics-server manifest %q: %w", path, err)
	}
	o.progressf("waiting up to 5m for metrics-server to become available")
	return o.Cluster.WaitForCondition(ctx, metricsNamespace, "deployment/"+metricsDeployment,
		"condition=Available", 5*time.Minute)
}

func (o Orchestrator) applyCentral(ctx context.Context, cfg config.Config) error {
	set, err := central.RenderServer(central.ServerRenderInput{
		Namespace:           cfg.CentralNamespace,
		MonitoringNamespace: cfg.MonitoringNamespace,
		ImageReference:      cfg.Image,
	})
	if err != nil {
		return err
	}
	o.progressf("applying central server manifests")
	_, err = o.Cluster.ApplyYAML(ctx, cfg.CentralNamespace, []byte(set.Concat()))
	if err != nil {
		return err
	}
	o.progressf("waiting up to 5m for central server to become available")
	return o.Cluster.WaitForCondition(ctx, cfg.CentralNamespace, "deployment/"+central.ServerDeploymentName,
		"condition=Available", 5*time.Minute)
}

func (o Orchestrator) applyPrometheus(ctx context.Context, cfg config.Config) error {
	set, err := central.RenderPrometheus(central.PrometheusRenderInput{Namespace: cfg.MonitoringNamespace})
	if err != nil {
		return err
	}
	o.progressf("applying Prometheus manifests")
	_, err = o.Cluster.ApplyYAML(ctx, cfg.MonitoringNamespace, []byte(set.Concat()))
	if err != nil {
		return err
	}
	o.progressf("waiting up to 5m for Prometheus to become available")
	return o.Cluster.WaitForCondition(ctx, cfg.MonitoringNamespace, "deployment/"+central.PrometheusDeploymentName,
		"condition=Available", 5*time.Minute)
}

func (o Orchestrator) applyValidationPostgres(ctx context.Context, cfg config.Config, storageClass string) error {
	set, err := central.RenderValidationPostgres(central.PostgresRenderInput{
		Namespace:        cfg.ValidationNamespace,
		StorageClassName: storageClass,
	})
	if err != nil {
		return err
	}
	o.progressf("applying validation Postgres manifests")
	_, err = o.Cluster.ApplyYAML(ctx, cfg.ValidationNamespace, []byte(set.Concat()))
	if err != nil {
		return err
	}
	o.progressf("waiting up to 5m for validation Postgres to become ready")
	return o.Cluster.WaitForCondition(ctx, cfg.ValidationNamespace, "statefulset/"+central.PostgresStatefulSetName,
		"jsonpath={.status.readyReplicas}=1", 5*time.Minute)
}

func (o Orchestrator) resolveUDFImage(ctx context.Context, cfg config.Config, opts Options, repoRoot string) (string, error) {
	image := strings.TrimSpace(cfg.UDFImage)
	if image == "" {
		sha, err := o.defaultGitSHA(ctx, repoRoot)
		if err != nil {
			return "", err
		}
		image = defaultUDFImageBase + ":" + sha
		if !opts.SkipUDFBuild {
			df := opts.UDFDockerfile
			if df == "" {
				df = "udfs/Dockerfile"
			}
			o.progressf("building UDF image %q", image)
			_, stderr, err := o.Commands.RunInDir(ctx, repoRoot, "docker", "build", "-t", image, "-f", df, ".")
			if err != nil {
				return "", fmt.Errorf("docker build udf image: %w: %s", err, strings.TrimSpace(stderr))
			}
		}
	}
	o.progressf("loading UDF image %q into kind cluster %q", image, cfg.Cluster)
	if err := o.Cluster.KindLoadImage(ctx, cfg.Cluster, image); err != nil {
		return "", err
	}
	return image, nil
}

func (o Orchestrator) defaultGitSHA(ctx context.Context, repoRoot string) (string, error) {
	out, stderr, err := o.Commands.RunInDir(ctx, repoRoot, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w: %s", err, strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(out), nil
}

func (o Orchestrator) verifyUDFOnNodes(ctx context.Context, cfg config.Config) error {
	nodes, err := o.Cluster.KindNodeList(ctx, cfg.Cluster)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		ok, err := o.Cluster.ImagePresentOnKindNode(ctx, node, cfg.UDFImage)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("udf image %q not present on kind node %s", cfg.UDFImage, node)
		}
	}
	return nil
}
