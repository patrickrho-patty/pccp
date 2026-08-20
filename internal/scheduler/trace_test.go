package scheduler

import (
	"testing"
	"time"
)

func TestTraceRecorderBounded(t *testing.T) {
	rec := NewTraceRecorder(2)
	for i := 0; i < 3; i++ {
		rec.Add(TraceEvent{RequestID: string(rune('a' + i)), Stage: TraceArrived})
	}
	out := rec.Export()
	if len(out.Events) != 2 {
		t.Fatalf("events = %d, want bounded 2", len(out.Events))
	}
	if out.Events[0].RequestID != "b" || out.Events[1].RequestID != "c" {
		t.Fatalf("kept events = %s,%s — want the most recent", out.Events[0].RequestID, out.Events[1].RequestID)
	}
}

func TestTraceRecorderVersionsAndClock(t *testing.T) {
	rec := NewTraceRecorder(4)
	rec.SetVersion("router", CostRouterVersion)
	rec.SetNow(func() int64 { return 1234 })
	rec.Add(TraceEvent{RequestID: "r1", Stage: TraceArrived})
	out := rec.Export()
	if out.Versions["router"] != CostRouterVersion {
		t.Fatalf("versions = %v", out.Versions)
	}
	if out.Events[0].AtUnixMs != 1234 {
		t.Fatalf("timestamp = %d, want injected clock 1234", out.Events[0].AtUnixMs)
	}
}

func TestDispatcherTraceLifecycle(t *testing.T) {
	rec := NewTraceRecorder(16)
	d := NewDispatcher(nil)
	d.SetTraceRecorder(rec)
	d.SetForwarder(&fakeForwarder{result: InferenceResult{
		Text:  "ok",
		Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 7},
	}})
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)
	startLoop(t, d)

	ch, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for completion")
	}

	events := rec.Export().Events
	stages := map[TraceStage]TraceEvent{}
	for _, e := range events {
		stages[e.Stage] = e
	}
	arrived, ok := stages[TraceArrived]
	if !ok || arrived.RequestID != "r1" || arrived.Model != "model-a" || arrived.Tenant != "tenant-1" {
		t.Fatalf("arrived event = %+v", arrived)
	}
	bound, ok := stages[TraceBound]
	if !ok || bound.WorkerID != "w1" || bound.QueueWaitMs < 0 {
		t.Fatalf("bound event = %+v", bound)
	}
	completed, ok := stages[TraceCompleted]
	if !ok || completed.WorkerID != "w1" || completed.OutputTokens != 7 {
		t.Fatalf("completed event = %+v", completed)
	}
}

func TestDispatcherTraceExpired(t *testing.T) {
	rec := NewTraceRecorder(16)
	d := NewDispatcher(nil)
	d.SetTraceRecorder(rec)
	sel := NewWorkerSelector()
	sel.Upsert(mkWorker("w1", "model-a", 4), 1)
	d.SetSelector(sel)

	qr := queueRequest("r-exp", "tenant-1", "interactive-paid", "model-a")
	qr.ArrivedAt = time.Now().Add(-time.Second)
	qr.TTL = time.Millisecond
	if err := d.Queue().Enqueue(qr); err != nil {
		t.Fatal(err)
	}
	// The expired head pops with OutcomeExpiredTTL and is traced as such.
	if got := d.Assign("w1"); got != nil {
		t.Fatalf("expired request dispatched: %+v", got)
	}
	for _, e := range rec.Export().Events {
		if e.Stage == TraceExpired && e.RequestID == "r-exp" {
			return
		}
	}
	t.Fatalf("no expired event in %v", rec.Export().Events)
}

func TestDispatcherTraceCancelled(t *testing.T) {
	rec := NewTraceRecorder(16)
	d := NewDispatcher(nil)
	d.SetTraceRecorder(rec)

	if _, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a")); err != nil {
		t.Fatal(err)
	}
	if !d.Cancel("r1") {
		t.Fatal("cancel returned false")
	}
	found := false
	for _, e := range rec.Export().Events {
		if e.Stage == TraceCancelled && e.RequestID == "r1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no cancelled event in %v", rec.Export().Events)
	}
}
