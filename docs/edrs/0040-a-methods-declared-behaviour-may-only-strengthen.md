---
id: 40
title: "Declare a method's behaviour in one annotation, and never weaken it"
summary: "A method's retry behaviour travels as a single MethodBehaviour extension rather than three, and once declared it may only strengthen. buf breaking ignores custom options, so a separate check compares every method against the base branch."
status: accepted
date: 2026-08-16
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [api, security]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

[EDR-0020](./0020-one-schema-generates-every-client.md) made every method declare whether repeating it
is safe. This record fixes two things that decision did not say, both of which are cheap now and
cannot be retrofitted once several methods exist.

**One annotation, not three.** The declaration is a single `MethodBehaviour` message extension —
`safe`, `idempotency`, `idempotency_field` — rather than three scalar extensions.

**A declaration may only strengthen.** Changing a method from `safe` to not-safe, from
`IDEMPOTENCY_NATURAL` to `IDEMPOTENCY_KEYED` or `IDEMPOTENCY_UNSAFE`, from `IDEMPOTENCY_KEYED` to
`IDEMPOTENCY_UNSAFE`, or moving the field its key travels in, is a **breaking change**. A method whose
behaviour genuinely changes gets a new method name, exactly as a field whose meaning changes gets a
new number.

Both are enforced by the build. `safe` additionally requires the standard
`option idempotency_level = NO_SIDE_EFFECTS`, because that is the option a generated Connect client
actually reads.

## Context

Three separate facts forced this, and each was found by trying to defeat the guard
[EDR-0020](./0020-one-schema-generates-every-client.md) asked for rather than by reading it.

**`buf breaking` does not look at custom method options.** Its rules compare field numbers, names,
types and cardinality. A method can be reclassified from `safe` to `IDEMPOTENCY_UNSAFE` and `buf
breaking` reports nothing — verified, exit 0. The annotation was enforced as *present* and not as
*stable*, which is half a guard: it stops a method being added without a declaration, and does
nothing about the declaration changing underneath clients that already compiled it.

**A stale declaration is worse than none.** A client built against the old schema has the retry
policy compiled in. It keeps applying it until somebody rebuilds it, against a server that has moved
on. That is the whole reason [ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/)
asks for the annotation in the first place, and it is exactly the property a silent reclassification
destroys.

**Extension numbers in 50000–99999 are not globally unique.** Protobuf reserves that range for use
*within one organisation*. Marque is a public, generic tool, so an adopter will import this schema
alongside their own options; a collision is a panic at registration, before `main` runs, with no
recoverable path. Three extensions were three chances to collide, and the numbers first chosen —
50001 upward — are the ones anybody numbering in-house options reaches for first.

There is also a plainer problem with three scalar extensions: they can be set half-way. An
`idempotency` of `IDEMPOTENCY_KEYED` with no `idempotency_field` is two annotations that disagree,
where one message is a single malformed value.

## Decision

**The annotation is one message.** `marque.v1.MethodBehaviour`, carried by one extension on
`google.protobuf.MethodOptions`, at a number deliberately away from the bottom of the in-house range.
Before Marque is imported by anyone outside this repository, a globally-unique range is requested
from the protobuf global extension registry; until then the residual risk is one number rather than
three, and it is stated rather than assumed away.

**A declaration may only strengthen.** The rule follows from one question — *does this change make
the cached policy of a client built against the previous schema unsafe?* Forbidden:

| Was | Becomes | Why it breaks |
|---|---|---|
| `safe` | not `safe` | The old client retries freely, and the method is no longer read-only |
| `NATURAL` | `KEYED` | The old client retries with no key, so the server cannot recognise the repeat |
| `NATURAL` | `UNSAFE` | The old client retries something that must not be retried |
| `KEYED` | `UNSAFE` | As above, carrying a key that no longer helps |
| `KEYED` field `a` | `KEYED` field `b` | The old client keeps filling `a`, which the server now ignores |

Allowed, including the ones that read as widenings: `UNSAFE` to anything, because the old client
never retried; `KEYED` to `NATURAL`, because repeating became harmless; not-`safe` to `safe`.

**The declaration and the standard `idempotency_level` agree, across all three of its values.**
Connect's generator reads `idempotency_level` and nothing else, emitting `WithIdempotency(…)` onto
the method's `Spec`, where every interceptor can see it — including the retry interceptor this record
undertakes to build. A disagreement therefore does not stay in the schema; the generated client is
the one that acts.

| `idempotency_level` | Must pair with | Because |
|---|---|---|
| `NO_SIDE_EFFECTS` | `safe` | It is the read-only claim, and it is what enables a GET |
| `IDEMPOTENT` | not `safe`, and not `IDEMPOTENCY_UNSAFE` | It claims only that repeating is harmless — weaker than `safe`, and the opposite of `unsafe` |
| unset | not `safe` | A `safe` method with nothing set is a claim no generated client can act on |

Read the table as a biconditional: `safe` requires `NO_SIDE_EFFECTS`, **and** `NO_SIDE_EFFECTS`
requires `safe`. Stating only the second is a live defect rather than an incomplete one, and it is
the mistake that was actually made here — a first implementation restructured this check around the
level and kept the `safe` direction in only one of its three branches, so `safe` with `IDEMPOTENT`
passed for a while.

The `IDEMPOTENT`-with-`unsafe` row is the dangerous one: without it a method could declare
`IDEMPOTENCY_UNSAFE` here and `IDEMPOTENT` there, and the generated client would believe the second.

**Scope.** This record covers the declaration and its stability. It does not cover client-side retry
*behaviour* beyond what `idempotency_level` already gives — an interceptor honouring `keyed` and
`unsafe` arrives with the first real client, and until then the annotation is enforced at build time
and consumed only for `safe`. Saying that plainly is better than implying the loop is closed.

## Consequences

**Easier.**

- A reclassification cannot happen quietly. The check names the method, the transition, and what an
  already-built client will do.
- One extension is one collision surface, and the three fields cannot be set half-way.
- `safe` means something to a generated client today, rather than only to this repository's build.

**Harder.**

- **A genuine change of behaviour now costs a new method name**, and the old one has to live until
  clients are gone. That is deliberate — it is the same cost a field number carries — but it will
  feel disproportionate the first time a method wants to move from `NATURAL` to `KEYED`.
- **The check needs the base branch's schema**, so CI has to fetch enough history to build it. That is
  a real constraint on the checkout, and a shallow one silently removes the comparison unless the
  ref is asserted — which is why the target fails rather than skips when the ref is absent.
- **The extension number is still not globally unique.** The risk is reduced, not removed, and it
  stays that way until a range is registered upstream. An adopter who collides gets a panic, not a
  diagnostic.
- Reading a declaration now means reading a message rather than three options, which is one more
  indirection in a `.proto` file for anyone unfamiliar with message-typed extensions.

**New obligations.**

- Every new method declares `MethodBehaviour`, and `safe` methods also declare `idempotency_level`.
- Request a globally-unique extension range before the first external adopter.
- An interceptor honouring `keyed` and `unsafe` ships with the first generated client that makes
  calls, and [EDR-0020](./0020-one-schema-generates-every-client.md)'s claim that "generated clients
  honour it" is only fully true from that point.
- **Delete the bootstrap escape in `make breaking`.** Until this record's own change is on the main
  branch there is no previous schema to compare against, so the target says so and exits. That is
  correct exactly once. While it survives, deleting `buf.yaml` from the main branch would disable
  both checks silently, and "both are enforced by the build" is not yet true of the change that
  introduces them. The next change to the schema removes it.

**A limit worth stating.** The rule above is one-directional: it asks what an *old* client does
against a *new* server. The reverse skew — a client newer than the server it calls — is not covered.
Moving a method to `safe` is permitted, and a client that then opts into an HTTP GET will be refused
by a server that has not yet been upgraded. Nothing enables GET today, so this is not reachable; when
something does, it becomes a deployment-ordering question rather than a schema one, and this record
does not answer it.

## References

- [EDR-0020](./0020-one-schema-generates-every-client.md) — one schema generates every client, and
  the annotations this record makes precise.
- [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — the execution nonce, which is the key an
  `IDEMPOTENCY_KEYED` method names.
- [ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/) — annotate read-only and
  idempotent endpoints.
- [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) — define the API with a schema
  and generate the clients.

## Changelog

- **2026-08-16**: Accepted.
