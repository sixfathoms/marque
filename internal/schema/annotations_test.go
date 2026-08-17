package schema

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

const (
	noOptions = true
	unknown   = descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	noSide    = descriptorpb.MethodOptions_NO_SIDE_EFFECTS
)

// behaviour is shorthand for a MethodBehaviour.
func behaviour(safe bool, idempotency marquev1.Idempotency, field string) *marquev1.MethodBehaviour {
	return &marquev1.MethodBehaviour{
		Safe:             safe,
		Idempotency:      idempotency,
		IdempotencyField: field,
	}
}

// natural is the commonest valid declaration.
func natural() *marquev1.MethodBehaviour {
	return behaviour(false, marquev1.Idempotency_IDEMPOTENCY_NATURAL, "")
}

// keyed names its key, which the default probe request carries.
func keyed(field string) *marquev1.MethodBehaviour {
	return behaviour(false, marquev1.Idempotency_IDEMPOTENCY_KEYED, field)
}

// method builds a method descriptor whose options carry b and level.
func method(name string, b *marquev1.MethodBehaviour, level descriptorpb.MethodOptions_IdempotencyLevel) *descriptorpb.MethodDescriptorProto {
	opts := &descriptorpb.MethodOptions{}
	if level != unknown {
		opts.IdempotencyLevel = level.Enum()
	}
	if b != nil {
		proto.SetExtension(opts, marquev1.E_Behaviour, b)
	}
	return &descriptorpb.MethodDescriptorProto{
		Name:       proto.String(name),
		InputType:  proto.String(".marque.v1.ProbeRequest"),
		OutputType: proto.String(".marque.v1.ProbeResponse"),
		Options:    opts,
	}
}

// bareMethod declares no options at all, which is distinct from declaring
// empty ones. Both must be caught.
func bareMethod(name string) *descriptorpb.MethodDescriptorProto {
	m := method(name, nil, unknown)
	m.Options = nil
	return m
}

func stringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// probe builds a one-file descriptor set. The request message carries a single
// string field "nonce", which is what the keyed rules resolve against.
func probe(methods ...*descriptorpb.MethodDescriptorProto) *descriptorpb.FileDescriptorSet {
	return probeWithFields([]*descriptorpb.FieldDescriptorProto{stringField("nonce", 1)}, methods...)
}

func probeWithFields(fields []*descriptorpb.FieldDescriptorProto, methods ...*descriptorpb.MethodDescriptorProto) *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    proto.String("marque/v1/probe.proto"),
			Package: proto.String("marque.v1"),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("ProbeRequest"), Field: fields},
				{Name: proto.String("ProbeResponse")},
			},
			Service: []*descriptorpb.ServiceDescriptorProto{{
				Name:   proto.String("ProbeService"),
				Method: methods,
			}},
		}},
	}
}

func TestCheckAnnotationsAccepts(t *testing.T) {
	tests := []struct {
		name   string
		fds    *descriptorpb.FileDescriptorSet
		reason string
	}{
		{
			name: "safe, with the standard option that makes it mean something",
			fds:  probe(method("M", behaviour(true, marquev1.Idempotency_IDEMPOTENCY_NATURAL, ""), noSide)),
		},
		{
			name:   "safe alone, without an idempotency",
			fds:    probe(method("M", behaviour(true, marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED, ""), noSide)),
			reason: "EDR-0020's own example annotates a read-only method with safe and nothing else",
		},
		{
			name: "an idempotency alone",
			fds:  probe(method("M", natural(), unknown)),
		},
		{
			name: "unsafe is a decision like any other",
			fds:  probe(method("M", behaviour(false, marquev1.Idempotency_IDEMPOTENCY_UNSAFE, ""), unknown)),
		},
		{
			name: "IDEMPOTENT alongside a natural declaration, which is what it means",
			fds:  probe(method("M", natural(), descriptorpb.MethodOptions_IDEMPOTENT)),
		},
		{
			name: "keyed, naming a singular string the request has",
			fds:  probe(method("M", keyed("nonce"), unknown)),
		},
		{
			name: "keyed on bytes",
			fds: probeWithFields(
				[]*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("nonce"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
				method("M", keyed("nonce"), unknown),
			),
		},
		{
			name: "keyed on a proto3 optional field, whose synthetic oneof is not a real one",
			fds: probeWithFields(
				[]*descriptorpb.FieldDescriptorProto{{
					Name:           proto.String("nonce"),
					Number:         proto.Int32(1),
					Type:           descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					OneofIndex:     proto.Int32(0),
					Proto3Optional: proto.Bool(true),
				}},
				method("M", keyed("nonce"), unknown),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := CheckAnnotations(tt.fds, tt.fds)
			if len(report.Violations) != 0 {
				t.Errorf("CheckAnnotations() = %v, want none (%s)", report.Violations, tt.reason)
			}
			if report.MethodsChecked != 1 {
				t.Errorf("MethodsChecked = %d, want 1", report.MethodsChecked)
			}
		})
	}
}

func TestCheckAnnotationsRejects(t *testing.T) {
	tests := []struct {
		name string
		fds  *descriptorpb.FileDescriptorSet
		want string
	}{
		{
			name: "no options at all",
			fds:  probe(bareMethod("M")),
			want: "declares no (marque.v1.behaviour)",
		},
		{
			name: "options present but no behaviour",
			fds:  probe(method("M", nil, unknown)),
			want: "declares no (marque.v1.behaviour)",
		},
		{
			name: "safe explicitly false is not a declaration of safety",
			fds:  probe(method("M", behaviour(false, marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED, ""), unknown)),
			want: "declares no (marque.v1.behaviour)",
		},
		{
			name: "safe and unsafe at once",
			fds:  probe(method("M", behaviour(true, marquev1.Idempotency_IDEMPOTENCY_UNSAFE, ""), noSide)),
			want: "cannot be one that must not be retried",
		},
		{
			name: "safe without the standard option a Connect client reads",
			fds:  probe(method("M", behaviour(true, marquev1.Idempotency_IDEMPOTENCY_NATURAL, ""), unknown)),
			want: "does not set `option idempotency_level = NO_SIDE_EFFECTS`",
		},
		{
			name: "the standard option without safe",
			fds:  probe(method("M", natural(), noSide)),
			want: "so a generated client treats it as read-only and this repository does not",
		},
		{
			name: "keyed without naming the key",
			fds:  probe(method("M", keyed(""), unknown)),
			want: "without an idempotency_field naming the key",
		},
		{
			name: "keyed naming a field the request does not have",
			fds:  probe(method("M", keyed("absent"), unknown)),
			want: "which marque.v1.ProbeRequest does not have",
		},
		{
			name: "keyed naming a repeated field",
			fds: probeWithFields(
				[]*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("nonce"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				}},
				method("M", keyed("nonce"), unknown),
			),
			want: "is repeated; a key is a single value",
		},
		{
			name: "keyed naming an integer field",
			fds: probeWithFields(
				[]*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("nonce"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
				method("M", keyed("nonce"), unknown),
			),
			want: "is INT64; a key is a string or bytes",
		},
		{
			name: "keyed naming a real oneof member",
			fds: probeWithFields(
				[]*descriptorpb.FieldDescriptorProto{{
					Name:       proto.String("nonce"),
					Number:     proto.Int32(1),
					Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					OneofIndex: proto.Int32(0),
				}},
				method("M", keyed("nonce"), unknown),
			),
			want: "is a member of a oneof",
		},
		{
			name: "a key on a method that is not keyed",
			fds:  probe(method("M", behaviour(false, marquev1.Idempotency_IDEMPOTENCY_NATURAL, "nonce"), unknown)),
			want: "so the field means nothing",
		},
		{
			name: "an idempotency value this build does not know",
			fds:  probe(method("M", behaviour(false, marquev1.Idempotency(99), ""), unknown)),
			want: "which this build does not know",
		},
		{
			name: "IDEMPOTENT on a method declared unsafe",
			fds: probe(method("M", behaviour(false, marquev1.Idempotency_IDEMPOTENCY_UNSAFE, ""),
				descriptorpb.MethodOptions_IDEMPOTENT)),
			want: "would advertise to its interceptors that repeating this is harmless",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := CheckAnnotations(tt.fds, tt.fds)
			if len(report.Violations) == 0 {
				t.Fatalf("CheckAnnotations() = none, want a violation containing %q", tt.want)
			}
			if !containsReason(report.Violations, tt.want) {
				t.Errorf("violations = %v, want one containing %q", report.Violations, tt.want)
			}
			for _, v := range report.Violations {
				if v.Method != "marque.v1.ProbeService.M" {
					t.Errorf("Method = %q, want the fully-qualified name", v.Method)
				}
				if v.File != "marque/v1/probe.proto" {
					t.Errorf("File = %q, want the declaring file", v.File)
				}
			}
		})
	}
}

func containsReason(violations []Violation, want string) bool {
	for _, v := range violations {
		if strings.Contains(v.Reason, want) {
			return true
		}
	}
	return false
}

// Methods live only in the owned set. A service in an imported file — a health
// service, google.longrunning — must not be put through Marque's rules, because
// it is code this project does not own and cannot annotate.
func TestCheckAnnotationsIgnoresImportedServices(t *testing.T) {
	owned := probe(method("M", natural(), unknown))

	all := probe(method("M", natural(), unknown))
	all.File = append(all.File, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("google/longrunning/operations.proto"),
		Package: proto.String("google.longrunning"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("GetOperationRequest")},
			{Name: proto.String("Operation")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:   proto.String("Operations"),
			Method: []*descriptorpb.MethodDescriptorProto{bareMethod("GetOperation")},
		}},
	})

	report := CheckAnnotations(owned, all)
	if len(report.Violations) != 0 {
		t.Errorf("CheckAnnotations() = %v, want none; imported services are not ours to annotate",
			report.Violations)
	}
	if report.MethodsChecked != 1 {
		t.Errorf("MethodsChecked = %d, want 1 — only the owned method", report.MethodsChecked)
	}
}

// A request message living in an imported file must still resolve, which is the
// whole reason two sets are passed rather than one.
func TestCheckAnnotationsResolvesImportedRequestMessages(t *testing.T) {
	owned := probe(method("M", keyed("nonce"), unknown))
	owned.File[0].Service[0].Method[0].InputType = proto.String(".other.v1.Imported")

	all := probe(method("M", keyed("nonce"), unknown))
	all.File = append(all.File, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("other/v1/other.proto"),
		Package: proto.String("other.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("Imported"),
			Field: []*descriptorpb.FieldDescriptorProto{stringField("nonce", 1)},
		}},
	})

	if report := CheckAnnotations(owned, all); len(report.Violations) != 0 {
		t.Errorf("CheckAnnotations() = %v, want none", report.Violations)
	}
}

func TestCheckAnnotationsResolvesNestedRequestMessages(t *testing.T) {
	fds := probe(method("M", keyed("nonce"), unknown))
	file := fds.GetFile()[0]
	file.MessageType = []*descriptorpb.DescriptorProto{{
		Name: proto.String("Outer"),
		NestedType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("ProbeRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{stringField("nonce", 1)},
		}},
	}}
	file.Service[0].Method[0].InputType = proto.String(".marque.v1.Outer.ProbeRequest")

	if report := CheckAnnotations(fds, fds); len(report.Violations) != 0 {
		t.Errorf("CheckAnnotations() = %v, want none for a nested request message", report.Violations)
	}
}

// An unresolvable request message must fail rather than be assumed correct.
func TestCheckAnnotationsFailsOnUnresolvableRequest(t *testing.T) {
	fds := probe(method("M", keyed("nonce"), unknown))
	fds.GetFile()[0].Service[0].Method[0].InputType = proto.String(".marque.v1.Missing")

	report := CheckAnnotations(fds, fds)
	if !containsReason(report.Violations, "is not in the descriptor set") {
		t.Errorf("violations = %v, want one reporting the message as unresolvable", report.Violations)
	}
}

// Violations are sorted by method, then reason, so output is stable and a diff
// of it means something. Two files and three methods, declared out of order —
// with one method the sort is unexercised and the test passes even if the sort
// is deleted.
func TestCheckAnnotationsIsOrdered(t *testing.T) {
	fds := probe(bareMethod("Zulu"), bareMethod("Alpha"))
	second := probe(bareMethod("Mike"))
	second.File[0].Name = proto.String("marque/v1/second.proto")
	fds.File = append(fds.File, second.File[0])

	report := CheckAnnotations(fds, fds)
	if report.MethodsChecked != 3 {
		t.Fatalf("MethodsChecked = %d, want 3", report.MethodsChecked)
	}

	got := make([]string, 0, len(report.Violations))
	for _, v := range report.Violations {
		got = append(got, v.Method)
	}
	want := []string{"marque.v1.ProbeService.Alpha", "marque.v1.ProbeService.Mike", "marque.v1.ProbeService.Zulu"}
	if len(got) != len(want) {
		t.Fatalf("violations = %v, want %d", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("violation %d = %q, want %q (order: %v)", i, got[i], want[i], got)
		}
	}
}

// Streaming methods are methods. Nothing about a stream makes retry safety
// somebody else's problem.
func TestCheckAnnotationsChecksStreamingMethods(t *testing.T) {
	for _, kind := range []string{"client", "server", "bidi"} {
		t.Run(kind, func(t *testing.T) {
			m := bareMethod("Stream")
			m.ClientStreaming = proto.Bool(kind == "client" || kind == "bidi")
			m.ServerStreaming = proto.Bool(kind == "server" || kind == "bidi")

			report := CheckAnnotations(probe(m), probe(m))
			if len(report.Violations) == 0 {
				t.Error("CheckAnnotations() = none, want a violation; a streaming method still declares")
			}
		})
	}
}

// schemacheck reads descriptor sets off disk, so the decode path is the one
// that matters. Setting extensions in memory never exercises it.
func TestCheckAnnotationsSurvivesAWireRoundTrip(t *testing.T) {
	original := probe(method("Keyed", keyed("nonce"), unknown), bareMethod("Bare"))

	raw, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	report := CheckAnnotations(&decoded, &decoded)
	if report.MethodsChecked != 2 {
		t.Fatalf("MethodsChecked = %d, want 2", report.MethodsChecked)
	}
	// The keyed method's annotation must have survived the round trip; if the
	// extension decoded as an unknown field it would look unannotated and
	// produce a second violation here.
	if len(report.Violations) != 1 {
		t.Errorf("violations = %v, want exactly one (the bare method)", report.Violations)
	}
	if !containsReason(report.Violations, "declares no (marque.v1.behaviour)") {
		t.Errorf("violations = %v, want the bare method's", report.Violations)
	}
}

// The committed schema is checked here as well as by `make lint`, so an
// unannotated method in an already-imported package fails `go test` without
// anyone having built buf first.
//
// This is a convenience, not the guard. It ranges protoregistry, which holds
// only files some Go package imported — the very limitation that made a
// descriptor set the input to CheckAnnotations. A service in a new proto
// package that nothing imports is invisible here, and `make schema-check` is
// what covers it.
func TestCommittedSchemaIsAnnotated(t *testing.T) {
	var fds descriptorpb.FileDescriptorSet
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if strings.HasPrefix(fd.Path(), "marque/") {
			fds.File = append(fds.File, protodesc.ToFileDescriptorProto(fd))
		}
		return true
	})

	report := CheckAnnotations(&fds, &fds)
	if report.MethodsChecked == 0 {
		t.Fatal("no methods found in the committed schema; this test is inspecting nothing")
	}
	if len(report.Violations) != 0 {
		t.Errorf("the committed schema has violations: %v", report.Violations)
	}
}

func TestUncoveredProtoFiles(t *testing.T) {
	owned := probe(method("M", natural(), unknown))

	got := UncoveredProtoFiles(owned, []string{
		"marque/v1/probe.proto",
		"marque/v1/hidden.proto",
		"elsewhere/thing.proto",
	})
	want := []string{"elsewhere/thing.proto", "marque/v1/hidden.proto"}

	if len(got) != len(want) {
		t.Fatalf("UncoveredProtoFiles() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UncoveredProtoFiles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
