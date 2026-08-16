---
title: "Operator playbook"
sidebar_position: 1
---

What running Marque involves: the duties, the signals, and the procedures.

> [!NOTE]
> **Written at design stage.** Nothing here has been performed against running software. It is
> written now on purpose — a playbook drafted against a design surfaces the capabilities the design
> forgot, and every procedure below that has no obvious implementation is a gap worth arguing about
> before it is expensive. Treat it as a specification of what must be operable, and expect it to
> change once someone has actually done these things.

## Who operates this

Three distinct duties, which may be the same person early on and should not stay that way:

| Duty | Does | Needs |
|---|---|---|
| **Deployment operator** | runs the control plane, Pilots and relays; applies policy; watches the signals below | infrastructure access, policy repository write |
| **Approver** | reviews and signs marques; answers escalations | an enrolled device key; membership in an approver group |
| **Grantor** | creates and reviews delegations and standing orders; owns agents | approval authority over the relevant target |

Marque deliberately cannot give anybody these — group membership comes from the identity provider
([EDR-0015](../../edrs/0015-policy-is-reviewed-configuration.md)).

## The signals that matter

Most of Marque's failure modes are quiet. A working deployment and a broken one look identical from
the outside, because the visible output of both is "requests get approved". These are the signals
that separate them, and **the first three are the ones that will actually bite**.

| Signal | Healthy | Why it is the one that bites |
|---|---|---|
| **Replication slot retained WAL** | flat, small | A stalled WAL listener retains WAL until the primary's disk fills, which takes Marque's own database down. This is the highest-severity alert in the system ([EDR-0013](../../edrs/0013-async-work-rides-the-wal.md)). |
| **Audit queue age** (Tier B) | under a day | The sampled audit is the *only* mechanism that makes a model's routing correctable. An unread queue silently removes it while everything still looks fine ([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)). |
| **Pilot reachability** | all registered | A relayed Pilot that silently deregistered looks healthy until the first request fails — during an incident ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)). |
| Time-in-stage, per approver group | minutes | A group with a median of hours has a rota problem, and people will route around the queue |
| Escalation rate per agent | stable | Rising means the agent's delegation no longer matches its job |
| Declared-versus-used scope gap per agent | small | A wide declaration for a narrow task is a badly built or compromised agent |
| Panel disagreement rate (Tier B) | low and stable | A delegation whose panel keeps disagreeing needs rewriting |
| Fast-path share of all executions | known | If it drifts to nearly everything, the queue has stopped being a control |
| Rehearsal-versus-actual row divergence | near zero | A large divergence means the data moved, or the fence is not doing what anyone thinks |
| **Roster age and epoch, per Pilot** | current | A compromised or broken control plane cannot forge an approver key, but it *can* withhold a roster update — so a new approver is unrecognised and a retired key stays live until Pilots see a newer epoch ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)) |
| Pilot pinned genesis root | unchanged | A changed root means someone re-deployed a Pilot against a different definition of who may approve |
| Rehearsal rate per principal | stable | A step change is what oracle extraction looks like ([EDR-0034](../../edrs/0034-the-pilot-api-has-one-authorisation-model.md)) |
| Pilot clock skew | milliseconds | A wrong clock either honours expired marques or refuses valid ones ([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)) |
| Logbook chain verification | passing | A break is an alert, never a log line ([EDR-0012](../../edrs/0012-the-logbook-is-append-only.md)) |
| Execution ledger size | bounded | Unbounded growth means reaping stopped ([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)) |

**Silence is ambiguous.** A day with no approvals is either a quiet day or a broken notifier. Alert
on the *absence* of expected activity — a notification handler that has posted nothing in an hour
during working hours is a finding, not a quiet period.

## Routine duties

**Daily.** Clear the Tier-B audit queue if one is enabled. Answer escalations. Look at anything
flagged `indeterminate` in the logbook — those are executions whose effect Marque genuinely does not
know, and only a human can resolve them
([EDR-0011](../../edrs/0011-execution-is-idempotent-and-fenced.md)).

**Weekly.** Review new and modified delegations, particularly Tier-B ones and any whose compilation
carried `inferred` clauses. Review role-introspection findings — a role that has *widened* is the
important one ([EDR-0006](../../edrs/0006-every-statement-names-a-role.md)). Review standing-order
invocation counts and retire the unused.

**Monthly.** Verify the logbook chain against its external anchor by actually restoring from it — an
anchor nobody has read is a hope ([ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/)).
**Audit the enrolled approver roster** against the logbook and the external anchor: every entry
should trace to an enrolment someone remembers, and a roster that verifies but is not anchored is a
finding ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)). Review
`secret-ref` carve-outs against their review dates. Review agents whose owners have left.

**Per release.** Re-run the compiler and Surveyor regression suites. A prompt or model change that
alters a compilation or flips a near-miss statement to `conforms` fails the build, and that is a
release blocker rather than a warning.

## Procedures

### Grant a delegation

1. Write the sentence, or the structured scope directly.
2. `marque delegate --from-text "…"` and **read the compiled form, not your sentence**. Check every
   clause marked `inferred`.
3. If a clause is refused as unexpressible, prefer rewriting the sentence so it compiles (Tier A)
   over accepting a Tier-B delegation. Tier A needs no model at request time at all.
4. Confirm the compiled scope. The signature is over the compilation
   ([EDR-0016](../../edrs/0016-natural-language-delegations-are-compiled.md)).

**The failure to watch for:** accepting a compilation with no row fence on a large table. Marque
refuses this above a configured size, but a table just under the threshold is your problem, not the
system's.

### Enrol an agent

1. Register the agent with an **owner** and a purpose. An ownerless agent is
   [the anonymous system actor](https://zrz.io/zfn/40-no-anonymous-system-actor/) arriving by
   attrition.
2. Provision its workload identity. It never receives a database credential and never receives a
   copy of anybody's token.
3. Delegate to it, narrowly. Start narrower than you think — escalation is the release valve, and an
   agent that escalates is working correctly, not failing.
4. Confirm the integration declares a per-task scope. One that declares its whole delegation has
   forfeited the best containment available to it
   ([EDR-0018](../../edrs/0018-agents-are-submitters-under-intersected-scope.md)).

### Suspend an agent

`marque agent suspend <agent>` — stops new tasks and submissions, revokes open tasks, and leaves the
owner's credentials and the agent's other grants untouched. Outstanding marques the agent already
holds are separately revoked with `marque revoke`, and take effect within the Pilots' revocation
refresh interval.

For "stop every agent on this target", suspend at the target level rather than one at a time.

### Turn off Tier-B surveying

`marque surveyor disable --target <target>` (or globally). It is **polled state, not an environment
variable** — it takes effect in seconds without an apply or a deploy, by design
([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)). Every Tier-B delegation
falls back to referring everything to a human; Tier-A delegations are unaffected.

Do this on: an audit finding you did not expect, a rising panel-disagreement rate, a model or prompt
change you did not review, or any suspicion of prompt injection.

### Revoke a marque

`marque revoke <mrq>` appends the revocation and publishes it. **There is a bounded window** — the
Pilots' refresh interval — in which a marque with `revocation.policy = required` may still execute.
If the marque is the urgent kind, also suspend the principal, or stop the Pilot.

### A stuck escalation

The request shows its chain, the current stage, who is being waited on, and what happens at timeout.
Options, in order of preference: notify the stage again; add a member to the approver group in the
identity provider; have the submitter resubmit with a narrower request that an available stage *can*
approve. **A timeout will never approve it for you**
([EDR-0019](../../edrs/0019-escalation-is-a-chain.md)).

If the chain is empty — "nobody can approve this" — that is a policy problem, not a request problem.
Fix the policy through review, or use break-glass.

### Break-glass policy change

`marque policy apply --break-glass` requires two authenticated principals, posts to the notification
channel immediately, and **sets an automatic expiry after which the previous version is restored**
unless the change has been merged. Merge the corresponding pull request the same day, or your change
reverts itself at the worst possible moment
([EDR-0015](../../edrs/0015-policy-is-reviewed-configuration.md)).

### Working an incident with the control plane down

This is the case the design exists for. **A marque already issued still executes** — the Pilot
verifies it locally and does not consult the Harbourmaster
([EDR-0004](../../edrs/0004-marques-are-signed-leases.md)).

What does not work: submitting, approving, and — after the revocation refresh interval — executing
marques with `revocation.policy = required`. **Execution itself does not need the identity provider**:
the caller proves possession of the key the marque names rather than presenting a token
([EDR-0032](../../edrs/0032-a-marque-binds-its-executor-tenant-and-pilot.md)), and execution requires
no fresh interactive authentication even on a `critical` target
([EDR-0035](../../edrs/0035-execution-freshness-is-a-property-of-the-approval.md)). Marques deliberately marked break-glass carry a
`grace` window and keep working; every such execution is flagged in the logbook as having run without
a fresh revocation check, and each one should be reviewed afterwards.

There is no way to mint a new marque with the control plane down. That is deliberate, and it is why
pre-issued break-glass marques for a `critical` target are worth holding *before* you need them.

### Suspected compromise

| Compromised | Immediate action | What it did not get |
|---|---|---|
| Harbourmaster | Stop it. Rotate its signing key. **Verify the current roster's chain independently before trusting any approval made during the window** ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)), and review rehearsal volume per principal for signs of an extraction oracle. Every marque signed after the suspected time is suspect — revoke them. **Also review fast-path invocations**: it could have invoked genuine standing orders as principals of its choosing where `invokers` names a group ([EDR-0029](../../edrs/0029-the-fast-path-authority-chain.md)) | Any database access; a marque for any statement shape no human had already signed |
| A Pilot | Stop it; rotate the credentials it could dereference; audit its ledger against the logbook | Authority to create marques; other Pilots' targets |
| An approver's device key | Suspend the person in the identity provider; revoke marques bearing their signature since the suspected time; re-enrol | Validity on its own — a marque also needs the countersignature |
| An agent | `marque agent suspend`; revoke its marques; review its declared-versus-used scope history | A credential; approval authority; anything outside the intersection |
| The Tender | Stop it; Pilots reconnect elsewhere | Statement or result contents — it is not a party to the session |

In every case the logbook is the ground truth, and it is the thing to verify first: run the chain
verification and compare against the external anchor before drawing conclusions from it.

## Things that will go wrong first

Honest predictions, so they are recognised rather than debugged from scratch:

- **The replication slot.** It is the one failure that takes down Marque's own database, and it
  arrives as a disk alert rather than as anything mentioning Marque.
- **Approver availability.** The first time the queue takes four hours, someone will ask for a
  standing credential "just for now". The answer is a delegation with an expiry.
- **A write-set abort on a delegation that used to work**, because the relation gained a cascading
  child. That is a finding about the delegation, not about the operator: look at what the relation is
  attached to before widening anything
  ([EDR-0033](../../edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)).
- **A Tier-B delegation that should have been Tier A.** Written vaguely, compiled partially, surveyed
  on every request forever. The compilation report says so; someone has to read it.
- **Clock skew on a Pilot** in a corner of the estate nobody looks at.
- **An agent integration that never declares a task scope**, because the SDK made it optional and the
  author was busy.
