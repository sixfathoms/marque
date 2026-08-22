package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A Store is the control plane's own database, and the only place M1 keeps
// state. It holds no target credential and opens no target connection — that is
// EDR-0005's boundary, and it is why every query here is about requests rather
// than about the statements they carry.
type Store struct {
	db *sql.DB

	// newReference is a field so a test can make references predictable.
	//
	// No test sets it today, which by this repository's own standard — stated
	// in internal/pilot, "a seam in production code is a cost" — makes it a
	// cost buying nothing yet. It is kept because the alternative is a package
	// variable, and it is named here so the next person knows it is unused
	// rather than assuming it is load-bearing.
	newReference func() (string, error)
}

// New wraps a pool. The caller still owns the pool and must Close it.
func New(db *sql.DB) *Store {
	return &Store{db: db, newReference: newReference}
}

// Errors a caller has to tell apart. Everything else is a fault.
var (
	// ErrNoSuchRequest is returned when a reference names nothing in this
	// tenant. EDR-0038: a reference is an identifier, not a capability, and
	// resolving one must not confirm its own existence to someone without
	// entitlement — so this is the 404 that the API turns into NotFound, never
	// a 403.
	ErrNoSuchRequest = errors.New("no such request")

	// ErrWrongState is returned when a request is real but not in a state the
	// transition allows: approving something already executed, recording an
	// execution against something never approved.
	ErrWrongState = errors.New("the request is not in a state that allows this")
)

// Request is one row of `requests`. The tenant is not a field: it comes from
// the caller's authenticated principal at M4 and from configuration until then
// (EDR-0025), and putting it here would make it look like something a caller
// could choose.
type Request struct {
	Reference      string
	Statement      string
	Target         string
	Role           string
	Submitter      string
	Reason         string
	State          string
	IdempotencyKey string
	CreatedAt      time.Time
}

// The seven states of EDR-0038. Spelled here as well as in the schema's CHECK
// because Go code comparing against a typo would otherwise fail at the database
// rather than at the comparison.
const (
	StatePending       = "pending"
	StateVerifying     = "verifying"
	StateApproved      = "approved"
	StateRefused       = "refused"
	StateExpired       = "expired"
	StateExecuted      = "executed"
	StateIndeterminate = "indeterminate"
)

// The four outcomes EDR-0042 decides. `in_progress` is deliberately not among
// them: a control-plane report is written when an attempt ENDS.
const (
	OutcomeCommitted         = "committed"
	OutcomeRolledBack        = "rolled_back"
	OutcomeAbortedNotApplied = "aborted_not_applied"
	OutcomeIndeterminate     = "indeterminate"
)

// newReference makes the `req_…` an operator pastes into chat (EDR-0038).
//
// Random, not sequential: a sequential one leaks how many requests exist and
// invites guessing the neighbours. Base32 without padding and lower-cased, for
// looks rather than for safety — lookups are case-sensitive and normalise
// nothing, so a client that capitalises a pasted reference gets a NotFound. An
// earlier version of this comment claimed the lower-casing survived that, which
// it does not.
func newReference() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a reference: %w", err)
	}
	return "req_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
}

// Submit records a request and returns its reference.
//
// Idempotent on the caller's key: submitting twice with the same key is one
// request, and returns the reference of the first. That is done in ONE
// statement rather than a read followed by a write, because two callers racing
// on the same key would both read nothing and both insert.
func (s *Store) Submit(ctx context.Context, tenant string, r Request) (string, error) {
	reference, err := s.newReference()
	if err != nil {
		return "", err
	}

	// DO UPDATE with a no-op SET, not DO NOTHING.
	//
	// DO NOTHING returns no row on conflict, so the reference had to be read
	// back in the same statement — and that is where it failed: eight
	// goroutines on one key, five of them got "sql: no rows in result set".
	//
	// The reason is a snapshot, not a lock. DO NOTHING does WAIT for the
	// conflicting transaction — measured: two seconds, and a LATER statement
	// then sees the committed row perfectly well. But the fallback SELECT was
	// part of the same statement, and a statement runs on one snapshot taken
	// before the wait began, so it looked at a moment when the other row did
	// not yet exist. An earlier version of this comment said the row "is not
	// visible until it commits", which is wrong in a way that matters: it would
	// send someone to add a retry rather than to stop using two snapshots.
	//
	// DO UPDATE takes the row lock, waits for the other transaction, and
	// RETURNING then yields the surviving row whichever way the race went.
	// Setting the key to itself is the cheapest no-op that makes the row the
	// statement's target.
	var stored string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO requests
			(tenant_id, reference, statement, target, role, submitter, reason, state, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, idempotency_key)
			DO UPDATE SET idempotency_key = requests.idempotency_key
		RETURNING reference`,
		tenant, reference, r.Statement, r.Target, r.Role, r.Submitter, r.Reason, StatePending, r.IdempotencyKey,
	).Scan(&stored)
	if err != nil {
		return "", fmt.Errorf("submitting: %w", err)
	}
	return stored, nil
}

// Request reads one request. It is scoped to the tenant, and a reference from
// another tenant is ErrNoSuchRequest rather than a permission error.
func (s *Store) Request(ctx context.Context, tenant, reference string) (Request, error) {
	var r Request
	err := s.db.QueryRowContext(ctx, `
		SELECT reference, statement, target, role, submitter, reason, state, idempotency_key, created_at
		FROM requests WHERE tenant_id = $1 AND reference = $2`, tenant, reference,
	).Scan(&r.Reference, &r.Statement, &r.Target, &r.Role, &r.Submitter,
		&r.Reason, &r.State, &r.IdempotencyKey, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNoSuchRequest
	}
	if err != nil {
		return Request{}, fmt.Errorf("reading %s: %w", reference, err)
	}
	return r, nil
}

// Approve records an approval and moves the request to approved.
//
// Naturally idempotent, per the proto's declaration: the row is keyed
// (tenant, request, stage, approver), so the same person approving the same
// stage twice is one approval. M1 has no chain, so any approval is sufficient
// — EDR-0030's per-stage thresholds arrive with signing at M3.
//
// The safety is the `FOR UPDATE` below, and naming it precisely matters because
// an earlier version of this comment named something else. It said "the state
// change is conditional in SQL", and it is not: the UPDATE has no state
// predicate. What this does IS read-then-write — the thing that comment said
// would be wrong — and it is safe only because the read takes a row lock, so
// the second caller blocks until the first commits and then re-reads.
//
// Measured with the lock removed: an approval moved an already-executed request
// back to approved in nineteen rounds of twenty, and four concurrent executions
// landed against one approval in thirty-nine of forty. An UNBARRIERED
// concurrency test passes against both, which is why the test that pins this
// starts every goroutine from a barrier.
func (s *Store) Approve(ctx context.Context, tenant, reference, approver string, stage uint32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning the approval: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Existence first, so a reference that names nothing is ErrNoSuchRequest
	// rather than ErrWrongState. The two are different answers and a caller
	// acts differently on each.
	var state string
	err = tx.QueryRowContext(ctx,
		`SELECT state FROM requests WHERE tenant_id = $1 AND reference = $2 FOR UPDATE`,
		tenant, reference).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSuchRequest
	}
	if err != nil {
		return fmt.Errorf("locking %s: %w", reference, err)
	}
	if state != StatePending && state != StateApproved {
		return fmt.Errorf("%w: %s is %s", ErrWrongState, reference, state)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO approvals (tenant_id, reference, stage, approver)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, reference, stage, approver) DO NOTHING`,
		tenant, reference, stage, approver); err != nil {
		return fmt.Errorf("recording the approval: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE requests SET state = $3 WHERE tenant_id = $1 AND reference = $2`,
		tenant, reference, StateApproved); err != nil {
		return fmt.Errorf("approving %s: %w", reference, err)
	}
	// UNPINNED, stated rather than implied: replacing this with `_ = tx.Commit()`
	// makes Approve report success having recorded nothing, and no test
	// notices. Reaching it needs a commit that fails, which needs either a
	// deferred constraint this schema does not have or a seam this function
	// does not want. Named here so it is a known gap rather than an assumed
	// guarantee.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the approval: %w", err)
	}
	return nil
}

// An Execution is one attempt's report, as the control plane recorded it. It is
// NOT EDR-0011's ledger: that is Pilot-local, claimed BEFORE the statement runs,
// and carries an incarnation. This is written when an attempt has ended.
type Execution struct {
	Nonce        string
	Outcome      string
	RowsAffected *int64
	At           time.Time
}

// RecordExecution stores one attempt and returns what is stored.
//
// Keyed on the nonce, per the proto: repeating a nonce returns the stored
// outcome rather than recording a second attempt, which is what makes retrying
// the REPORT safe. It does not make retrying the EXECUTION safe: if the first
// report never lands the request is still approved, and the next attempt runs
// the statement again. EDR-0011's ledger is what would prevent that, by
// claiming the nonce before the statement runs; M1 has none (issue #34).
//
// The return value is the STORED row, not the argument, so a caller
// that retries with a different outcome learns what was actually recorded
// instead of believing its own second answer.
func (s *Store) RecordExecution(ctx context.Context, tenant, reference string, e Execution) (Execution, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Execution{}, fmt.Errorf("beginning the report: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var state string
	err = tx.QueryRowContext(ctx,
		`SELECT state FROM requests WHERE tenant_id = $1 AND reference = $2 FOR UPDATE`,
		tenant, reference).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNoSuchRequest
	}
	if err != nil {
		return Execution{}, fmt.Errorf("locking %s: %w", reference, err)
	}
	// A statement nobody approved must not be reportable as executed: that
	// would let the Pilot's report be the only record that anything happened.
	//
	// And once a request is terminal, only a nonce ALREADY RECORDED may be
	// reported again. Accepting a fresh one moved an executed request to
	// indeterminate and back — a reviewer did exactly that, twice, and the last
	// report won. A repeat is an acknowledgement of something recorded; a new
	// attempt against a finished request is a second execution nobody approved.
	if state != StateApproved {
		if state != StateExecuted && state != StateIndeterminate {
			return Execution{}, fmt.Errorf("%w: %s is %s", ErrWrongState, reference, state)
		}
		var known bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM executions
				WHERE tenant_id = $1 AND reference = $2 AND nonce = $3)`,
			tenant, reference, e.Nonce).Scan(&known); err != nil {
			return Execution{}, fmt.Errorf("looking for nonce %s: %w", e.Nonce, err)
		}
		if !known {
			return Execution{}, fmt.Errorf(
				"%w: %s is %s, and nonce %q is not one of its recorded attempts",
				ErrWrongState, reference, state, e.Nonce)
		}
	}

	// DO UPDATE for the same reason as Submit.
	var stored Execution
	err = tx.QueryRowContext(ctx, `
		INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, reference, nonce)
			DO UPDATE SET nonce = executions.nonce
		RETURNING nonce, outcome, rows_affected, at`,
		tenant, reference, e.Nonce, e.Outcome, e.RowsAffected,
	).Scan(&stored.Nonce, &stored.Outcome, &stored.RowsAffected, &stored.At)
	if err != nil {
		return Execution{}, fmt.Errorf("recording the execution: %w", err)
	}

	// A repeat that CONTRADICTS what is stored is refused, rather than silently
	// acknowledged.
	//
	// Returning the stored row is right for a Pilot retrying the same report
	// after a timeout — it learns what was recorded. It is wrong when the
	// second call is describing a DIFFERENT attempt, and moving clean failures
	// back to `approved` made that reachable: a re-run under the same nonce
	// commits, reports `committed`, and this handed back the first attempt's
	// `aborted_not_applied` with zero rows. The statement ran, three rows
	// changed, the control plane recorded that nothing happened, and the
	// command exited 0. A reviewer walked it end to end.
	//
	// A nonce identifies ONE attempt (EDR-0011). Two different outcomes under
	// one nonce means the caller has reused it for a second attempt, and the
	// honest answer is to refuse and say so.
	if stored.Outcome != e.Outcome || !sameRows(stored.RowsAffected, e.RowsAffected) {
		return Execution{}, fmt.Errorf(
			"%w: nonce %q is recorded as %s (rows %s) and was reported as %s (rows %s); "+
				"a nonce identifies one attempt, so a re-run needs a new one",
			ErrWrongState, e.Nonce,
			stored.Outcome, rowsText(stored.RowsAffected),
			e.Outcome, rowsText(e.RowsAffected))
	}

	// The request's state follows the STORED outcome, and only moves while the
	// request is still approved — the guard below. A retry cannot move it in
	// either direction: a contradicting one is refused above, and an agreeing
	// one against a terminal request is acknowledged without touching the
	// state.
	//
	// Four outcomes, three destinations. `committed` is the only one that
	// executed anything, and `indeterminate` is the truthful terminal state a
	// human resolves (EDR-0011). The other two are **provably not applied** —
	// `aborted_not_applied` is an error received before COMMIT, `rolled_back` is
	// the server refusing it — so the request goes back to being RUNNABLE
	// rather than terminal.
	//
	// An earlier version sent all three of the non-indeterminate outcomes to
	// `executed`. That made the control plane's record say a change had
	// happened that provably had not, and left the request a dead end: it could
	// not be approved again, and a fresh nonce was refused, so the real result
	// of a re-run was discarded. A reviewer walked exactly that sequence.
	//
	// Note where this departs from EDR-0011: that record has a clean abort
	// re-run under the SAME nonce, against a Pilot-local ledger with an attempt
	// count. M1 has no ledger, and a repeat of a recorded nonce here returns
	// the stored outcome — so an M1 re-run needs a new nonce, which staying
	// approved is what permits. Issue #34 is where the ledger belongs.
	next := ""
	switch stored.Outcome {
	case OutcomeCommitted:
		next = StateExecuted
	case OutcomeIndeterminate:
		next = StateIndeterminate
	case OutcomeAbortedNotApplied, OutcomeRolledBack:
		next = StateApproved
	default:
		return Execution{}, fmt.Errorf(
			"stored outcome %q has no request state; the four EDR-0042 decides all do", stored.Outcome)
	}
	// ONLY a request still approved may be moved.
	//
	// A repeat against a terminal request acknowledges an OLD attempt, and
	// applying its outcome un-executes a request a later attempt already
	// committed. The two fixes above combined to open exactly that: clean
	// failures return to `approved`, and an agreeing repeat is blessed as an
	// acknowledgement, so
	//
	//	n1 aborted_not_applied/0  → approved
	//	n2 committed/3            → executed, three rows changed
	//	n1 again, identical       → approved   ← un-executed
	//	n3 committed/3            → executed, the SAME three rows changed again
	//
	// walked over the wire by a reviewer. The contradiction guard cannot catch
	// it, because the duplicate agrees with what is stored; what is wrong is
	// the ordering, and the state is the thing that must not move.
	if state != StateApproved {
		if err := tx.Commit(); err != nil {
			return Execution{}, fmt.Errorf("committing the acknowledgement: %w", err)
		}
		return stored, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE requests SET state = $3 WHERE tenant_id = $1 AND reference = $2`,
		tenant, reference, next); err != nil {
		return Execution{}, fmt.Errorf("updating %s: %w", reference, err)
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, fmt.Errorf("committing the report: %w", err)
	}
	return stored, nil
}

// sameRows compares two optional counts.
//
// UNPINNED, and unreachable: the nil-mismatch branch would need one report with
// a count and another without under one nonce, and rows-absent-iff-indeterminate
// is enforced by the schema's CHECK and by the API. Mutating it to `return true`
// leaves both suites green. Labelled rather than tested, per the convention in
// this package.
func sameRows(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func rowsText(n *int64) string {
	if n == nil {
		return "absent"
	}
	return strconv.FormatInt(*n, 10)
}
