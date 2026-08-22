---
title: "The walking skeleton walks"
tags: [product, cli, ops, security]
order: 3
---

M1's sentence, executed against a real database: submit a statement, store it, approve it, run it
against a target, and the result and the statement land in a table. The
[implementation plan](/overview/implementation-plan/)'s exit criterion for the milestone is a test
that runs those six steps and asserts the row changed — because every step can report success while
the database does not change, and only the last assertion knows the difference.

**It is not secure, and it says so whenever it does anything.** M1 has no signing, no grammar, no identity and
no fence, which means an approval is a name the caller typed. Every command that touches anything
refuses to run without `MARQUE_INSECURE_SKELETON=1`, and prints a banner naming each of those
absences — because a banner that says "not secure" and stops is one people learn to skip. `version`
is the deliberate exception: inspecting a binary should not require agreeing to what it would do if
you ran it. M5 deletes the flag; the test that
asserts it is gone is written now, skipped with that reason, and greps the built binaries rather
than the source.

### Added

- **Three binaries that do something.** `harbourmaster serve` records requests, approvals and
  reports; `pilot execute` runs one approved statement against a target and reports what happened;
  `marque submit | approve | status` is the operator's client, generated from the same schema
  ([EDR-0020](/edrs/0020-one-schema-generates-every-client/)).
- **`harbourmaster migrate` is a command, and startup is not.** Serving verifies the schema and
  refuses against one the binary does not match, rather than quietly changing it — migrating
  implicitly turns every deploy into a schema change nobody chose to run
  ([EDR-0042](/edrs/0042-the-control-plane-keeps-its-own-store/)).
- **The Pilot is the only thing that touches a target**, and the only thing that holds a target
  credential: it takes `--target-dsn` and the control plane never sees it
  ([EDR-0005](/edrs/0005-control-plane-holds-no-credentials/)). It runs one statement per
  invocation, because a long-lived Pilot with a queue runs statements with nobody watching, and M1
  is not where that should first exist.
- **The Pilot asks the control plane whether it may run.** A request the control plane calls pending
  is refused. Executing one anyway would make the Pilot the thing that authorises work, which is the
  arrangement the whole design exists to refuse.

### What the outcome vocabulary is for

The Pilot's four outcomes are most of M1's value, because the outcome is what the control plane
records and an outcome that lies is worse than no Pilot at all. Two of them look identical from the
outside and are not:

- the server **refused** the commit — a deferred constraint fired, say — so the transaction
  definitively rolled back and nothing was applied;
- **no answer** to the commit arrived, so it may have been applied and the acknowledgement lost.

Collapsing them is wrong in both directions. Reporting a refused commit as `indeterminate` sends a
human to inspect a database that is provably unchanged; reporting a lost one as `rolled_back` tells
them a statement did not run when it may have. There is no retry
([EDR-0021](/edrs/0021-connections-identity-and-read-routing/)), because replaying a write after a
failover applies a statement outside the accounting that was supposed to bound it.

The first version of that classification said "the server sent a message, so it refused". Then a
test took the connection away and watched it call a dead backend `rolled_back`: terminating a
backend sends `57P01`, which **is** a server message. A refusal is now an error the server chose to
return, judged by **severity alone** — a first attempt also excluded SQLSTATE classes, and a
deferred constraint trigger that raises at commit produces `XX000` while rolling the transaction
back, so the class carried no such meaning.

### Two bugs worth the telling

**Idempotency lost a race no sequential test could see.** `Submit` and `RecordExecution` are keyed —
on the caller's key and on the attempt's nonce — and both were written as
`INSERT … ON CONFLICT DO NOTHING` followed by a read-back. That returns *nothing*: `DO NOTHING`
yields no row, and the read-back cannot see a concurrent submitter's row until it commits. Eight
goroutines on one key, five of them got `sql: no rows in result set`. Both are now `DO UPDATE` with
a no-op `SET`, which takes the row lock, waits, and returns the surviving row whichever way the race
went.

**A `CHECK` constraint cannot be `DEFERRABLE`.** Only `UNIQUE`, `PRIMARY KEY`, `FOREIGN KEY` and
`EXCLUDE` can. Assumed while writing a test, then measured — which is this project's recurring
lesson and the reason the previous entry exists.

### What this still does not do

Everything the milestone said it would not. No statement is parsed, so `DROP TABLE` and
`UPDATE … WHERE id = 1` are the same to it. No approval is verified, no scope is applied, no
rehearsal is run, and nothing is signed. The tenant and the submitter come from configuration
because there is no identity to take them from
([EDR-0025](/edrs/0025-tenants-are-partitioned-from-day-one/)), and every request records its
submitter as `unauthenticated`, which is the truth.
