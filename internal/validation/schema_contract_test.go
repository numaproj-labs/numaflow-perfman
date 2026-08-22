// Package validation_test includes structural SQL contract tests that do not require Docker.
// Live PostgreSQL JSONB DDL, upsert, and validator JOIN behavior are covered when
// PERFHARNESS_INTEGRATION=1 and PERFHARNESS_INTEGRATION_VALIDATION=1 (see test/integration).
package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"numa-perfman/internal/oracle"
	"numa-perfman/internal/validation"
)

func TestSchemaDDLTableContract(t *testing.T) {
	t.Parallel()
	scenarios := []oracle.Scenario{
		oracle.ScenarioMap,
		oracle.ScenarioReduce,
		oracle.ScenarioSlidingReduce,
		oracle.ScenarioMonoVertex,
	}
	for _, s := range scenarios {
		t.Run(string(s), func(t *testing.T) {
			ddl := validation.SchemaDDLForTest(s)
			issues := validation.ContractIssues(s, ddl)
			for _, iss := range issues {
				if strings.HasPrefix(iss, "note:") {
					continue
				}
				t.Error(iss)
			}
		})
	}
}

func TestUDFSourceQueriesReferenceContractColumns(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join("..", "..")
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	mapQuery := read("udfs/source/adevent.go")
	for _, col := range []string{"event_id", "user_id", "page_id", "ad_type", "event_type", "event_time", "ip_address", "route_tag"} {
		if !strings.Contains(mapQuery, col) {
			t.Fatalf("map source query missing %q", col)
		}
	}
	reduceQuery := read("udfs/source/reduce.go")
	for _, col := range []string{"event_id", "reduce_key", "category", "amount", "event_time"} {
		if !strings.Contains(reduceQuery, col) {
			t.Fatalf("reduce source query missing %q", col)
		}
	}
	sinkSQL := read("udfs/sink/postgres.go")
	for _, col := range []string{"dedup_key", "event_id", "payload", "processed_by", "child_index", "total_children"} {
		if !strings.Contains(sinkSQL, col) {
			t.Fatalf("sink upsert missing %q", col)
		}
	}
}

func TestValidatorSQLReferencesExpectedTables(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(repoRoot, "udfs/validator/validator.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, table := range []string{"source_events", "expected_sink_events", "sink_events"} {
		if !strings.Contains(body, table) {
			t.Fatalf("validator missing table %q", table)
		}
	}
	if !regexp.MustCompile(`dedup_key`).MatchString(body) {
		t.Fatal("validator must join on dedup_key")
	}
}
