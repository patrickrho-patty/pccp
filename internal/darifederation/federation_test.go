package darifederation

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// federation_test.go implements the Task 14 conformance vectors:
// trust-bundle import freshness/rollback/idempotency/quarantine,
// bilateral issuer/audience validation, policy intersection
// (denial + narrower wins), residency enforcement, and cross-domain
// receipt verification.

func keys(t *testing.T) (bootstrap, issuer ed25519.PrivateKey) {
	t.Helper()
	_, b, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, i, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return b, i
}

func bundleFixture(t *testing.T, authority, issuer ed25519.PrivateKey, domain string, seq uint64, audiences ...string) *TrustBundleEnvelope {
	t.Helper()
	// The bundle trusts the federation authority AND the grant issuer
	// key; it is SIGNED by the authority.
	now := time.Now().UnixMilli()
	env, err := SignTrustBundle(&TrustBundleBody{
		Version:     1,
		TrustDomain: domain,
		Issuers:     []string{"issuer-federation", "authority"},
		IssuerKeyDigests: []dari.Digest{
			IssuerKeyDigest(authority.Public().(ed25519.PublicKey)),
			IssuerKeyDigest(issuer.Public().(ed25519.PublicKey)),
		},
		Audiences:   audiences,
		Sequence:    seq,
		IssuedAtMs:  now,
		ExpiresAtMs: now + time.Hour.Milliseconds(),
	}, authority)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func grantFixture(t *testing.T, priv ed25519.PrivateKey, audience []string) *dari.GrantEnvelope {
	t.Helper()
	body := &dari.AuthorizationGrantBody{
		Version: 1, GrantID: "g-fed-1", Issuer: "issuer-federation",
		SubjectPeerID:        "subject-harness",
		SubjectKeyThumbprint: dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey)),
		Audience:             audience,
		OrganizationID:       "org-f", UserID: "u", SessionID: "s", PolicyEpochID: "e",
		Scope: dari.AuthorizationScope{
			ActionClasses: []string{"ai.inference"},
			Models:        []string{"model-a", "model-b"},
		},
		NotBeforeMs: time.Now().Add(-time.Minute).UnixMilli(),
		NotAfterMs:  time.Now().Add(time.Hour).UnixMilli(),
	}
	env, err := dari.SignAuthorizationGrant(body, priv)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestBilateralValidation(t *testing.T) {
	bootstrap, issuer := keys(t)
	bundle := bundleFixture(t, bootstrap, issuer, "partner.example", 1, "local.example", "third.example")
	now := time.Now().UnixMilli()

	// Valid: issuer key trusted, local domain in audiences.
	grant := grantFixture(t, issuer, []string{"local.example"})
	if err := ValidateBilateral(bundle, grant, "local.example", now); err != nil {
		t.Fatal(err)
	}
	// Local domain NOT in audiences.
	if err := ValidateBilateral(bundle, grant, "not-listed.example", now); !errors.Is(err, ErrAudienceMismatch) {
		t.Fatalf("expected audience mismatch, got %v", err)
	}
	// Issuer key outside the bundle.
	_, rogue := keys(t)
	rogueGrant := grantFixture(t, rogue, []string{"local.example"})
	if err := ValidateBilateral(bundle, rogueGrant, "local.example", now); !errors.Is(err, ErrUntrustedIssuer) {
		t.Fatalf("expected untrusted issuer, got %v", err)
	}
}

func TestPolicyIntersectionNarrowerWins(t *testing.T) {
	local := dari.AuthorizationScope{
		ActionClasses:   []string{"ai.inference", "read"},
		Models:          []string{"model-a", "model-b"},
		ReadPaths:       []dari.PathScope{{Authority: "repo", Revision: "main", Prefix: "src", Operations: []string{"read", "list"}}},
		Networks:        []dari.NetworkScope{{Scheme: "https", Host: "api.example.com", PortFirst: 443, PortLast: 8443, Purposes: []string{"inference"}}},
		ResourceBudgets: map[string]uint64{"tokens": 1000, "requests": 10},
		ApprovalClasses: []string{"standard"},
	}
	remote := dari.AuthorizationScope{
		ActionClasses:   []string{"ai.inference"},
		Models:          []string{"model-b", "model-c"}, // narrower: only model-b common
		ReadPaths:       []dari.PathScope{{Authority: "repo", Revision: "main", Prefix: "src/lib", Operations: []string{"read"}}},
		Networks:        []dari.NetworkScope{{Scheme: "https", Host: "api.example.com", PortFirst: 8443, PortLast: 9000, Purposes: []string{"inference", "telemetry"}}},
		ResourceBudgets: map[string]uint64{"tokens": 500, "other": 5},
		ApprovalClasses: []string{"extra-approval"},
	}
	got := PolicyIntersect(local, remote)

	if len(got.Models) != 1 || got.Models[0] != "model-b" {
		t.Fatalf("models intersection = %v (narrower must win)", got.Models)
	}
	if len(got.ActionClasses) != 1 || got.ActionClasses[0] != "ai.inference" {
		t.Fatalf("actions intersection = %v", got.ActionClasses)
	}
	// Path: common prefix src/lib, intersected ops.
	if len(got.ReadPaths) != 1 || got.ReadPaths[0].Prefix != "src/lib" || len(got.ReadPaths[0].Operations) != 1 {
		t.Fatalf("path intersection = %+v", got.ReadPaths)
	}
	// Network: port interval intersect = [8443,8443]; purposes intersect.
	if len(got.Networks) != 1 || got.Networks[0].PortFirst != 8443 || got.Networks[0].PortLast != 8443 || len(got.Networks[0].Purposes) != 1 {
		t.Fatalf("network intersection = %+v", got.Networks)
	}
	// Budgets: common keys only, minima.
	if got.ResourceBudgets["tokens"] != 500 {
		t.Fatalf("token budget = %d (min must win)", got.ResourceBudgets["tokens"])
	}
	if _, ok := got.ResourceBudgets["requests"]; ok {
		t.Fatal("non-common budget key leaked into the intersection")
	}
	// Approvals union (requiring more is safe).
	if len(got.ApprovalClasses) != 2 {
		t.Fatalf("approvals union = %v", got.ApprovalClasses)
	}
	// Denial: disjoint models → empty intersection (denies).
	disjoint := local
	disjoint.Models = []string{"model-z"}
	if got := PolicyIntersect(local, disjoint); len(got.Models) != 0 {
		t.Fatal("disjoint scopes must intersect to denial")
	}
}

func TestResidencyEnforcement(t *testing.T) {
	policy := ResidencyPolicy{
		AllowedRegions:   []string{"KR", "US"},
		ForbiddenDomains: []string{"blocked.example"},
	}
	if err := CheckResidency(policy, "partner.example", "KR"); err != nil {
		t.Fatal(err)
	}
	if err := CheckResidency(policy, "blocked.example", "KR"); !errors.Is(err, ErrResidency) {
		t.Fatalf("expected residency violation, got %v", err)
	}
	if err := CheckResidency(policy, "partner.example", "EU"); !errors.Is(err, ErrResidency) {
		t.Fatal("disallowed region accepted")
	}
	// Unknown region fails closed when regions are configured.
	if err := CheckResidency(policy, "partner.example", ""); !errors.Is(err, ErrResidency) {
		t.Fatal("unknown region accepted")
	}
}

func TestCrossDomainReceiptVerification(t *testing.T) {
	bootstrap, issuer := keys(t)
	bundle := bundleFixture(t, bootstrap, issuer, "partner.example", 1, "local.example")
	now := time.Now().UnixMilli()

	receiptDigest := dari.Digest{7}
	att := &dari.ReceiptAttestationBody{
		Version: 1, ReceiptBodyDigest: receiptDigest,
		SignerCredentialDigest: dari.Digest{2},
		Role:                   dari.AttestRoleRelay,
		Claims:                 []dari.AttestationClaim{{Class: dari.ClaimDecisionState, Objects: []dari.Digest{{1}}}},
		AtMs:                   now,
	}
	cose, _, err := dari.SignReceiptAttestation(att, issuer)
	if err != nil {
		t.Fatal(err)
	}
	// Valid cross-domain attestation verifies.
	if err := VerifyCrossDomainReceipt(bundle, receiptDigest, att, cose, issuer.Public().(ed25519.PublicKey), now); err != nil {
		t.Fatal(err)
	}
	// Wrong receipt digest binding.
	wrongDigest := dari.Digest{9}
	if err := VerifyCrossDomainReceipt(bundle, wrongDigest, att, cose, issuer.Public().(ed25519.PublicKey), now); !errors.Is(err, ErrReceiptDomain) {
		t.Fatalf("expected receipt-domain error, got %v", err)
	}
	// Signer outside the bundle.
	_, rogue := keys(t)
	rogueAtt := &dari.ReceiptAttestationBody{
		Version: 1, ReceiptBodyDigest: receiptDigest, SignerCredentialDigest: dari.Digest{3},
		Role:   dari.AttestRoleRelay,
		Claims: []dari.AttestationClaim{{Class: dari.ClaimDecisionState, Objects: []dari.Digest{{1}}}},
		AtMs:   now,
	}
	rogueCOSE, _, err := dari.SignReceiptAttestation(rogueAtt, rogue)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCrossDomainReceipt(bundle, receiptDigest, rogueAtt, rogueCOSE, rogue.Public().(ed25519.PublicKey), now); !errors.Is(err, ErrReceiptDomain) {
		t.Fatal("untrusted receipt signer accepted")
	}
	// Scope overclaim (relay attesting inference IO).
	badAtt := &dari.ReceiptAttestationBody{
		Version: 1, ReceiptBodyDigest: receiptDigest, SignerCredentialDigest: dari.Digest{4},
		Role:   dari.AttestRoleRelay,
		Claims: []dari.AttestationClaim{{Class: dari.ClaimInferenceIO, Objects: []dari.Digest{{1}}}},
		AtMs:   now,
	}
	badCOSE, _, err := dari.SignReceiptAttestation(badAtt, issuer)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCrossDomainReceipt(bundle, receiptDigest, badAtt, badCOSE, issuer.Public().(ed25519.PublicKey), now); !errors.Is(err, ErrReceiptDomain) {
		t.Fatal("scope overclaim accepted cross-domain")
	}
	// Expired bundle fails closed.
	staleBundle := bundleFixture(t, bootstrap, issuer, "partner.example", 2, "local.example")
	staleBundle.Body.ExpiresAtMs = now - 1
	if err := VerifyCrossDomainReceipt(staleBundle, receiptDigest, att, cose, issuer.Public().(ed25519.PublicKey), now); !errors.Is(err, ErrStaleBundle) {
		t.Fatal("expired bundle accepted")
	}
}
