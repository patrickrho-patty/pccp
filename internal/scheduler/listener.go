package scheduler

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// DARIListener accepts worker DARI connections and drives the registration
// protocol: preface → HELLO/HELLO_ACK → AUTH_CHALLENGE/AUTH_PROOF →
// WORKER_REGISTER / WORKER_LEASEE. The handshake mirrors the relay's
// listener; admission runs the five-rung ladder (DARI scheduler §5–§7).
type DARIListener struct {
	svc       *Scheduler
	tlsConfig *tls.Config
}

// NewDARIListener creates a worker-facing DARI listener. A nil tlsConfig gets
// a TLS 1.3 config with a generated self-signed certificate (dev default),
// mirroring the relay listener.
func NewDARIListener(svc *Scheduler, tlsConfig *tls.Config) *DARIListener {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS13,
			NextProtos:         dari.DARIProtocols(),
		}
		if cert, err := generateListenerCert(); err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = dari.DARIProtocols()
	}
	return &DARIListener{svc: svc, tlsConfig: tlsConfig}
}

// TLSConfig returns the listener's TLS configuration (used when wiring the
// network listener with dari.ListenTCP).
func (l *DARIListener) TLSConfig() *tls.Config {
	return l.tlsConfig
}

// ServeTCP accepts worker connections until the listener closes.
func (l *DARIListener) ServeTCP(ln net.Listener) error {
	for {
		netConn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go l.handleConn(netConn)
	}
}

func (l *DARIListener) handleConn(netConn net.Conn) {
	defer netConn.Close()

	conn, err := dari.AcceptTCP(netConn, dari.DefaultTransportConfig())
	if err != nil {
		log.Printf("scheduler: dari preface failed from %s: %v", netConn.RemoteAddr(), err)
		return
	}

	hello, err := conn.AcceptHandshake()
	if err != nil {
		log.Printf("scheduler: dari HELLO from %s failed: %v", netConn.RemoteAddr(), err)
		return
	}

	serverNonce := make([]byte, 32)
	if _, err := rand.Read(serverNonce); err != nil {
		return
	}
	ack := &dari.HelloAckMessage{
		CoreVersion:   1,
		CryptoProfile: "DARI-BASE-1",
		ServerNonce:   serverNonce,
		ResourceLimits: map[string]uint64{
			"max_payload_len": uint64(dari.MaxPayloadLen),
		},
	}
	if err := conn.SendHelloAck(ack); err != nil {
		return
	}

	challenge := &dari.AuthChallengeMessage{
		ServerNonce:       serverNonce,
		ChallengeID:       []byte(fmt.Sprintf("wkr-%s-%d", netConn.RemoteAddr(), time.Now().UnixNano())),
		CredentialIssuers: l.issuerIDs(),
		RevocationEpoch:   0,
		AuthDeadlineMs:    uint64(time.Now().Add(30 * time.Second).UnixMilli()),
	}
	if err := conn.AuthChallenge(challenge); err != nil {
		return
	}

	proof, err := conn.RecvAuthProof()
	if err != nil {
		return
	}

	cred, err := l.verifyProof(hello, ack, challenge, proof)
	if err != nil {
		log.Printf("scheduler: rejecting worker proof %s: %v", netConn.RemoteAddr(), err)
		errPayload, _ := json.Marshal(map[string]string{"error": "authentication failed"})
		conn.SendMessage(dari.MsgClose, nil, errPayload, 0, 1)
		return
	}

	authAck, _ := json.Marshal(map[string]string{"status": "ready"})
	if err := conn.SendControl(dari.MsgAuthAck, nil, authAck); err != nil {
		return
	}
	log.Printf("scheduler: worker authenticated, peer=%s", cred.SubjectPeerID)

	// The signed config presented at the first successful registration is
	// reused to re-admit heartbeats on this connection.
	var storedConfig *SignedConfig
	for {
		record, err := conn.RecvRecord()
		if err != nil {
			return
		}
		msgType := dari.MessageType(record.MessageType)
		switch {
		case record.Kind == dari.KindControl && msgType == dari.MsgPing:
			conn.SendControl(dari.MsgPong, record.Header, []byte("pong"))

		case record.Kind == dari.KindMessage && msgType == dari.MsgEndpointRegister:
			var reg RegisterPayload
			if err := json.Unmarshal(record.Payload, &reg); err != nil {
				log.Printf("scheduler: bad register payload from %s", cred.SubjectPeerID)
				continue
			}
			result := l.svc.Admit(AdmissionRequest{Card: reg.Card, PPC: cred, Config: reg.Config, Now: time.Now()})
			if result.Outcome != OutcomeDenied {
				l.svc.Registry.Register(reg.Card, ed25519.PublicKey(cred.PublicKey), time.Now())
				l.svc.Serving.Dispatcher.SetSelector(l.svc.Serving.selectorFor(l.svc))
			}
			if result.Outcome == OutcomeQuarantined {
				l.svc.Registry.MarkQuarantined(reg.Card.WorkerID, result.Reason)
			}
			if result.Outcome == OutcomeAdmitted {
				storedConfig = reg.Config
			}
			l.emitOutcome(result, reg.Card.WorkerID)
			l.sendAck(conn, dari.MsgEndpointRegister, result, reg.Card.WorkerID)

		case record.Kind == dari.KindMessage && msgType == dari.MsgKVJournal:
			var journal struct {
				Seq    uint64    `json:"seq"`
				Blocks []KVBlock `json:"blocks"`
			}
			if err := json.Unmarshal(record.Payload, &journal); err != nil {
				log.Printf("scheduler: bad KV journal from %s", cred.SubjectPeerID)
				continue
			}
			applied := l.svc.KV.ApplyJournal(cred.SubjectPeerID, journal.Seq, journal.Blocks)
			ack, _ := json.Marshal(map[string]interface{}{
				"applied": applied,
				"seq":     journal.Seq,
			})
			conn.SendMessage(dari.MsgKVJournalAck, nil, ack, record.LaneID, record.LaneSequence+1)

		case record.Kind == dari.KindMessage && msgType == dari.MsgKVEviction:
			var ev struct {
				WorkerID string `json:"worker_id"`
			}
			if err := json.Unmarshal(record.Payload, &ev); err == nil {
				l.svc.KV.EvictWorker(ev.WorkerID)
			}

		case record.Kind == dari.KindMessage && msgType == dari.MsgEndpointLease:
			var hb HeartbeatPayload
			if err := json.Unmarshal(record.Payload, &hb); err != nil {
				log.Printf("scheduler: bad heartbeat payload from %s", cred.SubjectPeerID)
				continue
			}
			result := l.svc.Admit(AdmissionRequest{Card: hb.Card, PPC: cred, Config: storedConfig, Now: time.Now()})
			if result.Outcome != OutcomeDenied {
				l.svc.Registry.Heartbeat(hb.Card, ed25519.PublicKey(cred.PublicKey), time.Now())
				// Push the live load into the S2 selector; a freed slot
				// wakes the dispatch loop.
				l.svc.Serving.Dispatcher.SetSelector(l.svc.Serving.selectorFor(l.svc))
			}
			if result.Outcome == OutcomeQuarantined {
				l.svc.Registry.MarkQuarantined(hb.Card.WorkerID, result.Reason)
			}
			l.emitOutcome(result, hb.Card.WorkerID)
			l.sendAck(conn, dari.MsgEndpointLease, result, hb.Card.WorkerID)

		default:
			log.Printf("scheduler: unhandled %s/%d from %s", record.Kind, record.MessageType, cred.SubjectPeerID)
		}
	}
}

func (l *DARIListener) issuerIDs() []string {
	ids := make([]string, 0, len(l.svc.Admission.trust.Issuers))
	for id := range l.svc.Admission.trust.Issuers {
		ids = append(ids, id)
	}
	return ids
}

// verifyProof validates the presented PPC chain and the subject's
// proof-of-possession over the handshake transcript.
func (l *DARIListener) verifyProof(hello *dari.HelloMessage, ack *dari.HelloAckMessage, challenge *dari.AuthChallengeMessage, proof *dari.AuthProofMessage) (*dari.PeerCredential, error) {
	if proof == nil || len(proof.Credential) == 0 {
		return nil, fmt.Errorf("missing credential")
	}
	sign1, err := dari.DecodeCOSESign1(proof.Credential)
	if err != nil {
		return nil, fmt.Errorf("decode credential: %w", err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		return nil, fmt.Errorf("credential body: %w", err)
	}
	issuerKey, ok := l.svc.Admission.trust.Issuers[cred.Issuer]
	if !ok {
		return nil, fmt.Errorf("untrusted issuer %q", cred.Issuer)
	}
	if err := dari.VerifyCOSESign1(sign1, issuerKey); err != nil {
		return nil, fmt.Errorf("credential signature: %w", err)
	}
	if !cred.IsValidAt(time.Now()) {
		return nil, fmt.Errorf("credential outside validity window")
	}

	helloCBOR, err := dari.CanonicalHelloCBOR(hello)
	if err != nil {
		return nil, err
	}
	ackCBOR, err := dari.CanonicalAckCBOR(ack)
	if err != nil {
		return nil, err
	}
	digest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, proof.Credential)
	transcript := dari.AuthContext(helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, []byte("tcp-exporter"), digest.Bytes())
	epoch := dari.DecodeRevocationEpochOrZero(proof.RevocationEvidence)
	signingBytes := dari.PeerProofSigningBytes(transcript.Bytes(), proof.ChallengeID, epoch)
	if !dari.VerifyEd25519(ed25519.PublicKey(cred.PublicKey), signingBytes, proof.Signature) {
		return nil, fmt.Errorf("proof-of-possession verification failed")
	}
	cred.SignedCredential = append([]byte(nil), proof.Credential...)
	return cred, nil
}

func (l *DARIListener) emitOutcome(result AdmissionResult, workerID string) {
	switch result.Outcome {
	case OutcomeAdmitted:
		l.svc.Evidence.Emit(EventWorkerRegister, workerID, "")
	case OutcomeQuarantined:
		l.svc.Evidence.Emit(EventWorkerQuarantine, workerID, result.Reason)
	case OutcomeDenied:
		l.svc.Evidence.Emit(EventWorkerDeny, workerID, result.Reason)
	}
}

func (l *DARIListener) sendAck(conn *dari.TransportConn, msgType dari.MessageType, result AdmissionResult, workerID string) {
	ack := RegisterAckPayload{
		Outcome:  result.Outcome,
		WorkerID: workerID,
		Reason:   result.Reason,
	}
	if result.Outcome != OutcomeDenied {
		ack.LeaseTTLSeconds = int(l.svc.Registry.ttl.Seconds())
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return
	}
	conn.SendMessage(msgType, nil, payload, 0, 1)
}

// generateListenerCert creates a self-signed TLS certificate for the dev
// listener (mirrors the relay listener helper).
func generateListenerCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"PCCP Scheduler"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost", "scheduler"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
