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
	got, err := Execute(t.Context(), db, `UPDATE `+table(t)+` SET tier = 2 WHERE id <= 3`, postgres.RunOne, postgres.CommitWasRefused)
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
	got, err := Execute(t.Context(), db, `UPDATE `+table(t)+` SET tier = 'not a number' WHERE id = 1`, postgres.RunOne, postgres.CommitWasRefused)
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
	got, _ := Execute(t.Context(), db, `INSERT INTO `+table(t)+` (id, tier) VALUES (1, 9)`, postgres.RunOne, postgres.CommitWasRefused)
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
	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET slot = 99`, postgres.RunOne, postgres.CommitWasRefused)
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
// exists for.
//
// A deferred CONSTRAINT TRIGGER that terminates its own backend reaches it with
// no seam and no race: the trigger fires during COMMIT processing, so the
// connection dies after the statement has run and before any answer arrives.
// An earlier version of this test needed a hook in Execute for that, and a
// reviewer showed the hook was never necessary — the trigger is also a better
// simulation, because it kills *during* the commit rather than before it is
// sent.
func TestALostCommitIsIndeterminate(t *testing.T) {
	db := target(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION `+table(t)+`_die() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN
			PERFORM pg_terminate_backend(pg_backend_pid());
			RETURN NULL;
		END $$;
		CREATE CONSTRAINT TRIGGER die_at_commit
			AFTER UPDATE ON `+table(t)+`
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION `+table(t)+`_die()`); err != nil {
		t.Fatalf("creating the deferred trigger: %v", err)
	}

	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET tier = 4 WHERE id = 1`,
		postgres.RunOne, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("the commit succeeded although the backend terminated during it")
	}
	if got.Outcome != OutcomeIndeterminate {
		t.Errorf("outcome is %s, want %s — the connection died, so the server neither refused it nor confirmed it",
			got.Outcome, OutcomeIndeterminate)
	}
	if got.RowsAffected != nil {
		t.Errorf("an indeterminate outcome carries %v rows; nobody knows means nobody knows", *got.RowsAffected)
	}
}

// One statement means ONE statement. pgx speaks the SIMPLE protocol whenever a
// query has no arguments, and the simple protocol accepts multi-command
// strings — so this ran all four commands, the embedded COMMIT ended the
// transaction, the final error rolled back nothing, and Execute reported
// `aborted_not_applied` with zero rows while five rows had permanently
// changed. The control plane recorded that nothing happened.
//
// This is not "M1 has no grammar". A missing grammar means the statement is not
// understood; this was the OUTCOME being false about what the statement did.
func TestAMultiCommandStringIsRefusedRatherThanRun(t *testing.T) {
	db := target(t)
	ctx := t.Context()

	got, err := Execute(ctx, db,
		`UPDATE `+table(t)+` SET tier = 2; COMMIT; UPDATE `+table(t)+` SET tier = 3; SELECT 1/0`,
		postgres.RunOne, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("a multi-command string was accepted")
	}
	if got.Outcome != OutcomeAbortedNotApplied {
		t.Errorf("outcome is %s, want %s", got.Outcome, OutcomeAbortedNotApplied)
	}

	var changed int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+table(t)+` WHERE tier <> 1`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 0 {
		t.Errorf("%d rows changed while the outcome said nothing was applied", changed)
	}
	// And the refusal names the reason, so an operator is not left guessing.
	if !strings.Contains(err.Error(), "multiple commands") {
		t.Errorf("the refusal does not say why: %v", err)
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
			got, _ := Execute(t.Context(), db, statement, postgres.RunOne, postgres.CommitWasRefused)
			if (got.Outcome == OutcomeIndeterminate) != (got.RowsAffected == nil) {
				t.Errorf("outcome %s with rows %v breaks the biconditional", got.Outcome, got.RowsAffected)
			}
		})
	}
}

// A deferred CONSTRAINT TRIGGER that RAISEs at commit produces ERROR XX000 —
// an SQLSTATE application code chooses. PostgreSQL rolls the transaction back
// and leaves nothing applied, so this is a refusal, and an earlier classifier
// that excluded the XX class called it indeterminate.
func TestARaisedErrorAtCommitIsARefusal(t *testing.T) {
	db := target(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION `+table(t)+`_refuse() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN
			RAISE EXCEPTION 'refused at commit' USING ERRCODE = 'XX000';
		END $$;
		CREATE CONSTRAINT TRIGGER refuse_at_commit
			AFTER UPDATE ON `+table(t)+`
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION `+table(t)+`_refuse()`); err != nil {
		t.Fatalf("creating the deferred trigger: %v", err)
	}

	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET tier = 7 WHERE id = 1`, postgres.RunOne, postgres.CommitWasRefused)
	if err == nil {
		t.Fatal("a commit the trigger refuses reported no error")
	}
	if got.Outcome != OutcomeRolledBack {
		t.Errorf("outcome is %s, want %s — the server answered and rolled it back, so a human has nothing to inspect",
			got.Outcome, OutcomeRolledBack)
	}

	var changed int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+table(t)+` WHERE tier = 7`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 0 {
		t.Errorf("%d rows survived a refused commit", changed)
	}
}

// The pool must be clean and usable after an indeterminate commit.
//
// It asserts the PROPERTY, not the mechanism: removing RunOne's explicit
// discard on this path does not fail it, because a terminated connection is
// already dead and database/sql drops it anyway. The explicit discard is for
// the case a local server will not produce on request — a lost answer over a
// connection that survives.
func TestAConnectionWithAnUnknownTransactionIsDiscarded(t *testing.T) {
	db := target(t)
	ctx := t.Context()

	// The deferred trigger kills the backend during COMMIT: no answer arrives,
	// so whether that connection is still in a transaction is exactly what
	// nobody knows.
	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION `+table(t)+`_die() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN
			PERFORM pg_terminate_backend(pg_backend_pid());
			RETURN NULL;
		END $$;
		CREATE CONSTRAINT TRIGGER die_at_commit
			AFTER UPDATE ON `+table(t)+`
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION `+table(t)+`_die()`); err != nil {
		t.Fatalf("creating the deferred trigger: %v", err)
	}

	got, err := Execute(ctx, db, `UPDATE `+table(t)+` SET tier = 4 WHERE id = 1`,
		postgres.RunOne, postgres.CommitWasRefused)
	if err == nil || got.Outcome != OutcomeIndeterminate {
		t.Fatalf("expected an indeterminate failure, got %s / %v", got.Outcome, err)
	}

	// Drop the trigger through a NEW connection, then check the pool is usable
	// and outside any transaction.
	if _, err := db.ExecContext(ctx,
		`DROP TRIGGER IF EXISTS die_at_commit ON `+table(t)); err != nil {
		t.Fatalf("the pool is unusable after an indeterminate commit: %v", err)
	}
	var state string
	if err := db.QueryRowContext(ctx,
		`SELECT state FROM pg_stat_activity WHERE pid = pg_backend_pid()`).Scan(&state); err != nil {
		t.Fatalf("reading the connection's state: %v", err)
	}
	if strings.HasPrefix(state, "idle in transaction") {
		t.Errorf("a connection went back to the pool %q", state)
	}
}
