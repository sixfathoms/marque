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

GOLANGCI              := $(TOOL_DIR)/golangci-lint
BUF                   := $(TOOL_DIR)/buf
PROTOC_GEN_GO         := $(TOOL_DIR)/protoc-gen-go
PROTOC_GEN_CONNECT_GO := $(TOOL_DIR)/protoc-gen-connect-go
DESCRIPTOR            := $(BIN_DIR)/descriptor.binpb

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
.PHONY: help build test lint docs dev clean tools generate generate-check schema-check breaking

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

lint: $(GOLANGCI) $(BUF) ## Check formatting, and lint the schema and the Go
	$(BUF) format --diff --exit-code
	$(BUF) lint
	@$(MAKE) --no-print-directory schema-check
	$(GOLANGCI) fmt --diff
	$(GOLANGCI) run ./...

generate: $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_CONNECT_GO) ## Regenerate gen/ from proto/
	$(BUF) generate

generate-check: generate ## Fail if the committed generated code is out of date
	@if [ -n "$$(git status --porcelain -- gen)" ]; then \
		echo "gen/ does not match proto/. Run 'make generate' and commit the result:"; \
		git status --porcelain -- gen; \
		git --no-pager diff -- gen; \
		exit 1; \
	fi

schema-check: $(BUF) ## Fail if any method is missing its retry annotation
	@mkdir -p $(BIN_DIR)
	$(BUF) build -o $(DESCRIPTOR)
	go run ./internal/schema/schemacheck $(DESCRIPTOR)

# A wire contract is a thing you cannot take back once a client has shipped, so
# it is reviewed like a schema migration (EDR-0020).
#
# The guard exists only while main predates the schema; once this lands there is
# always something to compare against, and it should be deleted. Keeping it
# would mean that deleting buf.yaml from main silently disables the check.
breaking: $(BUF) ## Check the schema against main for a breaking change
	@if git cat-file -e origin/main:buf.yaml 2>/dev/null; then \
		$(BUF) breaking --against '.git#ref=origin/main'; \
	else \
		echo "origin/main carries no schema yet, so there is nothing to compare against."; \
	fi

docs: ## Build the documentation site — the validator for records and entries
	cd website && pnpm install --frozen-lockfile && pnpm run build

dev: ## Run the local development loop
	@echo "The service stack arrives with the walking skeleton (M1). Until then"
	@echo "the only thing to run is the documentation site."
	cd website && pnpm run serve

clean: ## Remove build output
	rm -rf $(BIN_DIR) dist website/dist

tools: $(GOLANGCI) $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_CONNECT_GO) ## Build the pinned developer tools into ./bin/tools

# golangci-lint and buf live in their own module so that their dependency
# graphs — some two hundred packages between them — never enter ours. Go skips
# nested modules when resolving ./..., so nothing else has to know.
$(GOLANGCI): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/golangci/golangci-lint/v2/cmd/golangci-lint

$(BUF): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/bufbuild/buf/cmd/buf

# The two protoc plugins are built from *this* module, not tools/, on purpose.
# protoc-gen-go ships inside google.golang.org/protobuf and protoc-gen-connect-go
# inside connectrpc.com/connect — the same modules the generated code links
# against at runtime. Building them here makes the generator's version and the
# runtime's version the same by construction.
$(PROTOC_GEN_GO): go.mod go.sum
	@mkdir -p $(TOOL_DIR)
	go build -o $@ google.golang.org/protobuf/cmd/protoc-gen-go

$(PROTOC_GEN_CONNECT_GO): go.mod go.sum
	@mkdir -p $(TOOL_DIR)
	go build -o $@ connectrpc.com/connect/cmd/protoc-gen-connect-go
