---
id: 18
title: "An agent's authority is the intersection of three scopes, including its own"
summary: "An agent submits as itself, on behalf of a named human. What it may do without asking is the intersection of operator policy, its human's delegation, and the narrower scope the agent declared for its own task."
status: accepted
implementation: none
implementation_note: "The only mention in code is .golangci.yml, which anchors its gen/ exclusion so that a future agent package will not be silenced by it. No agent surface, no scope intersection, and not the test asserting an agent cannot approve."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [identity, policy, product, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

An agent — a language model with tools, a script, a scheduled job, anything that acts without a human
at the keyboard — is a **submitter**, never an approver. It authenticates as itself and acts on behalf
of a named human. What it can run without asking anyone is:

```
effective scope  =  operator policy
                 ∩  the human's delegation to this agent
                 ∩  the scope the agent declared for this task
```

Three properties follow, and they are the point:

- **The agent narrows itself.** At the start of a task it declares the least it needs. That
  declaration is binding for the task and cannot be widened mid-task — widening is a new task, and a
  new task the human can see.
- **Anything outside is referred, never refused.** An out-of-scope request escalates to the agent's
  human ([EDR-0019](./0019-escalation-is-a-chain.md)), who can approve it in the console. The agent
  is not blocked; it is *supervised*.
- **Every record names two parties.** Actor is the agent, principal is the human. An audit entry with
  one name for a delegated action is rejected at write time
  ([ZFN-38](https://zrz.io/zfn/38-agents-are-principals/)).

An agent can never approve anything, **nor break glass** ([EDR-0037](./0037-emergency-paths.md)),
under any configuration. Both require a fresh interactive authentication — a user-verification
assertion — which no workload principal can satisfy
([EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)) — the prohibition is
mechanical rather than a rule someone has to remember.

## Context

Agents are about to be the majority of the callers, and the expedient way to wire one is
impersonation: hand it a copy of the operator's credential and let everything downstream believe it
*is* them. It works on day one and destroys attribution, scope and revocation simultaneously.
[ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) is unambiguous about the alternative, and
Marque is unusually well-placed to implement it, because it already has every piece: principals with
their own identities, scoped and expiring delegations, an approval queue, and a two-party audit
record.

What is genuinely new here is the **third** term in the intersection. Operator policy and human
delegation are the familiar two. The agent's own declaration matters because an agent's task scope is
knowable *to the agent* and not to anyone else: the human who delegated "you may fix stuck orders"
cannot know that this particular run only needs to touch one order. Making the agent state its own
bound turns that knowledge into a control, and it is nearly free, because the agent has no incentive
to over-declare for a task it defined.

It also produces a signal available nowhere else. **An agent that declares a wide scope for a narrow
task is worth looking at** — that is either a badly built agent or a compromised one, and it is
visible before anything runs.

The failure mode to avoid is the one every "AI with production access" story has: the agent gets a
credential, the credential has more authority than the task, and one bad tool call later there is no
way to tell what was the model and what was the person. Marque's answer is that the agent never holds
a credential at all — it holds a marque, for one statement, that names it.

## Decision

**Enrolment.** An agent is registered as a principal with its own workload identity, an owner, and a
purpose. It is not a user account with a shared password, and it is not a service account whose key
is pasted into a prompt.

**Task declaration.** An agent opens a task before submitting:

```jsonc
{
  "task": "tsk_01JB…",
  "on_behalf_of": "sam@acme.example",
  "purpose": "Unstick order 88213 reported in ticket ACME-4471",
  "declared_scope": {
    "target": "prod-primary", "role": "support_writer",
    "operations": ["select", "update"],
    "objects": [ { "schema": "public", "relation": "orders", "columns": ["status", "updated_at"] } ],
    "fence": ["id = '88213'"],
    "max_rows": 1
  },
  "expires_in": "30m"
}
```

- **`on_behalf_of` is not the agent's to choose freely.** It must name a human who holds an active
  delegation to this agent, or the agent's enrolled owner; anything else is refused. The named human is
  **notified at task open**, with a one-action disown — otherwise "its human" is an assertion by the
  party being supervised.
- The declaration is **attenuating only** — it can never exceed the human's delegation, which can
  never exceed operator policy. The intersection is computed and stored at task open, so the
  effective scope is fixed for the task's life.
- **It cannot be widened.** A statement outside it escalates
  ([EDR-0019](./0019-escalation-is-a-chain.md)) even if the human's delegation would have permitted
  it, because the agent said it would not need to.
- Tasks expire. `expires_in` is bounded by the delegation's own `not_after` and by a policy
  `max_task_ttl`, and policy also caps **concurrent open tasks** and **tasks opened per hour** — a task
  is cheap to open, and without a ceiling "declare narrowly" is satisfied by declaring narrowly many
  times ([ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/)).
- **Only the agent's own declaration is frozen for the task.** The delegation and policy terms are
  evaluated live at every mint, so revoking a delegation takes effect immediately rather than at the
  end of an open task.

**Submission and execution.** Within the effective scope, the agent submits and executes exactly as a
human would: same fence, same rehearsal, same magnitude assertions, same idempotency nonce, same
logbook. **Being an agent buys no shortcut through any control** — it only changes who is asked when
something falls outside.

**Marques name both parties.** A marque minted for an agent carries `sub` = the agent and an `act`
chain naming the human, per RFC 8693. Sub-agents extend the chain, bounded in depth by deployment
configuration, attenuating at every hop.

**Agent traffic is visibly agent traffic.** The console, the CLI and notifications mark it. An
approver being asked to approve something must always know whether a person or a model is asking, and
whose model it is.

**Independent quotas.** Agents have their own rate limits, separate from their human's
([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)). A runaway agent must not consume its
owner's ability to work, and a per-agent limit is the crudest and most reliable containment there is.

**Surgical revocation.** Revoking an agent's delegation, or killing a task, touches neither the
human's credentials nor the agent's other grants. There is a one-action stop for a single agent, and
one for all agents on a target. `marque agent suspend` **revokes outstanding marques by default**, and
`marque revoke --principal <agent>` does it directly. **Containment is not instantaneous**: revocation
propagates within the Pilots' revocation-refresh interval
([EDR-0004](./0004-marques-are-signed-leases.md)), so an agent holding a live marque may still execute
inside that window.

**Anomaly signals**, recorded and alertable, because they are cheap and only exist in this design:

| Signal | Why it matters |
|---|---|
| declared scope ≫ what the task actually used | badly built or compromised agent. **This bounds accidental blast radius; it is not a compromise detector** — an agent under an attacker's control declares narrowly and looks exemplary |
| escalation rate rising for one agent | its delegation no longer matches its job |
| submissions outside declared scope, repeatedly | the agent is probing, or its task model is wrong |
| a task re-opened many times in quick succession | widening by attrition |

## Consequences

**Easier.**

- An agent can be given production access that is genuinely narrow, genuinely expiring and genuinely
  attributable — which is currently a thing organisations either refuse outright or do badly.
- "Was that the model or the person?" is a column, not an investigation.
- Stopping a misbehaving agent is one action and costs its owner nothing.

**Harder.**

- **Agents need real identity infrastructure** before they can do anything: enrolment, workload
  identity, ownership. Considerably more setup than pasting a key, and that is the trade
  [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) asks you to accept.
- **Task declaration is work for whoever builds the agent**, and a lazy integration will declare its
  whole delegation as its task scope, collapsing the third term to nothing. The anomaly signal makes
  that visible; it cannot prevent it.
- **Escalation latency is now in an agent's loop.** An agent that blocks on a human takes minutes or
  hours, so agents must be built to park work and resume — which is a real constraint on integrators
  and needs to be stated plainly in the SDK docs, not discovered.
- More principals means more to review: every agent is an identity someone must own and eventually
  retire.

**New obligations.**

- Agent enrolment records an owner, and an agent whose owner has left the organisation is reported
  and suspended. An ownerless agent is the anonymous system actor
  [ZFN-40](https://zrz.io/zfn/40-no-anonymous-system-actor/) forbids, arriving by attrition.
- The prohibition on agents approving is asserted by a test, not by a review comment.

## References

- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — delegate, never impersonate; both names on
  every action.
- [ZFN-40](https://zrz.io/zfn/40-no-anonymous-system-actor/) — every automated action runs as a named
  identity.
- [ZFN-39](https://zrz.io/zfn/39-break-loops-not-spirals/) — bound agent work with budgets; a task
  that keeps reopening is a loop to break.
- [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) — the `act` chain.
- [EDR-0019](./0019-escalation-is-a-chain.md) — what happens outside the intersection.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: constrained `on_behalf_of` and notified the named human at task open, bounded task TTL and rate, stated that only the agent's own declaration is frozen, and made explicit that the declared-scope signal bounds accidental blast radius rather than detecting compromise.
- **2026-08-16**: Amended for the emergency paths and operator surfaces: an agent can never break glass either, for the same mechanical reason it cannot approve ([EDR-0037](./0037-emergency-paths.md)).
- **2026-08-19**: Amended for the artefact spelling ([EDR-0041](./0041-one-spelling-for-a-scope.md)): the declared scope's `fence` is an array of conjuncts, its relation is two fields, and its operations are lowercase. Making the spellings agree exposed a question this record and [EDR-0029](./0029-the-fast-path-authority-chain.md) had between them and neither stated — an effective fence is the *union* of three conjunct sets, which check 7's identity comparison refuses. Open as [issue #20](https://github.com/sixfathoms/marque/issues/20), due before M3.
