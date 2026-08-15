package attestation

import "testing"

func TestCollectEvidence(t *testing.T) {
	svc := New()
	evidence, err := svc.CollectEvidence(CollectRequest{
		EndpointID:    "ep-1",
		NodeIdentity:  "node-1",
		GPUIDs:        []string{"gpu-0"},
		RequiredTypes: []AttestationType{AttestPIABinary, AttestSecureBoot, AttestTPM},
		RequiredLevel: Level2Host,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 3 {
		t.Fatalf("expected 3 evidence items, got %d", len(evidence))
	}
}

func TestVerifyEvidence(t *testing.T) {
	svc := New()
	evidence, _ := svc.CollectEvidence(CollectRequest{
		RequiredTypes: []AttestationType{AttestPIABinary},
	})

	// Create matching reference values
	ref := make(map[string]string)
	for k, v := range evidence[0].Measurements {
		ref[k] = v
	}

	err := svc.VerifyEvidence(&evidence[0], ref)
	if err != nil {
		t.Fatalf("expected verification to pass: %v", err)
	}
	if !evidence[0].Verified {
		t.Fatal("expected verified=true")
	}
}

func TestVerifyEvidenceMismatch(t *testing.T) {
	svc := New()
	evidence, _ := svc.CollectEvidence(CollectRequest{
		RequiredTypes: []AttestationType{AttestPIABinary},
	})

	ref := map[string]string{"pia_binary": "sha256:wrong"}
	err := svc.VerifyEvidence(&evidence[0], ref)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestAssuranceLevelRequirements(t *testing.T) {
	l1 := AssuranceLevelRequirements(Level1Software)
	if len(l1) < 5 {
		t.Fatal("expected L1 requirements")
	}

	l2 := AssuranceLevelRequirements(Level2Host)
	if len(l2) < 4 {
		t.Fatal("expected L2 requirements")
	}

	l3 := AssuranceLevelRequirements(Level3Confidential)
	if len(l3) < 4 {
		t.Fatal("expected L3 requirements")
	}

	// Check Korean text
	hasKorean := false
	for _, req := range l1 {
		for _, r := range req {
			if r >= 0xAC00 && r <= 0xD7A3 {
				hasKorean = true
				break
			}
		}
	}
	if !hasKorean {
		t.Fatal("expected Korean text in requirements")
	}
}

func TestModelKeyRelease(t *testing.T) {
	svc := New()

	// All evidence verified
	evidence, _ := svc.CollectEvidence(CollectRequest{
		RequiredTypes: []AttestationType{AttestPIABinary},
	})
	ref := make(map[string]string)
	for k, v := range evidence[0].Measurements {
		ref[k] = v
	}
	svc.VerifyEvidence(&evidence[0], ref)

	result := svc.EvaluateKeyRelease(ModelKeyReleaseRequest{
		EndpointID:          "ep-1",
		OrganizationID:      "org-1",
		ModelPackageID:      "pmp-1",
		AssuranceLevel:      Level1Software,
		AttestationEvidence: evidence,
	})
	if !result.Granted {
		t.Fatalf("expected key release granted: %s", result.Reason)
	}

	// Unverified evidence should fail
	evidence[0].Verified = false
	result = svc.EvaluateKeyRelease(ModelKeyReleaseRequest{
		AttestationEvidence: evidence,
	})
	if result.Granted {
		t.Fatal("expected key release denied for unverified evidence")
	}
}
