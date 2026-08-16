---
title: "Twenty-two amendments, and the failure mode that produced most of them"
tags: [docs, security, policy]
order: 3
---

The second panel finished: **63 findings, 58 surviving** verification, and its synthesis found that two
thirds of its own report was already closed by the previous three commits. What remained was
**22 must-fixes, every one an amendment** — no supersessions, no new records, because none of them
reversed a decision.

### The failure mode

The synthesis named something worth quoting, because it is now this corpus's dominant defect:

> a new record states an obligation on an older one — *"EDR-0010's report gains `write_set`"*, *"this
> joins the list in EDR-0004"*, *"EDR-0026's capability table gains…"* — and the obligation is not
> discharged.

Three of those had shipped. Each was written in good faith by a record that *believed* it had changed
another one, and none had. Two were caught only because a third adversarial read went looking.
[CLAUDE.md](https://github.com/sixfathoms/marque/blob/main/CLAUDE.md) now carries a rule: if you write
that another record gains a field, a row or a rule, edit that record **in the same change** — and grep
your own diff for `gains`, `joins the list`, `is added to` before pushing.

### Records whose Decision contradicted their own TL;DR

The worst class, because an implementer builds from the Decision section:

- **[EDR-0030](/edrs/0030-a-marque-states-its-own-approval-requirement/)** restructured `approvals`
  into per-stage thresholds in its TL;DR and left the verification steps specifying the flat encoding
  it had replaced — so building the verifier from the Decision reproduced exactly the defect the
  restructure fixed.
- **[EDR-0004](/edrs/0004-marques-are-signed-leases/)**, the record a reader is sent to *first*, still
  said the Pilot verifies "given the deployment's JWKS … the subject against the authenticated
  caller" — verbatim the construction
  [EDR-0031](/edrs/0031-approver-keys-are-anchored-outside-the-control-plane/) exists to close, and the
  check [EDR-0032](/edrs/0032-a-marque-binds-its-executor-tenant-and-pilot/) replaced.
- **[EDR-0011](/edrs/0011-execution-is-idempotent-and-fenced/)** step 1 still checked the caller
  against `sub`, the dependency that broke offline execution for the caller.

### Genuine residuals

- **A rebuilt Pilot starts at genesis.** Making the roster's epoch high-water mark durable fixed a
  restart, not a re-deployment: a fresh container or restored volume begins at genesis, the genesis
  roster chains validly to the pinned root, and a compromised control plane walks it forward
  reinstating every retired key — the exact rollback EDR-0031's May-not table says is impossible. Now
  bound to the Pilot incarnation, with a `min_epoch` floor pinned at deployment.
- **`pg_stat_xact_all_tables` is not what I said it was.** Those are the backend's *pending*,
  session-scoped counters on a flush throttle, and connections are pooled — so back-to-back executions
  could read the previous one's relations. The check is now a **delta** captured at `BEGIN` and before
  `COMMIT`, which is exact regardless.
- **The write-set assertion is blind to `TRUNCATE`** (it zeroes counters rather than incrementing
  them) **and to writes on a separate session**. Both are now stated, with the `pg_relation_filenode()`
  detector for the first.
- **The machinery fingerprint had nowhere to live.** It was bound inside the `analysis` digest — which
  is one-way, is never delivered to the Pilot, and does not exist at all on a fast-path marque. Now its
  own signed payload field.
- **"Narrow or veto, never widen" was overclaimed.** The three mechanisms in
  [EDR-0028](/edrs/0028-statement-pipeline-and-provider-spi/) enforce containment within the
  submitter's authority; they do **not** enforce narrowing. A transform rewriting `WHERE id = 42` to
  `id = 43` stays in scope and passes every check. The record now says so, and says what a checkable
  version would require.
- **Break-glass reversion could not be signed.** An automatic reversion has no signers, so it could not
  produce the k-of-n policy epoch [EDR-0036](/edrs/0036-what-is-signed-must-be-what-was-seen/)
  requires. Both epochs are now pre-signed at apply time.
- **Recovery under the roster is heavier than the old ceremony.** With no live approver keys, no epoch
  can be signed at all — so recovery means a new genesis roster and re-pinning every Pilot by
  re-deployment, not reopening a window.

### Propagation

`EDR-0003`, `EDR-0006` and `scope.md` still carried the execution-freshness clause
[EDR-0035](/edrs/0035-execution-freshness-is-a-property-of-the-approval/) was written to remove — and
since the records win over the synthesis pages, the authoritative text still shipped the failure that
record documents. `require_execution_presence` existed in one record and in no schema. `EDR-0023`
contradicted `EDR-0015` on the `critical` signing default. `EDR-0008` had no `objects` field although
`EDR-0033` sources the fast-path reference set from it. `EDR-0005` was the only amended record in the
corpus with no dated changelog line — rule 1, broken in the most visible place.

All closed.
