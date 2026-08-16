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
                 "pilot": "pilot-us-west-2", "displayable_columns": [ … ] } ],
  "roles":   [ { "role": "settings_writer", "target": "prod-primary",
                 "db_user": "app_settings_writer", "credential": { "kind": "aws-iam" },
                 "criticality": "sensitive" } ],
  "groups":  [ { "name": "support", "from": "idp:group:support-engineers" } ],
  "policy":  [ { "targets": ["prod-primary"], "roles": ["settings_writer"],
                 "approvers": ["group:data-oncall"],
                 "min_approvals": 1, "self_approval": false,
                 "may_delegate": ["group:data-oncall"],
                 "max_marque_ttl": "1h", "max_grace_seconds": 0,
                 "require_envelope": "webauthn",
                 "surveyor": { "jurors": 3 },
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
- **`require_envelope` names which approver-signature envelope is acceptable**
  ([EDR-0023](./0023-approver-keys-enrolment-and-recovery.md)). A `critical` target **defaults to
  `webauthn`** — hardware-backed, user-verified per signature — so a file-backed platform key cannot
  approve the highest-consequence changes. Relaxing it on a `critical` target requires the
  acknowledgement field.
- **`surveyor.jurors` sets the Tier-B panel size**, with a floor of 3
  ([EDR-0017](./0017-conformance-matching-may-route-never-widen.md)).
- **`may_delegate` bounds who can create delegations**, and a delegation may never exceed the
  delegator's own policy grant.

**Applying.** `marque policy apply` requires a fresh authentication, prints a **diff of authority**
rather than a diff of text — "adds 4 people to who can approve writes on prod-primary", "widens
`settings_writer` from 1 column to 3" — and requires confirmation of that summary. The applied
version's digest, its diff, and the applier's identity are appended to the logbook
([EDR-0012](./0012-the-logbook-is-append-only.md)).

**Refusals are loud.** A policy that would leave a target with no eligible approver, or grant
approval over a target to a group that does not resolve, is refused at apply time with the reason.
An empty approver set must never silently mean "anyone" or "no one" — the first is a hole and the
second is an outage.

**Emergency changes exist and are conspicuous.** `--break-glass` applies without a merged review, and
it: requires two authenticated principals present, sets an automatic expiry after which the previous
version is restored unless the change has been merged, and posts to the deployment's notification
channel immediately. The emergency path is not blocked; it is made impossible to use invisibly.

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
