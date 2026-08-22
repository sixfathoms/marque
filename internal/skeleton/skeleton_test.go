package skeleton

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireRefusesUnlessAcknowledged(t *testing.T) {
	for value, want := range map[string]bool{
		"1":    true,
		"":     false,
		"0":    false,
		"true": false,
		"yes":  false,
		"TRUE": false,
		" 1":   false,
		"1 ":   false,
		"11":   false,
		"2":    false,
	} {
		t.Run("value "+value, func(t *testing.T) {
			var out bytes.Buffer
			err := Require("harbourmaster", func(string) string { return value }, &out)
			if got := err == nil; got != want {
				t.Fatalf("%s=%q: accepted=%v, want %v (err=%v)", EnvVar, value, got, want, err)
			}
			if !want {
				if !errors.Is(err, ErrNotAcknowledged) {
					t.Errorf("want ErrNotAcknowledged, got %v", err)
				}
				// The explanation is printed, the BANNER is not: a banner
				// saying "running because the flag is set" above a refusal
				// would be the opposite of true.
				if strings.Contains(out.String(), "NOT SECURE") {
					t.Errorf("the running-banner was printed for a refused start: %q", out.String())
				}
				if !strings.Contains(out.String(), EnvVar+"=1") {
					t.Errorf("the refusal explained nothing on stderr: %q", out.String())
				}
				// The refusal must say how to proceed, or it is a wall.
				if !strings.Contains(err.Error(), EnvVar+"=1") {
					t.Errorf("the refusal does not say what to set; got %v", err)
				}
			}
		})
	}
}

// The gate reads the variable it names. An earlier shape could have read one
// name and told the operator another.
func TestRequireReadsTheVariableItNames(t *testing.T) {
	var asked []string
	_ = Require("pilot", func(k string) string { asked = append(asked, k); return "1" }, &bytes.Buffer{})
	if len(asked) != 1 || asked[0] != EnvVar {
		t.Errorf("the gate read %v, want exactly [%s]", asked, EnvVar)
	}
}

func TestTheBannerNamesWhatIsAbsent(t *testing.T) {
	var out bytes.Buffer
	if err := Require("harbourmaster", func(string) string { return "1" }, &out); err != nil {
		t.Fatalf("acknowledged start refused: %v", err)
	}
	banner := out.String()
	// Each absence is a thing M1 genuinely does not do. A banner that says
	// "not secure" and stops is a banner people learn to skip.
	for _, want := range []string{"HARBOURMASTER", "signatures", "grammar", "identity", "fence", EnvVar + "=1"} {
		if !strings.Contains(banner, want) {
			t.Errorf("the banner does not mention %q:\n%s", want, banner)
		}
	}
	if !strings.Contains(banner, "NOT SECURE") {
		t.Errorf("the banner does not say it is not secure:\n%s", banner)
	}
}

// TestTheSkeletonGateIsGone is M5's, written now and skipped until then.
//
// The plan commits M5 to deleting the flag, and a promise to delete something
// later is worth what the test that checks it is worth. It greps the built
// binaries rather than the source, because the question is what shipped.
func TestTheSkeletonGateIsGone(t *testing.T) {
	t.Skip("M1 builds the insecure skeleton on purpose; M5 deletes the flag and un-skips this")

	root := repoRoot(t)
	for _, binary := range []string{"marque", "harbourmaster", "pilot"} {
		out := filepath.Join(t.TempDir(), binary)
		build := exec.CommandContext(t.Context(), "go", "build", "-o", out, "./cmd/"+binary)
		build.Dir = root
		if b, err := build.CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v\n%s", binary, err, b)
		}
		body, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("reading %s: %v", binary, err)
		}
		if bytes.Contains(body, []byte(EnvVar)) {
			t.Errorf("%s still contains %s; M5 was supposed to remove the flag and this path with it",
				binary, EnvVar)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("finding the repository root: %v", err)
	}
	return strings.TrimSpace(string(out))
}
