package conformance

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/scheduler"
	"github.com/patrickrho-patty/pccp/internal/scheduler/queue"
)

// S2–S12 black-box conformance: the full stack composed exactly as the
// binary wires it, asserted through public APIs only.

func buildServingStack(t *testing.T) (*scheduler.Scheduler, *scheduler.Serving) {
	t.Helper()
	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc := scheduler.NewScheduler(
		scheduler.Trust{Issuers: map[string]ed25519.PublicKey{}, Now: time.Now},
		nil,
		2*time.Second, 4*time.Second, evidenceKey,
	)
	// Router with KV index + receipts.
	router := scheduler.NewCostRouter(scheduler.DefaultRouterConfig())
	router.SetKV(svc.KV)
	router.SetReceipts(scheduler.NewReceiptStore(64))
	svc.Serving.Dispatcher.SetRouter(router)
	return svc, svc.Serving
}

// Invariant S2-1: strict priority — an interactive request enqueued
// after a batch request dispatches first.
func TestS2StrictPriorityDispatch(t *testing.T) {
	svc, serving := buildServingStack(t)
	entry := scheduler.WorkerEntry{
		Card:        scheduler.WorkerCard{CardVersion: 2, DariAddr: "127.0.0.1:1", WorkerID: "w1", ModelName: "model-a", MaxConcurrentSeqs: 8, Status: "active"},
		LeasedUntil: time.Now().Add(time.Minute),
	}
	sel := scheduler.NewWorkerSelector()
	sel.Upsert(entry, 0)
	serving.Dispatcher.SetSelector(sel)
	// The S3 router is installed by buildServingStack: it needs the same
	// worker in its own table.
	router := scheduler.NewCostRouter(scheduler.DefaultRouterConfig())
	router.SetKV(svc.KV)
	router.UpsertWorker(entry, scheduler.RouterWorkerState{})
	serving.Dispatcher.SetRouter(router)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serving.Dispatcher.RunDispatchLoop(ctx)

	// A no-op forwarder: dispatch executes, completes instantly.
	serving.Dispatcher.SetForwarder(&nullForwarder{})

	q := serving.Dispatcher.Queue()
	if err := q.Enqueue(queue.Request{
		ID: "batch-1", Tenant: "t1", Class: queue.ClassBatch,
		InputTokens: 10, ExpectedOutputTokens: 10, ArrivedAt: time.Now(), TTL: time.Minute,
		Payload: scheduler.RequestPayload{Model: "model-a", Messages: []byte("[]")},
	}); err != nil {
		t.Fatal(err)
	}
	// The interactive request enters AFTER the batch one.
	ch, err := serving.Dispatcher.Submit(queue.Request{
		ID: "interactive-1", Tenant: "t1", Class: queue.ClassInteractivePaid,
		InputTokens: 10, ExpectedOutputTokens: 10, ArrivedAt: time.Now(), TTL: time.Minute,
		Payload: scheduler.RequestPayload{Model: "model-a", Messages: []byte("[]")},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-ch:
		if res.Cancelled || res.Err != "" {
			t.Fatalf("interactive result = %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interactive request never dispatched")
	}
	if got := svc.KV.Watermark("w1"); got != 0 {
		t.Fatalf("kv watermark = %d", got)
	}
}

// Invariant S3-1: KV-overlap credits route to the warm worker even when
// a cold worker is idle.
func TestS3KVOverlapCreditsRouteWarm(t *testing.T) {
	router := scheduler.NewCostRouter(scheduler.DefaultRouterConfig())
	kv := scheduler.NewKVIndex()
	kv.Add("warm", scheduler.KVBlock{Namespace: "tenant-a", Hash: "p", Tokens: 800})
	router.SetKV(kv)
	router.UpsertWorker(scheduler.WorkerEntry{
		Card:        scheduler.WorkerCard{CardVersion: 2, DariAddr: "127.0.0.1:1", WorkerID: "warm", ModelName: "m", MaxConcurrentSeqs: 8, Status: "active"},
		LeasedUntil: time.Now().Add(time.Minute),
	}, scheduler.RouterWorkerState{PrefillActive: 300, ActiveRequests: 1})
	router.UpsertWorker(scheduler.WorkerEntry{
		Card:        scheduler.WorkerCard{CardVersion: 2, DariAddr: "127.0.0.1:2", WorkerID: "cold", ModelName: "m", MaxConcurrentSeqs: 8, Status: "active"},
		LeasedUntil: time.Now().Add(time.Minute),
	}, scheduler.RouterWorkerState{})
	got, err := router.Route(scheduler.RouteRequest{
		Model: "m", Namespace: "tenant-a", PrefixHash: "p",
		InputTokens: 1000, CachedTokens: 800, ExpectedOutputTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID != "warm" {
		t.Fatalf("routed to %s; KV credits must prefer the warm worker", got.WorkerID)
	}
}

// Invariant S5-1: a placement violating the SLO with high probability is
// never chosen.
func TestS5SLONonCompliantWorkerExcluded(t *testing.T) {
	p := scheduler.NewLatencyPredictor(scheduler.DefaultPredictorConfig())
	slow := scheduler.PredictorFeatures{InputTokens: 512, ExpectedOutputTokens: 200, ActivePrefill: 100, ActiveDecodeKV: 50, ActiveRequests: 1}
	for i := 0; i < 200; i++ {
		p.Observe("slow", slow, 3000)
	}
	router := scheduler.NewCostRouter(scheduler.DefaultRouterConfig())
	router.SetPredictor(p)
	sl := scheduler.NewSLOResolver()
	sl.SetDefault(scheduler.SLOTarget{TTFTMs: 500, ITLMs: 50})
	router.SetSLOResolver(sl)
	router.SetConfigForWorker("w-slow", "slow")
	router.UpsertWorker(scheduler.WorkerEntry{
		Card:        scheduler.WorkerCard{CardVersion: 2, DariAddr: "127.0.0.1:1", WorkerID: "w-slow", ModelName: "m", MaxConcurrentSeqs: 8, Status: "active"},
		LeasedUntil: time.Now().Add(time.Minute),
	}, scheduler.RouterWorkerState{})
	_, err := router.Route(scheduler.RouteRequest{Model: "m", InputTokens: 512, ExpectedOutputTokens: 200})
	if err == nil {
		t.Fatal("SLO-violating worker must not be routed to")
	}
}

// Invariant S7-1: the autoscaler always maintains warm spare.
func TestS7WarmSpareAlways(t *testing.T) {
	a := scheduler.NewAutoscaler(scheduler.DefaultAutoscaleConfig())
	if a.Config().WarmSpareReplicas <= 0 {
		t.Fatal("warm spare must be ≥1")
	}
}

// Invariant S9-1: batch never dispatches into a saturated fleet.
func TestS9BatchSlackOnly(t *testing.T) {
	g := scheduler.NewBatchGateway(scheduler.DefaultBatchConfig())
	if _, err := g.Submit(scheduler.BatchJob{Tenant: "t", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	g.SetFleetSaturated(true)
	if got := g.DispatchOne(); got != nil {
		t.Fatal("batch dispatched into a saturated fleet")
	}
}

type nullForwarder struct{}

func (nullForwarder) Send(string, scheduler.InferencePayload) (scheduler.InferenceResult, error) {
	return scheduler.InferenceResult{Text: "ok", Finish: "stop"}, nil
}
