//go:build integration

package pilot

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/sixfathoms/marque/internal/pilot/postgres"
)

// The outcome vocabulary is the Pilot's whole contract at M1, and every case
// here is about telling apart failures that look alike and are not.
func target(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MARQUE_TEST_DSN")
	if dsn == "" {
		t.Fatal("MARQUE_TEST_DSN is unset; run `make test-integration`, which starts a PostgreSQL and sets it")
	}
	db, err := postgres.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connecting to the target: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// A table of this test's own, so concurrent tests do not collide.
	name := "pilot_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	if _, err := db.ExecContext(t.Context(),
		`DROP TABLE IF EXISTS `+name+`; CREATE TABLE `+name+` (id integer PRIMARY KEY, tier integer NOT NULL)`); err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.WithoutCancel(t.Context()), `DROP TABLE IF EXISTS `+name)
	})
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO `+name+` (id, tier) SELECT i, 1 FROM generate_series(1, 5) AS i`); err != nil {
		t.Fatalf("seeding %s: %v", name, err)
	}
	t.Setenv("PILOT_TEST_TABLE", name)
	return db
}

func table(_ *testing.T) string { return os.Getenv("PILOT_TEST_TABLE") }

func TestACommittedStatementReportsItsRowCount(t *testing.T) {
	db := target(t)
	got, err := Execute(t.Context(), db, `UPDATE `+table(t)+` SET tier = 2 WHERE id <= 3`, postgres.CommitWasRefused)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if got.Outcome != OutcomeCommitted {
		t.Errorf("outcome is %s, want %s", got.Outcome, OutcomeCommitted)
	}
	if got.RowsAffected == nil || *got.RowsAffected != 3 {
		t.Errorf("rows affected is %v, want 3", got.RowsAffected)
	}

	// And it is actually applied, which is the only thing the operator cares
	// about.
	var changed int
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+table(t)+` WHERE tier = 2`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 3 {
		t.Errorf("%d rows have tier 2, want 3 — the outcome said committed", changed)
	}
}

// A failing statement is aborted_not_applied with a count of zero, not
// indeterminate: the transaction rolled back and nothing reached the table, and
// reporting that as "nobody knows" would send a human to look at a database
// that is provably unchanged.
func TestAFailingStatementIsAbortedAndAppliesNothing(t *testing.T) {
	db := target(t)
	got, err := Execute(t.Context(), db, `UPDATE `+table(t)+` SET tier = 'not a number' WHERE id = 1`, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("a statement that cannot run reported no error")
	}
	if got.Outcome != OutcomeAbortedNotApplied {
		t.Errorf("outcome is %s, want %s", got.Outcome, OutcomeAbortedNotApplied)
	}
	if got.RowsAffected == nil || *got.RowsAffected != 0 {
		t.Errorf("rows affected is %v, want 0 — the schema requires a count for every outcome but indeterminate", got.RowsAffected)
	}

	var changed int
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+table(t)+` WHERE tier <> 1`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 0 {
		t.Errorf("%d rows changed despite aborted_not_applied", changed)
	}
}

// A constraint violation is the same shape: it is the statement failing, not
// the commit.
func TestAConstraintViolationIsAborted(t *testing.T) {
	db := target(t)
	got, _ := Execute(t.Context(), db, `INSERT INTO `+table(t)+` (id, tier) VALUES (1, 9)`, postgres.CommitWasRefused)
	if got.Outcome != OutcomeAbortedNotApplied {
		t.Errorf("outcome is %s, want %s", got.Outcome, OutcomeAbortedNotApplied)
	}
}

// The two commit failures that look alike and are not. Both are produced for
// real here, and both go through Execute, because the earlier version of this
// test constructed a Result by hand and asserted that it equalled itself.

// A commit the SERVER refuses is definitively rolled back. A deferred
// constraint is the deterministic way to produce one: the statement succeeds
// and the constraint fires at COMMIT.
func TestARefusedCommitIsRolledBack(t *testing.T) {
	db := target(t)
	ctx := t.Context()
	// UNIQUE, not CHECK: PostgreSQL refuses to mark a CHECK deferrable, and
	// only UNIQUE, PRIMARY KEY, FOREIGN KEY and EXCLUDE may be. Measured, after
	// assuming otherwise.
	// A column of this test's own, distinct per row so the constraint can be
	// created at all — the seeded tier is the same everywhere.
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE `+table(t)+` ADD COLUMN slot integer;
		UPDATE `+table(t)+` SET slot = id;
		ALTER TABLE `+table(t)+` ADD CONSTRAINT slot_unique
			UNIQUE (slot) DEFERRABLE INITIALLY DEFERRED`); err != nil {
		t.Fatalf("adding a deferred constraint: %v", err)
	}

	// Every row to the same slot: legal row by row, and a duplicate the moment
	// the constraint is checked, which is at COMMIT.
	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET slot = 99`, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("a commit the server refuses reported no error")
	}
	if got.Outcome != OutcomeRolledBack {
		t.Errorf("outcome is %s, want %s — the server answered, so this is not indeterminate",
			got.Outcome, OutcomeRolledBack)
	}
	if got.RowsAffected == nil || *got.RowsAffected != 0 {
		t.Errorf("rows affected is %v, want 0", got.RowsAffected)
	}

	var changed int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+table(t)+` WHERE slot = 99`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 0 {
		t.Errorf("%d rows survived a rolled-back commit", changed)
	}
}

// A commit whose answer never arrives is INDETERMINATE — the case EDR-0021
// exists for. The connection is taken away between the statement and the
// commit, deterministically, through the seam in Execute: doing it from outside
// races with the statement, and a test that has to accept either outcome
// cannot tell them apart.
func TestALostCommitIsIndeterminate(t *testing.T) {
	db := target(t)
	ctx := t.Context()

	killer, err := postgres.Open(ctx, os.Getenv("MARQUE_TEST_DSN"))
	if err != nil {
		t.Fatalf("connecting the killer: %v", err)
	}
	defer func() { _ = killer.Close() }()

	// The pool holds one connection, so this is the pid Execute will use.
	var pid int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("reading the backend pid: %v", err)
	}

	afterExec = func() {
		if _, err := killer.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
			t.Errorf("terminating: %v", err)
		}
	}
	t.Cleanup(func() { afterExec = nil })

	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET tier = 4 WHERE id = 1`, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("the commit succeeded although the connection was terminated before it")
	}
	if got.Outcome != OutcomeIndeterminate {
		t.Errorf("outcome is %s, want %s — no answer arrived, so the server did not refuse it and it did not commit",
			got.Outcome, OutcomeIndeterminate)
	}
	if got.RowsAffected != nil {
		t.Errorf("an indeterminate outcome carries %v rows; nobody knows means nobody knows", *got.RowsAffected)
	}
}

// The biconditional every layer enforces: a count exactly when not
// indeterminate.
func TestEveryOutcomeCarriesACountExceptIndeterminate(t *testing.T) {
	db := target(t)
	for name, statement := range map[string]string{
		"committed": `UPDATE ` + table(t) + ` SET tier = 2 WHERE id = 1`,
		"aborted":   `UPDATE ` + table(t) + ` SET tier = 'x' WHERE id = 1`,
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := Execute(t.Context(), db, statement, postgres.CommitWasRefused)
			if (got.Outcome == OutcomeIndeterminate) != (got.RowsAffected == nil) {
				t.Errorf("outcome %s with rows %v breaks the biconditional", got.Outcome, got.RowsAffected)
			}
		})
	}
}
