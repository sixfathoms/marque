---
id: 20
title: "One schema generates every client, and annotates what may be retried"
summary: "A single protobuf definition is the source of truth for the API. Server stubs, CLI and console clients are generated from it, every method declares whether it is read-only or idempotent, and CI fails on a breaking change."
status: accepted
implementation: partial
implementation_note: "Built: the schema, the annotation extension, the build failure for an unannotated method (internal/schema/annotations.go), committed Go and Connect stubs, and the `buf breaking` check on every pull request. Not built: clients/ts/, the Pilot, Surveyor and relay schemas, typed errors, streams, and a client interceptor that honours keyed and unsafe. Five methods exist — GetVersion, Submit, GetRequest, Approve and RecordExecution."
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, api]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

One protobuf definition per service — Harbourmaster, Pilot, Surveyor, relay registration — is the
only place the API is described. Go server stubs, the CLI's client and the console's TypeScript
client are all generated from it. Nothing hand-builds a request or hand-parses a response
([ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/)).

Transport is **Connect**, which speaks gRPC, gRPC-Web and plain HTTP/JSON over one handler. The CLI
gets efficient binary; the console calls the same methods from a browser with no proxy in between;
`curl` works during an incident.

Every method carries two annotations, and they are load-bearing rather than documentation:

- `safe` — read-only, so it may be retried freely and served from a replica.
- `idempotency` — `natural` (repeating it is inherently harmless), `keyed` (the caller supplies a
  key), or `unsafe` (must not be retried automatically).

A generated client will not auto-retry an `unsafe` method. `Execute` is `keyed`, and the key is the
execution nonce from [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — one concept, declared
once, enforced at the transport layer rather than remembered at each call site.

## Context

Marque has at least four consumers of its API — the CLI, the console, an agent SDK, and whatever an
adopting team writes — and one of them runs in a browser. Hand-written clients drift, and the drift
is silent: a field renamed on the server keeps working for the client that never sends it, until the
day it matters. That is the whole of
[ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/)'s argument, and it applies here
with more force than usual because a client that quietly misunderstands a response in this system
misunderstands who approved what.

The retry annotation is the part that is specific to Marque. Most APIs treat retry policy as client
configuration; here, retrying the wrong method **applies a production change twice**. Making retry
safety a property of the *method*, declared in the schema and compiled into every generated client,
means an integrator cannot get it wrong by omission — which is the way they would get it wrong.
[ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/) asks for exactly this, and
this system is the case that justifies the ceremony.

Connect rather than raw gRPC is chosen for one concrete reason: the console must call the API
directly from a browser. gRPC-Web requires a translating proxy in front of every deployment, which is
another component to run, another place for a bug, and another thing an adopting team has to
understand. Connect removes it.

## Decision

**Layout.**

```
proto/marque/v1/
  common.proto        principals, scopes, digests, the annotation extensions
  harbourmaster.proto submit, approve, delegate, escalate, policy, logbook
  pilot.proto         rehearse, execute, introspect, revocation list
  surveyor.proto      conformance judgment (internal)
  relay.proto         Pilot registration and framing
gen/                  generated Go; committed
clients/ts/           generated TypeScript; committed
```

Generated code is **committed**, not built on demand. A reviewer should see the wire contract change
in the diff, because that is the change that breaks people.

**Annotations.**

```proto
rpc GetRequest(GetRequestReq) returns (Request) {
  option (marque.v1.safe) = true;
}
rpc Execute(ExecuteReq) returns (ExecuteResp) {
  option (marque.v1.idempotency) = KEYED;
  option (marque.v1.idempotency_field) = "nonce";
}
rpc SignMarque(SignMarqueReq) returns (Marque) {
  option (marque.v1.idempotency) = UNSAFE;   // a second signature is a second grant
}
```

The generated clients read these: `safe` methods retry with backoff and jitter; `keyed` methods retry
carrying the same key; `unsafe` methods **do not retry** and surface the ambiguity to the caller. A
method with no annotation fails the build — the default must be a decision, not an accident.

**Compatibility is checked, not assumed.** `buf breaking` runs against the `main` schema on every
pull request. Field numbers are never reused; a removed field is reserved. The bootstrap document's
`min_client_version` ([EDR-0002](./0002-bootstrap-discovery-document.md)) handles the cases where
compatibility is genuinely impossible.

**Errors are typed.** A refusal carries a structured detail — which fence check failed and by how
many rows, which stage of an escalation is waiting, which parameter constraint a standing order
rejected. A human-readable string is the *fallback*, not the contract; the CLI and console both
render from the structured form, so improving an error message does not mean changing what clients
parse.

**Streams where the operation is long.** `Rehearse` and `Execute` stream progress events
([EDR-0011](./0011-execution-is-idempotent-and-fenced.md) requires visible progress), as does the
logbook tail. Two consequences that bite in practice and are handled once, here, rather than
rediscovered per client: a load balancer will kill a quiet stream at its idle timeout, so streams
carry application-level keepalives inside that interval; and a client that reconnects mid-execution
must resume by nonce rather than restart, which the `keyed` annotation already implies.

**The Pilot API stays deliberately small.** It is the component holding credentials
([EDR-0005](./0005-control-plane-holds-no-credentials.md)), so its schema is the one to guard.
Adding a method to `pilot.proto` should require an argument in the pull request for why it cannot
live on the Harbourmaster.

## Consequences

**Easier.**

- Adding a surface — an agent SDK, a Terraform provider, a second console — is generation plus
  presentation, and it cannot silently disagree with the server.
- Retry safety is correct by default in every client, including ones written by people who have not
  read [EDR-0011](./0011-execution-is-idempotent-and-fenced.md).
- The browser talks to the API directly, so there is no proxy in the deployment topology.

**Harder.**

- **Protobuf is a real cost for a small team**: a toolchain, generated code in review diffs, and a
  learning curve for contributors who have only written JSON APIs.
- **Committed generated code creates merge conflicts** in files nobody edits by hand. The mitigation
  is regenerating rather than resolving, and it still costs time.
- Connect is a smaller ecosystem than plain REST. The tradeoff buys one schema and no proxy, which is
  worth it here, but it is a genuine dependency choice.
- Typed errors are more work than a string, and the temptation to add "just one" untyped message will
  be constant.

**New obligations.**

- Every new method declares its annotation, and the build enforces it.
- Schema changes are reviewed as wire-contract changes, with the same care as a database migration —
  because both are things you cannot take back once a client has shipped.

## References

- [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) — define the API with a schema
  and generate the clients.
- [ZFN-19](https://zrz.io/zfn/19-annotate-readonly-idempotent-endpoints/) — annotate read-only and
  idempotent endpoints; make every mutation idempotent.
- [ZFN-13](https://zrz.io/zfn/13-load-shedding-and-flow-control/) — retries with backoff and jitter
  belong in the client from day one.
- [EDR-0011](./0011-execution-is-idempotent-and-fenced.md) — the nonce the `keyed` annotation names.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended as implemented, in M0 of the
  [implementation plan](../content/overview/implementation-plan.md). The decision is unchanged —
  one schema, generated clients, annotations that are load-bearing, and a build that rejects an
  unannotated method — but five details above are not what shipped, and the code is right in each
  case:
  - **The three annotations ship as one message-typed extension**, `marque.v1.MethodBehaviour`,
    rather than three scalar ones, and a declaration may only ever strengthen. Both are
    [EDR-0040](./0040-a-methods-declared-behaviour-may-only-strengthen.md), which exists because
    `buf breaking` does not compare custom method options at all.
  - **`Idempotency` has four values, not three.** proto3 requires a zero value, so
    `IDEMPOTENCY_UNSPECIFIED = 0` is the "no decision" state the build rejects.
  - **Enum values ship prefixed** — `IDEMPOTENCY_KEYED`, not `KEYED`. The short spelling in the
    Decision section above does not compile, and buf's `ENUM_VALUE_PREFIX` rule requires the prefix.
  - **Request and response messages are named `<Method>Request` / `<Method>Response`.** The
    `GetRequest(GetRequestReq) returns (Request)` sketch above fails the `STANDARD` lint set this
    repository adopted; the stricter convention is the one CI can enforce.
  - **`clients/ts/` is not generated yet**, and will not be until the console in Phase 2. Only Go
    and Connect stubs are produced today.

  One clarification rather than a correction: "every method carries two annotations" reads as though
  both are required. One suffices — `safe = true`, or an `idempotency` other than unspecified — which
  is what this record's own `GetRequest` example does.
