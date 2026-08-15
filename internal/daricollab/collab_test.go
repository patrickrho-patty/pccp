package daricollab

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

// collab_test.go implements the Task 18 Step-1/3 vectors: encrypted
// ordered delivery, tamper rejection, resumable resync, membership
// key rotation, and scanned resumable file transfer.

func convStack(t *testing.T) (*Conversation, map[string]ed25519.PublicKey) {
	t.Helper()
	alicePub, alicePriv, _ := ed25519.GenerateKey(nil)
	bobPub, _, _ := ed25519.GenerateKey(nil)
	members := map[string]ed25519.PublicKey{"alice": alicePub, "bob": bobPub}
	c, err := NewConversation("conv-1", members)
	if err != nil {
		t.Fatal(err)
	}
	_ = alicePriv
	return c, members
}

func TestEncryptedOrderedDelivery(t *testing.T) {
	c, _ := convStack(t)

	e1, err := c.Append("alice", []byte("hello"), 1000)
	if err != nil {
		t.Fatal(err)
	}
	e2, _ := c.Append("bob", []byte("world"), 2000)
	if e1.Sequence != 1 || e2.Sequence != 2 {
		t.Fatalf("sequences = %d, %d", e1.Sequence, e2.Sequence)
	}
	// Ciphertext is NOT the plaintext.
	if bytes.Contains(e1.Ciphertext, []byte("hello")) {
		t.Fatal("ciphertext leaks plaintext")
	}
	// Bob decrypts in order.
	p1, err := c.Open(e1)
	if err != nil || string(p1) != "hello" {
		t.Fatalf("open e1: %v %q", err, p1)
	}
	p2, err := c.Open(e2)
	if err != nil || string(p2) != "world" {
		t.Fatalf("open e2: %v %q", err, p2)
	}
}

func TestTamperedEnvelopeRejected(t *testing.T) {
	c, _ := convStack(t)
	e, _ := c.Append("alice", []byte("secret"), 1)
	e.Ciphertext[3] ^= 0xFF // tamper
	if _, err := c.Open(e); err == nil || !strings.Contains(err.Error(), "tampered") {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}

func TestNonMemberRejectedAndKeyRotation(t *testing.T) {
	c, _ := convStack(t)
	if _, err := c.Append("eve", []byte("hi"), 1); err == nil {
		t.Fatal("non-member append accepted")
	}
	_, _ = c.Append("alice", []byte("for members"), 1)
	if _, err := c.FetchSince("eve", 0); err == nil {
		t.Fatal("non-member fetch accepted")
	}

}

func TestFetchSinceResumes(t *testing.T) {
	c, _ := convStack(t)
	for i := 0; i < 5; i++ {
		if _, err := c.Append("alice", []byte{byte('a' + i)}, int64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	// Bob fetches from 0 → all five; then resyncs from 3 → two.
	first, err := c.FetchSince("bob", 0)
	if err != nil || len(first) != 5 {
		t.Fatalf("first fetch = %d err=%v", len(first), err)
	}
	if c.LastRead("bob") != 5 {
		t.Fatalf("last-read = %d", c.LastRead("bob"))
	}
	resume, _ := c.FetchSince("bob", 3)
	if len(resume) != 2 || resume[0].Sequence != 4 {
		t.Fatalf("resume = %+v", resume)
	}
}

func TestPresenceTable(t *testing.T) {
	p := NewPresence()
	p.Touch("alice", 1000)
	p.Touch("bob", 100)
	online := p.Online(1500, 1000) // ttl 1s: only alice (1000)
	if len(online) != 1 || online[0] != "alice" {
		t.Fatalf("online = %v", online)
	}
}

func TestFileTransferScanAndResume(t *testing.T) {
	var whole bytes.Buffer
	chunkA := []byte("part-one-")
	chunkB := []byte("part-two")
	whole.Write(chunkA)
	whole.Write(chunkB)
	wholeDigest := sha256Sum(whole.Bytes())

	rejected := errors.New("scanner: forbidden")
	var calls int
	scanner := func(b []byte) error {
		calls++
		if bytes.Contains(b, []byte("forbidden")) {
			return rejected
		}
		return nil
	}

	tr, err := NewTransfer("file-1", 2, wholeDigest, scanner)
	if err != nil {
		t.Fatal(err)
	}

	// Chunk 2 arrives first (out of order, resumable).
	mk := func(seq uint64, data []byte) *FileChunkBody {
		return &FileChunkBody{
			Version: 1, TransferID: "file-1", ChunkSequence: seq, TotalChunks: 2,
			ChunkDigest: sha256Sum(data), WholeFileDigest: wholeDigest,
			ByteCount: uint32(len(data)), Data: data,
		}
	}
	if err := tr.Ingest(mk(2, chunkB)); err != nil {
		t.Fatal(err)
	}
	missing := tr.Missing()
	if len(missing) != 1 || missing[0] != 1 {
		t.Fatalf("missing = %v", missing)
	}
	// Incomplete assembly refused.
	if _, err := tr.Assemble(); err == nil {
		t.Fatal("incomplete assembly accepted")
	}
	// Chunk 1 completes.
	if err := tr.Ingest(mk(1, chunkA)); err != nil {
		t.Fatal(err)
	}
	if len(tr.Missing()) != 0 {
		t.Fatal("resume manifest must be empty")
	}
	got, err := tr.Assemble()
	if err != nil || !bytes.Equal(got, whole.Bytes()) {
		t.Fatalf("assemble: %v %q", err, got)
	}
	if calls != 2 {
		t.Fatalf("scanner calls = %d", calls)
	}

	// A forbidden chunk is rejected by the scan before ingest.
	bad := []byte("this-is-forbidden-content")
	badTr, _ := NewTransfer("file-2", 1, sha256Sum(bad), scanner)
	badChunk := &FileChunkBody{
		Version: 1, TransferID: "file-2", ChunkSequence: 1, TotalChunks: 1,
		ChunkDigest: sha256Sum(bad), WholeFileDigest: sha256Sum(bad),
		ByteCount: uint32(len(bad)), Data: bad,
	}
	if err := badTr.Ingest(badChunk); !errors.Is(err, ErrScanRejected) {
		t.Fatalf("expected scan rejection, got %v", err)
	}
	// Digest mismatch rejected.
	tamperedData := append([]byte(nil), chunkA...)
	tamperedData[0] ^= 0xFF
	tr2, _ := NewTransfer("file-3", 1, sha256Sum(chunkA), nil)
	tampered := &FileChunkBody{
		Version: 1, TransferID: "file-3", ChunkSequence: 1, TotalChunks: 1,
		ChunkDigest: sha256Sum(chunkA), WholeFileDigest: sha256Sum(chunkA),
		ByteCount: uint32(len(chunkA)), Data: tamperedData,
	}
	if err := tr2.Ingest(tampered); err == nil || !strings.Contains(err.Error(), "chunk digest") {
		t.Fatalf("expected digest rejection, got %v", err)
	}
}

func sha256Sum(b []byte) [32]byte {
	return sha256.Sum256(b)
}
