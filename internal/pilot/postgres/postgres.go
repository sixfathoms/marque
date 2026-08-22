// Package postgres is the Pilot's PostgreSQL adapter, and one of the two
// packages EDR-0042 allows to import a driver.
//
// The Pilot is the only component that touches a target, and the only one that
// holds a target credential (EDR-0005). The Harbourmaster links a PostgreSQL
// driver too — for its own database, since EDR-0013 fixed Marque's state on
// PostgreSQL — so the boundary is not the driver's absence but where a TARGET
// connection may be opened, which is here. EDR-0042 has why.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	// The driver, imported for its side effect of registering with
	// database/sql. EDR-0042 permits this package and the Harbourmaster's store
	// to hold one, and nothing else.
	//
	// The comment must stay ADJACENT to the import: it is what silences
	// revive's blank-imports rule, and EDR-0042 records at length why that
	// matters more than it looks.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open connects to a target. The caller owns the pool and must Close it.
//
// Deliberately small: one connection at a time, because M1's Pilot runs one
// statement and a pool that keeps spares open against a production database is
// a cost the operator did not ask for.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the target: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verified positively, for EDR-0005's reason: a pool that connects on first
	// use hides broken credentials until an incident.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connecting to the target: %w", err)
	}
	return db, nil
}

// CommitWasRefused reports whether a failed COMMIT was refused BY THE SERVER,
// as opposed to failing because the connection did not survive to hear the
// answer.
//
// The difference decides between two outcomes that look identical from the
// caller's side and are not:
//
//   - the server replied with an error — a deferred constraint fired, say — so
//     the transaction definitively rolled back and nothing was applied;
//   - the connection failed — so the commit may have been applied and the
//     acknowledgement lost, which is INDETERMINATE and is the case EDR-0021
//     exists for.
//
// Collapsing them would be wrong in both directions: reporting a refused commit
// as indeterminate sends a human to inspect a database that is provably
// unchanged, and reporting a lost one as rolled back tells them a statement did
// not run when it may have.
//
// This lives in the adapter because deciding it means reading a driver's error
// types, and EDR-0042 confines those to here and to the Harbourmaster's store.
func CommitWasRefused(err error) bool {
	if err == nil {
		return false
	}
	// A *pgconn.PgError means the server sent an ErrorResponse. That is
	// necessary and NOT sufficient: terminating a backend also produces one —
	// 57P01, "terminating connection due to administrator command" — and a
	// server that is going away mid-commit has not refused anything, it has
	// stopped answering with a message attached. Found by writing the test that
	// takes the connection away and watching this call it rolled_back.
	//
	// So: a refusal is an ERROR the server chose to return, in a class that is
	// about the statement rather than about the connection or the server.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// SEVERITY, and severity alone.
	//
	// An ERROR response is the server having processed the COMMIT and declined
	// it: the transaction is rolled back and nothing was applied, whatever the
	// SQLSTATE says. FATAL and PANIC are the server ending the session, which
	// is not a decision about this statement.
	//
	// An earlier version also excluded SQLSTATE classes 08, 57, 58 and XX on
	// the theory that those mean "the transaction's fate is unsettled". That
	// was wrong, and a reviewer showed it: a deferred CONSTRAINT TRIGGER that
	// RAISEs at commit produces ERROR XX000, PostgreSQL rolls the transaction
	// back and leaves zero rows, and this called it indeterminate — sending a
	// human to inspect a database that is provably unchanged. Application code
	// chooses that SQLSTATE, so the class cannot carry the meaning the class
	// list assumed.
	return pgErr.Severity != "FATAL" && pgErr.Severity != "PANIC"
}
