---
id: 41
title: "Spell a scope the same way in every artefact"
summary: "A fence is an array of conjuncts in every artefact that carries one, a relation is a schema field and a relation field, and an operation is lowercase. Conjuncts compare byte-for-byte, so canonicalisation normalises nothing."
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
- Two conjuncts are equal when their **bytes** are equal. That is what "canonicalisation" means in
  [EDR-0029](./0029-the-fast-path-authority-chain.md) check 7: nothing is normalised.
- A **relation** is a `schema` field and a `relation` field. Never one dotted string.
- An **operation** is lowercase.

Whoever implements the containment proof at M2, or check 7 at M3, implements these. The records
carrying the losing spelling are corrected by the same change that adds this one.

## Context

`fence` was an array in the artefacts the Pilot verifies and a string in the grants authority
descends from:

| Spelling | Where |
|---|---|
| `"fence": ["tier = 'sandbox'"]` | the marque payload — [EDR-0004](./0004-marques-are-signed-leases.md), [EDR-0029](./0029-the-fast-path-authority-chain.md) |
| `"fence": "tier = 'sandbox'"` | the delegation ([EDR-0007](./0007-delegation-by-containment-proof.md)), the compiled delegation ([EDR-0016](./0016-natural-language-delegations-are-compiled.md)), an agent's declared scope ([EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md)) |

A relation had three spellings, not two: `"table": "public.accounts"` in most grants,
`{"schema": "public", "table": "*"}` in [EDR-0037](./0037-emergency-paths.md), and
`{"schema": "public", "relation": "accounts"}` in the conformance corpus. Operations were uppercase
in every record and lowercase in the corpus.

Two checks are specified across the split.

**EDR-0029 check 7** states the fast-path rule as *"syntactic identity after canonicalisation:
`marque.fence == artefact.fence`"*. On that path no human is present at mint time. As written the
equality has an array on one side and a string on the other, and the whole reconciliation hides
inside a word no record defines.

**EDR-0007's attenuation rule** requires *"syntactic conjunct-set inclusion, never by entailment"*
and says "the `fence` array is conjunctive" — eleven lines after its own worked example shows a
string. If the authored form is a string, recovering its conjuncts means parsing SQL, which puts
`libpg_query` ([EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)) inside the
offline Pilot's authority path, an architectural consequence no record states.

The failure direction is the bad one. An implementer meeting an undefined comparison between a
one-element array and a string will most likely make it succeed, and a fence comparison that resolves
permissively is exactly the error both records say they exist to avoid.

This is the discipline of [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) applied
where the schema does not reach. `proto/` generates one client per language and cannot disagree with
itself; these artefacts are hand-authored in records, and hand-authored is precisely what drifts.

## Decision

### A fence is an array of conjuncts

Everywhere: the marque payload, the delegation, the compiled delegation, the break-glass grant's
scope, an agent's declared scope. The fence is the conjunction of its elements.

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
`((c1) AND (c2)) IS NOT TRUE`.

**An absent `fence` is no row restriction. An empty array is refused at authoring.** Both would mean
the same thing under conjunction, and one of them reads as a restriction — a reviewer scanning a
grant for a fence sees `"fence": []` and has to know the semantics of the empty conjunction to know
they are looking at an unfenced grant.

### Two conjuncts are equal when their bytes are equal

No whitespace folding, no identifier case folding, no reordering, no SQL normalisation. Every
normalisation rule is a claim that two different texts denote the same predicate; deciding that is
the parser's job, and the parser is what a syntactic rule exists to keep out of this path. An
approximate answer here is approximate in the permissive direction.

So **canonicalisation adds nothing**, and that is the answer to what EDR-0029 check 7 left open.
Three things follow.

- **Check 7 compares the arrays element-wise and in order.** A correctly-minted fast-path marque
  copies the fence from the artefact, so the order survives by construction and requiring it costs
  nothing. What it buys is not having to say what equality means for two collections holding the same
  elements in a different order — which is one more rule, and rules are what this is removing.
- **EDR-0007's attenuation compares conjuncts as a set**, because "must literally carry every
  conjunct of the wider one and may add more" is membership, and membership has no order. The two
  comparisons differ because their jobs differ: one verifies a copy, the other verifies a narrowing.
- **An element is opaque.** `["tier = 'sandbox' AND region = 'eu'"]` and
  `["tier = 'sandbox'", "region = 'eu'"]` are different fences, and nothing splits an element on
  `AND`, because splitting is parsing. An author who wants two conjuncts writes two elements.

**A repeated element is refused at authoring.** It changes nothing under conjunction and nothing
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
more normalisation on the path this record is clearing them off.

### What stays different, deliberately

Three asymmetries are correct, and are written down here so they are not refiled as defects.

- **`operation` singular in a vector, `operations` plural in a grant.** A vector describes one
  statement; a grant covers a set of them.
- **`columns_written` in a vector, `columns` in a grant.** A vector records what a statement *does*
  write; a grant records what *may* be written. The containment proof compares them, and it is a
  subset test rather than an equality — naming both sides the same thing would hide that.
- **EDR-0016's `derivation[].became` stays prose.** It is an account for a human of how one clause of
  a sentence was compiled, not a copy of the compiled field; its first entry already reads
  `UPDATE on public.accounts(settings)`, which is not JSON either.

### The records this corrects

Amended in the change that adds this record, each with a dated Changelog line:
[EDR-0007](./0007-delegation-by-containment-proof.md),
[EDR-0008](./0008-standing-orders.md),
[EDR-0016](./0016-natural-language-delegations-are-compiled.md),
[EDR-0018](./0018-agents-are-submitters-under-intersected-scope.md),
[EDR-0037](./0037-emergency-paths.md), and the agents concept page. EDR-0029 gains a pointer to this
record for what canonicalisation means; its rule is unchanged.

No record is superseded. Every decision stands — containment and never entailment, syntactic and
never semantic, the fence as a bound the Pilot applies. What was wrong was the encoding, which is the
same instrument EDR-0007's 2026-08-15 amendment used for the `NOT (fence)` correction.

## Consequences

**Easier.** Check 7 becomes evaluable: byte equality over two arrays of strings needs no parser, no
schema and no catalogue, which is the budget an offline Pilot has. EDR-0007's attenuation becomes a
set operation over strings, so a delegation chain verifies at every hop without SQL parsing —
`libpg_query` stays where EDR-0039 puts it, on the statement, and out of the authority comparison.
And the extracted scope and the grant it is checked against are finally spelled the same, so M2's
containment proof compares like with like instead of translating.

**Harder.** Three real costs.

- **A human signing a compiled delegation reads a list rather than a predicate.**
  `["tier = 'sandbox'"]` is less legible than `tier = 'sandbox'`, and the person signing is the whole
  point of EDR-0016. The console and the CLI render the conjunction for display; what is signed is
  the array. That is the right way round, and it is still a legibility cost rather than a free win.
- **Byte identity refuses fences that differ only in spacing.** `tier='sandbox'` and
  `tier = 'sandbox'` are different conjuncts. That is the intended direction — the alternative is a
  normalisation rule, and every normalisation rule is a claim about SQL — but an operator meets it as
  a refusal that looks like a bug, so the message must say the bytes differ and show both.
- **Authoring by hand gets fussier.** Two fields for a relation and an array for a fence is more to
  type than a dotted string and a predicate.

**An open question this record does not settle.** EDR-0018 computes an agent's effective scope as the
**intersection** of three grants, and for fences an intersection is the *union* of conjunct sets —
more conjuncts is tighter. A marque minted for an agent under its effective fence would therefore
carry conjuncts the delegation it claims authority from does not, and check 7's identity comparison
would refuse it. Making the spellings agree is what brings that into view; resolving it means
deciding whether check 7 stays identity, which is EDR-0029's decision and not this one's. It is
[issue #20](https://github.com/sixfathoms/marque/issues/20), and it is due before M3 mints a marque
for an agent.

**Obligations.** M2 writes the vectors and the containment proof, and both read this spelling. M3
implements check 7, as byte equality. M5 builds the fence, and is where the per-element parentheses
have to be — furthest from here, and the rule likeliest to be lost, because by then an array of
strings joined with `AND` will look obviously correct. A future artefact that carries a fence, a
relation or an operation uses these names without a further decision — and one that wants a different
spelling needs a record saying why, not a new field.

## References

- [ZFN-14](https://zrz.io/zfn/14-schema-first-apis-generate-clients/) — one schema generates every
  client. This record is that instinct applied to the artefacts a schema does not cover.
- [EDR-0007](./0007-delegation-by-containment-proof.md) — the containment proof and the attenuation
  rule this defines a comparison for.
- [EDR-0029](./0029-the-fast-path-authority-chain.md) — check 7, whose "canonicalisation" this
  defines.
- [EDR-0039](./0039-the-grammar-is-parsed-by-postgresqls-own-parser.md) — the parser this keeps out
  of the authority path.
- [`testdata/conformance/README.md`](https://github.com/sixfathoms/marque/blob/main/testdata/conformance/README.md)
  — the format that already had the winning spelling.
- [#17](https://github.com/sixfathoms/marque/issues/17) and
  [#16](https://github.com/sixfathoms/marque/issues/16) — the two reports this settles.

## Changelog

- **2026-08-19**: Accepted.
