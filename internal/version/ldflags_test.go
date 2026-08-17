package version_test

// A `-X` linker path that does not resolve is ignored in silence: no warning,
// no error, just a binary reporting "dev". Nothing else in this repository can
// tell a working stamp from an ignored one —
//
//   - TestGetReportsSomething cannot, because firstNonEmpty guarantees a
//     non-empty Version whether the stamp applied or not;
//   - `make snapshot-check` cannot, because it compares two values: delete a
//     -X from both configs and both sides report "unknown" and agree. It now
//     rejects "unknown" explicitly, but agreement was never arrival, and a
//     mutation pass proved it by killing a stamp on both paths at once.
//
// So the only way to know is to build a binary with the flags and run it. This
// test is what the Makefile's LDFLAGS and .goreleaser.yaml's ldflags are
// checked against: rename a build variable without updating them, and it fails.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/sixfathoms/marque/internal/version"
)

func TestLdflagsReachTheBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	// Derived rather than written out, so moving the package cannot leave this
	// test asserting against a path nothing stamps. A *renamed variable* still
	// breaks it, which is the case worth catching.
	pkg := reflect.TypeFor[version.Info]().PkgPath()

	const (
		wantVersion    = "v9.9.9-probe"
		wantCommit     = "1a2b3c4-probe"
		wantSourceDate = "2001-02-03T04:05:06Z"
	)

	ldflags := strings.Join([]string{
		"-X " + pkg + ".buildVersion=" + wantVersion,
		"-X " + pkg + ".buildCommit=" + wantCommit,
		"-X " + pkg + ".buildSourceDate=" + wantSourceDate,
	}, " ")

	binary := filepath.Join(t.TempDir(), "marque-probe")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	// The real command, not a fixture. A stamp that reaches a test harness but
	// not the shipped binary would be the same defect wearing a disguise.
	//
	// -buildvcs=false matches what .goreleaser.yaml passes, and matters here:
	// with VCS stamping on, a dead -X path is backfilled from the repository
	// and the assertions below would pass on a broken build.
	// t.Context is cancelled when the test ends, so a hung toolchain fails the
	// test rather than the whole run.
	build := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false",
		"-ldflags", ldflags, "-o", binary, "../../cmd/marque")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v\n%s", err, out)
	}

	out, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running the built binary: %v\n%s", err, out)
	}

	got := string(out)
	for _, want := range []string{wantVersion, wantCommit, wantSourceDate} {
		if !strings.Contains(got, want) {
			t.Errorf("built binary printed %q, want it to contain %q — the -X path for it "+
				"no longer resolves, so a release would report a stamp it never received", got, want)
		}
	}
}

// TestBuildConfigsStampEveryVariable reads the two files that actually do the
// stamping and checks them against this package's build variables.
//
// TestLdflagsReachTheBinary above proves the mechanism works, using flags it
// composes itself — so it stays green when the Makefile or .goreleaser.yaml
// has a typo in a path neither of them shares with it. This one closes that:
// both sides are derived, the variables from the source and the paths from the
// configs, so a rename, a typo, or a variable stamped by one build path and
// not the other all fail here.
func TestBuildConfigsStampEveryVariable(t *testing.T) {
	want := buildVariables(t)
	if len(want) == 0 {
		t.Fatal("found no build variables in version.go; this test is checking nothing")
	}

	module := makeVariable(t, "MODULE")
	pkg := reflect.TypeFor[version.Info]().PkgPath()

	for _, config := range []string{"../../Makefile", "../../.goreleaser.yaml"} {
		t.Run(filepath.Base(config), func(t *testing.T) {
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatalf("reading %s: %v", config, err)
			}

			// The Makefile writes the import path through a make variable, so
			// it is expanded before comparison rather than matched loosely —
			// a typo in the literal half must still fail.
			// Comments are dropped first — whole lines and trailing ones
			// alike. Both files are comment-dense and both write `-X` in
			// prose, and stripping only line-leading comments failed in both
			// directions: a trailing `# -X …buildDate=x` failed the build over
			// an explanation, and a trailing mention of a `-X` line that had
			// been *deleted* kept this test green while the release binary
			// printed "unknown".
			var code []string
			for _, line := range strings.Split(string(raw), "\n") {
				if i := strings.Index(line, "#"); i >= 0 {
					line = line[:i]
				}
				code = append(code, line)
			}
			text := strings.ReplaceAll(strings.Join(code, "\n"), "$(MODULE)", module)

			got := map[string]bool{}
			for _, m := range regexp.MustCompile(`-X\s+(\S+?)\.(\w+)=`).FindAllStringSubmatch(text, -1) {
				if m[1] != pkg {
					t.Errorf("%s stamps %q, but the package is %q", config, m[1], pkg)
				}
				got[m[2]] = true
			}

			for name := range want {
				if !got[name] {
					t.Errorf("%s stamps no value for %s, so a binary built this way reports "+
						"the fallback rather than what it was built from", config, name)
				}
			}
			for name := range got {
				if !want[name] {
					t.Errorf("%s stamps %s, which version.go does not declare; the flag is "+
						"silently ignored", config, name)
				}
			}
		})
	}
}

// buildVariables returns the package-level variables version.go expects the
// linker to set, read from the source rather than listed here.
func buildVariables(t *testing.T) map[string]bool {
	t.Helper()

	// The whole package, not just version.go: a build variable declared in a
	// sibling file is stamped by the same flags and must be found here too.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	names := map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		collectBuildVars(file, names)
	}
	return names
}

func collectBuildVars(file *ast.File, names map[string]bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Variables *with* initialisers count too. An earlier version of
			// this test skipped them, on the belief that -X only replaces an
			// uninitialised string. That is false — `-X` overwrites
			// `var v = "dev"` exactly as it overwrites `var v string`,
			// verified — and the exclusion was worse than a gap: writing
			// `var buildVersion = "dev"`, the commonest idiom for this
			// pattern, made the test report that version.go "does not
			// declare" a variable it plainly declares, which invites deleting
			// two working -X lines to make the error go away.
			for _, name := range value.Names {
				if strings.HasPrefix(name.Name, "build") {
					names[name.Name] = true
				}
			}
		}
	}
}

// makeVariable reads a simple `NAME := value` assignment out of the Makefile.
func makeVariable(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	m := regexp.MustCompile(`(?m)^` + name + `\s*:?=\s*(\S+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no %s assignment in the Makefile", name)
	}
	return string(m[1])
}

// TestGoreleaserStampsTheWorkingTreeState asserts statically what CI structurally
// cannot observe.
//
// The Makefile computes whether the tree differs from HEAD and exports it, so
// that goreleaser — which cannot see the working tree, because --snapshot skips
// its repository validation — stamps the same thing. `make snapshot-check`
// catches it going missing only when run on a *dirty* tree, and CI always
// checks out clean. So without this, removing the template reference is a
// regression that merges green on every runner.
func TestGoreleaserStampsTheWorkingTreeState(t *testing.T) {
	raw, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("reading .goreleaser.yaml: %v", err)
	}

	const marker = "{{ .Env.MARQUE_DIRTY }}"
	if !strings.Contains(string(raw), marker) {
		t.Errorf(".goreleaser.yaml does not stamp %s, so a snapshot built over uncommitted "+
			"changes would carry a commit that does not contain them", marker)
	}
}
