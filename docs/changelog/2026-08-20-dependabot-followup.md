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
  **off** for this repository — alerts were on, security updates were not — so the commit whose whole
  justification was fixing security grouping was behaviourally a no-op. It is on now, verified
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

  M2's exit criteria now carry it: `pg_query_go` uses semantic import versioning, so `/v6` and `/v7`
  are different module paths and Dependabot will never propose that upgrade at all. Watching for it
  is a human job from the day the dependency lands.

Both were found by a reviewer reading the merged result rather than the diff — which is the only way
either would have surfaced, since the diff was internally consistent and the defects were in what it
assumed about the world outside it.
