---
title: "Introduction"
sidebar_position: 1
---

Marque is a broker for statements run against production data stores.

Instead of someone holding a production credential and running SQL at a prompt, they **submit** the
statement. It is analysed, a human with authority over that database **approves** it by signing a
grant — a *marque* — with a validity window and an execution budget, and only then does it run:
that exact statement, as that role, inside that window, once.

Everything that happens is appended to a journal that names who asked, who approved, what they were
shown, and what changed.

## The problem

Every organisation running a database in production eventually changes something in it by hand. A
migration went wrong. A customer is stuck in a state the admin UI cannot express. A flag needs
flipping before the feature that sets it properly has shipped.

The usual answers share one flaw: **the authorisation and the action are different objects.**

- **Standing credentials.** Someone has the password. It does not expire, its use is invisible, and
  the blast radius is the whole schema.
- **A shared console with session recording.** The log records that a session happened, not what was
  authorised. Nobody approved anything.
- **A Slack thread and a screen share.** The approval is real, human, and gone within the hour.
  Nothing durable connects "yes, do it" to what was actually executed.
- **Locking it down completely.** Then the on-call engineer at 3am has no path, and someone keeps a
  break-glass credential in a password manager — which is the first answer with extra steps.

There is no artefact anyone can point at that says *this exact statement, by this person, as this
role, until this time*. Marque makes that artefact the only way in.

## How it works

```mermaid
flowchart TB
  OP[Operator] -->|1 submit| HM[Harbourmaster]
  HM -->|2 analyse and rehearse| LM[Leadsman]
  AP[Approver] -->|3 sign| HM
  HM -->|4 marque| OP
  OP -->|5 execute| PI[Pilot]
  PI --> DB[(Target)]
  PI --> LOG[(Logbook)]
```

1. **Submit.** An operator names a target, a role, one or more statements, and a reason.
2. **Sound.** The statements are parsed for what they touch, and *rehearsed* — run inside a
   transaction that is always rolled back — so the affected row count is measured rather than
   guessed. A language model writes a plain summary beside those facts. It cannot approve anything.
3. **Review.** An approver reads the statement, the measured numbers, and the summary. They can edit
   the statement, narrow the window, or refuse.
4. **Sign.** Approval produces a marque: a signed grant naming the statement's digest, the role, the
   submitter, a not-before, an expiry and a budget. It carries the approver's own signature *and*
   the control plane's — neither can produce a valid one alone.
5. **Run.** The submitter executes. The database checks its grants, Marque checks the fence and the
   row count, and the transaction commits or rolls back whole.
6. **Log.** Statement, analysis, signature, result — appended, never edited.

Routine work never reaches step 3. **Standing orders** are statements approved once and invoked with
constrained parameters; **delegation** lets an approver hand a narrow slice of their authority to
someone else, with an expiry.

## What Marque is not

- **Not a SQL client.** There is no browsing, no autocomplete-driven exploration, no ad-hoc session.
  You bring a statement you have already decided to run.
- **Not a migration tool.** Schema changes belong in a migration pipeline with a review, a rollback
  and a deploy. Marque is for the one-off that a pipeline cannot express.
- **Not a privilege manager.** It never grants privileges on your database. The role's existing
  grants are the outer bound on everything it can authorise
  ([EDR-0006](../../edrs/0006-every-statement-names-a-role.md)).
- **Not a tunnel.** A Pilot inside your network speaks one narrow API. There is no port forwarding
  and no shell ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)).
- **Not a place where a model approves anything.** The analyser writes prose beside deterministic
  facts and holds no authority whatsoever
  ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)).

## Design commitments

Six properties the design gives other things up to keep:

| Commitment | Why it holds |
|---|---|
| A control-plane compromise grants no database access | It holds no target credential ([EDR-0005](../../edrs/0005-control-plane-holds-no-credentials.md)) and cannot sign a marque alone ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)) |
| An issued marque works while the control plane is down | The Pilot verifies it by computation, not by asking ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)) |
| No standing access, anywhere | Marques, standing orders and delegations all expire ([ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/)) |
| No anonymous actor | Every principal is federated, including the machine ones ([EDR-0003](../../edrs/0003-federated-identity-and-sender-constrained-tokens.md)) |
| Nothing is silently narrowed | A statement outside your delegated scope aborts and says by how much ([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md)) |
| A retry never double-applies | Executions are fenced by a nonce claimed before the statement runs ([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)) |

## Status

**Design.** The decision records are published and implementation has not started. Nothing here runs
in production anywhere yet. See [Scope](./scope.md) for what is in the first release and what is
deliberately deferred.

## Where next

- [Architecture](./architecture.md) — the components, the object model, and the trust boundaries.
- [Scope](./scope.md) — what is in, what is out, the phases, and the prior art.
- [The cast](../concepts/cast.md) — Harbourmaster, Pilot, Leadsman, Tender: what each is *for*, and
  what each would never do.
- [Decision records](/edrs/) — every load-bearing decision, with the trade-off named.
