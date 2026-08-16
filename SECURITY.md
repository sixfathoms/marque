# Security

## Reporting a vulnerability

Report privately through [GitHub's private vulnerability
reporting](https://github.com/sixfathoms/marque/security/advisories/new). Please do not open a
public issue for a suspected vulnerability.

Include what you did, what you expected, what happened, and — if you have one — a proof of concept.
You will get an acknowledgement, and we will tell you what we intend to do about it and when.

## Status

Marque is at the design stage: there is no released software and no deployment to attack. What
exists is a design, and **a flaw in the design is exactly the kind of report that is most valuable
right now** — it is cheaper to fix here than anywhere later.

## What we consider a vulnerability in the design

The design makes specific claims. A demonstration that one of them does not hold is a security
issue, not a documentation issue:

| Claim | Where |
|---|---|
| A compromised control plane cannot cause a statement to execute **whose shape no human signed** — it holds no target credential, cannot produce an approver signature, and a fast-path marque must carry the human-signed artefact that authorised it | [EDR-0004](docs/edrs/0004-marques-are-signed-leases.md), [EDR-0005](docs/edrs/0005-control-plane-holds-no-credentials.md), [EDR-0029](docs/edrs/0029-the-fast-path-authority-chain.md) |
| A marque cannot be stripped of approver signatures to weaken its approval requirement | [EDR-0030](docs/edrs/0030-a-marque-states-its-own-approval-requirement.md) |
| A stolen session or a compromised control plane cannot enrol approver authority — the approver roster is co-signed and anchored outside the control plane | [EDR-0031](docs/edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md) |
| A statement cannot write to a relation outside **the marque's** declared object scope without the transaction aborting — bounded to what the mechanism measures: tuple changes, in this database, in this transaction (see limitations) | [EDR-0033](docs/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md) |
| A row whose fence expression is NULL is treated as outside the fence, not inside it | [EDR-0007](docs/edrs/0007-delegation-by-containment-proof.md) |
| Every Pilot method verifies a submitter signature — the control plane relays, it does not authorise | [EDR-0034](docs/edrs/0034-the-pilot-api-has-one-authorisation-model.md) |
| An authentic approver signature attests a payload the approver actually saw — a compromised control plane cannot render one marque and obtain a signature over another | [EDR-0036](docs/edrs/0036-what-is-signed-must-be-what-was-seen.md) |
| The approval requirement a marque claims is recomputed by the Pilot from anchored policy, not believed from the payload | [EDR-0036](docs/edrs/0036-what-is-signed-must-be-what-was-seen.md), [EDR-0030](docs/edrs/0030-a-marque-states-its-own-approval-requirement.md) |
| A stolen session cannot enrol an approver key or otherwise become approval authority | [EDR-0023](docs/edrs/0023-approver-keys-enrolment-and-recovery.md), [EDR-0031](docs/edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md) |
| Cross-tenant confusion cannot produce a valid marque — a Pilot trusts only its own tenant's keys | [EDR-0025](docs/edrs/0025-tenants-are-partitioned-from-day-one.md), [EDR-0032](docs/edrs/0032-a-marque-binds-its-executor-tenant-and-pilot.md) |
| The local proxy forwards no bytes and brokers every statement that crosses it | [EDR-0022](docs/edrs/0022-local-proxy-brokers-every-statement.md) |
| A compromised Pilot cannot create authority it was not given | [EDR-0004](docs/edrs/0004-marques-are-signed-leases.md) |
| A statement cannot affect rows outside a delegation's fence without the transaction aborting | [EDR-0007](docs/edrs/0007-delegation-by-containment-proof.md) |
| A standing order's parameters cannot alter the shape of its statement | [EDR-0008](docs/edrs/0008-standing-orders.md) |
| No configuration gives the analyser authority to approve, deny or alter anything | [EDR-0009](docs/edrs/0009-the-leadsman-is-advisory.md) |
| No model can create authority a human did not sign — a compiled delegation is signed by its grantor, and a conformance judgment can only choose between two paths that both end in a human-granted scope | [EDR-0016](docs/edrs/0016-natural-language-delegations-are-compiled.md), [EDR-0017](docs/edrs/0017-conformance-matching-may-route-never-widen.md) |
| A conformance judgment cannot widen a scope, deny a request, or resolve uncertainty toward `conforms` | [EDR-0017](docs/edrs/0017-conformance-matching-may-route-never-widen.md) |
| An agent cannot approve anything under any configuration, and cannot exceed the intersection of operator policy, its human's delegation, and its own task declaration | [EDR-0018](docs/edrs/0018-agents-are-submitters-under-intersected-scope.md) |
| No escalation stage is satisfied by a timeout, and every stage is a human | [EDR-0019](docs/edrs/0019-escalation-is-a-chain.md) |
| A rehearsal cannot commit | [EDR-0010](docs/edrs/0010-rehearse-before-you-sign.md) |
| A retried execution cannot apply a statement twice | [EDR-0011](docs/edrs/0011-execution-is-idempotent-and-fenced.md) |
| Marque's own role cannot rewrite the logbook | [EDR-0012](docs/edrs/0012-the-logbook-is-append-only.md) |
| The relay cannot read what it carries, and offers no route into the private network | [EDR-0014](docs/edrs/0014-relay-for-targets-with-no-inbound-route.md) |

## Known and accepted limitations

These are documented trade-offs rather than oversights. Reports about them are welcome as design
discussion, but they are not surprises:

- **Hash chaining does not defend against an attacker who controls the database and can recompute the
  chain.** It detects tampering by anyone who does not, which is the realistic case. The external
  anchor is what extends this ([EDR-0012](docs/edrs/0012-the-logbook-is-append-only.md)).
- **Revocation has a bounded propagation window**, set by the Pilot's revocation-list refresh
  interval ([EDR-0004](docs/edrs/0004-marques-are-signed-leases.md)).
- **A rehearsal executes an unapproved statement** inside a rolled-back transaction. It can still
  fire triggers, consume sequence values and write WAL
  ([EDR-0010](docs/edrs/0010-rehearse-before-you-sign.md)).
- **An approver can approve something they did not read**, and a grantor can sign a compilation they
  did not read. Nothing in the design prevents either, and the records say so
  ([EDR-0009](docs/edrs/0009-the-leadsman-is-advisory.md),
  [EDR-0016](docs/edrs/0016-natural-language-delegations-are-compiled.md)).
- **A conformance judgment can be wrong inside a signed scope.** Tier-B surveying bounds a model
  error to "failed to escalate something already within a human-signed scope" — it does not eliminate
  it. This is why Tier B ships **off by default** and why the sampled audit is mandatory rather than
  advisory ([EDR-0017](docs/edrs/0017-conformance-matching-may-route-never-widen.md)).
- **Prompt injection in statement text is an ongoing arms race.** The structural mitigation is the
  deterministic outer bound, which holds when the prompt defence does not.
- **A compromised control plane retains a bounded read channel.** It can relay operator-signed
  rehearsals, returning `rows_affected` and `duration_ms` — an oracle, quota'd per principal and
  visible in the target's own logs, but an oracle
  ([EDR-0034](docs/edrs/0034-the-pilot-api-has-one-authorisation-model.md)).
- **WebAuthn user verification proves presence, not agreement to a payload.** The challenge is an
  opaque digest, so a compromised renderer can display one thing and challenge over another. The
  mitigations are `signing_surface: local` for `critical` targets and the signed `display` field, and
  neither makes a browser served by the adversary a sound signing surface
  ([EDR-0036](docs/edrs/0036-what-is-signed-must-be-what-was-seen.md)).
- **Infrastructure control is a trust root.** Whoever deploys Pilots sets their genesis roster root
  and therefore can define who approves. Marque bounds what that can do *silently* — the root is
  recorded, reported by each Pilot, and changing it is a re-deployment — but it does not pretend
  infrastructure access is not authority
  ([EDR-0031](docs/edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)).
- **A compromised control plane can withhold a roster update**, so a newly-enrolled approver may be
  unrecognised and a retired key may stay live until Pilots see a newer epoch. Roster age is a
  monitored signal.
- **Fast-path volume is unbounded against a compromised control plane.** Rate limits are enforced at
  ingress, by the component that is compromised, and a budget bounds one marque rather than how many
  are minted ([EDR-0029](docs/edrs/0029-the-fast-path-authority-chain.md)).
- **The revocation list is signed by the component whose compromise it exists to remediate.** A
  compromised control plane can suppress a revocation — bounded by `next_update`, after which a
  `required`-policy Pilot refuses — or forge one, which is a visible denial of service. It cannot use
  it to authorise anything ([EDR-0004](docs/edrs/0004-marques-are-signed-leases.md)).
- **The write-set assertion is blind to `TRUNCATE` and to writes made on a separate session** (dblink,
  an extension, a `SECURITY DEFINER` function opening its own connection). The first has a stated
  detector; the second is bounded only by the role
  ([EDR-0033](docs/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)).
- **A pipeline `transform` provider is trusted for statement content.** The SPI's mechanisms enforce
  containment within the submitter's authority, **not** narrowing: a transform can rewrite
  `WHERE id = 42` to `id = 43` inside the scope and pass every check
  ([EDR-0028](docs/edrs/0028-statement-pipeline-and-provider-spi.md)).
- **Catalog introspection is a read channel over object definitions** — function bodies, view bodies,
  defaults and check expressions. A deployment keeping secrets in them must treat it as one
  ([EDR-0027](docs/edrs/0027-be-psql-then-be-better-than-psql.md)).
- **Statement text may contain personal data and the logbook is immutable**
  ([EDR-0012](docs/edrs/0012-the-logbook-is-append-only.md)).
