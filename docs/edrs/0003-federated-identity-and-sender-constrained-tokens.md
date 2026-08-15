---
id: 3
title: "Every principal is federated, and every token is bound to a key"
summary: "Marque has no local accounts and no long-lived keys. Humans authenticate through any configured OIDC issuer, workloads through their cloud's own identity, and every token is DPoP-bound so a stolen one is useless."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [identity, security, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

- **No local accounts, no local passwords, no API keys.** A human is whoever an accepted OIDC issuer
  says they are. A workload is whoever its cloud says it is.
- **Every access token is sender-constrained** with DPoP (RFC 9449): the token names a public key,
  and using it requires proving possession of the private half. A token lifted from a log, a proxy
  or a laptop is not usable on its own.
- **Workloads federate, never carry static keys.** A Pilot on AWS presents its task role; on GCP,
  its service account. Neither has an AWS access key or a GCP service-account JSON file anywhere.
- **The Leadsman is a principal too.** It has its own identity and acts under an explicit delegation
  from the submitter, and both names are on every record it produces.
- Approval and execution additionally require a **fresh, interactive** authentication — a token
  minted more than a few minutes ago is not sufficient to sign a marque.

## Context

Marque brokers changes to production data, so its authentication is the whole of its security
posture; nothing downstream re-checks who you are. Three standing positions apply almost verbatim.

[ZFN-6](https://zrz.io/zfn/6-sender-constrained-tokens-dpop/): a bearer token grants access to
whoever holds it. For a tool like this, the interesting theft is not a breach — it is a token in a
shell history, a CI log, or a screenshot in a support thread. Binding the token to a key that never
leaves the operator's machine removes that entire class.

[ZFN-9](https://zrz.io/zfn/9-no-long-lived-cloud-keys/): static cloud credentials are a documented
carve-out at best. A tool whose components hold database credentials must not *also* be a place
where cloud keys accumulate.

[ZFN-38](https://zrz.io/zfn/38-agents-are-principals/): the Leadsman is a language model reading an
operator's SQL. Handing it the operator's token would make its actions indistinguishable from
theirs, which is exactly the attribution failure that note is about — and the failure would surface
during an incident review, when the question is whether a human or a model decided something.

There is a fourth force the notes do not cover: **an approval must be an act, not a session
property**. If yesterday's login is enough to sign today's marque, then a laptop left unlocked is an
approval authority. Approval is the one operation where re-authentication is worth the friction.

## Decision

**Human authentication.** The client performs an OAuth 2.0 authorization-code flow with PKCE against
any issuer listed in the bootstrap document ([EDR-0002](./0002-bootstrap-discovery-document.md)). The
resulting identity token is exchanged at the Harbourmaster for a Marque access token. Where a
deployment already runs a token service, the exchange is RFC 8693 token exchange against it; where it
does not, the Harbourmaster performs the exchange itself. Either way the client code is the same.

**Key binding.** Every access token carries `cnf.jkt`, and every request carries a DPoP proof over
the method and URL. The client's private key is held in platform-backed hardware where the platform
has it (Secure Enclave, TPM) and in a file with restrictive permissions where it does not. A token
with `cnf.jkt` presented as a plain bearer is rejected — a token that committed to proof-of-possession
at mint time must be used that way, or the binding is decoration.

**Workload authentication.** Components authenticate as themselves:

| Runtime | Presents | Exchanged for |
|---|---|---|
| AWS (ECS/EKS) | task-role signature or the pod's projected token | a Marque workload token |
| GCP (Cloud Run/GKE) | metadata-server ID token | a Marque workload token |
| Elsewhere | any OIDC token from a configured issuer | a Marque workload token |

Marque itself stores no cloud credential. A deployment that has to use a static credential records
it as a carve-out with an owner and a review date, per ZFN-9.

**Delegated principals.** When the Leadsman analyses a request it acts under a delegation from the
submitter, expressed as an RFC 8693 `act` claim chain. Every artefact it writes names both the actor
(the analyst identity, with its model and prompt version) and the principal (the submitter). A record
with one name is a bug, not a shorthand.

**Freshness.** Two operations require an authentication no older than a deployment-configured
interval (default five minutes): **signing a marque**, and **executing** one against a target marked
`critical`. The client re-runs the interactive flow; the token carries `auth_time` and the server
checks it. A workload principal cannot satisfy a freshness requirement at all, which is the intended
answer — a machine may not approve.

## Consequences

**Easier.**

- There is no credential to rotate, revoke, leak, or find in a repository. Offboarding is removing
  someone from the identity provider, and it takes effect within a token lifetime.
- Attribution is exact, including for the model: "who ran this" and "what advised them" are separate
  columns with separate identities.
- A deployment reuses whatever identity infrastructure the organisation already runs. Marque does not
  become a second directory.

**Harder.**

- **The identity provider is now a hard dependency for every operation.** If it is down, nobody can
  submit or approve. Short of building a local account fallback — which would reintroduce every
  problem this record removes — the mitigation is that an *already-signed* marque stays executable
  ([EDR-0004](./0004-marques-are-signed-leases.md)), so an incident in progress can be worked.
- **DPoP is more client code than a bearer token**, and hardware-backed keys are platform-specific
  and awkward to test. The cost is real and it is paid once.
- **Freshness will annoy people.** An approver signing several marques in a session will
  re-authenticate more than once. Making it configurable is the escape hatch; making it default-off
  would mean it is never on.
- Every component needs a workload identity provisioned before it can start, which is more
  deployment prerequisite than a shared secret would have been.

**New obligations.**

- The delegation chain is validated, not merely recorded: a nested `act` chain deeper than the
  deployment allows is rejected.
- Model identity is versioned. Changing the model or the prompt bundle changes the actor recorded on
  an analysis, so an old analysis stays attributable to what actually produced it.

## References

- [RFC 9449](https://www.rfc-editor.org/rfc/rfc9449) — DPoP.
- [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) — token exchange, and the `act` claim.
- [ZFN-6](https://zrz.io/zfn/6-sender-constrained-tokens-dpop/),
  [ZFN-9](https://zrz.io/zfn/9-no-long-lived-cloud-keys/),
  [ZFN-38](https://zrz.io/zfn/38-agents-are-principals/),
  [ZFN-40](https://zrz.io/zfn/40-no-anonymous-system-actor/).
- [EDR-0002](./0002-bootstrap-discovery-document.md) — where the issuer list comes from.

## Changelog

- **2026-08-15**: Accepted.
