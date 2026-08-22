---
id: 38
title: "A request is a shareable, watchable object with a live status"
summary: "Every request has a reference an operator can paste into chat, a status block naming the chain and who is being waited on, and a queue command that lists pending and approved work so it can be run without hunting."
status: accepted
implementation: partial
implementation_note: "The `req_…` reference exists, is randomly generated so it cannot be enumerated, and resolves through GetRequest — where an unknown one and another tenant's are the same NotFound, never a PermissionDenied that would confirm it. All seven states are in the schema and the proto, and M1 produces four of them. There is no status block, no chain, no watch and no notification; and the entitlement the 404 rule protects has nothing to enforce it against, because M1 has no identity."
date: 2026-08-16
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [cli, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Being told "no" is the interaction an operator has most often, and it is currently one line of
SQLSTATE. It should carry everything needed to *get to yes*:

```
ERROR:  outside your delegated scope; submitted for approval
DETAIL:  req_01JB2Q9F3K8Z · 412 rows rehearsed, 0 outside fence
HINT:   waiting on sam@acme.example (stage 1 of 2), then group:data-oncall
        share:  marque://acme/req_01JB2Q9F3K8Z   ·   https://marque.acme.example/r/01JB2Q9F3K8Z
        watch:  marque watch req_01JB2Q9F3K8Z
```

Three decisions:

1. **A request has a stable reference and a URL**, designed to be pasted into a chat window. **The
   reference is not a capability** — holding it grants nothing; viewing still requires entitlement.
   That has to be true, because people will paste it in public channels.
2. **`marque requests` is the work queue** — pending *and* approved together, because "what am I
   waiting on" and "what can I run now" are the same question asked ten minutes apart.
3. **The session never blocks by default.** An out-of-scope statement returns immediately with the
   status above; `--wait` is opt-in. A psql session hung with no output is indistinguishable from a
   broken one.

## Context

Everything in this design is about making the approved path better than the unapproved one, and the
approved path currently has a hole in the middle of it: after submitting, the operator has no idea
what is happening. They know a request exists. They do not know who was asked, whether that person is
awake, whether it has already been approved, or what to do next — so they go and ask in chat, which
is the workflow Marque was supposed to replace.

The specific frictions worth naming, because each has a mechanism below:

- **"Who do I chase?"** The chain is computed at submission
  ([EDR-0019](./0019-escalation-is-a-chain.md)) and known immediately. Not showing it is withholding
  the one fact the operator needs.
- **"How do I tell them?"** The answer today would be to describe the request in prose. A reference
  and a link is the whole of it — and once operators are pasting references into chat, the reference
  must be safe to paste.
- **"Did it get approved?"** An approved marque sitting unnoticed until it expires is the worst
  outcome available: the approver was interrupted, the operator waited anyway, and the window closed.

## Decision

### The reference

`req_01JB2Q9F3K8Z` — sortable, unambiguous, short enough to read aloud on a call. Rendered alongside a
deep link into the console.

**It is an identifier, not a capability.** Anyone may hold it; only an entitled principal may resolve
it. This is a deliberate departure from the "capability URL" pattern, chosen because the primary
intended use is *pasting it into a shared channel*, and a URL that is also a credential is a credential
that ends up in a channel.

### The status block

Printed on refusal, by `marque watch`, and by `marque requests --verbose`. It shows, always:

| Field | Why |
|---|---|
| reference and share link | so it can be sent |
| state | `pending`, `verifying`, `approved`, `refused`, `expired`, `executed`, `indeterminate` |
| the chain, per stage | with names, which stage is current, and what happens at timeout ([EDR-0019](./0019-escalation-is-a-chain.md)) |
| measured facts | rows rehearsed, fence violations, write set ([EDR-0010](./0010-rehearse-before-you-sign.md), [EDR-0033](./0033-assert-the-whole-write-set-not-just-the-named-relation.md)) |
| time in stage | the number that makes a stalled queue visible to the person it is stalling |
| for `approved` | the marque's expiry and remaining budget, and the command to run it |
| for `refused` | the reason and the refusing principal |

Where a request was submitted `--urgent`, or under break glass, the block says so prominently
([EDR-0037](./0037-emergency-paths.md)) — an operator should never be unsure whether they used an
emergency path.

### The queue

```
$ marque requests
REF                 STATE      TARGET        AGE    WAITING ON / EXPIRES
req_01JB2Q9F3K8Z    pending    prod-primary  4m     sam@acme.example (1 of 2)
req_01JB2Q4M7X2A    approved   prod-primary  11m    expires in 49m · budget 1
req_01JB2Q1B9K3C    executed   prod-primary  2h     1 row · committed
```

- **Pending and approved in one view**, sorted so the actionable things are at the top.
- `marque requests --mine` (default), `--approving` (things waiting on *you*), `--all`.
- `marque run <ref>` executes an approved one; `marque output <ref>` shows what happened; `marque watch
  <ref>` follows a pending one live.
- **An approved marque nearing expiry unused is surfaced**, in the queue and by notification. It is
  the cheapest signal in the system and it prevents the most annoying failure: waiting for something
  that arrived.

### Not blocking

An out-of-scope statement returns immediately. `--wait [duration]` opts into blocking, and prints
progress rather than sitting silent. This is stated because the opposite default is the tempting one —
it *feels* more like a normal database client — and it produces a session that cannot be told from a
hung one.

## Consequences

**Easier.**

- The refusal becomes useful. An operator can act on it without leaving the terminal, and the person
  they need is named rather than guessed at.
- Approval stops being a black hole, which is the single biggest reason people route around a queue.
- Time-in-stage becomes visible to the person it affects, not only to whoever reads a dashboard —
  which is how a rota problem gets reported by the people it is hurting.

**Harder.**

- **More surface to keep truthful.** A status block that says "waiting on Sam" when Sam approved
  thirty seconds ago is worse than no status block, so this is a real-time correctness obligation on
  the projection ([EDR-0012](./0012-the-logbook-is-append-only.md)).
- **Sharable references invite over-sharing.** Making the reference non-capability is what makes that
  safe, and it costs an entitlement check on every view, including from the console link.
- The queue is a new default surface people will want more from — filters, saved views, sorting — and
  each addition is a small pull toward being a ticketing system rather than a broker.

**New obligations.**

- The non-capability property is asserted by a test: resolving a reference as an unentitled principal
  must 404 rather than 403, so the reference does not confirm its own existence.
- Expiring-unused is measured. A deployment where many approved marques expire unused has an approval
  latency problem that its operators have already worked around.

## References

- [EDR-0019](./0019-escalation-is-a-chain.md) — the chain this surfaces.
- [EDR-0022](./0022-local-proxy-brokers-every-statement.md),
  [EDR-0027](./0027-be-psql-then-be-better-than-psql.md) — the surfaces it is printed on.
- [EDR-0037](./0037-emergency-paths.md) — what urgency and break glass add to it.
- [ZFN-13](https://zrz.io/zfn/13-load-shedding-and-flow-control/) — push the wait back to the caller
  visibly rather than hanging.

## Changelog

- **2026-08-16**: Accepted.
