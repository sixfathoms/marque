---
id: 34
title: "Give the whole Pilot API an authorisation model, not just Execute"
summary: "Rehearse and Introspect are statement-execution paths with no stated caller check, so a compromised control plane has an exact-count oracle over every target. Every Pilot method now verifies a submitter signature."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, architecture]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0005](./0005-control-plane-holds-no-credentials.md) claims "an attacker who owns the entire
Harbourmaster gets no database access". That is false as written. `pilot.proto`
([EDR-0020](./0020-one-schema-generates-every-client.md)) exposes `Rehearse`, `Execute`, `Introspect`
and the revocation list, and **only `Execute` has a stated caller check**. By design the Harbourmaster
requests rehearsals ([EDR-0010](./0010-rehearse-before-you-sign.md)), so a compromised control plane
can run attacker-chosen statements against every target — rolled back, but returning
`rows_affected`.

That is an **exact-count oracle over arbitrary predicates**: `SELECT … WHERE email = 'x' AND
substr(hash,1,1) = 'a'` extracts data a row at a time, without committing anything. Plus
`duration_ms` as a timing channel, plus — through the compiler path in
[EDR-0016](./0016-natural-language-delegations-are-compiled.md) — column names, types and the
distinct values of low-cardinality columns.

The fix is one authorisation model for the whole API:

- **Every Pilot method verifies a submitter signature** over the request, from a named principal,
  against the caller. The Harbourmaster relays; it does not authorise.
- **Introspection is re-checked against a Pilot-held allowlist**, so classifying a statement as
  harmless is not something the control plane can assert.
- **Redaction is applied at the Pilot from Pilot-held configuration**, not from control-plane policy.
- **`rows_affected` and `duration_ms` are disclosure**, quota'd per principal and recorded.

And [EDR-0005](./0005-control-plane-holds-no-credentials.md)'s claim is corrected to what is true: no
credential and no ability to commit a change — a bounded, quota'd, target-visible read channel
remains.

## Context

Found by the expert panel. The design spent a great deal of care on who may *change* data and almost
none on who may *ask questions of* it, even though both go through the same component and the same
connection.

The gap follows from a reasonable-looking division of labour. The Harbourmaster orchestrates: it asks
for a rehearsal, it asks for schema for the compiler, it decides whether a statement counts as
introspection ([EDR-0027](./0027-be-psql-then-be-better-than-psql.md)). Each of those is the control
plane telling the Pilot to do something, and the Pilot obliging — which is exactly the relationship
[EDR-0005](./0005-control-plane-holds-no-credentials.md) exists to prevent for writes, left in place
for reads.

`cast.md` names "the Pilot deciding something" as a tripwire, which is why this needs recording
rather than assuming. The resolution is that the Pilot is not deciding policy here: it verifies a
signature and checks a list it was given. **Verifying is not deciding.** A Pilot that started
classifying statements itself would be the tripwire; a Pilot that refuses to take the control plane's
word for a classification is the plane split working.

## Decision

### Every method carries a submitter signature

A request to any Pilot method carries a signature by the **submitter's own key** over the request
digest — the same key bound into a marque as `cnf.jkt`
([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)). The Pilot verifies it against the **principal roster** — the same k-of-n, epoch-chained,
genesis-anchored artefact that carries approver keys, extended by
[EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) to carry operator and agent keys with a
capability field. An earlier draft of this record said "the enrolled principal set distributed with
it", which had no schema, no author, no k-of-n rule and no anchor — and therefore reopened exactly
the oracle this record closes.

| Method | Requires |
|---|---|
| `Execute` | a valid marque, plus proof of possession of `cnf.jkt` |
| `Rehearse` | a submitter signature over the request digest, from a principal permitted that target and role |
| `Introspect` | the same, plus the statement matching the **Pilot-held** introspection allowlist |
| `RevocationList` | nothing — it is public, signed, and revealing it helps nobody |

So a compromised Harbourmaster can relay a rehearsal a real operator asked for. It cannot invent one.
Constructing an oracle now requires a real operator's key, which reduces it to "an operator can
measure things through their own role" — which is true of any database client and is bounded by
[EDR-0006](./0006-every-statement-names-a-role.md).

### Introspection is classified twice

[EDR-0027](./0027-be-psql-then-be-better-than-psql.md) lets catalog reads run without a marque. That
classification is made in the control plane for UX, and **re-made at the Pilot** against an allowlist
the Pilot holds as its own configuration. A statement the control plane calls introspection but the
Pilot's allowlist does not recognise is refused.

Without this, "no approval needed" is a label the compromised component gets to apply.

### Redaction moves to the Pilot

[EDR-0010](./0010-rehearse-before-you-sign.md) redacts rehearsal samples by default, driven by
`displayable_columns`. That was control-plane policy, so the component that must not read customer
data decided which customer data it could read. Redaction is now applied **at the Pilot**, from
Pilot-held configuration, before the sample leaves the process.

### Counts and timings are disclosure

`rows_affected` and `duration_ms` are treated as what they are — a channel — and therefore:

- quota'd per principal at ingress ([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)),
  separately from execution quotas, because a rehearsal is cheap and an oracle needs volume;
- recorded in the logbook in aggregate per principal per target, so an unusual rehearsal rate is
  visible;
- and named in `SECURITY.md`'s accepted limitations beside the existing rehearsal entry, because a
  bounded oracle is still an oracle.

### The corrected claim

[EDR-0005](./0005-control-plane-holds-no-credentials.md)'s TL;DR, both compromise tables and
`introduction.md` are corrected from "gets no database access" to:

> A compromised control plane obtains **no credential and no ability to commit a change**. It can
> relay operator-signed reads, so a bounded, quota'd, target-visible read channel remains.

"Target-visible" matters: every such read appears in the target's own logs under the operator's
identity ([EDR-0021](./0021-connections-identity-and-read-routing.md)), so it is not a silent channel.

## Consequences

**Easier.**

- The plane split now holds for reads as well as writes, which is what everyone reading
  [EDR-0005](./0005-control-plane-holds-no-credentials.md) already believed.
- One authorisation model instead of one method with a rule and three without — a new Pilot method
  cannot be added without answering the question.
- Redaction is enforced by the component that holds the data, which is the only place it can be
  enforced.

**Harder.**

- **Every client must sign requests, not just carry a token**, including for a rehearsal. More client
  code, and a signing step in a path that felt like a read.
- **The Pilot holds more configuration** — an introspection allowlist and redaction rules — in the
  component that should stay smallest. Both are static lists rather than logic, which is the least-bad
  form.
- Two classifications of introspection can disagree, and the failure ("the console said this was
  fine, the Pilot refused it") is confusing until the error explains which list rejected it.
- The read channel is reduced, not removed. An attacker holding both the control plane and any
  operator's key still has it.

**New obligations.**

- A test issues a `Rehearse` with a control-plane signature and no submitter signature, and asserts
  refusal.
- Rehearsal rate per principal is monitored; a step change is a finding, since that is what oracle
  extraction looks like.

## References

- [EDR-0005](./0005-control-plane-holds-no-credentials.md) — the claim this corrects.
- [EDR-0010](./0010-rehearse-before-you-sign.md) — the path that had no caller check.
- [EDR-0027](./0027-be-psql-then-be-better-than-psql.md) — the introspection class this re-checks.
- [EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md) — the submitter key this reuses.

## Changelog

- **2026-08-15**: Accepted, following an expert-panel finding that only `Execute` had a stated caller
  check.
- **2026-08-16**: Amended after a second expert panel: deleted "the enrolled principal set distributed with it" — an undefined artefact with no anchor, which reopened the very oracle this record closes — in favour of the principal roster ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
