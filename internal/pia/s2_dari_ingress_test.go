package pia

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// TestDARIIngressGovernedRequest verifies the §13.1 path: an AI_OPEN
// record enters the scheduler over DARI, queues, late-binds to a worker,
// and completes with AI_COMPLETE — one transport end-to-end.
func TestDARIIngressGovernedRequest(t *testing.T) {
	cfg, svc := integrationFixture(t)
	t.Setenv("PCCP_PIA_DARI_ADDR", "")

	// PIA DARI listener (the worker's dispatch target). Direct mode
	// skips CP lease issuance (no CP in this test).
	t.Setenv("PCCP_PIA_DIRECT", "1")
	piaService, err := New(nil, Config{PeerID: "pia-s2", ServingType: "vllm", ServingURL: fakeChatEngineURL(t)})
	if err != nil {
		t.Fatal(err)
	}
	piaListener := NewDARIListener(piaService)
	probe, err := dari.ListenTCP("127.0.0.1:0", piaListener.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	piaAddr := probe.Addr().String()
	probe.Close()
	go piaListener.ListenTCP(context.Background(), piaAddr)
	t.Setenv("PCCP_PIA_DARI_ADDR", piaAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent, err := NewWorkerAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go agent.Run(ctx)
	go svc.Serving.Start(ctx)

	waitFor(t, 5*time.Second, func() bool {
		entry, ok := svc.Registry.Get("wkr-integration-001")
		return ok && entry.Card.Servable()
	})

	// Dial the scheduler's DARI listener as a governed client and send
	// AI_OPEN. Need the scheduler listener's TLS config.
	svcDARI := schedulerDARIAddr(t, svc)
	conn, err := dari.DialTCP(svcDARI, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: dari.DARIProtocols()}, dari.DefaultTransportConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	hello := &dari.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        dari.ProfileRelay,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{"dari.ai/1": 1},
		CryptoProfiles:     []string{"DARI-BASE-1"},
		ClientNonce:        make([]byte, 32),
		ImplementationName: "s2-test-client",
	}
	ack, err := conn.Handshake(hello)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		t.Fatalf("auth challenge: %v", err)
	}
	// The test client re-authenticates with the enrolled worker's PPC.
	proof := &dari.AuthProofMessage{
		Credential:   cfg.Credential.SignedCredential,
		ChallengeID:  challenge.ChallengeID,
		KeyAlgorithm: dari.COSEAlgEdDSA,
	}
	helloCBOR, _ := dari.CanonicalHelloCBOR(hello)
	ackCBOR, _ := dari.CanonicalAckCBOR(ack)
	digest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, proof.Credential)
	transcript := dari.AuthContext(helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, []byte("tcp-exporter"), digest.Bytes())
	epoch := dari.DecodeRevocationEpochOrZero(nil)
	signingBytes := dari.PeerProofSigningBytes(transcript.Bytes(), challenge.ChallengeID, epoch)
	proof.Signature = ed25519.Sign(cfg.SubjectPriv, signingBytes)
	if err := conn.AuthProof(proof); err != nil {
		t.Fatalf("auth proof: %v", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"model":      "Qwen3.6-27B-FP8",
		"messages":   []map[string]string{{"role": "user", "content": "안녕하세요"}},
		"max_tokens": 20,
		"tenant":     "tenant-a",
		"class":      "interactive-paid",
	})
	if err := conn.SendMessage(dari.MsgAIOpen, nil, payload, 1, 1); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		record, err := conn.RecvRecord()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if dari.MessageType(record.MessageType) == dari.MsgAIComplete {
			var res scheduler.InferenceResult
			if err := json.Unmarshal(record.Payload, &res); err != nil {
				t.Fatalf("decode completion: %v (%s)", err, record.Payload)
			}
			if res.Err != "" || res.Cancelled {
				t.Fatalf("completion = %+v", res)
			}
			return
		}
	}
	t.Fatal("no AI_COMPLETE within deadline")
}

func fakeChatEngineURL(t *testing.T) string {
	t.Helper()
	eng := fakeChatEngine(t)
	t.Cleanup(eng.Close)
	return eng.URL
}

// schedulerDARIAddr spins the scheduler's DARI listener for the test
// (integrationFixture built the service without one).
func schedulerDARIAddr(t *testing.T, svc *scheduler.Scheduler) string {
	t.Helper()
	listener := scheduler.NewDARIListener(svc, nil)
	// Reserve a port, then hand it to the serving loop.
	probe, err := dari.ListenTCP("127.0.0.1:0", listener.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()
	go func() {
		_ = listener.Listen(addr)
	}()
	// Give the accept loop a moment to bind.
	time.Sleep(50 * time.Millisecond)
	return addr
}

// Ensure the test compiles even if helpers change: unused-import guards.
var _ = rand.Reader
var _ = time.Second
