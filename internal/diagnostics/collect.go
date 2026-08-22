package diagnostics

import (
	"context"
	"fmt"
	"strings"

	"numa-perfman/internal/cluster"
)

// Options bound output size for artifact collection.
type Options struct {
	LogTailLines    int
	MaxEventLines   int
	MaxPodListRunes int
}

// DefaultOptions returns sensible bounded defaults.
func DefaultOptions() Options {
	return Options{LogTailLines: 500, MaxEventLines: 200, MaxPodListRunes: 128 * 1024}
}

// Collector gathers run diagnostics through the cluster client.
type Collector struct {
	Cluster cluster.Client
	Opts    Options
}

// Bundle is written to the run artifact directory.
type Bundle struct {
	Pods          string
	Events        string
	ControllerLog string
	PipelineLog   string
}

func (c Collector) opts() Options {
	if c.Opts.LogTailLines == 0 && c.Opts.MaxEventLines == 0 {
		return DefaultOptions()
	}
	o := c.Opts
	if o.LogTailLines == 0 {
		o.LogTailLines = DefaultOptions().LogTailLines
	}
	if o.MaxEventLines == 0 {
		o.MaxEventLines = DefaultOptions().MaxEventLines
	}
	if o.MaxPodListRunes == 0 {
		o.MaxPodListRunes = DefaultOptions().MaxPodListRunes
	}
	return o
}

// Collect gathers pod list, events, controller logs, and pipeline/mono pod logs.
func (c Collector) Collect(ctx context.Context, ns string) (Bundle, error) {
	opts := c.opts()
	var bundle Bundle
	pods, err := c.Cluster.ListPods(ctx, ns)
	if err != nil {
		return bundle, err
	}
	bundle.Pods = truncateRunes(pods, opts.MaxPodListRunes)

	events, err := c.Cluster.ListEvents(ctx, ns)
	if err != nil {
		return bundle, err
	}
	bundle.Events = truncateLines(events, opts.MaxEventLines)

	controllerLog, err := c.Cluster.Logs(ctx, ns, "deployment/numaflow-controller", opts.LogTailLines, "controller-manager")
	if err != nil {
		bundle.ControllerLog = fmt.Sprintf("controller logs unavailable: %v\n", err)
	} else {
		bundle.ControllerLog = truncateRunes(controllerLog, opts.MaxPodListRunes)
	}

	pipelineLog, err := c.collectPipelineLogs(ctx, ns, opts.LogTailLines, opts.MaxPodListRunes)
	if err != nil {
		bundle.PipelineLog = fmt.Sprintf("pipeline logs unavailable: %v\n", err)
	} else {
		bundle.PipelineLog = pipelineLog
	}
	return bundle, nil
}

func (c Collector) collectPipelineLogs(ctx context.Context, ns string, tail, maxRunes int) (string, error) {
	selectors := []string{"app.kubernetes.io/component=pipeline", "app.kubernetes.io/component=mono-vertex"}
	var b strings.Builder
	for _, sel := range selectors {
		out, err := c.Cluster.Logs(ctx, ns, "pod/"+sel, tail, "")
		if err != nil {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(out)
	}
	if b.Len() == 0 {
		// fallback: any pod containing pipeline or mono in name
		pods, err := c.Cluster.ListPods(ctx, ns)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(pods, "\n") {
			if strings.Contains(line, "pipeline") || strings.Contains(line, "mono") {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				podName := fields[0]
				out, err := c.Cluster.Logs(ctx, ns, podName, tail, "")
				if err != nil {
					continue
				}
				if b.Len() > 0 {
					b.WriteString("\n---\n")
				}
				fmt.Fprintf(&b, "# pod %s\n", podName)
				b.WriteString(out)
			}
		}
	}
	return truncateRunes(b.String(), maxRunes), nil
}

func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n... truncated ...\n"
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... truncated ...\n"
}
