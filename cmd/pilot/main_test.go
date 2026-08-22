package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/internal/pilot"
	"github.com/sixfathoms/marque/internal/skeleton"
)

func TestDispatch(t *testing.T) {
	const nowhere = "-harbourmaster=http://127.0.0.1:1"
	for name, c := range map[string]struct {
		args      []string
		wantErr   bool
		wantGated bool
	}{
		"no arguments":          {nil, true, false},
		"an unknown one":        {[]string{"steer"}, true, false},
		"version":               {[]string{"version"}, false, false},
		"execute with no flags": {[]string{"execute"}, true, false},
		// Gated before anything is fetched or opened.
		"execute with every flag": {[]string{"execute", nowhere,
			"-reference=req_x", "-target-dsn=host=203.0.113.1 connect_timeout=1", "-nonce=n"}, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(skeleton.EnvVar, "")
			var out bytes.Buffer
			err := run(c.args, &out)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, c.wantErr)
			}
			if c.wantGated && !errors.Is(err, skeleton.ErrNotAcknowledged) {
				t.Errorf("this reached past the gate: %v", err)
			}
			if !c.wantGated && errors.Is(err, skeleton.ErrNotAcknowledged) {
				t.Errorf("this was gated and should not be: %v", err)
			}
		})
	}
}

func TestExecuteRequiresEveryFlag(t *testing.T) {
	t.Setenv(skeleton.EnvVar, "1")
	for _, missing := range []string{"-reference=", "-target-dsn=", "-nonce="} {
		args := []string{"execute", "-harbourmaster=http://127.0.0.1:1",
			"-reference=req_x", "-target-dsn=x", "-nonce=n"}
		for i, a := range args {
			if strings.HasPrefix(a, missing) {
				args[i] = missing
			}
		}
		var out bytes.Buffer
		err := run(args, &out)
		if err == nil || !strings.Contains(err.Error(), "are all required") {
			t.Errorf("missing %s: %v", missing, err)
		}
	}
}

// Every outcome the Pilot can produce must have a wire value. A gap would make
// a real execution unreportable, which is the worst outcome available: the
// statement ran and the control plane never hears.
func TestEveryPilotOutcomeHasAWireValue(t *testing.T) {
	for _, o := range []string{
		pilot.OutcomeCommitted, pilot.OutcomeRolledBack,
		pilot.OutcomeAbortedNotApplied, pilot.OutcomeIndeterminate,
	} {
		got, ok := outcomes[o]
		if !ok {
			t.Errorf("the Pilot can produce %q and it has no wire value", o)
			continue
		}
		if got == v1.ExecutionOutcome_EXECUTION_OUTCOME_UNSPECIFIED {
			t.Errorf("%q maps to UNSPECIFIED", o)
		}
	}
	if len(outcomes) != 4 {
		t.Errorf("%d outcomes are mapped; EDR-0042 decides four", len(outcomes))
	}
}
