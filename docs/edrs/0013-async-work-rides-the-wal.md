---
id: 13
title: "Async work rides the write-ahead log, not a job table"
summary: "Notifications, analysis dispatch and reaping are emitted transactionally into PostgreSQL's WAL and consumed by a replication listener, so an event cannot exist without its state change or be lost after it."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [architecture, ops]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Everything Marque does *after* a state change — post to Slack, dispatch an analysis, refresh a
projection, reap an expired marque, publish a revocation list — is driven by a listener on
PostgreSQL's write-ahead log, not by a job table and not by a call made after commit.

The event is emitted **inside the transaction that made the change**. Either both happen or neither
does. There is no window in which a marque is signed and the notification is lost, and none in which
a notification fires for an approval that rolled back.

Delivery is at-least-once and ordered per subject, so every consumer is idempotent. The listener
tracks its own position and replays from it after a restart.

## Context

Marque's async work is small in volume and unforgiving in correctness. An approver who is not
notified is an approval that does not happen; a notification for a rolled-back approval is a person
told a lie by an audit system.

The three usual approaches each fail in a specific way:

- **Call after commit.** The process dies between the commit and the call, and the notification is
  gone with no record that it was owed.
- **Dual write.** The state change and the event go to two systems, without a transaction spanning
  them. [ZFN-24](https://zrz.io/zfn/24-one-transactional-store-per-write/) is explicit: one
  transactional store per write, propagate asynchronously, never two-phase commit.
- **A job table.** Correct — it is transactional — but it puts polling load on the primary, needs
  claim/lease/retry machinery, and becomes a second source of truth about what is pending.
  [ZFN-37](https://zrz.io/zfn/37-every-lock-is-a-lease/) applies to the claim, and now there is a
  lease to operate.

[ZFN-48](https://zrz.io/zfn/48-emit-async-work-into-the-wal/) is the fourth option: emit the event
into the WAL with `pg_logical_emit_message`, which is transactional by construction, and consume it
with a replication listener. Outbox semantics with no outbox table.

There is a second reason it fits here. The logbook is already an ordered journal
([EDR-0012](./0012-the-logbook-is-append-only.md)), and the WAL *is* the ordering. Consuming the WAL
means the notification stream and the audit stream cannot disagree about what happened or in what
order.

## Decision

**Emission.** Every state-changing transaction ends with a logical message carrying the entry
identifier and its kind:

```sql
SELECT pg_logical_emit_message(true, 'marque', '{"seq":918273,"kind":"marque.signed", …}');
```

Transactional, so it commits with the change. The message is a **pointer plus enough to route on** —
consumers read the journal for the rest, so the message stays small and there is one copy of the
truth.

**Consumption.** A logical replication listener consumes the stream, tracks its confirmed position,
and fans out to handlers. Restart replays from the last confirmed position, so nothing is lost and
some things arrive twice.

- **Handlers are idempotent**, keyed on the journal sequence number. Slack posts carry a
  deterministic idempotency key; projection updates are upserts keyed on sequence and ignore
  anything already applied.
- **Ordering is per subject.** Two events about one marque arrive in order; two about different
  marques may interleave.
- **Handler failure does not advance the position** for that handler. A failing Slack integration
  retries with backoff rather than dropping the notification, and its lag is a metric.

**Heartbeats matter.** A stretch of WAL with no Marque messages must still advance the listener's
position, or a quiet period looks identical to a stalled listener and the slot grows without bound.
The listener consumes keepalives as well as messages.

**Replication slots need operating.** A slot whose consumer is gone retains WAL until the disk fills
— which takes the primary down. The slot's retained size is a monitored, alerted metric, and the
runbook covers dropping an abandoned slot.

**Reaping is a timer, not a message.** Expiry is derived from the clock, so the reaper is a scheduled
sweep that appends `marque.expired` entries, which then flow through the same stream. Its lag is
monitored: a stopped reaper leaves marques listed as live past their expiry, which is a display bug
now and a trust bug later ([EDR-0004](./0004-marques-are-signed-leases.md) means the Pilot enforces
`exp` regardless, so this is presentation, not authorisation).

## Consequences

**Easier.**

- The impossible states are impossible: no notification without the change, no change without an
  eventual notification.
- No job table, no claim/lease protocol, no polling load on the primary.
- Adding a consumer — an export, a metric, a second chat integration — is a new handler on an
  existing stream, with no change to the write path.

**Harder.**

- **This ties Marque to PostgreSQL** for its own state. That is an accepted constraint, not a
  regrettable one; the target databases it *manages* remain pluggable, and only Marque's own store is
  fixed.
- **Logical replication is real operational surface**: slots, WAL retention, `wal_level = logical`,
  the parameters a managed provider may or may not let you set. A team that has never run it will
  meet it here first.
- Debugging a stuck listener is harder than reading a job table, because "what is pending" is a
  position rather than a set of rows. The listener's position, lag and slot size all need to be
  first-class in the console.
- A managed PostgreSQL that forbids logical decoding cannot host Marque's own store.

**New obligations.**

- Slot size, listener lag, per-handler failure rate and reaper lag are alerted, not merely graphed.
- Handler idempotency is tested by replaying a known segment twice and asserting no duplicate effect.

## References

- [ZFN-48](https://zrz.io/zfn/48-emit-async-work-into-the-wal/) — emit async work into the WAL.
- [ZFN-24](https://zrz.io/zfn/24-one-transactional-store-per-write/) — one transactional store per
  write.
- [ZFN-12](https://zrz.io/zfn/12-queues-topics-journals/) — this is a journal.
- [EDR-0012](./0012-the-logbook-is-append-only.md) — what the messages point at.

## Changelog

- **2026-08-15**: Accepted.
