package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyPath runs kubectl apply -f path.
func (c Client) ApplyPath(ctx context.Context, path string) error {
	args := c.kubectlBase()
	args = append(args, "apply", "-f", path)
	_, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl apply -f %q: %w: %s", path, err, strings.TrimSpace(stderr))
	}
	return nil
}

// EnsureNamespace creates namespace name when missing.
func (c Client) EnsureNamespace(ctx context.Context, name string) error {
	exists, err := c.NamespaceExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	yaml := []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + name + "\n")
	_, err = c.ApplyYAML(ctx, "", yaml)
	return err
}

// CRDExists reports whether a CRD is registered.
func (c Client) CRDExists(ctx context.Context, name string) (bool, error) {
	_, stderr, err := c.kubectl(ctx, "", "get", "crd", name)
	if err != nil {
		if strings.Contains(stderr, "NotFound") || strings.Contains(stderr, "not found") {
			return false, nil
		}
		return false, fmt.Errorf("kubectl get crd: %w: %s", err, strings.TrimSpace(stderr))
	}
	return true, nil
}

// DeploymentReady reports whether deployment has an Available condition with status True.
func (c Client) DeploymentReady(ctx context.Context, ns, name string) (bool, error) {
	out, err := c.GetResourceJSON(ctx, ns, "deployment/"+name)
	if err != nil {
		return false, err
	}
	var obj struct {
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
		Spec struct {
			Replicas int `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return false, err
	}
	want := obj.Spec.Replicas
	if want == 0 {
		want = 1
	}
	if obj.Status.ReadyReplicas >= want {
		return true, nil
	}
	for _, cnd := range obj.Status.Conditions {
		if cnd.Type == "Available" && cnd.Status == "True" {
			return true, nil
		}
	}
	return false, nil
}

// StatefulSetReady reports whether all replicas are ready.
func (c Client) StatefulSetReady(ctx context.Context, ns, name string) (bool, error) {
	out, err := c.GetResourceJSON(ctx, ns, "statefulset/"+name)
	if err != nil {
		return false, err
	}
	var obj struct {
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
			Replicas      int `json:"replicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return false, err
	}
	if obj.Status.Replicas == 0 {
		return false, nil
	}
	return obj.Status.ReadyReplicas >= obj.Status.Replicas, nil
}

// DefaultStorageClass returns the name of the default StorageClass, or empty if none.
func (c Client) DefaultStorageClass(ctx context.Context) (string, error) {
	out, stderr, err := c.kubectl(ctx, "", "get", "storageclass", "-o", "json")
	if err != nil {
		return "", fmt.Errorf("kubectl get storageclass: %w: %s", err, strings.TrimSpace(stderr))
	}
	var obj struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return "", err
	}
	for _, item := range obj.Items {
		if item.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
			item.Metadata.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			return item.Metadata.Name, nil
		}
	}
	return "", nil
}

// NodesReadyJSON returns node objects JSON.
func (c Client) NodesReadyJSON(ctx context.Context) (string, error) {
	return c.GetResourceJSON(ctx, "", "nodes")
}

// KindInstallCA copies certPath into each kind node and runs update-ca-certificates.
func (c Client) KindInstallCA(ctx context.Context, clusterName, certPath string) error {
	if certPath == "" {
		return nil
	}
	abs, err := filepath.Abs(certPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("CA certificate not found: %w", err)
	}
	nodes, err := c.KindNodeList(ctx, clusterName)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := c.copyCAIntoNode(ctx, abs, node); err != nil {
			return err
		}
	}
	_, stderr, err := c.kubectl(ctx, "", "wait", "--for=condition=Ready", "node", "--all", "--timeout=120s")
	if err != nil {
		return fmt.Errorf("wait for nodes after CA install: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (c Client) copyCAIntoNode(ctx context.Context, certPath, node string) error {
	dest := "/usr/local/share/ca-certificates/proxy-root-ca.crt"
	args := []string{"cp", certPath, node + ":" + dest}
	if _, stderr, err := c.runner().Run(ctx, "docker", args...); err != nil {
		return fmt.Errorf("docker cp CA to %s: %w: %s", node, err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := c.runner().Run(ctx, "docker", "exec", node, "update-ca-certificates"); err != nil {
		return fmt.Errorf("update-ca-certificates on %s: %w: %s", node, err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := c.runner().Run(ctx, "docker", "exec", node, "systemctl", "restart", "containerd"); err != nil {
		return fmt.Errorf("restart containerd on %s: %w: %s", node, err, strings.TrimSpace(stderr))
	}
	return nil
}

// DeploymentReplicas returns desired replicas for a deployment.
func (c Client) DeploymentReplicas(ctx context.Context, ns, name string) (int, error) {
	out, err := c.GetResourceJSON(ctx, ns, "deployment/"+name)
	if err != nil {
		return 0, err
	}
	var obj struct {
		Spec struct {
			Replicas int `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return 0, err
	}
	return obj.Spec.Replicas, nil
}

// RunWithDir executes a command with working directory (used for docker build).
func RunWithDir(ctx context.Context, r Runner, dir, name string, args ...string) (stdout, stderr string, err error) {
	if r == nil {
		r = DefaultRunner
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}
