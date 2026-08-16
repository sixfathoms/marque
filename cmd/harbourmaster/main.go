// Command harbourmaster is the control plane: it holds requests, policy,
// delegations and the logbook, and it never touches a target database or holds
// a target credential (EDR-0001, EDR-0005).
//
// At M0 it prints its version and exits. The walking skeleton lands in M1.
package main

import (
	"fmt"
	"os"

	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if _, err := fmt.Fprintln(os.Stdout, "harbourmaster", version.Get()); err != nil {
		// Stdout is gone, so there is nowhere to report this. The exit code is
		// the only signal left.
		os.Exit(1)
	}
}
