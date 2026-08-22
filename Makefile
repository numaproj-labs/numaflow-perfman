SHELL := /bin/bash

VERSION ?= dev
CLUSTER ?= numaflow
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
UDFS_IMAGE ?= numaflow-perfman-udfs:$(GIT_SHA)
IMAGE ?= quay.io/numaproj/numaflow:v1.8.2
KIND_CONFIG ?=
KIND_NODE_IMAGE ?=
BIN := bin/perfman
LDFLAGS := -X 'numa-perfman/internal/cli.Version=$(VERSION)'

.PHONY: build vet test test-all test-integration-compile test-integration udfs-build udfs-test udfs-image docker setup doctor clean help

help:
	@echo "Targets: build, test, test-integration-compile, test-integration, setup, doctor, udfs-image (optional), docker (optional CLI image), clean"

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/perfman

vet:
	go vet ./...

test:
	go test -race -timeout=180s ./...

# Compile integration tests without running them (no kind cluster).
test-integration-compile:
	go test -tags=integration ./test/integration -run '^$'

# Opt-in E2E: requires PERFHARNESS_INTEGRATION=1 and image env vars (see docs/integration-tests.md).
test-integration: build
	./scripts/run-integration-tests.sh run

test-all: test udfs-test

udfs-build:
	cd udfs && go build -o bin/udfs .

udfs-test:
	cd udfs && go vet ./... && go test -race -timeout=120s ./...

# Build the repository UDF fixture image (setup also builds/loads this by default).
udfs-image:
	docker build -t $(UDFS_IMAGE) -f udfs/Dockerfile .

# Optional: minimal container with the host CLI binary only (not the primary install path).
docker:
	docker build -t perfman:$(VERSION) --build-arg VERSION=$(VERSION) .

setup: build
	./$(BIN) --cluster $(CLUSTER) setup --create-cluster --image $(IMAGE) \
		$(if $(filter 1 true,$(WITH_VALIDATION)),--with-validation,) \
		$(if $(CA_CERT),--ca-cert $(CA_CERT),) \
		$(if $(KIND_CONFIG),--kind-config $(KIND_CONFIG),) \
		$(if $(KIND_NODE_IMAGE),--kind-node-image $(KIND_NODE_IMAGE),)

doctor: build
	./$(BIN) doctor $(if $(filter 1 true,$(WITH_VALIDATION)),--with-validation,)

clean:
	rm -rf bin udfs/bin
	rm -f results/perfman.db results/perfman.db-wal results/perfman.db-shm
