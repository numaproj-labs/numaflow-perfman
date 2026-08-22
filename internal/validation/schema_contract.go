package validation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"numa-perfman/internal/oracle"
)

// UDF contract column lists mirror SELECT/INSERT usage in udfs/source and udfs/sink.

var (
	mapSourceUDFColumns = []string{
		"event_id", "user_id", "page_id", "ad_type", "event_type", "event_time", "ip_address", "route_tag", "ack_status",
	}
	reduceSourceUDFColumns = []string{
		"event_id", "reduce_key", "category", "amount", "event_time", "ack_status",
	}
	sinkTableColumns = []string{
		"dedup_key", "event_id", "payload", "processed_by", "child_index", "total_children", "receive_count",
	}
	expectedSinkTableColumns = []string{
		"dedup_key", "event_id", "payload", "processed_by", "child_index", "total_children",
	}
	runConfigColumns = []string{"total_events", "source_completed"}
)

var createTableHeader = regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS ([a-z_]+)\s*\(`)

// TableColumns parses CREATE TABLE statements from ddl and returns column names per table.
func TableColumns(ddl string) (map[string][]string, error) {
	out := make(map[string][]string)
	lines := strings.Split(ddl, "\n")
	var current string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if m := createTableHeader.FindStringSubmatch(trim); m != nil {
			current = strings.ToLower(m[1])
			continue
		}
		if current == "" {
			continue
		}
		if strings.HasPrefix(trim, ")") || strings.HasPrefix(trim, ");") {
			current = ""
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "CREATE INDEX") {
			continue
		}
		col := strings.Fields(trim)[0]
		col = strings.Trim(col, ",")
		col = strings.ToLower(col)
		if col == "insert" || col == "into" || col == "values" {
			continue
		}
		out[current] = append(out[current], col)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tables parsed from ddl")
	}
	return out, nil
}

// ContractIssues compares parsed DDL columns to UDF/orchestrator contracts for scenario.
func ContractIssues(scenario oracle.Scenario, ddl string) []string {
	tables, err := TableColumns(ddl)
	if err != nil {
		return []string{err.Error()}
	}
	var issues []string
	require := func(table string, want []string) {
		got := tables[table]
		if len(got) == 0 {
			issues = append(issues, fmt.Sprintf("missing table %q in ddl", table))
			return
		}
		gotSet := make(map[string]struct{}, len(got))
		for _, c := range got {
			gotSet[c] = struct{}{}
		}
		for _, c := range want {
			if _, ok := gotSet[c]; !ok {
				issues = append(issues, fmt.Sprintf("table %q missing column %q (udf contract)", table, c))
			}
		}
	}
	switch scenario {
	case oracle.ScenarioReduce, oracle.ScenarioSlidingReduce:
		require("source_events", reduceSourceUDFColumns)
	default:
		require("source_events", mapSourceUDFColumns)
	}
	require("sink_events", sinkTableColumns)
	require("expected_sink_events", expectedSinkTableColumns)
	require("run_config", runConfigColumns)
	if strings.Contains(strings.ToLower(ddl), "jsonb") {
		issues = append(issues, "note: JSONB columns require live PostgreSQL contract tests (see validation/integration)")
	}
	sort.Strings(issues)
	return issues
}
