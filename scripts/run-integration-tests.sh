#!/usr/bin/env bash
# Run perfman integration/E2E tests (plan §16.3–§16.4).
#
# Requires: docker, kind, kubectl, Go toolchain.
# Creates a disposable kind cluster named perfh-int-* — never the default "numaflow" cluster.
#
# When PERFHARNESS_INTEGRATION=1:
#   PERFHARNESS_IMAGE or IMAGE          Central Numaflow OCI reference (setup --image; default v1.8.2)
#   PERFHARNESS_BENCHMARK_IMAGE         Controller/data-plane image for benchmark runs
#
# Optional:
#   PERFHARNESS_BENCHMARK_IMAGE_B        Second image for compare tests / acceptance
#   PERFHARNESS_EXTERNAL_IMAGE           Registry image not pre-loaded (pull-if-absent test)
#   PERFHARNESS_INTEGRATION_VALIDATION=1 Install Postgres and run live validation subtests
#   PERFHARNESS_INTEGRATION_FULL=1       Long §16.4 acceptance suite
#
# Examples:
#   PERFHARNESS_INTEGRATION=1 \
#   PERFHARNESS_BENCHMARK_IMAGE=quay.io/numaproj/numaflow:v1.8.2 \
#     ./scripts/run-integration-tests.sh
#
# Compile integration tests only (no cluster):
#   ./scripts/run-integration-tests.sh compile
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

mode="${1:-run}"

case "${mode}" in
  compile)
    go test -tags=integration ./test/integration -run TestNonExistent -count=0
    echo "integration package compile OK"
    ;;
  run)
    export PERFHARNESS_INTEGRATION="${PERFHARNESS_INTEGRATION:-1}"
    if [[ "${PERFHARNESS_INTEGRATION}" != "1" ]]; then
      echo "PERFHARNESS_INTEGRATION must be 1 to run E2E tests" >&2
      exit 1
    fi
    make build
    go test -tags=integration -timeout=6h -count=1 ./test/integration "$@"
    ;;
  *)
    echo "usage: $0 [compile|run] [extra go test args...]" >&2
    exit 2
    ;;
esac
