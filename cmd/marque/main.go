// Command marque is the operator's client.
//
// It submits a statement, approves one, and asks what happened. At M1 it does
// all three over plain HTTP with no identity behind any of them: an approval is
// a name the caller typed. That is the walking skeleton, and every invocation
// says so.
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
	"github.com/sixfathoms/marque/internal/skeleton"
	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "marque:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: marque <version|submit|approve|status> [flags]")
	}
	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, "marque", version.Get())
		return err
	case "submit":
		return submit(args[1:], stdout)
	case "approve":
		return approve(args[1:], stdout)
	case "status":
		return status(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q: expected version, submit, approve or status", args[0])
	}
}

func client(addr string) marquev1connect.HarbourmasterServiceClient {
	return marquev1connect.NewHarbourmasterServiceClient(http.DefaultClient, addr)
}

func withHarbourmaster(fs *flag.FlagSet) *string {
	return fs.String("harbourmaster", "http://127.0.0.1:8080", "base URL of the control plane")
}

func started(args []string, fs *flag.FlagSet) (context.Context, context.CancelFunc, error) {
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if err := skeleton.FromEnv("marque"); err != nil {
		return nil, nil, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx, cancel, nil
}

func submit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	addr := withHarbourmaster(fs)
	statement := fs.String("statement", "", "the SQL to run, verbatim")
	target := fs.String("target", "", "the database it is meant for, by name")
	role := fs.String("role", "", "the database identity it should run as")
	reason := fs.String("reason", "", "why, in your words — an approver cannot judge a request without one")
	key := fs.String("key", "", "idempotency key; a retried submission with the same key is one request")

	ctx, cancel, err := started(args, fs)
	if err != nil {
		return err
	}
	defer cancel()

	if *key == "" {
		return errors.New("-key is required: it is what makes a retried submission one request rather than two, and choosing it for you would make a retry after a timeout a second request")
	}

	res, err := client(*addr).Submit(ctx, connect.NewRequest(&v1.SubmitRequest{
		Statement:      *statement,
		Target:         *target,
		Role:           *role,
		Reason:         *reason,
		IdempotencyKey: *key,
	}))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, res.Msg.GetReference())
	return err
}

func approve(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	addr := withHarbourmaster(fs)
	reference := fs.String("reference", "", "the req_… to approve")
	approver := fs.String("approver", "", "who you are; at M1 nothing verifies this")

	ctx, cancel, err := started(args, fs)
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := client(*addr).Approve(ctx, connect.NewRequest(&v1.ApproveRequest{
		Reference: *reference,
		Approver:  *approver,
		// M1 has no escalation chain, so there is one stage and no flag for it.
		// Offering a choice would suggest a policy exists to satisfy.
		Stage: 1,
	})); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "approved", *reference)
	return err
}

func status(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	addr := withHarbourmaster(fs)
	reference := fs.String("reference", "", "the req_… to look up")

	ctx, cancel, err := started(args, fs)
	if err != nil {
		return err
	}
	defer cancel()

	res, err := client(*addr).GetRequest(ctx, connect.NewRequest(&v1.GetRequestRequest{
		Reference: *reference,
	}))
	if err != nil {
		return err
	}
	r := res.Msg.GetRequest()
	for _, line := range [][2]string{
		{"reference", r.GetReference()},
		{"state", r.GetState().String()},
		{"target", r.GetTarget()},
		{"role", r.GetRole()},
		{"submitter", r.GetSubmitter()},
		{"reason", r.GetReason()},
		{"statement", r.GetStatement()},
	} {
		if _, err := fmt.Fprintf(stdout, "%-10s %s\n", line[0], line[1]); err != nil {
			return err
		}
	}
	return nil
}
