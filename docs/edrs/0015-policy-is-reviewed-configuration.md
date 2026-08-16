---
id: 15
title: "Who may approve what is reviewed configuration, not a console setting"
summary: "Targets, roles, approval policy and standing orders live in a versioned repository, are applied by a signed change, and every applied version is recorded in the logbook. Delegation is the runtime path."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, ops]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Marque's **configuration** — targets, roles, approval policy, standing orders, groups — is a
declarative document in a version-controlled repository. It is changed by a reviewed pull request and
applied by `marque policy apply`, which is itself an authenticated, signed, logged act.

Marque's **runtime authority** — delegations — is granted in the product, by people, within the
bounds configuration allows ([EDR-0007](./0007-delegation-by-containment-proof.md)).

The line between them: **configuration says who is allowed to grant; delegation is granting.** A
change to who can approve production writes should be as hard to make quietly as a change to
production infrastructure, and should be reviewable by someone who was not in the room. A change to
who may unlock sandbox accounts this month should take thirty seconds.

There is no console screen that widens approval authority. That is deliberate, and it is the first
thing someone will ask for.

## Context

Approval policy is the part of an approval system that gets quietly loosened. Not maliciously —
someone is on holiday, a release is blocked, an approver is needed at 2am and the on-call engineer
adds themselves to the approver group "just for tonight". Nobody removes it. Six months later the
group is everyone, and the control has decayed to nothing without a single decision having been made
to remove it.

Two properties defeat that decay. **Review**: a change is proposed, seen by someone else, and merged,
which is enough friction to make "just for tonight" visible. **Expiry**: temporary access is
expressed as something that ends by itself, so the holiday case does not need a permanent change at
all — which is exactly what delegation is for.

Configuration-as-reviewed-code also solves the disaster-recovery question. If the policy exists only
in Marque's database, restoring Marque means restoring who could approve what from a backup, and
being unsure whether it is current. With the repository as the source, the answer is `apply`.

## Decision

**The document.** One declarative file set, engine-agnostic:

```jsonc
{
  "targets": [ { "name": "prod-primary", "engine": "postgres", "criticality": "critical",
                 "pilot": "pilot-us-west-2", "displayable_columns": [ … ],
                 "require_key_backing": "hardware", "signing_surface": "local",
                 "require_execution_presence": false } ],
  "roles":   [ { "role": "settings_writer", "target": "prod-primary",
                 "db_user": "app_settings_writer", "credential": { "kind": "aws-iam" },
                 "criticality": "sensitive" } ],
  "groups":  [ { "name": "support", "from": "idp:group:support-engineers" } ],
  "policy":  [ { "targets": ["prod-primary"], "roles": ["settings_writer"],
                 "approvers": ["group:data-oncall"],
                 "min_approvals": 1, "self_approval": false,
                 "may_delegate": ["group:data-oncall"],
                 "max_marque_ttl": "1h", "max_grace_seconds": 0,
                 "require_envelope": "webauthn", "signing_surface": "local",
                 "surveyor": { "jurors": 3 },
                 "emergency_approvers": ["group:incident-command"],
                 "urgency_may_collapse_stages": false,
                 "may_grant_unbounded_break_glass": ["theo@acme.example"],
                 "default_budget": { "executions": 1 } } ],
  "standing_orders": [ … ]
}
```

**Rules that are structural, not conventional:**

- **Groups come from the identity provider.** Marque does not maintain a membership list, so
  offboarding happens in one place ([EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md)).
- **`self_approval` defaults to false**, and setting it true on a `critical` target requires an
  explicit acknowledgement field naming who accepted the risk. A one-person team genuinely needs it;
  it should not be reachable by accident.
- **`min_approvals` may exceed one.** Two signatures on a marque
  ([EDR-0004](./0004-marques-are-signed-leases.md)) is a natural extension of a format that already
  carries multiple.
- **`max_marque_ttl` is a ceiling on what an approver may grant**, not a default. An approver cannot
  sign a longer window than policy permits.
- **`max_grace_seconds` is the same ceiling for revocation grace**, and defaults to **zero**. Without
  it, one approver could mint a marque whose `grace` covers its entire life, which is a standing
  credential wearing a lease. Raising it above zero requires the same explicit acknowledgement field
  `self_approval` needs ([EDR-0004](./0004-marques-are-signed-leases.md)).
- **`require_key_backing`** is what actually expresses "not a file on a laptop": `hardware` or `any`,
  checked against the backing recorded in the signer's roster entry
  ([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)). `critical` targets
  default to `hardware`. The envelope was the wrong proxy for this — `es256` covers both a Secure
  Enclave key and the file fallback, so selecting on the envelope excluded a hardware CLI key while
  admitting nothing it meant to.
- **`require_envelope`** remains, for a deployment that genuinely wants to constrain the wire
  envelope ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)). It is not the `critical`
  default any more.
- **`signing_surface` names where the payload is rendered and signed**, which is a *different*
  question and was originally conflated with the one above. `local` requires locally-installed code
  the control plane does not serve; `critical` targets **default to `local`**
  ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)). Making `webauthn` the `critical`
  default on its own pushed those approvals into the browser — the surface the control plane serves —
  which was strictly worse than the file-backed key it was guarding against. Relaxing either on a
  `critical` target requires the acknowledgement field.
- **`surveyor.jurors` sets the Tier-B panel size**, with a floor of 3
  ([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)).
- **`may_delegate` bounds who can create delegations**, and a delegation may never exceed the
  delegator's own policy grant.
- **`emergency_approvers`** join every stage's eligible set when a request is marked urgent
  ([EDR-0037](./0037-emergency-paths.md)). They are additional, never a replacement.
- **`urgency_may_collapse_stages`** decides whether urgency may reduce a multi-stage chain to one,
  and defaults to **false**. On, it lets urgency manufacture authority nobody granted, which is why it
  is a deliberate per-target choice rather than a global behaviour.
- **`may_grant_unbounded_break_glass`** names who may issue a break-glass grant whose `scope` is
  `any`. That is the widest object in the system, so granting it is its own permission rather than a
  value someone can type ([EDR-0037](./0037-emergency-paths.md)).
- **Break-glass grants themselves are not policy** — they are per-principal signed artefacts, granted
  in the product like a delegation, bounded by what policy above permits.

**Applying produces an anchored artefact.** The applied version is co-signed by **k approver device
keys** and epoch-chained, in the same family as the roster
([EDR-0031](./0031-approver-keys-are-anchored-outside-the-control-plane.md)), and distributed to
Pilots — which is what lets a Pilot **recompute** an approval requirement rather than believe the one
in a marque's payload ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)). The control plane
transports the artefact and cannot author it.

**Applying.** `marque policy apply` requires a fresh authentication, prints a **diff of authority**
rather than a diff of text — "adds 4 people to who can approve writes on prod-primary", "widens
`settings_writer` from 1 column to 3" — and requires confirmation of that summary. The applied
version's digest, its diff, and the applier's identity are appended to the logbook
([EDR-0012](./0012-the-logbook-is-append-only.md)).

**Refusals are loud.** A policy that would leave a target with no eligible approver, grant approval
over a target to a group that does not resolve, or declare a `transform` provider on a target that
also has standing orders, is refused at apply time with the reason. That last one is the composition
[EDR-0028](./0028-statement-pipeline-and-provider-spi.md) forbids: a transform changes the statement,
and a fast path exists precisely because a human signed the statement's shape in advance — so the
combination is caught where it is configured rather than discovered at execution.
An empty approver set must never silently mean "anyone" or "no one" — the first is a hole and the
second is an outage.

**Emergency changes exist and are conspicuous.** `--break-glass` applies without a merged review, and
it: requires two authenticated principals present, sets an automatic expiry after which the previous
version is restored unless the change has been merged, and posts to the deployment's notification
channel immediately. The emergency path is not blocked; it is made impossible to use invisibly.

**Both epochs are signed at apply time.** A policy version is a k-of-n co-signed artefact
([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)), and an *automatic* reversion has no
signers present — so the break-glass ceremony pre-signs **both** the change and its reversion, and
the automatic step is the publication of an already-signed artefact rather than an unsignable act.
Its two principals must therefore be approver-key holders. Without this the reversion either ships an
unsigned artefact no Pilot accepts, or leaves Pilots enforcing the break-glass policy while the
control plane believes it reverted — the worst of both.

**Reversion is an apply, not a restore.** The automatic reversion runs the **same validation** as an
ordinary apply, including the refusal rules above. If the previous version no longer validates — a
group has since emptied, a target has gone — the reversion **does not proceed**: the break-glass
version is held in place and the deployment is alerted loudly. Silently restoring a policy that
cannot be applied would be the worst of both outcomes. Notification goes out **before** expiry as
well as at it, and **both the attempted reversion and its outcome are recorded in the logbook** — a
reversion that did not happen is otherwise indistinguishable from one that did.

## Consequences

**Easier.**

- Policy drift is visible in a history someone can read, and "who widened this, when, and who
  reviewed it" is answerable.
- Standing up a second deployment, or rebuilding after a loss, is applying the repository.
- Reviewers see an authority diff rather than JSON, so the review is about the consequence rather
  than the syntax.

**Harder.**

- **Routine changes need a pull request**, which is friction for small legitimate things. Delegation
  is the intended answer, and if people are opening pull requests for day-to-day access then
  delegation is under-used and that is the thing to fix.
- **Two places to look** — repository for policy, product for delegations — and someone will look in
  the wrong one. The console shows both on one screen, sourced from the journal, precisely so the
  effective answer is in a single place even though the inputs are not.
- The authority diff is real work to compute correctly, and a wrong summary is worse than no summary
  because it will be trusted.
- Break-glass with automatic reversion can revert a change that turned out to be needed. Loud
  notification and a generous window are the mitigation; it is a real trade.

**New obligations.**

- The repository holding policy is itself protected: required review, no self-merge, and its access
  is part of Marque's threat model rather than an assumption.
- Automatic reversion is tested. An untested reversion path will fail on the night it matters.

## References

- [ZFN-1](https://zrz.io/zfn/1-engineering-decision-records/) — decisions written down and cited.
- [ZFN-47](https://zrz.io/zfn/47-govern-the-contract-between-teams/) — govern the contract centrally,
  and let teams operate within it.
- [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) — temporary authority expires by itself.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the runtime half of authority.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: added `max_grace_seconds`, `require_envelope` (hardware on `critical` by default) and `surveyor.jurors`, and specified break-glass reversion as an apply that can refuse rather than a silent restore.
- **2026-08-16**: Amended after a second expert panel: separated `signing_surface` from `require_envelope` — conflating them had pushed `critical` approvals into the browser, which is worse than the file-backed key it was guarding against — and made the applied policy version an anchored, co-signed artefact ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)).
- **2026-08-16**: Amended after the second panel's should-fix pass: split `require_key_backing` from `require_envelope` — the envelope was the wrong proxy, since `es256` covers both a Secure Enclave key and the file fallback — and made a `transform` provider on a target carrying standing orders a loud refusal at apply time.
- **2026-08-16**: Amended after the second panel's synthesis: break-glass now pre-signs both epochs, since an automatic reversion has no signers and cannot produce the k-of-n artefact [EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) requires; added the per-target signing and presence fields.
- **2026-08-16**: Amended for the emergency paths and operator surfaces: added `emergency_approvers`, `urgency_may_collapse_stages` (default false) and `may_grant_unbounded_break_glass` ([EDR-0037](./0037-emergency-paths.md)).
