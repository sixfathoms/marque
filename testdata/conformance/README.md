# Conformance vectors

What the grammar must decide about a statement, written as data rather than as prose.

`statements.json` is **normative**. A change to what the checkable subset admits is a change to these
vectors in the same commit — not a change to a document that describes them, and not a change to a
Go test that happens to encode the same thing in a different shape.

## Why data and not tests

The subset is the foundation the authority model stands on
([EDR-0039](../../docs/edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)): a statement is
`in_subset` only if the grammar can prove what it touches, and everything else escalates. Two things
follow.

**Widening it must be visible.** A newer `pg_query_go` parses statements the previous one refused,
which silently widens what an already-signed delegation permits. The corpus is what makes that show
up as a diff a reviewer reads, rather than as a behaviour nobody noticed — which is why
[CLAUDE.md](../../CLAUDE.md) calls a parser upgrade a reviewed change on the order of a schema
migration.

**A corpus of only happy paths tests nothing.** The cases the grammar must *not admit* are the ones worth
writing: a function call in a predicate, a CTE, a second relation arriving through `FROM`, a
subquery. Each of those is a way a statement can do more than it appears to.

## Status

**The corpus is empty**, deliberately. The harness, the format and the validator are M0; the vectors
and the grammar that runs them are M2. `internal/conformance` loads and validates this file today, so
an empty corpus and an unreadable one are not the same outcome: a truncated, `null` or `{}` file
fails the build, and a missing one fails to open. The vector count is logged under `go test -v`.

## Format

```json
{
  "subset_version": 0,
  "vectors": []
}
```

Relations are **schema-qualified**, as two fields rather than one dotted string.
[EDR-0007](../../docs/edrs/0007-delegation-by-containment-proof.md) places a relation resolved
through `search_path` outside the subset, because an unqualified name can bind elsewhere — which is
the escape the pinned `search_path` closes. Separate fields mean there is nothing to parse: a single
string checked for a dot would accept `.accounts`, `public.`, and the quoted identifier
`"accounts.archive"`, none of which is a qualified relation.

That spelling is now [EDR-0041](../../docs/edrs/0041-one-spelling-for-a-scope.md)'s, and it is what
every grant uses too. A vector's extracted scope and the delegation it is proved against are the two
sides of one comparison, so they are named the same thing. What stays different is deliberate: a
vector's `operation` is singular because it describes one statement, and its `columns_written` says
what a statement does write where a grant's `columns` says what may be written.

**A key is legal only where the format puts it.** `predicate` belongs to a scope and
`subset_version` to the corpus; neither is a vector's, and the strict struct decode is what enforces
that. It is a separate guard from the one below, and losing either loses half the check.

**Field names are matched exactly, and a duplicate key is rejected.** Go's JSON decoder folds case
and takes the last of two identical keys; a strict parser elsewhere would reject the file or read a
different value from the same bytes. For a file that is normative *and* language-neutral, the same
bytes must mean the same thing to every reader.

**A forbidden field is rejected for being present, not for being empty.** `"because": ""` on an
admitted vector, `"scope": null` on an unadmitted one, and `"columns_written": []` on a delete are all
shapes the format does not have.

`predicate` is the statement's own predicate over the **target** relation. An unconditional
statement records `"predicate": "TRUE"` rather than omitting the field; an `insert` omits it, which
is not the same thing — an `INSERT … SELECT … WHERE` does contain a `WHERE`, but it filters the
source relation, and recording it would give the fence a predicate over the wrong table. EDR-0007's
subset rules do not require a `WHERE`, so a required-but-omittable predicate would put a statement
the records admit outside the corpus; TRUE keeps the field meaningful and says what the fence is
built from. The implementation plan narrows the *initial* M2 subset further than the records do,
which is a matter for which vectors exist rather than for the format.

`subset_version` is the version of the checkable subset these vectors describe. It is bounded at the
largest integer every JSON reader represents exactly (2^53 − 1) — past that, a reader without 64-bit
integers rounds the value and the same file names a different subset. It is recorded on
every extracted scope, so a delegation signed against one subset stays pinned to it when the subset
later widens.

A vector the grammar must **admit** carries the scope it must extract:

```json
{
  "name": "single-relation update with a conjunctive predicate over literals",
  "statement": "UPDATE public.accounts SET tier = 'pro' WHERE id = 42 AND region = 'eu'",
  "verdict": "in_subset",
  "scope": {
    "operation": "update",
    "schema": "public",
    "relation": "accounts",
    "columns_written": ["tier"],
    "predicate": "id = 42 AND region = 'eu'"
  }
}
```

A `delete` assigns to no column, so its scope carries no `columns_written`. What it removes —
including anything the engine removes on its behalf — is asserted at execution by the write-set
check ([EDR-0033](../../docs/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)),
not stated here:

```json
{
  "name": "single-relation delete with a predicate over literals",
  "statement": "DELETE FROM public.settings WHERE account_id = 42",
  "verdict": "in_subset",
  "scope": {
    "operation": "delete",
    "schema": "public",
    "relation": "settings",
    "predicate": "account_id = 42"
  }
}
```

A vector the grammar must **not admit** carries the reason, which is the message a reader is shown.
Whether that ends in an escalation or a refusal is the verdict's business, not the reason's:

```json
{
  "name": "a function call in the predicate cannot be evaluated at review time",
  "statement": "UPDATE public.accounts SET tier = 'pro' WHERE created_at < now()",
  "verdict": "out_of_grammar",
  "because": "a function call cannot be evaluated at review time"
}
```

The three verdicts are [EDR-0039](../../docs/edrs/0039-the-grammar-is-parsed-by-postgresqls-own-parser.md)'s,
quoted rather than restated — an earlier version of this file put them in its own words and had
`unsupported` mean "the engine cannot enforce a control", which is not what the record says.

| Verdict | Meaning | What happens | Carries |
|---|---|---|---|
| `in_subset` | Provable shape; scope and predicate extracted | May match a delegation or standing order | `scope` |
| `out_of_grammar` | Valid SQL the subset cannot prove anything about | **Escalates to a human** | `because` |
| `unsupported` | Marque will not broker it at all (DDL, implicit commit, multi-statement) | Refused with the reason | `because` |

The middle row is the one to keep straight. `out_of_grammar` is not a refusal — it is the ordinary
path to a person. [EDR-0022](../../docs/edrs/0022-local-proxy-brokers-every-statement.md) is explicit
that an operator who cannot tell "ask someone" from "never going to work" will assume the latter and
route around the system.

There is no fourth verdict, and there is no verdict for SQL that does not parse. `out_of_grammar` is
*valid* SQL the subset cannot reason about, so a statement PostgreSQL itself rejects has no place
here; the differential harness of M2 compares what the parser and a real server accept, and its
inputs are a separate concern from this corpus.

Examples here use the neutral fictional schema this repository uses throughout — `accounts`,
`settings`, `tier`. No organisation's schema, table or column names belong in a vector.
