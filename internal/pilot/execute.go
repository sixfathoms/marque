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

	"github.com/sixfathoms/marque/internal/pilot/postgres"
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
//   - nothing was opened, or the statement itself failed and rolled back —
//     aborted_not_applied, zero rows;
//   - the statement succeeded and the server REFUSED the commit — a deferred
//     constraint, say — so it definitively rolled back: rolled_back, zero rows;
//   - the statement succeeded and no answer to the commit arrived, or the
//     rollback of a failed statement itself failed — indeterminate, because it
//     may have been applied and the acknowledgement lost.
//
// run and commitRefused are parameters, and the honest reason is smaller than
// the one that was here first: this package already imports the adapter for its
// phase sentinels, so calling into it directly would confine nothing further.
// They are parameters so this function is about CLASSIFYING an outcome rather
// than about producing one, and so a second engine implements two small
// functions rather than editing this switch. Nothing substitutes a fake today;
// all twelve call sites pass postgres.RunOne and postgres.CommitWasRefused.
func Execute(
	ctx context.Context,
	db *sql.DB,
	statement string,
	run func(context.Context, *sql.DB, string) (int64, error),
	commitRefused func(error) bool,
) (Result, error) {
	rows, err := run(ctx, db, statement)
	if err == nil {
		return Result{Outcome: OutcomeCommitted, RowsAffected: &rows}, nil
	}

	switch {
	case errors.Is(err, postgres.ErrBegin), errors.Is(err, postgres.ErrExec):
		// Nothing was opened, or the statement failed and the transaction
		// rolled back. Either way it is provably not applied.
		return Result{Outcome: OutcomeAbortedNotApplied, RowsAffected: zero()}, err

	// UNPINNED, and behaviourally the same as default: below, so deleting
	// either leaves both suites green. Kept separate because the two mean
	// different things and the next engine will want to tell them apart.
	case errors.Is(err, postgres.ErrRollback):
		// The statement failed AND the rollback failed. The statement is not
		// applied — the server aborts the transaction regardless — but the
		// connection's state is unknown, and indeterminate is the conservative
		// report rather than the accurate one.
		return Result{Outcome: OutcomeIndeterminate}, err

	case errors.Is(err, postgres.ErrCommit):
		if commitRefused(err) {
			// The server processed the COMMIT and declined it. It rolled back,
			// and saying "nobody knows" would send a human to inspect a
			// database that is provably unchanged.
			return Result{Outcome: OutcomeRolledBack, RowsAffected: zero()}, err
		}
		// No answer arrived. The commit may have been applied and the
		// acknowledgement lost. EDR-0021 exists because the tempting things
		// here — retry, or assume failure — are each wrong in a way that shows
		// up once, in production, as a statement applied twice or a change
		// reported as absent when it is not.
		return Result{Outcome: OutcomeIndeterminate}, err

	default:
		// A failure the runner did not label — a pool that could not hand out a
		// connection, a panic in the callback. UNPINNED: mutating this to
		// return committed leaves both suites green, because nothing today
		// produces an unlabelled error. Reporting it as anything definite would
		// be inventing knowledge.
		return Result{Outcome: OutcomeIndeterminate}, err
	}
}

func zero() *int64 {
	var z int64
	return &z
}
