---
id: 2
title: "One bootstrap URL is the only client configuration"
summary: "A Marque deployment publishes its own configuration at a well-known path. A client is configured with one URL and discovers issuers, audiences, endpoints, relays and capabilities from the server."
status: accepted
date: 2026-08-15
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [identity, cli, ops]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

`marque login https://marque.example.com` is the whole of client setup. The server publishes a
configuration document at `/.well-known/marque-configuration`, and everything else — which identity
providers to use, what audience to ask for, where the Pilots and relays are, where the revocation
list lives, what the deployment supports — is read from it and cached. The document is **served over
TLS and unauthenticated**: every field in it is a pointer, never a secret.

Clients hold exactly one piece of configuration: the bootstrap URL. There are no per-environment
client flags, no shipped defaults for issuers or endpoints, and no client release needed to move a
service or add a provider.

## Context

The alternative is what most internal tools do: the client ships a table of environments, each with
an issuer, an audience, an API host, a bastion identifier and a namespace, and every one of those
values is a chance for an operator to be pointed at the wrong environment while believing they are
in the right one. In a tool whose entire job is running statements against production, "pointed at
the wrong environment" is the worst failure it has.

That table also has to be *maintained by the client*. Rotating an issuer, moving a Pilot, or adding
a second cloud means shipping a client release and then chasing everyone who has not upgraded — with
the operators on stale binaries being, reliably, the ones reached for during an incident.

There is a settled shape for this. OpenID Connect Discovery publishes issuer metadata at a
well-known path; OAuth 2.0 Authorization Server Metadata (RFC 8414) does the same; Protect's own
environment discovery document does it for an operator CLI. The pattern is proven and the client
code is boring, which is what [ZFN-30](https://zrz.io/zfn/30-use-standards-dont-reinvent/) asks for.

## Decision

A Marque deployment serves an unauthenticated document at
`GET {bootstrap}/.well-known/marque-configuration`:

```jsonc
{
  "deployment": "acme-production",
  "display_name": "Acme (production)",
  "issued_at": "2026-08-15T09:00:00Z",
  "authentication": {
    "issuers": [
      { "issuer": "https://accounts.google.com", "audience": "…apps.googleusercontent.com",
        "subject_domains": ["acme.example"] },
      { "issuer": "https://sts.acme.example", "audience": "marque", "token_exchange": true }
    ],
    "dpop_required": true,
    "dpop_algorithms": ["ES256"]
  },
  "endpoints": {
    "harbourmaster": "https://marque.example.com",
    "console": "https://marque.example.com/console"
  },
  "pilots": [
    { "id": "pilot-us-west-2", "cloud": "aws", "region": "us-west-2",
      "address": "https://pilot-usw2.marque.example.com" },
    { "id": "pilot-eu-1", "cloud": "gcp", "region": "europe-west1",
      "relay": { "kind": "tender", "id": "tender-eu-1" } }
  ],
  "capabilities": ["standing-orders", "delegation", "rehearsal", "offline-execution"],
  "revocation_uri": "https://marque.example.com/.well-known/revocations",
  "relays": [ { "id": "tender-eu-1", "addresses": ["https://tender-eu-1.marque.example.com"] } ],
  "jwks_uri": "https://marque.example.com/.well-known/jwks.json",
  "min_client_version": "0.4.0"
}
```

Rules:

- **The document is the authority.** A client that has a cached copy refreshes it on a cache-control
  expiry, on any authentication failure, and on any `unknown capability` error. It never merges the
  document with built-in defaults — there are none to merge with.
- **It is unauthenticated but not untrusted.** It is served over TLS from the bootstrap origin, and
  every field in it is a *pointer*, never a secret. The signing keys it names are fetched from
  `jwks_uri` and pinned per deployment on first use; a change to the key set is reported to the
  operator rather than accepted silently.
- **`min_client_version` is advisory and one-directional.** A client older than the floor warns and
  continues if it can, and refuses only the specific operations it cannot perform correctly. A
  version gate that hard-fails is a way to strand the on-call engineer.
- **The bootstrap URL is the deployment's name.** Client state — cached credentials, the DPoP key
  binding, the instance cache — is keyed on it, so having two deployments configured cannot cause
  one's credentials to be presented to the other.
- The document is also the **console's** configuration, fetched by the web UI at load, so there is
  no second place where a deployment describes itself.

## Consequences

**Easier.**

- Onboarding is one URL, given out once. There is no runbook step that consists of pasting five
  values, and therefore no step that can be pasted wrong.
- Moving a Pilot, adding a region, adding a second identity provider or turning on a capability is a
  server-side change that every client picks up on its next refresh.
- The document is a genuinely useful diagnostic: `marque env show` prints what a deployment claims
  about itself, which is the first question in any "why can I not authenticate" conversation.

**Harder.**

- **The bootstrap URL is now a trust root.** Whoever controls that origin controls where clients
  send their tokens. It is one URL to get right rather than five, which is a real improvement, but
  it is not zero — hence pinning `jwks_uri` keys per deployment and reporting changes.
- **An unreachable bootstrap origin blocks a cold client.** A client that has never contacted the
  deployment cannot do anything. Mitigated by caching the document durably and treating an expired
  cache as usable-with-a-warning rather than fatal, so an operator who used the tool yesterday can
  use it during today's outage.
- The document leaks deployment topology — region names, service hostnames, which capabilities are
  on — to anyone who can reach it. That is the same disclosure OIDC discovery makes, and it names no
  target, no schema and no principal.

## References

- [RFC 8414](https://www.rfc-editor.org/rfc/rfc8414) — OAuth 2.0 Authorization Server Metadata; the
  shape this imitates.
- [ZFN-30](https://zrz.io/zfn/30-use-standards-dont-reinvent/) — use the standard.
- [ZFN-35](https://zrz.io/zfn/35-dereference-secrets-not-store-in-config/) — configuration holds
  references, not values.
- [EDR-0003](./0003-federated-identity-and-sender-constrained-tokens.md) — what the client does with
  the issuers it discovers.

## Changelog

- **2026-08-15**: Accepted.
- **2026-08-16**: Amended after the expert panel's should-fix pass: resolved a contradiction between the TL;DR ("signed") and the Decision ("unauthenticated"), and added `revocation_uri` and a `relays` block.
