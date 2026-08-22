package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sixfathoms/marque/internal/skeleton"
)

func TestDispatch(t *testing.T) {
	// An address nothing is listening on, so a command that gets past the gate
	// and the flag checks fails to connect rather than doing anything.
	const nowhere = "-harbourmaster=http://127.0.0.1:1"
	for name, c := range map[string]struct {
		args      []string
		wantErr   bool
		wantGated bool
	}{
		"no arguments":   {nil, true, false},
		"an unknown one": {[]string{"berth"}, true, false},
		"version":        {[]string{"version"}, false, false},
		"submit":         {[]string{"submit", nowhere, "-key=k"}, true, true},
		"approve":        {[]string{"approve", nowhere, "-reference=req_x", "-approver=sam"}, true, true},
		"status":         {[]string{"status", nowhere, "-reference=req_x"}, true, true},
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

// -key is required, and the refusal says why: choosing one for the caller
// would make a retry after a timeout a second request.
func TestSubmitRequiresAnIdempotencyKey(t *testing.T) {
	t.Setenv(skeleton.EnvVar, "1")
	var out bytes.Buffer
	err := run([]string{"submit", "-harbourmaster=http://127.0.0.1:1"}, &out)
	if err == nil {
		t.Fatal("submit without -key was accepted")
	}
	if !strings.Contains(err.Error(), "-key is required") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}

// There is no -stage flag. M1 has one stage, and offering a choice would
// suggest a policy exists to satisfy.
func TestApproveHasNoStageFlag(t *testing.T) {
	t.Setenv(skeleton.EnvVar, "1")
	var out bytes.Buffer
	err := run([]string{"approve", "-harbourmaster=http://127.0.0.1:1",
		"-reference=req_x", "-approver=sam", "-stage=2"}, &out)
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Errorf("-stage was accepted or failed for another reason: %v", err)
	}
}
