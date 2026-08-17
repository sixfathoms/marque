// Package schema enforces the rules the API description has to obey, against a
// descriptor set produced by `buf build`.
//
// The descriptor set is deliberately the input rather than protoregistry: the
// registry holds only files that some Go package happened to import, so a new
// service nobody imported here would go unchecked and the guard would pass
// having inspected nothing. A descriptor set is whatever buf found on disk.
package schema

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

// Violation is one method that breaks an annotation rule.
type Violation struct {
	// File is the path of the .proto file declaring the method.
	File string
	// Method is the fully-qualified method name.
	Method string
	// Reason says what is wrong, in the imperative.
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.File, v.Method, v.Reason)
}

// CheckAnnotations returns every method in fds that breaks a rule, sorted by
// method name so that output is stable between runs.
//
// The rules, and what each one is defending (EDR-0020):
//
//   - A method declares `safe = true`, or an `idempotency` other than
//     IDEMPOTENCY_UNSPECIFIED. The default must be a decision rather than an
//     accident, and an unannotated method is the way an integrator would get
//     retry safety wrong — by omission.
//   - `safe = true` and IDEMPOTENCY_UNSAFE contradict each other. A generated
//     client reading one and not the other would retry a method that must not
//     be retried.
//   - IDEMPOTENCY_KEYED names its key in `idempotency_field`, and that field
//     exists on the request message. A key the client cannot find is a method
//     that silently degrades to no key at all.
//   - `idempotency_field` on anything other than IDEMPOTENCY_KEYED is
//     meaningless, and a meaningless annotation is one a reader will trust.
func CheckAnnotations(fds *descriptorpb.FileDescriptorSet) []Violation {
	messages := indexMessages(fds)

	var violations []Violation
	for _, file := range fds.GetFile() {
		for _, service := range file.GetService() {
			for _, method := range service.GetMethod() {
				name := fmt.Sprintf("%s.%s.%s", file.GetPackage(), service.GetName(), method.GetName())
				for _, reason := range checkMethod(method, messages) {
					violations = append(violations, Violation{
						File:   file.GetName(),
						Method: name,
						Reason: reason,
					})
				}
			}
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Method != violations[j].Method {
			return violations[i].Method < violations[j].Method
		}
		return violations[i].Reason < violations[j].Reason
	})
	return violations
}

func checkMethod(method *descriptorpb.MethodDescriptorProto, messages map[string]*descriptorpb.DescriptorProto) []string {
	opts := method.GetOptions()
	safe := extensionBool(opts, marquev1.E_Safe)
	idempotency := extensionIdempotency(opts, marquev1.E_Idempotency)
	keyField := extensionString(opts, marquev1.E_IdempotencyField)

	var reasons []string

	if !safe && idempotency == marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED {
		reasons = append(reasons, "declares neither (marque.v1.safe) = true nor an (marque.v1.idempotency); "+
			"every method states whether repeating it is safe")
	}

	if safe && idempotency == marquev1.Idempotency_IDEMPOTENCY_UNSAFE {
		reasons = append(reasons, "is marked safe and IDEMPOTENCY_UNSAFE at once; a read-only method "+
			"cannot be one that must not be retried")
	}

	switch idempotency {
	case marquev1.Idempotency_IDEMPOTENCY_KEYED:
		switch {
		case keyField == "":
			reasons = append(reasons, "is IDEMPOTENCY_KEYED without an (marque.v1.idempotency_field) "+
				"naming the key")
		case !requestHasField(method, messages, keyField):
			reasons = append(reasons, fmt.Sprintf(
				"names %q as its idempotency field, which %s does not have",
				keyField, trimLeadingDot(method.GetInputType())))
		}
	case marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED,
		marquev1.Idempotency_IDEMPOTENCY_NATURAL,
		marquev1.Idempotency_IDEMPOTENCY_UNSAFE:
		if keyField != "" {
			reasons = append(reasons, fmt.Sprintf(
				"sets (marque.v1.idempotency_field) = %q but is not IDEMPOTENCY_KEYED, "+
					"so the field means nothing", keyField))
		}
	}

	return reasons
}

func requestHasField(
	method *descriptorpb.MethodDescriptorProto,
	messages map[string]*descriptorpb.DescriptorProto,
	field string,
) bool {
	message, ok := messages[trimLeadingDot(method.GetInputType())]
	if !ok {
		// The request message is not in this descriptor set, so nothing can be
		// proved about it. Report the field as missing rather than assume it is
		// there: a check that resolves doubt toward yes is not a check.
		return false
	}
	for _, f := range message.GetField() {
		if f.GetName() == field {
			return true
		}
	}
	return false
}

// indexMessages maps every message's fully-qualified name to its descriptor,
// descending into nested messages.
func indexMessages(fds *descriptorpb.FileDescriptorSet) map[string]*descriptorpb.DescriptorProto {
	messages := make(map[string]*descriptorpb.DescriptorProto)
	for _, file := range fds.GetFile() {
		for _, message := range file.GetMessageType() {
			indexMessage(messages, file.GetPackage(), message)
		}
	}
	return messages
}

func indexMessage(into map[string]*descriptorpb.DescriptorProto, prefix string, message *descriptorpb.DescriptorProto) {
	name := message.GetName()
	if prefix != "" {
		name = prefix + "." + name
	}
	into[name] = message
	for _, nested := range message.GetNestedType() {
		indexMessage(into, name, nested)
	}
}

func trimLeadingDot(s string) string { return strings.TrimPrefix(s, ".") }

// The three accessors below read the value rather than merely its presence, so
// an explicit `option (marque.v1.safe) = false;` does not count as a
// declaration of safety.

func extensionBool(opts *descriptorpb.MethodOptions, xt protoreflect.ExtensionType) bool {
	if opts == nil {
		return false
	}
	value, ok := proto.GetExtension(opts, xt).(bool)
	return ok && value
}

func extensionString(opts *descriptorpb.MethodOptions, xt protoreflect.ExtensionType) string {
	if opts == nil {
		return ""
	}
	value, _ := proto.GetExtension(opts, xt).(string)
	return value
}

func extensionIdempotency(opts *descriptorpb.MethodOptions, xt protoreflect.ExtensionType) marquev1.Idempotency {
	if opts == nil {
		return marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED
	}
	value, _ := proto.GetExtension(opts, xt).(marquev1.Idempotency)
	return value
}
