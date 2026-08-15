---
id: NNNN
title: "Short imperative title"
summary: "One or two sentences saying what the decision is. Required, 240 characters max — it is the abstract on the decision index, so it is read far more often than the record itself."
status: accepted
date: YYYY-MM-DD
authors:
  - "Your Name <you@example.com>"
tags: []
supersedes: null
superseded_by: null
aliases: []                       # prior slugs to redirect from, when renaming
# proposed_until: YYYY-MM-DD      # required only when status: proposed
---

## TL;DR

State the decision in plain language, and the obligations it places on whoever implements it. A
reader should be able to act after reading only this section. A paragraph, maybe two. Save the *why*
for Context and the trade-offs for Consequences.

## Context

What forced a decision? The technical, operational, or organisational pressures at play — facts and
constraints, not solutions. Someone reading this in two years should understand *why this was even a
question*. Cite the Field Note (`ZFN-N`) if this is a standing position being applied, and say so
explicitly if the decision departs from one.

## Decision

One or two sentences, active voice, present tense: "Marque will …". Then enough detail that someone
could implement it without asking. Name the scope limits explicitly — what this decision does *not*
cover is as useful as what it does.

## Consequences

What gets easier. What gets harder. What new obligations this creates — operational, security,
documentation. Be honest; a record with no cost section has not been thought through.

## References

- Pull requests, RFCs, external material. Cite a pull request as a link —
  `[#12](https://github.com/sixfathoms/marque/pull/12)` — never a bare `#12`, which resolves only on
  GitHub and only for a reader who already knows the repository.
- Field Notes: `[ZFN-N](https://zrz.io/zfn/N-slug/)`.
- Related records: `[EDR-NNNN](./NNNN-slug.md)`.

## Changelog

- **YYYY-MM-DD**: Accepted.
