package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// startLoop runs the dispatch loop for a test and stops it on cleanup.
func startLoop(t *testing.T, d *Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go d.RunDispatchLoop(ctx)
	t.Cleanup(cancel)
}

// fakeForwarder is a test double implementing Forwarder.
type fakeForwarder struct {
	result InferenceResult
	err    error
	calls  int
}

func (f *fakeForwarder) Send(workerAddr string, payload InferencePayload) (InferenceResult, error) {
	f.calls++
	return f.result, f.err
}

func TestDispatcherSubmitCompletes(t *testing.T) {
	d := NewDispatcher(nil)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "답변입니다"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queueRequest("r1", "tenant-1", "interactive-paid", "model-a")
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	// The dispatch loop wakes and binds r1 to w1.
	select {
	case res := <-ch:
		if res.Text != "답변입니다" {
			t.Fatalf("result = %q", res.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatch completion")
	}
}

func TestDispatcherSubmitCancel(t *testing.T) {
	d := NewDispatcher(nil)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-b", 4), 1) // serves a different model
	d.SetSelector(sel)
	d.SetForwarder(&fakeForwarder{})
	startLoop(t, d)

	// The request's model has no worker: it stays queued (late binding),
	// so cancellation is the only way it ends.
	qr := queueRequest("r1", "tenant-1", "interactive-paid", "model-a")
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Cancel("r1") {
		t.Fatal("cancel returned false")
	}
	select {
	case res := <-ch:
		if res.Cancelled != true {
			t.Fatalf("result should be cancelled, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not complete the pending request")
	}
}

func TestDispatcherSubmitRejectedWhenQueueFull(t *testing.T) {
	q := queue.New(queue.Limits{
		GlobalMaxRequests: 2,
		BandMaxRequests:   map[queue.Class]int{},
		Ordering:          map[queue.Class]queue.Ordering{},
	})
	d := NewDispatcher(q)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	d.SetForwarder(&fakeForwarder{})

	for i := 0; i < 3; i++ {
		qr := queueRequest("fill", "tenant-1", "batch", "model-a")
		qr.Class = "batch"
		if _, err := d.Submit(qr); err != nil {
			// Reached the limit — the point of the test.
			return
		}
	}
	t.Fatal("queue accepted requests beyond its global limit")
}
