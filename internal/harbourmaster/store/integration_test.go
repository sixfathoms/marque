//go:build integration

// These tests need a real PostgreSQL, because everything they check is
// behaviour PostgreSQL decides: which function a search_path resolves, whether
// an advisory lock serialises, what a CHECK refuses, what a GRANT grants.
//
// They are behind a build tag so `make test` stays offline, and they are the
// reason the confinement test parses files rather than asking a linter: with no
// tags, `golangci-lint` and `go list` do not read this file at all.
//
// The DSN comes from MARQUE_TEST_DSN and its absence is a FAILURE, not a skip.
// A build-tagged suite that skips itself when unconfigured is the vacuous pass
// this package keeps finding in its own tests: CI reports success having run
// nothing. `make test-integration` provides the DSN.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("MARQUE_TEST_DSN")
	if d == "" {
		t.Fatal("MARQUE_TEST_DSN is unset; run `make test-integration`, which starts a PostgreSQL and sets it")
	}
	return d
}

// freshDB gives each test its own database, so one test's migration cannot
// decide another's outcome.
func freshDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := t.Context()

	admin, err := Open(ctx, dsn(t))
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = admin.Close() }()

	name := "marque_test_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	if len(name) > 60 {
		name = name[:60]
	}
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
		t.Fatalf("dropping %s: %v", name, err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	t.Cleanup(func() {
		a, err := Open(context.WithoutCancel(ctx), dsn(t))
		if err != nil {
			return
		}
		defer func() { _ = a.Close() }()
		_, _ = a.ExecContext(context.WithoutCancel(ctx), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})

	d := replaceDBName(dsn(t), name)
	db, err := Open(ctx, d)
	if err != nil {
		t.Fatalf("connecting to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, d
}

func replaceDBName(d, name string) string {
	fields := strings.Fields(d)
	out := make([]string, 0, len(fields)+1)
	replaced := false
	for _, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			out = append(out, "dbname="+name)
			replaced = true
			continue
		}
		out = append(out, f)
	}
	if !replaced {
		out = append(out, "dbname="+name)
	}
	return strings.Join(out, " ")
}

func TestMigrateThenVerify(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	if err := Verify(ctx, db); !errors.Is(err, ErrPending) {
		t.Fatalf("an empty database should be pending; got %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	if err := Verify(ctx, db); err != nil {
		t.Fatalf("verifying after a migration: %v", err)
	}
	// Idempotent: a second run applies nothing and still verifies.
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating twice: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("schema_migrations holds %d rows, want 1", n)
	}
}

// The defect this file exists for. `SET search_path = public, pg_catalog` puts
// public FIRST, because pg_catalog is searched implicitly first only when it is
// NOT named. A public.length(text) then binds into every length CHECK at DDL
// time, and a constraint holds the pg_proc OID so the shadow cannot even be
// dropped afterwards.
func TestAShadowedBuiltinDoesNotCaptureTheSchema(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx, `
		CREATE FUNCTION public.length(text) RETURNS integer
		LANGUAGE sql IMMUTABLE AS $$ SELECT 9999 $$`); err != nil {
		t.Fatalf("planting the shadow: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating with a shadowed length(): %v", err)
	}

	// If the CHECKs bound to the decoy, this insert succeeds.
	_, err := db.ExecContext(ctx, `
		INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
		VALUES ('development', '', '', '', 't', 'r', 's', 'pending', 'k1')`)
	if err == nil {
		t.Fatal("an empty reference and statement inserted: the length CHECKs bound to public.length, so the search_path pin is inverted")
	}
	if !strings.Contains(err.Error(), "check constraint") {
		t.Errorf("want a check-constraint refusal, got %v", err)
	}
}

// Schema-qualifying public.schema_migrations is what stops a role whose
// search_path names an earlier writable schema verifying against a decoy
// history. Note what this does NOT defend: a decoy FUNCTION cannot shadow a
// pg_catalog one on a normal search_path, because pg_catalog is searched
// implicitly first unless it is named — which is exactly why naming it last was
// the bug. The exposure is a decoy TABLE, and that is what this plants.
func TestADecoyHistoryInAnEarlierSchemaIsNotRead(t *testing.T) {
	db, d := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// A decoy that claims a different migration was applied.
	if _, err := db.ExecContext(ctx, `
		CREATE SCHEMA decoy;
		CREATE TABLE decoy.schema_migrations (
			number integer PRIMARY KEY, name text NOT NULL,
			digest text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now());
		INSERT INTO decoy.schema_migrations (number, name, digest)
		VALUES (1, '0001_something_else.sql', 'not the digest');
		ALTER DATABASE `+currentDB(t, db)+` SET search_path = decoy, public`); err != nil {
		t.Fatalf("planting the decoy: %v", err)
	}

	// A NEW pool, so the altered search_path is in force.
	fresh, err := Open(ctx, d)
	if err != nil {
		t.Fatalf("reconnecting: %v", err)
	}
	defer func() { _ = fresh.Close() }()

	var path string
	if err := fresh.QueryRowContext(ctx, `SELECT current_setting('search_path')`).Scan(&path); err != nil {
		t.Fatalf("reading search_path: %v", err)
	}
	if !strings.Contains(path, "decoy") {
		t.Fatalf("the decoy schema is not on the search_path (%s), so this test proves nothing", path)
	}

	if err := Verify(ctx, fresh); err != nil {
		t.Errorf("a decoy schema_migrations in an earlier schema changed Verify's answer: %v", err)
	}
}

func currentDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	var name string
	if err := db.QueryRowContext(t.Context(), `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatalf("reading the database name: %v", err)
	}
	return `"` + name + `"`
}

func TestConcurrentMigratorsSerialise(t *testing.T) {
	db, d := freshDB(t)
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			own, err := Open(ctx, d)
			if err != nil {
				errs[i] = err
				return
			}
			defer func() { _ = own.Close() }()
			errs[i] = Migrate(ctx, own)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("migrator %d: %v", i, err)
		}
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("schema_migrations holds %d rows after four concurrent migrators, want 1", n)
	}
	var locks int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory'`).Scan(&locks); err != nil {
		t.Fatalf("reading pg_locks: %v", err)
	}
	if locks != 0 {
		t.Errorf("%d advisory locks are still held", locks)
	}
}

// The connection the migrator poisons must not return to the pool carrying its
// session settings.
func TestTheMigratorLeaksNothingOntoThePool(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	for _, guc := range []string{"lock_timeout", "search_path"} {
		for range 8 {
			var v string
			if err := db.QueryRowContext(ctx, `SELECT current_setting($1)`, guc).Scan(&v); err != nil {
				t.Fatalf("reading %s: %v", guc, err)
			}
			switch guc {
			case "lock_timeout":
				if v != "0" {
					t.Errorf("a pooled connection carries lock_timeout=%s from the migrator", v)
				}
			case "search_path":
				if strings.Contains(v, "pg_catalog") {
					t.Errorf("a pooled connection carries the migrator's search_path=%s", v)
				}
			}
		}
	}
}

// lock_timeout must bound every lock wait on the migrator's connection, not
// only the advisory lock. The earlier LOCAL scoping bounded the advisory lock
// and nothing else.
func TestAHeldLockTimesOutRatherThanWaitingForever(t *testing.T) {
	db, d := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	holder, err := Open(ctx, d)
	if err != nil {
		t.Fatalf("connecting the holder: %v", err)
	}
	defer func() { _ = holder.Close() }()

	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning the holder's transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `LOCK TABLE public.schema_migrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}

	// Shorten the bound rather than wait out the real one: at thirty seconds
	// this test either takes thirty seconds or asserts nothing.
	restore := lockWait
	lockWait = "1s"
	t.Cleanup(func() { lockWait = restore })

	// The context deadline must be comfortably LONGER than the lock bound, or
	// the test cannot tell "the bound fired" from "the deadline fired" — which
	// is how the first version of this test failed itself.
	short, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Verify(short, db) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Verify returned nil while an ACCESS EXCLUSIVE lock was held")
		}
		if !strings.Contains(err.Error(), "lock timeout") {
			t.Errorf("want a lock-timeout refusal, got %v", err)
		}
	case <-short.Done():
		t.Fatal("Verify blocked past its deadline: its read is not bounded by lock_timeout")
	}
}

func TestTheGrantsLandAndTheRuntimeRoleOwnsNothing(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	for _, c := range []struct {
		table string
		priv  string
		want  bool
	}{
		{"public.requests", "SELECT", true},
		{"public.requests", "INSERT", true},
		{"public.requests", "UPDATE", true},
		// EDR-0012's shape: nothing the runtime role holds lets it erase.
		{"public.requests", "DELETE", false},
		{"public.approvals", "DELETE", false},
		{"public.executions", "DELETE", false},
		{"public.schema_migrations", "SELECT", true},
		{"public.schema_migrations", "INSERT", false},
	} {
		var got bool
		if err := db.QueryRowContext(ctx,
			`SELECT has_table_privilege('marque_runtime', $1, $2)`, c.table, c.priv).Scan(&got); err != nil {
			t.Fatalf("reading privileges on %s: %v", c.table, err)
		}
		if got != c.want {
			t.Errorf("marque_runtime %s on %s = %v, want %v", c.priv, c.table, got, c.want)
		}
	}

	var owner string
	if err := db.QueryRowContext(ctx,
		`SELECT tableowner FROM pg_tables WHERE schemaname = 'public' AND tablename = 'requests'`).Scan(&owner); err != nil {
		t.Fatalf("reading the owner: %v", err)
	}
	if owner == "marque_runtime" {
		t.Error("marque_runtime owns requests; an owner can grant itself anything, which makes the withheld DELETE decorative (EDR-0012)")
	}
}

// The schema's vocabularies and bounds, each watched refusing. Every one of
// these deleted silently in review with the suite still green.
func TestTheSchemaRefusesWhatItSaysItRefuses(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	insertRequest := func(ref string) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
			VALUES ('development', $1, 'UPDATE accounts SET tier = 2', 'a reason', 't', 'r', 's', 'pending', $1)`, ref)
		return err
	}
	if err := insertRequest("req_ok"); err != nil {
		t.Fatalf("a well-formed request was refused: %v", err)
	}

	for name, exec := range map[string]func() error{
		"an eighth state": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
				VALUES ('development', 'req_8', 's', 'r', 't', 'r', 's', 'contemplating', 'k8')`)
			return err
		},
		"a whitespace-only reference": func() error { return insertRequest("   ") },
		"a request in a tenant that does not exist": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
				VALUES ('nosuch', 'req_t', 's', 'r', 't', 'r', 's', 'pending', 'kt')`)
			return err
		},
		"an approval at stage 0": func() error {
			_, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('development', 'req_ok', 0, 'sam')`)
			return err
		},
		"an approval whose tenant differs from its request's": func() error {
			if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ('other', 'Other')`); err != nil {
				return err
			}
			_, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('other', 'req_ok', 1, 'sam')`)
			return err
		},
		"an anonymous approver": func() error {
			_, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('development', 'req_ok', 1, '')`)
			return err
		},
		"a committed execution with no row count": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'n1', 'committed', NULL)`)
			return err
		},
		"an indeterminate execution WITH a row count": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'n2', 'indeterminate', 3)`)
			return err
		},
		"a negative row count": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'n3', 'committed', -1)`)
			return err
		},
		"a fifth outcome": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'n4', 'probably', 1)`)
			return err
		},
		"an over-long nonce, which is an index-row error if unbounded": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', repeat('n', 4000), 'committed', 1)`)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := exec(); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// EDR-0042 promises the migrator refuses a divergent history. Watched, against
// a real recorded digest rather than a fixture.
func TestAnEditedAppliedMigrationIsRefused(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE public.schema_migrations SET digest = 'not the digest' WHERE number = 1`); err != nil {
		t.Fatalf("editing the record: %v", err)
	}

	err := Verify(ctx, db)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("want ErrSchemaMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "has been edited since it was applied") {
		t.Errorf("the message should name the cause; got %q", err)
	}
	if err := Migrate(ctx, db); !errors.Is(err, ErrSchemaMismatch) {
		t.Errorf("the migrator applied over a divergent history: %v", err)
	}
}

// The queue's partial index is written for one query. An index nothing uses is
// a cost with no benefit, and only the planner can say which it is.
func TestTheQueueIndexIsUsed(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
		SELECT 'development', 'req_' || i, 's', 'r', 't', 'r', 's',
		       (ARRAY['pending','approved','executed','refused'])[1 + i % 4], 'k_' || i
		FROM generate_series(1, 20000) AS i`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	//nolint:misspell // ANALYZE is PostgreSQL's spelling of the statement.
	if _, err := db.ExecContext(ctx, `ANALYZE requests`); err != nil {
		t.Fatalf("analysing: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		EXPLAIN SELECT reference FROM requests
		WHERE tenant_id = 'development' AND state IN ('pending', 'approved')
		ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		t.Fatalf("explaining: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("reading the plan: %v", err)
		}
		fmt.Fprintln(&plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the plan: %v", err)
	}
	if !strings.Contains(plan.String(), "requests_queue") {
		t.Errorf("the queue query does not use requests_queue:\n%s", plan.String())
	}
}
