---
id: 17
title: "A model may choose a route, never widen a bound"
summary: "Where a written delegation will not fully compile, a Surveyor judges whether a request conforms to it — but only inside a deterministic bound a human signed, with two possible outcomes: take the fast path, or refer to a human."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, security, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0016](./0016-natural-language-delegations-are-compiled.md) compiles most written delegations
into deterministic scopes, and those need no model at request time at all. Some clauses will not
compile — *"fix obvious data-entry typos in customer names"* has no structured equivalent. For those,
a **Surveyor** judges conformance at request time, under four constraints that are the whole of this
record:

1. **It runs inside a signed bound.** Every delegation with an unexpressible clause still carries a
   compiled outer bound — objects, fence, `max_rows` — that a human signed and that
   [EDR-0007](./0007-delegation-by-containment-proof.md) enforces deterministically. The Surveyor
   decides only whether to take the fast path *within* that bound.
2. **It has exactly two outcomes: `conforms` or `refer`.** It cannot deny, cannot alter a statement,
   cannot widen a scope, and cannot extend a window. Denial is a human act.
3. **Refer is the default for everything.** Any error, timeout, ambiguity, injected instruction, or
   less-than-unanimous panel resolves to `refer`. There is no configuration in which uncertainty
   resolves to `conforms`.
4. **It is off unless deliberately enabled**, per deployment and per target, and it can be switched
   off in one action without a deploy.

So the worst a Surveyor failure can do is **fail to escalate something that was already inside a
scope a human signed**. It cannot produce authority that did not exist.

## Context

The honest limit of [EDR-0016](./0016-natural-language-delegations-are-compiled.md) is that natural
language expresses scopes the structured grammar cannot. "Correct obviously malformed email
addresses", "unstick orders that have been pending more than a day", "fix typos" — an operator knows
exactly what they mean, the intent is genuinely narrow, and no `fence` predicate captures it.

Refusing those outright is a defensible position and it is the wrong one, because the alternative
people take is a wider delegation that *does* compile. Faced with "I cannot express 'fix typos'", the
operator grants `UPDATE customers(name)` with no fence — which is strictly more dangerous than a
model checking each statement against the sentence. **The safe-looking refusal produces the less safe
outcome.**

The risk to design against is the one named in
[EDR-0009](./0009-the-leadsman-is-advisory.md): authority creeping toward the model by convenience.
It starts as a router, becomes a score, becomes auto-approval of anything scored low. And the input
is adversarial — a statement is attacker-controlled text, and an SQL comment is a fine place to put
an instruction aimed at whatever reads it.

The resolution is structural rather than behavioural. Do not ask "will the model judge well?" — ask
"what is the worst outcome if it judges badly?" With the model *inside* a human-signed deterministic
bound, the answer is bounded and acceptable. Outside one, it is not, and no amount of prompt quality
changes that.

## Decision

**A new principal: the Surveyor.** Distinct from the Leadsman, with its own identity, its own
delegation, and a deliberately different temperament — where the Leadsman is discursive and helpful,
the Surveyor is pedantic, literal and conservative. It certifies conformance to a written scope. It
never reasons about whether something is a good idea. It never sees the request queue, other
targets, or the logbook.

**Two tiers, and Tier A is strongly preferred.**

| Tier | When | Enforcement at request time |
|---|---|---|
| **A — compiled** | The whole sentence compiled | Fully deterministic. No model runs. |
| **B — surveyed** | Some clause is unexpressible | Deterministic outer bound **plus** a Surveyor conformance judgment on the residual clause |

The console shows which tier a delegation is, because it is the most important thing about it. A
delegation that could have been Tier A but was written vaguely should be rewritten, and the
compilation report says so.

**The outer bound is mandatory for Tier B.** A delegation with an unexpressible clause and no
compiled bound cannot be signed. "Fix typos in customer names" must still resolve to at least
`UPDATE public.customers(name)`, a row fence or a `max_rows`, and an expiry. The unexpressible clause
*narrows* that bound; it never replaces it.

**The panel.** A conformance judgment is not one call. `n` independent judgments (default 3) run with
distinct framings — one asked to certify, one asked to *refute* conformance, one given only the
compiled bound and the statement with the sentence withheld. **Unanimity is required for
`conforms`.** Any disagreement is a `refer`. Disagreement rate is a monitored metric, and a
delegation whose panel disagrees often is a delegation that needs rewriting.

**Untrusted input handling.** The statement, and any text in it, is untrusted. The compiled bound and
the delegation sentence come from the signed delegation, never from the request. The Surveyor's
output is parsed as one of two enum values and nothing else — no free text is interpreted, and a
response that is not exactly one of the two outcomes is a `refer`. Egress to a model provider goes
through the deployment's egress path
([ZFN-11](https://zrz.io/zfn/11-outbound-http-egress-proxy/)).

**Sampled audit, after the fact.** A configurable proportion of `conforms` decisions (default 10%,
minimum one per delegation per day) is queued for human review *after* execution. A reviewer marking
one wrong produces a logbook finding, notifies the grantor, and — above a threshold — automatically
suspends the delegation's fast path back to `refer` for everything. **This is what makes the design
correctable rather than merely bounded**, and it is not optional.

**Rate limits.** The fast path is quota'd per delegation and per principal at ingress
([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)). A conforming judgment does not make
volume safe.

**The switch is polled state, not an environment variable.** Turning Tier B off must take effect in
seconds, everywhere, without an apply and a deploy. It defaults off.

**Recorded as a decision, not a fact.** Every judgment is a logbook entry naming the Surveyor's
identity, model, prompt-bundle version, the panel's individual verdicts, and the delegation. A marque
minted on the fast path names the judgment in its payload, so "why did this run without a human?" is
answerable exactly.

**Relationship to [EDR-0009](./0009-the-leadsman-is-advisory.md).** That record stands unchanged: the
*Leadsman* has no authority, produces no verdict, and nothing in the approval path reads its output.
The Surveyor is a different principal with a different, strictly-bounded duty. What both share is the
invariant that a model can never create authority a human did not sign — the Leadsman by having none,
the Surveyor by only ever choosing between two paths that both end in a human-granted scope.

## Consequences

**Easier.**

- Delegations can be written the way people actually think about permissions, without the
  compile-or-nothing cliff pushing them toward wider grants.
- The blast radius of a model error is a bounded, stated quantity rather than an open question.
- The audit sample turns model quality into something measured on live traffic instead of assumed.

**Harder.**

- **This is the most dangerous feature in the system**, and it should be read as such. Every
  constraint above is load-bearing; removing any one of them — the outer bound, unanimity,
  default-refer, the audit sample, the kill switch — changes the risk category, not the risk level.
- **Latency and cost on the fast path.** Three model calls before a statement runs is slower than a
  deterministic check and is not free. That is a reason to prefer Tier A, which is the intended
  pressure.
- **Sampled audit needs a human who actually does it.** An audit queue nobody reads makes the
  design's central mitigation decorative, so the queue's age is alerted and the suspension threshold
  is automatic rather than discretionary.
- Prompt-injection resistance is an ongoing arms race with an adversary who can write arbitrary SQL
  comments. The structural mitigation — the outer bound — is what holds when the prompt defence does
  not.

**New obligations.**

- The regression suite includes injection attempts, near-miss statements just outside a scope, and
  statements that conform in form but not in intent. A prompt or model change that flips any of these
  to `conforms` fails the build.
- Panel disagreement rate, fast-path volume, audit-queue age and audit failure rate are dashboarded
  and alerted. **Silence is ambiguous**: a Surveyor that has quietly stopped referring anything looks
  identical to one with an unusually well-scoped tenant.

## References

- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — the Surveyor is a principal, and its
  judgments name it.
- [ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/) — quota the fast path.
- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — uncertainty resolves toward refusal.
- [EDR-0016](./0016-natural-language-delegations-are-compiled.md) — what produces the bound this runs
  inside.
- [EDR-0009](./0009-the-leadsman-is-advisory.md) — the analyst, which remains authority-free.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Amended after review: a `surveyed` marque names the human-signed outer bound in its `auth` block, and the Pilot verifies **that bound** rather than the judgment ([EDR-0029](./0029-the-fast-path-authority-chain.md)). A control plane skipping the Surveyor is equivalent to one that answered `conforms`, and both are contained by the signed bound — which is what this record claimed, now stated as a verification property rather than an intention.
