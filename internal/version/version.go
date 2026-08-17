// Package version reports what a binary is, so that a marque, a logbook entry
// or a bug report can name the software that produced it.
//
// The three build variables below are injected at link time — by the Makefile
// during development, and by goreleaser for a release. When they are empty (a
// bare "go build", or "go run") the module's own embedded build information is
// used instead, so a development binary still reports something true rather
// than a row of "unknown".
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Injected with -ldflags -X; see LDFLAGS in the Makefile.
var (
	buildVersion    string
	buildCommit     string
	buildSourceDate string
)

// Unknown is what a field reports when neither the linker nor the embedded
// build information supplied it.
const Unknown = "unknown"

// Info is what a binary knows about its own provenance.
type Info struct {
	// Version is a release tag, or "dev" for anything not built from one.
	Version string
	// Commit is the source revision, suffixed "-dirty" if the working tree
	// had uncommitted changes at build time.
	Commit string
	// SourceDate is when the source this was built from was committed, in
	// RFC 3339 — or SOURCE_DATE_EPOCH where a distribution sets it.
	//
	// It is the source's date rather than the build's on purpose: building the
	// same commit twice then produces the same binary, and for a tool whose
	// logbook entries name the software that produced them, "same source, same
	// binary" is worth more than knowing when the artefact was made.
	SourceDate string
	// Go is the toolchain version.
	Go string
	// Platform is GOOS/GOARCH.
	Platform string
}

// Get returns this binary's version information.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return assemble(buildVersion, buildCommit, buildSourceDate, bi)
}

// assemble is Get with its inputs passed in, so the fallback behaviour can be
// tested without building a binary three different ways.
func assemble(version, commit, sourceDate string, bi *debug.BuildInfo) Info {
	if bi != nil {
		if version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		settings := buildSettings(bi)
		if commit == "" {
			commit = settings["vcs.revision"]
			if settings["vcs.modified"] == "true" && commit != "" {
				commit += "-dirty"
			}
		}
		if sourceDate == "" {
			sourceDate = settings["vcs.time"]
		}
	}

	return Info{
		Version:    firstNonEmpty(version, "dev"),
		Commit:     firstNonEmpty(commit, Unknown),
		SourceDate: firstNonEmpty(sourceDate, Unknown),
		Go:         runtime.Version(),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func buildSettings(bi *debug.BuildInfo) map[string]string {
	settings := make(map[string]string, len(bi.Settings))
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	return settings
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// String renders one line: "v0.1.0 (1a2b3c4, 2026-08-16T09:00:00Z) go1.26.5 darwin/arm64".
func (i Info) String() string {
	return fmt.Sprintf("%s (%s, %s) %s %s", i.Version, i.Commit, i.SourceDate, i.Go, i.Platform)
}
