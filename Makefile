# Marque's build entry points.
#
# Six uniform verbs — build test lint docs dev clean — each of which works from
# a clean checkout with nothing installed but Go, and pnpm for the docs.
#
# CGO_ENABLED=1 is exported once here rather than set per-target, deliberately.
# The grammar *will* parse statements with PostgreSQL's own parser via
# libpg_query, which is C (EDR-0039), and from then on every build, test and
# release of every component is a cgo build. A target that quietly dropped the
# flag would produce a binary that cannot parse anything, and would do it at the
# worst possible moment.
#
# Today nothing in the dependency graph has a cgo file, so the flag changes no
# binary yet — the same fact .goreleaser.yaml and the implementation plan state.
# It is set now because retrofitting it across every target later is how one
# gets missed.
export CGO_ENABLED := 1

MODULE   := github.com/sixfathoms/marque
BINARIES := marque harbourmaster pilot
BIN_DIR  := bin
TOOL_DIR := $(BIN_DIR)/tools

GOLANGCI              := $(TOOL_DIR)/golangci-lint
BUF                   := $(TOOL_DIR)/buf
PROTOC_GEN_GO         := $(TOOL_DIR)/protoc-gen-go
PROTOC_GEN_CONNECT_GO := $(TOOL_DIR)/protoc-gen-connect-go
GORELEASER            := $(TOOL_DIR)/goreleaser
PROTO_ROOT            := proto
DESCRIPTOR_ALL        := $(BIN_DIR)/descriptor-all.binpb
DESCRIPTOR_OWNED      := $(BIN_DIR)/descriptor-owned.binpb
DESCRIPTOR_BEFORE     := $(BIN_DIR)/descriptor-before.binpb

# The ref the wire contract is compared against. CI overrides it on a push to
# main, where origin/main is the commit being pushed and comparing against it
# would compare the schema with itself.
BASE_REF ?= origin/main
export BASE_REF

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
# One definition of "the working tree differs from HEAD", shared by the commit
# stamp and by goreleaser, because two definitions would let the two build paths
# disagree about the same tree.
#
# It counts *untracked* files, which `git diff HEAD` does not — and an untracked
# .go file inside a package is compiled, so a build over one is not HEAD however
# clean git diff says the tree is.
#
# The git-dir check comes first so that a git *failure* cannot read as
# "modified": two concatenated $(shell)s once produced "unknown-dirty" from a
# source archive with no .git, because both commands failed and the second
# failure looked like a modification.
# --untracked-files=normal is passed explicitly because status.showUntrackedFiles=no
# is a common developer setting and would otherwise switch off the untracked
# counting that is the whole point of this variable. The flag overrides the config.
#
# A status that *fails* is not clean. It was not dirty either, so it says neither:
# claiming clean would put HEAD's commit on an artefact whose contents nobody
# could verify, which is the one direction that lies.
MARQUE_DIRTY := $(shell git rev-parse --git-dir >/dev/null 2>&1 || exit 0; \
                  status=$$(git status --porcelain --untracked-files=normal 2>/dev/null) \
                    || { printf -- '-unverified'; exit 0; }; \
                  test -z "$$status" || printf -- '-dirty')
export MARQUE_DIRTY

COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'unknown')$(MARQUE_DIRTY)
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

# Exported for goreleaser, which has no SOURCE_DATE_EPOCH support of its own and
# would otherwise stamp .CommitDate while make stamped the override — a
# guaranteed disagreement, and a documented reproducibility switch silently
# ignored on the release path.
SOURCE_EPOCH ?= $(strip $(if $(SOURCE_DATE_EPOCH),$(SOURCE_DATE_EPOCH),\
                  $(shell git log -1 --format=%ct 2>/dev/null)))
export SOURCE_DATE
export SOURCE_EPOCH

LDFLAGS := -X $(MODULE)/internal/version.buildVersion=$(VERSION) \
           -X $(MODULE)/internal/version.buildCommit=$(COMMIT) \
           -X $(MODULE)/internal/version.buildSourceDate=$(SOURCE_DATE)

.DEFAULT_GOAL := help
.PHONY: help build test lint docs dev clean tools generate generate-check schema-check breaking compat snapshot snapshot-check platform-check

help: ## Show this help
	@grep -hE '^[a-z][a-zA-Z0-9_-]*:.*## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*## "}; {printf "  %-15s %s\n", $$1, $$2}'

# -buildvcs=false matches the release path. It is a deliberate trade, not a
# tidy-up: the three -X stamps carry version, commit and source date, but they
# are program data, and with `-trimpath` alongside they do not appear in
# `go version -m` either — so an SBOM generator reading build settings gets
# nothing from either path. (Without `-trimpath` they would show up in the
# recorded `build -ldflags=` setting; the two flags travel together here.) What it buys is that a
# dead -X path is visible as "unknown" rather than silently backfilled, and
# that a build inside a linked worktree stops reporting the *parent*
# repository's HEAD as its own. Revisit before releases are enabled — see
# https://github.com/sixfathoms/marque/issues/14
build: ## Build every binary into ./bin
	@mkdir -p $(BIN_DIR)
	@for b in $(BINARIES); do \
		echo "  build $$b"; \
		go build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$$b ./cmd/$$b || exit 1; \
	done

test: ## Run the test suite
	go test -race -count=1 ./...

# A disposable PostgreSQL, the runtime role created BEFORE the first migration
# because the schema grants unconditionally, and the DSN the tests read. The
# container is removed on exit, including on failure.
#
# The host port is EPHEMERAL and read back from docker, not fixed. A fixed one
# was already taken on this machine by an unrelated PostgreSQL, so the tests
# connected to that instead and reported a missing database — while the health
# check, which ran inside the container, passed. Readiness is checked from the
# host, through the same address the tests use, for the same reason.
PGIMAGE ?= postgres:18

test-integration: ## Run the tests that need a real PostgreSQL
	@docker rm -f marque-test-pg >/dev/null 2>&1 || true
	@docker run -d --name marque-test-pg \
		-e POSTGRES_PASSWORD=marque -e POSTGRES_DB=marque \
		-p 127.0.0.1::5432 $(PGIMAGE) >/dev/null
	@trap 'docker rm -f marque-test-pg >/dev/null 2>&1 || true' EXIT; \
		port=$$(docker port marque-test-pg 5432 | head -1 | sed 's/.*://'); \
		test -n "$$port" || { echo "no host port was published"; exit 1; }; \
		dsn="host=127.0.0.1 port=$$port user=postgres password=marque dbname=marque sslmode=disable"; \
		echo "PostgreSQL on 127.0.0.1:$$port"; \
		for i in $$(seq 1 60); do \
			docker exec marque-test-pg \
				psql -qtA -U postgres -d marque -c 'SELECT 1' >/dev/null 2>&1 && break; \
			sleep 1; \
		done; \
		docker exec marque-test-pg psql -q -U postgres -c \
			"CREATE ROLE marque_runtime NOLOGIN" >/dev/null || exit 1; \
		MARQUE_TEST_DSN="$$dsn" \
			go test -tags integration -count=1 -timeout 10m ./internal/harbourmaster/store/

lint: $(GOLANGCI) $(BUF) ## Check formatting, and lint the schema and the Go
	$(BUF) format --diff --exit-code
	$(BUF) lint
	@$(MAKE) --no-print-directory schema-check
	$(GOLANGCI) fmt --diff
	$(GOLANGCI) run ./...
	# Again with the tag, because golangci-lint evaluates build constraints and
	# the default run does not read internal/harbourmaster/store's integration
	# tests at all. Not a second opinion — a second set of files.
	$(GOLANGCI) run --build-tags integration ./...

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
# There is no escape for a base ref without a schema. There was one, needed
# exactly once, while main predated the schema; keeping it past that would have
# meant that deleting buf.yaml from main silently disabled both checks, which is
# the shape of failure this target is built to refuse.
#
# The two checks are separate recipe lines, not one shell block. Make runs each
# line in its own shell and tests each one's status, so a failing `buf breaking`
# stops the target. Written as one block it would not have: make runs a recipe
# under sh without -e, so a multi-command `if` exits with the status of its
# *last* command, and a passing compat check handed back success for the whole
# target while buf was reporting an incompatible change.
breaking: $(BUF) ## Check the schema against $(BASE_REF) for a breaking change
	@git rev-parse -q --verify "$$BASE_REF^{commit}" >/dev/null || { \
		echo "$$BASE_REF is not in this clone, so the wire contract cannot be compared."; \
		echo "Two causes, and the fix differs:"; \
		echo "  - the ref exists but was not fetched: CI needs actions/checkout"; \
		echo "    with fetch-depth: 0."; \
		echo "  - the ref does not meaningfully exist. github.event.before is the"; \
		echo "    all-zeros SHA when a branch is created, and an orphaned commit"; \
		echo "    after a force-push. No checkout depth helps, because the commit"; \
		echo "    is unreachable from any ref. Compare against a commit that is,"; \
		echo "    and resolve it here: buf takes an object id, a ref name or"; \
		echo "    HEAD, but re-clones first, so origin/main~1 has nothing to"; \
		echo "    walk back through and fails while origin/main works."; \
		echo "      make breaking BASE_REF=\"\$$(git rev-parse origin/main~1)\""; \
		echo "    Note that after a force-push that is the parent of the new"; \
		echo "    head, not the tip clients actually saw."; \
		exit 1; \
	}
	@git cat-file -e "$$BASE_REF:buf.yaml" 2>/dev/null || { \
		echo "$$BASE_REF carries no buf.yaml, so the wire contract cannot be compared."; \
		echo "The schema has been on the main branch since"; \
		echo "https://github.com/sixfathoms/marque/pull/2 — so this means something is wrong."; \
		exit 1; \
	}
	$(BUF) breaking --against ".git#ref=$$BASE_REF"
	@$(MAKE) --no-print-directory compat

# What `buf breaking` cannot see. Its rules compare field numbers, names and
# types and ignore custom method options entirely, so a method can be
# reclassified from safe to unsafe with every check green (EDR-0040).
#
# `-proto-root` is deliberately not passed here: coverage is `schema-check`'s
# job, it runs in the lint job, and repeating it would report the same violation
# twice in two places.
compat: $(BUF) ## Fail if a method's declared behaviour weakened since $(BASE_REF)
	@mkdir -p $(BIN_DIR)
	$(BUF) build ".git#ref=$$BASE_REF" --exclude-imports -o $(DESCRIPTOR_BEFORE)
	$(BUF) build -o $(DESCRIPTOR_ALL)
	$(BUF) build --exclude-imports -o $(DESCRIPTOR_OWNED)
	go run ./internal/schema/schemacheck \
		-owned $(DESCRIPTOR_OWNED) -all $(DESCRIPTOR_ALL) -before $(DESCRIPTOR_BEFORE)

# A release build for *this* platform only. cgo needs a C toolchain for the
# target, so one machine cannot produce the whole matrix; CI runs this on a
# native runner per supported platform. Nothing is published — see
# .goreleaser.yaml, where release is disabled.
snapshot: $(GORELEASER) ## Build a release snapshot for this platform; publishes nothing
	$(GORELEASER) build --snapshot --single-target --clean

# What this proves, stated accurately, because it is not what it was when it was
# written. The two paths no longer derive the source date independently — they
# read one exported value, so that SOURCE_DATE_EPOCH can be honoured by both.
# A shared source is a stronger guarantee than two sources that agree, but it
# does mean the comparison is no longer evidence about the date itself.
#
# What it still proves, stated only as far as it goes. Agreement alone does not
# show a stamp arrived: delete a commit or source-date -X from both configs and
# both sides say "unknown" and agree, which is why the unknown check below
# exists. Deleting the *version* -X does not even reach that check, because the
# version is outside the compared string. TestBuildConfigsStampEveryVariable,
# not this target, is what catches a missing -X.
# What is uniquely enforced here is that the artefact this platform produced
# actually executes here, and that the two paths were handed the same tree
# state — though CI always checks out clean, so the dirty half of that is
# asserted statically in internal/version instead.
#
# The commit *is* compared, now that MARQUE_DIRTY reaches both sides. Leaving it out
# was how a dirty tree could produce a goreleaser binary stamped with a clean
# HEAD it does not contain, and have the check agree.
#
# `head -1` is safe only because `snapshot` passes --single-target, so dist
# holds one binary per name. Drop that flag and this silently compares the
# first of several — on Linux a foreign-arch binary produces no stdout at all,
# so the coverage would shrink without anything failing.
snapshot-check: platform-check build snapshot ## Fail if make and goreleaser disagree about a build
	@mine=$$(./$(BIN_DIR)/marque | sed 's/.*(\(.*\)) go.*/\1/'); \
	theirs=$$(find dist -type f -name marque -exec {} \; | head -1 | sed 's/.*(\(.*\)) go.*/\1/'); \
	if [ -z "$$theirs" ]; then \
		echo "the snapshot binary produced no version line — it is missing, or it did not run"; \
		exit 1; \
	fi; \
	if [ "$$mine" != "$$theirs" ]; then \
		echo "make and goreleaser disagree about what they built:"; \
		echo "  make:       $$mine"; \
		echo "  goreleaser: $$theirs"; \
		exit 1; \
	fi; \
	case "$$mine" in *unknown*) \
		echo "both paths report $$mine, so they agree about a stamp that never arrived."; \
		echo "a -X path stopped resolving; agreement is not arrival."; \
		exit 1;; \
	esac; \
	echo "  commit and source date agree across both build paths: $$mine"

# Two callers: `snapshot-check`, where it is ordered before `build snapshot` so
# that under serial make — which is what CI runs — a mislabelled runner fails in
# a second rather than after a full cgo build; and the `build-test` job, whose
# tier the implementation plan describes by platform, so something has to hold
# that description true. Under `make -j` a build may start first; the assertion
# still fails the target, just less cheaply. EXPECT_PLATFORM is unset for local
# use; CI requires it, and checks that it required it — see
# .github/workflows/ci.yml.
platform-check: ## Fail if this host is not the platform EXPECT_PLATFORM names
	@if [ -n "$$EXPECT_PLATFORM" ]; then \
		actual="$$(go env GOOS)/$$(go env GOARCH)"; \
		if [ "$$actual" != "$$EXPECT_PLATFORM" ]; then \
			echo "this runner is $$actual, but the job says it is $$EXPECT_PLATFORM."; \
			echo "a matrix entry naming a platform it is not means CI reports coverage"; \
			echo "it never had — a build never made, or a test never run there."; \
			echo "Fix the runner label or the matrix entry."; \
			exit 1; \
		fi; \
		echo "  platform: $$actual"; \
	fi

docs: ## Build the documentation site — the validator for records and entries
	cd website && pnpm install --frozen-lockfile && pnpm run build

dev: ## Run the local development loop
	@echo "The service stack arrives with the walking skeleton (M1). Until then"
	@echo "the only thing to run is the documentation site."
	cd website && pnpm run serve

clean: ## Remove build output, including what `make docs` installs
	rm -rf $(BIN_DIR) dist website/dist website/node_modules

tools: $(GOLANGCI) $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_CONNECT_GO) $(GORELEASER) ## Build the pinned developer tools into ./bin/tools

# golangci-lint, buf and goreleaser live in their own module so that their
# dependency graphs — some hundreds of packages between them — never enter
# ours. Go skips nested modules when resolving ./..., so nothing else has to
# know.
#
# The isolation is from *this* module, not from each other. Adding goreleaser
# moved the protobuf and connect versions that buf links, without buf's own
# version changing — harmless, and verified so, but it means upgrading one tool
# can relink another. buf builds every descriptor the schema checks run on, so
# a `go get -u` in tools/ deserves the same suspicion as a dependency bump in
# the main module.
$(GOLANGCI): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/golangci/golangci-lint/v2/cmd/golangci-lint

$(BUF): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/bufbuild/buf/cmd/buf

$(GORELEASER): tools/go.mod tools/go.sum
	@mkdir -p $(TOOL_DIR)
	go -C tools build -o $(CURDIR)/$@ github.com/goreleaser/goreleaser/v2

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
