//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sixfathoms/marque/internal/harbourmaster/store"
	"github.com/sixfathoms/marque/internal/skeleton"
)

// EDR-0042: startup VERIFIES and refuses; it does not migrate. Migrating
// implicitly turns every deploy into a schema change nobody chose to run.
//
// Replacing Verify with Migrate here passed every suite — it silently reverses
// the invariant, and the only way to see it is to point serve at an unmigrated
// database and check that the schema is still not there afterwards.
func TestServeRefusesAnUnmigratedSchemaAndDoesNotCreateIt(t *testing.T) {
	t.Setenv(skeleton.EnvVar, "1")
	ctx := t.Context()

	dsn := os.Getenv("MARQUE_TEST_DSN")
	if dsn == "" {
		t.Fatal("MARQUE_TEST_DSN is unset; run `make test-integration`")
	}
	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = admin.Close() }()

	const name = "marque_serve_unmigrated"
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		t.Fatalf("dropping: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("creating: %v", err)
	}
	t.Cleanup(func() {
		a, err := store.Open(context.WithoutCancel(ctx), dsn)
		if err != nil {
			return
		}
		defer func() { _ = a.Close() }()
		_, _ = a.ExecContext(context.WithoutCancel(ctx), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})

	own := strings.Join(append(strings.Fields(withoutDBName(dsn)), "dbname="+name), " ")

	// Bounded, and in a goroutine, because the failure mode to catch is serve
	// SUCCEEDING: it would then migrate, start, and block until signalled.
	// Waiting on it directly turns that into a ten-minute test timeout instead
	// of an assertion.
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		// Port 0 so this cannot collide; it must fail before it ever binds.
		done <- run([]string{"serve", "-dsn=" + own, "-addr=127.0.0.1:0"}, &out)
	}()

	var err2 error
	select {
	case err2 = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not refuse an unmigrated schema; it is still running, which means it migrated")
	}
	if err2 == nil {
		t.Fatal("serve started against an unmigrated schema")
	}
	err = err2
	if !strings.Contains(err.Error(), "harbourmaster migrate") {
		t.Errorf("the refusal does not say how to fix it: %v", err)
	}

	// And it created nothing. This is the half that a Migrate would pass.
	fresh, err := store.Open(ctx, own)
	if err != nil {
		t.Fatalf("reconnecting: %v", err)
	}
	defer func() { _ = fresh.Close() }()
	var exists bool
	if err := fresh.QueryRowContext(ctx,
		`SELECT pg_catalog.to_regclass('public.requests') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("looking for the schema: %v", err)
	}
	if exists {
		t.Error("serve created the schema it was supposed to refuse")
	}
}

func withoutDBName(dsn string) string {
	var out []string
	for _, f := range strings.Fields(dsn) {
		if strings.HasPrefix(f, "dbname=") {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}
