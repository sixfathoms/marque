package store_test

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// Drivers for target engines. EDR-0005's sentence is engine-agnostic and
// EDR-0026 plans MySQL, so this list grows with the engine list rather than
// naming PostgreSQL alone.
var targetEngineDrivers = []string{
	"github.com/jackc/pgx",
	"github.com/lib/pq",
	"github.com/go-sql-driver/mysql",
}

// Packages permitted to import one. The Harbourmaster's own store must, since
// EDR-0013 fixed Marque's own state on PostgreSQL; the Pilot's adapter must,
// since reaching a target is its entire job.
var permitted = []string{
	"github.com/sixfathoms/marque/internal/harbourmaster/store",
	"github.com/sixfathoms/marque/internal/pilot",
}

// TestDriverConfinement is EDR-0042's mechanism, and it is a dependency-graph
// test rather than a lint rule for a reason worth recording.
//
// EDR-0042 first specified this as a `depguard` rule. depguard does not report
// BLANK imports — and a database driver is imported blank essentially always,
// because the point is the driver's registration side effect. The rule could
// not see the one import it existed to police.
//
// That is measured rather than reasoned, and it was worth measuring twice: a
// reviewer read depguard's source and concluded it compares import paths
// without examining the alias, so blank and named should be indistinguishable.
// They are not, in the pinned version. Blank import of a denied package: no
// diagnostic. Named import of the same package: a diagnostic. Reproduced for a
// driver and for a standard-library package, so the axis is the blank alias and
// not anything about drivers.
//
// A dependency graph does not have that blind spot: `go list` reports every
// import, blank or not, which is what made EDR-0005's original sentence
// checkable in the first place. depguard stays enabled as a cheap redundant
// check on named imports; this test is the mechanism.
func TestDriverConfinement(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json",
		// A module-rooted pattern, NOT "./...": under `go test` the working
		// directory is the package under test, so "./..." would list only this
		// package and the check would pass vacuously — which it did, until a
		// probe that should have failed did not.
		"github.com/sixfathoms/marque/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	type pkg struct {
		ImportPath string
		Imports    []string
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	offenders := map[string][]string{}

	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		// Only first-party packages are ours to constrain; a dependency
		// importing a driver is a transitive link this cannot police, which
		// EDR-0042 names as one of the ways the rule is defeated.
		if !strings.HasPrefix(p.ImportPath, "github.com/sixfathoms/marque/") {
			continue
		}
		if slices.Contains(permitted, p.ImportPath) {
			continue
		}
		for _, imp := range p.Imports {
			for _, d := range targetEngineDrivers {
				if imp == d || strings.HasPrefix(imp, d+"/") {
					offenders[p.ImportPath] = append(offenders[p.ImportPath], imp)
				}
			}
		}
	}

	for path, imports := range offenders {
		t.Errorf(
			"%s imports %v.\n"+
				"  A driver for a target engine belongs in %v and nowhere else (EDR-0042).\n"+
				"  This is the mechanism replacing EDR-0005's \"no database driver for target\n"+
				"  engines linked in\", which stopped being available when EDR-0013 fixed\n"+
				"  Marque's own state on PostgreSQL.",
			path, imports, permitted)
	}
}

// TestDriverConfinementSeesBlankImports asserts the property that made the
// depguard version useless. If a future change moves this check to something
// that reads named imports only, this fails and says why.
func TestDriverConfinementSeesBlankImports(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "list", "-json",
		"github.com/sixfathoms/marque/internal/harbourmaster/store").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var p struct{ Imports []string }
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	// The store imports the driver blank. If `go list` did not report it, the
	// confinement test above would pass vacuously on every package.
	found := slices.ContainsFunc(p.Imports, func(s string) bool {
		return strings.HasPrefix(s, "github.com/jackc/pgx")
	})
	if !found {
		t.Fatal("go list did not report the store's blank driver import, so the confinement " +
			"test above cannot be trusted — it would pass whether or not a driver were linked")
	}
}
