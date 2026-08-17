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
PROTO_ROOT            := proto
DESCRIPTOR_ALL        := $(BIN_DIR)/descriptor-all.binpb
DESCRIPTOR_OWNED      := $(BIN_DIR)/descriptor-owned.binpb
DESCRIPTOR_BEFORE     := $(BIN_DIR)/descriptor-before.binpb

# The ref the wire contract is compared against. CI overrides it on a push to
# main, where origin/main is the commit being pushed and comparing against it
# would compare the schema with itself.
BASE_REF ?= origin/main

# Version stamps. goreleaser overrides these for a release; a developer build
# describes the working tree instead, so a binary never claims to be a release
# it is not. See internal/version.
#
# SOURCE_DATE is the date of the *source*, not of the build, so that building
# the same commit twice produces the same binary. For a tool whose logbook
# entries name the software that produced them, "same source, same binary" is
# worth more than knowing when the artefact was made — and the field it lands in
# is named source_date, so the name and the value agree.
#
# SOURCE_DATE_EPOCH wins where a distribution sets it, which is the whole point
# of that convention; otherwise the commit date is used. Left empty outside a
# git checkout, which lets internal/version fall back to Go's embedded VCS stamp
# rather than inventing a date.
#
# The format string is explicit rather than `--date=iso-strict-local`, which
# renders a UTC offset as "Z" on git 2.50 and as "+00:00" on git 2.39 — the same
# instant, a different string, and therefore a different binary from the same
# commit on two machines. Normalising the value is not enough; the
# representation has to be pinned too.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
# The dirty suffix is decided inside the same shell that resolved the revision.
# Two separate $(shell)s concatenated produced "unknown-dirty" from a source
# archive with no .git, because both commands failed and the second failure
# reads as "modified".
COMMIT ?= $(shell \
            if rev=$$(git rev-parse --short HEAD 2>/dev/null); then \
              git diff --quiet HEAD 2>/dev/null || rev="$$rev-dirty"; \
              printf '%s' "$$rev"; \
            else \
              printf 'unknown'; \
            fi)
#
# The epoch is validated before either date(1) is tried, because `date -r`
# also accepts a *filename* and reports its mtime — on GNU that is all it
# accepts, and BSD takes seconds or a filename. So on every platform an
# unconvertible value that happens to name a file, "Makefile" say, quietly
# yielded a plausible date and walked past the guard below.
#
# Leading zeros are rejected rather than merely tolerated: BSD parses the
# seconds with base-0 semantics, so "010" is octal there and decimal on
# GNU/BusyBox — the same input, two instants, two binaries from one commit,
# which is the whole thing this variable exists to prevent.
#
# The test is `grep` with a top-level alternation rather than the more natural
# `case`, or a grouped regex, because make counts parentheses inside
# $(shell ...): a `)` closing a case pattern, or a group, ends the expansion
# early and produces a linker flag assembled out of shell fragments.
#
# $(strip) is belt-and-braces here — the continuations now sit inside the shell
# argument, where leading whitespace is inert — and stays because the expansion
# feeds an -X linker flag, in which a stray leading space fails the build with a
# message naming nothing.
SOURCE_DATE ?= $(strip $(shell \
                 epoch='$(SOURCE_DATE_EPOCH)'; \
                 if [ -n "$$epoch" ]; then \
                   printf '%s' "$$epoch" | grep -qE '^0$$|^[1-9][0-9]*$$' || exit 0; \
                   date -u -d "@$$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
                     || date -u -r "$$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null; \
                 else \
                   TZ=UTC git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>/dev/null; \
                 fi))

# A distribution that sets SOURCE_DATE_EPOCH has asked for a reproducible date.
# Silently falling back to something else is the one response it must not get.
ifdef SOURCE_DATE_EPOCH
ifeq ($(SOURCE_DATE),)
$(error SOURCE_DATE_EPOCH is set to '$(SOURCE_DATE_EPOCH)', which is not usable as a source date: \
it must be a decimal integer with no leading zeros, and convertible by date(1) here)
endif
endif

LDFLAGS := -X $(MODULE)/internal/version.buildVersion=$(VERSION) \
           -X $(MODULE)/internal/version.buildCommit=$(COMMIT) \
           -X $(MODULE)/internal/version.buildSourceDate=$(SOURCE_DATE)

.DEFAULT_GOAL := help
.PHONY: help build test lint docs dev clean tools generate generate-check schema-check breaking compat

help: ## Show this help
	@grep -hE '^[a-z][a-zA-Z0-9_-]*:.*## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*## "}; {printf "  %-15s %s\n", $$1, $$2}'

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

# Two descriptor sets: the owned one says which methods are ours to check, the
# full one resolves request messages that live in imported files. See the
# package comment in internal/schema.
schema-check: $(BUF) ## Fail if a method's retry declaration is missing or malformed
	@mkdir -p $(BIN_DIR)
	@stray=$$(git ls-files '*.proto' | grep -v '^$(PROTO_ROOT)/' || true); \
	if [ -n "$$stray" ]; then \
		echo "these .proto files are outside $(PROTO_ROOT)/, where buf never sees them:"; \
		echo "$$stray"; \
		exit 1; \
	fi
	$(BUF) build -o $(DESCRIPTOR_ALL)
	$(BUF) build --exclude-imports -o $(DESCRIPTOR_OWNED)
	go run ./internal/schema/schemacheck \
		-owned $(DESCRIPTOR_OWNED) -all $(DESCRIPTOR_ALL) -proto-root $(PROTO_ROOT)

# A wire contract is a thing you cannot take back once a client has shipped, so
# it is reviewed like a schema migration (EDR-0020).
#
# The ref is asserted before anything else. The previous shape of this target
# treated "$(BASE_REF) is not a ref here" the same as "there is no schema to
# compare against" and printed a reassuring message for both — so a shallow
# checkout, a renamed remote or a renamed default branch all turned a loud
# failure into a silent pass.
#
# The remaining escape is for bootstrapping only: main genuinely has no schema
# until this lands. Delete it in the next change; keeping it means that
# deleting buf.yaml from main silently disables the check.
#
# The `&&` is load-bearing and was not there at first. Make runs a recipe under
# sh without -e, so a multi-command `if` block exits with the status of its
# *last* command: `buf breaking` could report an incompatible change, the shell
# would carry on to `make compat`, and a passing compat check would hand back a
# zero exit status for the whole target. That defect could not fire while the
# bootstrap branch was the one being taken, and would have armed itself on the
# merge that made this target live.
breaking: $(BUF) ## Check the schema against $(BASE_REF) for a breaking change
	@git rev-parse -q --verify '$(BASE_REF)^{commit}' >/dev/null || { \
		echo "$(BASE_REF) is not in this clone, so the wire contract cannot be compared."; \
		echo "Fetch it first; CI needs actions/checkout with fetch-depth: 0."; \
		exit 1; \
	}
	@if git cat-file -e '$(BASE_REF):buf.yaml' 2>/dev/null; then \
		$(BUF) breaking --against '.git#ref=$(BASE_REF)' \
			&& $(MAKE) --no-print-directory compat; \
	else \
		echo "NOTE: $(BASE_REF) carries no schema, so there is nothing to compare against."; \
		echo "This is true only while bootstrapping the schema; remove this branch once it lands."; \
	fi

# What `buf breaking` cannot see. Its rules compare field numbers, names and
# types and ignore custom method options entirely, so a method can be
# reclassified from safe to unsafe with every check green (EDR-0040).
#
# `-proto-root` is deliberately not passed here: coverage is `schema-check`'s
# job, it runs in the lint job, and repeating it would report the same violation
# twice in two places.
compat: $(BUF) ## Fail if a method's declared behaviour weakened since $(BASE_REF)
	@mkdir -p $(BIN_DIR)
	$(BUF) build '.git#ref=$(BASE_REF)' --exclude-imports -o $(DESCRIPTOR_BEFORE)
	$(BUF) build -o $(DESCRIPTOR_ALL)
	$(BUF) build --exclude-imports -o $(DESCRIPTOR_OWNED)
	go run ./internal/schema/schemacheck \
		-owned $(DESCRIPTOR_OWNED) -all $(DESCRIPTOR_ALL) -before $(DESCRIPTOR_BEFORE)

docs: ## Build the documentation site — the validator for records and entries
	cd website && pnpm install --frozen-lockfile && pnpm run build

dev: ## Run the local development loop
	@echo "The service stack arrives with the walking skeleton (M1). Until then"
	@echo "the only thing to run is the documentation site."
	cd website && pnpm run serve

clean: ## Remove build output, including what `make docs` installs
	rm -rf $(BIN_DIR) dist website/dist website/node_modules

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
