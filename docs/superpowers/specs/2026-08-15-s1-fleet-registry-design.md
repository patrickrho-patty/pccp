# S1 — Fleet Registry & Worker Capability Cards (Design)

**Date:** 2026-08-15
**Status:** Approved (in-session)
**Branch:** `forge/dynamo-fabric`

## 1. Context & Vision

PCCP is a Go governance/control plane for AI coding (Korean market): Control Plane (admin),
DARI Relay (data plane), and PIA (inference agent proxying to a single vLLM today). DARI
(CBOR + COSE-Sign1 over QUIC/TCP) already carries enrollment, leases, policy, and provenance.

The product goal is a distributed inference orchestration platform — best of Dynamo +
llm-d — integrated with PCCP: **route → queue → cache → schedule → transfer → scale →
recover → observe**, with DARI governance wrapped around every decision. Reference
implementations live in `tools/dynamo` (Rust, NVIDIA) and `tools/llm-d` (Go,
kubernetes-sigs); we adopt their ideas and proven solutions, not their source.

The full vision decomposes into 11 sub-projects, each with its own
spec → plan → implementation cycle:

| # | Sub-project | Depends on |
|---|---|---|
| S1 | Fleet registry + worker capability cards + signed worker config | — |
| S2 | Unified gateway (OpenAI/Anthropic ingress, model rewriting/A-B/canary, media controls, embeddings, model discovery) + global admission + late-binding queue (fair priority classes, token-debited DRR, tenant QoS, SLO ordering, two-layer overload protection, fail-closed) | S1 |
| S3 | Cost-model router (uncached prefill + projected decode KV + active requests − KV credits) + exact KV index (engine-restart-safe) + media-hash routing + breakable session affinity + LoRA affinity + EP/DP-rank/gang awareness + topology inventory | S1, S2 |
| S4 | Online Bayesian latency prediction (per serving config, heterogeneous fleet, zero-hop, variance → P(SLO violation)) | S1, S3 |
| S5 | SLO- and MTP-aware scheduling (TTFT/ITL objectives, agent priority) | S2, S3, S4 |
| S6 | Aggregated-first serving + conditional P/D (SGLang caveat) + tiered KV fabric + encoder cache + NIXL/RDMA transfer | S3 |
| S7 | Dual-loop autoscaling (forecast floor + burst fast-loop; MTP-aware; warm spare) + heterogeneous/WVA + fast model actuation (residency, snapshots) + engine lifecycle + LoRA lifecycle + power/cost + RL weight updates | S1, S4, S5 (+S6 for P/D scaling) |
| S8 | Resilience: request migration, cancellation propagation, shadow failover, health probing | S2–S6 |
| S9 | Batch/async gateway with slack-capacity filling + pause/resume | S2, S8 |
| S10 | Observability, routing explainability, admin control plane (fleet/models/traffic/cache/perf/routing/scaling views) | all |
| S11 | Digital-twin simulation + benchmark autotuning (queue/KV/P-D/autoscaler simulation — increments as S3/S6/S7 land) | S1, S5 |
| S12 | Kubernetes-native adapter: InferencePool/Gateway API (GAIE v1.5 conformance), CRDs (ModelDeployment, ServingVariant, RoutingPolicy, KVCachePolicy, InferenceSLO, ScalingPolicy, MediaPolicy), HPA/KEDA | S2, S7 |

**This spec covers S1 only.**

## 2. Reference Sources & Adoption Map

Reference codebases are vendored in `tools/` (this worktree). We study their logic and
designs and re-implement the ideas in Go/DARI — never copy source. `tools/dynamo` is the
Rust source tree; `tools/llm-d` is a docs/proposals-only checkout (no Go tree — consult
upstream `kubernetes-sigs/llm-d` for source when needed). Versioned reference points at
review time: Dynamo 1.3.1, llm-d 0.8, Gateway API Inference Extension (GAIE) v1.5.0 (GA).

### 2.1 S1 direct references

| S1 feature | Reference (tools/) | What we adopt |
|---|---|---|
| Registry + lease/eviction semantics | `dynamo/lib/llm/src/discovery/{controller,watcher,worker_set,worker_monitor,model_manager}.rs` | Event-driven, watch-based discovery (no polling), lease expiry → eviction |
| Capability card as registration unit | `dynamo/lib/llm/src/discovery/endpoint_card.rs`, `dynamo/lib/llm/src/model_card.rs` (`ModelDeploymentCard`: display name, model info, tokenizer, chat template, gen config, architectural context length, runtime config) | Card schema + `list_and_watch` consumption pattern (S3 router will watch the registry the same way) |
| Worker lifecycle | `dynamo/lib/backend-common/src/worker.rs` | register → publish card → keepalive → shutdown cleanup |
| GPU inventory | `dynamo/lib/backend-common` NVML/nvidia-smi inventory | SKU/count/HBM fields |
| Pool concept + membership | `llm-d/docs/architecture` (InferencePool, EndpointSlices); `llm-d/proposals/non-kubernetes-mode.md` | Pool/tenant binding in the signed config; non-K8s-first sequencing confirmed |
| Capability-matched selection | `llm-d/docs/architecture` (gateway backend selection) | Card fields designed as the match surface for S3 |

### 2.2 Full adoption matrix (original 15 core capabilities → sub-projects)

| # | Core capability (original list) | Sub-project | Primary reference |
|---|---|---|---|
| 1 | Global fair queue with late GPU binding | S2 | `llm-d/docs/architecture` Flow Control; Dynamo strict-priority tiers |
| 2 | Exact fleet-wide KV-cache map | S3 | `dynamo/lib/kv-router`, `dynamo/lib/kv-hashing`, `kvbm-*` |
| 3 | KV + session + topology + load-aware placement | S3 | Dynamo router; llm-d precise KV mode |
| 4 | Online learned TTFT/TPOT prediction | S4 | llm-d latency predictor; `llm-d/proposals/llm-d-planner.md` |
| 5 | SLO-aware routing | S5 | Dynamo Planner TTFT/ITL objectives (`dynamo/components/src`, `examples/global_planner`) |
| 6 | MTP-aware capacity modeling | S5 | Dynamo Planner MTP acceptance-length |
| 7 | Tiered KV fabric + P2P transfer | S6 | `dynamo/lib/kvbm-*` (engine/physical/logical), `kv-hashing`; llm-d tiered-cache docs |
| 8 | Media/encoder-aware caching + routing | S3 (routing) + S6 (encoder cache) | Dynamo multimodal KV routing (Qwen3.6/SGLang path) |
| 9 | Aggregated + P/D disaggregated serving | S6 | Dynamo P/D via NIXL (`dynamo/deploy/pre-deployment/nixl`); llm-d P/D routing |
| 10 | NIXL/RDMA-aware state movement | S6 | Dynamo NIXL integration |
| 11 | Heterogeneous cost/SLO-aware autoscaling | S7 | `llm-d/proposals/autoscaler.md` (WVA); Dynamo Planner |
| 12 | Fast model actuation (weight residency, snapshots) | S7 | `dynamo/lib/gpu_memory_service`, `dynamo/lib/memory`; Dynamo Snapshot |
| 13 | Request migration + failure recovery | S8 | Dynamo request migration; `llm-d/proposals/inference-resilience-operator.md` |
| 14 | Batch/async workload consolidation | S9 | `llm-d/proposals/batch-gateway.md`, `llm-d/proposals/llm-d-async.md` |
| 15 | Full-fleet simulation, replay, autotuning | S11 | `dynamo/lib/mocker`, `dynamo/aisimulate`; llm-d inference simulator |

### 2.3 S1 coverage of the original 40-section list

S1 implements a slice of these original sections, extended by the security work
(signed config + measured reachability) that neither reference system has:

- **§34 Service Discovery (core)** — dynamic worker registration, capability
  registration, health leases, model metadata, automatic stale-worker removal;
  DP/TP rank and cache-capability fields reserved in the card for S3.
- **§13 Multi-Engine Runtime (capability card v1)** — engine kind/version, GPU,
  precision, context, modalities, TP/DP/EP; MTP/LoRA/P/D optional.
- **§24 Failure Management (worker-level subset)** — health via heartbeat-as-card,
  lease-expiry eviction, quarantine; full drain/probing is S8.
- **§40 Administrative Control Plane (Fleet subset)** — Workers page: GPUs, nodes,
  engine versions, reachability, lease status.
- **§21 Engine Lifecycle (first operation only)** — registration is the first
  lifecycle event; the rest of the lifecycle (sleep/wake/reload) is S7.
- **§32/§33 (deployment modes)** — non-Kubernetes first; the card schema is shaped so
  a K8s adapter can map pods → cards later.

Everything else in the 40 sections belongs to S2–S11 per the matrix above.

## 3. Design Principles

1. **Security is woven through, never bolted on.** Enrollment, signed config, measured
   reachability, and evidence ride along every later capability (routing, KV fabric,
   autoscaling). This is PCCP's differentiator versus Dynamo/llm-d.
2. **Minimum slice per milestone.** Each sub-project builds only what its milestone needs.
   Schemas are forward-compatible so later sub-projects extend, never rework. Zero
   speculative code.
3. **DARI-native from day one.** Worker registration uses the DARI paper
   client/server — no HTTP-then-migrate.
4. **Hot state is ephemeral by design.** The registry is in-memory + leases; workers
   re-register on restart. No restore/durability code.
5. **Every admission outcome is evidence.** register/deny/evict/degrade events land on
   the existing event spine.

## 4. Components

| Piece | What it is | Reuses |
|---|---|---|
| `cmd/pccp-scheduler` | New binary: in-memory worker registry, lease manager, offline PPC verifier, DARI listener (workers), HTTP API (CP) | `internal/dari` paper client/server |
| `internal/scheduler/` | Registry, admission ladder, capability-card validator, lease/eviction | `internal/policy` canonical-signing pattern |
| PIA worker-agent mode | New mode in existing binary: DARI registration client, engine introspection (`/v1/models`, `/metrics`), host GPU info (NVML), socket-reachability measurement, signed-config loader + verifier, lease renewer | `internal/pia` |
| CP additions | Config-signing endpoint + new `config` key domain; worker read-through API; tenant policy field; revocation feed; events | `internal/keymgmt`, `internal/identity`, `internal/policy`, `internal/events` |

## 5. Trust Model & Admission

**Worker identity:** PIA is the unit of trust. Engines bind to `127.0.0.1` in production
(co-located with PIA, as Dynamo/llm-d deploy their worker frontends) and are never
network-exposed. Backend reachability has three grades:

- `localhost` — engine bound to `127.0.0.1`, PIA on same host (production grade)
- `private` — engine on a private NIC, firewall allowlisted to PIA only (test/transitional)
- `mtls` — engine behind an mTLS sidecar (cross-site)

**Admission ladder** (a random process cannot attach; each rung rejects):

1. DARI handshake presents a COSE-Sign1 PPC (`internal/dari/handshake.go` already
   exchanges credentials) → no PPC = rejected.
2. PPC verifies against this CP's trust domain (enrollment exists via
   `EnrollHarness`, `internal/identity/service.go`).
3. Enrollment active (revocation list consulted).
4. Signed config from this CP validates (config *is* authorization: which models,
   backend mode, tenant/pool binding).
5. Tenant policy gate (`min_reachability`, registration allowed) → violation =
   **quarantined**: admitted but flagged non-compliant (excluded from serving pools
   once routing exists in S3).

Rungs 1–4 fail = **deny** (rejected). Rung 5 fails = **quarantine** (admitted,
visible, non-compliant).

The scheduler verifies PPCs **offline** against the CP public key with a periodically
synced revocation list — the fleet keeps working during CP outages, and revocation
propagates at sync cadence.

## 6. Capability Card v1

Signed with COSE-Sign1 over canonical bytes (same pattern as
`internal/policy/canonical.go`). Fields:

| Group | Fields | Status |
|---|---|---|
| identity | worker ID, enrollment ID, PPC fingerprint | required |
| host | node ID, hostname, IP, region/zone | region/zone optional |
| engine | kind (vllm/sglang/trt-llm), version, endpoint URL, reachability mode (configured) + measured grade | required |
| model | name, version, precision, context length, max concurrent seqs, modalities, TP/DP/EP; MTP, LoRA list, P/D role optional | optional fields reserved for S3–S8 |
| gpu | accelerator family (nvidia/amd/intel/tpu), SKU, count, HBM per GPU; NVLink/topology deferred to S3 | family required — multi-accelerator neutrality from day one (§31) |
| health | status, last heartbeat, lease expiry | required |
| signature | COSE-Sign1 over canonical card | required |

## 7. Registration Protocol & Leases

```
PIA                                    pccp-scheduler                Control Plane
 │── DARI AUTH handshake (PPC) ───────►│ verify PPC offline (pubkey  │
 │                                      │  + synced revocation list)  │
 │── WORKER_REGISTER {card, config} ──►│ 1 card sig   2 enrollment   │
 │                                      │ 3 config sig 4 policy gate  │
 │◄── REGISTER_OK {lease TTL} ─────────│                             │
 │── WORKER_HEARTBEAT {fresh card} ───►│ renew lease, bump version    │
 │   (every TTL/3)                      │ TTL expiry → evict + event  │
```

- **Heartbeat carries the full card** — the card *is* the health signal. A worker whose
  engine died fails introspection on the next heartbeat and is re-admitted as degraded
  or evicted. No separate health-check system in S1 (active probing is S8).
- **Lease TTL:** 30s; heartbeat every 10s; re-registration grace 2×TTL so brief
  scheduler restarts cause no mass churn (workers' heartbeats re-populate the registry —
  no restore code).
- **Evidence:** `worker.register` / `worker.deny` / `worker.evict` / `worker.degrade`
  events on the existing spine with receipts.

## 8. Signed Config & Socket Verification

**Envelope:** `{config, signature, cp_key_id}` — canonical JSON signed with COSE-Sign1
under a new key domain `config` in `internal/keymgmt`. PIA verifies at boot and
**fails closed** (mismatch/absent in production profile = refuse to start, log
evidence). Dev profile allows unsigned config but still measures and reports.

**Config is authorization:** allowed models, backend mode, tenant/pool binding. A
stolen PPC alone admits nothing.

**Measured reachability:** config can claim `localhost` while the engine binds
`0.0.0.0`, so PIA reads `/proc/net/tcp{,6}` for the engine port's actual bind address
and puts the **measured** grade in the capability card. The registry policy rejects
mismatches — a config file cannot lie about the socket.

**Distribution:** out-of-band (admin downloads envelope, installs on host) in S1 —
deliberately human-in-the-loop. DARI push is a later option.

## 9. CP Integration & Admin UI

- **API:** `POST /api/v1/scheduler/configs` (admin signs PIA config → envelope);
  `GET /api/v1/workers` (read-through from scheduler); tenant policy gains
  `min_reachability`; revocation feed the scheduler polls.
- **UI:** one new *Workers* page — fleet table (ID, host, model, engine, reachability
  grade, lease status, last heartbeat). Click-through detail view: full card, recent
  admission events, and the edit-config signing flow (edit → sign → download → install
  on host). No dashboards/charts in S1.
- **Shared policy** (reachability minimum, lease TTL, quotas) lives on the existing
  tenant/policy pages. **Engine tuning** is out of scope — engines are launched
  externally until S7 lifecycle management.

## 10. Testing

- **Unit:** card canonicalization + signature; each admission rung (no PPC → reject,
  bad config sig → reject, policy fail → quarantine); lease-expiry eviction.
- **Integration:** fake engine (HTTP stub with `/v1/models` + `/metrics`) + real PIA +
  real scheduler on localhost: register → heartbeat → TTL eviction; assert measured
  `exposed` grade when the fake engine binds `0.0.0.0`.
- **Conformance:** black-box runner under `conformance/` (existing repo pattern)
  starting all three components and asserting admission behavior.
- **Failure modes:** scheduler restart → re-registration within TTL; CP down →
  scheduler keeps admitting via offline PPC verification.

## 11. Out of Scope (explicit)

| Deferred | To |
|---|---|
| Topology/NVLink/IB inventory | S3 (router needs it) |
| Active health probing | S8 |
| Registry persistence, HA scheduler | later |
| K8s/CRD adapter (llm-d style) | S12 (GAIE v1.5 + CRDs + HPA/KEDA; non-K8s core first — gov/bare-metal market) |
| DARI push of signed configs | later |
| Queue, routing, KV, autoscaling — everything else | S2–S11 |

## 12. Cross-Sub-Project Requirements Register (plan-review 2026-08-15)

Locked requirements for later sub-projects, captured from the revised production-design
review. Design inputs for S2–S11, not S1 implementation targets. Each numbered decision
below is binding on its sub-project's future spec.

### 12.1 Routing inputs (S3/S4 — every placement decision consumes these)

KV-prefix locality · active prefill tokens · active decode KV · expected output length ·
active request count · GPU pressure · image/media cache affinity · session affinity ·
topology.

### 12.2 Scaling inputs (S5/S7)

TTFT target · ITL target · queue pressure · KV utilization · traffic forecast ·
autoscaling.

### 12.3 Locked decisions

1. **No user-count balancing (S3).** Cost model:
   `cost = prefillScale × max(activePrefill + newPrompt − KVOverlapCredits, 0) +
   projectedDecodeKV + w × activeRequests`; pick the lowest-cost eligible worker.
   Add model-based prefill-duration estimation, output-length hints, overload filters,
   queue-class scheduling.
2. **Affinity is a preference, never a pin (S3).** `(worker, dp_rank)` affinity is used
   while warm; the router breaks it when worker load (KV%, queue depth) is unacceptable —
   sacrificing a cache hit beats overloading a hot worker.
3. **Expected-output-length hints (S2/S3).** Per request, internally expose:
   `input_tokens, cached_input_tokens, expected_output_tokens, max_output_tokens,
   media_tokens, request_class, tenant_priority`. Remaining expected length decays a
   request's projected future load as it nears completion.
4. **Fair queues, not FIFO (S2).** Priority classes (interactive / standard / batch, plus
   agentic/background from the original list) scheduled by weighted Deficit Round Robin
   with tenant fairness within each class. Reference weights: interactive-paid 10,
   interactive-normal 6, background-agent 2, batch 1.
5. **Classify by workload, don't pre-partition GPUs (S2/S7).** Request classes
   S 0–8K / M 8–32K / L 32–64K / XL 64–128K / IMAGE. Start with a unified fleet;
   create separate pools only when production telemetry shows XL requests degrading
   interactive latency (an idle long-context pool wastes GPUs).
6. **Media-hash-aware KV routing (S3).** Media hash joins the cache key
   (Qwen3.6/SGLang verified); repeated-image conversations route back to warm media
   state; encoder cache lands in S6.
7. **Two-layer overload protection (S2).** Edge admission on fleet signals — total
   queued tokens, P95 TTFT, P95 ITL, fleet KV utilization, active prefill tokens,
   active decode KV, available replicas — with a short bounded wait budget, then
   reject/retry. Plus small worker-local queues to keep continuous batching saturated.
8. **Dual-loop autoscaling from tokens + latency, never GPU utilization (S7).** Long
   loop (~minutes): traffic forecast, historical ISL/OSL, time-of-day/MAU patterns,
   TTFT/ITL targets → warm capacity floor. Fast loop (~seconds): queue tokens, KV%,
   TTFT, ITL, active prefills → burst scale-up. Always maintain warm spare capacity —
   GPU cold starts are not Lambda cold starts.
9. **MTP-aware capacity (S5/S7).** Planner decode-latency estimates must use MTP
   accepted-token length (e.g. ~1.8 accepted tokens per verification; real deployment
   ~30 tok/s with MTP), otherwise decode-GPU count is badly overestimated.
10. **Aggregated serving first; no P/D at start (S6).** Qwen3.6-27B FP8 fits one H100 →
    all replicas aggregated. Evaluate P/D only when traces show long prefills hurting
    decode. Conditional disaggregation is the target design but is **not yet supported
    with SGLang** (Dynamo docs) — do not architect around it yet.

### 12.4 Reference-status updates (llm-d v0.8, GAIE GA)

- llm-d v0.8: precise prefix-cache routing, event-driven KV indexing, CPU/SSD tiered KV,
  P/D disaggregation, predicted TTFT/ITL routing, flow control + fairness, multimodal
  serving, HPA/KEDA integration, Workload Variant Autoscaler.
- Gateway API Inference Extension (GAIE) is GA at v1.5.0; richer endpoint-picker logic
  lives in llm-d; InferencePool remains the K8s-standard API — our later K8s adapter
  must target InferencePool.
- llm-d's online XGBoost TTFT/TPOT prediction (learned per request×worker from live
  traffic; matches or beats hand-weighted scores when request costs vary widely) is the
  S4 design baseline.

### 12.5 One scheduler rule (S2–S7)

PCCP builds exactly ONE placement path combining the strongest attributes of both
systems — never parallel Dynamo-style and llm-d-style implementations of KV routing,
P/D, autoscaling, or load balancing. (The advisor's "deploy Dynamo as the fleet brain"
is superseded by the product premise: PCCP *is* the brain. We adopt the formulas and
insights, not the deployment.)

### 12.6 Deployment assumptions (inform S5/S7 tuning)

FP8 model (+ experimentally validated FP8 KV), MTP on, 128K hard context, images
yes / video no, ~25–35% fleet headroom rather than running at theoretical saturation.

## 13. Efficiency & Differentiation Register

Where our implementation beats the references at the same feature — and how we build
it, in Go, inside PCCP. Every claim below was validated against the vendored sources
(cited); wording is adjusted where validation disproved an earlier claim. Each item
binds its sub-project's future spec.

### 13.1 One signed transport end-to-end; no frontend tier

**Claim (corrected):** Dynamo 1.3's serving frontend is Rust (`lib/llm/src/http/service.rs`
— "forwards the incoming OAI Chat Request … to the backend"); no Python hop. llm-d's
path is Gateway → Envoy → EPP → pod. Neither carries a signed transport.
**Ours:** DARI ingress → scheduler → PIA → engine-localhost. The wins are (a) one
CBOR/COSE transport with signed correlation IDs end-to-end — no protocol translation
layers, (b) co-located last hop, (c) no independently scaling frontend tier to fail.
**Go application:** the scheduler's DARI listener reuses `internal/dari` (same paper
server as the relay); token streams relay as DARI frames via `io.Copy` on framed
payloads — CBOR marshalled once at ingress, never re-encoded mid-path. PIA↔engine
stays HTTP because engines define that boundary; it terminates at localhost.

### 13.2 Engine-agnostic scheduler core

**Claim:** Dynamo leaks backend differences into its router and **explicitly does not
support conditional disaggregation on SGLang** (docs: "SGLang | Not supported yet",
`developer-guide/advanced-customizations/conditional-disaggregation.md`).
**Ours:** the scheduler core consumes only capability cards; all engine specifics live
behind PIA adapters (`adapters/vllm`, `adapters/sglang` — pattern exists). Because we
own the prefill/decode decision logic (S6), SGLang conditional disaggregation becomes
our choice to make, not upstream's.
**Go application:** `internal/scheduler` imports only the card schema; adapters
implement one Go interface (`EngineAdapter`) in PIA. New engine = one adapter file.

### 13.3 In-process Bayesian latency prediction (S4)

**Claim:** llm-d trains XGBoost in batch via sidecars; predictions are a per-request
**network hop** to a prediction server with a fallback path
(`docs/architecture/advanced/latency-predictor.md`); models emit point estimates, and
"heterogeneous pools are not yet modeled".
**Ours:** online Bayesian linear models with predictive **variance → P(SLO violation)**,
updated O(p²) per completion, prediction = local lookup. No retrain cycles, no sidecar,
no predictor-outage fallback mode, and risk-aware (not just mean-aware) routing.
**Go application:** `internal/scheduler/predictor` — precision-form online updates
(pure `math` stdlib, no ML dependencies), ~15 features, updated by a goroutine
consuming completion events from the DARI channel.

### 13.4 Tenant-scoped KV namespaces (S3)

**Claim:** llm-d's KV indexer has no tenant concept (`docs/architecture/advanced/
kv-management/kv-indexer.md`); Dynamo's `cache_namespace` field exists but is not a
tenant-governance mechanism. Shared KV across tenants is a data-side channel.
**Ours:** tenant → cache-namespace mapping enforced by signed policy; the S3 KV index
keys on `(namespace, block_hash)`; policy may pin tenants to exclusive workers.
**Go application:** `cache_namespace` on the card + admission check in the scheduler;
namespace partition is a registry-policy lookup, no hot-path branching.

### 13.5 Token-debited weighted DRR (S2)

**Claim (corrected):** llm-d fairness plugins are `round-robin-fairness-policy` and
`global-strict-fairness-policy` (`flow-control.md`) — request-cycling, not
token-debited; token load exists only as an endpoint *scorer* (`token-load-scorer`,
`scheduling.md`).
**Ours:** deficit in **tokens**, debit = `input_tokens + expected_output_tokens` at
admission, quantum scaled by class weight. Fairness tracks actual GPU work.
**Go application:** `internal/scheduler/queue` — classic DRR with per-flow token
deficits (O(1) amortized dequeue), class weights from signed policy.

### 13.6 Routing receipts as signed evidence (S3/S10)

**Claim:** Dynamo emits `router_hint`/`routing_hashes` tracing events
(`kv_router/push_router/selection.rs`) and llm-d emits metrics — logs, unsigned,
non-queryable.
**Ours:** every placement decision is a signed, queryable record: worker, overlap
tokens, predicted TTFT/ITL + variance, affinity decision, class.
**Go application:** reuse `internal/events` Emit (ed25519, already built) with a
`routing.receipt` event type; stdlib `crypto/ed25519` sign ≈50µs — acceptable on the
hot path; queried via the CP API (S10).

### 13.7 Forecast-driven pre-warming + weight residency as core (S7)

**Claim:** Dynamo Snapshot is "⚠️ Experimental Feature … in preview", SGLang "Highly
experimental" (`kubernetes-operator/snapshot.md`); both systems cold-load pods on
scale-up.
**Ours:** the long forecast loop doesn't just predict counts — it issues signed
lifecycle directives to pre-warm standby workers before the predicted burst;
residency/snapshot are core paths, not preview.
**Go application:** `internal/scheduler/warmpool` maintains the warm inventory in the
registry; PIA executes directives (weight preload; CRIU/cuda-checkpoint invoked as
external tooling from Go).

### 13.8 Virtual engine = free digital twin (S11)

**Claim:** Dynamo's simulation (`lib/mocker`, `aisimulate/`) and llm-d's simulator are
standalone systems that re-implement engine behavior.
**Ours:** because the engine boundary is the PIA adapter (13.2), a virtual engine
behind the same interface means replaying production traces through the *real*
scheduler with synthetic GPUs.
**Go application:** `adapters/virtual` implements `EngineAdapter`, replays trace
records, synthesizes metrics/KV events; the S1 fake-engine test stub grows into it.

### 13.9 Content-aware signed heartbeat (S1)

**Claim:** llm-d health = K8s probes (coarse, unsigned); Dynamo = ETCD leases
(content-free).
**Ours:** the heartbeat *is* the signed capability card — failure carries its own
diagnosis (introspection failure → degraded card; measured socket state included),
so failure handling (S8) knows *what* failed.
**Go application:** as designed in §6–§7; card status enum + measured grade; no extra
health system.

### 13.10 Slack-filling with pause/resume (S9)

**Claim (adjusted):** llm-d Async gates dispatch on capacity signals (`proposals/
llm-d-async.md`: max-concurrency semaphores, pluggable Gate) but never yields
in-flight work; Dynamo has migration, not pause.
**Ours:** batch work admitted only into slack; token-level pause/resume yields to
interactive instantly.
**Go application:** S9 batch gateway tracks per-sequence state in PIA; pause = abort
with KV retained, resume = re-submit reusing the warm prefix (cheap precisely because
the KV index knows it's warm); coordination via signed DARI directives.

### 13.11 KV index survives engine restarts (S3)

**Claim:** Dynamo's KV state agent is vLLM-only, hosted inside the engine handler
process, terminated on engine death, "Cross-incarnation KVCC continuity is not
implemented", at-least-once delivery across restarts is an open TODO
(`kv_router/publisher/state_agent.rs` header, verbatim).
**Ours:** PIA — a separate process — owns the KV event broker with a per-incarnation
append-only journal and sequence numbers; on engine restart PIA replays to the
scheduler, which dedups by `(worker, seq)`. The fleet's cache map survives engine
crashes; Dynamo's does not.
**Go application:** `internal/pia/kvjournal` (append-only file + seq); scheduler-side
dedup watermark per worker.

### 13.12 Heterogeneous-fleet latency models (S4)

**Claim:** llm-d's predictor "assumes a homogeneous inference pool … Heterogeneous
pools are not yet modeled" (`latency-predictor.md`, verbatim).
**Ours:** one model per serving config (card hash) with GPU SKU/precision/MTP/engine
as one-hot features and hierarchical priors, so H100/H200/B200 mixes are first-class
and rare configs borrow strength from similar ones.
**Go application:** `internal/scheduler/predictor` — model store keyed by card hash;
feature vector built from card + request fields (§12.3.3).

### 13.13 Zero-hop prediction serving (S4)

**Claim:** llm-d's EPP calls prediction servers over the network per request, with
shared-volume model exchange and a heuristic fallback path (`latency-predictor.md`).
**Ours:** predictor runs inside `pccp-scheduler` — the same process as the router.
Prediction is a local matrix-vector multiply; there is no predictor-outage failure
mode.
**Go application:** single binary; completion events arrive over the existing DARI
channel; no IPC.

### 13.14 Signed traffic classes (S2)

**Claim:** llm-d extracts fairness ID and priority from **client-supplied HTTP
headers** (`x-llm-d-inference-fairness-id`, `flow-control.md`) — spoofable.
**Ours:** tenant/priority metadata arrives in COSE-signed DARI ingress metadata and
is checked against the tenant's issued class capabilities at admission; clients
cannot claim classes they don't own.
**Go application:** scheduler reads classes from the verified envelope only (never
headers); CP-issued tenant capabilities already exist in the policy layer.

### 13.15 Fail-closed admission (S2)

**Claim:** llm-d's documented EPP-down behavior is `FailOpen`: Envoy "bypasses the
extension and routes the request directly to a model-server endpoint" with **no flow
control, fairness, or saturation gating** (`flow-control.md`).
**Ours:** there is no bypass path. The scheduler is the only route to a worker;
scheduler down = DARI handshake fails = requests are rejected with retry signaling —
governance never silently disappears.
**Go application:** architectural (single ingress path); overload responses reuse the
existing DARI error envelope with retry metadata.

## 14. Full Coverage Matrix (original 40 sections → sub-projects)

Every section of the original feature list is assigned. This matrix is the
completeness gate: a feature with no row is a gap; a row with no S is a gap.

| Original § | Feature | S |
|---|---|---|
| 1 | Unified gateway (OpenAI/Anthropic ingress, streaming, tool calling, structured outputs, reasoning, embeddings, multimodal, model discovery/readiness, cancellation, correlation IDs, routing/tenant/priority/SLO/session metadata) + model rewriting (aliases, remap, version migration, traffic split, canary, A/B, fallback) | S2 |
| 2 | Global admission + late-binding queue | S2 |
| 3 | Multi-tenant QoS & fairness (priority bands, weighted fairness, DRR, ordering policies) | S2 |
| 4 | Intelligent GPU router (full routing-input set) | S3 |
| 5 | Exact KV-aware routing (event-driven, global/sharded index, dedup) | S3 |
| 6 | Session affinity ((worker, dp_rank), breakable) | S3 |
| 7 | Predicted-latency routing (online learned, per-config, variance) | S4 |
| 8 | SLO-aware scheduling (per-model/tenant/request, TTFT/ITL targets, latency/availability tiers) | S5 |
| 9 | MTP/speculative awareness (draft count, acceptance rate, capacity modeling) | S5 |
| 10 | Global KV cache fabric (L1–L4 tiers, promotion/demotion, prefetch, retention) | S6 |
| 11 | High-speed KV transport (NVLink/RDMA/IB/RoCE/EFA/UCX, topology-aware cost) | S6 |
| 12 | Multimodal: media-hash routing (S3), encoder cache (S6), media controls/SSRF/limits (S2) | S2, S3, S6 |
| 13 | Multi-engine runtime (capability cards, normalized capabilities) | S1 |
| 14 | Aggregated serving (default mode) | S6 (baseline from day one) |
| 15 | P/D disaggregation (aggregated-first; conditional P/D; SGLang caveat) | S6 |
| 16 | Expert/parallelism-aware routing (TP/DP/EP, WideEP, DP-rank, gang scheduling, worker-group readiness) | S3 |
| 17 | Multi-model serving (aliases, per-tenant access, A/B, canary, model pools) | S2 (rewrite) + S3 (pools) |
| 18 | LoRA management (affinity routing, residency/popularity tracking, dynamic load/unload, capacity prediction) | S3 (affinity) + S7 (lifecycle) |
| 19 | Fleet-wide autoscaling (reactive + predictive, heterogeneous, scale-to-zero, warm floor, P/D independent) | S7 |
| 20 | Fast model actuation (GMS-style residency, snapshots, weight caching/prefetch) | S7 |
| 21 | Engine lifecycle (start/readiness/drain/sleep/wake/pause/resume/memory release/reload/weight update/termination) | S7 |
| 22 | Request migration (failure- and load-triggered, budgeted) | S8 |
| 23 | Request cancellation (disconnect detection, propagation, KV reservation cleanup) | S8 |
| 24 | Failure management (worker/process/GPU/network/control-plane) | S8 |
| 25 | Shadow engine / rapid failover (optional HA tier) | S8 |
| 26 | Batch inference (submit/upload/status/cancel/retries/deadlines/quotas) | S9 |
| 27 | Asynchronous inference (Redis/PubSub, dispatch gating, backpressure) | S9 |
| 28 | Agentic workloads (long sessions, tool-call-heavy, branching, context-block identity, agent SLOs, cache-retention hints) | S3 + S5 |
| 29 | Post-training / RL serving (rollouts, in-place weight updates, scheduler hooks, pool separation) | S7 (optional tier) |
| 30 | Hardware & topology intelligence (NVLink/PCIe/NUMA/IB/rack/zone, placement constraints) | S3 |
| 31 | Multi-accelerator (CUDA/ROCm/XPU/TPU/CPU) | S1 (card) + S7 (pools) |
| 32 | K8s-native control plane (Gateway API, InferencePool, CRDs, HPA/KEDA, Prometheus) | S12 |
| 33 | Non-Kubernetes mode (bare metal, Slurm, static nodes) | S1 (core) + S12 (adapter) |
| 34 | Service discovery (registration, capability, health leases, topology, stale removal) | S1 |
| 35 | Observability (metrics, distributed traces, routing explainability) | S10 + S3 (receipts §13.6) |
| 36 | Capacity simulation / digital twin | S11 |
| 37 | Benchmark & autotuning (model×GPU×engine×quant×context×MTP profiles) | S11 |
| 38 | Load shedding & overload protection (multi-level, priority shedding, retryable overload) | S2 |
| 39 | Power & cost optimization (cost/token, power caps, scale-to-zero, cheaper HW for background) | S7 |
| 40 | Administrative control plane (fleet/models/traffic/cache/perf/routing/scaling views) | S10 (+S1 workers page) |

All 15 core capabilities: §2.2 matrix (capability → S). All 10 locked decisions: §12.3
(decision → S). All 15 differentiation items: §13 (item → S). No unassigned features
remain.

