package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sixfathoms/marque/internal/skeleton"
)

// The dispatch and the gate's PLACEMENT, which is the part that matters: a
// command that reaches a database before asking is a command that did not ask.
func TestDispatch(t *testing.T) {
	for name, c := range map[string]struct {
		args      []string
		wantErr   bool
		wantGated bool
	}{
		"no arguments":          {nil, true, false},
		"an unknown one":        {[]string{"sail"}, true, false},
		"version":               {[]string{"version"}, false, false},
		"migrate without a dsn": {[]string{"migrate"}, true, false},
		// Gated BEFORE the database is opened. -dsn is deliberately nonsense:
		// if the gate ran after Open, this would fail to connect instead.
		"migrate with a dsn": {[]string{"migrate", "-dsn=host=203.0.113.1 connect_timeout=1"}, true, true},
		"serve with a dsn":   {[]string{"serve", "-dsn=host=203.0.113.1 connect_timeout=1"}, true, true},
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

// version must work without the acknowledgement: inspecting a binary should
// not require agreeing to what it would do if you ran it.
func TestVersionNeedsNoAcknowledgement(t *testing.T) {
	t.Setenv(skeleton.EnvVar, "")
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(out.String(), "harbourmaster ") {
		t.Errorf("version printed %q", out.String())
	}
	if strings.Contains(out.String(), "NOT SECURE") {
		t.Error("version printed the banner; it does nothing that needs one")
	}
}
