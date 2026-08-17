---
id: 19
title: "Escalation is a chain of named stages, and every stage is a person"
summary: "A request outside its submitter's scope escalates to a defined sequence of approvers — an agent's human first, then whoever policy additionally requires — with each stage timed, notified, and recorded."
status: accepted
implementation: none
implementation_note: "The only mention in code is .golangci.yml, alongside EDR-0018's, and it is about a lint path exclusion rather than about escalation. No chain, no stages, no timers."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Falling outside your scope is not a refusal. It is an **escalation** to a chain of stages computed at
submission time and shown to the submitter immediately, so they know who is being asked and in what
order.

For an agent submitting on behalf of a human, the first stage is **that human**. If what is being
asked exceeds what the human may themselves approve, the chain continues to whoever policy requires
— a data owner, a second approver — until a stage exists that can authorise the whole request, or
the chain is empty and the request is refused with the reason *"nobody can approve this"*.

Three rules:

1. **Every stage is a human.** No stage is ever an agent, a group with no members, or a rule. If
   policy produces a chain containing something that cannot be a person, the chain is invalid.
2. **A stage may approve only what it holds.** Approving does not confer the next stage's authority;
   it advances the request to it. The final marque carries every signature the chain required
   ([EDR-0004](./0004-marques-are-signed-leases.md) already supports several).
3. **The chain is computed once, at submission, and recorded.** It cannot lengthen or shorten while
   the request is open — a policy change mid-flight applies to the next request, not this one. **That
   frozen chain is what a marque's `approvals.stages` encodes**, `chain` is its digest, and the
   preimage travels in the marque bundle so a Pilot can recompute it rather than merely carry it
   ([EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)).

## Context

[EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) makes an agent a supervised
submitter, which only works if "supervised" resolves to a specific, reachable person quickly. Without
that, an out-of-scope agent request is functionally a refusal with extra steps, and integrators route
around it — usually by widening the delegation until nothing escalates, which defeats the design.

There is also a real authority question that a single approval step cannot express. An agent asks for
something outside its scope; its human approves. But what if the thing asked for is outside what
*that human* may approve? Treating their approval as sufficient silently promotes them. Refusing
outright makes the human's approval pointless. The correct answer is that their approval is
**necessary but not sufficient**, and the request carries on to someone who can supply the rest.

That shape — an ordered sequence, each stage contributing the authority it holds — is also what
production changes at a certain blast radius genuinely need, independent of agents. Two-person rule
for anything touching a `critical` target is a policy plenty of organisations want and few can
express.

The failure mode to design against is the **stalled queue**. An escalation nobody is told about, or
one waiting on a person who is asleep, is worse than a refusal: the submitter waits, believing
progress is happening. Every stage therefore has a timeout, a notification, and a visible state.

## Decision

**Chain construction.** At submission, given the submitter, the request and policy, Marque computes:

```jsonc
{
  "stages": [
    { "n": 1, "approvers": ["sam@acme.example"], "reason": "principal of agent svc:order-bot",
      "timeout": "30m", "on_timeout": "advance_and_notify" },
    { "n": 2, "approvers": ["group:data-oncall"], "reason": "orders is a critical target",
      "timeout": "2h",  "on_timeout": "notify_only" }
  ],
  "refuse_if_unfilled": true          // an unfillable chain ALWAYS refuses; this selects the reason
}
```

- **Stage 1 for an agent is always its principal.** The human on whose behalf it acts sees it first,
  always, even when policy would not otherwise require them. They are the person who delegated, and
  they are accountable for what their agent asks for.
- Subsequent stages come from policy
  ([EDR-0015](./0015-policy-is-reviewed-configuration.md)) — target criticality, role criticality, the
  objects touched, and the magnitude the rehearsal measured.
- **The chain is shown to the submitter at once**, with names. An agent reports it to its own caller,
  so a person watching a model work can see it is waiting on Sam, not stuck.

**Advancing.** A stage is satisfied when the required approvals within it are collected. Each approver
signs, and each signature goes on the marque. **Refusal at any stage ends the request** — later
stages are not consulted, because a refusal is a decision, not a vote.

**Timeouts do not approve.** `on_timeout` is `notify_only` (escalate the *notification* — remind,
widen to the group, page) or `advance_and_notify`, which moves to the next stage **while leaving the
skipped stage's approval still required**. Nothing in the design lets a timeout satisfy a stage. A
request that ages past its total budget is refused with `expired`, and the submitter resubmits
([EDR-0012](./0012-the-logbook-is-append-only.md)).

**Self-approval within a chain** follows [EDR-0015](./0015-policy-is-reviewed-configuration.md). A
human who is both submitter and a stage's approver satisfies that stage only where `self_approval` is
enabled for that target; otherwise the stage requires someone else and says so.

The test applies to **every human party to the request** — the submitter and each principal in the
`act` chain, not merely whoever pressed submit — and **a principal satisfies at most one stage**.
[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md) already makes such a marque
unmintable, since distinctness there is by principal rather than by key; stating it here is what
makes the Harbourmaster **refuse cleanly** rather than wait forever for a stage that cannot be
filled.

**The marque names the agent, not the approver.** When Sam approves an agent's request, the marque's
`sub` is the **agent** — it is the agent that will execute — with the `act` chain naming Sam and the
signatures recording every stage. So the record reads: *the agent ran it, on Sam's behalf, authorised
by Sam and the data on-call.* That is exactly what happened, and it is the sentence
[ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) asks the audit log to be able to produce.

**Urgency reroutes the chain without changing it.** A request marked `--urgent`
([EDR-0037](./0037-emergency-paths.md)) notifies **every stage at once** rather than in sequence,
pages rather than messages, and adds the target's `emergency_approvers` to each stage's eligible set.
The stages, their thresholds and the scope are untouched — unless `urgency_may_collapse_stages` is on
for that target, in which case the chain becomes one stage drawn from `emergency_approvers`, the
marque records that it was collapsed, and the request joins the post-hoc review queue.

**Notification per stage, where the approver already is.** Slack first, driven off the WAL stream
([EDR-0013](./0013-async-work-rides-the-wal.md)), with the request, the measured rehearsal numbers,
who is asking, whether it is an agent, and a link. Reminders before timeout, not after.

**Visible state throughout.** Every request shows its chain, the current stage, who is being waited
on, how long it has waited, and what happens at timeout — to the submitter, to the approvers, and in
the agent's own reply to its caller.

## Consequences

**Easier.**

- Agent supervision is real: a human sees what their agent could not do by itself, at the moment it
  matters, with enough context to answer in seconds.
- Two-person rules on high-blast-radius changes become expressible without a bespoke workflow.
- "Who is this waiting on?" is always answerable, which is the question that otherwise turns an
  approval queue into a black hole.

**Harder.**

- **Latency multiplies with stages.** A two-stage chain is two human response times, and an agent
  blocked on both is slow in a way its author must design for. Chains should be short, and policy
  that produces long ones should be treated as a smell.
- **Chain construction is subtle**, and a policy change that empties a chain would turn every request
  into "nobody can approve this". Refusal at apply time
  ([EDR-0015](./0015-policy-is-reviewed-configuration.md)) is what stops that shipping.
- **More notifications, and notification fatigue is real.** An approver drowning in requests stops
  reading them, which converts this control into a rubber stamp — the risk named in
  [EDR-0009](./0009-the-leadsman-is-advisory.md), arriving from a different direction. Standing
  orders, compiled delegations and well-declared agent scopes are what keep the volume survivable.
- Availability of approvers is now an operational property with a rota behind it.

**New obligations.**

- Time-in-stage is measured and reported per target and per approver group. A group whose median
  response is hours has a rota problem, and Marque should be the thing that says so.
- Escalation volume per agent is monitored: a rising rate means the agent's delegation no longer
  matches its job, and the fix is to rewrite the delegation rather than to keep approving.

## References

- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — the audit sentence this produces.
- [ZFN-13](https://zrz.io/zfn/13-load-shedding-and-flow-control/) — a queue that cannot be served in
  time must push back rather than accumulate.
- [EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) — what falls outside a scope.
- [EDR-0015](./0015-policy-is-reviewed-configuration.md) — where stages after the first come from.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Amended after review: the chain's freeze at submission is now load-bearing for [EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md), which binds the required approval count, the eligible approvers and the chain digest into the signed payload. Loosening the freeze would break that record's verification.
- **2026-08-16**: Amended after the expert panel's should-fix pass: defined `refuse_if_unfilled` and stated that the self-approval and distinctness tests apply to every human party to a request, so an unfillable chain refuses cleanly instead of deadlocking.
- **2026-08-16**: Amended in the second panel's should-fix pass: stated that the frozen chain is what EDR-0030's `approvals.stages` encodes and that its preimage travels in the marque bundle.
- **2026-08-16**: Amended for the emergency paths and operator surfaces: stated how urgency reroutes the chain — every stage at once, paged, with `emergency_approvers` added — without changing stages, thresholds or scope unless a target enables collapse ([EDR-0037](./0037-emergency-paths.md)).
