package kindconfig

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultConfigPath is the repository-relative Kind cluster config.
	DefaultConfigPath = "deploy/kind/cluster.yaml"

	// MinKindVersion is the minimum supported kind release (Kubernetes 1.36+).
	MinKindVersion = "0.32.0"

	// DefaultNodeImage is the pinned kindest/node image for reproducible clusters.
	DefaultNodeImage = "kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"

	// ReservedCPUs is the number of host CPUs reserved for kubelet/system via
	// reservedSystemCPUs and kubeReserved.cpu.
	ReservedCPUs = 1
)

// ParseKindVersion extracts the semver from `kind version` output.
func ParseKindVersion(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kind v") {
			version := strings.TrimPrefix(line, "kind v")
			if idx := strings.IndexByte(version, ' '); idx >= 0 {
				version = version[:idx]
			}
			if version == "" {
				break
			}
			return version, nil
		}
	}
	return "", fmt.Errorf("unable to parse kind version from %q", strings.TrimSpace(output))
}

// VersionAtLeast reports whether version is >= min (semver major.minor.patch).
func VersionAtLeast(version, min string) bool {
	v, err := parseVersionParts(version)
	if err != nil {
		return false
	}
	m, err := parseVersionParts(min)
	if err != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if v[i] > m[i] {
			return true
		}
		if v[i] < m[i] {
			return false
		}
	}
	return true
}

func parseVersionParts(version string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 1 {
		return out, fmt.Errorf("invalid version %q", version)
	}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			break
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, fmt.Errorf("invalid version %q", version)
		}
		out[i] = n
	}
	return out, nil
}
