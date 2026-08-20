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
		{number: 1, name: "0001_a.sql", digest: "aa"},
		{number: 2, name: "0002_b.sql", digest: "bb"},
	}

	t.Run("in step is no error", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "aa"}, {2, "bb"}})
		if err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("behind is pending, which a migrator run repairs", func(t *testing.T) {
		err := compare(embedded, []applied{{1, "aa"}})
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
		err := compare(embedded[:1], []applied{{1, "aa"}, {2, "bb"}})
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
		err := compare(embedded, []applied{{1, "aa"}, {2, "DIFFERENT"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
		if !strings.Contains(err.Error(), "has been edited since it was applied") {
			t.Errorf("the message should name the cause; got %q", err)
		}
	})

	t.Run("reordered history refuses", func(t *testing.T) {
		err := compare(embedded, []applied{{2, "bb"}})
		if !errors.Is(err, ErrSchemaMismatch) {
			t.Fatalf("want ErrSchemaMismatch, got %v", err)
		}
	})
}

// The boundary EDR-0042 states honestly: a deleted TRAILING unapplied migration
// is undetectable, because nothing recorded that it existed. This test exists
// so the limit is asserted rather than merely written down — if a later change
// makes it detectable, this fails and the record needs updating.
func TestDeletedTrailingUnappliedMigrationIsUndetectable(t *testing.T) {
	full := []migration{
		{number: 1, name: "0001_a.sql", digest: "aa"},
		{number: 2, name: "0002_b.sql", digest: "bb"},
	}
	// Migration 2 was never applied anywhere, then deleted from the source.
	truncated := full[:1]
	if err := compare(truncated, []applied{{1, "aa"}}); err != nil {
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
	for name, files := range map[string]fstest.MapFS{
		"not a .sql file":      {"migrations/README.md": &fstest.MapFile{Data: []byte("x")}},
		"no number prefix":     {"migrations/initial.sql": &fstest.MapFile{Data: []byte("x")}},
		"zero is not a number": {"migrations/0000_a.sql": &fstest.MapFile{Data: []byte("x")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadMigrationsFrom(files); err == nil {
				t.Fatalf("%s was accepted", name)
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
