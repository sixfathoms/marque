// Command pilot runs one approved statement against a target and reports what
// happened.
//
// It is the only component that touches a target, and it is the only one that
// holds a target credential (EDR-0005). At M1 it verifies nothing: there is no
// marque to check, no fence to apply and no rehearsal to run, so what it does
// is fetch a request, refuse unless the control plane says it is approved, run
// it, and report the outcome.
//
// One statement per invocation, deliberately. A long-lived Pilot with a queue
// is a thing that runs statements without anyone watching, and M1 is not where
// that should first exist.
//
// The nonce makes the REPORT idempotent and not the execution, which is a
// weaker thing than it sounds and is worth being exact about. Re-running this
// command with a nonce already recorded is refused, because the request is
// terminal by then. But if the FIRST report never landed — the process died
// between the commit and the call — the request is still approved, and running
// the command again executes the statement a second time before anything looks
// at the nonce.
//
// That is exactly what EDR-0011's ledger prevents, by claiming the nonce
// BEFORE the statement runs and letting a crash lose the attempt rather than
// the count. M1 does not have it, and where it should live is issue #34.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"connectrpc.com/connect"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/gen/marque/v1/marquev1connect"
	"github.com/sixfathoms/marque/internal/pilot"
	"github.com/sixfathoms/marque/internal/pilot/postgres"
	"github.com/sixfathoms/marque/internal/skeleton"
	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pilot:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pilot <version|execute> [flags]")
	}
	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, "pilot", version.Get())
		return err
	case "execute":
		return execute(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q: expected version or execute", args[0])
	}
}

func execute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("execute", flag.ContinueOnError)
	addr := fs.String("harbourmaster", "http://127.0.0.1:8080", "base URL of the control plane")
	reference := fs.String("reference", "", "the req_… to execute")
	dsn := fs.String("target-dsn", "", "connection string for the TARGET database; the control plane never sees this (EDR-0005)")
	nonce := fs.String("nonce", "", "identifies this attempt to the control plane; it makes the REPORT idempotent, not the execution — see below")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reference == "" || *dsn == "" || *nonce == "" {
		return errors.New("-reference, -target-dsn and -nonce are all required")
	}
	if err := skeleton.FromEnv("pilot"); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hm := marquev1connect.NewHarbourmasterServiceClient(http.DefaultClient, *addr)

	got, err := hm.GetRequest(ctx, connect.NewRequest(&v1.GetRequestRequest{Reference: *reference}))
	if err != nil {
		return fmt.Errorf("fetching %s: %w", *reference, err)
	}
	req := got.Msg.GetRequest()

	// The control plane decides whether this may run. Executing something it
	// calls pending would make the Pilot the thing that authorises work, which
	// is the arrangement the whole design exists to refuse.
	if req.GetState() != v1.RequestState_REQUEST_STATE_APPROVED {
		return fmt.Errorf("%s is %s, not approved", *reference, req.GetState())
	}

	target, err := postgres.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer func() { _ = target.Close() }()

	result, execErr := pilot.Execute(ctx, target, req.GetStatement(), postgres.CommitWasRefused)

	// Reported whatever happened. A Pilot that runs a statement and then says
	// nothing is the worst of the available outcomes: the control plane's
	// record would show a request that was approved and never resolved, and
	// nobody would know whether the database had changed.
	outcome, ok := outcomes[result.Outcome]
	if !ok {
		return fmt.Errorf("the execution produced outcome %q, which is not one of the four (EDR-0042)", result.Outcome)
	}
	reported, err := hm.RecordExecution(ctx, connect.NewRequest(&v1.RecordExecutionRequest{
		Reference:    *reference,
		Nonce:        *nonce,
		Outcome:      outcome,
		RowsAffected: result.RowsAffected,
	}))
	if err != nil {
		return fmt.Errorf("the statement produced %s and the report failed, so the control plane does not know: %w", result.Outcome, err)
	}

	stored := reported.Msg.GetExecution()
	if _, err := fmt.Fprintf(stdout, "%s %s", *reference, stored.GetOutcome()); err != nil {
		return err
	}
	if stored.RowsAffected != nil {
		if _, err := fmt.Fprintf(stdout, " rows=%d", stored.GetRowsAffected()); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	// The execution's own failure is reported and then returned, so a caller
	// scripting this sees a non-zero exit for a statement that did not commit.
	return execErr
}

// The Pilot does not link the control plane's storage, so it maps its own
// outcome strings onto the wire enum here.
var outcomes = map[string]v1.ExecutionOutcome{
	pilot.OutcomeCommitted:         v1.ExecutionOutcome_EXECUTION_OUTCOME_COMMITTED,
	pilot.OutcomeRolledBack:        v1.ExecutionOutcome_EXECUTION_OUTCOME_ROLLED_BACK,
	pilot.OutcomeAbortedNotApplied: v1.ExecutionOutcome_EXECUTION_OUTCOME_ABORTED_NOT_APPLIED,
	pilot.OutcomeIndeterminate:     v1.ExecutionOutcome_EXECUTION_OUTCOME_INDETERMINATE,
}
