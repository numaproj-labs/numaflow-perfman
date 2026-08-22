package cli

import (
	"fmt"
	"io"
)

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, `perfman — synchronous Numaflow benchmark and validation CLI

Usage:
  perfman [global flags] <command> [command flags]

Global flags (must appear before the command name):
  --context                Kubernetes context
  --cluster                kind cluster name (default numaflow)
  --results-db             SQLite results database
  --prometheus-url         Prometheus URL (auto port-forward when empty)
  --central-namespace      Central Numaflow namespace
  --monitoring-namespace   Prometheus namespace
  --validation-namespace   Validation Postgres namespace
  --log-format             text or json
  --verbose                Verbose logging
  --udf-image              Repository UDF image reference

Commands:
  setup                    Install long-lived cluster components
  config show|set          Inspect or update stored configuration
  doctor                   Read-only preflight checks
  benchmark list|run|report|compare
  validation list|run
  runs list|show|delete|artifacts
  reports list|show
  serve                    Browse reports and run benchmarks in a local web UI
  cleanup                  Remove stale run namespaces and locks
  version                  Print version
  help                     Show this help

Environment variables override stored config defaults using prefix PERFHARNESS_
(for example PERFHARNESS_CLUSTER). Command-line global flags override both.

Example:
  perfman --cluster numaflow benchmark run --scenario single-map --image quay.io/numaproj/numaflow:v1.8.0`)
}

func printBenchmarkUsage(w io.Writer) {
	fmt.Fprintln(w, `benchmark list
benchmark run --scenario ID --image REF [options]
benchmark report --scenario ID (--run-id UUID | --image REF | --image-digest DIGEST) [options]
benchmark compare --scenario ID --baseline REF --candidate REF [options]

run options:
  --scenario           required benchmark scenario ID
  --image              required complete OCI image reference (not a bare tag)
  --duration           default 15m
  --yes                overwrite an existing scenario/image result without prompting

report options:
  --run-id                     completed benchmark run UUID
  --image / --image-digest
  --after / --before            RFC3339 time bounds for selector-based lookup

compare options:
  --baseline / --candidate     image references
  --baseline-digest / --candidate-digest
  --run-id                     explicit UUIDs classified by side selectors
  --baseline-run-id / --candidate-run-id  explicit UUIDs assigned to each side
  --after / --before             RFC3339 time bounds
  --center                     mean or median (default median)
  --percentile-band            lower,upper percentiles (default 25,75)

Stored reports are written to the results database; use reports show to render them.`)
}

func printValidationUsage(w io.Writer) {
	fmt.Fprintln(w, `validation list
validation run --scenario ID --image REF [options]

  --scenario           required validation scenario ID
  --image              required complete OCI image reference
  --events             event count (tier default when unset)
  --tier               smoke|standard|soak
  --seed               deterministic seed (generated and printed when omitted)
  --base-event-time    RFC3339 base event time
  --timeout            total timeout (default 90m)
  --dump-failure-db    pg_dump failed scenario database to SQLite artifacts`)
}

func printRunsUsage(w io.Writer) {
	fmt.Fprintln(w, `runs list [--kind benchmark|validation] [--scenario] [--status] [--limit N]
runs show --id RUN_ID
runs delete --id RUN_ID
runs artifacts --id RUN_ID [--kind KIND --name NAME]`)
}

func printReportsUsage(w io.Writer) {
	fmt.Fprintln(w, `reports list [--kind comparison|single] [--scenario] [--limit N]
reports show --id REPORT_ID [--format json|html]`)
}

func printConfigUsage(w io.Writer) {
	fmt.Fprintln(w, `config show
config set --key KEY --value VALUE

Configuration is stored in harness_config inside the results database.
results_db is bootstrap-only; select it with --results-db or PERFHARNESS_RESULTS_DB.`)
}

func printServeUsage(w io.Writer) {
	fmt.Fprintln(w, `serve [options]

  Browse completed benchmark runs, start new benchmarks, and compare reports
  in a local web UI. Also port-forwards the central Numaflow UI to
  https://127.0.0.1:8443 while the server is running.

options:
  --port               HTTP port (default 8080)
  --listen             bind address (default 127.0.0.1)

Keep --listen on loopback unless you intentionally expose cluster mutation
endpoints. UI runs use the same engine as 'benchmark run'.`)
}

func printCleanupUsage(w io.Writer) {
	fmt.Fprintln(w, `cleanup [--retained-only] [--remove-stale-lock]

  --retained-only      only delete retained failure namespaces (controller already scaled to zero)
  --remove-stale-lock  remove abandoned database lock when PID is gone and namespace is inactive`)
}

func printSetupUsage(w io.Writer) {
	fmt.Fprintln(w, `setup --image REF [options]

  --create-cluster     create kind cluster when missing
  --image              central Numaflow server image (required)
  --kind-config        kind cluster config path (default deploy/kind/cluster.yaml)
  --kind-node-image    kindest/node image (default pinned v1.36.1 digest)
  --udf-image          UDF image tag to build/load
  --with-validation    install validation Postgres
  --ca-cert            corporate CA certificate path for kind nodes
  --crds               Numaflow CRD manifest path

Harness configuration is persisted in the results database.`)
}
