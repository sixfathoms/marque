// Command pilot is the data plane: it holds target credentials by reference and
// the connection, and it never decides whether something may run (EDR-0001).
//
// At M0 it prints its version and exits. The walking skeleton lands in M1.
package main

import (
	"fmt"
	"os"

	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if _, err := fmt.Fprintln(os.Stdout, "pilot", version.Get()); err != nil {
		// Stdout is gone, so there is nowhere to report this. The exit code is
		// the only signal left.
		os.Exit(1)
	}
}
