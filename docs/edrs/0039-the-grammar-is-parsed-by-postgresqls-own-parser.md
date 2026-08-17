---
id: 39
title: "The checkable grammar is parsed by PostgreSQL's own parser"
summary: "Every component that parses a statement uses libpg_query — PostgreSQL's real grammar — not a re-implementation. cgo is accepted in every binary, including the CLI, rather than maintain a second grammar that can disagree with the server."
status: accepted
implementation: none
implementation_note: "The consequences are staged for it — the Makefile exports CGO_ENABLED=1 for every target and CI builds on two operating systems because of this record — but libpg_query is not linked in, there is no internal/grammar, and no conformance corpus exists to pin a subset version against."
date: 2026-08-16
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The checkable statement grammar of
[EDR-0007](./0007-delegation-by-containment-proof.md) is parsed by **libpg_query** — PostgreSQL's
own parser, extracted from the server sources — through its Go binding, in **every** component that
parses a statement: Harbourmaster, Pilot, CLI, and the loopback proxy. There is no second parser.

This makes `CGO_ENABLED=1` a property of every Marque binary, including the one that ships to
laptops. That cost is accepted, and the release process changes to suit it: builds run on a runner
per operating system and architecture rather than cross-compiling from one.

The parse tree is not the decision. A single package walks that tree and returns one of three
verdicts — **in subset** with the extracted scope, **out of grammar**, or **unsupported** — and the
subset is defined by an allowlist over node types, never by pattern-matching text.

## Context

Seven records lean on "the checkable grammar" without saying what implements it. It is the input to
the static containment proof, the source of the predicate the fence is built from, the gate on
standing-order parameters, and the thing the proxy consults before brokering a statement. It is the
most load-bearing unnamed component in the design, so it needs deciding before Phase 1 sequences
anything around it.

**A re-implemented SQL grammar is a soundness risk in a way most re-implementations are not.**
PostgreSQL's grammar is large and full of cases that a derived or hand-written parser gets subtly
different: operator precedence in the presence of user-defined operators, `AT TIME ZONE`, the several
forms of `UPDATE … FROM`, dollar-quoting, escape-string behaviour that depends on
`standard_conforming_strings`, and a grammar that changes between major versions. If the parser
believes a statement is one shape and the server executes another, the containment proof is proving
something about a statement that does not exist.

**But it is worth being precise about where that risk actually lives, because overstating it would
justify the wrong thing.** The CLI and the proxy run on the operator's machine and are not trusted:
everything they decide is re-checked by the Harbourmaster at submission and by the Pilot's fence at
execution ([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)), and
[EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) binds the statement digest so the server
always re-parses the same bytes. A client-side parser that wrongly says "in scope" produces a
refusal, not a breach. **Soundness genuinely requires the real grammar only in the Harbourmaster and
the Pilot.**

So the honest argument for using it on the client too is not soundness. It is that a client which
says *in scope* and a server which says *out of scope* is a confusing product and an expensive bug to
chase, that the subset allowlist would otherwise have to be written and widened twice in step, and
that one person maintaining two SQL grammars as a side activity is not a realistic plan.

[ZFN-31](https://zrz.io/zfn/31-own-your-components/) is the standing position, and it points the
other way here on purpose: owning a component is justified when you understand the domain better than
the alternative. Nobody understands PostgreSQL's grammar better than PostgreSQL, and the alternative
is free.

The rejected option was **libpg_query server-side with a pure-Go advisory parse in the CLI**, which is
sound and buys a trivial cross-compile. It was rejected on the maintenance argument above, not on a
security one; if the cgo cost proves worse in practice than it looks here, this is the record to
supersede, and the reasoning to revisit is that paragraph rather than this one.

## Decision

**One parser, one package.** `internal/grammar` wraps `pg_query_go` and is the only place a statement
is parsed. Its interface is engine-shaped, taking and returning types that name no PostgreSQL
concept, so the second engine of
[EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) is an implementation of it rather than a
fork of everything above it.

**Three verdicts, allowlisted.** The subset walk enumerates the node types it accepts and rejects
everything else by default. The distinction
[EDR-0022](./0022-local-proxy-brokers-every-statement.md) draws is preserved in the return type:

| Verdict | Meaning | What happens |
|---|---|---|
| `in_subset` | Provable shape; scope and predicate extracted | May match a delegation or standing order |
| `out_of_grammar` | Valid SQL the subset cannot prove anything about | Escalates to a human |
| `unsupported` | Marque will not broker it at all (DDL, implicit commit, multi-statement) | Refused with the reason |

Never a boolean, and never a fourth verdict meaning "probably fine".

**The subset is versioned, and the version is recorded on every delegation and marque**, which
[EDR-0007](./0007-delegation-by-containment-proof.md) already requires so that widening the grammar
cannot retroactively change what an old delegation permits. This record names the mechanism that
makes it enforceable: see the obligation on parser upgrades below.

**A conformance corpus is the specification.** A language-neutral file of
`(statement, verdict, extracted scope)` cases is executed by the test suite. Widening the subset is a
diff to that file, reviewed as such. A statement that changes verdict without a corresponding
corpus change fails the build.

**The supported PostgreSQL major range is declared, and checked at connection.** The parser is pinned
to a major version; a target running a *newer* major may parse a statement the pinned grammar does
not have. The Pilot reads `server_version_num` when it connects and, outside the declared range,
marks the target as unable to support a proved scope — so statements against it take the human path
rather than a fast path. This is the same principle
[EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) applies to engines, applied to a version
rather than an engine; it introduces no new verification case, only a narrower one.

**cgo everywhere.** Every binary sets `CGO_ENABLED=1`. Release builds run on a native runner per
operating system and architecture. `go install` continues to work and requires a C toolchain, which
is stated in the installation instructions rather than discovered.

## Consequences

**Easier.**

- There is exactly one answer to "what does this statement do", and it is the server's answer. Dollar
  quoting, escape-string behaviour, precedence and every other grammar subtlety are correct without
  anyone reasoning about them.
- Widening the checkable subset is one allowlist and one corpus, not two kept in step.
- A statement rejected by the CLI is rejected by the Harbourmaster for the identical reason, so the
  error the operator reads is the real one.

**Harder.**

- **Cross-compilation is gone.** The release matrix needs a native runner per platform, or a
  cross-toolchain shim; either is more moving parts than `GOOS=… go build`, and this is the single
  largest cost of the decision.
- **Contributing needs a C toolchain**, so the barrier to a first patch is higher than a pure-Go
  project's, and CI is slower.
- **Anyone embedding Marque's grammar package inherits cgo**, whether or not they wanted it.
- **The console cannot parse anything.** No cgo in a browser, and WebAssembly is not on the table for
  a CSP-strict static app. The console's statement view is display-only — which
  [EDR-0024](./0024-the-console-is-for-deciding.md) independently requires, so nothing is lost, but
  it is now also structural.
- Container images need the parser's static library present, so "scratch plus a static binary" needs
  care rather than being the default.

**New obligations.**

- **A libpg_query upgrade is a reviewed change, on the order of a schema migration.** A new major can
  parse a statement the previous one could not, which silently *widens* the checkable subset and
  therefore widens what an existing delegation permits — precisely what
  [EDR-0007](./0007-delegation-by-containment-proof.md) forbids. An upgrade re-runs the conformance
  corpus, and any verdict that changes requires the subset version to be bumped, so old delegations
  stay pinned to the subset they were signed against.
- The supported PostgreSQL major range is declared publicly, and each major in it is covered by the
  integration suite.
- The installation instructions state the toolchain requirement.

## References

- [ZFN-31](https://zrz.io/zfn/31-own-your-components/) — own a component only where you understand
  the domain; the grammar is the case where that argues for not owning it.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the containment proof and the versioned
  subset this record implements.
- [EDR-0022](./0022-local-proxy-brokers-every-statement.md) — the out-of-grammar versus unsupported
  distinction the verdict type carries.
- [EDR-0026](./0026-a-second-engine-is-a-capability-matrix.md) — engine capability, extended here to
  a server version outside the parser's range.
- [EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) — the digest binding that makes a
  client-side parse advisory rather than load-bearing.
- [libpg_query](https://github.com/pganalyze/libpg_query) — PostgreSQL's parser as a library;
  [pg_query_go](https://github.com/pganalyze/pg_query_go) is the binding.

## Changelog

- **2026-08-16**: Accepted.
