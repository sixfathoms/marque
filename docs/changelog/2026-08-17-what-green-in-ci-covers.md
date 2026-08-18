---
title: "Three CI tiers, named, with the reason each stops where it does"
tags: [ops, security, docs]
order: -1
---

The [implementation plan](/overview/implementation-plan/) said "CI green on all supported platforms"
while the test suite ran on two of the four. That is the shape of disagreement this repository is
supposed to catch — a claim in prose with a mechanism that does not meet it — so it is settled rather
than left standing.

### Added

- **Each test-tier runner now asserts which platform it is.** The plan names the tier's platforms, so
  something has to hold that true: `macos-latest` has already migrated from Intel to ARM once, and
  without the assertion the table would go quietly false while CI stayed green. It is the guard the
  snapshot job already had, for the same reason.
- **The repository requires actions to be pinned to a full-length commit SHA**, enforced by GitHub
  rather than by review. Both workflows already complied, which is what made now the cheap moment:
  the setting applies at run time, so enabling it against a non-compliant workflow breaks CI instead
  of a pull request.

### Changed

- **"Green in CI" now names three tiers.** Build-and-smoke on all four platforms; the test suite on
  one runner per operating system, as a sample rather than a proof; integration on linux/amd64,
  arriving with M1. Each tier carries the reason it stops where it does — and the test tier says
  plainly that it is a cost decision, because two attempts to justify it by argument were both
  false.
- **Both milestone exit criteria read against those tiers** rather than against a phrase that meant
  whatever the reader assumed — and **M2's exit criterion now carries the obligation to widen the
  test tier**, since M2 is where the parser becomes C and the person implementing it will read M2's
  criterion rather than a table three sections earlier.
- **`make breaking` names both causes of an unusable base ref.** It advised fetching more history,
  which is the wrong repair when the ref is the all-zeros SHA a branch creation produces, or the
  orphan a force-push leaves behind.

### Fixed

- **Every checkout in both workflows now discards the token.** Three CI jobs kept it available for
  the duration of the build. On a public repository with `contents: read` the exposure is close to
  nil; the reason to fix it is that a selectively-hardened workflow reads as though the jobs that set
  the flag needed it for something particular, and none of them do.
- **A latent trap in the Pages upload is now visible at the line that causes it.**
  `upload-pages-artifact` stopped including dotfiles at v4.0.0, and the pinned v5.0.0 is what added
  an input to override it. Nothing the site emits is hidden today, so this changes no behaviour — but
  the first `.well-known/security.txt` would have been dropped from the artefact with the build green
  and the deploy successful.
