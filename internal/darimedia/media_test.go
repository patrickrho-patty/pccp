package darimedia

import (
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

// media_test.go implements the Task 18 media vectors: explicit
// authorization, ordered chained delivery, digest rejection, sequence
// gaps, cancellation receipts, usage accounting.

func chunk(sessionID string, seq uint64, prev [32]byte, data []byte, dur uint32) *ChunkBody {
	return &ChunkBody{
		Version: 1, MediaSessionID: sessionID, ChunkSequence: seq,
		PrevChunkDigest: prev, ChunkDigest: sha256.Sum256(data),
		DurationMs: dur, Data: data,
	}
}

func TestMediaRequiresAuthorization(t *testing.T) {
	if _, err := NewSession("m-1", false); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("expected authorization refusal, got %v", err)
	}
	if _, err := NewSession("", true); err == nil {
		t.Fatal("empty session ID accepted")
	}
}

func TestOrderedChainedDelivery(t *testing.T) {
	s, err := NewSession("m-2", true)
	if err != nil {
		t.Fatal(err)
	}
	c1 := chunk("m-2", 1, [32]byte{}, []byte("aaa"), 20)
	if err := s.Append(c1); err != nil {
		t.Fatal(err)
	}
	c2 := chunk("m-2", 2, c1.ChunkDigest, []byte("bbb"), 30)
	if err := s.Append(c2); err != nil {
		t.Fatal(err)
	}
	// Sequence gap rejected.
	c4 := chunk("m-2", 4, c2.ChunkDigest, []byte("ddd"), 10)
	if err := s.Append(c4); err == nil || !strings.Contains(err.Error(), "sequence gap") {
		t.Fatalf("expected gap rejection, got %v", err)
	}
	// Broken chain (wrong prev digest) rejected.
	c3bad := chunk("m-2", 3, [32]byte{9}, []byte("ccc"), 10)
	if err := s.Append(c3bad); err == nil || !strings.Contains(err.Error(), "previous-chunk") {
		t.Fatalf("expected chain rejection, got %v", err)
	}
	// Digest mismatch rejected.
	c3 := chunk("m-2", 3, c2.ChunkDigest, []byte("ccc"), 10)
	c3.Data[0] ^= 0xFF
	if err := s.Append(c3); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected digest rejection, got %v", err)
	}
}

func TestTerminalReceiptAndUsage(t *testing.T) {
	s, _ := NewSession("m-3", true)

	var observed int
	s.OnChunk(func(*ChunkBody) { observed++ })
	c1 := chunk("m-3", 1, [32]byte{}, []byte("aaaa"), 100)
	c2 := chunk("m-3", 2, c1.ChunkDigest, []byte("bbbb"), 200)
	_ = s.Append(c1)
	_ = s.Append(c2)
	if observed != 2 {
		t.Fatalf("observer saw %d chunks", observed)
	}
	if s.Receipt() != nil {
		t.Fatal("receipt issued before close")
	}
	rc := s.Close("completed")
	if rc.Chunks != 2 || rc.DurationMs != 300 || rc.Bytes != 8 || rc.Reason != "completed" {
		t.Fatalf("receipt = %+v", rc)
	}
	// Idempotent close.
	if s.Close("again") != rc {
		t.Fatal("close not idempotent")
	}
	// Chunks after close refused.
	if err := s.Append(chunk("m-3", 3, c2.ChunkDigest, []byte("x"), 1)); err == nil {
		t.Fatal("post-close chunk accepted")
	}
}

func TestRegistryCancellation(t *testing.T) {
	r := NewSessionRegistry()
	s1, _ := r.Open("a", true)
	s2, _ := r.Open("b", true)
	_ = s1.Append(chunk("a", 1, [32]byte{}, []byte("x"), 10))
	_ = s2.Append(chunk("b", 1, [32]byte{}, []byte("y"), 10))

	receipts := r.Cancel()
	if len(receipts) != 2 {
		t.Fatalf("receipts = %d", len(receipts))
	}
	for _, rc := range receipts {
		if rc.Reason != "cancelled" {
			t.Fatalf("reason = %s", rc.Reason)
		}
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("registry dropped the session on cancel")
	}
}
