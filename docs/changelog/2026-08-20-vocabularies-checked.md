---
title: "The build now checks the vocabularies it used to trust you to remember"
tags: [docs, ops]
---

Three closed vocabularies were written down twice — once as a constant the build enforces, once as a
table in a README that people actually read — with nothing asserting the two agreed. `CLAUDE.md`
stated the coupling as a rule, *"adding a tag means editing both"*, which is the honest admission that
nothing enforced it. This repository's own position is that a rule depending on someone remembering
it is not a rule, and there were three instances of the same hope.

### Added

- **The build parses each README table back out and compares it to the constant**, failing with the
  value and the file to fix: *"'in-flight' is enforced but undocumented — add it to
  docs/edrs/README.md"*. It covers the changelog tags, the EDR implementation states and the EDR
  statuses, by one mechanism rather than three — a fix for one that ignored the others would just
  have doubled the inconsistency.
- **Order is compared too**, for the two vocabularies where order is load-bearing: implementation
  states run most-built to least and the roadmap reverses them, and the changelog filter bar renders
  tags in array order. Statuses are a `Set` with no meaningful order and are compared as one.
- **Each table is located by an explicit `<!-- @vocabulary:… -->` marker**, not by guessing which
  table in the file is the vocabulary. A heuristic that silently matched the wrong table would be
  worse than no check, because it would pass while comparing nothing — so a table that loses its
  marker fails the build rather than quietly stopping being checked.

### Fixed

- **`docs/changelog/README.md` claimed the constant was "the only place it is written down"** while
  sitting directly above a second copy of it. The same sentence appeared as a comment in
  `website/build.mjs`. Both now say the vocabulary lives in two places and name what keeps them
  together.
- **The EDR status vocabulary was the third instance**, which the original report did not count. It
  is the same shape and is covered by the same mechanism.

Every failure mode was **seen to fail** before being relied on — a value removed from a README, a
value added to one, the same values reordered, a marker deleted — each producing the intended message
and each restoring cleanly to green.

That was not sufficient. Review found three ways the first version passed *while the vocabularies
disagreed*: an annotated row like `` | `hotfix` (planned) | `` did not match the value pattern and
was silently dropped; a second `<!-- @vocabulary:… -->` marker shadowed the real table, so every
later edit to it would have gone unchecked; and a value listed twice was invisible to a
membership test. Each is now a loud failure of its own, and each was seen to fail before being
believed. The first version had been watched to fail in the four directions its author thought of,
which is exactly how a guard ends up trusted for more than it does — and is the same defect, one
level up, as the arrangement it replaces.
