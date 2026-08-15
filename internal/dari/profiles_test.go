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

	// dari.web/1 (Task 13): runtime implemented + in-process vectors
	// passing; negotiated EXACT for implemented capabilities, DEGRADED
	// when a capability outside the implemented set is requested.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.web/1", Capabilities: []CapabilityOffer{{ID: "origin-binding", Critical: 1}}}})
	if err != nil || res[0].Status != ProfileExact {
		t.Fatalf("web core capability must be EXACT: %+v %v", res[0], err)
	}
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.web/1", Capabilities: []CapabilityOffer{{ID: "browser-deployment-evidence", Critical: 0}}}})
	if err != nil || res[0].Status != ProfileDegraded {
		t.Fatalf("web deployment evidence must be DEGRADED (non-critical omission): %+v %v", res[0], err)
	}

	// CRITICAL web capability the runtime does NOT implement fails
	// negotiation entirely (F.14 case 10: an unimplemented critical
	// capability on a critical offer).
	_, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.web/1", Capabilities: []CapabilityOffer{{ID: "not-implemented-critical", Critical: 1}}}})
	if !errors.Is(err, ErrNegotiationFailed) {
		t.Fatalf("critical UNSUPPORTED must fail negotiation, got %v", err)
	}

	// Non-critical capability outside the implemented set: explicit
	// DEGRADED omission, no failure.
	res, err = reg.Negotiate([]ProfileOffer{{Profile: "dari.collab/1", Capabilities: []CapabilityOffer{{ID: "live-deployment-evidence", Critical: 0}}}})
	if err != nil || res[0].Status != ProfileDegraded {
		t.Fatalf("non-critical omission must be DEGRADED: %+v %v", res[0], err)
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
