package scheduler

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// TestExecuteErrorDropsRequestNotRequeues (round-2 review): when
// execution cannot proceed (no forwarder / no worker addr), the waiter
// gets exactly one error result AND the request must NOT be requeued —
// a requeue would execute the work after the client already received
// the error (double billing).
func TestExecuteErrorDropsRequestNotRequeues(t *testing.T) {
	d := NewDispatcher(nil)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 2), 1)
	d.SetSelector(sel)
	startLoop(t, d)
	// NOTE: no forwarder installed.

	qr := queue.Request{
		ID: "req-drop", Tenant: "t", Class: queue.ClassInteractivePaid,
		InputTokens: 10, ExpectedOutputTokens: 10, Payload: RequestPayload{Model: "model-a"},
		ArrivedAt: time.Now(), TTL: time.Minute,
	}
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	d.wakeDispatch()
	select {
	case res := <-ch:
		if res.Err == "" {
			t.Fatalf("expected error result, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never completed — leak")
	}
	// The request must not linger in the queue.
	time.Sleep(50 * time.Millisecond)
	if got := d.queue.Pending(); got != 0 {
		t.Fatalf("request was requeued after error (pending=%d) — double-execution risk", got)
	}
}

// TestExpiredRequestCompletesWaiter (round-2 review): a request whose
// TTL lapses in-queue must unblock its waiter with the expiry error,
// not leave the HTTP handler parked until its own timer.
func TestExpiredRequestCompletesWaiter(t *testing.T) {
	d := NewDispatcher(nil)
	sel := NewWorkerSelector()
	// No compatible worker for the model → nothing dispatchable.
	sel.Upsert(mkWorker("w1", "other-model", 2), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queue.Request{
		ID: "req-exp", Tenant: "t", Class: queue.ClassInteractivePaid,
		InputTokens: 10, ExpectedOutputTokens: 10, Payload: RequestPayload{Model: "unavailable"},
		ArrivedAt: time.Now(), TTL: 30 * time.Millisecond,
	}
	ch, err := d.Submit(qr)
	if err != nil {
		t.Fatal(err)
	}
	d.wakeDispatch()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case res := <-ch:
			if !res.Cancelled || res.Err == "" {
				t.Fatalf("expected cancelled/expiry result, got %+v", res)
			}
			return
		case <-deadline:
			t.Fatal("expired request never completed its waiter")
		default:
			// Re-nudge the dispatch loop so it observes the TTL lapse.
			d.wakeDispatch()
			time.Sleep(10 * time.Millisecond)
		}
	}
}
