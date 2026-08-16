---
title: "Ideas"
sidebar_position: 4
---

> [!NOTE]
> **Nothing on this page is decided.** These are candidates, not commitments — no record covers any
> of them, and anything that gets built gets a [decision record](/edrs/) first, with its trade-offs
> named. Read [Scope](./scope.md) for what is actually in the first release. This page exists so that
> a good idea is written down once rather than re-derived in three conversations, and so the ones
> that were *considered and rejected* stay rejected for a stated reason.

The ideas worth having are not generic features. They fall out of three things this design has that
nothing else does at once: **a corpus of what humans approved and why**, **a rehearsal engine**, and
**per-relation write measurement**. Everything below leans on at least one of those.

Ranked by what I would build.

## 1. Shadow mode — watch before enforcing

Point Marque at a target and enforce **nothing**. It reads the target's own audit — which it can,
because [per-operator database identity](../../edrs/0021-connections-identity-and-read-routing.md)
already exists — and reports what last month *would* have looked like: what needed approval, what a
delegation would have covered, who would have been paged at 3am, how deep the queue would have got.

**Why it is first.** [Scope](./scope.md) names adoption as the top risk, and this attacks it directly.
Nobody switches on a control that gates production without knowing what it will cost them, and every
honest answer to "how much will this slow us down" is currently a guess. This replaces the guess with
last month, replayed.

It is also the honest version. If the answer is *"your team would have waited forty minutes a day"*,
that is much better learned in shadow than after adoption.

**What makes it hard.** It needs the target's audit to be good enough to reconstruct statements, which
varies by engine and configuration; where it is not, shadow mode can report volume and shape but not
the statements themselves, and must say which it is doing.

## 2. Delegation mining

The logbook is a labelled dataset nobody else can build: **statements humans approved, with the
reasons they gave**. So mine it.

> You approved 47 requests matching this shape in 90 days. Here is a compiled delegation covering 44
> of them — and here are the 3 it would **not** have covered, so you can see what you are giving up.

The output is a *candidate compiled delegation*, which a human reads and signs — so it reuses
[EDR-0016](../../edrs/0016-natural-language-delegations-are-compiled.md) wholesale and introduces no
new authority path. The model proposes; a person signs the compilation; enforcement stays
deterministic.

**Why it matters.** A long queue is the failure mode every other mechanism is defending against, and
this is the only idea here that *shortens* it using evidence rather than intuition. It also gets
better the longer a deployment runs, which is a good property for a second feature to have.

**The trap to avoid.** Showing only what a delegation *would have covered* is salesmanship. The
counterexamples — the requests it would have missed — are the part that makes it a decision rather
than a suggestion, and they must be as prominent as the coverage number.

## 3. Compensating statements

Marque knows the statement, the fence, and — from the rehearsal — the affected rows and their prior
values. For a bounded `UPDATE` it can generate the **revert**, which then goes through approval like
any other request.

**Honest limits, which are most of the design:**

- Only where affected rows were captured and bounded. A four-million-row `DELETE` is not undoable
  this way and should not pretend to be.
- Never where triggers, cascades or rewritten targets ran — and the
  [write-set assertion](../../edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)
  already tells you exactly when that is true, which is what makes the limit checkable rather than a
  warning in a doc.
- The compensation is a **new request**, fenced and approved like anything else. It is not an undo
  button, and calling it one would be the mistake.

**Why it is worth it anyway.** *"I updated three rows and got it wrong at 2am"* is the most common bad
night in this whole problem space, and a one-click **reviewed** revert is worth a great deal to the
person having it.

## 4. Cumulative blast-radius budgets

Every budget today bounds **one marque**
([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)). Nothing bounds the aggregate: ten
individually-reasonable approvals in a day are invisible to every mechanism in the design.

A per-principal, per-target, per-window ceiling — *"at most 10,000 rows across all marques per day"* —
catches death by a thousand approved cuts. The second expert panel gestured at this when it noted that
a real volume bound would have to live at the Pilot, as a counter in the execution ledger. It is not
built, and it is a genuine gap rather than a nice-to-have.

## 5. Evidence bundles

The hard part is already done: anchored rosters, epoch-chained policy, a signed `display`, an external
anchor. So a compliance request becomes an **exportable, independently verifiable bundle**:

> This change, this approver, this policy epoch, this is what they were shown, here is the chain head
> in the external anchor. Verify it yourself with our public keys, with no cooperation from us.

Most audit trails reduce to *trust our database*. This one genuinely does not, and the difference is
the sort of thing that closes a procurement conversation.

## 6. Migration-invalidated delegations

The [machinery fingerprint](../../edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)
already records a relation's rules, triggers and referential actions. So notice when a migration adds
a cascading child to a table somebody holds a delegation on, and tell the grantor that their scope now
means something different from what they signed.

Cheap given what exists, purely operational, and nobody does it — because nobody else is fingerprinting
the relation in the first place.

## Thinner, but written down

- **Rehearsal as a product on its own.** *"What would this statement do?"* is useful outside approval —
  in CI, in review, in a migration dry-run. The engine exists; it is the API that does not.
- **Planned change windows.** Approve now, execute later, inside a window. The marque's `nbf` already
  expresses it; what is missing is the surface that makes it a first-class thing to ask for.
- **Reverse forensics.** Given a row that changed, work backwards: which marques could have produced
  it? The logbook plus the write set make this answerable in a way a database audit alone is not.
- **Approval as a primitive for other systems.** The escalation chain, signed grants and logbook are
  general. This is the honest long form of the non-database-targets deferral in
  [Scope](./scope.md) — and it is a different product, not a feature.

## Two that are already refused

Recorded here so they are decided rather than re-argued.

**Approve from Slack.** Somebody will ask within a week of shipping. The answer is no, and
[EDR-0036](../../edrs/0036-what-is-signed-must-be-what-was-seen.md) already gives the reason: the
signing surface must not be one the control plane renders — and a chat client is strictly worse,
because now a third party renders it too. A WebAuthn challenge is an opaque digest, so *what you see*
and *what you sign* are separable, and the fix is to sign where the code is yours. **Notify in Slack,
always. Sign in Slack, never.**

**"Approve similar" / approval templates.** This is bulk approve wearing a disguise, and
[EDR-0024](../../edrs/0024-the-console-is-for-deciding.md) already refuses the undisguised version. If
a shape is approvable repeatedly, it is a
[standing order](../../edrs/0008-standing-orders.md) — which has parameter constraints, an expiry, a
rate limit and a review. A template is the same convenience with none of the safety, and it would be
adopted much faster precisely because it asks for nothing.
