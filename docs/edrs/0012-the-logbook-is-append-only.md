---
id: 12
title: "The logbook is an append-only hash-chained journal"
summary: "Every request, analysis, approval, execution and revocation is appended to a hash-chained journal that Marque's own role cannot update or delete. Resubmission cites the prior entry rather than reopening it."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [security, ops, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

The logbook is a **journal**, not a table of current state: an ordered, append-only sequence of
immutable entries, each carrying the hash of its predecessor. Nothing is ever updated in place and
nothing is deleted.

- Marque's own database role holds `INSERT` and `SELECT` on the journal and **nothing else**. Not
  `UPDATE`, not `DELETE`. A bug, an injection, or a compromised control plane cannot rewrite history,
  because the privilege to do so is not held by the process.
- **And it must not own the table.** An owner can grant itself anything, so ownership would make the
  withheld grant decorative. The journal is owned by a separate role used only by migrations, which
  the running service cannot assume — the same distinction between *operating* and *authority* that
  [EDR-0025](./0025-tenants-are-partitioned-from-day-one.md) draws for deployment operators.
- Every entry names **actor and principal** — the identity that acted and, when acting under
  delegation, who it acted for. A one-name entry for a delegated action is rejected at write time
  ([ZFN-38](https://zrz.io/zfn/38-agents-are-principals/)).
- Current state — "which requests are open" — is a **projection** rebuilt from the journal, and is
  disposable.
- **Resubmission is a new request that cites the old one.** A closed request is never reopened, so
  the record of what was decided the first time stays intact.

## Context

The audit log is the deliverable. Everything else in Marque exists so that this record can be
trustworthy, and a mutable audit log is not an audit log — it is a table that usually agrees with
what happened.

[ZFN-12](https://zrz.io/zfn/12-queues-topics-journals/) distinguishes the three tools, and this is
unambiguously a journal: ordered, replayable, and read by more than one consumer (the console, the
projections, exports, the analyser's similarity lookup). Modelling it as mutable rows would lose the
ordering and the replay, which are the two properties worth having.

The privilege point is the one that does real work. It is easy to write "we never delete audit rows"
in a design document and then hold `DELETE` anyway, at which point the guarantee rests on every
future contributor remembering. Withholding the grant makes it enforced by the database
([EDR-0006](./0006-every-statement-names-a-role.md) is the same argument applied to targets).

Hash chaining is deliberately modest: it detects tampering by anyone who does *not* also control the
chain head, which covers the realistic cases — a compromised application, a careless migration, a
support engineer with database access. It is not a defence against an attacker who owns the database
and can recompute the chain, and this record does not claim otherwise.

## Decision

**Entry shape.** Append-only, one row per event:

```jsonc
{
  "seq": 918273,
  "at": "2026-08-15T09:14:02.113Z",
  "kind": "marque.signed",
  "subject": "mrq_01JB2Q9F3K8Z",
  "actor":     { "id": "approver@acme.example", "kind": "human" },
  "principal": null,                       // set when acting under delegation
  "delegation": null,                      // the grant that permitted it
  "payload": { … },                        // kind-specific, immutable
  "prev": "sha256:…",
  "hash": "sha256:…"                       // over (seq, at, kind, subject, actor, …, prev)
}
```

Kinds cover the lifecycle. **This list is illustrative, not closed** — later records add to it, and a
list presented as complete goes stale the moment one does:

`request.submitted`, `request.amended`, `analysis.written`, `rehearsal.completed`, `marque.signed`,
`marque.refused`, `marque.revoked`, `marque.expired`, `execution.claimed`, `execution.committed`,
`execution.rolled_back`, `execution.indeterminate`, `standing_order.approved`,
`standing_order.invoked`, `delegation.granted`, `delegation.revoked`, `policy.changed`,
`policy.reverted`, `surveyor.judged`, `audit.reviewed`, `key.enrolled`, `key.retired`,
`roster.published`, `task.opened`, `task.closed`, `agent.suspended`, `introspection.summarised`.

The **registry** of kinds lives with the schema ([EDR-0020](./0020-one-schema-generates-every-client.md)),
so adding one is a wire-contract change that a reviewer sees — which is the only way this stays
honest.

**Statement text is stored verbatim**, both as submitted and as signed. The digest is the identity;
the text is the evidence. Neither is truncated.

**Chain heads are published.** The current head hash is emitted periodically to a location Marque
cannot write to — an object store with object-lock, an append-only external log, or simply a
notification channel where it is recorded by something else. Without an external anchor, chaining
only proves internal consistency.

**Projections are rebuildable and marked as such.** The console reads projections; the journal is the
truth. Any inconsistency is resolved by rebuilding the projection, never by writing to the journal.

**Retention is longer than anything it describes.** Marques expire in hours; the journal is kept for
years. A retention policy that removes journal entries is a deployment decision that must be recorded
*in the journal*, and Marque itself never removes them — expiring the log of what happened is a
separate, deliberate, externally-operated act.

**Resubmission.** `marque resubmit <request>` opens a **new** request, pre-filled with the prior
statements, carrying a `resubmits` reference. The approver sees the earlier request's outcome, its
analysis and its approver inline — a request refused twice for the same reason should be visibly a
third attempt. Nothing about the old request changes.

## Consequences

**Easier.**

- The compliance question — who approved what, when, on what evidence — is one query with a
  cryptographic integrity argument attached.
- Incident reconstruction is a replay, and the analyser's "similar past requests" is a query over the
  same data rather than a separate index.
- A projection bug is recoverable rather than lossy.

**Harder.**

- **Storage grows without bound**, and statement text is not small. Sizing is a deployment concern
  from day one, and the honest guidance is that this is the last thing to economise on.
- **Corrections must be expressed as new entries.** A mistyped reason is corrected by appending, and
  the console has to render "this was later corrected" rather than showing the fixed value. Users
  will ask for edit; the answer is no.
- Chaining adds a serialisation point on write, since each entry needs its predecessor's hash. At
  Marque's write rate this is irrelevant, and it is worth naming so nobody is surprised.
- **Deleting data on request is genuinely hard.** If a statement or a sample contains personal data,
  a right-to-erasure request meets an immutable log. Mitigation is not storing it: samples are
  redacted by default ([EDR-0010](./0010-rehearse-before-you-sign.md)), and deployments should treat
  statement text as sensitive rather than assume it is not. There is no clean answer here and
  pretending otherwise would be dishonest.

**New obligations.**

- Chain verification runs on a schedule and on demand, and a break is an alert, not a log line.
- The external anchor is tested by restoring from it — an anchor nobody has ever read is a hope
  ([ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/)).

## References

- [ZFN-12](https://zrz.io/zfn/12-queues-topics-journals/) — journals versus queues and topics.
- [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/) — actor and principal on every delegated
  action.
- [ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/) — an untested anchor is not an anchor.
- [EDR-0013](./0013-async-work-rides-the-wal.md) — how entries reach Slack and the console.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: the kind list was presented as complete and had gone stale against four later records; it is now explicitly illustrative, with the registry living with the schema.
- **2026-08-16**: Amended after a second expert panel: Marque's role must not **own** the journal table: an owner can grant itself anything, which would make the withheld `DELETE` grant decorative.
