package pia

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// WorkerAgentConfig holds the worker-agent runtime configuration.
type WorkerAgentConfig struct {
	SchedulerAddr  string
	Credential     *dari.PeerCredential
	SubjectPriv    ed25519.PrivateKey
	EngineURL      string
	EngineKind     string
	SignedConfig   *scheduler.SignedConfig
	NodeID         string
	Region         string
	Zone           string
	Heartbeat      time.Duration
	ReconnectDelay time.Duration
	// KVJournal (optional) is the worker's KV event broker (spec §13.11):
	// records append locally and are published to the scheduler, which
	// dedups by (worker, seq).
	KVJournal *KVJournal
}

// WorkerAgent is the PIA worker identity (DARI scheduler §4): it connects to
// the scheduler over DARI, presents its PPC, registers its signed capability
// card, and renews its lease with fresh cards on every heartbeat.
type WorkerAgent struct {
	cfg WorkerAgentConfig

	mu          sync.Mutex
	conn        *dari.TransportConn
	LastOutcome scheduler.Outcome
	LastReason  string
}

// NewWorkerAgent validates the configuration and builds the agent.
func NewWorkerAgent(cfg WorkerAgentConfig) (*WorkerAgent, error) {
	if cfg.SchedulerAddr == "" {
		return nil, fmt.Errorf("pia: worker agent requires a scheduler address")
	}
	if cfg.Credential == nil || len(cfg.Credential.SignedCredential) == 0 {
		return nil, fmt.Errorf("pia: worker agent requires a signed PPC")
	}
	if len(cfg.SubjectPriv) == 0 {
		return nil, fmt.Errorf("pia: worker agent requires the subject key")
	}
	if cfg.SignedConfig == nil {
		return nil, fmt.Errorf("pia: worker agent requires a signed worker config")
	}
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = 10 * time.Second
	}
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}
	return &WorkerAgent{cfg: cfg}, nil
}

// Run is the blocking connect → register → heartbeat loop. It reconnects on
// failure and exits when the context is cancelled.
func (a *WorkerAgent) Run(ctx context.Context) {
	for {
		if err := a.runConnection(ctx); err != nil {
			log.Printf("pia worker: connection ended: %v", err)
		}
		select {
		case <-ctx.Done():
			a.Close()
			return
		case <-time.After(a.cfg.ReconnectDelay):
		}
	}
}

// runConnection drives one scheduler connection to completion.
func (a *WorkerAgent) runConnection(ctx context.Context) error {
	conn, err := a.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	ack, err := a.registerOnce(conn)
	if err != nil {
		return err
	}
	switch ack.Outcome {
	case scheduler.OutcomeDenied:
		a.setOutcome(ack.Outcome, ack.Reason)
		return fmt.Errorf("registration denied: %s", ack.Reason)
	case scheduler.OutcomeQuarantined:
		log.Printf("pia worker: registered QUARANTINED: %s", ack.Reason)
	}
	log.Printf("pia worker: registered with scheduler (lease %ds)", ack.LeaseTTLSeconds)

	ticker := time.NewTicker(a.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			hb, err := a.heartbeatOnce(conn)
			if err != nil {
				return err
			}
			if hb.Outcome == scheduler.OutcomeDenied {
				a.setOutcome(hb.Outcome, hb.Reason)
				return fmt.Errorf("heartbeat denied: %s", hb.Reason)
			}
			a.publishKVJournal(conn)
		}
	}
}

// connect performs dial + DARI handshake + auth proof.
func (a *WorkerAgent) connect(ctx context.Context) (*dari.TransportConn, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // dev default, mirrors the relay paper client
		MinVersion:         tls.VersionTLS13,
		NextProtos:         dari.DARIProtocols(),
	}
	conn, err := dari.DialTCP(a.cfg.SchedulerAddr, tlsConfig, dari.DefaultTransportConfig())
	if err != nil {
		return nil, fmt.Errorf("dial scheduler: %w", err)
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
		conn.Close()
		return nil, err
	}
	ack, err := conn.Handshake(hello)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	challenge, err := conn.RecvAuthChallenge()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("auth challenge: %w", err)
	}
	proof, err := buildAuthProof(a.cfg.Credential, a.cfg.SubjectPriv, hello, ack, challenge)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.AuthProof(proof); err != nil {
		conn.Close()
		return nil, fmt.Errorf("auth proof: %w", err)
	}
	authAck, err := conn.RecvRecord()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("auth ack: %w", err)
	}
	if authAck.Kind != dari.KindControl || dari.MessageType(authAck.MessageType) != dari.MsgAuthAck {
		conn.Close()
		return nil, fmt.Errorf("unexpected record during auth (kind=%s type=%d)", authAck.Kind, authAck.MessageType)
	}
	return conn, nil
}

// registerOnce sends the full registration and reads the ack.
func (a *WorkerAgent) registerOnce(conn *dari.TransportConn) (scheduler.RegisterAckPayload, error) {
	card, err := a.buildCard()
	if err != nil {
		return scheduler.RegisterAckPayload{}, fmt.Errorf("build card: %w", err)
	}
	payload, err := json.Marshal(scheduler.RegisterPayload{Card: card, Config: a.cfg.SignedConfig})
	if err != nil {
		return scheduler.RegisterAckPayload{}, err
	}
	if err := conn.SendMessage(dari.MsgEndpointRegister, nil, payload, 0, 1); err != nil {
		return scheduler.RegisterAckPayload{}, err
	}
	return a.recvAck(conn)
}

// heartbeatOnce sends a fresh card as the lease renewal.
func (a *WorkerAgent) heartbeatOnce(conn *dari.TransportConn) (scheduler.RegisterAckPayload, error) {
	card, err := a.buildCard()
	if err != nil {
		return scheduler.RegisterAckPayload{}, fmt.Errorf("build card: %w", err)
	}
	payload, err := json.Marshal(scheduler.HeartbeatPayload{Card: card})
	if err != nil {
		return scheduler.RegisterAckPayload{}, err
	}
	if err := conn.SendMessage(dari.MsgEndpointLease, nil, payload, 0, 1); err != nil {
		return scheduler.RegisterAckPayload{}, err
	}
	return a.recvAck(conn)
}

func (a *WorkerAgent) recvAck(conn *dari.TransportConn) (scheduler.RegisterAckPayload, error) {
	record, err := conn.RecvRecord()
	if err != nil {
		return scheduler.RegisterAckPayload{}, err
	}
	var ack scheduler.RegisterAckPayload
	if err := json.Unmarshal(record.Payload, &ack); err != nil {
		return scheduler.RegisterAckPayload{}, fmt.Errorf("decode ack: %w", err)
	}
	a.setOutcome(ack.Outcome, ack.Reason)
	return ack, nil
}

func (a *WorkerAgent) setOutcome(outcome scheduler.Outcome, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.LastOutcome = outcome
	a.LastReason = reason
}

// Close closes the active connection, if any.
func (a *WorkerAgent) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.Close()
		a.conn = nil
	}
}

// buildCard assembles the signed capability card from the signed config,
// engine introspection, host inventory, and socket measurement.
func (a *WorkerAgent) buildCard() (scheduler.WorkerCard, error) {
	cfg := a.cfg
	card := scheduler.WorkerCard{
		CardVersion:      2,
		DariAddr:         os.Getenv("PCCP_PIA_DARI_ADDR"),
		WorkerID:         cfg.Credential.SubjectPeerID,
		EnrollmentID:     cfg.Credential.Serial,
		EngineKind:       cfg.EngineKind,
		EngineURL:        cfg.EngineURL,
		NodeID:           cfg.NodeID,
		Region:           cfg.Region,
		Zone:             cfg.Zone,
		TP:               1,
		DP:               1,
		Status:           "active",
		ReachabilityMode: cfg.SignedConfig.Config.BackendMode,
		MeasuredGrade:    MeasureReachability(cfg.EngineURL),
	}
	if hostname, err := os.Hostname(); err == nil {
		card.Hostname = hostname
	}
	digest := sha256.Sum256(cfg.Credential.SignedCredential)
	card.PPCFingerprint = "sha256:" + hex.EncodeToString(digest[:])

	engine, err := introspectEngine(cfg.EngineURL)
	if err != nil {
		card.Status = "degraded"
		log.Printf("pia worker: engine introspection failed: %v", err)
	} else {
		card.ActiveSeqs = engine.RunningSeqs
		card.ModelName = engine.ModelName
		card.ModelVersion = engine.ModelVersion
		card.Precision = engine.Precision
		card.ContextLength = engine.ContextLength
		card.MaxConcurrentSeqs = engine.MaxConcurrentSeqs
		card.Modalities = engine.Modalities
	}

	gpu := detectGPUs()
	card.AcceleratorFamily = gpu.AcceleratorFamily
	card.GPUSKU = gpu.SKU
	card.GPUCount = gpu.Count
	card.HBMGB = gpu.HBMGB

	if err := card.Sign(cfg.SubjectPriv); err != nil {
		return scheduler.WorkerCard{}, fmt.Errorf("sign card: %w", err)
	}
	return card, nil
}

// buildAuthProof builds the client AUTH_PROOF over the handshake transcript
// (mirrors the scheduler listener's verification).
func buildAuthProof(cred *dari.PeerCredential, subjectPriv ed25519.PrivateKey, hello *dari.HelloMessage, ack *dari.HelloAckMessage, challenge *dari.AuthChallengeMessage) (*dari.AuthProofMessage, error) {
	helloCBOR, err := dari.CanonicalHelloCBOR(hello)
	if err != nil {
		return nil, err
	}
	ackCBOR, err := dari.CanonicalAckCBOR(ack)
	if err != nil {
		return nil, err
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
	}, nil
}

// publishKVJournal sends the journal's newest records to the scheduler
// as a KV_JOURNAL batch (spec §13.11). A nil journal is a no-op.
func (a *WorkerAgent) publishKVJournal(conn *dari.TransportConn) {
	if a.cfg.KVJournal == nil {
		return
	}
	recs := a.cfg.KVJournal.Replay()
	if len(recs) == 0 {
		return
	}
	// Batch everything the scheduler has not acknowledged yet; the
	// scheduler dedups by (worker, seq), so replays are idempotent.
	var blocks []scheduler.KVBlock
	var lastSeq uint64
	for _, r := range recs {
		lastSeq = r.Seq
		blocks = append(blocks, scheduler.KVBlock{
			Namespace: a.cfg.SignedConfig.Config.TenantID,
			Hash:      r.Key,
			Tokens:    r.Tokens,
		})
	}
	payload, err := json.Marshal(map[string]interface{}{
		"seq":    lastSeq,
		"blocks": blocks,
	})
	if err != nil {
		log.Printf("pia worker: marshal KV journal: %v", err)
		return
	}
	if err := conn.SendMessage(dari.MsgKVJournal, nil, payload, 0, 1); err != nil {
		log.Printf("pia worker: publish KV journal: %v", err)
	}
}
