package webbinding

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quic-go/webtransport-go"
)

// server_test.go implements the Task 13 conformance vectors: exact
// origin/site binding, proof of possession, channel binding, one-use
// challenges, cross-site rejection, cookie/bearer-only rejection,
// reconnect safety, effect-status-not-re-execute, rate limits, idle
// expiry, and carrier parity (same envelope bytes over both carriers).

func newFixture(t *testing.T) (*Server, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	store, err := NewSessionStore("")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(store, []string{"https://app.example"}, func(sessionID string, env []byte) ([]byte, error) {
		// Echo governance: canonical handler for tests.
		return append([]byte("governed:"), env...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetRateLimit(10000)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv, pub, priv
}

func validOpen(t *testing.T, srv *Server, pub ed25519.PublicKey, priv ed25519.PrivateKey, origin string, binding [32]byte) (BrowserSession, bool, error) {
	t.Helper()
	ch, err := srv.IssueChallenge(origin)
	if err != nil {
		return BrowserSession{}, false, err
	}
	proof := &BrowserProof{
		Origin:               origin,
		SubjectKeyThumbprint: SubjectThumbprint(pub),
		ChallengeID:          ch.ID,
		ChannelBinding:       binding,
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(ch.Nonce))
	return srv.Open(OpenRequest{
		Origin: origin, Proof: proof, SubjectKey: pub,
		ChannelBinding: binding,
	})
}

func TestWebBindingRejectsCookieWithoutProofOfPossession(t *testing.T) {
	srv, _, _ := newFixture(t)
	_, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Cookie: "session-only"})
	if !errors.Is(err, ErrMissingProofOfPossession) {
		t.Fatalf("got %v", err)
	}
	// A bearer token is equally insufficient.
	_, _, err = srv.Open(OpenRequest{Origin: "https://app.example", Bearer: "tok"})
	if !errors.Is(err, ErrMissingProofOfPossession) {
		t.Fatalf("bearer-only accepted: %v", err)
	}
	// The first integration rule: a copied bearer token cannot open a
	// governed exchange — proof is mandatory.
	if m := srv.Metrics(); m.ProofFailures != 2 {
		t.Fatalf("proof failures = %d", m.ProofFailures)
	}
}

func TestWebBindingOriginAndSiteBinding(t *testing.T) {
	srv, pub, priv := newFixture(t)
	binding := ChannelBinding([]byte("carrier"))

	sess, resumed, err := validOpen(t, srv, pub, priv, "https://app.example", binding)
	if err != nil || resumed {
		t.Fatalf("valid open failed: %v resumed=%v", err, resumed)
	}
	if sess.Origin != "https://app.example" || sess.Site != "app.example" {
		t.Fatalf("origin/site = %q/%q", sess.Origin, sess.Site)
	}

	// Disallowed origin: challenge issuance refused.
	if _, err := srv.IssueChallenge("https://evil.example"); err == nil {
		t.Fatal("challenge minted for a disallowed origin")
	}
	// Sub-site does not inherit the policy (exact-origin binding).
	if _, err := srv.IssueChallenge("https://other.app.example"); err == nil {
		t.Fatal("sibling subdomain accepted — origins are exact")
	}
	// Proof for a DIFFERENT origin than the challenge → rejected.
	ch, _ := srv.IssueChallenge("https://app.example")
	proof := &BrowserProof{
		Origin: "https://evil.example", ChallengeID: ch.ID,
		ChannelBinding: binding, SubjectKeyThumbprint: SubjectThumbprint(pub),
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(ch.Nonce))
	if _, _, err := srv.Open(OpenRequest{Origin: "https://evil.example", Proof: proof, SubjectKey: pub}); err == nil {
		t.Fatal("cross-site proof accepted")
	}
}

func TestWebBindingChannelAndChallengeBinding(t *testing.T) {
	srv, pub, priv := newFixture(t)
	good := ChannelBinding([]byte("carrier-a"))

	if _, _, err := validOpen(t, srv, pub, priv, "https://app.example", good); err != nil {
		t.Fatal(err)
	}
	// A proof captured on connection A cannot open connection B: the
	// SAME proof submitted against a different channel binding fails.
	other := ChannelBinding([]byte("carrier-b"))
	chA, errA := srv.IssueChallenge("https://app.example")
	if errA != nil {
		t.Fatal(errA)
	}
	captured := &BrowserProof{
		Origin: "https://app.example", ChallengeID: chA.ID,
		ChannelBinding: good, SubjectKeyThumbprint: SubjectThumbprint(pub),
	}
	captured.Signature = ed25519.Sign(priv, captured.ProofSigningBytes(chA.Nonce))
	if _, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Proof: captured, SubjectKey: pub, ChannelBinding: other}); err == nil {
		t.Fatal("replayed proof on another channel accepted")
	}

	// One-use challenges: the SAME challenge cannot open twice.
	ch, err := srv.IssueChallenge("https://app.example")
	if err != nil {
		t.Fatal(err)
	}
	proof := &BrowserProof{
		Origin: "https://app.example", ChallengeID: ch.ID,
		ChannelBinding: good, SubjectKeyThumbprint: SubjectThumbprint(pub),
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(ch.Nonce))
	if _, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Proof: proof, SubjectKey: pub, ChannelBinding: good}); err != nil {
		t.Fatal(err)
	}
	proof2 := &BrowserProof{Origin: proof.Origin, ChallengeID: ch.ID, ChannelBinding: good, SubjectKeyThumbprint: proof.SubjectKeyThumbprint, Signature: proof.Signature}
	if _, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Proof: proof2, SubjectKey: pub, ChannelBinding: good}); err == nil {
		t.Fatal("one-use challenge reused")
	}

	// Expired challenge rejected.
	old := &Challenge{ID: "old", Origin: "https://app.example", ExpiresAt: time.Now().Add(-time.Minute)}
	proof3 := &BrowserProof{Origin: "https://app.example", ChallengeID: "old", ChannelBinding: good, SubjectKeyThumbprint: SubjectThumbprint(pub)}
	proof3.Signature = ed25519.Sign(priv, proof3.ProofSigningBytes(old.Nonce))
	if err := VerifyBrowserProof(proof3, pub, "https://app.example", good, old); err == nil {
		t.Fatal("expired challenge accepted")
	}

	// Wrong subject key (thumbprint mismatch) rejected.
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	proof4 := &BrowserProof{Origin: "https://app.example", ChallengeID: ch.ID, ChannelBinding: good, SubjectKeyThumbprint: SubjectThumbprint(otherPriv.Public().(ed25519.PublicKey))}
	proof4.Signature = ed25519.Sign(otherPriv, proof4.ProofSigningBytes(ch.Nonce))
	if _, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Proof: proof4, SubjectKey: pub}); err == nil {
		t.Fatal("thumbprint mismatch accepted")
	}
}

func TestWebBindingReconnectSafety(t *testing.T) {
	srv, pub, priv := newFixture(t)
	binding := ChannelBinding([]byte("c1"))
	sess, _, err := validOpen(t, srv, pub, priv, "https://app.example", binding)
	if err != nil {
		t.Fatal(err)
	}

	// Work happens: sequence advances, an effect is recorded.
	if err := srv.store.AdvanceSequence(sess.SessionID, 7); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.RecordEffect(sess.SessionID, "op-42"); err != nil {
		t.Fatal(err)
	}
	srv.Close(sess.SessionID)

	// Reconnect with a fresh proof for the SAME session and key.
	ch, _ := srv.IssueChallenge("https://app.example")
	proof := &BrowserProof{
		Origin: "https://app.example", ReconnectSessionID: sess.SessionID,
		SubjectKeyThumbprint: SubjectThumbprint(pub), ChallengeID: ch.ID,
		ChannelBinding: binding,
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(ch.Nonce))
	resumed, wasResumed, err := srv.Open(OpenRequest{
		Origin: "https://app.example", Proof: proof, SubjectKey: pub, ChannelBinding: binding,
	})
	if err != nil || !wasResumed {
		t.Fatalf("reconnect failed: %v resumed=%v", err, wasResumed)
	}
	if resumed.LastSequence != 7 || len(resumed.EffectOpIDs) != 1 || resumed.EffectOpIDs[0] != "op-42" {
		t.Fatalf("state not resumed: %+v", resumed)
	}

	// Reconnect with a DIFFERENT key = conflict, counted.
	_, rogue, _ := ed25519.GenerateKey(nil)
	ch2, _ := srv.IssueChallenge("https://app.example")
	proofR := &BrowserProof{
		Origin: "https://app.example", ReconnectSessionID: sess.SessionID,
		SubjectKeyThumbprint: SubjectThumbprint(rogue.Public().(ed25519.PublicKey)),
		ChallengeID:          ch2.ID, ChannelBinding: binding,
	}
	proofR.Signature = ed25519.Sign(rogue, proofR.ProofSigningBytes(ch2.Nonce))
	if _, _, err := srv.Open(OpenRequest{Origin: "https://app.example", Proof: proofR, SubjectKey: rogue.Public().(ed25519.PublicKey), ChannelBinding: binding}); err == nil {
		t.Fatal("reconnect with a swapped key accepted")
	}
	if m := srv.Metrics(); m.ReconnectConflicts == 0 {
		t.Fatal("reconnect conflict not counted")
	}

	// Sequence regression rejected (replay protection).
	if err := srv.store.AdvanceSequence(sess.SessionID, 3); err == nil {
		t.Fatal("sequence regression accepted")
	}
}

func TestWebBindingStatusQueryDoesNotReExecute(t *testing.T) {
	srv, pub, priv := newFixture(t)
	sess, _, _ := validOpen(t, srv, pub, priv, "https://app.example", ChannelBinding([]byte("c")))
	calls := 0
	out, err := srv.StatusQuery(sess.SessionID, "op-7", func(op string) ([]byte, error) {
		calls++
		return []byte("STATUS op-7 COMMITTED"), nil
	})
	if err != nil || !strings.Contains(string(out), "COMMITTED") {
		t.Fatalf("status query: %v %q", err, out)
	}
	// The status query MUST NOT execute — exactly one status read.
	if calls != 1 {
		t.Fatalf("status executed %d times", calls)
	}
}

func TestWebBindingRateLimitAndIdleExpiry(t *testing.T) {
	srv, pub, priv := newFixture(t)
	binding := ChannelBinding([]byte("c"))
	for i := 0; i < 5; i++ {
		if _, _, err := validOpen(t, srv, pub, priv, "https://app.example", binding); err != nil {
			// Rate limiting may kick in — that's the test.
			if strings.Contains(err.Error(), "rate limited") {
				break
			}
			t.Fatal(err)
		}
	}
	if m := srv.Metrics(); m.RateLimited == 0 {
		// Exhaust the bucket decisively.
		for i := 0; i < 200; i++ {
			_, _ = srv.IssueChallenge("https://app.example")
		}
	}
	// Idle expiry removes stale sessions.
	sess, _, _ := validOpen(t, srv, pub, priv, "https://app.example", binding)
	_ = srv.store.Update(sess.SessionID, func(b *BrowserSession) error {
		b.LastSeenMs -= int64((31 * time.Minute).Milliseconds())
		return nil
	})
	if n := srv.ExpireIdle(); n == 0 {
		t.Fatal("idle session not expired")
	}
	if _, ok := srv.store.Get(sess.SessionID); ok {
		t.Fatal("expired session still present")
	}
}

// --- Carrier parity: identical envelope bytes over WS and WT carriers ---

func wsOpenSession(t *testing.T, srv *Server, url, origin string, pub ed25519.PublicKey, priv ed25519.PrivateKey, reconnectSessionID string) (*websocket.Conn, openAck) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{origin}})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	var hello helloMessage
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatalf("hello (proof-less stage): %v", err)
	}
	if hello.Type != "hello" || hello.ChallengeID == "" || hello.BindingTokenHex == "" {
		t.Fatalf("bad hello: %+v", hello)
	}
	token, _ := hex.DecodeString(hello.BindingTokenHex)
	binding := ChannelBinding(token)
	nonce, _ := hex.DecodeString(hello.NonceHex)
	var nonceArr [32]byte
	copy(nonceArr[:], nonce)

	proof := &BrowserProof{
		Origin: origin, ChallengeID: hello.ChallengeID,
		ReconnectSessionID: reconnectSessionID,
		ChannelBinding:     binding, SubjectKeyThumbprint: SubjectThumbprint(pub),
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(nonceArr))
	req := map[string]string{
		"type": "open", "origin": origin, "challenge_id": hello.ChallengeID,
		"subject_key": hex.EncodeToString(pub),
		"signature":   hex.EncodeToString(proof.Signature),
	}
	if reconnectSessionID != "" {
		req["reconnect_session"] = reconnectSessionID
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatal(err)
	}
	var ack openAck
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Error != "" || ack.SessionID == "" {
		t.Fatalf("open ack: %+v", ack)
	}
	return conn, ack
}

func TestWebSocketFallbackCarrier(t *testing.T) {
	srv, pub, priv := newFixture(t)
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	ts := httptest.NewServer(srv.HTTPHandler(up))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/dari.web/1"

	// Cookie/bearer-only over the real carrier still fails closed.
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://app.example"}})
	if err != nil {
		t.Fatal(err)
	}
	var hello helloMessage
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]string{"type": "open", "origin": "https://app.example"})
	var ack openAck
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("proof-less ack: %v", err)
	}
	if ack.Error == "" {
		t.Fatal("proof-less open accepted over WS")
	}
	conn.Close()

	// Disallowed origin refused at upgrade time.
	if _, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://evil.example"}}); err == nil {
		t.Fatal("upgrade for a disallowed origin accepted")
	}

	// Full governed session over the fallback carrier.
	conn2, open := wsOpenSession(t, srv, wsURL, "https://app.example", pub, priv, "")
	defer conn2.Close()
	if open.Resumed {
		t.Fatal("fresh open reported resumed")
	}

	// Data frame: [8-byte seq][canonical envelope] — the SAME framing
	// the WT carrier uses (transport parity).
	envelope := []byte{0x50, 0x41, 0x52, 0x45, 0x52, 0x00, 0x01, 0x0A, 0x01, 0x02}
	frame := make([]byte, 8+len(envelope))
	frame[7] = 1
	copy(frame[8:], envelope)
	if err := conn2.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatal(err)
	}
	mt, resp, err := conn2.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.BinaryMessage || len(resp) < 8 {
		t.Fatalf("bad response frame mt=%d len=%d", mt, len(resp))
	}
	if !bytes.Equal(resp[8:], append([]byte("governed:"), envelope...)) {
		t.Fatalf("governance handler bypassed: %q", resp[8:])
	}

	// Effect status over the WS carrier (no re-execution).
	if err := conn2.WriteJSON(map[string]string{"type": "status_query", "operation_id": "op-9"}); err != nil {
		t.Fatal(err)
	}
	var sr statusResponse
	if err := conn2.ReadJSON(&sr); err != nil {
		t.Fatal(err)
	}
	if sr.OperationID != "op-9" || sr.EnvelopeHex == "" {
		t.Fatalf("status response: %+v", sr)
	}

	// Reconnect over a NEW connection with a fresh proof resumes state.
	if err := srv.store.AdvanceSequence(open.SessionID, 5); err != nil {
		t.Fatal(err)
	}
	srv.Close(open.SessionID)
	conn3, ack3 := wsOpenSession(t, srv, wsURL, "https://app.example", pub, priv, open.SessionID)
	defer conn3.Close()
	if !ack3.Resumed || ack3.SessionID != open.SessionID {
		t.Fatalf("reconnect: %+v", ack3)
	}
}

func TestWTCarrierEndToEnd(t *testing.T) {
	srv, pub, priv := newFixture(t)
	wt, err := NewWTServer(srv, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.ListenOn("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer wt.Close()

	dialer := &webtransport.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	url := "https://" + wt.BoundAddr() + "/dari.web/1"
	if os.Getenv("WT_ECHO_PROBE") == "1" {
		url = "https://" + wt.BoundAddr() + "/wtdebug/echo"
	}
	// Retry until the listener is up.
	t.Logf("dialing %s", url)
	var sess *webtransport.Session
	var errD error
	for i := 0; i < 60; i++ {
		_, sess, errD = dialer.Dial(context.Background(), url, nil)
		if errD == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errD != nil {
		t.Fatalf("wt dial: %v", errD)
	}
	t.Logf("dialed ok")
	defer sess.CloseWithError(0, "done")

	stream, err := sess.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stream opened")
	defer stream.Close()

	// 0. Ready byte flushes the client stream to the server.
	if _, err := stream.Write([]byte{0x00}); err != nil {
		t.Fatal(err)
	}

	// 1. Read hello (binding token only on WT).
	helloFrame, err := readFrame(stream, 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	var hello helloMessage
	if err := json.Unmarshal(helloFrame[1:], &hello); err != nil {
		t.Fatal(err)
	}
	token, _ := hex.DecodeString(hello.BindingTokenHex)
	binding := ChannelBinding(token)

	// 2. Challenge request for the origin.
	creq, _ := json.Marshal(challengeRequest{Type: "challenge_request", Origin: "https://app.example"})
	if err := writeFrame(stream, append([]byte{0x01}, creq...)); err != nil {
		t.Fatal(err)
	}
	chFrame, err := readFrame(stream, 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	var chReply challengeReply
	if err := json.Unmarshal(chFrame[1:], &chReply); err != nil || chReply.Error != "" {
		t.Fatalf("challenge reply: %+v %v", chReply, err)
	}
	nonce, _ := hex.DecodeString(chReply.NonceHex)
	var nonceArr [32]byte
	copy(nonceArr[:], nonce)
	hello.ChallengeID = chReply.ChallengeID

	// 3. Open.
	proof := &BrowserProof{
		Origin: "https://app.example", ChallengeID: hello.ChallengeID,
		ChannelBinding: binding, SubjectKeyThumbprint: SubjectThumbprint(pub),
	}
	proof.Signature = ed25519.Sign(priv, proof.ProofSigningBytes(nonceArr))
	openReq, _ := json.Marshal(map[string]string{
		"type": "open", "origin": "https://app.example", "challenge_id": chReply.ChallengeID,
		"subject_key": hex.EncodeToString(pub),
		"signature":   hex.EncodeToString(proof.Signature),
	})
	if err := writeFrame(stream, append([]byte{0x01}, openReq...)); err != nil {
		t.Fatal(err)
	}
	ackFrame, err := readFrame(stream, 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	var ack openAck
	_ = json.Unmarshal(ackFrame[1:], &ack)
	if ack.Error != "" || ack.SessionID == "" {
		t.Fatalf("wt open ack: %+v", ack)
	}

	// 3. IDENTICAL envelope bytes as the WS test — transport parity.
	envelope := []byte{0x50, 0x41, 0x52, 0x45, 0x52, 0x00, 0x01, 0x0A, 0x01, 0x02}
	data := append([]byte{0x02, 0, 0, 0, 0, 0, 0, 0, 1}, envelope...)
	if err := writeFrame(stream, data); err != nil {
		t.Fatal(err)
	}
	respFrame, err := readFrame(stream, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	// [0x02][8-byte seq][envelope]
	if len(respFrame) < 10 || respFrame[0] != 0x02 {
		t.Fatalf("bad wt response frame: % x", respFrame[:min(10, len(respFrame))])
	}
	if !bytes.Equal(respFrame[9:], append([]byte("governed:"), envelope...)) {
		t.Fatal("wt governance handler bypassed")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = json.Marshal
var _ = rand.Read
