---
title: "The statement pipeline opens to providers that may narrow or veto"
tags: [policy, security, ops]
order: 3
---

Deployments need statement handling Marque should not hard-code: injecting a tenant constraint,
mapping a column name across a migration in flight, synthesising a value, or asking another system
whether a change freeze is on. Those are organisation-specific and the list is open-ended, so they
become an extension point rather than features.

An extension point in an authorisation system is also where a security property usually goes to die,
so this one is bounded by construction.

### Added

- **[The statement pipeline and provider SPI](/edrs/0028-statement-pipeline-and-provider-spi/)** —
  every statement, from every surface, moves through one named pipeline. Out-of-process **providers**
  may join `transform` (rewrite the statement) and `verify` (inspect and veto, possibly
  asynchronously), plus a veto-only `pre_execute` and a no-veto `observe`.

### The rule

> **A provider may narrow or veto. It can never widen, permit, or replace a check.**

Three mechanisms enforce that instead of asking for it:

- **The digest is taken after transformation**, so the marque signs what will actually run and a
  human approves the rewritten statement with the original shown beside it.
- **Scope and fence re-run on the transformed statement**, so a rogue or buggy transform is bounded
  by the submitter's own authority — it cannot reach beyond what they already had.
- **There is no stage** at which a provider can disable the fence, the magnitude assertion, marque
  verification, the role or the logbook.

The line for deciding what may become a provider: *if disabling it would let something run that
otherwise could not, it is not a provider.*

### Reconciled with EDR-0007

[EDR-0007](/edrs/0007-delegation-by-containment-proof/) refuses to conjoin a delegation's predicate
into an operator's `WHERE`, because silently narrowing a statement produces a partially-applied
change nobody reviewed. Constraint injection is that same operation — and it is allowed here because
it is **visible**: the transform's output is what gets digested, displayed, approved and signed. The
fence could not offer that, because it runs after approval, inside the transaction.

On fast paths where no human sees the individual request, the review has still happened: providers
are declared in reviewed configuration, once, exactly as a standing order is.

### Two subtleties worth knowing

- **Transforms run once and their output is frozen.** Otherwise value synthesis is a correctness bug:
  a provider injecting a timestamp would produce different text at rehearsal and at execution, so the
  rehearsed statement would not be the executed one.
- **A failed transform fails the request.** Never "skip and continue" — a skipped tenant-scoping
  transform is a data breach, and skipping is exactly what a tired operator would configure.

### Moved onto the SPI

The analyser and the Surveyor both become providers. Neither loses anything: the analyser was already
authority-free, and the Surveyor's two outcomes were already exactly `veto` and `refer`.
