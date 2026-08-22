package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WaitForPodsReady waits until pods in ns match labelSelector (or all pods when selector is empty) are Ready.
func (c Client) WaitForPodsReady(ctx context.Context, ns, labelSelector string, timeout time.Duration) error {
	args := c.kubectlBase()
	if ns != "" {
		args = append(args, "-n", ns)
	}
	args = append(args, "wait", "pod", "--for=condition=Ready", fmt.Sprintf("--timeout=%s", timeout))
	if strings.TrimSpace(labelSelector) != "" {
		args = append(args, "-l", labelSelector)
	} else {
		args = append(args, "--all")
	}
	_, stderr, err := c.runner().Run(ctx, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl wait pods: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}
