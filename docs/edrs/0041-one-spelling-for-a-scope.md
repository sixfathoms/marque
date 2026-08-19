---
id: 41
title: "Spell a scope the same way in every artefact"
summary: "A fence is an array of conjuncts in every artefact that carries one, a relation is a schema field and a relation field, and an operation is lowercase. Conjuncts compare as decoded strings, so canonicalisation normalises nothing."
status: accepted
implementation: partial
implementation_note: "The vector half exists: `internal/conformance` already decodes `operation`, `schema` and `relation` in this spelling and rejects anything else. The artefact half — delegations, compiled delegations, grants, marque payloads — is prose in records, because nothing parses one yet."
date: 2026-08-19
authors:
  - "Theo Zourzouvillys <theo@sixfathoms.dev>"
tags: [policy, execution, foundational]
supersedes: null
superseded_by: null
aliases: []
---

## TL;DR

Three concepts were spelled inconsistently across the normative artefacts — a fence two ways, a
relation three, an operation two — and two specified checks compare across the split. This record
picks the spelling and defines the comparison.

- A **fence** is a JSON array of conjunct strings, in every artefact that carries one.
- Two conjuncts are equal when their **decoded characters** are equal. That is what
  "canonicalisation" means in [EDR-0029](./0029-the-fast-path-authority-chain.md) check 7: nothing is
  normalised.
- A **relation** is a `schema` field and a `relation` field. Never one dotted string.
- An **operation** is lowercase.

Whoever extracts scope at M2, or implements the containment proof or check 7 on the fast path,
implements these. The records carrying the losing spelling are corrected by the same change that adds
this one.

## Context

`fence` was an array in the artefacts the Pilot verifies and a string in the grants authority
descends from:

| Spelling | Where |
|---|---|
| `"fence": ["tier = 'sandbox'"]` | the marque payload — [EDR-0004](./0004-marques-are-signed-leases.md), [EDR-0029](./0029-the-fast-path-authority-chain.md) |
| `"fence": "tier = 'sandbox'"` | the delegation ([EDR-0007](./0007-delegation-by-containment-proof.md)), the compiled delegation ([EDR-0016](./0016-natural-language-delegations-are-compiled.md)), an agent's declared scope ([EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md)) |

A relation had three spellings, not two: `"table": "public.accounts"` in most grants,
`{"schema": "public", "table": "*"}` in [EDR-0037](./0037-emergency-paths.md), and
`{"schema": "public", "relation": "accounts"}` in the conformance *format*. Operations were uppercase
in every record and lowercase in that format. (The corpus itself is empty until M2; what disagreed
was the format and its loader.)

Two checks are specified across the split.

**EDR-0029 check 7** states the fast-path rule as *"syntactic identity after canonicalisation:
`marque.fence == artefact.fence`"*. On that path no human is present at mint time. As written the
equality has an array on one side and a string on the other, and the whole reconciliation hides
inside a word no record defines.

**EDR-0007's attenuation rule** requires *"syntactic conjunct-set inclusion, never by entailment"*
and says "the `fence` array is conjunctive" — eleven lines after its own worked example shows a
string. If the authored form is a string, recovering its conjuncts means parsing SQL. The parser
belongs in the Pilot anyway — EDR-0029 step 5 has it re-check object scope — so the cost is not a
new
dependency but a moving one: [EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)
says a `pg_query_go` upgrade parses statements the previous one refused, and a fence comparison that
went through the grammar would make an **already-signed delegation mean something different after a
dependency bump**. A comparison over strings cannot move.

The failure direction is the bad one. An undefined comparison between a one-element array and a
string invites a coercion, and a fence comparison that resolves permissively is exactly the error
both records say they exist to avoid. How a given implementation would break is not knowable from
here, which is the reason to define it rather than to predict it.

This is the discipline of [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) applied
where the schema does not reach. [EDR-0020](./0020-one-schema-generates-every-client.md) makes one
schema the source of every client, so the clients cannot disagree with each other about a field; these
artefacts are hand-authored in records, and hand-authored is what drifts.

## Decision

### A fence is an array of conjuncts

Everywhere a fence appears: the marque payload, the delegation, the compiled delegation, an agent's
declared scope. The fence is the conjunction of its elements.

A break-glass grant ([EDR-0037](./0037-emergency-paths.md)) is **not** in that list, and this record
does not put it there. Two facts, neither inferred: EDR-0037's grant carries no `fence` field, and
EDR-0029's break-glass verification enumerates what the Pilot checks — the grant's signatures, its
digest, its `not_after`, the statement against its `scope`, `sub`, the marque's `exp`, and the bound
justification — with no fence among them. A fence written on a break-glass grant would therefore be
a bound nothing compares. Giving break-glass a fence means giving its verification a case for it,
which is EDR-0029's decision to make. EDR-0037 meanwhile lists the fence among the controls
break-glass leaves unchanged, which is true of the mechanism and hollow as a control while no signed
artefact carries one — [issue #26](https://github.com/sixfathoms/marque/issues/26), due before
Phase 2.

```jsonc
"fence": ["tier = 'sandbox'", "region = 'eu'"]     // tier = 'sandbox' AND region = 'eu'
```

**Each element is parenthesised on its own when the fence becomes SQL.** The Pilot builds
`(c1) AND (c2) AND …`, never `c1 AND c2`. A conjunct containing a top-level `OR` otherwise rebinds
against the following `AND` — `tier = 'sandbox' OR tier = 'trial'` beside `region = 'eu'` binds as
`tier = 'sandbox' OR (tier = 'trial' AND region = 'eu')`, which admits every sandbox row in every
region. The fence comes out **wider than it was written** — a different mechanism from the
`NOT (fence)` bug EDR-0007 corrected, and the same class of defect: a predicate composed into SQL
without regard for how SQL will read it. The TRUE-only rule applies to the whole conjunction:
`((c1) AND (c2)) IS NOT TRUE`. The rule is about a *fence's conjuncts*: it does not make every
`(A) AND (B) IS NOT TRUE` in the corpus wrong, and
[EDR-0028](./0028-statement-pipeline-and-provider-spi.md)'s comparison of two whole predicates is
correct as written.

**A conjunct is a complete boolean expression, and the Pilot proves it before composing.** Wrapping
an element in parentheses is sound only if the element is one expression. `tier = 'sandbox') OR (1=1`
survives wrapping, rebinds the composition and erases the tier bound — valid SQL, identical to the
artefact, past every check. So a conjunct must parse standalone as a boolean expression; must carry
no comment token, no newline and no character of Unicode category Cc or Cf; and must contain **no
parameter reference**, because the composed statement already carries the operator's own `$n`
bindings and `["id = $1"]` is a fence that reads as precise and binds whatever the submitter passes.

The Pilot revalidates each conjunct immediately before composition rather than trusting that an
author did — a hand-authored delegation and an agent's declared scope never pass through EDR-0016's
compiler. The parse is the Pilot's own
([EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md) puts the parser there; it is
not linked in yet), and it runs against the **subset version the artefact was signed against**, never
the version the Pilot happens to ship. A Pilot that cannot evaluate that version refuses. Otherwise a
`pg_query_go` upgrade changes which already-signed conjuncts compose, which is the widening EDR-0039
exists to prevent. Where that pin travels on a fence-bearing artefact is not stated by any record —
[issue #24](https://github.com/sixfathoms/marque/issues/24), due before M2.

**This rule bounds a conjunct's shape, not what it may do.** It closes the composition escape and
nothing else. A conjunct that parses as a boolean may still call a function, cast to a domain whose
`CHECK` calls one, read another relation through a subquery, or name an operator explicitly qualified
past the `search_path` pin — all demonstrated. Reading another relation is already forbidden by
EDR-0007 rule 5, which names no mechanism that enforces it, so the Pilot's revalidation is where that
rule acquires one. The rest are bounded by nothing: EDR-0007 defines a *statement* subset
and no record defines an *expression* subset for a fence, which is
[issue #25](https://github.com/sixfathoms/marque/issues/25) and is due before M5. Saying so here is
better than letting "the Pilot validates each conjunct" read as though the question were settled.

**An absent `fence` is no row restriction. An empty array is refused.** Both would mean
the same thing under conjunction, and one of them reads as a restriction — a reviewer scanning a
grant for a fence sees `"fence": []` and has to know the semantics of the empty conjunction to know
they are looking at an unfenced grant.

**These are Pilot refusals, not authoring conventions.** An empty array, a duplicate conjunct, an
empty-string conjunct and a malformed one are all refused by the Pilot, on the marque payload and on
every artefact, before any comparison runs. Stating them as authoring rules would put them in the
Harbourmaster — the component that authors the artefact, so the rule would be one the control plane
enforces on itself, which is what [EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md) says a
Pilot may not rely on.

Note what this does **not** buy. Refusing `[]` is not a defence against an adversarial author: one
that wanted an unfenced grant would delete the key, which this record says is legitimately no row
restriction, at exactly the same cost. The refusal is for the reader — `"fence": []` looks like a
restriction and is not. What check 7 defends against is the marque carrying a fence the artefact did
not.

### Two conjuncts are equal when their decoded characters are equal

The comparison is over the decoded string — the sequence of Unicode code points the JSON parser
yields — and **not** over the wire bytes. Wire bytes are the wrong unit and would fail correct
marques: Go's `encoding/json` escapes `<`, `>` and `&` by default, so a fence reading `amount < 100`
serialises as `amount \u003c 100` from a Go producer and as `amount < 100` from most others, and
`\/`, `/` and any `\uXXXX` spelling of an ASCII character are all legal JSON for one string. The two
sides of check 7 are independently serialised documents, minted months apart by different producers.

Both documents are strict-decoded **once, after their signatures verify**, and the comparison, the
`display` rendering and the SQL composition all consume that one decoded value. A signature covers
bytes; decoding twice, or composing from a second read of the payload, means the thing verified and
the thing executed are different objects — which is
[EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)'s subject one layer down.

The decode refuses: unknown fields; **duplicate keys**; invalid UTF-8; keys that differ only in case;
`null` where an array is expected; and **unpaired surrogate escapes**. Each of the last three is a
demonstrated bypass rather than a precaution. Go matches field names case-insensitively, so `"Fence"`
is neither unknown nor a duplicate — and `{"fence": [...], "Fence": null}` decodes clean to *no row
restriction* while the document plainly carries a fence. The same bytes, two values, depending on who
reads them: exactly the divergence a signed artefact cannot afford, since the thing a human reviewed
and the thing a Pilot enforces are then different objects. `null` and absent decode alike, so
`"fence": null` does it with one key. And a lone `\ud800` is ASCII on the wire, so a UTF-8 check
passes it and Go substitutes
U+FFFD — making two artefacts that differ compare equal, while a stricter reader elsewhere keeps them
apart. The conformance format already implements all three — the first two with their reasons in
[`testdata/conformance/README.md`](https://github.com/sixfathoms/marque/blob/main/testdata/conformance/README.md),
the surrogate rule in `checkEscapes` in `internal/conformance/vectors.go` — and this record was
weaker than its own cited precedent until it matched them.

Beyond decoding, nothing is normalised: no whitespace folding, no identifier case folding, no
reordering, no SQL normalisation, and no Unicode normalisation — NFC and NFD spellings of one
predicate are **not** equal, which fails closed and is meant to. Every
normalisation rule is a claim that two different texts denote the same predicate; deciding that is
the parser's job, and the parser is what a syntactic rule exists to keep out of this path. An
approximate answer here is approximate in the permissive direction.

So **canonicalisation adds nothing**, and that is the answer to what EDR-0029 check 7 left open.
Three things follow.

- **Check 7 compares the arrays element-wise and in order.** A correctly-minted fast-path marque
  copies the fence from the artefact, so the order survives by construction and requiring it costs
  nothing. What it buys is not having to say what equality means for two collections holding the same
  elements in a different order — which would be one more rule, and removing rules is the point.
- **EDR-0007's attenuation compares conjuncts as a set**, because "must literally carry every
  conjunct of the wider one and may add more" is membership, and membership has no order. The two
  comparisons differ because their jobs differ: one verifies a copy, the other verifies a narrowing.
- **An element is opaque.** `["tier = 'sandbox' AND region = 'eu'"]` and
  `["tier = 'sandbox'", "region = 'eu'"]` are different fences, and nothing splits an element on
  `AND`, because splitting is parsing. An author who wants two conjuncts writes two elements.

**A repeated element is refused.** It changes nothing under conjunction and nothing
under set inclusion, so the only thing a duplicate can do is make an ordered comparison and a set
comparison disagree about the same two fences.

### A relation is two fields

```jsonc
"objects": [ { "schema": "public", "relation": "accounts", "columns": ["settings"] } ]
```

Never `"table": "public.accounts"`. The argument is EDR-0007's own: a relation resolved through
`search_path` is outside the checkable subset, because an unqualified name can bind elsewhere. A
single dotted string checked for a dot accepts `.accounts`, `public.`, and the quoted identifier
`"accounts.archive"` — none of which is a qualified relation, and each of which a split on the dot
turns into a pair. `"accounts.archive"` becomes the schema `"accounts` and the relation `archive"`,
two malformed identifiers that name nothing, arrived at without an error anywhere.

The field is `relation` rather than `table` because that is what the *extracted scope* calls it, and
the extracted scope is the thing a grant is compared against. One name for the two sides of one
comparison is the point; two names is how the mismatch got here.

A wildcard stays a value of the field rather than a different shape: EDR-0037's grant reads
`{ "schema": "public", "relation": "*" }`.

### An operation is lowercase

`"update"`, not `"UPDATE"`. The value names an operation; it is not the SQL keyword it resembles.
Where SQL would read `UPDATE` and `update` as one token, comparing two JSON strings does not — so an
uppercase spelling sitting beside a lowercase one invites a case-folding comparison, which is one
more normalisation on a path this record exists to clear.

### What stays different, deliberately

Four asymmetries are correct, and are written down here so they are not refiled as defects.

- **`operation` singular in a vector, `operations` plural in a grant.** A vector describes one
  statement; a grant covers a set of them.
- **`columns_written` in a vector, `columns` in a grant.** A vector records what a statement *does*
  write; a grant records what *may* be written. The containment proof compares them, and it is a
  subset test rather than an equality — naming both sides the same thing would hide that.
- **EDR-0016's `derivation[].became` stays prose.** It is an account for a human of how one clause of
  a sentence was compiled, not a copy of the compiled field; its first entry already reads
  `UPDATE on public.accounts(settings)`, which is not the JSON encoding of a scope either.
- **`predicate` in a vector, `fence` in a grant — and the vector's is one string.** A vector records
  the statement's own `WHERE` over the target relation; a grant records the bound a fence is built
  from. The two are **composed**, never compared, so the list rule does not reach the vector. They
  also take opposite conventions for the empty case, correctly: an unconditional statement writes
  `"predicate": "TRUE"` rather than omitting it, where an absent `fence` is simply no row
  restriction.

### The records this corrects

Amended in the change that adds this record, each with a dated Changelog line:
[EDR-0007](./0007-delegation-by-containment-proof.md),
[EDR-0008](./0008-standing-orders.md),
[EDR-0016](./0016-natural-language-delegations-are-compiled.md),
[EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md),
[EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md),
[EDR-0037](./0037-emergency-paths.md), and the agents concept page. EDR-0029 gains a pointer to this
record for what canonicalisation means; its rule is unchanged.

No record is superseded. Every decision stands — containment and never entailment, syntactic and
never semantic, the fence as a bound the Pilot applies. What was wrong was the encoding, which is the
same instrument EDR-0007's 2026-08-15 amendment used for the `NOT (fence)` correction.

## Consequences

**Easier.**

- **Check 7 becomes evaluable.** Comparing two lists of strings needs no parser, no schema and no
  catalogue, which is the budget an offline Pilot has.
- **The comparison at every hop is a set operation over strings**, so a parser upgrade cannot change
  what an already-signed delegation permits by changing how its fence *compares*. The conjunct
  revalidation below does parse, and is pinned to the signed subset version for exactly that
  reason — the comparison is what this buys, not the whole verification.
- **The relation and the operation are spelled the same on both sides of the containment proof**, so
  it compares those rather than translating them.

**Harder.**

- **A human signing a compiled delegation reads a list rather than a predicate.**
  `["tier = 'sandbox'"]` is less legible than `tier = 'sandbox'`, and the person signing is the whole
  point of [EDR-0016](./0016-natural-language-delegations-are-compiled.md). The cost lands on them,
  and rendering does not remove it.
- **Two fences differing only in spacing are different fences.** `tier='sandbox'` and
  `tier = 'sandbox'` are not equal, and an operator meets that as a refusal that looks like a bug —
  so the refusal must say the strings differ and show both.
- **The containment proof composes the extracted predicate with the fence rather than comparing
  them.** A vector's `predicate` is one string and a grant's `fence` is a list, deliberately, and
  they meet at M5 where the fence is built. This
  record narrows what has to be translated; it does not remove the seam.
- **Every Pilot now parses on the authority path, not only the statement path.** Revalidating each
  conjunct means a parse per conjunct per verification, and it means a Pilot must be able to evaluate
  the subset version each artefact was signed against — so retaining old subsets is a maintenance
  obligation, and refusing when it cannot is an availability cost taken deliberately.
- **A malformed fence now fails at execution rather than at authoring.** A marque a human read,
  approved and signed can still abort at the Pilot on an empty array, a duplicate conjunct or an
  unparseable one, and the operator meets that after the approval instead of before it. The
  Harbourmaster can run the same checks as an advisory preflight so the common case is caught while
  someone is still typing — but it is advisory, the Pilot repeats them authoritatively, and a check
  run only by the component that authored the artefact is not a check. Late refusal is the residual,
  not the design.
- **Refusing unknown fields makes artefact evolution version-sensitive.** A new optional field is no
  longer free: an older Pilot refuses an artefact carrying it. That is the right direction for a
  signed authority artefact and it is still a cost, paid whenever the shape grows.
- **Authoring by hand gets fussier.** Two fields for a relation and a list for a fence is more to
  type than a dotted string and a predicate.

**New obligations.**

- **M2** extracts scope in this spelling, and its vectors carry it.
- **The fast path** — [EDR-0029](./0029-the-fast-path-authority-chain.md) check 7 — implements the
  comparison as decoded-string equality. It arrives with `delegation`- and `surveyed`-kind marques,
  which [scope](../content/overview/scope.md) puts in **Phase 3** for `delegation` and **Phase 3b**
  for `surveyed` — not with the interactive marques of M3. A standing order is not affected either
  way: check 7 has no fence to compare for one,
  because the order's template is the bound.
- **M5 builds the fence, and is where the per-element parentheses have to be.** Furthest from here
  and the rule likeliest to be lost, because by then a list of strings joined with `AND` will look
  obviously correct. M5's exit criteria carry it, and so does EDR-0007's worked SQL.
- **The signed `display`** ([EDR-0036](./0036-what-is-signed-must-be-what-was-seen.md)) renders a
  fence for the human who signs it. Its rendering rules are canonical and versioned, and they now
  have a list to render rather than a predicate —
  [issue #23](https://github.com/sixfathoms/marque/issues/23), due before M3.
- **An agent has no fast path while the question below is open.** That is the fail-closed reading,
  and stating it is what stops the alternative: an implementer meeting a refusal on a legitimate
  agent marque and relaxing check 7 to clear it. It bounds **fast-path minting only** — an agent's
  declared conjuncts still reach the composed fence on the ordinary path, which is
  [issue #25](https://github.com/sixfathoms/marque/issues/25)'s territory rather than this rule's.
- A future artefact carrying a fence, a relation or an operation uses these names without a further
  decision. One that wants a different spelling needs a record saying why, not a new field.

**The question this record does not settle.**
[EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) computes an agent's effective
scope as the **intersection** of three grants, and for fences an intersection is the *union* of
conjunct sets — more conjuncts is tighter. A marque minted for an agent under its effective fence
would therefore carry conjuncts the delegation it claims authority from does not, and check 7's
identity comparison would refuse it for being tighter. Making the spellings agree is what brings that
into view; resolving it means deciding whether check 7 stays identity, which is EDR-0029's decision
and not this one's. It is [issue #20](https://github.com/sixfathoms/marque/issues/20), and it is due
before Phase 3b, where agents land.

**A hazard this record inherits and does not fix.** EDR-0037 spells a wildcard relation `"*"`, and
PostgreSQL permits `"*"` as a quoted identifier — so a grant over one literal relation is
indistinguishable from a grant over all of them. Renaming the field does not touch that, and an
in-band sentinel wants replacing rather than renaming:
[issue #22](https://github.com/sixfathoms/marque/issues/22), due before the first grant is signed.

## References

- [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) — one schema generates every
  client. This record is that instinct applied to the artefacts a schema does not cover.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the containment proof and the attenuation
  rule this defines a comparison for.
- [EDR-0029](./0029-the-fast-path-authority-chain.md) — check 7, whose "canonicalisation" this
  defines.
- [EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md) — the versioned grammar the
  fence comparison stays clear of, and that the conjunct check must be pinned to.
- [EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md) — the intersected scope the
  open question lives in.
- [`testdata/conformance/README.md`](https://github.com/sixfathoms/marque/blob/main/testdata/conformance/README.md)
  — the format that already had two of these three. It carries no fence.
- [#17](https://github.com/sixfathoms/marque/issues/17) and
  [#16](https://github.com/sixfathoms/marque/issues/16) — the two reports this settles.

## Changelog

- **2026-08-19**: Accepted.
