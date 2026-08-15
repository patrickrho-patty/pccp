package webbinding

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// server.go is the dari.web/1 WebBindingServer: origin policy,
// one-use challenges, proof-gated open/reconnect, bounded send queues,
// per-origin rate limits, idle expiry, and the metrics the map
// requires (proof failures, reconnect conflicts, backpressure).

// GovernanceHandler processes one canonical DARI record envelope and
// returns the response envelope — the relay installs the SAME governed
// path (auth → grant → decision → effect → receipt) the native
// transport uses. The web carrier never bypasses it.
type GovernanceHandler func(sessionID string, envelope []byte) ([]byte, error)

// Metrics are the observable health signals (map §10).
type Metrics struct {
	ProofFailures      uint64
	ReconnectConflicts uint64
	BackpressureDrops  uint64
	OpenSessions       uint64
	RateLimited        uint64
}

// Server is the web binding server.
type Server struct {
	mu         sync.Mutex
	store      *SessionStore
	origins    map[string]bool // exact origins allowed by org policy
	challenges map[string]*Challenge
	handler    GovernanceHandler
	queueCap   int
	sendQueue  chan []byte
	rates      map[string]*tokenBucket // per-origin
	ratePerMin int
	idleTTL    time.Duration
	metrics    Metrics
	now        func() time.Time
	// challengeSeq / sessionSeq disambiguate same-nanosecond IDs.
	challengeSeq uint64
	sessionSeq   uint64
}

// NewServer builds a server. origins is the exact allowlist and MUST
// be non-empty — an unconfigured carrier fails closed (deny-all), it
// never defaults to allow-every-origin.
func NewServer(store *SessionStore, origins []string, handler GovernanceHandler) (*Server, error) {
	if store == nil {
		return nil, errors.New("webbinding: nil session store")
	}
	if len(origins) == 0 {
		return nil, errors.New("webbinding: origin allowlist must not be empty (fail-closed)")
	}
	allowed := map[string]bool{}
	for _, o := range origins {
		norm, err := NormalizeOrigin(o)
		if err != nil {
			return nil, fmt.Errorf("webbinding: bad allowed origin %q: %w", o, err)
		}
		allowed[norm.String()] = true
	}
	return &Server{
		store:      store,
		origins:    allowed,
		challenges: map[string]*Challenge{},
		handler:    handler,
		queueCap:   256,
		rates:      map[string]*tokenBucket{},
		ratePerMin: 120,
		idleTTL:    30 * time.Minute,
		now:        time.Now,
	}, nil
}

// Metrics returns a snapshot.
func (s *Server) Metrics() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.metrics
	m.OpenSessions = uint64(s.store.Count())
	return m
}

// tokenBucket is a per-origin limiter.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (s *Server) allowRate(origin string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	b, ok := s.rates[origin]
	if !ok {
		b = &tokenBucket{tokens: float64(s.ratePerMin), last: now}
		s.rates[origin] = b
	}
	// Refill by elapsed minutes.
	elapsed := now.Sub(b.last).Minutes()
	b.tokens += elapsed * float64(s.ratePerMin)
	if b.tokens > float64(s.ratePerMin) {
		b.tokens = float64(s.ratePerMin)
	}
	b.last = now
	if b.tokens < 1 {
		s.metrics.RateLimited++
		return false
	}
	b.tokens--
	return true
}

// IssueChallenge mints a one-use challenge for an allowed origin.
func (s *Server) IssueChallenge(origin string) (*Challenge, error) {
	norm, err := NormalizeOrigin(origin)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.origins[norm.String()] {
		return nil, fmt.Errorf("webbinding: origin %q not allowed by policy", norm.String())
	}
	now := s.now()
	// Prune expired challenges (abandoned tabs / floods must not leak).
	for id, ch := range s.challenges {
		if now.After(ch.ExpiresAt) {
			delete(s.challenges, id)
		}
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	s.challengeSeq++
	ch := &Challenge{
		ID:        fmt.Sprintf("ch-%d-%d", s.now().UnixNano(), s.challengeSeq),
		Nonce:     nonce,
		Origin:    norm.String(),
		ExpiresAt: s.now().Add(2 * time.Minute),
	}
	s.challenges[ch.ID] = ch
	return ch, nil
}

// OpenRequest opens (or reconnects) a browser session.
type OpenRequest struct {
	Origin         string
	Cookie         string // tolerated but never sufficient
	Bearer         string // tolerated but never sufficient
	Proof          *BrowserProof
	SubjectKey     ed25519.PublicKey
	ChannelBinding [32]byte
	GrantDigest    [32]byte
}

// Open validates the binding and proof, then creates or resumes the
// session. Cookie/bearer WITHOUT a valid proof fails with
// ErrMissingProofOfPossession — no alternate authorization exists.
func (s *Server) Open(req OpenRequest) (BrowserSession, bool, error) {
	norm, err := NormalizeOrigin(req.Origin)
	if err != nil {
		return BrowserSession{}, false, err
	}
	if len(s.origins) > 0 && !s.origins[norm.String()] {
		return BrowserSession{}, false, fmt.Errorf("webbinding: origin %q not allowed by policy", norm.String())
	}
	if !s.allowRate(norm.String()) {
		return BrowserSession{}, false, errors.New("webbinding: rate limited")
	}
	if req.Proof == nil {
		s.mu.Lock()
		s.metrics.ProofFailures++
		s.mu.Unlock()
		return BrowserSession{}, false, ErrMissingProofOfPossession
	}
	// Pop the challenge ATOMICALLY before verification: a concurrent
	// replay of the same captured proof finds it already gone (the
	// one-use guarantee holds under race).
	s.mu.Lock()
	ch := s.challenges[req.Proof.ChallengeID]
	if ch != nil {
		delete(s.challenges, ch.ID)
	}
	s.mu.Unlock()
	if err := VerifyBrowserProof(req.Proof, req.SubjectKey, norm.String(), req.ChannelBinding, ch); err != nil {
		s.mu.Lock()
		s.metrics.ProofFailures++
		s.mu.Unlock()
		return BrowserSession{}, false, err
	}

	nowMs := s.now().UnixMilli()
	if req.Proof.ReconnectSessionID != "" {
		// Reconnect: the session must exist, share the origin and the
		// SAME subject key; a different key is a reconnect conflict.
		sess, ok := s.store.Get(req.Proof.ReconnectSessionID)
		if !ok {
			s.mu.Lock()
			s.metrics.ReconnectConflicts++
			s.mu.Unlock()
			return BrowserSession{}, false, fmt.Errorf("webbinding: unknown reconnect session")
		}
		if sess.Origin != norm.String() {
			s.mu.Lock()
			s.metrics.ReconnectConflicts++
			s.mu.Unlock()
			return BrowserSession{}, false, errors.New("webbinding: reconnect origin changed")
		}
		if sess.SubjectThumb != req.Proof.SubjectKeyThumbprint {
			s.mu.Lock()
			s.metrics.ReconnectConflicts++
			s.mu.Unlock()
			return BrowserSession{}, false, errors.New("webbinding: reconnect subject key changed")
		}
		if err := s.store.Update(sess.SessionID, func(b *BrowserSession) error {
			b.Connected = true
			b.LastSeenMs = nowMs
			if req.GrantDigest != ([32]byte{}) {
				b.GrantDigest = req.GrantDigest
			}
			return nil
		}); err != nil {
			return BrowserSession{}, false, err
		}
		out, _ := s.store.Get(sess.SessionID)
		return out, true, nil
	}

	// Fresh open.
	s.sessionSeq++
	sess := BrowserSession{
		SessionID:    fmt.Sprintf("ws-%d-%d", s.now().UnixNano(), s.sessionSeq),
		Origin:       norm.String(),
		Site:         norm.TopLevelSite(),
		SubjectThumb: req.Proof.SubjectKeyThumbprint,
		GrantDigest:  req.GrantDigest,
		CreatedAtMs:  nowMs,
		LastSeenMs:   nowMs,
		Connected:    true,
	}
	if err := s.store.Create(&sess); err != nil {
		return BrowserSession{}, false, err
	}
	return sess, false, nil
}

// Deliver sends one envelope toward the browser through the bounded
// queue; overflow sheds (counted) rather than blocks the relay path.
func (s *Server) Deliver(sessionID string, envelope []byte) error {
	s.mu.Lock()
	q := s.sendQueue
	s.mu.Unlock()
	if q == nil {
		return nil
	}
	select {
	case q <- envelope:
		return nil
	default:
		s.mu.Lock()
		s.metrics.BackpressureDrops++
		s.mu.Unlock()
		return errors.New("webbinding: send queue full — envelope shed")
	}
}

// Process routes one inbound canonical envelope through the governed
// handler and advances the session sequence.
func (s *Server) Process(sessionID string, sequence uint64, envelope []byte) ([]byte, error) {
	if s.handler == nil {
		return nil, errors.New("webbinding: no governance handler installed")
	}
	if err := s.store.AdvanceSequence(sessionID, sequence); err != nil {
		return nil, err
	}
	resp, err := s.handler(sessionID, envelope)
	if err != nil {
		return nil, err
	}
	_ = s.store.Update(sessionID, func(b *BrowserSession) error {
		b.LastSeenMs = s.now().UnixMilli()
		return nil
	})
	return resp, nil
}

// StatusQuery carries the map §10 reconnect rule: after an uncertain
// disconnect the caller queries EFFECT_STATUS; it never re-executes.
// The handler resolves the durable status; the session records the op.
func (s *Server) StatusQuery(sessionID, operationID string, status func(opID string) ([]byte, error)) ([]byte, error) {
	if err := s.store.RecordEffect(sessionID, operationID); err != nil {
		return nil, err
	}
	return status(operationID)
}

// Close marks a session disconnected (state persists for reconnect).
func (s *Server) Close(sessionID string) {
	_ = s.store.Update(sessionID, func(b *BrowserSession) error {
		b.Connected = false
		b.LastSeenMs = s.now().UnixMilli()
		return nil
	})
}

// ExpireIdle sweeps idle sessions.
func (s *Server) ExpireIdle() int {
	return len(s.store.ExpireIdle(s.now().UnixMilli(), s.idleTTL))
}

// VerifyWebOrigin exposes the origin-policy check for carriers.
func (s *Server) VerifyWebOrigin(origin string) error {
	norm, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.origins[norm.String()] {
		return fmt.Errorf("webbinding: origin %q not allowed", norm.String())
	}
	return nil
}

// BeginConnection mints the fused challenge + per-connection binding
// token every carrier sends as its first message. One-use, expiring,
// origin-checked; the binding token is unique per connection so proofs
// are channel-bound by construction.
func (s *Server) BeginConnection(expectedOrigin string) (*Challenge, [32]byte, error) {
	norm, err := NormalizeOrigin(expectedOrigin)
	if err != nil {
		return nil, [32]byte{}, err
	}
	ch, err := s.IssueChallenge(norm.String())
	if err != nil {
		return nil, [32]byte{}, err
	}
	token := NewBindingToken()
	return ch, token, nil
}

// SetRateLimit adjusts the per-origin open rate (operations may raise
// it for trusted first-party origins).
func (s *Server) SetRateLimit(perMinute int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ratePerMin = perMinute
	s.rates = map[string]*tokenBucket{}
}
