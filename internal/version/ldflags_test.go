package version_test

// A `-X` linker path that does not resolve is ignored in silence: no warning,
// no error, just a binary reporting "dev". Nothing else in this repository can
// tell a working stamp from an ignored one —
//
//   - TestGetReportsSomething cannot, because firstNonEmpty guarantees a
//     non-empty Version whether the stamp applied or not;
//   - `make snapshot-check` cannot, because it compares two values and
//     internal/version falls back to debug.ReadBuildInfo's vcs.time, which in
//     a git checkout is byte-identical to the date the Makefile stamps. A
//     mutation pass renamed the variable so that *both* stamps were dead, and
//     that check still passed.
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
			text := strings.ReplaceAll(string(raw), "$(MODULE)", module)

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

	file, err := parser.ParseFile(token.NewFileSet(), "version.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing version.go: %v", err)
	}

	names := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Values) != 0 {
				// A variable with an initialiser is not one the linker sets:
				// -X only replaces an uninitialised string.
				continue
			}
			for _, name := range value.Names {
				if strings.HasPrefix(name.Name, "build") {
					names[name.Name] = true
				}
			}
		}
	}
	return names
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
