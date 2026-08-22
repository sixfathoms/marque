//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/gen/marque/v1/marquev1connect"
	"github.com/sixfathoms/marque/internal/harbourmaster/api"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
	"github.com/sixfathoms/marque/internal/skeleton"
)

// The command's own paths, which unit tests over flag parsing cannot reach.
// Five mutations of this file survived both suites: deleting the approval
// check, skipping the report, swallowing the execution's error, and disabling
// the unknown-outcome refusal. The first is the Pilot's ONLY check that the
// control plane assented, and internal/e2e never covered it — that suite tests
// the HARBOURMASTER refusing a report, never the PILOT refusing to run.

const tenant = "development"

type world struct {
	addr  string
	dsn   string
	table string
	db    *sql.DB
}

func setUp(t *testing.T) world {
	t.Helper()
	t.Setenv(skeleton.EnvVar, "1")
	ctx := t.Context()

	dsn := os.Getenv("MARQUE_TEST_DSN")
	if dsn == "" {
		t.Fatal("MARQUE_TEST_DSN is unset; run `make test-integration`")
	}
	if strings.Contains(dsn, "://") {
		t.Fatal("MARQUE_TEST_DSN must be a keyword/value DSN, not a URL")
	}

	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer func() { _ = admin.Close() }()

	name := "marque_cmd_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	if len(name) > 60 {
		name = name[:60]
	}
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
		t.Fatalf("dropping: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("creating: %v", err)
	}
	t.Cleanup(func() {
		a, err := store.Open(context.WithoutCancel(ctx), dsn)
		if err != nil {
			return
		}
		defer func() { _ = a.Close() }()
		_, _ = a.ExecContext(context.WithoutCancel(ctx), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})

	own := strings.Join(append(strings.Fields(strings.ReplaceAll(dsn, "dbname="+dbName(dsn), "")), "dbname="+name), " ")
	control, err := store.Open(ctx, own)
	if err != nil {
		t.Fatalf("connecting to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = control.Close() })
	if err := store.Migrate(ctx, control); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(marquev1connect.NewHarbourmasterServiceHandler(api.New(store.New(control), tenant)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The target lives in the same server, in its own table.
	table := "accounts_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	if _, err := control.ExecContext(ctx,
		`CREATE TABLE `+table+` (id integer PRIMARY KEY, tier integer NOT NULL);
		 INSERT INTO `+table+` (id, tier) SELECT i, 1 FROM generate_series(1, 5) AS i`); err != nil {
		t.Fatalf("creating the target table: %v", err)
	}

	return world{addr: srv.URL, dsn: own, table: table, db: control}
}

func dbName(dsn string) string {
	for _, f := range strings.Fields(dsn) {
		if after, ok := strings.CutPrefix(f, "dbname="); ok {
			return after
		}
	}
	return ""
}

func (w world) submit(t *testing.T, statement string) string {
	t.Helper()
	c := marquev1connect.NewHarbourmasterServiceClient(http.DefaultClient, w.addr)
	res, err := c.Submit(t.Context(), connect.NewRequest(&v1.SubmitRequest{
		Statement: statement, Target: "prod-primary", Role: "marque_writer",
		Reason: "a command test", IdempotencyKey: t.Name(),
	}))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	return res.Msg.GetReference()
}

func (w world) approve(t *testing.T, ref string) {
	t.Helper()
	c := marquev1connect.NewHarbourmasterServiceClient(http.DefaultClient, w.addr)
	if _, err := c.Approve(t.Context(), connect.NewRequest(&v1.ApproveRequest{
		Reference: ref, Approver: "sam", Stage: 1,
	})); err != nil {
		t.Fatalf("approving: %v", err)
	}
}

func (w world) state(t *testing.T, ref string) v1.RequestState {
	t.Helper()
	c := marquev1connect.NewHarbourmasterServiceClient(http.DefaultClient, w.addr)
	res, err := c.GetRequest(t.Context(), connect.NewRequest(&v1.GetRequestRequest{Reference: ref}))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	return res.Msg.GetRequest().GetState()
}

func (w world) changed(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+w.table+` WHERE tier <> 1`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	return n
}

// The Pilot's only check that the control plane assented. Deleting it was
// undetectable.
func TestExecuteRefusesAnUnapprovedRequest(t *testing.T) {
	w := setUp(t)
	ref := w.submit(t, `UPDATE `+w.table+` SET tier = 2`)

	var out bytes.Buffer
	err := run([]string{"execute", "-harbourmaster=" + w.addr,
		"-reference=" + ref, "-target-dsn=" + w.dsn, "-nonce=n1"}, &out)
	if err == nil {
		t.Fatal("the Pilot ran a statement the control plane had not approved")
	}
	if !strings.Contains(err.Error(), "not approved") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if n := w.changed(t); n != 0 {
		t.Errorf("%d rows changed for an unapproved request", n)
	}
}

// It runs, it reports, and the report is what moves the request.
func TestExecuteRunsAndReports(t *testing.T) {
	w := setUp(t)
	ref := w.submit(t, `UPDATE `+w.table+` SET tier = 2 WHERE id <= 3`)
	w.approve(t, ref)

	var out bytes.Buffer
	if err := run([]string{"execute", "-harbourmaster=" + w.addr,
		"-reference=" + ref, "-target-dsn=" + w.dsn, "-nonce=n1"}, &out); err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !strings.Contains(out.String(), "COMMITTED") || !strings.Contains(out.String(), "rows=3") {
		t.Errorf("it printed %q", out.String())
	}
	if got := w.state(t, ref); got != v1.RequestState_REQUEST_STATE_EXECUTED {
		t.Errorf("the request is %s; the report is what should have moved it", got)
	}
	if n := w.changed(t); n != 3 {
		t.Errorf("%d rows changed, want 3", n)
	}
}

// A statement that fails is still reported, and the command still exits
// non-zero — a Pilot that runs a statement and says nothing is the worst
// outcome available, and one that says nothing about failing is the second.
func TestAFailedStatementIsReportedAndExitsNonZero(t *testing.T) {
	w := setUp(t)
	ref := w.submit(t, `UPDATE `+w.table+` SET tier = 'not a number'`)
	w.approve(t, ref)

	var out bytes.Buffer
	err := run([]string{"execute", "-harbourmaster=" + w.addr,
		"-reference=" + ref, "-target-dsn=" + w.dsn, "-nonce=n1"}, &out)
	if err == nil {
		t.Error("a statement that could not run reported success to the caller")
	}
	// Reported anyway: the control plane must know an attempt happened.
	if !strings.Contains(out.String(), "ABORTED_NOT_APPLIED") {
		t.Errorf("the outcome was not reported: %q", out.String())
	}
	// A clean abort leaves the request runnable.
	if got := w.state(t, ref); got != v1.RequestState_REQUEST_STATE_APPROVED {
		t.Errorf("the request is %s after a clean abort; it should still be runnable", got)
	}
	if n := w.changed(t); n != 0 {
		t.Errorf("%d rows changed by a statement that failed", n)
	}
}
