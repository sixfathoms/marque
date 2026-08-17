---
id: 8
title: "Approve routine work once, as a parameterised standing order"
summary: "A standing order is a statement template approved once, invoked with parameters that must satisfy declared constraints. Invocation mints a marque with no human in the loop, and every invocation is still logged."
status: accepted
implementation: none
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

A **standing order** is a named, parameterised statement — approved once, by the same signing
ceremony as any other marque — that an authorised principal may invoke without queueing. Parameters
are typed and constrained; a value that fails its constraint is refused at invocation.

```jsonc
{
  "name": "unlock-account",
  "target": "prod-primary",
  "role": "support_writer",
  "statement": "UPDATE public.accounts SET locked_at = NULL WHERE id = :account_id AND tier = :tier",
  "parameters": [
    { "name": "account_id", "type": "uuid" },
    { "name": "tier", "type": "string", "one_of": ["sandbox", "trial"] }
  ],
  "objects": [ { "table": "public.accounts" } ],   // write-set reference set (EDR-0033)
  "issued_at": "…", "roster_epoch": 47,            // signatures resolve against this epoch (EDR-0030)
  "max_rows": 1,
  "invokers": ["group:support"],
  "rate_limit": { "per_invoker": "20/hour", "total": "200/hour" },
  "expires": "2027-02-01T00:00:00Z"
}
```

Parameters are **bound as values, never interpolated into text**. The statement template is fixed at
approval time and cannot be altered by a parameter — a standing order that could be reshaped by its
inputs would be an approved SQL injection.

## Context

An approval queue that everything goes through becomes an approval queue nobody respects. Support
unlocking twenty accounts a day cannot wake an approver twenty times, and the predictable outcome is
a shared credential that bypasses Marque entirely — the control is defeated by being too strict, not
too loose.

The observation that makes this safe is that routine work is *routine*: it is the same statement
with different values. The thing worth reviewing is the statement shape and the constraints on its
inputs, and that is reviewable once. What is left at invocation time is a bounded question — does
this value satisfy this constraint — which a machine answers correctly every time.

This is also the shape of every SQL injection ever written, so the boundary has to be drawn in
exactly the right place: **parameters bind values; they never contribute syntax.** Not a table name,
not a column name, not an operator, not a fragment of a `WHERE` clause. If a use case needs a
varying table name, it needs several standing orders.

## Decision

**A standing order is approved like a marque.** It is submitted, analysed, and signed with an
approver's device key and the Harbourmaster's countersignature
([EDR-0004](./0004-marques-are-signed-leases.md)). The signed artefact covers the statement template,
the parameter declarations, the role, the limits and the invoker list. Editing any of them is a new
approval; there is no in-place amendment.

**Parameters are typed and constrained.** Types are `string`, `integer`, `number`, `boolean`, `uuid`,
`timestamp`, and a homogeneous `array` of those. Constraints, all optional and combinable:

| Constraint | Applies to | Notes |
|---|---|---|
| `one_of` | any | An explicit allowlist. The safest constraint; prefer it. |
| `pattern` | string | An **anchored** regular expression, compiled with a linear-time engine and a size bound. |
| `min` / `max` | integer, number, timestamp | Range, inclusive. |
| `max_length`, `max_items` | string, array | Bounds. |
| `from_query` | any | The value must appear in the result of a declared, read-only, role-scoped lookup. |

Rules on constraints:

- **Every write standing order must carry `max_rows`**, or an explicit acknowledgement field naming
  who accepted the risk — the shape [EDR-0015](./0015-policy-is-reviewed-configuration.md) already
  uses for `self_approval`. It appears in the worked example above and was never stated as required,
  which is how it would have been omitted.
- **Every parameter must carry at least one constraint.** An unconstrained parameter is refused at
  approval time. This is deliberate friction: the unconstrained case is the one that turns a standing
  order into a general-purpose write.
- Regular expressions are anchored automatically and rejected if they are not linear-time. An
  unanchored pattern is a constraint that matches a substring of an attacker-chosen value, which is
  no constraint at all.
- `from_query` runs against the same target under the same role, read-only, with a statement timeout.
  Its text is **inside the checkable subset and covered by the order's signature**, it takes no
  invoker-supplied input beyond the value under test, and it is logged and quota'd like any other read
  — otherwise it would be an unreviewed query that runs on every invocation.

**Binding is by value.** The Pilot prepares the statement and binds parameters through the driver's
parameter protocol. Marque never builds a statement by string substitution, and the template is
carried inside the signed payload so the executed shape is the approved shape.

**Invocation mints a marque.** The result is an ordinary marque with a short window and a budget of
one, whose `auth` block names this standing order and its digest instead of an interactive approval
([EDR-0029](./0029-the-fast-path-authority-chain.md)). **The signed standing order itself travels
with the marque and supplies the approver limb** — the human signed the *shape*, here, rather than
the instance — and the Pilot verifies it offline: the order's own signatures, the digest, the
template rebound with the supplied parameters against `req`, each parameter against its constraint,
and that the marque's limits are within the order's. Everything downstream — the magnitude assertion, the write-set assertion, the execution nonce and
budget, and the logbook — is unchanged. **A standing order is a way to skip the queue, not a way to
skip the record.**

**Name principals in `invokers` where offline verification matters.** Group membership is
control-plane state, so a Pilot verifying an invocation offline cannot confirm the executing
principal was in a group. An order whose `invokers` are groups is therefore verifiable as to its
*shape* but not as to *who invoked it*: a compromised control plane could invoke a genuine order as a
principal of its choosing, bounded to that order's approved **statement shape** and **parameter
constraints** — and **not** by its budget or rate limits, which are enforced at ingress by the very
component that is compromised ([EDR-0029](./0029-the-fast-path-authority-chain.md)). Fast-path volume
is unbounded against it. Orders on `critical` targets must name principals directly.

**Rate limits are enforced at ingress** ([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)),
per invoker and in total, with a `Retry-After`. An unlimited standing order is one compromised
support account away from being a bulk-modification tool.

**Standing orders expire.** `expires` is required, and the deployment defaults it to a bounded period.
Renewal is a re-approval, which is the moment someone re-reads it — the whole point.

## Consequences

**Easier.**

- The queue stays short enough to be respected, which is what makes the queue work for the requests
  that genuinely need a human.
- Support and on-call get a self-service path with a genuinely small blast radius, and it is faster
  than asking, so it is the path people actually take.
- The review that matters happens once, unhurried, on the shape — rather than twenty times, hurried,
  on values.

**Harder.**

- **A badly-scoped standing order is a standing hole.** `WHERE id = :id` with no other constraint is a
  permanent grant over the whole table. Mandatory constraints, `max_rows`, expiry and rate limits are
  all aimed at this, and none of them stops an approver who does not think about it.
- Genuinely varying shapes need several orders, or fall back to the queue. Some legitimate use cases
  will be awkward.
- `from_query` puts a query on the invocation path, which is latency and a second thing that can fail.

**New obligations.**

- Every standing order is re-read at renewal, and the console shows invocation counts so an unused one
  is retired rather than renewed reflexively.
- Invocation volume is monitored per order. A sudden change in the rate of a routine operation is a
  signal worth alerting on, independent of whether each invocation was permitted.

## References

- [ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/) — quota every endpoint, including the
  unabused ones.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — standing orders expire.
- [EDR-0004](./0004-marques-are-signed-leases.md) — what invocation mints.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the other way to avoid the queue, for
  statements that vary in shape rather than in values.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Amended after review: the signed standing order supplies the approver limb of a marque minted by invocation, and travels with it for offline verification ([EDR-0029](./0029-the-fast-path-authority-chain.md)). Added the residual that group-named `invokers` are not offline-verifiable, and the rule that `critical` targets must name principals.
- **2026-08-16**: Amended after the expert panel's should-fix pass: required `max_rows` on every write standing order (or an explicit acknowledgement), specified `from_query`'s own constraints, and corrected the "unchanged" list, which named fencing rather than the assertions that actually apply.
- **2026-08-16**: Amended after the second panel's synthesis: added the `objects` field [EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md) sources the fast-path write-set reference set from, and corrected the residual, which counted budget and rate limits the compromised component itself enforces.
- **2026-08-16**: Amended in the second panel's should-fix pass: added signed `issued_at` and `roster_epoch`, so a long-lived artefact verified against roster keys has a stated answer to whether the signing key was live.
