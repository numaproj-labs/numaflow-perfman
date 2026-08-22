package runmeta

import (
	"encoding/json"
	"time"

	"numa-perfman/internal/cluster"
	"numa-perfman/internal/config"
)

// ResourceRequests mirrors persisted benchmark config resource requests.
type ResourceRequests struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// Resources is the workload resource profile recorded with a run.
type Resources struct {
	Requests ResourceRequests `json:"requests,omitempty"`
}

// DefaultBenchmarkResources matches benchmark map UDF manifest requests.
var DefaultBenchmarkResources = Resources{
	Requests: ResourceRequests{CPU: "1", Memory: "500Mi"},
}

// Snapshot captures host and cluster context at run start.
type Snapshot struct {
	Host        string    `json:"host"`
	KubeContext string    `json:"kube_context,omitempty"`
	Cluster     string    `json:"cluster"`
	CapturedAt  time.Time `json:"captured_at"`
	ToolVersion string    `json:"tool_version"`
	Resources   Resources `json:"resources"`
}

// Provider supplies environment snapshots (tests inject fakes).
type Provider interface {
	Snapshot() Snapshot
}

// StaticProvider returns a fixed snapshot.
type StaticProvider struct {
	Snap Snapshot
}

func (p StaticProvider) Snapshot() Snapshot { return p.Snap }

// FromConfig builds a snapshot from resolved global configuration.
func FromConfig(cfg config.Config, toolVersion string, resources Resources) Snapshot {
	if resources.Requests.CPU == "" && resources.Requests.Memory == "" {
		resources = DefaultBenchmarkResources
	}
	return Snapshot{
		Host:        cluster.Hostname(),
		KubeContext: cfg.Context,
		Cluster:     cfg.Cluster,
		CapturedAt:  time.Now().UTC(),
		ToolVersion: toolVersion,
		Resources:   resources,
	}
}

// JSON marshals the snapshot for runs.environment_json.
func (s Snapshot) JSON() string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
