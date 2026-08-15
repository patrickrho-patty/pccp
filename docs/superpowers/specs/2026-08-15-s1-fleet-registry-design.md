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
| S2 | Global admission + late-binding queue (tenant QoS, fairness, SLO ordering) | S1 |
| S3 | Capability/load/KV-aware router + exact KV index + session affinity | S1, S2 |
| S4 | Online-learned latency prediction (TTFT/TPOT per serving config) | S1, S3 |
| S5 | SLO- and MTP-aware scheduling | S2, S3 |
| S6 | P/D disaggregation + tiered KV fabric + NIXL/RDMA transfer | S3 |
| S7 | Autoscaling (reactive/predictive/heterogeneous) + fast model actuation + engine lifecycle | S1, S5 |
| S8 | Resilience: request migration, cancellation propagation, shadow failover, health probing | S2–S6 |
| S9 | Batch/async gateway with slack-capacity filling | S2 |
| S10 | Observability, routing explainability, admin dashboards | all |
| S11 | Digital-twin simulation + benchmark autotuning | S1, S5 |

**This spec covers S1 only.**

## 2. Reference Sources & Adoption Map

Reference codebases are vendored in `tools/` (this worktree). We study their logic and
designs and re-implement the ideas in Go/DARI — never copy source. `tools/dynamo` is the
Rust source tree; `tools/llm-d` is a docs/proposals-only checkout (no Go tree — consult
upstream `kubernetes-sigs/llm-d` for source when needed).

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
| 8 | Media/encoder-aware caching + routing | S6 | Dynamo multimodal KV routing (Qwen3.6/SGLang path) |
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
| gpu | SKU, count, HBM per GPU; NVLink/topology deferred to S3 | required SKU/count |
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
| K8s/CRD adapter (llm-d style) | later (non-K8s first — gov/bare-metal market) |
| DARI push of signed configs | later |
| Queue, routing, KV, autoscaling — everything else | S2–S11 |
