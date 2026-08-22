# Numaflow performance harness

A CLI for easily running Numaflow benchmarks and data validation. Runs on a single EC2 instance (or laptop) with a local kind cluster.

- The benchmarks or data validation runs can be triggered from CLI parameters or using the builtin web UI (`perfman serve`). It also port forwards the Numaflow web ui.
- The webui can be used to view the throughput, latency and resource usage graphs or compare these metrics with another version of the Numaflow controller.
- The pipeline templates can be found in `internal/scenario/templates/benchmarks/`
- Each benchmark run creates a new namespace (pipeline_name, controller_version combination) since there can only be on Numaflow controller in a namespace.
- One process uses your current `kubectl` context and stores all durable state in a single **SQLite** file at `./results/perfman.db`.There is no in-cluster orchestrator.

The `kind` cluster is spec in `deploy/kind/cluster.yaml` enables static CPU Manager with CPU 0 reserved for the system, `kubeReserved.cpu: "1"` so scheduler allocatable matches exclusive CPUs, and `strict-cpu-reservation`.

## Prerequisites

| Tool              | Purpose                                                            |
| ----------------- | ------------------------------------------------------------------ |
| Docker            | Build/load UDF images; optional Numaflow image build               |
| kind **v0.32.0+** | Local Kubernetes cluster (Kubernetes 1.36+ for static CPU Manager) |
| kubectl           | Cluster operations (matches kind kubeconfig)                       |
| git               | Deterministic UDF tags from commit SHA                             |
| tmux              | Long-running benchmarks and validation in SSH sessions             |
| Go 1.27+          | Build `perfman` (or use `make docker` optionally)              |

## Quick start

The `make setup` creates a `kind` cluster named `numaflow` and installs the necessary CRDs.

```bash
kind delete cluster --name numaflow
make setup
```

The `perfman` binary will be in `bin/` directory. This also builds the UDF image needed for running the tests and imports it into the `kind` cluster.
After setup, harness configuration (cluster name, UDF image reference, namespaces, and related defaults) is persisted in the `harness_config` table inside `./results/perfman.db`.

Inspect or update persisted values without creating a config file:

```bash
./bin/perfman config show
```

Open the web UI:

```bash
./bin/perfman serve
```

## Cleanup

For a complete fresh start:

```sh
rm -rf results/
kind delete cluster --name numaflow
```

### Running tests with local Numaflow images

To test your locally built numaflow image, you should import the image into the kind cluster named `numaflow`. Image pull policy for Numaflow workloads is `IfNotPresent`.

```bash
# Example: build Numaflow elsewhere, tag for kind
docker tag my-numaflow:built quay.io/numaproj/numaflow:local-test
kind load docker-image quay.io/numaproj/numaflow:local-test --name numaflow

perfman --cluster numaflow benchmark run \
  --scenario single-map --image quay.io/numaproj/numaflow:local-test
```

### External registry and imagePullSecrets

When the image is not loaded locally, kind nodes pull from the registry if credentials and network allow it.

1. Create a pull secret in the **temporary run namespace** is handled by the CLI for controller/data-plane pulls where needed; for cluster-wide pulls you can pre-create secrets in `numaflow-system` or configure your registry on the host Docker/`containerd` inside kind nodes.
2. Standard pattern on kind for private registries:

```bash
kubectl create secret docker-registry regcred \
  --docker-server=registry.example.com \
  --docker-username=… --docker-password=… \
  -n numaflow-system
# Link secret to default service account or follow your org’s kind + registry docs.
```

## UDF fixture images

All pipeline/mono UDFs are built from this repository (`udfs/`), tagged deterministically as:

```text
numaflow-perfman-udfs:<git-short-sha>
```

`setup` builds, tags with `git rev-parse --short HEAD`, loads into kind, and verifies presence on every node. UDF pull policy is **Never**.
