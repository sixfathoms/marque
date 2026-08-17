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
	"strconv"
	"strings"
	"unicode/utf8"
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

// All four operations EDR-0007 puts in the checkable subset. Which of them a
// corpus actually uses is the subset *version*'s business, not the format's —
// the implementation plan's M2 starts narrower than the records allow, and
// `subset_version` exists so the admitted set can widen without invalidating a
// delegation signed against an earlier one. Hard-coding the narrower set here
// would make a statement the records admit inexpressible, which is the mistake
// this format has already made twice.
const (
	// Select reads and assigns nothing, so it carries no columns_written.
	Select Operation = "select"
	// Insert assigns to its column list (EDR-0007 names it alongside SET) and
	// has no WHERE, so it carries no predicate.
	Insert Operation = "insert"
	// Update assigns to named columns, and the scope lists them.
	Update Operation = "update"
	// Delete removes rows and assigns to no column. What it does remove —
	// including anything the engine removes on its behalf — is asserted at
	// execution by the write-set check (EDR-0033), not stated here.
	Delete Operation = "delete"
)

var operations = []Operation{Select, Insert, Update, Delete}

// assignsColumns says whether an operation names columns it writes. EDR-0007
// counts "SET, INSERT column list" as assigned columns; a select and a delete
// assign to none.
func (o Operation) assignsColumns() bool { return o == Update || o == Insert }

// hasPredicate says whether the statement can carry a WHERE at all. An insert
// has none — which is different from an unconditional update, whose predicate
// is TRUE.
func (o Operation) hasPredicate() bool { return o != Insert }

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
	Predicate      string   `json:"predicate,omitempty"`
}

// Vector is one statement and the verdict the grammar must reach on it.
type Vector struct {
	Name      string  `json:"name"`
	Statement string  `json:"statement"`
	Verdict   Verdict `json:"verdict"`
	// Scope is required for InSubset and must be absent otherwise.
	Scope *Scope `json:"scope,omitempty"`
	// Because is why the grammar could not admit the statement, and is the
	// message a reader is shown — whether that ends in an escalation
	// (out_of_grammar) or a refusal (unsupported). Required for both.
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

// fields are every key the format defines, spelled exactly.
//
// This set is flat and deliberately says nothing about *where* a key belongs.
// It exists for the two things a struct decode cannot do: Go matches JSON field
// names case-insensitively, so `DisallowUnknownFields` alone accepts "Verdict";
// and it takes the last of duplicate keys without complaint. Which level a key
// is legal at is enforced by decoding into the structs, strictly — the two
// checks are complementary, and an earlier version of this file dropped the
// second while claiming the first was sufficient, which let `predicate` sit on
// a vector.
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

// checkEscapes rejects a \uXXXX escape that does not form a character.
//
// A lone surrogate written as an escape is pure ASCII, so the byte-level UTF-8
// check above cannot see it — and Go then substitutes U+FFFD while another
// reader keeps the surrogate. Same bytes, two values, which is the one thing a
// normative language-neutral file must never do.
func checkEscapes(raw []byte) error {
	text := string(raw)
	// Escapes are consumed in order, not searched for. A scan that treats every
	// `\u` as an escape misreads two things: the literal characters `\ud800`
	// inside a string are written `\\ud800`, which SQL vectors exercising
	// PostgreSQL escape strings will contain; and a valid non-BMP character is
	// a *pair* of escapes, so the second must be consumed with the first rather
	// than met again as a lone trailing surrogate.
	for i := 0; i < len(text); {
		if text[i] != '\\' {
			i++
			continue
		}
		if i+1 >= len(text) {
			break // Malformed; the decoder reports it with more context.
		}
		if text[i+1] != 'u' {
			i += 2 // `\\`, `\"`, `\n` — consumed whole, so it cannot start one.
			continue
		}
		if i+6 > len(text) {
			break
		}
		point, err := strconv.ParseUint(text[i+2:i+6], 16, 16)
		if err != nil {
			i += 2
			continue
		}

		switch r := rune(point); {
		case r >= 0xDC00 && r <= 0xDFFF:
			return fmt.Errorf("the corpus contains a trailing surrogate escape \\u%04X with no "+
				"leading one; readers disagree about what that means", r)

		case r >= 0xD800 && r <= 0xDBFF:
			if i+12 > len(text) || text[i+6] != '\\' || text[i+7] != 'u' {
				return fmt.Errorf("the corpus contains an unpaired surrogate escape \\u%04X; Go "+
					"substitutes a replacement character here and other readers do not", r)
			}
			low, err := strconv.ParseUint(text[i+8:i+12], 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return fmt.Errorf("the corpus contains an unpaired surrogate escape \\u%04X; Go "+
					"substitutes a replacement character here and other readers do not", r)
			}
			i += 12 // The pair is one character.

		default:
			i += 6
		}
	}
	return nil
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
	// Invalid UTF-8 is replaced with U+FFFD by encoding/json rather than
	// rejected, so a corpus carrying bad bytes would load here and be refused —
	// or read differently — by a stricter implementation. Same class as the
	// duplicate-key and case-folding checks: the bytes must mean one thing.
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("the corpus is not valid UTF-8; Go would silently substitute a " +
			"replacement character where another reader would reject the file")
	}
	if err := checkEscapes(raw); err != nil {
		return nil, err
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

	// Non-nil even when empty, so that marshalling a loaded corpus produces
	// `"vectors": []` rather than `"vectors": null` — which this very loader
	// rejects as an absent field. A normative format that cannot round-trip
	// through its own reader is one that disagrees with itself.
	corpus := Corpus{
		SubsetVersion: *envelope.SubsetVersion,
		Vectors:       make([]Vector, 0, len(*envelope.Vectors)),
	}
	if corpus.SubsetVersion < 0 {
		return nil, fmt.Errorf("subset_version is %d; it identifies a subset and cannot be negative",
			corpus.SubsetVersion)
	}

	seen := make(map[string]struct{}, len(*envelope.Vectors))
	for i, rawVector := range *envelope.Vectors {
		// Strict, so that a key legal elsewhere in the format is still rejected
		// here: `predicate` belongs to a scope and `subset_version` to the
		// corpus, and neither is a vector's.
		var v Vector
		vectorDecoder := json.NewDecoder(bytes.NewReader(rawVector))
		vectorDecoder.DisallowUnknownFields()
		if err := vectorDecoder.Decode(&v); err != nil {
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

	// The two shapes are exclusive, in both directions. A vector the grammar
	// did not admit cannot carry a scope, because nothing was extracted; an
	// admitted one without a scope would pass by asserting only that the
	// grammar said yes — which is half of what the vector is for.
	if v.Verdict == InSubset {
		if v.Scope == nil {
			return fmt.Errorf("verdict %q requires a scope saying what the grammar must extract",
				v.Verdict)
		}
		if _, ok := present["because"]; ok {
			return fmt.Errorf("verdict %q must not carry `because`; nothing needed explaining",
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
	// which is the escape the pinned search_path closes. A vector without a
	// schema therefore describes a statement the grammar would not have
	// admitted — which is an escalation, not a rejection (EDR-0007).
	if strings.TrimSpace(s.Schema) == "" {
		return fmt.Errorf("scope.schema is required; the subset admits a directly-named base " +
			"table, not one resolved through search_path")
	}
	if strings.TrimSpace(s.Relation) == "" {
		return fmt.Errorf("scope.relation is required")
	}

	// Which keys the author wrote, so a forbidden field is rejected for being
	// present rather than for being empty.
	var scopeKeys map[string]json.RawMessage
	if err := json.Unmarshal(vectorKeys["scope"], &scopeKeys); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	_, hasColumns := scopeKeys["columns_written"]
	_, hasPredicate := scopeKeys["predicate"]

	switch {
	case s.Operation.assignsColumns() && len(s.ColumnsWritten) == 0:
		return fmt.Errorf("scope.operation %q assigns columns, so columns_written is required",
			s.Operation)
	case !s.Operation.assignsColumns() && hasColumns:
		return fmt.Errorf("scope.operation %q assigns to no column, so columns_written must be "+
			"absent; what it touches is asserted at execution", s.Operation)
	}
	for i, c := range s.ColumnsWritten {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("scope.columns_written[%d] is empty", i)
		}
	}

	// The predicate is what the fence is built from (EDR-0007). An
	// unconditional statement records TRUE rather than omitting it, so that
	// requiring the field does not put a statement the records admit outside
	// the corpus — EDR-0007's subset rules do not require a WHERE. An insert
	// has no WHERE at all, which is a different thing from having a vacuous
	// one.
	switch {
	case s.Operation.hasPredicate() && strings.TrimSpace(s.Predicate) == "":
		return fmt.Errorf("scope.predicate is required; it is what the fence is built from, and " +
			"an unconditional statement records TRUE rather than omitting it")
	case !s.Operation.hasPredicate() && hasPredicate:
		return fmt.Errorf("scope.operation %q has no WHERE, so predicate must be absent",
			s.Operation)
	}
	return nil
}
