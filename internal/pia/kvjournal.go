package pia

import (
	"encoding/json"
	"os"
	"sync"
)

// kvjournal.go implements spec §13.11: the PIA owns the KV event broker
// with a per-incarnation append-only journal and sequence numbers. On
// engine restart the journal replays to the scheduler, which dedups by
// (worker, seq) — the fleet's cache map survives engine crashes
// (Dynamo's does not).

// KVJournalRecord is one journaled cache-block event.
type KVJournalRecord struct {
	Seq    uint64 `json:"seq"`
	Action string `json:"action"` // "add" | "evict"
	Key    string `json:"key"`    // block hash
	Tokens int    `json:"tokens"`
}

// KVJournal is the append-only per-incarnation journal.
type KVJournal struct {
	mu     sync.Mutex
	path   string
	seq    uint64
	closed bool
}

// OpenKVJournal opens (or creates) the journal file at path and resumes
// the sequence from the last record — replays survive restarts.
func OpenKVJournal(path string) (*KVJournal, error) {
	j := &KVJournal{path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return j, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return j, nil
	}
	var recs []KVJournalRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		// A corrupt journal must not take down the PIA: start a fresh
		// incarnation (the scheduler's dedup watermark tolerates it).
		return j, nil
	}
	for _, r := range recs {
		if r.Seq > j.seq {
			j.seq = r.Seq
		}
	}
	return j, nil
}

// Append records one event and returns its sequence number.
func (j *KVJournal) Append(action, key string, tokens int) (uint64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return 0, os.ErrClosed
	}
	j.seq++
	rec := KVJournalRecord{Seq: j.seq, Action: action, Key: key, Tokens: tokens}

	// Append-only: read existing records, append, write back. Records are
	// small and bounded; production hosts journal to a tmpfs-backed file.
	var recs []KVJournalRecord
	if raw, err := os.ReadFile(j.path); err == nil && len(raw) > 0 {
		json.Unmarshal(raw, &recs)
	}
	recs = append(recs, rec)
	out, err := json.Marshal(recs)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(j.path, out, 0o600); err != nil {
		return 0, err
	}
	return rec.Seq, nil
}

// Replay returns every recorded event in sequence order.
func (j *KVJournal) Replay() []KVJournalRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, err := os.ReadFile(j.path)
	if err != nil {
		return nil
	}
	var recs []KVJournalRecord
	json.Unmarshal(raw, &recs)
	return recs
}

// Watermark returns the highest recorded sequence.
func (j *KVJournal) Watermark() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.seq
}

// Close marks the journal closed (no further appends).
func (j *KVJournal) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.closed = true
}
