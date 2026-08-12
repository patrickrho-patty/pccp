package paper

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"time"
)

// Client is a reference PAPER protocol client for peers (Harness, PIA).
// It establishes a PAPER connection, performs the full handshake
// (HELLO → AUTH_CHALLENGE → AUTH_PROOF), and provides methods for
// opening governed exchanges and sending AI inference requests.
type Client struct {
	conn     *TransportConn
	peerID   string
	orgID    string
	privKey  ed25519.PrivateKey
	cred     *PeerCredential
}

// ClientConfig holds client connection configuration.
type ClientConfig struct {
	Addr          string // relay address (host:port)
	TLSConfig     *tls.Config
	PeerID        string
	OrganizationID string
	PrivateKey    ed25519.PrivateKey
	Credential    *PeerCredential
	Profile       PeerProfile
}

// DialClient connects to a PAPER relay and performs the full handshake.
func DialClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Profile == "" {
		cfg.Profile = ProfileHarness
	}

	transportCfg := DefaultTransportConfig()
	conn, err := DialTCP(cfg.Addr, cfg.TLSConfig, transportCfg)
	if err != nil {
		return nil, fmt.Errorf("paper-client: dial: %w", err)
	}

	client := &Client{
		conn:    conn,
		peerID:  cfg.PeerID,
		orgID:   cfg.OrganizationID,
		privKey: cfg.PrivateKey,
		cred:    cfg.Credential,
	}

	// Perform handshake
	if err := client.handshake(ctx, cfg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("paper-client: handshake: %w", err)
	}

	log.Printf("paper-client: connected to %s as %s (profile=%s)", cfg.Addr, cfg.PeerID, cfg.Profile)
	return client, nil
}

// handshake performs HELLO → AUTH_CHALLENGE → AUTH_PROOF.
func (c *Client) handshake(ctx context.Context, cfg ClientConfig) error {
	// Phase 1: Send HELLO
	clientNonce := make([]byte, 32)
	hello := &HelloMessage{
		CoreVersions:           []uint8{1},
		PeerProfile:            cfg.Profile,
		TransportFeatures:      []string{"tcp-tls"},
		Extensions:             map[string]uint8{"paper.ai/1": 1, "paper.context/1": 1, "paper.tools/1": 1},
		CryptoProfiles:         []string{"PAPER-BASE-1"},
		ClientNonce:            clientNonce,
		CredentialHint:         []byte(cfg.PeerID),
		ImplementationName:     "pccp-paper-client",
		ImplementationVersion:  "1.0",
	}

	ack, err := c.conn.Handshake(hello)
	if err != nil {
		return fmt.Errorf("HELLO exchange: %w", err)
	}

	// Phase 2: Receive AUTH_CHALLENGE
	challenge, err := c.conn.RecvAuthChallenge()
	if err != nil {
		return fmt.Errorf("recv AUTH_CHALLENGE: %w", err)
	}

	// Compute auth proof
	// auth_context = HASH("PAPER-AUTH-v1" || canonical(HELLO) || canonical(HELLO_ACK) ||
	//                     client_nonce || server_nonce || channel_binding || peer_credential_digest)
	helloCBOR, _ := MarshalCBOR(hello)
	ackCBOR, _ := MarshalCBOR(ack)
	credDigest := ComputeObjectDigest(ObjTypePeerCredential, c.cred.SigningBytes())
	authContext := AuthContext(helloCBOR, ackCBOR, clientNonce, challenge.ServerNonce, []byte("tcp-exporter"), credDigest.Bytes())

	// Sign the auth context
	signature, err := SignWithEd25519(c.privKey, authContext.Bytes())
	if err != nil {
		return fmt.Errorf("sign auth context: %w", err)
	}

	// Phase 3: Send AUTH_PROOF
	credBytes := c.cred.SigningBytes()
	proof := &AuthProofMessage{
		Credential:   credBytes,
		Signature:    signature,
		KeyAlgorithm: COSEAlgEdDSA,
		ChallengeID:  challenge.ChallengeID,
	}

	if err := c.conn.AuthProof(proof); err != nil {
		return fmt.Errorf("send AUTH_PROOF: %w", err)
	}

	log.Printf("paper-client: authentication completed")
	return nil
}

// OpenSession sends a SESSION_OPEN request and returns the grant.
func (c *Client) OpenSession(ctx context.Context, projectID, repoID, branch, modelClass string) (*SessionGrantMessage, error) {
	sessionNonce := make([]byte, 16)

	req := &SessionOpenMessage{
		Organization:        c.orgID,
		Project:             projectID,
		Repository:          repoID,
		Branch:              branch,
		RequestedExtensions: []string{"paper.ai/1", "paper.context/1"},
		RequestedModel:      modelClass,
		RequestedTools:      []string{"read", "write"},
		SessionNonce:        sessionNonce,
	}

	reqBytes, err := MarshalCBOR(req)
	if err != nil {
		return nil, fmt.Errorf("marshal SESSION_OPEN: %w", err)
	}

	if err := c.conn.SendMessage(MsgSessionOpen, nil, reqBytes, 0, 0); err != nil {
		return nil, fmt.Errorf("send SESSION_OPEN: %w", err)
	}

	// Wait for SESSION_GRANT
	record, err := c.conn.RecvRecord()
	if err != nil {
		return nil, fmt.Errorf("recv SESSION_GRANT: %w", err)
	}

	if MessageType(record.MessageType) != MsgSessionGrant {
		return nil, fmt.Errorf("expected SESSION_GRANT, got %s", MessageType(record.MessageType))
	}

	var grant SessionGrantMessage
	if err := UnmarshalCBOR(record.Payload, &grant); err != nil {
		return nil, fmt.Errorf("decode SESSION_GRANT: %w", err)
	}

	return &grant, nil
}

// RequestInference sends an AI_OPEN + inference request.
func (c *Client) RequestInference(ctx context.Context, sessionID string, model string, maxTokens int, contextRef []byte) error {
	aiOpen := &AIOpenMessage{
		RequestedModel:  model,
		InferenceMode:   "code",
		MaxInputTokens:  32768,
		MaxOutputTokens: uint32(maxTokens),
		ContextManifestRef: contextRef,
	}

	aiBytes, err := MarshalCBOR(aiOpen)
	if err != nil {
		return fmt.Errorf("marshal AI_OPEN: %w", err)
	}

	return c.conn.SendMessage(MsgAIOpen, nil, aiBytes, 1, 1)
}

// SendPing sends a PAPER-level liveness probe.
func (c *Client) SendPing() error {
	return c.conn.SendControl(MsgPing, nil, []byte("ping"))
}

// Close closes the PAPER connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// PeerID returns the client's peer identifier.
func (c *Client) PeerID() string {
	return c.peerID
}

// IsConnected reports whether the connection is active.
func (c *Client) IsConnected() bool {
	// In production, this would check connection state
	return c.conn != nil
}

// PublicKeyHex returns the client's public key in hex.
func (c *Client) PublicKeyHex() string {
	if c.privKey == nil {
		return ""
	}
	pub := c.privKey.Public().(ed25519.PublicKey)
	return hex.EncodeToString(pub)
}

// Ensure time import is used
var _ = time.Second
