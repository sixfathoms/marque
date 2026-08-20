// Package skeleton gates M1's binaries behind an explicit acknowledgement that
// what they run is not secure.
//
// M1 is the walking skeleton: submit a statement, store it, approve it, run it,
// record the result. It has no signing, no grammar, no identity and no fence —
// which means an approval is an unauthenticated string, and a statement is
// whatever the operator typed. That is deliberate, because integration is worth
// proving before anything is deep, and it is dangerous, because the shape of
// the thing looks like the real one.
//
// So every binary refuses to start without MARQUE_INSECURE_SKELETON=1 and says
// on stderr what it is missing. The implementation plan commits M5 to deleting
// this package outright; TestTheSkeletonGateIsGone in this package asserts that
// and is skipped until then.
package skeleton

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// EnvVar is the acknowledgement. Spelled once, so the banner, the error and the
// test that removes it all name the same thing.
const EnvVar = "MARQUE_INSECURE_SKELETON"

// ErrNotAcknowledged is returned when the variable is unset or not exactly "1".
var ErrNotAcknowledged = errors.New("the insecure skeleton was not acknowledged")

// Require refuses unless the environment acknowledges the milestone, and prints
// the banner naming what is absent when it does.
//
// Exactly "1", not "any non-empty value": "0", "false" and "no" all mean no,
// and a gate that accepts them is a gate someone disables by accident while
// believing they turned it off.
func Require(binary string, env func(string) string, stderr io.Writer) error {
	if env(EnvVar) != "1" {
		// The explanation goes to stderr and the error stays one line: an
		// error is for a program deciding what to do, and a wall of prose is
		// for the person reading it.
		// Ignored deliberately: the refusal below is the outcome, and a
		// failure to explain it does not change the decision.
		_, _ = fmt.Fprintf(stderr,
			"\n  %s is M1's walking skeleton. It does not sign anything, does not parse\n"+
				"  the statement, does not authenticate the approver, and does not fence the\n"+
				"  write. An \"approval\" is a string the caller chose.\n\n"+
				"  Set %s=1 to run it anyway. M5 deletes this flag.\n\n",
			binary, EnvVar)
		return fmt.Errorf("%w: set %s=1", ErrNotAcknowledged, EnvVar)
	}

	// The banner goes to stderr, so piping stdout somewhere does not lose it.
	_, err := fmt.Fprint(stderr, Banner(binary))
	return err
}

// Banner is what Require prints. Separate so a test can read it without
// capturing a process's output.
func Banner(binary string) string {
	var b strings.Builder
	line := strings.Repeat("─", 74)
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "  %s — M1 WALKING SKELETON, NOT SECURE\n\n", strings.ToUpper(binary))
	for _, absent := range []string{
		"no signatures: nothing verifies who approved, or that anyone did",
		"no grammar: the statement is stored and run verbatim, unparsed",
		"no identity: the approver is a string the caller chose",
		"no fence: nothing bounds which rows the statement may touch",
	} {
		fmt.Fprintf(&b, "  · %s\n", absent)
	}
	fmt.Fprintf(&b, "\n  Running because %s=1.\n", EnvVar)
	fmt.Fprintf(&b, "%s\n", line)
	return b.String()
}

// FromEnv is Require against the real environment.
func FromEnv(binary string) error {
	return Require(binary, os.Getenv, os.Stderr)
}
