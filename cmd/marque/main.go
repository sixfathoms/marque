// Command marque is the operator's command-line client, and the primary
// surface: submit a statement, watch a request, execute a marque.
//
// At M0 it prints its version and exits. The walking skeleton lands in M1.
package main

import (
	"fmt"
	"os"

	"github.com/sixfathoms/marque/internal/version"
)

func main() {
	if _, err := fmt.Fprintln(os.Stdout, "marque", version.Get()); err != nil {
		// Stdout is gone, so there is nowhere to report this. The exit code is
		// the only signal left.
		os.Exit(1)
	}
}
