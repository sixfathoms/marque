//go:build integration

package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

const devTenant = "development"

// migrated gives a Store over a fresh, migrated database.
func migrated(t *testing.T) *Store {
	t.Helper()
	db, _ := freshDB(t)
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return New(db)
}

func aRequest(key string) Request {
	return Request{
		Statement:      "UPDATE accounts SET tier = 2 WHERE id = 42",
		Target:         "prod-primary",
		Role:           "marque_writer",
		Submitter:      "sam",
		Reason:         "raising one account's tier after a billing correction",
		IdempotencyKey: key,
	}
}

func TestSubmitIsIdempotentOnTheCallersKey(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()

	first, err := s.Submit(ctx, devTenant, aRequest("k1"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if !strings.HasPrefix(first, "req_") {
		t.Errorf("reference %q is not a req_ reference (EDR-0038)", first)
	}

	again, err := s.Submit(ctx, devTenant, aRequest("k1"))
	if err != nil {
		t.Fatalf("resubmitting: %v", err)
	}
	if again != first {
		t.Errorf("the same key produced two requests: %s and %s", first, again)
	}

	// A different key is a different request even for an identical statement:
	// sameness is asserted by the caller, never inferred.
	other, err := s.Submit(ctx, devTenant, aRequest("k2"))
	if err != nil {
		t.Fatalf("submitting with a second key: %v", err)
	}
	if other == first {
		t.Error("two different keys produced one request")
	}
}

// The read-then-write version of idempotency passes a sequential test and loses
// a race: both callers read nothing, both insert.
func TestConcurrentSubmitsWithOneKeyMakeOneRequest(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()

	const n = 8
	refs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refs[i], errs[i] = s.Submit(ctx, devTenant, aRequest("racing"))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("submitter %d: %v", i, err)
		}
	}
	for i, ref := range refs {
		if ref != refs[0] {
			t.Errorf("submitter %d got %s, submitter 0 got %s", i, ref, refs[0])
		}
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM requests WHERE tenant_id = $1`, devTenant).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("%d requests exist after %d concurrent submissions of one key", count, n)
	}
}

func TestRequestIsScopedToItsTenant(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()

	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name) VALUES ('other', 'Other')`); err != nil {
		t.Fatalf("creating a second tenant: %v", err)
	}

	got, err := s.Request(ctx, devTenant, ref)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if got.Statement != aRequest("k").Statement || got.State != StatePending {
		t.Errorf("read back %+v", got)
	}

	// EDR-0038: a reference is an identifier, not a capability. Another
	// tenant's reference must not exist here — not "exist but be forbidden",
	// which would confirm it.
	if _, err := s.Request(ctx, "other", ref); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("another tenant's reference: want ErrNoSuchRequest, got %v", err)
	}
	if _, err := s.Request(ctx, devTenant, "req_nothing"); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("an unknown reference: want ErrNoSuchRequest, got %v", err)
	}
}

func TestApproveMovesTheStateAndIsIdempotent(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}

	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}
	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateApproved {
		t.Errorf("state is %s after approval, want %s", got.State, StateApproved)
	}

	// NATURAL idempotency, as the proto declares: same person, same stage.
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving twice: %v", err)
	}
	var approvals int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM approvals WHERE tenant_id = $1 AND reference = $2`,
		devTenant, ref).Scan(&approvals); err != nil {
		t.Fatalf("counting approvals: %v", err)
	}
	if approvals != 1 {
		t.Errorf("%d approvals recorded for one person at one stage", approvals)
	}

	if err := s.Approve(ctx, devTenant, "req_nothing", "sam", 1); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("approving nothing: want ErrNoSuchRequest, got %v", err)
	}
}

func TestAnExecutionNeedsAnApprovalFirst(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}

	rows := int64(1)
	_, err = s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "n1", Outcome: OutcomeCommitted, RowsAffected: &rows,
	})
	if !errors.Is(err, ErrWrongState) {
		t.Fatalf("reporting an execution of an unapproved request: want ErrWrongState, got %v", err)
	}

	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}
	stored, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "n1", Outcome: OutcomeCommitted, RowsAffected: &rows,
	})
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	if stored.RowsAffected == nil || *stored.RowsAffected != 1 {
		t.Errorf("stored rows_affected is %v, want 1", stored.RowsAffected)
	}

	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateExecuted {
		t.Errorf("state is %s after a committed execution, want %s", got.State, StateExecuted)
	}

	// And approving something already executed is refused, rather than moving
	// it back to approved.
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); !errors.Is(err, ErrWrongState) {
		t.Errorf("approving an executed request: want ErrWrongState, got %v", err)
	}
}

// KEYED on the nonce, as the proto declares: a repeat returns what is STORED,
// not what the caller asked for the second time. A Pilot retrying after a
// timeout must learn the recorded outcome rather than overwrite it.
func TestRepeatingANonceReturnsTheStoredOutcome(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}

	rows := int64(7)
	first, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "same", Outcome: OutcomeCommitted, RowsAffected: &rows,
	})
	if err != nil {
		t.Fatalf("recording: %v", err)
	}

	// A different outcome under the same nonce.
	second, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "same", Outcome: OutcomeIndeterminate, RowsAffected: nil,
	})
	if err != nil {
		t.Fatalf("recording again: %v", err)
	}
	if second.Outcome != first.Outcome {
		t.Errorf("the retry returned %s; the stored outcome is %s", second.Outcome, first.Outcome)
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM executions WHERE tenant_id = $1 AND reference = $2`,
		devTenant, ref).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("%d executions recorded for one nonce", count)
	}

	// And the request's state followed the STORED outcome, not the retry's.
	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateExecuted {
		t.Errorf("state is %s; the retry's indeterminate moved it", got.State)
	}
}

func TestAnIndeterminateExecutionMovesTheRequestToIndeterminate(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "n", Outcome: OutcomeIndeterminate,
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}
	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateIndeterminate {
		t.Errorf("state is %s, want %s — EDR-0021: a failover surfaces as indeterminate",
			got.State, StateIndeterminate)
	}
}

// References must not be guessable from one another (EDR-0038 says they get
// pasted into shared channels).
func TestReferencesAreNotSequential(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	seen := map[string]bool{}
	for i := range 16 {
		ref, err := s.Submit(ctx, devTenant, aRequest(string(rune('a'+i))))
		if err != nil {
			t.Fatalf("submitting: %v", err)
		}
		if seen[ref] {
			t.Fatalf("reference %s was issued twice", ref)
		}
		seen[ref] = true
		if len(ref) < len("req_")+12 {
			t.Errorf("reference %s is short enough to enumerate", ref)
		}
	}
}
