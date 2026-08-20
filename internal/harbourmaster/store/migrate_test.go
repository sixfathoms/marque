package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
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
		"REINDEX":                   "REINDEX TABLE t;",
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
