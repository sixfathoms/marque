---
title: "Three CI tiers, named, with the reason each stops where it does"
tags: [ops, security]
order: -1
---

The [implementation plan](/overview/implementation-plan/) said "CI green on all supported platforms"
while the test suite ran on two of the four. That is the shape of disagreement this repository is
supposed to catch — a claim in prose with a mechanism that does not meet it — so it is settled rather
than left standing.

### Changed

- **"Green in CI" now names three tiers.** Build-and-smoke on all four platforms; the test suite on
  one runner per operating system, which is the boundary the C toolchain follows; integration on
  linux/amd64, because GitHub-hosted macOS runners have no Docker daemon and a testcontainers suite
  cannot run there at all. Each tier carries the reason it stops where it does, and the test tier
  carries **when to revisit it**: M2, when `pg_query_go` puts a C parser in the dependency graph.
- **Both milestone exit criteria now read against those tiers** rather than against a phrase that
  meant whatever the reader assumed.
- **`make breaking` names both causes of an unusable base ref.** It advised fetching more history,
  which is the wrong repair when the ref is the all-zeros SHA a branch creation produces, or the
  orphan a force-push leaves behind. No checkout depth helps there.

### Fixed

- **Every checkout in both workflows now discards the token.** Three CI jobs kept it in
  `.git/config` for the duration of the build. On a public repository with `contents: read` the
  exposure is close to nil; the reason to fix it is that a selectively-hardened workflow reads as
  though the jobs that set the flag needed it for something particular, and none of them do.
- **A latent trap in the Pages upload is now visible at the line that causes it.**
  `upload-pages-artifact` stopped including dotfiles by default at v4.0.0. Nothing the site emits is
  hidden today, so this changes no behaviour — but the first `.well-known/security.txt` would have
  been dropped from the artefact with the build green and the deploy successful.

### Added

- **The repository now requires actions to be pinned to a full-length commit SHA**, enforced by
  GitHub rather than by review. Both workflows already complied, which is what made now the cheap
  moment: the setting applies at run time, so enabling it against a non-compliant workflow breaks CI
  instead of a pull request.
