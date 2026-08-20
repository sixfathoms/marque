package store_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Drivers this project could plausibly link, mapped to the ONE package allowed
// to import each. A driver with no entry is allowed nowhere.
//
// This is a denylist and therefore not engine-complete: anything published
// tomorrow is not on it. EDR-0042 says so rather than implying otherwise — a
// denylist is the weaker shape and it is the shape available. What it buys is
// that the drivers anyone here would actually reach for cannot arrive by
// accident.
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
const harbourmasterPrefix = "internal/harbourmaster"

// TestDriverConfinement is EDR-0042's mechanism, in its third shape.
//
//   - `depguard` does not report BLANK imports, and a driver is imported blank
//     essentially always, because the point is the registration side effect.
//   - `go list` evaluates build constraints — host GOOS, no tags — so a file
//     behind `//go:build integration` is invisible, which is where M1's own
//     integration test lives.
//
// Both were measured, and both were specified and reviewed before anyone ran
// them. Parsing files directly has neither blind spot: build tags are
// irrelevant, `_test.go` files are included, `gen/` is included because it is
// compiled into the binaries, and a file that cannot be parsed is a failure
// rather than a skip.
func TestDriverConfinement(t *testing.T) {
	files := firstPartyGoFiles(t)
	if len(files) < 5 {
		t.Fatalf("found only %d Go files; the walk is not reaching the repository", len(files))
	}
	for rel, imports := range files {
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, imp := range imports {
			if permittedHere(imp, dir) {
				continue
			}
			home, _ := driverHome(imp)
			t.Errorf(
				"%s imports %q.\n"+
					"  That driver belongs in %s and nowhere else (EDR-0042). It replaces\n"+
					"  EDR-0005's \"no database driver for target engines linked in\", which\n"+
					"  stopped being available when EDR-0013 fixed Marque's own state on\n"+
					"  PostgreSQL.",
				rel, imp, home)
		}
	}
}

// TestHarbourmasterDoesNotImportAPilotAdapter enforces the transitive half.
func TestHarbourmasterDoesNotImportAPilotAdapter(t *testing.T) {
	for rel, imports := range firstPartyGoFiles(t) {
		dir := filepath.ToSlash(filepath.Dir(rel))
		if !strings.HasPrefix(dir, harbourmasterPrefix) {
			continue
		}
		for _, imp := range imports {
			pkg, ok := strings.CutPrefix(imp, "github.com/sixfathoms/marque/")
			if !ok {
				continue
			}
			if pkg == "internal/pilot" || strings.HasPrefix(pkg, "internal/pilot/") {
				t.Errorf(
					"%s imports %q.\n"+
						"  A Harbourmaster package importing a Pilot adapter links that adapter's\n"+
						"  driver transitively, which no direct-import check can see. EDR-0042\n"+
						"  calls this the boundary that carries the weight.",
					rel, imp)
			}
		}
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
func firstPartyGoFiles(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string][]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			// Not a skip. Unknown imports treated as permitted is the silent
			// pass this exists to avoid. ImportsOnly stops after the import
			// block, so a file broken below it still reads correctly; what
			// fails here is a broken header, which would not compile either.
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
		out[rel] = imports
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	return out
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
	t.Fatal("the walk skipped gen/, which is compiled into every binary")
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
