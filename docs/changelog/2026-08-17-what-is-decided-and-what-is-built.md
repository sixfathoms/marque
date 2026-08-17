---
title: "Every decision record now says whether it is built"
tags: [docs]
---

A record's `status` says what was **decided**. It has never said whether a line of it exists, and at
forty records that gap had become the most misleading thing on the site: forty `accepted` records
read as a system, and Marque is a design with one scaffolding milestone behind it.

Every record now carries a required `implementation` field recording what **exists**, and the site
gains a [roadmap](/roadmap/) derived from those fields and from nothing else — no manifest and no
second list, for the same reason the changelog has no index.

The two axes are orthogonal on purpose. A record is routinely `accepted` and `none` at once, and
that is not a contradiction: the decision is settled, and not a line of it is written. Folding "not
built yet" into the status vocabulary was the obvious alternative and it is wrong here, because
`proposed` requires a `proposed_until` and *fails the build* once that date passes. It would have
armed a deadline timer on every settled decision that is merely waiting its turn in the
[implementation plan](/overview/implementation-plan/).

**The field is required, and the build rejects a record without one.** That is the load-bearing part
rather than a nicety. A derived page whose source field is optional under-reports the work
outstanding the first time somebody omits the field — and a roadmap that under-reports is worse than
no roadmap at all, because it is read as complete.

The first tally is **38 of 40 records with nothing implementing them**, one `partial` and one
`shipped`. Every state was assigned by reading the code rather than the record, which is the only way
the field means anything: filled in from what a record claims, it would reproduce exactly the drift
it exists to detect.

### Added

- **`implementation` on every record** — `shipped`, `partial`, `in-flight` or `none`, a closed
  vocabulary ordered most-built to least. `partial` and `in-flight` must also carry an
  `implementation_note` saying which half is missing, or which branch it is on. A note is worth
  writing on a `none` too, wherever scaffolding exists that a reader would otherwise mistake for the
  decision — [EDR-0039](/edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser/) says that the
  Makefile exports `CGO_ENABLED=1` for it while no parser is linked in.
- **A roadmap page** — grouped most outstanding first, with a tally, and a record's implementation
  drawn as an outlined badge beside its filled status badge so the two are never read as one.
- **Two new build failures** — a record with a missing or unknown `implementation`, and a `partial`
  or `in-flight` record with no note.

### Changed

- **[EDR-0020](/edrs/0020-one-schema-generates-every-client/) is `partial`** — the schema, the
  annotation extension, the build failure for an unannotated method and the committed Go and Connect
  stubs exist; `clients/ts/`, the Pilot, Surveyor and relay schemas, typed errors, streams and a
  client interceptor honouring `keyed` and `unsafe` do not.
- **[EDR-0040](/edrs/0040-a-methods-declared-behaviour-may-only-strengthen/) is `shipped`** — one
  `MethodBehaviour` extension, the strengthen-only comparison and the `idempotency_level` agreement
  check, all enforced in CI. Its own Scope section puts client-side retry behaviour outside the
  record, so the absent interceptor is not a missing half of it.
