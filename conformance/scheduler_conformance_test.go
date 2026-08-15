package conformance

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// Scheduler admission invariants (DARI scheduler §5) — black-box checks
// against the scheduler's public API.

func schedFixture(t *testing.T) (scheduler.Trust, *dari.PeerCredential, ed25519.PublicKey, ed25519.PrivateKey, *scheduler.SignedConfig, scheduler.WorkerCard) {
	t.Helper()
	ca, err := dari.NewPeerCredentialIssuer("conformance-ca")
	if err != nil {
		t.Fatal(err)
	}
	subjectPub, subjectPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.Issue(dari.IssueRequest{
		SubjectPeerID:           "wkr-conformance-001",
		Organization:            "test-org",
		Profile:                 dari.ProfileInference,
		PublicKey:               subjectPub,
		Validity:                time.Hour,
		RevocationAuthority:     "conformance-ca",
		AllowedProtocolVersions: []uint8{1},
		BuildChannel:            "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configPub := ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey))
	envelope, err := scheduler.SignConfig(configPriv, scheduler.PIAWorkerConfig{
		WorkerID:      "wkr-conformance-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     "http://127.0.0.1:8033",
		AllowedModels: []string{"qwen3.6-27b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trust := scheduler.Trust{
		Issuers:      map[string]ed25519.PublicKey{ca.IssuerID: ca.PublicKey},
		ConfigPubKey: configPub,
		Now:          time.Now,
	}
	card := scheduler.WorkerCard{
		CardVersion:       1,
		WorkerID:          "wkr-conformance-001",
		EnrollmentID:      cred.Serial,
		EngineKind:        "vllm",
		EngineURL:         "http://127.0.0.1:8033",
		ReachabilityMode:  "localhost",
		MeasuredGrade:     "localhost",
		ModelName:         "qwen3.6-27b",
		AcceleratorFamily: "nvidia",
		GPUSKU:            "H100",
		GPUCount:          1,
		Status:            "active",
		TP:                1,
		DP:                1,
	}
	if err := card.Sign(subjectPriv); err != nil {
		t.Fatal(err)
	}
	return trust, cred, subjectPub, subjectPriv, envelope, card
}

func conformanceScheduler(t *testing.T, trust scheduler.Trust) *scheduler.Scheduler {
	t.Helper()
	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler.NewScheduler(trust, nil, 30*time.Second, 60*time.Second, evidenceKey)
}

// Invariant S1-1: No worker registration without a valid PPC chain.
func TestSchedulerInvariant1_PPCRequired(t *testing.T) {
	trust, _, _, _, envelope, card := schedFixture(t)
	s := conformanceScheduler(t, trust)

	res := s.Admit(scheduler.AdmissionRequest{Card: card, Config: envelope, Now: time.Now()})
	if res.Outcome == scheduler.OutcomeAdmitted {
		t.Fatal("INVARIANT VIOLATION: admitted without a PPC")
	}
}

// Invariant S1-2: A valid PPC alone admits nothing (signed config required).
func TestSchedulerInvariant2_ConfigRequired(t *testing.T) {
	trust, cred, _, _, _, card := schedFixture(t)
	s := conformanceScheduler(t, trust)

	res := s.Admit(scheduler.AdmissionRequest{Card: card, PPC: cred, Now: time.Now()})
	if res.Outcome == scheduler.OutcomeAdmitted {
		t.Fatal("INVARIANT VIOLATION: PPC alone admitted a worker")
	}
}

// Invariant S1-3: The config allow-list binds the served model.
func TestSchedulerInvariant3_ModelAllowList(t *testing.T) {
	trust, cred, _, subjectPriv, envelope, card := schedFixture(t)
	card.ModelName = "not-on-the-allow-list"
	if err := card.Sign(subjectPriv); err != nil {
		t.Fatal(err)
	}
	s := conformanceScheduler(t, trust)
	res := s.Admit(scheduler.AdmissionRequest{Card: card, PPC: cred, Config: envelope, Now: time.Now()})
	if res.Outcome == scheduler.OutcomeAdmitted {
		t.Fatal("INVARIANT VIOLATION: disallowed model admitted")
	}
}

// Invariant S1-4: Reachability mismatch → quarantine, never silent admit.
func TestSchedulerInvariant4_ReachabilityQuarantine(t *testing.T) {
	trust, cred, _, _, envelope, card := schedFixture(t)
	s := conformanceScheduler(t, trust)

	mismatched := card
	mismatched.MeasuredGrade = "exposed"
	res := s.Admit(scheduler.AdmissionRequest{Card: mismatched, PPC: cred, Config: envelope, Now: time.Now()})
	if res.Outcome != scheduler.OutcomeQuarantined && res.Outcome != scheduler.OutcomeDenied {
		t.Fatalf("INVARIANT VIOLATION: reachability mismatch produced %s", res.Outcome)
	}
}

// Invariant S1-5: Lease expiry evicts and emits evidence.
func TestSchedulerInvariant5_LeaseEviction(t *testing.T) {
	trust, cred, pub, _, envelope, card := schedFixture(t)
	s := conformanceScheduler(t, trust)

	res := s.Admit(scheduler.AdmissionRequest{Card: card, PPC: cred, Config: envelope, Now: time.Now()})
	if res.Outcome != scheduler.OutcomeAdmitted {
		t.Fatalf("setup admission failed: %s", res.Outcome)
	}
	now := time.Now()
	if _, err := s.Registry.Register(card, pub, now); err != nil {
		t.Fatal(err)
	}
	evicted := s.Sweep(now.Add(100 * time.Second))
	if len(evicted) != 1 {
		t.Fatalf("INVARIANT VIOLATION: expired worker not evicted (%v)", evicted)
	}
	events := s.Evidence.Events()
	if len(events) == 0 || events[len(events)-1].EventType != scheduler.EventWorkerEvict {
		t.Fatalf("INVARIANT VIOLATION: eviction not evidenced (%+v)", events)
	}
}
