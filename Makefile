# Marque's build entry points.
#
# Six uniform verbs — build test lint docs dev clean — each of which works from
# a clean checkout with nothing installed but Go, and pnpm for the docs.
#
# CGO_ENABLED=1 is exported once here rather than set per-target, deliberately.
# The grammar parses statements with PostgreSQL's own parser via libpg_query,
# which is C (EDR-0039), so every build, test and release of every component is
# a cgo build. A target that quietly dropped the flag would produce a binary
# that cannot parse anything, and would do it at the worst possible moment.
export CGO_ENABLED := 1

MODULE   := github.com/sixfathoms/marque
BINARIES := marque harbourmaster pilot
BIN_DIR  := bin
TOOL_DIR := $(BIN_DIR)/tools
GOLANGCI := $(TOOL_DIR)/golangci-lint

# Version stamps. goreleaser overrides these for a release; a developer build
# describes the working tree instead, so a binary never claims to be a release
# it is not. See internal/version.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(MODULE)/internal/version.buildVersion=$(VERSION) \
           -X $(MODULE)/internal/version.buildCommit=$(COMMIT) \
           -X $(MODULE)/internal/version.buildDate=$(DATE)

.DEFAULT_GOAL := help
.PHONY: help build test lint docs dev clean tools

help: ## Show this help
	@grep -hE '^[a-z][a-zA-Z0-9_-]*:.*## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*## "}; {printf "  %-8s %s\n", $$1, $$2}'

build: ## Build every binary into ./bin
	@mkdir -p $(BIN_DIR)
	@for b in $(BINARIES); do \
		echo "  build $$b"; \
		go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$$b ./cmd/$$b || exit 1; \
	done

test: ## Run the test suite
	go test -race -count=1 ./...

lint: $(GOLANGCI) ## Check formatting and lint
	$(GOLANGCI) fmt --diff
	$(GOLANGCI) run ./...

docs: ## Build the documentation site — the validator for records and entries
	cd website && pnpm install --frozen-lockfile && pnpm run build

dev: ## Run the local development loop
	@echo "The service stack arrives with the walking skeleton (M1). Until then"
	@echo "the only thing to run is the documentation site."
	cd website && pnpm run serve

clean: ## Remove build output
	rm -rf $(BIN_DIR) dist website/dist

tools: $(GOLANGCI) ## Build the pinned developer tools into ./bin/tools

# The linter lives in its own module so that its dependency graph — some two
# hundred packages, including a pin on google.golang.org/protobuf, which is a
# real dependency of this project — never enters ours. Go skips nested modules
# when resolving ./..., so nothing else has to know it is there.
$(GOLANGCI): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/golangci/golangci-lint/v2/cmd/golangci-lint
