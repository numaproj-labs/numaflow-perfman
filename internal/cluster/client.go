package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes external commands. Production uses execRunner; tests inject fakes.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

// Process is a started command (e.g. port-forward).
type Process interface {
	Wait() error
	Kill() error
	PID() int
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) Wait() error { return p.cmd.Wait() }
func (p *execProcess) Kill() error { return p.cmd.Process.Kill() }
func (p *execProcess) PID() int    { return p.cmd.Process.Pid }

func (execRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}

// DefaultRunner is the production command runner.
var DefaultRunner Runner = execRunner{}

// Client wraps kubectl and kind invocations with explicit context and namespace.
type Client struct {
	Runner  Runner
	Context string
}

func (c Client) runner() Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return DefaultRunner
}

func (c Client) kubectlBase() []string {
	args := []string{}
	if c.Context != "" {
		args = append(args, "--context", c.Context)
	}
	return args
}

func (c Client) kubectl(ctx context.Context, ns string, args ...string) (string, string, error) {
	base := c.kubectlBase()
	if ns != "" {
		base = append(base, "-n", ns)
	}
	base = append(base, args...)
	return c.runner().Run(ctx, "kubectl", base...)
}

// CheckTool verifies an executable is on PATH.
func CheckTool(ctx context.Context, r Runner, name string) error {
	if r == nil {
		r = DefaultRunner
	}
	_, _, err := r.Run(ctx, name, "--help")
	if err != nil {
		return fmt.Errorf("%s not available: %w", name, err)
	}
	return nil
}

// CurrentContext returns the active kubeconfig context name.
func (c Client) CurrentContext(ctx context.Context) (string, error) {
	out, stderr, err := c.runner().Run(ctx, "kubectl", "config", "current-context")
	if err != nil {
		return "", fmt.Errorf("current context: %w: %s", err, strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(out), nil
}

// KindClusterExists reports whether the named kind cluster is present.
func (c Client) KindClusterExists(ctx context.Context, cluster string) (bool, error) {
	out, stderr, err := c.runner().Run(ctx, "kind", "get", "clusters")
	if err != nil {
		return false, fmt.Errorf("kind get clusters: %w: %s", err, strings.TrimSpace(stderr))
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == cluster {
			return true, nil
		}
	}
	return false, nil
}

// KindCreateCluster creates a kind cluster by name.
func (c Client) KindCreateCluster(ctx context.Context, cluster string, extraArgs ...string) error {
	args := append([]string{"create", "cluster", "--name", cluster}, extraArgs...)
	_, stderr, err := c.runner().Run(ctx, "kind", args...)
	if err != nil {
		return fmt.Errorf("kind create cluster: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

// KindVersion returns the installed kind release string (for example "0.32.0").
func (c Client) KindVersion(ctx context.Context) (string, error) {
	out, stderr, err := c.runner().Run(ctx, "kind", "version")
	if err != nil {
		return "", fmt.Errorf("kind version: %w: %s", err, strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(out), nil
}

// KindNodeReadFile reads a file from inside a kind node container.
func (c Client) KindNodeReadFile(ctx context.Context, nodeContainer, path string) (string, error) {
	out, stderr, err := c.runner().Run(ctx, "docker", "exec", nodeContainer, "cat", path)
	if err != nil {
		return "", fmt.Errorf("read %s on node %s: %w: %s", path, nodeContainer, err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// KindLoadImage loads a local image reference into the kind cluster nodes.
func (c Client) KindLoadImage(ctx context.Context, cluster, image string) error {
	args := []string{"load", "docker-image", image, "--name", cluster}
	_, stderr, err := c.runner().Run(ctx, "kind", args...)
	if err != nil {
		return fmt.Errorf("kind load docker-image: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

// KindNodeList returns docker container names for kind nodes in the cluster.
func (c Client) KindNodeList(ctx context.Context, cluster string) ([]string, error) {
	out, stderr, err := c.runner().Run(ctx, "kind", "get", "nodes", "--name", cluster)
	if err != nil {
		return nil, fmt.Errorf("kind get nodes: %w: %s", err, strings.TrimSpace(stderr))
	}
	var nodes []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			nodes = append(nodes, line)
		}
	}
	return nodes, nil
}

// ImageDigestOnKindNode returns an immutable image ID or digest for imageRef when present on the node.
func (c Client) ImageDigestOnKindNode(ctx context.Context, nodeContainer, imageRef string) (string, error) {
	if imageRef == "" {
		return "", nil
	}
	if at := strings.LastIndex(imageRef, "@sha256:"); at >= 0 {
		return imageRef[at+1:], nil
	}
	out, stderr, err := c.runner().Run(ctx, "docker", "exec", nodeContainer, "crictl", "inspecti", imageRef)
	if err != nil {
		return "", fmt.Errorf("inspect image on node %s: %w: %s", nodeContainer, err, strings.TrimSpace(stderr))
	}
	// Prefer status.id (sha256:...) from crictl inspecti JSON.
	if idx := strings.Index(out, `"id":`); idx >= 0 {
		rest := out[idx+5:]
		start := strings.Index(rest, `"`)
		if start >= 0 {
			rest = rest[start+1:]
			end := strings.Index(rest, `"`)
			if end > 0 {
				id := rest[:end]
				if strings.HasPrefix(id, "sha256:") {
					return id, nil
				}
			}
		}
	}
	return "", nil
}

// ImagePresentOnKindNode checks docker images on a kind node container for the reference.
func (c Client) ImagePresentOnKindNode(ctx context.Context, nodeContainer, imageRef string) (bool, error) {
	out, stderr, err := c.runner().Run(ctx, "docker", "exec", nodeContainer, "crictl", "images", "-o", "json")
	if err != nil {
		// fallback to docker inspect inside node
		out, stderr, err = c.runner().Run(ctx, "docker", "exec", nodeContainer, "ctr", "-n=k8s.io", "images", "ls", "-q")
		if err != nil {
			return false, fmt.Errorf("list images on node: %w: %s", err, strings.TrimSpace(stderr))
		}
		return strings.Contains(out, imageRef), nil
	}
	return strings.Contains(out, imageRef), nil
}

// ApplyYAML applies manifest bytes to the cluster; cluster-scoped when ns is empty.
func (c Client) ApplyYAML(ctx context.Context, ns string, yaml []byte) (string, error) {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "apply", "-f", "-")
	return c.runWithStdin(ctx, "kubectl", yaml, args...)
}

// DeleteYAML deletes resources described by manifest bytes.
func (c Client) DeleteYAML(ctx context.Context, ns string, yaml []byte, ignoreNotFound bool) (string, error) {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "delete", "-f", "-")
	if ignoreNotFound {
		args = append(args, "--ignore-not-found=true")
	}
	return c.runWithStdin(ctx, "kubectl", yaml, args...)
}

// DeleteResource deletes a named resource in the given namespace.
func (c Client) DeleteResource(ctx context.Context, ns, resource string, ignoreNotFound bool) error {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "delete", resource)
	if ignoreNotFound {
		args = append(args, "--ignore-not-found=true")
	}
	_, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl delete %s: %w: %s", resource, err, strings.TrimSpace(stderr))
	}
	return nil
}

// ClearResourceFinalizers removes metadata finalizers from a named resource.
func (c Client) ClearResourceFinalizers(ctx context.Context, ns, resource string, ignoreNotFound bool) error {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "patch", resource, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
	_, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		message := strings.ToLower(stderr)
		if ignoreNotFound && (strings.Contains(message, "not found") ||
			strings.Contains(message, "no matching resources") ||
			strings.Contains(message, "the server doesn't have a resource type")) {
			return nil
		}
		return fmt.Errorf("kubectl patch %s finalizers: %w: %s", resource, err, strings.TrimSpace(stderr))
	}
	return nil
}

func (c Client) runWithStdin(ctx context.Context, name string, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errBuf.String()))
	}
	return outBuf.String(), nil
}

// WaitForCondition runs kubectl wait with the given condition expression.
func (c Client) WaitForCondition(ctx context.Context, ns, resource, condition string, timeout time.Duration) error {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "wait", resource, "--for="+condition, fmt.Sprintf("--timeout=%s", timeout))
	_, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl wait: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

// GetResourceJSON fetches a resource as JSON.
func (c Client) GetResourceJSON(ctx context.Context, ns, resource string) (string, error) {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "get", resource, "-o", "json")
	out, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return "", fmt.Errorf("kubectl get: %w: %s", err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// Logs returns pod logs (single pod name or selector via kubectl syntax).
func (c Client) Logs(ctx context.Context, ns, pod string, tailLines int, container string) (string, error) {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "logs", pod)
	if container != "" {
		args = append(args, "-c", container)
	}
	if tailLines > 0 {
		args = append(args, fmt.Sprintf("--tail=%d", tailLines))
	}
	out, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return "", fmt.Errorf("kubectl logs: %w: %s", err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// ListEvents returns namespace events as plain text.
func (c Client) ListEvents(ctx context.Context, ns string) (string, error) {
	out, stderr, err := c.kubectl(ctx, ns, "get", "events", "--sort-by=.lastTimestamp")
	if err != nil {
		return "", fmt.Errorf("kubectl get events: %w: %s", err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// ListPods returns pod list text in namespace.
func (c Client) ListPods(ctx context.Context, ns string) (string, error) {
	out, stderr, err := c.kubectl(ctx, ns, "get", "pods", "-o", "wide")
	if err != nil {
		return "", fmt.Errorf("kubectl get pods: %w: %s", err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// ScaleDeployment sets deployment replicas in namespace.
func (c Client) ScaleDeployment(ctx context.Context, ns, name string, replicas int) error {
	_, stderr, err := c.kubectl(ctx, ns, "scale", "deployment", name, fmt.Sprintf("--replicas=%d", replicas))
	if err != nil {
		return fmt.Errorf("kubectl scale: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

// NamespaceExists checks whether a namespace exists.
func (c Client) NamespaceExists(ctx context.Context, name string) (bool, error) {
	_, stderr, err := c.kubectl(ctx, "", "get", "namespace", name)
	if err != nil {
		if strings.Contains(stderr, "NotFound") || strings.Contains(stderr, "not found") {
			return false, nil
		}
		return false, fmt.Errorf("kubectl get namespace: %w: %s", err, strings.TrimSpace(stderr))
	}
	return true, nil
}

// ListNamespacesByLabel lists namespace names matching label selector.
func (c Client) ListNamespacesByLabel(ctx context.Context, labelSelector string) ([]string, error) {
	out, stderr, err := c.kubectl(ctx, "", "get", "namespaces", "-l", labelSelector, "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return nil, fmt.Errorf("kubectl get namespaces: %w: %s", err, strings.TrimSpace(stderr))
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Fields(out), nil
}

// PortForward starts kubectl port-forward in the background.
func (c Client) PortForward(ctx context.Context, ns, resource, localPort, remotePort string) (Process, error) {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "port-forward", resource, localPort+":"+remotePort)
	return c.runner().Start(ctx, "kubectl", args...)
}

// Hostname returns the local hostname for lock metadata.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// KubectlArgsForTest exposes argument construction for unit tests.
func (c Client) KubectlArgsForTest(ns string, subcommand ...string) []string {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	return append(args, subcommand...)
}
