package webbinding

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// carriers.go implements the two dari.web/1 carriers. Both carry the
// IDENTICAL canonical DARI envelope (the native transport's record
// framing bytes) — transport parity is a conformance vector. Control
// messages are JSON; data frames are [8-byte BE sequence][record].
//
// Channel binding: the WS fallback binds to the handshake's
// Sec-WebSocket-Key; the WebTransport carrier binds to a server-random
// correlation token delivered only over the authenticated WT session
// (the security property: a proof captured on one connection cannot be
// replayed on another — every connection's binding value differs).

// frame frame helpers -------------------------------------------------------

const (
	ctrlOpen       = "open"
	ctrlOpenAck    = "open_ack"
	ctrlStatus     = "status_query"
	ctrlStatusResp = "status_resp"
	ctrlError      = "error"
)

// openRequest is the control message that opens/reconnects a session.
// helloMessage is the FIRST message on every carrier connection: the
// one-use challenge fused with the per-connection binding token. The
// client cannot sign a proof without it, and the token differs per
// connection, so proofs are channel-bound by construction.
type helloMessage struct {
	Type            string `json:"type"`
	ChallengeID     string `json:"challenge_id,omitempty"`
	NonceHex        string `json:"nonce,omitempty"`
	BindingTokenHex string `json:"binding_token"`
}

// challengeRequest asks the server to mint a one-use challenge for an
// origin (used when the carrier does not know the origin up front).
type challengeRequest struct {
	Type   string `json:"type"`
	Origin string `json:"origin"`
}

// challengeReply carries the minted challenge.
type challengeReply struct {
	Type        string `json:"type"`
	ChallengeID string `json:"challenge_id"`
	NonceHex    string `json:"nonce"`
	Error       string `json:"error,omitempty"`
}

type openRequest struct {
	Type             string `json:"type"`
	Origin           string `json:"origin"`
	ChallengeID      string `json:"challenge_id"`
	ReconnectSession string `json:"reconnect_session,omitempty"`
	SubjectKeyHex    string `json:"subject_key"`
	SignatureHex     string `json:"signature"`
	GrantDigestHex   string `json:"grant_digest,omitempty"`
	// WS only: the handshake key both sides use for channel binding.
	WSHandshakeKey string `json:"ws_key,omitempty"`
}

type openAck struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Resumed   bool   `json:"resumed"`
	// WT only: the server-random channel-binding token (delivered over
	// this authenticated session only).
	BindingTokenHex string `json:"binding_token,omitempty"`
	Error           string `json:"error,omitempty"`
}

type statusRequest struct {
	Type        string `json:"type"`
	OperationID string `json:"operation_id"`
}

type statusResponse struct {
	Type        string `json:"type"`
	OperationID string `json:"operation_id"`
	EnvelopeHex string `json:"envelope"`
	Error       string `json:"error,omitempty"`
}

// parseSubjectKey decodes the browser subject public key.
func parseSubjectKey(hexKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("webbinding: bad subject key")
	}
	return ed25519Pub(raw), nil
}

// decodeOpenProof builds the BrowserProof from the wire form.
func decodeOpenProof(req *openRequest, binding [32]byte) (*BrowserProof, error) {
	sig, err := hex.DecodeString(req.SignatureHex)
	if err != nil {
		return nil, errors.New("webbinding: bad proof signature")
	}
	p := &BrowserProof{
		Origin:             req.Origin,
		ReconnectSessionID: req.ReconnectSession,
		ChallengeID:        req.ChallengeID,
		ChannelBinding:     binding,
		Signature:          sig,
	}
	return p, nil
}

// FrameWriter writes data frames.
type FrameWriter interface {
	WriteFrame(sequence uint64, envelope []byte) error
	WriteControl(payload []byte) error
}

// FrameReader reads frames/control messages.
type ReadResult struct {
	Sequence uint64
	Envelope []byte
	Control  []byte
}

// readFrame reads one length-prefixed frame payload.
func readFrame(r io.Reader, max int) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n == 0 || int(n) > max {
		return nil, fmt.Errorf("webbinding: bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// writeFrame writes one length-prefixed frame.
func writeFrame(w io.Writer, payload []byte) error {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// writeSeqFrame writes [8-byte BE sequence][4-byte len][envelope].
func writeSeqFrame(w io.Writer, sequence uint64, envelope []byte) error {
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], sequence)
	if _, err := w.Write(seqBuf[:]); err != nil {
		return err
	}
	return writeFrame(w, envelope)
}

// readSeqFrame reads [8-byte sequence][4-byte len][envelope].
func readSeqFrame(r io.Reader, max int) (uint64, []byte, error) {
	var seqBuf [8]byte
	if _, err := io.ReadFull(r, seqBuf[:]); err != nil {
		return 0, nil, err
	}
	env, err := readFrame(r, max)
	if err != nil {
		return 0, nil, err
	}
	return binary.BigEndian.Uint64(seqBuf[:]), env, nil
}

// sessionLoop drives an authenticated session over a byte stream:
// length-prefixed frames of [type byte][body]. Used by WT.
func (s *Server) streamSessionLoop(sessionID string, rw io.ReadWriter, stop <-chan struct{}) error {
	reader := bufio.NewReader(rw)
	writer := bufio.NewWriter(rw)
	// frame writes buffer; flush per frame (single syscall per frame).
	writeFrameBuf := func(payload []byte) error {
		if err := writeFrame(writer, payload); err != nil {
			return err
		}
		return writer.Flush()
	}
	var seqCounter uint64
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		env, err := readFrame(reader, 1<<20)
		if err != nil {
			s.Close(sessionID)
			return err
		}
		if len(env) == 0 {
			continue
		}
		if env[0] == 0x01 {
			// Control (status query).
			var sr statusRequest
			if err := json.Unmarshal(env[1:], &sr); err != nil {
				continue
			}
			if sr.Type != ctrlStatus {
				continue
			}
			resp := statusResponse{Type: ctrlStatusResp, OperationID: sr.OperationID}
			out, err := s.StatusQuery(sessionID, sr.OperationID, func(op string) ([]byte, error) {
				if s.handler == nil {
					return nil, errors.New("no handler")
				}
				return s.handler(sessionID, []byte("EFFECT_STATUS:"+op))
			})
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.EnvelopeHex = hex.EncodeToString(out)
			}
			payload, _ := json.Marshal(resp)
			frame := append([]byte{0x01}, payload...)
			if err := writeFrameBuf(frame); err != nil {
				return err
			}
			continue
		}
		// Data: [8-byte sequence][record].
		seq, record, err := func() (uint64, []byte, error) {
			if len(env) < 9 {
				return 0, nil, errors.New("webbinding: short data frame")
			}
			seq := binary.BigEndian.Uint64(env[1:9])
			return seq, env[9:], nil
		}()
		if err != nil {
			continue
		}
		resp, err := s.Process(sessionID, seq, record)
		if err != nil {
			errPayload, _ := json.Marshal(openAck{Type: ctrlError, Error: err.Error()})
			frame := append([]byte{0x01}, errPayload...)
			_ = writeFrame(rw, frame)
			continue
		}
		if resp == nil {
			continue
		}
		seqCounter++ // server-initiated band: 1_000_001, 1_000_002, …
		var seqBuf [8]byte
		binary.BigEndian.PutUint64(seqBuf[:], 1_000_000+seqCounter)
		binary.BigEndian.PutUint64(seqBuf[:], seqCounter)
		frame := append([]byte{0x02}, seqBuf[:]...)
		frame = append(frame, resp...)
		if err := writeFrameBuf(frame); err != nil {
			s.Close(sessionID)
			return err
		}
	}
}

// AcceptWebSocketFallback serves one WebSocket connection as the
// constrained fallback carrier. Text frames carry control JSON;
// binary frames carry [seq][record] exactly like WT. The first server
// message is the hello (challenge + per-connection binding token).
func (s *Server) AcceptWebSocketFallback(conn *websocket.Conn, expectedOrigin string) error {
	defer conn.Close()
	// Bounded frames + deadlines with pong keepalive (slowloris and
	// memory-exhaustion defense).
	conn.SetReadLimit(1 << 20)
	const wsWriteWait = 30 * time.Second
	const wsPongWait = 90 * time.Second
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(wsPongWait)); return nil })
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()
	defer func() {
		// stop the keepalive by closing from our side after return
	}()

	ch, bindingToken, err := s.BeginConnection(expectedOrigin)
	if err != nil {
		_ = conn.WriteJSON(openAck{Type: ctrlError, Error: err.Error()})
		return err
	}
	nonceHex := hex.EncodeToString(ch.Nonce[:])
	tokenHex := hex.EncodeToString(bindingToken[:])
	if err := conn.WriteJSON(helloMessage{Type: "hello", ChallengeID: ch.ID, NonceHex: nonceHex, BindingTokenHex: tokenHex}); err != nil {
		return err
	}
	binding := ChannelBinding(bindingToken[:])

	// 2. OPEN (text frame).
	var req openRequest
	if err := conn.ReadJSON(&req); err != nil {
		return err
	}

	sess, resumed, err := s.completeOpen(&req, binding)
	if err != nil {
		ack, _ := json.Marshal(openAck{Type: ctrlOpenAck, Error: err.Error()})
		_ = conn.WriteMessage(websocket.TextMessage, ack)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()))
		return err
	}
	ack, _ := json.Marshal(openAck{Type: ctrlOpenAck, SessionID: sess.SessionID, Resumed: resumed})
	if err := conn.WriteMessage(websocket.TextMessage, ack); err != nil {
		return err
	}

	var serverSeq uint64
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			s.Close(sess.SessionID)
			return err
		}
		switch mt {
		case websocket.TextMessage:
			var sr statusRequest
			if json.Unmarshal(data, &sr) != nil || sr.Type != ctrlStatus {
				continue
			}
			resp := statusResponse{Type: ctrlStatusResp, OperationID: sr.OperationID}
			out, err := s.StatusQuery(sess.SessionID, sr.OperationID, func(op string) ([]byte, error) {
				if s.handler == nil {
					return nil, errors.New("no handler")
				}
				return s.handler(sess.SessionID, []byte("EFFECT_STATUS:"+op))
			})
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.EnvelopeHex = hex.EncodeToString(out)
			}
			payload, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, payload)
		case websocket.BinaryMessage:
			if len(data) < 8 {
				continue
			}
			seq := binary.BigEndian.Uint64(data[:8])
			record := data[8:]
			resp, err := s.Process(sess.SessionID, seq, record)
			if err != nil {
				errAck, _ := json.Marshal(openAck{Type: ctrlError, Error: err.Error()})
				_ = conn.WriteMessage(websocket.TextMessage, errAck)
				continue
			}
			if resp != nil {
				serverSeq++
				var out []byte
				out = append(out, data[:0]...)
				var seqBuf [8]byte
				binary.BigEndian.PutUint64(seqBuf[:], 1_000_000+serverSeq)
				out = append(out, seqBuf[:]...)
				out = append(out, resp...)
				_ = conn.WriteMessage(websocket.BinaryMessage, out)
			}
		}
	}
}

// HTTPHandler adapts the WS fallback to an http.Handler with origin
// policy applied at upgrade time.
func (s *Server) HTTPHandler(upgrader websocket.Upgrader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		origin := r.Header.Get("Origin")
		if err := s.VerifyWebOrigin(origin); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() { _ = s.AcceptWebSocketFallback(conn, origin) }()
	})
}

// NewBindingToken mints the WT channel-binding token.
func NewBindingToken() [32]byte {
	var t [32]byte
	if _, err := rand.Read(t[:]); err != nil {
		panic("webbinding: rand: " + err.Error())
	}
	return t
}

// WTOpenHandshake runs the server side of the WT open handshake on a
// fresh stream: hello (challenge + binding token) → client open → ack.
func (s *Server) WTOpenHandshake(rw io.ReadWriter, expectedOrigin string) (BrowserSession, bool, [32]byte, error) {
	// Consume the client's ready byte: WebTransport client streams
	// are not visible to the server until the first write, so the
	// client flushes with a single 0x00 before waiting for hello.
	ready := make([]byte, 1)
	if _, err := io.ReadFull(rw, ready); err != nil || ready[0] != 0x00 {
		return BrowserSession{}, false, [32]byte{}, errors.New("webbinding: expected ready byte")
	}
	// The WT carrier does not know the origin up front (native clients
	// send no HTTP Origin), so hello carries ONLY the binding token;
	// the origin-checked challenge is minted on challenge_request.
	bindingToken := NewBindingToken()
	hello, _ := json.Marshal(helloMessage{
		Type:            "hello",
		BindingTokenHex: hex.EncodeToString(bindingToken[:]),
	})
	if err := writeFrame(rw, append([]byte{0x01}, hello...)); err != nil {
		return BrowserSession{}, false, [32]byte{}, err
	}
	binding := ChannelBinding(bindingToken[:])

	reader := bufio.NewReader(rw)
	// 1. challenge_request (origin).
	frame, err := readFrame(reader, 1<<16)
	if err != nil {
		return BrowserSession{}, false, binding, err
	}
	if len(frame) == 0 || frame[0] != 0x01 {
		return BrowserSession{}, false, binding, errors.New("webbinding: expected control")
	}
	var creq challengeRequest
	if err := json.Unmarshal(frame[1:], &creq); err != nil || creq.Type != "challenge_request" {
		return BrowserSession{}, false, binding, errors.New("webbinding: expected challenge_request")
	}
	ch, err := s.IssueChallenge(creq.Origin)
	if err != nil {
		reply, _ := json.Marshal(challengeReply{Type: "challenge", Error: err.Error()})
		_ = writeFrame(rw, append([]byte{0x01}, reply...))
		return BrowserSession{}, false, binding, err
	}
	reply, _ := json.Marshal(challengeReply{
		Type: "challenge", ChallengeID: ch.ID,
		NonceHex: hex.EncodeToString(ch.Nonce[:]),
	})
	if err := writeFrame(rw, append([]byte{0x01}, reply...)); err != nil {
		return BrowserSession{}, false, binding, err
	}

	// 2. open.
	frame, err = readFrame(reader, 1<<16)
	if err != nil {
		return BrowserSession{}, false, binding, err
	}
	if len(frame) == 0 || frame[0] != 0x01 {
		return BrowserSession{}, false, binding, errors.New("webbinding: expected control open")
	}
	var req openRequest
	if err := json.Unmarshal(frame[1:], &req); err != nil {
		return BrowserSession{}, false, binding, err
	}
	if req.Type != ctrlOpen {
		return BrowserSession{}, false, binding, errors.New("webbinding: expected open")
	}
	sess, resumed, err := s.completeOpen(&req, binding)
	if err != nil {
		ack, _ := json.Marshal(openAck{Type: ctrlOpenAck, Error: err.Error()})
		_ = writeFrame(rw, append([]byte{0x01}, ack...))
		return BrowserSession{}, false, binding, err
	}
	ack, _ := json.Marshal(openAck{Type: ctrlOpenAck, SessionID: sess.SessionID, Resumed: resumed})
	if err := writeFrame(rw, append([]byte{0x01}, ack...)); err != nil {
		return BrowserSession{}, false, binding, err
	}
	return sess, resumed, binding, nil
}

// WTSession serves one upgraded WebTransport session: the open
// handshake on the first client stream, then the record loop on it.
func (s *Server) WTSession(openStream io.ReadWriter, done <-chan struct{}) error {
	sess, _, _, err := s.WTOpenHandshake(openStream, "")
	if err != nil {
		return err
	}
	return s.streamSessionLoop(sess.SessionID, openStream, done)
}

// completeOpen is the shared OPEN processing for both carriers: parse
// the subject key, derive the thumbprint server-side, decode the
// grant digest, and run the proof-gated Open.
func (s *Server) completeOpen(req *openRequest, binding [32]byte) (BrowserSession, bool, error) {
	key, err := parseSubjectKey(req.SubjectKeyHex)
	if err != nil {
		return BrowserSession{}, false, err
	}
	proof, err := decodeOpenProof(req, binding)
	if err != nil {
		return BrowserSession{}, false, err
	}
	// The thumbprint is derived from the PRESENTED key (the client
	// proves possession of the matching private key by signing).
	proof.SubjectKeyThumbprint = SubjectThumbprint(key)
	grantDigest := [32]byte{}
	if req.GrantDigestHex != "" {
		gd, derr := hex.DecodeString(req.GrantDigestHex)
		if derr != nil || len(gd) != 32 {
			return BrowserSession{}, false, errors.New("webbinding: bad grant digest")
		}
		copy(grantDigest[:], gd)
	}
	return s.Open(OpenRequest{
		Origin: req.Origin, Proof: proof, SubjectKey: key,
		ChannelBinding: binding, GrantDigest: grantDigest,
	})
}
