package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"numa-perfman/internal/host"
	"numa-perfman/internal/kindconfig"
)

func (ex Examiner) checkKindVersion(ctx context.Context) []Check {
	runner := ex.Runner
	if runner == nil {
		return []Check{skip("kind.version", "kind version", "runner not configured")}
	}
	out, _, err := runner.Run(ctx, "kind", "version")
	if err != nil {
		return []Check{warn("kind.version", "kind version", err.Error())}
	}
	version, err := kindconfig.ParseKindVersion(out)
	if err != nil {
		return []Check{warn("kind.version", "kind version", err.Error())}
	}
	if !kindconfig.VersionAtLeast(version, kindconfig.MinKindVersion) {
		return []Check{warn("kind.version", "kind version",
			fmt.Sprintf("kind %s is older than recommended %s", version, kindconfig.MinKindVersion))}
	}
	return []Check{pass("kind.version", "kind version", version)}
}

func (ex Examiner) checkHostCgroup(ctx context.Context) []Check {
	_ = ctx
	report, err := host.CheckCgroup()
	if err != nil {
		return []Check{warn("host.cgroup", "host cgroup v2", err.Error())}
	}
	if !report.Ready() {
		return []Check{warn("host.cgroup", "host cgroup v2",
			fmt.Sprintf("filesystem=%s cpuset=%v (see static-qos.md)", report.FilesystemType, report.HasCPUSet))}
	}
	runner := ex.Runner
	if runner != nil {
		out, _, err := runner.Run(ctx, "docker", "info", "--format", "{{.CgroupVersion}}")
		if err == nil {
			cgv := strings.TrimSpace(out)
			if cgv != "" && cgv != "2" {
				return []Check{warn("host.docker_cgroup", "docker cgroup version",
					fmt.Sprintf("Docker reports cgroup version %q; version 2 is recommended", cgv))}
			}
		}
	}
	return []Check{pass("host.cgroup", "host cgroup v2",
		fmt.Sprintf("filesystem=%s cpuset enabled", report.FilesystemType))}
}

type nodeCapacity struct {
	Name        string
	CapacityCPU float64
	AllocCPU    float64
}

func parseNodeCapacities(raw string) ([]nodeCapacity, error) {
	var obj struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Capacity struct {
					CPU string `json:"cpu"`
				} `json:"capacity"`
				Allocatable struct {
					CPU string `json:"cpu"`
				} `json:"allocatable"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, err
	}
	var nodes []nodeCapacity
	for _, item := range obj.Items {
		capCPU, err := parseCPUQuantity(item.Status.Capacity.CPU)
		if err != nil {
			return nil, err
		}
		allocCPU, err := parseCPUQuantity(item.Status.Allocatable.CPU)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, nodeCapacity{
			Name:        item.Metadata.Name,
			CapacityCPU: capCPU,
			AllocCPU:    allocCPU,
		})
	}
	return nodes, nil
}

func (ex Examiner) checkStaticCPUReservation(ctx context.Context) []Check {
	raw, err := ex.Inspector.NodesReadyJSON(ctx)
	if err != nil {
		return []Check{fail("cpu.static.allocatable", "static CPU reservation", err.Error(), true)}
	}
	nodes, err := parseNodeCapacities(raw)
	if err != nil {
		return []Check{fail("cpu.static.allocatable", "static CPU reservation", err.Error(), true)}
	}
	if len(nodes) == 0 {
		return []Check{fail("cpu.static.allocatable", "static CPU reservation", "no nodes", true)}
	}
	reserved := float64(kindconfig.ReservedCPUs)
	var out []Check
	for _, node := range nodes {
		want := node.CapacityCPU - reserved
		if node.AllocCPU+0.001 < want || node.AllocCPU-0.001 > want {
			out = append(out, fail("cpu.static.allocatable", "static CPU reservation",
				fmt.Sprintf("node %q allocatable %.0f CPU but capacity %.0f expects allocatable %.0f (recreate kind cluster with deploy/kind/cluster.yaml)",
					node.Name, node.AllocCPU, node.CapacityCPU, want), true))
			continue
		}
		out = append(out, pass("cpu.static.allocatable", "static CPU reservation",
			fmt.Sprintf("%s capacity=%.0f allocatable=%.0f", node.Name, node.CapacityCPU, node.AllocCPU)))
	}
	return out
}

func (ex Examiner) checkKubeletStaticCPU(ctx context.Context, clusterName string) []Check {
	nodes, err := ex.Inspector.KindNodeList(ctx, clusterName)
	if err != nil {
		return []Check{fail("cpu.static.kubelet", "kubelet static CPU config", err.Error(), true)}
	}
	const kubeletConfig = "/var/lib/kubelet/config.yaml"
	var out []Check
	for _, node := range nodes {
		raw, err := ex.Inspector.KindNodeReadFile(ctx, node, kubeletConfig)
		if err != nil {
			out = append(out, fail("cpu.static.kubelet", "kubelet static CPU config", err.Error(), true))
			continue
		}
		missing := staticKubeletMissingFields(raw)
		if len(missing) > 0 {
			out = append(out, fail("cpu.static.kubelet", "kubelet static CPU config",
				fmt.Sprintf("node %s missing %s (recreate kind cluster)", node, strings.Join(missing, ", ")), true))
			continue
		}
		out = append(out, pass("cpu.static.kubelet", "kubelet static CPU config", node+" configured"))
	}
	return out
}

func staticKubeletMissingFields(config string) []string {
	checks := []struct {
		label   string
		matches []string
	}{
		{"cpuManagerPolicy: static", []string{"cpuManagerPolicy: static"}},
		{"reservedSystemCPUs", []string{"reservedSystemCPUs: \"0\"", "reservedSystemCPUs: '0'", "reservedSystemCPUs: 0"}},
		{"kubeReserved.cpu", []string{"cpu: \"1\"", "cpu: '1'", "cpu: 1"}},
		{"strict-cpu-reservation", []string{"strict-cpu-reservation: \"true\"", "strict-cpu-reservation: 'true'", "strict-cpu-reservation: true"}},
	}
	var missing []string
	for _, check := range checks {
		found := false
		for _, match := range check.matches {
			if strings.Contains(config, match) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, check.label)
		}
	}
	return missing
}

// StaticKubeletMissingFieldsForTest exposes kubelet field validation for unit tests.
func StaticKubeletMissingFieldsForTest(config string) []string {
	return staticKubeletMissingFields(config)
}
