package dari

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// QUICConfig configures the DARI QUIC transport.
type QUICConfig struct {
	TLSConfig          *tls.Config
	MaxIdleTimeout     time.Duration
	KeepAlivePeriod    time.Duration
	MaxIncomingStreams int
}

func DefaultQUICConfig() QUICConfig {
	return QUICConfig{
		MaxIdleTimeout:     30 * time.Second,
		KeepAlivePeriod:    15 * time.Second,
		MaxIncomingStreams: 100,
	}
}

// QUICConn wraps a QUIC connection with DARI framing.
type QUICConn struct {
	conn          *quic.Conn
	controlStream *quic.Stream
	config        QUICConfig
}

// DialQUIC dials a DARI peer over QUIC.
func DialQUIC(ctx context.Context, addr string, tlsConfig *tls.Config, config QUICConfig) (*QUICConn, error) {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         DARIProtocols(),
			MinVersion:         tls.VersionTLS13,
		}
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = DARIProtocols()
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:         config.MaxIdleTimeout,
		KeepAlivePeriod:        config.KeepAlivePeriod,
		MaxIncomingStreams:     int64(config.MaxIncomingStreams),
		EnableDatagrams:        true,
	}

	conn, err := quic.DialAddr(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("dari: QUIC dial %s: %w", addr, err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(1, "failed to open control stream")
		return nil, fmt.Errorf("dari: open control stream: %w", err)
	}

	return &QUICConn{
		conn:          conn,
		controlStream: stream,
		config:        config,
	}, nil
}

// AcceptQUIC accepts a QUIC connection as a DARI server.
func AcceptQUIC(ctx context.Context, listener *quic.Listener, config QUICConfig) (*QUICConn, error) {
	conn, err := listener.Accept(ctx)
	if err != nil {
		return nil, fmt.Errorf("dari: QUIC accept: %w", err)
	}

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		conn.CloseWithError(1, "failed to accept control stream")
		return nil, fmt.Errorf("dari: accept control stream: %w", err)
	}

	return &QUICConn{
		conn:          conn,
		controlStream: stream,
		config:        config,
	}, nil
}

// ListenQUIC creates a QUIC listener for DARI connections.
func ListenQUIC(addr string, tlsConfig *tls.Config, config QUICConfig) (*quic.Listener, error) {
	if tlsConfig == nil {
		return nil, fmt.Errorf("dari: TLS config required for QUIC listener")
	}
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = DARIProtocols()
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:     config.MaxIdleTimeout,
		KeepAlivePeriod:    config.KeepAlivePeriod,
		MaxIncomingStreams: int64(config.MaxIncomingStreams),
		EnableDatagrams:    true,
	}

	listener, err := quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("dari: QUIC listen %s: %w", addr, err)
	}
	return listener, nil
}

// SendRecord sends a DARI record over the QUIC control stream.
func (qc *QUICConn) SendRecord(rec *Record) error {
	return EncodeRecord(qc.controlStream, rec)
}

// RecvRecord reads a DARI record from the QUIC control stream.
func (qc *QUICConn) RecvRecord() (*Record, error) {
	return DecodeRecord(qc.controlStream)
}

// OpenLane opens a new bidirectional QUIC stream as a DARI lane.
func (qc *QUICConn) OpenLane(ctx context.Context) (*quic.Stream, error) {
	return qc.conn.OpenStreamSync(ctx)
}

// AcceptLane accepts a new lane from the peer.
func (qc *QUICConn) AcceptLane(ctx context.Context) (*quic.Stream, error) {
	return qc.conn.AcceptStream(ctx)
}

// SendMessage sends a MESSAGE record on the control lane.
func (qc *QUICConn) SendMessage(msgType MessageType, header, payload []byte, laneID, laneSeq uint64) error {
	rec := &Record{
		Kind:         KindMessage,
		MessageType:  uint16(msgType),
		Header:       header,
		Payload:      payload,
		LaneID:       laneID,
		LaneSequence: laneSeq,
	}
	return qc.SendRecord(rec)
}

// Close closes the QUIC connection.
func (qc *QUICConn) Close() error {
	qc.controlStream.Close()
	return qc.conn.CloseWithError(0, "normal closure")
}

// RemoteAddr returns the remote address.
func (qc *QUICConn) RemoteAddr() net.Addr {
	return qc.conn.RemoteAddr()
}

// QUICMigrationSupport reports whether QUIC connection migration is active.
func (qc *QUICConn) QUICMigrationSupport() bool {
	return true
}
