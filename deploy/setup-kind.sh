#!/usr/bin/env bash
# Thin bootstrap wrapper: build the host CLI and run cluster setup.
# Does not deploy an in-cluster perfman orchestrator.
#
# Environment:
#   IMAGE             Central Numaflow server image (default: quay.io/numaproj/numaflow:v1.8.2)
#   CLUSTER           kind cluster name (default: numaflow)
#   WITH_VALIDATION   Set to 1 or true to install validation Postgres
#   CA_CERT           Optional corporate proxy CA PEM for kind nodes (never copied into Docker builds)
#   KIND_CONFIG       Optional kind cluster config path
#   KIND_NODE_IMAGE   Optional kindest/node image reference
#
# Example:
#   ./deploy/setup-kind.sh
#   IMAGE=quay.io/numaproj/numaflow:v1.8.2 ./deploy/setup-kind.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

IMAGE="${IMAGE:-${PERFHARNESS_IMAGE:-quay.io/numaproj/numaflow:v1.8.2}}"
CLUSTER="${CLUSTER:-numaflow}"
WITH_VALIDATION="${WITH_VALIDATION:-}"
CA_CERT="${CA_CERT:-}"
KIND_CONFIG="${KIND_CONFIG:-}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-}"

make build

args=(--cluster "${CLUSTER}" setup --create-cluster --image "${IMAGE}")
if [[ "${WITH_VALIDATION}" == "1" || "${WITH_VALIDATION}" == "true" ]]; then
  args+=(--with-validation)
fi
if [[ -n "${CA_CERT}" ]]; then
  args+=(--ca-cert "${CA_CERT}")
fi
if [[ -n "${KIND_CONFIG}" ]]; then
  args+=(--kind-config "${KIND_CONFIG}")
fi
if [[ -n "${KIND_NODE_IMAGE}" ]]; then
  args+=(--kind-node-image "${KIND_NODE_IMAGE}")
fi

exec ./bin/perfman "${args[@]}" "$@"
