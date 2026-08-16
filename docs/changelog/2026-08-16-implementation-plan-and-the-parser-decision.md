---
title: "An implementation plan, and the parser the grammar rests on"
date: 2026-08-16
tags: [docs, security]
---

Phase 0 closes with the two things that were missing between a design and a build: what parses a
statement, and in what order the rest gets made.

**[EDR-0039](/edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser/) — the checkable grammar is
parsed by PostgreSQL's own parser.** Seven records leaned on "the checkable grammar" without naming
what implements it; it was the most load-bearing unnamed component in the design. It is
`libpg_query` — the server's real grammar — in every component that parses, rather than a
re-implementation that can disagree with the server about what a statement does.

The record is careful about *where* that matters, because overstating it would justify the wrong
thing: the CLI and the proxy are untrusted and everything they decide is re-checked, so a
client-side parser error produces a refusal rather than a breach. Soundness needs the real grammar
only in the Harbourmaster and the Pilot. Using it on the client too is a maintenance argument — one
allowlist instead of two kept in step — and it is stated as one. The cost is `CGO_ENABLED=1` in every
binary, including the one that ships to laptops, which means native release runners instead of
cross-compilation. That is the largest cost of the decision and it is named as such.

The sharpest new obligation: **a parser upgrade is a reviewed change on the order of a schema
migration.** A newer libpg_query can parse a statement the previous one could not, which silently
widens the checkable subset — and therefore widens what an already-signed delegation permits, which
[EDR-0007](/edrs/0007-delegation-by-containment-proof/) forbids.

**[Implementation plan](/overview/implementation-plan/).** Eight milestones from an empty repository
to the Phase 1 exit criterion, each with the test that proves it. It is shaped by three choices:
Phase 1 ends at a local PostgreSQL and deploys nowhere; it is built by one person alongside other
work, so steps are sized to finish in a sitting and sequenced strictly; and the grammar is built
second, because six of the eight milestones stand on it.

Two things in it worth naming here. The walking skeleton is **deliberately insecure** and contained
by construction — it refuses to start without an explicit environment variable, and the milestone
that deletes that variable also un-skips a test asserting the path is gone. And every milestone's
exit criterion must be a test that has been *seen to fail* for the right reason, because a guard
nobody has watched fail is a guard nobody knows works.

There is also a **decision debt** list: four decisions Phase 1 will force, each of which becomes a
record before the milestone that needs it closes.
