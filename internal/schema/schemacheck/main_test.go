package main

// These tests exist because a mutation pass found that every guard in this file
// could be disabled by a one-token edit with the whole build still green — the
// rules in internal/schema were meticulously tested, and the wiring deciding
// whether to fail was not tested at all. Changing `if len(violations) > 0` to
// `if false` made a genuinely weakened schema pass while printing the very line
// that is meant to be evidence it did not.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

// declared builds a one-method descriptor set. A nil behaviour means the method
// carries no options at all.
func declared(service, rpc string, behaviour *marquev1.MethodBehaviour) *descriptorpb.FileDescriptorSet {
	var opts *descriptorpb.MethodOptions
	if behaviour != nil {
		opts = &descriptorpb.MethodOptions{}
		proto.SetExtension(opts, marquev1.E_Behaviour, behaviour)
	}

	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    proto.String("marque/v1/probe.proto"),
			Package: proto.String("marque.v1"),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("ProbeRequest")},
				{Name: proto.String("ProbeResponse")},
			},
			Service: []*descriptorpb.ServiceDescriptorProto{{
				Name: proto.String(service),
				Method: []*descriptorpb.MethodDescriptorProto{{
					Name:       proto.String(rpc),
					InputType:  proto.String(".marque.v1.ProbeRequest"),
					OutputType: proto.String(".marque.v1.ProbeResponse"),
					Options:    opts,
				}},
			}},
		}},
	}
}

func natural() *marquev1.MethodBehaviour {
	return &marquev1.MethodBehaviour{Idempotency: marquev1.Idempotency_IDEMPOTENCY_NATURAL}
}

func unsafeBehaviour() *marquev1.MethodBehaviour {
	return &marquev1.MethodBehaviour{Idempotency: marquev1.Idempotency_IDEMPOTENCY_UNSAFE}
}

// noMethods is a descriptor set with a file but no service — the shape a
// deleted service file or a buf.yaml `excludes:` entry produces.
func noMethods() *descriptorpb.FileDescriptorSet {
	fds := declared("ProbeService", "M", natural())
	fds.File[0].Service = nil
	return fds
}

// write marshals a descriptor set into dir and returns its path.
func write(t *testing.T, dir, name string, fds *descriptorpb.FileDescriptorSet) string {
	t.Helper()
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

// runWith invokes run with owned and all pointing at fds, plus extra flags.
func runWith(t *testing.T, fds *descriptorpb.FileDescriptorSet, extra ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := write(t, dir, "owned.binpb", fds)

	args := append([]string{"-owned", path, "-all", path}, extra...)
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestRunAcceptsAnAnnotatedSchema(t *testing.T) {
	stdout, _, err := runWith(t, declared("ProbeService", "M", natural()))
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !strings.Contains(stdout, "1 method(s) checked") {
		t.Errorf("stdout = %q, want it to report what was inspected", stdout)
	}
}

func TestRunRejectsAnUnannotatedMethod(t *testing.T) {
	_, stderr, err := runWith(t, declared("ProbeService", "M", nil))
	if err == nil {
		t.Fatal("run() = nil, want an error for an unannotated method")
	}
	if !strings.Contains(stderr, "declares no (marque.v1.behaviour)") {
		t.Errorf("stderr = %q, want the violation reported", stderr)
	}
	if !strings.Contains(err.Error(), "1 schema violation") {
		t.Errorf("error = %v, want it to count the violations", err)
	}
}

// The guard the package comment is proudest of: files present, methods zero.
func TestRunRejectsASchemaWithNoMethods(t *testing.T) {
	_, stderr, err := runWith(t, noMethods())
	if err == nil {
		t.Fatal("run() = nil, want an error; a check that inspects nothing is not a check")
	}
	if !strings.Contains(stderr, "declares no methods") {
		t.Errorf("stderr = %q, want it to say nothing was checked", stderr)
	}
	// With no violations to count, the summary must describe the fault it
	// actually found. Reporting "0 schema violation(s)" while exiting 1, and
	// citing two records that have nothing to do with it, is worse than silence.
	if !strings.Contains(err.Error(), "1 problem(s) with the schema check itself") {
		t.Errorf("error = %v, want it to name the problem as being with the check", err)
	}
}

func TestRunRejectsAWeakenedDeclaration(t *testing.T) {
	dir := t.TempDir()
	before := write(t, dir, "before.binpb", declared("ProbeService", "M", natural()))
	after := write(t, dir, "after.binpb", declared("ProbeService", "M", unsafeBehaviour()))

	var stdout, stderr bytes.Buffer
	err := run([]string{"-owned", after, "-all", after, "-before", before}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() = nil, want an error for a weakened declaration")
	}
	if !strings.Contains(stderr.String(), "is now IDEMPOTENCY_UNSAFE") {
		t.Errorf("stderr = %q, want the weakening reported", stderr.String())
	}
}

func TestRunAcceptsAnUnchangedDeclarationAndSaysWhatItCompared(t *testing.T) {
	dir := t.TempDir()
	before := write(t, dir, "before.binpb", declared("ProbeService", "M", natural()))
	after := write(t, dir, "after.binpb", declared("ProbeService", "M", natural()))

	var stdout, stderr bytes.Buffer
	if err := run([]string{"-owned", after, "-all", after, "-before", before}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "1 of 1 compared against the previous schema") {
		t.Errorf("stdout = %q, want the compared count", stdout.String())
	}
}

// A rename matches nothing by name, so nothing is compared. buf breaking
// catches it and runs first, but this must not look like a pass.
func TestRunRejectsAComparisonThatMatchedNothing(t *testing.T) {
	dir := t.TempDir()
	before := write(t, dir, "before.binpb", declared("ProbeService", "M", natural()))
	after := write(t, dir, "after.binpb", declared("RenamedService", "M", natural()))

	var stdout, stderr bytes.Buffer
	err := run([]string{"-owned", after, "-all", after, "-before", before}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() = nil, want an error; zero pairs compared is not a pass")
	}
	if !strings.Contains(stderr.String(), "matched a method here by name") {
		t.Errorf("stderr = %q, want it to say nothing was compared", stderr.String())
	}
}

func TestRunRejectsAProtoTheSchemaDoesNotCover(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "marque", "v1"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Named exactly as the descriptor set names it, so it is covered.
	covered := filepath.Join(root, "marque", "v1", "probe.proto")
	if err := os.WriteFile(covered, []byte("syntax = \"proto3\";\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// This one is on disk and in no descriptor set — hidden by an excludes
	// entry, or outside the module.
	hidden := filepath.Join(root, "marque", "v1", "hidden.proto")
	if err := os.WriteFile(hidden, []byte("syntax = \"proto3\";\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runWith(t, declared("ProbeService", "M", natural()), "-proto-root", root)
	if err == nil {
		t.Fatal("run() = nil, want an error for a file no rule has run on")
	}
	if !strings.Contains(stderr, "hidden.proto") {
		t.Errorf("stderr = %q, want the uncovered file named", stderr)
	}
	if strings.Contains(stderr, "probe.proto:") {
		t.Errorf("stderr = %q, want the covered file not reported", stderr)
	}
}

// Asserting only that an error came back is not enough: with the flag guard
// removed, run still errors — from trying to read the file "" — and the test
// passes having proved nothing. It has to be *this* error.
func TestRunRequiresItsFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"nothing at all", nil},
		{"owned without all", []string{"-owned", "x.binpb"}},
		{"all without owned", []string{"-all", "x.binpb"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil {
				t.Fatal("run() = nil, want an error for missing flags")
			}
			if !strings.Contains(err.Error(), "both -owned and -all are required") {
				t.Errorf("error = %v, want the missing-flag error rather than a read failure", err)
			}
		})
	}
}

// The evidence line has to report what was *compared*, not what was available.
// Deleting a method is legal, so the two numbers legitimately differ — and with
// only one method in every fixture, a count that printed the wrong one would
// look identical.
func TestRunReportsHowManyPairsItCompared(t *testing.T) {
	dir := t.TempDir()

	twoMethods := declared("ProbeService", "Kept", natural())
	svc := twoMethods.File[0].Service[0]
	gone := declared("ProbeService", "Gone", natural()).File[0].Service[0].Method[0]
	svc.Method = append(svc.Method, gone)

	before := write(t, dir, "before.binpb", twoMethods)
	after := write(t, dir, "after.binpb", declared("ProbeService", "Kept", natural()))

	var stdout, stderr bytes.Buffer
	if err := run([]string{"-owned", after, "-all", after, "-before", before}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, want nil; deleting a method is legal", err)
	}
	if !strings.Contains(stdout.String(), "1 of 2 compared") {
		t.Errorf("stdout = %q, want it to report 1 of 2 compared", stdout.String())
	}
}

// The two descriptor sets are not interchangeable, and nothing exercised the
// difference through run() — every other fixture passes the same file as both.
// Methods must come from `owned` only; messages must resolve from `all`.
func TestRunDistinguishesTheOwnedSetFromTheFullOne(t *testing.T) {
	dir := t.TempDir()

	// An imported file declaring an unannotated service. It is ours to resolve
	// against, never ours to annotate.
	imported := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("google/longrunning/operations.proto"),
		Package: proto.String("google.longrunning"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("GetOperationRequest")},
			{Name: proto.String("Operation")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Operations"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("GetOperation"),
				InputType:  proto.String(".google.longrunning.GetOperationRequest"),
				OutputType: proto.String(".google.longrunning.Operation"),
			}},
		}},
	}

	owned := declared("ProbeService", "M", natural())
	full := declared("ProbeService", "M", natural())
	full.File = append(full.File, imported)

	ownedPath := write(t, dir, "owned.binpb", owned)
	allPath := write(t, dir, "all.binpb", full)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"-owned", ownedPath, "-all", allPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, want nil; an imported service is not ours to annotate", err)
	}
	if !strings.Contains(stdout.String(), "1 method(s) checked") {
		t.Errorf("stdout = %q, want only the owned method counted", stdout.String())
	}

	// Swapping them is the mutation this guards: the imported service would
	// then be walked and rejected.
	stdout.Reset()
	stderr.Reset()
	err := run([]string{"-owned", allPath, "-all", ownedPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() = nil with the sets swapped, want the imported service to be rejected")
	}
	if !strings.Contains(stderr.String(), "google.longrunning.Operations.GetOperation") {
		t.Errorf("stderr = %q, want the imported method named — the swap must fail for that reason",
			stderr.String())
	}
}

// Coverage is resolved against the owned set, not the full one: a file present
// only as an import must not count as covered for the module's own tree.
func TestRunResolvesCoverageAgainstTheOwnedSet(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "marque", "v1"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"probe.proto", "extra.proto"} {
		if err := os.WriteFile(filepath.Join(root, "marque", "v1", name),
			[]byte("syntax = \"proto3\";\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	owned := declared("ProbeService", "M", natural())

	// `all` also carries marque/v1/extra.proto. If coverage were resolved
	// against it, the uncovered file would look covered.
	full := declared("ProbeService", "M", natural())
	full.File = append(full.File, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("marque/v1/extra.proto"),
		Package: proto.String("marque.v1"),
		Syntax:  proto.String("proto3"),
	})

	ownedPath := write(t, dir, "owned.binpb", owned)
	allPath := write(t, dir, "all.binpb", full)

	var stdout, stderr bytes.Buffer
	err := run([]string{"-owned", ownedPath, "-all", allPath, "-proto-root", root}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() = nil, want extra.proto reported as uncovered")
	}
	if !strings.Contains(stderr.String(), "extra.proto") {
		t.Errorf("stderr = %q, want the uncovered file named", stderr.String())
	}
}

// Everything wrong is reported in one run. An operator who has to fix problems
// one build at a time is an operator who stops reading the output.
func TestRunReportsViolationsAndFatalsTogether(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "marque", "v1"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "marque", "v1", "hidden.proto"),
		[]byte("syntax = \"proto3\";\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runWith(t, noMethods(), "-proto-root", root)
	if err == nil {
		t.Fatal("run() = nil, want an error")
	}
	if !strings.Contains(stderr, "hidden.proto") {
		t.Errorf("stderr = %q, want the uncovered file reported", stderr)
	}
	if !strings.Contains(stderr, "declares no methods") {
		t.Errorf("stderr = %q, want the vacuity problem reported in the same run", stderr)
	}
	// The other side of the same branch: with a violation present, the summary
	// counts violations rather than reporting a problem with the check.
	if !strings.Contains(err.Error(), "schema violation(s)") {
		t.Errorf("error = %v, want it to count the violations", err)
	}
}

func TestRunRejectsAnUnreadableOrEmptyDescriptorSet(t *testing.T) {
	dir := t.TempDir()
	empty := write(t, dir, "empty.binpb", &descriptorpb.FileDescriptorSet{})

	tests := []struct {
		name, path, want string
	}{
		{"absent", filepath.Join(dir, "nope.binpb"), "reading descriptor set"},
		{"empty", empty, "contains no files"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run([]string{"-owned", tt.path, "-all", tt.path}, &stdout, &stderr)
			if err == nil {
				t.Fatalf("run() = nil, want an error for a %s descriptor set", tt.name)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
