package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// The embedded set is the one the binary ships. If this fails, the migration
// directory is malformed and no deployment of this binary could migrate.
func TestEmbeddedMigrationsLoad(t *testing.T) {
	got, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no migrations embedded; the schema cannot be created")
	}
	for i, m := range got {
		if m.number != i+1 {
			t.Errorf("migration %d is numbered %d", i, m.number)
		}
		if m.digest == "" || len(m.digest) != 64 {
			t.Errorf("%s: digest is %q, want 64 hex characters", m.name, m.digest)
		}
		if strings.TrimSpace(m.sql) == "" {
			t.Errorf("%s is empty", m.name)
		}
	}
}

// compare is where every refusal lives, so every refusal is exercised here.
// These are the mutations EDR-0042 says the migrator must refuse; each is
// watched failing rather than assumed.
func TestCompareRefusals(t *testing.T) {
	embedded := []migration{
		{number: 1, name: "0001_x.sql", digest: "aa"},
		{number: 2, name: "0002_x.sql", digest: "bb"},
	}

	t.Run("in step is no error", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "0001_x.sql", "aa"}, {2, "0002_x.sql", "bb"}})
		if err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("behind is pending, which a migrator run repairs", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "0001_x.sql", "aa"}})
		if !errors.Is(err, ErrPending) {
			t.Fatalf("want ErrPending, got %v", err)
		}
	})

	t.Run("empty database is pending, not a mismatch", func(t *testing.T) {
		err := compare(embedded, nil)
		if !errors.Is(err, ErrPending) {
			t.Fatalf("want ErrPending, got %v", err)
		}
	})

	// The case that matters most: an old binary against a database a newer one
	// has migrated. Starting would let it write to a schema it does not know.
	t.Run("ahead refuses", func(t *testing.T) {
		err := compare(embedded[:1], []applied{{1, "0001_x.sql", "aa"}, {2, "0002_x.sql", "bb"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "do not start this one") {
			t.Errorf("the message should say what to do; got %q", err)
		}
	})

	// Forward-only means an applied migration is history. Editing one makes two
	// deployments silently different.
	t.Run("edited applied migration refuses", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "0001_x.sql", "aa"}, {2, "0002_x.sql", "DIFFERENT"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "has been edited since it was applied") {
			t.Errorf("the message should name the cause; got %q", err)
		}
	})

	// One field differs per case, and the message is asserted. An earlier
	// version used a fixture differing in number, name AND digest at once, so
	// deleting the number check and the name check together still passed — the
	// digest comparison caught it and the test could not tell which check had
	// done the work.
	t.Run("a renumbered migration refuses, naming the position", func(t *testing.T) {
		err := compare(embedded, []applied{{2, "0001_x.sql", "aa"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "is where 1 was expected") {
			t.Errorf("the message should name the position; got %q", err)
		}
	})

	t.Run("a renamed migration refuses, naming both names", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "0002_x.sql", "aa"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "was applied as") {
			t.Errorf("the message should name what was applied; got %q", err)
		}
	})

	t.Run("an edited migration refuses on its digest", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "0001_x.sql", "zz"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "has been edited since it was applied") {
			t.Errorf("the message should name the cause; got %q", err)
		}
	})
}

// The boundary EDR-0042 states honestly: a deleted TRAILING unapplied migration
// is undetectable, because nothing recorded that it existed. This test exists
// so the limit is asserted rather than merely written down — if a later change
// makes it detectable, this fails and the record needs updating.
func TestDeletedTrailingUnappliedMigrationIsUndetectable(t *testing.T) {
	full := []migration{
		{number: 1, name: "0001_x.sql", digest: "aa"},
		{number: 2, name: "0002_x.sql", digest: "bb"},
	}
	// Migration 2 was never applied anywhere, then deleted from the source.
	truncated := full[:1]
	if err := compare(truncated, []applied{{1, "0001_x.sql", "aa"}}); err != nil {
		t.Fatalf("expected the truncation to pass unnoticed, which is the stated limit; got %v", err)
	}
}

// A gap is a removal from the middle, which contiguity catches. Built over a
// synthetic filesystem, because a gap cannot be made from the real directory
// without deleting a migration from the repository — the earlier version of
// this test called loadMigrations and asserted nothing at all.
func TestGapInEmbeddedSetIsRefused(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/0003_c.sql": &fstest.MapFile{Data: []byte("SELECT 3;")},
	}
	_, err := loadMigrationsFrom(fsys)
	if err == nil {
		t.Fatal("a gap between 0001 and 0003 was accepted")
	}
	if !strings.Contains(err.Error(), "contiguously") {
		t.Errorf("the message should name the cause; got %q", err)
	}
	if !strings.Contains(err.Error(), "Renumbering is not a fix") {
		t.Errorf("the message should say why renumbering does not help; got %q", err)
	}
}

func TestMalformedMigrationNamesAreRefused(t *testing.T) {
	// The filename is chosen so that ONLY the check under test can refuse it,
	// and the message is asserted. An earlier version used README.md for the
	// .sql case, which also fails the NNNN_slug branch — so deleting the
	// suffix check left the suite green while 0001_x.md silently became a
	// migration.
	for name, c := range map[string]struct {
		files fstest.MapFS
		says  string
	}{
		"not a .sql file": {
			fstest.MapFS{"migrations/0001_x.md": &fstest.MapFile{Data: []byte("x")}},
			"is not a .sql file",
		},
		"no NNNN_slug shape": {
			fstest.MapFS{"migrations/initial.sql": &fstest.MapFile{Data: []byte("x")}},
			"is not named NNNN_slug.sql",
		},
		"a prefix that is not a number": {
			fstest.MapFS{"migrations/initial_a.sql": &fstest.MapFile{Data: []byte("x")}},
			"does not start with a positive number",
		},
		"zero is not a number": {
			fstest.MapFS{"migrations/0000_a.sql": &fstest.MapFile{Data: []byte("x")}},
			"does not start with a positive number",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadMigrationsFrom(c.files)
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("the message should name the cause %q; got %q", c.says, err)
			}
		})
	}
}

// The digest is compared as a value, so it has to be a real one — the earlier
// version of this test checked only that the string was 64 characters long.
func TestDigestIsTheSHA256OfTheFile(t *testing.T) {
	body := []byte("SELECT 1;")
	want := sha256.Sum256(body)
	got, err := loadMigrationsFrom(fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: body},
	})
	if err != nil {
		t.Fatalf("loadMigrationsFrom: %v", err)
	}
	raw, err := hex.DecodeString(got[0].digest)
	if err != nil {
		t.Fatalf("digest is not hex: %v", err)
	}
	if !bytes.Equal(raw, want[:]) {
		t.Errorf("digest is not the SHA-256 of the file")
	}
}

// EDR-0042 says a migration containing a statement PostgreSQL will not run
// inside a transaction is refused by the migrator. It said so for a while with
// nothing implementing it, which a reviewer found by grepping the package.
func TestNonTransactionalStatementsAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"CREATE INDEX CONCURRENTLY": "CREATE INDEX CONCURRENTLY x ON t (c);",
		"lowercase concurrently":    "create index concurrently x on t (c);",
		"VACUUM":                    "VACUUM FULL t;",
		"ALTER SYSTEM":              "ALTER SYSTEM SET work_mem = '1GB';",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadMigrationsFrom(fstest.MapFS{
				"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(body)},
			})
			if err == nil {
				t.Fatalf("%s was accepted", name)
			}
			if !strings.Contains(err.Error(), "25001") {
				t.Errorf("the message should say what would happen; got %q", err)
			}
		})
	}

	// The real migration must still load, or this guard is a build break.
	if _, err := loadMigrations(); err != nil {
		t.Errorf("the shipped migrations no longer load: %v", err)
	}
}

// A raw substring match over the body was defeated in both directions: a
// reviewer split a keyword with a comment and slipped past it, and found an
// ordinary table named vacuum_log wrongly refused.
func TestNonTransactionalDetectionReadsSQLNotSubstrings(t *testing.T) {
	load := func(body string) error {
		_, err := loadMigrationsFrom(fstest.MapFS{
			"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(body)},
		})
		return err
	}

	for name, body := range map[string]string{
		"a keyword split by a block comment": "CREATE/* split */DATABASE probe;",
		"a keyword split across lines":       "CREATE\nDATABASE probe;",
		"a keyword after a line comment":     "-- harmless\nVACUUM FULL t;",
		"DROP DATABASE":                      "DROP DATABASE probe;",
		"CREATE DATABASE":                    "CREATE DATABASE probe;",
		"REINDEX DATABASE":                   "REINDEX DATABASE x;",
		"REINDEX SCHEMA":                     "REINDEX SCHEMA public;",
		"REINDEX SYSTEM":                     "REINDEX SYSTEM x;",
		// Options sit between the keyword and the target, so no phrase matches.
		"REINDEX with an option list":      "REINDEX (VERBOSE) DATABASE x;",
		"REINDEX with a tablespace option": "REINDEX (TABLESPACE pg_default) SCHEMA public;",
		"CLUSTER":                          "CLUSTER;",
		"ALTER DATABASE SET TABLESPACE":    "ALTER DATABASE m SET TABLESPACE pg_default;",
		// One body per pass, so neither can be deleted: the first is invisible
		// to the stripped pass because an escape string derails the lexer, the
		// second is invisible to the raw pass because the phrase is not
		// contiguous.
		"a phrase only the raw pass sees":      `SELECT E'x\'/*'; REINDEX DATABASE d;`,
		"a phrase only the stripped pass sees": "REINDEX/*c*/DATABASE d;",
		"DISCARD ALL":                          "DISCARD ALL;",
		"CREATE SUBSCRIPTION":                  "CREATE SUBSCRIPTION s CONNECTION 'x' PUBLICATION p;",
		"DROP SUBSCRIPTION":                    "DROP SUBSCRIPTION s;",
		"CREATE TABLESPACE":                    "CREATE TABLESPACE ts LOCATION '/x';",
		"DROP TABLESPACE":                      "DROP TABLESPACE ts;",
		"ALTER SYSTEM":                         "ALTER SYSTEM SET work_mem = '1GB';",
		// The lexer under-refused on each of these — an escape string, a $tag$
		// inside an unquoted identifier, a Unicode dollar tag. The raw-body
		// pass refuses them whatever the lexer makes of them.
		"an escape-string quote":       `SELECT E'x\'/*'; VACUUM;`,
		"a $tag$ inside an identifier": "CREATE TABLE foo$tag$ (id integer); VACUUM;",
		"a Unicode dollar tag":         "SELECT $\u00e9$/*$\u00e9$; VACUUM;",
	} {
		t.Run(name, func(t *testing.T) {
			if err := load(body); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}

	// And the known coarseness, asserted so it is a decision rather than a
	// surprise: a bare identifier equal to one of the words is refused, because
	// there the word stands alone exactly as it does in the statement.
	t.Run("a column named vacuum is refused, which is the accepted cost", func(t *testing.T) {
		if err := load("CREATE TABLE t (vacuum boolean);"); err == nil {
			t.Error("expected a refusal; if there is none, the comment in migrate.go is stale")
		}
	})

	for name, body := range map[string]string{
		"an identifier containing VACUUM":       "CREATE TABLE vacuum_log (id integer);",
		"an identifier ENDING in VACUUM":        "CREATE TABLE log_vacuum (id integer);",
		"an identifier containing CONCURRENTLY": "CREATE TABLE concurrently_applied (id integer);",
		// Measured on 18: each of these runs inside a transaction perfectly
		// well, and the list said otherwise until a reviewer ran them.
		"REINDEX TABLE, which is transactional": "REINDEX TABLE t;",
		"REINDEX INDEX, which is transactional": "REINDEX INDEX i;",
		"ALTER TYPE ADD VALUE, likewise":        "ALTER TYPE mood ADD VALUE 'ecstatic';",
		"an ordinary migration":                 "CREATE TABLE t (id integer);",
		"ALTER DATABASE SET, which is fine":     "ALTER DATABASE m SET work_mem = '1GB';",
		"ALTER DATABASE SET default_tablespace": "ALTER DATABASE m SET default_tablespace = 'pg_default';",
	} {
		t.Run(name, func(t *testing.T) {
			if err := load(body); err != nil {
				t.Errorf("%s was refused: %v", name, err)
			}
		})
	}
}

// Contiguity would refuse a duplicate anyway, with a different message, so the
// duplicate check could be deleted green. It exists to say which two files
// collide, which is the only actionable part.
func TestDuplicateNumbersAreRefusedByName(t *testing.T) {
	_, err := loadMigrationsFrom(fstest.MapFS{
		"migrations/0001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/0001_b.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
	})
	if err == nil {
		t.Fatal("two migrations claiming number 1 were accepted")
	}
	for _, want := range []string{"0001_a.sql", "0001_b.sql", "a number is an identity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message should contain %q; got %q", want, err)
		}
	}
}

// stripSQLComments is tested directly, on its output, because every branch of
// it could be deleted with the loader tests still green — the raw-body pass in
// rejectNonTransactional refuses those inputs whatever the lexer does, which is
// the point of that pass and also why it hides the lexer's own regressions.
func TestStripSQLComments(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"a line comment": {
			"SELECT 1; -- and a note\nSELECT 2;",
			"SELECT 1; \nSELECT 2;",
		},
		"a block comment becomes a space": {
			"CREATE/* split */TABLE t (id int);",
			"CREATE TABLE t (id int);",
		},
		"block comments nest, as PostgreSQL nests them": {
			"a /* outer /* inner */ still outer */ b",
			"a   b",
		},
		"a marker inside a string literal is not a comment": {
			"INSERT INTO t VALUES ('/*'); SELECT 2;",
			"INSERT INTO t VALUES ('/*'); SELECT 2;",
		},
		"a line marker inside a string literal is not a comment": {
			"INSERT INTO t VALUES ('a -- b'); SELECT 2;",
			"INSERT INTO t VALUES ('a -- b'); SELECT 2;",
		},
		"a doubled quote does not end the literal": {
			"INSERT INTO t VALUES ('it''s /*'); SELECT 2;",
			"INSERT INTO t VALUES ('it''s /*'); SELECT 2;",
		},
		"a quoted identifier is not a comment either": {
			`CREATE TABLE "od--d" (id int);`,
			`CREATE TABLE "od--d" (id int);`,
		},
		"a dollar-quoted body is replaced by a space": {
			"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; -- x $$ LANGUAGE sql;",
			"CREATE FUNCTION f() RETURNS int AS   LANGUAGE sql;",
		},
		"a tagged dollar quote too": {
			"AS $body$ anything /* at all $body$ END",
			"AS   END",
		},
		"an unterminated block comment eats the rest": {
			"SELECT 1; /* and then nothing",
			"SELECT 1;  ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := stripSQLComments(c.in); got != c.want {
				t.Errorf("stripSQLComments(%q)\n = %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// The under-refusal the raw-body pass exists for, kept as a named regression:
// a DO block runs its dollar-quoted body as statements, so dropping that body
// is right for reading SQL and wrong for this question.
func TestADOBlockExecutingAForbiddenStatementIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"DO ... EXECUTE 'VACUUM'": `DO $$ BEGIN EXECUTE 'VACUUM t'; END $$;`,
		"DO ... EXECUTE CONCURRENTLY": `
			DO $$ BEGIN EXECUTE 'CREATE INDEX CONCURRENTLY i ON t(c)'; END $$;`,
		"an unterminated tag truncating the file": "ALTER TABLE a$b$c ADD COLUMN x int;\nVACUUM t;",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadMigrationsFrom(fstest.MapFS{
				"migrations/0001_a.sql": &fstest.MapFile{Data: []byte(body)},
			})
			if err == nil {
				t.Errorf("%s was accepted; PostgreSQL refuses it at run time", name)
			}
		})
	}
}

// Migrations are sorted numerically, and with one migration in the repository
// nothing could see the sort reversed — a reviewer negated it and both suites
// stayed green. Two migrations is the smallest set where order is observable,
// and ten is where lexical and numeric order disagree.
func TestMigrationsAreSortedNumerically(t *testing.T) {
	files := fstest.MapFS{}
	for i := 1; i <= 10; i++ {
		files[fmt.Sprintf("migrations/%04d_m.sql", i)] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf("SELECT %d;", i)),
		}
	}
	got, err := loadMigrationsFrom(files)
	if err != nil {
		t.Fatalf("loading ten migrations: %v", err)
	}
	for i, m := range got {
		if m.number != i+1 {
			t.Fatalf("migration %d of the set is number %d (%s); the set is not in numeric order",
				i, m.number, m.name)
		}
	}

	// And unpadded names, where lexical order puts 10 before 2.
	got, err = loadMigrationsFrom(fstest.MapFS{
		"migrations/1_a.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/2_b.sql":  &fstest.MapFile{Data: []byte("SELECT 2;")},
		"migrations/10_c.sql": &fstest.MapFile{Data: []byte("SELECT 10;")},
	})
	if err == nil {
		if got[1].number != 2 {
			t.Errorf("unpadded names sorted lexically: got %d second, want 2", got[1].number)
		}
	}
}

// containsPhrase's identifier boundaries, at the edges. Changing any endpoint
// of the ranges made vacuum0, vacuuma or vacuumZ match, and nothing noticed.
func TestContainsPhraseIdentifierBoundaries(t *testing.T) {
	for body, want := range map[string]bool{
		"VACUUM":     true,
		"VACUUM;":    true,
		"a VACUUM b": true,
		"(VACUUM)":   true,
		"vacuum0":    false,
		"vacuum9":    false,
		"vacuuma":    false,
		"vacuumz":    false,
		"vacuumA":    false,
		"vacuumZ":    false,
		"vacuum_":    false,
		"vacuum$":    false,
		"0vacuum":    false,
		"zvacuum":    false,
		"Avacuum":    false,
		"_vacuum":    false,
		"$vacuum":    false,
		"log_vacuum": false,
		"vacuum_log": false,
	} {
		if got := containsPhrase(strings.ToUpper(body), "VACUUM"); got != want {
			t.Errorf("containsPhrase(%q, VACUUM) = %v, want %v", body, got, want)
		}
	}
}

// The default bound, which no other test observes: both timeout tests set
// lockWait themselves, so "0" — the unbounded wait rounds 4 and 8 each fixed —
// could be restored with the whole suite green.
func TestLockWaitIsBounded(t *testing.T) {
	if lockWait == "" || lockWait == "0" {
		t.Errorf("lockWait defaults to %q, which is an unbounded lock wait", lockWait)
	}
	if _, err := time.ParseDuration(lockWait); err != nil {
		t.Errorf("lockWait %q is not a duration PostgreSQL will accept: %v", lockWait, err)
	}
}
