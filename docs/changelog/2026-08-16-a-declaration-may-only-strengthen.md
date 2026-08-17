---
title: "A method's declared behaviour may only ever strengthen"
tags: [security, docs]
---

[EDR-0020](/edrs/0020-one-schema-generates-every-client/) made every API method declare whether
repeating it is safe, and made an unannotated method fail the build. That enforced the declaration as
*present*. It said nothing about the declaration changing underneath clients that had already
compiled it — and `buf breaking` cannot help, because its rules compare field numbers, names and
types and ignore custom method options entirely.

So a method could be reclassified from read-only to must-never-be-retried with every check green, and
a client built against the previous schema would carry on retrying it.
[EDR-0040](/edrs/0040-a-methods-declared-behaviour-may-only-strengthen/) closes that, and states the
rule as one question: does this change make the cached retry policy of an already-built client
unsafe?

### Added

- **A declaration may only strengthen.** `safe` to not-safe, `natural` to `keyed` or `unsafe`,
  `keyed` to `unsafe`, or moving the field a key travels in, are each breaking changes. A method
  whose behaviour genuinely changes gets a new name, exactly as a field whose meaning changes gets a
  new number. Enforced against the base branch on every pull request.
- **The three annotations became one.** `MethodBehaviour` carries `safe`, `idempotency` and
  `idempotency_field` together, so they cannot be set half-way — and because extension numbers in
  the in-house range are not globally unique, one extension is one chance to collide with an
  adopter's own options instead of three.
- **`safe` now reaches the generated client.** It requires the standard
  `idempotency_level = NO_SIDE_EFFECTS`, which is the option Connect's generator actually reads.
  Honouring `keyed` and `unsafe` needs an interceptor, which arrives with the first real client;
  EDR-0040 says so rather than implying the loop is closed.

### Changed

- **The status text is honest again.** The README, the introduction and the site's home page said
  implementation had not started. It has — at the M0 scaffolding milestone, described in the
  [implementation plan](/overview/implementation-plan/). Nothing runs against a database yet.
- **A build stamps the source's date, not the wall clock**, so building the same commit twice
  produces the same binary. `SOURCE_DATE_EPOCH` wins where a distribution sets it.
