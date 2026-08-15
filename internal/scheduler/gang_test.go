package scheduler

import "testing"

func gangWorker(id, model string, tp, dp, ep uint32) WorkerEntry {
	e := mkWorker(id, model, 8)
	e.Card.TP = tp
	e.Card.DP = dp
	e.Card.EP = ep
	return e
}

func TestGangReadiness(t *testing.T) {
	// Spec §14 row 16: gang scheduling — a request needs ALL ranks of its
	// worker group ready before any member serves it.
	gr := NewGangRegistry()
	gr.Upsert(gangWorker("w1", "model-a", 2, 1, 1))
	gr.Upsert(gangWorker("w2", "model-a", 2, 1, 1))
	// Both ranks of the TP=2 gang are present → ready.
	if !gr.Ready("model-a") {
		t.Fatal("complete gang must be ready")
	}
	if n := gr.ReadyCount("model-a"); n != 2 {
		t.Fatalf("ready workers = %d, want 2", n)
	}
}

func TestGangIncompleteNotReady(t *testing.T) {
	gr := NewGangRegistry()
	gr.Upsert(gangWorker("w1", "model-b", 4, 1, 1)) // 3 ranks missing
	if gr.Ready("model-b") {
		t.Fatal("incomplete gang must not be ready")
	}
	if n := gr.ReadyCount("model-b"); n != 0 {
		t.Fatalf("ready workers = %d, want 0", n)
	}
}

func TestGangQuarantinedRankBlocksGroup(t *testing.T) {
	gr := NewGangRegistry()
	gr.Upsert(gangWorker("w1", "model-c", 2, 1, 1))
	w2 := gangWorker("w2", "model-c", 2, 1, 1)
	w2.Quarantined = true
	gr.Upsert(w2)
	if gr.Ready("model-c") {
		t.Fatal("quarantined rank must block gang readiness")
	}
}

func TestGangEviction(t *testing.T) {
	gr := NewGangRegistry()
	gr.Upsert(gangWorker("w1", "model-d", 2, 1, 1))
	gr.Upsert(gangWorker("w2", "model-d", 2, 1, 1))
	gr.Remove("w2")
	if gr.Ready("model-d") {
		t.Fatal("gang must be incomplete after a member leaves")
	}
}

func TestGangDPRanks(t *testing.T) {
	// DP=2 means two independent ranks, each a complete gang of its own.
	gr := NewGangRegistry()
	gr.Upsert(gangWorker("w1", "model-e", 1, 2, 1))
	gr.Upsert(gangWorker("w2", "model-e", 1, 2, 1))
	if !gr.Ready("model-e") {
		t.Fatal("DP ranks are independent; each is individually ready")
	}
	if n := gr.ReadyCount("model-e"); n != 2 {
		t.Fatalf("ready = %d, want both DP ranks", n)
	}
}
