# Patty Code Control Plane — Master Plan

**Repo:** `patrickrho-patty/pccp` (this repo)
**Status:** Draft v1 — derived from the frozen PRDs and harness inspection on 2026-08-11
**Owner:** PCCP team
**Audience:** Anyone implementing, reviewing, or operating PCCP

---

## 1. What we're building

**Patty Code Control Plane (PCCP)** is the governance, identity, security, provenance, communication, and operational backbone of the Patty Code product line. It is the enterprise control plane that pairs with the **Patty Code Harness** to turn AI-assisted software development into something a regulated organisation can actually buy.

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
| **Individual / Public** | Patty cloud | Service-internal CP; no admin console |
| **Enterprise** | Patty SaaS, customer private cloud, or on-prem | Full admin console, organisation hierarchy |
| **Government / Sovereign** | On-prem, closed network, air-gapped | Same core, stricter policy defaults, offline updates, local KMS/HSM/PKI |

The three profiles share one codebase, one protocol, one provenance schema, one policy engine. Differences are **policy defaults and deployment topology**, not feature forks.

---

## 2. Where things live

```
pccp/                                    ← this repo (the control plane product + protocol)
├── docs/
│   ├── MASTER_PLAN.md                   ← you are here
│   └── plans/
│       ├── Patty_Code_Control_Plane_PRD_v1.md    ← the product PRD
│       └── PAPER/
│           ├── PAPER_Comprehensive_PRD_v1.0.md   ← the protocol's PRD
│           ├── PAPER_Protocol_Specification_v1.0.md  ← the wire protocol
│           └── PAPER_arXiv_Paper_v1.0.md         ← the research manuscript
├── src/                                 ← (TBD) control plane implementation
├── relay/                               ← (TBD) PAPER Relay data plane
├── pia/                                 ← (TBD) Patty Inference Agent
└── patty-code-pccp/                     ← .gitignored: worktree of the harness repo
    └── docs/GongCode_Master_Plan.md     ← harness's existing master plan (security/govt baseline)
```

### Repo responsibilities

| Repo | Owns | Does not own |
|---|---|---|
| **`pccp`** (this) | Control Plane, PAPER Relay, PIA, protocol spec, schema, conformance suite, public docs | The Harness binary itself |
| **`patty-code`** (via `patty-code-pccp/` worktree) | The Harness — terminal/IDE agent, the thing developers actually run | Inline policy enforcement at the Relay level (the Harness enforces at the application boundary) |

The boundary is intentional: PCCP is authoritative. The Harness is an enrolled peer that asks for permission, never grants it.

---

## 3. The three components and how they fit

```
┌──────────────────────────────────────────────────────────────────┐
│                  Patty Code Control Plane (PCCP)                 │
│                                                                  │
│   identity / policy / registry / audit / provenance / admin     │
│                  / comms / work intelligence                     │
│                                                                  │
│   ┌────────────────────────────────────────────────────────┐     │
│   │              PAPER Relay (data plane)                 │     │
│   │   inline auth · lease · policy · DLP · verdict · evi  │     │
│   └────────────────────────────────────────────────────────┘     │
│                  ▲                          ▲                    │
│      PAPER       │                          │  PAPER             │
│   (QUIC/TCP)     │                          │                    │
└──────────────────┼──────────────────────────┼────────────────────┘
                   │                          │
                   │                          ▼
            ┌──────┴───────┐         ┌─────────────────┐
            │   Harness    │         │      PIA        │ ← only PAPER peer
            │  (HARNESS    │         │   (INFERENCE)   │    allowed to call
            │   profile)   │         │                 │    vLLM / SGLang
            └──────────────┘         └────────┬────────┘
            developer machine                │  localhost / unix socket
                                             ▼
                                       vLLM / SGLang
                                       Patty-signed PMP
```

The Harness talks **only** to the Relay. vLLM/SGLang is **never** reachable from the Harness network. Raw model-serving ports are an internal detail of PIA.

### Component summaries

- **Control Plane (PCCP proper).** Authoritative for orgs, users, harness identities, projects, repositories, model registry, endpoint registry, policy packs, approvals, provenance store, evidence, audit, billing, admin, comms. Not in the hot token path.
- **PAPER Relay.** Horizontally scalable data plane. Authenticates peers, validates capability leases, binds policy epochs, performs DLP/injection/security checks inline, routes to enrolled PIA, emits evidence receipts. May be decomposed into connection/inspection/routing/evidence services as long as external protocol behaviour remains conformant.
- **PIA (Patty Inference Agent).** A small, signed, registered INFERENCE peer. Stands between the Relay and vLLM/SGLang. Holds a workload identity, verifies the PMP at startup, holds an `EndpointLease`, and re-attests periodically. The only thing the Harness sees as a model endpoint.
- **Patty Code Harness.** The developer-facing terminal/IDE agent. Already exists as `patty-code-pccp` (worktree). Adds PAPER enrolment, lease-aware sessions, and evidence emission on top of its existing capability/evidence/control machinery.

The full protocol surface is defined in [`docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md`](plans/PAPER/PAPER_Protocol_Specification_v1.0.md). The protocol's own PRD is [`docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md`](plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md). The research narrative is [`docs/plans/PAPER/PAPER_arXiv_Paper_v1.0.md`](plans/PAPER/PAPER_arXiv_Paper_v1.0.md).

---

## 4. Current state (as of 2026-08-11)

### What exists

| Component | Status | Evidence |
|---|---|---|
| Product PRD (PCCP) | v1.0 draft, all 55 sections + 8 appendices | `docs/plans/Patty_Code_Control_Plane_PRD_v1.md` |
| Protocol PRD (PAPER) | v1.0, 67 sections + 8 appendices | `docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md` |
| Protocol specification | v1.0, normative Peer Profiles, Transport, Capability Leases, Policy Epochs, Relay Verdicts, Provenance Spine, Evidence Receipts | `docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md` |
| Research manuscript | v1.0, 5 design contributions, 6 research questions, threat model, security boundary | `docs/plans/PAPER/PAPER_arXiv_Paper_v1.0.md` |
| Harness (worktree) | `patty-code-pccp/`, Go 1.25, 85 internal packages, ~hundreds of test files. Existing `capability`, `evidence`, `control`, `remote`, `provider/openai` packages are a strong substrate. | `patty-code-pccp/internal/` |
| Harness master plan (security/govt baseline) | `patty-code-pccp/docs/GongCode_Master_Plan.md` (3,401 lines, 30 sections) | referenced as the security/government baseline by PRD §Appendix F |

### What does not exist yet

- **No PCCP source code.** `src/`, `relay/`, `pia/` directories are not yet created.
- **No PAPER protocol implementation.** The Harness today talks to OpenAI-compatible endpoints via `internal/provider/openai`; the model resolution path is `patty.example.toml → default_model = "patty/medium" → https://omni.agents.patty.io/v1`.
- **No Control Plane admin console.**
- **No enrolment / lease / attestation infrastructure.**
- **No PIA.** vLLM (or `omni.agents.patty.io`) is the only reachable model endpoint today.
- **No PAPER reference implementation, conformance suite, or test vectors.**

The current Harness has many of the *concepts* already (capability ledger, approval gates, session leases, evidence, projection, control) — but they are local to the harness and not yet wired to a control plane over a governed protocol. The integration job is to make those local concepts part of a *live protocol exchange* with PCCP.

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

The PRD §48 defines a 6-phase, 18-month roadmap spanning trust foundation → enterprise fleet → provenance/comms → work intelligence → sovereign hardening → scale. Each phase ends in a **gate**: a deployable, demonstrable, testable milestone that proves the phase's claims.

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
| **PAPER protocol reference impl** | `paper-go` (or chosen language) — framing, transport, leases, policy epochs, evidence, conformance suite | Inbound `HELLO` from Harness accepted; `ActionEnvelope` parsed |
| **PAPER Relay** | AuthN, lease validation, policy epoch binding, DLP, routing, verdicts, evidence | One Relay rejecting an endpoint without a valid `EndpointLease` |
| **PIA** | `EndpointLease` consumer, PMP verification, local-serving bridge, attestation re-issue | One PIA in front of the dev vLLM, enrolled + attested |
| **Model registry & PMP** | `PattyModelPackage` schema, signing, registry, content addressing | One signed PMP for `Patty-KoCoder-v1` |
| **Control Plane core** | Org/User/Harness/Project/Repository/Session/Action APIs, audit, admin shell | Minimal Control UI showing the first end-to-end flow |
| **Provenance** | `ChangeSet`, `ProvenanceSpan`, content-addressed DAG, Evidence Receipts | One `ChangeSet` with full user/Harness/model/commit lineage |
| **Harness integration** | PAPER enrolment, lease-aware session, evidence emission in `patty-code-pccp` | Harness talks to PCCP dev instance using PAPER; OpenAI-compatible path remains as dev fallback only |
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
7. **Harness dual-stack.** How long do we keep the OpenAI-compatible path in `patty-code-pccp` after PAPER is live? (Affects the harness integration plan.)
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
5. **Conformance is part of the protocol.** PAPER comes with a conformance suite. Reference implementation must pass it. Independent implementations must be able to pass it.
6. **Open-source boundary is enforced.** What is open stays open; what is proprietary stays proprietary. PRD §9.9 is the canonical statement of the trust boundary.
7. **Harness changes ship via the worktree.** `patty-code-pccp/` is a worktree of the patty-code repo. Harness commits land there and are pushed to that repo, not to `pccp`. This repo holds the PCCP contract; the harness holds the harness wiring.
8. **No phantom compliance.** The product does not claim "CSAP compliant" or "KISA certified" or "ISMS-P compliant" merely because a feature exists. Maps and evidence are the product; the certification is the customer's process.

---

## 11. References

### In this repo

- Product PRD: [`docs/plans/Patty_Code_Control_Plane_PRD_v1.md`](plans/Patty_Code_Control_Plane_PRD_v1.md)
- Protocol PRD: [`docs/plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md`](plans/PAPER/PAPER_Comprehensive_PRD_v1.0.md)
- Protocol specification: [`docs/plans/PAPER/PAPER_Protocol_Specification_v1.0.md`](plans/PAPER/PAPER_Protocol_Specification_v1.0.md)
- Research manuscript: [`docs/plans/PAPER/PAPER_arXiv_Paper_v1.0.md`](plans/PAPER/PAPER_arXiv_Paper_v1.0.md)

### In the harness repo (worktree)

- Harness security/government baseline: `patty-code-pccp/docs/GongCode_Master_Plan.md`
- Harness capability substrate: `patty-code-pccp/internal/capability/`
- Harness evidence substrate: `patty-code-pccp/internal/evidence/`
- Harness control substrate: `patty-code-pccp/internal/control/`
- Harness provider abstraction (today's OpenAI-compatible path): `patty-code-pccp/internal/provider/`

### External (PRD §Appendix G)

- vLLM / SGLang serving documentation; SPIFFE/SPIRE; Sigstore/Cosign; NVIDIA Confidential Computing.
- 한국 개인정보 보호법 + Enforcement Decree; 인공지능 발전과 신뢰 기반 조성 등에 관한 기본법 + Enforcement Decree; ISMS-P criteria.

---

## 12. Document notes

- This master plan is a **navigation document**, not a spec. The PRDs and the protocol spec are the source of truth for any decision this doc summarises. When in doubt, the PRD wins.
- This doc will be revised in place each time **scope, repo layout, phase order, or the first build slice** changes. Smaller details are tracked in tickets.
- The next concrete update lands after Phase 0 ships, when the team has real evidence to back the "current state" table.
