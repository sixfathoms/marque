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
that separate them. **The ranking is phase-dependent**: for a Phase 1–3 deployment — no agents, no
Tier B — the three that will actually bite are the **replication slot**, **Pilot reachability** and
**Pilot clock skew**. The agent and surveying signals matter once those features are on.

| Signal | Healthy | Why it is the one that bites |
|---|---|---|
| **Replication slot retained WAL** | flat, small | A stalled WAL listener retains WAL until the primary's disk fills, which takes Marque's own database down. This is the highest-severity alert in the system ([EDR-0013](../../edrs/0013-async-work-rides-the-wal.md)). |
| **Audit queue age** (Tier B) | under a day | The sampled audit is the *only* mechanism that makes a model's routing correctable. An unread queue silently removes it while everything still looks fine ([EDR-0017](../../edrs/0017-conformance-matching-may-route-never-widen.md)). |
| **Pilot reachability** | all registered | A relayed Pilot that silently deregistered looks healthy until the first request fails — during an incident ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)). |
| Time-in-stage, per approver group | minutes | A group with a median of hours has a rota problem, and people will route around the queue |
| **Break-glass rate per principal** | rare, and falling | A grant used routinely is a delegation somebody should have written properly; this is the drift signal, and drift is the real risk rather than abuse ([EDR-0037](../../edrs/0037-emergency-paths.md)) |
| **Break-glass review queue age** | under its deadline | An unreviewed break-glass removes the only mechanism that makes the capability correctable |
| **Approved marques expiring unused** | near zero | Someone got approval and never noticed; an approval latency problem its operators have already worked around ([EDR-0038](../../edrs/0038-a-request-is-a-shareable-watchable-object.md)) |
| Escalation rate per agent | stable | Rising means the agent's delegation no longer matches its job |
| Declared-versus-used scope gap per agent | small | A wide declaration for a narrow task is a badly built or compromised agent |
| Panel disagreement rate (Tier B) | low and stable | A delegation whose panel keeps disagreeing needs rewriting |
| Fast-path share of all executions | known | If it drifts to nearly everything, the queue has stopped being a control |
| Rehearsal-versus-actual **write-set** divergence | near zero | Compared per relation, not on top-level counts — a cascade measures identically on both sides, so the top-level comparison cannot fire ([EDR-0033](../../edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)) |
| **Roster age and epoch, per Pilot** | current | A compromised or broken control plane cannot forge an approver key, but it *can* withhold a roster update — so a new approver is unrecognised and a retired key stays live until Pilots see a newer epoch ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)) |
| Pilot pinned genesis root | unchanged | A changed root means someone re-deployed a Pilot against a different definition of who may approve |
| **Pilot incarnation versus roster/policy epoch** | incarnation stable, epochs at or above `min_epoch` | This is the observable for the reset-to-genesis case: a new incarnation sitting at a low epoch is a rebuilt Pilot that a compromised control plane could walk forward, reinstating retired keys ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)) |
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

**Weekly.** Review every break-glass use against its justification, and every break-glass **grant**
with its scope and usage count side by side — no uses in a quarter means remove it, many uses means
turn it into a delegation. Review new and modified delegations, particularly Tier-B ones and any whose compilation
carried `inferred` clauses. Review role-introspection findings — a role that has *widened* is the
important one ([EDR-0006](../../edrs/0006-every-statement-names-a-role.md)). Review standing-order
invocation counts and retire the unused.

**Monthly.** Verify the logbook chain against its external anchor by actually restoring from it — an
anchor nobody has read is a hope ([ZFN-36](https://zrz.io/zfn/36-test-backups-by-restoring/)).
**Audit the enrolled approver roster and the policy artefact** — both are epoch-chained and both need
their chains verified — against the logbook and the external anchor: every entry
should trace to an enrolment someone remembers, and a roster that verifies but is not anchored is a
finding ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)). Review
`secret-ref` carve-outs against their review dates. Review agents whose owners have left.

**Per release.** Re-run the compiler and Surveyor regression suites. A prompt or model change that
alters a compilation or flips a near-miss statement to `conforms` fails the build, and that is a
release blocker rather than a warning.

## Procedures

### Add a target

1. Add the target, its roles and their credential references to the policy repository
   ([EDR-0015](../../edrs/0015-policy-is-reviewed-configuration.md)); set its `criticality`.
2. Assign it to a Pilot in the target→Pilot map — a Pilot cannot volunteer for a target it was not
   assigned ([EDR-0014](../../edrs/0014-relay-for-targets-with-no-inbound-route.md)).
3. Provision the database roles and, for `per_operator` identity, the per-operator users. Marque never
   issues `GRANT` ([EDR-0021](../../edrs/0021-connections-identity-and-read-routing.md)).
4. Classify columns: `displayable_columns` for rehearsal samples, and the non-sensitive set the
   delegation compiler may see distinct values for
   ([EDR-0016](../../edrs/0016-natural-language-delegations-are-compiled.md)). **The default is
   redacted and ungrounded**, so a target added without this step works and compiles poorly.
5. Apply, then **verify positively**: exercise a read and confirm the session's actual database user
   on the target. A lazily-initialised pool hides broken authentication indefinitely.

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

### Respond to a break-glass

You will hear about it before you look — that is the design
([EDR-0037](../../edrs/0037-emergency-paths.md)).

1. **Read the justification.** It is bound into the signed marque, so it is what the person actually
   typed, not a reconstruction.
2. **Do not treat it as an incident by itself.** A break-glass during an outage is the system working.
   The finding is a break-glass that is *routine*.
3. **Review it before the deadline.** An unreviewed break-glass is a reported finding and, above a
   configured count, suspends the grant.
4. **If the same grant keeps being used, rewrite it as a delegation.** A capability used weekly is not
   an emergency capability, and leaving it as one means the loud path is the normal path.

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
pre-issued break-glass marques for a `critical` target are worth holding *before* you need them —
noting that break-glass **without** a control plane is itself deferred
([scope](../overview/scope.md)), so what exists today is a pre-issued `grace` marque and nothing more
automatic.

### Suspected compromise

| Compromised | Immediate action | What it did not get |
|---|---|---|
| Harbourmaster | **Confirmed compromise: stop it first.** Revoking asks the attacker to publish its own revocations, so it proves nothing; stopping it is what reliably works, because the revocation list then goes stale and `required`-policy marques refuse. Rebuild, then revoke from the rebuilt control plane, and treat any revocation published by the suspect plane as unverified. **For a partial or merely suspected compromise**, revoking first costs seconds and is the only thing that stops a `grace` marque inside its `grace_seconds`. Note that **rotating the signing key does not invalidate outstanding marques**; revocation does. **Verify the current roster's chain independently before trusting any approval made during the window** ([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)), and review rehearsal volume per principal for signs of an extraction oracle. Every marque signed after the suspected time is suspect — revoke them. **Also review fast-path invocations**: it could have invoked genuine standing orders as principals of its choosing where `invokers` names a group ([EDR-0029](../../edrs/0029-the-fast-path-authority-chain.md)) | Any database access; a marque for any statement shape no human had already signed |
| A Pilot | Stop it; rotate the credentials it could dereference; audit its ledger against the logbook | Authority to create marques; other Pilots' targets |
| An approver's device key | Suspend the person in the identity provider; revoke marques bearing their signature since the suspected time; re-enrol | Validity on its own — a marque also needs the countersignature |
| An agent | `marque agent suspend`; revoke its marques; review its declared-versus-used scope history | A credential; approval authority; anything outside the intersection |
| The Tender | Stop it; Pilots reconnect elsewhere | Statement or result contents — it is not a party to the session |

In every case the logbook is the ground truth, and it is the thing to verify first: run the chain
verification and compare against the external anchor before drawing conclusions from it.

**Also re-render `display`.** On a suspected control-plane compromise, re-render the canonical
`display` for every marque signed during the window and compare it to what the logbook says the
approver was shown ([EDR-0036](../../edrs/0036-what-is-signed-must-be-what-was-seen.md)). A
substituted payload is authentic in every other respect; this is the check that finds it.

**But be precise about what that proves.** Chain plus anchor prove **no rewrite** of entries up to the
last anchored head. They do not prove **no fabrication**: entries after that head are
attacker-influenced in *both* directions — invented as well as omitted. So for the window since the
last anchor, verify each `marque.signed` approver signature directly against the roster
([EDR-0031](../../edrs/0031-approver-keys-are-anchored-outside-the-control-plane.md)), and reconcile
`execution.*` entries against the Pilots' own ledgers and the target's own audit
([EDR-0021](../../edrs/0021-connections-identity-and-read-routing.md)). Three sources that must agree
is the actual control.

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
