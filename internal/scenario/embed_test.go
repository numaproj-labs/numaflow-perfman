package scenario

import (
	"io/fs"
	"strings"
	"testing"
)

func TestBenchmarkTemplatesCoverCatalog(t *testing.T) {
	templates, err := fs.Glob(benchmarkTemplates, "templates/benchmarks/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]struct{}, len(templates))
	for _, path := range templates {
		base := strings.TrimPrefix(path, "templates/benchmarks/")
		byName[base] = struct{}{}
	}

	for _, id := range ListBenchmarks() {
		base := benchmarkBaseName(id)
		manifest := base + ".yaml"
		if _, ok := byName[manifest]; !ok {
			t.Fatalf("missing benchmark template %q for scenario %q", manifest, id)
		}

		sc, err := LookupBenchmark(id)
		if err != nil {
			t.Fatal(err)
		}
		if sc.NeedsISB {
			isb := base + "_isb.yaml"
			if _, ok := byName[isb]; !ok {
				t.Fatalf("missing ISB template %q for scenario %q", isb, id)
			}
		}
	}
}
