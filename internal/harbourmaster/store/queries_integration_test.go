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

// Once a request is terminal, a NEW nonce is a second execution nobody
// approved. A reviewer recorded a committed attempt, then reported a different
// nonce as indeterminate, and watched the request move backwards — and the
// reverse worked too, so the last report won.
func TestAFreshNonceIsRefusedAfterTheRequestIsTerminal(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	ref, err := s.Submit(ctx, devTenant, aRequest("k"))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}
	rows := int64(1)
	if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "first", Outcome: OutcomeCommitted, RowsAffected: &rows,
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	// A different nonce, after the fact.
	if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "second", Outcome: OutcomeIndeterminate,
	}); !errors.Is(err, ErrWrongState) {
		t.Errorf("a fresh nonce against an executed request: want ErrWrongState, got %v", err)
	}
	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateExecuted {
		t.Errorf("state moved to %s; a refused report must change nothing", got.State)
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM executions WHERE tenant_id = $1 AND reference = $2`,
		devTenant, ref).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("%d executions recorded; the second was refused and must not have landed", n)
	}

	// The recorded nonce may still be repeated: that is an acknowledgement,
	// and it is what makes a Pilot's retry of the REPORT safe.
	again, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "first", Outcome: OutcomeIndeterminate,
	})
	if err != nil {
		t.Fatalf("repeating a recorded nonce: %v", err)
	}
	if again.Outcome != OutcomeCommitted {
		t.Errorf("the repeat returned %s; committed is what was stored", again.Outcome)
	}
}

// The symmetric case. The test above pins executed → fresh nonce, and a
// mutation permitting fresh nonces only after `indeterminate` survived it —
// so the reverse direction was described as covered and was not.
func TestAFreshNonceIsRefusedAfterAnIndeterminateToo(t *testing.T) {
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
		Nonce: "first", Outcome: OutcomeIndeterminate,
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	rows := int64(1)
	if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
		Nonce: "second", Outcome: OutcomeCommitted, RowsAffected: &rows,
	}); !errors.Is(err, ErrWrongState) {
		t.Errorf("a fresh nonce against an indeterminate request: want ErrWrongState, got %v", err)
	}
	got, _ := s.Request(ctx, devTenant, ref)
	if got.State != StateIndeterminate {
		t.Errorf("state moved to %s; a refused report must change nothing", got.State)
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM executions WHERE tenant_id = $1 AND reference = $2`,
		devTenant, ref).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("%d executions recorded; the second was refused and must not have landed", n)
	}
}

// Four outcomes, three destinations. An earlier version sent everything but
// `indeterminate` to `executed`, so a statement that provably did not run was
// recorded as one that did — and the request became a dead end, refusing both a
// fresh nonce and a second approval, which discarded the real result of the
// re-run the design blesses.
func TestTheRequestStateFollowsWhatTheOutcomeMeans(t *testing.T) {
	for name, c := range map[string]struct {
		outcome   string
		rows      *int64
		wantState string
	}{
		"committed is the only one that executed anything": {
			OutcomeCommitted, ptr(3), StateExecuted,
		},
		"indeterminate is terminal and a human resolves it": {
			OutcomeIndeterminate, nil, StateIndeterminate,
		},
		// Provably not applied, so the request stays runnable.
		"a clean abort leaves it approved": {
			OutcomeAbortedNotApplied, ptr(0), StateApproved,
		},
		"a refused commit leaves it approved": {
			OutcomeRolledBack, ptr(0), StateApproved,
		},
	} {
		t.Run(name, func(t *testing.T) {
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
				Nonce: "n1", Outcome: c.outcome, RowsAffected: c.rows,
			}); err != nil {
				t.Fatalf("recording %s: %v", c.outcome, err)
			}
			got, err := s.Request(ctx, devTenant, ref)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if got.State != c.wantState {
				t.Fatalf("%s left the request %s, want %s", c.outcome, got.State, c.wantState)
			}

			// And the consequence that matters: a request left approved can be
			// run again under a new nonce, and one left terminal cannot.
			_, err = s.RecordExecution(ctx, devTenant, ref, Execution{
				Nonce: "n2", Outcome: OutcomeCommitted, RowsAffected: ptr(1),
			})
			if c.wantState == StateApproved {
				if err != nil {
					t.Errorf("a request left approved refused a second attempt: %v", err)
				}
				after, _ := s.Request(ctx, devTenant, ref)
				if after.State != StateExecuted {
					t.Errorf("the re-run committed and the request is %s", after.State)
				}
			} else if !errors.Is(err, ErrWrongState) {
				t.Errorf("a terminal request accepted a fresh nonce: %v", err)
			}
		})
	}
}

func ptr(n int64) *int64 { return &n }

// The row lock, pinned. Both of these pass without `FOR UPDATE` if the
// goroutines are merely started in a loop — the window is small and the loop is
// slower than it. A barrier releases them together and the window opens.
func TestTheRowLockSerialisesConcurrentWriters(t *testing.T) {
	t.Run("an approval cannot overtake an execution", func(t *testing.T) {
		s := migrated(t)
		ctx := t.Context()
		for i := range 20 {
			ref, err := s.Submit(ctx, devTenant, aRequest("a"+string(rune('a'+i))))
			if err != nil {
				t.Fatalf("submitting: %v", err)
			}
			if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
				t.Fatalf("approving: %v", err)
			}

			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, _ = s.RecordExecution(ctx, devTenant, ref, Execution{
					Nonce: "n", Outcome: OutcomeCommitted, RowsAffected: ptr(1),
				})
			}()
			go func() {
				defer wg.Done()
				<-start
				_ = s.Approve(ctx, devTenant, ref, "kim", 1)
			}()
			close(start)
			wg.Wait()

			got, err := s.Request(ctx, devTenant, ref)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			// Whichever won, an executed request must not be sitting at
			// approved: that would mean an approval overwrote a terminal state.
			var executions int
			if err := s.db.QueryRowContext(ctx,
				`SELECT count(*) FROM executions WHERE tenant_id = $1 AND reference = $2`,
				devTenant, ref).Scan(&executions); err != nil {
				t.Fatalf("counting: %v", err)
			}
			if executions == 1 && got.State != StateExecuted {
				t.Fatalf("round %d: an execution was recorded and the request is %s", i, got.State)
			}
		}
	})

	t.Run("one approval admits one execution", func(t *testing.T) {
		s := migrated(t)
		ctx := t.Context()
		for i := range 20 {
			ref, err := s.Submit(ctx, devTenant, aRequest("b"+string(rune('a'+i))))
			if err != nil {
				t.Fatalf("submitting: %v", err)
			}
			if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
				t.Fatalf("approving: %v", err)
			}

			// Four DIFFERENT nonces racing. Exactly one may land: the others
			// arrive after the request has left `approved`.
			start := make(chan struct{})
			var wg sync.WaitGroup
			for n := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, _ = s.RecordExecution(ctx, devTenant, ref, Execution{
						Nonce: string(rune('a' + n)), Outcome: OutcomeCommitted, RowsAffected: ptr(1),
					})
				}()
			}
			close(start)
			wg.Wait()

			var executions int
			if err := s.db.QueryRowContext(ctx,
				`SELECT count(*) FROM executions WHERE tenant_id = $1 AND reference = $2`,
				devTenant, ref).Scan(&executions); err != nil {
				t.Fatalf("counting: %v", err)
			}
			if executions != 1 {
				t.Fatalf("round %d: %d executions recorded against one approval", i, executions)
			}
		}
	})
}

// What was submitted is what an approver judged, so resubmitting a key must
// not change it. A one-token diff — `DO UPDATE SET statement =
// EXCLUDED.statement` instead of the no-op — let an approved request's
// statement be replaced by anyone who knew its idempotency key, and no test
// noticed.
func TestResubmittingCannotChangeWhatWasSubmitted(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()

	first := aRequest("k")
	ref, err := s.Submit(ctx, devTenant, first)
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
		t.Fatalf("approving: %v", err)
	}

	// The same key, every other field different.
	second := Request{
		Statement:      "DROP TABLE accounts",
		Target:         "somewhere-else",
		Role:           "someone-else",
		Submitter:      "mallory",
		Reason:         "a different reason entirely",
		IdempotencyKey: "k",
	}
	again, err := s.Submit(ctx, devTenant, second)
	if err != nil {
		t.Fatalf("resubmitting: %v", err)
	}
	if again != ref {
		t.Fatalf("resubmitting one key made a second request: %s and %s", ref, again)
	}

	got, err := s.Request(ctx, devTenant, ref)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	for name, pair := range map[string][2]string{
		"statement": {got.Statement, first.Statement},
		"target":    {got.Target, first.Target},
		"role":      {got.Role, first.Role},
		"reason":    {got.Reason, first.Reason},
		"submitter": {got.Submitter, first.Submitter},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s became %q; it was %q when it was approved", name, pair[0], pair[1])
		}
	}
	if got.State != StateApproved {
		t.Errorf("resubmitting moved the state to %s", got.State)
	}
}

// The nonce-already-recorded check is scoped to the REQUEST. Without that
// scope, a nonce recorded for one request lets a second execution land on a
// different terminal one — and nonces are caller-chosen, so no collision is
// needed to arrange it.
func TestARecordedNonceDoesNotUnlockAnotherRequest(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()

	var refs [2]string
	for i := range refs {
		ref, err := s.Submit(ctx, devTenant, aRequest(string(rune('a'+i))))
		if err != nil {
			t.Fatalf("submitting: %v", err)
		}
		if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
			t.Fatalf("approving: %v", err)
		}
		if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
			Nonce: "shared", Outcome: OutcomeCommitted, RowsAffected: ptr(1),
		}); err != nil {
			t.Fatalf("recording: %v", err)
		}
		refs[i] = ref
	}

	// Both are terminal, each with its own row under the nonce "shared". A
	// DIFFERENT nonce must still be refused on both.
	for _, ref := range refs {
		if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
			Nonce: "fresh", Outcome: OutcomeCommitted, RowsAffected: ptr(1),
		}); !errors.Is(err, ErrWrongState) {
			t.Errorf("%s accepted a fresh nonce: %v", ref, err)
		}
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM executions WHERE tenant_id = $1`, devTenant).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 2 {
		t.Errorf("%d executions recorded for two requests with one attempt each", n)
	}
}

// A recorded nonce may be repeated after EITHER terminal state — that is the
// acknowledgement path a Pilot actually retries on. The shipped tests covered
// the fresh-nonce direction after both, and the repeat direction after
// `executed` only.
func TestARecordedNonceMayBeRepeatedAfterEitherTerminalState(t *testing.T) {
	for name, outcome := range map[string]string{
		"after executed":      OutcomeCommitted,
		"after indeterminate": OutcomeIndeterminate,
	} {
		t.Run(name, func(t *testing.T) {
			s := migrated(t)
			ctx := t.Context()
			ref, err := s.Submit(ctx, devTenant, aRequest("k"))
			if err != nil {
				t.Fatalf("submitting: %v", err)
			}
			if err := s.Approve(ctx, devTenant, ref, "sam", 1); err != nil {
				t.Fatalf("approving: %v", err)
			}
			var rows *int64
			if outcome != OutcomeIndeterminate {
				rows = ptr(2)
			}
			if _, err := s.RecordExecution(ctx, devTenant, ref, Execution{
				Nonce: "n", Outcome: outcome, RowsAffected: rows,
			}); err != nil {
				t.Fatalf("recording: %v", err)
			}

			again, err := s.RecordExecution(ctx, devTenant, ref, Execution{
				Nonce: "n", Outcome: OutcomeRolledBack, RowsAffected: ptr(0),
			})
			if err != nil {
				t.Fatalf("repeating a recorded nonce: %v", err)
			}
			if again.Outcome != outcome {
				t.Errorf("the repeat returned %s; %s is what was stored", again.Outcome, outcome)
			}
		})
	}
}

// References are random, and a test that checks length and uniqueness checks
// neither. Sixteen references share no character position, which a two-byte
// generator or a counter would fail.
func TestReferencesCarryEntropy(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	var refs []string
	for i := range 16 {
		ref, err := s.Submit(ctx, devTenant, aRequest("e"+string(rune('a'+i))))
		if err != nil {
			t.Fatalf("submitting: %v", err)
		}
		refs = append(refs, strings.TrimPrefix(ref, "req_"))
	}
	// Every position must vary across the sixteen. A counter varies only its
	// last characters; a two-byte generator varies only its first few.
	for pos := range len(refs[0]) {
		seen := map[byte]bool{}
		for _, r := range refs {
			seen[r[pos]] = true
		}
		if len(seen) == 1 {
			t.Errorf("character %d is the same in all sixteen references; this is not random", pos)
		}
	}
}
