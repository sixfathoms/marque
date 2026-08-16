package version

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func buildInfo(settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: settings}
}

func TestAssemble(t *testing.T) {
	tests := []struct {
		name                    string
		version, commit, date   string
		info                    *debug.BuildInfo
		wantVersion, wantCommit string
		wantDate                string
	}{
		{
			name:    "linker values are used as given",
			version: "v0.1.0", commit: "1a2b3c4", date: "2026-08-16T09:00:00Z",
			info:        buildInfo(debug.BuildSetting{Key: "vcs.revision", Value: "ffffff"}),
			wantVersion: "v0.1.0", wantCommit: "1a2b3c4", wantDate: "2026-08-16T09:00:00Z",
		},
		{
			name: "build information fills in what the linker did not set",
			info: buildInfo(
				debug.BuildSetting{Key: "vcs.revision", Value: "1a2b3c4"},
				debug.BuildSetting{Key: "vcs.time", Value: "2026-08-16T09:00:00Z"},
			),
			wantVersion: "dev", wantCommit: "1a2b3c4", wantDate: "2026-08-16T09:00:00Z",
		},
		{
			name: "a modified working tree is reported as dirty",
			info: buildInfo(
				debug.BuildSetting{Key: "vcs.revision", Value: "1a2b3c4"},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			wantVersion: "dev", wantCommit: "1a2b3c4-dirty", wantDate: Unknown,
		},
		{
			name:        "no build information at all",
			info:        nil,
			wantVersion: "dev", wantCommit: Unknown, wantDate: Unknown,
		},
		{
			name:        "the placeholder module version is not a version",
			info:        &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			wantVersion: "dev", wantCommit: Unknown, wantDate: Unknown,
		},
		{
			name:        "a real module version is",
			info:        &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}},
			wantVersion: "v0.2.0", wantCommit: Unknown, wantDate: Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assemble(tt.version, tt.commit, tt.date, tt.info)
			if got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.Commit != tt.wantCommit {
				t.Errorf("Commit = %q, want %q", got.Commit, tt.wantCommit)
			}
			if got.Date != tt.wantDate {
				t.Errorf("Date = %q, want %q", got.Date, tt.wantDate)
			}
			if got.Go != runtime.Version() {
				t.Errorf("Go = %q, want %q", got.Go, runtime.Version())
			}
			if want := runtime.GOOS + "/" + runtime.GOARCH; got.Platform != want {
				t.Errorf("Platform = %q, want %q", got.Platform, want)
			}
		})
	}
}

func TestInfoString(t *testing.T) {
	got := Info{
		Version:  "v0.1.0",
		Commit:   "1a2b3c4",
		Date:     "2026-08-16T09:00:00Z",
		Go:       "go1.26.5",
		Platform: "darwin/arm64",
	}.String()

	want := "v0.1.0 (1a2b3c4, 2026-08-16T09:00:00Z) go1.26.5 darwin/arm64"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// Get is exercised as well as assemble, because the wiring between them — the
// three package-level variables — is the part a bad -ldflags path breaks.
func TestGetReportsSomething(t *testing.T) {
	got := Get()
	if strings.TrimSpace(got.Version) == "" {
		t.Error("Version is empty; every binary must report something")
	}
	if !strings.Contains(got.String(), runtime.GOOS) {
		t.Errorf("String() = %q, want it to name the platform", got.String())
	}
}
