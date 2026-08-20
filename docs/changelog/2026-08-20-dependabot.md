---
title: "Dependabot, configured before the dependency it exists to guard"
tags: [ops, security]
---

`CLAUDE.md` and [EDR-0039](/edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser/) legislate a
rule about `pg_query_go` upgrades that had nothing to attach to: there was no `.github/dependabot.yml`
in the repository, so the rule described a configuration that did not exist. That is the
claim-without-its-mechanism failure this corpus is otherwise careful about, and it is cheapest to
close now — the dependency arrives at M2, and a guard that lands after the first bump is not a guard.

### Added

- **`.github/dependabot.yml`, four update locations across three ecosystems**: `gomod` at `/` and at
  `/tools` — two module graphs on purpose, so the linter's dependencies stay out of the service's —
  `npm` at `/website`, and `github-actions` at `/`.
- **Each location groups its updates, and every group says what it applies to.** One pull request per
  directory per dependency is the pattern that produces a queue nobody reads. The `/tools` graph is
  why this is not optional: buf, golangci-lint and goreleaser carry 537 indirect requirements between
  them.
- **`pg_query_go` is excluded from the Go groups**, so its bump arrives as its own pull request
  rather than one line inside a batch. The dependency does not exist yet; the entry is written first
  deliberately.

### Fixed

- **A group with no `applies-to` covers version updates only.** The first draft of this file grouped
  everything and then claimed in its own comments that security fixes therefore arrived consolidated
  too. They would not have: security updates would have stayed ungrouped, and the `pg_query_go`
  exclusion would not have held for them either — which is the one case where isolating it matters
  most. Every group now declares `applies-to` explicitly, including where the value is the default,
  and the three Go and npm locations carry a parallel `security-updates` group.

**Actions deliberately have no security group, and that is a cost of SHA pinning.** Dependabot does
not raise alerts for an action pinned to a commit SHA, and a security update is driven by an alert —
so the pinning enabled for [#8](https://github.com/sixfathoms/marque/issues/8), which is the right
call and stays, means this ecosystem gets weekly version updates and no Dependabot security updates.
A fourth group would have been inert while reading as coverage. Nobody had written that trade down;
it is written down now.

Two limits are written into the file, because it reads stronger than it is.

**The exclusion covers minor and patch bumps, not the major.** `pg_query_go` uses semantic import
versioning: the module is `…/pg_query_go/v6`, and `/v7` is a *different module path*. Dependabot does
not migrate Go imports across a major suffix, so it will never propose that upgrade at all — with or
without an `ignore` entry, which is why there is not one. The upgrade EDR-0039 cares about most is
therefore the one no tool will raise, and M2 inherits the obligation to watch releases by hand.

**Dependabot cannot require review.** `allow_auto_merge` is false for this repository, which disables
GitHub's native auto-merge — it does not stop an installed app or a workflow holding write permission
from calling the merge API. The invariant *"a `pg_query_go` bump is read by a human"* needs a
branch-protection rule with no automation bypass, and this file is not that rule.
