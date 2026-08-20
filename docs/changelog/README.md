# Changelog entries

One file per entry. `website/build.mjs` globs this directory and splices the entries into
`docs/content/changelog.md` at its `<!-- @entries -->` marker, newest first.

**Why a directory and not a file.** In a single `CHANGELOG.md`, every change inserts at the top —
which is the one line in a file where two changes always collide. Two pull requests merging on the
same day conflicted every time. A new entry is now a **new file**, so concurrent changes touch
disjoint paths and there is nothing to resolve.

Nothing enumerates these files; the build globs them. Do not add an index, a manifest, or a list of
entries anywhere. A registry would simply move the conflicting line out of the changelog and into
the registry.

## Adding an entry

Copy `template.md` to `YYYY-MM-DD-a-short-slug.md` and write it.

```markdown
---
title: "Marques carry an execution budget"
tags: [product, security]
---

A paragraph of context: what changed, and why anyone should care. Link the decision record that is
the source of truth — [EDR-0011](/edrs/0011-execution-is-idempotent-and-fenced/).

### Added

- **The lede in bold** — then the detail.
```

**Filename.** `YYYY-MM-DD-slug.md`, lowercase, digits and dashes only. The **date comes from the
filename and nowhere else**, so it cannot drift from a frontmatter copy of itself; the build rejects
a name that does not match.

**Frontmatter.**

| Key | Required | Notes |
|---|---|---|
| `title` | yes | Sentence case, no trailing full stop. Rendered as `<date> — <title>`. |
| `tags` | yes | A non-empty array from the closed vocabulary below. An unknown tag fails the build. |
| `order` | no | Breaks ties **within one day** — higher sorts earlier. Defaults to `0`. |

**Headings.** An entry must not contain a `## ` heading; the dated heading is generated. Use `### `
for `Added` / `Changed` / `Fixed` sections.

## Tags

The vocabulary is **closed**. `CHANGELOG_TAGS` in [`website/build.mjs`](../../website/build.mjs) is
what the build enforces — it validates entries against that array and renders the filter bar from it
— and this table is the copy people read. Adding a tag means editing both, and **the build fails if
you edit only one**: it parses this table back out and compares, naming the value and the file to
fix. The marker below is how it finds the table.

<!-- @vocabulary:changelog-tags -->

| Tag | For |
|---|---|
| `product` | A user-visible capability |
| `policy` | Approval policy, delegation and scope |
| `cli` | The `marque` command-line client |
| `console` | The web console |
| `security` | Authentication, signing, isolation and hardening |
| `ops` | Deployment, relays, runbooks and observability |
| `docs` | Documentation and decision records |
