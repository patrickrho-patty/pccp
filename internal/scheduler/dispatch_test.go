package scheduler

import (
	"testing"
	"time"
)

func mkWorker(id, model string, seqs uint64) WorkerEntry {
	return WorkerEntry{
		Card: WorkerCard{
			WorkerID:          id,
			ModelName:         model,
			MaxConcurrentSeqs: seqs,
			Status:            "ready",
		},
		LeasedUntil: time.Now().Add(time.Minute),
	}
}

func TestSelectWorkerModelAndCapacity(t *testing.T) {
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 8), 1)
	sel.Upsert(mkWorker("w2", "model-b", 8), 2)
	sel.Upsert(mkWorker("w3", "model-a", 8), 3)
	sel.SetLoad("w3", 8, 0) // fully loaded

	got, ok := sel.Select("model-a")
	if !ok {
		t.Fatal("no worker selected for model-a")
	}
	if got != "w1" && got != "w2" {
		t.Fatalf("selected %s, want a model-a worker with free capacity", got)
	}
	if got == "w3" {
		t.Fatal("selected a saturated worker")
	}
	// model-b has no traffic: w2 eligible only for model-b.
	if got2, ok := sel.Select("model-b"); !ok || got2 != "w2" {
		t.Fatalf("model-b selection = %s,%v", got2, ok)
	}
}

func TestSelectWorkerSkipsQuarantined(t *testing.T) {
	sel := NewWorkerSelector()
	w := mkWorker("w1", "model-a", 8)
	sel.Upsert(w, 1)
	w2 := mkWorker("w2", "model-a", 8)
	w2.Quarantined = true
	sel.Upsert(w2, 2)

	got, ok := sel.Select("model-a")
	if !ok || got != "w1" {
		t.Fatalf("selected %s (ok=%v), want w1 — quarantined workers are excluded from serving pools", got, ok)
	}
}

func TestSelectWorkerNoEligible(t *testing.T) {
	sel := NewWorkerSelector()
	if _, ok := sel.Select("missing-model"); ok {
		t.Fatal("selected a worker for an unknown model")
	}
}

func TestDispatchRoundTrip(t *testing.T) {
	// Late binding: the dispatcher pulls from the global queue and assigns
	// a worker only when capacity exists.
	d := NewDispatcher(nil)
	q := d.Queue()
	if err := q.Enqueue(queueRequest("r1", "tenant-1", "interactive-paid", "model-a")); err != nil {
		t.Fatal(err)
	}
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)

	// The dispatch loop assigns when a worker slot is free.
	first := d.Assign("w1")
	if first == nil || first.Request.ID != "r1" || first.WorkerID != "w1" {
		t.Fatalf("first assign = %+v, want r1 bound to w1", first)
	}
	sel.SetLoad("w1", 1, 0)
	d.Assign("w1")
	sel.SetLoad("w1", 2, 0)
	d.Assign("w1")
	sel.SetLoad("w1", 3, 0)
	d.Assign("w1")
	sel.SetLoad("w1", 4, 0)
	// Now saturated: assignment must not dispatch anything new.
	if got := d.Assign("w1"); got != nil {
		t.Fatalf("saturated worker dispatched %s", got.Request.ID)
	}
}
