package store_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Drivers this project could plausibly link, mapped to the ONE package allowed
// to import each. A driver with no entry is allowed nowhere.
//
// This is a denylist and therefore not engine-complete: a driver with no entry
// here is caught by nothing. A reviewer imported go-mssqldb, go-ora, sqlite and
// clickhouse-go and watched both this test and depguard stay silent. That is
// the fourth way EDR-0042 lists the rule being defeated, and it is listed there
// because a comment claiming the record admits something the record does not
// say is its own small version of the problem this file is about.
//
// A denylist is the weaker shape and it is the shape available: an allowlist
// over every import would refuse the standard library. What it buys is that the
// drivers anyone here would actually reach for cannot arrive by accident.
var driverHomes = map[string]string{
	"github.com/jackc/pgx":           "internal/harbourmaster/store",
	"github.com/jackc/pgconn":        "internal/harbourmaster/store",
	"github.com/jackc/pgproto3":      "internal/harbourmaster/store",
	"github.com/lib/pq":              "internal/harbourmaster/store",
	"github.com/go-sql-driver/mysql": "internal/pilot/mysql",
	"github.com/ziutek/mymysql":      "internal/pilot/mysql",
}

// The Pilot's PostgreSQL adapter is a second home for the PostgreSQL drivers:
// reaching a target is its entire job.
const pilotPostgres = "internal/pilot/postgres"

// EDR-0042 calls the boundary between Harbourmaster packages and the Pilot's
// adapters "the boundary that carries the weight": a Harbourmaster package
// importing an adapter links its driver transitively, which no direct-import
// check can see.
//
// cmd/harbourmaster is in the list because it is the binary that actually
// ships. A reviewer blank-imported a Pilot adapter into cmd/harbourmaster/main.go
// and watched `go list -deps` grow twelve pgx packages while this test stayed
// green — the prefix was internal/harbourmaster alone, so the check covered
// every package except the one being built.
var harbourmasterPrefixes = []string{"internal/harbourmaster", "cmd/harbourmaster"}

func inHarbourmaster(dir string) bool {
	for _, p := range harbourmasterPrefixes {
		if dir == p || strings.HasPrefix(dir, p+"/") {
			return true
		}
	}
	return false
}

// TestDriverConfinement is EDR-0042's mechanism, in its third shape.
//
// Three justifications have been given for doing this here rather than in
// depguard, and all three were capability claims about tooling that turned out
// to be false — that depguard cannot report blank imports (it can; revive's
// blank-imports rule reports at the same line and --uniq-by-line hid it), that
// golangci-lint cannot read a file behind a build tag (it can, under
// --build-tags), and that it cannot read gen/ (an anchored exclusion in
// .golangci.yml, and gen/ is in no binary's dependency graph anyway). EDR-0042
// records all three.
//
// So this claims nothing about capability. Both mechanisms' coverage is
// configuration: this test's is its walk and skipDir, the linter's is its
// invocation flags and exclusions. Neither is safe to assert, so both are
// probed — see TestSkipDirMatchesTheGoToolchain and TestWalkReachesGeneratedCode
// below, and the symlink refusal in firstPartyGoFiles.
func TestDriverConfinement(t *testing.T) {
	files := firstPartyGoFiles(t)
	if len(files) < 5 {
		t.Fatalf("found only %d Go files; the walk is not reaching the repository", len(files))
	}
	for _, v := range violations(files) {
		t.Errorf(
			"%s (EDR-0042).\n"+
				"  The rule replaces EDR-0005's \"no database driver for target engines\n"+
				"  linked in\", which stopped being available when EDR-0013 fixed Marque's\n"+
				"  own state on PostgreSQL.",
			v)
	}
}

// TestHarbourmasterDoesNotImportAPilotAdapter enforces the transitive half.
func TestHarbourmasterDoesNotImportAPilotAdapter(t *testing.T) {
	for _, v := range pilotImports(firstPartyGoFiles(t)) {
		t.Errorf(
			"%s.\n"+
				"  A Harbourmaster package importing a Pilot adapter links that adapter's\n"+
				"  driver transitively, which no direct-import check can see. EDR-0042\n"+
				"  calls this the boundary that carries the weight.",
			v)
	}
}

// The allowlist is per driver and per package, not per directory tree. An
// earlier version exempted whole trees, which let the store hold a MySQL driver
// and any Pilot package hold any driver.
//
// A table rather than probe files: a probe inside the store's own package
// cannot compile with an import that is not a dependency, so the test binary
// never builds and the probe proves nothing — itself a way a probe quietly
// proves nothing.
func TestDriverHomesAreExactPackages(t *testing.T) {
	for _, c := range []struct {
		imp, dir string
		want     bool
	}{
		{"github.com/jackc/pgx/v5/stdlib", "internal/harbourmaster/store", true},
		{"github.com/jackc/pgx/v5/stdlib", "internal/pilot/postgres", true},
		{"github.com/jackc/pgx/v5/stdlib", "internal/pilot/mysql", false},
		{"github.com/jackc/pgx/v5/stdlib", "internal/harbourmaster/api", false},
		{"github.com/go-sql-driver/mysql", "internal/harbourmaster/store", false},
		{"github.com/go-sql-driver/mysql", "internal/pilot/mysql", true},
		{"github.com/go-sql-driver/mysql", "internal/pilot/postgres", false},
		{"github.com/ziutek/mymysql/godrv", "internal/harbourmaster/store", false},
		{"github.com/lib/pq", "internal/harbourmaster/store", true},
		{"database/sql", "internal/harbourmaster/api", true},
	} {
		if got := permittedHere(c.imp, c.dir); got != c.want {
			t.Errorf("%s in %s: permitted=%v, want %v", c.imp, c.dir, got, c.want)
		}
	}
}

func permittedHere(imp, dir string) bool {
	home, isDriver := driverHome(imp)
	if !isDriver {
		return true
	}
	if dir == home {
		return true
	}
	return home == "internal/harbourmaster/store" && dir == pilotPostgres
}

func driverHome(imp string) (string, bool) {
	for root, home := range driverHomes {
		if imp == root || strings.HasPrefix(imp, root+"/") {
			return home, true
		}
	}
	return "", false
}

// firstPartyGoFiles maps every repository-relative .go path to its imports.
// `gen/` is NOT skipped: generated code is compiled into the binaries — the
// schema package imports it — so a generated file importing a driver links one.
// skipDir reports whether the Go toolchain itself never compiles from a
// directory of this name, which is the only defensible basis for skipping one.
// Two hand-curated lists have failed here already: the first skipped by
// basename anywhere in the tree, so internal/anything/bin/ escaped; the second
// skipped root-level bin/ and dist/, and `go build ./bin/probe/` compiles a
// package there like any other. A rule tied to what the toolchain ignores has
// no entries to get wrong.
func skipDir(name string) bool {
	// cmd/go: directories beginning with "." or "_" are ignored, as is
	// "testdata", whose contents are never part of any build.
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

// violations reports every file in the set importing a driver it may not.
// Factored out so it can be tested over a synthetic set: on this repository the
// loop body never executes — no file violates the rule — so replacing the
// failure with a no-op left the suite green.
func violations(files map[string][]string) []string {
	var out []string
	for rel, imports := range files {
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, imp := range imports {
			if permittedHere(imp, dir) {
				continue
			}
			home, _ := driverHome(imp)
			out = append(out, fmt.Sprintf("%s imports %q, which belongs in %s and nowhere else", rel, imp, home))
		}
	}
	sort.Strings(out)
	return out
}

// pilotImports reports every Harbourmaster file importing a Pilot package.
// Factored out for the same reason: internal/pilot does not exist yet, so on
// this repository the check cannot fail and cannot be seen to work.
func pilotImports(files map[string][]string) []string {
	var out []string
	for rel, imports := range files {
		dir := filepath.ToSlash(filepath.Dir(rel))
		if !inHarbourmaster(dir) {
			continue
		}
		for _, imp := range imports {
			pkg, ok := strings.CutPrefix(imp, "github.com/sixfathoms/marque/")
			if !ok {
				continue
			}
			if pkg == "internal/pilot" || strings.HasPrefix(pkg, "internal/pilot/") {
				out = append(out, fmt.Sprintf("%s imports %q", rel, imp))
			}
		}
	}
	sort.Strings(out)
	return out
}

func firstPartyGoFiles(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string][]string{}

	// Symlinks are FOLLOWED, not skipped and not refused. filepath.WalkDir
	// reports a symlinked directory as a non-directory entry and does not
	// descend it, while `go build` follows it happily — a reviewer put a
	// Harbourmaster package behind one, watched the binary link a driver
	// through it, and watched this test stay green. Refusing outright was the
	// first fix and it was wrong: pnpm builds node_modules out of symlinks, so
	// refusing meant carving node_modules out, and a carve-out is what failed
	// twice already. Following costs a visited set and nothing else.
	visited := map[string]bool{}

	var walk func(dir, rel string)
	walk = func(dir, rel string) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Errorf("%s could not be resolved, so its contents are unchecked: %v", rel, err)
			return
		}
		if visited[resolved] {
			return
		}
		visited[resolved] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("%s could not be read, so its contents are unchecked: %v", rel, err)
			return
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			childRel := filepath.ToSlash(filepath.Join(rel, e.Name()))

			// Stat, not the dirent's own type: a symlink's type says
			// "symlink", and what matters is what it points at.
			info, err := os.Stat(path)
			if err != nil {
				// A dangling symlink points at nothing, so nothing escapes.
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				t.Errorf("%s could not be inspected, so it is unchecked: %v", childRel, err)
				continue
			}
			if info.IsDir() {
				if !skipDir(e.Name()) {
					walk(path, childRel)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			out[childRel] = fileImports(t, path, childRel)
		}
	}
	walk(root, "")

	return out
}

// fileImports reads one file's import paths. A file that cannot be parsed is a
// failure, not a skip: unknown imports treated as permitted is the silent pass
// this whole test exists to avoid. ImportsOnly stops after the import block, so
// a file broken below it still reads correctly; what fails here is a broken
// header, which would not compile either.
func fileImports(t *testing.T, path, rel string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("%s could not be parsed, so its imports are unchecked: %v", rel, err)
		return nil
	}
	imports := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Errorf("%s has an import path that is not a quoted string: %s", rel, spec.Path.Value)
			continue
		}
		imports = append(imports, p)
	}
	return imports
}

// The guard is only as good as its reach.
func TestDriverConfinementReachesBlankImports(t *testing.T) {
	imports, ok := firstPartyGoFiles(t)["internal/harbourmaster/store/migrate.go"]
	if !ok {
		t.Fatal("the walk did not reach the store, so the confinement test proves nothing")
	}
	for _, imp := range imports {
		if strings.Contains(imp, "jackc/pgx") {
			return
		}
	}
	t.Fatal("the store no longer imports a driver, so the confinement test proves nothing")
}

// gen/ is compiled into the binaries, so it must be walked.
func TestWalkReachesGeneratedCode(t *testing.T) {
	for rel := range firstPartyGoFiles(t) {
		if strings.HasPrefix(rel, "gen/") {
			return
		}
	}
	t.Fatal("the walk skipped gen/, which the test binaries and schemacheck link")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// TestSkipDirMatchesTheGoToolchain pins the skip rule, because the two hand-
// curated lists that preceded it were each defeated by a directory nobody
// thought of. The rule is "what the Go toolchain itself never compiles", and
// the cases that matter are the ones that look skippable and are not.
func TestSkipDirMatchesTheGoToolchain(t *testing.T) {
	for _, c := range []struct {
		dir  string
		skip bool
		why  string
	}{
		{".git", true, "a dot directory; cmd/go ignores it"},
		{".github", true, "a dot directory"},
		{"_scratch", true, "an underscore directory; cmd/go ignores it"},
		{"testdata", true, "cmd/go never compiles testdata"},

		{"bin", false, "go build ./bin/x/ compiles a package there; skipping it hid one"},
		{"dist", false, "same, and it was skipped for the same bad reason"},
		{"node_modules", false, "nothing stops a Go file living there, and it is in the module"},
		{"gen", false, "generated code is compiled code"},
		{"internal", false, ""},
		{"vendor", false, "vendored code is compiled, and its imports are still imports"},
	} {
		if got := skipDir(c.dir); got != c.skip {
			t.Errorf("skipDir(%q) = %v, want %v — %s", c.dir, got, c.skip, c.why)
		}
	}
}

// TestViolationsReportsAForbiddenImport is the test that makes the rule
// enforceable rather than merely present. On this repository no file violates
// it, so the reporting loop never runs and deleting the failure entirely left
// the suite green. A synthetic set is the only way to see the check fire.
func TestViolationsReportsAForbiddenImport(t *testing.T) {
	got := violations(map[string][]string{
		// Forbidden: a Harbourmaster package that is not the store.
		"internal/harbourmaster/api/x.go": {"github.com/jackc/pgx/v5/stdlib"},
		// Forbidden: the store may hold a PostgreSQL driver, not any driver.
		"internal/harbourmaster/store/y.go": {"github.com/go-sql-driver/mysql"},
		// Permitted: each driver in its declared home.
		"internal/harbourmaster/store/z.go": {"github.com/jackc/pgx/v5/stdlib"},
		"internal/pilot/postgres/a.go":      {"github.com/jackc/pgx/v5/stdlib"},
		// Not a driver at all.
		"internal/harbourmaster/api/b.go": {"database/sql", "fmt"},
	})
	want := []string{
		`internal/harbourmaster/api/x.go imports "github.com/jackc/pgx/v5/stdlib", which belongs in internal/harbourmaster/store and nowhere else`,
		`internal/harbourmaster/store/y.go imports "github.com/go-sql-driver/mysql", which belongs in internal/pilot/mysql and nowhere else`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("violations() =\n  %q\nwant\n  %q", got, want)
	}
}

// TestPilotImportsReportsATransitiveDriver covers the other half, which is
// vacuous for a different reason: internal/pilot does not exist yet, so on this
// repository the check cannot fail and cannot be seen to work.
func TestPilotImportsReportsATransitiveDriver(t *testing.T) {
	got := pilotImports(map[string][]string{
		"internal/harbourmaster/api/x.go":   {"github.com/sixfathoms/marque/internal/pilot/postgres"},
		"internal/harbourmaster/store/y.go": {"github.com/sixfathoms/marque/internal/pilot"},
		// A Pilot package importing a Pilot package is not the boundary.
		"internal/pilot/postgres/a.go": {"github.com/sixfathoms/marque/internal/pilot"},
		// A prefix that merely starts the same way is not a Pilot package.
		"internal/harbourmaster/api/b.go": {"github.com/sixfathoms/marque/internal/pilotage"},
	})
	want := []string{
		`internal/harbourmaster/api/x.go imports "github.com/sixfathoms/marque/internal/pilot/postgres"`,
		`internal/harbourmaster/store/y.go imports "github.com/sixfathoms/marque/internal/pilot"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("pilotImports() =\n  %q\nwant\n  %q", got, want)
	}
}
