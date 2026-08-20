package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
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
	if len(pkgs) < 3 {
		t.Fatalf("%s listed only %d packages; the query is not reaching the repository", c.name, len(pkgs))
	}
	return pkgs
}

// normalise strips go list -test's decorations: a test binary's packages are
// reported as "p.test", "p [p.test]" and "p_test [p.test]", all of which are
// the package p for this rule's purposes.
func normalise(path string) string {
	if i := strings.Index(path, " ["); i >= 0 {
		path = path[:i]
	}
	return strings.TrimSuffix(path, ".test")
}

// TestNoBinaryLinksADriverOutsideItsHome is EDR-0042's mechanism.
//
// The rule, stated as a graph property: cut the permitted homes out of the
// dependency graph, and no first-party package may still reach a driver. That
// phrasing is what makes a wrapper visible — internal/harbourmaster/wal
// importing github.com/jackc/pglogrepl, which imports pgx/v5/pgconn, reaches a
// driver without naming one, and a direct-import check reports nothing. A
// reviewer did exactly that and put a PostgreSQL wire driver in the shipped
// binary with both of the previous mechanisms green.
func TestNoBinaryLinksADriverOutsideItsHome(t *testing.T) {
	for _, c := range buildConfigs {
		t.Run(c.name, func(t *testing.T) {
			pkgs := goList(t, c)

			imports := map[string][]string{}
			for _, p := range pkgs {
				path := normalise(p.ImportPath)
				for _, imp := range p.Imports {
					imports[path] = append(imports[path], normalise(imp))
				}
			}

			// Permitted homes are sinks: cmd/harbourmaster reaching pgx
			// THROUGH internal/harbourmaster/store is the arrangement the
			// record describes, not a violation.
			var reaches func(string, map[string]bool) string
			reaches = func(path string, seen map[string]bool) string {
				if seen[path] {
					return ""
				}
				seen[path] = true
				for _, imp := range imports[path] {
					if home, ok := driverHome(imp); ok {
						if isPermittedHome(path) {
							continue
						}
						return fmt.Sprintf("%s (whose home is %s)", imp, home)
					}
					if isPermittedHome(imp) {
						continue
					}
					if via := reaches(imp, seen); via != "" {
						if !isFirstParty(imp) {
							return fmt.Sprintf("%s, through %s", via, imp)
						}
						return via
					}
				}
				return ""
			}

			for path := range imports {
				if !isFirstParty(path) || isPermittedHome(path) {
					continue
				}
				if via := reaches(path, map[string]bool{}); via != "" {
					t.Errorf(
						"%s links %s.\n"+
							"  Configuration: go list -deps%s%s %s\n"+
							"  A driver belongs only in the packages EDR-0042 names, and reaching one\n"+
							"  through a wrapper is reaching one. This is the graph the compiler builds,\n"+
							"  so a directory a filesystem walk declines to enter does not help here.",
						path, via,
						map[bool]string{true: " -test"}[c.test],
						map[bool]string{true: " -tags " + c.tags}[c.tags != ""],
						c.pattern)
				}
			}
		})
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
