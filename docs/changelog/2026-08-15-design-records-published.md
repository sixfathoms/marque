---
title: "Marque begins: fifteen decision records and a docs site"
tags: [docs, policy, security]
---

Marque is a broker for statements run against production data stores: submit a statement, have it
analysed, have a human with authority sign a scoped and expiring grant, then run exactly that
statement under exactly that role. This is the first public state of the project — the design,
written down before any of it is built.

Nothing is implemented. The [scope](/overview/scope/) page says what is in the first release, what
is deliberately deferred, and what already exists in this space.

### Added

- **[Fifteen decision records](/edrs/)** covering the whole design, from the plane split to the
  fence that enforces a delegated row scope. The four worth reading first are
  [EDR-0001](/edrs/0001-marque-platform-architecture/) for the shape,
  [EDR-0004](/edrs/0004-marques-are-signed-leases/) and
  [EDR-0005](/edrs/0005-control-plane-holds-no-credentials/) for the security argument, and
  [EDR-0007](/edrs/0007-delegation-by-containment-proof/) for the hardest problem in the system.
- **[An architecture page](/overview/architecture/)** synthesising the records, including the table
  that is the design's real argument: what each component's compromise does and does not buy an
  attacker.
- **[A cast list](/concepts/cast/)** — Harbourmaster, Pilot, Leadsman, Tender — naming what each
  component is for and, more usefully, what each would never do.
- **This changelog**, one file per entry so that two changes on the same day cannot conflict.

### Decided

- **A marque carries two signatures**, the approver's own device key and the control plane's. Neither
  party can produce a valid grant alone, so compromising the server yields the ability to *ask* and
  nothing more — [EDR-0004](/edrs/0004-marques-are-signed-leases/).
- **Delegated row scope is never proved and never silently applied.** Predicate entailment over SQL
  is undecidable, and rewriting a statement to fit a scope produces a partially-applied change nobody
  reviewed. Instead the scope is a transactional fence that aborts and reports how many rows fell
  outside it — [EDR-0007](/edrs/0007-delegation-by-containment-proof/).
- **The analyser holds no authority, and no setting grants it any.** It writes prose beside facts
  that came from a parser and a rehearsal, with no risk score and no recommendation — because a score
  is the shape people automate against — [EDR-0009](/edrs/0009-the-leadsman-is-advisory/).
- **One bootstrap URL is the entire client configuration.** A deployment publishes its own issuers,
  endpoints, Pilots and capabilities — [EDR-0002](/edrs/0002-bootstrap-discovery-document/).
