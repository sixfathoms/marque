package store_test

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
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
// below, and the symlink FOLLOWING in firstPartyGoFiles — refusing them was
// the first fix and it was wrong.
//
// This walk is no longer the primary check. TestNoBinaryLinksADriverOutsideItsHome
// asks the toolchain what each binary links, which is the question the rule is
// about; this asks what files exist, which is a different one, and it is kept
// as the cross-check.
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

// isFirstParty reports whether a package path belongs to this repository.
func isFirstParty(path string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

const modulePath = "github.com/sixfathoms/marque"

// isPermittedHome reports whether a first-party PACKAGE PATH is one of the
// packages EDR-0042 allows to hold a driver.
func isPermittedHome(path string) bool {
	dir, ok := strings.CutPrefix(path, modulePath+"/")
	if !ok {
		return false
	}
	for _, home := range driverHomes {
		if dir == home {
			return true
		}
	}
	return dir == pilotPostgres
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
// `gen/` is NOT skipped: the test binaries and schemacheck compile it — no
// shipped binary does (EDR-0042) — so a generated file importing a driver
// would link one into a binary this repository builds.
// skipDir skips ONLY .git, and only because walking a large object store is
// slow. It is a performance trade with a stated residue, not a claim about what
// compiles.
//
// The three lists that preceded it were each a claim about what compiles, and
// each was wrong. The last is the instructive one: it skipped directories
// beginning with "." or "_", and "testdata", citing cmd/go — and that rule
// governs WILDCARD EXPANSION, not import resolution. `go build ./x/testdata/pg`
// compiles a package there perfectly well, and an explicit import of it links
// whatever it imports. Two reviewers and codex found that independently, one
// round after this rule was rewritten to be "principled". It was the fourth
// false capability claim on this branch.
//
// So: no capability claim. Walking .git could surface a .go file inside a
// packfile path, and nothing imports one, and if that ever matters the fix is
// to walk it.
func skipDir(name string) bool {
	return name == ".git"
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
	files, problems := goFilesUnder(t, repoRoot(t))
	for _, p := range problems {
		// Not a skip. A file whose imports could not be read is a file whose
		// imports are unchecked, which is the silent pass this exists to avoid.
		//
		// UNPINNED, stated rather than implied: deleting these four lines
		// leaves the suite green, and so does turning TestDriverConfinement's
		// t.Errorf into a t.Logf. A test cannot observe its own reporting from
		// inside the same binary. Everything either side is pinned —
		// goFilesUnder returns its problems so they can be asserted, and
		// violations() is exercised over a synthetic set — so what is left is
		// the glue, and the honest thing is to name it rather than let the
		// coverage claim cover it.
		t.Error(p)
	}
	return files
}

// goFilesUnder RETURNS its problems rather than reporting them, so a test can
// plant an unreadable file and assert the refusal. Reporting them inline meant
// turning the refusal into a silent skip left the suite green — the mutation
// this signature exists to kill.
func goFilesUnder(t *testing.T, root string) (map[string][]string, []string) {
	t.Helper()
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	out := map[string][]string{}

	// Symlinks are FOLLOWED, not skipped and not refused. filepath.WalkDir
	// reports a symlinked directory as a non-directory entry and does not
	// descend it, while `go build` follows it happily — a reviewer put a
	// Harbourmaster package behind one, watched the binary link a driver
	// through it, and watched this test stay green. Refusing outright was the
	// first fix and it was wrong: pnpm builds node_modules out of symlinks, so
	// refusing meant carving node_modules out, and a carve-out is what failed
	// twice already.
	//
	// Cycle detection is per-ANCESTRY, not global. A global visited set was the
	// second fix and it was also wrong: a directory reachable by two names was
	// recorded only under whichever os.ReadDir reached first, so ALPHABETICAL
	// ORDER decided whether a forbidden alias was examined. A reviewer aliased a
	// permitted package to a forbidden path, watched the permitted name win the
	// sort, and watched twelve driver packages reach the binary silently.
	// Per-ancestry cycle detection means a directory reachable by several
	// distinct paths is visited once per path, which is correct — each alias is
	// a place a driver could be imported from — and is exponential in the depth
	// of nested aliases. Measured by a reviewer at 3^n on a synthetic tree; the
	// real walk is a tenth of a second, because nothing here nests symlink sets.
	// A bound so a pathological tree fails with its cause named rather than
	// appearing to hang.
	const maxVisits = 200_000
	visits := 0

	var walk func(dir, rel string, ancestry map[string]bool)
	walk = func(dir, rel string, ancestry map[string]bool) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			problem("%s could not be resolved, so its contents are unchecked: %v", rel, err)
			return
		}
		if ancestry[resolved] {
			// A loop, not an alias. Descending would not terminate.
			return
		}
		visits++
		if visits > maxVisits {
			if visits == maxVisits+1 {
				problem("the walk visited more than %d directories and stopped; a deeply nested set of "+
					"symlink aliases makes it exponential, so anything below this point is unchecked", maxVisits)
			}
			return
		}
		ancestry = maps.Clone(ancestry)
		ancestry[resolved] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			problem("%s could not be read, so its contents are unchecked: %v", rel, err)
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
				problem("%s could not be inspected, so it is unchecked: %v", childRel, err)
				continue
			}
			if info.IsDir() {
				if !skipDir(e.Name()) {
					walk(path, childRel, ancestry)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			imports, err := fileImports(path)
			if err != nil {
				problem("%s could not be parsed, so its imports are unchecked: %v", childRel, err)
				continue
			}
			out[childRel] = imports
		}
	}
	walk(root, "", map[string]bool{})

	return out, problems
}

// fileImports reads one file's import paths. ImportsOnly stops after the
// import block, so a file broken below it still reads correctly; what fails
// here is a broken header, which would not compile either.
func fileImports(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	imports := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import path %s is not a quoted string", spec.Path.Value)
		}
		imports = append(imports, p)
	}
	return imports, nil
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
		// cmd/harbourmaster is the binary that ships, and the prefix list
		// omitted it once while every internal package was covered.
		"cmd/harbourmaster/main.go": {"github.com/sixfathoms/marque/internal/pilot/postgres"},
		// A Pilot package importing a Pilot package is not the boundary.
		"internal/pilot/postgres/a.go": {"github.com/sixfathoms/marque/internal/pilot"},
		// A prefix that merely starts the same way is not a Pilot package.
		"internal/harbourmaster/api/b.go": {"github.com/sixfathoms/marque/internal/pilotage"},
	})
	want := []string{
		`cmd/harbourmaster/main.go imports "github.com/sixfathoms/marque/internal/pilot/postgres"`,
		`internal/harbourmaster/api/x.go imports "github.com/sixfathoms/marque/internal/pilot/postgres"`,
		`internal/harbourmaster/store/y.go imports "github.com/sixfathoms/marque/internal/pilot"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("pilotImports() =\n  %q\nwant\n  %q", got, want)
	}
}

// TestTheWalkReachesEveryPlaceThatHasEscapedIt builds a synthetic tree holding
// one file in each location that defeated this check in review, and asserts the
// walk reports all of them.
//
// It exists because EDR-0042 claimed "standing tests plant a driver import in
// each place that has escaped a check so far" while no test planted anything —
// a claim about coverage asserted from reasoning, inside the paragraph
// retracting three claims about coverage asserted from reasoning. A reviewer
// grepped for it. This is the sentence made true.
//
// Every case here shipped green at least once:
//
//   - bin/ and dist/ were skipped by name, and `go build ./bin/pkg/` compiles.
//   - testdata/, _x/ and .x/ were skipped citing cmd/go, whose rule about them
//     governs wildcard expansion rather than import resolution.
//   - a symlinked directory was not descended at all; then, once it was, a
//     global visited set meant the alias was examined only if it sorted before
//     the real directory.
func TestTheWalkReachesEveryPlaceThatHasEscapedIt(t *testing.T) {
	root := t.TempDir()
	const driver = "github.com/jackc/pgx/v5/stdlib"

	plant := func(dir string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("building the tree: %v", err)
		}
		rel := filepath.Join(dir, "p.go")
		body := "package p\n\nimport _ \"" + driver + "\"\n"
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("building the tree: %v", err)
		}
		return filepath.ToSlash(rel)
	}

	want := []string{
		plant("bin/probe"),
		plant("dist/probe"),
		plant("internal/nested/bin/probe"),
		plant("internal/harbourmaster/testdata/pg"),
		plant("_underscore/probe"),
		plant(".dotted/probe"),
		plant("vendorish/probe"),
	}

	// The real directory sorts AFTER both aliases, so a global visited set
	// keyed on the resolved path would examine only the first alias to be read
	// and silently drop the other — which is the bug this reproduces. Both
	// names must be reported.
	target := "zz_real"
	plant(target)
	for _, alias := range []string{"aa_alias", "mm_alias"} {
		if err := os.Symlink(filepath.Join(root, target), filepath.Join(root, alias)); err != nil {
			t.Fatalf("linking %s: %v", alias, err)
		}
		want = append(want, alias+"/p.go")
	}
	want = append(want, target+"/p.go")

	got, problems := goFilesUnder(t, root)
	if len(problems) != 0 {
		t.Errorf("the synthetic tree produced problems: %s", strings.Join(problems, "; "))
	}
	for _, rel := range want {
		if _, ok := got[rel]; !ok {
			t.Errorf("the walk did not reach %s, which `go build` compiles", rel)
			continue
		}
		if !slices.Contains(got[rel], driver) {
			t.Errorf("the walk reached %s but did not read its imports", rel)
		}
	}

	// And the reporting half: every planted file is at a forbidden path, so
	// every one must be reported. This is what makes deleting the failure
	// visible — on the real repository the loop body never executes.
	if len(violations(got)) != len(want) {
		t.Errorf("violations() reported %d of %d planted imports:\n%s",
			len(violations(got)), len(want), strings.Join(violations(got), "\n"))
	}
}

// TestTheWalkTerminatesOnASymlinkLoop pins the other half of the cycle-detection
// change: per-ancestry rather than global. A loop must not hang the test, and a
// non-looping alias must not be discarded as though it were one.
func TestTheWalkTerminatesOnASymlinkLoop(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "p.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "a", "loop")); err != nil {
		t.Fatalf("linking: %v", err)
	}
	got, _ := goFilesUnder(t, root)
	if _, ok := got["a/p.go"]; !ok {
		t.Error("the walk did not reach a/p.go")
	}
}

// TestAnUnreadableFileIsAProblemNotASkip pins the refusal that matters most,
// because its failure mode is silence: a file whose imports cannot be read is a
// file whose imports are unchecked, and treating that as a skip is exactly the
// silent pass this package exists to prevent. Turning the refusal into a no-op
// left the whole suite green until this test existed.
func TestAnUnreadableFileIsAProblemNotASkip(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"truncated.go": "package p\n\nimport (\n\t_ \"github.com/jackc/pgx/v5/stdlib\"\n",
		"nopackage.go": "import _ \"github.com/jackc/pgx/v5/stdlib\"\n",
		"notevengo.go": "\x00\x01 this is not Go at all\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("building the tree: %v", err)
		}
	}

	_, problems := goFilesUnder(t, root)
	if len(problems) != 3 {
		t.Errorf("want 3 unreadable files reported, got %d:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	for _, p := range problems {
		if !strings.Contains(p, "unchecked") {
			t.Errorf("the problem should say the file is unchecked; got %q", p)
		}
	}
}
