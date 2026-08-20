package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
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
	// PATH. With it off, a package whose only files are cgo files has no
	// buildable files at all, so `go list` EXITS NON-ZERO and goList below
	// t.Fatals — TestCgoIsConfined would not run rather than run and see
	// nothing. Two earlier versions of this comment got that wrong in opposite
	// directions; this one was measured.
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
// EDR-0042 counts six combinations as its answer to "coverage is
// configuration", and deleting entries from the matrix was invisible: the
// remaining ones still found nothing, because there is nothing to find.
func TestEveryBuildConfigurationIsPresent(t *testing.T) {
	want := map[string]bool{}
	for _, pattern := range []string{"./cmd/...", "./..."} {
		for _, tags := range []string{"", "integration"} {
			want[pattern+"|"+tags+"|false"] = false
			if pattern == "./..." {
				want[pattern+"|"+tags+"|true"] = false
			}
		}
	}
	for _, c := range buildConfigs {
		key := fmt.Sprintf("%s|%s|%v", c.pattern, c.tags, c.test)
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected configuration %s; if it is deliberate, add it here", key)
			continue
		}
		want[key] = true
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("configuration %s is missing: each pattern must be listed with and without the tag, and the tree with and without -test", key)
		}
	}
}

// The -test and -tags switches must actually change what is listed, or the six
// configurations are one configuration written six times. Both could be dropped
// with everything green, because on a repository with no violation every
// listing finds the same nothing.
func TestTheModeSwitchesChangeWhatIsListed(t *testing.T) {
	store := modulePath + "/internal/harbourmaster/store"

	plain := graphOf(t, buildConfig{"plain", "./...", "", false})
	withTest := graphOf(t, buildConfig{"test", "./...", "", true})
	tagged := graphOf(t, buildConfig{"tagged", "./...", "integration", true})

	// -test pulls in the test binary's own dependencies; testing is the one
	// every test binary has and no library here imports.
	if _, ok := plain["testing"]; ok {
		t.Error("the non-test listing names testing, so -test cannot be distinguishing anything")
	}
	if _, ok := withTest["testing"]; !ok {
		t.Error("the -test listing does not name testing, so -test is not in effect")
	}

	// The tag adds integration_test.go to the store's external test package —
	// which normalise folds back onto the store itself — and that file imports
	// sync, which nothing else in the package does.
	if slices.Contains(withTest[store], "sync") {
		t.Error("the untagged test listing already imports sync; pick another import that only the tagged file has")
	}
	if !slices.Contains(tagged[store], "sync") {
		t.Errorf("the tagged listing does not show %s importing sync, so --tags integration is not in effect:\n  %v",
			store, tagged[store])
	}
}

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
		// isPermittedHome decides the sink, and it must be EXACT too: treating
		// a subdirectory of a home as a home cuts it out of the graph, so a
		// driver behind it becomes invisible.
		"a subpackage of a home is NOT a home": {
			map[string][]string{
				me + "cmd/harbourmaster":                {me + "internal/harbourmaster/store/sub"},
				me + "internal/harbourmaster/store/sub": {pgx},
			},
			me + "cmd/harbourmaster", true,
		},
		// The Pilot's adapter is a second sink, and it must be exact too.
		"a subpackage of the Pilot adapter is NOT a home": {
			map[string][]string{
				me + "cmd/harbourmaster":           {me + "internal/pilot/postgres/sub"},
				me + "internal/pilot/postgres/sub": {pgx},
			},
			me + "cmd/harbourmaster", true,
		},
		"nor is internal/pilot/postgresql": {
			map[string][]string{
				me + "cmd/harbourmaster":         {me + "internal/pilot/postgresql"},
				me + "internal/pilot/postgresql": {pgx},
			},
			me + "cmd/harbourmaster", true,
		},
		"nor is a same-prefix sibling": {
			map[string][]string{
				me + "cmd/harbourmaster":                 {me + "internal/harbourmaster/storefront"},
				me + "internal/harbourmaster/storefront": {pgx},
			},
			me + "cmd/harbourmaster", true,
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

	// PER CONFIGURATION, not an OR across them. An OR is satisfied by any one
	// config still seeing the edge, so repointing five of six left it green.
	//
	// The same assertion for every configuration, now that cmd/harbourmaster
	// links the store — while nothing imported it, a `./cmd/...` listing did
	// not reach it and this had to ask those two for something else.
	for _, c := range buildConfigs {
		t.Run(c.name, func(t *testing.T) {
			imports := graphOf(t, c)
			if !slices.ContainsFunc(imports[store], func(imp string) bool {
				_, isDriver := driverHome(imp)
				return isDriver
			}) {
				t.Errorf("this listing does not show %s importing a driver, so the check above is not looking at this repository", store)
			}
		})
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

// cgoViolations reports every first-party package using cgo outside a reviewed
// home. Factored out, like reachesDriver and violations, because no package
// here uses cgo — so on this repository the loop body never executes, and a
// reviewer replaced the whole test with `return` and watched the suite pass.
func cgoViolations(pkgs []listedPackage) []string {
	var out []string
	for _, p := range pkgs {
		path := normalise(p.ImportPath)
		if !isFirstParty(path) || slices.Contains(cgoHomes, path) {
			continue
		}
		if len(p.CgoFiles) > 0 {
			out = append(out, fmt.Sprintf("%s uses cgo", path))
		}
		for _, f := range p.CgoLDFLAGS {
			out = append(out, fmt.Sprintf("%s passes %q to the linker", path, f))
		}
	}
	sort.Strings(out)
	return out
}

func TestCgoViolationsReportsAnUnreviewedCgoPackage(t *testing.T) {
	got := cgoViolations([]listedPackage{
		{ImportPath: modulePath + "/internal/harbourmaster/wal", CgoFiles: []string{"tail.go"}, CgoLDFLAGS: []string{"-lpq"}},
		{ImportPath: modulePath + "/internal/version"},
		// Third-party cgo is not this rule's business.
		{ImportPath: "github.com/someone/else", CgoFiles: []string{"x.go"}},
	})
	want := []string{
		modulePath + "/internal/harbourmaster/wal passes \"-lpq\" to the linker",
		modulePath + "/internal/harbourmaster/wal uses cgo",
	}
	if !slices.Equal(got, want) {
		t.Errorf("cgoViolations() =\n  %q\nwant\n  %q", got, want)
	}
}

// UNPINNED glue, like the two named in driver_confinement_test.go: replacing
// this loop with an empty one leaves the suite green, because no package here
// uses cgo and a test cannot observe its own reporting. What it reports is
// pinned by TestCgoViolationsReportsAnUnreviewedCgoPackage.
func TestCgoIsConfined(t *testing.T) {
	for _, c := range buildConfigs {
		t.Run(c.name, func(t *testing.T) {
			for _, v := range cgoViolations(goList(t, c)) {
				t.Errorf(
					"%s, and no first-party package may.\n"+
						"  A #cgo LDFLAGS line links a C library with no Go import for any\n"+
						"  import-based check to see — -lpq is the reference PostgreSQL driver.\n"+
						"  EDR-0042 lists this; adding a package to cgoHomes is a reviewed edit.",
					v)
			}
		})
	}
}

// normalise is tested directly because its two behaviours pull in opposite
// directions and neither can fire on this repository: nothing is named
// something.test, and no external test package here imports a driver.
func TestNormalise(t *testing.T) {
	for in, want := range map[string]string{
		// go list -test's three decorations for package p.
		"example.com/p.test":                      "example.com/p.test",
		"example.com/p [example.com/p.test]":      "example.com/p",
		"example.com/p_test [example.com/p.test]": "example.com/p",
		// A real directory named test, or ending in _test, is left alone —
		// stripping unconditionally turned a package at
		// internal/harbourmaster/store.test into the permitted store package.
		"example.com/store.test": "example.com/store.test",
		"example.com/thing_test": "example.com/thing_test",
		"example.com/plain":      "example.com/plain",
	} {
		if got := normalise(in); got != want {
			t.Errorf("normalise(%q) = %q, want %q", in, got, want)
		}
	}
}
