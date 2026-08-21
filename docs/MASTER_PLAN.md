# Patty Code Control Plane — Master Plan

**Repo:** `patrickrho-patty/pccp` (this repo)
**Status:** Revised 2026-08-21 for PRD v2 alignment and the shipped implementation. Original draft v1 derived from the frozen PRDs and harness inspection on 2026-08-11.
**Owner:** PCCP team
**Audience:** Anyone implementing, reviewing, or operating PCCP

---

## 1. What we're building

**Patty Code Control Plane (PCCP)** is the governance, identity, security, provenance, communication, and operational backbone of the Patty Code product line. Since PRD v2 it is a **single shared kernel** that powers every deployment profile — Public Cloud, Enterprise, and Government/Sovereign — pairing with the **Patty Code Harness** to turn AI-assisted software development into something from an individual subscriber to a regulated organisation can actually buy.

Concretely, PCCP is six systems stitched into one product:

1. **AI Engineering Control Plane** — orgs, users, harnesses, sessions, repos, models, tools, runtime.
2. **AI Security & Governance Platform** — policy, DLP, PII/secrets, injection defence, tool/MCP/network control, approvals, incidents.
3. **AI Provenance & Software Lineage Platform** — prompt → code → commit attribution with evidence.
4. **Enterprise Engineering Operations Console** — live operational visibility, GPU/model health, usage, budgets, investigations.
5. **Engineering Communications Hub** — presence, chat, governed file transfer, broadcasts, all native to the harness.
6. **Work Intelligence Platform** — evidence-backed productivity, quality, AI-use, and contribution analytics with a human finalization gate.

The gateway is real, but it is **infrastructure** — not the product identity. Every feature exists to make "AI development you can actually govern" real.

This is the same product across three deployment profiles:

| Profile | Where it runs | What changes |
|---|---|---|
| **Patty Public Cloud** | Patty cloud | First-class profile since v2: subscriber Account Portal plus the internal Patty Ops console; scale, fair use, and abuse resistance are its hardest problems |
| **Enterprise** | Patty SaaS, customer private cloud, or on-prem | Full customer console, organisation hierarchy |
| **Government / Sovereign** | On-prem, closed network, air-gapped | Same core, stricter policy defaults, offline updates, local KMS/HSM/PKI |

The three profiles share one codebase, one protocol, one provenance schema, one policy engine. Differences are **policy defaults and deployment topology**, not feature forks.

---

## 2. Where things live

```
pccp/                                    ← this repo (the control plane product + protocol)
├── docs/
│   ├── MASTER_PLAN.md                   ← you are here
│   ├── CURRENT_STATE.md                 ← living implementation status (updated per milestone)
│   ├── V2_DOD_AUDIT.md                  ← PRD v2 Definition of Done, criterion by criterion
│   ├── pccp_v2/
│   │   └── Patty_Code_Control_Plane_PCCP_PRD_v2.0.md  ← authoritative PRD (supersedes v1)
│   └── plans/
│       ├── Patty_Code_Control_Plane_PRD_v1.md    ← the v1 product PRD
│       └── DARI/
│           ├── DARI_Comprehensive_PRD_v1.0.md   ← the protocol's PRD
│           ├── DARI_Protocol_Specification_v1.0.md  ← the wire protocol
│           └── DARI_arXiv_Paper_v1.0.md         ← the research manuscript
├── cmd/                                 ← binary entrypoints: server · relay · scheduler · pia · bench · alert-backfill
├── internal/                            ← Go packages: dari protocol, scheduler, relay, pia, api, models, …
├── web/                                 ← React admin console (three consoles)
├── conformance/                         ← DARI conformance suite
├── adapters/ · sdk/ · registry/         ← open-source protocol deliverables
├── deployments/                         ← Docker · Kubernetes
└── patty-code -> ../patty-code          ← symlink to the sibling harness repo
```

### Repo responsibilities

| Repo | Owns | Does not own |
|---|---|---|
| **`pccp`** (this) | Control Plane, DARI Relay, Model Scheduler, PIA, protocol spec, schema, conformance suite, public docs | The Harness binary itself |
| **`patty-code`** (sibling repo, symlinked at `./patty-code`) | The Harness — terminal/IDE agent, the thing developers actually run | Inline policy enforcement at the Relay level (the Harness enforces at the application boundary) |

The boundary is intentional: PCCP is authoritative. The Harness is an enrolled peer that asks for permission, never grants it.

---

## 3. The three components and how they fit

```
┌──────────────────────────────────────────────────────────────────┐
│                  Patty Code Control Plane (PCCP)                 │
│                                                                  │
│     identity / model catalog / policy / registry / audit         │
│        / provenance / comms / metering / work intelligence       │
└───────────────────────────────┬──────────────────────────────────┘
                                │  signed hot state — leases, policy
                                │  epochs, catalog snapshots, keys
                                ▼
 ┌──────────┐  DARI   ┌────────┐       ┌───────────┐  DARI   ┌──────┐
 │ Harness  │ ──────► │ Relay  │ ────► │ Scheduler │ ──────► │ PIA  │ ──► vLLM / SGLang
 │ HARNESS  │         │ :8090  │       │  :8455    │         │:9090 │       GPU
 │ profile  │         └────────┘       └───────────┘         └──────┘
 └──────────┘
 developer machine
```

The Harness talks **only** to the Relay. The hot path is signed-state driven: Relay authenticates and judges, Scheduler places, PIA verifies its lease before touching a serving engine. Raw model-serving ports are an internal detail of PIA.

### Component summaries

- **Control Plane (PCCP proper).** Authoritative for orgs, users, harness identities, projects, repositories, model registry and catalog, endpoint registry, policy packs, approvals, provenance store, evidence, audit, billing, admin, comms. Not in the hot token path.
- **DARI Relay.** Horizontally scalable data plane entry point. Authenticates peers, validates capability leases, binds policy epochs, performs DLP/injection/security checks inline, forwards to the scheduler with signed traffic envelopes, emits evidence receipts.
- **Model Scheduler.** The traffic director between Relay and the PIA fleet (`internal/scheduler`, `cmd/pccp-scheduler`). Owns admission and queueing, KV-cache-aware placement, prefill/decode disaggregated execution, shadow/canary rollout of routing capabilities, region selection with preauthorized failover honoring data residency, and per-stage SLO measurement.
- **PIA (Patty Inference Agent).** A small, signed, registered INFERENCE peer. Stands between the Scheduler and vLLM/SGLang. Holds a workload identity, verifies the PMP at startup, holds an `EndpointLease`, and re-attests periodically. The only thing the Harness sees as a model endpoint.
- **Patty Code Harness.** The developer-facing terminal/IDE agent, developed in the sibling `patty-code` repository. Speaks DARI for all Patty service inference; no provider/base-URL configuration exists on the official route.

The full protocol surface is defined in [`docs/plans/DARI/DARI_Protocol_Specification_v1.0.md`](plans/DARI/DARI_Protocol_Specification_v1.0.md). The protocol's own PRD is [`docs/plans/DARI/DARI_Comprehensive_PRD_v1.0.md`](plans/DARI/DARI_Comprehensive_PRD_v1.0.md). The research narrative is [`docs/plans/DARI/DARI_arXiv_Paper_v1.0.md`](plans/DARI/DARI_arXiv_Paper_v1.0.md).

---

## 4. Current state (as of 2026-08-21)

The original §4 (2026-08-11) recorded the greenfield moment: no source code, no protocol
implementation, nothing deployed. That era is over — the table below is today's reality, and
point-in-time status now lives in [`docs/CURRENT_STATE.md`](CURRENT_STATE.md).

| Component | Status | Evidence |
|---|---|---|
| Implementation | Shipped through PRD v2 Definition of Done — 26/30 criteria fully implemented | `docs/V2_DOD_AUDIT.md` |
| Binaries | Six: server, relay, scheduler, pia, bench, alert-backfill | `cmd/` |
| Tests | 1,157 passing across 57 packages, 0 failures | `go test ./...` |
| Admin console | 54 React pages over three consoles (Patty Ops / Enterprise / Account Portal) | `web/src/pages/` |
| REST surface | 451 registered routes | `internal/api/server.go` |
| Scheduler evolution | PAT-1445 complete — KV-aware routing, P/D disaggregation, canary rollout, region failover | `docs/plans/2026-08-20-pat-1445-router-evolution-completion.md` |
| Protocol deliverables | DARI spec + PRD + arXiv manuscript; vLLM/SGLang adapters; PIA SDK; registry CSVs | `docs/plans/DARI/`, `adapters/`, `sdk/`, `registry/` |

---

## 5. Definition of Done (for PCCP)

Lift-and-edit from PRD §54; the team holds this as the single bar that decides "is PCCP shipping?":

1. Enterprise admins can manage users, harnesses, projects, models, policies, and entitlements from one Korean-first Control interface.
2. User identity and Harness identity are independently authenticated and correlated for every protected action.
3. The official Gateway will not route to an arbitrary vLLM/SGLang endpoint merely because it claims a Patty model name.
4. Patty model endpoints have verifiable endpoint identity and model-package evidence under the configured assurance profile.
5. Every protected session produces an attributable chain from user/Harness → prompt/context → model/tools → files/code → commit → outcome.
6. Administrators can inspect and contain live Harness sessions according to role and purpose.
7. Security controls can block secrets, Korean PII, unauthorized context, unsafe tools/commands/networks, and unapproved endpoints before protected actions occur.
8. A reviewer can click AI-assisted code and see grounded provenance that survives ordinary repository evolution.
9. Enterprise users can securely communicate, transfer approved files, hand off tasks, and receive targeted/critical broadcasts from within the Harness/IDE.
10. Work Intelligence can generate evidence-backed role-specific engineering/AI-use scorecards without reducing employee performance to raw activity volume or autonomously finalizing personnel decisions.
11. Enterprise subscription/cloud and Government on-prem/air-gap profiles use the same core schemas, Harness, Control Plane, policy engine, provenance model, and event contracts.
12. Government deployments can operate without a required external Patty cloud dependency.
13. Admin access and admin actions are themselves fully auditable.
14. The product can demonstrate meaningful differentiation even if all routing/Gateway functionality is described in one paragraph — governance, security, provenance, operational control, communications, and work intelligence remain the product.

The first build slice (§7) is the smallest slice that begins to satisfy criterion 5. The phases below (§6) are the path to satisfying all 14.

---

## 6. Phased roadmap

> **Historical context.** The v1 phase gates below carried the build from zero to the
> Enterprise fleet + Gateway milestone (Phases 0–1 shipped August 2026). PRD v2 then replaced
> the greenfield roadmap with the migration/expansion roadmap in [PRD v2 §48](pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md);
> progress against v2 is tracked criterion-by-criterion in `docs/V2_DOD_AUDIT.md`. The table is kept
> because the gate discipline — every phase ends deployable, demonstrable, testable — still governs how we work.

The v1 roadmap was a 6-phase, 18-month plan spanning trust foundation → enterprise fleet → provenance/comms → work intelligence → sovereign hardening → scale:

| Phase | Window | Theme | Gate (must pass before next phase starts) |
|---|---|---|---|
| **0** | Weeks 0–6 | Contracts & trust foundation | One enrolled user + one enrolled Harness can send one governed request only to an attested Patty endpoint; the complete action is auditable. |
| **1** | Months 2–4 | Enterprise fleet + Gateway | Admins can enroll/revoke Harnesses, manage users, restrict models, trace one session to repository state. |
| **2** | Months 4–7 | Provenance + security enforcement | Click a code span and recover full session/user/Harness/model/context/tools/policies/tests; critical incident can isolate session/Harness. |
| **3** | Months 6–9 | Communications + enterprise operations | Enterprise can run PCCP as the central developer-AI operations hub without external chat for necessary engineering coordination. |
| **4** | Months 8–12 | Work Intelligence + advanced analytics | An evaluation scorecard can be explained entirely from underlying signed work evidence and requires human finalization. |
| **5** | Months 10–15 | Private/sovereign hardening | Representative Government/Sovereign environment can install, license, attest endpoints, operate, audit, and update without public internet. |
| **6** | Months 13–18 | Scale + ecosystem | Large-group tenancy, MCP registry, advanced connectors, certification-readiness. |

Each phase is implemented as **vertical slices** — every increment must produce deployable, testable software and evidence before the next slice begins. Horizontal "build all the dashboards first" is explicitly anti-pattern.

---

## 7. First build slice (Phase 0 — the "one request" milestone)

This is from PRD §Appendix H verbatim. Everything else expands around these same IDs and contracts. **If only one thing ships in Phase 0, it is this.**

1. Org, User, Harness, Project, Repository, Session, Action schemas.
2. User SSO + independent Harness enrollment / certificate.
3. Signed `ActionEnvelope` and audit stream.
4. `ModelPackage`, `InferenceEndpoint`, `EndpointAttestation`, `EndpointLease` schemas.
5. PIA in front of one vLLM deployment.
6. Signed Patty model manifest verification.
7. PCCP Gateway that rejects any endpoint without a valid `EndpointLease`.
8. One repository baseline + one model request correlated end-to-end.
9. One code patch written as `ChangeSet` with provenance to user/Harness/model.
10. Minimal Control UI showing user, Harness, session, repo/branch, model endpoint, request, and signed timeline.

**First demonstrable milestone (the thing we want to demo at the end of Phase 0):**

> An enterprise admin enrolls 김개발 and his managed Patty Code Harness, assigns him to Project A, permits only `Patty-KoCoder-v1`, starts a session on `repo/payment-service` branch `feature/refund`, sends one Korean coding request, routes it only to an attested Patty endpoint, records the resulting edit against exact Git state, and then opens Control to see the complete user → Harness → prompt → model → file → diff → policy → commit provenance chain.

That single flow proves the architecture. Every later phase extends the same IDs, contracts, and evidence chain.

---

## 8. Workstreams

The cross-cutting workstreams that cut across phases. Each one owns a slice of the protocol and the schemas.

| Workstream | Owns | Phase 0 deliverable |
|---|---|---|
| **Identity & Harness enrollment** | `Organization`, `User`, `Harness`, `Device`, certificate issuance, SCIM/SSO | One user + one harness enrolled, independent certs |
| **DARI protocol reference impl** | `paper-go` (or chosen language) — framing, transport, leases, policy epochs, evidence, conformance suite | Inbound `HELLO` from Harness accepted; `ActionEnvelope` parsed |
| **DARI Relay** | AuthN, lease validation, policy epoch binding, DLP, routing, verdicts, evidence | One Relay rejecting an endpoint without a valid `EndpointLease` |
| **PIA** | `EndpointLease` consumer, PMP verification, local-serving bridge, attestation re-issue | One PIA in front of the dev vLLM, enrolled + attested |
| **Model registry & PMP** | `PattyModelPackage` schema, signing, registry, content addressing | One signed PMP for `Patty-KoCoder-v1` |
| **Control Plane core** | Org/User/Harness/Project/Repository/Session/Action APIs, audit, admin shell | Minimal Control UI showing the first end-to-end flow |
| **Provenance** | `ChangeSet`, `ProvenanceSpan`, content-addressed DAG, Evidence Receipts | One `ChangeSet` with full user/Harness/model/commit lineage |
| **Harness integration** | DARI enrolment, lease-aware session, evidence emission in `patty-code-pccp` | Harness talks to PCCP dev instance using DARI; OpenAI-compatible path remains as dev fallback only |
| **Security ops** | DLP/PII/secrets/injection checks, tool/MCP/network policy | Inline DLP on context disclosure |
| **Admin / UX** | Korean-first admin console, role-based access, audit trail | Phase-0 minimal UI |
| **Deployment profiles** | Patty cloud / Enterprise private / Government air-gap | One baseline that runs unmodified in dev (cloud); isolated config for on-prem |

---

## 9. Open questions (track these before they bite)

From PRD §53, plus a few the integration work has surfaced:

1. **Product naming.** "Patty Code Control Plane" is temporary — the official marketing name is still an open decision.
2. **PMP manifest schema.** Whole-artifact hash vs. Merkle/chunk manifest for very large models (likely a mix).
3. **PIA architecture.** Sidecar vs. local reverse proxy vs. engine plugin — phase-by-phase decision.
4. **Hardware attestation.** Required for Government only, or an Enterprise premium tier?
5. **Model encryption.** Which Patty distributions require encrypted-at-rest packages + attested key release?
6. **Default transcript retention.** Metadata-only / redacted / full by enterprise profile.
7. **Harness dual-stack.** How long do we keep the OpenAI-compatible path in `patty-code-pccp` after DARI is live? (Affects the harness integration plan.)
8. **Open-source boundary.** Exact split between this repo (public) and the proprietary Patty control plane services. PRD §42 + PRD §9.9 make this explicit.
9. **Korean PII lexicon.** Needs a maintained, versioned policy pack — works hand-in-hand with the data-classification taxonomy.
10. **Work Intelligence scoring defaults.** Ship sample templates or require customer-defined rubrics? (Default: ship samples, allow override.)

Each open question is a ticket, not a footnote. Resolve it before the sprint that depends on it.

---

## 10. Iteration rules

These are non-negotiable for the team. They prevent the failure mode where the work threads itself into a single product the PRD never intended.

1. **One product, three profiles.** Never accept code that forks for Government. Different policy defaults, different deployment topology, same code.
2. **Schema before UI.** Every entity talked about in the PRD becomes a signed schema before any dashboard renders it.
3. **Vertical slices, not horizontal layers.** Every increment must ship end-to-end through Harness → Relay → PIA → Control, even if the surface is tiny.
4. **Evidence is part of the build.** Every protected action emits an event in the same sprint the action is implemented. No retroactive "add logging later".
5. **Conformance is part of the protocol.** DARI comes with a conformance suite. Reference implementation must pass it. Independent implementations must be able to pass it.
6. **Open-source boundary is enforced.** What is open stays open; what is proprietary stays proprietary. PRD §9.9 is the canonical statement of the trust boundary.
7. **Harness changes ship via the worktree.** `patty-code-pccp/` is a worktree of the patty-code repo. Harness commits land there and are pushed to that repo, not to `pccp`. This repo holds the PCCP contract; the harness holds the harness wiring.
8. **No phantom compliance.** The product does not claim "CSAP compliant" or "KISA certified" or "ISMS-P compliant" merely because a feature exists. Maps and evidence are the product; the certification is the customer's process.

---

## 11. References

### In this repo

- PRD v2 (authoritative): [`docs/pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md`](pccp_v2/Patty_Code_Control_Plane_PCCP_PRD_v2.0.md)
- Product PRD v1: [`docs/plans/Patty_Code_Control_Plane_PRD_v1.md`](plans/Patty_Code_Control_Plane_PRD_v1.md)
- Protocol PRD: [`docs/plans/DARI/DARI_Comprehensive_PRD_v1.0.md`](plans/DARI/DARI_Comprehensive_PRD_v1.0.md)
- Protocol specification: [`docs/plans/DARI/DARI_Protocol_Specification_v1.0.md`](plans/DARI/DARI_Protocol_Specification_v1.0.md)
- Research manuscript: [`docs/plans/DARI/DARI_arXiv_Paper_v1.0.md`](plans/DARI/DARI_arXiv_Paper_v1.0.md)
- Living status: [`docs/CURRENT_STATE.md`](CURRENT_STATE.md) · DoD audit: [`docs/V2_DOD_AUDIT.md`](V2_DOD_AUDIT.md)

### In the harness repo (sibling checkout, symlinked at `./patty-code`)

- Harness security/government baseline: `patty-code/docs/GongCode_Master_Plan.md`
- Harness provider abstraction (legacy OpenAI-compatible path): `patty-code/internal/provider/`

> The original references to `patty-code-pccp/` described a worktree layout that predates the
> current sibling-repo arrangement; harness internals move independently of this doc.

### External (PRD §Appendix G)

- vLLM / SGLang serving documentation; SPIFFE/SPIRE; Sigstore/Cosign; NVIDIA Confidential Computing.
- 한국 개인정보 보호법 + Enforcement Decree; 인공지능 발전과 신뢰 기반 조성 등에 관한 기본법 + Enforcement Decree; ISMS-P criteria.

---

## 12. Document notes

- This master plan is a **navigation document**, not a spec. The PRDs and the protocol spec are the source of truth for any decision this doc summarises. When in doubt, the PRD wins.
- This doc will be revised in place each time **scope, repo layout, phase order, or the first build slice** changes. Smaller details are tracked in tickets.
- Last full revision: 2026-08-21 — aligned to PRD v2, real repo layout, and the shipped implementation. Point-in-time status belongs in `docs/CURRENT_STATE.md`, not here.
