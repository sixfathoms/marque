package schema

import (
	"cmp"
	"fmt"
	"slices"

	"google.golang.org/protobuf/types/descriptorpb"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

// CheckCompatibility returns a violation for every method whose declared
// behaviour weakened between before and after.
//
// `buf breaking` does not do this. Its rules compare field numbers, names and
// types; they do not look at custom method options at all, so a method can be
// reclassified from safe to unsafe and every check stays green
// (EDR-0040).
//
// The rule follows from one fact: a client built against `before` has already
// compiled that retry policy in. It will keep applying it until it is rebuilt,
// against a server that has moved on. So the only forbidden changes are the
// ones that make the *old* client's cached policy unsafe:
//
//   - safe true to false. The old client retries freely, and the method is no
//     longer read-only.
//   - NATURAL to KEYED. The old client retries without a key, because it was
//     told none was needed; the server can no longer recognise the repeat.
//   - NATURAL or KEYED to UNSAFE. The old client retries something that must
//     not be retried.
//   - Renaming the field a KEYED method's key travels in. The old client keeps
//     filling the previous field, which the server now ignores.
//
// Everything else is safe, including the ones that look like weakening:
// UNSAFE to anything is fine because the old client never retried, and KEYED to
// NATURAL is fine because repeating became harmless.
//
// A method whose declaration must genuinely weaken gets a new method name,
// exactly as a field whose meaning changes gets a new number.
func CheckCompatibility(before, after *descriptorpb.FileDescriptorSet) CompatReport {
	previous := indexMethods(before)

	report := CompatReport{MethodsBefore: len(previous)}
	for name, now := range indexMethods(after) {
		was, existed := previous[name]
		if !existed {
			// A new method constrains nobody.
			continue
		}
		report.MethodsCompared++
		for _, reason := range compareBehaviour(was.behaviour, now.behaviour) {
			report.Violations = append(report.Violations,
				Violation{File: now.file, Method: name, Reason: reason})
		}
	}

	slices.SortFunc(report.Violations, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Method, b.Method), cmp.Compare(a.Reason, b.Reason))
	})
	return report
}

// CompatReport is what the comparison found, and how many pairs it actually
// compared.
//
// The count is not decoration. Methods are matched by fully-qualified name, so
// renaming a service — or moving an RPC into another one — matches nothing, and
// a comparison of zero pairs reports no violations at all. `buf breaking`
// catches a rename and runs first, but a check whose vacuous case looks
// identical to its passing case is the thing this package exists not to be.
type CompatReport struct {
	// MethodsBefore is how many methods the previous schema declared.
	MethodsBefore int
	// MethodsCompared is how many of them were matched by name in the new one.
	MethodsCompared int
	// Violations is every weakened declaration found.
	Violations []Violation
}

func compareBehaviour(was, now *marquev1.MethodBehaviour) []string {
	var reasons []string

	// The previous schema never passed through CheckAnnotations, so this is the
	// one place an idempotency value the build does not know can appear —
	// a base ref whose enum has since shrunk. Rejecting it keeps this side
	// failing closed, as the annotation rules do; falling through to the
	// switch below would silently allow every transition out of it.
	for label, value := range map[string]marquev1.Idempotency{"was": was.GetIdempotency(), "is now": now.GetIdempotency()} {
		if _, known := marquev1.Idempotency_name[int32(value)]; !known {
			reasons = append(reasons, fmt.Sprintf(
				"%s idempotency %d, which this build does not know, so no transition from it "+
					"can be judged; regenerate gen/", label, value))
		}
	}
	if len(reasons) > 0 {
		return reasons
	}

	if was.GetSafe() && !now.GetSafe() {
		reasons = append(reasons, "was declared safe and no longer is; a client built against the "+
			"previous schema still retries it freely. Add a new method instead")
	}

	wasIdempotency, nowIdempotency := was.GetIdempotency(), now.GetIdempotency()
	if reason := weakened(wasIdempotency, nowIdempotency); reason != "" {
		reasons = append(reasons, reason)
	}

	if wasIdempotency == marquev1.Idempotency_IDEMPOTENCY_KEYED &&
		nowIdempotency == marquev1.Idempotency_IDEMPOTENCY_KEYED &&
		was.GetIdempotencyField() != now.GetIdempotencyField() {
		reasons = append(reasons, fmt.Sprintf(
			"moved its idempotency key from %q to %q; a client built against the previous "+
				"schema still sends the key in %[1]q, which this server now ignores",
			was.GetIdempotencyField(), now.GetIdempotencyField()))
	}

	return reasons
}

// weakened reports why moving from was to now breaks a client that compiled the
// previous declaration, or "" when the move is safe.
func weakened(was, now marquev1.Idempotency) string {
	switch was {
	case marquev1.Idempotency_IDEMPOTENCY_NATURAL:
		switch now {
		case marquev1.Idempotency_IDEMPOTENCY_KEYED:
			return "was IDEMPOTENCY_NATURAL and is now IDEMPOTENCY_KEYED; a client built against " +
				"the previous schema retries without a key, which this server cannot recognise " +
				"as a repeat. Add a new method instead"
		case marquev1.Idempotency_IDEMPOTENCY_UNSAFE:
			return "was IDEMPOTENCY_NATURAL and is now IDEMPOTENCY_UNSAFE; a client built against " +
				"the previous schema retries it. Add a new method instead"
		case marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED,
			marquev1.Idempotency_IDEMPOTENCY_NATURAL:
			return ""
		}
	case marquev1.Idempotency_IDEMPOTENCY_KEYED:
		if now == marquev1.Idempotency_IDEMPOTENCY_UNSAFE {
			return "was IDEMPOTENCY_KEYED and is now IDEMPOTENCY_UNSAFE; a client built against " +
				"the previous schema retries it carrying a key. Add a new method instead"
		}
	case marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED,
		marquev1.Idempotency_IDEMPOTENCY_UNSAFE:
		// Nothing was promised, or nothing was retried. Any move is safe.
	}
	return ""
}

type declaredMethod struct {
	file      string
	behaviour *marquev1.MethodBehaviour
}

func indexMethods(fds *descriptorpb.FileDescriptorSet) map[string]declaredMethod {
	methods := make(map[string]declaredMethod)
	for _, file := range fds.GetFile() {
		for _, service := range file.GetService() {
			for _, method := range service.GetMethod() {
				name := qualify(file.GetPackage(), service.GetName(), method.GetName())
				methods[name] = declaredMethod{file: file.GetName(), behaviour: behaviourOf(method)}
			}
		}
	}
	return methods
}
