package scheduler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

type workerFixture struct {
	cred        *dari.PeerCredential
	subjectPub  ed25519.PublicKey
	subjectPriv ed25519.PrivateKey
	card        WorkerCard
	config      *SignedConfig
	trust       Trust
}

func newWorkerFixture(t *testing.T) workerFixture {
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
	card := testCard()
	if err := card.Sign(subjectPriv); err != nil {
		t.Fatal(err)
	}
	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.WorkerID = card.WorkerID
	signed, err := SignConfig(configPriv, cfg)
	if err != nil {
		t.Fatal(err)
	}
	trust := Trust{
		Issuers:      map[string]ed25519.PublicKey{"test-ca": ca.PublicKey},
		ConfigPubKey: ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey)),
		Now:          time.Now,
	}
	return workerFixture{
		cred:        cred,
		subjectPub:  subjectPub,
		subjectPriv: subjectPriv,
		card:        card,
		config:      signed,
		trust:       trust,
	}
}

func testEvidenceKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func clientTLS() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         dari.DARIProtocols(),
	}
}

func clientProof(t *testing.T, cred *dari.PeerCredential, subjectPriv ed25519.PrivateKey, hello *dari.HelloMessage, ack *dari.HelloAckMessage, challenge *dari.AuthChallengeMessage) *dari.AuthProofMessage {
	t.Helper()
	helloCBOR, err := dari.CanonicalHelloCBOR(hello)
	if err != nil {
		t.Fatal(err)
	}
	ackCBOR, err := dari.CanonicalAckCBOR(ack)
	if err != nil {
		t.Fatal(err)
	}
	digest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, cred.SignedCredential)
	transcript := dari.AuthContext(helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, []byte("tcp-exporter"), digest.Bytes())
	epoch := uint64(0)
	signingBytes := dari.PeerProofSigningBytes(transcript.Bytes(), challenge.ChallengeID, epoch)
	return &dari.AuthProofMessage{
		Credential:         cred.SignedCredential,
		Signature:          ed25519.Sign(subjectPriv, signingBytes),
		KeyAlgorithm:       dari.COSEAlgEdDSA,
		ChallengeID:        challenge.ChallengeID,
		RevocationEvidence: dari.EncodeRevocationEpoch(epoch),
	}
}

func dialWorker(t *testing.T, addr string, fx workerFixture) *dari.TransportConn {
	t.Helper()
	conn, err := dari.DialTCP(addr, clientTLS(), dari.DefaultTransportConfig())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	hello := &dari.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        dari.ProfileInference,
		TransportFeatures:  []string{"tcp-tls"},
		Extensions:         map[string]uint8{},
		CryptoProfiles:     []string{"DARI-BASE-1"},
		ClientNonce:        make([]byte, 32),
		ImplementationName: "pccp-pia",
	}
	if _, err := rand.Read(hello.ClientNonce); err != nil {
		t.Fatal(err)
	}
	ack, err := conn.Handshake(hello)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		t.Fatalf("auth challenge: %v", err)
	}
	if err := conn.AuthProof(clientProof(t, fx.cred, fx.subjectPriv, hello, ack, challenge)); err != nil {
		t.Fatalf("auth proof: %v", err)
	}
	authAck, err := conn.RecvRecord()
	if err != nil {
		t.Fatalf("auth ack: %v", err)
	}
	if authAck.Kind != dari.KindControl || dari.MessageType(authAck.MessageType) != dari.MsgAuthAck {
		t.Fatalf("expected AUTH_ACK, got kind=%s type=%d", authAck.Kind, authAck.MessageType)
	}
	return conn
}

func startTestListener(t *testing.T, fx workerFixture, policy PolicySource) (*Scheduler, string) {
	t.Helper()
	scheduler := NewScheduler(fx.trust, policy, 30*time.Second, 60*time.Second, testEvidenceKey(t))
	listener := NewDARIListener(scheduler, nil)
	ln, err := dari.ListenTCP("127.0.0.1:0", listener.tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go listener.ServeTCP(ln)
	t.Cleanup(func() { ln.Close() })
	return scheduler, addr
}

func registerWorker(t *testing.T, conn *dari.TransportConn, fx workerFixture, config *SignedConfig) RegisterAckPayload {
	t.Helper()
	payload, err := json.Marshal(RegisterPayload{Card: fx.card, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SendMessage(dari.MsgEndpointRegister, nil, payload, 0, 1); err != nil {
		t.Fatal(err)
	}
	record, err := conn.RecvRecord()
	if err != nil {
		t.Fatalf("recv ack: %v", err)
	}
	var ack RegisterAckPayload
	if err := json.Unmarshal(record.Payload, &ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	return ack
}

func TestListenerRegisterAdmitFlow(t *testing.T) {
	fx := newWorkerFixture(t)
	scheduler, addr := startTestListener(t, fx, nil)

	conn := dialWorker(t, addr, fx)
	defer conn.Close()

	ack := registerWorker(t, conn, fx, fx.config)
	if ack.Outcome != OutcomeAdmitted {
		t.Fatalf("outcome %s, want admitted (%s)", ack.Outcome, ack.Reason)
	}
	if ack.LeaseTTLSeconds != 30 {
		t.Fatalf("lease ttl %d, want 30", ack.LeaseTTLSeconds)
	}

	entry, ok := scheduler.Registry.Get(fx.card.WorkerID)
	if !ok {
		t.Fatal("worker missing from registry")
	}
	if entry.Card.EngineKind != "vllm" {
		t.Fatalf("stored card engine %q", entry.Card.EngineKind)
	}
	if got := scheduler.Evidence.Events(); len(got) != 1 || got[0].EventType != "worker.register" {
		t.Fatalf("evidence events %+v, want single worker.register", got)
	}
}

func TestListenerRegisterDeniesBadConfig(t *testing.T) {
	fx := newWorkerFixture(t)
	scheduler, addr := startTestListener(t, fx, nil)

	conn := dialWorker(t, addr, fx)
	defer conn.Close()

	_, roguePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.WorkerID = fx.card.WorkerID
	bad, err := SignConfig(roguePriv, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ack := registerWorker(t, conn, fx, bad)
	if ack.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", ack.Outcome)
	}
	if _, ok := scheduler.Registry.Get(fx.card.WorkerID); ok {
		t.Fatal("denied worker must not be registered")
	}
	events := scheduler.Evidence.Events()
	if len(events) != 1 || events[0].EventType != "worker.deny" {
		t.Fatalf("evidence events %+v, want single worker.deny", events)
	}
}

func TestListenerHeartbeatRenewsLease(t *testing.T) {
	fx := newWorkerFixture(t)
	scheduler, addr := startTestListener(t, fx, nil)

	conn := dialWorker(t, addr, fx)
	defer conn.Close()

	if ack := registerWorker(t, conn, fx, fx.config); ack.Outcome != OutcomeAdmitted {
		t.Fatalf("register outcome %s (%s)", ack.Outcome, ack.Reason)
	}
	before, ok := scheduler.Registry.Get(fx.card.WorkerID)
	if !ok {
		t.Fatal("worker missing before heartbeat")
	}

	fresh := fx.card
	fresh.Status = "active"
	fresh.LeaseExpiryUnixMs = time.Now().UnixMilli()
	if err := fresh.Sign(fx.subjectPriv); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(HeartbeatPayload{Card: fresh})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SendMessage(dari.MsgEndpointLease, nil, payload, 0, 2); err != nil {
		t.Fatal(err)
	}
	record, err := conn.RecvRecord()
	if err != nil {
		t.Fatalf("recv heartbeat ack: %v", err)
	}
	var ack RegisterAckPayload
	if err := json.Unmarshal(record.Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Outcome != OutcomeAdmitted {
		t.Fatalf("heartbeat outcome %s (%s)", ack.Outcome, ack.Reason)
	}

	after, _ := scheduler.Registry.Get(fx.card.WorkerID)
	if !after.LeasedUntil.After(before.LeasedUntil) {
		t.Fatalf("lease not renewed: before %v after %v", before.LeasedUntil, after.LeasedUntil)
	}
}

func TestListenerRejectsInvalidProofOfPossession(t *testing.T) {
	fx := newWorkerFixture(t)
	scheduler, addr := startTestListener(t, fx, nil)

	conn, err := dari.DialTCP(addr, clientTLS(), dari.DefaultTransportConfig())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hello := &dari.HelloMessage{
		CoreVersions:       []uint8{1},
		PeerProfile:        dari.ProfileInference,
		TransportFeatures:  []string{"tcp-tls"},
		CryptoProfiles:     []string{"DARI-BASE-1"},
		ClientNonce:        make([]byte, 32),
		ImplementationName: "pccp-pia",
	}
	rand.Read(hello.ClientNonce)
	ack, err := conn.Handshake(hello)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		t.Fatalf("auth challenge: %v", err)
	}
	proof := clientProof(t, fx.cred, fx.subjectPriv, hello, ack, challenge)
	proof.Signature = []byte("forged")

	if err := conn.AuthProof(proof); err != nil {
		t.Fatalf("auth proof send: %v", err)
	}
	conn.SendMessage(dari.MsgEndpointRegister, nil, []byte(`{}`), 0, 1)

	// The listener must reject the forged proof and close the connection
	// without admitting the worker.
	closed := make(chan struct{})
	go func() {
		for {
			if _, err := conn.RecvRecord(); err != nil {
				close(closed)
				return
			}
		}
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not close connection after forged proof")
	}
	if _, ok := scheduler.Registry.Get(fx.card.WorkerID); ok {
		t.Fatal("forged proof must never admit a worker")
	}
}
