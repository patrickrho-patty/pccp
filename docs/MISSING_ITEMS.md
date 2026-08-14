# PCCP v2 — Missing Items & Functionality Audit

**Audited:** 2026-08-13
**Source of truth:** `docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md` (v2 PRD), `docs/MASTER_PLAN.md`
**Method:** every finding is verified against actual code (`file:line`), not against the self-reported status docs (`V2_DOD_AUDIT.md`, `IMPLEMENTATION_STATUS.md`), which are **unreliable** — they claim 26/30 DoD criteria done and every page ✅, which this audit contradicts.

---

## Legend

- 🔴 **MISSING** — no code exists for the requirement.
- 🟡 **FAKE / STUB** — code exists but does not do the real thing (stub return, hardcoded/mock data, placeholder, static template).
- ⚠️ **UNWIRED** — real logic exists but is NOT connected to the live request path, so it is never executed / never enforced.
- ✅ **REAL** — genuinely implemented and functional (called out for honesty).

---

## Executive summary — the dominant failure pattern

**The codebase is a collection of admin CRUD endpoints over a database plus a thin pass-through relay. The governance, trust, metering, and enforcement layers either do not exist, are stubs, or were written but never wired into the live inference path.**

A single AI request today enters the Relay and reaches the GPU having passed through **zero** of the 14 mandated governance stages (§10.2), recorded **zero** usage, and emitted **zero** evidence. This is the difference between "vibe-coded" and enterprise: the seams between components are not welded.

Concrete proof of the pattern — these functions all exist and are documented as if they work, but have **zero callers on the live path**:

| Function | File | Reality |
|---|---|---|
| `RunPipeline` (14-stage governance) | `internal/relay/pipeline.go:40` | 0 callers — dead code |
| `scheduler.Service` (fair scheduler) | `internal/scheduler/` | not imported by api OR relay — dead |
| `detection.Service` (account-sharing) | `internal/detection/` | not imported by api OR relay — dead |
| `CloseExchange` (evidence receipt) | `internal/relay/service.go:333` | reachable only via manual HTTP close, never on automatic AI flow |
| `RecordUsage` (metering) | `internal/workintel/service.go:23` | never called from relay/PIA |

### Headline counts
- **~50 distinct missing/fake/unwired items** identified across 12 areas (10 domains + Information Architecture + cross-cutting).
- **9 of 15 Korean differentiators (§33)** have no code at all.
- **Falsified DoD claims** (claimed ✅, actually false): #3, #4, #6, #7, #8, #14, #22, #27, #28, and substantial parts of #9, #10, #11, #15, #16.
- *Round 2 (completeness) added: §6 two-console enforcement gap, §10B AI-semantic layer entirely unused, §47 onboarding/migration, §50 KPIs/observability — and resolved all prior NEEDS-VERIFY items to definitive findings.*

---

## Domain 1–2 — Protocol / Gateway / Relay / PIA
*PRD §0.2, §9, §10, §10A, §10B, §38. Verification depth: DEEP.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 1.1 | DARI is the **sole** Harness protocol (§0.2, §38.1) | ⚠️ UNWIRED | `getDARIInferenceClient()` activates **only if `PCCP_PIA_DARI_ADDR` set**; otherwise silent HTTP fallback (`internal/relay/dari_listener.go:285`). Not enforced. |
| 1.2 | Every request runs the 14-stage pipeline (§10.2) | ⚠️ UNWIRED | `RunPipeline` has **0 callers**; live path `handleApplicationMessages`→`forwardAIToPIA` runs 0 stages (`internal/relay/dari_listener.go:206,252`). |
| 1.3 | §10.5 Bifrost: governance **before** routing; in-memory hot state; no DB-per-token | 🔴 MISSING | No cache layer; dead pipeline queries DB per stage. |
| 1.4 | Raw/fake model ID rejected (§10A.11) | 🟡 FAKE | `ValidateCatalogModel` only rejects `""` and is never called (`internal/relay/pipeline.go:17`). `forwardAIToPIA` accepts any model string. |
| 1.5 | Metering on path (§10.9, §29.13) | ⚠️ UNWIRED | `RecordUsage` exists but never called from relay/PIA. |
| 1.6 | Evidence emitted per request (§10.2 stage 14) | ⚠️ UNWIRED | `CloseExchange` issues receipts but only on manual close, never automatically. |
| 1.7 | Policy-aware fallback, never to OpenAI/Anthropic (§10.7) | 🔴 MISSING | Silent HTTP→OpenAI-format fallback; no capability-change handling. |
| 1.8 | Streaming: ordered, metered, tool correlation, backpressure (§10.9) | 🔴 MISSING | `forwardAIHTTP` returns a single non-streaming blob. |
| 1.9 | Local cached relay state (trust bundles, leases, epochs, catalog) (§10.8) | 🔴 MISSING | No local cache; conceptually DB-per-request. |
| 1.10 | Hot-path module contract (§10.3, §10.4 A/B/C/D) | 🔴 MISSING | No module contract / categorization implemented. |
| 1.11 | Catalog Model → PMP → Endpoint validation chain (§9.5) | 🟡 FAKE | `findEndpoint` does a DB lookup but the catalog→PMP→lease attestation chain is not enforced on live path. |
| 1.12 | CP must NOT expose OpenAI-compat model invocation (§10.11) | 🔴 VIOLATED | `internal/api/server.go:121` registers `POST /v1/chat/completions`; `:885` returns a **mock** response when PIA unconfigured. |

**What IS real (honest):** the DARI crypto/transport library — `cbor.go` (real `fxamacker/cbor` deterministic RFC 8949), `quic.go` (real `quic-go`), `cose.go`/`peer.go` (real ed25519 sign/verify), 52 message types, handshake/framing/transport with tests. ✅ (Caveat: `QUICMigrationSupport() bool { return true }` is a hardcoded lie; `InsecureSkipVerify:true` defaults in dev TLS configs.)

---

## PIA trust boundary
*PRD §9.4, §9.6. Verification depth: DEEP.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| P.1 | Verify Patty Model Package signature/hash (§9.4) | 🔴 MISSING | `pia/service.go:24` *comment* claims "verifying model packages"; code never checks PMP signature/hash. `VerifyModelLoaded` (`vllm.go:199`) is a name check only. |
| P.2 | Hardware/measured attestation + key release (§9.6) | 🟡 FAKE | `attestation/service.go`: `RawEvidence = {"tpm_quote":"placeholder"}`, `{"attestation_report":"placeholder"}`, `WrappedKey = "wrapped_model_key_placeholder"`. Comment: "provides the framework structure." |
| P.3 | Re-attestation loop | 🟡 FAKE | `StartAttestationLoop` (`pia/service.go:391`) only calls `RequestLease()` on a timer — lease renewal, not attestation. |
| P.4 | Mock inference fallback | 🟡 FAKE | `handleMockInference` (`pia/service.go:308`) returns canned Korean text. |

---

## Domain 3 — Identity, Auth, Org Hierarchy
*PRD §8, §12, §13, §32.1. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 3.1 | SSO/SAML signature verification | 🔴 MISSING / INSECURE | `sso.HandleSAMLCallback` parses XML but **never verifies the signature**, and falls back to `mockSAMLResponse()` on any parse gap → accepts anything (`internal/sso/service.go:73`). |
| 3.2 | OIDC login | 🟡 PARTIAL | `OIDCAuthURL`/`HandleOIDCCallback` build real URLs; not wired to HTTP routes for actual login flow. |
| 3.3 | SCIM provisioning | 🟡 STUB | `HandleSCIMRequest` exists; not routed. |
| 3.4 | Independent Harness enrollment + cert issuance (§8.4) | ⚠️ PARTIAL | Enrollment records exist; cert issuance not verified to produce enforceable peer credentials. |
| 3.5 | Korean org hierarchy: Group→Affiliate→Division (§12.1, §33.1) | 🔴 MISSING | No group/affiliate model. Flat org only. |
| 3.6 | Delegated administration (§12.3) | 🔴 MISSING | — |
| 3.7 | Contractor / SI mode (§12.4, §33.2) | 🟡 PARTIAL | `users.contractor_info` column exists; no SI-mode logic, contract expiry, or auto-disable. |
| 3.8 | ABAC policy decisions (§13.2) | 🔴 MISSING | `policy` package is 212 LOC; no attribute-based decision engine. |

---

## Domain — Model Catalog & Registry
*PRD §10A, §11. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| C.1 | Server-authoritative catalog **sent to harness over DARI** (§10A.4 `dari.models/1`) | 🔴 MISSING | Extension is only *advertised* in HELLO (`paper_client.go:60`) + message-type stubs (`paper/models.go`). **No code ever pushes a snapshot to a connected harness.** Falsifies DoD #3, #4. |
| C.2 | Catalog epoch lifecycle (§10A.5) | ⚠️ UNWIRED | `GenerateCatalogEpoch`/`ValidateCatalogEpoch` exist (`catalog/service.go:81,130`); not bound to the relay path. |
| C.3 | Admin catalog CRUD | ✅ REAL | `RegisterCatalogModel`, `WithdrawModel`, `AnnounceModel`, `SeedDefaultCatalog` (DB-backed). |

---

## Domain 4 — Live Ops: Fleet, Sessions, Live View
*PRD §14. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 4.1 | Fleet actions (revoke/quarantine/lockdown) | ⚠️ PARTIAL | `fleet.PerformAction` does real DB updates + session/lease revocation (`fleet/service.go:129`). **BUT not enforced**: the live relay path never checks harness status, so a revoked harness can still connect/infer. |
| 4.2 | Session inspector (§14.3) | ⚠️ UNWIRED | `InspectSession` exists; reads DB only, no live data plane correlation. |
| 4.3 | Live view = real live harness output | 🟡 FAKE | `LiveView.tsx` is scripted/simulated; no real-time feed from the relay. |
| 4.4 | Session state machine enforced (§14.5) | ⚠️ UNWIRED | State stored in DB; not enforced at the data plane. |
| 4.5 | Revocation/quarantine enforced on connect & per-request | 🔴 MISSING | Status checks exist only in dead `pipeline.go`; `paper_listener.go` enforces nothing. |

---

## Domain 5 — Security Ops
*PRD §15, §16, §35. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 5.1 | Security findings from real detection | 🟡 FAKE | `korean.DetectShadowAI`: "For now, return findings from recorded security events" (`korean/service.go:87`). |
| 5.2 | Inline DLP / context firewall (§16) | ⚠️ UNWIRED | `security`/`context` packages exist with rule catalogs; **not invoked on the live inference path** (relay imports `security` but never calls it in `forwardAI*`). |
| 5.3 | Token counting for metering/privacy | 🟡 FAKE | `context/service.go:175`: "placeholder — production should use an actual tokenizer." |
| 5.4 | Prompt-injection defence (§16.4, §35.8) | ⚠️ UNWIRED | Patterns defined; not enforced inline. |
| 5.5 | Incident containment / lockdown | ⚠️ PARTIAL | `fleet` lockdown writes DB; not propagated to live peers. |
| 5.6 | Active threat detection on connections | 🔴 MISSING/DEAD | `detection` package (geo-implausible, credential replay, multi-shift) is **not imported by api or relay**; `RecordConcurrentHarness` is a stub (`detection/service.go:48`). |

---

## Domain 6 — Tools/MCP/Network/Secrets + Git/SCM + Provenance
*PRD §17, §18, §19, §20. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 6.1 | Tool registry / MCP governance (§17.1, §17.2) | ⚠️ PARTIAL | `tools`/`mcp`/`mcpmarket` packages + `/tools` API exist; governance not enforced at request time. |
| 6.2 | Network broker usage accounting (§17.4) | 🟡 FAKE | `network/service.go:182`: "would update a counter. For now we record in audit." |
| 6.3 | Secret broker (§17.5) | ⚠️ PARTIAL | `secret` package exists; brokering not wired to harness sessions. |
| 6.4 | Git baselines / change-set graph / branch governance (§18) | ⚠️ PARTIAL | `gitscm` package + provenance endpoints exist; no real SCM integration (no GitHub, no file browsing — matches user complaint). |
| 6.5 | Line-level provenance (§19) | ⚠️ UNWIRED | `provenance.CreateProvenanceSpan`/`LookupCodeSpan` are real logic, **but never fed by the live path** (no session records changes), so the explorer shows nothing. |
| 6.6 | Change impact intelligence (§20) | ⚠️ PARTIAL | `impact` package (455 LOC) exists; not bound to live changesets. |
| 6.7 | Sandbox runtime (§31) | 🟡 FAKE | `sandbox/service.go:97,169`: "For now, we record the sandbox definition" / "reconstruct from audit." No real runtime. |

---

## Domain 7 — Public Cloud
*PRD §10C, §29. Verification depth: MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 7.1 | Subscription / account / work-slot model | ✅ REAL | `publiccloud` package: `CreateAccount/Subscription`, `AcquireWorkSlot` (in-memory tracker), 4 account states. Data model is sound. |
| 7.2 | Capacity lease enforced at request time | ⚠️ UNWIRED | `IssueCapacityLease`/`ValidateCapacityLease` exist; the dead pipeline's stage 7 is `return nil`. Not enforced. |
| 7.3 | Fair scheduler (§10C.7) | ⚠️ UNWIRED/DEAD | `scheduler` package has real Enqueue/Admit/CLU logic but is **not imported by api or relay**. Admission today = "always admit." Falsifies DoD #14. |
| 7.4 | Account-sharing / abuse detection (§10C.9, §10C.10) | ⚠️ UNWIRED/DEAD | `detection` package real-ish but dead (7.2 above). Graduated-response ladder not driven by live signals. |
| 7.5 | SRE console real metrics | 🟡 PARTIAL | `SREConsole.tsx` renders; data not from live capacity/risk state. |

---

## Domain 8 — Communications Hub
*PRD §21–23. Verification depth: LOW–MEDIUM.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 8.1 | Chat / presence / broadcast (CRUD) | ✅ REAL (metadata) | `communications` package: real DB-backed conversations, messages, presence, broadcasts. |
| 8.2 | Real-time delivery to harness (§21.2) | 🔴 MISSING | No WebSocket/SSE; not delivered over DARI to harness. CP-side only. |
| 8.3 | File transfer actual storage (§23) | 🟡 PARTIAL | `CreateFileTransfer`/`CompleteFileTransfer` record metadata; no real file storage/transfer. |
| 8.4 | End-to-end encryption (§21.5) | 🔴 MISSING | — |

---

## Domain 9 — Work Intelligence
*PRD §24–26, §28. Verification depth: LOW.*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 9.1 | Usage analytics | ⚠️ UNWIRED | `GetUsageSummary`/`GetEngineeringMetrics` exist; `RecordUsage` never called from path → analytics run on empty/seeded data only. |
| 9.2 | Scorecards / rubric (§25) | 🟡 PARTIAL | `GenerateScorecard` (`workintel/service.go:201`) is a real weighted rubric (30/25/20/15/10 per §25.2) and correctly forces `RequiresHumanFinalization` always (§26). BUT scoring is naive — raw change/line counts drive scores, which §24.3 classifies as weak signals — and it runs on empty data (`RecordUsage` never called). |
| 9.3 | Employment-decision guardrails (§26) | 🟡 PARTIAL | Human-finalization gate correctly enforced (`RequiresHumanFinalization=true` always). Dispute/correction (§26.3) + bias/gaming controls (§26.4) not implemented. |
| 9.4 | Natural-language analytics (§28.4) | 🔴 MISSING | — |

---

## Domain 10 — Compliance / Privacy / Audit / Korean Differentiators / Crypto / Events / NFRs
*PRD §27, §33, §36, §39, §40, §41, §43, §44. Verification depth: MEDIUM.*

### §33 Korean differentiators — 9 of 15 have NO code
| § | Feature | Status |
|---|---|---|
| 33.1 | Group / Affiliate Control Tower | 🔴 MISSING |
| 33.2 | SI / Outsourced Developer Mode | 🟡 PARTIAL (column only) |
| 33.3 | Shadow AI Discovery | 🟡 FAKE (stub) |
| 33.4 | AI Change Control Board | 🔴 MISSING |
| 33.5 | Repository Sensitivity Heatmap | 🔴 MISSING |
| 33.6 | Mandatory Policy Acknowledgement | 🔴 MISSING |
| 33.7 | Org AI Skills Matrix | 🟡 STUB (`GetAISkillsMatrix`) |
| 33.8 | Policy Exception Marketplace | 🔴 MISSING |
| 33.9 | Emergency Model Recall | 🟡 STUB (`EmergencyModelRecall`) |
| 33.10 | Forced Harness Version / Rings | 🟡 STUB (`SetForcedHarnessVersion`) |
| 33.11 | Architecture / Coding Standard Packs | 🔴 MISSING |
| 33.12 | Executive Governance Brief | 🟡 STUB (`GenerateGovernanceBrief`) |
| 33.13 | Change-Freezing / Critical Period | 🟡 STUB (`InitiateChangeFreeze`) — not enforced by relay |
| 33.14 | Project Offboarding / Evidence Handoff | 🔴 MISSING |
| 33.15 | AI Model ROI Comparison | 🔴 MISSING |

### Other Domain 10 items
| # | Requirement | Status | Evidence |
|---|---|---|---|
| 10.1 | Compliance assessment (§41) — CSAP 간편/일반, ISMS-P levels | 🟡 FAKE | `compliance.AssessCompliance` scores off a **hardcoded** `control.Status` field → identical result for every org; no real evidence gathering (`compliance/service.go:110`). |
| 10.2 | Audit retention classes / legal hold (§40) | 🟡 PARTIAL | `LegalHold` field on Base/provenance; `privacy.RetentionPolicy` + defaults defined; **no enforcement/purge job** — retention is declarative only (see X.6). |
| 10.3 | Privacy visibility levels enforced (§27) | 🟡 FAKE | `privacy/service.go:187`: "placeholder — the security service handles actual detection." |
| 10.4 | Sovereign offline updates (§34.5) | 🟡 FAKE | `sovereign/service.go:114`: "would apply the update to the local system." |
| 10.5 | External integrations: SIEM/HRIS/SCM (§32) | 🟡 STUB | `connectors/service.go:177`: "Implementation Stubs." |
| 10.6 | Durable event spine / event topics (§39) | ⚠️ PARTIAL | `events` package exists; not a real durable spine wired to consumers. |
| 10.7 | Key management / rotation (§36) | 🟡 PARTIAL | `keymgmt`: Ed25519 key gen + rotation (`RotatedFrom`) modeled; no HSM/KMS; keys held in DB/memory (see X.4). |

---

## Domain — Information Architecture & Operations Surfaces
*PRD §6, §7. Verification depth: MEDIUM (completeness round 2).*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| IA.1 | Two-console model (Patty Ops + Customer Control) (§6.1, §6.2) | ✅ REAL (UI) | Three profiles `patty_ops`/`portal`/customer with separate layouts + routes (`App.tsx:66,75,87`). |
| IA.2 | §6.5 privacy-aware role/content separation (PCCPPlatformAdmin must NOT auto-see T&S/billing/prompt/comms/WI) | 🔴 NOT ENFORCED | Console switching is a **client-side dropdown only**; no server-side authorization boundary. A platform admin can view all content regardless of console. |
| IA.3 | §6.4 global search (account/harness/session/exchange/IP/ASN/subscription/payment/incident + user/repo/commit/file/symbol/provenance) | 🟡 PARTIAL | `GlobalSearch` (`Layout.tsx:98`) fetches full lists of 5 entity types and filters client-side. Missing: exchange, IP/ASN, subscription, payment, incident, commit, file, symbol, provenance. Non-scaling; not console-specific. |
| IA.4 | §6.6 Account Portal self-service | 🟡 PARTIAL | `AccountPortal.tsx` exists; invoices/payment-provider integration MISSING (`PaymentProvider` is a bare field; SRE shows Payments `not_configured`); "sign out all"/recovery/data-privacy unverified. |
| IA.5 | §7.5 Wallboard mode | 🔴 MISSING | No wallboard/kiosk/fullscreen mode. |
| IA.6 | §7.6 Historical comparison | 🔴 MISSING | No period-over-period comparison. |

## Cross-cutting & previously-omitted sections
*PRD §10B, §29, §30, §36, §37, §39, §40, §42, §45, §46, §47, §49, §50. Verification depth: MEDIUM (round 2).*

| # | Requirement | Status | Evidence |
|---|---|---|---|
| X.1 | §10B DARI AI Semantic Contract (tools, structured output, multimodal, streaming events, cache) | ⚠️ UNWIRED | Rich types defined (`paper/ai_v2.go`: `ToolDescriptorV2`, `AIStreamEvent`, `ToolCallV2`, …) but used **nowhere** outside that file. Live path uses bare `{model,messages,max_tokens}`. Falsifies DoD #8. |
| X.2 | §29 Billing / chargeback / rate-limit hierarchy | 🟡 PARTIAL | Subscription/usage modeled; chargeback (§29.12), rate-limit hierarchy (§29.8), payment provider (§29.9) MISSING. |
| X.3 | §30 Model & GPU operations | ✅ REAL / ⚠️ | `gpuops`: real endpoint/GPU metrics + routing decision + tests. Not fed by live telemetry from PIA/vLLM. |
| X.4 | §36 Key management / rotation | 🟡 PARTIAL | `keymgmt`: Ed25519 key gen + rotation fields modeled; NO HSM/KMS; keys held in DB/memory. |
| X.5 | §37 / §39 Durable event spine | ✅ REAL (spine) | `events` signs+persists+queries events (§39), tested. Account sharding (§37.3) N/A single-tenant dev. Under-fed (inference path emits nothing). |
| X.6 | §40 Audit retention classes / legal hold | 🟡 PARTIAL | `LegalHold` field + `privacy.RetentionPolicy` defaults exist; **no enforcement/purge job** — declarative only. |
| X.7 | §42 Open-source deliverables (PIA SDK) | 🟡 PARTIAL | `adapters/{vllm,sglang}` real Go; `registry/*.csv` real; **PIA SDK is `.go.txt` — non-compilable documentation, not an importable library** (`sdk/piapi/piapi.go.txt`). |
| X.8 | §45 Reporting (scheduled/standard) | 🟡 PARTIAL | `reporting`: real generators (governance/usage/security/executive + digest); §45.2 scheduled delivery (email/cron) MISSING. |
| X.9 | §46 Product admin / config change mgmt | ✅ REAL | `configmgmt`: full lifecycle state machine (draft→validating→pending_approval→approved→publishing→rolling_out→enforcing→rolled_back). |
| X.10 | §47 Public onboarding / enterprise rollout / v1→v2 migration | 🔴 MISSING | No onboarding wizard, no rollout flow, no brownfield migration tooling. |
| X.11 | §49 Cross-product acceptance criteria (A–J) | ⚠️ MOSTLY FAIL | Maps to P0/P1 gaps: auth/subscription (B), DARI-only (C), model catalog (D), AI semantics (E), capacity (F), account integrity (G), SRE (H). Most acceptance gates currently fail. |
| X.12 | §50 Product KPIs / observability | 🔴 MISSING | No real metrics/KPI collection pipeline. |

## Severity-ranked rebuild priorities

**P0 — product is not functional without these (the inference path is ungoverned):**
1. Wire the 14-stage `RunPipeline` onto the live DARI path (§10.2) — currently dead code.
2. Enforce DARI as the sole transport; remove/gate the CP `/v1/chat/completions` OpenAI endpoint (§0.2, §10.11, §38.1).
3. Wire metering (`RecordUsage`) + evidence (`CloseExchange`) into the automatic inference lifecycle (§10.2 stages 13–14, §10.9).
4. Enforce fleet revocation/quarantine + catalog/PMP validation on the live path (so admin actions actually take effect).

**P1 — trust boundary & enforcement:**
5. PMP signature/hash verification at PIA (§9.4) — replace placeholder attestation.
6. Push the model catalog to harnesses over DARI (`dari.models/1`) (§10A).
7. Inline DLP/injection/security on the live path (§16).
8. Real fair scheduler admission (§10C.7) — revive the dead `scheduler` package or remove the claim.

**P2 — enterprise completeness:**
9. Real compliance assessment (CSAP/ISMS-P) replacing the static template (§41).
10. Korean org hierarchy + the 9 missing §33 differentiators.
11. Real-time comms delivery to harness (§21) + real provenance feeding (§19).
12. SSO signature verification + remove mock fallback (security hole).

---

## Notes on confidence
- **DEEP** (domains 1–2, PIA): findings read from full function bodies; high confidence.
- **MEDIUM**: service-level signatures + key method bodies verified; wiring confirmed via import graph.
- **LOW**: package presence + stub markers confirmed; internal logic not fully read.
- Round 2 (completeness) **resolved all prior LOW-confidence "NEEDS VERIFY" items** — work-intel scorecard, employment guardrails, keymgmt, events spine, and audit retention are now definitive findings (see 9.2/9.3 and X.4/X.5/X.6).
- This audit prioritized the **inference/governance spine** (where the product's value lives). The long-tail admin CRUD (most pages) is DB-backed and largely real *as data entry*, but nothing is enforced end-to-end.
