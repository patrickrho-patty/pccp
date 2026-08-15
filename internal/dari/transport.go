package dari

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"
)

// The connection preface and ALPN identifiers (canonical dari/1 plus
// the legacy fallback) live in legacy_paper1.go.

// TransportConfig configures the DARI transport.
type TransportConfig struct {
	// TLS configuration
	TLSConfig *tls.Config `json:"tls_config"`
	// Read/write timeouts
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	// Keepalive
	KeepAlive time.Duration `json:"keep_alive"`
	// Max message size
	MaxMessageSize int `json:"max_message_size"`
}

// DefaultTransportConfig returns sensible defaults.
func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		KeepAlive:      30 * time.Second,
		MaxMessageSize: 2 * 1024 * 1024, // 2 MiB
	}
}

// Conn wraps a TLS connection with DARI framing.
type TransportConn struct {
	conn   net.Conn
	config TransportConfig
	mu     sync.Mutex
}

// DialTCP dials a DARI peer over TLS/TCP.
func DialTCP(addr string, tlsConfig *tls.Config, config TransportConfig) (*TransportConn, error) {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true, // dev only
			NextProtos:         DARIProtocols(),
			MinVersion:         tls.VersionTLS13,
		}
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = DARIProtocols()
	}

	dialer := &net.Dialer{
		Timeout:   config.ReadTimeout,
		KeepAlive: config.KeepAlive,
	}

	tcpConn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dari: dial %s: %w", addr, err)
	}

	tlsConn := tls.Client(tcpConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("dari: TLS handshake: %w", err)
	}

	tc := &TransportConn{
		conn:   tlsConn,
		config: config,
	}

	// Send DARI preface
	if err := tc.sendPreface(); err != nil {
		tc.Close()
		return nil, fmt.Errorf("dari: send preface: %w", err)
	}

	return tc, nil
}

// AcceptTCP wraps an accepted TLS connection with DARI framing.
func AcceptTCP(conn net.Conn, config TransportConfig) (*TransportConn, error) {
	tc := &TransportConn{
		conn:   conn,
		config: config,
	}

	// Read and verify DARI preface
	if err := tc.recvPreface(); err != nil {
		return nil, fmt.Errorf("dari: recv preface: %w", err)
	}

	return tc, nil
}

// sendPreface writes the DARI connection preface.
func (tc *TransportConn) sendPreface() error {
	tc.conn.SetWriteDeadline(time.Now().Add(tc.config.WriteTimeout))
	_, err := tc.conn.Write(LegacyPaper1Preface)
	return err
}

// recvPreface reads and validates the DARI connection preface.
func (tc *TransportConn) recvPreface() error {
	tc.conn.SetReadDeadline(time.Now().Add(tc.config.ReadTimeout))
	preface := make([]byte, len(LegacyPaper1Preface))
	if _, err := io.ReadFull(tc.conn, preface); err != nil {
		return err
	}
	for i, b := range LegacyPaper1Preface {
		if preface[i] != b {
			return fmt.Errorf("dari: invalid preface byte %d: got %02x, expected %02x", i, preface[i], b)
		}
	}
	return nil
}

// SendRecord writes a DARI record to the connection.
func (tc *TransportConn) SendRecord(rec *Record) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.conn.SetWriteDeadline(time.Now().Add(tc.config.WriteTimeout))
	return EncodeRecord(tc.conn, rec)
}

// RecvRecord reads a DARI record from the connection.
func (tc *TransportConn) RecvRecord() (*Record, error) {
	tc.conn.SetReadDeadline(time.Now().Add(tc.config.ReadTimeout))
	return DecodeRecord(tc.conn)
}

// SendMessage is a convenience method that encodes a CBOR payload and sends
// a MESSAGE record.
func (tc *TransportConn) SendMessage(msgType MessageType, header, payload []byte, laneID, laneSeq uint64) error {
	rec := &Record{
		Kind:         KindMessage,
		MessageType:  uint16(msgType),
		Header:       header,
		Payload:      payload,
		LaneID:       laneID,
		LaneSequence: laneSeq,
	}
	return tc.SendRecord(rec)
}

// SendControl sends a CONTROL record.
func (tc *TransportConn) SendControl(msgType MessageType, header, payload []byte) error {
	rec := &Record{
		Kind:        KindControl,
		MessageType: uint16(msgType),
		Header:      header,
		Payload:     payload,
		LaneID:      0, // control lane
	}
	return tc.SendRecord(rec)
}

// SendData sends a DATA record.
func (tc *TransportConn) SendData(laneID, laneSeq uint64, payload []byte, final bool) error {
	flags := Flags(0)
	if final {
		flags |= FlagFinal
	}
	rec := &Record{
		Kind:         KindData,
		Flags:        flags,
		Payload:      payload,
		LaneID:       laneID,
		LaneSequence: laneSeq,
	}
	return tc.SendRecord(rec)
}

// SendReceipt sends a RECEIPT record.
func (tc *TransportConn) SendReceipt(laneID, laneSeq uint64, header, payload []byte) error {
	rec := &Record{
		Kind:         KindReceipt,
		Header:       header,
		Payload:      payload,
		LaneID:       laneID,
		LaneSequence: laneSeq,
	}
	return tc.SendRecord(rec)
}

// Close closes the underlying connection.
func (tc *TransportConn) Close() error {
	return tc.conn.Close()
}

// RemoteAddr returns the remote address.
func (tc *TransportConn) RemoteAddr() net.Addr {
	return tc.conn.RemoteAddr()
}

// LocalAddr returns the local address.
func (tc *TransportConn) LocalAddr() net.Addr {
	return tc.conn.LocalAddr()
}

// ListenTCP creates a TLS listener for DARI connections.
func ListenTCP(addr string, tlsConfig *tls.Config) (net.Listener, error) {
	if tlsConfig == nil {
		return nil, errors.New("dari: TLS config required for listener")
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = DARIProtocols()
	}

	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("dari: listen %s: %w", addr, err)
	}
	return ln, nil
}

// Handshake performs the DARI HELLO/HELLO_ACK exchange.
// This is a simplified synchronous handshake for Phase 1.
func (tc *TransportConn) Handshake(hello *HelloMessage) (*HelloAckMessage, error) {
	// Encode and send HELLO
	helloBytes, err := MarshalCBOR(hello)
	if err != nil {
		return nil, fmt.Errorf("dari: marshal HELLO: %w", err)
	}
	if err := tc.SendControl(MsgHello, nil, helloBytes); err != nil {
		return nil, fmt.Errorf("dari: send HELLO: %w", err)
	}

	// Receive HELLO_ACK
	rec, err := tc.RecvRecord()
	if err != nil {
		return nil, fmt.Errorf("dari: recv HELLO_ACK: %w", err)
	}
	if rec.Kind != KindControl || MessageType(rec.MessageType) != MsgHelloAck {
		return nil, fmt.Errorf("dari: expected HELLO_ACK, got %s/%s", rec.Kind, MessageType(rec.MessageType))
	}

	var ack HelloAckMessage
	if err := UnmarshalCBOR(rec.Payload, &ack); err != nil {
		return nil, fmt.Errorf("dari: decode HELLO_ACK: %w", err)
	}

	return &ack, nil
}

// AcceptHandshake handles the server-side HELLO reception.
func (tc *TransportConn) AcceptHandshake() (*HelloMessage, error) {
	rec, err := tc.RecvRecord()
	if err != nil {
		return nil, fmt.Errorf("dari: recv HELLO: %w", err)
	}
	if rec.Kind != KindControl || MessageType(rec.MessageType) != MsgHello {
		return nil, fmt.Errorf("dari: expected HELLO, got %s/%s", rec.Kind, MessageType(rec.MessageType))
	}

	var hello HelloMessage
	if err := UnmarshalCBOR(rec.Payload, &hello); err != nil {
		return nil, fmt.Errorf("dari: decode HELLO: %w", err)
	}

	return &hello, nil
}

// SendHelloAck sends a HELLO_ACK in response to a received HELLO.
func (tc *TransportConn) SendHelloAck(ack *HelloAckMessage) error {
	ackBytes, err := MarshalCBOR(ack)
	if err != nil {
		return fmt.Errorf("dari: marshal HELLO_ACK: %w", err)
	}
	return tc.SendControl(MsgHelloAck, nil, ackBytes)
}

// AuthChallenge sends an AUTH_CHALLENGE to the peer.
func (tc *TransportConn) AuthChallenge(challenge *AuthChallengeMessage) error {
	data, err := MarshalCBOR(challenge)
	if err != nil {
		return err
	}
	return tc.SendControl(MsgAuthChallenge, nil, data)
}

// AuthProof sends an AUTH_PROOF in response to a challenge.
func (tc *TransportConn) AuthProof(proof *AuthProofMessage) error {
	data, err := MarshalCBOR(proof)
	if err != nil {
		return err
	}
	return tc.SendControl(MsgAuthProof, nil, data)
}

// RecvAuthChallenge receives and decodes an AUTH_CHALLENGE.
func (tc *TransportConn) RecvAuthChallenge() (*AuthChallengeMessage, error) {
	rec, err := tc.RecvRecord()
	if err != nil {
		return nil, err
	}
	var challenge AuthChallengeMessage
	if err := UnmarshalCBOR(rec.Payload, &challenge); err != nil {
		return nil, err
	}
	return &challenge, nil
}

// RecvAuthProof receives and decodes an AUTH_PROOF.
func (tc *TransportConn) RecvAuthProof() (*AuthProofMessage, error) {
	rec, err := tc.RecvRecord()
	if err != nil {
		return nil, err
	}
	var proof AuthProofMessage
	if err := UnmarshalCBOR(rec.Payload, &proof); err != nil {
		return nil, err
	}
	return &proof, nil
}

// Ensure binary import used
var _ = binary.BigEndian

// DevSelfSignedCert mints an ephemeral ECDSA P-256 self-signed
// certificate for dev/test listeners (shared by the relay, PIA, and
// web carriers).
func DevSelfSignedCert(org string, dnsNames []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{org}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
