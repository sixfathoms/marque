package api

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
)

// fakeStore records what it was asked and returns what it was told to.
type fakeStore struct {
	tenant    string
	submitted store.Request
	err       error
	execution store.Execution
	request   store.Request
}

func (f *fakeStore) Submit(_ context.Context, tenant string, r store.Request) (string, error) {
	f.tenant, f.submitted = tenant, r
	return "req_stub", f.err
}

func (f *fakeStore) Request(_ context.Context, tenant, _ string) (store.Request, error) {
	f.tenant = tenant
	return f.request, f.err
}

func (f *fakeStore) Approve(_ context.Context, tenant, _, _ string, _ uint32) error {
	f.tenant = tenant
	return f.err
}

func (f *fakeStore) RecordExecution(_ context.Context, tenant, _ string, e store.Execution) (store.Execution, error) {
	f.tenant = tenant
	if f.execution.Nonce != "" {
		return f.execution, f.err
	}
	return e, f.err
}

func aSubmit() *v1.SubmitRequest {
	return &v1.SubmitRequest{
		Statement:      "UPDATE accounts SET tier = 2",
		Target:         "prod-primary",
		Role:           "marque_writer",
		Reason:         "a billing correction",
		IdempotencyKey: "k1",
	}
}

// Every required field, one at a time, so each is refused for its own reason
// rather than by whichever check happens to run first.
func TestSubmitRefusesAnIncompleteRequest(t *testing.T) {
	for _, field := range []string{"statement", "target", "role", "reason", "idempotency_key"} {
		for _, blank := range []string{"", "   ", "\t\n"} {
			t.Run(field+" "+blank, func(t *testing.T) {
				m := aSubmit()
				switch field {
				case "statement":
					m.Statement = blank
				case "target":
					m.Target = blank
				case "role":
					m.Role = blank
				case "reason":
					m.Reason = blank
				case "idempotency_key":
					m.IdempotencyKey = blank
				}
				_, err := New(&fakeStore{}, "development").
					Submit(t.Context(), connect.NewRequest(m))
				if connect.CodeOf(err) != connect.CodeInvalidArgument {
					t.Errorf("blank %s: code is %v, want InvalidArgument (err=%v)", field, connect.CodeOf(err), err)
				}
			})
		}
	}
}

// The tenant is configuration, and no request field may reach it (EDR-0025).
func TestTheTenantComesFromConfigurationAndNotTheRequest(t *testing.T) {
	f := &fakeStore{}
	if _, err := New(f, "acme").Submit(t.Context(), connect.NewRequest(aSubmit())); err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if f.tenant != "acme" {
		t.Errorf("the store was given tenant %q, want the configured acme", f.tenant)
	}
	// And the submitter is likewise not the caller's to choose. M1 has nobody
	// to authenticate, and the recorded value says so.
	if f.submitted.Submitter != "unauthenticated" {
		t.Errorf("submitter recorded as %q; M1 has no identity and the record should say so", f.submitted.Submitter)
	}
}

func TestApproveRefusesAStageM1CannotHonour(t *testing.T) {
	for _, stage := range []uint32{0, 2, 3, 99} {
		_, err := New(&fakeStore{}, "development").Approve(t.Context(),
			connect.NewRequest(&v1.ApproveRequest{Reference: "req_x", Approver: "sam", Stage: stage}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("stage %d: code is %v, want InvalidArgument", stage, connect.CodeOf(err))
		}
	}
	if _, err := New(&fakeStore{}, "development").Approve(t.Context(),
		connect.NewRequest(&v1.ApproveRequest{Reference: "req_x", Approver: "sam", Stage: 1})); err != nil {
		t.Errorf("stage 1 refused: %v", err)
	}
	if _, err := New(&fakeStore{}, "development").Approve(t.Context(),
		connect.NewRequest(&v1.ApproveRequest{Reference: "req_x", Approver: " ", Stage: 1})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a blank approver was accepted")
	}
}

// The biconditional the schema also enforces, checked here so a caller gets an
// argument error rather than a constraint violation reported as Internal.
func TestRowsAffectedMustBeAbsentExactlyWhenIndeterminate(t *testing.T) {
	rows := int64(3)
	for name, c := range map[string]struct {
		outcome v1.ExecutionOutcome
		rows    *int64
		ok      bool
	}{
		"committed with a count":        {v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED, &rows, true},
		"committed without one":         {v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED, nil, false},
		"indeterminate without a count": {v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE, nil, true},
		"indeterminate WITH a count":    {v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE, &rows, false},
		"rolled back with a count":      {v1.ExecutionOutcome_EXECUTION_OUTCOME_ROLLED_BACK, &rows, true},
		"an unspecified outcome":        {v1.ExecutionOutcome_EXECUTION_OUTCOME_UNSPECIFIED, &rows, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(&fakeStore{}, "development").RecordExecution(t.Context(),
				connect.NewRequest(&v1.RecordExecutionRequest{
					Reference: "req_x", Nonce: "n", Outcome: c.outcome, RowsAffected: c.rows,
				}))
			if c.ok && err != nil {
				t.Errorf("refused: %v", err)
			}
			if !c.ok && connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("code is %v, want InvalidArgument", connect.CodeOf(err))
			}
		})
	}
}

// A retry that reports a different outcome gets the STORED one back.
func TestRecordExecutionReturnsWhatIsStored(t *testing.T) {
	rows := int64(7)
	f := &fakeStore{execution: store.Execution{
		Nonce: "n", Outcome: store.OutcomeCommitted, RowsAffected: &rows,
	}}
	got, err := New(f, "development").RecordExecution(t.Context(),
		connect.NewRequest(&v1.RecordExecutionRequest{
			Reference: "req_x", Nonce: "n",
			Outcome: v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE,
		}))
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	if got.Msg.GetExecution().GetOutcome() != v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED {
		t.Errorf("the retry was told %s; the stored outcome is committed",
			got.Msg.GetExecution().GetOutcome())
	}
}

// EDR-0038: a reference must not confirm its own existence, so an unknown one
// is NotFound and never PermissionDenied.
func TestStoreErrorsBecomeCodesAClientCanActOn(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want connect.Code
	}{
		"unknown reference": {store.ErrNoSuchRequest, connect.CodeNotFound},
		"wrong state":       {store.ErrWrongState, connect.CodeFailedPrecondition},
		"anything else":     {errors.New("the disk caught fire"), connect.CodeInternal},
	} {
		t.Run(name, func(t *testing.T) {
			s := New(&fakeStore{err: c.err}, "development")
			if _, err := s.GetRequest(t.Context(),
				connect.NewRequest(&v1.GetRequestRequest{Reference: "req_x"})); connect.CodeOf(err) != c.want {
				t.Errorf("GetRequest: code is %v, want %v", connect.CodeOf(err), c.want)
			}
			if _, err := s.Approve(t.Context(),
				connect.NewRequest(&v1.ApproveRequest{Reference: "req_x", Approver: "sam", Stage: 1})); connect.CodeOf(err) != c.want {
				t.Errorf("Approve: code is %v, want %v", connect.CodeOf(err), c.want)
			}
		})
	}
	// PermissionDenied must never be the answer to a reference question.
	s := New(&fakeStore{err: store.ErrNoSuchRequest}, "development")
	_, err := s.GetRequest(t.Context(), connect.NewRequest(&v1.GetRequestRequest{Reference: "req_x"}))
	if connect.CodeOf(err) == connect.CodePermissionDenied {
		t.Error("an unknown reference answered PermissionDenied, which confirms it exists")
	}
}

// A state the database holds and this build does not know is a fault here, not
// a silent UNSPECIFIED on the wire.
func TestAnUnknownStoredStateIsInternalNotUnspecified(t *testing.T) {
	f := &fakeStore{request: store.Request{Reference: "req_x", State: "contemplating"}}
	_, err := New(f, "development").GetRequest(t.Context(),
		connect.NewRequest(&v1.GetRequestRequest{Reference: "req_x"}))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("code is %v, want Internal", connect.CodeOf(err))
	}
}
