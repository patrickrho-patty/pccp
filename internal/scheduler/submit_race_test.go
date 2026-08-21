package scheduler

import (
	"fmt"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// TestSubmitRollsBackWaiterOnEnqueueFailure: registration-first means a
// rejected enqueue removes the waiter again (no leaked pending entries).
func TestSubmitRollsBackWaiterOnEnqueueFailure(t *testing.T) {
	q := queue.New(queue.Limits{
		GlobalMaxRequests: 1,
		BandMaxRequests:   map[queue.Class]int{},
		Ordering:          map[queue.Class]queue.Ordering{},
	})
	d := NewDispatcher(q)
	first := queueRequest("first", "t1", "batch", "model-a")
	if _, err := d.Submit(first); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Submit(queueRequest("second", "t1", "batch", "model-a")); err == nil {
		t.Fatal("second submit must fail past the global limit")
	}
	d.fwMu.Lock()
	_, leaked := d.pending["second"]
	d.fwMu.Unlock()
	if leaked {
		t.Fatal("failed enqueue left a pending waiter registered")
	}
}

// TestSubmitNeverStrandsUnderReap: the dispatch loop's 500ms reap can
// fire between enqueue and (previously) waiter registration, dropping a
// fast completion into a missing waiter. Registration-first closes the
// window: hundreds of instant completions must ALL deliver.
func TestSubmitNeverStrandsUnderReap(t *testing.T) {
	d := NewDispatcher(nil)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "ok"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 64), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	for i := 0; i < 200; i++ {
		qr := queueRequest("r", "t1", "interactive-paid", "model-a")
		qr.ID = fmt.Sprintf("r-%d", i)
		ch, err := d.Submit(qr)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("request %d stranded (registration race)", i)
		}
	}
}
