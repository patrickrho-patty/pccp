package dari

import (
	"errors"
	"testing"
)

// profiles_test.go implements the map §3 / F.14-case-10/12 negotiation
// conformance matrix.

func TestProfileNegotiationMatrix(t *testing.T) {
	reg := NewProfileRegistry()

	// Kernel EXACT.
	res, err := reg.Negotiate([]ProfileOffer{{Profile: "dari/1", Capabilities: []CapabilityOffer{{ID: "authorization-grant", Critical: 1}}}})
	if err != nil || res[0].Status != ProfileExact {
		t.Fatalf("kernel must be EXACT: %v %v", res, err)
	}

	// Effects family gated on dari.tools/1.
	if !reg.SupportsEffects() {
		t.Fatal("dari.tools/1 must be registered as passed")
	}

	// dari.web/1 is UNSUPPORTED with the recorded reason.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.web/1"}})
	if err != nil || res[0].Status != ProfileUnsupported || res[0].ReasonCode == "" {
		t.Fatalf("web must be UNSUPPORTED with reason: %+v %v", res[0], err)
	}

	// CRITICAL web offer fails negotiation entirely (F.14 case 10).
	_, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.web/1", Capabilities: []CapabilityOffer{{ID: "origin-binding", Critical: 1}}}})
	if !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("critical UNSUPPORTED must fail negotiation, got %v", err)
	}

	// Non-critical unsupported: explicit result, no failure.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.collab/1", Capabilities: []CapabilityOffer{{ID: "chat", Critical: 0}}}})
	if err != nil || res[0].Status != ProfileUnsupported {
		t.Fatalf("non-critical unsupported must be explicit: %+v %v", res[0], err)
	}

	// Unknown profile → UNSUPPORTED.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.unknown/1"}})
	if err != nil || res[0].Status != ProfileUnsupported {
		t.Fatalf("unknown profile: %+v %v", res[0], err)
	}

	// Duplicate offer → hard error.
	if _, err := reg.Negotiate([]ProfileOffer{{Profile: "dari/1"}, {Profile: "dari/1"}}); !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("duplicate offer must fail, got %v", err)
	}

	// Duplicate capability → hard error.
	if _, err := reg.Negotiate([]ProfileOffer{{Profile: "dari/1", Capabilities: []CapabilityOffer{{ID: "a"}, {ID: "a"}}}}); err == nil {
		t.Fatal("duplicate capability must fail")
	}

	// Unsorted capability offer → hard error.
	if _, err := reg.Negotiate([]ProfileOffer{{Profile: "dari/1", Capabilities: []CapabilityOffer{{ID: "zzz"}, {ID: "aaa"}}}}); err == nil {
		t.Fatal("unsorted offer must fail")
	}

	// Degraded dropping a CRITICAL capability fails.
	_, err = reg.Negotiate([]ProfileOffer{{Profile: "dari/1", Capabilities: []CapabilityOffer{{ID: "not-a-kernel-capability", Critical: 1}}}})
	if !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("critical omission must fail, got %v", err)
	}
	// Degraded dropping a NON-critical capability is explicit.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari/1", Capabilities: []CapabilityOffer{{ID: "not-a-kernel-capability", Critical: 0}}}})
	if err != nil || res[0].Status != ProfileDegraded || len(res[0].Omitted) != 1 {
		t.Fatalf("non-critical degradation: %+v %v", res[0], err)
	}
}
