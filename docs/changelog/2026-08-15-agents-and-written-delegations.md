---
title: "Agents become a first-class submitter, and delegations can be written in English"
tags: [policy, security, product, docs]
order: 1
---

Marque now targets a second use case directly: **giving an agent production access without giving it
a credential.** An agent submits as itself on behalf of a named human, runs what is inside a scope it
partly declares for its own task, and escalates everything else to that human rather than failing.

Alongside it, a delegation can be written as a sentence and compiled into an enforceable scope. Four
new decision records cover both, and the security question they raise — *how close may a model get to
authority?* — is answered structurally rather than behaviourally.

### Added

- **[Agents are submitters under an intersected scope](/edrs/0018-agents-are-submitters-under-intersected-scope/)**
  — what an agent may do without asking is `operator policy ∩ its human's delegation ∩ the scope the
  agent declared for this task`. The third term is the novel one: an agent knows what *this run*
  needs, declares it, and is held to it, which turns over-declaration into a visible anomaly rather
  than an invisible risk. See the new [Agents](/concepts/agents/) page.
- **[Escalation is a chain](/edrs/0019-escalation-is-a-chain/)** — out-of-scope work is referred, not
  refused. Stage one for an agent is always its own principal; later stages come from policy. Every
  stage is a human, each contributes only the authority it holds, and **a timeout never approves**.
- **[Written delegations are compiled](/edrs/0016-natural-language-delegations-are-compiled/)** —
  *"Sam can update `settings` on sandbox accounts, up to 100 rows"* is compiled by a model into a
  structured scope. The grantor signs **the compilation, not the sentence**, and enforcement runs
  entirely on the compilation. Ambiguity, missing schema evidence and unbounded scopes are refused
  rather than guessed at.
- **[A model may choose a route, never widen a bound](/edrs/0017-conformance-matching-may-route-never-widen/)**
  — for the clauses that genuinely will not compile, a new principal (the **Surveyor**) judges
  conformance per request, inside the human-signed bound, with a unanimous three-way panel, exactly
  two possible answers, default-refer on any doubt, ingress quotas, a mandatory sampled human audit
  that can automatically suspend the fast path, and a polled kill switch. It ships **off**.
- **[An operator playbook](/operations/playbook/)** — duties, the signals that separate a working
  deployment from a quietly broken one, and procedures including suspending an agent, turning off
  surveying, working an incident with the control plane down, and responding to each kind of
  compromise.

### Changed

- **[EDR-0009](/edrs/0009-the-leadsman-is-advisory/) gained a scope section** rather than being
  superseded. Its decision is unchanged — the *analyst* still holds no authority and produces no
  verdict — but it now says explicitly that it governs the Leadsman rather than every model in the
  system, and points at the separate, bounded principal that does gate.
- The [architecture](/overview/architecture/), [introduction](/overview/introduction/),
  [scope](/overview/scope/) and [cast](/concepts/cast/) pages carry the agent surface, and the cast
  gained the Surveyor with a note on why it and the Leadsman are deliberately different temperaments.
- Scope gained agent gateways and in-framework human-in-the-loop approval as prior art, three new
  risks, and a phase for the agent surface.

### The invariant worth arguing with

**A model can never create authority a human did not sign.** The analyst holds none; a compiled
delegation is signed by its grantor; a conformance judgment only ever chooses between two paths that
both end in a human-granted scope, with the deterministic fence and magnitude assertions still
running underneath. The worst a model error achieves is failing to escalate something *already inside
a signed scope*.

That is a bound, not an elimination, and
[EDR-0017](/edrs/0017-conformance-matching-may-route-never-widen/) says so plainly — which is why
surveying is off by default and why its sampled audit is mandatory rather than advisory.
