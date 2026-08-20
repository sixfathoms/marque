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

That was not sufficient, twice. Review made the build pass *while the vocabularies disagreed* in
seven distinct ways across two rounds — an annotated row the value pattern did not match; a duplicate
marker shadowing the real table; a value listed twice, invisible to a membership test; a row with a
leading space, which GFM permits; a row with no leading pipe, which GFM also permits; a value
smuggled into the header row, which was skipped unconditionally; and a marker with a decoy table
inside a fenced code block.

The root cause of the last four is one decision: **the table was inferred by matching lines that
begin with `|`.** That is not what a markdown table is, and the build already had a real parser
imported for other purposes. The check now reads the **AST** — a marker is only a marker if it is a
top-level HTML node, so a fence cannot hold one; the header is a row like any other and is checked
rather than skipped; and the shape of a row is a fact about the tree rather than a guess about the
text.

A third round found two more, and they are the reason a value is now matched against an explicit
lowercase pattern rather than trimmed: `String.trim()` strips U+FEFF and NBSP but leaves U+200B
alone, so an invisible character inside a value was accepted, rejected, or silently normalised
depending on which invisible character it was — three behaviours, none of them chosen. And a second
table immediately after the vocabulary one, which reads to a person as more of the same vocabulary,
went unchecked.

The lesson is not that the mutations were wrong. It is that they were chosen from the same mental
model that wrote the code, so they tested the four failures its author had thought of — which is
exactly how a guard ends up trusted for more than it does, and is the same defect one level up as
the arrangement it replaces. All twelve directions now fail, and are recorded here so the next
person to extend this knows which ones were not obvious.
