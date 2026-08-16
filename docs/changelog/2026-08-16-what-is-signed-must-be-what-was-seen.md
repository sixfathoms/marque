---
title: "A second panel: an authentic signature over a payload nobody saw"
tags: [security, docs]
order: 2
---

A second expert panel reviewed the records the first never saw — EDR-0028 through 0035 — plus the
nineteen amendments the first review prompted. It found something that defeats the design *after* it
had already been reviewed once and hardened, and the fix is
[EDR-0036](/edrs/0036-what-is-signed-must-be-what-was-seen/).

### The finding

The console is static assets served **from the same origin as the API** — that is, by the
Harbourmaster. A WebAuthn assertion attests **user presence and consent to tap**; it attests nothing
about the payload, because the challenge is an opaque 32-byte digest.

So a compromised control plane serves modified console assets, renders one marque on screen, and
calls the authenticator with the digest of a *different* one.

The signature that comes back is genuine. The key is on the roster. `approvals.required` is satisfied.
`cnf.jkt` verifies. **Every offline check added by EDR-0029, 0030, 0031 and 0032 passes perfectly** —
on a payload no human ever saw. The approver limb is not forged; it is **induced**.

Two companion findings shared the root cause. `approvals.required` is composed by the Harbourmaster
*before any signature exists*, so the adversary authors the requirement it is bound by. And `cnf.jkt`
had no anchored source at all — [EDR-0034](/edrs/0034-the-pilot-api-has-one-authorisation-model/)
reached for "the enrolled principal set distributed with it", a phrase with no schema, no author, no
k-of-n rule and no genesis root anywhere in 35 records. That one sentence quietly reopened the
extraction oracle that record was written to close.

### The lesson

**A signature is only as good as three things: the key set, the payload the signer actually saw, and
the policy that payload claims to satisfy.**
[EDR-0031](/edrs/0031-approver-keys-are-anchored-outside-the-control-plane/) anchored the first.
Anchoring one of three is not a third of the property — it is none of it.

Galling, because the design had already got this right twice elsewhere.
[EDR-0016](/edrs/0016-natural-language-delegations-are-compiled/) makes a grantor sign **the
compilation, not the sentence**, precisely because a human must sign the artefact they can read.
[EDR-0028](/edrs/0028-statement-pipeline-and-provider-spi/) takes the digest **after** transformation
for the same reason. Neither insight was carried to the surface that renders the payload.

### Changed

- **Policy becomes an anchored artefact**, co-signed by k approvers and epoch-chained like the roster.
  A Pilot **recomputes** the approval requirement and refuses a marque whose payload disagrees.
- **The payload carries a signed `display`** — a canonical rendering shown verbatim to the signer and
  re-renderable by an auditor, so a substitution is detectable from the artefact alone.
- **The roster becomes the *principal* roster.** Entries gain a capability, so operator and agent keys
  ride the same anchored artefact and `cnf.jkt` resolves against something real.
- **`critical` signing leaves the browser.** `signing_surface: local` is now a setting distinct from
  `require_envelope`, defaulting `critical` to the installed CLI, which renders exactly what it signs.

### A regression of ours, corrected

Making `webauthn` the required envelope on `critical` targets — added a day earlier — was **worse than
what it replaced**. It forced the highest-consequence approvals into the browser, the one surface the
control plane serves, and excluded the locally-installed CLI. Envelope governs the *key*; surface
governs *who renders*. They are orthogonal and were conflated.

The cost of the fix is real and is not hidden: **`critical` approvals are no longer possible from a
phone**, which is exactly what [EDR-0024](/edrs/0024-the-console-is-for-deciding/) was optimised for.
The browser was never a sound signing surface against an adversary who serves it.

### Four more criticals from the same panel

Round two also found four execution-layer defects, all confirmed and all now closed:

- **`search_path` was never pinned.** PostgreSQL resolves unqualified relations, functions **and
  operators** through it, so a fence written as `tier = 'sandbox'` can be redefined by anyone able to
  create an object in an earlier schema. The session now pins `search_path = pg_catalog` and a fence
  containing an unqualified non-builtin reference is refused at compile time.
- **A `DEFERRABLE INITIALLY DEFERRED` constraint trigger fires at `COMMIT`** — *after* the write-set
  assertion has read a clean write set — so its writes landed inside the committed transaction
  unchecked, by a mechanism designed to defer until commit. `SET CONSTRAINTS ALL IMMEDIATE` now runs
  immediately before the check.
- **The write-set assertion could not tell "nothing was written" from "the write was not counted".**
  Both read zero, so `track_counts` being off silently degraded the strongest containment in the
  design into a no-op reporting success. It now calibrates against the statement's own `RETURNING`
  count and refuses if the counters disagree.
- **A transform provider would have broken the standing-order fast path**, because
  [EDR-0029](/edrs/0029-the-fast-path-authority-chain/) requires the Pilot to recompute
  `template + binding` offline and match `req`. Transforms no longer run there; tenant scoping on a
  standing order belongs in the signed template.

Plus two majors: a fence may now reference only columns of the target relation (REPEATABLE READ
protects rows *this* transaction writes, not a tenant row a concurrent transaction changes), and
Marque's role must not **own** the logbook table — an owner can grant itself anything, which would
have made the withheld `DELETE` grant decorative.

### The rest of round two's survivors

Fourteen more, all now closed. Three changed a security property rather than a specification:

- **`approvals` collapsed a conjunction into a disjunction.** A flat `required: 2` over a flat
  `eligible: [sam, group:data-oncall]` is not a stage-preserving encoding of
  [EDR-0019](/edrs/0019-escalation-is-a-chain/)'s chain — it was satisfiable offline by **two members
  of data-oncall with no signature from Sam at all**, on a chain whose first stage was Sam. The block
  now mirrors the chain's stages, each threshold met by distinct principals from that stage's own set.
- **Withholding a roster silently extended key validity.** With no `next_update` and no stated failure
  action, a Pilot on a stale roster kept honouring keys retired in an epoch it never saw. It now
  refuses past the bound, so withholding degrades to denial of service. The epoch high-water mark must
  also be durable — a memory-only one resets on every deploy, which is the rollback defence gone.
- **The WebAuthn envelope checked the challenge and nothing else.** No `type`, no `origin`, no
  rpIdHash, and no ceremony domain separation — so an assertion was replayable from another origin, or
  an enrolment assertion replayable as an approval. The whole W3C §7.2 set is now specified.

And: `"within"` is decidable for a fence (syntactic identity — predicate containment is undecidable
and a semantic reading would approximate in the permissive direction); the revocation list's `revoked`
array is typed, because the fast path verifies *artefacts* and a list of marque ids cannot revoke the
standing order that keeps minting them; `require_key_backing` splits from `require_envelope`, since
`es256` covers both a Secure Enclave key and the file fallback; and a `40001` abort now has a state to
land in (`aborted_not_applied`), which [EDR-0007](/edrs/0007-delegation-by-containment-proof/) had
been claiming without one.

Two corrections of overstatement, both ours: the compromised-control-plane residual **counted rate
limits and budgets that the compromised component itself enforces** — fast-path volume is unbounded
against it, and that is now said; and EDR-0033 claimed EDR-0026's capability table had gained
`fence.write_set`, which it had not.

