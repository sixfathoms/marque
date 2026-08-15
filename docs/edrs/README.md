# Marque decision records

This directory holds the **Engineering Decision Records** for Marque.

An EDR is a short, durable memo recording *a decision that has already been made*, the context that
led to it, and the consequences accepted. Discussion happens in pull requests and design reviews; by
the time something is an EDR, the decision is made and the merged pull request *is* the acceptance.

The [architecture page](../content/overview/architecture.md) is a synthesis of these records. Where
the two disagree, **the records win** — the architecture page is a reading of them, not a second
source of truth.

The practice is [ZFN-1](https://zrz.io/zfn/1-engineering-decision-records/). Marque's records cite
Field Notes (`ZFN-N`) freely: a Field Note is a standing engineering position, and an EDR is that
position applied to this system, with the trade-off named. Citing one is how a record says "this is
not a fresh opinion" — and when a record *departs* from a Field Note, it has to say so and say why.

## Create a record

1. Pick the next free number: `ls docs/edrs | tail -1`. Numbers are never reused.
2. Copy the template:
   ```sh
   cp docs/edrs/template.md docs/edrs/NNNN-short-kebab-slug.md
   ```
3. Fill in the frontmatter:
   - `id` — the number, unpadded. Must match the filename, or the build fails.
   - `title` — imperative and short ("Reach private targets through a relay the target dials out to").
   - `summary` — **required, 240 characters maximum.** One or two sentences saying what the decision
     *is*, written as an abstract rather than an introduction. It is what the index shows under the
     title, so it is read far more often than the record. Write it last, from the TL;DR. The build
     rejects a missing or oversized one; unbounded, these grow into a wall of text that makes an
     index unreadable.
   - `status` — almost always `accepted`. Merging means accepted.
   - `date` — today, ISO `YYYY-MM-DD`.
   - `authors` — name and email per author.
   - `tags` — free-form (`identity`, `policy`, `execution`, `ops`).
   - `supersedes` / `superseded_by` — record numbers, for lineage.
   - `aliases` — prior slugs, only when renaming a file.
4. Write **TL;DR**, **Context**, **Decision**, **Consequences**. Keep it tight; most records should
   fit on one screen. Be honest in Consequences — a record with no "harder" section is marketing.
5. Cross-reference sibling records in prose as `[EDR-NNNN](./NNNN-slug.md)`. The build rewrites those
   to rendered pages, so the source file stays valid markdown on GitHub.
6. Open a pull request. Reviewers argue the *decision* there. The merged record is the outcome.

## Status values

| Status | Meaning |
|---|---|
| `accepted` | Merged and authoritative. The default for almost every record on `main`. |
| `proposed` | **Rare.** Merged in draft to invite review. Requires `proposed_until: YYYY-MM-DD`, and the build fails once that date passes without the record moving on — a `proposed` record nobody came back to is the failure mode this guards. |
| `deprecated` | No longer recommended, and nothing replaced it. |
| `superseded` | Replaced by a later record. Set `superseded_by`. |

## Amending versus superseding

- **Typo, dead link, clarification, a footnote added after implementation** → amend in place and add
  a dated line to the record's Changelog. The decision is unchanged.
- **The decision itself changes** → write a *new* record with a higher number, set `supersedes: N` on
  it, and set `status: superseded` + `superseded_by: M` on the old one. Never edit the decision text
  of a superseded record: its whole value is being an accurate account of what we used to think.

## Do not write a record for

- Bug fixes.
- Implementation detail inside an already-decided design.
- Open questions. Those belong in a pull request description or a design note, not here.
- Conventions that are not load-bearing. "Use trailing commas" is a lint rule.

## Referencing records from code

Use `EDR-NNNN` in comments and commit messages so the reference is greppable:

```go
// EDR-0011: the execution nonce is the fence. A retry with the same nonce
// returns the first result rather than applying the statement twice.
```

```sh
git grep -nE '(EDR|ZFN)-[0-9]+'
```

## Building the site

```sh
cd website
pnpm install
pnpm run build     # validates every record's frontmatter; outputs website/dist
pnpm run serve     # http://localhost:8080/marque/
```
