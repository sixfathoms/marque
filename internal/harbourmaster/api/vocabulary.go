package api

import (
	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
)

// The two closed vocabularies, crossing the wire boundary.
//
// Tables rather than switch statements, so TestEveryProtoStateIsMapped can
// enumerate them against the generated enum and fail when a value is added to
// the proto and not here. A switch with a default returns something plausible
// for a value nobody has mapped, which is how a new state becomes
// REQUEST_STATE_UNSPECIFIED in a client that would otherwise have complained.
var stateToProto = map[string]v1.RequestState{
	store.StatePending:       v1.RequestState_REQUEST_STATE_PENDING,
	store.StateVerifying:     v1.RequestState_REQUEST_STATE_VERIFYING,
	store.StateApproved:      v1.RequestState_REQUEST_STATE_APPROVED,
	store.StateRefused:       v1.RequestState_REQUEST_STATE_REFUSED,
	store.StateExpired:       v1.RequestState_REQUEST_STATE_EXPIRED,
	store.StateExecuted:      v1.RequestState_REQUEST_STATE_EXECUTED,
	store.StateIndeterminate: v1.RequestState_REQUEST_STATE_INDETERMINATE,
}

var outcomeToProto = map[string]v1.ExecutionOutcome{
	store.OutcomeCommitted:         v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED,
	store.OutcomeRolledBack:        v1.ExecutionOutcome_EXECUTION_OUTCOME_ROLLED_BACK,
	store.OutcomeAbortedNotApplied: v1.ExecutionOutcome_EXECUTION_OUTCOME_ABORTED_NOT_APPLIED,
	store.OutcomeIndeterminate:     v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE,
}

var outcomeFromProto = func() map[v1.ExecutionOutcome]string {
	m := make(map[v1.ExecutionOutcome]string, len(outcomeToProto))
	for s, p := range outcomeToProto {
		m[p] = s
	}
	return m
}()
