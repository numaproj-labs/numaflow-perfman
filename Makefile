COMMIT_SHA=$(shell git rev-parse HEAD)
TAG ?= stable
PUSH ?= false
IMAGE_REGISTRY ?= quay.io/numaio/numaproj-labs/perfman:${TAG}

# Builds only for current platform
build:
	CGO_ENABLED=0 go build -ldflags "-X github.com/numaproj-labs/numaflow-perfman/util.CommitSHA=$(COMMIT_SHA)" -v -o dist/perfman main.go

.PHONY: image-push
image-push:
	docker buildx build -t ${IMAGE_REGISTRY} --build-arg COMMIT_SHA=$(COMMIT_SHA) --platform linux/amd64,linux/arm64 --target perfman . --push

.PHONY: image
image:
	docker build -t ${IMAGE_REGISTRY} --build-arg COMMIT_SHA=$(COMMIT_SHA) --target perfman .
	@if [ "$(PUSH)" = "true" ]; then docker push ${IMAGE_REGISTRY}; fi

.PHONY: run
run:
	mkdir -p output
	docker run -it --network host \
 	-v ~/.kube/config:/perfmanuser/.kube/config:ro \
 	-v ./output:/home/perfman/output ${IMAGE_REGISTRY}

$(GOPATH)/bin/golangci-lint:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b `go env GOPATH`/bin v1.54.1

.PHONY: lint
lint: $(GOPATH)/bin/golangci-lint
	go mod tidy
	golangci-lint run --fix --verbose --concurrency 4 --timeout 5m --enable goimports

.PHONY: test
test:
	@go test ./...

.PHONY: clean
clean:
	-rm -rf dist
	-rm -rf output
