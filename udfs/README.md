# Validation UDFs (Go)

Implementation of the data-validation UDFs

This is a **separate Go module** (`numa-perfman/udfs`) so its `numaflow-go` SDK
dependency never leaks into the orchestrator build at the repo root. Build it from
within `udfs/`.

## Design

- **Single binary, mode-flag dispatch** (mirrors the Kotlin `Main.kt`): one image, one
  entrypoint, a flag selects which Numaflow server to run.
- **Shared deterministic logic** lives in `internal/event` — the `Event` struct,
  enrichment (geo / campaign / risk), fan-out rules, dedup keys, and the reduce/monovertex
  models. This is the correctness contract: it is kept **byte/value-identical** to the
  orchestrator's data generator (`../internal/pipelines/datagen.go`,
  `monovertex_datagen.go`, `reduce_datagen.go`), which pre-computes `expected_sink_events`.
  The UDFs produce the actual `sink_events`; validation compares the two via PostgreSQL
  JSONB. `internal/event/parity_test.go` locks this contract with goldens captured from
  the generator.
- **numaflow-go is pinned** to the `main` commit `6f4297d8` (pseudo-version
  `v0.11.1-0.20260512143432-6f4297d8e5ee`) because the monovertex primary sink needs
  `sinker.ResponseOnSuccess`, which is not in the v0.11.0 release.

## Modes

| Flag                          | UDF                                       | Pipeline                |
| ----------------------------- | ----------------------------------------- | ----------------------- |
| `--source`                    | Ad event source                           | map-validations         |
| `--sourcetransformer`         | Route-tag → Numaflow tag                  | map-validations         |
| `--unarymap`                  | Unary map (geo_location)                  | map-validations         |
| `--batchmap`                  | Batch map (campaign_id)                   | map-validations         |
| `--streammap`                 | Stream map (risk_score)                   | map-validations         |
| `--sink`                      | Postgres dedup sink                       | map-validations         |
| `--validator`                 | One-shot SQL validator (exit 0/1)         | map-validations         |
| `--reduce-source`             | Reduce source (key = reduce_key)          | reduce / sliding-reduce |
| `--reduce`                    | Fixed-window reducer                      | reduce / sliding-reduce |
| `--reduce-sink`               | Reduce sink (`PROCESSED_BY` env)          | reduce / sliding-reduce |
| `--monovertex-source`         | MV source (route_tag in payload + header) | monovertex              |
| `--monovertex-transformer`    | MV transformer (fan-out by ad_type)       | monovertex              |
| `--monovertex-map`            | MV map (geo + fan-out by event_type)      | monovertex              |
| `--monovertex-sink`           | MV primary sink (onSuccess + fallback)    | monovertex              |
| `--monovertex-onsuccess-sink` | MV onSuccess sink                         | monovertex              |
| `--monovertex-fallback-sink`  | MV fallback sink                          | monovertex              |

## Configuration

Postgres connection (all sinks/sources/validator) via env, with defaults matching the
Kotlin implementation:

| Env                       | Default                                                                                   |
| ------------------------- | ----------------------------------------------------------------------------------------- |
| `POSTGRES_HOST`           | `localhost`                                                                               |
| `POSTGRES_PORT`           | `5432`                                                                                    |
| `POSTGRES_USER`           | `numaflow`                                                                                |
| `POSTGRES_PASSWORD`       | `numaflow`                                                                                |
| `POSTGRES_DB`             | `numaflow_validation`                                                                     |
| `VALIDATION_SAMPLE_LIMIT` | `100` (validator only)                                                                    |
| `PROCESSED_BY`            | `fixed-window-reduce` (reduce sink; set `sliding-window-reduce` for the sliding pipeline) |

## Build & test

From the repo root (Makefile targets):

```sh
make udfs-build    # go build -o udfs/bin/udfs
make udfs-test     # go vet ./... && go test ./...   (in udfs/)
make udfs-image    # docker build -t $(UDFS_IMAGE) udfs   (default numaflow-data-validations-go:latest)
```

Or directly inside `udfs/`:

```sh
go build -o bin/udfs .
go test ./...
```
