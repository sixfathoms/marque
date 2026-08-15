# CLAUDE.md

Guidance for AI assistants working in this repository.

## What this is

**Marque** brokers statements run against production data stores. An operator submits a statement; it
is analysed and rehearsed; a human with authority signs a **marque** — a scoped, time-bounded,
revocable grant; the statement executes under a named role inside that window; everything is appended
to a hash-chained logbook.

**The repository is public.** It is a generic tool and must stay one. Do not write any organisation's
schema, instance identifiers, hostnames, internal service names or customer data into examples, tests
or fixtures. Worked examples use a neutral fictional schema (`accounts`, `settings`, `tier`).

**Status: design.** There is no implementation. `docs/edrs/` is the entire substance of the project
today.

## Layout

```
docs/edrs/       Decision records — the source of truth for the design
docs/content/    Documentation pages, one directory per sidebar category
docs/changelog/  One file per changelog entry
website/         Static site generator (build.mjs), templates, styles
```

## Commands

```sh
cd website && pnpm install
cd website && pnpm run build     # the validator; run before pushing
cd website && pnpm run serve     # http://localhost:8080/marque/
```

The build **is** the test suite. It fails on: a record with a missing or over-240-character
`summary`; a duplicate id or an id/filename mismatch; a `proposed` record past its `proposed_until`;
a changelog entry with an unknown tag, a malformed filename, or a `## ` heading; a doc page in a
directory that is not a sidebar category; a changelog page missing its `<!-- @entries -->` marker.

## Rules

1. **A decision that changes goes in a NEW record.** Never edit the decision text of an existing one
   — set `status: superseded` and `superseded_by` on the old, `supersedes` on the new. An amended
   record gets a dated Changelog line, not a rewritten Decision section. See
   `docs/edrs/README.md`.
2. **A changelog entry is a NEW FILE** at `docs/changelog/YYYY-MM-DD-slug.md`. Never edit
   `docs/content/changelog.md` to add one; that single-file pattern is what conflicted on every
   same-day change. Do not add an index or manifest of entries — the build globs them, and a registry
   would just move the conflicting line.
3. **The changelog tag vocabulary is closed**, defined once in `CHANGELOG_TAGS` in
   `website/build.mjs` and documented in `docs/changelog/README.md`. Adding a tag means editing both.
4. **Render mermaid diagrams before committing.** Nothing validates them; a broken one builds fine and
   renders as an error box.
5. **Cite pull requests as links**, never a bare `#12`. Cite Field Notes as
   `[ZFN-N](https://zrz.io/zfn/N-slug/)`.
6. **British spelling in prose.**
7. **Be honest in Consequences.** A record with no "harder" section has not been thought through, and
   writing one that reads like marketing copy is worse than not writing it.

## The design's invariants

Load-bearing, and easy to undo by accident. A change that weakens one needs a superseding record, not
a commit message.

- **A marque carries two signatures** — the approver's device key and the control plane's. Neither
  alone is valid. Reducing this to one signature collapses the entire security argument, because a
  compromised control plane could then manufacture authority (EDR-0004).
- **The control plane holds no target credential**, and has no target database driver linked in. Any
  feature that needs a connection is a Pilot API, not a Harbourmaster one (EDR-0005).
- **A delegated row scope is a fence that aborts, never a rewrite.** Conjoining the predicate into the
  operator's `WHERE` is sound and silently narrows the statement, which produces a partially-applied
  change nobody reviewed. The pre-check, the post-assert and the row-count assert are three separate
  checks and all three are needed — the post-assert is the one that catches an `UPDATE` moving a row
  *out* of scope (EDR-0007).
- **The analyser has no authority, and no setting grants it any.** No risk score, no recommendation,
  no auto-approval — those are the shapes that get automated against. Pre-authorised automation is
  standing orders and delegation, where a human granted it in advance (EDR-0009).
- **A rehearsal has no code path that can commit.** Not a flag that defaults to rollback — no commit
  in the package (EDR-0010).
- **The execution nonce is claimed before the statement runs**, and the budget is consumed by the
  claim rather than by success. A crash must lose the attempt, not the count (EDR-0011).
- **Marque's own database role holds `INSERT` and `SELECT` on the logbook and nothing else.** The
  immutability is a withheld grant, not a convention (EDR-0012).
- **Standing-order parameters bind values and never contribute syntax** — not a table name, not a
  column, not a fragment of a predicate (EDR-0008).
- **The relay is a dumb pipe.** It never parses the Pilot API. Every feature request aimed at it
  ("could it cache…", "could it retry…") should be refused on principle (EDR-0014).
- **A model can never create authority a human did not sign.** This is the invariant that spans
  EDR-0009, EDR-0016 and EDR-0017 and it is the one to defend hardest. Concretely: a written
  delegation is compiled and **the human signs the compilation, not the sentence** (EDR-0016); the
  Surveyor has exactly two outcomes, `conforms` and `refer`, runs only *inside* a compiled bound a
  human signed, requires a unanimous panel, and resolves every error, timeout and ambiguity to
  `refer` (EDR-0017). Adding a third outcome, dropping unanimity, defaulting to `conforms`, or
  letting a Tier-B delegation exist without a compiled outer bound each changes the risk *category*.
  Tier B ships off and its sampled audit is what makes it correctable — an audit queue nobody reads
  silently removes the mitigation.
- **An agent's effective scope is the intersection of three grants**, including one the agent
  declares for its own task, and the declaration attenuates only and cannot be widened mid-task
  (EDR-0018). An agent can never approve: approval needs a fresh interactive authentication no
  workload principal can satisfy, and that mechanical impossibility is asserted by a test.
- **Escalation stage 1 for an agent is always its own principal**, every stage is a human, and **a
  timeout never satisfies a stage** (EDR-0019).

## Naming

Components are archetypes in one register — Harbourmaster (control), Pilot (data plane), Leadsman
(advisory), Surveyor (conformance), Tender (relay) — per [ZFN-43](https://zrz.io/zfn/43-name-systems-as-archetypes/). The
cast list at `docs/content/concepts/cast.md` is the authority and is part of the architecture. Do
not add an archetype for plumbing; plumbing gets plain descriptive names.

If a proposed feature feels wrong for a character, treat that as an architectural signal: either it
belongs elsewhere, or the component is becoming two components.
