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

- **`.github/dependabot.yml`, covering the four ecosystems that exist**: `gomod` at `/` and at
  `/tools` — two module graphs on purpose, so the linter's dependencies stay out of the service's —
  `npm` at `/website`, and `github-actions` at `/`.
- **Every ecosystem updates as one grouped pull request.** One per directory per dependency is the
  pattern that produces a queue nobody reads, and an unread queue is a security problem rather than
  an annoyance, because security updates arrive by the same path. The `/tools` graph is the reason
  this is not optional: buf, golangci-lint and goreleaser between them pull in several hundred
  indirect modules.
- **`pg_query_go` is excluded from the Go group**, so its bump always arrives as its own pull request
  rather than one line inside a batch. The dependency does not exist yet; the entry is written first
  deliberately.

### Changed

- **Nothing about how actions are pinned.** They already carry a full-length commit SHA with a
  version comment beside it, and Dependabot updates the two together — which is what makes those
  comments load-bearing rather than decoration.

The `pg_query_go` rule is deliberately **not** an `ignore` entry. Ignoring major bumps would stop
Dependabot proposing the upgrade that most needs a human to know it exists, including a
security-motivated one — and a major is not the risky case anyway: EDR-0039's hazard is a newer
grammar admitting statements the old one refused, and a minor does that just as well. The rule is
that the bump is *reviewed*, not that it is invisible.

Worth stating plainly, because the configuration alone reads stronger than it is: **Dependabot cannot
enforce "never auto-merge".** That is a repository setting, and the two have to agree. Today
`allow_auto_merge` is false for the whole repository, so nothing merges itself and the exclusion is
sufficient. If auto-merge is ever enabled, this file stops being enough and a branch-protection rule
has to carry the weight.
