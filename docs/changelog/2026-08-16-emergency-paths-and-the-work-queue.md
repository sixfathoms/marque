---
title: "Emergency paths, and making \"waiting for approval\" a useful thing to be told"
tags: [product, cli, policy, security]
order: 5
---

Two new records covering the part of the design that was consistently good at refusing and
consistently poor at 3am.

### Being told no is now useful

[EDR-0038](/edrs/0038-a-request-is-a-shareable-watchable-object/). A request is a **shareable,
watchable object**. The refusal carries the reference, the escalation chain with names, who is being
waited on right now, the measured rehearsal facts, a share link and how to watch it:

```
ERROR:  outside your delegated scope; submitted for approval
DETAIL:  req_01JB2Q9F3K8Z · 412 rows rehearsed, 0 outside fence
HINT:   waiting on sam@acme.example (stage 1 of 2), then group:data-oncall
        share:  https://marque.acme.example/r/01JB2Q9F3K8Z
        watch:  marque watch req_01JB2Q9F3K8Z
```

**The reference is an identifier, not a capability.** People will paste it into shared channels, so
holding it must grant nothing — and resolving one as an unentitled principal must 404 rather than 403,
so the reference does not confirm its own existence.

`marque requests` is the work queue: **pending and approved together**, because "what am I waiting on"
and "what can I run now" are the same question ten minutes apart. Then `marque run <ref>` and
`marque output <ref>`. An approved marque nearing expiry unused is surfaced — the most annoying
failure available is waiting for something that already arrived.

### Emergency paths

[EDR-0037](/edrs/0037-emergency-paths/), and the framing matters: **an emergency changes who is asked
and how loudly, not what is checked.**

**`--urgent`** notifies every stage at once instead of sequentially, pages instead of messaging, and
adds the target's emergency approvers. It never widens scope. Whether it may collapse a multi-stage
chain to one is a per-target setting, **default off** — the second stage exists precisely because the
first person's authority was insufficient, so collapsing it lets urgency manufacture authority nobody
granted, and left on by default everyone marks everything urgent within a month.

**Break glass is a pre-granted scope that lies dormant.** Someone grants you, by name, a scope you may
use only by explicitly breaking the glass — typing a justification, confirming deliberately, and
producing an authenticator assertion. Per-actor, and the shape is the deployment's to choose:

- *"Theo may run any statement if he breaks glass."*
- *"Theo may run any statement in an emergency **if a second holder co-signs**."*
- *"Theo may run any `UPDATE` on `public.*` in a break-glass scenario."*

The design consequence worth noting: because the grant is a **signed artefact**, breaking the glass
mints an ordinary fast-path marque ([EDR-0029](/edrs/0029-the-fast-path-authority-chain/)) and
**introduces no new verification case at all**. The human signed the shape in advance, exactly as with
a standing order. There is no code path that skips a check — which is the difference between an
emergency path and a hole.

And it is loud by construction: at the moment the glass breaks, the deployment channel, the target's
owners and every stage that *would* have been asked are notified, naming the person and quoting the
justification; a distinct logbook entry; a console banner for as long as the marque lives; and a
**mandatory post-hoc review** that suspends the grant if it goes unread. **No configuration suppresses
the notification** — a deployment wanting quiet emergency access wants a standing credential and
should say so.

### Why an emergency path at all

Because a control with no emergency path has an undocumented one. An operator watching an outage with
a fix in hand and no approver awake will route around anything that cannot answer them, and the thing
they route around it *with* is a standing credential in a password manager — the exact failure this
system exists to remove.

The hazard is not abuse; it is **drift**. The emergency path is faster, so it becomes the normal path.
Every mechanism above targets drift rather than malice: loudness, mandatory review, short expiry, and
a break-glass-rate-per-principal metric with a stated response — *a grant used routinely is a
delegation somebody should have written properly.*
