package schema

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

type option func(*descriptorpb.MethodOptions)

func safe(v bool) option {
	return func(o *descriptorpb.MethodOptions) { proto.SetExtension(o, marquev1.E_Safe, v) }
}

func idempotency(v marquev1.Idempotency) option {
	return func(o *descriptorpb.MethodOptions) { proto.SetExtension(o, marquev1.E_Idempotency, v) }
}

func keyField(v string) option {
	return func(o *descriptorpb.MethodOptions) { proto.SetExtension(o, marquev1.E_IdempotencyField, v) }
}

// options builds a MethodOptions from the given annotations. Passing none
// yields an empty-but-present options message, which is distinct from a method
// carrying no options at all — both must be caught.
func options(opts ...option) *descriptorpb.MethodOptions {
	o := &descriptorpb.MethodOptions{}
	for _, apply := range opts {
		apply(o)
	}
	return o
}

// probe builds a one-method descriptor set. requestFields names the fields on
// the request message, so the idempotency-key rule has something to resolve
// against.
func probe(opts *descriptorpb.MethodOptions, requestFields ...string) *descriptorpb.FileDescriptorSet {
	fields := make([]*descriptorpb.FieldDescriptorProto, 0, len(requestFields))
	for i, name := range requestFields {
		fields = append(fields, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(int32(i + 1)),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		})
	}

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
				Name: proto.String("ProbeService"),
				Method: []*descriptorpb.MethodDescriptorProto{{
					Name:       proto.String("Probe"),
					InputType:  proto.String(".marque.v1.ProbeRequest"),
					OutputType: proto.String(".marque.v1.ProbeResponse"),
					Options:    opts,
				}},
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
			name:   "safe alone is a declaration",
			fds:    probe(options(safe(true))),
			reason: "EDR-0020's own example annotates a read-only method with safe and nothing else",
		},
		{
			name: "an idempotency alone is a declaration",
			fds:  probe(options(idempotency(marquev1.Idempotency_IDEMPOTENCY_NATURAL))),
		},
		{
			name: "unsafe is a decision like any other",
			fds:  probe(options(idempotency(marquev1.Idempotency_IDEMPOTENCY_UNSAFE))),
		},
		{
			name: "keyed naming a field the request has",
			fds: probe(
				options(idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED), keyField("nonce")),
				"nonce",
			),
		},
		{
			name: "safe and keyed together are not contradictory",
			fds: probe(
				options(safe(true), idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED), keyField("nonce")),
				"nonce",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckAnnotations(tt.fds); len(got) != 0 {
				t.Errorf("CheckAnnotations() = %v, want none (%s)", got, tt.reason)
			}
		})
	}
}

func TestCheckAnnotationsRejects(t *testing.T) {
	tests := []struct {
		name string
		fds  *descriptorpb.FileDescriptorSet
		want string // substring the reason must contain
	}{
		{
			name: "no options at all",
			fds:  probe(nil),
			want: "declares neither",
		},
		{
			name: "options present but empty",
			fds:  probe(options()),
			want: "declares neither",
		},
		{
			name: "safe explicitly false is not a declaration of safety",
			fds:  probe(options(safe(false))),
			want: "declares neither",
		},
		{
			name: "safe and unsafe at once",
			fds:  probe(options(safe(true), idempotency(marquev1.Idempotency_IDEMPOTENCY_UNSAFE))),
			want: "cannot be one that must not be retried",
		},
		{
			name: "keyed without naming the key",
			fds:  probe(options(idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED))),
			want: "without an (marque.v1.idempotency_field)",
		},
		{
			name: "keyed naming a field the request does not have",
			fds: probe(
				options(idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED), keyField("nonce")),
				"reason",
			),
			want: "which marque.v1.ProbeRequest does not have",
		},
		{
			name: "a key on a method that is not keyed",
			fds: probe(
				options(idempotency(marquev1.Idempotency_IDEMPOTENCY_NATURAL), keyField("nonce")),
				"nonce",
			),
			want: "so the field means nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckAnnotations(tt.fds)
			if len(got) == 0 {
				t.Fatalf("CheckAnnotations() = none, want a violation containing %q", tt.want)
			}
			if !strings.Contains(got[0].Reason, tt.want) {
				t.Errorf("reason = %q, want it to contain %q", got[0].Reason, tt.want)
			}
			if got[0].Method != "marque.v1.ProbeService.Probe" {
				t.Errorf("Method = %q, want the fully-qualified name", got[0].Method)
			}
			if got[0].File != "marque/v1/probe.proto" {
				t.Errorf("File = %q, want the declaring file", got[0].File)
			}
		})
	}
}

// A request message declared inside another message still has to be resolvable,
// or the key rule would silently pass every method whose request is nested.
func TestCheckAnnotationsResolvesNestedRequestMessages(t *testing.T) {
	fds := probe(options(idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED), keyField("nonce")))
	file := fds.GetFile()[0]
	file.MessageType = []*descriptorpb.DescriptorProto{{
		Name: proto.String("Outer"),
		NestedType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("ProbeRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   proto.String("nonce"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		}},
	}}
	file.Service[0].Method[0].InputType = proto.String(".marque.v1.Outer.ProbeRequest")

	if got := CheckAnnotations(fds); len(got) != 0 {
		t.Errorf("CheckAnnotations() = %v, want none for a nested request message", got)
	}
}

// An unresolvable request message must fail rather than be assumed correct.
// A check that resolves doubt toward yes is not a check.
func TestCheckAnnotationsFailsOnUnresolvableRequest(t *testing.T) {
	fds := probe(options(idempotency(marquev1.Idempotency_IDEMPOTENCY_KEYED), keyField("nonce")))
	fds.GetFile()[0].Service[0].Method[0].InputType = proto.String(".marque.v1.Missing")

	got := CheckAnnotations(fds)
	if len(got) != 1 {
		t.Fatalf("CheckAnnotations() = %v, want one violation", got)
	}
	if !strings.Contains(got[0].Reason, "does not have") {
		t.Errorf("reason = %q, want it to report the field as absent", got[0].Reason)
	}
}

// Violations are sorted, so a schema with several problems produces the same
// output every run and a diff of the output means something.
func TestCheckAnnotationsIsOrdered(t *testing.T) {
	fds := probe(options(safe(true), idempotency(marquev1.Idempotency_IDEMPOTENCY_UNSAFE), keyField("nonce")))
	got := CheckAnnotations(fds)
	if len(got) != 2 {
		t.Fatalf("CheckAnnotations() = %v, want two violations", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Reason > got[i].Reason {
			t.Errorf("violations are not sorted: %q before %q", got[i-1].Reason, got[i].Reason)
		}
	}
}
