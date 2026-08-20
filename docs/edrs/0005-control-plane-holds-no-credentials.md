---
id: 5
title: "The control plane never holds a target credential"
summary: "Target credentials live only where connections are made. The Harbourmaster stores a reference; the Pilot dereferences it at connect time using its own workload identity and never returns it."
status: accepted
implementation: none
implementation_note: "cmd/harbourmaster and proto/marque/v1/harbourmaster.proto cite this record; nothing stores a reference, dereferences one, or connects to anything, and no binary links a database driver — all true of three binaries that print a version and exit. From M1 the Harbourmaster links one for its OWN store, which is why the driver mechanism below is EDR-0042's import discipline rather than absence."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, architecture]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The Harbourmaster stores, for each role on each target, a **reference** — a path in a secret store,
or an instruction to mint a credential from an identity. It never stores, receives, caches, logs or
returns the credential itself.

The Pilot resolves that reference at connect time, using its own workload identity, and the resolved
value never leaves the process. Preferred order: **native identity-based auth** (IAM database
authentication, Cloud SQL IAM, `pg_ident`) first; a short-lived minted credential second; a
long-lived stored secret only as a documented carve-out with an owner and a review date.

The consequence that matters: **an attacker who owns the entire Harbourmaster obtains no credential
and no ability to commit a change.** They can read requests and approvals, and — because the control
plane legitimately relays rehearsals — they can relay *operator-signed* reads, so a bounded, quota'd,
target-visible read channel remains ([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)).
They cannot connect to a *target* themselves — the Harbourmaster reaches one database, its own
([EDR-0042](./0042-the-control-plane-keeps-its-own-store.md)).

## Context

[EDR-0004](./0004-marques-are-signed-leases.md) removed the control plane's ability to *authorise*
execution on its own. That is only half the property. If the control plane also holds the database
passwords, compromising it still yields full access to every target — the approval workflow becomes
an elaborate wrapper around a credential vault, and the vault is the thing worth attacking.

[ZFN-35](https://zrz.io/zfn/35-dereference-secrets-not-store-in-config/) gives the rule: config holds
a reference, the workload dereferences it at runtime through its own identity, and rotation needs no
redeploy. [ZFN-16](https://zrz.io/zfn/16-separate-data-plane-control-plane/) gives the placement: the
thing that connects is the thing that holds the connection's secrets.

There is a specific trap worth naming, because it has bitten this exact pattern before. A service
given a *choice* between identity-based auth and a stored secret will silently take the stored-secret
path if the secret is configured, even when the operator believed identity auth was in use — and a
stored secret pointing at an administrative account then connects with far more privilege than
anyone intended, quietly, forever. The configuration must not offer both at once.

## Decision

**Roles are references.** A role definition names a target, a database identity, and how to obtain a
credential for it:

```jsonc
{
  "role": "settings_writer",
  "target": "prod-primary",
  "db_user": "app_settings_writer",
  "credential": { "kind": "aws-iam" }
  // or { "kind": "gcp-iam" }
  // or { "kind": "secret-ref", "uri": "…", "carve_out": { "owner": "…", "review_by": "2026-11-01" } }
}
```

- `aws-iam` / `gcp-iam` — the Pilot generates a short-lived auth token from its own workload identity.
  Nothing is stored anywhere.
- `secret-ref` — a pointer into a secret store, resolved by the Pilot's identity. **Requires a
  `carve_out` block**: an owner and a review date, both surfaced in the operator console. The build
  refuses a `secret-ref` without one, so the exceptional path stays visibly exceptional.
- The kinds are mutually exclusive by schema. There is no configuration in which one silently wins
  over the other.

**Never point a role at a rotating credential you copied.** A reference resolves the *managed* secret
at connect time. A snapshot of a rotating secret is wrong within a rotation period and fails in a way
that looks like a Marque bug.

**The Pilot is the only holder.** Resolved credentials live in process memory for the life of a
connection, are excluded from every error path and log line, and are never returned over any API. A
Pilot exposes no endpoint that reads a credential, by construction and not by permission.

**The Harbourmaster cannot connect to a target, and does not try.** Anything requiring a *target*
connection — a rehearsal ([EDR-0010](./0010-rehearse-before-you-sign.md)), a schema introspection for
the analyser, a health check — is a request *to a Pilot*, and comes back as data. It connects to one
database only, its own ([EDR-0042](./0042-the-control-plane-keeps-its-own-store.md)). It holds no target credential and
no target connection parameters, which is what makes that true.

This originally read: *"It has no database driver for target engines linked in."*, which was a stronger
and cleaner mechanism and is **not available**: [EDR-0013](./0013-async-work-rides-the-wal.md) fixes
Marque's own state on PostgreSQL, PostgreSQL is also a target engine, and one driver serves both. The
sentence was never achievable after that record was accepted and went unchallenged only while the
Harbourmaster had no storage code and so linked no drivers at all.
[EDR-0042](./0042-the-control-plane-keeps-its-own-store.md) replaces it with import discipline — a
driver confined by a test that parses every first-party file to the two packages that need one — the
Harbourmaster's store and the Pilot's adapter, which must have it — with no exception at all for an
engine Marque does not store its own state in — and is explicit that this is weaker: it reads
imports, not capability.

**Verify positively after any change.** A lazily-initialised connection pool hides broken database
authentication indefinitely: no connection attempt, no error, quiet logs, and the first symptom
arrives during an incident. After any change to a role or a Pilot's identity, exercise it and confirm
the session's actual database user on the target — not the configuration's opinion of it.

## Consequences

**Easier.**

- The blast radius of a control-plane compromise is bounded to disclosure of request text and
  approval history, plus a quota'd, target-visible read channel
  ([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)) and the group-invoker standing-order
  residual ([EDR-0029](./0029-the-fast-path-authority-chain.md)). Bad, and much better than access.
- Credential rotation is invisible: the reference is stable, the value is fetched fresh.
- Least privilege is enforceable per role, by the database, using primitives the database already
  has.

**Harder.**

- **The Pilot is now the crown jewels.** Everything this record removes from the Harbourmaster is
  concentrated in the Pilot, which must therefore be the most boring, smallest, least-featured
  component in the system. Every feature request aimed at it should be read as an attempt to widen
  the one component that holds credentials.
- **The Harbourmaster cannot do anything requiring a *target* connection**, which makes several
  convenient features into round trips: schema autocomplete, rehearsal, table metadata for the analyser. Each
  becomes a Pilot API, and each of those is new surface on the component that must stay small.
- Identity-based database auth has to be configured on the target, which is real work per target and
  not always available on managed engines.

**New obligations.**

- Every `secret-ref` carve-out is reviewed on its date. An expired review is reported as a finding.
- Pilot task roles are scoped to exactly the secrets and identities they use. A Pilot able to
  dereference every role in the deployment has re-created the vault this record removed.

## References

- [ZFN-35](https://zrz.io/zfn/35-dereference-secrets-not-store-in-config/) — reference secrets,
  dereference at runtime.
- [ZFN-9](https://zrz.io/zfn/9-no-long-lived-cloud-keys/) — federated identity over static keys.
- [ZFN-16](https://zrz.io/zfn/16-separate-data-plane-control-plane/) — plane separation.
- [EDR-0006](./0006-every-statement-names-a-role.md) — why every statement must name one of these.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after review — the TL;DR's "no database access" is corrected to "no
  credential and no ability to commit a change", with the bounded read channel named
  ([EDR-0034](./0034-the-pilot-api-has-one-authorisation-model.md)). This line was missing when the
  amendment was made, which is the corpus's own rule 1 broken in the one place it is most visible.
- **2026-08-16**: Amended after the second panel's synthesis: extended the blast-radius bullet to name the read channel and the fast-path residual.
- **2026-08-20**: Amended again. The mechanism named here was `lint`; EDR-0042's rule is a test that parses every first-party file, with a `depguard` block beside it as the edit-time report. The three claims that justified moving off the linter were each false and are retracted in EDR-0042; the reason the test is kept claims nothing about capability. The decision is untouched.
- **2026-08-20**: Amended. The driver sentence — "no database driver for target engines linked in" — is replaced, because it had been unachievable since [EDR-0013](./0013-async-work-rides-the-wal.md) fixed Marque's own state on PostgreSQL: PostgreSQL is also a target engine and one driver serves both. It survived only while the Harbourmaster had no storage code. The decision is unchanged — the control plane holds no target credential and cannot mint authority to reach one, which is not the same as "cannot reach a target": the bounded operator-signed read channel this record already carves out survives — and the mechanism is now import discipline ([EDR-0042](./0042-the-control-plane-keeps-its-own-store.md)), which that record states plainly is weaker. `implementation_note` corrected for the same reason. Where a target's connection parameters live is [issue #36](https://github.com/sixfathoms/marque/issues/36).
