package pia

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

func agentFixture(t *testing.T) (WorkerAgentConfig, ed25519.PublicKey, ed25519.PublicKey) {
	t.Helper()
	ca, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	subjectPub, subjectPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.Issue(dari.IssueRequest{
		SubjectPeerID:           "wkr-agent-001",
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
	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configPub := ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey))
	envelope, err := scheduler.SignConfig(configPriv, scheduler.PIAWorkerConfig{
		WorkerID:      "wkr-agent-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     "http://127.0.0.1:8033",
		AllowedModels: []string{"qwen3.6-27b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := WorkerAgentConfig{
		SchedulerAddr:  "127.0.0.1:8445",
		Credential:     cred,
		SubjectPriv:    subjectPriv,
		EngineURL:      "http://127.0.0.1:8033",
		EngineKind:     "vllm",
		SignedConfig:   envelope,
		Heartbeat:      100 * time.Millisecond,
		ReconnectDelay: 100 * time.Millisecond,
	}
	return cfg, subjectPub, configPub
}

func fakeEngine(t *testing.T, models, metrics string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(models))
		case "/metrics":
			w.Write([]byte(metrics))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestBuildCardFromIntrospection(t *testing.T) {
	engine := fakeEngine(t,
		`{"object":"list","data":[{"id":"Qwen3.6-27B-FP8"}]}`,
		"vllm:num_requests_running 7\n",
	)
	defer engine.Close()

	cfg, subjectPub, _ := agentFixture(t)
	cfg.EngineURL = engine.URL
	cfg.SignedConfig.Config.EngineURL = engine.URL
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}

	card, err := agent.buildCard()
	if err != nil {
		t.Fatalf("build card: %v", err)
	}
	if card.WorkerID != "wkr-agent-001" {
		t.Fatalf("worker id %q", card.WorkerID)
	}
	if card.EnrollmentID != cfg.Credential.Serial {
		t.Fatalf("enrollment %q, want %q", card.EnrollmentID, cfg.Credential.Serial)
	}
	digest := sha256.Sum256(cfg.Credential.SignedCredential)
	if card.PPCFingerprint != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("ppc fingerprint %q", card.PPCFingerprint)
	}
	if card.ModelName != "Qwen3.6-27B-FP8" {
		t.Fatalf("model %q", card.ModelName)
	}
	if card.MaxConcurrentSeqs != 7 {
		t.Fatalf("seqs %d", card.MaxConcurrentSeqs)
	}
	if card.MeasuredGrade != "localhost" {
		t.Fatalf("measured grade %q, want localhost", card.MeasuredGrade)
	}
	if card.ReachabilityMode != "localhost" {
		t.Fatalf("mode %q", card.ReachabilityMode)
	}
	if card.Status != "active" {
		t.Fatalf("status %q, want active", card.Status)
	}
	if err := card.Verify(subjectPub); err != nil {
		t.Fatalf("card signature: %v", err)
	}
}

func TestBuildCardDegradedWhenEngineDown(t *testing.T) {
	engine := fakeEngine(t, `{}`, "")
	engineURL := engine.URL
	engine.Close()

	cfg, subjectPub, _ := agentFixture(t)
	cfg.EngineURL = engineURL
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}

	card, err := agent.buildCard()
	if err != nil {
		t.Fatalf("build card: %v", err)
	}
	if card.Status != "degraded" {
		t.Fatalf("status %q, want degraded", card.Status)
	}
	if err := card.Verify(subjectPub); err != nil {
		t.Fatalf("degraded card must still be signed: %v", err)
	}
}

func TestLoadWorkerCredentialRoundtrip(t *testing.T) {
	cfg, _, _ := agentFixture(t)
	path := filepath.Join(t.TempDir(), "cred.hex")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(cfg.Credential.SignedCredential)), 0o600); err != nil {
		t.Fatal(err)
	}

	cred, err := LoadWorkerCredential(path)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if cred.SubjectPeerID != "wkr-agent-001" || cred.Serial != cfg.Credential.Serial {
		t.Fatalf("credential mismatch: %+v", cred)
	}
}

func TestLoadSignedConfigRejectsTamperedWhenRequired(t *testing.T) {
	cfg, _, configPub := agentFixture(t)
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg.SignedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadSignedConfig(path, configPub, true); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}

	// Semantic tampering: keep valid JSON, change an authorized model.
	tamperedEnvelope := *cfg.SignedConfig
	tamperedEnvelope.Config.AllowedModels = []string{"some-other-model"}
	tampered, _ := json.Marshal(tamperedEnvelope)
	os.WriteFile(path, tampered, 0o600)
	if _, err := LoadSignedConfig(path, configPub, true); err == nil {
		t.Fatal("tampered envelope must be rejected")
	}

	// Dev mode (signature not required) still loads the envelope.
	if _, err := LoadSignedConfig(path, configPub, false); err != nil {
		t.Fatalf("dev mode should tolerate tamper: %v", err)
	}
}

func TestLoadSignedConfigMissingFileDevOnly(t *testing.T) {
	_, _, configPub := agentFixture(t)
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := LoadSignedConfig(missing, configPub, true); err == nil {
		t.Fatal("production must reject missing envelope")
	}
	if _, err := LoadSignedConfig(missing, configPub, false); err != nil {
		t.Fatalf("dev mode must allow missing envelope: %v", err)
	}
}
