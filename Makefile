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

# dogfood copies the version-stamped build into ~/.local/bin, the "user-local
# install" slot the dotfiles resolve_bin helper checks before the packaged mise
# shim, so the tmux popup and a bare `tprompt` use this build. Mirrors Rust's
# `cargo install --path .`: a copy (survives `git worktree remove`), so re-run
# after each rebuild. A sentinel marks the copy as dogfood-managed so dogfood
# never clobbers — and undogfood never deletes — a tprompt you installed there by
# other means.
LOCAL_BIN    := $(HOME)/.local/bin
DOGFOOD_MARK := $(LOCAL_BIN)/.tprompt.dogfood

.PHONY: dogfood
dogfood: build
	@if [ -e $(LOCAL_BIN)/tprompt ] && [ ! -e $(DOGFOOD_MARK) ]; then \
		echo "refusing to overwrite $(LOCAL_BIN)/tprompt: not installed by 'make dogfood' — remove it first" >&2; \
		exit 1; \
	fi
	@mkdir -p $(LOCAL_BIN)
	@install -m 0755 bin/tprompt $(LOCAL_BIN)/tprompt
	@touch $(DOGFOOD_MARK)
	@echo "dogfood ON  → $(LOCAL_BIN)/tprompt overrides the packaged binary (run 'make undogfood' to revert)"

.PHONY: undogfood
undogfood:
	@if [ -e $(DOGFOOD_MARK) ]; then \
		rm -f $(LOCAL_BIN)/tprompt $(DOGFOOD_MARK); \
		echo "dogfood OFF → tprompt resolves to the packaged (mise) binary"; \
	else \
		echo "nothing to do: no dogfood install at $(LOCAL_BIN)/tprompt"; \
	fi

.PHONY: dogfood-status
dogfood-status:
	@if [ -e $(DOGFOOD_MARK) ]; then \
		echo "dogfood ON  → $(LOCAL_BIN)/tprompt"; \
	else \
		echo "dogfood OFF → packaged binary (mise shim)"; \
	fi

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
