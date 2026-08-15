---
title: "Scope"
sidebar_position: 3
---

What Marque is for, what it will not do, what ships in which phase, and what already exists.

## Goals

1. **Make the grant the artefact.** Every statement run against a managed target is authorised by a
   signed object naming that exact statement, principal, role and window — and nothing runs without
   one.
2. **Make routine work cheap.** Standing orders and delegation keep the approval queue short enough
   that people respect the queue rather than routing around it.
3. **Make the record complete and hard to alter.** Who asked, who approved, what they were shown,
   what changed — appended, chained, and beyond the reach of Marque's own database role.
4. **Survive its own outage.** A grant already issued executes while the control plane is down.
5. **Reach the databases that actually exist**, including ones with no inbound route, in a different
   cloud from the control plane.
6. **Be adoptable by a team that did not write it.** One bootstrap URL, no per-client configuration,
   an operator playbook, and a deployment that does not assume any particular cloud.

## Non-goals

Named explicitly, because each is a plausible thing to ask for and each would change the system.

| Not doing | Why |
|---|---|
| **An interactive SQL client** | Exploration is a different product with a different security model. Marque takes a statement you already decided to run. Read-only exploration belongs behind a read-only role, and probably behind a different tool. |
| **Schema migrations** | Migrations need review, ordering, rollback and a deploy pipeline. Marque is for the one-off a pipeline cannot express. |
| **Managing database privileges** | Marque never issues `GRANT`. The role's existing privileges are the outer bound on everything it can authorise ([EDR-0006](../../edrs/0006-every-statement-names-a-role.md)). |
| **A general network tunnel** | A relayed Pilot speaks one narrow API. Adding port-forwarding would make it a bastion, and bastions cannot be deployed where this needs to go ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)). |
| **Model-driven approval** | The analyser holds no authority, and no configuration grants it any. Pre-authorised automation is standing orders and delegation, where a human granted the authority in advance over a shape they read ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)). |
| **Being an identity provider** | Groups and identities come from whatever the organisation already runs. Marque maintains no membership list. |
| **Data masking or DLP** | Rehearsal samples are redacted by default, which is a disclosure control, not a masking product. |
| **Non-database targets** | The object model is statements against data stores. Filesystems, queues and cloud APIs need a different scope grammar; if it happens it is a separate design. |

## In scope for the first release

**Targets.** PostgreSQL only — self-managed, Amazon RDS and Aurora, Cloud SQL. One engine done
properly, because the scope checker is a parser and a parser is per-engine
([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md)).

**Flow.** Submit → parse → rehearse → analyse → approve → execute → log, with edit-before-approve,
refusal with a reason, and resubmission.

**Authority.** Approval policy as reviewed configuration; delegation with object scope, row fence and
row limits; standing orders with constrained parameters; marques with not-before, expiry and
execution budget.

**Identity.** Any OIDC issuer; DPoP-bound tokens; AWS and GCP workload identity; freshness
requirements on approval and on critical execution.

**Deployment.** Control plane plus one or more Pilots; direct and relayed connectivity; AWS and GCP;
a local development mode needing nothing but PostgreSQL.

**Surfaces.** CLI (primary), web console (review and read), Slack notifications, operator playbook,
docs, decision records, changelog.

## Deferred, deliberately

| Deferred | Until |
|---|---|
| MySQL, then others | PostgreSQL has met real traffic and the parser boundary has settled |
| Multiple approvals per marque (`min_approvals > 1`) | The signature format already supports it; the review UX does not |
| Break-glass without a control plane | Needs a pre-issued, longer-lived marque shape and a careful ceremony |
| Scheduled and recurring executions | A marque that fires later is a different lifetime model |
| Per-tenant hosting for multiple organisations | The data model is tenant-partitioned from the start ([ZFN-15](https://zrz.io/zfn/15-partition-customer-data-by-tenant/)); the operational story is not |
| Notification channels beyond Slack | The WAL stream makes each one a handler, so this is cheap to add later and not on the critical path |

## Phases

**Phase 0 — design.** *This.* Architecture, scope, cast, decision records, docs site, changelog. Ends
with a design review.

**Phase 1 — the spine.** Harbourmaster, Pilot, PostgreSQL, the logbook, CLI submit/approve/run,
federated identity with DPoP, signed marques. No analyser, no console, no delegation, no relay. The
goal is one statement, correctly brokered, end to end.

**Phase 2 — usable.** Rehearsal, the analyser, Slack, the console, standing orders. This is the
point at which a team could adopt it.

**Phase 3 — scale of authority.** Delegation with fences, policy-as-configuration with authority
diffs, role introspection findings, the relay and cross-cloud Pilots.

**Phase 4 — leaving home.** Whatever the first outside team needs: the playbook proved by someone who
did not write the system, packaging, and the deployment story for an organisation that is not the
one it was built in.

## Prior art

Marque is not a new category. What follows is what exists and where it lands differently — this is a
"why build" argument, and [ZFN-31](https://zrz.io/zfn/31-own-your-components/) is clear that owning a
component is only justified when you genuinely understand the domain.

- **Bytebase** — database DevOps with schema review and change workflows. Strong on migrations and
  review; the centre of gravity is the change pipeline rather than a signed, expiring grant for an
  ad-hoc statement.
- **Teleport / StrongDM / Boundary** — identity-aware access proxies. Excellent at *reaching* a
  database as yourself, with session recording. The unit is a **session**, not a statement: they
  answer "who connected", where Marque answers "who approved this exact statement".
- **AWS Systems Manager Session Manager, Cloud SQL IAM** — remove standing credentials, which is real
  progress, and stop there. No approval step and no per-statement record.
- **CyberArk and other PAM** — approval workflows and vaulting, aimed at broad privileged access.
  Heavy, credential-vault-centric, and the vault is the thing worth attacking.
- **Home-grown "run this SQL" bots** — nearly every organisation has one. They are usually a Slack
  command that runs a statement as a shared superuser, with a chat message as the audit log.

What is genuinely different here, and worth the build:

1. **The grant is a signed artefact requiring the approver's own key**, so a compromised control
   plane cannot manufacture authority ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)).
2. **Delegated scope is enforced by a transactional fence that aborts**, rather than by predicate
   analysis that approximates or by a rewrite that silently narrows
   ([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md)).
3. **Measured rehearsal, not planner estimates**, in front of the approver
   ([EDR-0010](../../edrs/0010-rehearse-before-you-sign.md)).
4. **The analyser is structurally advisory** and cannot become an approver by configuration
   ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)).

If an existing product grew those four properties, the honest answer would be to use it.

## Success criteria

Phase 3 is a success if, in the deployment that adopts it first:

- **No standing production database credential is held by a person.** The bypass path is closed, not
  merely discouraged.
- **The median routine request never reaches a human**, via a standing order or a delegation.
- **The 95th-percentile time from submission to executable marque, for a request that does need a
  human, is under ten minutes** during working hours. Slower than that and people route around it.
- **A randomly chosen production change from three months ago can be fully reconstructed** — statement,
  approver, what they were shown, what changed — in one query.
- **One incident has been worked with the control plane unavailable**, using a marque issued before
  it went down.

## Risks

| Risk | Mitigation | Residual |
|---|---|---|
| People route around it | Standing orders, delegation, a fast CLI, and closing the bypass at the database | If adoption stalls, this is why; it is the top risk |
| Approvers rubber-stamp | Facts separated from prose, measured row counts, provenance markers | Real and unsolved ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)) |
| The checkable subset is too narrow to be useful | Good error messages; the subset is versioned so it can widen safely | May need several iterations against real statements |
| Marque becomes the outage | Marques verify offline; the analyser and the console never block | The submit-and-approve path is genuinely a new dependency |
| A rehearsal causes an incident | Structural rollback, short lock timeout, statement timeout | Sequence consumption and WAL churn are permanent side effects of a rehearsal |
| Scope creep into a SQL client | The non-goals table, and this line in it | Constant pressure |
