//go:build integration

// These tests need a real PostgreSQL, because everything they check is
// behaviour PostgreSQL decides: which function a search_path resolves, whether
// an advisory lock serialises, what a CHECK refuses, what a GRANT grants.
//
// They are behind a build tag so `make test` stays offline. `make lint` passes
// `--build-tags integration`, so the linter reads this file too — the claim
// that it could not was one of the several EDR-0042 retracts.
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
	if err == nil {
		// A schema named after the migrating role, present in every test
		// database. PostgreSQL's default search_path is `"$user", public`, and
		// an unqualified CREATE TABLE targets the first schema in that list
		// that EXISTS — so without this, `"$user"` never resolves and the
		// migration lands in public whether it is pinned or not. A reviewer
		// deleted the pin entirely and watched the whole suite pass.
		if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS "`+currentUser(t, db)+`"`); err != nil {
			t.Fatalf("creating the role's own schema: %v", err)
		}
	}
	if err != nil {
		t.Fatalf("connecting to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, d
}

func currentUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	var user string
	if err := db.QueryRowContext(t.Context(), `SELECT current_user`).Scan(&user); err != nil {
		t.Fatalf("reading current_user: %v", err)
	}
	return user
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
// The pin's real job, and the one no test covered: an unqualified CREATE TABLE
// targets the first EXISTING schema on the search_path, and the default path
// leads with `"$user"`. With a schema of that name present — which freshDB now
// guarantees — every table in an unpinned migration lands there instead of
// public, `Verify` still passes because it qualifies schema_migrations, and the
// runtime role then finds nothing.
func TestTheMigrationLandsInPublic(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	for _, table := range []string{"schema_migrations", "tenants", "requests", "approvals", "executions"} {
		var schema string
		err := db.QueryRowContext(ctx,
			`SELECT schemaname FROM pg_tables WHERE tablename = $1`, table).Scan(&schema)
		if err != nil {
			t.Errorf("looking for %s: %v", table, err)
			continue
		}
		if schema != "public" {
			t.Errorf("%s landed in schema %q, not public: the search_path pin is not in force", table, schema)
		}
	}
}

func TestAShadowedBuiltinDoesNotCaptureTheSchema(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	// 50, deliberately IN RANGE for every bound. A decoy returning 9999 makes
	// every CHECK false, so the insert below fails either way and the assertion
	// cannot tell the pin from the decoy — a reviewer found the test was being
	// carried by the migration's own seed row failing instead.
	if _, err := db.ExecContext(ctx, `
		CREATE FUNCTION public.length(text) RETURNS integer
		LANGUAGE sql IMMUTABLE AS $$ SELECT 50 $$`); err != nil {
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

	// The pools stay OPEN until after pg_locks is read. Closing them first —
	// which the earlier version did, by deferring Close inside each goroutine —
	// ends every migrator's session, so no advisory lock could be held by the
	// time the assertion ran and it could not fail. A reviewer deleted the
	// unlock defer AND the connection discard together and watched it pass.
	pools := make([]*sql.DB, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range pools {
		own, err := Open(ctx, d)
		if err != nil {
			t.Fatalf("connecting migrator %d: %v", i, err)
		}
		pools[i] = own
		t.Cleanup(func() { _ = own.Close() })
	}
	for i, own := range pools {
		wg.Add(1)
		go func() {
			defer wg.Done()
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

	// Scoped to THIS database and THIS key. pg_locks is cluster-wide, so an
	// unscoped count reports locks taken by anything else on the server —
	// including another test, which makes it a flake rather than a check.
	var locks int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory'
		  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&locks); err != nil {
		t.Fatalf("reading pg_locks: %v", err)
	}
	if locks != 0 {
		t.Errorf("%d advisory locks are still held in this database", locks)
	}
}

// The connection the migrator poisons must not return to the pool carrying its
// session settings.
func TestTheMigratorLeaksNothingOntoThePool(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	// Captured BEFORE, and compared for equality. Asserting the value merely
	// lacks "pg_catalog" is an assertion that can never fail: the default is
	// `"$user", public` and the pin is `public`, so neither contains it. A
	// reviewer removed the connection discard and the timeout together and
	// watched this pass while the connection provably carried the migrator's
	// search_path back to the pool.
	before := map[string]string{}
	for _, guc := range []string{"lock_timeout", "search_path"} {
		var v string
		if err := db.QueryRowContext(ctx, `SELECT current_setting($1)`, guc).Scan(&v); err != nil {
			t.Fatalf("reading %s: %v", guc, err)
		}
		before[guc] = v
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	for guc, want := range before {
		for range 8 {
			var got string
			if err := db.QueryRowContext(ctx, `SELECT current_setting($1)`, guc).Scan(&got); err != nil {
				t.Fatalf("reading %s: %v", guc, err)
			}
			if got != want {
				t.Errorf("a pooled connection carries the migrator's %s: %q, want %q", guc, got, want)
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

// The twin of the test above, and the one that matters: the regression round 4
// fixed was in MIGRATE's timeout, and the existing test calls Verify. A
// reviewer reverted the migrator's set_config to the transaction-local form it
// had when it covered nothing, and the whole suite stayed green.
func TestMigrateTimesOutRatherThanWaitingForever(t *testing.T) {
	db, d := freshDB(t)
	ctx := t.Context()

	restore := lockWait
	lockWait = "1s"
	t.Cleanup(func() { lockWait = restore })

	// Bootstrap the history table first, so the lock below is contended by the
	// migrator's own reads and DDL rather than by its creation.
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

	short, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Migrate(short, db) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Migrate returned nil while an ACCESS EXCLUSIVE lock was held on schema_migrations")
		}
		if !strings.Contains(err.Error(), "lock timeout") {
			t.Errorf("want a lock-timeout refusal, got %v", err)
		}
	case <-short.Done():
		t.Fatal("Migrate blocked past its deadline: its lock waits are not bounded")
	}
}

func TestTheGrantsLandAndTheRuntimeRoleOwnsNothing(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// Every table, every privilege. Sampling SELECT/INSERT/UPDATE on `requests`
	// alone meant removing the grants on tenants, approvals and executions left
	// the suite green.
	var cases []struct {
		table string
		priv  string
		want  bool
	}
	add := func(table, priv string, want bool) {
		cases = append(cases, struct {
			table string
			priv  string
			want  bool
		}{table, priv, want})
	}
	for _, table := range []string{"public.tenants", "public.requests", "public.approvals", "public.executions"} {
		add(table, "SELECT", true)
		add(table, "INSERT", true)
		add(table, "UPDATE", true)
		// EDR-0012's shape: nothing the runtime role holds lets it erase.
		add(table, "DELETE", false)
		add(table, "TRUNCATE", false)
		// REFERENCES lets a role point a foreign key at the table, TRIGGER lets
		// it attach code that runs on every write, and MAINTAIN lets it VACUUM,
		// ANALYSE, REINDEX and CLUSTER. "Every privilege" named five of eight,
		// then seven of eight, each time because someone counted from memory
		// instead of asking the server.
		//
		// Ask it with aclexplode, not information_schema. Measured on 18.3:
		// aclexplode over pg_class.relacl after GRANT ALL reports eight, and
		// information_schema.table_privileges reports seven — it omits MAINTAIN
		// because that view is the SQL standard's and MAINTAIN is not in it.
		// Whoever next checks this count will reach for information_schema and
		// conclude there are seven.
		add(table, "REFERENCES", false)
		add(table, "TRIGGER", false)
		add(table, "MAINTAIN", false)
	}
	for _, priv := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER", "MAINTAIN"} {
		add("public.schema_migrations", priv, false)
	}
	add("public.schema_migrations", "SELECT", true)

	for _, c := range cases {
		var got bool
		if err := db.QueryRowContext(ctx,
			`SELECT has_table_privilege('marque_runtime', $1, $2)`, c.table, c.priv).Scan(&got); err != nil {
			t.Fatalf("reading privileges on %s: %v", c.table, err)
		}
		if got != c.want {
			t.Errorf("marque_runtime %s on %s = %v, want %v", c.priv, c.table, got, c.want)
		}
	}

	// Every table, for the same reason.
	for _, table := range []string{"schema_migrations", "tenants", "requests", "approvals", "executions"} {
		var owner string
		if err := db.QueryRowContext(ctx,
			`SELECT tableowner FROM pg_tables WHERE schemaname = 'public' AND tablename = $1`, table).Scan(&owner); err != nil {
			t.Errorf("reading the owner of %s: %v", table, err)
			continue
		}
		if owner == runtimeRole {
			t.Errorf("%s owns %s; an owner can grant itself anything, which makes the withheld DELETE decorative (EDR-0012)",
				runtimeRole, table)
		}
	}
}

// EDR-0012's non-ownership is only checkable at the moment the tables are
// created, so the migrator refuses to run as the runtime role at all. Nothing
// exercised that: the harness always migrates as the superuser, so the guard
// could be neutered green, and the ownership assertions above were asserting a
// property of the DSN rather than of the code.
func TestMigratingAsTheRuntimeRoleIsRefused(t *testing.T) {
	db, d := freshDB(t)
	ctx := t.Context()

	if _, err := db.ExecContext(ctx,
		`ALTER ROLE `+runtimeRole+` LOGIN PASSWORD 'marque'`); err != nil {
		t.Fatalf("giving the runtime role a login: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.WithoutCancel(ctx), `ALTER ROLE `+runtimeRole+` NOLOGIN`)
	})
	if _, err := db.ExecContext(ctx,
		`GRANT CREATE, USAGE ON SCHEMA public TO `+runtimeRole); err != nil {
		t.Fatalf("granting CREATE: %v", err)
	}

	asRuntime, err := Open(ctx, replaceUser(d, runtimeRole))
	if err != nil {
		t.Fatalf("connecting as %s: %v", runtimeRole, err)
	}
	defer func() { _ = asRuntime.Close() }()

	err = Migrate(ctx, asRuntime)
	if err == nil {
		t.Fatal("the migrator ran as the runtime role, which would make it the owner of every table")
	}
	if !strings.Contains(err.Error(), runtimeRole) {
		t.Errorf("the refusal should name the role; got %v", err)
	}

	// And it must refuse BEFORE creating anything: the bootstrap CREATE TABLE
	// assigns ownership, and IF NOT EXISTS then preserves it for every later
	// run. A reviewer watched the SQL-level guard refuse while leaving
	// schema_migrations owned by the runtime role.
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT pg_catalog.to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("looking for schema_migrations: %v", err)
	}
	if exists {
		var owner string
		_ = db.QueryRowContext(ctx,
			`SELECT tableowner FROM pg_tables WHERE tablename = 'schema_migrations'`).Scan(&owner)
		t.Errorf("the refused run still created schema_migrations, owned by %q", owner)
	}
}

func replaceUser(d, user string) string {
	fields := strings.Fields(d)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "user=") {
			out = append(out, "user="+user)
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// The schema's vocabularies and bounds, each watched refusing. Every one of
// these deleted silently in review with the suite still green.
func TestTheSchemaRefusesWhatItSaysItRefuses(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// Distinct values, so exactly one CHECK can refuse each case. Passing the
	// same string as reference AND idempotency_key meant deleting either bound
	// left the suite green: the other refused the same value.
	insertRequest := func(ref, key string) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
			VALUES ('development', $1, 'UPDATE accounts SET tier = 2', 'a reason', 't', 'r', 's', 'pending', $2)`, ref, key)
		return err
	}
	if err := insertRequest("req_ok", "key_ok"); err != nil {
		t.Fatalf("a well-formed request was refused: %v", err)
	}

	// One column at a time, each with every other column well-formed.
	field := func(column, value string) func() error {
		return func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
				VALUES ('development',
					COALESCE(NULLIF($1, ''), 'req_x') || $2,
					CASE WHEN $1 = 'statement' THEN $3 ELSE 'UPDATE accounts SET tier = 2' END,
					CASE WHEN $1 = 'reason'    THEN $3 ELSE 'a reason' END,
					CASE WHEN $1 = 'target'    THEN $3 ELSE 't' END,
					CASE WHEN $1 = 'role'      THEN $3 ELSE 'r' END,
					CASE WHEN $1 = 'submitter' THEN $3 ELSE 's' END,
					'pending',
					CASE WHEN $1 = 'idempotency_key' THEN $3 ELSE 'key_' || $2 END)`,
				column, column+value[:1], value)
			return err
		}
	}

	for name, exec := range map[string]func() error{
		"an eighth state": func() error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO requests (tenant_id, reference, statement, reason, target, role, submitter, state, idempotency_key)
				VALUES ('development', 'req_8', 's', 'r', 't', 'r', 's', 'contemplating', 'k8')`)
			return err
		},
		"a whitespace-only reference":       func() error { return insertRequest("   ", "key_ws") },
		"a whitespace-only statement":       field("statement", "   "),
		"a whitespace-only reason":          field("reason", "   "),
		"a whitespace-only target":          field("target", "   "),
		"a whitespace-only role":            field("role", "   "),
		"a whitespace-only submitter":       field("submitter", "   "),
		"a whitespace-only idempotency key": field("idempotency_key", "   "),
		"an over-long submitter":            field("submitter", strings.Repeat("s", 400)),
		"an over-long target":               field("target", strings.Repeat("t", 400)),
		"a second request with the same idempotency key": func() error {
			return insertRequest("req_second", "key_ok")
		},
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
				// A setup failure returned here would PASS the subtest, which
				// is what it did until a reviewer made the insert fail.
				t.Fatalf("setting up a second tenant: %v", err)
			}
			_, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('other', 'req_ok', 1, 'sam')`)
			return err
		},
		// The constraints that carry the proto's idempotency declarations:
		// Approve is NATURAL because the row is keyed (request, stage,
		// approver), and RecordExecution is KEYED on the nonce. Both deleted
		// green until these existed.
		"the same approver approving the same stage twice": func() error {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('development', 'req_ok', 1, 'sam')`); err != nil {
				t.Fatalf("setting up the first approval: %v", err)
			}
			_, err := db.ExecContext(ctx,
				`INSERT INTO approvals (tenant_id, reference, stage, approver) VALUES ('development', 'req_ok', 1, 'sam')`)
			return err
		},
		"the same nonce reported twice": func() error {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'nonce_twice', 'committed', 1)`); err != nil {
				t.Fatalf("setting up the first execution: %v", err)
			}
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('development', 'req_ok', 'nonce_twice', 'committed', 1)`)
			return err
		},
		"an execution whose tenant differs from its request's": func() error {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO tenants (id, name) VALUES ('other_exec', 'Other')`); err != nil {
				t.Fatalf("setting up a second tenant: %v", err)
			}
			_, err := db.ExecContext(ctx, `
				INSERT INTO executions (tenant_id, reference, nonce, outcome, rows_affected)
				VALUES ('other_exec', 'req_ok', 'n_cross', 'committed', 1)`)
			return err
		},
		"a whitespace-only tenant id": func() error {
			_, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ('   ', 'Whitespace')`)
			return err
		},
		"a whitespace-only tenant name": func() error {
			_, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ('t_ws', '   ')`)
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
		"an over-long nonce": func() error {
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

	// And that it is PARTIAL, which is the whole argument in its comment: a
	// plain index on the same columns satisfies the name above, so dropping the
	// WHERE clause left this green. Read the definition rather than infer it.
	var def string
	if err := db.QueryRowContext(ctx,
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'requests_queue'`).Scan(&def); err != nil {
		t.Fatalf("reading the index definition: %v", err)
	}
	if !strings.Contains(def, "WHERE") {
		t.Errorf("requests_queue is not partial, so it indexes every state: %s", def)
	}
	for _, state := range []string{"pending", "approved"} {
		if !strings.Contains(def, state) {
			t.Errorf("requests_queue does not cover %s: %s", state, def)
		}
	}
}

// Verify opens a read transaction, and without its rollback the connection is
// never returned — sixteen calls exhaust the pool and the seventeenth blocks.
func TestVerifyDoesNotLeakItsConnection(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	for i := range 40 {
		if err := Verify(ctx, db); err != nil {
			t.Fatalf("Verify %d: %v", i, err)
		}
	}
	if inUse := db.Stats().InUse; inUse != 0 {
		t.Errorf("%d connections are still checked out after 40 Verify calls", inUse)
	}
}

// EDR-0005's reason for pinging on Open: "a pool that connects on first use
// hides broken authentication until an incident". Deleting the ping left the
// suite green, because every other test connects successfully.
func TestOpenRefusesABadDSNImmediately(t *testing.T) {
	ctx := t.Context()
	_, err := Open(ctx, replaceUser(dsn(t), "no_such_role_"+t.Name()))
	if err == nil {
		t.Fatal("Open returned a pool for a role that does not exist; the failure is deferred to first use")
	}
	if !strings.Contains(err.Error(), "connecting") {
		t.Errorf("the error should say connecting failed; got %v", err)
	}
}

// Migrate must propagate a migration's failure. Changing the error check after
// applyOne from `err != nil` to `err == nil` left the whole suite green: every
// test migrates a clean database, where nothing fails.
func TestAFailingMigrationIsReported(t *testing.T) {
	db, _ := freshDB(t)
	ctx := t.Context()

	// A table the first migration also creates, so its CREATE TABLE fails.
	if _, err := db.ExecContext(ctx, `CREATE TABLE public.tenants (id text)`); err != nil {
		t.Fatalf("planting the conflict: %v", err)
	}

	err := Migrate(ctx, db)
	if err == nil {
		t.Fatal("Migrate reported success while its migration failed")
	}
	if !strings.Contains(err.Error(), "0001") {
		t.Errorf("the error should name the migration that failed; got %v", err)
	}

	// And nothing was recorded, because the DDL and its bookkeeping row commit
	// together.
	var n int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_tables WHERE tablename = 'schema_migrations'`).Scan(&n); err != nil {
		t.Fatalf("looking for schema_migrations: %v", err)
	}
	if n == 1 {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&n); err != nil {
			t.Fatalf("counting: %v", err)
		}
		if n != 0 {
			t.Errorf("a failed migration recorded %d rows", n)
		}
	}
}

// Zero means forever, and a pool that never retires a connection ages badly
// across a failover, a DNS change or a pooler in front.
func TestThePoolRetiresConnections(t *testing.T) {
	db, _ := freshDB(t)
	// Observed on the pool, not asserted about the constants: deleting the
	// SetMaxOpenConns / SetConnMaxLifetime calls in Open left a constants-only
	// test green.
	if got := db.Stats().MaxOpenConnections; got != 16 {
		t.Errorf("the pool allows %d open connections, want 16; unbounded is a control plane turning into the target's max_connections", got)
	}

	if poolMaxLifetime <= 0 || poolMaxIdleTime <= 0 {
		t.Errorf("poolMaxLifetime=%v poolMaxIdleTime=%v; zero disables retirement entirely",
			poolMaxLifetime, poolMaxIdleTime)
	}
	if poolMaxIdleTime > poolMaxLifetime {
		t.Errorf("an idle bound longer than the lifetime bound never applies")
	}
}
