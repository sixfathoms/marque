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

// Drivers for target engines. EDR-0005's sentence is engine-agnostic and
// EDR-0026 plans MySQL, so this list grows with the engine list rather than
// naming PostgreSQL alone. The pgx v4-era standalone modules are listed
// separately: they reach a target with no database/sql involved at all.
var targetEngineDrivers = []string{
	"github.com/jackc/pgx",
	"github.com/jackc/pgconn",
	"github.com/jackc/pgproto3",
	"github.com/lib/pq",
	"github.com/go-sql-driver/mysql",
}

// Directories permitted to hold one, matched as prefixes so a subpackage is
// covered — the Pilot's adapter will be internal/pilot/postgres, not
// internal/pilot. The Harbourmaster's own store must have a driver because
// EDR-0013 fixed Marque's own state on PostgreSQL; the Pilot's must because
// reaching a target is its entire job.
var permittedDirs = []string{
	"internal/harbourmaster/store",
	"internal/pilot",
}

// TestDriverConfinement is EDR-0042's mechanism.
//
// It parses every .go file in the repository directly rather than asking
// `go list`, and that is the whole design. Two blind spots rule out the
// alternatives:
//
//   - `depguard` does not report BLANK imports, and a driver is imported blank
//     essentially always, because the point is the registration side effect.
//     Measured against the pinned linter, with a control on a standard-library
//     package, so the axis is the blank alias and nothing about drivers. A
//     reviewer reading depguard's source concluded otherwise; the measurement
//     is what settled it.
//   - `go list` evaluates build constraints. It uses the host GOOS and passes
//     no tags, so a file behind `//go:build integration` — which is where M1's
//     own integration test lives, the file most certain to import a driver —
//     is invisible to it. Also measured.
//
// go/parser reads the file whatever its build tags say, on any host, including
// _test.go files. There is no configuration under which a first-party file
// escapes it.
func TestDriverConfinement(t *testing.T) {
	root := repoRoot(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "bin", "gen":
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
		for _, p := range permittedDirs {
			if rel == p || strings.HasPrefix(rel, p+"/") {
				return nil
			}
		}

		// ImportsOnly, so a file that does not compile is still checked.
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			// Not a skip. A file this cannot read is a file whose imports are
			// unknown, and treating unknown as permitted is the silent pass
			// this guard exists to avoid. ImportsOnly stops after the import
			// block, so a file broken *below* it still reads correctly — what
			// fails here is a broken header, which would not compile either.
			t.Errorf("%s could not be parsed, so its imports are unchecked: %v", rel, err)
			return nil
		}
		for _, spec := range f.Imports {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			for _, d := range targetEngineDrivers {
				if p == d || strings.HasPrefix(p, d+"/") {
					t.Errorf(
						"%s imports %q.\n"+
							"  A driver for a target engine belongs in %v and nowhere else (EDR-0042).\n"+
							"  This replaces EDR-0005's \"no database driver for target engines linked\n"+
							"  in\", which stopped being available when EDR-0013 fixed Marque's own\n"+
							"  state on PostgreSQL.",
						rel, p, permittedDirs)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
}

// The guard is only as good as its reach, so assert it reaches the file it most
// needs to: the store's own blank driver import. If this stops finding it, the
// test above passes whether or not a driver is linked anywhere.
func TestDriverConfinementReachesBlankImports(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal/harbourmaster/store/migrate.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing the store: %v", err)
	}
	for _, spec := range f.Imports {
		if strings.Contains(spec.Path.Value, "jackc/pgx") {
			if spec.Name == nil || spec.Name.Name != "_" {
				t.Fatal("the store's driver import is no longer blank; this test asserts the wrong thing now")
			}
			return
		}
	}
	t.Fatal("the store no longer imports a driver, so the confinement test above proves nothing")
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
