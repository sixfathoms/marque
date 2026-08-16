---
id: 4
title: "A marque is a doubly-signed lease, verified by computation"
summary: "A marque is a JWS carrying both the approver's signature and the control plane's, binding one statement digest to a role, a window and an execution budget. The Pilot verifies it locally; only revocations are looked up."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, execution, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

A marque is not a row that says "approved". It is a signed artefact carrying **two** signatures:

- the **approver's**, made with their own device key — *this human agreed to this exact statement*;
- the **Harbourmaster's** — *that human was permitted to agree to it, under policy, at that moment*.

The Pilot requires both. Neither party can manufacture a valid marque alone: a compromised control
plane can attest to an approval nobody made but cannot forge the human's signature, and a stolen
device key can approve things its holder was never entitled to approve but cannot get the
countersignature.

The marque binds a **statement digest**, a target, a role, a submitter, a `not_before`, an `expires`
and an **execution budget**. The Pilot verifies all of it by computation, offline. The only thing it
looks *up* is whether the marque has been revoked — and revocations are the rare case, so
revocations are what gets stored.

## Context

The obvious design is a table: `requests` with an `approved_by` column, and the executor asks "may I
run request 4172?". It is simple, and it puts the control plane on the critical path of every
execution, in a system that exists to be used when things are broken
([ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/)). It also means the control plane's
database *is* the authority: anyone who can write that row — an SQL injection, a compromised admin,
a bug in the policy code, a careless migration — has approved everything.

[ZFN-49](https://zrz.io/zfn/49-verify-by-computation-not-lookup/) says the opposite: make
verification a computation, pass the claims by value, and where revocation is rarer than issuance,
store the revocations rather than a row per grant. That fits exactly. Marque will issue thousands of
grants and revoke a handful; the revocation list stays small precisely because every marque expires
on its own.

[ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) supplies the rest of the shape: a grant that
can outlive its holder is a standing credential scheduled for later. Every marque has a TTL, a named
owner, and server-side expiry, and its side effects are fenced so a stale holder cannot re-apply
them.

The two-signature requirement is not from a Field Note; it comes from asking what a control-plane
compromise buys an attacker. With one signature — the server's — it buys everything, because the
server is what decides. With the approver's signature required as well, it buys the ability to
*ask*, and nothing else.

## Decision

**Representation.** A marque is a JWS in general JSON serialisation (RFC 7515), which carries
multiple signatures over one payload natively. The payload:

```jsonc
{
  "mrq": "mrq_01JB2Q9F3K8Z",           // identifier, used for revocation
  "deployment": "acme-production",
  "target": "prod-primary",
  "role": "settings_writer",
  "sub": "operator@acme.example",       // who may execute; nobody else
  "req": "sha256:9f2c…",                // digest of the canonical statement set
  "stmt_count": 1,
  "nbf": 1786953600,
  "exp": 1786957200,
  "budget": { "executions": 1, "max_rows": 250 },
  "fence": ["tier = 'sandbox'"],        // EDR-0007, applied by the Pilot
  "revocation": { "policy": "required", "grace_seconds": 0 },
  "act": [ … ],                          // delegation chain, if any
  "analysis": "sha256:1a7e…",           // what the approver was shown
  "display": "…",                        // canonical rendering, shown verbatim to the signer (EDR-0036)
  "objects": [ … ],                      // write-set reference set (EDR-0033)
  "machinery": "sha256:…",               // relation fingerprint, re-checked at execution (EDR-0033)
  "roster_epoch": 47,                    // which epoch signatures resolve against (EDR-0030)
  "justification": "…",                  // required when auth.kind = break_glass (EDR-0037);
                                         //   bound here so it cannot be added or edited after
  "urgent": false, "stages_collapsed": false,   // EDR-0037, recorded on the artefact
  "require_execution_presence": false    // EDR-0035; monotone — the Pilot requires it if EITHER
                                         //   this or the anchored policy says so
}
```

Two signatures are required, and the Pilot rejects a marque carrying fewer:

| Signature | Key | Asserts |
|---|---|---|
| `approver` | the approver's enrolled device key — a WebAuthn credential in the console, a platform key from the CLI ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)). **Not necessarily the DPoP key**: in the console the two are different keys | a named human read this payload and agreed to it |
| `authority` | the Harbourmaster's signing key, from the deployment's KMS or key store | policy permitted that approver, for that target, at that time |

**Algorithms are named, not left to the key store.** `authority` is **ES256**; `approver` is ES256 in
the `es256` envelope and the authenticator's algorithm in the `webauthn` one, restricted to ES256 or
Ed25519. A verifier rejects any other `alg`, and the accepted set is deployment configuration with no
"whatever the JWS header says" path — that is the classic downgrade.

**`req` is the identity of the request.** It is a digest over the canonicalised statement text — not
the request id. An approver who edits the statement produces a different digest and therefore a
different marque, and the logbook shows both what was submitted and what was signed. A marque cannot
be moved to a different statement, because the statement is what it names.

**`analysis` binds the advice.** The digest of the analysis the approver was shown is inside the
signed payload, so "the approver saw a report that said this was 3 rows" is provable after the fact,
and a later re-analysis cannot quietly replace what they read.

**Verification is local.** The Pilot checks the signatures, the window, the caller, and the statement
it was handed against `req`, without asking the Harbourmaster anything. Two parts of that sentence as
originally written have since been replaced, and they are named here because this record is where a
reader is sent first:

- **not "the deployment's JWKS"** — approver keys come from the per-tenant anchored roster
  ([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)). A control-plane-served
  key set is the exact construction that record exists to close.
- **not "the subject against the authenticated caller"** — the caller proves possession of `cnf.jkt`
  ([EDR-0032](./0032-a-marque-binds-its-executor-tenant-and-pilot.md)); `sub` is recorded for the
  logbook and policy, not checked as a credential.

> [!NOTE]
> Two later records complete this payload and this rule, and both were written after review found
> them missing:
>
> - **[EDR-0029](./0029-the-fast-path-authority-chain.md)** adds `auth`, naming the artefact that
>   authorised a marque minted with no human present — a standing order or a compiled delegation,
>   each already human-signed. On those paths the approver limb is satisfied by that artefact, which
>   travels with the marque and is verified offline. **The human signed the shape, not the instance.**
> - **[EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)** adds `approvals`, so the
>   payload states how many distinct approvers are required and who was eligible. "At least one
>   approver signature" was not sufficient: JWS signature entries are independent, so a two-approver
>   marque could be stripped to one and still verify.

**Revocation is a pull, not a question.** The Harbourmaster publishes a **signed revocation list** —
an artefact with a defined shape, because it is the single value that decides whether any marque in
the estate is executable:

```jsonc
{ "tenant": "acme", "sequence": 9182, "issued_at": "…", "next_update": "…",
  "revoked": [ { "kind": "marque",        "id": "mrq_…" },
               { "kind": "standing_order", "id": "sto_…", "digest": "sha256:…" },
               { "kind": "delegation",    "id": "dlg_…" },
               { "kind": "task",          "id": "tsk_…" },
               { "kind": "roster_entry",  "jkt": "…" } ] }
```

**The revoked set is typed, not a list of marque ids.** The fast path verifies *artefacts* —
standing orders, compiled delegations ([EDR-0029](./0029-the-fast-path-authority-chain.md)) — and a
Pilot that can only revoke individual marques cannot revoke the thing that keeps minting them. The
Pilot already refuses on artefact expiry, so this is one more predicate in the same place.

**Who signs it is a stated limitation.** The list is signed by the control plane, which means a
compromised Harbourmaster can **suppress** a revocation (bounded by `next_update`, after which a
`required`-policy Pilot refuses) and can **forge** one (a denial of service, and a visible one). It
cannot use it to authorise anything. Co-signing it with k approvers like the roster
([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)) would close the forgery
half, and was **not** adopted: revocation must be fast, and a k-of-n ceremony in the path of an
urgent revocation is the wrong trade. The asymmetry is deliberate — the roster grants authority and
must be co-signed; the revocation list only ever removes it.

- **Signed, sequenced and self-dating.** Staleness is measured against the **signed** `issued_at` and
  `next_update`, cross-checked against monotonic elapsed time since fetch — never against local wall
  clock alone. A Pilot **refuses a list whose `sequence` is lower than one it already holds**, so an
  older signed list cannot be replayed.
- Bounded by the maximum marque lifetime: nothing stays on it longer than the thing it revokes.
- It is served by the control plane, so **a control-plane outage stops `required`-policy execution
  once the list goes stale.** That asterisk on the offline property is real and is stated wherever
  the property is claimed, rather than being quietly true only for `grace` marques.

`revocation.policy` is per-marque:

- `required` (the default) — the Pilot refuses if its list is older than the refresh interval.
  Security over availability, per [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/).
- `grace` — the Pilot may execute with a list up to `grace_seconds` stale. Reserved for marques an
  approver has deliberately marked with `grace` — a **grace marque**, which is *not* break glass
  ([EDR-0037](./0037-emergency-paths.md)) — and every such execution is flagged in the
  logbook as having run without a fresh revocation check.

**Expiry is server-side and automatic.** `exp` is enforced by the Pilot from its own clock, and the
Harbourmaster reaps expired marques into a terminal state without anyone acting. A marque is never
extended; a longer window means a new approval.

**The budget is a fence, not a hint.** `budget.executions` is decremented by the fencing mechanism in
[EDR-0011](./0011-execution-is-idempotent-and-fenced.md); `budget.max_rows` aborts the transaction if
the statement affects more rows than were approved.

## Consequences

**Easier.**

- An approved marque works while the control plane is down. This is the single most valuable property
  the design has: the tool stays usable during exactly the incidents it exists for.
- A control-plane compromise does not become a data-plane compromise. The attacker can list requests
  and see analyses; they cannot cause anything to run.
- Non-repudiation is real. "I did not approve that" is answerable with a signature over the exact
  payload, including the advice that was on screen.
- Revocation state is small and stays small, because expiry does most of the work.

**Harder.**

- **Approvers now have keys, and keys can be lost.** A new laptop means re-enrolling, and until then
  that person cannot approve. This is genuine friction and there is no version of the property that
  avoids it.
- **Clock skew becomes a correctness concern.** A Pilot with a wrong clock either honours expired
  marques or refuses valid ones. Pilots run NTP, report skew, and alarm on it — but that is
  **self-reported**, and a Pilot in a network with blocked or hijacked time is exactly the one that
  cannot know. The independent check is the signed revocation list: its `issued_at` and `next_update`
  come from the control plane, so a Pilot whose local time disagrees with a freshly-fetched list by
  more than a bounded margin refuses rather than trusting itself. Skew is therefore detected by
  something the Pilot did not generate.
- **Revoking is slower than deleting a row.** There is a bounded window, the refresh interval, in
  which a revoked marque may still execute. Shortening it costs traffic; `required` policy plus a
  short marque lifetime is the mitigation.
- Key rotation has to preserve verifiability of past marques for as long as the logbook is expected
  to be checkable, which means retaining public keys long after the private half is retired.

**New obligations.**

- The canonicalisation of statements is part of the security boundary and needs its own test vectors.
  Two statements that differ only in whitespace must produce the same digest, and two that differ in
  any other way must not.
- Device-key enrolment and recovery is a documented ceremony, not an afterthought — losing every
  approver's key at once must not be an unrecoverable state.

## References

- [RFC 7515](https://www.rfc-editor.org/rfc/rfc7515) — JWS, general JSON serialisation with multiple
  signatures.
- [ZFN-49](https://zrz.io/zfn/49-verify-by-computation-not-lookup/) — verify by computation; store
  revocations, not issuances.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — every lock is a lease.
- [ZFN-2](https://zrz.io/zfn/2-engineering-priority-ordering/) — security over availability, when
  they conflict.
- [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — how the budget is enforced.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-15**: Amended after an expert-panel review found two gaps in this record's payload and
  verification rule. The decision is unchanged — a marque still requires an approver limb and an
  authority limb, and neither party can produce one alone — but *how* the approver limb is satisfied
  on a fast path was never stated ([EDR-0029](./0029-the-fast-path-authority-chain.md)), and the
  "at least one approver signature" rule could not detect a stripped signature
  ([EDR-0030](./0030-a-marque-states-its-own-approval-requirement.md)). A note above points at both.
- **2026-08-16**: Amended after the expert panel's should-fix pass: gave the revocation list a field-level definition (signed `issued_at`, monotonic `sequence`, `next_update`, no downgrade), named the signature algorithms, corrected the claim that the approver's device key is the DPoP key (it is not, in the console), and made the revocation list the independent check on Pilot clock skew.
- **2026-08-16**: Amended after a second expert panel: the payload gains `display`, a canonical rendering covered by every signature ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
- **2026-08-16**: Amended after the second panel's should-fix pass: widened the revocation list's `revoked` array to a typed set — the fast path verifies *artefacts*, and a list of marque ids cannot revoke the standing order that keeps minting them — and stated plainly who signs the list and what a compromised signer can and cannot do with it.
- **2026-08-16**: Amended after the second panel's synthesis: corrected "Verification is local", which was verbatim the construction [EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md) exists to close, and added the payload fields five later records had claimed it carried.
- **2026-08-16**: Amended for the emergency paths and operator surfaces: added `justification`, `urgent` and `stages_collapsed` to the payload — a break-glass justification is bound into what every signature covers, so it cannot be added or edited afterwards ([EDR-0037](./0037-emergency-paths.md)).
- **2026-08-16**: Terminology and staleness fix: stopped calling a `revocation.policy: grace` marque "break-glass" — it is a **grace marque**, and break glass now means one thing only ([EDR-0037](./0037-emergency-paths.md)).
