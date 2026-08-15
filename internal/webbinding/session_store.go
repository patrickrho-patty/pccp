package webbinding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// session_store.go holds the durable browser-session state (map §10):
// origin, subject-key thumbprint, last sequence, grant digest, and
// effect operation IDs — the state reconnect resumes from, persisted
// at the database/cache boundary.

// BrowserSession is the durable state of one browser session.
type BrowserSession struct {
	SessionID    string   `json:"session_id"`
	Origin       string   `json:"origin"`
	Site         string   `json:"top_level_site"`
	SubjectThumb [32]byte `json:"subject_key_thumbprint"`
	GrantDigest  [32]byte `json:"grant_digest,omitempty"`
	LastSequence uint64   `json:"last_sequence"`
	EffectOpIDs  []string `json:"effect_operation_ids,omitempty"`
	CreatedAtMs  int64    `json:"created_at_ms"`
	LastSeenMs   int64    `json:"last_seen_ms"`
	Connected    bool     `json:"connected"`
}

// SessionStore is the durable session store. In-memory map with an
// optional JSON snapshot path (the deployments mount a volume there);
// every mutation snapshots so a restart resumes sessions.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*BrowserSession
	path     string // "" = memory only
}

// NewSessionStore builds a store; path "" keeps it in memory.
func NewSessionStore(path string) (*SessionStore, error) {
	s := &SessionStore{sessions: map[string]*BrowserSession{}, path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, &s.sessions); err != nil {
				return nil, fmt.Errorf("webbinding: corrupt session store %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return s, nil
}

func (s *SessionStore) persistLocked() {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.sessions)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o600)
}

// Create registers a new session.
func (s *SessionStore) Create(sess *BrowserSession) error {
	if sess.SessionID == "" {
		return errors.New("webbinding: empty session id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sess.SessionID]; exists {
		return fmt.Errorf("webbinding: session %s already exists", sess.SessionID)
	}
	cp := *sess
	s.sessions[sess.SessionID] = &cp
	s.persistLocked()
	return nil
}

// Get returns a copy of the session state.
func (s *SessionStore) Get(sessionID string) (BrowserSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return BrowserSession{}, false
	}
	return *sess, true
}

// Update mutates a session atomically under the store lock.
func (s *SessionStore) Update(sessionID string, fn func(*BrowserSession) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("webbinding: unknown session %s", sessionID)
	}
	if err := fn(sess); err != nil {
		return err
	}
	s.persistLocked()
	return nil
}

// RecordEffect appends an effect operation ID (idempotent).
func (s *SessionStore) RecordEffect(sessionID, opID string) error {
	return s.Update(sessionID, func(b *BrowserSession) error {
		for _, id := range b.EffectOpIDs {
			if id == opID {
				return nil
			}
		}
		b.EffectOpIDs = append(b.EffectOpIDs, opID)
		return nil
	})
}

// AdvanceSequence moves last_sequence forward; regressions rejected.
func (s *SessionStore) AdvanceSequence(sessionID string, seq uint64) error {
	return s.Update(sessionID, func(b *BrowserSession) error {
		if seq < b.LastSequence {
			return fmt.Errorf("webbinding: sequence regression %d < %d", seq, b.LastSequence)
		}
		b.LastSequence = seq
		return nil
	})
}

// ExpireIdle removes sessions idle longer than ttl and returns their IDs.
func (s *SessionStore) ExpireIdle(nowMs int64, ttl time.Duration) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []string
	for id, sess := range s.sessions {
		if nowMs-sess.LastSeenMs > ttl.Milliseconds() {
			expired = append(expired, id)
			delete(s.sessions, id)
		}
	}
	sort.Strings(expired)
	if len(expired) > 0 {
		s.persistLocked()
	}
	return expired
}

// Count reports the number of live sessions.
func (s *SessionStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
