// Package queue implements the S2 global admission queue: a token-debited
// weighted Deficit Round Robin scheduler with strict priority classes,
// per-tenant fairness flows, ordering policies, and band/global capacity
// limits (DARI scheduler spec §12.3.4, §13.5, §14 rows 2–3).
//
// Design notes (locked by spec):
//   - Deficit is denominated in TOKENS, not requests: debit =
//     input_tokens + expected_output_tokens at admission. Fairness tracks
//     actual GPU work (spec §13.5).
//   - Strict priority across classes; weighted DRR across tenants (flows)
//     within a class; ordering policy (FCFS/EDF/SLO-deadline) within a flow
//     (llm-d three-tier dispatch: band → flow → item).
//   - Work-conserving: an idle fleet serves the lowest class rather than
//     throttling (llm-d flow control guarantees).
//   - Capacity limits (global + per-band) protect the gateway process
//     itself; GPU protection is the caller's saturation gate (spec §12.3.7).
package queue

import (
	"container/heap"
	"errors"
	"sync"
	"time"
)

// Class is a signed traffic class. Class values arrive ONLY from verified
// DARI ingress metadata (spec §13.14); callers outside this package must
// never construct one from client headers.
type Class string

const (
	ClassInteractivePaid   Class = "interactive-paid"   // weight 10
	ClassInteractiveNormal Class = "interactive-normal" // weight 6
	ClassBackgroundAgent   Class = "background-agent"   // weight 2
	ClassBatch             Class = "batch"              // weight 1
)

// classOrder is strict priority, highest first.
var classOrder = []Class{ClassInteractivePaid, ClassInteractiveNormal, ClassBackgroundAgent, ClassBatch}

// Weight returns the DRR quantum weight for a class (spec §12.3.4:
// interactive-paid 10, interactive-normal 6, background-agent 2, batch 1).
func (c Class) Weight() int {
	switch c {
	case ClassInteractivePaid:
		return 10
	case ClassInteractiveNormal:
		return 6
	case ClassBackgroundAgent:
		return 2
	case ClassBatch:
		return 1
	}
	return 1
}

// Workload is the request-size bucket (spec §12.3.5: S 0–8K / M 8–32K /
// L 32–64K / XL 64–128K / IMAGE). Classify by workload; do not pre-partition
// GPUs.
type Workload string

const (
	WorkloadS     Workload = "S"
	WorkloadM     Workload = "M"
	WorkloadL     Workload = "L"
	WorkloadXL    Workload = "XL"
	WorkloadImage Workload = "IMAGE"
)

// Ordering is the within-flow item-selection policy.
type Ordering string

const (
	OrderingFCFS Ordering = "fcfs"
	OrderingEDF  Ordering = "edf"
	OrderingSLO  Ordering = "slo-deadline"
)

// Outcome is the terminal result of a queued request.
type Outcome string

const (
	OutcomeDispatched Outcome = "dispatched"
	OutcomeExpiredTTL Outcome = "expired-ttl"
	OutcomeCancelled  Outcome = "cancelled"
	OutcomeDrained    Outcome = "drained"
)

// Errors returned by Enqueue map to wire-level 429 rejections.
var (
	ErrGlobalLimit = errors.New("queue: global capacity limit exceeded")
	ErrBandLimit   = errors.New("queue: priority-band capacity limit exceeded")
)

// CacheIdentity is the request's cache compatibility identity (mirrors
// the scheduler's CacheIdentity; the queue package must not import the
// scheduler). Zero value = no identity = legacy routing path.
type CacheIdentity struct {
	ModelPackage string
	TokenizerID  string
	TemplateID   string
	AdapterID    string
	PolicyEpoch  string
}

// Request is one admission candidate. Token fields feed the DRR debit
// (input + expected output); Deadline/Ordering feed item selection; TTL
// bounds queue residency.
type Request struct {
	ID                   string
	Tenant               string
	Class                Class
	Region               string        // residency constraint from tenant policy (empty = unconstrained)
	ProgramID            string        // opaque WS3 program identifier (empty = none)
	TurnSeq              int           // program turn sequence
	ToolPaused           bool          // the request ended waiting on a tool (WS3)
	Cache                CacheIdentity // cache compatibility identity from the model package (WS1)
	PrefixHash           string        // tenant-keyed prefix identity from the governed caller (never computed from content here)
	InputTokens          int
	CachedInputTokens    int
	ExpectedOutputTokens int
	MaxOutputTokens      int
	MediaTokens          int
	Deadline             time.Time     // explicit deadline (EDF)
	SLOBudget            time.Duration // SLO-deadline ordering: deadline = arrival + budget
	ArrivedAt            time.Time
	TTL                  time.Duration
	PayloadBytes         int
	Payload              any
}

// Debit is the token cost this request charges against its flow's deficit.
func (r Request) Debit() int {
	d := r.InputTokens + r.ExpectedOutputTokens
	if d <= 0 {
		d = 1
	}
	return d
}

// WorkloadClass buckets the request per spec §12.3.5.
func (r Request) WorkloadClass() Workload {
	if r.MediaTokens > 0 {
		return WorkloadImage
	}
	switch {
	case r.InputTokens <= 8*1024:
		return WorkloadS
	case r.InputTokens <= 32*1024:
		return WorkloadM
	case r.InputTokens <= 64*1024:
		return WorkloadL
	default:
		return WorkloadXL
	}
}

// Limits are the gateway-process resource guardrails (llm-d flow control
// maxBytes/maxRequests; spec §14 row 3, §12.3.7 layer 1).
type Limits struct {
	GlobalMaxRequests int
	GlobalMaxBytes    int
	BandMaxRequests   map[Class]int
	BandMaxBytes      map[Class]int
	// Ordering selects the within-flow item policy per class (default FCFS).
	Ordering map[Class]Ordering
}

// DefaultLimits returns permissive guardrails (large headroom; operators
// tighten per fleet).
func DefaultLimits() Limits {
	return Limits{
		GlobalMaxRequests: 10000,
		GlobalMaxBytes:    1 << 30,
		BandMaxRequests:   map[Class]int{ClassInteractivePaid: 5000, ClassInteractiveNormal: 5000, ClassBackgroundAgent: 2000, ClassBatch: 1000},
		BandMaxBytes:      map[Class]int{ClassInteractivePaid: 512 << 20, ClassInteractiveNormal: 512 << 20, ClassBackgroundAgent: 256 << 20, ClassBatch: 128 << 20},
		Ordering:          map[Class]Ordering{},
	}
}

// Dispatch is the result of Next: the released request and its outcome.
// Expired requests are returned so callers can map them to the correct
// wire error (503 retryable vs 429), mirroring llm-d's QueueOutcome table.
type Dispatch struct {
	Request *Request
	Outcome Outcome
}

// queued is the internal item holding a request plus its flow-local
// ordering key.
type queued struct {
	req      Request
	deadline time.Time // effective deadline for EDF/SLO ordering
	seq      int64     // arrival sequence for FCFS tie-break
	index    int       // heap index
}

// flow is one tenant's queue within a class band.
type flow struct {
	tenant  string
	items   flowHeap
	deficit int
}

type flowHeap []*queued

func (h flowHeap) Len() int { return len(h) }
func (h flowHeap) Less(i, j int) bool {
	if h[i].deadline.Equal(h[j].deadline) {
		return h[i].seq < h[j].seq
	}
	return h[i].deadline.Before(h[j].deadline)
}
func (h flowHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *flowHeap) Push(x any) {
	n := len(*h)
	item := x.(*queued)
	item.index = n
	*h = append(*h, item)
}
func (h *flowHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// band is one priority class: its flows, capacity counters, and RR cursor.
type band struct {
	class    Class
	ordering Ordering
	flows    map[string]*flow
	order    []string // round-robin visitation order
	cursor   int
	requests int
	bytes    int
}

// Queue is the S2 global admission queue. All methods are safe for
// concurrent use: ingress goroutines (Submit) enqueue while the dispatch
// loop pops, so every operation is serialized by mu.
type Queue struct {
	mu         sync.Mutex
	limits     Limits
	bands      map[Class]*band
	seq        int64
	globalReqs int
	globalByt  int
}

// New builds an empty queue with the given limits.
func New(limits Limits) *Queue {
	q := &Queue{
		limits: limits,
		bands:  make(map[Class]*band, len(classOrder)),
	}
	for _, c := range classOrder {
		ordering := limits.Ordering[c]
		if ordering == "" {
			ordering = OrderingFCFS
		}
		q.bands[c] = &band{class: c, ordering: ordering, flows: make(map[string]*flow)}
	}
	return q
}

// Enqueue admits a request into its class band, rejecting immediately when
// global or per-band capacity limits are exceeded (429 rejected-saturated).
func (q *Queue) Enqueue(r Request) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	r = normalize(r)
	b := q.bands[r.Class]
	if b == nil {
		return ErrBandLimit
	}
	if q.globalReqs >= q.limits.GlobalMaxRequests || q.globalByt+r.PayloadBytes > q.limits.GlobalMaxBytes {
		return ErrGlobalLimit
	}
	if maxReqs, ok := q.limits.BandMaxRequests[r.Class]; ok && b.requests >= maxReqs {
		return ErrBandLimit
	}
	if maxBytes, ok := q.limits.BandMaxBytes[r.Class]; ok && b.bytes+r.PayloadBytes > maxBytes {
		return ErrBandLimit
	}

	item := &queued{req: r, deadline: effectiveDeadline(b.ordering, r, q.seq), seq: q.seq}
	f := b.flows[r.Tenant]
	if f == nil {
		f = &flow{tenant: r.Tenant}
		b.flows[r.Tenant] = f
		if !contains(b.order, r.Tenant) {
			b.order = append(b.order, r.Tenant)
		}
	}
	heap.Push(&f.items, item)
	b.requests++
	b.bytes += r.PayloadBytes
	q.globalReqs++
	q.globalByt += r.PayloadBytes
	return nil
}

// Next returns the next dispatchable request: highest non-empty priority
// band, weighted DRR across that band's flows, ordering policy within the
// selected flow. Expired heads are evicted and returned with
// OutcomeExpiredTTL so the caller emits the correct wire error. Returns
// ok=false only when the queue is empty.
func (q *Queue) Next() (Dispatch, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	// Outer loop re-enters the rotation until a request is released. A pass
	// that only accrues deficit must keep rotating: DRR serves a flow once
	// its accumulated quantum covers the head debit (spec §13.5), so a
	// single pass is not a round trip.
	for {
		progress := false
		for _, c := range classOrder {
			b := q.bands[c]
			if b.requests == 0 {
				continue
			}
			// Cycle flows round-robin; a flow gets one visit per pass.
			for i := 0; i < len(b.order); i++ {
				tenant := b.order[b.cursor%len(b.order)]
				b.cursor++
				f := b.flows[tenant]
				if f == nil || len(f.items) == 0 {
					continue
				}
				head := f.items[0]
				if time.Since(head.req.ArrivedAt) > head.req.TTL {
					heap.Pop(&f.items)
					req := head.req
					q.release(b, f, req)
					return Dispatch{Request: &req, Outcome: OutcomeExpiredTTL}, true
				}
				// Token-debited DRR: serve while the deficit covers the debit;
				// otherwise accrue quantum and rotate (spec §13.5).
				if f.deficit < head.req.Debit() {
					f.deficit += q.quantum(c)
					progress = true
					continue
				}
				f.deficit -= head.req.Debit()
				heap.Pop(&f.items)
				req := head.req
				q.release(b, f, req)
				return Dispatch{Request: &req, Outcome: OutcomeDispatched}, true
			}
		}
		if !progress {
			return Dispatch{}, false
		}
	}
}

// contains reports whether s holds v (tiny helper for tenant rotation).
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Cancel removes a queued request by ID (client disconnect, 503
// rejected-context-cancelled). Reports whether the request was present.
func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, c := range classOrder {
		b := q.bands[c]
		for _, tenant := range b.order {
			f := b.flows[tenant]
			for i, item := range f.items {
				if item.req.ID == id {
					heap.Remove(&f.items, i)
					q.release(b, f, item.req)
					return true
				}
			}
		}
	}
	return false
}

// Drain removes every queued request with OutcomeDrained (graceful
// shutdown; 503 rejected-shutting-down on the wire).
func (q *Queue) Drain() []Dispatch {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []Dispatch
	for _, c := range classOrder {
		b := q.bands[c]
		for _, tenant := range b.order {
			f := b.flows[tenant]
			for len(f.items) > 0 {
				item := heap.Pop(&f.items).(*queued)
				req := item.req
				q.release(b, f, req)
				out = append(out, Dispatch{Request: &req, Outcome: OutcomeDrained})
			}
			delete(b.flows, tenant)
		}
		b.order = nil
	}
	return out
}

// Pending returns the number of queued requests.
func (q *Queue) Pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.globalReqs
}

// PendingTokens returns the total token debit queued — the fleet's "true
// demand" signal consumed by overload admission and autoscaling (spec
// §12.3.7; llm-d queue-depth metric).
func (q *Queue) PendingTokens() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	var total int64
	for _, c := range classOrder {
		b := q.bands[c]
		for _, tenant := range b.order {
			f := b.flows[tenant]
			for _, item := range f.items {
				total += int64(item.req.Debit())
			}
		}
	}
	return total
}

// ClassPending returns queued requests per class (observability).
func (q *Queue) ClassPending() map[Class]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[Class]int, len(classOrder))
	for _, c := range classOrder {
		out[c] = q.bands[c].requests
	}
	return out
}

// quantum is the per-round token allowance for one flow, scaled by class
// weight (spec §12.3.4 reference weights).
func (q *Queue) quantum(c Class) int {
	const base = 1024
	return base * c.Weight()
}

// release decrements the shared counters after a request leaves a band.
// Empty flows stay registered (one entry per tenant — bounded by the
// tenant population, never by request volume) so b.order and b.flows stay
// consistent for every iterator; Drain is the only path that removes
// flows. An empty flow is skipped on visitation and reused on re-enqueue.
func (q *Queue) release(b *band, f *flow, r Request) {
	b.requests--
	b.bytes -= r.PayloadBytes
	q.globalReqs--
	q.globalByt -= r.PayloadBytes
}

// normalize fills defaults so ordering and expiry are well-defined.
func normalize(r Request) Request {
	if r.ArrivedAt.IsZero() {
		r.ArrivedAt = time.Now()
	}
	if r.TTL <= 0 {
		r.TTL = time.Minute
	}
	if r.Class == "" {
		r.Class = ClassBatch
	}
	return r
}

// effectiveDeadline computes the item's ordering key. FCFS uses arrival
// sequence (heap ordered by zero deadline + seq tie-break is unnecessary —
// deadlines are all zero, so Less falls back to arrival via seq? no: Less
// only compares deadlines. See fix below.)
func effectiveDeadline(o Ordering, r Request, seq int64) time.Time {
	switch o {
	case OrderingEDF:
		if !r.Deadline.IsZero() {
			return r.Deadline
		}
		return r.ArrivedAt.Add(r.TTL)
	case OrderingSLO:
		if !r.Deadline.IsZero() {
			return r.Deadline
		}
		if r.SLOBudget > 0 {
			return r.ArrivedAt.Add(r.SLOBudget)
		}
		return r.ArrivedAt.Add(r.TTL)
	default: // FCFS
		return r.ArrivedAt
	}
}

// DropClass removes every queued request of the given classes and returns
// the removed requests (multi-level load shedding: retryable overload
// responses for shed work, spec §14 row 38).
func (q *Queue) DropClass(classes ...Class) []Request {
	q.mu.Lock()
	defer q.mu.Unlock()
	drop := make(map[Class]bool, len(classes))
	for _, c := range classes {
		drop[c] = true
	}
	var removed []Request
	for _, c := range classOrder {
		b := q.bands[c]
		if !drop[c] {
			continue
		}
		for _, tenant := range b.order {
			f := b.flows[tenant]
			for len(f.items) > 0 {
				item := heap.Pop(&f.items).(*queued)
				req := item.req
				q.release(b, f, req)
				removed = append(removed, req)
			}
		}
	}
	return removed
}
