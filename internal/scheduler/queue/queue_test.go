package queue

import (
	"testing"
	"time"
)

// req is a test helper building a Request with sensible defaults.
func req(id, tenant string, class Class, in, out int) Request {
	return Request{
		ID:                   id,
		Tenant:               tenant,
		Class:                class,
		InputTokens:          in,
		ExpectedOutputTokens: out,
		ArrivedAt:            time.Now(),
		TTL:                  time.Minute,
	}
}

func TestStrictPriorityAcrossClasses(t *testing.T) {
	q := New(DefaultLimits())
	// Enqueue batch first, then interactive — interactive must come out first.
	if err := q.Enqueue(req("b1", "t1", ClassBatch, 100, 100)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("i1", "t1", ClassInteractivePaid, 100, 100)); err != nil {
		t.Fatal(err)
	}
	first := mustNext(t, q)
	if first.ID != "i1" {
		t.Fatalf("first dispatched = %s, want i1 (strict priority)", first.ID)
	}
	second := mustNext(t, q)
	if second.ID != "b1" {
		t.Fatalf("second dispatched = %s, want b1", second.ID)
	}
}

func TestDRRTokenFairnessBetweenTenants(t *testing.T) {
	q := New(DefaultLimits())
	// Tenant A floods tiny requests; tenant B sends huge ones. Token-debited
	// DRR must keep both flows progressing — request-count fairness is not
	// the goal, work fairness is.
	for i := 0; i < 50; i++ {
		if err := q.Enqueue(req("a", "tenant-a", ClassInteractiveNormal, 10, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Enqueue(req("b", "tenant-b", ClassInteractiveNormal, 5000, 5000)); err != nil {
		t.Fatal(err)
	}

	order := drainAll(t, q, 51)
	// tenant-b's huge request must be served within the first few dispatches —
	// its queue age earns quantum, and DRR rotates flows regardless of
	// volume. It must NOT wait for all 50 of tenant-a's requests.
	bPos := -1
	for i, r := range order {
		if r.Tenant == "tenant-b" {
			bPos = i
			break
		}
	}
	if bPos < 0 {
		t.Fatal("tenant-b never dispatched")
	}
	if bPos > 10 {
		t.Fatalf("tenant-b waited for %d dispatches of tenant-a; DRR must rotate flows (got %d)", bPos, bPos)
	}
}

func TestDRRDeficitPersistsUntilQuantumCovered(t *testing.T) {
	q := New(DefaultLimits())
	// One request larger than the per-round quantum must still go eventually.
	if err := q.Enqueue(req("big", "t1", ClassInteractiveNormal, 10000, 10000)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("small", "t2", ClassInteractiveNormal, 5, 5)); err != nil {
		t.Fatal(err)
	}
	got := drainAll(t, q, 2)
	if got[0].ID != "small" {
		t.Fatalf("first = %s, want small (big request exceeds quantum, deficit accrues)", got[0].ID)
	}
	if got[1].ID != "big" {
		t.Fatalf("second = %s, want big (deficit must accumulate across rounds)", got[1].ID)
	}
}

func TestFCFSOrderingWithinFlow(t *testing.T) {
	q := New(DefaultLimits())
	if err := q.Enqueue(req("first", "t1", ClassInteractiveNormal, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("second", "t1", ClassInteractiveNormal, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("third", "t1", ClassInteractiveNormal, 10, 10)); err != nil {
		t.Fatal(err)
	}
	got := drainAll(t, q, 3)
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d = %s, want %s", i, got[i].ID, want[i])
		}
	}
}

func TestEDFOrdering(t *testing.T) {
	lim := DefaultLimits()
	lim.Ordering = map[Class]Ordering{ClassInteractiveNormal: OrderingEDF}
	q := New(lim)
	now := time.Now()
	late := req("late", "t1", ClassInteractiveNormal, 10, 10)
	late.Deadline = now.Add(5 * time.Second)
	soon := req("soon", "t1", ClassInteractiveNormal, 10, 10)
	soon.Deadline = now.Add(1 * time.Second)
	middle := req("middle", "t1", ClassInteractiveNormal, 10, 10)
	middle.Deadline = now.Add(3 * time.Second)
	for _, r := range []Request{late, soon, middle} {
		if err := q.Enqueue(r); err != nil {
			t.Fatal(err)
		}
	}
	got := drainAll(t, q, 3)
	if got[0].ID != "soon" || got[1].ID != "middle" || got[2].ID != "late" {
		t.Fatalf("EDF order = %s,%s,%s; want soon,middle,late", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestTTLExpiry(t *testing.T) {
	q := New(DefaultLimits())
	r := req("expiring", "t1", ClassBatch, 10, 10)
	r.TTL = 1 * time.Millisecond
	if err := q.Enqueue(r); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	out, ok := q.Next()
	if !ok {
		t.Fatal("Next() returned ok=false unexpectedly")
	}
	if out.Outcome != OutcomeExpiredTTL {
		t.Fatalf("outcome = %v, want expired-ttl", out.Outcome)
	}
	if out.Request == nil || out.Request.ID != "expiring" {
		t.Fatalf("expired request not returned with outcome")
	}
}

func TestCancellationRemovesFromQueue(t *testing.T) {
	q := New(DefaultLimits())
	if err := q.Enqueue(req("c1", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("c2", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if !q.Cancel("c1") {
		t.Fatal("Cancel returned false for queued request")
	}
	if q.Cancel("c1") {
		t.Fatal("Cancel returned true twice")
	}
	got := drainAll(t, q, 1)
	if got[0].ID != "c2" {
		t.Fatalf("dispatched %s after cancel; want c2", got[0].ID)
	}
}

func TestBandLimitRejects(t *testing.T) {
	lim := DefaultLimits()
	lim.BandMaxRequests = map[Class]int{ClassBatch: 2}
	q := New(lim)
	if err := q.Enqueue(req("b1", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("b2", "t2", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("b3", "t3", ClassBatch, 10, 10)); err == nil {
		t.Fatal("third batch request should be rejected by band limit")
	}
	// A higher class is unaffected by the batch band limit.
	if err := q.Enqueue(req("i1", "t4", ClassInteractivePaid, 10, 10)); err != nil {
		t.Fatalf("interactive enqueue rejected: %v", err)
	}
}

func TestGlobalLimitRejects(t *testing.T) {
	lim := DefaultLimits()
	lim.GlobalMaxRequests = 2
	q := New(lim)
	if err := q.Enqueue(req("1", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("2", "t2", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("3", "t3", ClassInteractivePaid, 10, 10)); err == nil {
		t.Fatal("third request should exceed the global limit regardless of class")
	}
}

func TestDrainReturnsRemainingInPriorityOrder(t *testing.T) {
	q := New(DefaultLimits())
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(req("i", "t1", ClassInteractiveNormal, 10, 10)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := q.Enqueue(req("b", "t2", ClassBatch, 10, 10)); err != nil {
			t.Fatal(err)
		}
	}
	got := q.Drain()
	if len(got) != 5 {
		t.Fatalf("drained %d, want 5", len(got))
	}
	for _, r := range got {
		if r.Outcome != OutcomeDrained {
			t.Fatalf("drain outcome = %v, want drained", r.Outcome)
		}
	}
}

func TestWorkConservingBatchServedWhenIdle(t *testing.T) {
	q := New(DefaultLimits())
	if err := q.Enqueue(req("batch-only", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	// Nothing else queued: the queue is work-conserving — batch must flow.
	got := mustNext(t, q)
	if got.ID != "batch-only" {
		t.Fatalf("got %+v; work-conserving queue must serve batch when idle", got)
	}
}

// --- helpers ---

func mustNext(t *testing.T, q *Queue) *Request {
	t.Helper()
	out, ok := q.Next()
	if !ok {
		t.Fatal("Next() returned ok=false unexpectedly")
	}
	if out.Request == nil {
		t.Fatal("Next() returned a nil request")
	}
	return out.Request
}

func drainAll(t *testing.T, q *Queue, n int) []Request {
	t.Helper()
	var got []Request
	for i := 0; i < n; i++ {
		out, ok := q.Next()
		if !ok {
			t.Fatalf("Next() drained early at %d/%d", i, n)
		}
		if out.Request != nil {
			got = append(got, *out.Request)
		}
	}
	return got
}

func TestWorkloadClassification(t *testing.T) {
	// Spec §12.3.5 buckets in LLM-context units (K = 1024): S ≤8K,
	// M ≤32K, L ≤64K, XL beyond, IMAGE when media is present.
	cases := []struct {
		in, out, media int
		want           Workload
	}{
		{100, 100, 0, WorkloadS},
		{8 * 1024, 100, 0, WorkloadS},
		{8*1024 + 1, 100, 0, WorkloadM},
		{32 * 1024, 100, 0, WorkloadM},
		{32*1024 + 1, 100, 0, WorkloadL},
		{64 * 1024, 100, 0, WorkloadL},
		{64*1024 + 1, 100, 0, WorkloadXL},
		{128 * 1024, 100, 0, WorkloadXL},
		{128*1024 + 1, 100, 0, WorkloadXL},
		{100, 100, 1, WorkloadImage},
	}
	for _, c := range cases {
		r := Request{InputTokens: c.in, ExpectedOutputTokens: c.out, MediaTokens: c.media}
		if got := r.WorkloadClass(); got != c.want {
			t.Errorf("Classify(in=%d,media=%d) = %v, want %v", c.in, c.media, got, c.want)
		}
	}
}
