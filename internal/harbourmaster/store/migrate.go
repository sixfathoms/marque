// Package store is the Harbourmaster's own database, and the only package in
// the control plane permitted to import a driver for a target engine.
//
// That confinement is EDR-0042's replacement for EDR-0005's original mechanism,
// which said the Harbourmaster linked no such driver at all. It could not
// survive EDR-0013 fixing Marque's own state on PostgreSQL: PostgreSQL is also
// a target engine, and one driver serves both. The replacement is import
// discipline, and it is weaker — a check reads imports, not capability. It is
// enforced by TestNoBinaryLinksADriverOutsideItsHome in this package, which
// asks `go list -deps` what each binary links, with a filesystem walk as a
// cross-check and a depguard block as the edit-time report. Not because a
// linter is incapable: several claims of that shape were made here and every
// one was false, and EDR-0042 retracts them. Because each mechanism's reach is
// configuration — flags and exclusions, a skip rule, the patterns given to
// `go list` — so all of them are probed rather than asserted. See EDR-0042 for
// the four ways import discipline is defeated.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	// The driver, imported for its side effect of registering with
	// database/sql. EDR-0042 confines this import to this package.
	//
	// The comment must stay ADJACENT to the import: it is what silences
	// revive's blank-imports rule, and with revive silenced depguard's
	// diagnostic is the one that prints. A formatter moved "regexp" between
	// them once and the build failed, which is the tidiest demonstration of
	// EDR-0042's retraction anyone could ask for.
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
func loadMigrations() ([]migration, error) { return loadMigrationsFrom(migrationFS) }

// loadMigrationsFrom is loadMigrations over any filesystem, so the refusals
// below can be tested against a synthetic set. A gap cannot be built out of
// the real directory without deleting a migration from the repository.
func loadMigrationsFrom(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
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
		body, err := fs.ReadFile(fsys, "migrations/"+name)
		if err != nil {
			return nil, fmt.Errorf("reading migrations/%s: %w", name, err)
		}
		if err := rejectNonTransactional(name, string(body)); err != nil {
			return nil, err
		}
		// Raw bytes. Trimming would let a whitespace-only edit to applied
		// history verify clean, which is the forward-only guarantee itself.
		sum := sha256.Sum256(body)
		out = append(out, migration{
			number: n,
			name:   name,
			sql:    string(body),
			digest: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].number < out[j].number })
	for i := 1; i < len(out); i++ {
		if out[i].number == out[i-1].number {
			return nil, fmt.Errorf(
				"migrations %s and %s both claim number %04d; a number is an identity, not a label",
				out[i-1].name, out[i].name, out[i].number)
		}
	}
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

// Statements PostgreSQL refuses to run inside a transaction block. applyOne
// wraps every migration in one, so a migration containing any of these fails
// with SQLSTATE 25001 — after the advisory lock is taken, part-way through a
// production migration. Refusing at LOAD time instead means the refusal happens
// in CI, on every build, before a database is involved.
//
// EDR-0042 promised this and nothing implemented it; a reviewer grepped the
// package and found the mechanism did not exist.
//
// Deliberately coarse, and coarse in the safe direction. A migration needing
// CREATE INDEX CONCURRENTLY is a real need, and the answer is a decision about
// how this migrator runs statements outside a transaction, not a quiet
// exception here.
//
// **This is a denylist of spellings, not a decision procedure.** It catches the
// statements a migration author here would plausibly reach for. It does not
// enumerate everything PostgreSQL refuses inside a transaction block, and it
// does not try to. A reviewer found `CLUSTER`, `DISCARD ALL`,
// `CREATE SUBSCRIPTION`, `REINDEX (VERBOSE) DATABASE` and
// `ALTER DATABASE … SET TABLESPACE` missing; the first three are phrases, the
// fourth needed a pattern, and the fifth is `SET TABLESPACE`.
//
// That last one was argued away once, on the grounds that no phrase separates
// it from the transactional `ALTER DATABASE … SET` forms. A reviewer measured
// otherwise: `SET TABLESPACE` refuses none of them. So the justification was
// wrong even though the conclusion could have been defended.
//
// **Stop enumerating the costs.** Twice a sentence here has said "its only cost
// is X" and a reviewer has found Y — `ALTER INDEX … SET TABLESPACE` and
// an ALTER of a materialised view with the same clause, both transactional and
// refused; and `CLUSTER t USING i`, transactional and refused by bare
// `CLUSTER`. The general statement is the true one and it is shorter:
//
//	This matches a PHRASE, not a statement. It refuses every statement in
//	which either pass sees one, transactional or not.
//
// "either pass sees" is load-bearing and was missing from the first version of
// this sentence — the two passes' blind spots overlap, which is the ninth
// retracted claim, twenty lines below.
//
// That is the trade the list makes everywhere, it is why CONCURRENTLY refuses a
// concurrent REFRESH, and it needs no list of examples to stay true.
//
// What is left over fails as SQLSTATE 25001 at migration time, which is what
// this reduces, not what it eliminates.
//
// What it gets wrong, stated rather than discovered. A bare identifier equal to
// one of these words — a column named `vacuum` — is refused, because the word
// stands alone there exactly as it does in the statement; `vacuum_log` and
// `log_vacuum` are fine, since the underscore makes each one identifier. And
// a concurrent REFRESH of a materialised view is refused although PostgreSQL
// runs it inside a transaction perfectly well, because the list matches
// CONCURRENTLY on its own. Both over-refuse, which is the direction to be wrong
// in, and both are a reworded migration away from passing. `CREATE SUBSCRIPTION`
// is a third: with `connect = false` it runs inside a transaction, and the list
// does not read options. This is an accident-guard for
// the people who write these migrations, not a security control; nothing stops
// someone determined from spelling a statement in a way it does not match.
func rejectNonTransactional(name, body string) error {
	// BOTH forms. The stripped one so a keyword split by a comment is still
	// found; the raw one so a keyword this lexer wrongly believes is inside a
	// string or a dollar-quoted body is found anyway.
	//
	// That second pass exists because the lexer kept under-refusing — an escape
	// string, a $tag$ inside an unquoted identifier, a Unicode dollar tag, each
	// found by a reviewer, each letting a statement PostgreSQL refuses reach a
	// production migration.
	//
	// It REDUCES that; it does not eliminate it, and an earlier version of this
	// comment said "impossible by construction", which was the ninth false
	// claim on this branch. The two passes' blind spots overlap: the raw pass
	// only sees a CONTIGUOUS phrase, and a comment between two keywords is
	// removed only by the stripped pass — which is exactly what a lexer derail
	// disables. So
	//
	//	SELECT E'x\''; CREATE/*c*/DATABASE d;
	//
	// gets past both, measured. Closing that needs a real SQL lexer, which is
	// the treadmill three previous attempts were on.
	//
	// The cost of the second pass is refusing a migration that merely mentions
	// one of these words in a string or a comment. For an accident-guard over
	// files this repository writes, over-refusing with a clear message is the
	// right trade, and the shipped migration contains none of these words.
	stripped := collapseSpace(stripSQLComments(body))
	raw := collapseSpace(body)
	// REINDEX needs a pattern rather than a phrase: the non-transactional forms
	// are DATABASE, SCHEMA and SYSTEM, and PostgreSQL allows an option list
	// between the keyword and the target, so `REINDEX (VERBOSE) DATABASE x`
	// matched no phrase and loaded cleanly. Found by a reviewer running it.
	for _, body := range []string{stripped, raw} {
		if reindexNonTransactional.MatchString(strings.ToUpper(body)) {
			return fmt.Errorf(
				"migrations/%s contains a REINDEX of a DATABASE, SCHEMA or SYSTEM, which PostgreSQL\n"+
					"  refuses to run inside a transaction block. REINDEX TABLE and REINDEX INDEX are fine\n"+
					"  and are deliberately not refused (EDR-0042)", name)
		}
	}

	for _, s := range []string{
		// Every entry measured on PostgreSQL 18, not assumed. Bare REINDEX was
		// on this list and should not have been: REINDEX TABLE and REINDEX
		// INDEX run inside a transaction perfectly well, and only the DATABASE,
		// SCHEMA and SYSTEM forms do not. ALTER TYPE … ADD VALUE likewise runs
		// inside one on a supported server — this list never held it, and the
		// record claimed it did.
		"CONCURRENTLY",
		"VACUUM",
		"CLUSTER",
		"DISCARD ALL",
		"CREATE DATABASE",
		"DROP DATABASE",
		"CREATE TABLESPACE",
		"DROP TABLESPACE",
		"CREATE SUBSCRIPTION",
		"DROP SUBSCRIPTION",
		"SET TABLESPACE",
		"ALTER SYSTEM",
	} {
		if !containsPhrase(stripped, s) && !containsPhrase(raw, s) {
			continue
		}
		return fmt.Errorf(
			"migrations/%s contains %s, which PostgreSQL refuses to run inside a transaction\n"+
				"  block in the forms this list is about.\n"+
				"  Every migration is applied inside one, so this would fail as SQLSTATE 25001 part-way\n"+
				"  through a migration rather than here. Running statements outside a transaction is a\n"+
				"  decision this migrator has not taken (EDR-0042)", name, s)
	}
	return nil
}

// stripSQLComments removes -- and /* */ comments, skipping over string
// literals, quoted identifiers and dollar-quoted bodies so a marker inside one
// is not mistaken for a comment, and nesting block comments because PostgreSQL
// nests them.
//
// Every one of those was a real miss, and the string case was wrong in the
// UNSAFE direction — it refused less, not more. Without it,
//
//	INSERT INTO settings (key, value) VALUES ('glob', '/*');
//	CREATE INDEX CONCURRENTLY idx ON t (c);
//
// loaded cleanly: the `/*` inside the literal opened a comment that never
// closed, so the rest of the file was discarded and the statement PostgreSQL
// will refuse was never seen. It would then fail as SQLSTATE 25001 part-way
// through a production migration, which is the exact outcome this function
// exists to prevent. A reviewer found it; an earlier comment here claimed the
// only wrongness was in the safe direction, and that was not true.
func stripSQLComments(body string) string {
	var out strings.Builder
	for i := 0; i < len(body); {
		switch {
		case strings.HasPrefix(body[i:], "--"):
			j := strings.IndexByte(body[i:], '\n')
			if j < 0 {
				return out.String()
			}
			out.WriteByte('\n')
			i += j + 1

		case strings.HasPrefix(body[i:], "/*"):
			// Nested, as PostgreSQL nests them. A space, so
			// `CREATE/* x */DATABASE` becomes two words and the phrase match
			// sees it.
			depth, j := 1, i+2
			for j < len(body) && depth > 0 {
				switch {
				case strings.HasPrefix(body[j:], "/*"):
					depth++
					j += 2
				case strings.HasPrefix(body[j:], "*/"):
					depth--
					j += 2
				default:
					j++
				}
			}
			out.WriteByte(' ')
			i = j

		case body[i] == '\'' || body[i] == '"':
			// Copied verbatim, including a doubled quote, so the statement
			// separators either side of it survive.
			q := body[i]
			out.WriteByte(q)
			i++
			for i < len(body) {
				if body[i] == q {
					if i+1 < len(body) && body[i+1] == q {
						out.WriteString(string([]byte{q, q}))
						i += 2
						continue
					}
					out.WriteByte(q)
					i++
					break
				}
				out.WriteByte(body[i])
				i++
			}

		default:
			if tag, ok := dollarTag(body[i:]); ok {
				end := strings.Index(body[i+len(tag):], tag)
				if end < 0 {
					// Unterminated. Everything after it is inside the body,
					// and a space keeps the words either side apart.
					out.WriteByte(' ')
					return out.String()
				}
				// The body is dropped, and a space stands in for it: a
				// dollar-quoted function body is not statements this
				// migrator runs, it is a value.
				out.WriteByte(' ')
				i += len(tag) + end + len(tag)
				continue
			}
			out.WriteByte(body[i])
			i++
		}
	}
	return out.String()
}

// The option list is optional and may contain anything but a closing paren.
var reindexNonTransactional = regexp.MustCompile(`\bREINDEX\s*(\([^)]*\)\s*)?(DATABASE|SCHEMA|SYSTEM)\b`)

// dollarTag returns the $tag$ opening s, if s starts with one. A tag is $ then
// zero or more identifier characters then $, which makes both $$ and $body$
// tags and neither of them arithmetic.
func dollarTag(s string) (string, bool) {
	if len(s) == 0 || s[0] != '$' {
		return "", false
	}
	for j := 1; j < len(s); j++ {
		if s[j] == '$' {
			return s[:j+1], true
		}
		if s[j] != '_' && (s[j] < '0' || s[j] > '9') &&
			(s[j] < 'a' || s[j] > 'z') && (s[j] < 'A' || s[j] > 'Z') {
			return "", false
		}
	}
	return "", false
}

// collapseSpace turns every run of whitespace into one space, so `CREATE
// DATABASE` split across lines still reads as the phrase it is. PostgreSQL does
// not care where the newline falls and neither should this.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// containsPhrase reports whether upper-cased phrase appears in body delimited
// by non-identifier characters, so `vacuum_log` is not a VACUUM.
func containsPhrase(body, phrase string) bool {
	// Upper case only: body is upper-cased below before any of this runs, so a
	// lowercase range here would be dead code that reads as though it were
	// load-bearing — a reviewer mutated one and nothing noticed, which is how
	// it was found.
	isIdent := func(b byte) bool {
		return b == '_' || b == '$' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z')
	}
	upper := strings.ToUpper(body)
	for i := 0; ; {
		j := strings.Index(upper[i:], phrase)
		if j < 0 {
			return false
		}
		s := i + j
		e := s + len(phrase)
		// Both ends. Only checking the end would let `log_vacuum` match, which
		// is the mirror of the `vacuum_log` case that motivated this.
		beforeOK := s == 0 || !isIdent(upper[s-1])
		afterOK := e == len(upper) || !isIdent(upper[e])
		if beforeOK && afterOK {
			return true
		}
		i = s + 1
	}
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
	// In a transaction so SET LOCAL bounds the read's lock wait and dies with
	// it rather than leaking onto the pooled connection. Unbounded, a startup
	// check blocks behind anything holding AccessExclusiveLock on
	// schema_migrations — and a process that hangs at startup looks like a
	// process that is starting.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("beginning the verification read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`SELECT set_config('lock_timeout', $1, true)`, lockWait); err != nil {
		return fmt.Errorf("bounding the verification read: %w", err)
	}
	applied, err := appliedOn(ctx, tx)
	if err != nil {
		return err
	}
	return compare(embedded, applied)
}

type applied struct {
	number int
	name   string
	digest string
}

// Every reference to the schema_migrations TABLE is schema-qualified. Verify
// runs on the pool, where the migrator's one-off SET search_path cannot reach,
// and an unqualified name there lets a role whose search_path names an earlier
// writable schema satisfy the check against a decoy history — measured, not
// theorised. Qualifying works on any connection with no DSN dependence; the
// session pin in Migrate stays as defence in depth.
//
// The `pg_catalog.` on to_regclass is a different thing and buys almost
// nothing: pg_catalog is searched implicitly FIRST unless it is named, so a
// decoy public.to_regclass loses on the default search_path and on the
// migrator's pin. It would win only where pg_catalog is named explicitly and
// late — which is the bug EDR-0042 retracts, and which this file no longer
// does. So the qualification covers a configuration nothing here produces. It
// is written for symmetry, no test covers it, and saying so is better than
// leaving a reader to assume one does.
//
// querier is whatever the history is read through. Migrate reads it on the
// connection holding the advisory lock; Verify reads it on the pool, which is
// correct because Verify takes no lock and writes nothing.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func appliedOn(ctx context.Context, q querier) ([]applied, error) {
	var exists bool
	err := q.QueryRowContext(ctx,
		`SELECT pg_catalog.to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("looking for schema_migrations: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT number, name, digest FROM public.schema_migrations ORDER BY number`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []applied
	for rows.Next() {
		var a applied
		if err := rows.Scan(&a.number, &a.name, &a.digest); err != nil {
			return nil, fmt.Errorf("reading schema_migrations: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// lockWait bounds every lock wait the migrator and Verify make: the advisory
// lock, the history read, and each migration's DDL. Set through set_config so
// it is a parameter rather than string-built SQL — SET itself takes no
// parameters.
//
// A var, not a const, because the test that watches it bite holds a conflicting
// lock and waits for the refusal — which means its DEFAULT is what no test
// observes, and "0" here is the unbounded wait rounds 4 and 8 each fixed.
// TestLockWaitIsBounded asserts the default. At thirty seconds that test either takes
// thirty seconds or asserts nothing; the earlier version chose a deadline
// shorter than the timeout and failed itself.
// Bounds on how long a pooled connection lives. Zero means forever, which is
// what a mutation to either of these produced while nothing noticed.
const (
	poolMaxLifetime = 30 * time.Minute
	poolMaxIdleTime = 5 * time.Minute
)

// configurePool applies the pool's whole policy, taking the durations as
// parameters so a test can shorten them and watch a connection actually retire.
// Asserting the constants proved nothing: deleting the SetConnMaxLifetime call
// left them true and the pool unbounded.
//
// Bounded open connections because database/sql defaults to unlimited, which a
// control plane under load turns into the target's max_connections; bounded
// lifetimes because an unbounded one ages badly across a failover, a DNS change
// or a pooler in front, where the pool keeps handing out connections to
// somewhere that has moved.
func configurePool(db *sql.DB, lifetime, idleTime time.Duration) {
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(lifetime)
	db.SetConnMaxIdleTime(idleTime)
}

var lockWait = "30s"

// runtimeRole is the role the Harbourmaster serves as, and the one the migrator
// must NOT be. It is spelled here and in 0001_initial.sql, which cannot take a
// parameter — see issue #40.
const runtimeRole = "marque_runtime"

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
		if a.name != e.name {
			return fmt.Errorf(
				"%w: migration %d was applied as %s and is now called %s.\n"+
					"  Renaming an applied migration makes two deployments disagree about their\n"+
					"  own history while every digest still matches",
				ErrSchemaMismatch, a.number, a.name, e.name)
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
	// Discarded, not returned. This connection carries a moved search_path, and
	// sql.Conn.Close() returns a live session to the pool — the advisory lock
	// does NOT die with it, contrary to what an earlier comment here claimed.
	defer func() {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}()

	// Pinned before anything is read or written. The migrator and the runtime
	// deliberately run as different roles, and a $user-dependent search_path
	// resolves different tables for each — while a writable earlier schema can
	// shadow schema_migrations and let a decoy history verify. EDR-0007 pins it
	// on the fence for the same class of reason.
	// Session-scoped deliberately: every statement on this connection needs it,
	// and the connection is discarded at the end of the run rather than
	// returned to the pool carrying it.
	//
	// TO public, and NOT `public, pg_catalog`. When pg_catalog is not named it
	// is searched implicitly FIRST; naming it explicitly last demotes it below
	// public, so the pin written that way was strictly worse than no pin. A
	// public.length(text) would then bind into every length CHECK at DDL time,
	// and because a constraint holds the pg_proc OID the shadow cannot even be
	// dropped afterwards. Measured on 18.3, in review, after it shipped to this
	// branch.
	if _, err := conn.ExecContext(ctx, `SET search_path TO public`); err != nil {
		return fmt.Errorf("pinning search_path: %w", err)
	}
	// Session-scoped for the same reason as search_path, and that reason is
	// what an earlier draft got wrong: it set this LOCAL inside a throwaway
	// transaction to keep it off the pool, which the connection discard already
	// guarantees. The timeout then died at that transaction's commit and
	// covered neither the CREATE TABLE below, nor the history read, nor the
	// DDL. A reviewer held an AccessExclusiveLock on schema_migrations and
	// watched Migrate wait out the full hold.
	//
	// Without it pg_advisory_lock waits forever, and a CLI passing
	// context.Background() waits with it rather than saying another migrator
	// holds the lock.
	if _, err := conn.ExecContext(ctx,
		`SELECT set_config('lock_timeout', $1, false)`, lockWait); err != nil {
		return fmt.Errorf("bounding the migration's lock waits: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("taking the migration lock (another migrator may hold it): %w", err)
	}
	// Registered immediately after the lock is taken, not after any later step.
	// A session advisory lock survives the rollback of the transaction that
	// took it, so any window between acquisition and this defer is a window
	// where the lock is held and nothing is scheduled to release it.
	defer func() {
		// Explicit, because a session lock does not die when the connection is
		// returned to the pool. WithoutCancel so a cancelled caller still
		// releases it — bounded, so a stalled server cannot make this defer
		// hang and prevent the discard that would release it anyway.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, lockKey)
	}()

	// Checked HERE, before anything is created. The same check exists in
	// 0001_initial.sql, and by the time that runs the bootstrap table below has
	// already been created and owned — a reviewer watched the SQL guard refuse
	// the migration and leave schema_migrations owned by marque_runtime, which
	// IF NOT EXISTS then preserves forever. EDR-0012's non-ownership needs the
	// refusal to come first.
	var user string
	if err := conn.QueryRowContext(ctx, `SELECT current_user`).Scan(&user); err != nil {
		return fmt.Errorf("reading the migrating role: %w", err)
	}
	if user == runtimeRole {
		return fmt.Errorf(
			"refusing to migrate as %s: it would own these tables, and an owner can grant "+
				"itself anything, which is what EDR-0012's withheld grant is for", runtimeRole)
	}

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
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
	// Through the LOCKED connection, not the pool: reading the history on some
	// other connection reads a session the advisory lock does not hold.
	appliedRows, err := appliedOn(ctx, conn)
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

	// No SET LOCAL here: lock_timeout is session-scoped on the connection this
	// transaction runs on, so it already bounds this DDL's lock wait. It bounds
	// the WAIT, not the statement's duration — statement_timeout is 0, and a
	// migration that holds a lock for an hour once it has one is not something
	// this bounds.
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("applying %s: %w", m.name, err)
	}
	// The record commits with the DDL. PostgreSQL has transactional DDL, which
	// is why a half-applied migration is not a state this design has.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO public.schema_migrations (number, name, digest) VALUES ($1, $2, $3)`,
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
//
// The caller owns the returned *sql.DB and must Close it.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the control plane's database: %w", err)
	}
	// Bounded. database/sql defaults to unlimited open connections, which a
	// control plane under load turns into the target's max_connections.
	configurePool(db, poolMaxLifetime, poolMaxIdleTime)

	// Verify positively rather than lazily. A pool that connects on first use
	// hides broken authentication until an incident (EDR-0005).
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to the control plane's database: %w", err)
	}
	return db, nil
}
