// Package conformance loads the normative vectors that say what the grammar
// must decide about a statement.
//
// The vectors live in testdata/conformance/statements.json and are data rather
// than Go tests on purpose (EDR-0039). The subset the grammar admits is the
// foundation of the authority model, and widening it silently widens what an
// already-signed delegation permits — so what the subset admits has to be
// something a reviewer reads as a diff, not something encoded across a dozen
// test functions.
//
// The corpus is empty until M2, which is when the grammar that runs it exists.
// What is here now is the format and the validator, so that the first vector to
// arrive is checked rather than checked against later.
package conformance

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Verdict is what the grammar must decide about a statement.
type Verdict string

const (
	// InSubset means the grammar proves what the statement touches, and must
	// extract the scope the vector states.
	InSubset Verdict = "in_subset"
	// OutOfGrammar means the checkable subset cannot express the statement.
	OutOfGrammar Verdict = "out_of_grammar"
	// Unsupported means the engine cannot enforce a control the statement needs.
	Unsupported Verdict = "unsupported"
)

var verdicts = []Verdict{InSubset, OutOfGrammar, Unsupported}

// Scope is what the grammar must extract from a statement it admits: the
// relation, the columns written, and the predicate the fence is built from.
type Scope struct {
	Relation       string   `json:"relation"`
	ColumnsWritten []string `json:"columns_written"`
	Predicate      string   `json:"predicate"`
}

// Vector is one statement and the verdict the grammar must reach on it.
type Vector struct {
	Name      string  `json:"name"`
	Statement string  `json:"statement"`
	Verdict   Verdict `json:"verdict"`
	// Scope is required for InSubset and must be absent otherwise.
	Scope *Scope `json:"scope,omitempty"`
	// Because is the reason a statement is refused, and is the message a
	// reader will be shown. Required for everything except InSubset.
	Because string `json:"because,omitempty"`
}

// Corpus is the whole vector file.
type Corpus struct {
	// SubsetVersion is the version of the checkable subset these vectors
	// describe. It is recorded on every extracted scope, so a delegation signed
	// against one subset stays pinned to it when the subset later widens.
	SubsetVersion int      `json:"subset_version"`
	Vectors       []Vector `json:"vectors"`
}

// Load reads and validates a corpus.
//
// Every vector is checked, and the first file to carry one gets the check —
// rather than the check being written once the corpus has already drifted,
// which is the order that never happens.
func Load(r io.Reader) (*Corpus, error) {
	decoder := json.NewDecoder(r)
	// An unknown field is a typo or a format change, and silently ignoring it
	// would drop a constraint the vector's author believed they had written.
	decoder.DisallowUnknownFields()

	var corpus Corpus
	if err := decoder.Decode(&corpus); err != nil {
		return nil, fmt.Errorf("parsing the corpus: %w", err)
	}
	if corpus.SubsetVersion < 0 {
		return nil, fmt.Errorf("subset_version is %d; it identifies a subset and cannot be negative",
			corpus.SubsetVersion)
	}

	seen := make(map[string]struct{}, len(corpus.Vectors))
	for i, v := range corpus.Vectors {
		if err := v.validate(); err != nil {
			return nil, fmt.Errorf("vector %d (%q): %w", i, v.Name, err)
		}
		if _, duplicate := seen[v.Name]; duplicate {
			return nil, fmt.Errorf("vector %d: %q is used twice; a name identifies a case in a "+
				"failure report and has to be unique", i, v.Name)
		}
		seen[v.Name] = struct{}{}
	}

	return &corpus, nil
}

func (v Vector) validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("name is required; it is how a failing case is identified")
	}
	if strings.TrimSpace(v.Statement) == "" {
		return fmt.Errorf("statement is required")
	}
	if !slices.Contains(verdicts, v.Verdict) {
		return fmt.Errorf("verdict is %q, want one of %v", v.Verdict, verdicts)
	}

	// The two shapes are exclusive, in both directions. A refused vector
	// carrying a scope would state an expectation nothing can check, and an
	// admitted one without a scope would pass by asserting only that the
	// grammar said yes — which is half of what the vector is for.
	if v.Verdict == InSubset {
		if v.Scope == nil {
			return fmt.Errorf("verdict %q requires a scope saying what the grammar must extract",
				v.Verdict)
		}
		if v.Because != "" {
			return fmt.Errorf("verdict %q must not carry `because`; there is nothing being refused",
				v.Verdict)
		}
		return v.Scope.validate()
	}

	if strings.TrimSpace(v.Because) == "" {
		return fmt.Errorf("verdict %q requires `because`, which is the reason a reader is shown",
			v.Verdict)
	}
	if v.Scope != nil {
		return fmt.Errorf("verdict %q must not carry a scope; nothing was extracted", v.Verdict)
	}
	return nil
}

func (s Scope) validate() error {
	if strings.TrimSpace(s.Relation) == "" {
		return fmt.Errorf("scope.relation is required")
	}
	if len(s.ColumnsWritten) == 0 {
		return fmt.Errorf("scope.columns_written is required; a statement in the subset writes " +
			"something, and the write set is what the fence asserts over")
	}
	for i, c := range s.ColumnsWritten {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("scope.columns_written[%d] is empty", i)
		}
	}
	return nil
}
