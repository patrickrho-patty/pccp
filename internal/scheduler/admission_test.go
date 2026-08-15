package scheduler

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

type staticPolicy map[string]string // tenantID → min reachability grade

func (p staticPolicy) MinReachability(tenantID string) (string, bool) {
	g, ok := p[tenantID]
	return g, ok
}

func testTrust(t *testing.T) (Trust, *dari.PeerCredentialIssuer, ed25519.PrivateKey) {
	t.Helper()
	ca, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := Trust{
		Issuers:      map[string]ed25519.PublicKey{"test-ca": ca.PublicKey},
		ConfigPubKey: ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey)),
		Now:          time.Now,
	}
	return trust, ca, configPriv
}

func testPPC(t *testing.T, ca *dari.PeerCredentialIssuer, subjectPub ed25519.PublicKey) *dari.PeerCredential {
	t.Helper()
	cred, err := ca.Issue(dari.IssueRequest{
		SubjectPeerID:           "wkr-test-001",
		Organization:            "test-org",
		Profile:                 dari.ProfileInference,
		PublicKey:               subjectPub,
		Validity:                time.Hour,
		RevocationAuthority:     "test-ca",
		AllowedProtocolVersions: []uint8{1},
		BuildChannel:            "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cred
}

func testSignedConfig(t *testing.T, configPriv ed25519.PrivateKey, cfg PIAWorkerConfig) *SignedConfig {
	t.Helper()
	signed, err := SignConfig(configPriv, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func testConfig() PIAWorkerConfig {
	return PIAWorkerConfig{
		WorkerID:      "wkr-test-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     "http://127.0.0.1:8033/v1",
		AllowedModels: []string{"qwen3.6-27b"},
	}
}

func testAdmission(t *testing.T, trust Trust, revoked *RevocationStore, policy PolicySource) *Admission {
	t.Helper()
	return NewAdmission(trust, revoked, policy)
}

func TestAdmissionDeniesMissingPPC(t *testing.T) {
	trust, _, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, _, priv := signedTestCard(t)
	card.Sign(priv)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesUnsignedPPC(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)
	cred.SignedCredential = nil

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesForeignIssuer(t *testing.T) {
	trust, _, configPriv := testTrust(t)
	foreignCA, err := dari.NewPeerCredentialIssuer("foreign-ca")
	if err != nil {
		t.Fatal(err)
	}
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, foreignCA, pub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesExpiredPPC(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now().Add(2 * time.Hour),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesRevokedSerial(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	revoked := NewRevocationStore()
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)
	revoked.RevokeSerial(cred.Serial)

	adm := testAdmission(t, trust, revoked, staticPolicy{})
	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesCardSignedByOtherKey(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})

	card := testCard()
	_, roguePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card.Sign(roguePriv)

	subjectPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cred := testPPC(t, ca, subjectPub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesBadConfigSignature(t *testing.T) {
	trust, ca, _ := testTrust(t)
	_, roguePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)
	badConfig := testSignedConfig(t, roguePriv, testConfig())

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: badConfig,
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesUnauthorizedModel(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)
	cfg := testConfig()
	cfg.AllowedModels = []string{"some-other-model"}
	cfg.WorkerID = card.WorkerID

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, cfg),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionDeniesMismatchedBackendMode(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})
	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)
	cfg := testConfig()
	cfg.BackendMode = "mtls" // card says localhost
	cfg.WorkerID = card.WorkerID

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, cfg),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}

func TestAdmissionQuarantinesBelowMinReachability(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	policy := staticPolicy{"tenant-a": "localhost"}
	adm := testAdmission(t, trust, NewRevocationStore(), policy)

	card, pub, priv := signedTestCard(t)
	card.MeasuredGrade = "private"
	card.ReachabilityMode = "private"
	card.Sign(priv)
	cred := testPPC(t, ca, pub)
	cfg := testConfig()
	cfg.BackendMode = "private"

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, cfg),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeQuarantined {
		t.Fatalf("outcome %s, want quarantined (%s)", res.Outcome, res.Reason)
	}
}

func TestAdmissionQuarantinesMeasuredMismatch(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})

	card, pub, priv := signedTestCard(t)
	card.MeasuredGrade = "exposed" // config claims localhost
	card.Sign(priv)
	cred := testPPC(t, ca, pub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeQuarantined {
		t.Fatalf("outcome %s, want quarantined (%s)", res.Outcome, res.Reason)
	}
}

func TestAdmissionAdmitsValidWorker(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{"tenant-a": "localhost"})

	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeAdmitted {
		t.Fatalf("outcome %s, want admitted (%s)", res.Outcome, res.Reason)
	}
	if res.TenantID != "tenant-a" {
		t.Fatalf("tenant %q, want tenant-a", res.TenantID)
	}
}

func TestAdmissionAllowsWorkerWithoutTenantPolicy(t *testing.T) {
	trust, ca, configPriv := testTrust(t)
	adm := testAdmission(t, trust, NewRevocationStore(), staticPolicy{})

	card, pub, _ := signedTestCard(t)
	cred := testPPC(t, ca, pub)

	res := adm.Admit(AdmissionRequest{
		Card:   card,
		PPC:    cred,
		Config: testSignedConfig(t, configPriv, testConfig()),
		Now:    time.Now(),
	})
	if res.Outcome != OutcomeAdmitted {
		t.Fatalf("outcome %s, want admitted (%s)", res.Outcome, res.Reason)
	}
}
