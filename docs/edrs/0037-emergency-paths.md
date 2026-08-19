---
id: 37
title: "An emergency changes who is asked and how loudly, not what is checked"
summary: "Urgency reroutes and pages without widening scope. Break-glass is a pre-granted, dormant scope that activates only on an explicit act with a bound justification, mints an ordinary fast-path marque, and is very loud."
status: accepted
implementation: none
date: 2026-08-16
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, product, security, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Two emergency paths, deliberately different from each other.

**`--urgent`** changes **routing and volume**: every stage is notified at once rather than in
sequence, the deployment's on-call integration is paged rather than messaged, and the target's
`emergency_approvers` join the eligible set. It never widens scope. Whether it may **collapse a
multi-stage chain to one** is a per-target policy setting, `urgency_may_collapse_stages`, defaulting
to **off**.

**Break glass** is a **pre-granted scope that lies dormant**. Someone with authority grants you, by
name, a scope you may use *only* by explicitly breaking the glass — typing a justification, confirming
deliberately, and producing an authenticator assertion. That grant is a signed artefact, so breaking
the glass mints an **ordinary fast-path marque**
([EDR-0029](./0029-the-fast-path-authority-chain.md)) and the Pilot needs no special case: the human
signed the shape in advance, exactly as with a standing order.

The grant is per-actor and its shape is the deployment's to choose:

- *"Theo may run any statement if he breaks glass."*
- *"Theo may run any statement in an emergency **if a second holder co-signs**."*
- *"Theo may run any `UPDATE` on `public.*` in a break-glass scenario."*

And it is **loud by construction**: the moment the glass breaks, the deployment channel, the target's
owners and the whole normal chain are notified — naming the person and quoting the justification —
with a distinct logbook entry, a banner in the console for as long as the marque lives, and a
**mandatory post-hoc review**.

**An agent can never break glass**, for the same mechanical reason it can never approve: it cannot
produce a user-verification assertion.

## Context

The design has been consistently good at refusing and consistently poor at the 3am case. An operator
watching an outage with a fix in hand and no approver awake will route around any control that cannot
answer them — and the thing they route around it *with* is a standing credential in a password
manager, which is the failure this whole system exists to remove
([ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/)).

So an emergency path is not a concession; **it is the feature that makes the ordinary path
survivable.** A control with no emergency path is a control with an undocumented one.

The hazard is equally clear, and it is not "someone abuses it once". It is **drift**: the emergency
path is faster, so it becomes the normal path, and six months later the approval queue is decorative.
Every mechanism below is aimed at drift rather than at abuse — loudness, review, expiry, and metrics
that make routine use visible as routine.

Two smaller traps, both worth naming because they are the obvious designs:

**Urgency must not lower the requirement by default.** If `--urgent` reduces a two-stage chain to one,
everyone marks everything urgent within a month and the second stage stops existing. The second stage
is there precisely because the first person's authority was insufficient
([EDR-0019](./0019-escalation-is-a-chain.md)), so collapsing it lets urgency *manufacture* authority
nobody granted. It is available as a per-target setting because some deployments genuinely want it,
and it is off unless someone chose it in reviewed configuration.

**Break-glass must not be a mode the software can enter.** If it is a flag that skips a check, then
the code path that skips the check exists, and the only thing between it and everything else is
correctness. Making it a *pre-granted signed scope* means there is no skip: the same verification runs
on the same artefact family, and what changed is that a human signed the shape earlier instead of
signing the instance now.

## Decision

### Urgency

`marque submit --urgent --reason "…"`, or `\urgent` in the shell. A reason is required and is
recorded.

| Effect | Detail |
|---|---|
| Notification | every stage at once, not sequentially |
| Channel | pages via the deployment's on-call integration, not a chat message |
| Eligibility | the target's `emergency_approvers` join every stage's eligible set |
| Scope | **unchanged, always** |
| Stages | unchanged, unless `urgency_may_collapse_stages` is on for that target |

Where collapse *is* enabled, the chain becomes a single stage drawn from `emergency_approvers`, the
marque records that it was collapsed, and the request joins the same post-hoc review queue as a
break-glass. A collapsed chain is an authority decision made by a setting, so it is reviewed like one.

### The break-glass grant

A signed artefact in the same family as a standing order
([EDR-0008](./0008-standing-orders.md)) and a compiled delegation
([EDR-0016](./0016-natural-language-delegations-are-compiled.md)):

```jsonc
{
  "id": "bgg_01JB…",
  "to": "theo@acme.example",              // a named principal, never a group
  "target": "prod-primary",
  "role": "settings_writer",              // or "*" where policy permits
  "scope": { "operations": ["update"],
             "objects": [ { "schema": "public", "relation": "*" } ] },   // or "any"
  "co_sign": "none",                      // | "any_break_glass_holder" | "group:data-oncall"
  "max_ttl": "15m",
  "not_after": "2026-11-30T00:00:00Z",
  "granted_by": "sam@acme.example",
  "issued_at": "…", "roster_epoch": 47
}
```

- **Named principals only.** A group cannot hold a break-glass grant: the point is that a specific
  person accepted a specific responsibility, and "whoever is in this group tonight" is not that.
- **`scope` may be `any`**, where policy's `may_grant_unbounded_break_glass` permits it. That is the
  widest thing in the system and it is deliberately a separate permission to grant, not a value
  someone can type.
- **`co_sign`** turns the break-glass into a one-stage chain: a second holder's signature is required
  before the marque is valid. This is the *"any query, if a second person signs off"* shape, and it
  costs one person's attention rather than a full approval round.
- **`max_ttl` is short** — minutes. A break-glass marque that outlives the incident is a standing
  credential.
- **`not_after` is required.** The grant itself expires and is renewed deliberately
  ([ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/)).

### Breaking the glass

Three deliberate acts, in this order, none skippable:

1. **An explicit break.** `marque psql --break-glass`, or `\breakglass` mid-session. Not a fallback
   the client takes automatically when approval is slow — the operator says it.
2. **A justification**, free text, minimum length enforced, and **bound into the signed payload** so
   it cannot be added, edited or lost afterwards. "Why" is the only durable artefact an emergency
   leaves behind.
3. **An authenticator assertion with user verification**, the same physical act as approving
   ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)) — because this *is* an authorisation,
   and it must be as hard to do accidentally.

The result is a marque with `auth.kind: "break_glass"`, `auth.artefact: bgg_…`, and `justification`
in the signed payload. Everything else is unchanged: the fence, the write-set assertion, the
magnitude assert, the nonce, the role's own grants, the logbook. **Break-glass removes the wait, not
the checks.**

### Loudness

The requirement the operator asked for, stated as mechanism:

- **At the moment the glass breaks** — not on completion — the deployment channel, the target's
  owners, and every stage of the chain that *would* have been asked are notified, naming the person
  and quoting the justification verbatim.
- A distinct logbook kind, `break_glass.used`, carrying the grant, the justification, the statement
  and the marque ([EDR-0012](./0012-the-logbook-is-append-only.md)).
- The console shows an **active banner** for as long as any break-glass marque is live in the
  deployment, on every page.
- **Post-hoc review is mandatory**, with an owner and a deadline. An unreviewed break-glass past its
  deadline is a reported finding, and — above a configurable count — **automatically suspends the
  grant** until reviewed. This is the same shape as the Tier-B sampled audit
  ([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)), and for the same reason: it is
  what makes the mechanism correctable rather than merely bounded.
- **Break-glass rate per principal is a first-class metric.** A grant used routinely is not an
  emergency capability; it is a delegation somebody should have written properly, and the playbook
  says to go and write it.

### What break-glass is not

- **Not available to agents.** No user-verification assertion, no break-glass, under any
  configuration ([EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md)).
- **Not a way past the role.** The database's grants remain the outer bound
  ([EDR-0006](./0006-every-statement-names-a-role.md)). A break-glass grant of `any` means *any
  statement the role can execute*, which is why role narrowness still matters.
- **Not usable with the control plane down.** The grant is verified offline, but *minting* needs the
  control plane, so this does not solve that case; a pre-issued `grace` marque still does
  ([EDR-0004](./0004-marques-are-signed-leases.md)).
- **Not silent, ever.** There is no configuration that suppresses the notification. A deployment that
  wants quiet emergency access wants a standing credential, and should say so.

## Consequences

**Easier.**

- The 3am case has an answer inside the system, which is what stops it being answered outside the
  system.
- An organisation can express its actual policy per person — narrow scope for most, `any` with a
  co-signature for a few, unbounded for the one person who genuinely needs it — rather than choosing
  one setting for everybody.
- Urgency is useful without being corrosive, because it moves attention rather than lowering bars.

**Harder.**

- **This is the second most dangerous feature in the system**, after Tier-B surveying, and it is
  more dangerous in practice because it is *convenient*. Every mechanism above exists to fight drift,
  and none of them prevents an organisation from granting unbounded break-glass to twenty people and
  calling it operations.
- **Loudness has a cost.** Pages wake people; a banner is alarming. That is intended, and a deployment
  that finds it annoying is a deployment using break-glass too often — which is the signal working.
- **`scope: "any"` is genuinely unbounded within the role**, and no mechanism here narrows it. It is
  gated by a separate grant permission and by review, which is policy rather than enforcement.
- Post-hoc review is another queue somebody must actually read, and an unread one removes the
  mitigation exactly as it does for Tier B.

**New obligations.**

- Break-glass grants are reviewed on a schedule with their scope and their usage count side by side;
  a grant with no uses in a quarter should be removed, and one with many should become a delegation.
- A test asserts an agent principal cannot break glass, and that no configuration suppresses the
  notification.

## References

- [ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/) — a control with no emergency path has
  an undocumented one.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — the grant expires.
- [EDR-0029](./0029-the-fast-path-authority-chain.md) — the artefact family this reuses, which is why
  no new verification case exists.
- [EDR-0019](./0019-escalation-is-a-chain.md) — the chain urgency reroutes.
- [EDR-0038](./0038-a-request-is-a-shareable-watchable-object.md) — how an operator sees and shares
  any of this.

## Changelog

- **2026-08-16**: Accepted.
- **2026-08-19**: Amended for the artefact spelling ([EDR-0041](./0041-one-spelling-for-a-scope.md)): the grant's `operations` are lowercase, and its relation field is named `relation` rather than `table`. The wildcard is unaffected — it is a value of the field, not a different shape.
