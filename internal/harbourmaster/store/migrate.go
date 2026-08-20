// Package store is the Harbourmaster's own database, and the only package in
// the control plane permitted to import a driver for a target engine.
//
// That confinement is EDR-0042's replacement for EDR-0005's original mechanism,
// which said the Harbourmaster linked no such driver at all. It could not
// survive EDR-0013 fixing Marque's own state on PostgreSQL: PostgreSQL is also
// a target engine, and one driver serves both. The replacement is import
// discipline, enforced by depguard, and it is weaker — a linter reads imports,
// not capability. See EDR-0042's Consequences for the four ways it is defeated.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	// The driver, imported for its side effect of registering with
	// database/sql. EDR-0042 confines this import to this package.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Migrations are embedded so the binary carries them and a deployment cannot
// arrive missing half of itself. Embedding does NOT catch a deleted migration
// file: removing the last unapplied one leaves the embedded and applied sets
// agreeing, and nothing here notices, because no record of it having existed
// survives. These rules protect applied history (EDR-0042).
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// lockKey is the advisory lock every migrator run takes. It is arbitrary and
// only has to be stable.
const lockKey int64 = 0x6d61727175653031 // "marque01"

type migration struct {
	number int
	name   string
	sql    string
	digest string
}

// loadMigrations reads the embedded set and returns it in order, refusing a
// gap. A gap means a migration was removed from the middle, which the digest
// check would otherwise notice only once it had been applied somewhere.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("migrations/%s is not a .sql file; the directory holds migrations and nothing else", name)
		}
		numPart, _, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		if !ok {
			return nil, fmt.Errorf("migrations/%s is not named NNNN_slug.sql", name)
		}
		n, err := strconv.Atoi(numPart)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("migrations/%s does not start with a positive number", name)
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading migrations/%s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			number: n,
			name:   name,
			sql:    string(body),
			digest: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].number < out[j].number })
	for i, m := range out {
		if m.number != i+1 {
			return nil, fmt.Errorf(
				"embedded migrations are not numbered contiguously from 1: expected %04d, found %s.\n"+
					"  A gap means a migration was removed from the middle. Renumbering is not a fix — the\n"+
					"  digest of an applied migration is recorded, so a deployment that ran the old one will\n"+
					"  refuse the renumbered set", i+1, m.name)
		}
	}
	return out, nil
}

// Verify reports whether the database's applied history matches this binary's
// embedded set. It never writes. Startup calls it and refuses to serve on any
// mismatch, rather than migrating: migrating implicitly turns every deploy into
// a schema change nobody chose to run (EDR-0042).
func Verify(ctx context.Context, db *sql.DB) error {
	embedded, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	return compare(embedded, applied)
}

type applied struct {
	number int
	digest string
}

func appliedMigrations(ctx context.Context, db *sql.DB) ([]applied, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("looking for schema_migrations: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx,
		`SELECT number, digest FROM schema_migrations ORDER BY number`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []applied
	for rows.Next() {
		var a applied
		if err := rows.Scan(&a.number, &a.digest); err != nil {
			return nil, fmt.Errorf("reading schema_migrations: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ErrSchemaMismatch is returned when the database's history and the binary's
// embedded set disagree in a way no migration can fix.
var ErrSchemaMismatch = errors.New("schema mismatch")

// ErrPending is returned when the database is behind this binary. It is the one
// mismatch a migrator run repairs.
var ErrPending = errors.New("migrations pending")

func compare(embedded []migration, applied []applied) error {
	if len(applied) > len(embedded) {
		return fmt.Errorf(
			"%w: the database is at migration %d and this binary knows %d.\n"+
				"  A newer schema than the binary means an older binary is being run against a\n"+
				"  database a newer one has migrated. Deploy the newer binary, or restore the\n"+
				"  database — do not start this one",
			ErrSchemaMismatch, applied[len(applied)-1].number, len(embedded))
	}
	for i, a := range applied {
		e := embedded[i]
		if a.number != e.number {
			return fmt.Errorf("%w: applied migration %d is where %d was expected", ErrSchemaMismatch, a.number, e.number)
		}
		if a.digest != e.digest {
			return fmt.Errorf(
				"%w: migration %s has been edited since it was applied.\n"+
					"  applied digest:  %s\n"+
					"  embedded digest: %s\n"+
					"  Migrations are forward-only: repair this by writing another one, not by\n"+
					"  changing this one",
				ErrSchemaMismatch, e.name, a.digest, e.digest)
		}
	}
	if len(applied) < len(embedded) {
		return fmt.Errorf("%w: %d applied, %d embedded — run the migrator",
			ErrPending, len(applied), len(embedded))
	}
	return nil
}

// Migrate applies every pending migration, in order, each in its own
// transaction together with the row recording it. It is an explicit act: no
// code path reaches it during ordinary startup.
func Migrate(ctx context.Context, db *sql.DB) error {
	// A SESSION lock, not a transaction-scoped one. pg_advisory_xact_lock is
	// released at commit, so with each migration in its own transaction the
	// second and later ones would race another migrator (EDR-0042).
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("taking a connection for the migration lock: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("taking the migration lock: %w", err)
	}
	defer func() {
		// Best effort: the lock also dies with the session.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
	}()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			number     integer     PRIMARY KEY,
			name       text        NOT NULL,
			digest     text        NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	embedded, err := loadMigrations()
	if err != nil {
		return err
	}
	appliedRows, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	if err := compare(embedded, appliedRows); err != nil && !errors.Is(err, ErrPending) {
		return err
	}

	for _, m := range embedded[len(appliedRows):] {
		if err := applyOne(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, conn *sql.Conn, m migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning %s: %w", m.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("applying %s: %w", m.name, err)
	}
	// The record commits with the DDL. PostgreSQL has transactional DDL, which
	// is why a half-applied migration is not a state this design has.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (number, name, digest) VALUES ($1, $2, $3)`,
		m.number, m.name, m.digest); err != nil {
		return fmt.Errorf("recording %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing %s: %w", m.name, err)
	}
	return nil
}

// Open connects to the control plane's own database.
//
// The driver import lives here and nowhere else in the control plane, which is
// the whole of EDR-0042's mechanism. It is registered with database/sql as a
// side effect, which is process-wide — so this package being the only importer
// does not stop another package calling sql.Open by name. That is stated in
// EDR-0042 as one of the four ways the rule is defeated, and it is why this is
// import discipline rather than containment.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the control plane's database: %w", err)
	}
	// Verify positively rather than lazily. A pool that connects on first use
	// hides broken authentication until an incident (EDR-0005).
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to the control plane's database: %w", err)
	}
	return db, nil
}
