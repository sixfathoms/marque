---
title: "The last seventeen: specification completeness across fourteen records"
tags: [docs, policy, security]
order: 4
---

The second panel's should-fix tail — seventeen items of specification completeness rather than design
error, and the kind that becomes an implementer's guess. Fourteen records amended; no decision
changed, so nothing superseded. **Both panels' findings are now closed.**

### Where a rule was stated in one place and contradicted in another

- **Attenuation still asked for entailment.** A delegation had to carry "a fence at least as tight" —
  the undecidable predicate-containment check that
  [EDR-0029](/edrs/0029-the-fast-path-authority-chain/) had already been rewritten to avoid, arriving
  once per hop in a chain, with the permissive approximation as the failure direction. It now uses the
  same rule: syntactic conjunct-set inclusion.
- **Check 7 had no meaning for a standing order**, whose artefact has no `fence` and no per-marque
  budget counterpart. It now says what it compares per artefact kind. And a delegation **chain** now
  ships whole, so a Pilot can verify attenuation at every hop rather than trusting it happened.
- **Long-lived artefacts had no signing moment.** [EDR-0030](/edrs/0030-a-marque-states-its-own-approval-requirement/)
  fixed the temporal acceptance rule for marques with `roster_epoch`; standing orders and compiled
  delegations — which a Pilot also verifies against roster keys, and which *outlive* marques — carried
  neither. Both now do.

### Where a claim was slightly larger than its mechanism

- **The compiler's output constraints do not bound meaning.** They make a compilation *ungroundable in
  fabricated evidence* — a literal it never received, a column that does not exist. They do not stop
  injected schema evidence steering the compiler to `tier <> 'production'` instead of
  `tier = 'sandbox'`: a wider predicate that is perfectly well-formed. What bounds *that* is the
  grantor's signature on the compiled form and attenuation against their own authority, which is now
  what the record says.
- **`prosrc` was not the whole channel.** `BEGIN ATOMIC` bodies live in `prosqlbody` and come back
  through `pg_get_function_sqlbody`. The exclusion is now a **closure invariant** — a function that can
  return the value of an excluded column is itself excluded — because a list of names goes stale and a
  rule does not.
- **The role-escalation findings omitted four ways to become powerful**: `pg_read_all_data`,
  `pg_write_all_data`, `BYPASSRLS`, and `CREATEROLE` (which on PostgreSQL 15 and earlier lets a role
  grant itself membership of any non-superuser role). Plus a writable schema on the resolution path —
  the introspection counterpart of EDR-0007's `search_path` pin.

### Relation identity, finally stated in full

TOAST relations enter the write set whenever a value crosses the toast threshold, which is
**data-dependent** — so without a rule a rehearsal passes and the execution aborts the first time
somebody's value happens to be large enough. They are excluded as storage for an in-scope relation;
inheritance children resolve like partitions; and a relation the mapping cannot resolve **aborts**
rather than being assumed benign.

### Five residuals added to SECURITY.md

The security page invites researchers to test its claims, so it now states what the records state:
fast-path volume is unbounded against a compromised control plane; the revocation list is signed by
the component whose compromise it exists to remediate; the write-set assertion is blind to `TRUNCATE`
and to writes on a separate session; a `transform` provider is trusted for statement content; and
catalog introspection is a read channel over object definitions. The object-scope claim is also
corrected from "the delegation's" to "the marque's", which [EDR-0033](/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation/)
moved.

### And the deferred row that read as deferring a shipped feature

"Multiple approvals per marque" implied escalation chains were deferred — they ship in the first
release and already put several signatures on one marque. What is actually deferred is **threshold
approvals within a single stage**: the payload encodes it, the review UX for collecting a second
signature at one stage does not exist.
