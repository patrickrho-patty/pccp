package compliance

import "testing"

func TestGetCertificationPack(t *testing.T) {
	svc := New(nil)

	// Test CSAP
	pack, err := svc.GetCertificationPack(CertCSAP)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.ControlMappings) == 0 {
		t.Fatal("expected control mappings")
	}
	if pack.NameKo != "클라우드보안인증" {
		t.Fatalf("expected Korean name, got %s", pack.NameKo)
	}

	// Test ISMS-P
	pack, _ = svc.GetCertificationPack(CertISMSP)
	if pack.NameKo != "정보보호관리체계 인증" {
		t.Fatalf("expected Korean name")
	}

	// Test Privacy
	pack, _ = svc.GetCertificationPack(CertPrivacy)
	if len(pack.ControlMappings) < 3 {
		t.Fatal("expected privacy controls")
	}
}

func TestAssessCompliance(t *testing.T) {
	svc := New(nil)

	assessment, err := svc.AssessCompliance("org-1", CertCSAP)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.OverallStatus == "" {
		t.Fatal("expected overall status")
	}
	if len(assessment.ControlResults) == 0 {
		t.Fatal("expected control results")
	}
}

func TestListCertifications(t *testing.T) {
	svc := New(nil)
	certs := svc.ListCertifications()
	if len(certs) != 5 {
		t.Fatalf("expected 5 certifications, got %d", len(certs))
	}
}
