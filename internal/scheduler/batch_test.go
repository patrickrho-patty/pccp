package scheduler

import (
	"testing"
	"time"
)

func TestBatchSubmitAndStatus(t *testing.T) {
	g := NewBatchGateway(DefaultBatchConfig())
	job, err := g.Submit(BatchJob{
		Tenant:   "tenant-a",
		Model:    "model-a",
		Payload:  []byte("batch payload"),
		Deadline: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	st, ok := g.Status(job.ID)
	if !ok || st != BatchQueued {
		t.Fatalf("status = %v, want queued", st)
	}
}

func TestBatchSlackOnlyDispatch(t *testing.T) {
	// Spec §13.10: batch work is admitted ONLY into slack — a saturated
	// fleet never dispatches batch, even if queued.
	g := NewBatchGateway(DefaultBatchConfig())
	job, _ := g.Submit(BatchJob{Tenant: "t", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)})

	g.SetFleetSaturated(true)
	if got := g.DispatchOne(); got != nil {
		t.Fatalf("saturated fleet dispatched batch job %s", got.ID)
	}
	g.SetFleetSaturated(false)
	if got := g.DispatchOne(); got == nil || got.ID != job.ID {
		t.Fatalf("slack dispatch = %+v, want the queued job", got)
	}
}

func TestBatchPauseResume(t *testing.T) {
	// Spec §13.10: token-level pause/resume yields to interactive
	// instantly; resume re-submits reusing the warm prefix.
	g := NewBatchGateway(DefaultBatchConfig())
	if _, err := g.Submit(BatchJob{Tenant: "t", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	g.SetFleetSaturated(false)
	dispatched := g.DispatchOne()
	if dispatched == nil {
		t.Fatal("dispatch failed")
	}
	g.Pause(dispatched.ID, 42) // paused after 42 tokens
	st, _ := g.Status(dispatched.ID)
	if st != BatchPaused {
		t.Fatalf("status after pause = %v, want paused", st)
	}
	if tok := g.ResumeFrom(dispatched.ID); tok != 42 {
		t.Fatalf("resume-from token = %d, want 42 (warm prefix reuse)", tok)
	}
	g.Resume(dispatched.ID)
	if st, _ := g.Status(dispatched.ID); st != BatchQueued {
		t.Fatalf("status after resume = %v, want queued", st)
	}
}

func TestBatchDeadlineExpiry(t *testing.T) {
	g := NewBatchGateway(DefaultBatchConfig())
	job, _ := g.Submit(BatchJob{Tenant: "t", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(5 * time.Millisecond)})
	time.Sleep(10 * time.Millisecond)
	if got := g.DispatchOne(); got != nil {
		t.Fatalf("expired job dispatched: %s", got.ID)
	}
	if st, _ := g.Status(job.ID); st != BatchExpired {
		t.Fatalf("status = %v, want expired", st)
	}
}

func TestBatchQuotaPerTenant(t *testing.T) {
	g := NewBatchGateway(DefaultBatchConfig())
	g.SetTenantQuota("tenant-a", 2)
	for i := 0; i < 2; i++ {
		if _, err := g.Submit(BatchJob{Tenant: "tenant-a", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.Submit(BatchJob{Tenant: "tenant-a", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("tenant quota must reject the third concurrent job")
	}
	// Another tenant is unaffected.
	if _, err := g.Submit(BatchJob{Tenant: "tenant-b", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

func TestBatchCancellation(t *testing.T) {
	g := NewBatchGateway(DefaultBatchConfig())
	job, _ := g.Submit(BatchJob{Tenant: "t", Model: "m", Payload: []byte("x"), Deadline: time.Now().Add(time.Hour)})
	if !g.Cancel(job.ID) {
		t.Fatal("cancel failed")
	}
	if st, _ := g.Status(job.ID); st != BatchCancelled {
		t.Fatalf("status = %v, want cancelled", st)
	}
}
