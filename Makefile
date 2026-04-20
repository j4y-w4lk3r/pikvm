# Convenience wrappers for common dev tasks. None of these are required —
# you can always invoke the underlying commands directly. See README for
# the full workflow.

.PHONY: build test vet lint fmt install snapshot release clean help

BIN := pikvm
PKG := ./cmd/pikvm

build: ## Compile the binary for the host platform
	go build -o $(BIN) $(PKG)

test: ## Run all tests (live-PiKVM tests skip if no config.json)
	go test -v ./...

vet: ## go vet everything
	go vet ./...

lint: vet ## Alias for vet (hook for golangci-lint later)

fmt: ## gofmt + goimports everything
	gofmt -w .

install: build ## Symlink the binary into ~/.local/bin (no sudo)
	@mkdir -p "$(HOME)/.local/bin"
	ln -sf "$(PWD)/$(BIN)" "$(HOME)/.local/bin/$(BIN)"
	@echo "linked $(HOME)/.local/bin/$(BIN) -> $(PWD)/$(BIN)"
	@echo "ensure $(HOME)/.local/bin is in your PATH"

snapshot: ## Build all release targets locally into ./dist/ (no push)
	goreleaser release --snapshot --clean

release: ## Cut a release on HEAD — requires a vX.Y.Z tag already pushed
	goreleaser release --clean

clean: ## Remove build artifacts
	rm -rf $(BIN) dist/

help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
