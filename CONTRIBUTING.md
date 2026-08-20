# Contributing

Marque is at the design stage. Right now the most valuable contribution is an argument about a
decision record — especially one that says "this breaks when…" with a concrete case.

## Arguing with a decision

Open an issue or a pull request against the record. Records are meant to be falsifiable: each one
states a decision, the context that forced it, and the trade-offs accepted, so there is something
specific enough to be shown wrong.

The four most load-bearing, and therefore the most worth attacking:

- [EDR-0004](docs/edrs/0004-marques-are-signed-leases.md) — two signatures on a marque.
- [EDR-0005](docs/edrs/0005-control-plane-holds-no-credentials.md) — the control plane holds no
  credentials.
- [EDR-0007](docs/edrs/0007-delegation-by-containment-proof.md) — object scope proved, row scope
  fenced, everything else escalated.
- [EDR-0009](docs/edrs/0009-the-leadsman-is-advisory.md) — the analyser has no authority.

If a change alters what a record *decided*, do not edit it. Write a new record that supersedes it —
[docs/edrs/README.md](docs/edrs/README.md) has the procedure. The value of a superseded record is
being an accurate account of what we used to think.

## Changes that need a record

A new record is warranted for a cross-component contract, a hard-to-reverse choice, a security
property, or a convention others must follow. Not for bug fixes, implementation detail inside a
decided design, or open questions.

## Changes that need a changelog entry

Anything user- or operator-visible. One **new file** at `docs/changelog/YYYY-MM-DD-slug.md` — never
an edit to the changelog page, which is what keeps two same-day changes from conflicting. Format:
[docs/changelog/README.md](docs/changelog/README.md).

## Docs

```sh
cd website
pnpm install
pnpm run build     # this is the validator; run it before pushing
pnpm run serve
```

The build fails on a record with a missing or oversized `summary`, a record with a missing or unknown
`implementation`, a duplicate or mismatched id, a `proposed` record past its `proposed_until`, a
changelog entry with an unknown tag or a malformed filename, a doc page in a directory that is not a
sidebar category, and a vocabulary table in a README that has drifted from the constant it mirrors or
cannot be read as a vocabulary table. That is deliberate: these are the mistakes that otherwise ship and are noticed months
later.

**Render any mermaid diagram before committing.** Nothing validates them at build time — a broken
diagram builds fine and renders as an error box on the page.

## Style

- British spelling in prose.
- Pull requests are cited as links, never as a bare `#12`, which resolves only on GitHub and only for
  a reader who already knows the repository.
- Field Notes are cited as `[ZFN-N](https://zrz.io/zfn/N-slug/)`. If a decision departs from one, say
  so and say why — that is the interesting part.
- Be honest in Consequences. A record with no "harder" section has not been thought through.

## Licence

Contributions are accepted under the Apache-2.0 licence in [LICENSE](LICENSE).
