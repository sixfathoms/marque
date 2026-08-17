package conformance_test

// The real corpus is empty until M2, so these tests exercise the validator
// against corpora written here. That is the point of building the harness at
// M0: the first vector to arrive is checked on arrival, rather than the check
// being written after the corpus has drifted — which is the order that never
// happens.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixfathoms/marque/internal/conformance"
)

const corpusPath = "../../testdata/conformance/statements.json"

func load(t *testing.T, body string) (*conformance.Corpus, error) {
	t.Helper()
	return conformance.Load(strings.NewReader(body))
}

func TestLoadAcceptsAWellFormedCorpus(t *testing.T) {
	corpus, err := load(t, `{
	  "subset_version": 3,
	  "vectors": [
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
	    },
	    {
	      "name": "an insert, which names its columns and has no WHERE",
	      "statement": "INSERT INTO public.settings (account_id, tier) VALUES (42, 'pro')",
	      "verdict": "in_subset",
	      "scope": {
	        "operation": "insert",
	        "schema": "public",
	        "relation": "settings",
	        "columns_written": ["account_id", "tier"]
	      }
	    },
	    {
	      "name": "a select, which reads and assigns nothing",
	      "statement": "SELECT tier FROM public.accounts WHERE id = 42",
	      "verdict": "in_subset",
	      "scope": {
	        "operation": "select",
	        "schema": "public",
	        "relation": "accounts",
	        "predicate": "id = 42"
	      }
	    },
	    {
	      "name": "single-relation delete, which assigns to no column",
	      "statement": "DELETE FROM public.settings WHERE account_id = 42",
	      "verdict": "in_subset",
	      "scope": {
	        "operation": "delete",
	        "schema": "public",
	        "relation": "settings",
	        "predicate": "account_id = 42"
	      }
	    },
	    {
	      "name": "a function call in the predicate",
	      "statement": "UPDATE public.accounts SET tier = 'pro' WHERE created_at < now()",
	      "verdict": "out_of_grammar",
	      "because": "a function call cannot be evaluated at review time"
	    },
	    {
	      "name": "DDL, which Marque does not broker at all",
	      "statement": "ALTER TABLE public.accounts ADD COLUMN nickname text",
	      "verdict": "unsupported",
	      "because": "a statement that cannot be rolled back cannot be fenced or rehearsed"
	    }
	  ]
	}`)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if corpus.SubsetVersion != 3 {
		t.Errorf("SubsetVersion = %d, want 3", corpus.SubsetVersion)
	}
	if len(corpus.Vectors) != 6 {
		t.Fatalf("loaded %d vectors, want 6", len(corpus.Vectors))
	}
	if got := corpus.Vectors[0].Scope.ColumnsWritten; len(got) != 1 || got[0] != "tier" {
		t.Errorf("columns_written = %v, want [tier]", got)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{
			name: "a verdict outside the closed set",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"probably","because":"b"}]}`,
			want: "want one of",
		},
		{
			name: "an admitted statement with no scope",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset"}]}`,
			want: "requires a scope",
		},
		{
			name: "an unadmitted statement with no reason",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar"}]}`,
			want: "requires `because`",
		},
		{
			name: "an unadmitted statement carrying a scope",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"unsupported","because":"b",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "must not carry a scope",
		},
		{
			name: "an admitted statement carrying a reason",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset","because":"b",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "must not carry `because`",
		},
		{
			name: "a scope with no relation",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.relation is required",
		},
		{
			name: "a column that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","relation":"accounts",
			        "columns_written":["  "],"predicate":"id = 1"}}]}`,
			want: "columns_written[0] is empty",
		},
		{
			name: "a statement that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"   ","verdict":"out_of_grammar","because":"b"}]}`,
			want: "statement is required",
		},
		{
			name: "a reason that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar","because":"  "}]}`,
			want: "requires `because`",
		},
		{
			name: "a relation that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","relation":"  ","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.relation is required",
		},
		{
			name: "a predicate that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":["tier"],"predicate":" "}}]}`,
			want: "scope.predicate is required",
		},
		{
			name: "an unqualified relation, which search_path could rebind",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","relation":"accounts","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.schema is required",
		},
		{
			name: "a scope that writes nothing",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":[],"predicate":"id = 1"}}]}`,
			want: "assigns columns, so columns_written is required",
		},
		{
			name: "a nameless vector",
			body: `{"subset_version":0,"vectors":[{"name":"  ","statement":"s","verdict":"out_of_grammar","because":"b"}]}`,
			want: "name is required",
		},
		{
			name: "a vector with no statement",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"","verdict":"out_of_grammar","because":"b"}]}`,
			want: "statement is required",
		},
		{
			name: "two vectors sharing a name",
			body: `{"subset_version":0,"vectors":[
			        {"name":"n","statement":"a","verdict":"out_of_grammar","because":"b"},
			        {"name":"n","statement":"c","verdict":"out_of_grammar","because":"d"}]}`,
			want: "is used twice",
		},
		{
			// The level a key belongs at is enforced by the struct decode; the
			// flat key set cannot see it, and a regression once let this pass.
			name: "a scope key on a vector",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s",
			        "verdict":"out_of_grammar","because":"b","predicate":"id = 1"}]}`,
			want: "unknown field",
		},
		{
			name: "a corpus key on a vector",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s",
			        "verdict":"out_of_grammar","because":"b","subset_version":1}]}`,
			want: "unknown field",
		},
		{
			name: "an operation outside the closed set",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"merge","schema":"public","relation":"accounts",
			        "columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.operation is",
		},
		{
			// An insert has no WHERE at all, which is different from an
			// unconditional statement whose predicate is TRUE.
			name: "an insert carrying a predicate",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"insert","schema":"public","relation":"accounts",
			        "columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "has no WHERE",
		},
		{
			name: "an insert assigning no columns",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"insert","schema":"public","relation":"accounts"}}]}`,
			want: "columns_written is required",
		},
		{
			name: "a select carrying columns_written",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"select","schema":"public","relation":"accounts",
			        "columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "assigns to no column",
		},
		{
			// The corpus decode must reject a key that belongs to a vector,
			// which the flat key set cannot see.
			name: "a vector key at the corpus level",
			body: `{"subset_version":0,"vectors":[],"verdict":"in_subset"}`,
			want: "unknown field",
		},
		{
			name: "a whole vector written outside the vectors array",
			body: `{"subset_version":0,"vectors":[],"name":"n","statement":"s","verdict":"in_subset"}`,
			want: "unknown field",
		},
		{
			name: "a schema that is only whitespace",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"  ","relation":"accounts",
			        "columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.schema is required",
		},
		{
			name: "a duplicate key, which two parsers read differently",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s",
			        "verdict":"out_of_grammar","verdict":"in_subset","because":"b"}]}`,
			want: "duplicate key",
		},
		{
			name: "a field name in the wrong case, which Go alone would fold",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","Verdict":"out_of_grammar","because":"b"}]}`,
			want: "unknown field",
		},
		{
			name: "an admitted vector carrying an empty because",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset","because":"",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "must not carry `because`",
		},
		{
			name: "an unadmitted vector carrying a null scope",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar","because":"b","scope":null}]}`,
			want: "must not carry a scope",
		},
		{
			name: "a delete carrying an empty columns_written",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"delete","schema":"public","relation":"settings",
			        "columns_written":[],"predicate":"id = 1"}}]}`,
			want: "must be absent",
		},
		{
			name: "a delete carrying columns_written",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"delete","schema":"public","relation":"settings","columns_written":["tier"],
			        "predicate":"id = 1"}}]}`,
			want: "assigns to no column",
		},
		{
			name: "a scope with no operation",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"schema":"public","relation":"accounts","columns_written":["tier"],"predicate":"id = 1"}}]}`,
			want: "scope.operation is",
		},
		{
			// The predicate is what the fence is built from, and the initial
			// subset admits nothing without one.
			name: "an admitted scope with no predicate",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"operation":"update","schema":"public","relation":"accounts","columns_written":["tier"]}}]}`,
			want: "scope.predicate is required",
		},
		{
			// Deleting the file's contents must not read as a valid empty corpus.
			name: "a null document",
			body: `null`,
			want: "subset_version is absent",
		},
		{
			name: "an empty object",
			body: `{}`,
			want: "subset_version is absent",
		},
		{
			name: "a corpus with no vectors key",
			body: `{"subset_version":0}`,
			want: "vectors is absent",
		},
		{
			name: "a file concatenated with a second document",
			body: `{"subset_version":0,"vectors":[]}{"subset_version":9,"vectors":[]}`,
			want: "followed by more JSON",
		},
		{
			name: "a negative subset version",
			body: `{"subset_version":-1,"vectors":[]}`,
			want: "cannot be negative",
		},
		{
			// A field nobody reads is a constraint its author believed they
			// had written.
			name: "a field the format does not define",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar",
			        "because":"b","expected":"in_subset"}]}`,
			want: "unknown field",
		},
		{
			name: "invalid UTF-8, which Go would quietly repair",
			body: "{\"subset_version\":0,\"vectors\":[{\"name\":\"\xff\",\"statement\":\"s\"," +
				"\"verdict\":\"out_of_grammar\",\"because\":\"b\"}]}",
			want: "not valid UTF-8",
		},
		{
			name: "an unpaired surrogate escape, which readers disagree about",
			body: `{"subset_version":0,"vectors":[{"name":"a\ud800b","statement":"s",
			        "verdict":"out_of_grammar","because":"b"}]}`,
			want: "unpaired surrogate",
		},
		{
			name: "a trailing surrogate escape with no leading one",
			body: `{"subset_version":0,"vectors":[{"name":"a\udc00b","statement":"s",
			        "verdict":"out_of_grammar","because":"b"}]}`,
			want: "trailing surrogate",
		},
		{
			name: "not JSON at all",
			body: `UPDATE public.accounts SET tier = 'pro'`,
			want: "parsing the corpus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(t, tt.body)
			if err == nil {
				t.Fatalf("Load() = nil error, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// The committed corpus must parse and validate. It is empty today — the
// vectors and the grammar that runs them are M2 — but an empty corpus and an
// unreadable one must not look the same, so this reports what it found.
func TestCommittedCorpusIsValid(t *testing.T) {
	file, err := os.Open(corpusPath)
	if err != nil {
		t.Fatalf("opening the corpus: %v", err)
	}
	defer file.Close() //nolint:errcheck // read-only

	corpus, err := conformance.Load(file)
	if err != nil {
		t.Fatalf("the committed corpus does not validate: %v", err)
	}
	t.Logf("%s: subset version %d, %d vector(s)",
		filepath.Base(corpusPath), corpus.SubsetVersion, len(corpus.Vectors))
}

// A corpus that loads must survive being written back out and read again.
// With no vectors the slice was nil, so marshalling produced `"vectors": null`
// — which this same loader rejects as an absent field. The committed corpus is
// exactly that case, so the format disagreed with itself about the one file it
// actually ships.
func TestLoadedCorpusRoundTrips(t *testing.T) {
	file, err := os.Open(corpusPath)
	if err != nil {
		t.Fatalf("opening the corpus: %v", err)
	}
	defer file.Close() //nolint:errcheck // read-only

	first, err := conformance.Load(file)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshalling the corpus: %v", err)
	}
	second, err := conformance.Load(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("re-loading what Load produced: %v\n%s", err, encoded)
	}

	if second.SubsetVersion != first.SubsetVersion || len(second.Vectors) != len(first.Vectors) {
		t.Errorf("round trip changed the corpus: %+v then %+v", first, second)
	}
}

// Escapes a corpus is entitled to contain, which an earlier scan rejected: a
// non-BMP character is a *pair* of escapes, and a literal backslash before a
// "u" is not an escape at all — the second is what a vector exercising
// PostgreSQL escape-string behaviour would carry.
func TestLoadAcceptsLegitimateEscapes(t *testing.T) {
	tests := []struct{ name, body string }{
		{
			name: "a non-BMP character, written as a surrogate pair",
			body: `{"subset_version":0,"vectors":[{"name":"emoji \ud83d\ude00","statement":"s",
			        "verdict":"out_of_grammar","because":"b"}]}`,
		},
		{
			// A literal backslash followed by hex digits. Reading one byte too
			// far here finds "d800" and calls it a lone surrogate, so this is
			// what pins the escape walk to consuming `\\` whole.
			name: "an escaped backslash followed by hex digits",
			body: `{"subset_version":0,"vectors":[{"name":"n","statement":"SELECT E'\\d800'",
			        "verdict":"out_of_grammar","because":"b"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := conformance.Load(strings.NewReader(tt.body)); err != nil {
				t.Errorf("Load() error = %v, want nil", err)
			}
		})
	}
}

// The round trip must hold for a corpus with vectors in it, not only the empty
// committed one. An insert carries no predicate, and a marshalled empty string
// would be a key the loader rejects — which the empty-corpus test cannot see.
func TestPopulatedCorpusRoundTrips(t *testing.T) {
	const body = `{
	  "subset_version": 1,
	  "vectors": [
	    {"name":"u","statement":"s","verdict":"in_subset",
	     "scope":{"operation":"update","schema":"public","relation":"accounts",
	              "columns_written":["tier"],"predicate":"id = 1"}},
	    {"name":"i","statement":"s","verdict":"in_subset",
	     "scope":{"operation":"insert","schema":"public","relation":"settings",
	              "columns_written":["tier"]}},
	    {"name":"d","statement":"s","verdict":"in_subset",
	     "scope":{"operation":"delete","schema":"public","relation":"settings","predicate":"id = 1"}},
	    {"name":"s","statement":"s","verdict":"in_subset",
	     "scope":{"operation":"select","schema":"public","relation":"accounts","predicate":"id = 1"}},
	    {"name":"o","statement":"s","verdict":"out_of_grammar","because":"b"}
	  ]
	}`

	first, err := conformance.Load(strings.NewReader(body))
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	second, err := conformance.Load(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("re-loading what Load produced: %v\n%s", err, encoded)
	}
	if len(second.Vectors) != len(first.Vectors) {
		t.Errorf("round trip changed the vector count: %d then %d", len(first.Vectors), len(second.Vectors))
	}
}

// TestScopeShapeMatrix pins the whole format, exhaustively: for each operation,
// exactly one combination of the two optional scope fields is legal.
//
// The rules come from EDR-0007 — "assigned columns (SET, INSERT column list)"
// is what `columns_written` records, so a select and a delete assign none — and
// from SQL: an insert has no WHERE, while an unconditional statement has the
// predicate TRUE. Three review rounds each found a shape the format could not
// express, so the shape is stated here in full rather than inferred from the
// rules that produce it.
func TestScopeShapeMatrix(t *testing.T) {
	// The one legal combination per operation: {columns, predicate}.
	legal := map[conformance.Operation][2]bool{
		conformance.Select: {false, true},
		conformance.Insert: {true, false},
		conformance.Update: {true, true},
		conformance.Delete: {false, true},
	}

	for _, op := range []conformance.Operation{
		conformance.Select, conformance.Insert, conformance.Update, conformance.Delete,
	} {
		for _, columns := range []bool{false, true} {
			for _, predicate := range []bool{false, true} {
				name := fmt.Sprintf("%s/columns=%v/predicate=%v", op, columns, predicate)
				t.Run(name, func(t *testing.T) {
					parts := []string{
						fmt.Sprintf(`"operation":%q`, op),
						`"schema":"public"`,
						`"relation":"accounts"`,
					}
					if columns {
						parts = append(parts, `"columns_written":["tier"]`)
					}
					if predicate {
						parts = append(parts, `"predicate":"id = 1"`)
					}
					body := `{"subset_version":0,"vectors":[{"name":"n","statement":"s",` +
						`"verdict":"in_subset","scope":{` + strings.Join(parts, ",") + `}}]}`

					_, err := conformance.Load(strings.NewReader(body))
					want := legal[op] == [2]bool{columns, predicate}
					switch {
					case want && err != nil:
						t.Errorf("Load() error = %v, want nil — this is the shape %s takes", err, op)
					case !want && err == nil:
						t.Errorf("Load() = nil error, want one: %s does not take this shape", op)
					}
				})
			}
		}
	}
}
