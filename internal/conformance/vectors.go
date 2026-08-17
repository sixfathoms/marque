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
	"bytes"
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
//
// Schema and relation are separate fields rather than one qualified string.
// EDR-0007 puts a relation resolved through `search_path` outside the subset,
// so both halves are required — and encoding them separately means there is
// nothing to parse and nothing to get wrong. A single string checked for a dot
// accepts ".accounts", "public." and the quoted identifier "accounts.archive",
// each of which is either malformed or not qualified at all.
type Scope struct {
	Operation Operation `json:"operation"`
	Schema    string    `json:"schema"`
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

// fields are every key the format defines, spelled exactly. Go matches JSON
// field names case-insensitively, so `DisallowUnknownFields` alone accepts
// "Verdict" — which another implementation of this format would reject. The
// names are distinct across every level, so one flat set is enough.
var fields = map[string]struct{}{
	"subset_version": {}, "vectors": {},
	"name": {}, "statement": {}, "verdict": {}, "scope": {}, "because": {},
	"operation": {}, "schema": {}, "relation": {}, "columns_written": {}, "predicate": {},
}

// checkKeys rejects a duplicate key and any spelling other than the documented
// one, before anything is decoded.
//
// Both matter because this file is normative and language-neutral (CLAUDE.md).
// encoding/json takes the last of two `verdict` keys and accepts `Verdict` for
// `verdict`; a strict parser elsewhere would reject the file, or read a
// different verdict from the same bytes — which is the corpus disagreeing with
// itself depending on who reads it.
func checkKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))

	// Inside an object the token stream alternates key, value, key, value, so
	// which of the two is expected has to be tracked rather than guessed —
	// a string value like "in_subset" is otherwise indistinguishable from a key.
	type frame struct {
		object    bool
		expectKey bool
		seen      map[string]struct{}
	}
	var stack []frame

	consumedValue := func() {
		if n := len(stack); n > 0 && stack[n-1].object {
			stack[n-1].expectKey = true
		}
	}

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parsing the corpus: %w", err)
		}

		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				stack = append(stack, frame{object: true, expectKey: true, seen: map[string]struct{}{}})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				stack = stack[:len(stack)-1]
				consumedValue()
			}
			continue
		}

		top := len(stack) - 1
		if top >= 0 && stack[top].object && stack[top].expectKey {
			key, _ := token.(string)
			if _, ok := fields[key]; !ok {
				return fmt.Errorf("unknown field %q; the format defines no such key, and field "+
					"names are matched exactly here even though Go would fold the case", key)
			}
			if _, duplicate := stack[top].seen[key]; duplicate {
				return fmt.Errorf("duplicate key %q; one parser takes the first and another the "+
					"last, so the corpus would say different things to different readers", key)
			}
			stack[top].seen[key] = struct{}{}
			stack[top].expectKey = false
			continue
		}
		consumedValue()
	}
}

// Load reads and validates a corpus.
//
// Every vector is checked, and the first file to carry one gets the check —
// rather than the check being written once the corpus has already drifted,
// which is the order that never happens.
func Load(r io.Reader) (*Corpus, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading the corpus: %w", err)
	}
	if err := checkKeys(raw); err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	// An unknown field is a typo or a format change, and silently ignoring it
	// would drop a constraint the vector's author believed they had written.
	decoder.DisallowUnknownFields()

	// Decoded through pointers so that an absent field is distinguishable from
	// a zero one. `null`, `{}` and `{"vectors":[]}` all decode into a
	// zero-valued Corpus, so without this, deleting the file's contents — or
	// replacing the normative corpus with `null` — reads as a valid empty one.
	var envelope struct {
		SubsetVersion *int               `json:"subset_version"`
		Vectors       *[]json.RawMessage `json:"vectors"`
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

	corpus := Corpus{SubsetVersion: *envelope.SubsetVersion}
	if corpus.SubsetVersion < 0 {
		return nil, fmt.Errorf("subset_version is %d; it identifies a subset and cannot be negative",
			corpus.SubsetVersion)
	}

	seen := make(map[string]struct{}, len(*envelope.Vectors))
	for i, rawVector := range *envelope.Vectors {
		var v Vector
		if err := json.Unmarshal(rawVector, &v); err != nil {
			return nil, fmt.Errorf("vector %d: %w", i, err)
		}
		// Which keys the author actually wrote, so that a forbidden field can
		// be rejected for being *present* rather than for being non-zero.
		// `"because": ""` on an admitted vector is a shape the format forbids,
		// and a zero value cannot be told from an omission.
		var present map[string]json.RawMessage
		if err := json.Unmarshal(rawVector, &present); err != nil {
			return nil, fmt.Errorf("vector %d: %w", i, err)
		}
		if err := v.validate(present); err != nil {
			return nil, fmt.Errorf("vector %d (%q): %w", i, v.Name, err)
		}
		if _, duplicate := seen[v.Name]; duplicate {
			return nil, fmt.Errorf("vector %d: %q is used twice; a name identifies a case in a "+
				"failure report and has to be unique", i, v.Name)
		}
		seen[v.Name] = struct{}{}
		corpus.Vectors = append(corpus.Vectors, v)
	}

	return &corpus, nil
}

func (v Vector) validate(present map[string]json.RawMessage) error {
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
		if _, ok := present["because"]; ok {
			return fmt.Errorf("verdict %q must not carry `because`; there is nothing being refused",
				v.Verdict)
		}
		return v.Scope.validate(present)
	}

	if strings.TrimSpace(v.Because) == "" {
		return fmt.Errorf("verdict %q requires `because`, which is the reason a reader is shown",
			v.Verdict)
	}
	if _, ok := present["scope"]; ok {
		return fmt.Errorf("verdict %q must not carry a scope; nothing was extracted", v.Verdict)
	}
	return nil
}

func (s Scope) validate(vectorKeys map[string]json.RawMessage) error {
	if !slices.Contains(operations, s.Operation) {
		return fmt.Errorf("scope.operation is %q, want one of %v", s.Operation, operations)
	}
	// Both halves required, because EDR-0007 puts a relation resolved through
	// `search_path` outside the subset: an unqualified name can bind elsewhere,
	// which is the escape the pinned search_path closes. The grammar extracts a
	// directly-named base table, so a vector without a schema describes a
	// statement it should have refused.
	if strings.TrimSpace(s.Schema) == "" {
		return fmt.Errorf("scope.schema is required; the subset admits a directly-named base " +
			"table, not one resolved through search_path")
	}
	if strings.TrimSpace(s.Relation) == "" {
		return fmt.Errorf("scope.relation is required")
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
		// Present at all, not merely non-empty: `"columns_written": []` on a
		// delete states an expectation the format does not have.
		var scopeKeys map[string]json.RawMessage
		if err := json.Unmarshal(vectorKeys["scope"], &scopeKeys); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		if _, ok := scopeKeys["columns_written"]; ok {
			return fmt.Errorf("scope.operation %q assigns to no column, so columns_written "+
				"must be absent; the rows it removes are measured at execution", s.Operation)
		}
	}
	return nil
}
