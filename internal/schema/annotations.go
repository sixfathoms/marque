// Package schema enforces the rules the API description has to obey, against
// descriptor sets produced by `buf build`.
//
// A descriptor set is deliberately the input rather than protoregistry: the
// registry holds only files that some Go package happened to import, so a new
// service nobody imported here would go unchecked and the guard would pass
// having inspected nothing.
//
// Two sets are needed, not one. `buf build` includes imported files, and
// walking those would put a dependency's methods — google.longrunning, a health
// service — through rules they cannot satisfy and fail the build on code this
// project does not own. `buf build --exclude-imports` gives exactly the files
// this repository owns, but drops the imported *messages* a request type might
// live in. So: methods come from the owned set, message resolution from the
// full one.
package schema

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

// Violation is one method that breaks a rule.
type Violation struct {
	// File is the path of the .proto file declaring the method.
	File string
	// Method is the fully-qualified method name.
	Method string
	// Reason says what is wrong.
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.File, v.Method, v.Reason)
}

// Report is what a check found, and how much it looked at.
//
// MethodsChecked exists because the interesting failure of a guard like this is
// not a wrong answer but a vacuous one. A descriptor set with files but no
// services — one service file deleted, a `buf.yaml` `excludes:` entry, a
// `--path` narrowing — produces no violations while inspecting nothing at all.
type Report struct {
	MethodsChecked int
	Violations     []Violation
}

// CheckAnnotations checks every method declared in owned, resolving request
// messages against all. Pass the same set twice only if it genuinely has no
// imports.
//
// The rules, and what each one defends (EDR-0020, EDR-0040):
//
//   - A method declares `safe = true`, or an `idempotency` other than
//     IDEMPOTENCY_UNSPECIFIED. The default must be a decision rather than an
//     accident, and an unannotated method is how an integrator gets retry
//     safety wrong — by omission.
//   - `safe = true` and IDEMPOTENCY_UNSAFE contradict each other.
//   - `safe` agrees with the standard `idempotency_level` in both directions.
//     Connect's own generator reads `idempotency_level` and nothing else, so a
//     disagreement means the generated client and this repository's metadata
//     describe different methods.
//   - IDEMPOTENCY_KEYED names its key in `idempotency_field`; that field exists
//     on the request message and is a singular string or bytes, because that is
//     what a client can actually carry through a retry.
//   - `idempotency_field` on anything else is meaningless, and a meaningless
//     annotation is one a reader will trust.
//   - An idempotency value outside the known set is rejected rather than
//     ignored, so a stale `gen/` cannot let a new enum value skip every rule.
func CheckAnnotations(owned, all *descriptorpb.FileDescriptorSet) Report {
	messages := indexMessages(all)

	var report Report
	for _, file := range owned.GetFile() {
		for _, service := range file.GetService() {
			for _, method := range service.GetMethod() {
				report.MethodsChecked++
				name := qualify(file.GetPackage(), service.GetName(), method.GetName())
				for _, reason := range checkMethod(method, messages) {
					report.Violations = append(report.Violations, Violation{
						File:   file.GetName(),
						Method: name,
						Reason: reason,
					})
				}
			}
		}
	}

	slices.SortFunc(report.Violations, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Method, b.Method), cmp.Compare(a.Reason, b.Reason))
	})
	return report
}

func checkMethod(method *descriptorpb.MethodDescriptorProto, messages map[string]*descriptorpb.DescriptorProto) []string {
	behaviour := behaviourOf(method)
	safe := behaviour.GetSafe()
	idempotency := behaviour.GetIdempotency()
	keyField := behaviour.GetIdempotencyField()

	var reasons []string

	if _, known := marquev1.Idempotency_name[int32(idempotency)]; !known {
		// Checked before the switch rather than with a `default:` case. A
		// `default:` would satisfy the exhaustive linter's
		// default-signifies-exhaustive setting and stop it flagging a genuinely
		// new enum value, which is the more useful warning of the two.
		return []string{fmt.Sprintf(
			"declares idempotency %d, which this build does not know; "+
				"regenerate gen/ or add the value to the rules", idempotency)}
	}

	if !safe && idempotency == marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED {
		reasons = append(reasons, "declares no (marque.v1.behaviour); every method states "+
			"whether repeating it is safe")
	}

	if safe && idempotency == marquev1.Idempotency_IDEMPOTENCY_UNSAFE {
		reasons = append(reasons, "is marked safe and IDEMPOTENCY_UNSAFE at once; a read-only "+
			"method cannot be one that must not be retried")
	}

	reasons = append(reasons, checkIdempotencyLevel(method, safe, idempotency)...)

	switch idempotency {
	case marquev1.Idempotency_IDEMPOTENCY_KEYED:
		reasons = append(reasons, checkKey(method, messages, keyField)...)
	case marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED,
		marquev1.Idempotency_IDEMPOTENCY_NATURAL,
		marquev1.Idempotency_IDEMPOTENCY_UNSAFE:
		if keyField != "" {
			reasons = append(reasons, fmt.Sprintf(
				"sets idempotency_field %q but is not IDEMPOTENCY_KEYED, so the field "+
					"means nothing", keyField))
		}
	}

	return reasons
}

// safeNeedsNoSideEffects is shared by the two branches that can reach it, so
// that the `safe` direction of the rule cannot be implemented in one and
// forgotten in the other — which is exactly how it was lost once already, when
// this switch was restructured around the level and the direction survived only
// in the branch for an unset one.
const safeNeedsNoSideEffects = "is marked safe but does not set " +
	"`option idempotency_level = NO_SIDE_EFFECTS`, which is the option a generated Connect " +
	"client reads to treat a method as read-only."

// checkIdempotencyLevel keeps the declaration and the standard option in step,
// across all three of the standard option's values.
//
// Connect's generator reads `idempotency_level` and nothing else, emitting
// WithIdempotency(...) onto the Spec, where every interceptor can see it —
// including the retry interceptor EDR-0040 undertakes to build. A disagreement
// therefore does not stay in the schema: the generated client is the one that
// acts, and it would act on the standard option.
//
// NO_SIDE_EFFECTS is the read-only claim, so it pairs with `safe` — and the
// pairing is a biconditional, because stating only that NO_SIDE_EFFECTS needs
// `safe` is what let `safe` with IDEMPOTENT through once. IDEMPOTENT is the
// weaker "repeating is harmless" claim, so it pairs with NATURAL or KEYED and
// must never appear on a method declared UNSAFE — the pairing that would
// otherwise let a method Marque says must not be retried advertise itself to
// the retry interceptor as one that may.
func checkIdempotencyLevel(
	method *descriptorpb.MethodDescriptorProto,
	safe bool,
	idempotency marquev1.Idempotency,
) []string {
	level := method.GetOptions().GetIdempotencyLevel()

	var reasons []string

	switch level {
	case descriptorpb.MethodOptions_NO_SIDE_EFFECTS:
		if !safe {
			reasons = append(reasons, "sets `option idempotency_level = NO_SIDE_EFFECTS` without "+
				"(marque.v1.behaviour).safe, so a generated client treats it as read-only and this "+
				"repository does not")
		}
	case descriptorpb.MethodOptions_IDEMPOTENT:
		if safe {
			reasons = append(reasons, safeNeedsNoSideEffects+
				" It sets IDEMPOTENT instead, which claims only that repeating it is harmless")
		}
		if idempotency == marquev1.Idempotency_IDEMPOTENCY_UNSAFE {
			reasons = append(reasons, "sets `option idempotency_level = IDEMPOTENT` while declaring "+
				"IDEMPOTENCY_UNSAFE; a generated client would advertise to its interceptors that "+
				"repeating this is harmless")
		}
	case descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN:
		if safe {
			reasons = append(reasons, safeNeedsNoSideEffects+" It sets no level at all")
		}
	}

	return reasons
}

func checkKey(
	method *descriptorpb.MethodDescriptorProto,
	messages map[string]*descriptorpb.DescriptorProto,
	keyField string,
) []string {
	if keyField == "" {
		return []string{"is IDEMPOTENCY_KEYED without an idempotency_field naming the key"}
	}

	request := trimLeadingDot(method.GetInputType())
	message, ok := messages[request]
	if !ok {
		// Nothing can be proved about a message that is not in the set. Report
		// it rather than assume it is fine: a check that resolves doubt toward
		// yes is not a check.
		return []string{fmt.Sprintf("names %q as its idempotency field, but its request message %s "+
			"is not in the descriptor set", keyField, request)}
	}

	for _, field := range message.GetField() {
		if field.GetName() != keyField {
			continue
		}
		if reason := keyFieldShape(field); reason != "" {
			return []string{fmt.Sprintf("names %q as its idempotency field, which %s", keyField, reason)}
		}
		return nil
	}

	return []string{fmt.Sprintf("names %q as its idempotency field, which %s does not have",
		keyField, request)}
}

// keyFieldShape returns why a field cannot carry an idempotency key, or "".
// A key has to survive a retry intact, which a repeated, message-typed or
// oneof-member field cannot do.
func keyFieldShape(field *descriptorpb.FieldDescriptorProto) string {
	switch {
	case field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED:
		return "is repeated; a key is a single value"
	case field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_STRING &&
		field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return fmt.Sprintf("is %s; a key is a string or bytes",
			strings.TrimPrefix(field.GetType().String(), "TYPE_"))
	case field.OneofIndex != nil && !field.GetProto3Optional():
		// A proto3 `optional` field is represented as a synthetic one-member
		// oneof, which is fine. A real oneof is not: the key would be absent
		// whenever another member is set.
		return "is a member of a oneof; a key cannot be optional on the wire"
	default:
		return ""
	}
}

// UncoveredProtoFiles returns every path in onDisk that owned does not account
// for.
//
// This closes the gap that method counting alone leaves. A `.proto` outside the
// buf module, or one hidden by a `buf.yaml` `excludes:` entry, is not in any
// descriptor set — so no rule runs on it, and every check stays green having
// never seen it. Paths in onDisk are relative to the module root, matching the
// names buf gives files in a descriptor set.
func UncoveredProtoFiles(owned *descriptorpb.FileDescriptorSet, onDisk []string) []string {
	covered := make(map[string]struct{}, len(owned.GetFile()))
	for _, file := range owned.GetFile() {
		covered[file.GetName()] = struct{}{}
	}

	var uncovered []string
	for _, path := range onDisk {
		if _, ok := covered[path]; !ok {
			uncovered = append(uncovered, path)
		}
	}
	slices.Sort(uncovered)
	return uncovered
}

// behaviourOf returns a method's declared behaviour, or the zero value when it
// declares none. The value is read rather than merely its presence, so an
// explicit `{safe: false}` is not a declaration of safety.
func behaviourOf(method *descriptorpb.MethodDescriptorProto) *marquev1.MethodBehaviour {
	opts := method.GetOptions()
	if opts == nil {
		return nil
	}
	behaviour, _ := proto.GetExtension(opts, marquev1.E_Behaviour).(*marquev1.MethodBehaviour)
	return behaviour
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
	name := qualify(prefix, message.GetName())
	into[name] = message
	for _, nested := range message.GetNestedType() {
		indexMessage(into, name, nested)
	}
}

// qualify joins name parts with dots, skipping empty ones so a file with no
// package does not produce a leading dot.
func qualify(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ".")
}

func trimLeadingDot(s string) string { return strings.TrimPrefix(s, ".") }
