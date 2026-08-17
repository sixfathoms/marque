---
title: "M0 closes: the toolchain, the schema, and the guards that hold them"
tags: [ops, security, docs]
---

Implementation has started. The first milestone of the
[implementation plan](/overview/implementation-plan/) is the one that builds nothing a user can see
and everything that makes the next seven cheaper — the toolchain, the release skeleton, and the
checks that fail when a convention is broken rather than when someone notices.

Nothing here runs against a database. The
[roadmap](/roadmap/) is the honest account of that: 38 of 40 decision records remain unbuilt.

### Added

- **Three binaries** — `marque`, `harbourmaster` and `pilot` — as separate programs rather than one
  with a role flag, because four components with sharply different trust is the whole security
  argument ([EDR-0001](/edrs/0001-marque-platform-architecture/)).
- **One schema, and clients generated from it.** `proto/marque/v1` with committed Go and Connect
  stubs, `buf breaking` on every pull request, and a build that rejects a method which does not
  declare whether repeating it is safe ([EDR-0020](/edrs/0020-one-schema-generates-every-client/)).
- **[EDR-0040](/edrs/0040-a-methods-declared-behaviour-may-only-strengthen/) — a method's declared
  behaviour may only strengthen.** `buf breaking` ignores custom method options entirely, so a
  method could be reclassified from read-only to never-retry with every check green, while clients
  built against the old schema carried on retrying it. A separate check now compares every method
  against the base branch.
- **An implementation state on every record**, and a page derived from it. `status` says what was
  decided; `implementation` says what exists. They are orthogonal on purpose: a record is routinely
  `accepted` and `none` at once.
- **The conformance-vector harness.** `testdata/conformance/statements.json` is normative and
  currently empty; the format and its validator exist now so that the first vector to arrive is
  checked on arrival. The vectors and the grammar that runs them are M2.
- **A release skeleton that releases nothing.** goreleaser builds a snapshot on a native runner for
  each of linux and darwin on amd64 and arm64, so a broken release matrix is discovered on a pull
  request rather than at version 0.1.

### Changed

- **The documentation site records what is built.** Every record carries an `implementation` field
  the build refuses to let you omit, and `/roadmap/` groups records by it — derived from the
  frontmatter and nothing else, so there is no second list to fall out of step.
- **The docs workflow holds write permissions only on the job that deploys.** The job that runs the
  site build — repository code, from the branch under test — no longer inherits the ability to mint
  an OIDC identity.
- **Builds stamp the source's date rather than the wall clock**, so the same commit produces the
  same binary, and `SOURCE_DATE_EPOCH` is honoured on both build paths.

### Fixed

- **A guard that reported success having compared nothing.** `make breaking` treated "the base
  branch carries no schema" as a reason to exit 0, which was correct exactly once — while the schema
  was being introduced. Left in place, deleting `buf.yaml` from the main branch would have silently
  disabled both the wire-contract check and the compatibility check. It is now a hard failure, and
  removing it is what let the check run against a real base for the first time.
