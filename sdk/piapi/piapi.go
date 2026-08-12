// Package piapi provides a SDK for building PAPER Inference Agents (PIAs).
//
// A PIA is the component that bridges the PAPER protocol to a local
// serving engine (vLLM, SGLang, TGI, etc.). It:
//   - Accepts PAPER connections from Relays
//   - Authenticates as an INFERENCE peer
//   - Receives AI_OPEN requests
//   - Translates them to the local serving engine API
//   - Returns AI_COMPLETE responses
//
// Per PAPER §40.1: "PIA authenticates to Relay exactly as a first-class
// PAPER peer."
//
// Usage:
//
//	// Create an engine adapter (implement EngineAdapter interface)
//	adapter := piapi.HTTPAdapter("http://localhost:8000", "my-model")
//
//	// Create and start the PIA
//	pia := piapi.New("my-pia-01", adapter)
//	pia.Listen(":9444")
//
// To implement a custom adapter:
//
//	type MyAdapter struct{}
//	func (a *MyAdapter) Complete(ctx context.Context, req *piapi.Request) (*piapi.Response, error) {
//	    // Call your serving engine here
//	    return &piapi.Response{Content: "Hello"}, nil
//	}
package piapi

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
	"sync"
	"time"
)

// EngineAdapter is implemented by serving engine adapters (vLLM, SGLang, etc.).
// The PIA SDK calls Complete() for each inference request received over PAPER.
type EngineAdapter interface {
	// Complete processes an inference request and returns the response.
	Complete(ctx context.Context, req *Request) (*Response, error)
	// HealthCheck verifies the engine is running.
	HealthCheck(ctx context.Context) error
	// ModelID returns the model identifier the engine is serving.
	ModelID() string
}

// Request is a PAPER-normalized inference request.
type Request struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

// Message is a chat message.
type Message struct {
	Role    string
	Content string
}

// Response is a PAPER-normalized inference response.
type Response struct {
	Content      string
	FinishReason string // COMPLETED, MAX_OUTPUT, STOP_SEQUENCE, etc.
	Usage        Usage
}

// Usage contains normalized token accounting.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// PIA is a Patty Inference Agent that speaks PAPER.
type PIA struct {
	peerID  string
	adapter EngineAdapter
	mu      sync.Mutex
	conns   int
}

// New creates a new PIA with the given peer ID and engine adapter.
func New(peerID string, adapter EngineAdapter) *PIA {
	return &PIA{peerID: peerID, adapter: adapter}
}

// Listen starts a PAPER TLS/TCP listener on the given address.
func (p *PIA) Listen(addr string) error {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("pia: generate cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"paper/1"},
	}

	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("pia: listen %s: %w", addr, err)
	}
	defer listener.Close()

	log.Printf("PIA %s listening on %s (PAPER protocol)", p.peerID, addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go p.handleConn(conn)
	}
}

// handleConn processes a PAPER connection.
func (p *PIA) handleConn(conn net.Conn) {
	defer conn.Close()

	p.mu.Lock()
	p.conns++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.conns--
		p.mu.Unlock()
	}()

	log.Printf("PIA: connection from %s", conn.RemoteAddr())

	// In a full implementation, this would:
	// 1. Read and validate PAPER preface
	// 2. Perform HELLO/HELLO_ACK exchange
	// 3. Handle AUTH_CHALLENGE/AUTH_PROOF
	// 4. Process application records (AI_OPEN, PING, etc.)
	// 5. Send AI_COMPLETE responses
	//
	// For the SDK, we use a simplified JSON-over-TLS protocol
	// that carries the same semantics. Full PAPER framing can be
	// layered on top.

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}

		log.Printf("PIA: inference request model=%s tokens=%d", req.Model, req.MaxTokens)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		resp, err := p.adapter.Complete(ctx, &req)
		cancel()

		if err != nil {
			log.Printf("PIA: inference error: %v", err)
			enc.Encode(map[string]string{"error": err.Error()})
			continue
		}

		if err := enc.Encode(resp); err != nil {
			return
		}

		log.Printf("PIA: response sent tokens=%d", resp.Usage.OutputTokens)
	}
}

// PeerID returns the PIA's peer identifier.
func (p *PIA) PeerID() string { return p.peerID }

// ActiveConnections returns the number of active connections.
func (p *PIA) ActiveConnections() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns
}

// generateSelfSignedCert creates an ephemeral TLS certificate.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"PAPER PIA"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
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
