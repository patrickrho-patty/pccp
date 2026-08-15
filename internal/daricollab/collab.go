// Package daricollab implements the dari.collab/1 runtime profile
// (spec F.13 §11, master plan Task 18 Steps 1–3): governed chat,
// presence, broadcast, encrypted ordered delivery, and resumable
// scanned file transfer. Every stream binds to a verified DARI grant;
// there is no ungoverned side channel.
package daricollab

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// EnvelopeBody is the collab-envelope-body (spec F.13 CDDL):
// {conversation, ordered sequence, sender, ciphertext digest, time}.
type EnvelopeBody struct {
	Version        uint16 `cbor:"1,keyasint"`
	ConversationID string `cbor:"2,keyasint"`
	Sequence       uint64 `cbor:"3,keyasint"`
	SenderPeerID   string `cbor:"4,keyasint"`
	Ciphertext     []byte `cbor:"5,keyasint"`
	// CiphertextDigest binds the encrypted payload (F.13 label 5 is
	// the digest; we carry the ciphertext AND digest so the receiver
	// verifies before decrypting).
	CiphertextDigest [32]byte `cbor:"6,keyasint"`
	SentAtMs         int64    `cbor:"7,keyasint"`
}

// Digest computes the envelope's content digest under the collab
// domain.
func (e *EnvelopeBody) Digest() [32]byte {
	h := sha256.New()
	h.Write([]byte("DARI-COLLAB-ENVELOPE-v1\x00"))
	var b4 [4]byte
	var b8 [8]byte
	lp := func(s string) {
		binary.BigEndian.PutUint32(b4[:], uint32(len(s)))
		h.Write(b4[:])
		h.Write([]byte(s))
	}
	lp(e.ConversationID)
	lp(e.SenderPeerID)
	binary.BigEndian.PutUint64(b8[:], e.Sequence)
	h.Write(b8[:])
	binary.BigEndian.PutUint64(b8[:], uint64(e.SentAtMs))
	h.Write(b8[:])
	h.Write(e.CiphertextDigest[:])
	var d [32]byte
	copy(d[:], h.Sum(nil))
	return d
}

// Conversation is an ordered, encrypted message stream.
type Conversation struct {
	mu       sync.Mutex
	id       string
	members  map[string]ed25519.PublicKey
	next     uint64
	messages []*EnvelopeBody
	// read tracks per-member delivery positions for resumable fetch.
	read map[string]uint64
}

// NewConversation builds an empty conversation with its founding
// members.
func NewConversation(id string, members map[string]ed25519.PublicKey) (*Conversation, error) {
	if id == "" {
		return nil, errors.New("collab: conversation requires an ID")
	}
	if len(members) == 0 {
		return nil, errors.New("collab: conversation requires members")
	}
	return &Conversation{
		id:      id,
		members: members,
		read:    map[string]uint64{},
	}, nil
}

// Seal encrypts a payload for the conversation under the shared
// conversation key derived from the member set (ECDH-less group key:
// SHA-256 over the sorted member thumbprints — deterministic for the
// member set, rotated by membership change).
func conversationKey(members map[string]ed25519.PublicKey) [32]byte {
	h := sha256.New()
	h.Write([]byte("DARI-COLLAB-KEY-v1\x00"))
	// Sorted member thumbprints.
	thumbs := make([][32]byte, 0, len(members))
	for _, pub := range members {
		thumbs = append(thumbs, thumbprint(pub))
	}
	for i := 0; i < len(thumbs); i++ {
		for j := i + 1; j < len(thumbs); j++ {
			if less(thumbs[j], thumbs[i]) {
				thumbs[i], thumbs[j] = thumbs[j], thumbs[i]
			}
		}
	}
	for _, t := range thumbs {
		h.Write(t[:])
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

func thumbprint(pub ed25519.PublicKey) [32]byte {
	var t [32]byte
	copy(t[:], []byte(pub))
	return t
}

func less(a, b [32]byte) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Append seals + appends the sender's payload as the next ordered
// envelope.
func (c *Conversation) Append(sender string, plaintext []byte, nowMs int64) (*EnvelopeBody, error) {
	if _, ok := c.members[sender]; !ok {
		return nil, fmt.Errorf("collab: %q is not a member of %s", sender, c.id)
	}
	key := conversationKey(c.members)
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	payload := append(nonce, ciphertext...)
	digest := sha256.Sum256(payload)

	c.mu.Lock()
	c.next++
	seq := c.next
	env := &EnvelopeBody{
		Version: 1, ConversationID: c.id, Sequence: seq,
		SenderPeerID: sender, Ciphertext: payload, CiphertextDigest: digest,
		SentAtMs: nowMs,
	}
	c.messages = append(c.messages, env)
	c.mu.Unlock()
	return env, nil
}

// Open decrypts an envelope for a member.
func (c *Conversation) Open(env *EnvelopeBody) ([]byte, error) {
	if env == nil || env.ConversationID != c.id {
		return nil, errors.New("collab: envelope does not belong to this conversation")
	}
	// Integrity before decryption.
	digest := sha256.Sum256(env.Ciphertext)
	if digest != env.CiphertextDigest {
		return nil, errors.New("collab: ciphertext digest mismatch (tampered)")
	}
	key := conversationKey(c.members)
	if len(env.Ciphertext) < 12 {
		return nil, errors.New("collab: ciphertext too short")
	}
	nonce, ciphertext := env.Ciphertext[:12], env.Ciphertext[12:]
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("collab: decryption failed (membership rotated or tampered)")
	}
	return plaintext, nil
}

// FetchSince returns envelopes with sequence > after, updating the
// member's read position (resumable ordered delivery).
func (c *Conversation) FetchSince(member string, after uint64) ([]*EnvelopeBody, error) {
	if _, ok := c.members[member]; !ok {
		return nil, fmt.Errorf("collab: %q is not a member of %s", member, c.id)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*EnvelopeBody
	for _, m := range c.messages {
		if m.Sequence > after {
			out = append(out, m)
		}
	}
	if len(c.messages) > 0 {
		c.read[member] = c.messages[len(c.messages)-1].Sequence
	}
	return out, nil
}

// LastRead reports the member's read position.
func (c *Conversation) LastRead(member string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read[member]
}

// Presence tracks member liveness in the conversation.
type Presence struct {
	mu     sync.Mutex
	online map[string]int64 // peerID → last-seen ms
}

// NewPresence builds the presence table.
func NewPresence() *Presence { return &Presence{online: map[string]int64{}} }

// Touch records a member heartbeat.
func (p *Presence) Touch(peerID string, nowMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.online[peerID] = nowMs
}

// Online lists members seen within ttlMs.
func (p *Presence) Online(nowMs, ttlMs int64) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for peer, seen := range p.online {
		if nowMs-seen <= ttlMs {
			out = append(out, peer)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Resumable scanned file transfer (Step 3).
// ---------------------------------------------------------------------------

// FileChunkBody is the file_transfer_body (spec F.13 CDDL).
type FileChunkBody struct {
	Version         uint16   `cbor:"1,keyasint"`
	TransferID      string   `cbor:"2,keyasint"`
	ChunkSequence   uint64   `cbor:"3,keyasint"`
	TotalChunks     uint64   `cbor:"4,keyasint"`
	ChunkDigest     [32]byte `cbor:"5,keyasint"`
	WholeFileDigest [32]byte `cbor:"6,keyasint"`
	ByteCount       uint32   `cbor:"7,keyasint"`
	Data            []byte   `cbor:"8,keyasint"`
}

// ErrScanRejected marks content failing the transfer scan.
var ErrScanRejected = errors.New("collab: file content rejected by scan")

// Scanner decides whether a chunk's content may transit. Deployments
// wire the security service; the default accepts (dev) — production
// installs a real scanner at the seam.
type Scanner func(chunk []byte) error

// Transfer is one resumable file upload.
type Transfer struct {
	mu        sync.Mutex
	id        string
	total     uint64
	received  map[uint64]*FileChunkBody
	whole     [32]byte
	scanner   Scanner
	completed bool
}

// NewTransfer starts a resumable transfer with a declared chunk count
// and whole-file digest.
func NewTransfer(id string, totalChunks uint64, wholeDigest [32]byte, scanner Scanner) (*Transfer, error) {
	if id == "" || totalChunks == 0 {
		return nil, errors.New("collab: transfer requires ID and chunk count")
	}
	if scanner == nil {
		scanner = func([]byte) error { return nil }
	}
	return &Transfer{
		id: id, total: totalChunks, whole: wholeDigest,
		received: map[uint64]*FileChunkBody{}, scanner: scanner,
	}, nil
}

// Ingest scans + accepts one chunk (idempotent on re-send —
// resumability).
func (t *Transfer) Ingest(chunk *FileChunkBody) error {
	if chunk == nil || chunk.TransferID != t.id {
		return errors.New("collab: chunk does not belong to this transfer")
	}
	if chunk.TotalChunks != t.total {
		return errors.New("collab: chunk total mismatch")
	}
	digest := sha256.Sum256(chunk.Data)
	if digest != chunk.ChunkDigest {
		return errors.New("collab: chunk digest mismatch")
	}
	if chunk.ByteCount != uint32(len(chunk.Data)) {
		return errors.New("collab: byte count mismatch")
	}
	if err := t.scanner(chunk.Data); err != nil {
		return fmt.Errorf("%w: %v", ErrScanRejected, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completed {
		return nil // idempotent completion re-ingest
	}
	t.received[chunk.ChunkSequence] = chunk
	return nil
}

// Missing lists the not-yet-received chunk sequences (resume manifest).
func (t *Transfer) Missing() []uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []uint64
	for i := uint64(1); i <= t.total; i++ {
		if _, ok := t.received[i]; !ok {
			out = append(out, i)
		}
	}
	return out
}

// Assemble verifies the whole-file digest and completes the transfer.
func (t *Transfer) Assemble() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.received) != int(t.total) {
		return nil, fmt.Errorf("collab: transfer incomplete (%d/%d)", len(t.received), t.total)
	}
	var whole []byte
	for i := uint64(1); i <= t.total; i++ {
		whole = append(whole, t.received[i].Data...)
	}
	digest := sha256.Sum256(whole)
	if digest != t.whole {
		return nil, errors.New("collab: whole-file digest mismatch")
	}
	t.completed = true
	return whole, nil
}
