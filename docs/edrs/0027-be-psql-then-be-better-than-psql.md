---
id: 27
title: "Be psql first, then be better than psql"
summary: "`marque psql` accepts psql's flags, meta-commands and output formats so it can be aliased in place. Catalog introspection is a named statement class that runs under the role without approval, logged in aggregate."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [cli, product]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

```sh
alias psql='marque psql'
psql -h prod-primary -U settings_writer -c 'select count(*) from accounts'
```

`marque psql` is a **client**, emulating psql's interface rather than wrapping it: the same flags,
the same meta-commands, the same output formats, the same exit codes. It talks to Marque's API
directly and needs no local port — the loopback proxy
([EDR-0022](./0022-local-proxy-brokers-every-statement.md)) exists for the tools we cannot replace.

The decision this forces: **`\dt` is a catalog query, and requiring an approval to list tables would
make the tool unusable.** So *catalog introspection* becomes a named statement class — read-only,
restricted to an allowlist of catalog and information-schema relations, executed under the role's own
privileges, needing no marque, and logged in aggregate rather than one entry per keystroke.

Longer term the aim is to be **better** than psql, which Marque is unusually placed to do: it already
parses every statement, knows the schema, and knows your scope. It can tell you a statement is
outside your delegation *before* you press enter.

## Context

Familiarity is the adoption lever. An operator who has typed `psql` for fifteen years has muscle
memory in their fingers, `\d` in their reflexes, and a `.psqlrc` they have tuned. Asking them to
learn a new client to do the same job is a tax on the routine work Marque most needs to capture —
and taxed routes get avoided, which is how a direct credential ends up in a password manager.

Aliasing is the strongest possible version of "keep your tools": nothing about the operator's habits
changes, and the brokering is invisible until it needs not to be.

The awkward part is meta-commands. psql's `\dt`, `\d`, `\l` and `\df` are not protocol features —
they are client-side conveniences that expand into `pg_catalog` queries. Running them through the
approval machinery unchanged would mean an approval request to list tables, which is absurd; running
them outside the machinery would mean an unbrokered path to the database, which is the one thing this
system does not have. Neither is acceptable, so introspection needs to be a *class* with its own
stated rules rather than a special case somebody added.

## Decision

### psql compatibility

**Flags**, matching semantics: `-c`, `-f`, `-h`, `-p`, `-U`, `-d`, `-A`, `-t`, `-F`, `-x`, `-q`,
`-e`, `-v`, `-X`, `--csv`. `-h` accepts a Marque target name as well as a hostname, and `-U` accepts
a role name — so an aliased invocation reads naturally.

**Meta-commands**, first release: `\?`, `\h`, `\q`, `\c`, `\l`, `\dt`, `\d`, `\d+`, `\dn`, `\dv`,
`\di`, `\df`, `\du`, `\x`, `\timing`, `\pset`, `\a`, `\t`, `\o`, `\i`, `\set`, `\unset`, `\watch`,
`\g`.

**Output formats**: aligned, unaligned, expanded and CSV, with `\pset` behaving as expected. Exit
codes follow psql's: `0` success, `1` a client-side fatal error, `2` a connection error, `3` a script
error under `ON_ERROR_STOP`.

**Refused, with a specific message** rather than a confusing error:

| Refused | Why |
|---|---|
| `\copy`, `COPY` | bulk data movement is not brokered in the first release ([EDR-0022](./0022-local-proxy-brokers-every-statement.md)) |
| `-1` / `--single-transaction`, `BEGIN` | a marque authorises a statement set decided in advance |
| `\e`, `\!`, and the `\|command` argument form of `\o` / `\g` / `\w` | shelling out from a brokering client is a surprising amount of authority. Stated as a **capability** — the client contains no code path that spawns a process — rather than as a list of meta-command names, because a list goes stale |
| `.psqlrc` | read only with `-X` semantics inverted: it is **not** read by default, because a file that silently issues statements at startup is the wrong default here. `--rcfile` opts in |

That last one is a deliberate deviation from psql and will surprise someone; it is called out in
`--help` rather than left to be discovered.

### Catalog introspection as a statement class

A statement is **introspection** when all of the following hold:

- it is read-only, and inside the checkable grammar
  ([EDR-0007](./0007-delegation-by-containment-proof.md)) — with **one exemption**: its relations may
  be allowlisted catalog *views*, notwithstanding that record's base-table rule, which the allowlist
  itself discharges;
- every relation it reads is on the engine's allowlist of catalog and information-schema views;
- every function it calls is on the purity allowlist.

Introspection then:

- **runs under the role, with no marque and no approval.** But note what actually bounds it: on
  PostgreSQL the role's privileges **do not** meaningfully restrict catalog reads — most of
  `pg_catalog` is world-readable, so "the database decides what the catalog shows" does no work here.
  **The reviewed allowlist is the control**, not the role. Where a meta-command has an
  `information_schema` equivalent, Marque prefers it, since those views *are* privilege-filtered; and
  where it does not, Marque conjoins `has_table_privilege` / `has_column_privilege` into the query it
  composes, so the answer reflects what this role may actually see.
- **The allowlist is column-aware.** `pg_proc.prosrc` is excluded — function bodies are source code
  and frequently contain embedded credentials — as is anything else whose value is not metadata.
- **is logged in aggregate**: a per-session rollup naming the principal, the target, the role, the
  count and the distinct relations touched. One entry per `\d` would drown the logbook and make the
  entries that matter harder to find ([EDR-0012](./0012-the-logbook-is-append-only.md)).
- **is quota'd** like everything else ([ZFN-18](https://zrz.io/zfn/18-enforce-quotas-at-ingress/)).

The allowlist is **relations, not schemas**. "Anything in `pg_catalog`" would be wrong: that schema
contains functions and views that read files and system state, and an allowlist by schema would
inherit whatever an extension adds to it later. Extensions are the specific reason this is a list
that gets reviewed rather than a prefix match.

**Introspection is not a privilege escalation path**, and the test suite says so: a statement that
joins an allowlisted catalog view to a user table is *not* introspection — it is an ordinary read
against a user table, and it takes the ordinary path.

### Better than psql, later

Direction rather than commitment, recorded so it is not re-litigated:

- **Scope shown before you run.** Marque knows your delegation and can parse as you type, so a
  statement heading outside your scope can say so before you press enter rather than after.
- **Rehearsal on demand.** `\rehearse` returns the measured row count without submitting anything.
- **Escalation inline.** A referred statement shows the chain, and the prompt can tell you when it
  clears.
- **Schema-aware completion** that reflects what your role can actually reach, rather than everything.

None of this is in the first release, and none of it changes the security model — each is Marque
surfacing what it already knows.

## Consequences

**Easier.**

- `alias psql='marque psql'` is close to a zero-friction migration for the people whose habits matter
  most.
- Brokering becomes invisible for the routine case and loud only when it needs to be, which is the
  right shape for a control people should not resent.
- Introspection has a stated rule instead of accumulating as exceptions.

**Harder.**

- **psql's surface is large and its behaviour is precise.** Partial compatibility is worse than none,
  because an alias implies completeness: a missing meta-command shows up as a broken tool rather than
  an unimplemented feature. This is a long tail with a compatibility test suite behind it.
- **The introspection allowlist is a security-relevant list that must be maintained**, per engine, and
  reviewed when extensions change. A wrong entry is a read path nobody approved.
- **Aggregate logging is a deliberate loss of detail.** If someone enumerates a schema before an
  attack, the logbook has the rollup and not the sequence. That is the accepted trade for a usable
  logbook, and it is stated here so it is not discovered during an investigation.
- Refusing `.psqlrc` by default deviates from the tool being emulated, which is exactly the kind of
  small surprise that erodes trust in an emulation.

**New obligations.**

- A compatibility suite runs real psql and `marque psql` against equivalent inputs and compares
  output, because "behaves like psql" is a claim that decays silently.
- The introspection allowlist is reviewed with the purity allowlist
  ([EDR-0007](./0007-delegation-by-containment-proof.md)); they have the same failure mode and should
  not drift apart.

## References

- [EDR-0022](./0022-local-proxy-brokers-every-statement.md) — the server-side emulation, for tools
  that cannot be replaced.
- [EDR-0006](./0006-every-statement-names-a-role.md) — why introspection is safe without an approval.
- [ZFN-4](https://zrz.io/zfn/4-incident-tooling-independence/) — the interface reached for during an
  incident should be the familiar one.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: corrected the reasoning behind catalog introspection — on PostgreSQL the role does not bound catalog reads, so the reviewed allowlist is the control — made the allowlist column-aware, and restated the shell-out refusal as a capability.
