// Package darimedia implements the dari.media/1 runtime profile
// (spec F.13 §11, master plan Task 18): governed live-media/voice
// streams with explicit authorization, ordered chunk delivery,
// cancellation, usage accounting, and terminal receipts. Media never
// creates an ungoverned side channel — every stream binds a session
// and a verified grant.
package darimedia

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

// ChunkBody is the media-chunk-body (spec F.13 CDDL): session,
// sequence, previous-chunk digest (chained ordering), encrypted chunk
// digest, duration.
type ChunkBody struct {
	Version         uint16   `cbor:"1,keyasint"`
	MediaSessionID  string   `cbor:"2,keyasint"`
	ChunkSequence   uint64   `cbor:"3,keyasint"`
	PrevChunkDigest [32]byte `cbor:"4,keyasint"`
	ChunkDigest     [32]byte `cbor:"5,keyasint"`
	DurationMs      uint32   `cbor:"6,keyasint"`
	Data            []byte   `cbor:"7,keyasint"`
}

// Receipt is the terminal media receipt (usage + cancellation state).
type Receipt struct {
	MediaSessionID string `json:"media_session_id"`
	Reason         string `json:"reason"` // completed | cancelled | failed
	Chunks         uint64 `json:"chunks"`
	DurationMs     uint64 `json:"duration_ms"`
	Bytes          uint64 `json:"bytes"`
}

// ErrNotAuthorized is the explicit-authorization boundary: media
// without a bound session grant is refused.
var ErrNotAuthorized = errors.New("media: session not authorized for media")

// Session is one governed live-media stream.
type Session struct {
	mu         sync.Mutex
	id         string
	authorized bool
	chunks     []*ChunkBody
	totalDur   uint64
	totalBytes uint64
	last       [32]byte // previous chunk digest (chain head)
	closed     bool
	receipt    *Receipt
	// onChunk observes delivery (usage accounting seam).
	onChunk func(*ChunkBody)
}

// NewSession opens a media session. authorized MUST reflect a verified
// grant carrying the media capability; false fails closed.
func NewSession(id string, authorized bool) (*Session, error) {
	if id == "" {
		return nil, errors.New("media: session requires an ID")
	}
	if !authorized {
		return nil, ErrNotAuthorized
	}
	return &Session{id: id, authorized: true}, nil
}

// Append validates + delivers the next ordered chunk.
func (s *Session) Append(chunk *ChunkBody) error {
	if chunk == nil || chunk.MediaSessionID != s.id {
		return errors.New("media: chunk does not belong to this session")
	}
	digest := sha256.Sum256(chunk.Data)
	if digest != chunk.ChunkDigest {
		return errors.New("media: chunk digest mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("media: session closed")
	}
	// Chained ordering: chunk N binds chunk N-1's digest.
	wantSeq := uint64(len(s.chunks)) + 1
	if chunk.ChunkSequence != wantSeq {
		return fmt.Errorf("media: sequence gap — want %d, got %d", wantSeq, chunk.ChunkSequence)
	}
	if len(s.chunks) > 0 {
		if chunk.PrevChunkDigest != s.last {
			return errors.New("media: previous-chunk digest mismatch (out of order or tampered)")
		}
	} else if chunk.PrevChunkDigest != [32]byte{} {
		return errors.New("media: first chunk must carry a zero previous digest")
	}
	s.chunks = append(s.chunks, chunk)
	s.last = digest
	s.totalDur += uint64(chunk.DurationMs)
	s.totalBytes += uint64(len(chunk.Data))
	if s.onChunk != nil {
		s.onChunk(chunk)
	}
	return nil
}

// OnChunk installs the usage-accounting observer.
func (s *Session) OnChunk(fn func(*ChunkBody)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChunk = fn
}

// Close terminates the stream and issues the terminal receipt.
func (s *Session) Close(reason string) *Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.receipt
	}
	s.closed = true
	s.receipt = &Receipt{
		MediaSessionID: s.id,
		Reason:         reason,
		Chunks:         uint64(len(s.chunks)),
		DurationMs:     s.totalDur,
		Bytes:          s.totalBytes,
	}
	return s.receipt
}

// Receipt returns the terminal receipt (nil while open).
func (s *Session) Receipt() *Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil {
		return nil
	}
	cp := *s.receipt
	return &cp
}

// SessionRegistry tracks live media sessions per governed session.
type SessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSessionRegistry builds the registry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: map[string]*Session{}}

}

// Open creates (or resumes) a media session under explicit
// authorization.
func (r *SessionRegistry) Open(id string, authorized bool) (*Session, error) {
	s, err := NewSession(id, authorized)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = s
	return s, nil
}

// Get fetches a live session.
func (r *SessionRegistry) Get(id string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Cancel closes every live session with a cancelled receipt (the
// cancellation path).
func (r *SessionRegistry) Cancel() []*Receipt {
	r.mu.Lock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	var out []*Receipt
	for _, id := range ids {
		r.mu.Lock()
		s := r.sessions[id]
		r.mu.Unlock()
		if s != nil {
			if rc := s.Close("cancelled"); rc != nil {
				out = append(out, rc)
			}
		}
	}
	return out
}
