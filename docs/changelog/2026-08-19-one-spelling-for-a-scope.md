---
title: "One spelling for a scope, and canonicalisation that normalises nothing"
tags: [policy, security, docs]
---

The corpus agreed on the decision and disagreed on the encoding. Every record said a delegated row
scope is checked by syntactic containment and never by entailment; they then wrote the field down two
different ways, and specified a check that compares one against the other. This is the second time
that shape has surfaced — the first was a record claiming another had gained a field it had not — and
it is worth naming, because both times the disagreement was invisible to every reader who already
knew what the design meant. Only someone reading the artefacts as an implementer would see it.

### Added

- **[EDR-0041](/edrs/0041-one-spelling-for-a-scope/) picks the spelling.** A fence is a list of
  conjuncts everywhere; a relation is a `schema` field and a `relation` field; an operation is
  lowercase. Four asymmetries are recorded as **deliberate** — including a vector's `predicate`
  staying a single string, because a vector's predicate and a grant's fence are composed rather than
  compared.
- **"Canonicalisation" in check 7 is defined, and it normalises nothing.** It folds no whitespace, no
  identifier case, no ordering and no Unicode form: two conjuncts are equal when their decoded
  characters are equal. Deciding that two different texts mean the same predicate is the parser's
  job, and routing a fence comparison through the grammar would let a `pg_query_go` upgrade change
  what an **already-signed** delegation permits. (Statement canonicalisation, which
  [EDR-0004](/edrs/0004-marques-are-signed-leases/) calls part of the security boundary, is a
  different question and is still open.)
- **A conjunct must parse standalone, and the Pilot checks it before composing.** Wrapping an element
  in parentheses is only sound if the element is one expression: `tier = 'sandbox') OR (1=1` survives
  wrapping, erases the tier bound, and is identical to the artefact it came from.

### Changed

- **The structural rules are Pilot refusals, not authoring conventions.** An empty fence array, a
  duplicate conjunct, an empty-string conjunct and a malformed one are refused where they are
  verified rather than where they are written — a rule the Harbourmaster enforces on itself is not a
  rule. What they buy is stated narrowly: they are for the reader, not against an adversarial author,
  who would simply omit the key, which is legitimately no row restriction. `"fence": []` looks like a
  restriction and is none.
- **Seven records, the agents page and the conformance format were amended**, each record with a
  dated changelog line. No record is superseded: every decision stands, and only its encoding was
  wrong.
- **The grantor of a compiled delegation signs a list rather than a predicate.** A legibility cost
  landing on the record whose whole subject is the person signing, so it is named there.

### Fixed

- **The fence SQL had reopened a fail-open the corpus already closed once.**
  [EDR-0007](/edrs/0007-delegation-by-containment-proof/)'s worked transaction reads
  `AND (fence) IS NOT TRUE`, which is correct for one conjunct and wrong for two: `IS` binds tighter
  than `AND`, so `(c1) AND (c2) IS NOT TRUE` tests `c2` alone and a row failing `c1` is never
  counted. The record now says what `<fence>` denotes — the bare conjunction, wrapped by the template
  — and the rule is in M5's exit criteria and in `CLAUDE.md`'s invariant list rather than only in the
  record that introduced it. The session settings failed in two further ways:
  `standard_conforming_strings` and
  `backslash_quote` are read by the lexer, so a `SET` sent in the same message is inert while the GUC
  still reads back correct; and a `BEFORE` trigger on the target can call `set_config` to move
  `search_path` out from under every check that runs after the operator's statement.
- **A relation had three spellings, not the two reported.**
  [EDR-0037](/edrs/0037-emergency-paths/) had already split the field and named the second half
  `table`. Every grant now reads `{ "schema": …, "relation": … }`.
- **[EDR-0007](/edrs/0007-delegation-by-containment-proof/) stopped contradicting itself.** Its
  attenuation rule called the fence an array eleven lines below an example showing a string.

Six questions surfaced by writing the encoding down, and each is left open here because settling it
changes a rule a different record decided. An agent's effective fence is the **union** of three
conjunct sets, so it is tighter than its delegation's and an identity check refuses it for being
tighter ([#20](https://github.com/sixfathoms/marque/issues/20), Phase 3b). A wildcard relation is
spelled `"*"`, which PostgreSQL also accepts as a real relation name
([#22](https://github.com/sixfathoms/marque/issues/22), Phase 3, where fence-bearing grants land). The signed `display` renders a fence and now
has a list to render ([#23](https://github.com/sixfathoms/marque/issues/23), M3). The subset version a
delegation is pinned to has no carrier on any artefact
([#24](https://github.com/sixfathoms/marque/issues/24), M2). And what a conjunct may *reference* —
functions, casts to domains, subqueries, explicitly-qualified operators — is bounded by nothing at
all ([#25](https://github.com/sixfathoms/marque/issues/25), M5); that one is largely pre-existing and
is the reason to say plainly that this record bounds a conjunct's shape and not its behaviour. And
break-glass lists the fence among the controls it leaves unchanged while no signed artefact on that
path carries one ([#26](https://github.com/sixfathoms/marque/issues/26), Phase 2).

Until the first is settled an agent has no fast path, which is the fail-closed answer and is written
down so nobody reaches for the other one.
