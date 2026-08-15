package pia

import (
	"path/filepath"
	"testing"
)

func TestKVJournalAppendAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kv.journal")
	j, err := OpenKVJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, err := j.Append("add", "h1", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("add", "h2", 20); err != nil {
		t.Fatal(err)
	}
	if j.Watermark() != 2 {
		t.Fatalf("watermark = %d, want 2", j.Watermark())
	}
	recs := j.Replay()
	if len(recs) != 2 || recs[0].Key != "h1" || recs[1].Key != "h2" {
		t.Fatalf("replay = %+v", recs)
	}
}

func TestKVJournalSurvivesReopen(t *testing.T) {
	// Spec §13.11: the journal is per-incarnation append-only with
	// sequence numbers — reopening resumes the sequence.
	path := filepath.Join(t.TempDir(), "kv.journal")
	j, err := OpenKVJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append("add", "h1", 10); err != nil {
		t.Fatal(err)
	}
	j.Close()

	j2, err := OpenKVJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	if j2.Watermark() != 1 {
		t.Fatalf("reopened watermark = %d, want 1 (sequence survives restart)", j2.Watermark())
	}
	seq, err := j2.Append("add", "h2", 20)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 2 {
		t.Fatalf("next seq = %d, want 2 (monotonic across incarnations)", seq)
	}
}
