package host

import (
	"os"
	"strings"
)

// CgroupReport holds host cgroup diagnostics relevant to static CPU Manager.
type CgroupReport struct {
	FilesystemType string
	HasCPUSet      bool
}

// CheckCgroup inspects the host cgroup mount for v2 and cpuset support.
func CheckCgroup() (CgroupReport, error) {
	var report CgroupReport
	data, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return report, err
	}
	report.HasCPUSet = strings.Contains(string(data), "cpuset")
	if st, err := os.Stat("/sys/fs/cgroup"); err == nil {
		// Best-effort: cgroup v2 unified hierarchy is a single mount.
		_ = st
	}
	if data, err := readCgroupFilesystemType(); err == nil {
		report.FilesystemType = data
	}
	return report, nil
}

// Ready reports whether the host cgroup setup supports static CPU Manager.
func (r CgroupReport) Ready() bool {
	return r.FilesystemType == "cgroup2fs" && r.HasCPUSet
}

func readCgroupFilesystemType() (string, error) {
	// stat -fc %T /sys/fs/cgroup equivalent via reading mountinfo is heavy;
	// use the sysfs magic check documented for cgroup v2 hosts.
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	content := string(buf[:n])
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, " /sys/fs/cgroup ") && strings.Contains(line, " cgroup2 ") {
			return "cgroup2fs", nil
		}
	}
	// Fallback when mountinfo layout differs but unified cgroup controllers exist.
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		if _, err := os.Stat("/sys/fs/cgroup/cgroup.subtree_control"); err == nil {
			return "cgroup2fs", nil
		}
	}
	return "unknown", nil
}
