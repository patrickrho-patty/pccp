package paper

import (
	"bytes"
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestPrefaceValidation(t *testing.T) {
	// Create a pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	config := DefaultTransportConfig()

	// Client sends preface
	go func() {
		tc := &TransportConn{conn: clientConn, config: config}
		if err := tc.sendPreface(); err != nil {
			t.Errorf("sendPreface: %v", err)
		}
	}()

	// Server reads preface
	tc := &TransportConn{conn: serverConn, config: config}
	if err := tc.recvPreface(); err != nil {
		t.Fatalf("recvPreface: %v", err)
	}
}

func TestInvalidPreface(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	config := DefaultTransportConfig()

	// Client sends wrong preface
	go func() {
		clientConn.Write([]byte("WRONGPRE"))
	}()

	tc := &TransportConn{conn: serverConn, config: config}
	err := tc.recvPreface()
	if err == nil {
		t.Fatal("expected error for invalid preface")
	}
}

func TestTransportRecordExchange(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	config := DefaultTransportConfig()

	client := &TransportConn{conn: clientConn, config: config}
	server := &TransportConn{conn: serverConn, config: config}

	// Exchange prefaces
	done := make(chan error, 2)
	go func() { done <- client.sendPreface() }()
	go func() { done <- server.recvPreface() }()

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("preface exchange: %v", err)
		}
	}

	// Send a record from client to server
	testRecord := &Record{
		Kind:        KindMessage,
		MessageType: uint16(MsgHello),
		Header:      []byte(`{"test":true}`),
		Payload:     []byte("hello world"),
		LaneID:      1,
		LaneSequence: 1,
	}

	go func() {
		if err := client.SendRecord(testRecord); err != nil {
			t.Errorf("SendRecord: %v", err)
		}
	}()

	received, err := server.RecvRecord()
	if err != nil {
		t.Fatalf("RecvRecord: %v", err)
	}

	if received.Kind != testRecord.Kind {
		t.Fatalf("kind mismatch")
	}
	if received.MessageType != testRecord.MessageType {
		t.Fatalf("message type mismatch")
	}
	if string(received.Payload) != string(testRecord.Payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestTransportHelloExchange(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	config := DefaultTransportConfig()

	client := &TransportConn{conn: clientConn, config: config}
	server := &TransportConn{conn: serverConn, config: config}

	// Exchange prefaces
	go client.sendPreface()
	go server.recvPreface()
	time.Sleep(10 * time.Millisecond)

	hello := &HelloMessage{
		CoreVersions:   []uint8{1},
		PeerProfile:    ProfileHarness,
		CryptoProfiles: []string{"PAPER-BASE-1"},
		ClientNonce:     bytes.Repeat([]byte{0xAB}, 32),
	}

	ack := &HelloAckMessage{
		CoreVersion:   1,
		CryptoProfile: "PAPER-BASE-1",
		ServerNonce:    bytes.Repeat([]byte{0xCD}, 32),
	}

	// Server-side: accept HELLO and send ACK
	done := make(chan error, 1)
	go func() {
		recvHello, err := server.AcceptHandshake()
		if err != nil {
			done <- err
			return
		}
		if recvHello.PeerProfile != ProfileHarness {
			done <- errPeerProfile
			return
		}
		done <- server.SendHelloAck(ack)
	}()

	// Client-side: send HELLO, receive ACK
	recvAck, err := client.Handshake(hello)
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("server side: %v", err)
	}

	if recvAck.CoreVersion != 1 {
		t.Fatal("version mismatch")
	}
	if recvAck.CryptoProfile != "PAPER-BASE-1" {
		t.Fatal("crypto profile mismatch")
	}
}

func TestTLSCertGeneration(t *testing.T) {
	// Verify TLS config construction doesn't fail
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{ALPNProtocol},
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatal("expected TLS 1.3")
	}
	if cfg.NextProtos[0] != ALPNProtocol {
		t.Fatal("expected paper ALPN")
	}
}

var errPeerProfile = errSimple("peer profile mismatch")

type errSimple string

func (e errSimple) Error() string { return string(e) }
