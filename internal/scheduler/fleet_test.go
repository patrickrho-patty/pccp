package scheduler

import "testing"

func TestFleetUpsertRemoveLoad(t *testing.T) {
	f := NewWorkerFleet()
	e := mkWorker("w1", "model-a", 8)
	f.Upsert(e, RouterWorkerState{ActiveRequests: 2})
	got, ok := f.Get("w1")
	if !ok || got.Entry.Card.WorkerID != "w1" || got.State.ActiveRequests != 2 {
		t.Fatalf("get = %+v,%v", got, ok)
	}
	if n := len(f.List()); n != 1 {
		t.Fatalf("list = %d", n)
	}
	f.Remove("w1")
	if _, ok := f.Get("w1"); ok {
		t.Fatal("removed worker still present")
	}
}

func TestFleetMutate(t *testing.T) {
	f := NewWorkerFleet()
	f.Upsert(mkWorker("w1", "model-a", 8), RouterWorkerState{})
	ok := f.Mutate("w1", func(w *FleetWorker) {
		w.State.Load.Active++
	})
	if !ok {
		t.Fatal("mutate on present worker returned false")
	}
	got, _ := f.Get("w1")
	if got.State.Load.Active != 1 {
		t.Fatalf("load active = %d, want 1", got.State.Load.Active)
	}
	if f.Mutate("ghost", func(w *FleetWorker) {}) {
		t.Fatal("mutate on absent worker returned true")
	}
}
