// Package bench is the F3 latency/streaming benchmark harness
// (docs/feature-plans/harness/F-latency-streaming.md). It measures the
// DARI governed stack against the two incumbent transports under a
// deterministic, canned token schedule so the measured variable is the
// protocol stack, not the model:
//
//	Arm A — DARI: full governed loop (TLS+CBOR, mutual auth,
//	      session governance, streamed AI_TOKEN_CHUNKs, evidence
//	      receipt push) against an in-process relay + mock PIA.
//	Arm B — Responses/SSE: the "typical" method — HTTP POST with
//	      server-sent-event streaming, one connection per turn.
//	Arm C — Codex-style WS Responses: a cached WebSocket carrying
//	      Responses-shaped events, prewarmed between turns (mirrors
//	      codex-rs core/src/client.rs).
//
// Reported per arm: TTFT, inter-token latency (p50/p95), total
// completion, turn-start overhead (cold connect vs warm reuse), and
// bytes on the wire per exchange. All arms use the same schedule
// (interTokenDelay), the same payload sizes, and the same localhost
// topology.
package bench

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/relay"
)

// Schedule describes the canned token emission: N tokens, each after
// interTokenDelay (TTFT = first token after interTokenDelay too).
type Schedule struct {
	Tokens          int
	TokenText       string
	InterTokenDelay time.Duration
	FirstTokenDelay time.Duration
}

// DefaultSchedule is the benchmark's fixed emission plan.
func DefaultSchedule() Schedule {
	return Schedule{Tokens: 64, TokenText: "tok ", InterTokenDelay: 2 * time.Millisecond, FirstTokenDelay: 5 * time.Millisecond}
}

// Result is one arm's aggregate measurement.
type Result struct {
	Arm            string
	Turns          int
	TTFTms         float64 // median time-to-first-token across turns
	ITLp50ms       float64 // median inter-token latency
	ITLp95ms       float64
	TotalMs        float64 // median total completion
	ColdStartMs    float64 // median turn-start overhead (connect+auth+setup)
	WarmTurnMs     float64 // median warm turn (connection reuse)
	BytesPerTurn   int64   // wire bytes (app layer) per exchange
	ChunksObserved int     // total streamed chunks (sanity)
}

// Run executes all three arms and returns their results in order.
func Run(ctx context.Context, turns int, sched Schedule) ([]Result, error) {
	if turns < 1 {
		turns = 10
	}
	var out []Result

	// Arm A: DARI.
	paperRes, err := runDARIArm(ctx, turns, sched)
	if err != nil {
		return nil, fmt.Errorf("paper arm: %w", err)
	}
	out = append(out, paperRes)

	// Arm B: SSE.
	sseRes, err := runSSEArm(ctx, turns, sched)
	if err != nil {
		return nil, fmt.Errorf("sse arm: %w", err)
	}
	out = append(out, sseRes)

	// Arm C: prewarmed WebSocket.
	wsRes, err := runWSArm(ctx, turns, sched)
	if err != nil {
		return nil, fmt.Errorf("ws arm: %w", err)
	}
	out = append(out, wsRes)
	return out, nil
}

// ---------------------------------------------------------------------------
// Shared helpers.
// ---------------------------------------------------------------------------

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	v := append([]float64(nil), xs...)
	slices.Sort(v)
	return v[len(v)/2]
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	v := append([]float64(nil), xs...)
	slices.Sort(v)
	idx := int(float64(len(v)-1) * p)
	return v[idx]
}

// ---------------------------------------------------------------------------
// Arm A: DARI — full governed loop against an in-process relay.
// ---------------------------------------------------------------------------

// mockPIA serves the canned schedule over OpenAI-style SSE.
type mockPIA struct {
	srv   *http.Server
	addr  string
	sched Schedule
}

func startMockPIA(sched Schedule) (*mockPIA, error) {
	mux := http.NewServeMux()
	m := &mockPIA{sched: sched}
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		time.Sleep(sched.FirstTokenDelay)
		for i := 0; i < sched.Tokens; i++ {
			if i > 0 {
				time.Sleep(sched.InterTokenDelay)
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", sched.TokenText)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", sched.Tokens, sched.Tokens+10)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	m.srv = &http.Server{Handler: mux}
	m.addr = ln.Addr().String()
	go m.srv.Serve(ln)
	return m, nil
}

func (m *mockPIA) close() { m.srv.Close() }

// benchTrustPeer runs the HELLO→AUTH→SESSION_OPEN handshake against
// the relay using a fresh enrolled credential, then measures the
// governed exchange loop.
type dariWireClient struct {
	conn   *dari.TransportConn
	serial string
	peerID string
	cred   []byte
	priv   ed25519.PrivateKey
}

// runDARIArm spins the full stack in-process: relay listener with a
// real trust bundle, an enrolled harness credential, the session
// handshake, and N governed exchanges with streaming.
func runDARIArm(ctx context.Context, turns int, sched Schedule) (Result, error) {
	res := Result{Arm: "DARI (governed, streaming)", Turns: turns}

	// Mock PIA streaming the schedule. The env is set/restored ONCE per
	// Run (arm-scoped mutation is not concurrency-safe).
	pia, err := startMockPIA(sched)
	if err != nil {
		return res, err
	}
	defer pia.close()
	osSetenv("PCCP_PIA_URL", "http://"+pia.addr)
	defer osSetenv("PCCP_PIA_URL", "")

	// Relay + DB + trust bundle.
	dbPath := fmt.Sprintf("/tmp/pccp-bench-%d.db", time.Now().UnixNano())
	db, err := openBenchDB(dbPath)
	if err != nil {
		return res, err
	}
	svc, err := relay.New(db, "", "relay-bench")
	if err != nil {
		return res, err
	}
	epoch, revoked := svc.Identity().RevocationSnapshot()
	trust := relay.TrustBundle{
		Issuers:         map[string]ed25519.PublicKey{svc.Identity().CAIssuerID(): svc.Identity().CAPublicKeyRaw()},
		ProtocolVersion: 1,
		RevocationEpoch: epoch,
		RevokedSerials:  revoked,
	}
	addr := freeLocalAddr()
	listener := relay.NewDARIListener(svc, nil, trust)
	ctxLi, cancelLi := context.WithCancel(ctx)
	defer cancelLi()
	go listener.ListenTCP(ctxLi, addr)
	defer os.Remove(dbPath)

	// Enroll a bench harness via the admin API surface (direct service
	// call — same code path the HTTP handler drives).
	if err := db.Create(&models.User{
		AuditBase: models.AuditBase{Base: models.Base{ID: "bench-user"}, OrganizationID: "org-bench"},
		Email:     "bench-user@bench.invalid",
		Name:      "Benchmark User",
		Status:    models.UserStatusActive,
	}).Error; err != nil {
		return res, err
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, _, err = svc.Identity().EnrollHarness(identityEnrollRequest("org-bench", "bench-user", "harness-bench", hex.EncodeToString(pub)))
	if err != nil {
		return res, err
	}
	// Re-issue through the HTTP-visible shape to obtain the credential
	// bytes: EnrollHarness returns the credential; fetch from the DB row.
	credHex, err := svc.Identity().CredentialHexForHarness("harness-bench")
	if err != nil {
		return res, err
	}
	cred, _ := hex.DecodeString(credHex)

	// Explicit onboarding (audit finding: the fail-closed serving
	// hardening broke the bench unless PCCP_DEV_BOOTSTRAP was set).
	// Register the serving chain + policy epoch through the real
	// service APIs — the same calls an operator makes, no env escape.
	pkg, err := svc.RegisterModelServing("org-bench", "bench-model")
	if err != nil {
		return res, fmt.Errorf("bench onboarding: %w", err)
	}
	if _, err := svc.Policy().GetActiveEpoch("org-bench"); err != nil {
		if _, cerr := svc.Policy().CreatePolicyEpoch("org-bench", []string{pkg.PackageID}, "immediate"); cerr != nil {
			return res, fmt.Errorf("bench epoch: %w", cerr)
		}
	}

	var ttfts, itls, totals, colds, warms []float64
	var wireBytes int64
	chunks := 0
	var client *dariWireClient

	for turn := 0; turn < turns; turn++ {
		turnStart := time.Now()
		if client == nil {
			client, err = dialDARI(addr, "harness-bench", cred, priv)
			if err != nil {
				return res, err
			}
			colds = append(colds, float64(time.Since(turnStart).Microseconds())/1000.0)
		}
		warmSend := time.Now()

		// Governed exchange.
		payload, _ := json.Marshal(map[string]any{
			"model":      "bench-model",
			"messages":   []map[string]string{{"role": "user", "content": "bench"}},
			"max_tokens": 128,
			"stream":     true,
		})
		t0 := time.Now()
		if err := client.conn.SendMessage(dari.MsgAIOpen, nil, payload, 1, 1); err != nil {
			return res, err
		}
		wireBytes += int64(len(payload)) + 16
		firstRecord := true

		var ttft time.Duration
		var last time.Time
		itlThis := []float64{}
		done := false
		for !done {
			rec, err := client.conn.RecvRecord()
			if firstRecord {
				// Warm turn-start: reused-connection send→first-record.
				if turn > 0 {
					warms = append(warms, float64(time.Since(warmSend).Microseconds())/1000.0)
				}
				firstRecord = false
			}
			if err != nil {
				return res, err
			}
			wireBytes += int64(len(rec.Payload)) + 16
			switch dari.MessageType(rec.MessageType) {
			case dari.MsgAITokenChunk:
				now := time.Now()
				var ch struct {
					Text string `json:"text"`
				}
				json.Unmarshal(rec.Payload, &ch)
				if ch.Text != "" {
					if ttft == 0 {
						ttft = now.Sub(t0)
					} else {
						itlThis = append(itlThis, float64(now.Sub(last).Microseconds())/1000.0)
					}
					last = now
					chunks++
				}
			case dari.MsgEvidenceReceipt:
				// Governance traffic rides the same stream; ack with the
				// exact CBOR shape the relay decodes.
				var rcpt struct {
					ReceiptID  string `cbor:"1,keyasint"`
					ExchangeID string `cbor:"2,keyasint"`
				}
				if paperUnmarshal(rec.Payload, &rcpt) == nil && rcpt.ReceiptID != "" {
					d := sha256.Sum256([]byte("bench-ack|" + rcpt.ReceiptID))
					ackBody, _ := relay.BuildReceiptAckCBOR(rcpt.ReceiptID, rcpt.ExchangeID, d, time.Now().UnixMilli())
					client.conn.SendMessage(dari.MsgEvidenceReceiptAck, nil, ackBody, 0, 1)
					wireBytes += int64(len(ackBody)) + 16
				}
			case dari.MsgAIComplete:
				done = true
			case dari.MsgClose:
				return res, fmt.Errorf("relay closed during bench: %s", rec.Payload)
			}
		}
		total := time.Since(t0)
		if ttft > 0 {
			ttfts = append(ttfts, float64(ttft.Microseconds())/1000.0)
			itls = append(itls, itlThis...)
		}
		totals = append(totals, float64(total.Microseconds())/1000.0)
	}
	client.conn.Close()

	res.TTFTms = median(ttfts)
	res.ITLp50ms = median(itls)
	res.ITLp95ms = pct(itls, 0.95)
	res.TotalMs = median(totals)
	res.ColdStartMs = median(colds)
	res.WarmTurnMs = median(warms)
	res.BytesPerTurn = wireBytes / int64(turns)
	res.ChunksObserved = chunks
	return res, nil
}

// dialDARI performs HELLO + AUTH_PROOF + SESSION_OPEN + setup consume
// and returns the live client.
func dialDARI(addr, peerID string, cred []byte, priv ed25519.PrivateKey) (*dariWireClient, error) {
	conn, err := dari.DialTCP(addr, nil, dari.DefaultTransportConfig())
	if err != nil {
		return nil, err
	}
	hello := &dari.HelloMessage{
		CoreVersions:      []uint8{1},
		PeerProfile:       dari.ProfileHarness,
		TransportFeatures: []string{"tcp-tls"},
		Extensions:        map[string]uint8{"dari.ai/1": 1},
		EncodingProfiles:  []string{"cbor", "json"},
		CryptoProfiles:    []string{"DARI-BASE-1"},
		ClientNonce:       make([]byte, 32),
	}
	ack, err := conn.Handshake(hello)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// AUTH_CHALLENGE
	rec, err := conn.RecvRecord()
	if err != nil {
		conn.Close()
		return nil, err
	}
	var challenge dari.AuthChallengeMessage
	if err := paperUnmarshal(rec.Payload, &challenge); err != nil {
		conn.Close()
		return nil, fmt.Errorf("challenge decode: %v", err)
	}
	helloCBOR, _ := dari.CanonicalHelloCBOR(hello)
	ackCBOR, _ := dari.CanonicalAckCBOR(ack)
	credDigest := dari.ComputeObjectDigest(dari.ObjTypePeerCredential, cred)
	transcript := dari.AuthContext(helloCBOR, ackCBOR, hello.ClientNonce, challenge.ServerNonce, []byte("tcp-exporter"), credDigest.Bytes())
	proofBytes := dari.PeerProofSigningBytes(transcript.Bytes(), challenge.ChallengeID, challenge.RevocationEpoch)
	sig := ed25519.Sign(priv, proofBytes)
	proof := &dari.AuthProofMessage{
		Credential:         cred,
		Signature:          sig,
		KeyAlgorithm:       dari.COSEAlgEdDSA,
		ChallengeID:        challenge.ChallengeID,
		RevocationEvidence: dari.EncodeRevocationEpoch(challenge.RevocationEpoch),
	}
	proofCBOR, err := dari.MarshalCBOR(proof)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.SendControl(dari.MsgAuthProof, nil, proofCBOR); err != nil {
		conn.Close()
		return nil, err
	}
	// AUTH_ACK, then SESSION_OPEN; the relay answers with the setup
	// sequence (epoch → catalog → lease → grant).
	deadline := time.Now().Add(15 * time.Second)
	sawAuthAck := false
	for !sawAuthAck {
		if time.Now().After(deadline) {
			conn.Close()
			return nil, fmt.Errorf("auth-ack timeout")
		}
		rec, err = conn.RecvRecord()
		if err != nil {
			conn.Close()
			return nil, err
		}
		switch dari.MessageType(rec.MessageType) {
		case dari.MsgAuthAck:
			sawAuthAck = true
		case dari.MsgClose:
			conn.Close()
			return nil, fmt.Errorf("rejected: %s", rec.Payload)
		}
	}
	open, _ := json.Marshal(map[string]string{"session_id": "bench-sess", "user_id": "bench-user", "model": "bench-model"})
	if err := conn.SendMessage(dari.MsgSessionOpen, nil, open, 0, 0); err != nil {
		conn.Close()
		return nil, err
	}
	sawGrant := false
	for !sawGrant {
		if time.Now().After(deadline) {
			conn.Close()
			return nil, fmt.Errorf("setup timeout")
		}
		rec, err = conn.RecvRecord()
		if err != nil {
			conn.Close()
			return nil, err
		}
		switch dari.MessageType(rec.MessageType) {
		case dari.MsgSessionGrant:
			sawGrant = true
		case dari.MsgClose:
			conn.Close()
			return nil, fmt.Errorf("session rejected: %s", rec.Payload)
		}
	}
	return &dariWireClient{conn: conn, peerID: peerID, cred: cred, priv: priv}, nil
}

// ---------------------------------------------------------------------------
// Arm B: HTTP + SSE ("typical Responses/messages" transport).
// ---------------------------------------------------------------------------

func runSSEArm(ctx context.Context, turns int, sched Schedule) (Result, error) {
	res := Result{Arm: "Responses/SSE (HTTP per turn)", Turns: turns}
	pia, err := startMockPIA(sched)
	if err != nil {
		return res, err
	}
	defer pia.close()
	base := fmt.Sprintf("http://%s", pia.addr)

	client := &http.Client{}
	var ttfts, itls, totals, colds, warms []float64
	var wireBytes int64
	chunks := 0

	for turn := 0; turn < turns; turn++ {
		turnStart := time.Now()
		body, _ := json.Marshal(map[string]any{"model": "bench", "messages": []map[string]string{{"role": "user", "content": "bench"}}, "stream": true})
		req, _ := http.NewRequest("POST", base+"/v1/chat/completions", bytesReader(body))
		req.Header.Set("Accept", "text/event-stream")
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return res, err
		}
		// SSE reconnects every turn by design: the full connect+send is
		// recorded as the per-turn (cold) overhead each turn.
		colds = append(colds, float64(time.Since(turnStart).Microseconds())/1000.0)

		reader := bufio.NewReader(resp.Body)
		var ttft time.Duration
		var last time.Time
		itlThis := []float64{}
		sseFirstLine := true
		for {
			line, err := reader.ReadString('\n')
			if sseFirstLine {
				// Warm-equivalent: request-send→first-SSE-byte.
				warms = append(warms, float64(time.Since(t0).Microseconds())/1000.0)
				sseFirstLine = false
			}
			wireBytes += int64(len(line))
			if len(line) > 6 && line[:6] == "data: " {
				payload := line[6:]
				var frame struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if json.Unmarshal([]byte(payload), &frame) == nil && len(frame.Choices) > 0 && frame.Choices[0].Delta.Content != "" {
					now := time.Now()
					if ttft == 0 {
						ttft = now.Sub(t0)
					} else {
						itlThis = append(itlThis, float64(now.Sub(last).Microseconds())/1000.0)
					}
					last = now
					chunks++
				}
				if payload == "[DONE]\n" {
					break
				}
			}
			if err != nil {
				break
			}
		}
		resp.Body.Close()
		total := time.Since(t0)
		if ttft > 0 {
			ttfts = append(ttfts, float64(ttft.Microseconds())/1000.0)
			itls = append(itls, itlThis...)
		}
		totals = append(totals, float64(total.Microseconds())/1000.0)
	}
	res.TTFTms = median(ttfts)
	res.ITLp50ms = median(itls)
	res.ITLp95ms = pct(itls, 0.95)
	res.TotalMs = median(totals)
	res.ColdStartMs = median(colds)
	res.WarmTurnMs = median(warms)
	res.BytesPerTurn = wireBytes / int64(turns)
	res.ChunksObserved = chunks
	return res, nil
}

// ---------------------------------------------------------------------------
// Arm C: cached prewarmed WebSocket (Codex-style).
// ---------------------------------------------------------------------------

func runWSArm(ctx context.Context, turns int, sched Schedule) (Result, error) {
	res := Result{Arm: "WS Responses (cached, prewarmed)", Turns: turns}

	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	var conns int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		mu.Lock()
		conns++
		mu.Unlock()
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				Type    string `json:"type"`
				Extract bool   `json:"extract,omitempty"`
			}
			json.Unmarshal(msg, &req)
			if req.Type == "response.create" {
				// Codex prewarm: generate=false waits for a completion
				// event without running a model.
				if req.Extract {
					out, _ := json.Marshal(map[string]string{"type": "response.completed", "prewarm": "1"})
					c.WriteMessage(mt, out)
					continue
				}
				go func() {
					time.Sleep(sched.FirstTokenDelay)
					for i := 0; i < sched.Tokens; i++ {
						if i > 0 {
							time.Sleep(sched.InterTokenDelay)
						}
						out, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": sched.TokenText})
						if err := c.WriteMessage(websocket.TextMessage, out); err != nil {
							return
						}
					}
					done, _ := json.Marshal(map[string]string{"type": "response.completed"})
					c.WriteMessage(websocket.TextMessage, done)
				}()
			}
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return res, err
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	wsURL := fmt.Sprintf("ws://%s/v1/ws", ln.Addr().String())

	var conn *websocket.Conn
	var ttfts, itls, totals, colds, warms []float64
	var wireBytes int64
	chunks := 0

	for turn := 0; turn < turns; turn++ {
		turnStart := time.Now()
		if conn == nil {
			dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
			c, _, err := dialer.Dial(wsURL, nil)
			if err != nil {
				return res, err
			}
			conn = c
			// Prewarm exactly like codex-rs: response.create with
			// generate=false on the idle connection.
			pre, _ := json.Marshal(map[string]any{"type": "response.create", "extract": true})
			conn.WriteMessage(websocket.TextMessage, pre)
			_, _, err = conn.ReadMessage()
			if err != nil {
				return res, err
			}
			colds = append(colds, float64(time.Since(turnStart).Microseconds())/1000.0)
		}
		warmSend := time.Now()

		reqBody, _ := json.Marshal(map[string]any{"type": "response.create"})
		t0 := time.Now()
		if err := conn.WriteMessage(websocket.TextMessage, reqBody); err != nil {
			return res, err
		}
		wireBytes += int64(len(reqBody)) + 8
		firstRecord := true

		var ttft time.Duration
		var last time.Time
		itlThis := []float64{}
		for {
			mt, msg, err := conn.ReadMessage()
			if firstRecord {
				// Warm turn-start: reused-connection send→first-record.
				if turn > 0 {
					warms = append(warms, float64(time.Since(warmSend).Microseconds())/1000.0)
				}
				firstRecord = false
			}
			if err != nil {
				return res, err
			}
			wireBytes += int64(len(msg)) + 8
			var ev struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			}
			json.Unmarshal(msg, &ev)
			switch ev.Type {
			case "response.output_text.delta":
				now := time.Now()
				if ttft == 0 {
					ttft = now.Sub(t0)
				} else {
					itlThis = append(itlThis, float64(now.Sub(last).Microseconds())/1000.0)
				}
				last = now
				chunks++
			case "response.completed":
				goto turnDone
			}
			_ = mt
		}
	turnDone:
		total := time.Since(t0)
		if ttft > 0 {
			ttfts = append(ttfts, float64(ttft.Microseconds())/1000.0)
			itls = append(itls, itlThis...)
		}
		totals = append(totals, float64(total.Microseconds())/1000.0)
	}
	conn.Close()

	res.TTFTms = median(ttfts)
	res.ITLp50ms = median(itls)
	res.ITLp95ms = pct(itls, 0.95)
	res.TotalMs = median(totals)
	res.ColdStartMs = median(colds)
	res.WarmTurnMs = median(warms)
	res.BytesPerTurn = wireBytes / int64(turns)
	res.ChunksObserved = chunks
	return res, nil
}
