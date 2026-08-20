package scheduler

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestEnvelopeProgramMetadataSigned(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	env := NewTrafficEnvelope("req-1", "tenant-1", "interactive-paid", time.Minute)
	env.SetProgram(ProgramMeta{
		ProgramID: "prog-9", TurnSeq: 3, ParentID: "prog-1",
		ToolPaused: true, TaskSLOMs: 45000,
	})
	if err := env.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := env.Verify(pub); err != nil {
		t.Fatalf("signed program envelope must verify: %v", err)
	}
	// Tampering with program metadata invalidates the signature.
	env.Program.ProgramID = "prog-evil"
	if err := env.Verify(pub); err == nil {
		t.Fatal("tampered program metadata must fail verification")
	}
	// Envelopes without program metadata still verify (v2 optional).
	plain := NewTrafficEnvelope("req-2", "tenant-1", "batch", time.Minute)
	if err := plain.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := plain.Verify(pub); err != nil {
		t.Fatalf("plain envelope must verify under v2: %v", err)
	}
}

func TestProgramRegistryPauseLifecycle(t *testing.T) {
	dir := NewKVDirectory()
	dir.SetNow(func() int64 { return 0 })
	dir.Add("w1", L1GPU, dirBlock("tenant-a", "ph", 1000), testIdentity, true)

	r := NewProgramRegistry(dir)
	now := time.Now()
	r.SetNow(func() time.Time { return now })

	id := testIdentity
	// First turn: registers continuity and the program's cache key.
	if act := r.Turn("p1", "tenant-a", "ph", id, "w1", 1); act != KVActionNone {
		t.Fatalf("first turn action = %s", act)
	}
	// First pause, no history: retain (short/unknown estimate).
	if act := r.ToolPaused("p1"); act != KVActionRetain {
		t.Fatalf("first pause = %s, want retain", act)
	}
	if !r.Paused("p1") {
		t.Fatal("program must be paused")
	}
	// Short pause, continuation: prefetch back + first calibration sample.
	now = now.Add(2 * time.Second)
	if act := r.Turn("p1", "tenant-a", "ph", id, "w1", 2); act != KVActionPrefetch {
		t.Fatalf("continuation = %s, want prefetch", act)
	}
	if r.Paused("p1") {
		t.Fatal("continuation must clear the pause")
	}
	// Teach a long pause estimate with repeated 60s pauses (one long
	// pause is a blip; the bounded estimate needs evidence to cross the
	// retain/demote threshold).
	r.ToolPaused("p1")
	now = now.Add(60 * time.Second)
	r.Turn("p1", "tenant-a", "ph", id, "w1", 3)
	r.ToolPaused("p1")
	now = now.Add(60 * time.Second)
	r.Turn("p1", "tenant-a", "ph", id, "w1", 4)
	// Now the estimate is long: the next pause must demote HBM → L2.
	if act := r.ToolPaused("p1"); act != KVActionDemote {
		t.Fatalf("long-estimated pause = %s, want demote", act)
	}
	locs := dir.Locations("tenant-a", "ph", id)
	if len(locs) != 1 || locs[0].Tier != L2CPU {
		t.Fatalf("demoted residence = %+v, want L2", locs)
	}
	// Continuation restores to L1.
	now = now.Add(5 * time.Second)
	if act := r.Turn("p1", "tenant-a", "ph", id, "w1", 5); act != KVActionPrefetch {
		t.Fatalf("continuation after demote = %s, want prefetch", act)
	}
	locs = dir.Locations("tenant-a", "ph", id)
	if locs[0].Tier != L1GPU {
		t.Fatalf("restored residence = %+v, want L1", locs)
	}
}

func TestProgramRegistryCalibrationCounter(t *testing.T) {
	r := NewProgramRegistry(nil)
	now := time.Now()
	r.SetNow(func() time.Time { return now })
	// Establish a long estimate: pause 60s, continue.
	r.Turn("p1", "tenant-a", "ph", testIdentity, "w1", 1)
	r.ToolPaused("p1")
	now = now.Add(60 * time.Second)
	r.Turn("p1", "tenant-a", "ph", testIdentity, "w1", 2)
	// Pause again and resume far earlier than the estimate predicts.
	r.ToolPaused("p1")
	now = now.Add(500 * time.Millisecond)
	r.Turn("p1", "tenant-a", "ph", testIdentity, "w1", 3)
	_, _, errs, _ := r.Stats()
	if errs != 1 {
		t.Fatalf("prediction errors = %d, want 1 (early continuation)", errs)
	}
}

func TestDispatcherProgramHooks(t *testing.T) {
	dir := NewKVDirectory()
	reg := NewProgramRegistry(dir)
	d := NewDispatcher(nil)
	d.SetPrograms(reg)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "ok"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queueRequest("r1", "tenant-1", "interactive-paid", "model-a")
	qr.ProgramID = "p7"
	qr.TurnSeq = 2
	qr.ToolPaused = true
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	// Arrival registered the turn; completion with ToolPaused paused it.
	if !reg.Paused("p7") {
		t.Fatal("tool-paused completion must pause the program")
	}
	programs, paused, _, turns := reg.Stats()
	if programs != 1 || paused != 1 {
		t.Fatalf("stats = %d/%d, want 1/1", programs, paused)
	}
	if turns != 1 {
		t.Fatalf("turns = %d, want 1", turns)
	}
}
