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
	"os/exec"
	"path/filepath"
	"reflect"
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
