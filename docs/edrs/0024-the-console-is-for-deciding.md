---
id: 24
title: "The console is for deciding, and it has no bulk approve"
summary: "A static, same-origin web app for reviewing and signing marques, supervising agents and reading the logbook. It cannot author policy, cannot run ad-hoc SQL, and deliberately offers no way to approve many things at once."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [console, product, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The console exists so that **an approver can decide well, on a phone, in ninety seconds**. That is its
whole job, and everything else is deliberately absent:

- **No bulk approve.** No "approve all", no multi-select, no keyboard-repeatable accept. Each marque
  is signed individually with a WebAuthn user-verification prompt
  ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)). Bulk refuse *is* offered, because the
  asymmetry is the point.
- **No policy authoring.** Approval policy is reviewed configuration in a repository
  ([EDR-0015](./0015-policy-is-reviewed-configuration.md)). The console shows the effective answer;
  it never widens authority.
- **No ad-hoc SQL.** That is the CLI and the local proxy
  ([EDR-0022](./0022-local-proxy-brokers-every-statement.md)).
- **Static assets, same origin as the API**, with a strict Content-Security-Policy, no inline script
  and no third-party origin. The client is generated from the same schema as everything else
  ([EDR-0020](./0020-one-schema-generates-every-client.md)).

It is otherwise read-mostly: every mutating action is a signed act, and there are exactly four of
them — sign a marque, refuse a request, grant or revoke a delegation, suspend an agent.

## Context

The console is where approval quality is won or lost. The failure mode is not a vulnerability, it is
**a tired person on a phone approving something they did not read**, and every convenience feature
pushes gently in that direction. A multi-select checkbox column turns twelve considered decisions
into one click; a "same as last time" shortcut turns review into pattern-matching; a green badge
turns a fact into permission.

This is the same risk named in [EDR-0009](./0009-the-leadsman-is-advisory.md) and
[EDR-0019](./0019-escalation-is-a-chain.md), arriving through the interface rather than through a
model or a queue. It cannot be solved — nothing prevents a human from clicking without reading — but
the interface can decline to make it *easy*, and can make the thing worth reading the most prominent
thing on the screen.

The second force is deployment. A console that needs a build server, a CDN, a rendering backend and a
translating proxy is a console an adopting team will struggle to run, and Marque's whole adoption
story is "one bootstrap URL and go" ([EDR-0002](./0002-bootstrap-discovery-document.md)). Serving
static files from the same origin as the API removes the proxy, removes the CORS surface, and lets
DPoP-bound tokens and the WebAuthn relying-party identifier line up with no special cases.

## Decision

### The approve screen

The screen that matters. In order, top to bottom:

1. **The statement**, verbatim, monospaced, never truncated and never behind a disclosure control.
2. **The measured facts** — rows affected, duration, plan summary, fence violations — from the
   rehearsal, labelled as measured ([EDR-0010](./0010-rehearse-before-you-sign.md)).
3. **Who is asking**, and prominently **whether it is an agent**, on whose behalf, and under which
   task declaration ([EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md)).
4. **The analysis**, visually separated and provenance-marked so model prose cannot be mistaken for a
   computed fact ([EDR-0009](./0009-the-leadsman-is-advisory.md)).
5. **What you are about to grant** — role, window, execution budget, fence — as a plain sentence, not
   a JSON blob.
6. **The escalation chain**, and where this signature sits in it
   ([EDR-0019](./0019-escalation-is-a-chain.md)).

Signing raises the authenticator prompt. There is no way to sign without it, and no "remember for
this session".

**Editing before approval is supported**, and produces a different statement digest and therefore a
different marque; the console shows submitted-versus-signed side by side, because an approver quietly
changing what someone asked for is exactly the thing a record must capture
([EDR-0004](./0004-marques-are-signed-leases.md)).

### Deliberate absences

| Absent | Why |
|---|---|
| Bulk approve, multi-select, approve-and-next | Volume is the enemy of review. If the queue is long enough to want this, the fix is standing orders and compiled delegations, not faster clicking |
| Saved approvals / "same as last time" | Pattern-matching instead of reading |
| A risk score or traffic-light badge | The shape people automate against ([EDR-0009](./0009-the-leadsman-is-advisory.md)) |
| Policy editing | Authority changes get review ([EDR-0015](./0015-policy-is-reviewed-configuration.md)) |
| An SQL editor | [EDR-0022](./0022-local-proxy-brokers-every-statement.md) |
| Raw result browsing | The console is for approvers, who may not be entitled to the data they are approving access to |

That last one is subtle and worth stating plainly: **an approver is not necessarily authorised to
read the rows in question.** Rehearsal samples are redacted by default
([EDR-0010](./0010-rehearse-before-you-sign.md)) and the console never renders result sets from an
execution.

### Other surfaces

- **Agent supervision** — live tasks, declared versus used scope, escalation rate, and one-action
  suspend. The declared-versus-used gap is shown as a first-class number, because it is the anomaly
  signal that exists nowhere else.
- **The logbook** — searchable, filterable, with the chain verification status visible. Reading it is
  the console's most-used feature and the one that justifies it to people who never approve anything.
- **Delegations and standing orders** — what exists, who granted it, when it expires, how often it is
  used, and whether a delegation is Tier A or Tier B
  ([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)). An unused standing order should
  be visibly unused so it gets retired rather than renewed.
- **The Tier-B audit queue** — the sampled review that makes surveying correctable. Its age is shown
  on the landing page whether or not you go looking, because an unread queue removes the mitigation
  silently.

### Build and delivery

Static files: no server rendering, no framework requirement, no third-party origin at runtime. A
strict CSP with no `unsafe-inline`, which rules out a build that injects inline bootstrap script —
that constraint drives the tooling choice rather than the other way round.

Sessions are short and are **not sufficient to approve**: signing needs the authenticator regardless,
so a stolen cookie yields reading, not authority. That is what allows the session TTL to be a
usability decision rather than the last line of defence.

## Consequences

**Easier.**

- Approving from a phone is realistic, which is what keeps time-in-stage low
  ([EDR-0019](./0019-escalation-is-a-chain.md)).
- Deployment is copying files next to the API. No proxy, no CORS, no CDN configuration.
- A stolen console session cannot approve anything.

**Harder.**

- **The absence of bulk approve will be the most-requested feature**, and the request will be
  reasonable, and it will come from the person with the longest queue. The answer is to fix the queue.
  This will be an ongoing conversation rather than a settled one.
- **Rendering a decision well is more work than rendering a list.** Statement, measured facts,
  provenance-marked prose and an authority sentence, all legible on a small screen, is real design
  effort.
- WebAuthn in a console adds browser and authenticator compatibility to the test matrix, including the
  case where someone's only enrolled key is on their other device.
- A static, CSP-strict app forecloses some convenient libraries.

**New obligations.**

- The absences above are tested, not merely intended: a test asserts there is no endpoint that
  approves more than one marque per authenticator assertion.
- Time-in-stage and audit-queue age are surfaced in the console itself, so the people who could fix a
  slow rota are the ones who see it.

## References

- [ZFN-16](https://zrz.io/zfn/16-separate-data-plane-control-plane/) — the console is a control-plane
  surface and never touches the data path.
- [EDR-0023](./0023-approver-keys-enrolment-and-recovery.md) — the WebAuthn envelope it produces.
- [EDR-0015](./0015-policy-is-reviewed-configuration.md) — why authority is not editable here.
- [EDR-0009](./0009-the-leadsman-is-advisory.md) — why there is no score.

## Changelog

- **2026-08-15**: Accepted.
