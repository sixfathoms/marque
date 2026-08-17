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

**A corpus of only happy paths tests nothing.** The cases that must be *refused* are the ones worth
writing: a function call in a predicate, a CTE, a second relation arriving through `FROM`, a
subquery. Each of those is a way a statement can do more than it appears to.

## Status

**The corpus is empty**, deliberately. The harness, the format and the validator are M0; the vectors
and the grammar that runs them are M2. `internal/conformance` loads and validates this file today and
reports how many vectors it found, so an empty corpus is a stated fact rather than a silent one.

## Format

```json
{
  "subset_version": 0,
  "vectors": []
}
```

`subset_version` is the version of the checkable subset these vectors describe. It is recorded on
every extracted scope, so a delegation signed against one subset stays pinned to it when the subset
later widens.

A vector the grammar must **admit** carries the scope it must extract:

```json
{
  "name": "single-relation update with a conjunctive predicate over literals",
  "statement": "UPDATE accounts SET tier = 'pro' WHERE id = 42 AND region = 'eu'",
  "verdict": "in_subset",
  "scope": {
    "operation": "update",
    "relation": "accounts",
    "columns_written": ["tier"],
    "predicate": "id = 42 AND region = 'eu'"
  }
}
```

A `delete` assigns to no column, so its scope carries no `columns_written` — the rows it removes are
the write set the fence asserts over ([EDR-0033](../../docs/edrs/0033-assert-the-whole-write-set-not-just-the-named-relation.md)),
and they are measured at execution rather than stated here:

```json
{
  "name": "single-relation delete with a predicate over literals",
  "statement": "DELETE FROM settings WHERE account_id = 42",
  "verdict": "in_subset",
  "scope": {
    "operation": "delete",
    "relation": "settings",
    "predicate": "account_id = 42"
  }
}
```

A vector it must **refuse** carries the reason, which is the error message a reader will be shown:

```json
{
  "name": "a function call in the predicate cannot be evaluated at review time",
  "statement": "UPDATE accounts SET tier = 'pro' WHERE created_at < now()",
  "verdict": "out_of_grammar",
  "because": "a function call cannot be evaluated at review time"
}
```

| Verdict | Meaning |
|---|---|
| `in_subset` | The grammar proves what this touches. Requires `scope`. |
| `out_of_grammar` | The subset cannot express this, and may never. Requires `because`. |
| `unsupported` | The engine cannot enforce a control this needs. Requires `because`. |

Examples here use the neutral fictional schema this repository uses throughout — `accounts`,
`settings`, `tier`. No organisation's schema, table or column names belong in a vector.
