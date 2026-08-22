// Package postgres is the Pilot's PostgreSQL adapter, and one of the two
// packages EDR-0042 allows to import a driver.
//
// The Pilot is the only component that touches a target, and the only one that
// holds a target credential (EDR-0005). The Harbourmaster links a PostgreSQL
// driver too — for its own database, since EDR-0013 fixed Marque's state on
// PostgreSQL — so the boundary is not the driver's absence but where a TARGET
// connection may be opened, which is here. EDR-0042 has why.
package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	// The driver, imported for its side effect of registering with
	// database/sql. EDR-0042 permits this package and the Harbourmaster's store
	// to hold one, and nothing else.
	//
	// The comment must stay ADJACENT to the import: it is what silences
	// revive's blank-imports rule, and EDR-0042 records at length why that
	// matters more than it looks.
	"github.com/jackc/pgx/v5/stdlib"
)

// Open connects to a target. The caller owns the pool and must Close it.
//
// Deliberately small: one connection at a time, because M1's Pilot runs one
// statement and a pool that keeps spares open against a production database is
// a cost the operator did not ask for.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the target: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verified positively, for EDR-0005's reason: a pool that connects on first
	// use hides broken credentials until an incident.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to the target: %w", err)
	}
	return db, nil
}

// CommitWasRefused reports whether a failed COMMIT was refused BY THE SERVER,
// as opposed to failing because the connection did not survive to hear the
// answer.
//
// The difference decides between two outcomes that look identical from the
// caller's side and are not:
//
//   - the server replied with an error — a deferred constraint fired, say — so
//     the transaction definitively rolled back and nothing was applied;
//   - the connection failed — so the commit may have been applied and the
//     acknowledgement lost, which is INDETERMINATE and is the case EDR-0021
//     exists for.
//
// Collapsing them would be wrong in both directions: reporting a refused commit
// as indeterminate sends a human to inspect a database that is provably
// unchanged, and reporting a lost one as rolled back tells them a statement did
// not run when it may have.
//
// This lives in the adapter because deciding it means reading a driver's error
// types, and EDR-0042 confines those to here and to the Harbourmaster's store.
func CommitWasRefused(err error) bool {
	if err == nil {
		return false
	}
	// A *pgconn.PgError means the server sent an ErrorResponse. That is
	// necessary and NOT sufficient: terminating a backend also produces one —
	// 57P01, "terminating connection due to administrator command" — and a
	// server that is going away mid-commit has not refused anything, it has
	// stopped answering with a message attached. Found by writing the test that
	// takes the connection away and watching this call it rolled_back.
	//
	// So: a refusal is an ERROR the server chose to return. The SQLSTATE class is
	// deliberately NOT consulted — an earlier version excluded classes 08, 57, 58
	// and XX, and a deferred CONSTRAINT TRIGGER that RAISEs at commit produces
	// an ERROR while rolling the transaction back, in a SQLSTATE the trigger
	// itself chooses, so the class carried no such
	// meaning.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// SEVERITY, and severity alone — read from the UNLOCALISED field.
	//
	// An ERROR response is the server having processed the COMMIT and declined
	// it: the transaction is rolled back and nothing was applied, whatever the
	// SQLSTATE says. FATAL and PANIC are the server ending the session, which
	// is not a decision about this statement.
	//
	// An earlier version also excluded SQLSTATE classes 08, 57, 58 and XX on
	// the theory that those mean "the transaction's fate is unsettled". That
	// was wrong, and a reviewer showed it: a deferred CONSTRAINT TRIGGER that
	// RAISEs at commit returns a SQLSTATE it chooses — P0001 by default, or
	// whatever ERRCODE names — PostgreSQL rolls the transaction
	// back and leaves zero rows, and this called it indeterminate — sending a
	// human to inspect a database that is provably unchanged. Application code
	// chooses that SQLSTATE, so the class cannot carry the meaning the class
	// list assumed.
	//
	// PgError.Severity is LOCALISED: a server with lc_messages set to anything
	// but English returns a translated word, so comparing it against "FATAL"
	// fails open — a dying session would be classified as a refusal, and a
	// statement whose fate is unknown reported as definitely rolled back. A
	// reviewer constructed exactly that. SeverityUnlocalized is the English
	// one, and is empty against a server too old to send it, which is what the
	// fallback is for.
	// POSITIVELY "ERROR", not "not FATAL and not PANIC".
	//
	// SeverityUnlocalized exists only from PostgreSQL 9.6, so the fallback can
	// be reading a TRANSLATED word — and a negative check treats "SCHWERWIEGEND"
	// as a refusal, reporting a commit whose fate is unknown as definitely
	// rolled back. Checking positively fails the other way: an old server in
	// another language yields indeterminate, which sends a human to look at a
	// database instead of telling them there is nothing to look at.
	severity := pgErr.SeverityUnlocalized
	if severity == "" {
		severity = pgErr.Severity
	}
	return severity == "ERROR"
}

// Phase sentinels, so a caller can tell WHERE a run failed without importing a
// driver's error types. internal/pilot classifies the outcome from these.
var (
	// ErrBegin: no transaction was opened, so nothing ran.
	ErrBegin = errors.New("beginning the transaction")
	// ErrExec: the statement itself failed, inside a transaction that is being
	// rolled back.
	ErrExec = errors.New("running the statement")
	// ErrCommit: the statement succeeded and the commit did not report success.
	ErrCommit = errors.New("committing")
	// ErrRollback: the statement failed AND the rollback failed, so the
	// connection's state is unknown.
	ErrRollback = errors.New("rolling back")
)

// RunOne executes exactly one statement inside one transaction and reports the
// rows it affected.
//
// **The extended protocol, with zero parameters, and that is the whole point.**
// pgx uses the SIMPLE protocol whenever a query has no arguments, and the
// simple protocol accepts multi-command strings — so
//
//	UPDATE accounts SET tier = 2; COMMIT; UPDATE accounts SET tier = 3; SELECT 1/0
//
// ran all four commands, the embedded COMMIT ended the transaction, the final
// error rolled back nothing, and Execute reported `aborted_not_applied` with
// zero rows while five rows had permanently changed. The control plane recorded
// that nothing happened. A reviewer found it; it is the worst failure this
// package can have, because an outcome that lies is worse than no Pilot at all.
//
// ExecParams speaks the extended protocol whatever the argument count, and
// PostgreSQL refuses a multi-command string there — `cannot insert multiple
// commands into a prepared statement`, SQLSTATE 42601. That refusal arrives as
// an ordinary statement error, which is exactly the right shape: nothing ran.
//
// It is NOT a substitute for parsing the statement. M1 has no grammar and this
// does not give it one; it stops one statement from silently being four.
//
// The whole transaction runs inside one Raw callback because database/sql does
// not permit the driver connection to outlive it.
func RunOne(ctx context.Context, db *sql.DB, statement string) (int64, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrBegin, err)
	}
	defer func() { _ = conn.Close() }()

	var rows int64
	rawErr := conn.Raw(func(driverConn any) error {
		stdConn, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("%w: the target's connection is %T, not pgx's", ErrBegin, driverConn)
		}
		pg := stdConn.Conn().PgConn()

		if _, err := pg.ExecParams(ctx, "BEGIN", nil, nil, nil, nil).Close(); err != nil {
			return fmt.Errorf("%w: %w", ErrBegin, err)
		}

		tag, execErr := pg.ExecParams(ctx, statement, nil, nil, nil, nil).Close()
		if execErr != nil {
			// UNPINNED: deleting this ROLLBACK leaves both suites green,
			// because pgx discards a connection whose TxStatus is not 'I'. It
			// earns its place anyway — measured, without it the pooled
			// connection sits `idle in transaction (aborted)` against the
			// server, holding its snapshot, until the next borrow or the
			// five-minute lifetime bound retires it.
			if _, rbErr := pg.ExecParams(ctx, "ROLLBACK", nil, nil, nil, nil).Close(); rbErr != nil {
				// The connection's transaction state is now unknown, so say
				// so explicitly rather than rely on the pool noticing.
				//
				// BELT, and an earlier comment here overstated it: pgx
				// v5.10.0's ResetSession already returns driver.ErrBadConn when
				// TxStatus is not 'I', so a connection left in or aborted from
				// a transaction is discarded anyway — measured, a new backend
				// pid each time. Wrapping driver.ErrBadConn makes the intent
				// local and legible instead of depending on a driver detail
				// that a version bump could change. No test reaches this path;
				// making ROLLBACK fail on a live connection is not something a
				// local server does on request.
				return fmt.Errorf("%w: %w (and the statement failed: %w): %w",
					ErrRollback, rbErr, execErr, driver.ErrBadConn)
			}
			return fmt.Errorf("%w: %w", ErrExec, execErr)
		}
		rows = tag.RowsAffected()

		if _, err := pg.ExecParams(ctx, "COMMIT", nil, nil, nil, nil).Close(); err != nil {
			if CommitWasRefused(err) {
				// The server answered: the transaction is over and the
				// connection is clean.
				return fmt.Errorf("%w: %w", ErrCommit, err)
			}
			// No answer arrived, so whether this connection is still in a
			// transaction is exactly what nobody knows. Discard it.
			//
			// Belt, mostly: the usual way to get here is a connection that is
			// already dead, which database/sql discards on its own — removing
			// this line does not fail the test below. It earns its place for
			// the case where the commit's answer is lost but the connection
			// survives, which is a network partition rather than anything a
			// test can arrange locally.
			return fmt.Errorf("%w: %w: %w", ErrCommit, err, driver.ErrBadConn)
		}
		return nil
	})
	return rows, rawErr
}
