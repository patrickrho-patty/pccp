package pia

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// startScheduler spins a real scheduler DARI listener on an ephemeral port
// and returns it plus the address.
func startScheduler(t *testing.T, ca *dari.PeerCredentialIssuer, configPub ed25519.PublicKey, policy scheduler.PolicySource) (*scheduler.Scheduler, string) {
	t.Helper()
	trust := scheduler.Trust{
		Issuers:      map[string]ed25519.PublicKey{ca.IssuerID: ca.PublicKey},
		ConfigPubKey: configPub,
		Now:          time.Now,
	}
	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc := scheduler.NewScheduler(trust, policy, 2*time.Second, 4*time.Second, evidenceKey)
	listener := scheduler.NewDARIListener(svc, nil)
	ln, err := dari.ListenTCP("127.0.0.1:0", listener.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	go listener.ServeTCP(ln)
	t.Cleanup(func() { ln.Close() })
	return svc, ln.Addr().String()
}

func integrationFixture(t *testing.T) (WorkerAgentConfig, *scheduler.Scheduler) {
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
		SubjectPeerID:           "wkr-integration-001",
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
	engine := fakeEngine(t,
		`{"object":"list","data":[{"id":"Qwen3.6-27B-FP8"}]}`,
		"vllm:num_requests_running 3\n",
	)
	t.Cleanup(engine.Close)

	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configPub := ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey))
	envelope, err := scheduler.SignConfig(configPriv, scheduler.PIAWorkerConfig{
		WorkerID:      "wkr-integration-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     engine.URL,
		AllowedModels: []string{"Qwen3.6-27B-FP8"},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc, addr := startScheduler(t, ca, configPub, nil)
	cfg := WorkerAgentConfig{
		SchedulerAddr:  addr,
		Credential:     cred,
		SubjectPriv:    subjectPriv,
		EngineURL:      engine.URL,
		EngineKind:     "vllm",
		SignedConfig:   envelope,
		Heartbeat:      200 * time.Millisecond,
		ReconnectDelay: 100 * time.Millisecond,
	}
	return cfg, svc
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestWorkerAgentRegistersAndRenewsLease(t *testing.T) {
	cfg, svc := integrationFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go agent.Run(ctx)

	waitFor(t, 5*time.Second, func() bool {
		entry, ok := svc.Registry.Get("wkr-integration-001")
		return ok && entry.Card.Status == "active" && entry.Card.ModelName == "Qwen3.6-27B-FP8"
	})

	before, _ := svc.Registry.Get("wkr-integration-001")
	if !before.LeasedUntil.After(time.Now()) {
		t.Fatalf("lease not in the future: %v", before.LeasedUntil)
	}

	// Wait a few heartbeat cycles; the lease must keep renewing.
	waitFor(t, 5*time.Second, func() bool {
		after, ok := svc.Registry.Get("wkr-integration-001")
		return ok && after.LastSeen.After(before.LastSeen)
	})

	if got := agent.LastOutcome; got != scheduler.OutcomeAdmitted {
		t.Fatalf("last outcome %s (%s)", got, agent.LastReason)
	}
}

func TestWorkerAgentConnectsToLateScheduler(t *testing.T) {
	ca, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	engine := fakeEngine(t,
		`{"object":"list","data":[{"id":"Qwen3.6-27B-FP8"}]}`,
		"",
	)
	t.Cleanup(engine.Close)

	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	configPub := ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey))
	envelope, err := scheduler.SignConfig(configPriv, scheduler.PIAWorkerConfig{
		WorkerID:      "wkr-late-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     engine.URL,
		AllowedModels: []string{"Qwen3.6-27B-FP8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	subjectPub, subjectPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := ca.Issue(dari.IssueRequest{
		SubjectPeerID:           "wkr-late-001",
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

	// Reserve an ephemeral port, close it, and hand it to the agent — the
	// agent must retry until the scheduler appears.
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reservation.Addr().String()
	reservation.Close()

	cfg := WorkerAgentConfig{
		SchedulerAddr:  addr,
		Credential:     cred,
		SubjectPriv:    subjectPriv,
		EngineURL:      engine.URL,
		EngineKind:     "vllm",
		SignedConfig:   envelope,
		Heartbeat:      200 * time.Millisecond,
		ReconnectDelay: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go agent.Run(ctx)

	// The agent must survive the down period.
	time.Sleep(200 * time.Millisecond)

	// Now bring the scheduler up on the same address.
	trust := scheduler.Trust{
		Issuers:      map[string]ed25519.PublicKey{ca.IssuerID: ca.PublicKey},
		ConfigPubKey: configPub,
		Now:          time.Now,
	}
	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc := scheduler.NewScheduler(trust, nil, 2*time.Second, 4*time.Second, evidenceKey)
	listener := scheduler.NewDARIListener(svc, nil)
	ln, err := dari.ListenTCP(addr, listener.TLSConfig())
	if err != nil {
		t.Fatalf("listen on reserved addr: %v", err)
	}
	defer ln.Close()
	go listener.ServeTCP(ln)

	waitFor(t, 5*time.Second, func() bool {
		_, ok := svc.Registry.Get("wkr-late-001")
		return ok
	})
}
