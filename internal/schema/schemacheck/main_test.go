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
	_, _, err := runWith(t, noMethods())
	if err == nil {
		t.Fatal("run() = nil, want an error; a check that inspects nothing is not a check")
	}
	if !strings.Contains(err.Error(), "declares no methods") {
		t.Errorf("error = %v, want it to say nothing was checked", err)
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
	if !strings.Contains(stdout.String(), "1 compared against the previous schema") {
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
	if !strings.Contains(err.Error(), "matched a method here by name") {
		t.Errorf("error = %v, want it to say nothing was compared", err)
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
			if err := run(tt.args, &stdout, &stderr); err == nil {
				t.Error("run() = nil, want an error for missing flags")
			}
		})
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
