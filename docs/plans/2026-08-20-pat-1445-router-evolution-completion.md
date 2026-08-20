# PAT-1445 Router Evolution — Completion Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Execute every open decision item for PAT-1445 — nothing deferred: WorkerFleet consolidation, journal wire identity + KVLookup seam, request cache-identity population, PIA stage execution protocol, program metadata relay path, bounded early rejection, canary controller, web console panels, and the multi-region global stage.

**Architecture:** Each phase extends `internal/scheduler/**` (plus `internal/pia`, `internal/relay`, `internal/models`, `web/`) through the versioned seams already shipped in Phases 0–5: `Router` interface, `KVDirectory`, `NetworkOracle`, `StagePlanner`, `PDController`, `ProgramRegistry`, signed TrafficEnvelope v2, and the S10 observability views. Governance (residency, identity, policy) always precedes performance; every new capability is independently degradable with a conservative fallback and ships behind shadow/canary evidence.

**Tech Stack:** Go 1.2x (`go build ./... && go vet ./... && go test ./... -count=1` must stay green), React/TypeScript (`cd web && npm run build`) for Phase 8 only.

**Decision log (user-approved 2026-08-20):** B1–B6 all = **execute**. Order below is the dependency order: 1 (foundation) → 2 (WS1 live) → 3 (identity live) → 4 (WS2 live) → 5 (WS3 live) → 6 (admission) → 7 (rollout) → 8 (panels) → 9 (global stage).

**Repo conventions:**
- Work on `main` (AGENTS.md: no new branches/worktrees without explicit direction).
- Commit per task or per phase with `feat(scheduler): … (PAT-1445)` / `fix(…)` messages.
- Touch only the files a task lists. Never stage unrelated working-tree changes (other agents work concurrently in this tree).
- Env/secrets files are never modified (company policy).
- Every phase ends with `go build ./... && go vet ./... && go test ./internal/scheduler/... ./internal/pia/... ./internal/relay/... -count=1` green.

---

## Phase 1 (B1) — WorkerFleet consolidation

**Why:** Four modules keep private copies of worker state: `CostRouter.workers` (router.go), `PDPlanner.workers` (pd.go), `WorkerSelector.workers` (dispatch.go), and the registry; removals never propagate. One deep `WorkerFleet` module owns live entries + load; consumers read through it; `SyncRouter` shrinks to one fan-out; eviction becomes one removal event.

### Task 1.1: `WorkerFleet` module

**Files:**
- Create: `internal/scheduler/fleet.go`
- Test: `internal/scheduler/fleet_test.go`

**Step 1: Write the failing test** (`fleet_test.go`)

```go
package scheduler

import "testing"

func TestFleetUpsertRemoveLoad(t *testing.T) {
	f := NewWorkerFleet()
	e := mkWorker("w1", "model-a", 8)
	f.Upsert(e, RouterWorkerState{ActiveRequests: 2})
	got, ok := f.Get("w1")
	if !ok || got.Entry.Card.WorkerID != "w1" || got.State.ActiveRequests != 2 {
		t.Fatalf("get = %+v,%v", got, ok)
	}
	if n := len(f.List()); n != 1 {
		t.Fatalf("list = %d", n)
	}
	f.Remove("w1")
	if _, ok := f.Get("w1"); ok {
		t.Fatal("removed worker still present")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/scheduler/ -run TestFleetUpsertRemoveLoad -v`
Expected: FAIL — `NewWorkerFleet` undefined.

**Step 3: Minimal implementation** (`fleet.go`)

```go
package scheduler

import "sync"

// fleet.go implements the single worker-state module (PAT-1445 B1): one
// authoritative copy of live worker entries + load, fed by the signed
// card feed (SyncRouter) and read by every placement consumer (router,
// P/D planner, selector, topology). Removal propagates to all readers.

// FleetWorker is one worker's entry plus live state.
type FleetWorker struct {
	Entry WorkerEntry
	State RouterWorkerState
}

// WorkerFleet is the fleet-wide worker view. Safe for concurrent use.
type WorkerFleet struct {
	mu      sync.RWMutex
	workers map[string]FleetWorker
}

// NewWorkerFleet builds an empty fleet.
func NewWorkerFleet() *WorkerFleet {
	return &WorkerFleet{workers: make(map[string]FleetWorker)}
}

// Upsert installs or refreshes a worker.
func (f *WorkerFleet) Upsert(e WorkerEntry, s RouterWorkerState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workers[e.Card.WorkerID] = FleetWorker{Entry: e, State: s}
}

// Remove drops a worker (eviction, lease lapse).
func (f *WorkerFleet) Remove(workerID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workers, workerID)
}

// Get returns one worker.
func (f *WorkerFleet) Get(workerID string) (FleetWorker, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.workers[workerID]
	return w, ok
}

// List returns all workers (map iteration order — callers sort when
// deterministic order matters).
func (f *WorkerFleet) List() []FleetWorker {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FleetWorker, 0, len(f.workers))
	for _, w := range f.workers {
		out = append(out, w)
	}
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/scheduler/ -run TestFleetUpsertRemoveLoad -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scheduler/fleet.go internal/scheduler/fleet_test.go
git commit -m "feat(scheduler): WorkerFleet single worker-state module (PAT-1445 B1 task 1.1)"
```

### Task 1.2: Migrate `WorkerSelector` to read from the fleet

**Files:**
- Modify: `internal/scheduler/dispatch.go` (WorkerSelector wraps `*WorkerFleet`)
- Test: existing `dispatch_test.go`, `forward_test.go` must stay green unchanged

**Step 1:** Refactor `WorkerSelector` to hold `*WorkerFleet` instead of its own `workers`/`load` maps; keep its method signatures (`Upsert`, `SetLoad`, `Select`, `Remove`, `Count`, `FreeWorkers`, `WorkerAddr`, `ReserveLoad`, `ReleaseLoad`) as delegating adapters. `SetLoad` writes into the fleet's state. Do not change call sites in this step.

**Step 2:** Run: `go test ./internal/scheduler/ -run 'TestSelectWorker|TestDispatch' -count=1`
Expected: PASS (behavior unchanged).

**Step 3:** Commit: `refactor(scheduler): WorkerSelector reads from WorkerFleet (PAT-1445 B1 task 1.2)`

### Task 1.3: Migrate `PDPlanner.workers` to the fleet

**Files:**
- Modify: `internal/scheduler/pd.go` (planner holds `*WorkerFleet`)
- Modify: `internal/scheduler/stages_test.go`, `observability_view_test.go` (constructor call sites)

**Step 1:** `NewPDPlanner(fleet *WorkerFleet)`; `Place`/`RoleCounts` iterate `fleet.List()`; drop the planner's private map and its `Upsert`/`Remove` (fleet owns them). Fix the two test constructors.

**Step 2:** Run: `go test ./internal/scheduler/ -run 'TestStage|TestPD|TestKVDirView' -count=1`
Expected: PASS.

**Step 3:** Commit: `refactor(scheduler): PDPlanner reads roles from WorkerFleet (PAT-1445 B1 task 1.3)`

### Task 1.4: Migrate `CostRouter` worker tables to the fleet

**Files:**
- Modify: `internal/scheduler/router.go` (`workers`/`state` maps → `fleet *WorkerFleet`)
- Modify: `internal/scheduler/server.go` (`SyncRouter` becomes fleet fan-out)

**Step 1:** Router keeps its maps as the WRITE side (tests use `UpsertWorker` heavily — keep the method) but backs them with the shared fleet: `UpsertWorker` delegates to `fleet.Upsert`, `RemoveWorker` to `fleet.Remove`, `Route` iterates `fleet.List()`. `SyncRouter` then writes each card to the fleet ONCE; selector/planner/topology read it (no more per-consumer upserts). Topology feed stays in `SyncRouter` (card node/zone → inventory, as shipped in step-2 of the protocol loop).

**Step 2:** Run: `go test ./internal/scheduler/ -count=1`
Expected: PASS (all router tests unchanged).

**Step 3:** Commit: `refactor(scheduler): CostRouter reads from WorkerFleet; SyncRouter single fan-out (PAT-1445 B1 task 1.4)`

### Task 1.5: Removal discipline on eviction

**Files:**
- Modify: `internal/scheduler/server.go` (`Sweep`)
- Test: `internal/scheduler/pat1445_regression_test.go` (add eviction case)

**Step 1: Write the failing test** — register two workers, `Sweep` past lease expiry, assert `fleet.Get` reports them gone AND the router no longer routes to them.

**Step 2:** Implement: `Scheduler.Sweep` calls `fleet.Remove(id)` for every evicted worker (registry sweep already returns the IDs; `WorkerSelector.Remove`/`KVIndex.EvictWorker`/`KVDir.EvictWorker` keep their own data consistent via the same call).

**Step 3:** Run: `go test ./internal/scheduler/ -run TestSchedulerSweep -count=1` → PASS.

**Step 4:** Commit: `feat(scheduler): eviction removes workers from every consumer via WorkerFleet (PAT-1445 B1 task 1.5)`

---

## Phase 2 (B3.1 + B2) — Journal wire identity → KVLookup seam

**Why:** The WS1 directory is identity-gated but the journal wire carries no identity, so production can't feed it. Extend the journal (PIA record + `MsgKVJournal` payload) with `CacheIdentity`, dual-apply to legacy index + directory, then collapse the router's two cache branches behind one `KVLookup` seam.

### Task 2.1: Journal record + wire payload identity

**Files:**
- Modify: `internal/pia/kvjournal.go` (`KVJournalRecord` gains `Namespace`, `ModelPackage`, `TokenizerID`, `TemplateID`, `AdapterID`, `PolicyEpoch`)
- Modify: `internal/pia/kvjournal_publish.go` (or wherever `MsgKVJournal` blocks are built — find with `grep -rn "MsgKVJournal" internal/pia/`)
- Modify: `internal/scheduler/listener.go:235-260` (journal apply path)
- Modify: `internal/scheduler/kvindex.go` (`KVBlock` gains no fields — wire maps into directory identity separately)

**Step 1: Write the failing test** (`internal/pia/kvjournal_test.go` addition)

```go
func TestJournalRoundTripsIdentity(t *testing.T) {
	j, _ := OpenKVJournal(filepath.Join(t.TempDir(), "kv.json"))
	id := scheduler.CacheIdentity{ModelPackage: "m@1", TokenizerID: "tok", TemplateID: "tpl", PolicyEpoch: "e1"}
	if _, err := j.AppendWithIdentity("add", "ns", "hash", 100, id); err != nil {
		t.Fatal(err)
	}
	recs := j.Replay()
	if len(recs) != 1 || recs[0].ModelPackage != "m@1" || recs[0].Namespace != "ns" {
		t.Fatalf("replay = %+v", recs)
	}
}
```

**Step 2:** Run → FAIL (method/fields undefined).

**Step 3:** Implement: add the fields (old journals replay fine — absent fields zero-value) + `AppendWithIdentity`; keep `Append` as a compatibility shim calling `AppendWithIdentity` with empty identity.

**Step 4:** Run → PASS. Commit: `feat(pia): journal records carry cache identity (PAT-1445 B3.1 task 2.1)`

### Task 2.2: Wire message + scheduler dual-apply

**Files:**
- Modify: the `MsgKVJournal` payload struct (find with `grep -rn "KVJournal struct\|MsgKVJournal" internal/dari/ internal/scheduler/listener.go`)
- Modify: `internal/scheduler/listener.go` (also call `svc.KVDir.ApplyJournal(workerID, seq, blocks, identity)`)
- Test: extend the listener journal test with identity blocks

**Step 1: Write the failing test** — publish a journal with identity; assert `svc.KVDir.OverlapTokens(worker, ns, hash, identity) > 0` and legacy `svc.KV.OverlapTokens` also sees it.

**Step 2:** Run → FAIL.
**Step 3:** Implement payload field + dual-apply (legacy path unchanged when identity empty).
**Step 4:** Run → PASS. Commit: `feat(scheduler): journal feed dual-publishes to the WS1 directory (PAT-1445 B3.1 task 2.2)`

### Task 2.3: `KVLookup` seam in the router

**Files:**
- Modify: `internal/scheduler/router.go` (replace `kv`/`kvdir` fields with one seam)
- Create: `internal/scheduler/kvlookup.go`
- Test: `internal/scheduler/kvlookup_test.go`

**Step 1: Write the failing test**

```go
func TestKVLookupDirectoryPreferredWithIdentity(t *testing.T) {
	legacy := NewKVIndex()
	dir := NewKVDirectory()
	legacy.Add("w1", KVBlock{Namespace: "t", Hash: "h", Tokens: 100})
	dir.Add("w1", L1GPU, KVBlock{Namespace: "t", Hash: "h", Tokens: 800}, testIdentity, true)
	lookup := NewKVLookup(legacy, dir)
	// Identity present: directory answers (800, not the legacy 100).
	if got := lookup.OverlapTokens("w1", "t", "h", testIdentity); got != 800 {
		t.Fatalf("overlap = %d, want directory 800", got)
	}
	// No identity: legacy fallback (100).
	if got := lookup.OverlapTokens("w1", "t", "h", CacheIdentity{}); got != 100 {
		t.Fatalf("overlap = %d, want legacy 100", got)
	}
}
```

**Step 2:** Run → FAIL.
**Step 3:** Implement `KVLookup` (interface: `OverlapTokens(worker, ns, hash string, id CacheIdentity) int`, `WorkersWithMedia(ns, hash, media string, id CacheIdentity) []string`) with a composite adapter encoding exactly today's fallback policy; router calls the seam in both scoring branches (no more `if kvdir … else if kv`). `SetKV`/`SetKVDirectory` become wiring shims building the composite (existing tests stay green).
**Step 4:** Run: `go test ./internal/scheduler/ -count=1` → PASS.
**Step 5:** Commit: `refactor(scheduler): KVLookup seam unifies legacy index and WS1 directory (PAT-1445 B2 task 2.3)`

---

## Phase 3 (B3.2) — RouteRequest.Cache population from PAT-1444 packages

**Why:** Requests need a `CacheIdentity` for the directory to engage. `internal/models/model_registry.go` `ModelPackage` already has `TokenizerDigest`/`ChatTemplateDigest`; the gateway needs an offline-capable package view to build the identity per request.

### Task 3.1: `PackageSource` interface + gateway identity plumbing

**Files:**
- Create: `internal/scheduler/packages.go`
- Modify: `internal/scheduler/gateway.go` (after `resolveTenantClass`, resolve the model's package → `qr` cache identity)
- Modify: `internal/scheduler/queue/queue.go` (`Request` gains `Cache scheduler-free identity struct` — put the identity struct in the queue package or pass the five strings; prefer a small `CacheIdentity` mirror struct in `queue` to avoid an import cycle)
- Modify: `internal/scheduler/dispatch.go` (`assignLocked` maps it into `RouteRequest.Cache`)
- Test: `internal/scheduler/packages_test.go`

**Step 1: Write the failing test** — a fake `PackageSource` returns package info for `model-a`; an HTTP chat request produces an arrived trace event whose `RouteRequest.Cache` is populated (assert via a router with directory overlap, or expose the identity on the trace event).

**Step 2:** Run → FAIL.
**Step 3:** Implement:

```go
// PackageSource resolves a served model to its signed package identity
// (offline-capable: the scheduler reads a local view; no CP call on the
// request path). Missing package = zero identity = legacy routing path.
type PackageSource interface {
	PackageFor(model string) (PackageIdentity, bool)
}

// PackageIdentity is the scheduler-side view of a signed model package.
type PackageIdentity struct {
	ModelPackage string
	TokenizerID  string
	TemplateID   string
	PolicyEpoch  string
}
```

Gateway `SetPackageSource`; on resolve, populate the queued request's identity; dispatcher maps to `RouteRequest.Cache`.

**Step 4:** Run → PASS. Commit: `feat(scheduler): requests carry model-package cache identity (PAT-1445 B3.2 task 3.1)`

### Task 3.2: Wire the PAT-1444 registry as the production PackageSource

**Files:**
- Modify: `cmd/pccp-scheduler/main.go` or the server composition (find where `NewScheduler` is constructed)
- Read first: `internal/models/model_registry.go` (exact field names: `TokenizerDigest`, `ChatTemplateDigest`, package name/version fields)

**Step 1:** Read `model_registry.go` and record the exact fields for name/version/tokenizer/template.
**Step 2:** Implement a `modelsPackageSource` adapter (in the composition package, not `internal/scheduler`) reading the local registry DB view.
**Step 3:** Test: unit-test the adapter against a seeded registry.
**Step 4:** Commit: `feat(scheduler): PAT-1444 model registry backs cache identity (PAT-1445 B3.2 task 3.2)`

---

## Phase 4 (B3.3) — PIA stage execution protocol

**Why:** Disaggregated plans are evidence-only until the forwarder can execute prefill→transfer→decode. This makes WS2 real, behind the canary controller (Phase 7).

### Task 4.1: Stage-aware forwarder interface

**Files:**
- Modify: `internal/scheduler/forward.go` (`Forwarder` gains a stage-scoped method)
- Read first: `internal/scheduler/dari_forwarder.go` (production forwarder)
- Test: `internal/scheduler/stages_exec_test.go`

**Step 1: Write the failing test** — a fake stage forwarder records calls; a disaggregated dispatch produces `SendPrefill(w-pre)` then `SendDecode(w-dec, handle)` with stage timestamps in the trace (`lookup`, `prefill`, `transfer`, `decode` events).

**Step 2:** Run → FAIL.

**Step 3:** Implement:

```go
// StageForwarder executes a two-stage disaggregated plan (WS2): prefill
// on one worker returns an opaque KV handle; decode consumes it on the
// router's chosen worker. The transfer between them is engine-internal
// (NIXL/RDMA); the scheduler prices it via NetworkOracle and records
// stage timestamps. A plan that fails mid-stage falls back to a
// co-located retry on the decode worker (conservative).
type StageForwarder interface {
	Forwarder
	SendPrefill(workerAddr string, payload InferencePayload) (kvHandle string, err error)
	SendDecode(workerAddr, kvHandle string, payload InferencePayload) (InferenceResult, error)
}
```

`execute()`: when `plan.Mode == StageDisaggregated` AND the forwarder implements `StageForwarder`, run the two-stage path with per-stage trace events; any stage error → co-located `Send` fallback.

**Step 4:** Run → PASS. Commit: `feat(scheduler): two-stage disaggregated execution with co-located fallback (PAT-1445 B3.3 task 4.1)`

### Task 4.2: PIA stage endpoints

**Files:**
- Read first: `internal/pia/workeragent.go` (request handling shape)
- Modify: `internal/pia/workeragent.go` (+ engine adapter seam)
- Test: `internal/pia/` stage tests

**Step 1:** Read `workeragent.go` and the engine adapter; record how inference requests are dispatched to the engine today.
**Step 2:** Implement `prefill`/`decode` request kinds returning/consuming the KV handle (the engine adapter must advertise the capability — aggregated engines without P/D support reject with a capability error, and the scheduler falls back co-located per Task 4.1).
**Step 3:** Tests: fake engine verifies the round-trip + unsupported engines refuse.
**Step 4:** Commit: `feat(pia): stage-scoped prefill/decode request handling (PAT-1445 B3.3 task 4.2)`

### Task 4.3: DARI forwarder stage transport

**Files:**
- Modify: `internal/scheduler/dari_forwarder.go`

**Step 1:** Implement `SendPrefill`/`SendDecode` over the existing DARI client (new message types only if the worker agent from Task 4.2 requires them — keep backward compatibility: workers without stage support answer with an error the scheduler maps to co-located fallback).
**Step 2:** Tests with a fake DARI peer.
**Step 3:** Commit: `feat(scheduler): DARI transport for stage execution (PAT-1445 B3.3 task 4.3)`

---

## Phase 5 (B3.4) — Program metadata relay path

**Why:** Envelope v2 supports `ProgramMeta` but nothing populates it; the relay must carry it from the harness's authorized request.

### Task 5.1: Relay populates ProgramMeta

**Files:**
- Read first: `internal/relay/traffic_envelope.go` (`signTrafficEnvelope` callers)
- Modify: `internal/relay/traffic_envelope.go` and the caller that has harness request metadata

**Step 1:** Read the caller chain: `grep -rn "signTrafficEnvelope" internal/relay/` — record what harness-supplied fields exist at the call site (session/task identifiers the harness is authorized to claim).
**Step 2: Write the failing test** — a harness request carrying program metadata produces a signed envelope whose `Program` verifies.
**Step 3:** Implement: relay accepts the harness's *opaque* program ID/turn/parent/tool-pause (never content), validates boundedness (length caps, no free text), and calls `env.SetProgram(...)` before signing.
**Step 4:** Run → PASS. Commit: `feat(relay): signed envelopes carry governed program metadata (PAT-1445 B3.4 task 5.1)`

### Task 5.2: End-to-end program turn test

**Files:**
- Test: `internal/scheduler/gateway_test.go` (envelope-with-program → registry state)

**Step 1:** Test: signed envelope with `ProgramMeta` → gateway → dispatcher → `ProgramRegistry.Paused(programID)` true after a tool-paused completion (uses the Phase-4 hook).
**Step 2:** Run → PASS (expected to pass already if Task 5.1 plumbed correctly — it's the integration proof).
**Step 3:** Commit: `test(scheduler): end-to-end program metadata flow (PAT-1445 B3.4 task 5.2)`

*(Out-of-repo: patty-code harness emits the opaque program IDs — tracked as a patty-code task; this repo's relay+scheduler side is complete without it.)*

---

## Phase 6 (B5) — Bounded early rejection

**Why:** Acceptance criterion: admission rejects early with an honest retryable reason when NO eligible path can meet policy/SLO — today the request requeues until TTL regardless of whether the ineligibility is transient (load) or permanent (no such model/region).

### Task 6.1: Transient vs permanent ineligibility in the router

**Files:**
- Modify: `internal/scheduler/router.go` (no-route error carries the eligibility report)
- Modify: `internal/scheduler/eligibility.go` (classify reasons: permanent = model/region/pool/gang/not-servable; transient = overloaded/slo-risk)
- Test: `internal/scheduler/eligibility_test.go`

**Step 1: Write the failing test** — route for an unserved model yields a `RouteError{Permanent: true}` with the filtered counts; route with all workers saturated yields `Permanent: false`.

**Step 2:** Run → FAIL.

**Step 3:** Implement:

```go
// RouteError reports a no-placement outcome with the eligibility
// evidence (WS3 §bounded early rejection: honest, retryable reasons).
type RouteError struct {
	Model       string
	Permanent   bool // no eligible path exists at any load level
	Eligibility *EligibilityReport
}

func (e *RouteError) Error() string { … }
```

`Route` returns `*RouteError` instead of `fmt.Errorf`; permanence = every filter reason is in the permanent set (model/region/pool/gang/not-servable); any transient reason (overloaded, slo-risk) makes it retryable-later.

**Step 4:** Run → PASS (plus existing no-route test updated).

### Task 6.2: Dispatcher + gateway rejection path

**Files:**
- Modify: `internal/scheduler/dispatch.go` (`assignLocked`: permanent no-route → complete waiter with honest error + trace `rejected`, no requeue; transient → existing requeue)
- Modify: `internal/scheduler/gateway.go` (the parked handler surfaces the rejection as 503 with the reason)
- Modify: `internal/scheduler/trace.go` (`TraceRejected` stage)
- Test: dispatcher/gateway tests for both permanence classes

**Step 1: Write the failing test** — submit for an unserved model completes quickly with a "no eligible path" error (not a 60s TTL wait); submit with all-busy stays queued (late binding preserved).

**Step 2:** Run → FAIL.
**Step 3:** Implement.
**Step 4:** Run → PASS. Commit: `feat(scheduler): bounded early rejection with honest retryable reasons (PAT-1445 B5)`

---

## Phase 7 (B4) — Canary controller

**Why:** Acceptance criterion: per-capability canaries with thresholds, observation windows, automatic pause/rollback, and audit. Shadow mode (Phase 0) supplies the comparison stream; the controller promotes a candidate from shadow to scoped-active and pulls it back on regression.

### Task 7.1: `CanaryController`

**Files:**
- Create: `internal/scheduler/canary.go`
- Test: `internal/scheduler/canary_test.go`

**Step 1: Write the failing test** — a candidate with ≥N shadowed receipts at ≥threshold agreement promotes to active for its scope; a candidate below threshold after minSamples auto-pauses with an audit record.

**Step 2:** Run → FAIL.

**Step 3:** Implement:

```go
// CanaryConfig gates one capability's promotion (PAT-1445 §canary: one
// independent capability at a time, explicit thresholds, observation
// windows, automatic pause/rollback, operator review).
type CanaryConfig struct {
	Capability      string  // e.g. "stage-planner/v1"
	Candidate       Router  // the shadowed candidate
	ScopePool       string  // model/pool scope ("" = scheduler-wide, NOT default)
	MinSamples      int     // receipts required before any promotion
	MinAgreement    float64 // shadow agreement rate required to promote
	Window          time.Duration
}

// CanaryController evaluates the receipt stream and activates/pauses.
// All transitions are evidence-audited; pause always wins over promote.
```

State machine: `shadow → evaluating → active(scope) → paused`; evaluation consumes `ReceiptStore.Recent()` filtered by window; `Active()` reports whether the candidate currently decides for its scope (dispatcher consults it like the shadow path, scoped).

**Step 4:** Run → PASS. Commit: `feat(scheduler): canary controller with thresholds, windows, auto-pause (PAT-1445 B4 task 7.1)`

### Task 7.2: Wire + audit + view

**Files:**
- Modify: `internal/scheduler/server.go` (Scheduler owns the controller; evidence log records transitions)
- Modify: `internal/scheduler/observability.go` (`ShadowView` gains canary state)
- Test: transition audit test

**Step 1:** Wire with stage-planner as the first capability (it already produces shadow-grade plans).
**Step 2:** Test: transitions emit evidence events; view reports state.
**Step 3:** Commit: `feat(scheduler): canary wiring, audit, and view (PAT-1445 B4 task 7.2)`

---

## Phase 8 (B6i) — Web console panels

**Why:** The JSON views exist; operators need them in the console.

### Task 8.1: API client + types

**Files:**
- Modify: `web/src/api.ts` (fetchers for `/api/v1/kvdir`, `/api/v1/pd`, `/api/v1/programs`, `/api/v1/shadow` matching the Phase-5 JSON shapes)

**Step 1:** Add typed fetchers (`KVDirSummary`, `PDModelView[]`, `ProgramsView`, `ShadowView`).
**Step 2:** `cd web && npm run build` → green.

### Task 8.2: Scheduler panels

**Files:**
- Modify: `web/src/pages/Fleet.tsx` (or create `web/src/pages/Scheduler.tsx` if Fleet's shape doesn't fit — read Fleet.tsx first and follow its existing card/table patterns)
- Modify: `web/src/App.tsx` (route, if a new page) and `web/src/components/Layout.tsx` (nav, if a new page)

**Step 1:** Read `Fleet.tsx` and one existing view test (e.g. `allowedModelView.test.mjs`) for the house pattern.
**Step 2:** Panels: KV tier occupancy + hot prefixes; P/D capacity/imbalance per model; program/tool-pause counters; shadow agreement + policy versions. Each panel renders a degraded state when its view 404s (older scheduler).
**Step 3:** `cd web && npm run build && npm test` → green.
**Step 4:** Commit: `feat(web): scheduler KV/P-D/program/shadow panels (PAT-1445 B6i)`

---

## Phase 9 (B6ii) — Multi-region global stage

**Why:** The issue's hierarchy level 1: region health and preauthorized failover before cluster/worker selection. The residency constraint seam (Phase 1 of the original work) is the entry point; this adds the region *selection* layer above it.

### Task 9.1: Region registry + failover policy

**Files:**
- Create: `internal/scheduler/regions.go`
- Test: `internal/scheduler/regions_test.go`

**Step 1: Write the failing test** — a request constrained to an unhealthy region fails over ONLY to a preauthorized region; an unauthorized failover refuses with a clear availability error (never silently crosses).

**Step 2:** Run → FAIL.

**Step 3:** Implement:

```go
// RegionRegistry tracks region health and the preauthorized failover
// map (signed config; PAT-1445: cross-region failover occurs only to
// preauthorized regions).
type RegionRegistry struct { /* health map + failover allow-list */ }

// SelectRegion applies deployment/tenant boundary, residency, health,
// and permitted failover — the global stage above cluster/worker
// routing. Returns "" with ok=false when no authorized region can serve.
func (r *RegionRegistry) SelectRegion(want string, healthy func(string) bool) (string, bool)
```

**Step 4:** Run → PASS. Commit: `feat(scheduler): region registry with preauthorized failover (PAT-1445 B6ii task 9.1)`

### Task 9.2: Global stage in routing + hierarchical receipts

**Files:**
- Modify: `internal/scheduler/router.go` (region selection before worker filters; receipt gains the hierarchical path)
- Modify: `internal/scheduler/eligibility.go` (region-unhealthy reason)
- Test: router global-stage tests

**Step 1: Write the failing test** — receipt records `Path{Region, Pool, Worker}`; failover only to authorized regions.

**Step 2:** Run → FAIL.
**Step 3:** Implement `RoutingReceipt.Path` (signed body updated), router consults `RegionRegistry` when installed.
**Step 4:** Run → PASS. Commit: `feat(scheduler): global region stage with hierarchical receipts (PAT-1445 B6ii task 9.2)`

---

## Final verification (all phases)

```bash
go build ./... && go vet ./... && go test ./... -count=1   # all green
cd web && npm run build                                     # green (Phase 8)
```

Then: update PAT-1445 with per-phase completion comments (same discipline as Phases 0–5), tick the acceptance criteria each phase completes, and run the **team-protocol loop once** over the whole plan's diff (code-review → structure-code → improve-codebase, fix all findings, re-run each until clean).

## Risks / honest notes

- Phases 2, 3, 4, 5 touch signed wire formats (journal, forwarder, envelope). Each has a backward-compatibility shim (old journals replay, workers without stage support fall back co-located, envelopes without programs verify).
- Phase 4's real disaggregation depends on engine capability advertisement; engines without P/D support always fall back co-located — never a misroute.
- Phase 9's failover map source is signed config; until deployments supply it, `SelectRegion` with an empty registry returns the wanted region unchanged (no behavior change).
- The patty-code harness program-ID emission is out-of-repo; the relay+scheduler path is complete and tested without it.
