---
id: 9
title: "The Leadsman advises and can never decide"
summary: "The analyst reads a request and reports what it touches, but has no authority to approve, deny, alter or execute anything. Its output is data attached to a request, and the approval path does not consult it."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, policy, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The **Leadsman** reads a submitted request and produces an analysis: what the statements touch,
what a rehearsal changed, what looks unusual, and a plain-language summary of the intent it inferred.

It has **no authority**. It cannot approve, deny, alter a statement, mint a marque, or execute
anything. Its identity is a principal with exactly one permission — write an analysis to a request it
was given. Nothing in the approval path reads its verdict as an input, because it does not produce a
verdict.

Two rules make this hold under pressure:

1. **Structure the analysis so the model cannot be the only source of a fact.** Row counts come from
   a rehearsal. Table and column lists come from the parser. The model writes the *prose*, and every
   number beside it came from somewhere deterministic.
2. **Absence of analysis never blocks approval, and never speeds it up.** If the Leadsman is down,
   requests queue normally with the deterministic facts attached and a visible note that no summary
   was produced. A dependency that can hold up an incident is not acceptable
   ([ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/)).

## Context

A language model reading SQL and saying what it does is genuinely useful. An approver at 2am looking
at 40 lines of someone else's `UPDATE` will read a good summary and will not read the SQL as
carefully as they think they will. That is the value, and it is also the danger: the summary becomes
the thing that was approved.

The failure to design against is not the model hallucinating — it is **authority creeping toward the
model by convenience**. It starts as a summary, becomes a risk score, becomes a green badge, becomes
"auto-approve anything the analyser rates low". Each step is individually reasonable and the end
state is a language model with write access to production, reachable by anyone who can phrase a
statement persuasively. The request text is attacker-controlled input to the model; prompt injection
in an SQL comment is not hypothetical.

[ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) supplies the identity half: the analyst gets
its own identity and acts under an explicit delegation, so every record names both the model and the
human it acted for. This record supplies the authority half: the delegation it receives contains no
power to decide.

## Decision

**Composition of an analysis.** Each field records where it came from, and the console shows the
provenance:

| Field | Source |
|---|---|
| tables, columns, operations | parser ([EDR-0007](./0007-delegation-by-containment-proof.md)) |
| rows affected, duration, plan | rehearsal ([EDR-0010](./0010-rehearse-before-you-sign.md)) |
| role, role criticality, grants in play | deployment configuration |
| similar past requests | logbook query over statement digests |
| irreversibility notes | deterministic rules — unbounded `DELETE`, missing `WHERE`, DDL, no fence |
| **summary, intent, questions to ask** | **the model** |

The model's contribution is the last row only. Everything else is computed, and is displayed whether
or not the model ran.

**The Leadsman's principal.** It authenticates as itself, with its own workload identity, and its
delegation from the submitter grants exactly: read this request, read schema metadata for this target,
request a rehearsal, write one analysis. It cannot read other requests, other targets, or the
logbook's approval records.

**Untrusted input, treated as such — and it is not only the statement.** Every attacker-controlled
input is delimited and marked: **statement text, its comments and its literals, the agent's declared
`purpose`, the submitter's stated reason, and schema evidence**. An implementer who delimits only the
statement has protected the obvious half and left the reason field, which is free prose from whoever
submitted, wide open.

**The analysis distinguishes an assertion from a fact.** It may say *"the request states this is for
ticket ACME-4471"*; it may not say *"this is for ticket ACME-4471"*. A claim found in untrusted text
is never restated as fact — including claims of prior authorisation, which is the injection that
matters here because it is aimed directly at the approver's judgement. The console renders
submitter- and agent-supplied prose **as a quotation from an untrusted party**, visually distinct
from Marque's own text.

Nothing the model emits is ever parsed as an instruction, a policy decision, or a control character
in Marque's own protocol. Egress from the analyst to a model provider goes through the deployment's
egress path, not directly ([ZFN-11](https://zrz.io/zfn/11-outbound-http-egress-proxy/)).

**No score, no badge, no recommendation.** The analysis contains no numeric risk score and no
approve/reject suggestion, because those are the shapes that get automated against. It may say "this
statement has no `WHERE` clause and would affect 4.2 million rows" — a fact — and it may ask "was
this meant to be scoped to one account?" — a question. It may not say "low risk".

**Versioned and reproducible.** Every analysis records the model identifier and the prompt bundle
version, and both are part of the actor identity. An analysis is bound into the marque by digest
([EDR-0004](./0004-marques-are-signed-leases.md)), so what the approver read is fixed at signing time
and a later re-analysis cannot replace it.

**No model is a supported configuration.** A deployment with no model configured runs everything
except the summary row. That is a legitimate deployment, not a degraded one, and the tests cover it.

**Scope of this record.** It governs the **Leadsman** — the analyst that reads a request and writes a
summary. It is not a blanket statement that no model may appear anywhere in Marque. A separate
principal, the Surveyor, judges whether a request conforms to a scope a human already signed
([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)); it has two possible outcomes, can
only choose a route, and can never widen a bound. The invariant both share, and the one that actually
matters, is that **no model can create authority a human did not sign** — the Leadsman by holding
none at all, the Surveyor by choosing only between two paths that both end in a human-granted scope.

## Consequences

**Easier.**

- Approvers get a real head start on unfamiliar SQL, with the facts separated from the prose so they
  can tell which is which.
- The audit record answers "what did the approver actually see", including which model produced it.
- Model failures, rate limits, provider outages and bad output are all non-events for the control
  path.

**Harder.**

- **Approvers may trust the summary anyway.** Nothing in this design stops a tired human from reading
  the prose and skipping the SQL. Provenance markers and keeping the statement adjacent to the
  summary help; they do not solve it. This is an honest, unresolved limitation.
- **There will be pressure to add auto-approval**, and it will be well-argued. The answer is that
  auto-approval belongs to standing orders ([EDR-0008](./0008-standing-orders.md)) and delegation
  ([EDR-0007](./0007-delegation-by-containment-proof.md)), where the authority is a human's, granted
  in advance, over a shape they read. Both are strictly better than a model's opinion, and both
  already exist.
- Running a model on request text sends production statement text — which may contain customer
  identifiers — to a provider. Which provider, and under what data terms, is a deployment decision
  that has to be made explicitly, and a deployment may correctly decide not to.

**New obligations.**

- Prompt bundles are versioned source, reviewed like code, with a regression suite over known
  statements. A prompt change that makes the analyser miss an unbounded `DELETE` must fail a test —
  and so must one that lets **text claiming prior authorisation** reach the summary as an assertion
  rather than as a quoted claim.
- The analyst's delegation is asserted in tests: a change that gives it any additional permission
  should fail the build, not a review.

## References

- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — agents are principals; delegate, never
  impersonate.
- [ZFN-26](https://zrz.io/zfn/26-ai-assisted-content-cosign/) — the human co-signs; here, literally.
- [ZFN-11](https://zrz.io/zfn/11-outbound-http-egress-proxy/) — route outbound calls through an
  isolated egress path.
- [ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/) — the analyser must never be able to
  block an incident.
- [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) — the separate, bounded principal
  that does gate, and the constraints that bound it.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Added "Scope of this record" to say explicitly that this governs the Leadsman
  rather than every model in the system, and to point at
  [EDR-0017](./0017-conformance-matching-may-route-never-widen.md). The decision is unchanged: the
  analyst still holds no authority, produces no verdict, and is read by nothing in the approval path.
- **2026-08-16**: Amended after the expert panel's should-fix pass: enumerated every attacker-controlled input rather than only statement text, and required the analysis to distinguish "the request asserts X" from "X is true".
