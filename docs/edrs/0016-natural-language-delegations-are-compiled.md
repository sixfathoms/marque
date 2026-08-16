---
id: 16
title: "Compile a written delegation, and have the human sign the compilation"
summary: "A delegation may be written in plain language. A model compiles it into a structured scope, the grantor reads and signs the compiled form, and enforcement runs entirely on the compilation — never on the sentence."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, product, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

You can write a delegation as a sentence:

> *Sam can always update the `settings` field on `accounts`, for sandbox accounts, up to 100 rows.*

A model **compiles** it into the structured scope of
[EDR-0007](./0007-delegation-by-containment-proof.md). The grantor is then shown **the compilation,
not their sentence**, and signs that. From then on, nothing about enforcement involves a model or the
original text: the compiled scope is what is checked, fenced and asserted.

Three rules make this safe:

1. **The signature is over the compilation.** The sentence is provenance, kept in the logbook. The
   compiled scope is the authority.
2. **The compiler must refuse to guess.** Anything it cannot express as a structured scope is
   reported as unexpressible, and the grantor either rewrites the sentence or accepts a narrower
   compilation. A partial compilation is never silently completed.
3. **An unbounded compilation is refused.** A scope with no row fence and no `max_rows` on a table
   above a configured size cannot be signed — the grantor must supply a bound. "Update status on
   orders" is four million rows if nobody asks.

## Context

[EDR-0007](./0007-delegation-by-containment-proof.md) gives a sound, enforceable delegation
model — and it is written in JSON with table names, column lists and SQL predicates. That is a fine
format for a machine and a poor one for the person who actually knows what Sam should be allowed to
do. The people best placed to scope a delegation are frequently not the people who will write a
correct `fence` expression, and the gap between them is where delegations either do not get created
at all or get created too wide.

The obvious fix is to accept the sentence and have a model interpret it *at request time*, for every
request. That puts a model in the authorisation path permanently, and makes every future
authorisation decision depend on the model reading the same sentence the same way — which it will
not reliably do, and which nobody can audit after the fact.

Compiling once inverts that. The model does the part it is genuinely good at — turning intent into
structure — at the moment a human is present to check the result. The output is a static artefact.
Every subsequent decision is deterministic, reproducible, and inspectable, and re-running the
compiler tomorrow cannot change what Sam is allowed to do today.

This is the same shape as
[ZFN-26](https://zrz.io/zfn/26-ai-assisted-content-cosign/): a model may draft, and a human co-signs
what goes out under their name. Here the co-signature is literal.

## Decision

**The compile step.** Given a sentence, a target and a role, the compiler emits a candidate
delegation plus a per-clause account of how it got there:

```jsonc
{
  "source": "Sam can always update the settings field on accounts, for sandbox accounts, up to 100 rows.",
  "compiled": {
    "to": "sam@acme.example", "target": "prod-primary", "role": "settings_writer",
    "operations": ["UPDATE"],
    "objects": [ { "table": "public.accounts", "columns": ["settings"] } ],
    "fence": "tier = 'sandbox'",
    "max_rows": 100,
    "not_after": "2026-11-30T00:00:00Z"
  },
  "derivation": [
    { "clause": "update … settings … on accounts", "became": "UPDATE on public.accounts(settings)",
      "confidence": "exact", "schema_evidence": "column public.accounts.settings exists" },
    { "clause": "for sandbox accounts", "became": "tier = 'sandbox'",
      "confidence": "inferred", "schema_evidence": "public.accounts.tier has values sandbox|trial|production" },
    { "clause": "always", "became": "not_after defaulted to 90 days",
      "confidence": "policy_default", "note": "delegations cannot be perpetual" }
  ],
  "unexpressible": []
}
```

**The grantor signs the compilation.** The console and CLI show the compiled scope first and the
sentence second, with every `inferred` clause flagged. `marque delegate --from-text` prints the
compiled form and requires explicit confirmation of *it*. The signed payload covers the compilation;
the sentence and the derivation are recorded in the logbook as evidence of intent
([EDR-0012](./0012-the-logbook-is-append-only.md)).

**"Always" never means perpetual.** Every delegation carries `not_after`
([ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/)). A sentence saying "always" compiles to the
deployment's default period with the substitution shown, and the grantor may extend it up to what
policy permits — never beyond.

**Refusals are explicit, and are the interesting output.** The compiler reports rather than resolves:

| Situation | Result |
|---|---|
| A named table or column does not exist | Refused, naming what was not found |
| A clause has no structured equivalent ("fix obvious typos") | Listed in `unexpressible`; see [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) |
| The scope has no row bound on a table above the size threshold | Refused; the grantor must supply one |
| The sentence is ambiguous between two readings | Refused, with both readings shown, rather than a choice made silently |
| The compilation exceeds the grantor's own authority | Refused; attenuation is checked against the grantor, not the sentence |

**Attenuation is checked on the compilation.** A grantor cannot compile their way to more than they
hold. The check in [EDR-0007](./0007-delegation-by-containment-proof.md) runs against the compiled
scope exactly as if it had been written by hand.

**Schema evidence is required for an inference.** The compiler is given the target's schema — column
names, types, and for low-cardinality columns their distinct values — through the Pilot, read-only.
An inference with no schema evidence behind it (`tier = 'sandbox'` where no `tier` column exists) is
a refusal, not a guess.

**Recompilation is a new delegation.** Editing the sentence produces a new compilation requiring a
new signature. An existing delegation is never re-derived, so improving the compiler cannot change
what someone is already permitted to do.

## Consequences

**Easier.**

- The people who understand the domain can write delegations, which is the difference between this
  feature existing and not.
- The review is on the compiled form, so it is precise, diffable and short — usually easier to check
  than the sentence.
- The derivation makes the model's reasoning inspectable at exactly the moment a human can correct
  it.

**Harder.**

- **A grantor may sign a compilation they did not read carefully.** This is the central risk, and it
  is the same one as [EDR-0009](./0009-the-leadsman-is-advisory.md): a plausible summary invites
  trust. Flagging inferred clauses and refusing unbounded scopes reduces it; nothing removes it.
- **The refusal rate will be annoying at first.** Ambiguity refusals in particular will feel pedantic
  when the intent seems obvious. Resolving ambiguity silently is the alternative, and it is worse.
- Schema access for the compiler is a new read path from the control plane's analyser through the
  Pilot, and a new place where column names and sample values are handled.
- Compilation quality varies with the model, so the compiler is versioned and covered by a regression
  suite over known sentences ([EDR-0009](./0009-the-leadsman-is-advisory.md) sets the precedent).

**New obligations.**

- The compiler's test suite includes adversarial sentences — ones that try to compile to more than
  they appear to say, and ones with injected instructions in the text — and a failure to refuse is a
  build failure.
- Compiled delegations are reviewed at renewal against their sentence, which is when a drifted
  compilation is caught.

## References

- [ZFN-26](https://zrz.io/zfn/26-ai-assisted-content-cosign/) — a human co-signs what a model drafts.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — "always" is still a lease.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — what a compilation compiles *to*.
- [EDR-0017](./0017-conformance-matching-may-route-never-widen.md) — what happens to the clauses that
  will not compile.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Amended after review: the signed compilation supplies the approver limb of a marque minted by a delegation match, and travels with it so a Pilot can verify offline that a human signed the scope ([EDR-0029](./0029-the-fast-path-authority-chain.md)).
