---
title: "The panel's should-fix tail: nineteen records amended, and four choices settled"
tags: [security, policy, ops, docs]
---

The remaining 29 findings from the expert panel — specification completeness rather than design error,
but the kind that turns into an implementer's guess. Nineteen records are amended; no decision
changed, so none is superseded.

Four of them were genuine choices rather than omissions, and were settled deliberately.

### The four choices

- **The revocation list stays control-plane-served.** Moving it to an independent origin would have
  made offline execution unconditional; keeping it avoids new infrastructure. **So the asterisk is now
  stated wherever the offline claim appears**: an issued marque executes offline for as long as the
  Pilot's revocation list is fresh, and past that only under `revocation.policy: grace`. The list also
  gains a defined shape — signed `issued_at`, monotonic `sequence`, `next_update`, and a refusal to
  accept a lower sequence than one already held.
- **Tier-B jurors use distinct providers where a deployment has more than one**, and where it does not,
  the delegation is **visibly marked as running a correlated panel** and audited at a higher rate. The
  word "independent" is gone: three calls to one model narrow *careless error*, not *correlated
  injection*, and it is the outer bound that holds against the latter.
- **The delegation compiler sees distinct column values only for columns classified non-sensitive.**
  This is the only path by which production data reaches a model whose output becomes a candidate
  authority artefact. Elsewhere it gets names and types, cannot ground a value inference, and refuses —
  so compilation degrades exactly where the data is sensitive.
- **Policy gains `require_envelope`, and `critical` targets default to hardware.** A file-backed
  platform key cannot approve the highest-consequence changes unless someone explicitly acknowledges it.

### The corrections worth knowing about

- **Catalog introspection was justified by the wrong reason.** The record said it was bounded by "the
  role's own privileges — the database decides what the catalog shows". On PostgreSQL most of
  `pg_catalog` is world-readable, so the role does no work there: **the reviewed allowlist is the
  control.** The allowlist is now column-aware, `pg_proc.prosrc` is excluded, and where a meta-command
  has an `information_schema` equivalent Marque prefers it because those views *are* privilege-filtered.
- **"Rehearsal identity" was self-contradictory.** A rehearsal under a read-mostly grant cannot measure
  a write, which is the entire point of rehearsing. It runs under the request's own role; the
  connection discipline is what makes it safe.
- **`lock_timeout` bounds waiting, not holding.** A rehearsal that acquires a lock then runs a slow
  second statement blocks production writers for the difference. There is now a total transaction
  budget and an out-of-band watchdog.
- **The logbook's kind list had gone stale** against four later records while being presented as
  complete. It is explicitly illustrative now, with the registry living with the schema so adding a
  kind is a wire-contract change a reviewer sees.
- **An agent chose its own `on_behalf_of`.** It must now name a human holding an active delegation to
  that agent, or its enrolled owner — and that human is notified at task open with a one-action disown.
  Otherwise "its human" was an assertion by the party being supervised.
- **The declared-scope anomaly signal is honestly labelled**: it bounds accidental blast radius and is
  **not a compromise detector**. An agent under an attacker's control declares narrowly and looks
  exemplary.

### Also

Chain verification proves **no rewrite**, not **no fabrication** — the playbook now says so, and
requires reconciling the window since the last anchor against the Pilots' ledgers and the target's own
audit. `marque psql`'s shell-out refusal is stated as a capability rather than a list of names. Bulk
data movement and non-transactional statements are named in the deferred table rather than implied to
be covered. The prior-art entry for Bytebase concedes its ad-hoc approve-and-expire loop before
naming the real delta, and dynamic credential brokers are added beside it.
