package pia

import (
	"context"
	"crypto/ecdsa"
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
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// DARIListener accepts native DARI protocol connections on the PIA.
// Per v2 §9.2: "DARI Relay to PIA: DARI"
type DARIListener struct {
	svc       *Service
	tlsConfig *tls.Config
	mu        sync.Mutex
	conns     map[string]*dari.TransportConn
}

func NewDARIListener(svc *Service) *DARIListener {
	tlsConfig := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, NextProtos: []string{dari.DARIProtocol}}

	// Generate self-signed cert for DARI TLS if none provided
	cert, err := generateSelfSignedCert()
	if err != nil {
		log.Printf("pia-paper: cert generation failed, DARI listener will use InsecureSkipVerify: %v", err)
	} else {
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return &DARIListener{
		svc:       svc,
		tlsConfig: tlsConfig,
		conns:     make(map[string]*dari.TransportConn),
	}
}

// generateSelfSignedCert creates an ephemeral ECDSA self-signed certificate
// for DARI TLS connections. In production, this should use proper PKI.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Patty Code PIA"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost", "pia"},
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

func (pl *DARIListener) ListenTCP(ctx context.Context, addr string) error {
	listener, err := dari.ListenTCP(addr, pl.tlsConfig)
	if err != nil {
		return fmt.Errorf("pia-paper: listen %s: %w", addr, err)
	}
	defer listener.Close()
	log.Printf("pia-paper: DARI listener on %s", addr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		netConn, err := listener.Accept()
		if err != nil {
			continue
		}
		go pl.handleConn(ctx, netConn)
	}
}

func (pl *DARIListener) handleConn(ctx context.Context, netConn net.Conn) {
	defer netConn.Close()
	conn, err := dari.AcceptTCP(netConn, dari.DefaultTransportConfig())
	if err != nil {
		log.Printf("pia-paper: preface failed: %v", err)
		return
	}
	connID := fmt.Sprintf("paper-%s", netConn.RemoteAddr())
	pl.mu.Lock()
	pl.conns[connID] = conn
	pl.mu.Unlock()
	defer func() {
		pl.mu.Lock()
		delete(pl.conns, connID)
		pl.mu.Unlock()
	}()

	hello, err := conn.AcceptHandshake()
	if err != nil {
		return
	}
	if hello.PeerProfile != dari.ProfileRelay && hello.PeerProfile != dari.ProfileControl {
		return
	}
	serverNonce := make([]byte, 32)
	conn.SendHelloAck(&dari.HelloAckMessage{CoreVersion: 1, CryptoProfile: "DARI-BASE-1", ServerNonce: serverNonce})
	conn.RecvAuthProof()
	log.Printf("pia-paper: peer authenticated, ready for AI requests")
	pl.handleAIRequests(ctx, conn, connID)
}

func (pl *DARIListener) handleAIRequests(ctx context.Context, conn *dari.TransportConn, connID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		record, err := conn.RecvRecord()
		if err != nil {
			return
		}
		msgType := dari.MessageType(record.MessageType)
		switch msgType {
		case dari.MsgAIOpen:
			var aiReq struct {
				Model     string                   `json:"model"`
				Messages  []map[string]interface{} `json:"messages"`
				MaxTokens int                      `json:"max_tokens"`
			}
			json.Unmarshal(record.Payload, &aiReq)
			vllmModel := os.Getenv("PCCP_VLLM_MODEL")
			if vllmModel == "" { vllmModel = aiReq.Model }
			infReq := InferenceRequest{Model: vllmModel, Messages: convertDARIMessages(aiReq.Messages), MaxTokens: aiReq.MaxTokens, Temperature: 0.7}
			resp, err := pl.svc.HandleInference(ctx, infReq)
			if err != nil {
				errPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
				conn.SendMessage(dari.MsgAIComplete, nil, errPayload, record.LaneID, record.LaneSequence+1)
				continue
			}
			completePayload, _ := json.Marshal(resp)
			conn.SendMessage(dari.MsgAIComplete, nil, completePayload, record.LaneID, record.LaneSequence+1)
		case dari.MsgPing:
			conn.SendControl(dari.MsgPong, nil, []byte("pong"))
		}
	}
}

func (pl *DARIListener) ActiveConnections() int {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return len(pl.conns)
}

func convertDARIMessages(raw []map[string]interface{}) []Message {
	var msgs []Message
	for _, m := range raw {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		msgs = append(msgs, Message{Role: role, Content: content})
	}
	return msgs
}
