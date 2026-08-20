---
title: "The Dependabot security groups were inert, and an obligation was left where nobody would read it"
tags: [ops, security, docs]
order: -1
---

Review of the [Dependabot configuration](/changelog/) landed after it merged, and found two things
wrong with it. Recorded rather than quietly patched, because both are instances of failure modes this
repository names explicitly.

### Fixed

- **The three `security-updates` groups could not do anything.** They *shape* the pull requests
  GitHub's Dependabot security updates feature raises; they do not raise any. That feature was
  **off** for this repository — alerts were on, security updates were not — so the
  **security-grouping portion** of that change was behaviourally a no-op. The rest of it worked:
  scheduled version updates were grouped, and `pg_query_go` was isolated. It is on now, verified
  through the API, which makes the claim true rather than weakening it. The config says the groups
  depend on that setting, so the next person to find them quiet has somewhere to look.

  The sharper part: the same change had *deleted* the Actions security group for being "inert while
  reading as coverage", and left three more in precisely that condition. The standard was right and
  was applied to one case out of four.

- **An obligation was stated on M2 and written only in `.github/dependabot.yml`.** The config said
  "the major-version upgrade needs a human watching releases, and M2 is where that obligation lands"
  — and the [implementation plan](/overview/implementation-plan/) said nothing about it. That is
  `CLAUDE.md` rule 9, and the plan already carries the counter-example three sections earlier:
  *"that obligation is written into M2's own exit criterion rather than left here, because whoever
  implements M2 will read M2."*

  M2's **numbered steps** now carry it, as step 6 with a named owner: `pg_query_go` uses semantic
  import versioning, so `/v6` and `/v7` are different module paths and Dependabot will never propose
  that upgrade at all. It is a step rather than an exit criterion deliberately — an exit criterion
  has to be demonstrable, and "watch for releases" is a standing duty. What the exit checks is that
  the mechanism exists and someone owns it.

The two are worth telling apart. The inert groups depended on a repository setting no diff can show,
so reading the merged result was the only way to find them. The rule-9 violation was **visible in the
original diff** — a file assigning M2 an obligation while M2 went untouched — and was missed anyway,
which is the more useful of the two lessons.
