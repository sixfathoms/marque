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

// The three verdicts are EDR-0039's, quoted rather than paraphrased. An earlier
// version of this file restated them in its own words and had `unsupported`
// mean "the engine cannot enforce a control" — which is not what the record
// says, and which erases the distinction EDR-0022 insists an operator can draw:
// "ask someone" and "never going to work" must not look alike.
const (
	// InSubset — "Provable shape; scope and predicate extracted." May match a
	// delegation or standing order.
	InSubset Verdict = "in_subset"
	// OutOfGrammar — "Valid SQL the subset cannot prove anything about."
	// Escalates to a human, so it is not a refusal.
	OutOfGrammar Verdict = "out_of_grammar"
	// Unsupported — "Marque will not broker it at all (DDL, implicit commit,
	// multi-statement)." Refused with the reason.
	Unsupported Verdict = "unsupported"
)

var verdicts = []Verdict{InSubset, OutOfGrammar, Unsupported}

// Operation is the kind of statement, which decides what a scope must carry.
type Operation string

const (
	// Update assigns to named columns, and the scope lists them.
	Update Operation = "update"
	// Delete removes rows and assigns to no column, so a delete scope carries
	// no columns_written. The rows it removes are the write set the fence
	// asserts over (EDR-0033), which is measured at execution rather than
	// stated here.
	Delete Operation = "delete"
)

// EDR-0007 puts SELECT and INSERT in the checkable subset too. They are absent
// here because M2's initial subset is UPDATE and DELETE, and adding an
// operation later is additive — a vector written today stays valid.
var operations = []Operation{Update, Delete}

// Scope is what the grammar must extract from a statement it admits: the
// relation, the columns written, and the predicate the fence is built from.
type Scope struct {
	Operation Operation `json:"operation"`
	Relation  string    `json:"relation"`
	// ColumnsWritten is required for Update and must be absent for Delete.
	ColumnsWritten []string `json:"columns_written,omitempty"`
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

	// Decoded through pointers so that an absent field is distinguishable from
	// a zero one. `null`, `{}` and `{"vectors":[]}` all decode into a
	// zero-valued Corpus, so without this, deleting the file's contents — or
	// replacing the normative corpus with `null` — reads as a valid empty one.
	var envelope struct {
		SubsetVersion *int      `json:"subset_version"`
		Vectors       *[]Vector `json:"vectors"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("parsing the corpus: %w", err)
	}
	switch {
	case envelope.SubsetVersion == nil:
		return nil, fmt.Errorf("subset_version is absent; every corpus states the subset it describes")
	case envelope.Vectors == nil:
		return nil, fmt.Errorf("vectors is absent; an empty corpus writes it as []")
	}

	// A second value after the corpus means the file was concatenated, and
	// decoding one value would silently ignore everything past it.
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return nil, fmt.Errorf("the corpus is followed by more JSON; the file holds one document")
	}

	corpus := Corpus{SubsetVersion: *envelope.SubsetVersion, Vectors: *envelope.Vectors}
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
	if !slices.Contains(operations, s.Operation) {
		return fmt.Errorf("scope.operation is %q, want one of %v", s.Operation, operations)
	}
	if strings.TrimSpace(s.Relation) == "" {
		return fmt.Errorf("scope.relation is required")
	}
	// Schema-qualified, because EDR-0007 puts a relation resolved through
	// `search_path` outside the subset: an unqualified name can bind elsewhere,
	// which is the escape the pinned search_path closes. The grammar therefore
	// extracts a directly-named base table, and a vector saying otherwise
	// describes a statement it should have refused.
	if !strings.Contains(s.Relation, ".") {
		return fmt.Errorf("scope.relation %q is not schema-qualified; the subset admits a "+
			"directly-named base table, not one resolved through search_path", s.Relation)
	}
	// The predicate is what the fence is built from (EDR-0007). Required for
	// both operations because the initial subset is "single-relation UPDATE and
	// DELETE with a conjunctive predicate over literals" — the implementation
	// plan's M2, not a rule invented here. If `insert` ever joins the operation
	// set it has no WHERE, and this becomes operation-dependent like
	// columns_written below.
	if strings.TrimSpace(s.Predicate) == "" {
		return fmt.Errorf("scope.predicate is required; it is what the fence is built from")
	}

	switch s.Operation {
	case Update:
		if len(s.ColumnsWritten) == 0 {
			return fmt.Errorf("scope.operation %q requires columns_written", s.Operation)
		}
		for i, c := range s.ColumnsWritten {
			if strings.TrimSpace(c) == "" {
				return fmt.Errorf("scope.columns_written[%d] is empty", i)
			}
		}
	case Delete:
		if len(s.ColumnsWritten) != 0 {
			return fmt.Errorf("scope.operation %q assigns to no column, so columns_written "+
				"must be absent; the rows it removes are measured at execution", s.Operation)
		}
	}
	return nil
}
