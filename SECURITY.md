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
| A compromised control plane cannot cause any statement to execute | [EDR-0004](docs/edrs/0004-marques-are-signed-leases.md), [EDR-0005](docs/edrs/0005-control-plane-holds-no-credentials.md) |
| A compromised Pilot cannot create authority it was not given | [EDR-0004](docs/edrs/0004-marques-are-signed-leases.md) |
| A statement cannot affect rows outside a delegation's fence without the transaction aborting | [EDR-0007](docs/edrs/0007-delegation-by-containment-proof.md) |
| A standing order's parameters cannot alter the shape of its statement | [EDR-0008](docs/edrs/0008-standing-orders.md) |
| No configuration gives the analyser authority to approve, deny or alter anything | [EDR-0009](docs/edrs/0009-the-leadsman-is-advisory.md) |
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
- **An approver can approve something they did not read.** Nothing in the design prevents this, and
  the record says so ([EDR-0009](docs/edrs/0009-the-leadsman-is-advisory.md)).
- **Statement text may contain personal data and the logbook is immutable**
  ([EDR-0012](docs/edrs/0012-the-logbook-is-append-only.md)).
