// Package postgres is the Pilot's PostgreSQL adapter, and one of the two
// packages EDR-0042 allows to import a driver.
//
// The Pilot is the only component that touches a target. The Harbourmaster
// holds no target credential and links no target driver (EDR-0005), which is
// the boundary the confinement check in internal/harbourmaster/store enforces —
// and which this package is the other half of.
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
	// FATAL and PANIC are the server ending the session, not declining a
	// statement.
	if pgErr.Severity == "FATAL" || pgErr.Severity == "PANIC" {
		return false
	}
	// SQLSTATE classes where the transaction's fate is not settled by the
	// message: 08 connection exception, 57 operator intervention, 58 system
	// error, XX internal error. Everything else — 23 integrity constraint
	// violation being the case this exists for — is the server declining.
	switch class := pgErr.Code[:2]; class {
	case "08", "57", "58", "XX":
		return false
	default:
		return true
	}
}
