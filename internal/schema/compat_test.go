package schema

import (
	"testing"

	"google.golang.org/protobuf/proto"

	marquev1 "github.com/sixfathoms/marque/gen/marque/v1"
)

const (
	unspec  = marquev1.Idempotency_IDEMPOTENCY_UNSPECIFIED
	natrl   = marquev1.Idempotency_IDEMPOTENCY_NATURAL
	keyedI  = marquev1.Idempotency_IDEMPOTENCY_KEYED
	unsafeI = marquev1.Idempotency_IDEMPOTENCY_UNSAFE
)

// The whole matrix, written out rather than derived, because the interesting
// cases are the ones that look backwards. NATURAL to KEYED is forbidden even
// though the new schema is stricter — a client built against the old one
// retries with no key at all, and this server can no longer tell the repeat
// from a new call. UNSAFE to anything is allowed even though it looks like a
// widening, because a client built against UNSAFE never retried.
func TestCheckCompatibilityMatrix(t *testing.T) {
	tests := []struct {
		before, after marquev1.Idempotency
		allowed       bool
		why           string
	}{
		{unspec, unspec, true, "nothing was promised"},
		{unspec, natrl, true, "nothing was promised"},
		{unspec, keyedI, true, "nothing was promised"},
		{unspec, unsafeI, true, "nothing was promised"},

		{natrl, unspec, true, "an unannotated method is caught by the annotation rules, not here"},
		{natrl, natrl, true, "unchanged"},
		{natrl, keyedI, false, "the old client retries without the key this server now needs"},
		{natrl, unsafeI, false, "the old client retries something that must not be retried"},

		{keyedI, unspec, true, "an unannotated method is caught by the annotation rules, not here"},
		{keyedI, natrl, true, "repeating became harmless; the old client's key is simply ignored"},
		{keyedI, keyedI, true, "unchanged"},
		{keyedI, unsafeI, false, "the old client retries something that must not be retried"},

		{unsafeI, unspec, true, "the old client never retried"},
		{unsafeI, natrl, true, "the old client never retried"},
		{unsafeI, keyedI, true, "the old client never retried"},
		{unsafeI, unsafeI, true, "unchanged"},
	}

	for _, tt := range tests {
		name := tt.before.String() + "_to_" + tt.after.String()
		t.Run(name, func(t *testing.T) {
			before := probe(method("M", behaviour(false, tt.before, keyFor(tt.before)), unknown))
			after := probe(method("M", behaviour(false, tt.after, keyFor(tt.after)), unknown))

			got := CheckCompatibility(before, after)
			if tt.allowed && len(got) != 0 {
				t.Errorf("CheckCompatibility() = %v, want none (%s)", got, tt.why)
			}
			if !tt.allowed && len(got) == 0 {
				t.Errorf("CheckCompatibility() = none, want a violation (%s)", tt.why)
			}
		})
	}
}

func keyFor(i marquev1.Idempotency) string {
	if i == keyedI {
		return "nonce"
	}
	return ""
}

func TestCheckCompatibilitySafe(t *testing.T) {
	tests := []struct {
		name          string
		before, after bool
		allowed       bool
	}{
		{"safe stays safe", true, true, true},
		{"unsafe stays unsafe", false, false, true},
		{"becoming safe is fine; the old client simply did not retry", false, true, true},
		{"ceasing to be safe is not; the old client still retries freely", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := probe(method("M", behaviour(tt.before, natrl, ""), unknown))
			after := probe(method("M", behaviour(tt.after, natrl, ""), unknown))

			got := CheckCompatibility(before, after)
			if tt.allowed && len(got) != 0 {
				t.Errorf("CheckCompatibility() = %v, want none", got)
			}
			if !tt.allowed {
				if len(got) == 0 {
					t.Fatal("CheckCompatibility() = none, want a violation")
				}
				if !containsReason(got, "was declared safe and no longer is") {
					t.Errorf("violations = %v, want the safe-to-unsafe reason", got)
				}
			}
		})
	}
}

// Renaming the field a key travels in is as breaking as removing it: the old
// client keeps filling the previous field and this server stops reading it.
func TestCheckCompatibilityRejectsAMovedKey(t *testing.T) {
	before := probe(method("M", keyed("nonce"), unknown))
	after := probe(method("M", keyed("token"), unknown))

	got := CheckCompatibility(before, after)
	if !containsReason(got, `moved its idempotency key from "nonce" to "token"`) {
		t.Errorf("CheckCompatibility() = %v, want the moved-key reason", got)
	}
}

// A method that did not exist before constrains nobody.
func TestCheckCompatibilityAllowsNewMethods(t *testing.T) {
	before := probe(method("Old", natural(), unknown))
	after := probe(method("Old", natural(), unknown), method("New", behaviour(false, unsafeI, ""), unknown))

	if got := CheckCompatibility(before, after); len(got) != 0 {
		t.Errorf("CheckCompatibility() = %v, want none for an added method", got)
	}
}

// Removing a method is buf breaking's job, not this one's — and it must not be
// reported here as a weakened declaration.
func TestCheckCompatibilityIgnoresRemovedMethods(t *testing.T) {
	before := probe(method("Gone", natural(), unknown), method("Kept", natural(), unknown))
	after := probe(method("Kept", natural(), unknown))

	if got := CheckCompatibility(before, after); len(got) != 0 {
		t.Errorf("CheckCompatibility() = %v, want none; removal is buf breaking's business", got)
	}
}

// The reason a method is matched by name and not by position.
func TestCheckCompatibilityMatchesByQualifiedName(t *testing.T) {
	before := probe(method("A", natural(), unknown), method("B", behaviour(false, unsafeI, ""), unknown))
	after := probe(method("B", behaviour(false, unsafeI, ""), unknown), method("A", natural(), unknown))

	if got := CheckCompatibility(before, after); len(got) != 0 {
		t.Errorf("CheckCompatibility() = %v, want none; only the order changed", got)
	}
}

// Two methods weakening at once must both be reported, in a stable order.
func TestCheckCompatibilityIsOrdered(t *testing.T) {
	before := probe(method("Zulu", natural(), unknown), method("Alpha", natural(), unknown))
	after := probe(
		method("Zulu", behaviour(false, unsafeI, ""), unknown),
		method("Alpha", behaviour(false, unsafeI, ""), unknown),
	)

	got := CheckCompatibility(before, after)
	if len(got) != 2 {
		t.Fatalf("CheckCompatibility() = %v, want two violations", got)
	}
	if got[0].Method != "marque.v1.ProbeService.Alpha" || got[1].Method != "marque.v1.ProbeService.Zulu" {
		t.Errorf("violations are not sorted by method: %v", got)
	}
}

// A method whose options vanish entirely is a weakening the annotation rules
// catch; compat must not also crash on the nil behaviour.
func TestCheckCompatibilityHandlesMissingOptions(t *testing.T) {
	before := probe(method("M", behaviour(true, natrl, ""), noSide))
	after := probe(bareMethod("M"))

	got := CheckCompatibility(before, after)
	if !containsReason(got, "was declared safe and no longer is") {
		t.Errorf("CheckCompatibility() = %v, want the safe-to-unsafe reason", got)
	}
}

// The declaring file travels with the violation so the message points at
// something editable.
func TestCheckCompatibilityReportsTheFile(t *testing.T) {
	before := probe(method("M", natural(), unknown))
	after := probe(method("M", behaviour(false, unsafeI, ""), unknown))
	after.File[0].Name = proto.String("marque/v1/renamed.proto")

	got := CheckCompatibility(before, after)
	if len(got) != 1 {
		t.Fatalf("CheckCompatibility() = %v, want one violation", got)
	}
	if got[0].File != "marque/v1/renamed.proto" {
		t.Errorf("File = %q, want the file declaring the method now", got[0].File)
	}
}
