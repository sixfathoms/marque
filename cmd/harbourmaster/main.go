// Command harbourmaster is Marque's control plane.
//
// It records what an operator asked for, records that someone approved it, and
// records what the Pilot reported. It holds no target credential and opens no
// connection to anything but its own database (EDR-0005).
//
// It DOES link a PostgreSQL driver — for that database. EDR-0013 fixed Marque's
// own state on PostgreSQL, which is also a target engine, so "no target driver
// linked in" stopped being achievable and EDR-0042 replaced it with import
// discipline. Saying otherwise here would reinstate a claim that record
// retracts at length.
//
// M1 is the walking skeleton and is not secure: see internal/skeleton, and the
// banner every start prints.
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
	"time"

	"github.com/sixfathoms/marque/gen/marque/v1/marquev1connect"
	"github.com/sixfathoms/marque/internal/harbourmaster/api"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
	"github.com/sixfathoms/marque/internal/skeleton"
	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "harbourmaster:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: harbourmaster <version|migrate|serve> [flags]")
	}
	switch args[0] {
	case "version":
		_, err := fmt.Fprintln(stdout, "harbourmaster", version.Get())
		return err
	case "migrate":
		return migrate(args[1:], stdout)
	case "serve":
		return serve(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q: expected version, migrate or serve", args[0])
	}
}

// migrate is an explicit command and never a side effect of starting.
// Migrating implicitly at startup turns every deploy into a schema change
// nobody chose to run (EDR-0042).
func migrate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dsn := fs.String("dsn", "", "connection string for the control plane's own database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dsn == "" {
		return errors.New("-dsn is required")
	}
	// Gated too. It opens the control plane's database and changes its schema,
	// which is not a thing to do to an M1 deployment by accident — and the
	// plan says every binary refuses to start, which was not true while this
	// path did not ask.
	if err := skeleton.FromEnv("harbourmaster"); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := store.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := store.Migrate(ctx, db); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "schema is up to date")
	return err
}

func serve(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dsn := fs.String("dsn", "", "connection string for the control plane's own database")
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on")
	tenant := fs.String("tenant", "development", "tenant these requests belong to; configuration at M1, and the authenticated principal's from M4 (EDR-0025)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dsn == "" {
		return errors.New("-dsn is required")
	}
	if err := skeleton.FromEnv("harbourmaster"); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := store.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Verified, not migrated. Startup refuses to serve against a schema this
	// binary does not match, rather than quietly changing it (EDR-0042).
	if err := store.Verify(ctx, db); err != nil {
		return fmt.Errorf("the schema is not the one this binary expects, and `harbourmaster migrate` is the way to change that: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(marquev1connect.NewHarbourmasterServiceHandler(
		api.New(store.New(db), *tenant)))

	// Unencrypted HTTP/2 alongside HTTP/1.1, so a Connect client can use
	// either. Through net/http's own Protocols rather than x/net's h2c, which
	// is deprecated — and which would have been a dependency this needs for one
	// line.
	//
	// No TLS: M1 serves on loopback by default, and terminating TLS belongs
	// with identity at M4. That is one more thing the startup banner is for.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		if _, err := fmt.Fprintln(stdout, "harbourmaster listening on", *addr); err != nil {
			errs <- err
			return
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdown, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		return srv.Shutdown(shutdown)
	}
}
