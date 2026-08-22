package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A Store is the control plane's own database, and the only place M1 keeps
// state. It holds no target credential and opens no target connection — that is
// EDR-0005's boundary, and it is why every query here is about requests rather
// than about the statements they carry.
type Store struct {
	db *sql.DB

	// newReference is a field so a test can make references predictable. It is
	// not configuration: nothing outside this package sets it.
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
	// DO NOTHING returns no row on conflict, so the reference has to be read
	// back — and a concurrent submitter's row is not visible until it commits,
	// so that read finds nothing either and the whole statement returns no
	// rows. Measured: eight goroutines on one key, five of them got
	// "sql: no rows in result set". The concurrency test found it, the
	// sequential one could not have.
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
// The state change is conditional in SQL. Reading the state and then writing it
// would let two approvals race, and would let an approval land on a request
// that was executed in between.
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

	// DO UPDATE for the same reason as Submit: DO NOTHING returns nothing, and
	// the read-back cannot see a concurrent uncommitted row.
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

	// The request's state follows the STORED outcome, not the argument, so a
	// retry with a different outcome cannot move it.
	next := StateExecuted
	if stored.Outcome == OutcomeIndeterminate {
		next = StateIndeterminate
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
