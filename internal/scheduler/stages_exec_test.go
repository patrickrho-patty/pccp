package scheduler

import (
	"errors"
	"testing"
	"time"
)

// fakeStageForwarder records stage calls and answers scripted results.
type fakeStageForwarder struct {
	prefillCalls int
	prefillAddr  string
	decodeCalls  int
	decodeAddr   string
	handle       string
	sendCalls    int
	prefillErr   error
	result       InferenceResult
}

func (f *fakeStageForwarder) Send(addr string, p InferencePayload) (InferenceResult, error) {
	f.sendCalls++
	return f.result, nil
}

func (f *fakeStageForwarder) SendPrefill(addr string, p InferencePayload) (string, error) {
	f.prefillCalls++
	f.prefillAddr = addr
	if f.prefillErr != nil {
		return "", f.prefillErr
	}
	return "kv-handle-1", nil
}

func (f *fakeStageForwarder) SendDecode(addr, handle string, p InferencePayload) (InferenceResult, error) {
	f.decodeCalls++
	f.decodeAddr = addr
	f.handle = handle
	return f.result, nil
}

// stagedDispatcher builds a dispatcher whose plan for model-a is
// disaggregated: prefill on w-pre (saturated so selection binds w-dec),
// decode on w-dec, with a cheap same-node transfer.
func stagedDispatcher(t *testing.T, fw Forwarder) (*Dispatcher, *TraceRecorder) {
	t.Helper()
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddWorker("w-pre", "n1")
	inv.AddWorker("w-dec", "n1")

	pd := NewPDPlanner()
	pre := mkWorker("w-pre", "model-a", 4)
	pre.Card.PDRole = PDRolePrefill
	pre.Card.DariAddr = "10.0.0.1:9501"
	dec := mkWorker("w-dec", "model-a", 4)
	dec.Card.PDRole = PDRoleDecode
	dec.Card.DariAddr = "10.0.0.2:9502"
	pd.Upsert(pre, RouterWorkerState{})
	pd.Upsert(dec, RouterWorkerState{})
	for i := 0; i < 10; i++ {
		pd.ObservePrefillShare("model-a", 0.9)
	}

	d := NewDispatcher(nil)
	d.SetStagePlanner(NewStagePlanner(pd, NewStaticTopologyOracle(inv), nil))
	rec := NewTraceRecorder(32)
	d.SetTraceRecorder(rec)
	d.SetForwarder(fw)
	sel := NewWorkerSelector()
	sel.Upsert(pre, 1)
	sel.Upsert(dec, 1)
	sel.SetLoad("w-pre", 4, 0) // saturated: selection must bind w-dec
	d.SetSelector(sel)
	startLoop(t, d)
	return d, rec
}

func TestStagedExecutionPath(t *testing.T) {
	fw := &fakeStageForwarder{result: InferenceResult{Text: "staged", Usage: map[string]int{"prompt_tokens": 10, "completion_tokens": 3}}}
	d, rec := stagedDispatcher(t, fw)

	ch, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-ch:
		if res.Text != "staged" {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if fw.prefillCalls != 1 || fw.prefillAddr != "10.0.0.1:9501" {
		t.Fatalf("prefill = %d calls to %q", fw.prefillCalls, fw.prefillAddr)
	}
	if fw.decodeCalls != 1 || fw.handle != "kv-handle-1" || fw.decodeAddr != "10.0.0.2:9502" {
		t.Fatalf("decode = %d calls handle %q addr %q", fw.decodeCalls, fw.handle, fw.decodeAddr)
	}
	if fw.sendCalls != 0 {
		t.Fatalf("co-located Send used on a staged plan (%d calls)", fw.sendCalls)
	}
	stages := map[TraceStage]bool{}
	for _, e := range rec.Export().Events {
		stages[e.Stage] = true
	}
	for _, want := range []TraceStage{TraceBound, TracePrefill, TraceTransfer, TraceDecode, TraceCompleted} {
		if !stages[want] {
			t.Fatalf("missing stage event %s in trace", want)
		}
	}
}

func TestStagedExecutionFallsBackColocated(t *testing.T) {
	fw := &fakeStageForwarder{
		prefillErr: errors.New("stage execution not supported by this engine"),
		result:     InferenceResult{Text: "colocated"},
	}
	d, _ := stagedDispatcher(t, fw)

	ch, err := d.Submit(queueRequest("r1", "tenant-1", "interactive-paid", "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-ch:
		if res.Text != "colocated" {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if fw.prefillCalls != 1 {
		t.Fatalf("prefill attempts = %d, want 1", fw.prefillCalls)
	}
	if fw.decodeCalls != 0 {
		t.Fatalf("decode ran after prefill failure (%d calls)", fw.decodeCalls)
	}
	if fw.sendCalls != 1 {
		t.Fatalf("co-located fallback Send = %d calls, want 1", fw.sendCalls)
	}
}
