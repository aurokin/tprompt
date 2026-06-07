GOBIN := $(shell go env GOPATH)/bin

GOLANGCI_LINT_VERSION := v2.1.6
GOFUMPT_VERSION       := v0.7.0
GOIMPORTS_VERSION     := v0.26.0

PKG_LIST := ./...

.PHONY: all
all: check test-scripts build

VERSION := $(shell cat VERSION 2>/dev/null || echo dev)
LDFLAGS := -X github.com/aurokin/tprompt/internal/app.appVersion=$(VERSION)

.PHONY: build
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/tprompt ./cmd/tprompt

.PHONY: tools
tools:
	GOBIN=$(GOBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(GOBIN) go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	GOBIN=$(GOBIN) go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

.PHONY: fmt
fmt:
	$(GOBIN)/golangci-lint fmt

.PHONY: fmt-check
fmt-check:
	$(GOBIN)/golangci-lint fmt --diff

.PHONY: lint
lint:
	$(GOBIN)/golangci-lint run $(PKG_LIST)

.PHONY: test
test:
	go test -race -covermode=atomic -coverprofile=coverage.txt $(PKG_LIST)

# test-scripts runs the bash test harness for the macOS signing/notary
# helpers under scripts/. Kept separate from the Go-only `check` gate.
.PHONY: test-scripts
test-scripts:
	bash scripts/test/run.sh

.PHONY: check
check: fmt-check lint test

.PHONY: clean
clean:
	rm -rf bin coverage.txt
