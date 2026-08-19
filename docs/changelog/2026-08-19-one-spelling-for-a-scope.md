---
title: "One spelling for a scope, and canonicalisation that normalises nothing"
tags: [policy, security, docs]
order: -1
---

A fence was an array of conjuncts in the artefacts a Pilot verifies and a single predicate string in
the grants authority descends from. Two specified checks compare across that split, and both are on
paths where nobody is watching: the fast-path chain check runs with no human present at mint time,
and the attenuation test runs once per hop of a delegation chain. An undefined comparison between a
one-element array and a string resolves permissively far more often than not, which is the exact
error both records say they exist to avoid.

### Added

- **[EDR-0041](/edrs/0041-one-spelling-for-a-scope/) picks the spelling and defines the comparison.**
  A fence is an array of conjuncts everywhere; a relation is a `schema` field and a `relation` field
  rather than one dotted string; an operation is lowercase. Three asymmetries are written down as
  deliberate, so they are not refiled later as defects.
- **"Canonicalisation" now means nothing at all.** [EDR-0029](/edrs/0029-the-fast-path-authority-chain/)
  check 7 said "syntactic identity after canonicalisation" and no record said what canonicalisation
  was. It folds no whitespace, no identifier case and no ordering: two conjuncts are equal when their
  bytes are equal. Every normalisation rule is a claim that two different texts denote the same
  predicate, and deciding that is the parser's job — which is what a syntactic rule exists to keep
  out of an offline Pilot.
- **A fence's elements are parenthesised individually when it becomes SQL.** Writing `c1 AND c2`
  rather than `(c1) AND (c2)` lets a conjunct containing a top-level `OR` rebind against the
  following `AND`, and the fence comes out **wider than it was written**. Same fail-open shape as the
  `NOT (fence)` bug the corpus already corrected once; it only became reachable when the fence became
  a list.

### Changed

- **Five records and the agents page now carry the winning spelling**, each with a dated changelog
  line: the delegation ([EDR-0007](/edrs/0007-delegation-by-containment-proof/)), the standing order
  ([EDR-0008](/edrs/0008-standing-orders/)), the compiled delegation
  ([EDR-0016](/edrs/0016-natural-language-delegations-are-compiled/)), an agent's declared scope
  ([EDR-0018](/edrs/0018-agents-are-submitters-under-intersected-scope/)) and the break-glass grant
  ([EDR-0037](/edrs/0037-emergency-paths/)). No decision is superseded — every one of them stands
  exactly as it did, and what was wrong was how it was written down.
- **The grantor of a compiled delegation now signs a list rather than a predicate.** That is a real
  legibility cost landing on the record whose entire subject is the human doing the signing, and it
  is named there rather than absorbed quietly.

### Fixed

- **[EDR-0007](/edrs/0007-delegation-by-containment-proof/) stopped contradicting itself.** Its
  attenuation rule said "the `fence` array is conjunctive" eleven lines below a worked example
  showing a string. Had the string been the authored form, recovering its conjuncts would have meant
  parsing SQL — putting `libpg_query` inside the offline authority path, an architectural consequence
  no record states.
- **A relation had three spellings, not the two that were reported.**
  [EDR-0037](/edrs/0037-emergency-paths/) had already split the field in two and named the second half
  `table`, so the corpus disagreed with itself twice over.

Making the spellings agree exposed a question neither
[EDR-0018](/edrs/0018-agents-are-submitters-under-intersected-scope/) nor
[EDR-0029](/edrs/0029-the-fast-path-authority-chain/) had stated: an agent's effective fence is the
**union** of three conjunct sets, so it is strictly tighter than the delegation it descends from — and
an identity comparison refuses it for being tighter. That is
[issue #20](https://github.com/sixfathoms/marque/issues/20), and it is deliberately not settled here,
because settling it changes a rule EDR-0029 decided.
