---
title: "The rest of the panel's must-fixes: a trust anchor, a fence that failed open, and a read channel nobody had bounded"
tags: [security, policy, docs]
order: 1
---

The expert panel's full result: **109 findings, 98 surviving** both an attempt to refute them and a
search of the corpus for whether they were already addressed. Its verdict called the corpus "unusually
healthy for a design-stage artefact", with what survived concentrating in three places. All seven
must-fixes are now closed.

### The one that mattered most

**[EDR-0031](/edrs/0031-approver-keys-are-anchored-outside-the-control-plane/) — the Pilot was
learning which keys are approvers from the Harbourmaster.** Verification was "against the deployment's
JWKS", served from the control-plane origin, and enrolment countersignatures were checked by the
Harbourmaster too. So a compromised control plane could generate a keypair, publish it as an enrolled
approver, and sign both limbs — making the two-signature design a *detective* control rather than a
preventive one against exactly the adversary it was built for. It also silently undid
[EDR-0029](/edrs/0029-the-fast-path-authority-chain/) and
[EDR-0030](/edrs/0030-a-marque-states-its-own-approval-requirement/), written days earlier to close
earlier findings.

The enrolled set is now a **co-signed, epoch-chained roster** — signed by k already-enrolled approver
device keys, never by the control plane, verified back to a genesis root each Pilot pins out of band
at deployment, with monotonic epochs so a stale roster cannot be replayed and digests written to the
logbook's external anchor.

### Added

- **[EDR-0032](/edrs/0032-a-marque-binds-its-executor-tenant-and-pilot/)** — three bindings other
  records asserted and the payload never carried. `cnf.jkt` so the **caller** proves possession
  directly to the Pilot: offline execution had been built entirely for the marque and not at all for
  the person holding it, whose access token expires one lifetime into the outage. Plus `tenant` (so
  cross-tenant confusion fails signature verification, as
  [EDR-0025](/edrs/0025-tenants-are-partitioned-from-day-one/) always claimed) and `pilot` (so a
  budget of one cannot be spent once on each of two Pilots).
- **[EDR-0033](/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation/)** — a fourth fence
  check. `DELETE FROM accounts WHERE id = 42` with an `ON DELETE CASCADE` child returns **one** row,
  satisfies `max_rows = 1`, passes the fence post-assert, and destroys millions of rows in a table no
  delegation names. `RETURNING` reports only the target relation, and referential actions are not
  gated by the invoking role's privileges. The transaction's **per-relation write counts** are now
  read before commit and asserted against the declared object scope — which catches cascades,
  triggers and rewritten targets alike, rather than enumerating them.
- **[EDR-0034](/edrs/0034-the-pilot-api-has-one-authorisation-model/)** — `Rehearse` and `Introspect`
  had no stated caller check, so a compromised control plane had an **exact-count oracle** over every
  target via rolled-back rehearsals. Every Pilot method now verifies a submitter signature; the
  control plane relays, it does not authorise.
- **[EDR-0035](/edrs/0035-execution-freshness-is-a-property-of-the-approval/)** — requiring a fresh
  interactive authentication to execute against a `critical` target broke offline execution on exactly
  the targets the playbook holds break-glass marques for, and locked agents out of the flow escalation
  exists to serve. Freshness belongs to the approval.

### Changed

- **[EDR-0007](/edrs/0007-delegation-by-containment-proof/)'s fence SQL was unsound in two independent
  ways**, and the decision is unchanged while the encoding was wrong. `NOT (tier = 'sandbox')` does
  not count a row whose `tier` is NULL — `NOT NULL` is NULL and `WHERE` admits only TRUE — so a
  NULL-fenced row passed the pre-check, the post-assert and the row count **with no concurrency
  involved at all**. Every comparison is now `IS NOT TRUE`. And no isolation level was named, so
  `BEGIN` got READ COMMITTED and the pre-check and the statement took different snapshots; the
  execution transaction is now REPEATABLE READ.
- **[EDR-0028](/edrs/0028-statement-pipeline-and-provider-spi/)** described the Surveyor as a `verify`
  provider with outcomes `veto`/`refer`. Its outcomes are `conforms`/`refer`, and `veto` is precisely
  the power [EDR-0017](/edrs/0017-conformance-matching-may-route-never-widen/) says it must never
  have — an implementer building from that table would have shipped a Surveyor that can deny.
- **[EDR-0005](/edrs/0005-control-plane-holds-no-credentials/)'s "no database access"** is corrected to
  "no credential and no ability to commit a change; a bounded, quota'd, target-visible read channel
  remains", in the record and in both compromise tables.

### What this says about the process

Three of these — the trust anchor, the NULL fence, the cascade — are cases where the design stated a
property confidently and the mechanism did not deliver it. None was found by writing more records;
all were found by an adversarial read with a specific expert lens pointed at a specific claim. The
records being the source of truth is what made that possible: a claim and its mechanism sit in the
same file, and can be checked against each other.
