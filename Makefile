COMMIT_SHA=$(shell git rev-parse HEAD)

.PHONY: build
build:
	CGO_ENABLED=0 GOARCH=amd64 go build -ldflags "-X github.com/numaproj-labs/numaflow-perfman/util.CommitSHA=$(COMMIT_SHA)" -v -o perfman main.go

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
	-rm -f perfman
	-rm -rf output
