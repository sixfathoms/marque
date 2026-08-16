---
id: 35
title: "Execution freshness belongs to the approval, not to the executing principal"
summary: "Requiring a fresh interactive authentication to execute against a critical target broke offline execution, locked agents out of the flow escalation exists for, and keyed on a different criticality than another record. It is resolved here."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [identity, policy, execution]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md) required a fresh interactive
authentication for two things. Its recent amendment scoped the first correctly and left the second
untouched: *executing against a target marked `critical`*. That clause contradicts three other parts
of the design, and it is resolved as follows:

- **Freshness is satisfied by the approval.** The approver authenticated freshly and interactively
  when they signed ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) makes that a physical
  act), and the marque's short window bounds how long that remains true. **Executing does not require
  a second interactive authentication.**
- **Where a per-execution human act is genuinely wanted**, it is an explicit target setting
  (`require_execution_presence`, default off), and the proof is an **authenticator assertion with
  user verification** — not a token from an identity provider. That is satisfiable with every network
  down, which is the whole point.
- **Criticality composes as the maximum of target and role**, matching the design's
  narrow-never-widen idiom.
- **An agent cannot satisfy `require_execution_presence`** and that is deliberate: turning it on means
  a human must be at the keyboard for that target, and it is therefore off by default so the agent
  flow works.
- **The Pilot is the enforcement point**, and this is added to what it verifies.

## Context

Found by the expert panel. One unamended clause contradicted three things at once.

**It broke the flagship property.** `architecture.md`'s failure table says "Identity provider down →
already-signed marques still execute". False for a critical target under the old clause. Worse, the
playbook recommends holding pre-issued **grace** marques *specifically for critical targets* — the
exact class for which the requirement is unsatisfiable during the outage they are held for. The
design's most-repeated advantage failed precisely where it was advertised hardest.

**It locked agents out.** [EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) and
[EDR-0019](./0019-escalation-is-a-chain.md) make the agent the marque's `sub` — the agent executes,
the humans authorise. [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md) says a
workload principal can never satisfy freshness. So an agent could never execute against
`prod-primary` — which [EDR-0015](./0015-policy-is-reviewed-configuration.md)'s own example marks
`critical` — even after a fully completed escalation chain. The flagship agent flow failed on the
target class the escalation chain exists to serve.

**It keyed on the wrong thing, twice.** [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)
keys on **target** criticality; [EDR-0006](./0006-every-statement-names-a-role.md) keys on **role**
criticality and defers to EDR-0003. [EDR-0015](./0015-policy-is-reviewed-configuration.md)'s example
has them diverge, and no record composed them. Meanwhile
[EDR-0004](./0004-marques-are-signed-leases.md)'s enumeration of what the Pilot checks omits freshness
entirely, so the enforcement point was unstated as well.

The underlying error is worth naming: **freshness was treated as a property of the moment of
execution, when what it actually protects is the moment of decision.** An unlocked laptop should not
be able to *approve* a production change. Whether the same laptop then runs an already-approved,
narrowly-scoped, expiring grant five minutes later is a different and much smaller question.

## Decision

**Execution requires no interactive authentication.** It requires proof of possession of the key the
marque names ([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)), which is
verifiable offline and needs no identity provider. The approval carried the freshness.

**`require_execution_presence`** is a per-target setting, default **off**. Where a deployment wants a
human physically present at execution — a genuine want for the highest-consequence targets — the
proof is a WebAuthn assertion with user verification, the same ceremony as approval
([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)). Deliberately *not* a fresh token from an
identity provider, because that is the thing that does not work during an incident.

**Criticality composes as `max(target, role)`.** Where a `routine` role is used against a `critical`
target, the target governs; where a `critical` role is used against a `routine` target, the role does.
Narrow, never widen — the same idiom as every other composition in the design.

**Agents and presence.** An agent cannot produce a user-verification assertion, so on a target with
`require_execution_presence` on, an agent cannot execute even with a fully approved marque. **That is
the setting's meaning**, not a side effect: turning it on says "a human must be at the keyboard here".
It is off by default, so the agent flow in
[EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) works as described. A deployment
turning it on for a target its agents use will find out immediately, and the console says so at the
moment the setting is changed rather than at 3am.

**The Pilot enforces it**, from `require_execution_presence` in the signed payload
([EDR-0004](./0004-marques-are-signed-leases.md)) and in the target's entry in the anchored policy
artefact ([EDR-0015](./0015-policy-is-reviewed-configuration.md)). The rule is **monotone**: the
Pilot requires presence if **either** source says so, so a control plane can only ever *add* the
requirement, never remove it — which is what stops it being switched off for a marque already issued.
It also appears in the canonical `display`
([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)), so an approver signing on the
`signing_surface: local` path can see that presence was or was not required.

**Corrections.** `architecture.md`'s failure table and the playbook's grace-marque paragraph are
corrected: already-signed marques execute with the identity provider down, unconditionally, unless
the target requires execution presence — in which case the operator's authenticator still works,
because that is local.

## Consequences

**Easier.**

- The offline-execution property is true without an asterisk, including for critical targets, which is
  what the playbook has been assuming.
- The agent flow works end to end on the targets escalation exists for.
- One composition rule for criticality instead of two records keying on different fields.

**Harder.**

- **Someone will read this as a weakening.** "You removed the freshness check on critical execution"
  is a fair first reaction. The answer is that the check was on the wrong event and was unsatisfiable
  where it mattered most; the deliberate version of it survives as an explicit setting.
- **`require_execution_presence` is a foot-gun for agent deployments** — enabling it silently breaks
  automation that was working. The console warning at change time is the mitigation, and it is a
  warning, not a prevention.
- A signed presence requirement means changing the setting does not affect marques already issued,
  which is correct and will confuse someone during a rollout.

**New obligations.**

- A test executes an already-signed marque against a `critical` target with the identity provider
  unreachable and asserts success.
- A test turns on `require_execution_presence` and asserts an agent-held marque is refused with an
  error that names the setting.

## References

- [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md) — the clause this resolves.
- [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) — user verification as a local,
  network-independent act.
- [EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md) — proof of possession as the
  execution credential.
- [ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/) — incident tooling must not depend on
  what it might need to recover.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that execution freshness contradicted
  offline execution, the agent flow, and EDR-0006's criticality keying.
- **2026-08-16**: Amended after the second panel's synthesis: `require_execution_presence` now exists in EDR-0004's payload and EDR-0015's targets, and is monotone across the two sources.
- **2026-08-16**: Terminology and staleness fix: uses "grace marque" for the pre-issued offline case rather than "break-glass".
