// Package pilot runs an approved statement against a target and reports what
// happened.
//
// M1's Pilot is the walking skeleton's: it has no marque to verify, no fence to
// apply, no rehearsal to run and no nonce to claim before it starts. What it
// does have is the outcome vocabulary, and getting that right now is most of
// the value — because the outcome is what the control plane records, and an
// outcome that lies is worse than no Pilot at all.
package pilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The four outcomes EDR-0042 decides. Spelled here as strings rather than
// imported from the store, because the Pilot does not link the control plane's
// storage — it reports over the API.
const (
	OutcomeCommitted         = "committed"
	OutcomeRolledBack        = "rolled_back"
	OutcomeAbortedNotApplied = "aborted_not_applied"
	OutcomeIndeterminate     = "indeterminate"
)

// A Result is what one attempt produced.
//
// RowsAffected is absent exactly when the outcome is indeterminate, which is
// the same biconditional the schema and the API enforce. The one thing not to
// do about an outcome meaning "nobody knows" is invent a number for it.
type Result struct {
	Outcome      string
	RowsAffected *int64
}

// Execute runs one statement inside one transaction and reports the outcome.
//
// EDR-0021: there is no retry here, transparent or otherwise. A wrapper that
// replays a write after a failover applies a statement outside the nonce's
// accounting, so a failure whose outcome is unknown is reported as
// INDETERMINATE and left for a human. That is the whole reason the vocabulary
// has a fourth value.
//
// The distinction that matters is WHERE it failed:
//
//   - the statement itself failed, so the transaction rolls back and nothing
//     was applied — aborted_not_applied, zero rows;
//   - the statement succeeded and the server REFUSED the commit — a deferred
//     constraint, say — so it definitively rolled back: rolled_back, zero rows;
//   - the statement succeeded and no answer to the commit arrived —
//     indeterminate, because it may have been applied and the acknowledgement
//     lost.
//
// The last two look identical from here and are not, which is why commitRefused
// is a parameter: telling them apart means reading a driver's error types, and
// EDR-0042 confines those to the adapter. postgres.CommitWasRefused is the one
// implementation.
func Execute(
	ctx context.Context, db *sql.DB, statement string, commitRefused func(error) bool,
) (Result, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		// No transaction was opened, so nothing ran. This is the one failure
		// that is unambiguously "not applied".
		return Result{Outcome: OutcomeAbortedNotApplied, RowsAffected: zero()},
			fmt.Errorf("beginning the transaction: %w", err)
	}

	res, err := tx.ExecContext(ctx, statement)
	if err != nil {
		// Roll back explicitly and report what the rollback did, because a
		// rollback that itself fails leaves the outcome unknown.
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return Result{Outcome: OutcomeIndeterminate},
				fmt.Errorf("the statement failed (%w) and so did the rollback (%w)", err, rbErr)
		}
		return Result{Outcome: OutcomeAbortedNotApplied, RowsAffected: zero()},
			fmt.Errorf("the statement failed and was rolled back: %w", err)
	}

	// Read the count BEFORE committing: after a failed commit there is no
	// result to ask, and asking a driver for one is undefined.
	rows, err := res.RowsAffected()
	if err != nil {
		// The statement ran. Not knowing how many rows it touched is not the
		// same as not knowing whether it ran — but M1's whole notion of a
		// result IS the row count (EDR-0042), so a commit whose effect cannot
		// be described is reported as indeterminate rather than as a success
		// with a made-up number.
		_ = tx.Rollback()
		return Result{Outcome: OutcomeIndeterminate},
			fmt.Errorf("the statement ran and its row count could not be read: %w", err)
	}

	if afterExec != nil {
		afterExec()
	}

	if err := tx.Commit(); err != nil {
		if commitRefused(err) {
			// The server processed the COMMIT and declined it. The transaction
			// rolled back, and saying "nobody knows" would send a human to
			// inspect a database that is provably unchanged.
			return Result{Outcome: OutcomeRolledBack, RowsAffected: zero()},
				fmt.Errorf("the server refused the commit: %w", err)
		}
		// No answer arrived. The commit may have been applied and the
		// acknowledgement lost. EDR-0021 exists because the tempting things
		// here — retry, or assume failure — are each wrong in a way that shows
		// up once, in production, as a statement applied twice or a change
		// reported as absent when it is not.
		return Result{Outcome: OutcomeIndeterminate},
			fmt.Errorf("the statement ran and no answer to the commit arrived: %w", err)
	}

	return Result{Outcome: OutcomeCommitted, RowsAffected: &rows}, nil
}

// afterExec runs between the statement and the commit. It is nil in every
// build that ships and a test sets it to take the connection away.
//
// A seam in production code is a cost, and this one buys the only deterministic
// way to reach the lost-commit branch: terminating a backend from outside races
// with the statement, so the test that tried it accepted either the aborted or
// the indeterminate outcome — and could therefore not tell them apart, which is
// exactly what that branch exists to do. A test accepting either answer is a
// test asserting nothing.
var afterExec func()

func zero() *int64 {
	var z int64
	return &z
}
