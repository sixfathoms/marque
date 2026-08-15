---
title: "The cast"
sidebar_position: 1
---

Marque's components are named as archetypes rather than as machinery, in one coherent register: a
ship arriving at a port it may not enter unaided. The practice, and its costs, are
[ZFN-43](https://zrz.io/zfn/43-name-systems-as-archetypes/).

This page is the cast list that note requires. It is **an architecture document, not decoration**:
each entry says what the component is for, what temperament it has, and — most usefully — what it
would never do. When a proposed feature feels wrong for a character, that is a real architectural
signal, not a poetry problem. Either it belongs elsewhere, or the component is quietly becoming two
components.

There is an onboarding tax to names like these, and this page is how it gets paid. Nobody searching
for "approval service" finds the Harbourmaster. The plain-language description is always beside the
name, deliberately.

## Why a letter of marque

A **letter of marque** was a commission issued by a state to a private captain: authority to act on
the state's behalf, **scoped** to named waters and named adversaries, **time-bounded**, and
**revocable**. It was carried aboard, it was shown when challenged, and it was worthless once it
expired.

That is exactly the object at the centre of this system, and the name is doing work rather than
being clever. A marque cannot be open-ended, cannot be transferable, and cannot outlive its expiry —
not because a rule forbids it, but because a thing that did those would not be a marque. When a
proposal starts to sound like a standing credential, the name has already objected.

## The cast

<div class="cast">
<div class="cast-member">
<h4>Harbourmaster</h4>
<p class="role">Control plane</p>
<p>The authority that decides what may move. It holds requests, policy, delegations and the logbook;
it countersigns marques; it publishes the deployment's configuration and its revocation list.
Deliberate, procedural, and slow where slowness is correct.</p>
<p class="never"><b>Never:</b> connects to a target database, holds a target credential, executes a
statement, or signs a marque alone.</p>
</div>

<div class="cast-member">
<h4>Pilot</h4>
<p class="role">Data plane</p>
<p>The licensed expert who comes aboard and takes the helm through water the master cannot navigate
alone — under the master's authority, for one passage, and then leaves. It holds the credentials and
makes the connection. It verifies a marque by computation and executes exactly what the marque
names.</p>
<p class="never"><b>Never:</b> decides whether something may run, keeps a session open beyond a
passage, offers a shell or a forwarded port, or holds an opinion about policy.</p>
</div>

<div class="cast-member">
<h4>Leadsman</h4>
<p class="role">Advisory</p>
<p>The hand at the bow swinging the lead, calling the depth ahead so the master can decide — <i>"by
the mark, six fathoms."</i> It reads a request, reports what it touches and what a rehearsal
measured, and writes the summary an approver reads at two in the morning. It calls the water; it
does not steer.</p>
<p class="never"><b>Never:</b> approves, denies, alters a statement, executes anything, produces a
risk score, or holds any authority a configuration could widen.</p>
</div>

<div class="cast-member">
<h4>Surveyor</h4>
<p class="role">Conformance</p>
<p>The marine surveyor who inspects against a standard and certifies conformance — or declines to,
and refers you onward. Given a scope a human already signed and a statement in hand, it answers one
closed question: does this conform? Pedantic, literal, unimaginative, and conservative by
construction — it never has a view on whether something is a <i>good idea</i>.</p>
<p class="never"><b>Never:</b> widens a scope, denies anything, alters a statement, produces a
score, or resolves an uncertainty toward yes. Its only two answers are <i>conforms</i> and
<i>refer to a human</i>.</p>
</div>

<div class="cast-member">
<h4>Tender</h4>
<p class="role">Transport</p>
<p>The small boat that ferries between shore and a vessel that cannot come alongside. It carries
what it is given, to the vessel it is told, and understands none of it. It exists so a target with
no inbound route can still be served.</p>
<p class="never"><b>Never:</b> reads what it carries, terminates a session, holds a key, caches,
retries, or grows a feature.</p>
</div>
</div>

## Two models, two temperaments

The Leadsman and the Surveyor are both language models and they are deliberately different
characters, because they do different jobs and the difference is what keeps them safe.

The **Leadsman** is discursive and helpful. It explains, summarises, and asks the question the
approver had not thought of. It holds no authority at all, which is exactly why it is allowed to be
expansive ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)).

The **Surveyor** is the opposite: narrow, literal, and deeply boring. It answers one question with
one of two words, inside a bound a human signed, and refers on any doubt
([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)).

If either starts sounding like the other, something has gone wrong. A Leadsman that begins
recommending is drifting toward authority it must not have; a Surveyor that begins explaining is
being asked to exercise judgment it was specifically not given.

## Vocabulary

Archetypes are for the long-lived components with real agency — five of them. Everything else is named plainly,
because mythologising everything devalues the names that matter. Three domain terms earn their
keep — the rest of the vocabulary is deliberately boring.

| Term | Means |
|---|---|
| **marque** | The signed grant. The central object ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)). |
| **standing order** | A statement approved once and invoked with constrained parameters. Naval standing orders are persistent instructions that need no fresh authorisation ([EDR-0008](../../edrs/0008-standing-orders.md)). |
| **logbook** | The append-only journal. A ship's log is contemporaneous, signed, and not rewritten ([EDR-0012](../../edrs/0012-the-logbook-is-append-only.md)). |
| request, analysis, execution, delegation, target, role, fence, budget, task, escalation chain | Exactly what they sound like. No metaphor. |

## When a name stops telling the truth

A name that no longer fits is worse than a plain one, because it actively misleads. Two named
tripwires:

- **If the Pilot ever needs to decide something**, it has become a second control plane, and the
  privilege split that is the whole security argument
  ([EDR-0005](../../edrs/0005-control-plane-holds-no-credentials.md)) has quietly gone.
- **If the Leadsman's output is ever consulted by the approval path**, it is no longer advisory, and
  the answer is not to rename it — it is
  [EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md).
- **If the Surveyor ever gains a third possible answer**, it has stopped being a router and started
  being a judge. Two outcomes is not a simplification of the design; it *is* the design
  ([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)).

Renaming has a migration cost and you pay it anyway. Splitting is usually the better answer:
a character that grew a second unrelated job was always two characters.
