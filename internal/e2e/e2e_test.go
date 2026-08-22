//go:build integration

// Package e2e runs M1's six steps against a real PostgreSQL.
//
// Submit a statement → it is stored → approve it → run it against a target →
// the result and the statement land in a table. That sentence is the milestone,
// and this is the test that says it happened rather than that each piece works
// alone.
//
// It uses the real service, the real store and the real Pilot over a real HTTP
// connection. The one thing it does not use is the binaries' own flag parsing,
// which is cmd/'s.
package e2e

import (
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
	"github.com/sixfathoms/marque/internal/pilot"
	"github.com/sixfathoms/marque/internal/pilot/postgres"
)

const tenant = "development"

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("MARQUE_TEST_DSN")
	if d == "" {
		t.Fatal("MARQUE_TEST_DSN is unset; run `make test-integration`")
	}
	return d
}

// freshControlPlane creates a database for one test and drops it afterwards.
func freshControlPlane(t *testing.T) *sql.DB {
	t.Helper()
	ctx := t.Context()

	admin, err := store.Open(ctx, dsn(t))
	if err != nil {
		t.Fatalf("connecting to the control plane's database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	name := "marque_e2e_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
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
		a, err := store.Open(context.WithoutCancel(ctx), dsn(t))
		if err != nil {
			return
		}
		defer func() { _ = a.Close() }()
		_, _ = a.ExecContext(context.WithoutCancel(ctx), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})

	db, err := store.Open(ctx, replaceDBName(dsn(t), name))
	if err != nil {
		t.Fatalf("connecting to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// replaceDBName rewrites the dbname in a keyword/value DSN.
//
// It handles that form only, and says so: given a URL it would append a
// dbname= token that the URL parser ignores, and every test would silently
// share one database again — which is the failure this whole helper exists to
// prevent, so it is refused rather than papered over.
func replaceDBName(d, name string) string {
	if strings.Contains(d, "://") {
		panic("MARQUE_TEST_DSN must be a keyword/value DSN, not a URL: this rewrites dbname= and would silently share one database otherwise")
	}
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

// world is a migrated control plane, a served API, and a target table.
type world struct {
	client marquev1connect.HarbourmasterServiceClient
	target *sql.DB
	table  string
}

func setUp(t *testing.T) world {
	t.Helper()
	ctx := t.Context()

	// A control-plane database of this test's own.
	//
	// Sharing one made this suite green exactly once per database: the
	// idempotency keys below are fixed, so a second run replayed the first
	// run's requests and found them already executed. A reviewer ran it with
	// -count=2 and watched it fail. CI never noticed because the Makefile hands
	// it a brand-new container each time — which means the milestone's own exit
	// criterion could not be re-run on a developer's own server, and that is
	// exactly where someone would run it.
	control := freshControlPlane(t)
	if err := store.Migrate(ctx, control); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle(marquev1connect.NewHarbourmasterServiceHandler(
		api.New(store.New(control), tenant)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// A separate CONNECTION, opened through the Pilot's adapter — but the same
	// DSN, so this does not demonstrate the credential separation EDR-0005 is
	// about. What it does demonstrate is the code path: the control plane above
	// never receives this string, and the Pilot is what opens it. Two
	// credentials would need two roles and two grants, which is M4's.
	target, err := postgres.Open(ctx, dsn(t))
	if err != nil {
		t.Fatalf("connecting to the target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })

	// The neutral fictional schema the repository uses in examples.
	table := "accounts_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	if _, err := target.ExecContext(ctx,
		`DROP TABLE IF EXISTS `+table+`; CREATE TABLE `+table+` (id integer PRIMARY KEY, tier integer NOT NULL)`); err != nil {
		t.Fatalf("creating the target table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = target.ExecContext(context.WithoutCancel(ctx), `DROP TABLE IF EXISTS `+table)
	})
	if _, err := target.ExecContext(ctx,
		`INSERT INTO `+table+` (id, tier) SELECT i, 1 FROM generate_series(1, 5) AS i`); err != nil {
		t.Fatalf("seeding the target table: %v", err)
	}

	return world{
		client: marquev1connect.NewHarbourmasterServiceClient(srv.Client(), srv.URL),
		target: target,
		table:  table,
	}
}

// The milestone, in one test.
func TestTheSixSteps(t *testing.T) {
	w := setUp(t)
	ctx := t.Context()
	statement := `UPDATE ` + w.table + ` SET tier = 2 WHERE id <= 3`

	// 1. An operator submits a statement.
	submitted, err := w.client.Submit(ctx, connect.NewRequest(&v1.SubmitRequest{
		Statement:      statement,
		Target:         "prod-primary",
		Role:           "marque_writer",
		Reason:         "raising three accounts after a billing correction",
		IdempotencyKey: "e2e-1",
	}))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	reference := submitted.Msg.GetReference()

	// 2. It is stored, and readable by the reference an operator would paste.
	got, err := w.client.GetRequest(ctx, connect.NewRequest(&v1.GetRequestRequest{Reference: reference}))
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if got.Msg.GetRequest().GetState() != v1.RequestState_REQUEST_STATE_PENDING {
		t.Fatalf("a fresh request is %s, want PENDING", got.Msg.GetRequest().GetState())
	}
	if got.Msg.GetRequest().GetStatement() != statement {
		t.Errorf("the stored statement is not the one submitted")
	}

	// 3. Nothing may run before it is approved. The Pilot asks the control
	//    plane, and the control plane is what decides.
	if got.Msg.GetRequest().GetState() == v1.RequestState_REQUEST_STATE_APPROVED {
		t.Fatal("a request was approved by being submitted")
	}
	rows := int64(3)
	if _, err := w.client.RecordExecution(ctx, connect.NewRequest(&v1.RecordExecutionRequest{
		Reference: reference, Nonce: "early", RowsAffected: &rows,
		Outcome: v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED,
	})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("reporting an execution of an unapproved request: code is %v, want FailedPrecondition",
			connect.CodeOf(err))
	}

	// 4. A human approves it.
	if _, err := w.client.Approve(ctx, connect.NewRequest(&v1.ApproveRequest{
		Reference: reference, Approver: "sam", Stage: 1,
	})); err != nil {
		t.Fatalf("approving: %v", err)
	}

	// 5. The Pilot runs it against the target.
	after, err := w.client.GetRequest(ctx, connect.NewRequest(&v1.GetRequestRequest{Reference: reference}))
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if after.Msg.GetRequest().GetState() != v1.RequestState_REQUEST_STATE_APPROVED {
		t.Fatalf("state is %s after approval", after.Msg.GetRequest().GetState())
	}
	result, err := pilot.Execute(ctx, w.target, after.Msg.GetRequest().GetStatement(), postgres.RunOne, postgres.CommitWasRefused)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.Outcome != pilot.OutcomeCommitted {
		t.Fatalf("outcome is %s, want committed", result.Outcome)
	}

	// 6. The result and the statement land in a table.
	reported, err := w.client.RecordExecution(ctx, connect.NewRequest(&v1.RecordExecutionRequest{
		Reference:    reference,
		Nonce:        "attempt-1",
		Outcome:      v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED,
		RowsAffected: result.RowsAffected,
	}))
	if err != nil {
		t.Fatalf("reporting: %v", err)
	}
	if reported.Msg.GetExecution().GetRowsAffected() != 3 {
		t.Errorf("recorded %d rows, want 3", reported.Msg.GetExecution().GetRowsAffected())
	}

	final, err := w.client.GetRequest(ctx, connect.NewRequest(&v1.GetRequestRequest{Reference: reference}))
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if final.Msg.GetRequest().GetState() != v1.RequestState_REQUEST_STATE_EXECUTED {
		t.Errorf("final state is %s, want EXECUTED", final.Msg.GetRequest().GetState())
	}

	// And the thing the operator actually wanted: the row changed.
	var changed int
	if err := w.target.QueryRowContext(ctx,
		`SELECT count(*) FROM `+w.table+` WHERE tier = 2`).Scan(&changed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if changed != 3 {
		t.Errorf("%d rows have tier 2, want 3 — every step reported success and the database did not change", changed)
	}
}

// A retried REPORT learns what was recorded the first time, and writes nothing
// new. Note what this does not show: it retries the report, not the execution.
// A Pilot whose first report never landed would find the request still approved
// and run the statement again — EDR-0011's ledger is what would stop that, and
// M1 has none (issue #34).
func TestARetriedReportLearnsTheStoredOutcome(t *testing.T) {
	w := setUp(t)
	ctx := t.Context()

	submitted, err := w.client.Submit(ctx, connect.NewRequest(&v1.SubmitRequest{
		Statement:      `UPDATE ` + w.table + ` SET tier = tier + 1`,
		Target:         "prod-primary",
		Role:           "marque_writer",
		Reason:         "a retry",
		IdempotencyKey: "e2e-retry",
	}))
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	reference := submitted.Msg.GetReference()
	if _, err := w.client.Approve(ctx, connect.NewRequest(&v1.ApproveRequest{
		Reference: reference, Approver: "sam", Stage: 1,
	})); err != nil {
		t.Fatalf("approving: %v", err)
	}

	result, err := pilot.Execute(ctx, w.target, `UPDATE `+w.table+` SET tier = tier + 1`, postgres.RunOne, postgres.CommitWasRefused)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}

	// The first report, then a retry under the same nonce claiming something
	// else. The second must be told what is stored.
	if _, err := w.client.RecordExecution(ctx, connect.NewRequest(&v1.RecordExecutionRequest{
		Reference: reference, Nonce: "one", RowsAffected: result.RowsAffected,
		Outcome: v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED,
	})); err != nil {
		t.Fatalf("reporting: %v", err)
	}
	retried, err := w.client.RecordExecution(ctx, connect.NewRequest(&v1.RecordExecutionRequest{
		Reference: reference, Nonce: "one",
		Outcome: v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE,
	}))
	if err != nil {
		t.Fatalf("retrying the report: %v", err)
	}
	if retried.Msg.GetExecution().GetOutcome() != v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED {
		t.Errorf("the retry was told %s; committed is what was stored",
			retried.Msg.GetExecution().GetOutcome())
	}

	// The statement ran once, so every tier went up by exactly one.
	var maxTier int
	if err := w.target.QueryRowContext(ctx, `SELECT max(tier) FROM `+w.table).Scan(&maxTier); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if maxTier != 2 {
		t.Errorf("the highest tier is %d, want 2 — the statement ran more than once", maxTier)
	}
}

// A resubmission with the same key is one request, over the wire and not just
// in the store.
func TestResubmittingIsOneRequest(t *testing.T) {
	w := setUp(t)
	ctx := t.Context()
	send := func() string {
		t.Helper()
		res, err := w.client.Submit(ctx, connect.NewRequest(&v1.SubmitRequest{
			Statement:      `UPDATE ` + w.table + ` SET tier = 9`,
			Target:         "prod-primary",
			Role:           "marque_writer",
			Reason:         "the same request twice",
			IdempotencyKey: "e2e-same",
		}))
		if err != nil {
			t.Fatalf("submitting: %v", err)
		}
		return res.Msg.GetReference()
	}
	if first, second := send(), send(); first != second {
		t.Errorf("one key produced %s and %s", first, second)
	}
}
