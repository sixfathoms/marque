package conformance_test

// The real corpus is empty until M2, so these tests exercise the validator
// against corpora written here. That is the point of building the harness at
// M0: the first vector to arrive is checked on arrival, rather than the check
// being written after the corpus has drifted — which is the order that never
// happens.

import (
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
	      "statement": "UPDATE accounts SET tier = 'pro' WHERE id = 42 AND region = 'eu'",
	      "verdict": "in_subset",
	      "scope": {
	        "relation": "accounts",
	        "columns_written": ["tier"],
	        "predicate": "id = 42 AND region = 'eu'"
	      }
	    },
	    {
	      "name": "a function call in the predicate",
	      "statement": "UPDATE accounts SET tier = 'pro' WHERE created_at < now()",
	      "verdict": "out_of_grammar",
	      "because": "a function call cannot be evaluated at review time"
	    },
	    {
	      "name": "an engine that cannot assert the write set",
	      "statement": "DELETE FROM settings WHERE id = 1",
	      "verdict": "unsupported",
	      "because": "this engine has no RETURNING, so the post-assert cannot run"
	    }
	  ]
	}`)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if corpus.SubsetVersion != 3 {
		t.Errorf("SubsetVersion = %d, want 3", corpus.SubsetVersion)
	}
	if len(corpus.Vectors) != 3 {
		t.Fatalf("loaded %d vectors, want 3", len(corpus.Vectors))
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
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"probably","because":"b"}]}`,
			want: "want one of",
		},
		{
			name: "an admitted statement with no scope",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"in_subset"}]}`,
			want: "requires a scope",
		},
		{
			name: "a refused statement with no reason",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar"}]}`,
			want: "requires `because`",
		},
		{
			name: "a refused statement carrying a scope",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"unsupported","because":"b",
			        "scope":{"relation":"accounts","columns_written":["tier"]}}]}`,
			want: "must not carry a scope",
		},
		{
			name: "an admitted statement carrying a reason",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"in_subset","because":"b",
			        "scope":{"relation":"accounts","columns_written":["tier"]}}]}`,
			want: "must not carry `because`",
		},
		{
			name: "a scope with no relation",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"columns_written":["tier"]}}]}`,
			want: "scope.relation is required",
		},
		{
			name: "a scope that writes nothing",
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"in_subset",
			        "scope":{"relation":"accounts","columns_written":[]}}]}`,
			want: "columns_written is required",
		},
		{
			name: "a nameless vector",
			body: `{"vectors":[{"name":"  ","statement":"s","verdict":"out_of_grammar","because":"b"}]}`,
			want: "name is required",
		},
		{
			name: "a vector with no statement",
			body: `{"vectors":[{"name":"n","statement":"","verdict":"out_of_grammar","because":"b"}]}`,
			want: "statement is required",
		},
		{
			name: "two vectors sharing a name",
			body: `{"vectors":[
			        {"name":"n","statement":"a","verdict":"out_of_grammar","because":"b"},
			        {"name":"n","statement":"c","verdict":"out_of_grammar","because":"d"}]}`,
			want: "is used twice",
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
			body: `{"vectors":[{"name":"n","statement":"s","verdict":"out_of_grammar",
			        "because":"b","expected":"in_subset"}]}`,
			want: "unknown field",
		},
		{
			name: "not JSON at all",
			body: `UPDATE accounts SET tier = 'pro'`,
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
