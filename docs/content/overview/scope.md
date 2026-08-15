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
   that people respect the queue rather than routing around it. Delegations can be written as
   sentences and compiled, so the people who understand the domain can scope them.
3. **Make agents safe to give production access to.** An agent submits as itself on behalf of a named
   human, runs what is inside an intersected scope it partly declares itself, and escalates
   everything else to that human rather than failing. This is a primary use case, not an adaptation.
4. **Make the record complete and hard to alter.** Who asked, who approved, what they were shown,
   what changed — appended, chained, and beyond the reach of Marque's own database role.
5. **Survive its own outage.** A grant already issued executes while the control plane is down.
6. **Reach the databases that actually exist**, including ones with no inbound route, in a different
   cloud from the control plane.
7. **Be adoptable by a team that did not write it.** One bootstrap URL, no per-client configuration,
   an operator playbook, and a deployment that does not assume any particular cloud.

## Non-goals

Named explicitly, because each is a plausible thing to ask for and each would change the system.

| Not doing | Why |
|---|---|
| **A pass-through database tunnel** | Marque provides an SQL shell and a loopback proxy that speaks the PostgreSQL wire protocol, so existing tools work unchanged — but it **parses and brokers every statement and forwards no bytes**. The interface is familiar; the control is not relaxed ([EDR-0022](../../edrs/0022-local-proxy-brokers-every-statement.md)). Explicit client transactions (`BEGIN`) are refused in the first release. |
| **Schema migrations** | Migrations need review, ordering, rollback and a deploy pipeline. Marque is for the one-off a pipeline cannot express. |
| **Managing database privileges** | Marque never issues `GRANT`. The role's existing privileges are the outer bound on everything it can authorise ([EDR-0006](../../edrs/0006-every-statement-names-a-role.md)). |
| **A general network tunnel** | A relayed Pilot speaks one narrow API. Adding port-forwarding would make it a bastion, and bastions cannot be deployed where this needs to go ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)). |
| **A model that creates authority** | A model may compile a delegation (a human signs the compilation) and may route a request as conforming or referred (inside a bound a human signed, referring on any doubt). It can never widen a scope, deny anything, or approve outside a signed bound ([EDR-0016](../../edrs/0016-natural-language-delegations-are-compiled.md), [EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)). |
| **Agents as approvers** | An agent submits; it never approves. Approval requires a fresh interactive authentication that no workload principal can satisfy, so this is mechanical rather than a rule ([EDR-0018](../../edrs/0018-agents-are-submitters-under-intersected-scope.md)). |
| **Being an identity provider** | Groups and identities come from whatever the organisation already runs. Marque maintains no membership list. |
| **Data masking or DLP** | Rehearsal samples are redacted by default, which is a disclosure control, not a masking product. |
| **Non-database targets, for now** | The first release brokers SQL statements. The object model is deliberately an *operation* against a *target*, so an agent's tool calls are the natural second engine — but each engine needs its own scope grammar and its own parser, and shipping a second badly would undermine the first. See "Deferred" below. |

## In scope for the first release

**Targets.** PostgreSQL only — self-managed, Amazon RDS and Aurora, Cloud SQL. One engine done
properly, because the scope checker is a parser and a parser is per-engine
([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md)).

**Flow.** Submit → parse → rehearse → analyse → approve → execute → log, with edit-before-approve,
refusal with a reason, and resubmission.

**Authority.** Approval policy as reviewed configuration; delegation with object scope, row fence and
row limits; delegations written in plain language and compiled for signature (Tier A); standing
orders with constrained parameters; marques with not-before, expiry and execution budget.

**Agents.** Agent enrolment with owners; task declaration and the three-way scope intersection;
escalation chains with the agent's principal always first; two-party records throughout; per-agent
quotas and one-action revocation.

**Identity.** Any OIDC issuer; DPoP-bound tokens; AWS and GCP workload identity; freshness
requirements on approval and on critical execution; RFC 8693 `act` chains for delegated action.

**Deployment.** Control plane plus one or more Pilots; direct and relayed connectivity; AWS and GCP;
a local development mode needing nothing but PostgreSQL.

**Surfaces.** CLI (primary); `marque psql`, a psql-compatible client that can be aliased in place
([EDR-0027](../../edrs/0027-be-psql-then-be-better-than-psql.md)); a loopback PostgreSQL-wire proxy
for the tools that cannot be replaced
([EDR-0022](../../edrs/0022-local-proxy-brokers-every-statement.md)); web console (review,
approve, agent supervision), Slack notifications, operator playbook, docs, decision records,
changelog.

**Extensibility.** A staged statement pipeline with an out-of-process provider SPI: `transform`
providers may rewrite a statement (constraint injection, name mapping, value synthesis, casts) and
`verify` providers may veto asynchronously. Providers may narrow or veto and can never widen or
disable a check ([EDR-0028](../../edrs/0028-statement-pipeline-and-provider-spi.md)).

**Connections.** Pooled, dynamically-credentialled connections; RDS/Aurora and Cloud SQL IAM
authentication; **per-operator database identity** where the engine allows, so the target's own audit
names the human independently; failover-aware driver wrappers with transparent retry disabled on
writes; reads routed to replicas under a freshness bound
([EDR-0021](../../edrs/0021-connections-identity-and-read-routing.md)).

## Deferred, deliberately

| Deferred | Until |
|---|---|
| MySQL, then others | PostgreSQL has met real traffic and the parser boundary has settled. Not a driver swap: MySQL has no `RETURNING` for the fence post-assert, no statement timeout for writes, and non-transactional DDL — so it ships with a published capability matrix or not at all ([EDR-0026](../../edrs/0026-a-second-engine-is-a-capability-matrix.md)) |
| Explicit client transactions over the proxy (`BEGIN`) | A marque authorises a statement set decided in advance; an open transaction is a client deciding what to do next from what it just saw ([EDR-0022](../../edrs/0022-local-proxy-brokers-every-statement.md)) |
| **Tier-B surveyed delegations** | Tier-A compilation has met real sentences, so the residual is known rather than assumed. It ships **off by default** and stays off until the sampled-audit loop and its suspension threshold are proven ([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)) |
| **Non-SQL operations** (agent tool calls, cloud APIs) | The SQL engine's scope grammar, fence and rehearsal have settled. Each engine is a parser, a fence mechanism and a rehearsal story — not a configuration flag |
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

**Phase 2 — usable.** Rehearsal, the analyser, Slack, the console, standing orders, and the SQL shell
and loopback proxy — the interface most people will actually use. This is the point at which a team
could adopt it.

**Phase 3 — scale of authority.** Delegation with fences, compiled plain-language delegations,
policy-as-configuration with authority diffs, role introspection findings, the relay and cross-cloud
Pilots.

**Phase 3b — agents.** Agent enrolment, task declaration and the three-way intersection, escalation
chains, the agent surface of the console, and per-agent containment. Tier-B surveying is built last
within this phase, shipped off, and turned on only for a deployment that has the sampled-audit loop
running.

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
- **Agent gateways and MCP authorisation layers** — a fast-moving space concerned with which tools an
  agent may call. The unit is a **tool call**, and the decision is usually made from the call's name
  and arguments. Marque's unit is the *effect on data*: it rehearses what a statement would actually
  do, fences it transactionally, and escalates to a named human rather than returning a denial.
- **Human-in-the-loop approval built into an agent framework** — approval as a callback inside the
  agent's own process. Convenient, and the approval evaporates with the process: no signed artefact,
  no independent record, and the agent's own code is the thing enforcing its own limits.

What is genuinely different here, and worth the build:

1. **The grant is a signed artefact requiring the approver's own key**, so a compromised control
   plane cannot manufacture authority ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)).
2. **Delegated scope is enforced by a transactional fence that aborts**, rather than by predicate
   analysis that approximates or by a rewrite that silently narrows
   ([EDR-0007](../../edrs/0007-delegation-by-containment-proof.md)).
3. **Measured rehearsal, not planner estimates**, in front of the approver
   ([EDR-0010](../../edrs/0010-rehearse-before-you-sign.md)).
4. **A model can never create authority.** The analyser has none; the compiler's output is signed by
   a human; the router chooses between two paths that both end in a human-granted scope
   ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md),
   [EDR-0016](../../edrs/0016-natural-language-delegations-are-compiled.md),
   [EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)).
5. **An agent's scope includes a term the agent itself declares**, per task, so over-declaration is a
   visible anomaly rather than an invisible risk
   ([EDR-0018](../../edrs/0018-agents-are-submitters-under-intersected-scope.md)).
6. **Out-of-scope agent work escalates to a named human instead of failing**, through a chain where
   each stage contributes only the authority it holds
   ([EDR-0019](../../edrs/0019-escalation-is-a-chain.md)).

If an existing product grew those six properties, the honest answer would be to use it.

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
- **At least one agent runs against production with a scope its owner can state in a sentence**, and
  its escalations are answered in minutes rather than accumulating.

## Risks

| Risk | Mitigation | Residual |
|---|---|---|
| People route around it | Standing orders, delegation, a fast CLI, and closing the bypass at the database | If adoption stalls, this is why; it is the top risk |
| Approvers rubber-stamp | Facts separated from prose, measured row counts, provenance markers | Real and unsolved ([EDR-0009](../../edrs/0009-the-leadsman-is-advisory.md)) |
| The checkable subset is too narrow to be useful | Good error messages; the subset is versioned so it can widen safely | May need several iterations against real statements |
| Marque becomes the outage | Marques verify offline; the analyser and the console never block | The submit-and-approve path is genuinely a new dependency |
| A rehearsal causes an incident | Structural rollback, short lock timeout, statement timeout | Sequence consumption and WAL churn are permanent side effects of a rehearsal |
| Scope creep into a SQL client | The non-goals table, and this line in it | Constant pressure |
| **A model wrongly routes something onto the fast path** | It can only route inside a bound a human signed; the fence and magnitude assertions still run; unanimous panel; default-refer; sampled audit with automatic suspension | An error inside a signed scope can still execute without a human seeing it. This is the accepted trade, and it is why Tier B ships off ([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)) |
| **A grantor signs a compilation they did not read** | Compiled form shown first, inferred clauses flagged, unbounded scopes refused | Real, and longer-lived than a single bad approval — a wrong delegation is wrong repeatedly |
| **Escalation volume makes approvers stop reading** | Tier-A compilation, standing orders, well-declared agent scopes, time-in-stage reporting | Agents raise volume; this gets worse before it gets better |
| **An agent integration declares its whole delegation as its task scope** | Anomaly signal on declared-versus-used | Visible, not preventable |
