package api

import (
	"testing"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
)

// The vocabularies are closed and cross a wire boundary, so the failure to
// worry about is a value existing on one side and not the other. Enumerating
// the generated enum's own name table is what makes ADDING a value to the proto
// fail here, rather than quietly mapping to UNSPECIFIED in some client.
func TestEveryProtoStateIsMapped(t *testing.T) {
	mapped := map[v1.RequestState]bool{}
	for _, p := range stateToProto {
		mapped[p] = true
	}
	for value, name := range v1.RequestState_name {
		state := v1.RequestState(value)
		if state == v1.RequestState_REQUEST_STATE_UNSPECIFIED {
			// UNSPECIFIED is proto3's zero and means "the field was not set".
			// Nothing stored may map to it.
			continue
		}
		if !mapped[state] {
			t.Errorf("%s exists in the proto and no stored state maps to it; a request in that state would be reported as UNSPECIFIED", name)
		}
	}
	// And the other direction: a stored state with no proto value would be an
	// Internal error at read time, which is a worse way to find out.
	if len(stateToProto) != len(v1.RequestState_name)-1 {
		t.Errorf("%d states are mapped and the proto has %d besides UNSPECIFIED",
			len(stateToProto), len(v1.RequestState_name)-1)
	}
}

func TestEveryProtoOutcomeIsMapped(t *testing.T) {
	mapped := map[v1.ExecutionOutcome]bool{}
	for _, p := range outcomeToProto {
		mapped[p] = true
	}
	for value, name := range v1.ExecutionOutcome_name {
		outcome := v1.ExecutionOutcome(value)
		if outcome == v1.ExecutionOutcome_EXECUTION_OUTCOME_UNSPECIFIED {
			continue
		}
		if !mapped[outcome] {
			t.Errorf("%s exists in the proto and no stored outcome maps to it", name)
		}
	}
	if len(outcomeToProto) != len(v1.ExecutionOutcome_name)-1 {
		t.Errorf("%d outcomes are mapped and the proto has %d besides UNSPECIFIED",
			len(outcomeToProto), len(v1.ExecutionOutcome_name)-1)
	}
	// in_progress is deliberately absent from BOTH: a control-plane report is
	// written when an attempt ends (EDR-0042). If it ever appears, it should
	// appear because someone decided to add it.
	for _, name := range v1.ExecutionOutcome_name {
		if name == "EXECUTION_OUTCOME_IN_PROGRESS" {
			t.Error("in_progress reached the wire vocabulary; EDR-0042 excludes it deliberately")
		}
	}
}

// The store's constants and the proto's must agree about what the values ARE,
// not merely that there are the same number of them.
func TestTheMappedNamesMatchTheStoredOnes(t *testing.T) {
	for stored, p := range stateToProto {
		want := "REQUEST_STATE_" + upper(stored)
		if got := v1.RequestState_name[int32(p)]; got != want {
			t.Errorf("stored %q maps to %s, want %s", stored, got, want)
		}
	}
	for stored, p := range outcomeToProto {
		want := "EXECUTION_OUTCOME_" + upper(stored)
		if got := v1.ExecutionOutcome_name[int32(p)]; got != want {
			t.Errorf("stored %q maps to %s, want %s", stored, got, want)
		}
	}
	// And that the store's constants are the SQL vocabulary, spelled the way
	// the CHECK constraint spells them.
	for _, s := range []string{
		store.StatePending, store.StateVerifying, store.StateApproved, store.StateRefused,
		store.StateExpired, store.StateExecuted, store.StateIndeterminate,
	} {
		if _, ok := stateToProto[s]; !ok {
			t.Errorf("store constant %q is not mapped", s)
		}
	}
	for _, o := range []string{
		store.OutcomeCommitted, store.OutcomeRolledBack,
		store.OutcomeAbortedNotApplied, store.OutcomeIndeterminate,
	} {
		if _, ok := outcomeToProto[o]; !ok {
			t.Errorf("store constant %q is not mapped", o)
		}
	}
}

func upper(s string) string {
	out := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
