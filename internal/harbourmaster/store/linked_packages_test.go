package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The confinement rule's PRIMARY mechanism: ask the toolchain what each binary
// actually links, rather than inferring it from the filesystem.
//
// A filesystem walk was the mechanism for two rounds and was defeated five
// times, the last three by the same shape of mistake — a directory the walk
// decided not to enter and the compiler enters anyway. `testdata`, `_x` and
// `.x` are the sharpest: the Go toolchain ignores those names when EXPANDING A
// WILDCARD, not when resolving an import, so `go build ./internal/x/testdata/pg`
// compiles perfectly well and an explicit import of it links a driver. Two
// reviewers and codex found that independently, one round after the walk's skip
// rule was rewritten to be "principled".
//
// A symlinked directory was defeated the same way twice: first because the walk
// did not descend it, then — after it followed them — because a global visited
// set keyed on the resolved path meant a directory reachable by two names was
// recorded only under whichever os.ReadDir reached first. Alphabetical order
// was the entire defence.
//
// go list has none of those failure modes, and the reason is not that it is a
// better tool: it is that it answers a different question. The walk asks what
// files exist; this asks what the binary links. The second question is the one
// the rule is about.
//
// It is still not capability containment, and EDR-0042 says at length what it
// is: a guard against a driver arriving by accident. TestCgoIsConfined below
// covers the path that has no Go import at all, and the record lists what
// remains.

// buildConfig is one (pattern, tags, test) triple. All of them are checked,
// because a package invisible to one is not invisible to the compiler: the
// integration tests live behind a build tag, and a test binary links packages
// no shipped binary does.
type buildConfig struct {
	name    string
	pattern string
	tags    string
	test    bool
}

var buildConfigs = []buildConfig{
	{"the shipped binaries", "./cmd/...", "", false},
	{"the shipped binaries, tagged", "./cmd/...", "integration", false},
	{"every package", "./...", "", false},
	{"every package, tagged", "./...", "integration", false},
	{"every test binary", "./...", "", true},
	{"every test binary, tagged", "./...", "integration", true},
}

type listedPackage struct {
	ImportPath string
	Imports    []string
	Deps       []string
	CgoFiles   []string
	CgoLDFLAGS []string
}

// goList runs `go list -deps -json` for one configuration. -mod=readonly so a
// check cannot rewrite go.mod as a side effect of running.
func goList(t *testing.T, c buildConfig) []listedPackage {
	t.Helper()
	args := []string{"list", "-mod=readonly", "-deps", "-json"}
	if c.test {
		args = append(args, "-test")
	}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	args = append(args, c.pattern)

	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = repoRoot(t)
	// Explicit, because Go defaults CGO_ENABLED to 0 when no C compiler is on
	// PATH, and with it off `go list` reports a cgo file under IgnoredGoFiles
	// with CgoFiles empty. CgoLDFLAGS is NOT empty — a reviewer measured it,
	// correcting an earlier version of this comment that said it was — so the
	// -lpq half of TestCgoIsConfined would still fire. The CgoFiles half would
	// not, which is reason enough to force it.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}

	var pkgs []listedPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p listedPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	// Counting is not enough: pointing a configuration at `fmt` lists plenty of
	// standard-library dependencies and passes a count. At least one
	// first-party package must be present, or the query missed the repository.
	if !slices.ContainsFunc(pkgs, func(p listedPackage) bool { return isFirstParty(normalise(p.ImportPath)) }) {
		t.Fatalf("%s listed %d packages and none of them are this repository's", c.name, len(pkgs))
	}
	return pkgs
}

// normalise strips go list -test's decorations: a test binary's packages are
// reported as "p.test", "p [p.test]" and "p_test [p.test]", all of which are
// the package p for this rule's purposes.
func normalise(path string) string {
	i := strings.Index(path, " [")
	if i < 0 {
		// Undecorated. A ".test" suffix here is a real directory named test,
		// not go list's decoration — stripping it unconditionally turned a
		// package at internal/harbourmaster/store.test into the permitted
		// store package.
		return path
	}
	// p_test is p's external test package, and for this rule it is p: a driver
	// imported by the store's own tests is imported by the package that may
	// hold one, and the filesystem walk already treats it that way.
	return strings.TrimSuffix(path[:i], "_test")
}

// reachesDriver reports the path by which one first-party package reaches a
// driver it may not hold, or "" if it reaches none.
//
// The rule, as a graph property: cut the permitted homes out of the dependency
// graph, and no first-party package may still reach a driver. That phrasing is
// what makes a wrapper visible — internal/harbourmaster/wal importing
// github.com/jackc/pglogrepl, which imports pgx/v5/pgconn, reaches a driver
// without naming one, and a direct-import check reports nothing.
//
// The graph is a PARAMETER rather than read from the module, because on this
// repository nothing imports the store yet: no binary links a driver, so the
// traversal never runs and every line of it deleted green. A reviewer replaced
// the whole function with `return ""` and the suite passed. That is the same
// vacuity this package found in violations() a round earlier and then
// reproduced here.
func reachesDriver(imports map[string][]string, path string, seen map[string]bool) string {
	return reachesDriverFrom(imports, path, path, seen)
}

// origin is the first-party package the traversal started from, and it is what
// permission is judged against. Judging against the package holding the import
// meant a permitted home reaching a driver through a third-party wrapper was
// reported — the wrapper is not first-party, so nothing could permit it —
// although the home may hold that driver directly. Safe, but wrong, and the
// stated rule says the home is cut out.
func reachesDriverFrom(imports map[string][]string, origin, path string, seen map[string]bool) string {
	if seen[path] {
		return ""
	}
	seen[path] = true
	dir := strings.TrimPrefix(origin, modulePath+"/")
	for _, imp := range imports[path] {
		if home, ok := driverHome(imp); ok {
			// Permission is PER DRIVER, via permittedHere. Asking only whether
			// the importer is some home let internal/pilot/mysql hold pgx with
			// this check green while the walk refused it — the two mechanisms
			// disagreeing, with the designated one weaker.
			if isFirstParty(origin) && permittedHere(imp, dir) {
				continue
			}
			return fmt.Sprintf("%s (whose home is %s)", imp, home)
		}
		if isPermittedHome(imp) {
			// The sink. A home is cut out of the graph for the drivers it may
			// hold, and reaching one is how every binary is meant to.
			continue
		}
		if via := reachesDriverFrom(imports, origin, imp, seen); via != "" {
			return fmt.Sprintf("%s, through %s", via, imp)
		}
	}
	return ""
}

// TestReachesDriverOverASyntheticGraph is what makes the rule enforceable
// rather than merely present. Every case is a shape that has occurred in
// review.
func TestReachesDriverOverASyntheticGraph(t *testing.T) {
	const (
		pgx   = "github.com/jackc/pgx/v5/stdlib"
		mysql = "github.com/go-sql-driver/mysql"
		me    = modulePath + "/"
	)
	for name, c := range map[string]struct {
		graph map[string][]string
		from  string
		want  bool
	}{
		"a driver in its own home": {
			map[string][]string{me + "internal/harbourmaster/store": {pgx}},
			me + "internal/harbourmaster/store", false,
		},
		"a driver in the Pilot's PostgreSQL adapter": {
			map[string][]string{me + "internal/pilot/postgres": {pgx}},
			me + "internal/pilot/postgres", false,
		},
		"the WRONG driver in a home": {
			map[string][]string{me + "internal/harbourmaster/store": {mysql}},
			me + "internal/harbourmaster/store", true,
		},
		"a driver in a Pilot package that is not its home": {
			map[string][]string{me + "internal/pilot/mysql": {pgx}},
			me + "internal/pilot/mysql", true,
		},
		"a binary reaching a driver through its home": {
			map[string][]string{
				me + "cmd/harbourmaster":            {me + "internal/harbourmaster/store"},
				me + "internal/harbourmaster/store": {pgx},
			},
			me + "cmd/harbourmaster", false,
		},
		"a binary reaching a driver through a package that is not a home": {
			map[string][]string{
				me + "cmd/harbourmaster":          {me + "internal/harbourmaster/wal"},
				me + "internal/harbourmaster/wal": {pgx},
			},
			me + "cmd/harbourmaster", true,
		},
		"a third-party wrapper, which names no driver in the first-party import": {
			map[string][]string{
				me + "cmd/harbourmaster":          {me + "internal/harbourmaster/wal"},
				me + "internal/harbourmaster/wal": {"github.com/jackc/pglogrepl"},
				"github.com/jackc/pglogrepl":      {"github.com/jackc/pgx/v5/pgconn"},
			},
			me + "cmd/harbourmaster", true,
		},
		"a long first-party chain": {
			map[string][]string{
				me + "cmd/marque": {me + "internal/a"},
				me + "internal/a": {me + "internal/b"},
				me + "internal/b": {me + "internal/c"},
				me + "internal/c": {pgx},
			},
			me + "cmd/marque", true,
		},
		"a driver behind a permitted home is not attributed to the binary": {
			map[string][]string{
				me + "cmd/harbourmaster":            {me + "internal/harbourmaster/store"},
				me + "internal/harbourmaster/store": {me + "internal/harbourmaster/wal"},
				me + "internal/harbourmaster/wal":   {pgx},
			},
			me + "cmd/harbourmaster", false,
		},
		"and IS reported under the package that actually imports it": {
			map[string][]string{
				me + "cmd/harbourmaster":            {me + "internal/harbourmaster/store"},
				me + "internal/harbourmaster/store": {me + "internal/harbourmaster/wal"},
				me + "internal/harbourmaster/wal":   {pgx},
			},
			me + "internal/harbourmaster/wal", true,
		},
		"a permitted home reaching its own driver through a wrapper": {
			map[string][]string{
				me + "internal/harbourmaster/store": {"github.com/jackc/pglogrepl"},
				"github.com/jackc/pglogrepl":        {"github.com/jackc/pgx/v5/pgconn"},
			},
			me + "internal/harbourmaster/store", false,
		},
		"but the Pilot's MySQL home reaching a PostgreSQL driver that way is not": {
			map[string][]string{
				me + "internal/pilot/mysql":  {"github.com/jackc/pglogrepl"},
				"github.com/jackc/pglogrepl": {"github.com/jackc/pgx/v5/pgconn"},
			},
			me + "internal/pilot/mysql", true,
		},
		"a cycle terminates": {
			map[string][]string{
				me + "internal/a": {me + "internal/b"},
				me + "internal/b": {me + "internal/a"},
			},
			me + "internal/a", false,
		},
		"no driver anywhere": {
			map[string][]string{me + "cmd/marque": {"fmt", me + "internal/version"}},
			me + "cmd/marque", false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := reachesDriver(c.graph, c.from, map[string]bool{})
			if (got != "") != c.want {
				t.Errorf("reachesDriver(%s) = %q, want a violation: %v", c.from, got, c.want)
			}
		})
	}
}

// TestNoBinaryLinksADriverOutsideItsHome is EDR-0042's mechanism.
func TestNoBinaryLinksADriverOutsideItsHome(t *testing.T) {
	for _, c := range buildConfigs {
		t.Run(c.name, func(t *testing.T) {
			imports := graphOf(t, c)
			for path := range imports {
				if !isFirstParty(path) {
					continue
				}
				// A fresh seen map per root, deliberately. Sharing one across
				// roots makes the answer depend on map iteration order, which
				// is the order-dependence that defeated the walk's global
				// visited set one round ago.
				// UNPINNED, like the two named in driver_confinement_test.go:
				// turning this t.Errorf into a t.Logf leaves the suite green,
				// because a test cannot observe its own reporting. What it
				// reports — reachesDriver — is pinned over synthetic graphs.
				if via := reachesDriver(imports, path, map[string]bool{}); via != "" {
					t.Errorf(
						"%s reaches %s.\n"+
							"  Configuration: %s\n"+
							"  A driver belongs only in the packages EDR-0042 names, and reaching one\n"+
							"  through a wrapper is reaching one. This is the graph the compiler builds,\n"+
							"  so a directory a filesystem walk declines to enter does not help here.",
						path, via, c.name)
				}
			}
		})
	}
}

func graphOf(t *testing.T, c buildConfig) map[string][]string {
	t.Helper()
	imports := map[string][]string{}
	for _, p := range goList(t, c) {
		path := normalise(p.ImportPath)
		for _, imp := range p.Imports {
			imports[path] = append(imports[path], normalise(imp))
		}
	}
	return imports
}

// TestTheGraphContainsTheDriverEdge is the positive control. Without it, a
// configuration pointed at a subtree with no driver in it leaves every
// assertion above trivially satisfied — a reviewer repointed all six and
// watched the suite stay green.
func TestTheGraphContainsTheDriverEdge(t *testing.T) {
	store := modulePath + "/internal/harbourmaster/store"
	seenIt := false
	// Over the REAL buildConfigs, not a config of its own: an earlier version
	// built its own `./...` listing, so repointing all six of the configurations
	// the mechanism actually uses left this satisfied and the suite green.
	for _, c := range buildConfigs {
		imports := graphOf(t, c)
		if slices.ContainsFunc(imports[store], func(imp string) bool {
			_, isDriver := driverHome(imp)
			return isDriver
		}) {
			seenIt = true
		}
	}
	if !seenIt {
		t.Errorf("no configuration in buildConfigs lists %s importing a driver, so the check above is not looking at this repository", store)
	}

	// And that the listing reaches THIRD-PARTY edges, which is what -deps buys.
	// Without it `go list -json ./...` names ten packages and no driver at all,
	// so the wrapper defeat this mechanism exists to close reopens silently —
	// a reviewer deleted -deps and the suite stayed green.
	imports := graphOf(t, buildConfig{"every package", "./...", "", false})
	const driver = "github.com/jackc/pgx/v5/stdlib"
	if len(imports[driver]) == 0 {
		t.Errorf("the listing has no imports for %s, so it is not following dependencies and a wrapper would be invisible", driver)
	}
}

// TestCgoIsConfined covers the path with no Go import at all. A first-party
// file carrying
//
//	#cgo LDFLAGS: -lpq
//	import "C"
//
// links the reference PostgreSQL driver into the binary, and every
// import-based check in this package — this file's included — reports nothing.
// A reviewer demonstrated it, with otool -L naming libpq.5.dylib in the built
// harbourmaster.
//
// The allowlist is empty. EDR-0039 puts libpg_query behind cgo at M2, and that
// is a reviewed edit to this list rather than a quiet arrival, which is the
// whole of what import discipline claims to buy.
var cgoHomes []string

func TestCgoIsConfined(t *testing.T) {
	for _, c := range buildConfigs {
		t.Run(c.name, func(t *testing.T) {
			for _, p := range goList(t, c) {
				path := normalise(p.ImportPath)
				if !isFirstParty(path) || slices.Contains(cgoHomes, path) {
					continue
				}
				if len(p.CgoFiles) > 0 {
					t.Errorf(
						"%s uses cgo, and no first-party package may.\n"+
							"  A #cgo LDFLAGS line links a C library with no Go import for any\n"+
							"  import-based check to see — -lpq is the reference PostgreSQL driver.\n"+
							"  EDR-0042 lists this; adding a package here is a reviewed edit.",
						path)
				}
				for _, f := range p.CgoLDFLAGS {
					t.Errorf("%s passes %q to the linker, outside any reviewed cgo home", path, f)
				}
			}
		})
	}
}
