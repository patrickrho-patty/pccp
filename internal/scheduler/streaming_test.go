package scheduler

import (
	"testing"
	"time"
)

// fakeStreamForwarder emits token deltas before the completion.
type fakeStreamForwarder struct {
	deltas []string
}

func (f *fakeStreamForwarder) Send(workerAddr string, payload InferencePayload) (InferenceResult, error) {
	return f.SendStream(workerAddr, payload, func(string) {})
}

func (f *fakeStreamForwarder) SendStream(workerAddr string, payload InferencePayload, onDelta func(string)) (InferenceResult, error) {
	for _, d := range f.deltas {
		onDelta(d)
	}
	return InferenceResult{Text: "final", Finish: "stop"}, nil
}

func TestDispatcherSubmitStreaming(t *testing.T) {
	d := NewDispatcher(nil)
	d.SetStreamForwarder(&fakeStreamForwarder{deltas: []string{"안", "녕"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queueRequest("r1", "t1", "interactive-paid", "model-a")
	ch, deltas, err := d.SubmitStream(qr)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for d := range deltas {
			got = append(got, d)
		}
	}()
	select {
	case res := <-ch:
		if res.Text != "final" {
			t.Fatalf("final = %q", res.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deltas channel never closed")
	}
	if len(got) != 2 || got[0] != "안" || got[1] != "녕" {
		t.Fatalf("deltas = %v", got)
	}
}

func TestDispatcherStreamingFallsBackToPlainForwarder(t *testing.T) {
	// A dispatcher with only a plain Forwarder still completes streaming
	// submits: deltas are empty and the final result arrives once.
	d := NewDispatcher(nil)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{Text: "plain", Finish: "stop"}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	qr := queueRequest("r1", "t1", "interactive-paid", "model-a")
	ch, deltas, err := d.SubmitStream(qr)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-ch:
		if res.Text != "plain" {
			t.Fatalf("text = %q", res.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if _, ok := <-deltas; ok {
		t.Fatal("plain forwarder must not emit deltas")
	}
}
