package host_test

import (
	"testing"

	"numa-perfman/internal/host"
)

func TestCheckCgroup(t *testing.T) {
	report, err := host.CheckCgroup()
	if err != nil {
		t.Skipf("cgroup not available in test environment: %v", err)
	}
	if report.FilesystemType == "" {
		t.Fatal("expected filesystem type")
	}
	t.Logf("cgroup filesystem=%s cpuset=%v ready=%v", report.FilesystemType, report.HasCPUSet, report.Ready())
}
