package namespace

import (
	"fmt"
	"strings"
	"unicode"

	"numa-perfman/internal/runmeta"
	"numa-perfman/internal/scenario"
)

const (
	// PrefixBenchmark is the namespace prefix for benchmark runs (perf-<scenario>-<tag>).
	PrefixBenchmark = "perf-"
	// PrefixBenchmarkLegacy is the retired random run-id namespace prefix.
	PrefixBenchmarkLegacy = "numaflow-perf-"

	maxNamespaceLen = 63
)

// BenchmarkKey identifies a benchmark namespace and lock file from scenario and image reference.
type BenchmarkKey struct {
	Scenario     string
	ImageTag     string // raw tag segment before sanitization
	ImageTagSafe string // sanitized tag segment
	Namespace    string
	LockFileName string // e.g. perf-single-map-v1.8.0.lock
}

// ParseBenchmarkKey derives namespace and lock identity for a benchmark scenario and image reference.
func ParseBenchmarkKey(scenarioID, imageRef string) (BenchmarkKey, error) {
	if _, err := scenario.LookupBenchmark(scenarioID); err != nil {
		return BenchmarkKey{}, fmt.Errorf("benchmark scenario %q: %w", scenarioID, err)
	}
	tag := runmeta.ImageTagSegment(imageRef)
	if tag == "" {
		return BenchmarkKey{}, fmt.Errorf("image reference %q has no tag or digest", imageRef)
	}
	scenarioSafe := sanitizeDNS1123(scenarioID)
	tagSafe := sanitizeDNS1123(tag)
	if scenarioSafe == "" || tagSafe == "" {
		return BenchmarkKey{}, fmt.Errorf("scenario %q and image %q produce empty namespace segments", scenarioID, imageRef)
	}
	ns := fitNamespace(PrefixBenchmark, PrefixBenchmark+scenarioSafe+"-"+tagSafe)
	lockName := PrefixBenchmark + scenarioSafe + "-" + tagSafe + ".lock"
	return BenchmarkKey{
		Scenario:     scenarioID,
		ImageTag:     tag,
		ImageTagSafe: tagSafe,
		Namespace:    ns,
		LockFileName: lockName,
	}, nil
}

func sanitizeDNS1123(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

// fitNamespace keeps prefix and tag suffix when truncating to DNS-1123 length.
func fitNamespace(prefix, name string) string {
	if len(name) <= maxNamespaceLen {
		return name
	}
	if !strings.HasPrefix(name, prefix) {
		return name[:maxNamespaceLen]
	}
	rest := name[len(prefix):]
	dash := strings.LastIndexByte(rest, '-')
	if dash <= 0 || dash >= len(rest)-1 {
		return name[:maxNamespaceLen]
	}
	scenarioPart := rest[:dash]
	tagPart := rest[dash+1:]
	allowedScenario := maxNamespaceLen - len(prefix) - len(tagPart) - 1
	if allowedScenario < 1 {
		return prefix + tagPart[:maxNamespaceLen-len(prefix)]
	}
	if len(scenarioPart) > allowedScenario {
		scenarioPart = scenarioPart[:allowedScenario]
		scenarioPart = strings.TrimRight(scenarioPart, "-")
	}
	if scenarioPart == "" {
		return prefix + tagPart
	}
	return prefix + scenarioPart + "-" + tagPart
}
