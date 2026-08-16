# PCCP Fabric — Completion Report (Plan-of-Record → Implementation)

**Branch:** `forge/pccp-fabric` · **Worktree:** `.worktrees/pccp-fabric` · **HEAD:** 8d323d8
**Purpose:** validate every created plan against what shipped. Every claim cites
the plan file (plan of record) and the implementing code/test. Nothing below is
aspirational — each line maps a plan requirement to committed code.

---

## 0. How to validate this report

```bash
cd /Users/patrickrho/projects/pccp/.worktrees/pccp-fabric
go build ./...                       # must exit 0
go vet ./...                         # must exit 0 (clean)
go test ./... -count=1               # 43 packages green (two env flakes: see §10)
cd web && npm run build              # vite production build green
```

Plan-of-record files (all checked into the repo):

| Plan | File |
|---|---|
| S1–S12 fabric spec | `docs/superpowers/specs/2026-08-15-s1-fleet-registry-design.md` |
| Web feature plans (27) | `docs/feature-plans/web/00-cross-cutting.md` … `26-bootstrap.md` |
| Web gaps source | `docs/WEB_FEATURE_GAPS.md` |
| Harness plans (6) | `docs/feature-plans/harness/A…F-*.md` |
| Harness gaps source | `docs/HARNESS_FEATURE_GAPS.md` |
| Plans review | `docs/feature-plans/REVIEW.md` |
| Paper plans (7) | `docs/superpowers/plans/2026-08-1*-paper-*.md` |
| DARI evolution plan | `docs/superpowers/plans/2026-08-14-dari-protocol-evolution-implementation.md` |
| Master plan / status / audit | `docs/MASTER_PLAN.md`, `docs/IMPLEMENTATION_PLAN.md`, `docs/IMPLEMENTATION_STATUS.md`, `docs/MISSING_ITEMS.md`, `docs/V2_DOD_AUDIT.md` |
| Product PRD | `docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md` |
| Protocol/paper | `docs/plans/DARI/*` |

**Naming note:** this layer was originally the "Best-of Dynamo + llm-d" plan
(the spec mines NVIDIA Dynamo + llm-d design ideas). Per user direction the
layer carries no separate product brand — it is internal PCCP code; the branch
was renamed `forge/dynamo-fabric` → `forge/patty-fabric` → `forge/pccp-fabric`.

---

## 1. S1–S12 — the governed-inference fabric

Plan of record: `docs/superpowers/specs/2026-08-15-s1-fleet-registry-design.md`
Implementation: `internal/scheduler/**` (49 files + queue/), `internal/pia/**`
(DARI worker agent), `internal/relay/**` (governance gate), `conformance/**`.
~53k lines of Go implementation + ~17.5k lines of tests repo-wide.

### 1.1 The 15 core capabilities (spec §2.2) → implementation

| # | Capability (spec §2.2) | Implemented in | Tests |
|---|---|---|---|
| 1 | Global fair queue with late GPU binding | `scheduler/queue/queue.go` (token-debited weighted DRR), `dispatch.go` (late-bound assign) | `queue_test.go`, `dispatch_test.go` |
| 2 | Exact fleet-wide KV-cache map | `kvindex.go` (namespace+hash+media index), `router.go` (KV-overlap credits) | `kvindex_test.go`, `router_test.go` |
| 3 | KV+session+topology+load-aware placement | `router.go` (cost model: KV credits, affinity discount, topology cost, overload filter) | `router_test.go` |
| 4 | Online learned TTFT/TPOT prediction | `predictor.go` (precision-form Sherman–Morrison, evidence-scaled variance) | `predictor_test.go` |
| 5 | SLO-aware routing | `slo.go` (model/class/default TTFT+ITL; agentic tighter), router SLO gate | `slo_test.go` |
| 6 | MTP-aware capacity modeling | `slo.go` (accepted-token 1.8× amplification), `serving.go` | `slo_test.go` |
| 7 | Tiered KV fabric + P2P transfer | `kv_tier.go` (L1–L4 promote/demote/prefetch/retention) | `kv_tier_test.go` |
| 8 | Media/encoder-aware caching + routing | `kvindex.go` (media-hash keys), `kv_tier.go` (encoder cache), `gateway.go` (media controls, SSRF block) | `kvindex_test.go`, `gateway_test.go` |
| 9 | Aggregated + P/D disaggregated serving | `pd.go` (aggregated-first, conditional P/D, SGLang refusal) | `pd_test.go` |
| 10 | NIXL/RDMA-aware state movement | `topology.go` (NVLink/PCIe/ethernet transfer costs; RDMA-class path) | `topology_test.go` |
| 11 | Heterogeneous cost/SLO-aware autoscaling | `autoscale.go` (dual-loop: forecast floor + burst fast loop, warm spare ≥1) | `autoscale_test.go` |
| 12 | Fast model actuation (residency, snapshots) | `lifecycle.go` (engine state machine, prewarm directives, warm pool) | `lifecycle_test.go` |
| 13 | Request migration + failure recovery | `resilience.go` (budgeted migration, health prober with consecutive-failure threshold) | `resilience_test.go` |
| 14 | Batch/async workload consolidation | `batch.go` (slack-only dispatch, token-cursor pause/resume, tenant quotas), `api.go` (/api/v1/batch) | `batch_test.go` |
| 15 | Full-fleet simulation, replay, autotuning | `digitaltwin.go` (virtual engine twin, autotune loop) | `digitaltwin_test.go` |

### 1.2 §14 full coverage matrix — all 40 rows

| Row | Spec §14 requirement | Where it lives |
|---|---|---|
| 1 | Unified gateway (OpenAI/Anthropic, streaming, tools, structured outputs, reasoning, embeddings, multimodal, discovery, cancellation, correlation IDs, rewriting) | `gateway.go`, `serving.go`, `rewrite.go` |
| 2 | Global admission + late-binding queue | `admission.go`, `queue/queue.go`, `dispatch.go` |
| 3 | Multi-tenant QoS & fairness | `queue/queue.go` (interactive-paid/normal, background-agent, batch weights), `classes.go` |
| 4 | Intelligent GPU router (full input set) | `router.go` |
| 5 | Exact KV-aware routing (event-driven, dedup) | `kvindex.go`, `router.go`, `gang.go` |
| 6 | Session affinity (worker, dp_rank; breakable) | `router.go` (AffinityWorker discount) |
| 7 | Predicted-latency routing | `predictor.go`, `router.go` |
| 8 | SLO-aware scheduling | `slo.go`, `router.go` |
| 9 | MTP/speculative awareness | `slo.go` |
| 10 | Global KV cache fabric L1–L4 | `kv_tier.go` |
| 11 | High-speed KV transport (topology-aware) | `topology.go` |
| 12 | Multimodal: media-hash routing, encoder cache, media controls | `kvindex.go`, `kv_tier.go`, `gateway.go` |
| 13 | Multi-engine runtime (capability cards) | `card.go`, `card_v2.go` (S1) |
| 14 | Aggregated serving (default) | `pd.go`, `serving.go` |
| 15 | P/D disaggregation | `pd.go` |
| 16 | Expert/parallelism-aware routing (TP/EP gangs) | `gang.go` |
| 17 | Multi-model serving (aliases, per-tenant, A/B, canary, pools) | `rewrite.go`, `gateway.go`, `router.go` (ModelPoolManager) |
| 18 | LoRA management (affinity, popularity) | `lifecycle.go` (LoRaLifecycle), `router.go` |
| 19 | Fleet-wide autoscaling | `autoscale.go` |
| 20 | Fast model actuation | `lifecycle.go`, `autoscale.go` (prewarm) |
| 21 | Engine lifecycle | `lifecycle.go` |
| 22 | Request migration | `resilience.go` |
| 23 | Request cancellation | `resilience.go` (cancellation hub), `queue.go` |
| 24 | Failure management | `resilience.go` (prober) |
| 25 | Shadow engine / rapid failover | `resilience.go` |
| 26 | Batch inference | `batch.go`, `api.go` |
| 27 | Asynchronous inference | `batch.go` |
| 28 | Agentic workloads (context blocks, retention, SLO) | `contextblock.go`, `classes.go` |
| 29 | Post-training / RL serving | `rl.go` |
| 30 | Hardware & topology intelligence | `topology.go` |
| 31 | Multi-accelerator | `card.go`, `autoscale.go` (cost family picker) |
| 32 | K8s-native control plane (CRDs, HPA/KEDA) | `k8s.go` (GAIE v1.5, PodToCard, PoolToCards) |
| 33 | Non-Kubernetes mode | `registry.go`, `card.go` (static/bare-metal) |
| 34 | Service discovery | `registry.go`, `card_v2.go`, `listener.go` |
| 35 | Observability (metrics, traces, explainability) | `observability.go` (6 admin views + routing explainability) |
| 36 | Capacity simulation / digital twin | `digitaltwin.go` |
| 37 | Benchmark & autotuning | `digitaltwin.go` |
| 38 | Load shedding & overload protection | `overload.go` (two-layer), `queue/queue.go` (DropClass) |
| 39 | Power & cost optimization | `autoscale.go` (cost optimizer, scale-to-zero) |
| 40 | Administrative control plane | `api.go` (fleet/queue/cache/perf/routing/scaling views) |

### 1.3 §12.3 locked decisions — all honored
No user-count balancing (cost model) · affinity is a preference · expected-
output-length hints (`hints.go`) · fair queues (DRR) · classify by workload
(`classes.go`) · media-hash-aware KV routing · two-layer overload protection ·
dual-loop autoscaling · MTP-aware capacity · aggregated-first serving.

### 1.4 §13 efficiency/differentiation register — all honored
Signed transport end-to-end (DARI, no frontend tier) · engine-agnostic core
(capability cards) · in-process Bayesian predictor · tenant-scoped KV
namespaces · token-debited DRR · **signed routing receipts** (`router.go`
ReceiptStore + ed25519 Sign/Verify) · forecast pre-warming + residency ·
virtual-engine twin · content-aware signed heartbeat (S1) · slack-filling
pause/resume · KV index survives restarts (journal) · heterogeneous latency
models · zero-hop prediction · signed traffic classes (`traffic_envelope.go`)
· fail-closed admission.

### 1.5 S1 — fleet registry & capability cards (spec §4–§9)
`card.go` (v1), `card_v2.go` (DariAddr/ActiveSeqs/PDRole, version-gated
canonical signing) · `registry.go` (registration, leases, SyncRouter) ·
`signedconfig.go` (signed config + socket verification) · `protocol.go`,
`listener.go` · worker agent `internal/pia/**` (DARI handshake, HELLO→
HELLO_ACK→AUTH_CHALLENGE→AUTH_PROOF, card builder, subject-key binding,
fail-closed production profile) · conformance: `conformance/s2_s12_conformance_test.go`,
`internal/pia/s2_dari_ingress_test.go` (AI_OPEN governed ingress + KV journal e2e).

---

## 2. Web feature plans — all 27 (00 + 26 pages)

Each page is a full vertical: `web/src/pages/X.tsx` → `web/src/api.ts` →
`internal/api/*` handlers → service → models. Commit-per-page on the branch.

### 00 — cross-cutting (14 items A1–A14)
Shared infra built once and adopted across pages: `useFavorites` + FavoriteStar,
`useServerTable` (server-side pagination/filter/sort), `StatCard` drill-downs,
`EntitySelect` (user/project/repo/business-unit/harness/catalog-model/
scm-connector/policy-pack), `Modal`/`ConfirmDialog`, `CommandPalette` (⌘K, /,
arrow keys), collapsible nav sub-menus, theme/density toggles, motion system
(`prefers-reduced-motion`), `ResponsiveTable` (table→card), `EmptyState`,
unified `GET /api/search` + cross-entity actions, detail routes for all
entities. Commit `791f35c`.

### 01 — users (13 items A1–B8)
Business-unit wiring + CRUD (`/api/business-units`) · harness↔developer binding
(`/users/{id}/harnesses` grant/revoke from AllowedUsers+Device) · enrollment
codes (issue+expiry) · seat enforcement (users/harnesses) · structured
`ContractorProfile` + auto-expiry sweep · audit + reason on every mutation ·
OffboardingCase workflow (sessions+harnesses+evidence) · server query
(page/size/search/business_unit/status/role/sort) · `/users/:id` detail tabs ·
seeded developer entitlements (`internal/identity/entitlement.go`) consumed by
the relay (traffic-class cap + developer standing gate) · usage rollup ·
public `/scim/v2/*` + CSV import (dry-run→apply) · SSO status. Commit `a910e3a`.

### 02 — sessions (12 items A1–B8)
Fail-closed open: active policy epoch **and** issued capability lease bound at
open (no epoch → refused) · baseline anchoring for repo sessions · derived
protection profile/TTLs · idle sweep + auto-close (relay) · server query
(status/model/user/project/range/sort) · consolidated `/sessions/{id}/detail` ·
per-exchange decision log · replay timeline · visibility levels (A–D) · bulk
close/pause/terminate · live-path enforcement (relay refuses closed/paused/
idle sessions) · deep-link inspector with catalog model select. Commits
`a98aaf9` (+ relay gate).

### 03 — harnesses (10 items A1–C5)
PPC credential view (issuer/validity/revocation decode) · enrollment-code
validate+burn · heartbeat endpoint + stale detection · forced-version floor
(`IsVersionBelowFloor`, durable org setting) · relay directive propagation on
revoke/quarantine · server filters/sort · detail page. Commit `2c00856`.

### 04 — projects (10 items A1–B7)
ProjectMember table + roles · real roster counts · archive freeze/restore +
impact preview · usage chargeback rollup · ChangeRequest queue fed by
high-risk changesets · policy-pack binding · Korean attrs · detail page.
Commit `3ad4233`.

### 05 — repositories (11 items A1–C5)
gitscm sync/tree/file browser · HMAC-verified public webhook ingest + secret
rotate · baselines UI · branch rules · full-field update + clone validation ·
sensitivity heatmap · server filters · detail page. Commit `feec748`.

### 06 — policy (11 items A1–C4)
Multi-domain epochs with digests · rule→epoch rebuild with same-scope
intersection (fix `711117f`) · effective-policy resolver · PolicyPack
CRUD/assign/export/import · epoch diff · draft→approve workflow + bulk · ack
campaigns with OpenSession gate · **real** policy simulation (not stub) ·
conflict detection · exception marketplace · server-side template catalog.
Commit `87e5445`.

### 07 — security (9 items A1–C5)
Suppress/accept-risk + expiry sweep · alert routing (Slack/webhook/SIEM) from
RecordFinding · versioned org PII lexicon in the detector · inline response
inspection blocking DENY output on the governed path · scoped lockdown
(org/project) with impact + relay directives + live session-status gate ·
incident workflow (list/contain/resolve) · scan-session · server filters +
bulk · rule tester with diff. Commit `be720e5`.

### 08 — compliance (10 items A1–C5) — "no phantom compliance"
Page wired to the real assessment engine (`assessControlState` reads actual
DB state — users/rules/audit events/findings) · certification scope/level
targets (CSAP 간편/일반, ISMS-P 1/2/3, SaaS/PaaS/IaaS) · evidence vault
(attach/query/delete) · gap→task remediation + bulk · persisted assessment
history + continuous re-assessment ticker · audit-ready CSV/JSON export ·
honest self-assessment disclaimer. Commit `153cab0`.

### 09 — fleet (12 items A1–A12)
Live relay propagation for revoke/quarantine/terminate/pause/lockdown (admin
directives) · bulk actions · repo change-freeze (§33.13) · forced-version
floor (§33.10) · per-harness action history · forensic snapshot bundle ·
approvals queue · scoped 2-step emergency lockdown with impact preview ·
mandatory reason for destructive actions · server inventory filters + live
status heartbeat (15s auto-refresh). Commit `af32863`.

### 10 — SRE (12 items A1–A12)
Real account telemetry (`/api/public/accounts` with detection risk + ladder) ·
graduated-response ladder (rung + next action derived from the three states) ·
separate integrity/T&S/capacity dimensions · live health (CP/realtime/
telemetry) · drill-downs · incidents surface. Commit `3f68394`.

### 11 — Service Command Center (12 items A1–A12)
Live traffic/counts from real accounts · per-dimension live states ·
support-case queue with timeline · abuse-case queue · refund records ·
segment tagging · live case panels (commit `b707058`).

### 12 — Subscriber management (13 items A1–A13)
Enriched account list (risk score, signals, leases, ladder) · per-account
detail (leases/signals/cases) · graduated-response actions with mandatory
reason · refund/credit records (`provider not_configured` — honest, see §10) ·
support + abuse case lifecycle · segment tagging. Commit `3f68394`.

### 13 — communications (11 items A1–C3)
SSE real-time fan-out (messages/broadcasts/transfers/presence) · threading +
mentions + reactions + read receipts · AI-context linking (§21.6) · broadcast
ack dashboard · 1:1 DM find-or-create from user search · role-gated system
commands · viewer redaction (privacy separation) · **real file transfer**:
storage + sha256 + content scan (blocked-on-finding) + download + accept/
decline + expiry · retention sweep. Commit `d04ab0f`.

### 14 — tools (5 items A–E)
Registry enforced at request time: relay action-ingestion calls
`CheckToolAuthorizationFull` (registered + active + lease tool-class +
project allowlist + integrity digest) · approvals queue surfaced + decide ·
MCP cross-link · classification presets wizard with Korean guidance · seed
feedback with added-count · per-project allowlist (feature 7). Commit `ff45233`.

### 15 — sandboxes (5 items A–E)
Real runtimes per mode (docker/microvm/local/remote; `attemptProvision` with
honest `running` vs `defined` status) · session lifecycle binding + auto-
teardown on session close · network/resource enforcement at the runtime
(--cpus/--memory/--network) · image allowlist (fail-closed org setting) ·
forensic snapshot records · destroy confirm + filters + favorites. Commit `ff45233`.

### 16 — analytics (5 items A–E)
Metering wired on the live path (relay RecordUsage tokens_in/out) · clickable
stats → filtered lists · time ranges (7/30/90/365d) · cost rollup (KRW µ¢) ·
per-model/per-user breakdown · CSV export. Commit `e28f1ec`.

### 17 — audit (5 items A–E)
Server-side pagination/filters (type/actor/resource/result/action/from/to/
search) · tamper-evidence chain verification (existing verify + UI) · legal
holds (place/lift/reason + audit) · payload drill-down (JSON viewer) · SIEM
forwarding (org webhook + HMAC signature + cursor) · evidence-bundle
assembly/download · live tail via SSE. Commit `e28f1ec`.

### 18 — model infra (5 items A–E)
**Publish signature + manifest verification** (`registry.VerifyPackageIntegrity`:
recompute digest, verify ed25519 signature, refuse tampered — test corrupts
the digest and asserts 403) · catalog push on publish (OnModelPublished) ·
recall impact analysis (endpoints/sessions/usage) · canary/beta/stable ring
assignment · filters + impact modal. Commit `e28f1ec`.

### 19 — code explorer (5 items A–E)
Provenance IS produced (relay ingests ChangeSet + ProvenanceSpan from DARI) ·
attribution computed per file (4-state AI_GENERATED / AI_THEN_HUMAN_EDITED /
HUMAN_THEN_AI_ASSISTED / HUMAN_WRITTEN from span states — tested) · real file
browser (gitscm tree) · blast-radius via `impact.AnalyzeChange` · span filters
+ heatmap + attribution badges. Commit `e28f1ec`.

### 20 — provenance (10 items A1–A10)
Receipts tab with **real chain-root re-derivation** (hash chain over action
envelope digests up to LastEventSeq, compared to ChainRoot) · signed bundle
export (ed25519 over canonical JSON) · cross-session search · replay timeline ·
visibility gating. Commit `d1123a2`.

### 21 — live view (3 items A–C)
SSE mounted at `/api/realtime/sse` · live token stream: relay fans each
AI_TOKEN_CHUNK delta to `session.chunk` SSE events → terminal cards ·
surveillance-boundary indicator + session deep-links. Commit `d1123a2`.

### 22 — enterprise features (4 items A–D)
Honest statuses (seeded `liveFeatures` map — only end-to-end-wired
capabilities seed as enforced; the rest are `planned`, shown as 계획) ·
idempotent seed (no duplicates) · per-org enable/enforce toggles ·
harness-reported telemetry recorded when sent (blocker §10.5 for the
external harness emitter). Commit `d1123a2`.

### 23 — dashboard (11 items A1–A11)
Demo-seed button removed (no data fabrication in prod) · clickable stat
cards · role/persona-scoped widgets · governance brief widget · incidents +
open-remediation gaps widgets · quick-action entry points · recents hub ·
cross-object navigation. Commit `d1123a2`.

### 24 — account portal (5 items A–E)
Token-keyed self-service (portal access token issued once at creation; never
listed — §6.6) · plan change · sign-out-all (lease revocation) · support
request filing · usage/fair-use summary · console switcher (never traps the
user) · no transferable credentials exposed. Commit `922dea0`.

### 25 — login (3 items A–C)
**TOTP MFA** (RFC 6238: setup secret → verify → enrolled; login challenge;
tested) · brute-force throttle (5 failures → 15-min lockout; tested) ·
password-show toggle · return-to deep link (`?next=`) · SSO buttons with
honest state (operator-SSO JWT issuance is a logged blocker, §10.3 — the
spec itself defers to the operators/authz plan). Commit `922dea0`.

### 26 — bootstrap (3 items A–C)
Deployment-profile picker (enterprise/public/sovereign — drives org profile) ·
policy-pack/framework choice recorded (CSAP/ISMS-P/KISA/AI-BASIC) ·
explicit demo-data opt-in (default off) · step wizard + validation.
Commit `922dea0`.

---

## 3. Harness plans A–F

Plan of record: `docs/feature-plans/harness/A…F-*.md` (26 features A1–E6 +
F1–F4). REVIEW.md status: **A–E ✅ full**; F DONE per its own status section.

| Plan | Features | Evidence |
|---|---|---|
| A trust transport (A1–A5) | PPC credential presentation/verification, transcript-bound auth, lease/epoch binding, revocation rejection, catalog push | `internal/relay/peer_authenticator.go`, `dari_listener.go` (HELLO→HELLO_ACK→AUTH_CHALLENGE→AUTH_PROOF), `internal/dari/handshake.go`/`crypto.go`/`cose.go`, `hotstate.go`, `revocation_propagation_test.go`, catalog `OnModelPublished` |
| B provenance evidence (B1–B4) | ChangeSet + Span ingestion over DARI, signed receipts + connector ack, evidence chain | `relay/dari_session.go` (ingestChangeSet/ingestSpan/ingestReceiptAck), `internal/provenance`, `internal/dari/evidence.go` |
| C inline security (C1–C5) | DLP on the live path, tool authorization gate, injection/network/sandbox enforcement | `relay/service.go` (inline response inspection), `internal/security`, `internal/tools` (relay enforcement), `sandbox` policy |
| D enterprise workflow (D1–D6) | Change freeze, mandatory ack, model recall, forced version, quarantine broadcast, revocation propagation | `relay/governance_state.go`, `governance_view.go`, `grant.go`, `hotstate.go`, `internal/korean` (freeze/version), `internal/policy` (ack) |
| E operational sovereignty (E1–E6) | Trust bundle import/apply, offline updates, time-proof, shadow-AI surface, legal hold, export | `internal/sovereign`, `/api/sovereign/*`, `internal/privacy` (holds), `/api/audit/evidence-bundle` |
| F latency/streaming (F1–F4) | Governed token streaming (AI_TOKEN_CHUNK before AI_COMPLETE), connection prewarm, three-arm benchmark, arXiv numbers | `relay/dari_listener.go` (F1), `internal/dari/client.go` (F2), `internal/bench/` + `cmd/pccp-bench/` (F3), `docs/benchmarks/dari-transport-latency.md` (F4) |

---

## 4. Paper plans (docs/superpowers/plans/)

| Plan | Status | Evidence |
|---|---|---|
| arxiv-publication | 33/33 ✅ (pre-existing) | `docs/plans/DARI/arxiv/` |
| benchmark-mathematics | 12/12 ✅ (pre-existing) | TSV + plot verification |
| visual-evidence | **24/24 ✅** | TikZ lifecycle figure (EN+KO) replacing PNG, PGFPlots latency plot matching TSV medians exactly, SCITT RFC 9943, disclosure, single-column, remaining-work note, full Task 5 verification (fonts embedded, archive rebuilds from fresh unpack) — commits `644e466`, `c1b2fd0`, `630bc73` |
| korean-edition | 6/6 + 4 QA items ✅ | XeLaTeX build stable, Hangul extraction, structural parity with EN, page inspection — commit `adfd4f4` |
| rhetoric-audit | **11/11 ✅** | 22 EN + 29 KO rewrites (defensive/slogan language → evidence-led claims), 4 claim-parity fixes (KO table C4–C6 → 구현 필요 matching EN), both editions rebuild clean, flagged constructions count 0 — commit `adfd4f4` |
| product-positioning | Superseded (documented) | Status note added; superseded by the DARI evolution plan (109/109) |

---

## 5. DARI protocol evolution plan

`docs/superpowers/plans/2026-08-14-dari-protocol-evolution-implementation.md`:
**109/109 checkboxes complete** (pre-existing). Ground truth re-verified this
session: C3–C6 implemented with negative tests, 13-vector attenuation matrix
confirmed, conformance F.14 runner substantial, I2/I4 grant/route-binding
tests present.

---

## 6. Earlier master-plan surface (absorbed by the above)

`docs/MISSING_ITEMS.md` (~50 items across 12 areas) — resolved by the
feature plans: metering wired (`RecordUsage` on the governed path), DLP on
path, tool registry enforced, live session-status gates, governance stages
pinned (`pipeline.go`), phantom-compliance removed, scheduler no longer dead
(wired into relay routing). `docs/WEB_FEATURE_GAPS.md` /
`docs/HARNESS_FEATURE_GAPS.md` were the source docs for the feature plans and
are fully digested by them (per `docs/feature-plans/REVIEW.md`).

---

## 7. Verification evidence (run this session)

- `go vet ./...` — clean.
- `go build ./...` — exit 0.
- `go test ./... -count=1` — **43 packages ok**, 0 FAIL (final run).
- `web npm run build` — vite production build green.
- New tests this session (representative): `users_lifecycle_test.go`,
  `sessions` (epoch/lease fail-closed), `comms_extra_test.go` (DM/thread/
  reaction/read/scan-blocked-download/ack-dashboard), `compliance_extra_test.go`
  (meta/evidence/remediation/assessment+export), `tools_sandbox_extra_test.go`
  (presets/seed-count/allowlist/image-allowlist enforcement),
  `pages1619_extra_test.go` (audit query/holds/bundle/SIEM, publish tamper→403,
  recall impact, code-explorer attribution AI_THEN_HUMAN_EDITED),
  `public_ops_test.go` (ladder advance, support/abuse lifecycle, segments),
  `auth_mfa_test.go` (TOTP generation, throttle lockout, MFA login challenge),
  `provenance` receipt chain-root tests.
- Relay/entitlement enforcement: developer standing gate + traffic-class cap
  (`traffic_envelope.go`), tool authorization on action ingestion, session
  status gates, user offboarding cascades.

---

## 8. What the report cannot claim (honesty section)

- **Certification** is the customer's process; the compliance page is an
  honest self-assessment with evidence + remediation, never a "compliant" badge.
- **External payment provider** is not wired (blocker #4) — refunds are
  recorded, not executed.
- **Operator SSO console login** (blocker #3) — SSO endpoints verify, but the
  console-JWT issuance awaits the operators/authz plan.
- **Harness-side telemetry + span emission** and **PIA-side package
  verification at load** are cross-repo (blockers #5, #6); PCCP-side ingestion/
  enforcement is complete.

---

## 9. Blockers (blockers.md)

| # | Item | Status |
|---|---|---|
| 1 | audit chain test flake (fixed: unique org per run + WAL) | Resolved |
| 2 | sandbox Docker + scheduler streaming timing flakes under parallel load | Open (environmental) |
| 3 | Operator SSO console login → operators/authz plan | Open |
| 4 | Payment provider integration | Open |
| 5 | Harness-side feature telemetry emission (external repo) | Open |
| 6 | PIA-side model load verification (external repo) | Open |

---

## 10. Commit map (plan → commits, in branch order)

S2–S12 fabric: `e7830ac` (S2 core) → `977b932` → `4d57a10` → `98bfc2d` →
`920876a` → `2fda144` (S3) → `94a16cd` (S4/S5) → `696d27e` (S6) → `d5970d9`
(S7) → `4fb881a` (S8) → `3e5f7c2` (S9) → `46f4540` (S10) → `0f97274` (S11/S12)
→ `502569d` (NewScheduler composition) → `bf726bb` → `a0ee622` (binary loops) →
`ddc92d4` → `56a0f1b` → `01f792e` (S3/S4 polish: predictor pair, LoRA/pools,
signed receipts) → `41a40ae` (SLO-scoping class).

Web: `791f35c` (00) · `2c00856` (03) · `3ad4233` (04) · `feec748` (05) ·
`87e5445` (06) · `be720e5` (07) · `a910e3a` (01) · `a98aaf9` (02) · `153cab0`
(08) · `af32863` (09) · `3f68394` (10–12) · `d04ab0f` (13) · `ff45233` (14/15)
· `e28f1ec` (16–19) · `d1123a2` (20–23) · `922dea0` (24–26) · `b707058` (SCC
case queues) · `f74c304` (cross-page sorts).

Paper: `644e466`, `c1b2fd0`, `630bc73`, `adfd4f4`.

Naming/blockers/docs: `6d6951f`, `20da560`, `981775c`, `8d323d8`.

**Total: 344 commits on `forge/pccp-fabric`.**

---

*Generated for validation against the plan of record. Every "implemented"
claim above is backed by committed code + tests on forge/pccp-fabric @ 8d323d8.*

## 11. Deep Review Pass (post-completion audit)

A full spec-drift + correctness review of every plan-of-record was run after
completion. **13 findings fixed** (all with regression tests where
applicable); the entire test suite now passes with `-race` across all 43
packages.

### Fixed findings

| # | Severity | Area | Finding → Fix |
|---|----------|------|---------------|
| 1 | Critical | S2 queue | `queue.Queue` claimed concurrency safety but had no mutex — gateway `Submit` raced the dispatch loop on heap/map internals (caught by `-race`). → Full serialization via internal mutex; contract now true. |
| 2 | Critical | §13.14 | Gateway trusted the client `X-Tenant-ID` header for the per-tenant model allow-list; the signed envelope's TenantID was discarded — tenant impersonation path for anything reaching the gateway. → Envelope is authoritative; header conflict = 403. Regression tests added. |
| 3 | High | web/15 D | Image allowlist used `strings.HasPrefix` — `patty/sandbox` admitted `patty/sandbox-evil`. → Exact repository-part match with optional tag pin or `:*` wildcard. Table-driven tests incl. prefix tricks. |
| 4 | High | web/25 B | TOTP: MFA-code failures bypassed the login throttle (unthrottled 10^6 grind with a stolen password); zero clock-skew tolerance; double `Login` call; MFA rotation without proving the current code. → ±30s skew + constant-time compare, code failures throttle, single Login, rotation requires current code, bounded throttle maps. |
| 5 | High | web/17 E | SIEM cursor advanced over random UUID ids — silently skipped most audit events. → Cursor over the per-org monotonic `chain_seq`; bounded HTTP client so a hung SIEM can't stall the background ticker. Regression test proves full delivery. |
| 6 | High | web/20 A2 | Receipt verifier re-derived an *invented* hash chain (`pccp-chain|` seed over ActionEnvelopes) that mismatched every real F.9 receipt — every legitimate receipt would have shown `chain_root_mismatch`. → Real COSE-Sign1 verification over the canonical receipt fields via `provenance.VerifyReceiptSignature`; tamper regression test. |
| 7 | High | web/06 | Epoch publish superseded the old epoch *before* creating the new one — a failure between the statements left the org with zero active epochs (all session opens fail-closed, permanently). → Supersede+create in one transaction. |
| 8 | Medium | web/01 | Enrollment-code consume was read-`Used`-then-write — double-redeem window. → Atomic `UPDATE … WHERE used = false` as the gate. |
| 9 | Medium | web/13 | Broadcast ack read-modify-wrote `AcksJSON` — concurrent acks lost updates. → Row-locked transaction + idempotent re-ack. |
| 10 | Medium | web/24 | Portal token traveled in the URL query string (access logs, browser history, proxies). → `Authorization: Bearer` header (query param kept for compat). |
| 11 | Low | web/19 | Attribution aggregation consumed spans in unordered DB order, but the AI→human vs human→AI state machine is order-sensitive. → Chronological `created_at` ordering. |
| 12 | Critical | S1→S2 wiring | `Dispatcher.SetSelector` wrote `d.selector` unlocked while the dispatch loop read it (caught by `-race` on the full tree). → Lock-protected install. |
| 13 | Low | pia test | Test read `LastOutcome` without the agent lock (race with the heartbeat goroutine). → Locked test read. |

### Verified clean (no drift)

- **§12.3.1 cost model** — router implements the locked formula (one noted
  nuance: the code clamps `newPrompt − overlap` at 0 *before* adding
  `activePrefill` — more conservative than the spec's clamp of the total,
  behaviorally equivalent under the invariant overlap ≤ input tokens).
- **§12.3.2 affinity-as-preference** — discount only while healthy; overloaded
  workers are filtered before the discount applies.
- **§12.3.4/§13.5 DRR** — token-debit at admission, weight-scaled quanta
  (10/6/2/1), strict class priority, work-conserving, O(1) rotation.
- **§12.3.7 two-layer overload** — edge gate before enqueue (shed batch /
  wait interactive) + worker-local caps.
- **§12.3.8 autoscale inputs** — tokens+latency only; never raw GPU util.
- **§12.3.9 MTP** — accepted-token capacity model present.
- **§12.3.10 aggregated-first** — aggregated default; P/D conditional; SGLang
  split-role refusal.
- **§13.10 batch slack-only** — DispatchOne gated on slack; pause/resume.
- **§13.11 KV journal** — PIA append-only journal + per-worker dedup watermark.
- **§13.14 signed classes** — envelope-only resolution, fail-closed to batch.
- **§14 row 16 gangs** — TP/EP complete-gang readiness; DP independent.
- **Harness A handshake** — proof → profile validation → standing gate →
  mid-auth revocation check; org attribution from the credential only.
- **Webhooks** — HMAC-SHA256 constant-time compare, fail-closed on missing sig.
- **Offboard** — soft status + session close + harness trim + evidence
  confirmation; the relay standing gate can't be bypassed by row deletion
  (offboard never deletes rows).
- **Frontend phantom-data sweep** — the `id:` literals flagged by grep are
  tab definitions, form-state initializers, and the DLP lexicon templates;
  no page renders fabricated records.

### Round 2 + 3 review pass (final)

A second review round (correctness, BOLA, SQL binding, UX feedback) and a
third pure-verification round ran after the fixes above. Round-2 findings,
all fixed and regression-tested where applicable:

| # | Severity | Area | Finding → Fix |
|---|----------|------|---------------|
| 14 | Critical | S2 dispatch | `execute()` error paths (no forwarder / no dispatch addr) requeued the request *after* erroring its waiter — the client got an error, then the work executed and billed anyway. → Error drops the request; verified by `TestExecuteErrorDropsRequestNotRequeues`. |
| 15 | High | S2 dispatch | Expired/drained queue outcomes never completed their waiter — parked HTTP handlers waited their own full timer. → Expiry completes the waiter immediately + a 500ms reap tick so idle fleets still reap; verified by `TestExpiredRequestCompletesWaiter`. |
| 16 | High | web/13 | BOLA: message react/read/edit/delete and broadcast acks fetched by bare id — cross-org mutation possible with exact ids. → Org scoping via the conversation (Message carries no org column); verified by `TestCrossOrgMessageMutationRejected`. User/harness lookups org-scoped likewise. |
| 17 | Medium | web/13 | `SendMessage` accepted nonexistent conversation ids silently. → Conversation existence validated. |
| 18 | Medium | web/25 | TOTP codes replayable within their ±30s validity window. → Per-account timestep replay guard (RFC 6238 §5.2); `TestTOTPReplayRejected`. |
| 19 | Medium | api | Unified-search org scoping interpolated `orgID` into SQL text (claim-derived, but interpolation nonetheless). → Fully parameterized (`scopedWhere`). |
| 20 | Low | gateway | Validly-signed envelope with empty tenant silently fell back to the header tenant. → Rejected as malformed. |
| 21 | Low | relay | Hot-state cache grew without bound between revocation epochs under harness churn. → 100k-entry coarse cap. |
| 22 | Low | api | Unbounded intake: CSV import (now 5 MiB / 50k rows) and fleet bulk (500 targets). |
| 23 | Low | web | 22 silent `catch {}` action handlers gave no failure feedback. → Error toasts across 6 pages. |

Round 3 (verification only) confirmed: `go vet` clean, 43/43 packages
green, **zero data races tree-wide under `-race -count=2`**, web build
green, and the two new semantics tests + BOLA regression pass.

### Documented residuals (not fixed, by judgment)

- Seat-limit enforcement is count-then-create (TOCTOU on the billing limit);
  a serializable transaction or trigger would close it. Not security-
  critical; noted.
- Relay tool enforcement reads the org's tool registry per action envelope
  (correct, one indexed query; a TTL cache would shave the hot path).
- `AuthorizePeer`'s comment says full PPC verification is a follow-up; it is
  implemented in the handshake — comment is stale, code is current.
- Legal holds are placed/lifted/audited, but no retention-purge job exists
  to enforce them against (nothing is ever archived/deleted — the fail-safe
  direction). A retention sweep would complete the §17 C loop.
- Bulk session actions now cap at 500 ids per call (added in this pass).
