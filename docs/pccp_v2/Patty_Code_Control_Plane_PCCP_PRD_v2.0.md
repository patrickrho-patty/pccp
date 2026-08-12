# Patty Code Control Plane (PCCP)
## Unified Public Cloud, Enterprise & Government Product Requirements Document — v2.0

**Document status:** Migration and Expansion PRD — v2.0  
**Supersedes:** `Patty_Code_Control_Plane_PRD_v1.md` where requirements conflict  
**Working product name:** **Patty Code Control Plane (PCCP)**  
**Protocol baseline:** **PAPER — Patty AI Provenance & Enforcement Relay**  
**Primary market:** Republic of Korea; global use supported  
**Profiles:** Patty Public Cloud, Enterprise, Government / Sovereign  
**Prepared:** 2026-08-12  
**Architecture principle:** **One PCCP kernel, one PAPER protocol family, profile-specific modules and policy — no separate product forks**  
**Migration context:** PCCP already exists from v1 implementation. This document specifies required modifications, additive modules, compatibility constraints, and migration gates.

---

## Document Intent

PCCP v1 established a strong Enterprise/Government control plane with user and Harness identity, trusted PIA model endpoints, governance, security, Git-aware provenance, communications, Work Intelligence, usage, evidence, and sovereign deployment.

PCCP v2 keeps that foundation but makes four material changes:

1. **Patty Public Cloud becomes a first-class PCCP deployment profile**, not a lightweight account/session service.
2. **PCCP becomes the sole authority for model discovery and model capability announcement.** The official Harness contains no authoritative local model list and no supported arbitrary OpenAI/Anthropic-compatible provider configuration.
3. **PAPER becomes the only supported Patty Code Harness service protocol.** Admin/web/integration APIs may still use ordinary HTTP/gRPC, but Harness model traffic does not silently downgrade to generic provider APIs.
4. **PAPER's AI semantic layer must be expanded to cover the modern agentic capabilities required by coding Harnesses**, including rich tool calling, structured output, multimodality, cache accounting, context management, model capability negotiation, and resumable/streaming lifecycle semantics.

The rest of v1 remains authoritative unless amended below.

---

## Contents

- [0. Executive Decision Summary](#0-executive-decision-summary)
- [1A. PCCP v1 Gap Review and Required Migration Directives](#1a-pccp-v1-gap-review-and-required-migration-directives)
- [1. Product Family, Kernel, Module, and Deployment Strategy](#1-product-family-kernel-module-and-deployment-strategy)
- [2. Vision, Positioning and Product Promise](#2-vision-positioning-and-product-promise)
- [3. Foundational Product Principles](#3-foundational-product-principles)
- [4. Goals and Non-Goals](#4-goals-and-non-goals)
- [5. Target Customers and Personas](#5-target-customers-and-personas)
- [6. Information Architecture — Admin and Operations Surfaces](#6-information-architecture-admin-and-operations-surfaces)
- [7. Overview Dashboard Requirements](#7-overview-dashboard-requirements)
- [8. Core Identity, Account, Authentication, and Harness Model](#8-core-identity-account-authentication-and-harness-model)
- [9. Trusted Model and Endpoint Architecture — Critical Requirement](#9-trusted-model-and-endpoint-architecture-critical-requirement)
- [10. Gateway, Relay, Routing, and Hot-Path Requirements](#10-gateway-relay-routing-and-hot-path-requirements)
- [10A. Server-Authoritative Model Catalog and Model Announcements](#10a-server-authoritative-model-catalog-and-model-announcements)
- [10B. PAPER AI Semantic Contract — Required Capability Expansion](#10b-paper-ai-semantic-contract-required-capability-expansion)
- [10C. Patty Public Cloud: Subscription, Fair Use, Abuse, Capacity, and SRE](#10c-patty-public-cloud-subscription-fair-use-abuse-capacity-and-sre)
- [11. Model Registry, Catalog, Package, and Lifecycle](#11-model-registry-catalog-package-and-lifecycle)
- [12. Organization, Tenancy and Korean Enterprise Hierarchy](#12-organization-tenancy-and-korean-enterprise-hierarchy)
- [13. Authorization and Policy Hierarchy](#13-authorization-and-policy-hierarchy)
- [14. Live Harness and Session Operations](#14-live-harness-and-session-operations)
- [15. Security Operations and Assurance Controls](#15-security-operations-and-assurance-controls)
- [16. Prompt, Context, Data-Loss, and Injection Governance](#16-prompt-context-data-loss-and-injection-governance)
- [17. Tools, MCP, Commands, Network, and Secret Brokering](#17-tools-mcp-commands-network-and-secret-brokering)
- [18. Git/SCM as a First-Class Control-Plane Subsystem](#18-gitscm-as-a-first-class-control-plane-subsystem)
- [19. Line-Level Human/AI Provenance](#19-line-level-humanai-provenance)
- [20. Change Impact Intelligence](#20-change-impact-intelligence)
- [21. Enterprise Communications Hub](#21-enterprise-communications-hub)
- [22. Broadcast, Emergency, and Administrative Messaging](#22-broadcast-emergency-and-administrative-messaging)
- [23. Managed File Transfer and Secure Handoff](#23-managed-file-transfer-and-secure-handoff)
- [24. Work Intelligence: Engineering and AI-Use Analytics](#24-work-intelligence-engineering-and-ai-use-analytics)
- [25. Work Intelligence Rubric and Scorecards](#25-work-intelligence-rubric-and-scorecards)
- [26. Employment-Decision Guardrails and Review Workflow](#26-employment-decision-guardrails-and-review-workflow)
- [27. Privacy, Administrative Visibility, and Content Access](#27-privacy-administrative-visibility-and-content-access)
- [28. Engineering, Adoption, and Executive Analytics](#28-engineering-adoption-and-executive-analytics)
- [29. Usage, Entitlements, Subscription, Fair Use, Billing, and Chargeback](#29-usage-entitlements-subscription-fair-use-billing-and-chargeback)
- [30. Model and GPU Operations](#30-model-and-gpu-operations)
- [31. Runtime and Sandbox Control](#31-runtime-and-sandbox-control)
- [32. Enterprise Integration Requirements](#32-enterprise-integration-requirements)
- [33. Korean Enterprise-Specific Differentiators](#33-korean-enterprise-specific-differentiators)
- [34. Deployment Architecture and Profiles](#34-deployment-architecture-and-profiles)
- [35. Security Architecture and Threat Model](#35-security-architecture-and-threat-model)
- [36. Cryptography and Key Management](#36-cryptography-and-key-management)
- [37. Data Architecture](#37-data-architecture)
- [38. Protocol and API Boundary Requirements](#38-protocol-and-api-boundary-requirements)
- [39. Event Model and Event Topics](#39-event-model-and-event-topics)
- [40. Audit, Evidence, Retention, and Legal Hold](#40-audit-evidence-retention-and-legal-hold)
- [41. Korean Governance and Compliance Packs](#41-korean-governance-and-compliance-packs)
- [42. Open Source Strategy and Trust Boundary](#42-open-source-strategy-and-trust-boundary)
- [43. Non-Functional Requirements](#43-non-functional-requirements)
- [44. Korean-First UX and Administration](#44-korean-first-ux-and-administration)
- [45. Reporting and Scheduled Outputs](#45-reporting-and-scheduled-outputs)
- [46. Product Administration, Configuration, and Change Management](#46-product-administration-configuration-and-change-management)
- [47. Public Onboarding, Enterprise Rollout, and Migration](#47-public-onboarding-enterprise-rollout-and-migration)
- [48. PCCP v2 Migration and Expansion Roadmap](#48-pccp-v2-migration-and-expansion-roadmap)
- [49. Cross-Product Acceptance Criteria](#49-cross-product-acceptance-criteria)
- [50. Product KPIs](#50-product-kpis)
- [51. Key Risks and Mitigations](#51-key-risks-and-mitigations)
- [52. Explicit Non-Goals for Initial v2 Releases](#52-explicit-non-goals-for-initial-v2-releases)
- [53. Open Product and Tuning Decisions](#53-open-product-and-tuning-decisions)
- [54. Definition of Done](#54-definition-of-done)
- [Appendix A. PAPER Model Routing and Endpoint Trust — Reference Design](#appendix-a-paper-model-routing-and-endpoint-trust-reference-design)
- [Appendix B. Provenance Data Model — Reference](#appendix-b-provenance-data-model-reference)
- [Appendix C. Work Intelligence Example Scorecard](#appendix-c-work-intelligence-example-scorecard)
- [Appendix D. Administrative Permission Matrix — Example](#appendix-d-administrative-permission-matrix-example)
- [Appendix E. Profile and Module Matrix](#appendix-e-profile-and-module-matrix)
- [Appendix F. Relationship to the GongCode Master Plan](#appendix-f-relationship-to-the-gongcode-master-plan)
- [Appendix G. External Technical and Product Baseline for v2 Design Review](#appendix-g-external-technical-and-product-baseline-for-v2-design-review)
- [Appendix H. Immediate PCCP v2 Modification Slice](#appendix-h-immediate-pccp-v2-modification-slice)

---


# 0. Executive Decision Summary

Patty Code Control Plane (PCCP) v2 is an **evolution of the already-built PCCP v1 architecture**, not a replacement product. The principal change is that PCCP is promoted from an enterprise/government control plane with a lightweight individual mode into the **single shared service kernel that powers Patty Public Cloud, Enterprise, and Government/Sovereign deployments**.

PCCP remains more than an LLM gateway. The gateway is still a necessary data-plane subsystem, but the shared kernel now owns the entire authenticated service relationship among:

```text
Account / Organization
        ↓
User Identity
        ↓
Harness Identity
        ↓
Subscription / Entitlement
        ↓
PAPER Working Session
        ↓
Model Catalog + Capability Lease
        ↓
PAPER Relay / Admission / Governance
        ↓
PIA / Trusted Endpoint
        ↓
Patty Model Package / GPU
        ↓
Usage + Security + Provenance + Evidence
```

The core v2 decision is:

> **One PCCP kernel; different capability modules, policy packs, deployment topology, data collection, and administrative surfaces for Public, Enterprise, and Government. No product forks and no long-lived edition branches.**

PCCP v2 must support two very different operating pressures without splitting the product:

1. **Patty Public Cloud:** very large user population, subscription OAuth, consumer account and Harness lifecycle, fair-use scheduling, abuse prevention, Trust & Safety, payments, support, GPU capacity, and real-time SRE.
2. **Enterprise / Government:** organization governance, repositories, security/DLP, line-level provenance, communications, Work Intelligence, audit, on-prem/private GPU, and sovereign deployment.

Both share the same kernel primitives:

- `User`
- `HarnessInstance`
- `HarnessCredential`
- `WorkingSession`
- `Entitlement`
- `CapabilityLease`
- `ModelCatalog`
- `ModelDescriptor`
- `ModelPackage`
- `InferenceEndpoint`
- `EndpointLease`
- `PolicyEpoch`
- `GovernedExchange`
- `RelayVerdict`
- `UsageRecord`
- `SecurityFinding`
- `AuditEvent`
- `PAPER` protocol contracts

## 0.1 Model discovery becomes server authoritative

PCCP v2 introduces a non-negotiable model-discovery rule:

> **The Patty Code Harness does not contain a compiled, persistent, or user-editable list of models or providers. PCCP is the authoritative source of every model the Harness is allowed to display or select.**

The official Harness must not include user-configurable:

- OpenAI `base_url`,
- Anthropic `base_url`,
- generic provider URLs,
- OpenAI-compatible adapters,
- Anthropic-compatible adapters,
- arbitrary API keys for model inference,
- raw vLLM/SGLang endpoints,
- local aliases that can redirect a Patty logical model to a different service.

After PAPER authentication, PCCP supplies an entitlement- and policy-filtered **Model Catalog Snapshot**. It may later push **Model Catalog Delta/Announcement** events when a model is added, removed, degraded, recalled, becomes unavailable to the user's plan, or changes capabilities.

The Harness selects a PCCP-issued `catalog_model_id`, not an arbitrary provider/model string.

For Enterprise/Government, a customer may eventually run a Patty-certified third-party base model, but it must still become an enrolled `ModelPackage` behind PIA and appear through PCCP's model catalog. The Harness never receives a generic provider endpoint.

## 0.2 PAPER is the sole Harness service protocol

The v1 PRD predated completion of the PAPER design and still contained HTTP/JSON/WebSocket-style Harness gateway language. PCCP v2 supersedes that design:

> **All supported Harness↔PCCP service communication uses PAPER. There is no native OpenAI, Anthropic, REST, SSE, WebSocket, or generic LLM-protocol fallback from the official Harness.**

PCCP administrative and integration APIs may remain HTTP/JSON/gRPC where appropriate. PIA may translate PAPER internally into a local vLLM/SGLang API. Neither fact creates a supported alternative Harness inference route.

Because the Harness and PCCP are open source, a person with source code can create a modified client that speaks other protocols. PCCP does not claim the impossible property that open-source software cannot be modified. Instead, the official trust model is:

- the official Harness contains only PAPER for Patty service inference;
- PCCP accepts only enrolled PAPER peer identities for Harness sessions;
- generic API clients cannot consume a Patty subscription merely by possessing an OAuth token;
- Patty Public subscriptions are bound to users, registered Harness identities, session entitlements, and short-lived capacity authorization;
- enterprise network controls may additionally prevent external AI egress where the customer requires that stronger guarantee.

## 0.3 PAPER AI semantics must be a functional superset, not a lowest-common-denominator API

PAPER must not lose capabilities merely because Patty rejects OpenAI/Anthropic wire compatibility.

PCCP v2 therefore requires a provider-neutral AI semantic contract that can represent the important capabilities exposed by modern model protocols and cross-provider SDKs, including:

- ordered instruction/message roles;
- text, image, audio, file/document, and extensible content parts;
- streaming text and structured-content deltas;
- client-executed tools/functions;
- server/Relay/Runtime-executed tools;
- MCP tools;
- strict JSON-schema tool arguments;
- dynamic/deferred tools and tool search;
- `none`, `auto`, `required`, exact-tool, and allowed-tool-set selection;
- parallel tool calls;
- explicit tool approvals;
- tool-call argument streaming;
- multimodal tool results;
- structured JSON outputs;
- refusal and safety output;
- citations/sources/annotations;
- reasoning-effort controls and safe reasoning metadata without exposing hidden chain-of-thought;
- context-window and output-token limits;
- continuation and session state;
- context compaction/management;
- prompt/prefix caching controls and cache usage;
- uncached input, cache-write, cache-read, output, reasoning, and total usage accounting;
- granular finish/stop reasons;
- background/long-running exchange state where a model or task requires it;
- cancellation and resumability;
- provider/model-specific capability extensions where no stable common semantic exists.

PAPER is intentionally **not** a field-for-field copy of OpenAI Responses or Anthropic Messages. It defines stable semantic primitives with capability negotiation. PCCP/PIA adapters translate those primitives to whatever serving engine or model implementation is used.

## 0.4 Public Cloud is not lightweight

PCCP v1 described Public/Individual operation as a lightweight account/session/usage path. That is no longer accurate.

Patty Public Cloud is expected to become one of the most operationally demanding PCCP deployments because it must handle:

- hundreds of thousands of registered users;
- potentially tens of thousands of simultaneous Harness connections;
- large numbers of concurrent subagents;
- consumer OAuth and account recovery;
- subscription state and payment failures;
- multi-Harness registration;
- account-sharing detection;
- service abuse and exploit detection;
- dynamic fair use;
- hot-path admission without database round trips;
- regional PAPER Relay fleets;
- GPU saturation and queue management;
- model rollout/recall;
- 24×7 SRE and Trust & Safety operations.

Enterprise has deeper organizational control. Government has stronger sovereignty. Public has the hardest scale/fairness/abuse problem.

## 0.5 User, Harness, session, and workload are separate quota dimensions

For the Public profile, the commercial principal is the **user subscription**, not an API key.

PCCP v2 must distinguish:

```text
User Account
  ├─ Subscription / Entitlement
  ├─ Harness A
  │    ├─ Working Session 1
  │    │    ├─ Agent Work Slot
  │    │    └─ Subagent Work Slot
  │    └─ Working Session 2
  ├─ Harness B
  └─ Harness C
```

The initial recommended Public defaults are configurable, not protocol constants:

- maximum registered Harnesses: **3**
- maximum concurrently active Harnesses: **2–3**
- normal active agent work slots per account: **5**
- heavy/long-context slots: **1–2**
- bounded burst capacity for short subagent fan-out

PCCP must rate-limit **work**, not raw TCP/QUIC connection count. PAPER multiplexes many logical activities over one connection, so connection count is not an appropriate economic control.

## 0.6 Fair use is capacity allocation, not punishment for power users

"Unlimited" cannot mean physically infinite GPU allocation. Patty may market generous/unlimited individual interactive use subject to fair-use and anti-abuse controls, while PCCP internally manages scarce GPU resources through:

- active agent slots;
- weighted fair scheduling;
- model-specific capacity;
- estimated Compute Load Units;
- uncached prefill cost;
- decode cost;
- long-context/KV residency;
- cache discount;
- background-task weighting;
- account burst state;
- dynamic cluster pressure.

High legitimate usage is not itself abuse.

PCCP must separate:

1. **Capacity/Fairness**
2. **Account Integrity / account sharing**
3. **Trust & Safety / Terms enforcement**
4. **Platform Security / hostile behavior**

A user can be `HIGH_USAGE` while remaining `NORMAL` in every abuse/security dimension.

## 0.7 Control-plane and Relay data-plane separation is mandatory

PCCP's web/API control services must not forward every token.

```text
                      PCCP CONTROL PLANE
         Identity / Subscription / Catalog / Policy
         Harness Registry / Entitlement / Risk / Admin
                           │
                     signed hot state
                           │
                           ▼
                     PAPER DATA PLANE
              ┌───────────┼───────────┐
              ▼           ▼           ▼
         PAPER Relay  PAPER Relay  PAPER Relay
              │           │           │
              └────── Model Scheduler ┘
                           │
                    PIA Endpoint Fleet
                           │
                          GPU
```

Relays perform the hot path using locally cached signed/versioned state. Durable events flow asynchronously into metering, billing, analytics, Trust & Safety, security, audit, and support systems.

## 0.8 Definition of PCCP v2 in one sentence

> **PCCP v2 is the shared identity, entitlement, model-discovery, PAPER communication, governance, scheduling, security, provenance, metering, and operational control kernel that safely connects Patty Code users and organizations to approved Patty AI infrastructure across public cloud, enterprise, and sovereign environments.**

---

# 1A. PCCP v1 Gap Review and Required Migration Directives

This document is an update to an already implemented PCCP derived from PRD v1. The following gaps are migration requirements, not greenfield suggestions.

| Gap in v1/current implementation | Why it matters | v2 directive |
|---|---|---|
| Public profile described as lightweight | Public may be highest-scale PCCP deployment | Promote Public Cloud to first-class production profile |
| Harness model selection not fully server-authoritative | Stale/hard-coded lists allow inconsistency and invite provider abstraction | Add `paper.models/1`; PCCP is sole model catalog authority |
| v1 Harness path still referenced `/v1/gateway/responses`, WebSocket/gRPC concepts | Contradicts PAPER decision | Official Harness service traffic becomes PAPER-only |
| PAPER v1 AI surface is narrower than current mature model protocols | Tools/multimodal/structured output/caching can become ad hoc | Add provider-neutral PAPER AI Semantic Contract |
| Model package identity and user-visible model identity conflated | Prevents transparent package rollout/canary | Separate Catalog Model / PMP / Endpoint IDs |
| Gateway and CP could be interpreted as one traffic process | Public scale would overload administrative control services | Separate PAPER Relay data plane from CP authority |
| Rate limits focus on requests/tokens/concurrency | Coding agents multiplex subagents and long context | Introduce work slots + Compute Load Units + hierarchical limits |
| Network connection count could be mistaken for agent concurrency | PAPER multiplexes many operations on one connection | Rate-limit semantic workloads, not sockets |
| Public subscription OAuth not first-class | Public users need login, renewal, recovery and plan state | Add OAuth/OIDC + subscription module + Account Portal |
| Harness registry mostly enterprise-oriented | Public sharing/credential abuse requires per-install identity | Make Harness enrollment mandatory for Public |
| Account sharing/compromise not modeled separately | IP/location heuristics are noisy | Add Account Integrity state machine and graduated response |
| Trust & Safety and infrastructure security not clearly separated | Harmful-use review differs from attacks against Patty | Separate T&S / Platform Security / Capacity / Integrity |
| Public SRE/capacity operation under-specified | Hundreds of thousands of accounts require live operations | Add public operations console, SLOs and alerting |
| Public provenance default could inherit enterprise depth | Consumer privacy/retention risk | Add `operational` trace profile with minimal content retention |
| No dynamic Harness-facing model announcement | Model rollout/recall requires client refresh | Catalog epochs, snapshot/delta/announce/withdraw |
| No capability compatibility negotiation per model | New model features can break old Harnesses | ModelDescriptor declares capabilities and minimum client semantics |
| No explicit compatibility test against modern Responses/Messages features | PAPER may regress functionality users expect | Maintain protocol capability coverage matrix and conformance fixtures |
| Generic local model config may still exist in existing Harness | Violates Patty-only service design | Remove/disable provider/base URL/model-list config in official Harness |
| Existing v1 roadmap is greenfield | PCCP already exists | Replace with phased migration roadmap and coexistence plan |

## Migration rule

When v1 and v2 requirements conflict, this document is authoritative.

Existing Enterprise/Government functionality that does not conflict should be preserved and regression-tested rather than rewritten.

## Compatibility principle

The migration should prefer:
- additive schemas;
- feature flags;
- dual-read/dual-write where necessary;
- canary Relays;
- minimum-Harness-version gates;
- safe deprecation;

over a big-bang replacement.

The final state must remove the obsolete Harness generic API path.

---

# 1. Product Family, Kernel, Module, and Deployment Strategy

## 1.1 One kernel, three operating profiles

PCCP v2 SHALL be maintained as one product kernel.

| Profile | Primary operator | Primary pressure | Model location | Admin surface |
|---|---|---|---|---|
| **Patty Public Cloud** | Patty | Scale, subscriptions, fair use, abuse, SRE | Patty cloud | Patty internal ops + user account portal |
| **Enterprise** | Patty and/or customer | Organizational control, security, provenance | Patty cloud, customer GPU, hybrid | Customer Control + Patty service ops where managed |
| **Government / Sovereign** | Customer/government | Isolation, offline operation, evidence, strict policy | customer/government infrastructure | Customer-local Control |

A profile changes:

- enabled modules;
- default policy;
- deployment topology;
- trust anchors;
- data collection;
- retention;
- administrative permissions;
- licensing;
- operational integrations.

It does **not** change fundamental identities, PAPER message semantics, model-package identity, event schemas, or provenance identifiers.

## 1.2 PCCP Kernel

The kernel is the minimum coherent system required in every profile:

### Identity Kernel
- user/account identity abstraction;
- Harness identity and enrollment;
- service and Relay identity;
- PIA/inference identity;
- session binding;
- revocation.

### PAPER Kernel
- peer authentication;
- Working Sessions;
- extension negotiation;
- Capability Leases;
- Policy Epochs;
- Governed Exchanges;
- Relay Verdicts;
- evidence correlation.

### Model Kernel
- Model Registry;
- Model Catalog;
- capability descriptors;
- Patty Model Packages;
- PIA endpoints;
- Endpoint Leases;
- model health;
- model recall.

### Entitlement Kernel
- subscription/license/organization entitlement;
- model eligibility;
- Harness limits;
- concurrency/work-slot limits;
- feature availability;
- capacity rights.

### Policy Kernel
- common decision contract;
- profile-specific policy packs;
- deny/allow/transform/approval;
- versioned policy epochs.

### Gateway/Scheduler Kernel
- admission;
- routing;
- queueing;
- capacity control;
- endpoint health/fallback;
- cancellation;
- usage normalization.

### Metering Kernel
- input tokens;
- cache write/read;
- output tokens;
- reasoning usage where available;
- tool usage;
- GPU/runtime estimates and measurements;
- request/session/account correlation.

### Event Kernel
- durable normalized event envelope;
- asynchronous fan-out;
- security/billing/audit/analytics consumers;
- idempotency/deduplication;
- schema registry.

### Module Registry
- enabled capabilities;
- hot-path hooks;
- asynchronous processors;
- administrative modules;
- connector capabilities;
- edition/profile declaration.

## 1.3 Profile modules

### Public modules

```text
public.oauth
public.account
public.subscription
public.payment
public.harness-management
public.fair-use
public.capacity
public.account-integrity
public.trust-safety
public.support
public.sre
public.notifications
public.public-analytics
```

### Enterprise modules

```text
enterprise.organization
enterprise.sso-scim
enterprise.delegated-admin
enterprise.projects-repositories
enterprise.security-governance
enterprise.full-provenance
enterprise.comms
enterprise.file-transfer
enterprise.work-intelligence
enterprise.chargeback
enterprise.integrations
enterprise.reporting
```

### Sovereign modules/profiles

```text
sovereign.local-identity
sovereign.local-pki
sovereign.offline-entitlement
sovereign.offline-update
sovereign.local-kms-hsm
sovereign.airgap
sovereign.local-telemetry
sovereign.crypto-profile
sovereign.strict-policy
```

Government is primarily Enterprise + sovereign deployment constraints.

## 1.4 Module implementation model

PCCP must avoid a plugin architecture that allows arbitrary extension code to destabilize the inference hot path.

Three extension classes are defined:

### Class A — in-process hot-path modules

Use for:
- authentication cache;
- entitlement check;
- lease validation;
- scheduler/admission;
- endpoint routing;
- lightweight policy;
- usage counters.

Requirements:
- statically linked or tightly versioned interfaces;
- bounded execution time;
- no arbitrary network I/O in request critical section unless explicitly designed;
- panic/fault containment;
- metrics;
- failure-mode declaration.

### Class B — service-bound synchronous modules

Use when action must block on a heavier service:
- DLP;
- policy approval;
- selected security classification;
- external entitlement authority in enterprise;
- specialized routing.

Requirements:
- timeout;
- circuit breaker;
- fail mode;
- cache strategy;
- degraded behavior.

### Class C — asynchronous event consumers

Use for:
- analytics;
- billing reconciliation;
- Work Intelligence;
- support aggregation;
- historical abuse detection;
- notification;
- reporting;
- long-term provenance enrichment.

These components must not block token streaming merely because analytics is delayed.

## 1.5 DeploymentProfile object

The exact schema is implementation-defined, but the conceptual object is:

```yaml
profile_id: patty-public-cloud-v1
kernel_version: 2
required_modules:
  - identity
  - paper
  - model_registry
  - model_catalog
  - entitlement
  - gateway
  - scheduler
  - metering
  - event_spine
modules:
  public.oauth: enabled
  public.subscription: enabled
  public.fair_use: enabled
  public.account_integrity: enabled
  public.trust_safety: enabled
  enterprise.full_provenance: disabled
  enterprise.work_intelligence: disabled
policy_pack: patty-public-v1
trace_profile: operational
deployment:
  control_plane: patty-cloud
  relay: regional
  inference: patty-cloud
```

Profiles and high-impact module changes should be signed/versioned, testable, and auditable.

## 1.6 No code branches as editions

The following strategy is prohibited:

```text
main
enterprise-branch
government-branch
public-saas-branch
```

Long-running edition branches inevitably produce:
- security patch divergence;
- protocol divergence;
- feature drift;
- duplicated bugs;
- incompatible schemas;
- release overhead.

Feature gating belongs in profile manifests, policy, and modules.

## 1.7 Open-source versus Patty-operated services

The open-source PCCP project may contain the core kernel, interfaces, reference deployment, and self-hostable modules. Patty may operate proprietary commercial services around:

- consumer subscription/payment integration;
- Patty model hosting;
- Trust & Safety operations;
- production capacity control;
- managed infrastructure;
- support;
- proprietary model packages/evaluations;
- commercial analytics.

The open-source boundary must not force a second core architecture.

---

# 2. Vision, Positioning and Product Promise

## 2.1 Unified vision

PCCP shall become the **shared operating kernel for every Patty Code deployment**:

- Patty's own public subscription service;
- customer-facing Enterprise Cloud;
- Enterprise Private / On-Prem;
- Government / Sovereign / Air-Gapped.

The product must solve different operational problems in each profile without changing its fundamental identity model, model trust model, PAPER protocol, event spine, policy runtime, or inference control path.

The common vision is:

> **Every Patty Code interaction reaches AI infrastructure through an authenticated, policy-aware, observable control plane that knows who is using the Harness, what that Harness is entitled to do, which model capabilities are actually available, where inference is running, what resources are being consumed, and what evidence must be retained.**

For Public Cloud, the emphasis is **availability, fair access, subscription integrity, abuse resistance, operational safety, and GPU efficiency**.

For Enterprise, the emphasis expands to **organizational governance, data controls, Git/SCM provenance, security, communications, administrative control, and engineering intelligence**.

For Government/Sovereign, the same controls operate under **local trust anchors, local model/GPU infrastructure, offline operation, restrictive policy defaults, and stronger deployment assurance**.

## 2.2 Product identity

PCCP is not primarily an LLM gateway. A high-performance model gateway is a mandatory subsystem because every model interaction requires:

- authentication;
- entitlement;
- admission;
- model selection;
- routing;
- streaming;
- accounting;
- resource scheduling;
- safety/governance enforcement;
- observability.

But the product is broader than routing.

PCCP should be understood as the shared system for:

1. identity and Harness fleet management;
2. subscription and entitlement authority;
3. authoritative model discovery and capability announcement;
4. PAPER session and exchange control;
5. governed inference routing;
6. GPU/model admission and capacity control;
7. public-service fair use and account integrity;
8. enterprise policy/security/provenance;
9. communications and operational broadcasts;
10. evidence, audit, metering, and analytics.

## 2.3 Public product promise

For the individual subscriber:

> **Sign in once, enroll Patty Code, and use the models and capabilities included with your subscription. Patty Code automatically knows what models are available; there are no API keys, provider base URLs, or third-party endpoint configuration to manage.**

The user experience should feel simpler than a conventional LLM API client:

```text
Install Patty Code
      ↓
Sign in
      ↓
Subscription + Harness verified
      ↓
PCCP announces models/capabilities
      ↓
User selects a permitted model or accepts default
      ↓
PAPER session
      ↓
Patty model infrastructure
```

A public subscriber is purchasing **Patty Code service access**, not a transferable model API credential.

## 2.4 Enterprise product promise

For enterprise customers:

> **Developers retain a fast Korean-first terminal/IDE agent experience while the organization receives policy-governed control from user and Harness identity through model execution to source-code provenance and engineering outcomes.**

Enterprise administrators may additionally govern:

- organization and project membership;
- repositories and branches;
- context disclosure;
- tools/MCP;
- network and secrets;
- model and execution zones;
- approvals;
- communications;
- evidence and retention;
- optional Work Intelligence.

## 2.5 Government / Sovereign promise

> **The same Patty Code and PCCP architecture can operate with no mandatory public-cloud dependency, using local identity, local PAPER Relays, approved local PIA/model endpoints, customer-controlled keys, and offline update/entitlement processes.**

Government is a deployment and security profile of the same architecture, not a separate product lineage.

## 2.6 Korean-market positioning

Suggested enterprise positioning:

> **기업이 통제할 수 있는 AI 개발 환경**  
> 개발자는 AI로 더 빠르게 개발하고, 조직은 사용자·하네스·모델·소스코드·보안·비용·출처를 하나의 Control Plane에서 관리합니다.

Alternative:

> **AI 개발의 모든 행위를 보이게 하고, 통제하고, 증명합니다.**

Suggested public positioning should remain simpler:

> **한국 개발자를 위한 AI 코딩 환경 — 로그인하면 바로 사용할 수 있고, 모델과 인프라는 Patty가 관리합니다.**

## 2.7 What PCCP must not become

PCCP must not become:

- four separately maintained products;
- an arbitrary OpenAI/Anthropic-compatible proxy;
- a BYOK consumer gateway;
- a generic provider marketplace;
- a client-configurable base-URL router;
- a system where the Harness ships a hard-coded list of model IDs;
- a system where model availability is inferred from a provider's `/models` endpoint;
- a product whose "unlimited" plan is implemented as absence of resource controls;
- a single opaque abuse score that conflates high usage, account sharing, harmful content, and attacks on Patty;
- a public surveillance/provenance system that retains complete user code merely because Enterprise PCCP can;
- a government fork with its own protocol and schemas.

## 2.8 Architectural product promise

A single sentence should remain true in every profile:

> **PCCP is the source of authority for who may use Patty Code, which capabilities and models they may use, how those exchanges reach trusted inference infrastructure, and what control/evidence obligations apply.**

---

# 3. Foundational Product Principles

1. **One kernel, multiple profiles.** Public, Enterprise, and Sovereign share core identities, PAPER contracts, model registry, scheduler primitives, event schemas, and trust model.
2. **The Harness has no provider configuration.** Official Patty Code does not accept arbitrary inference URLs, base URLs, provider API keys, or provider compatibility adapters.
3. **PCCP is the model-catalog authority.** Every displayable/selectable model is announced by PCCP after authentication and filtered through entitlement and policy.
4. **A model name is presentation, not identity.** Model authorization uses catalog IDs, PMP identity, package digests, endpoint leases, and policy—not strings.
5. **PAPER is the only supported Harness service protocol.** No transparent fallback to HTTP/OpenAI/Anthropic/generic LLM APIs.
6. **PAPER must preserve modern model capabilities.** Security-driven protocol independence cannot justify a lower-quality agent experience.
7. **Capabilities are negotiated, not guessed.** Model, Harness, Relay, PIA, and tool capabilities are machine-readable and versioned.
8. **Gateway is subordinate to service control.** Routing is infrastructure beneath identity, entitlement, governance, model trust, and operations.
9. **Control plane is not token data plane.** Relays scale independently and consume signed/cached state.
10. **The Public commercial principal is the user.** Subscription benefits attach to an individual identity, then to enrolled Harnesses and Working Sessions—not transferable API keys.
11. **Harness identity is independent from user identity.** A valid login does not by itself authorize any arbitrary process to consume the subscription.
12. **Connection count is not concurrency.** PAPER multiplexes; PCCP controls active work slots and resource consumption.
13. **Unlimited means generous interactive service subject to physical capacity and anti-abuse controls.** Internal fairness must be resource-aware.
14. **High usage is not automatically abuse.** Capacity, account integrity, Trust & Safety, and platform security are separate states.
15. **IP is evidence, not identity.** Location/IP/ASN changes contribute to risk but do not automatically prove account sharing.
16. **Abuse response is graduated.** Step-up auth and suspicious-Harness revocation precede permanent account actions unless evidence indicates a clear platform attack.
17. **No trust by conversational fingerprint.** Behavioral signatures are anomaly signals, not model or Harness identity.
18. **No trust by generic endpoint compatibility.** Raw vLLM/SGLang/OpenAI-compatible servers are never direct Harness targets.
19. **PIA remains the serving boundary.** Generic serving engines stay behind the enrolled inference identity.
20. **Tools are authority-bearing operations.** Tool calls, shell actions, patch/edit operations, MCP, network and files pass through explicit capability and policy controls.
21. **Server-side and client-side tool semantics are explicit.** PAPER distinguishes where a tool executes and who authorizes it.
22. **Structured output is first-class.** JSON-schema output and strict tool schema behavior are part of the model capability contract.
23. **Multimodal content is typed.** Text, image, audio, file/document, tool calls/results, sources, and generated artifacts are not flattened into one string.
24. **Reasoning controls do not imply chain-of-thought exposure.** PAPER may carry effort configuration, reasoning-token usage, opaque state, or safe summaries; hidden chain-of-thought is not a product requirement.
25. **Caching is an observable resource.** Cache read/write usage and policy are metered independently from uncached input.
26. **Provenance is profile-dependent.** Enterprise may collect line-level code provenance; Public defaults to operational/security evidence with minimized raw content.
27. **Every protected action is correlated.** User, Harness, session, model, endpoint, policy, tool, and usage identifiers join across the event spine.
28. **Hot-path state is memory/cache friendly.** Database access is not required per token or per small streaming chunk.
29. **State has epochs and leases.** Entitlement, model catalog, policy and capacity state are versioned and expire or invalidate predictably.
30. **Fair scheduling is user-aware.** One power user's subagents must not monopolize the entire public GPU fleet.
31. **Model fallback is policy-preserving.** No fallback changes model class, residency, assurance, or entitlement without an allowed catalog rule.
32. **Public model changes are pushable.** Model release, degradation, recall, or plan eligibility can be announced without shipping a new Harness binary.
33. **Old Harnesses fail explicitly.** If a model requires an unsupported PAPER capability, PCCP marks it incompatible or requires upgrade rather than silently degrading.
34. **Administrative APIs are not inference APIs.** HTTP/JSON may remain for Control UI and integrations while Harness inference remains PAPER-only.
35. **Security does not rely on source secrecy.** PCCP, PAPER and Harness can be open source.
36. **Open-source modification limits claims.** A modified Harness can add arbitrary protocols; official Patty service access still requires enrolled PAPER identity and entitlement.
37. **Enterprise external-AI prohibition requires network controls for a malicious local administrator/user.** PAPER eliminates the official client pathway but cannot control every program on the device.
38. **Git remains first-class for enterprise provenance.**
39. **Models remain exact artifacts for trusted serving.**
40. **Korean-first operation remains a product requirement across Public, Enterprise, and Sovereign profiles.**

---

# 4. Goals and Non-Goals

## 4.1 Shared product goals

### G1 — One architecture at every customer scale
Run the same conceptual PCCP kernel for a single Public subscriber, a 50-person SME, a 20,000-developer group, and a sovereign government deployment.

### G2 — Server-authoritative model discovery
A Harness must only see/select models delivered by PCCP for its current user, entitlement, policy, Harness capability, region, deployment, and service state.

### G3 — PAPER-only official Harness inference
Eliminate provider endpoint configuration from the official Harness and make PAPER the only supported Patty service inference protocol.

### G4 — Preserve full agent capability
PAPER/PCCP must support a modern coding-agent experience comparable in semantic richness to leading model APIs without copying their wire protocols.

### G5 — Scale Public service predictably
Support hundreds of thousands of accounts and very large concurrent Harness/session populations by scaling Relay, scheduler, cache, event, and PIA data planes independently.

### G6 — Prevent subscription benefits from becoming a resellable API
A Public subscription must be difficult to turn into a shared proxy, SaaS backend, or transferable generic API credential.

### G7 — Provide generous subagent concurrency
Ordinary users should be able to run multiple simultaneous agents without accidentally triggering abuse controls.

### G8 — Allocate GPU fairly
Schedule by account/work class/resource demand, not only FIFO request arrival.

### G9 — Detect abuse without punishing legitimate security work or travel
Use multi-signal risk, history, account state, and human review where appropriate.

### G10 — Keep Enterprise/Government differentiation
Adding Public Cloud must not dilute deep organization governance, security, provenance, communications, and sovereign functionality.

## 4.2 Public Cloud goals

- OAuth/OIDC login suitable for terminal and IDE.
- Browser and device-code style login options.
- Subscription-gated Harness use.
- Account portal for Harness management.
- 2–3 simultaneously active Harnesses and ~5 normal agent work slots as configurable starting policy.
- High-volume regional Relay fleet.
- transparent queue/capacity status.
- real-time input/output/cache token metrics.
- GPU utilization and queue health.
- internal SRE alerting to Slack/email/on-call.
- account-sharing risk detection.
- platform attack detection.
- ToS/safety review workflows.
- dynamic model announcements and recalls.
- no generic API keys required for Harness inference.

## 4.3 Enterprise/Government goals retained from v1

- unified organization/Harness/session administration;
- deep Korean enterprise hierarchy;
- security/DLP/policy enforcement;
- full model/endpoint trust;
- Git-linked provenance;
- line/span AI attribution;
- communications and broadcasts;
- Work Intelligence with employment-decision safeguards;
- private/on-prem/air-gap deployment;
- offline evidence and update flows.

## 4.4 Protocol goals

PCCP requires PAPER to support:

- capability-negotiated model discovery;
- typed multimodal input/output;
- streaming;
- function/tool calling;
- parallel calls;
- strict schemas;
- dynamic tools;
- approvals;
- MCP;
- local shell/patch/editor operations;
- structured output;
- model reasoning controls;
- context/caching controls;
- usage details;
- stop/retry/resume semantics;
- model status changes;
- provenance/evidence.

## 4.5 Security goals

- A generic OpenAI/Anthropic client cannot directly consume a Patty Public subscription.
- A valid user login from an unregistered Harness cannot begin a protected PAPER AI session.
- A registered Harness cannot select a model absent from its current PCCP catalog.
- A user cannot supply a provider URL as model identity.
- A normal vLLM endpoint cannot become trusted by claiming a Patty model name.
- Account-sharing signals can trigger reauthentication/restriction without automatically banning legitimate multi-device users.
- Model output cannot authorize its own tool or network access.
- Public service abuse classification does not weaken enterprise deterministic policy controls.
- Revocations and model recalls propagate within defined SLOs.

## 4.6 Non-goals

PCCP v2 will not:

- become a generic public multi-provider LLM proxy;
- expose Public subscription usage through OpenAI- or Anthropic-compatible endpoints;
- ship an environment variable to redirect Patty Code to arbitrary inference servers;
- make IP/geolocation a sole identity proof;
- promise unlimited physical GPU compute;
- use raw token count as the only rate-limit input;
- promise perfect detection of account sharing;
- ban every cybersecurity-related prompt automatically;
- expose hidden model chain-of-thought;
- implement provider-specific quirks in the Harness when they belong in PIA/model adapters;
- require Enterprise customers to use consumer payment/OAuth modules;
- require Government deployments to contact Patty cloud at runtime;
- fork PCCP into separately maintained editions.

---

# 5. Target Customers and Personas

PCCP v2 adds Public-service personas while retaining all v1 enterprise/government personas.

## 5.1 Public subscriber

Primary needs:
- install Patty Code;
- authenticate once with minimal friction;
- see only models included in the current subscription;
- run several agents/subagents concurrently;
- understand temporary capacity/limit states;
- manage registered Harnesses;
- review account sign-ins/security;
- see subscription and renewal state;
- receive model/service notices;
- get predictable recovery when login/session state expires.

The subscriber should not need to understand:
- provider API keys;
- base URLs;
- vLLM;
- PIA;
- model package hashes;
- endpoint regions unless relevant to service status.

## 5.2 Patty Public SRE / On-call

Needs:
- global/regional live traffic;
- active accounts/Harnesses/work slots;
- authentication health;
- PAPER handshake errors;
- queue depth;
- TTFT and decode latency;
- cache efficiency;
- PIA/endpoint/GPU health;
- capacity headroom;
- model rollout state;
- entitlement/payment system health;
- event/metering health;
- alert routing;
- one-click drill-down without ordinary access to raw user code/prompts.

## 5.3 Patty Trust & Safety reviewer

Needs:
- account risk cases;
- policy-triggered content/security indicators according to retention policy;
- historical violation summaries;
- compromised-account signals;
- sharing/resale signals;
- user/ Harness/session correlation;
- graduated actions;
- appeal/restore workflow;
- reason codes;
- audit history.

Trust & Safety is not identical to platform security.

## 5.4 Patty Platform Security

Needs:
- attacks against PCCP/PAPER/Relay/PIA;
- malformed protocol activity;
- credential replay;
- scanning;
- exploit attempts;
- model extraction patterns;
- unauthorized service probing;
- abnormal file/tool abuse;
- cross-tenant attacks;
- immediate credential/session/Harness block.

## 5.5 Patty Customer Support

Needs:
- account/subscription state;
- Harness inventory;
- last authentication;
- model entitlement;
- capacity/fair-use state;
- safe diagnostic logs;
- known incidents;
- ability to revoke/re-register Harnesses;
- tightly permissioned access to content only with explicit user/support flow.

## 5.6 Patty Billing/Revenue Operations

Needs:
- subscription lifecycle;
- payment state;
- plan/entitlement history;
- refunds/credits;
- promotional plans;
- usage credits if offered;
- fraud signals;
- reconciliation;
- billing event integrity.

## 5.7 Existing enterprise/government personas

### Developer
Fast Korean-first coding, predictable model choices, tool use, clear denials, collaboration, repository context.

### Tech Lead / Engineering Manager
Team outcome, quality, review, provenance, AI effectiveness, capacity.

### Project Manager / TPM
Project progress, blockers, broadcasts, adoption, capacity.

### CISO / Security
Alerts, DLP, policy, investigations, containment, model/tool governance.

### AI Governance Officer
Model approval, use cases, transparency, evidence, risk.

### Platform / GPU Operator
PCCP, Relay, PIA, model, GPU, queues, scaling, updates.

### Compliance / Privacy
Retention, PII, evidence, access history, policy mapping.

### HR / People Analytics
Separately permissioned Work Intelligence evidence.

### Executive
Aggregate adoption, risk, delivery, cost, capacity.

### Auditor
Read-only evidence and policy/provenance verification.

### Contractor / SI partner
Narrow time-bounded project access.

## 5.8 Persona separation requirements

No single Patty internal role should automatically combine:

- payment administration;
- Trust & Safety content review;
- platform root operations;
- model signing;
- evidence deletion/retention;
- customer prompt viewing.

Likewise, Enterprise GPU operators do not need prompt/source access, and Public SRE does not need ordinary raw content access.

---

# 6. Information Architecture — Admin and Operations Surfaces

PCCP v2 requires two major administrative experiences built on the same control APIs and design system:

1. **Patty Operations Console** — internal operation of the public service.
2. **Customer Control Console** — Enterprise/Government administrative product.

They are not two backends. They are role- and profile-specific views over the same kernel objects.

## 6.1 Patty Operations Console — Public Cloud

Recommended navigation:

```text
Service Overview
Live Traffic
  ├─ Active Accounts
  ├─ Harnesses
  ├─ Working Sessions
  ├─ Agent Work Slots
  ├─ Queues
  └─ PAPER Exchanges
Accounts
  ├─ Subscribers
  ├─ Authentication
  ├─ Subscription / Payment State
  ├─ Harness Registry
  ├─ Capacity / Fair Use
  ├─ Account Integrity
  ├─ Trust & Safety
  └─ Support Actions
Models
  ├─ Public Model Catalog
  ├─ Catalog Epoch / Announcements
  ├─ Patty Model Packages
  ├─ Endpoints / PIA
  ├─ Routing
  ├─ Canary / Rollout
  └─ Recall / Deprecation
Capacity
  ├─ GPU Fleet
  ├─ Model Replicas
  ├─ Admission
  ├─ Account Capacity Leases
  ├─ Queue Health
  ├─ KV / Cache
  └─ Forecast
Risk
  ├─ Account Sharing Signals
  ├─ Credential / Session Anomalies
  ├─ Platform Abuse
  ├─ Trust & Safety Cases
  └─ Enforcement History
Usage
  ├─ Input / Output / Cached Tokens
  ├─ Compute Load Units
  ├─ GPU Time
  ├─ Plan Utilization
  ├─ Cohorts
  └─ Cost
Reliability
  ├─ SLOs
  ├─ Incidents
  ├─ Alerts
  ├─ Regional Health
  └─ Dependencies
Support
  ├─ Account Timeline
  ├─ Harnesses
  ├─ Sessions
  ├─ User-Consented Diagnostics
  └─ Refund / Entitlement Escalation
System
```

Public operations views shall default to metadata and service telemetry. Raw prompt/source content is not a routine operations field.

## 6.2 Customer Control Console — Enterprise/Government

The mature Enterprise information architecture remains:

```text
Overview
Live Operations
Organization
  ├─ Affiliates / Business Units
  ├─ Users & Groups
  ├─ Harnesses
  ├─ Roles & Delegated Admin
  └─ Presence
Projects & Repositories
AI Sessions
Provenance
Security
Governance
Models & Infrastructure
Communications
Work Intelligence
Usage & Billing
Evidence & Audit
Integrations
System
```

Government/Sovereign uses the same UI with a different deployment profile, local trust sources, and stricter defaults.

## 6.3 Shared object navigation

Both consoles should use the same object identifiers wherever applicable:

- `User`
- `Account`
- `Organization`
- `Harness`
- `PAPERPeerCredential`
- `WorkingSession`
- `CapabilityLease`
- `AccountCapacityLease`
- `ModelCatalogEpoch`
- `CatalogModel`
- `ModelPackage`
- `InferenceEndpoint`
- `PAPERExchange`
- `PolicyDecision`
- `UsageRecord`
- `SecurityFinding`
- `Incident`
- `EvidenceReceipt`

This avoids building a Public analytics taxonomy unrelated to Enterprise data.

## 6.4 Global search

Public internal operators may search authorized fields by:

- account ID/email;
- Harness ID;
- Working Session;
- Exchange ID;
- source IP/ASN;
- subscription ID;
- payment/support case;
- model/catalog ID;
- endpoint/PIA;
- security/risk event;
- incident.

Enterprise search retains user/repository/branch/commit/file/symbol/provenance capabilities.

## 6.5 Privacy-aware console separation

A single employee with the `PCCPPlatformAdmin` role must not automatically acquire:

- Trust & Safety case content;
- billing/payment access;
- private prompt/source content;
- enterprise communications content;
- Work Intelligence individual data.

Separate roles and purpose-specific access remain required even inside Patty's own operations environment.

## 6.6 User-facing Account Portal

Public users require a self-service web portal for:

- subscription/plan;
- invoices/payment method through payment provider integration;
- registered Harnesses;
- first/last seen;
- approximate device/OS label;
- revoke/remove Harness;
- sign out all;
- security events;
- current active sessions;
- account recovery;
- plan usage/fair-use state at an understandable level;
- data/privacy settings;
- support.

The portal must not expose transferable model API credentials.

---

# 7. Overview Dashboard Requirements

PCCP must provide profile-specific operational dashboards over shared telemetry.

## 7.1 Patty Public Cloud command center

The Public Cloud landing page is an SRE, capacity, subscription-integrity, and service-health dashboard.

Required top-level metrics:

### Accounts
- total accounts;
- active paid subscriptions;
- trial/promotional entitlements;
- active users 1m/5m/1h/24h;
- sign-in success/failure;
- new subscriptions;
- payment/grace/suspended counts;
- account-recovery activity.

### Harnesses
- total registered Harnesses;
- online Harnesses;
- active Harnesses;
- version distribution;
- unsupported/vulnerable versions;
- enrollment/revocation rate;
- users at Harness-count limit.

### Work and traffic
- active Working Sessions;
- active agent work slots;
- heavy/long-context slots;
- background jobs;
- queued jobs;
- requests/sec;
- streaming exchanges;
- cancellations;
- resumption rate;
- QUIC/TCP ratio.

### Token and cache telemetry
- input tokens/sec;
- output tokens/sec;
- cache-read tokens/sec;
- cache-write tokens/sec;
- reasoning/hidden-compute usage where model exposes safe usage metadata;
- context size distribution;
- cache hit ratio;
- Compute Load Units/sec.

### Performance
- auth latency;
- PAPER handshake latency;
- dispatch latency;
- queue wait p50/p95/p99;
- TTFT p50/p95/p99;
- inter-token/decode latency;
- output tokens/sec by model;
- completion success/error/cancel rate.

### Model/endpoints
- catalog models available;
- active model packages;
- active PIA endpoints;
- endpoints healthy/degraded/draining;
- per-model replica count;
- endpoint error rate;
- canary traffic;
- current model defaults;
- active recalls/deprecations.

### GPU/capacity
- GPU allocated/utilized;
- VRAM;
- KV-cache utilization;
- prefill/decode utilization where available;
- queue depth;
- capacity headroom;
- admission rejects;
- reserved/emergency capacity;
- predicted exhaustion window.

### Risk
- possible account-sharing cases;
- compromised-account signals;
- credential replay;
- rapid Harness enrollment;
- platform attack attempts;
- Trust & Safety review queue;
- fair-use throttling;
- banned/suspended accounts.

### System health
- OAuth/OIDC;
- subscription/entitlement;
- Harness Registry;
- PAPER ingress;
- Relay fleet;
- capacity authority;
- model catalog;
- PIA/model plane;
- event spine;
- metering;
- payments integration;
- notification/alerting.

## 7.2 Public drill-down

Every aggregate must support drill-down by:

- region;
- model;
- model package;
- PIA pool;
- plan;
- subscriber cohort;
- Harness version;
- transport;
- queue class;
- risk state;
- time window.

Operational dashboards should not require raw prompt access.

## 7.3 Enterprise/Government dashboard

Retain the v1 consolidated NOC/SOC/engineering command center:

- registered/active users;
- Harnesses;
- sessions;
- prompt/context/model activity;
- endpoint/GPU health;
- security findings;
- policy decisions;
- provenance;
- messaging/broadcast;
- Work Intelligence;
- cost/chargeback;
- evidence pipeline.

All existing organization/project/repository filters remain.

## 7.4 Capacity interpretation

Do not page merely because GPU utilization is high.

Healthy example:

```text
GPU utilization 98%
Queue p95       120 ms
TTFT p95        1.3 s
Decode TPS      within baseline
```

This is efficient operation.

Unhealthy example:

```text
GPU utilization 84%
Queue p95       11.2 s and rising
TTFT p95        14.7 s
Decode TPS      -42% from baseline
KV eviction     rapidly increasing
```

This requires investigation despite lower utilization.

Alerts must correlate user-facing degradation with infrastructure metrics.

## 7.5 Wallboard mode

Public wallboard:
- SLO/service health;
- traffic;
- queue;
- GPU/capacity;
- endpoint health;
- high-severity incidents.

Enterprise wallboard additionally:
- security findings;
- policy blocks;
- critical broadcasts;
- organizational activity, with content redacted by default.

## 7.6 Historical comparison

Dashboards shall support:
- current vs previous period;
- release/canary annotations;
- model rollout annotations;
- capacity-change annotations;
- incidents;
- plan/policy changes;
- abnormal cohort changes.

A performance regression should be correlatable to a model package, PIA build, Relay release, Harness release, policy change, or infrastructure event.

---

# 8. Core Identity, Account, Authentication, and Harness Model

PCCP v2 separates **human account identity**, **commercial entitlement**, **Harness identity**, **PAPER peer identity**, and **Working Session authority**.

## 8.1 Shared entities

Core:
- `Account`
- `User`
- `Organization` when applicable
- `IdentityProviderBinding`
- `Subscription`
- `Entitlement`
- `Harness`
- `HarnessEnrollment`
- `PAPERPeerCredential`
- `WorkingSession`
- `CapabilityLease`
- `AccountCapacityLease`
- `AuthenticationEvent`
- `RiskState`

Enterprise adds organization/employment/device attributes without changing the core identity chain.

## 8.2 Public authentication

The public flow uses browser-based OAuth/OIDC.

Recommended native application flow:
- Authorization Code with PKCE;
- system browser;
- short-lived authorization code;
- no client secret embedded in Harness.

For headless/remote terminal environments where browser redirection is unavailable, support a device authorization flow through an external browser.

PCCP may support identity providers such as:
- Google;
- Apple;
- Kakao;
- Naver;
- email/passkey;
- Patty identity.

The exact provider set is a product decision. Provider identity is normalized into one Patty `Account`.

## 8.3 OAuth is bootstrap identity, not the model credential

The OAuth access/identity assertion proves the user logged in. It is not sent as the long-lived authorization primitive for each AI inference exchange.

Flow:

```text
User authenticates in browser
        ↓
PCCP resolves Account + Subscription
        ↓
Harness generates/loads local peer key
        ↓
Harness enrollment
        ↓
PCCP issues PAPER Peer Credential
        ↓
PAPER authentication
        ↓
User binding
        ↓
Working Session + Capability Lease
        ↓
AI exchanges
```

This separates account recovery from inference authorization and allows a single account to revoke one Harness without signing out every device.

## 8.4 Harness enrollment

Every Harness installation creates an independent `Harness` identity.

Baseline Public enrollment:
1. Harness generates an asymmetric key pair locally.
2. User completes OAuth.
3. PCCP checks account/subscription state.
4. Harness sends enrollment metadata and public key.
5. PCCP checks Harness-count and version policy.
6. PCCP creates Harness identity.
7. PCCP issues PAPER peer credential.
8. User sees the Harness in the account portal.

Enterprise enrollment may additionally require:
- SSO;
- administrator approval;
- device posture;
- MDM;
- organization-signed build;
- network-zone restrictions.

Government may use local/offline enrollment.

Hardware-backed identity is optional and is not a universal prerequisite.

## 8.5 Public Harness limits

Initial default policy should be configurable, not hard-coded into protocol:

- maximum registered Harnesses: 3;
- maximum simultaneously active Harnesses: 2–3;
- normal agent work slots: approximately 5/account;
- heavy/long-context slots: approximately 1–2/account;
- background job slots: separately configurable.

These values are launch-tuning inputs, not customer promises or protocol constants.

If registered-Harness limit is reached:
- user goes to Account Portal;
- user removes/revokes an old Harness;
- then enrolls another.

Admins/support may perform a recovery override with audit.

## 8.6 No public API-key principal

The Public subscription principal is:

```text
Account
  ↓
Subscription
  ↓
Entitlement
  ↓
Harness
  ↓
PAPER Working Session
```

Do not issue a general-purpose API key that converts an individual unlimited coding subscription into a transferable inference API.

Patty may separately operate a future developer API product, but that would have its own entitlements, contracts, pricing, credentials, and abuse model.

## 8.7 User binding and Harness sharing

One Harness can support local user switch only by creating a fresh user binding. Credentials and session state must not silently migrate between users.

Public Harness identity is associated with one Account at a time unless product policy explicitly supports family/team plans in the future.

Enterprise can authorize multiple named users on a shared managed workstation, but each Working Session remains bound to one authenticated user.

## 8.8 Authentication/session anomalies

Capture signals including:
- source IP/ASN;
- broad geolocation;
- Harness ID;
- OS/build;
- login method;
- new device/Harness;
- concurrent Harnesses;
- session count;
- token/load behavior;
- credential replay failures;
- rapid enroll/revoke cycles.

No single IP/location signal proves sharing.

## 8.9 Account and security states

Separate:

### Subscription State
- `TRIAL`
- `ACTIVE`
- `GRACE`
- `PAST_DUE`
- `CANCELLED`
- `EXPIRED`

### Account Integrity State
- `NORMAL`
- `SUSPICIOUS`
- `POSSIBLY_SHARED`
- `COMPROMISED`
- `RECOVERY_REQUIRED`

### Trust & Safety State
- `NORMAL`
- `REVIEW`
- `RESTRICTED`
- `SUSPENDED`
- `BANNED`

### Platform Security State
- `NORMAL`
- `HOSTILE`
- `BLOCKED`

### Capacity State
- `NORMAL`
- `BUSY`
- `THROTTLED`
- `QUEUED`

A user can legitimately be `Capacity=THROTTLED` while all integrity/safety/security states remain normal.

## 8.10 Enterprise identity

Preserve existing:
- OIDC;
- SAML;
- LDAP/AD;
- Entra ID;
- Google Workspace;
- SCIM;
- MFA/passkeys;
- local/government PKI.

Public auth is a new identity adapter/profile, not a replacement.

---

# 9. Trusted Model and Endpoint Architecture — Critical Requirement

The existing PIA/PMP/Endpoint Lease architecture remains a core PCCP requirement and is now also mandatory for Public Cloud.

## 9.1 Trust target

PCCP must not trust a model because:
- a model string matches;
- `/models` claims the expected name;
- an endpoint is OpenAI compatible;
- behavior appears similar;
- an administrator typed an IP/base URL;
- a bearer API key works.

The trust target is:

> **an enrolled PIA inference workload associated with an approved Patty Model Package and a current Endpoint Lease under the active deployment profile.**

## 9.2 PAPER-only data path

Updated v2 path:

```text
Patty Code Harness
        │
        │ PAPER
        ▼
PAPER Relay
        │
        │ PAPER
        ▼
Patty Inference Agent (PIA)
        │
        │ local/private adapter only
        ▼
vLLM / SGLang / approved serving engine
        │
        ▼
Patty Model Package
```

The old v1 conceptual Harness↔Gateway HTTP/gRPC path is superseded.

PCCP admin/integration APIs may use HTTP/gRPC, but the official Harness AI/service channel uses PAPER.

## 9.3 Raw engine isolation

- Serving engine is not a Harness-reachable endpoint.
- PIA owns the PAPER `INFERENCE` peer credential.
- PIA binds the current local model artifact to a signed PMP identity.
- Relays route only with a current Endpoint Lease.
- Raw OpenAI-compatible routes on vLLM/SGLang, if enabled internally, are loopback/private adapter implementation details.
- They are never announced to Harness.

## 9.4 Patty Model Package

PMP remains the cryptographic model artifact identity and includes:
- weights/shards;
- tokenizer;
- configuration;
- chat/model template;
- adapters;
- quantization artifacts/config;
- serving image/config;
- engine compatibility;
- capability/evaluation metadata;
- release state;
- signature.

PMP is **not** the same as the user-visible Catalog Model. Multiple PMP versions/endpoints may implement one stable user-visible model offering.

## 9.5 Three separate model identities

PCCP v2 distinguishes:

1. **Catalog Model ID** — what the Harness/user selects, e.g. `patty-code-fast`.
2. **Model Package ID (PMP)** — exact signed artifact/version.
3. **Inference Endpoint ID** — exact PIA deployment serving that package.

The Harness only needs the Catalog Model ID and capability descriptor.

PCCP/Relay resolves the exact PMP and endpoint.

This enables:
- canary rollout;
- package replacement;
- hotfix;
- quantization transition;
- capacity rebalance;
- transparent endpoint failover;
- recall.

without requiring a Harness model list update.

## 9.6 Endpoint assurance

Retain explicit levels:
- L1 Software Verified;
- L2 Host Attested;
- L3 Confidential/Hardware Attested.

Public Cloud may use Patty-controlled infrastructure and select the assurance level internally.
Enterprise/Government policy may require stronger levels.

## 9.7 No engine fork as primary trust boundary

Preferred order:
1. PIA/sidecar boundary;
2. serving-engine plugin/extension;
3. small upstreamable integration;
4. fork only when unavoidable.

PCCP must not couple protocol evolution to vLLM/SGLang release cadence.

## 9.8 Model recall

Recall invalidates:
- Catalog availability if appropriate;
- eligible routing;
- Endpoint Leases;
- new Capability Leases referencing the recalled model.

PAPER sends model catalog withdrawal/status announcements to connected Harnesses.

A currently selected model that is recalled must become visibly unavailable; Harness must not keep using a stale local model list.

---

# 10. Gateway, Relay, Routing, and Hot-Path Requirements

## 10.1 Revised architecture

In PCCP v2 the term **Gateway** refers to a logical function, while the high-volume inline traffic implementation is the horizontally scalable **PAPER Relay data plane**.

The administrative Control Plane does not need to copy every token.

```text
                  PCCP CONTROL PLANE
 Identity / Subscription / Policy / Catalog / Capacity / Registry
                         │
                  signed/cached state
                         │
                         ▼
                    PAPER RELAYS
 Auth → Entitlement → Policy → Capacity → Routing → Stream control
                         │
                         ▼
                       PIA
                         │
                         ▼
                       GPU
```

## 10.2 Shared request pipeline

Every AI request follows a stable kernel pipeline.

```text
1. Peer Authentication
2. User / Account / Organization Binding
3. Subscription / Entitlement
4. Harness / Session Authorization
5. Account Integrity / Platform Security
6. Policy / Governance
7. Fair-Use / Budget / Capacity
8. Model Catalog Validation
9. Route Eligibility
10. Admission / Queue
11. PIA Dispatch
12. Stream Processing
13. Usage / Metering
14. Evidence / Events / Finalization
```

Enterprise adds richer policy stages; Public uses a lighter policy pack. The pipeline contract stays the same.

## 10.3 Hot-path module contract

Each synchronous module declares:

- inputs;
- outputs;
- latency budget;
- cacheability;
- fail mode;
- event emissions;
- configuration epoch;
- whether it may transform content;
- whether it is security critical.

Example:

```yaml
module: public.entitlement
phase: pre_route
latency_budget_ms_p95: 5
cache: signed_lease
failure_mode: fail_closed_after_grace
input:
  - account
  - harness
  - session
output:
  - allowed
  - entitlement_class
  - model_scope
  - capacity_class
```

## 10.4 Module execution categories

Do not implement every feature as an arbitrary dynamically loaded in-process plugin.

Use:

### A. Statically linked hot-path modules
For:
- identity;
- entitlement;
- lease;
- catalog validation;
- admission;
- routing;
- inexpensive policy;
- metering counters.

### B. Isolated synchronous services
For:
- expensive DLP/scanning;
- risk service where necessary;
- centralized capacity authority;
- policy decisions requiring external state.

### C. Async event consumers
For:
- analytics;
- Work Intelligence;
- long-horizon abuse analysis;
- reporting;
- aggregate billing;
- support indexing.

### D. Connector/extension boundary
For:
- SIEM;
- HRIS;
- SCM;
- external scanners;
- notification providers.

A broken analytics module must not break inference.

## 10.5 Lessons adopted from Bifrost

PCCP should learn architectural patterns from Bifrost without copying its code or product principal.

Adopt:
- hot frequently needed gateway state in memory;
- hierarchical budget/limit concepts;
- routing separated from provider execution;
- governance decision before adaptive routing;
- stable pre-request / per-attempt / post-response extension phases;
- horizontally coordinated runtime state;
- health-aware and performance-aware routing;
- state synchronization independent of durable DB reads on each request.

Do not adopt as PCCP Public semantics:
- Virtual Key as the primary subscriber principal;
- user-supplied provider API keys;
- OpenAI/Anthropic compatibility headers as Harness auth;
- client-selectable provider/base URL;
- arbitrary provider registration.

## 10.6 Routing criteria

Route eligibility considers:

- Catalog Model ID;
- entitlement;
- Policy Epoch;
- data classification;
- deployment profile;
- exact approved PMP set;
- Endpoint Lease;
- endpoint assurance;
- region/zone;
- context requirement;
- feature requirements (tools, images, structured output, etc.);
- PIA/model health;
- queue;
- KV/cache constraints;
- latency;
- capacity class;
- canary assignment;
- account/organization priority;
- incident/maintenance state.

Performance scoring applies only after eligibility filtering.

## 10.7 Fallback

Fallback is server-side and policy aware.

PCCP may:
- select another PIA for same PMP;
- select another approved PMP implementing same Catalog Model;
- select an explicitly declared fallback Catalog Model when entitlement/policy allows.

Fallback must never silently:
- switch to an unannounced user-visible model semantics if that would change capabilities materially;
- route outside residency policy;
- route to generic OpenAI/Anthropic endpoint;
- bypass endpoint assurance;
- downgrade structured-output/tool guarantees without an explicit compatibility rule.

If a fallback changes a capability relevant to the active exchange, PCCP must either:
- reject;
- renegotiate/announce;
- or return a structured capability-change error.

## 10.8 State distribution

Relays need locally cached:
- trust bundles/revocation;
- active Harness peer credentials/status;
- account/subscription entitlement leases;
- Policy Epochs;
- model catalog snapshots;
- Catalog Model → eligible PMP mapping;
- Endpoint Leases;
- routing weights/health;
- Account Capacity Leases;
- plan limit profiles.

The relational DB is not queried synchronously for every token or tool delta.

## 10.9 Streaming

PAPER Relay shall:
- preserve ordered stream semantics;
- enforce cancellation;
- meter input/output/cache usage;
- correlate tool calls/results;
- handle flow control/backpressure;
- prioritize interactive output;
- preserve final usage and completion reason;
- support session/exchange resumption according to PAPER.

Heavy stream processing should not run expensive analytics synchronously per token.

## 10.10 Caching

Cache policy differentiates:
- public Patty-owned system prefixes;
- user/session prompt cache;
- organization/project cache;
- model KV/prefix cache;
- result/semantic cache where product permits.

Cache must be:
- tenant/account isolated for private content;
- policy aware;
- model/package aware;
- purgable;
- metered;
- observable;
- included in usage accounting.

Cross-user reuse of private prompt/cache content is prohibited unless the cached content is Patty-owned immutable public/system material with explicit safe classification.

## 10.11 Admin paths vs Harness path

PCCP may continue exposing HTTP/JSON or gRPC for:
- admin UI;
- account web portal;
- integrations;
- webhooks;
- internal management APIs.

Those are not model invocation alternatives for Patty Code Harness.

The Harness AI/control/collaboration service path is PAPER.

---

# 10A. Server-Authoritative Model Catalog and Model Announcements

This is a new mandatory PCCP/PAPER subsystem.

## 10A.1 Core requirement

**The official Patty Code Harness must not own the authoritative model list.**

The Harness must not:
- ship a compiled list of service model IDs;
- accept arbitrary model IDs as a generic provider string;
- persist a user-editable provider model catalog;
- let users configure `base_url`, OpenAI-compatible URLs, Anthropic-compatible URLs, or raw vLLM/SGLang endpoints for the Patty subscription service;
- probe `/v1/models`;
- infer available models from a third-party API;
- map model aliases locally without a server-issued catalog rule.

PCCP is the authority.

## 10A.2 Catalog lifecycle

```text
Harness authenticates over PAPER
        ↓
PCCP resolves Account/User/Harness
        ↓
Entitlement + organization/project policy
        ↓
PCCP computes effective model catalog
        ↓
MODEL_CATALOG_SNAPSHOT
        ↓
Harness renders model selector
        ↓
User selects Catalog Model ID
        ↓
AI_OPEN(model_id, catalog_epoch)
        ↓
Relay validates catalog epoch + entitlement
        ↓
PCCP resolves PMP + PIA endpoint
```

## 10A.3 Catalog is effective, not global

Two users connected at the same moment may legitimately receive different catalogs because of:
- subscription tier;
- experiment/canary;
- geography;
- Enterprise organization policy;
- project/data classification;
- Government deployment;
- minimum Harness version;
- capacity or maintenance state;
- model retirement;
- user feature access.

Therefore PCCP exposes the **effective model catalog** for the authenticated context, not a global static array.

## 10A.4 Proposed PAPER extension: `paper.models/1`

Required message types:

- `MODEL_CATALOG_REQUEST`
- `MODEL_CATALOG_SNAPSHOT`
- `MODEL_CATALOG_DELTA`
- `MODEL_ANNOUNCE`
- `MODEL_WITHDRAW`
- `MODEL_DEFAULT_CHANGED`
- `MODEL_AVAILABILITY`
- `MODEL_CAPABILITY_CHANGED`
- `MODEL_UPGRADE_REQUIRED`
- `CATALOG_ACK`

PAPER Specification must be amended separately; this PRD defines the product requirement.

## 10A.5 Catalog Epoch

Every snapshot has:
- `catalog_epoch`;
- generation timestamp;
- user/account/organization scope digest;
- entitlement revision;
- policy epoch;
- minimum validity;
- descriptors;
- signature/authenticated transport binding as specified by PAPER.

`AI_OPEN` references `catalog_epoch`.

If the requested model is not in the effective catalog for that epoch, Relay rejects before model allocation.

## 10A.6 ModelDescriptor

A ModelDescriptor is the Harness-facing model contract.

Conceptual schema:

```yaml
catalog_model_id: patty-code-standard
display_name:
  ko: Patty Code Standard
  en: Patty Code Standard
description:
  ko: ...
  en: ...
family: code
release_channel: stable
availability: available
default_rank: 10

capabilities:
  input:
    text: true
    image: true
    audio: false
    file: true
    pdf: true
  output:
    text: true
    structured: true
  tools:
    client_tools: true
    runtime_tools: true
    server_tools: true
    mcp: true
    parallel_calls: true
    strict_schema: true
    dynamic_discovery: true
    approval: true
  reasoning:
    supported: true
    effort_levels: [low, medium, high]
    opaque_continuation_state: true
  context_management:
    compaction: true
    tool_result_clearing: true
  cache:
    prompt_cache: true
    cache_usage_reporting: true
  citations_sources: true
  streaming: true
  resumable_background: true

limits:
  max_input_tokens: 262144
  max_output_tokens: 32768
  max_tools: ...
  max_parallel_tool_calls: ...

entitlement:
  class: unlimited-developer
  UI_label: Included

client:
  min_harness_version: ...
  min_paper_ai_version: 2
  required_extensions:
    - paper.ai/2
    - paper.tools/2

lifecycle:
  announced_at: ...
  deprecated_at: null
  retire_after: null
```

The descriptor contains **capabilities**, not endpoint addresses.

## 10A.7 Catalog Model vs Model Package

Catalog Model is a stable service offer. A Catalog Model may map to:
- one production PMP;
- multiple quantized PMP variants;
- canary PMP;
- region-specific PMP;
- an emergency rollback PMP.

Harness need not know which exact package served an ordinary user-facing request unless diagnostics/provenance policy exposes it afterward.

Enterprise provenance can display exact PMP and endpoint.

## 10A.8 Live announcements

PCCP can push:
- newly available model;
- model maintenance;
- temporary capacity degradation;
- default model change;
- deprecation;
- recall;
- minimum-client requirement.

Harness model selector updates without restart.

A user who is actively using a withdrawn model receives a structured event:
- finish current exchange;
- stop immediately;
- migrate after current step;
- select replacement;
depending on recall policy.

## 10A.9 Offline behavior

Public:
- no new inference from stale persistent catalog when PCCP cannot authenticate/authorize;
- active already-authorized exchange may follow lease/resumption policy;
- Harness may display last-known names for UI history but cannot use them as authorization.

Enterprise:
- signed catalog snapshots can be cached for bounded outage if policy allows.

Government:
- local PCCP is catalog authority;
- catalogs may be updated through signed offline bundles;
- no Patty cloud call is necessary.

## 10A.10 User preference

Harness may persist:
- user's preferred Catalog Model ID;
- preferred reasoning/effort setting;
- preferred automatic/default selection.

It may not persist:
- endpoint;
- provider;
- base URL;
- raw API key.

If preferred model is no longer announced, Harness falls back only according to server-provided replacement/default policy or asks the user.

## 10A.11 Acceptance

1. Delete every local model-list file and the selector still works after PCCP connection.
2. Add a model in PCCP; eligible online Harness sees it without software release.
3. Withdraw a model; new requests from stale Harness are rejected.
4. A user types a fake model ID; Relay rejects it.
5. A user configures `api.openai.com`; official Harness has no supported inference path to use it.
6. A vLLM endpoint claiming `patty-code-standard` does not become routable.
7. An old Harness lacking a required capability does not see/activate incompatible model features.

---

# 10B. PAPER AI Semantic Contract — Required Capability Expansion

PAPER must not copy OpenAI Responses or Anthropic Messages wire formats. However, replacing those protocols is only viable if PAPER can represent the important capabilities expected from a modern agentic model interface.

This section defines the **coverage requirement** for a PAPER AI-layer revision.

## 10B.1 Design principle

Create a provider-neutral **PAPER AI Semantic IR**.

PIA adapters translate:

```text
PAPER AI Semantic IR
      ↕
model-specific serving template / vLLM / SGLang / future runtime
```

The Harness never speaks the internal serving protocol.

The IR should be informed by mature public API semantics and cross-provider libraries, but the object model and wire encoding remain PAPER-native.

## 10B.2 Input item model

An AI exchange must support ordered input items/content parts:

- user message;
- system/deployment instruction;
- developer/organization instruction;
- assistant/model prior output;
- text;
- image;
- audio where supported;
- PDF/document;
- file/blob reference;
- repository/context reference;
- tool call;
- tool result;
- structured data;
- model continuation state;
- citation/source attachment.

Instruction precedence must be explicit and governed. Organization/system instructions cannot be overwritten by untrusted repository content.

## 10B.3 Output item model

Streaming/final output may contain:

- text;
- structured object;
- refusal/safety response;
- reasoning summary or opaque reasoning continuation state where model supports it;
- tool call;
- server-tool event;
- citations;
- sources;
- generated file/artifact;
- code patch;
- usage;
- warnings;
- model-status event.

The protocol must not require disclosure of hidden chain-of-thought.

## 10B.4 Tool descriptor

Every tool descriptor requires:

- stable tool ID;
- name;
- description;
- input schema;
- optional output schema;
- strict-schema support;
- execution placement;
- risk class;
- approval policy;
- streaming-argument support;
- multimodal-result support;
- idempotency class;
- timeout/resource class;
- provenance requirements.

Execution placement enum:

- `HARNESS`
- `RUNTIME`
- `RELAY_SERVICE`
- `PIA_SERVER`
- `MCP`
- `EXTERNAL_APPROVED_SERVICE`

## 10B.5 Tool choice

PAPER must represent at least:
- automatic;
- none;
- required / one-or-more;
- force a specific tool;
- allowed subset;
- dynamic/deferred tool set.

The current model descriptor announces which modes are supported.

## 10B.6 Parallel tool calls

Support:
- multiple tool calls in one model step;
- stable call IDs;
- independent approvals;
- partial results;
- ordered or unordered completion declaration;
- aggregate continuation.

A tool call cannot execute merely because the model emitted it. PCCP policy remains authoritative.

## 10B.7 Tool-call streaming

Models may stream tool arguments incrementally.

PAPER needs events equivalent in semantics to:
- tool call created;
- argument delta;
- arguments complete;
- approval requested;
- execution started;
- output delta;
- completed/failed/cancelled.

The Harness must be able to render a tool being constructed without executing incomplete arguments.

## 10B.8 Client tools and server tools

PAPER shall support both:

### Client/runtime-executed
Model proposes; Harness/Runtime executes after authorization.

Examples:
- shell;
- patch/editor;
- build/test;
- Git operations.

### Server-executed
PCCP/PIA-approved infrastructure executes the tool without sending execution authority to the Harness.

Examples:
- centrally governed search;
- file retrieval;
- code execution service;
- internal knowledge service.

Server tools still generate visible tool-use/result/provenance events.

## 10B.9 MCP integration

PAPER remains the authoritative Harness protocol even when a tool is backed by MCP.

Flow:

```text
Model
  ↓ tool proposal
PAPER
  ↓ policy/approval
PCCP Tool Broker
  ↓
MCP server
  ↓
PAPER normalized ToolResult
```

For approved local MCP:
- Harness may host connector process;
- tool identity and capability must still be registered/governed;
- MCP does not become an alternative model transport.

Required semantics:
- list/discover tools;
- tool schema;
- approval;
- call;
- result;
- error;
- server identity;
- dynamic tool availability.

## 10B.10 Built-in coding tool classes

Define PAPER-native semantic classes for common coding actions:

- `shell`
- `apply_patch` / structured file edit
- `file_read`
- `file_search`
- `code_search`
- `code_execution`
- `git`
- `test`
- `package`
- `network_fetch`
- `web_search`
- `mcp`
- `custom_function`

These are protocol concepts. A model-specific adapter can map its native tool representation to them.

## 10B.11 Structured outputs

Support:
- JSON Schema or registered schema dialect;
- strict/constrained output flag;
- schema identifier/digest;
- model capability validation;
- structured-output stream;
- schema failure status.

PCCP must reject a request for strict structured output if the effective Catalog Model does not advertise it.

## 10B.12 Reasoning/thinking controls

Represent only safe/configurable semantics:
- reasoning enabled/disabled where model supports;
- effort levels;
- token/budget controls if model supports;
- user-visible reasoning summary where provided;
- opaque signed/encrypted continuation state if needed by the model/runtime.

Do not standardize or demand raw hidden chain-of-thought.

## 10B.13 Conversation/continuation

Support:
- explicit prior input/output items;
- continuation token/state;
- parent Exchange ID;
- multi-step agent loop;
- server-managed conversation state where enabled;
- compaction checkpoint;
- resumption.

A model implementation may remain stateless; these are optional announced capabilities.

## 10B.14 Context management

Modern long-running coding agents need:
- context-window tracking;
- compaction;
- clear/prune old tool results;
- summarize selected history;
- retain pinned context;
- context overflow warning;
- server-requested compaction event.

All context transformations should produce provenance/diagnostic metadata sufficient to explain what was retained/removed without exposing hidden reasoning.

## 10B.15 Prompt/prefix caching

PAPER usage model must distinguish:
- uncached input tokens;
- cache-write/creation tokens;
- cache-read tokens;
- output tokens;
- optional reasoning/model-specific billed units.

PAPER must carry:
- cache eligibility/directive;
- cache scope;
- cache TTL class;
- cache hit/write usage;
- cache invalidation reason where useful.

Cache semantics are model capability, not Harness assumption.

## 10B.16 Multimodality

The semantic layer should be extensible for:
- text;
- image;
- audio;
- document/PDF;
- file;
- future media types.

A ModelDescriptor announces accepted input and output content classes.

Large binaries should use PAPER file/content references instead of bloating control frames.

## 10B.17 Citations and sources

Represent:
- source object;
- source URL/document/file reference;
- cited span;
- title/metadata;
- citation position/range in output;
- source trust/provenance.

Citations are distinct from Git/code provenance but can link into it.

## 10B.18 Stop and completion reasons

Define normalized finish reasons at minimum:

- `COMPLETED`
- `MAX_OUTPUT`
- `STOP_SEQUENCE`
- `TOOL_CALL`
- `PAUSE_CONTINUE`
- `REFUSAL`
- `CONTEXT_LIMIT`
- `CANCELLED`
- `TIMEOUT`
- `SAFETY_BLOCK`
- `ERROR`
- `MODEL_UNAVAILABLE`

Adapters may preserve a `native_finish_reason` for diagnostics, but Harness behavior uses normalized semantics.

## 10B.19 Usage object

Required normalized fields:
- input tokens;
- output tokens;
- cache read;
- cache write;
- reasoning/compute tokens if safely exposed;
- tool-specific usage;
- GPU/compute time where PCCP records it;
- model package/catalog model;
- queue/TTFT/latency.

Provider-specific usage fields may exist under an extension namespace but cannot replace normalized fields.

## 10B.20 Streaming state machine

PAPER AI must expose deterministic lifecycle events such as:

```text
AI_ACCEPTED
OUTPUT_ITEM_STARTED
TEXT_DELTA*
REASONING_SUMMARY_DELTA*
TOOL_CALL_STARTED
TOOL_ARGUMENT_DELTA*
TOOL_CALL_READY
TOOL_RESULT*
OUTPUT_ITEM_COMPLETED
USAGE_UPDATE*
AI_COMPLETED
```

Unknown non-critical future event types should be ignorable/forward-compatible per PAPER extension rules.

## 10B.21 Background/long-running operations

Support model capabilities for:
- queued;
- in-progress;
- paused;
- resumable;
- completed;
- failed;
- cancelled.

Background capability must remain bounded by subscription/organization policy and capacity leases.

## 10B.22 Capability coverage matrix

For every production PAPER release, maintain a tested matrix against capability classes found in contemporary mature APIs/libraries:

| Capability | PAPER requirement |
|---|---|
| text streaming | mandatory |
| multimodal input | optional announced |
| files/PDF | optional announced |
| client tool calls | mandatory AI profile |
| strict tool schemas | optional announced |
| tool choice | mandatory |
| parallel tools | optional announced |
| streaming tool args | optional announced |
| server tools | optional announced |
| MCP | optional extension |
| structured output | optional announced |
| reasoning effort | optional announced |
| prompt caching/accounting | mandatory usage support when model provides |
| citations/sources | optional announced |
| context management/compaction | optional announced |
| background/resume | optional announced |
| normalized finish reasons | mandatory |
| rich usage accounting | mandatory |

## 10B.23 Source-design rule

OpenAI Responses, Anthropic Messages/Models, and cross-provider libraries are **coverage references**, not wire-format templates.

PAPER should copy neither:
- HTTP endpoint shapes;
- provider object names;
- provider IDs;
- provider-specific headers.

Instead it should provide an expressive semantic layer whose adapters can support comparable functionality.

---

# 10C. Patty Public Cloud: Subscription, Fair Use, Abuse, Capacity, and SRE

Public Cloud is not a lightweight PCCP deployment. It has shallower organizational governance but much greater Internet scale and abuse pressure.

## 10C.1 Public request principal

Primary principal:

```text
Account
 + active Subscription/Entitlement
 + enrolled Harness
 + PAPER peer credential
 + user binding
 + Working Session
```

A static API key is not the principal.

## 10C.2 Subscription entitlement

Entitlement contains:
- plan;
- active/grace/expiry;
- allowed Catalog Model classes;
- capability flags;
- maximum registered Harnesses;
- active Harness policy;
- normal work-slot policy;
- heavy/background slots;
- context class;
- priority/fairness weight;
- optional feature experiments.

Relays consume short-lived signed/cached entitlement state.

## 10C.3 Agent Work Slot

The semantic concurrency unit is an **Agent Work Slot**, not network connection.

A single PAPER connection may carry many lanes/subagents.

Work slots are allocated per:
- account;
- optionally Harness;
- class.

Classes:
- `INTERACTIVE`
- `SUBAGENT`
- `HEAVY_CONTEXT`
- `BACKGROUND`
- `SYSTEM_RESERVED`

One agent may consume more than one capacity weight depending on workload.

## 10C.4 Compute Load Units

PCCP should maintain an internal normalized load estimate.

Conceptually:

```text
CLU =
  uncached_prefill_weight
+ decode_weight
+ KV_residency_weight
+ context_length_weight
+ model_multiplier
+ background/tool_compute
- eligible_cache_discount
```

Exact coefficients are internal, empirically calibrated, and versioned.

Purpose:
- capacity fairness;
- scheduling;
- forecasting;
- abuse analysis.

Do not market CLU as a precise physical token/GPU accounting unit until validated.

## 10C.5 Account Capacity Lease

Central capacity authority issues short-lived leases to Relay/account scope:

```yaml
account_id: ...
entitlement_revision: ...
active_agent_slots: 5
heavy_slots: 2
background_slots: 2
burst_clu: ...
sustained_clu_window: ...
priority_weight: ...
valid_until: ...
signature: ...
```

Relays can admit work locally within the lease.

The authoritative allocator is sharded by Account ID so concurrent Harnesses cannot independently over-allocate the same user's quota.

## 10C.6 Lease reconciliation

- short TTL;
- periodic renewal;
- release unused slots;
- reclaim on disconnect;
- bounded grace during allocator failure;
- signed usage deltas;
- duplicate prevention;
- eventual reconciliation to durable metering.

Failure must not require one global database lock per request.

## 10C.7 Fair scheduler

After entitlement/policy/model eligibility:

Use weighted fairness across accounts.

The scheduler considers:
- queue age;
- plan weight;
- active slots;
- current CLU;
- heavy/background class;
- model pool;
- interactive priority;
- starvation prevention.

Do not let one user with many subagents monopolize a shared GPU pool.

## 10C.8 "Unlimited" product semantics

If marketed as unlimited, the contract/product copy must still define fair-use and anti-abuse safeguards.

The engineering system must not interpret "unlimited" as:
- unlimited simultaneous agents;
- unlimited registered devices;
- infinite context;
- guaranteed dedicated GPU;
- unrestricted automation/resale;
- absence of safety/security policy.

Legitimate heavy users should be queued/fairly scheduled before punitive account actions are considered.

## 10C.9 Account sharing detection

Signals:
- multiple concurrent Harnesses;
- geographically implausible simultaneous activity;
- distinct ASNs/network patterns;
- unrelated workloads at same time;
- repeated max-Harness churn;
- continuous multi-shift usage;
- unusual session-token refresh patterns;
- concurrent active repositories;
- automated client patterns;
- credential replay;
- Harness behavior/version inconsistencies.

IP/geolocation alone is never enough for automatic permanent ban.

## 10C.10 Graduated response

```text
observe
  ↓
risk flag
  ↓
step-up OAuth / reauthentication
  ↓
"user: was this you?"
  ↓
revoke suspicious Harness
  ↓
temporarily reduce concurrency
  ↓
temporary account restriction
  ↓
human review
  ↓
suspension/ban for confirmed violation
```

Immediate block may be justified for:
- credential cryptographic replay;
- malicious protocol exploit attempts;
- attacks on Patty infrastructure;
- known compromised/stolen credentials;
- repeated bypass after explicit enforcement.

## 10C.11 Trust & Safety

Public safety policy needs:
- classifier/rule findings;
- historical pattern;
- appeals/review;
- account restrictions.

Do not automatically interpret any security/cybersecurity prompt as abuse. Coding users legitimately perform:
- vulnerability remediation;
- authorized pentesting;
- malware analysis;
- CTF/security research.

Platform security and ToS abuse should focus on attacks against Patty, credential sharing/resale, model extraction/automated service misuse, or other defined prohibited uses.

## 10C.12 Prompt injection and service abuse

Public PCCP should detect:
- attempts to induce model/tool bypass of PAPER controls;
- attempts to expose hidden service credentials/system policy;
- prompt-injection artifacts from repository/context;
- repeated tool authorization bypass;
- suspicious exfiltration attempts against Patty-managed services.

Prompt injection is both:
- an agent safety issue;
- a signal that can reveal hostile external repository content.

It is not automatically a user violation.

## 10C.13 Public content retention profile

Default Public trace profile should emphasize:
- Account/Harness/session IDs;
- model/catalog/PMP;
- usage;
- timings;
- policy/security findings;
- request/content hashes;
- abuse/risk metadata;
- diagnostic error metadata.

Raw prompt/code content:
- transient when possible;
- retained only under disclosed policy;
- incident-triggered where justified;
- user-consented support diagnostics;
- minimized/redacted.

Do not inherit Enterprise full Git/source provenance by default.

## 10C.14 Public service alerts

### SEV-1
- OAuth unavailable broadly;
- PAPER ingress unavailable;
- production model unavailable across regions;
- entitlement system unusable beyond cached grace;
- metering integrity/durable accounting failure;
- severe security incident.

### SEV-2
- queue/TTFT SLO breach;
- decode performance degradation;
- capacity headroom critical;
- regional model loss;
- PIA failure surge;
- KV/cache thrashing;
- capacity allocator degraded.

### SEV-3
- one region near saturation;
- cache hit collapse;
- unusual abuse spike;
- T&S backlog;
- elevated error cohort;
- payment/support dependency degraded.

Routes:
- SEV-1: pager/on-call + Slack + email;
- SEV-2: on-call + Slack;
- SEV-3: Slack/dashboard.

Thresholds are dynamic/SLO based, not only static GPU percentages.

## 10C.15 Capacity forecasting

Forecast:
- active subscribers;
- daily/weekly concurrency;
- per-model demand;
- tokens and CLU;
- long-context growth;
- cache efficiency;
- GPU headroom;
- peak Korea time;
- regional failover capacity.

PCCP should answer: "If one GPU pool/zone fails now, can the remaining fleet maintain the target SLO?"

## 10C.16 Support tooling

Support role can:
- verify subscription state;
- see Harness inventory;
- revoke an old Harness;
- inspect auth failure metadata;
- inspect user-authorized diagnostics;
- reset enrollment limits with audit;
- escalate risk/T&S;
- issue account recovery.

Support should not have routine raw prompt/repository visibility.

## 10C.17 Public acceptance criteria

- 100k+ account simulation does not put relational DB on token hot path.
- Concurrent Harnesses cannot overspend account work slots through different Relays.
- Heavy user gets fairness queueing rather than accidental ban.
- Network-location anomaly alone cannot permanently ban.
- Revoked Harness loses ability to establish new PAPER Working Session.
- Suspended subscription cannot receive new entitlement lease.
- Public user can manage registered Harnesses self-service.
- SRE can locate TTFT regression by model/PIA/region/Harness version.

---

# 11. Model Registry, Catalog, Package, and Lifecycle

PCCP v2 turns "model" into a hierarchy of related but distinct objects.

## 11.1 Model object hierarchy

```text
CatalogModel
  └─ represents user-visible service offering
       │
       ├─ ModelPackage / PMP A
       │    └─ PIA Endpoint(s)
       │
       ├─ ModelPackage / PMP B (canary)
       │    └─ PIA Endpoint(s)
       │
       └─ ModelPackage / PMP C (rollback)
            └─ PIA Endpoint(s)
```

Core objects:

- `CatalogModel`
- `ModelDescriptor`
- `ModelCatalogEpoch`
- `ModelPackage` / PMP
- `ModelEvaluation`
- `ModelApproval`
- `InferenceEndpoint`
- `EndpointLease`
- `EndpointHealth`
- `ModelAnnouncement`
- `ModelEntitlementRule`
- `ModelRoutingPolicy`

## 11.2 Catalog Model lifecycle

States:
- Draft
- Internal
- Canary
- Public Preview
- Available
- Capacity Limited
- Degraded
- Maintenance
- Deprecated
- Withdrawn
- Emergency Recalled
- Retired

This is the lifecycle the Harness understands.

## 11.3 Model Package lifecycle

Retain detailed package lifecycle:
- Packaging
- Signed
- Evaluating
- Approved
- Approved with Restrictions
- Canary
- Production
- Suspended
- Revoked
- Expired
- Retired

A Catalog Model may remain `Available` while its underlying production package changes.

## 11.4 Endpoint lifecycle

- Discovered
- Enrollment Pending
- Attesting
- Trusted
- Degraded
- Lease Expired
- Quarantined
- Suspended
- Revoked
- Draining
- Maintenance

## 11.5 ModelDescriptor capability registry

Descriptor capability values must be generated from:
- model evaluation;
- runtime adapter capability;
- PCCP/PAPER semantic support;
- deployment policy.

Do not advertise a feature merely because the base model vendor claims it.

For example:

```text
model supports image input
+
PIA adapter supports image
+
PAPER negotiated image content
+
subscription/project allows image
=
effective image capability
```

## 11.6 Effective catalog generation

PCCP Model Catalog Service computes:

```text
Global Catalog
   ∩ Deployment Profile
   ∩ Account/Organization Entitlement
   ∩ User/Project Policy
   ∩ Harness/PAPER Capability
   ∩ Region/Residency
   ∩ Current Model/Endpoint State
   = Effective Catalog
```

The result is sent over `paper.models`.

## 11.7 Stable Catalog Model IDs

Stable model IDs should be product-oriented rather than infrastructure-specific.

Good:
- `patty-code-fast`
- `patty-code-standard`
- `patty-code-deep`

Bad as public stable IDs:
- `qwen3.6-35b-a3b-fp8-vllm-0.12-node17`

Exact package ancestry remains visible internally/Enterprise provenance as policy allows.

## 11.8 Alias/default policy

PCCP controls:
- default model by plan;
- user-selectable models;
- replacement model;
- auto/fast/deep modes;
- canary cohort.

Harness may show product aliases but does not resolve them itself. A default change arrives through catalog announcement.

## 11.9 Capability compatibility

Before cataloging a model for a Harness:
- `min_harness_version`;
- `min_paper_core`;
- required PAPER extensions;
- required tool/runtime version;
- content capabilities;
- structured output support;
- reasoning configuration;
- context features;

must be compatible.

If not, PCCP may:
- hide model;
- show "upgrade required";
- force Harness update under policy.

## 11.10 Catalog audit

Record:
- who/what published model;
- descriptor diff;
- entitlement rule;
- canary assignment;
- default change;
- announcement;
- withdrawal;
- recall;
- affected accounts/orgs/Harnesses.

Enterprise can optionally export catalog/model approval history as evidence.

## 11.11 Model capability validation tests

Every production catalog entry must pass conformance tests relevant to advertised capabilities:
- text stream;
- tool schema;
- tool-call streaming;
- strict structured output;
- parallel tools;
- image/document;
- cache accounting;
- reasoning control;
- context compaction;
- citation/source;
- cancellation/resume.

A failed capability should be removed from descriptor or block release.

## 11.12 Admin view

For each Catalog Model:
- descriptor;
- current status;
- eligible plans/orgs;
- Harness compatibility;
- production PMP;
- canary PMP;
- fallback;
- endpoints;
- current users/traffic;
- TTFT/TPS/errors;
- queue/capacity;
- capability health tests;
- announcement history;
- deprecation/recall.

For each endpoint retain v1 detailed attestation and GPU health fields.

---

# 12. Organization, Tenancy and Korean Enterprise Hierarchy

CP must model Korean organizational complexity natively.

## 12.1 Hierarchy

Support arbitrary hierarchy plus familiar concepts:

```text
Group / 그룹
  └─ Affiliate / 계열사
      └─ Division / 사업부
          └─ Headquarters / 본부
              └─ Department / 부서
                  └─ Team / 팀
                      └─ Project
```

Do not hard-code one exact hierarchy. Customers must be able to rename levels.

## 12.2 User organizational attributes

- employee number,
- legal entity,
- employment type,
- department,
- team,
- job family,
- job level/grade,
- job title/직책,
- position/직급 where customer uses it,
- manager,
- matrix manager,
- cost center,
- office/site,
- contractor company,
- start/end date,
- project memberships.

Sensitive HR fields should not be copied into CP unless needed for an explicit product purpose.

## 12.3 Delegated administration

Large enterprises need administrators at multiple scopes:

- Group admin
- Affiliate admin
- Division admin
- Department admin
- Project admin
- Repository owner
- Security admin
- AI governance admin
- Billing admin
- Auditor
- HR/Work Intelligence reviewer

A delegated admin cannot weaken mandatory policy inherited from a parent scope unless an explicit exception workflow permits it.

## 12.4 Contractor and SI mode

Common Korean enterprise/government development relies on SI partners and contractors. CP must support:

- sponsor-owned external identities,
- contract company,
- project-only access,
- repository restrictions,
- shorter credential lifetime,
- mandatory expiration,
- stricter download/file-transfer rules,
- different model/data access,
- visible external-user badges,
- sponsor review,
- automatic offboarding.

---

---

# 13. Authorization and Policy Hierarchy

Use RBAC for job roles plus ABAC for contextual decisions.

## 13.1 Policy hierarchy

```text
Patty mandatory safety baseline
        ↓
Deployment profile baseline
        ↓
Organization / Group
        ↓
Affiliate
        ↓
Business Unit / Department
        ↓
Project
        ↓
Repository
        ↓
Branch
        ↓
Task/session temporary policy
```

Lower levels may strengthen inherited mandatory policy. Weakening requires an authorized exception mechanism.

## 13.2 ABAC attributes

Evaluate:

- user,
- employment type,
- team,
- job function,
- security role,
- harness/device trust,
- network zone,
- project,
- repository,
- branch,
- file path,
- data classification,
- model,
- model endpoint assurance,
- action type,
- tool,
- MCP server,
- destination,
- time,
- incident state,
- approval state,
- risk score.

## 13.3 Decision outcomes

- allow,
- allow with transformation,
- allow with obligations,
- require user confirmation,
- require reviewer approval,
- require manager approval,
- require security approval,
- require two-person approval,
- deny,
- quarantine,
- pause session,
- terminate session,
- revoke harness,
- isolate endpoint,
- create incident.


---

---

# 14. Live Harness and Session Operations

The Control Plane must make the connected developer fleet feel like a manageable enterprise system rather than a collection of independent CLI clients.

## 14.1 Live Harness inventory

For every connected Harness instance, CP shall expose, subject to authorization:

- harness ID and certificate fingerprint,
- user identity and employment/contractor status,
- organization, affiliate, department, team, and project,
- device name, OS, version, posture, and management state,
- Harness version, build hash, release channel, and integrity state,
- network zone and source address,
- current repository, branch, worktree, and base commit,
- active task/session,
- selected Patty model and model endpoint,
- endpoint assurance level,
- session start time and last activity,
- token usage and current request state,
- context size and cache utilization where available,
- sandbox/runtime ID,
- open files and recently touched files according to configured visibility policy,
- current tool/command state,
- active MCP connections,
- network grants,
- current risk score,
- outstanding approvals,
- security findings,
- communication/presence state,
- synchronization status for audit/provenance events.

## 14.2 Fleet actions

Authorized operators shall be able to:

- request Harness re-authentication,
- force policy refresh,
- force configuration refresh,
- require client upgrade,
- move a Harness to a different release ring,
- suspend model access,
- reduce tool capabilities,
- disable one or more MCP servers,
- change token or concurrency quota,
- pause agent execution,
- terminate a session,
- revoke the Harness certificate,
- quarantine the device identity,
- isolate a sandbox,
- invalidate a temporary privilege grant,
- request a forensic snapshot,
- create an incident,
- send a direct administrative message,
- send a mandatory acknowledgement notice,
- invoke organization-wide emergency lockdown where authorized.

Every fleet action is itself an audited administrative event with actor, reason, scope, before/after state, approval, timestamp, and affected Harnesses.

## 14.3 Session Inspector

Each session shall have a unified inspector with the following views:

1. **Summary** — user, Harness, repository, branch, model, policy, task, duration, status, risk.
2. **Timeline** — every significant event in chronological order.
3. **Prompt/Response** — authorized transcript or metadata-only view depending on policy.
4. **Context** — every context item, why it was selected, transformations, classification, and token contribution.
5. **Files** — reads, writes, patches, renames, deletes, and resulting provenance.
6. **Commands** — executable, arguments, working directory, environment class, outcome, duration, policy decision.
7. **MCP/Tools** — server/tool identity, requested operation, parameters, decision, response classification.
8. **Network** — destination, DNS/IP, protocol, purpose, bytes transferred, policy.
9. **Models** — logical Patty model, exact artifact, endpoint, assurance level, inference parameters, request timings.
10. **Security** — warnings, blocks, suspicious behavior, findings, and containment actions.
11. **Approvals** — request, approvers, evidence shown, decision, expiry.
12. **Git/Provenance** — starting state, patches, commits, PR/MR, AI/human attribution.
13. **Communications** — linked user chat or operational messages if the session was shared or discussed.
14. **Resources** — CPU, memory, storage, token, context, model/GPU resource consumption.
15. **Evidence** — signed event chain and exportable evidence bundle.

## 14.4 Live view versus surveillance boundary

The product should support extensive operational visibility, but CP must distinguish:

- **operational metadata** that administrators may routinely need,
- **developer content** such as prompt text, source snippets, message bodies, and transferred files,
- **sensitive content** requiring a specific security, legal, audit, or management purpose,
- **employment analytics** used for aggregate or individual work-intelligence views.

This distinction is not intended to weaken Korean-enterprise administrator control. It makes that control explicit, permissioned, attributable, and defensible.

A content-view action should therefore record:

- who viewed it,
- what scope was opened,
- business/security purpose,
- whether content was redacted,
- linked incident or review case if applicable,
- how long the elevated view was available.

## 14.5 Session state machine

```text
NEW
  ↓
AUTHENTICATING
  ↓
POLICY_RESOLVING
  ↓
READY
  ↓
ACTIVE ───────→ WAITING_APPROVAL
  │                    │
  │                    └──→ ACTIVE
  ├──→ PAUSED
  ├──→ QUARANTINED
  ├──→ TERMINATED
  └──→ COMPLETED
             ↓
      EVIDENCE_FINALIZING
             ↓
          CLOSED
```

Protected sessions may not become `CLOSED` until required provenance and audit events are durably persisted or a defined signed-buffer recovery procedure is invoked.

## 14.6 Acceptance criteria

- An operator can locate any active Harness by user, device, repository, branch, IP/network zone, project, model, risk, or Harness ID.
- Session timeline ordering is stable across services using synchronized event IDs and timestamps.
- A revoked Harness cannot create a new session.
- A paused session cannot execute tool calls or send new model requests.
- Content viewing and administrative containment actions are independently auditable.
- A security operator can go from a critical alert to affected Harness isolation in no more than three deliberate actions.

---

---

# 15. Security Operations and Assurance Controls

CP must treat AI coding as a new privileged enterprise workload. Security controls are not warnings displayed after the model acts; they are policy enforcement points in the action path.

## 15.1 Assurance control families

The shared Enterprise/Government core should include independently testable controls for:

- identity,
- device and Harness posture,
- prompt classification,
- context authorization,
- secrets,
- Korean PII and sensitive information,
- prompt injection/untrusted instructions,
- repository/file permissions,
- command authorization,
- MCP/tool authorization,
- network egress,
- package/dependency acquisition,
- open-source license policy,
- cryptography policy,
- model authorization,
- endpoint attestation,
- runtime/sandbox confinement,
- output/artifact export,
- response inspection,
- provenance completeness,
- logging/evidence durability,
- resource limits,
- communication/file-transfer policy.

## 15.2 Security finding object

A normalized finding should include:

```yaml
finding_id: fnd_...
organization_id: org_...
severity: critical|high|medium|low|info
category: prompt_injection
status: open|investigating|contained|accepted|resolved|false_positive
confidence: 0.97
user_id: usr_...
harness_id: hrn_...
session_id: ses_...
repository_id: repo_...
branch: feature/auth
model_id: mdl_...
endpoint_id: ep_...
action_id: act_...
policy_rule_ids: [...]
first_seen_at: ...
last_seen_at: ...
evidence_refs: [...]
recommended_actions: [...]
```

## 15.3 Initial alert catalog

At minimum:

- credential or secret access,
- secret disclosure to a model,
- Korean PII exposure,
- unusual bulk repository read,
- access to unauthorized repository/path,
- prompt-injection content,
- successful/attempted policy evasion,
- suspicious encoding/obfuscation,
- dangerous shell operation,
- sandbox escape indicator,
- unexpected process tree,
- unauthorized network destination,
- data exfiltration pattern,
- unapproved MCP server/tool,
- package supply-chain risk,
- prohibited license,
- vulnerable dependency above policy threshold,
- prohibited cryptographic algorithm,
- protected branch write,
- unusual archive creation,
- model endpoint attestation loss,
- model artifact mismatch,
- unsigned or expired Harness,
- unusual login/Harness registration,
- excessive token/context/GPU use,
- audit pipeline interruption,
- provenance gap,
- abnormal file transfer,
- suspicious mass messaging/broadcast misuse,
- policy or configuration tampering.

## 15.4 Incident containment modes

**Session containment**
- pause one session,
- stop model requests,
- stop tool execution,
- preserve sandbox state,
- revoke session capabilities.

**Harness containment**
- revoke Harness lease/certificate,
- require re-enrollment,
- revoke local cached capabilities,
- disable communication/file transfer.

**Project containment**
- force read-only,
- disable exports/commits,
- disable selected models/tools,
- require all actions to receive approval.

**Organization lockdown**
- stop all new agent executions,
- optionally preserve read-only chat/status capability,
- suspend cloud model egress,
- reject new Harness enrollment,
- display mandatory emergency broadcast.

## 15.5 Security policy simulation

Before enabling a new rule, administrators should be able to run it against historical signed events and see:

- number of actions that would have been allowed/blocked,
- affected users/teams/repos,
- estimated false-positive candidates,
- expected developer friction,
- exceptions that would be required,
- security findings that would have been caught,
- comparison with current policy.

This is particularly important for Korean enterprises that frequently want highly restrictive policies but need a way to understand operational impact before enforcing them.

---

---

# 16. Prompt, Context, Data-Loss, and Injection Governance

## 16.1 Prompt governance

Every model request must have a governed prompt envelope containing:

- organization/user/Harness/session IDs,
- task type,
- prompt-template version,
- system policy version,
- user content hash,
- retention class,
- data classification,
- context manifest,
- tool capability manifest,
- model target and approved inference settings,
- correlation/provenance ID.

CP should allow configurable prompt-retention modes:

- metadata only,
- cryptographic hash only,
- redacted text,
- full encrypted text,
- incident-triggered retention,
- fixed-duration retention,
- project-specific legal hold.

## 16.2 Context firewall

The Harness may propose context, but CP policy decides what is eligible. Context authorization should be performed at file/span/object level and should consider:

- user's repository rights,
- repository/branch policy,
- file classification,
- secrets/PII content,
- code-owner restrictions,
- model classification approval,
- endpoint/data-residency profile,
- task purpose,
- token minimization budget,
- source trust level,
- injection risk.

The system should preserve a `ContextDecision` for every included and denied item.

## 16.3 Korean-sensitive data detection

Enterprise profile should ship with Korean-first detection for, at minimum:

- 주민등록번호,
- 외국인등록번호,
- 여권번호,
- 운전면허번호,
- 전화번호,
- 이메일,
- 주소,
- 계좌/카드 identifiers,
- employee IDs,
- customer IDs where organization dictionaries are configured,
- health/biometric terms and structured values,
- secrets, API keys, certificates, credentials, and connection strings.

Actions include block, mask, tokenize, pseudonymize, minimize, require purpose, require approval, and incident creation.

## 16.4 Prompt injection treatment

All repository text, issues, documentation, logs, websites, copied chat messages, MCP results, and package metadata must be treated as **data**, never as authority.

The policy boundary must ensure that untrusted content cannot:

- grant itself a tool,
- expand a file scope,
- add a network destination,
- change model endpoint,
- modify retention,
- suppress audit,
- alter the effective system policy,
- trigger an unapproved file transfer.

## 16.5 Response inspection

Before a response reaches the user or becomes an executable action, CP/Harness controls should detect:

- leaked secrets or PII,
- obviously dangerous generated commands,
- prohibited code patterns,
- license-risk snippets where detectable,
- fabricated tool completion claims,
- policy conflicts,
- unsupported high-risk actions.

Response inspection must not be relied upon as the sole barrier for tool actions. Authorization occurs before execution.

---

---

# 17. Tools, MCP, Commands, Network, and Secret Brokering

Modern coding agents are increasingly tool platforms. CP therefore must govern the tool surface as carefully as the model surface.

## 17.1 Tool Registry

Each approved tool/MCP server shall have:

- immutable tool ID,
- publisher/owner,
- version,
- package/container/binary hash,
- signature,
- supported operations,
- required capabilities,
- input/output schemas,
- network destinations,
- data classifications supported,
- credential requirements,
- risk level,
- approved organizations/projects,
- expiry/reapproval,
- test/evaluation results.

## 17.2 MCP governance

CP shall support:

- organization-wide MCP allow/deny list,
- project-specific MCP policy,
- managed MCP registry,
- signed MCP server identity,
- argument-level policy,
- result scanning,
- per-tool approval requirements,
- token/resource quota,
- audit/provenance correlation,
- kill switch,
- version pinning and forced upgrade/disable.

An MCP server may never become a side door that bypasses File, Network, Secrets, PII, or Audit controls.

## 17.3 Command authorization

Command policy should parse rather than string-match where feasible. Record:

- executable,
- arguments,
- shell expansion,
- working directory,
- referenced paths,
- environment variable names (values redacted as needed),
- requested network behavior,
- privilege implications,
- expected outputs,
- risk class.

Examples of policy actions:

- allow `pytest` within project workspace,
- deny `curl` except through broker,
- require approval for DB migration tooling,
- prohibit `sudo`, host mounts, raw sockets, cloud metadata access,
- block destructive Git operations on protected worktrees.

## 17.4 Network broker

No agent/sandbox receives broad internet access by default. Approved network grants should be scoped by:

- destination identity,
- DNS/IP range,
- port/protocol,
- method/path where possible,
- purpose,
- duration,
- byte budget,
- content class,
- user/session,
- audit requirement.

## 17.5 Secret broker

The model should not receive raw long-lived credentials. Secret access should use:

1. tool requests an operation requiring a secret,
2. CP policy authorizes purpose and target,
3. broker obtains/creates a short-lived scoped credential,
4. credential is injected directly to the approved process/connection,
5. raw value is excluded from prompt/context and normal logs,
6. use is recorded,
7. credential is revoked/expired after the action.

---

---

# 18. Git/SCM as a First-Class Control-Plane Subsystem

Git is not merely an external integration. CP's provenance promise depends on a deep, stable relationship with source state.

## 18.1 Repository identity

Every onboarded repository shall have a CP `Repository` object independent of provider. It maps to one or more SCM remotes and includes:

- provider and server,
- repository UUID,
- canonical remote URL,
- default branch,
- repository classification,
- owning organization/team,
- code owners,
- protected branches,
- policy overlays,
- retention requirements,
- provenance settings,
- CI/CD bindings.

## 18.2 Supported SCM patterns

Initial enterprise targets:

- GitHub Enterprise Cloud/Server,
- GitLab SaaS/Self-Managed,
- Bitbucket where customer demand exists,
- Gitea/Forgejo for private/on-prem deployments,
- optional Patty-managed Git service for customers without an approved SCM.

The Patty-managed Git service should be an optional deployment module, not a requirement to use CP.

## 18.3 Immutable task baseline

At session start, CP records:

- repository ID,
- remote state,
- branch,
- HEAD commit,
- worktree status,
- uncommitted human changes and their hashes where permitted,
- submodule/LFS state,
- relevant dependency lockfiles,
- CI baseline.

This becomes `RepoBaseline` and is referenced by every code-producing action.

## 18.4 Change-set graph

Every editing action emits a normalized change object:

```yaml
change_set_id: chg_...
session_id: ses_...
repository_id: repo_...
base_commit: 9b7...
branch: feature/payments
actor_type: AI|HUMAN|MIXED
actor_id: usr_...
model_artifact_id: mdlpkg_...   # when AI involved
files:
  - path: src/payment/PaymentService.java
    operation: modify
    hunks: [...]
parent_change_set_ids: [...]
tests: [...]
policy_decisions: [...]
created_at: ...
```

## 18.5 Branch-aware governance

Policies can vary by branch class:

- feature branch: ordinary AI edits permitted,
- release branch: stricter tests and review,
- protected main: no direct agent write,
- emergency/hotfix: explicit incident/change-ticket relationship.

## 18.6 Commit and merge integration

CP should be able to emit:

- signed commit candidate,
- PR/MR metadata,
- provenance check,
- policy status check,
- security scan check,
- AI contribution summary,
- model approval summary,
- evidence reference.

Organizations may require `CP Provenance Complete` as a merge gate.

---

---

# 19. Line-Level Human/AI Provenance

This is a flagship differentiator and should be treated as a core product, not an analytics afterthought.

## 19.1 Questions the system must answer

For any currently visible code span, an authorized user should be able to determine:

- whether AI participated,
- which user initiated the relevant task,
- which Harness/device/session was used,
- which exact model package and endpoint generated or modified it,
- the originating prompt/task where retention allows,
- what files/documents/issues/API schemas influenced it,
- what tools and commands executed,
- what policies applied,
- what tests/scans were run,
- who reviewed or approved it,
- which commit/PR introduced it,
- which portions remain unchanged from the AI proposal,
- which portions were subsequently modified by humans,
- whether another AI later refactored it,
- whether the provenance has ambiguity,
- what business/technical outcome is associated with the change.

## 19.2 Why line numbers are insufficient

Code moves. CP must combine:

- Git hunk mapping,
- rename detection,
- AST node identity,
- symbol lineage,
- normalized semantic fingerprints,
- token/structure similarity,
- patch ancestry,
- merge-parent analysis,
- explicit human reconciliation for ambiguous cases.

## 19.3 Attribution states

A code span can be:

- human-authored,
- AI-generated,
- AI-generated then human-edited,
- human-authored then AI-refactored,
- AI-generated then AI-refactored by a different model/session,
- template/generated-tool derived,
- copied/moved with provenance retained,
- mixed/ambiguous.

Do not force every line into an artificial binary percentage if the evidence is ambiguous.

## 19.4 Surviving-code metrics

Useful metrics include:

- initial AI-generated lines/spans,
- surviving AI-originated spans after N commits,
- human modification distance,
- AI rework rate,
- defect/rollback relationship,
- test coverage attributable to AI-assisted work,
- reviewer acceptance with/without modification.

These can feed engineering analytics but should not become simplistic employee scores by themselves.

## 19.5 Provenance source of truth

The full provenance graph lives in CP/evidence storage, not inside ordinary Git history. Git should contain only durable references such as:

- commit trailers,
- Git notes,
- PR checks,
- signed provenance manifest IDs.

Sensitive prompts and context should not be embedded in Git.

---

---

# 20. Change Impact Intelligence

A valuable enterprise extension beyond the existing GongCode plan is to calculate the likely **effect** of AI-assisted changes.

## 20.1 Impact graph

CP should maintain or integrate a graph connecting:

- repositories,
- modules/packages,
- symbols,
- APIs,
- DB schemas/tables,
- services,
- deployment units,
- tests,
- owners,
- incidents,
- issues/requirements,
- releases.

When a session edits code, CP can show:

- symbols changed,
- callers/dependents potentially affected,
- APIs/contracts affected,
- database objects affected,
- security-sensitive paths touched,
- tests covering the path,
- owners/reviewers who should be involved,
- downstream repositories/services likely impacted.

## 20.2 AI Change Risk Score

Each candidate change may receive a transparent risk score based on factors such as:

- authentication/authorization code touched,
- cryptography,
- production configuration,
- database schema/data migration,
- external API contract,
- number of modules/files affected,
- test coverage delta,
- dependency changes,
- package/license risk,
- provenance confidence,
- model approval/risk tier,
- unusually broad AI context or tool use,
- past incidents/rollbacks in affected area.

The score must show factor contributions and must not be model confidence masquerading as security truth.

## 20.3 Change impact workflows

- automatically recommend reviewers based on ownership and impact,
- require extra approval above configured thresholds,
- alert affected teams,
- generate a release/change-management summary,
- link to Jira/issue/change ticket,
- feed post-deployment monitoring and rollback analysis.

This turns provenance from a forensic capability into a day-to-day engineering control.

---

---

# 21. Enterprise Communications Hub

Enterprise CP shall provide a communication plane closely integrated with identity, Harness presence, project context, and audit.

The goal is not to replace a company's full collaboration suite. The goal is to provide **contextual operational communication inside the AI engineering environment**, particularly where external Slack/Teams access is prohibited or inconvenient.

## 21.1 Communication modes

- 1:1 direct message,
- group chat,
- project room,
- repository room,
- incident room,
- temporary session-sharing room,
- administrative message,
- broadcast announcement.

## 21.2 Harness experience

CLI/TUI should provide a side panel or toggleable view for:

- unread count,
- direct/group conversations,
- presence,
- mentions,
- attachments,
- linked repository/file/commit/session references,
- broadcast notices.

VS Code should provide a dedicated CP communications view with native links to source locations and provenance.

## 21.3 Presence

Presence states:

- available,
- busy,
- do not disturb,
- away,
- offline,
- in active agent session,
- in incident response,
- optional custom status.

Presence should distinguish human status from Harness connection status. A Harness being online does not necessarily mean the user is active.

## 21.4 Message object

```yaml
message_id: msg_...
conversation_id: conv_...
sender_user_id: usr_...
sender_harness_id: hrn_...    # optional
message_type: text|file|code_ref|session_ref|system|broadcast
classification: internal
body_ciphertext_ref: ...
mentions: [...]
linked_objects:
  - type: repository
    id: repo_...
  - type: provenance_span
    id: psp_...
created_at: ...
edited_at: ...
retention_policy_id: ret_...
```

## 21.5 Encryption

- TLS/mTLS in transit.
- Message content encrypted at rest.
- Per-tenant key separation.
- Government/sovereign deployment uses customer-controlled keys.
- File attachments use independent content encryption and malware/DLP scanning.
- Administrative access to message bodies follows a separate authorization path and is audited.

## 21.6 Context linking

A user should be able to send another user:

- `repo://project/path#L20-L43`,
- a commit,
- a PR/MR,
- a CP provenance span,
- an incident,
- a policy finding,
- an agent session handoff,
- an approved file attachment.

Recipient access is re-authorized at open time. Sending a link must not grant repository/data access the recipient does not otherwise have.


---

---

# 22. Broadcast, Emergency, and Administrative Messaging

Broadcasting uses the Communications transport but has different authorization, delivery, acknowledgement, and display semantics.

## 22.1 Broadcast severities

| Severity | Example | Harness behavior |
|---|---|---|
| Informational | maintenance notice, policy change | notification center / non-blocking |
| Advisory | dependency issue, planned model migration | prominent banner |
| Warning | active service degradation, project risk | persistent notice until read |
| Critical | security incident, credential compromise | interruptive modal/TUI alert; acknowledgement may be required |
| Emergency | stop work / suspected compromise | full-width blocking notice; may accompany automatic lockdown |

## 22.2 Targeting

Authorized senders can target:

- organization,
- affiliate,
- business unit,
- department/team,
- location/network zone,
- project,
- repository,
- Harness release channel,
- users of a specific model/tool/MCP server,
- users with active sessions matching a security condition,
- named users/groups.

## 22.3 Broadcast controls

Every broadcast includes:

- sender and role,
- severity,
- subject/body,
- target expression,
- effective start/expiry,
- acknowledgement requirement,
- optional action buttons,
- optional linked incident/runbook/policy,
- optional localization,
- delivery count/read count/ack count,
- immutable audit record.

Critical/Emergency broadcasts should support two-person approval in customers that require it.

## 22.4 Administrative command versus message

A broadcast is communication. A fleet-control action is enforcement. The UI must distinguish them clearly.

For example, `"Stop using Model X"` as a message does **not** disable Model X. The sender may pair it with an authorized policy action that actually suspends the model, and CP records the two linked events.

---

---

# 23. Managed File Transfer and Secure Handoff

File transfer is useful for enterprise collaboration but creates a direct data-exfiltration path, so it must be a governed CP capability.

## 23.1 Supported transfer types

- user-to-user attachment,
- group/project-room attachment,
- session handoff package,
- log/test artifact,
- patch/diff,
- approved binary/document,
- incident evidence shared to authorized responders.

## 23.2 Transfer pipeline

```text
Sender
  ↓
Authorization
  ↓
Classification + DLP + PII + Secret Scan
  ↓
Malware / Archive / File-Type Scan
  ↓
Recipient Access Check
  ↓
Encrypted Object Storage
  ↓
Message Reference
  ↓
Recipient Re-authorization at Download
  ↓
Download Audit
```

## 23.3 Transfer policy dimensions

- sender/recipient relationship,
- organization/affiliate boundaries,
- project membership,
- file extension/MIME,
- maximum size,
- data classification,
- PII/secrets,
- executable/archive restrictions,
- retention,
- watermarking for documents where appropriate,
- download count/expiry,
- external/contractor restrictions.

## 23.4 Session handoff

A developer may explicitly hand an active task to another authorized developer/reviewer. Handoff package can include:

- task summary,
- repository and branch,
- exact baseline/current worktree state,
- current diff,
- pending tool plan,
- test results,
- relevant conversation thread,
- provenance references,
- outstanding findings/approvals.

The recipient receives a new identity-bound session. User identity is never transferred or impersonated.

---

---

# 24. Work Intelligence: Engineering and AI-Use Analytics

This module addresses the customer request to use CP data in employee evaluation while avoiding a low-quality "count tokens and lines" surveillance product.

**Product name in this PRD:** `CP Work Intelligence`.

Work Intelligence should be marketed primarily as:

- engineering effectiveness analytics,
- AI adoption and capability analytics,
- quality/security adherence analytics,
- coaching and development support,
- project delivery intelligence.

It may provide individual-level inputs to an organization's evaluation process, but CP should not make autonomous employment decisions.

## 24.1 Why CP can provide unusually rich signals

Unlike an ordinary source-control analytics product, CP sees a connected chain of evidence:

```text
Requirement / Task
      ↓
User + Harness
      ↓
Prompt / Plan
      ↓
Context + Tools + Model
      ↓
Code Changes
      ↓
Human Rework
      ↓
Tests + Security + Review
      ↓
Commit / PR / Merge
      ↓
Release / Incident / Rollback
```

This enables outcome-oriented signals rather than only activity counts.

## 24.2 Metric taxonomy

### A. Delivery and scope

- tasks/features completed,
- requirements/issues linked to completed work,
- lead/cycle time,
- PR/MR throughput,
- review turnaround,
- distinct functional areas changed,
- change size and complexity,
- cross-repository impact,
- rework after review,
- rollback/revert frequency.

### B. Quality

- test additions and meaningful coverage delta,
- test failure rate before completion,
- escaped defects linked to changes where integrations provide evidence,
- reviewer-requested rework,
- static-analysis quality,
- maintainability trends,
- documentation completeness,
- provenance completeness.

### C. Security and governance

- security-policy adherence,
- repeated violations by category,
- unsafe action attempts,
- secret/PII handling quality,
- dependency/license compliance,
- required approval compliance,
- prompt-injection response,
- protected-branch compliance,
- exception frequency and quality of justification.

### D. AI effectiveness

- proportion of tasks assisted by AI,
- task success with AI,
- AI proposal acceptance with/without modification,
- surviving AI code after subsequent commits,
- AI-generated tests accepted,
- human rework distance after AI generation,
- unnecessary retry/loop behavior,
- model/tool selection appropriateness,
- context efficiency,
- prompt/plan quality rubric,
- effective use of specialized tools and organization-approved workflows.

### E. Collaboration

- review participation,
- useful feedback received/given where structured review integrations permit,
- task handoffs,
- issue resolution participation,
- cross-team dependency resolution,
- incident contribution,
- knowledge/documentation contribution.

### F. Learning and improvement

- trend in policy violations,
- trend in AI rework,
- trend in test quality,
- adoption of recommended secure patterns,
- completion of organization AI/security training where HR/LMS integration exists,
- improvement after coaching.

## 24.3 Metrics explicitly classified as weak signals

The following may be shown for capacity/usage but should **never be treated as standalone performance measures**:

- number of prompts,
- number of tokens,
- time connected,
- number of files touched,
- lines of code,
- number of Harness sessions,
- raw AI acceptance rate,
- number of chat messages,
- after-hours activity.

They can be gamed easily and can incentivize harmful behavior.

---

---

# 25. Work Intelligence Rubric and Scorecards

## 25.1 Configurable rubric model

CP should not ship one universal "developer score." Instead, organizations create role-specific scorecards from governed metric groups.

Example:

```yaml
rubric:
  name: Backend Engineer - Standard
  version: 3
  evaluation_period: quarterly
  dimensions:
    delivery_outcomes:
      weight: 30
    engineering_quality:
      weight: 25
    security_governance:
      weight: 20
    ai_effectiveness:
      weight: 15
    collaboration_learning:
      weight: 10
```

## 25.2 Example dimension definitions

### Delivery Outcomes — 30%

Prefer completed, accepted business/engineering outcomes over activity.

Signals:
- completed features/issues with traceable ownership,
- timeliness relative to agreed plan,
- change effectiveness,
- avoidable rework.

### Engineering Quality — 25%

Signals:
- tests and verification,
- defect/revert data,
- review outcomes,
- code-quality policies,
- maintainability.

### Security & Governance — 20%

Signals:
- serious violations,
- recurring risky behavior,
- adherence to approvals,
- dependency/license/security controls,
- incident behavior.

### AI Effectiveness — 15%

Signals:
- ability to use AI to finish appropriate work,
- low unnecessary iteration,
- context/prompt planning quality,
- appropriate skepticism/review of AI output,
- quality of final output rather than volume of AI output.

### Collaboration & Learning — 10%

Signals:
- review/helpfulness,
- handoff quality,
- documentation,
- improvement trends.

## 25.3 Prompting/AI-use rubric

An optional rubric can evaluate **observable interaction quality**, without evaluating hidden model reasoning:

1. **Task specification** — Is goal/acceptance criteria clear?
2. **Context discipline** — Does user provide/retrieve relevant context rather than indiscriminately exposing the repo?
3. **Planning** — Does user use plan/review modes appropriately for complex work?
4. **Verification** — Does user require tests, inspect diff, and validate behavior?
5. **Security judgment** — Does user avoid attempts to bypass controls and respond appropriately to warnings?
6. **Iteration efficiency** — Does interaction converge or repeatedly thrash without learning?
7. **Tool selection** — Are approved tools/models used appropriately?
8. **Human judgment** — Are AI proposals corrected when wrong rather than blindly accepted?

For each rubric dimension CP must expose **supporting evidence**, not only a score.

## 25.4 Score explainability

Every score shall show:

- rubric version,
- evaluation window,
- included/excluded projects,
- raw supporting metrics,
- weighting,
- confidence/data-completeness indicator,
- missing integration caveats,
- manager adjustments/comments,
- employee response/comment where enabled,
- change history.

## 25.5 Team versus individual analytics

Default dashboards should emphasize team/project trends. Individual drill-down should be an explicit role permission.

Examples:

- team AI adoption trend,
- review/rework trend,
- common security violations,
- model/tool effectiveness,
- skills/training opportunities,
- bottlenecks in approvals,
- repositories with unusual defect/rework patterns.

---

---

# 26. Employment-Decision Guardrails and Review Workflow

Because Work Intelligence may affect employment evaluation, CP must build procedural safeguards directly into the product rather than assuming customers will add them later.

## 26.1 Default rule

**CP generates evidence and recommendations; a responsible human makes consequential personnel decisions.**

CP shall not, by default:

- automatically rank employees for termination,
- automatically recommend firing/demotion,
- automatically deny promotion or compensation,
- silently generate immutable black-box employee scores,
- use private chat content as a general performance signal,
- use off-hours presence as a positive productivity signal.

## 26.2 Evaluation workflow

```text
Evaluation Period Opens
        ↓
Data Completeness Check
        ↓
CP Evidence + Draft Scorecard
        ↓
Manager Review
        ↓
Required Human Explanation / Adjustments
        ↓
Employee View / Comment or Appeal (configurable, recommended)
        ↓
Finalization in HR process
        ↓
Signed Evaluation Snapshot + Audit
```

## 26.3 Dispute and correction

The module should support:

- flagging inaccurate repository/task attribution,
- excluding work not relevant to an evaluation period,
- correcting missing issue/feature ownership,
- manager override with documented reason,
- employee comment/response,
- second-level review,
- immutable history of metric and rubric changes.

## 26.4 Bias and gaming controls

Monitor for:

- role bias caused by comparing unlike job functions,
- senior engineers looking "less productive" because they review/design more than code,
- incident/security work undercounted versus feature work,
- large monorepo work inflating file/line counts,
- deliberately generating excessive AI output to improve adoption metrics,
- avoiding high-risk work to preserve a clean score,
- managers changing weights after seeing results.

Require rubric version locking before an evaluation period unless a governed amendment is approved and disclosed.

## 26.5 Privacy/automated-decision product requirements

For deployments using Work Intelligence in consequential evaluation, CP should support configuration/evidence for:

- disclosure of evaluation purpose and data categories,
- description of major scoring criteria,
- human review state,
- correction/objection workflow,
- access history,
- retention/deletion,
- organization-specific privacy notices,
- impact/risk assessment evidence where applicable.

Legal applicability remains deployment-specific and must be reviewed by the customer; CP provides tooling and evidence rather than legal certification.

---

---

# 27. Privacy, Administrative Visibility, and Content Access

## 27.1 Product stance

Korean enterprise buyers may explicitly want broad supervisory visibility. CP should meet that need with **controlled transparency**:

> If the organization has a legitimate policy and grants an administrator authority to inspect specific employee AI activity, CP should make the evidence available. The product must also record that the administrator exercised that authority.

## 27.2 Visibility levels

### Level A — operational metadata

Typical operations roles can see:

- connected user/Harness,
- repo/branch/session,
- model,
- token/capacity,
- policy decisions,
- filenames or restricted metadata according to project policy,
- security finding metadata.

### Level B — engineering content

Authorized project/security roles may see:

- prompt/response text,
- code context,
- command output,
- source diffs,
- transferred files.

### Level C — communication content

Message bodies/attachments require dedicated communications-audit or investigation permission and should not be implied by ordinary platform-admin status.

### Level D — employment analytics

Individual Work Intelligence requires a separate personnel-analytics permission set.

## 27.3 Purpose-bound access

Sensitive views can optionally require:

- selecting a purpose,
- linked ticket/incident/evaluation,
- JIT elevation,
- manager/security/privacy approval,
- visible access banner,
- time-bounded grant.

## 27.4 Admin audit

Administrators are first-class actors in provenance/audit. Record:

- sensitive content viewed,
- search queries against employee content,
- exports,
- policy changes,
- evaluation-rubric changes,
- broadcast actions,
- user/Harness revocations,
- evidence deletion/retention actions,
- break-glass activity.

---

---

# 28. Engineering, Adoption, and Executive Analytics

## 28.1 Engineering dashboard

Views by organization, department, team, project, repository, and time:

- active developers/Harnesses,
- active/completed AI-assisted tasks,
- AI-assisted PRs and commits,
- human/AI attribution trends,
- test/quality outcomes,
- rework and rollback,
- security findings,
- approval bottlenecks,
- top affected modules,
- model/tool effectiveness.

## 28.2 AI adoption dashboard

- licensed/enrolled/weekly active users,
- percentage of engineering teams using CP,
- task categories using AI,
- model mix,
- Harness/IDE/TUI usage,
- approved tool/MCP adoption,
- users requiring training/support,
- successful task completion patterns,
- organization-wide AI maturity trend.

## 28.3 Executive dashboard

Executives should see outcomes, risk, and adoption—not raw prompt streams by default:

- adoption by business unit,
- estimated engineering time saved only where methodology is defined,
- completed AI-assisted initiatives,
- quality/rework trend,
- material security events,
- policy compliance trend,
- model/GPU cost and utilization,
- risk by project,
- top organizational blockers,
- Work Intelligence aggregate trends.

## 28.4 Admin natural-language analytics

Optional capability: authorized administrators can ask CP questions in Korean/English, e.g.:

- "지난 7일 동안 개인정보 차단이 가장 많았던 프로젝트를 보여줘."
- "Q2 결제 프로젝트에서 AI가 수정한 인증 관련 파일과 리뷰 결과를 보여줘."
- "이번 달 사용량이 30% 이상 증가한 부서를 알려줘."

Requirements:

- query only within caller authorization,
- translate question into auditable analytics query,
- show filters and source objects used,
- cite underlying records,
- never invent missing data,
- log the administrative query itself when sensitive.

---

---

# 29. Usage, Entitlements, Subscription, Fair Use, Billing, and Chargeback

PCCP v2 uses one measurement model for Patty Public commercial operation and Enterprise internal allocation.

## 29.1 Measurement dimensions

Always preserve:
- account/user;
- organization/project when applicable;
- Harness;
- Working Session;
- PAPER Exchange;
- Catalog Model;
- exact PMP/endpoint internally;
- input tokens;
- output tokens;
- cache-read tokens;
- cache-write tokens;
- context size;
- request count;
- tool/server-tool usage;
- compute/GPU duration;
- queue duration;
- TTFT;
- completion state;
- Compute Load Units (Public internal);
- storage/evidence where relevant.

Commercial billing may use only a subset.

## 29.2 Public Subscription object

```yaml
subscription_id: sub_...
account_id: acct_...
plan_id: unlimited-developer
status: active
effective_at: ...
renewal_at: ...
grace_until: ...
payment_provider_ref: ...
entitlement_revision: 184
```

Payment provider details and PCI-sensitive data should remain in payment-provider systems wherever possible.

## 29.3 Public entitlement

Entitlement is a service authorization, not an API key.

Fields:
- allowed catalog-model classes;
- enabled features;
- context class;
- registered Harness maximum;
- active Harness policy;
- agent slot class;
- heavy/background capacity;
- priority weight;
- fair-use profile;
- experiment flags;
- support tier;
- expiry/grace.

PAPER Capability Leases derive from the current entitlement.

## 29.4 Subscription login enforcement

Without valid entitlement:
- Harness may authenticate for account management;
- Account Portal may remain accessible;
- model catalog may show subscription-required state where useful;
- new inference Working Sessions are denied.

Cancellation can allow paid-through end date. Past-due state may have configurable grace.

## 29.5 Public model catalog and entitlement coupling

The effective Model Catalog contains only:
- models included by plan;
- models temporarily granted;
- models available to cohort/region;
- models compatible with Harness.

A user's local choice cannot override entitlement.

## 29.6 Account Capacity Lease

See Section 10C.

The capacity lease is separate from commercial subscription:
- Subscription says the service is included.
- Capacity Lease says what resources this account can consume concurrently right now.

This allows dynamic fairness and service protection without pretending the subscription has a fixed token allowance.

## 29.7 Fair-use windows

PCCP may maintain internal rolling windows:
- short burst;
- 5m/1h load;
- daily;
- weekly;
- abuse-research horizon.

These are operational safeguards, not necessarily publicly advertised quotas.

A long-window heavy user may be deprioritized/queued under fair-use policy before punitive account action.

## 29.8 Rate limit hierarchy

Shared limiter hierarchy may evaluate:

```text
Model endpoint hard limit
     ↓
Model pool capacity
     ↓
Deployment / plan
     ↓
Account
     ↓
Harness (optional anti-abuse)
     ↓
Working Session
     ↓
Agent Work Slot
     ↓
Exchange
```

Enterprise can additionally insert:
- organization;
- department;
- project;
- user.

Every relevant limiter must pass.

## 29.9 Cost model

Internal cost can combine:
- model-specific GPU cost;
- prefill;
- decode;
- cache/KV;
- server tools;
- object storage;
- bandwidth;
- Relay/scanning cost.

Public subscription margin dashboards compare resource cost to plan cohort revenue without exposing internal CLU to users unless product chooses to.

## 29.10 Enterprise subscription

Retain v1 options:
- Patty-hosted PCCP + Patty models;
- customer PCCP + Patty cloud model;
- customer PCCP + approved Patty model packages on customer GPU;
- hybrid.

Enterprise may charge:
- seats;
- subscription tiers;
- managed GPU;
- usage;
- deployment/support;
- feature modules.

## 29.11 Government entitlement

Signed offline entitlement:
- deployment identity;
- edition/module rights;
- model-package rights;
- capacity if contracted;
- effective/expiry;
- signature;
- offline revocation/update process.

No mandatory phone-home.

## 29.12 Enterprise chargeback

Allocate technical usage by:
- affiliate;
- department;
- cost center;
- project;
- repository;
- user;
- Catalog Model/PMP.

Keep invoice price distinct from technical consumption.

## 29.13 Metering integrity

Metering used for billing/fairness must be:
- server/Relay/PIA derived, not only Harness self-report;
- idempotent;
- Exchange correlated;
- durable;
- reconcilable;
- versioned.

Dropped final usage frames must be reconstructable from PIA/Relay event data where possible.

## 29.14 User-facing usage

For an "unlimited" plan, the account portal should avoid a misleading token countdown.

Show useful service state:
- current active agent slots;
- registered Harnesses;
- current queue/capacity state;
- unusual-account warnings;
- if fair-use throttling is active, clear explanation and retry/queue behavior.

Detailed token usage may be exposed for transparency/debugging, but must not imply a fixed token entitlement unless one exists.

---

# 30. Model and GPU Operations

CP must provide enough model/GPU visibility for enterprise operators while keeping the model-serving plane isolated from source and employee content.

## 30.1 Model operations

Per model/version:

- logical product name,
- artifact/package ID,
- base model ancestry where distributable,
- weight/tokenizer/config hashes,
- quantization,
- context limit,
- serving profile,
- supported tasks,
- Korean coding benchmark,
- security evaluation summary,
- approval scope,
- endpoint count,
- current traffic,
- latency/error metrics,
- suspension/retirement status.

## 30.2 Endpoint operations

Per endpoint:

- endpoint identity,
- Patty Inference Agent version,
- host identity,
- cluster/node,
- serving engine/version,
- loaded model artifact,
- attestation state/age,
- assurance level,
- certificate/lease expiry,
- active/queued requests,
- TTFT and decode latency,
- context/KV metrics where exposed,
- failures/restarts,
- current routing weight,
- drain/canary state.

## 30.3 GPU operations

Where infrastructure integration permits:

- GPU model and serial/attestation identity,
- utilization,
- VRAM,
- temperature,
- power,
- ECC/health,
- MIG/partitioning,
- model replicas,
- queue depth,
- concurrent requests,
- capacity reservations,
- maintenance/drain state.

GPU operators should not need access to source files or prompt bodies.

## 30.4 Routing policy

Routing considers:

- logical Patty model requested,
- endpoint/model attestation,
- organization entitlement,
- data classification,
- deployment zone/residency,
- context requirement,
- task type,
- endpoint health,
- concurrency/queue,
- cost/priority policy,
- canary policy.

Fallback may never silently route to a model or location outside the effective policy.

## 30.5 Data Residency Router

For Korean enterprise deployments with hybrid/cloud/on-prem choices, CP shall expose explicit routing zones such as:

- `KR-PATTY-CLOUD`,
- `CUSTOMER-ONPREM`,
- `CUSTOMER-AIRGAP`,
- `CUSTOMER-PRIVATE-CLOUD`,
- future approved regional zones.

Project/data-class policy selects eligible zones. Provenance records the exact execution location.

---

---

# 31. Runtime and Sandbox Control

Although the Harness may run some tools locally for the individual/public edition, Enterprise/Government profiles should support centrally governed execution boundaries.

## 31.1 Runtime modes

1. **Managed Local** — signed Harness invokes approved local tools under endpoint controls.
2. **Remote Sandbox** — disposable isolated workspace controlled by CP.
3. **Customer Sandbox Pool** — on-prem/private execution workers.
4. **Air-Gapped Sandbox** — local-only government execution.
5. **Review-Only** — no execution; model analysis and provenance review.

Project policy determines allowed modes.

## 31.2 Remote sandbox baseline

For sensitive work:

- immutable/signed base image,
- ephemeral encrypted workspace,
- non-root,
- no host socket,
- no broad network,
- resource limits,
- external audit agent,
- short-lived credentials only,
- explicit artifact-export gate,
- destruction/key discard after close.

## 31.3 Sandbox fleet view

Admins can see:

- sandbox ID/user/session,
- image/version/attestation,
- host/pool,
- resource use,
- current command,
- network grants,
- lifecycle age,
- findings,
- snapshot/forensic state,
- destruction evidence.

## 31.4 Local execution tradeoff

Enterprise customers may demand local shell performance. CP should therefore support managed-local execution where policy allows, but the UI must clearly show that it provides a different isolation assurance than a disposable microVM. Never market the two as equivalent.

---

---

# 32. Enterprise Integration Requirements

## 32.1 Identity and directory

Priority integrations:

- SAML 2.0,
- OIDC,
- LDAP/Active Directory,
- Microsoft Entra ID,
- Google Workspace where used,
- SCIM provisioning/deprovisioning,
- enterprise MFA,
- device certificate/MDM signals.

Korean organizations often have custom HR/groupware directories; provide documented identity adapter APIs.

## 32.2 Source and delivery systems

- GitHub Enterprise,
- GitLab,
- Bitbucket,
- Gitea/Forgejo,
- Jenkins,
- GitHub Actions/GitLab CI,
- Argo/Tekton where present,
- Nexus Repository,
- JFrog Artifactory,
- Harbor.

## 32.3 Work management

- Jira,
- GitHub/GitLab Issues,
- internal PMS/groupware through API,
- ServiceNow where present,
- custom Korean groupware/task systems.

Task links improve change-impact and Work Intelligence accuracy.

## 32.4 Security and operations

- SIEM,
- SOAR,
- EDR/MDM,
- DLP,
- vulnerability management,
- HSM/KMS,
- secrets managers,
- ITSM/incident systems,
- NTP/time authority,
- enterprise notification systems.

## 32.5 HRIS/LMS for Work Intelligence

Optional, separately permissioned:

- organization hierarchy,
- manager relationship,
- employment type,
- role/job family,
- evaluation period,
- training completion.

CP should avoid becoming the system of record for payroll/compensation. It exports governed evidence/scorecards to the customer's HR process.

## 32.6 Chat interoperability

Future optional connectors to Slack/Teams/Kakao Work/enterprise messengers can mirror or notify selected CP conversations/broadcasts, but CP-native secure messaging must remain available for closed/private environments.

---

---

# 33. Korean Enterprise-Specific Differentiators

The following are proposed additions specifically because they map well to operating patterns in Korean 중소기업, 중견기업, 대기업, chaebol/affiliate structures, SI environments, and regulated organizations.

## 33.1 Group / Affiliate Control Tower

Large Korean groups frequently operate many affiliates with central IT/security standards and local autonomy.

CP should support:

```text
Group Policy
  ├── Affiliate A
  │     ├── Division
  │     └── Projects
  ├── Affiliate B
  └── Shared IT / Security
```

Features:

- central mandatory baseline,
- affiliate-specific stronger overlays,
- central aggregate analytics without automatically exposing affiliate content,
- delegated local admins,
- shared model/GPU capacity with chargeback,
- cross-affiliate exception workflow,
- central emergency broadcast.

## 33.2 SI / Outsourced Developer Mode

Many Korean enterprises rely heavily on SI/contractor developers. CP should make contractor control a first-class capability:

- sponsor-owned identity,
- contract/project expiry,
- restricted repository list,
- restricted models/data classes,
- controlled file transfer,
- no cross-customer context,
- mandatory customer VPN/network zone where configured,
- automatic disable on contract end,
- immutable access/evidence handoff.

## 33.3 Shadow AI Discovery

Organizations will want to know whether employees are bypassing Patty Code and using unapproved AI tools.

CP can optionally ingest signals from:

- secure web gateway/proxy,
- DNS/network telemetry,
- EDR/MDM application inventory,
- browser/extension policy,
- enterprise API gateway,
- sanctioned SaaS discovery tools.

Show:

- potential unapproved AI endpoints/tools,
- affected teams/devices,
- risk category,
- migration recommendation to Patty Code.

CP itself should not perform invasive endpoint surveillance; it consumes customer-authorized security telemetry and correlates it with registered Harness identities.

## 33.4 AI Change Control Board

For high-risk repositories, customers can operate an AI-specific change-control queue:

- high-risk AI-generated patches,
- model-version changes,
- policy exceptions,
- new MCP/tools,
- new external network destinations,
- new dependency/license exceptions.

The queue resembles traditional enterprise change management but is populated automatically from CP evidence.

## 33.5 Repository Sensitivity Heatmap

CP automatically derives and allows owners to override a repository map showing areas such as:

- authentication,
- cryptography,
- payment/financial,
- personal data,
- infrastructure/IaC,
- secrets/config,
- customer-specific modules,
- public API contracts.

This drives stricter AI policy only where needed instead of making every file equally restrictive.

## 33.6 Mandatory Policy Acknowledgement

When an organization changes AI policy materially, CP can require affected users to acknowledge:

- new model restrictions,
- privacy/data rules,
- security rules,
- coding standards,
- incident notices.

The Harness blocks specified high-risk workflows until acknowledgement if the policy owner configures it.

## 33.7 Organization AI Skills Matrix

Aggregate observed, evidence-backed use patterns into coaching categories:

- planning with AI,
- testing/verification,
- secure AI use,
- tool/MCP use,
- repository/context discipline,
- review quality.

Managers can identify training needs without equating raw AI use with competence.

## 33.8 Policy Exception Marketplace / Catalog

Not a public marketplace: an internal catalog of pre-reviewed exception types.

Examples:

- temporary public package access,
- one-time external API testing,
- legacy SHA-1 compatibility exception,
- large-context repository read,
- contractor elevated repository access.

Each template has required approvers, maximum duration, compensating controls, and evidence requirements.

## 33.9 Emergency Model Recall

If Patty discovers a model-version safety or quality problem:

- signed recall advisory enters connected enterprise CPs,
- admins see affected endpoints/sessions/commits,
- cloud customers can auto-suspend based on configured policy,
- on-prem customers can approve/execute recall locally,
- air-gap customers receive a signed offline advisory bundle,
- CP can identify all changes produced by the recalled model for targeted review.

## 33.10 Forced Harness Version and Ring Management

Manage releases like an enterprise endpoint product:

- Canary,
- Early Access,
- Stable,
- Long-Term Support,
- Frozen/Air-Gap.

Admins can require minimum versions and block vulnerable builds while supporting staged rollout.

## 33.11 Architecture and Coding Standard Packs

Organizations can encode internal standards such as:

- approved Java/Spring versions,
- framework patterns,
- package boundaries,
- logging conventions,
- forbidden dependencies,
- mandatory testing,
- naming/API conventions,
- architecture decision references.

The Harness receives plain-Korean explanations when a rule blocks work.

## 33.12 Executive Weekly AI Governance Brief

Scheduled report generated from signed data:

- adoption,
- AI-assisted delivery outcomes,
- material security findings,
- unresolved exceptions,
- model/GPU cost,
- major policy changes,
- high-risk repositories,
- important incidents,
- training needs.

It should be concise enough for 임원회의 while each claim drills into CP evidence.

## 33.13 Change-Freezing / Critical Period Mode

For product launches, audits, financial close, or incidents, authorized leaders can activate a time-bounded mode that:

- restricts AI edits on selected branches/repos,
- permits read/review/test use,
- requires elevated approval for write/export,
- broadcasts the freeze,
- logs exceptions.

## 33.14 Project Offboarding and Evidence Handoff

At project/SI contract end:

- revoke contractor Harnesses,
- close temporary entitlements,
- freeze or archive provenance,
- generate evidence package,
- transfer repository/project ownership,
- expire communication rooms/file links,
- confirm access removal.

## 33.15 AI Model ROI Comparison

Because CP controls the entire workflow, enterprise admins can compare approved Patty model versions on:

- cost,
- TTFT/latency,
- task completion,
- rework,
- test outcome,
- security findings,
- context efficiency.

This supports model selection based on **final engineering outcome**, not a benchmark alone.

---

---

# 34. Deployment Architecture and Profiles

## 34.1 One kernel, three operational profiles

```text
                         PCCP KERNEL
          Identity | PAPER | Catalog | Entitlement
        Policy | Relay | Scheduler | PIA | Metering
               Event Spine | Modules | Audit
                   /          |          \
                  /           |           \
        PATTY PUBLIC     ENTERPRISE     SOVEREIGN
          SCALE           CONTROL         TRUST
```

There are no long-lived edition branches.

## 34.2 Patty Public Cloud

Required:
- multi-region/public OAuth;
- subscription/payment integration;
- public Harness registry;
- PAPER ingress/Relay fleet;
- authoritative model catalog;
- Patty-operated PIA/model GPU;
- Account Capacity Authority;
- fair scheduler;
- public account integrity;
- Trust & Safety;
- platform security;
- SRE/alerts/support;
- operational trace profile.

Public is likely the highest-scale profile.

## 34.3 Enterprise Cloud

Required:
- organization/SSO;
- full admin Control;
- project/repository;
- governance;
- security;
- model catalog filtered by org policy;
- provenance;
- communications;
- usage/chargeback;
- optional Work Intelligence;
- Patty cloud and/or approved private PIA depending profile.

## 34.4 Enterprise Private / On-Prem

- customer PCCP;
- customer DB/event/evidence;
- customer keys;
- local Relays;
- local PIA/GPU and/or Patty cloud if policy permits;
- private integrations;
- optional offline entitlement grace.

## 34.5 Government / Sovereign

- local PCCP;
- local model catalog;
- local identity/PKI;
- local Relays;
- local PIA/model/GPU;
- customer keys;
- offline entitlement;
- offline updates/catalog/model packages;
- no mandatory public telemetry;
- strict policy.

## 34.6 Shared trust zones

```text
USER/HARNESS
   │ PAPER
   ▼
PAPER DATA PLANE
   Relay / ingress / stream controls
   │
   ├────────► Control authority services
   │          identity, policy, catalog, capacity
   │
   ▼ PAPER
PIA / MODEL PLANE
   │ local adapter
   ▼
SERVING ENGINE / GPU

Separate:
EVENT / EVIDENCE / ANALYTICS
INTEGRATIONS
ADMIN WEB
```

## 34.7 Public multi-region

Public should support:
- global DNS/load balancer;
- region-local PAPER ingress;
- stateless or softly stateful Relay nodes;
- account-sharded capacity authority;
- regional model schedulers;
- model pools;
- durable event ingestion;
- multi-region account/control DB strategies;
- regional failure routing.

A user's active Working Session may be sticky to a region/Relay cohort for cache/KV efficiency.

## 34.8 Module manifest

Deployment profile controls modules:

```yaml
profile: patty-public-cloud
required:
  - core.identity
  - core.paper
  - core.catalog
  - core.entitlement
  - core.gateway
  - core.scheduler
  - core.pia
  - core.metering
  - core.events
  - public.oauth
  - public.subscription
  - public.fair_use
  - public.account_integrity
  - public.trust_safety
  - public.sre
disabled:
  - enterprise.work_intelligence
  - enterprise.employee_comms
trace_profile: operational
```

Profiles must be validated at boot and emitted as configuration evidence.

## 34.9 Feature flags are not product forks

Feature flags can stage:
- new catalog service;
- new PAPER semantic version;
- new scheduler;
- new abuse model;
- new model.

They must be time-bounded and converged into the shared product; avoid permanent nested if-statements by edition.

## 34.10 Admin vs data plane

Web/admin APIs must never be on the high-throughput token data path.

Public account portal outage should not necessarily terminate already-authorized active inference if cached leases permit bounded continuity.

Control authority outage behavior is explicit per service:
- entitlement lease grace;
- catalog cache;
- policy cache;
- capacity lease TTL;
- revocation freshness.

## 34.11 Regional data policy

Enterprise/Government uses Data Residency Router.

Public can additionally route by:
- legal availability;
- latency;
- capacity;
- product region policy.

Exact region of inference is recorded internally and, where required, surfaced to enterprise provenance.

---

# 35. Security Architecture and Threat Model

PCCP v2 expands the v1 threat model to include Internet-scale consumer abuse and PAPER/model-catalog integrity.

## 35.1 Shared threats

Retain:
- malicious user;
- compromised Harness;
- fake model endpoint;
- compromised model;
- prompt injection;
- malicious MCP/tool;
- dependency attack;
- admin abuse;
- audit tampering;
- provenance forgery;
- cross-tenant leakage;
- endpoint compromise;
- communications exfiltration;
- evaluation misuse.

## 35.2 Public-specific threats

| Threat | Example | Control |
|---|---|---|
| account sharing | one subscription used by several people | Harness registry, concurrency signals, step-up auth, graduated enforcement |
| credential theft | stolen OAuth/session/Harness key | token/key rotation, revocation, anomaly detection |
| Harness cloning | copied Harness credential | peer challenge, concurrent clone detection, revocation |
| generic client resale | subscription used as backend model API | PAPER-only, Harness identity, work-slot/fair-use policy |
| model catalog spoof | local user injects model/base URL | server-authoritative catalog, no generic provider config |
| stale catalog | withdrawn model remains selectable | catalog epoch + Relay validation + push withdrawal |
| protocol abuse | malformed frames/DoS | parser limits, auth-before-expensive-work, rate limiting |
| model extraction | automated high-volume probing | behavioral/rate/capacity analysis, T&S review |
| GPU starvation | one account launches many heavy agents | Account Capacity Lease + weighted fairness |
| abuse false positive | traveler/VPN looks shared | multi-signal evidence + step-up/review |
| payment entitlement fraud | plan state manipulated client-side | server-side entitlement authority |
| endpoint bypass | raw vLLM called | network isolation + PIA-only route |
| Paper downgrade | client falls back to generic API | no generic fallback, protocol version binding |

## 35.3 Explicit security claims

PCCP can claim:
- an official unmodified Harness has no supported generic OpenAI/Anthropic inference fallback;
- PCCP authorizes models through server catalog + signed model/endpoint identity;
- an unregistered Harness cannot use normal subscriber service;
- account/Harness/session authority is independently revocable;
- model tool authority is enforced outside model.

PCCP must not claim:
- an open-source Harness running on a fully attacker-controlled machine is mathematically impossible to modify;
- PAPER prevents a user from independently writing another HTTPS program;
- IP/location perfectly identifies account sharing;
- software-only PIA can defeat malicious root on the inference host without stronger attestation.

## 35.4 Defense against provider/base-URL substitution

Official Harness build:
- no generic `provider` config;
- no `OPENAI_BASE_URL` equivalent for subscriber inference;
- no Anthropic-compatible endpoint config;
- no custom API-key path for subscription;
- no raw `model` string outside catalog;
- no HTTP fallback from PAPER.

PCCP:
- validates catalog epoch;
- validates entitlement;
- resolves PMP;
- validates Endpoint Lease.

Network/EDR controls are required if an Enterprise customer wants to prohibit **other programs** from accessing external AI.

## 35.5 Account Integrity is not Trust & Safety

Separate data stores/workflows as appropriate.

Account Integrity:
- "is this the same subscriber / compromised / shared?"

Trust & Safety:
- "is use consistent with service terms/safety policy?"

Platform Security:
- "is the user/client attacking Patty infrastructure?"

Capacity:
- "can we fairly serve this workload right now?"

A single event may influence multiple states but actions are reason-coded.

## 35.6 Zero-trust requirements

- separate user/Harness/Relay/PIA/model identity;
- short-lived leases;
- transport-bound PAPER auth;
- no implicit network trust;
- signed Policy/Catalog/Endpoint state;
- service-to-service credentials;
- least privilege;
- admin audit.

## 35.7 Fail-closed vs degrade

Fail closed:
- invalid peer;
- revoked Harness;
- invalid entitlement;
- invalid catalog/model;
- invalid endpoint lease;
- security-critical protocol integrity;
- prohibited tool/action.

May degrade with bounded signed cache:
- temporary subscription authority outage for already-paid account;
- catalog service outage with unexpired snapshot;
- policy service outage with unexpired signed policy bundle;
- capacity service outage only within small already-issued lease.

Public degraded mode must not create unlimited unmetered usage.

## 35.8 Prompt injection

Repository/user-provided content cannot:
- modify Capability Lease;
- modify effective model catalog;
- change endpoint;
- create provider URL;
- expand tool set;
- disable audit;
- alter subscription.

This is especially important because the model itself may see arbitrary code/comments/instructions.

## 35.9 Security tests

Add:
- local model-list injection;
- base-URL environment variable attempts;
- fake model ID;
- stale catalog epoch;
- model withdrawn mid-session;
- generic HTTP endpoint downgrade;
- duplicate Harness credential;
- concurrent account slot race across Relays;
- OAuth replay;
- capacity lease overspend;
- malformed PAPER tool stream;
- unknown capability downgrade.

---

# 36. Cryptography and Key Management

## 36.1 Key domains

Separate keys for:

- CP service identity,
- Harness/device identity,
- endpoint/PIA identity,
- model package signing,
- model decryption/key release,
- software release signing,
- message/file encryption,
- evidence signing,
- tenant data encryption,
- offline update/license signing.

Avoid one root key that compromises every trust domain.

## 36.2 Enterprise key options

- Patty-managed KMS for SaaS profile,
- customer-managed KMS/HSM for private deployments,
- BYOK/HYOK patterns where supported,
- air-gap offline HSM/key ceremony for sovereign deployments.

## 36.3 Rotation/revocation

CP must support:

- key rotation without losing historical verification,
- certificate revocation,
- model-package signing-key revocation,
- endpoint identity rotation,
- emergency compromise procedure,
- timestamped trust anchors for historical evidence.

---

---

# 37. Data Architecture

PCCP v2 preserves v1 shared data domains and adds Public-scale account/catalog/capacity domains.

## 37.1 Logical domains

| Domain | Core data | Store class |
|---|---|---|
| Account/Identity | accounts, users, OAuth bindings, Harnesses | relational + cache |
| Subscription | plans, entitlements, payment refs | relational |
| Catalog | CatalogModels, descriptors, epochs, announcements | relational/registry + in-memory |
| Control | orgs, projects, sessions, policies | relational |
| Capacity | leases, counters, queue ownership | distributed in-memory + durable journal |
| Model | PMP, PIA endpoints, endpoint leases | registry/relational |
| Events | PAPER exchanges, policy, usage, lifecycle | durable event bus |
| Analytics | token/load/SRE/risk aggregates | OLAP |
| Risk | account integrity/T&S/platform security cases | relational + analytics |
| Search | operational/audit indices | search |
| Evidence | signed receipts/bundles | object/WORM-capable |
| Provenance | code/context causal graph | relational + graph/index |
| Communications | conversations/message metadata | relational/content store |
| Attachments | encrypted file/voice | object |
| Secrets | service/short-lived issuance | secret manager |
| Telemetry | metrics/traces/logs | observability pipeline |

## 37.2 Public hot state

Relays SHOULD NOT synchronously read the primary relational store for:
- every AI request;
- token chunk;
- tool delta.

Hot state includes:
- peer credential/revocation cache;
- entitlement lease;
- catalog snapshot;
- policy epoch;
- Account Capacity Lease;
- model route table;
- Endpoint Lease;
- rate/fairness local counters.

## 37.3 Account sharding

At Public scale, ownership for strongly coordinated per-account concurrency should be deterministically shardable:

```text
shard = hash(account_id) mod N
```

Implementation may use another consistent partitioning scheme, but all active Harnesses for one account must reconcile against one logical capacity authority.

## 37.4 Durable event spine

Durable events are the reconciliation source for:
- usage;
- billing;
- analytics;
- abuse;
- support;
- capacity forecasting.

Exactly-once delivery is not assumed. Consumers are idempotent.

## 37.5 Model catalog distribution

Model catalog snapshots:
- versioned;
- content-digested;
- small enough for memory;
- distributed to Relays;
- sent to Harness over PAPER after effective filtering.

PCCP stores current plus recent epochs sufficient for in-flight validation/audit.

## 37.6 Public privacy

Public trace data should be partitioned by content class:
- operational metadata;
- raw content;
- security-derived metadata;
- support-consented diagnostic content.

Raw user code/prompt must not be copied into generic telemetry/OLAP pipelines.

## 37.7 Enterprise provenance

Retain full v1 provenance/data domains where enabled.

## 37.8 Universal labels

Relevant objects carry:
- account/organization;
- user;
- Harness;
- session/exchange;
- classification;
- owner;
- retention;
- region;
- access labels;
- source/provenance;
- deletion/hold.

Public objects may omit irrelevant enterprise organization labels.

## 37.9 Data migrations from v1

Schema migration should be additive:
- create `Account` separate from `User` where needed;
- add Subscription/Entitlement revisions;
- create CatalogModel/ModelDescriptor/CatalogEpoch;
- migrate old direct model references to CatalogModel mappings;
- create CapacityLease;
- add explicit risk-state classes;
- map old Gateway request IDs to PAPER Exchange IDs where dual operation exists.

Historical v1 records remain readable.

---

# 38. Protocol and API Boundary Requirements

This section materially supersedes PCCP v1.

## 38.1 Hard boundary

**Official Patty Code Harness service communication uses PAPER.**

This includes:
- peer auth;
- user binding;
- session;
- model catalog;
- inference;
- context;
- tool calls/results;
- usage stream;
- public service notices;
- Enterprise chat/voice/file/broadcast where enabled;
- Harness administrative directives.

The Harness must not use a public OpenAI/Anthropic compatibility endpoint as its model path.

## 38.2 PAPER endpoint families

PAPER is not URL-oriented. Capability families include:
- `paper.core`
- `paper.models`
- `paper.ai`
- `paper.context`
- `paper.tools`
- `paper.provenance`
- `paper.chat`
- `paper.voice`
- `paper.file`
- `paper.broadcast`
- `paper.telemetry`

## 38.3 Web/admin APIs

HTTP/JSON and/or gRPC remain appropriate for:
- web Account Portal;
- PCCP admin UI;
- public OAuth callbacks;
- payment webhooks;
- SCM/CI integrations;
- SIEM;
- HRIS;
- external reporting;
- automation/admin APIs.

These APIs require their own auth/RBAC and are not alternative subscription inference APIs.

## 38.4 Internal service APIs

Internal PCCP services may use:
- Protobuf/gRPC;
- event bus;
- database contracts;
- PAPER where the peer is a protocol participant.

Implementation protocol is not customer-visible unless documented.

## 38.5 Legacy v1 Gateway API

If current PCCP has `/v1/gateway/responses` or equivalent:
- mark internal/legacy;
- block new Harness releases from using it;
- put behind service network/auth;
- migrate traffic to PAPER Relay;
- instrument remaining callers;
- remove external access after migration window.

Do not keep it permanently as a silent escape hatch.

## 38.6 Compatibility adapters

A PIA adapter MAY call local serving-engine OpenAI-compatible APIs.

This is allowed only:
- behind PIA;
- on loopback/private endpoint;
- with no direct subscriber credential;
- as implementation detail.

The compatibility interface does not become a supported PCCP external API.

## 38.7 Idempotency/correlation

All write/admin APIs and PAPER side effects:
- idempotency;
- stable event IDs;
- Working Session/Exchange correlation;
- actor identity;
- audit.

## 38.8 Versioning

Separate:
- admin API version;
- PAPER core/extension version;
- catalog schema version;
- model capability schema;
- event schema.

Harness compatibility is derived from PAPER/catalog versions, not admin API version.

---

# 39. Event Model and Event Topics

PCCP v2 retains the durable event spine and expands it for Public Cloud and model catalog.

## 39.1 Common event envelope

Retain:
- event ID;
- schema version;
- occurred/received time;
- account/organization;
- user;
- Harness;
- Working Session;
- Exchange;
- project/repo where applicable;
- Catalog Model;
- PMP/endpoint where applicable;
- classification;
- payload/reference;
- source identity;
- integrity data.

## 39.2 Shared topics/classes

Existing:
- identity lifecycle;
- Harness lifecycle;
- session lifecycle;
- prompt/context/action/policy/tool/runtime;
- model request;
- endpoint;
- Git/provenance;
- security;
- communication/file/broadcast;
- usage;
- entitlement;
- evidence/admin/config.

Add:

```text
pccp.account.lifecycle
pccp.account.auth
pccp.subscription.lifecycle
pccp.entitlement.revision
pccp.harness.enrollment
pccp.paper.connection
pccp.paper.exchange
pccp.catalog.epoch
pccp.catalog.announcement
pccp.catalog.withdrawal
pccp.catalog.selection
pccp.capacity.lease
pccp.capacity.admission
pccp.capacity.queue
pccp.fairuse.state
pccp.account_integrity.signal
pccp.account_integrity.case
pccp.trust_safety.case
pccp.platform_security.event
pccp.model.recall
pccp.sre.alert
```

## 39.3 Usage event

Normalized usage event:

```yaml
event_type: pccp.usage.record
account_id: ...
user_id: ...
harness_id: ...
session_id: ...
exchange_id: ...
catalog_model_id: ...
model_package_id: ...
input_tokens: ...
output_tokens: ...
cache_read_tokens: ...
cache_write_tokens: ...
reasoning_compute_tokens: ...
gpu_ms: ...
queue_ms: ...
ttft_ms: ...
compute_load_units: ...
status: completed
```

Fields that a model cannot provide remain null, not fabricated.

## 39.4 Risk events

Risk signals must include:
- risk domain;
- rule/model version;
- confidence;
- raw features references;
- action taken;
- reviewer state.

Do not collapse Account Integrity and T&S into one event type.

## 39.5 Catalog events

A model announcement produces:
- catalog epoch;
- before/after descriptor digest;
- target/entitlement scope;
- admin/release actor;
- rollout;
- Harness acknowledgement metrics.

## 39.6 Event integrity

Security/provenance critical events retain:
- schema validation;
- source identity;
- sequence/causal relationships;
- dedupe;
- durable buffering;
- tamper evidence/signatures where required.

Operational high-volume token deltas need not be individually signed if final authenticated evidence/usage records bind the stream according to PAPER.

## 39.7 Consumer isolation

Async consumers:
- billing;
- analytics;
- abuse;
- SRE;
- Work Intelligence;
- reports;

must have independent lag/backpressure so one consumer outage does not block Relay token streaming.

---

# 40. Audit, Evidence, Retention, and Legal Hold

## 40.1 Audit philosophy

CP should produce evidence continuously. An auditor or incident responder should not have to reconstruct an AI engineering session from screenshots and disconnected application logs.

## 40.2 Audit event coverage

Audit at minimum:

- login/logout/authentication failure,
- user/group/role changes,
- Harness enrollment/revocation,
- device/Harness attestation,
- project/repository onboarding,
- policy/exception changes,
- prompt/context/model/tool decisions,
- sensitive admin content access,
- code/provenance lifecycle,
- file transfers,
- communications administrative access,
- broadcasts,
- security findings/incident actions,
- model/endpoint lifecycle,
- entitlement/billing changes,
- Work Intelligence rubric/evaluation access and changes,
- evidence exports,
- retention/legal-hold changes,
- update/release operations.

## 40.3 Evidence bundles

Generate signed bundles by:

- session,
- commit/PR,
- release,
- repository,
- incident,
- policy change,
- model version,
- user/Harness access review,
- evaluation period.

Bundle contains manifests and references to the required evidence while preserving access controls on highly sensitive content.

## 40.4 Retention classes

Retention should be separately configurable for:

- raw prompts,
- redacted prompts,
- responses,
- source/context snapshots,
- tool/command output,
- message content,
- transferred files,
- operational metadata,
- security findings,
- provenance,
- admin access logs,
- Work Intelligence metrics/evaluations,
- evidence bundles.

Do not couple "we keep provenance for years" to "we keep every raw prompt for years."

## 40.5 Legal hold

Authorized roles can place scoped legal/investigation hold on:

- user,
- Harness/device,
- project/repository,
- session,
- conversation,
- incident,
- evaluation snapshot,
- time window.

Hold actions and later release are audited.

## 40.6 Export

Exports support:

- CSV/JSON for ordinary reports,
- signed JSON/CBOR/manifest for machine-verifiable evidence,
- PDF/HTML report rendering if desired,
- SIEM forwarding,
- offline verification tooling for sovereign deployments.

---

---

# 41. Korean Governance and Compliance Packs

The enterprise product should inherit the same policy/evidence framework as GongCode, with deployment-specific profiles rather than a separate government product fork.

The baseline GongCode plan already treats KISA/MOIS secure development, eGovFrame, CSAP, ISMS-P, privacy, AI governance, cryptography, and organizational standards as versioned profiles rather than hard-coded claims. CP should preserve that pattern.

## 41.1 Enterprise-first profiles

Initial enterprise packs should include:

- organization secure-coding baseline,
- Korean 개인정보 protection/evidence baseline,
- ISMS-P-oriented evidence mapping,
- software supply-chain baseline,
- OSS license governance,
- AI model/agent governance,
- organization-specific security policy,
- optional ISO/IEC 27001 and ISO/IEC 42001 cross-mapping,
- optional NIST SSDF/AI RMF cross-mapping for multinational customers.

## 41.2 Government overlay

Government/Sovereign deployments additionally enable and strengthen:

- public-sector secure-development mappings,
- eGovFrame profile,
- closed-network/air-gap policy,
- CSAP readiness where relevant to a cloud service scope,
- KCMVP-aware cryptographic deployment controls where applicable,
- public-sector evidence/records requirements,
- stricter model/tool/network defaults.

## 41.3 Policy-source model

CP shall distinguish:

- law/binding obligation,
- certification criterion,
- official guidance,
- secure-coding recommendation,
- vendor security baseline,
- organization policy,
- project convention.

The UI must not label a customer preference such as "no GPL" or "no console.log" as a statutory KISA requirement unless an applicable source specifically establishes it.

## 41.4 Versioning

Compliance-source objects contain:

- authority/publisher,
- title,
- version/notice number,
- publication/effective dates,
- checksum/source URL or controlled source reference,
- superseded state,
- applicability notes,
- reviewed interpretation,
- mapped CP controls/evidence.

Do not hard-code laws into model weights as the sole source of current truth.

---

---

# 42. Open Source Strategy and Trust Boundary

The Harness and CP are intended to be open source. Security must therefore survive complete source-code disclosure.

## 42.1 Open source principles

- No security by obscurity.
- Protocol/schema documentation is public where feasible.
- Official builds are reproducible or provenance-verifiable where practical.
- Official release artifacts are signed.
- Customers can inspect, self-host, and audit.
- Proprietary Patty model artifacts and subscription services can remain separately licensed.

## 42.2 Official-build trust

An organization may configure policy to require:

- Patty-signed Harness builds,
- approved enterprise-rebuilt Harness builds,
- approved CP builds,
- Patty-signed PIA/endpoint-agent builds,
- organization-signed variants.

Forked software is not automatically malicious, but it does not inherit an official trust profile until enrolled/approved.

## 42.3 Model restriction caveat

If an administrator controls the entire CP host, source code, inference host, keys, and model artifacts, no purely software-only scheme can make it mathematically impossible for that administrator to alter the system and route elsewhere.

Therefore the meaningful security goals are:

1. normal Patty Code Harnesses cannot route to arbitrary models through an unmodified/approved CP;
2. CP accepts only enrolled and attested PIA endpoints under the configured trust policy;
3. official encrypted Patty model packages can require approved key-release conditions;
4. customers cannot claim an altered fork is an official Patty-attested deployment without the required signatures/trust anchors;
5. bypasses are detectable within the defined trust boundary.

This is a stronger and more honest requirement than attempting to detect a model by its output style.

## 42.4 Extension architecture

Provide documented extension points for:

- identity adapters,
- SCM/CI connectors,
- policy packs,
- scanners,
- SIEM sinks,
- storage backends,
- inference-engine adapters,
- Work Intelligence data enrichers,
- enterprise messaging connectors.

Extensions must declare capabilities and receive scoped service identity.

---

---

# 43. Non-Functional Requirements

## 43.1 Availability

Initial objectives:

| Component | Public | Enterprise |
|---|---:|---:|
| OAuth/account auth | 99.95% target | N/A/customer IdP |
| PAPER ingress/Relay | 99.95% | 99.9%+ |
| entitlement authorization | 99.95% with bounded lease cache | 99.9% |
| model catalog | 99.95% for new sessions | 99.9% |
| model gateway/PIA routing | 99.95% excluding maintenance | 99.9% |
| policy cached evaluation | 99.99% | 99.99% |
| event/metering durable path | no unaccounted protected completion | same |
| admin console | 99.9% | 99.9% |

Targets are planning objectives until validated.

## 43.2 Public scale targets

Architecture must be tested at least for:
- 1,000,000+ registered accounts in control data;
- 300,000+ paid/entitled accounts scenario;
- 100,000+ online Harness connections scenario;
- 25,000+ simultaneously active Working Sessions scenario;
- 10,000+ streaming inference exchanges scenario;
- rapid spikes around Korea peak hours;
- millions of usage/event records per minute depending telemetry granularity.

These are design/load-test targets, not forecast claims.

## 43.3 Enterprise scale

Retain:
- SME: 10–200 developers;
- mid-market: 200–2,000;
- large group: 2,000–20,000+;
- complex organizations/repos/evidence.

## 43.4 Latency budgets

Public:
- cached peer/entitlement/catalog checks should be low single-digit milliseconds where local;
- admission/routing overhead p95 target < 20 ms inside data plane excluding external scanners;
- PAPER service overhead should remain small relative to TTFT;
- model catalog delivery after auth p95 target < 500 ms for normal snapshot;
- withdrawal/revocation propagation target < 10 s, faster for emergency where architecture permits.

Enterprise remote policy/scanner latency may be larger and profile-specific.

## 43.5 Queue SLO

Track:
- wait p50/p95/p99 by model/plan/region;
- percentage immediate admitted;
- time in fair-use queue;
- starvation;
- cancellation while queued.

A high-usage account cannot indefinitely starve low-usage accounts.

## 43.6 Resilience

- horizontally scalable Relay;
- signed state cache;
- account-sharded capacity authority;
- idempotent events;
- queue backpressure;
- endpoint draining;
- model fallback;
- regional failover;
- audit/metering buffering;
- DB/object/event HA;
- graceful OAuth/payment dependency failure where possible.

## 43.7 State convergence

Set measurable SLOs for:
- Harness revocation;
- entitlement update;
- catalog announcement/withdrawal;
- model recall;
- endpoint health;
- capacity lease;
- policy epoch.

A Relay that cannot refresh critical state beyond maximum staleness fails safe.

## 43.8 Observability

Metrics/traces/logs:
- no unnecessary prompt/source duplication;
- cardinality controls;
- account IDs pseudonymized/controlled in generic telemetry;
- detailed user lookup in protected operational stores.

## 43.9 Security performance

Security modules must publish latency and false-positive/false-negative validation.

A new scanner cannot be rolled globally without:
- shadow/canary;
- latency measurement;
- error budget;
- rollback.

## 43.10 Compatibility

CI must test a matrix of:
- current Harness;
- previous supported Harness;
- PAPER versions;
- Public/Enterprise/Sovereign profiles;
- current/next model descriptors;
- vLLM/SGLang adapters.

Model capability addition cannot silently break older Harnesses.

## 43.11 Disaster recovery

Public:
- account/subscription/catalog/registry recovery;
- Relay stateless recreation;
- event replay;
- usage reconciliation;
- regional model capacity failover.

Enterprise/Government retains v1 backup/DR/offline procedures.

---

# 44. Korean-First UX and Administration

## 44.1 Language

- Korean is a first-class product language, not a translated afterthought.
- English is fully supported for multinational companies.
- User/admin language can differ within one tenant.
- Security/policy terminology should use natural Korean enterprise language alongside stable technical identifiers.

## 44.2 Korean organization model

Support display/search fields for:

- 법인/계열사,
- 본부/사업부/부문,
- 실/센터,
- 팀,
- 직책,
- 직급,
- 고용/협력사 상태,
- 프로젝트,
- 비용센터.

Do not force every customer into a fixed hierarchy; these are configurable organization-unit types.

## 44.3 Korean names and search

- Hangul and romanized names,
- optional 사번,
- fuzzy Hangul search,
- organization-first disambiguation,
- no assumption that Western first/last-name ordering applies.

## 44.4 Time and business calendar

- KST first-class,
- absolute timestamps in evidence,
- timezone-aware multinational views,
- Korean holiday/business calendar integration for reporting if needed.

## 44.5 Admin density

The admin UI can deliberately favor information density over consumer simplicity. Requirements:

- high-density tables,
- persistent filters,
- saved views,
- bulk actions,
- multi-column drill-down,
- timeline and graph views,
- keyboard navigation,
- wallboard/NOC mode,
- export,
- Korean search.

The product should still use progressive disclosure so the same information can be inspected rather than presented as an unreadable single screen.

---

---

# 45. Reporting and Scheduled Outputs

## 45.1 Standard reports

- AI adoption report,
- usage/cost report,
- security findings report,
- policy compliance report,
- model inventory/approval report,
- Harness/device inventory,
- provenance coverage report,
- AI-assisted change report,
- Work Intelligence team report,
- evaluation audit report,
- contractor access report,
- exception/expiry report,
- file-transfer report,
- admin-sensitive-access report,
- incident report,
- affiliate/group summary.

## 45.2 Report delivery

- interactive Control dashboard,
- CSV/JSON,
- PDF where customer workflow requires,
- signed evidence bundle,
- email/enterprise-message notification in connected environments,
- scheduled export to customer-controlled storage.

## 45.3 Report provenance

Important reports should include:

- query/filter definition,
- source event window,
- data completeness,
- generated time,
- generator version,
- report hash/signature where required.

---

---

# 46. Product Administration, Configuration, and Change Management

## 46.1 Configuration domains

Shared:
- Deployment Profile;
- module enablement;
- identity/auth adapters;
- Harness enrollment/version policy;
- PAPER protocol/extension minimums;
- Model Catalog;
- PMP/endpoints;
- entitlements;
- routing/scheduling;
- policy;
- retention;
- integrations;
- keys/certificates;
- release rings.

Public-specific:
- OAuth providers;
- subscription plans;
- registered-Harness policy;
- work-slot/fair-use profiles;
- Account Capacity Lease policy;
- Account Integrity/T&S rules;
- SRE alerts;
- Public catalog eligibility;
- support/recovery controls.

Enterprise:
- organization hierarchy;
- SSO/SCIM;
- repository/project;
- tools/MCP;
- communications;
- Work Intelligence;
- chargeback.

Government:
- offline trust;
- local catalog/update;
- sovereign route;
- cryptographic profile.

## 46.2 Configuration lifecycle

High-impact changes use:
1. draft;
2. schema validation;
3. conflict check;
4. simulation where applicable;
5. reviewer approval;
6. signed publication;
7. canary/staged rollout;
8. observation;
9. enforcement;
10. rollback/expiry.

## 46.3 Catalog changes

Model catalog publication is treated as high-impact production configuration.

Before announce:
- descriptor validation;
- capability conformance;
- entitlement mapping;
- Harness compatibility;
- route/PMP health;
- rollout cohort;
- rollback model.

Withdraw/recall has an emergency path with audited authority.

## 46.4 Public policy changes

Fair-use/account integrity rules should support:
- shadow mode;
- cohort simulation;
- false-positive review;
- staged enforcement.

Platform exploit signatures may have emergency rollout.

## 46.5 Configuration drift

Detect:
- PCCP node differs from profile;
- Relay stale catalog/policy/revocation;
- Harness stale/incompatible;
- PIA stale;
- model package mismatch;
- Endpoint Lease expired;
- capacity authority state lag;
- integration failure;
- old legacy Gateway still externally reachable;
- prohibited provider/base URL enabled in official Harness config.

## 46.6 Configuration provenance

Record:
- actor;
- reviewer;
- before/after;
- rollout;
- affected profile/cohort;
- reason;
- version/digest;
- resulting incidents/regressions.

## 46.7 Release rings

Shared components:
- Internal
- Canary
- Early
- Stable
- LTS
- Frozen/Air-Gap

Model catalog and Harness release rings can be independent but compatibility must be validated.

---

# 47. Public Onboarding, Enterprise Rollout, and Migration

## 47.1 Public user onboarding

```text
Install Harness
  ↓
Launch
  ↓
PAPER bootstrap detects no enrollment
  ↓
Open browser OAuth / device flow
  ↓
Resolve Account
  ↓
Verify Subscription
  ↓
Generate Harness peer key
  ↓
Enroll Harness
  ↓
Receive PAPER credential
  ↓
PAPER auth + user bind
  ↓
Receive Model Catalog Snapshot
  ↓
Select/default model
  ↓
Start Working Session
```

User should not be asked for:
- API key;
- model provider;
- base URL;
- endpoint.

## 47.2 Subscription missing

If no eligible subscription:
- Harness displays sign-up/upgrade flow;
- may provide account/settings functions;
- does not create inference Capability Lease.

## 47.3 Harness limit reached

Harness shows registered devices and directs user to Account Portal:
- revoke an old Harness;
- retry enrollment.

Do not ask users to manually edit credential files.

## 47.4 Model upgrade

When new model announced:
- online Harness gets delta;
- UI adds it;
- no software update unless capability requires new PAPER/Harness version.

When Harness upgrade is required:
- descriptor includes min version;
- user receives update message;
- incompatible model remains disabled.

## 47.5 Enterprise pilot

Retain v1 sequence:
- org/SSO;
- units/groups;
- repositories;
- Harnesses;
- SCM/CI;
- model route;
- baseline policy/security;
- provenance;
- training;
- pilot;
- expand.

Add:
- validate PAPER model catalog policy;
- ensure no generic endpoint config remains in enterprise Harness;
- map organization Catalog Model IDs to approved private/cloud PMP routes.

## 47.6 Brownfield PCCP v1 migration

Existing deployment sequence:
1. inventory Harness versions and current Gateway API callers;
2. deploy v2-compatible PCCP control schemas;
3. enable Model Catalog Service in observe mode;
4. map all current model references to Catalog Models;
5. deploy PAPER Relay alongside legacy Gateway;
6. update PIA/PAPER semantic capabilities;
7. canary new Harness;
8. dual-record usage and compare;
9. migrate user cohorts;
10. block generic API path for migrated Harnesses;
11. upgrade remaining supported clients;
12. remove public legacy model endpoint.

## 47.7 Enterprise compatibility

Do not break:
- existing org IDs;
- user IDs;
- Harness IDs where possible;
- repository/provenance IDs;
- audit records;
- policy packs.

Use mapping/migration rather than delete/recreate.

## 47.8 Government migration

For current Government derived deployment:
- import PAPER/model-catalog update offline;
- map local approved models to Catalog Models;
- keep local PIA;
- validate no public dependency;
- run conformance;
- cut Harness to PAPER-only path.

## 47.9 Support/training

Public:
- sign-in troubleshooting;
- Harness management;
- account security;
- queue/fair-use explanation.

Enterprise:
- administrators additionally receive model catalog, policy, provenance, security and SRE training.

## 47.10 Rollback

Migration rollback may restore previous PCCP service build, but must not re-enable an intentionally removed insecure/generic endpoint for new compatible Harnesses without explicit emergency decision and audit.

---

# 48. PCCP v2 Migration and Expansion Roadmap

PCCP already exists. This roadmap assumes an implemented v1 system and prioritizes safe evolution.

## M0 — Inventory, Compatibility Freeze, and Baseline (Weeks 0–2)

Deliver:
- inventory current PCCP services/schemas;
- identify existing Gateway request paths;
- inventory Harness local model/provider/base-URL configuration;
- inventory current model IDs and endpoint mappings;
- benchmark current request latency/RPS/streaming;
- snapshot Public/Enterprise/Government tests;
- define v2 migration feature flags;
- freeze new features on legacy generic Harness gateway.

Gate:
- complete dependency map and migration rollback plan.

## M1 — Shared Kernel Profiles and New Core Schemas (Weeks 2–6)

Deliver:
- `DeploymentProfile`;
- module registry/capability graph;
- `Account`, `Subscription`, `EntitlementRevision`;
- `CatalogModel`, `ModelDescriptor`, `ModelCatalogEpoch`;
- `AccountCapacityLease`;
- separate risk-domain states;
- additive event schemas;
- migration mapping for existing `Model`.

Gate:
- existing Enterprise runs unchanged using v2 schemas behind compatibility adapters.

## M2 — PAPER Model Catalog + Harness Authority Migration (Weeks 4–8)

Deliver:
- `paper.models/1`;
- catalog snapshot/delta;
- model announce/withdraw/default;
- `AI_OPEN` references Catalog Model + epoch;
- Harness model selector driven only by PCCP;
- remove/ignore local provider/base URL for v2 subscription service;
- version compatibility/min-client rules.

Gate:
- model can be added/withdrawn server-side without Harness release;
- fake local model ID/base URL cannot be used.

## M3 — PAPER AI Semantic v2 Capability Expansion (Weeks 5–12)

Deliver:
- normalized content-item model;
- multimodal input;
- ToolDescriptor;
- tool choice;
- parallel tools;
- streaming arguments;
- approval events;
- client/runtime/server tool placement;
- MCP normalization;
- structured output;
- reasoning/effort controls;
- cache directives/accounting;
- context management/compaction;
- normalized finish reasons;
- citations/sources;
- long-running/resumable status;
- capability conformance suite.

Gate:
- agreed OpenAI Responses / Anthropic Messages / AI SDK capability coverage matrix passes using internal model adapters, without exposing their wire protocols.

## M4 — Public OAuth, Subscription, and Harness Account Portal (Weeks 6–12)

Deliver:
- OAuth/OIDC PKCE flow;
- headless device flow;
- subscription integration;
- public Harness enrollment;
- self-service Harness management;
- account/session security;
- no API-key subscriber credential;
- public model catalog based on entitlement.

Gate:
- unpaid account cannot infer;
- paid account enrolls and uses PAPER end-to-end without API key/base URL.

## M5 — Relay Data Plane, Capacity Authority, and Fair Scheduler (Weeks 8–16)

Deliver:
- separate horizontally scalable PAPER Relay fleet;
- hot signed state caches;
- account-sharded Capacity Authority;
- Agent Work Slots;
- Compute Load Units v1;
- Account Capacity Leases;
- weighted fair scheduler;
- queue classes;
- model/PIA capacity routing;
- dual metering comparison.

Gate:
- multiple Harnesses cannot race beyond account slot allowance;
- Public scale load test meets latency target.

## M6 — Account Integrity, Trust & Safety, and Platform Security (Weeks 12–18)

Deliver:
- risk domain separation;
- sharing/compromise signals;
- step-up auth;
- suspicious Harness revoke;
- T&S case workflow;
- platform-attack detections;
- graduated enforcement;
- appeal/support audit.

Gate:
- test cohort demonstrates no automatic permanent ban from single IP/location anomaly.

## M7 — Public SRE, Capacity, Alerts, and Support Console (Weeks 12–20)

Deliver:
- Public operations dashboard;
- model/GPU/queue/TTFT/TPS metrics;
- Slack/email/on-call alerts;
- SLO/error budgets;
- capacity forecasting;
- support account timeline;
- canary/release annotations;
- incident runbooks.

Gate:
- simulated capacity failure produces correct alerts and operator drill-down.

## M8 — Enterprise/Government Regression and Model Catalog Integration (Weeks 16–24)

Deliver:
- enterprise org-aware catalogs;
- data-residency model availability;
- Government local/offline catalogs;
- catalog evidence;
- provenance exact PMP resolution;
- enterprise communications/security regression;
- Work Intelligence regression;
- air-gap update bundle changes.

Gate:
- same Harness binary can connect to Public, Enterprise, or Government PCCP and receives only the capabilities/catalog authorized by that deployment.

## M9 — Legacy Path Removal (After adoption threshold)

Deliver:
- telemetry shows no supported Harness uses legacy gateway;
- external generic inference route removed or firewalled internal;
- old model list/config migrated;
- deprecated environment variables/config documented;
- minimum Harness version enforced;
- security scan verifies no hidden compatibility downgrade.

Gate:
- official Harness cannot call generic OpenAI/Anthropic-compatible endpoint through a supported configuration.

## M10 — Scale, Chaos, Security Review, and Open-Source Release

Deliver:
- 100k connection/load scenarios;
- large account simulations;
- regional loss;
- capacity authority loss;
- stale catalog/revocation tests;
- fuzz PAPER AI/model catalog;
- external security review;
- reference docs;
- migration guide;
- conformance suite.

Gate:
- Definition of Done in v2 satisfied.

## Roadmap rule

Migrations are vertical slices. Do not build a large Public dashboard before:
- OAuth;
- Harness identity;
- catalog authority;
- PAPER;
- capacity/metering

are correct in the data path.

---

# 49. Cross-Product Acceptance Criteria

## A. Public authentication/subscription

1. A user without active entitlement cannot open a new inference Working Session.
2. OAuth identity alone does not authorize inference without subscription/entitlement.
3. A valid subscriber with an unregistered Harness cannot use the model service.
4. User can revoke one Harness without disabling other registered Harnesses.
5. User at Harness limit must revoke/remove one before ordinary new enrollment.
6. Account recovery revokes or rotates appropriate credentials.

## B. PAPER-only Harness path

7. Official Harness has no supported OpenAI/Anthropic-compatible subscription inference setting.
8. Setting an arbitrary `base_url` cannot redirect official Harness model traffic.
9. PAPER downgrade does not fall back to REST/WebSocket/generic provider protocol.
10. Legacy Gateway route is inaccessible to v2 Harness after migration.

## C. Model catalog

11. Harness starts without compiled model list and receives catalog after auth.
12. PCCP adds model; eligible Harness sees announcement without application release.
13. PCCP withdraws model; stale client cannot start new exchange with it.
14. Catalog Model selection references catalog epoch.
15. Entitlement-ineligible model cannot be forced by raw string.
16. Incompatible Harness sees upgrade-required or model hidden.
17. Catalog contains capabilities but no endpoint/base URL.
18. User preference survives only as Catalog Model ID and becomes invalid safely on withdrawal.

## D. Model trust

19. Generic vLLM/SGLang endpoint claiming Patty name is rejected.
20. Valid PIA with wrong PMP is rejected.
21. Expired Endpoint Lease stops new routing.
22. Model recall stops new requests and updates catalog state.
23. User-visible Catalog Model can change underlying PMP without local Harness list change.

## E. PAPER AI semantics

24. Text streaming interoperates end-to-end.
25. Tool call has stable ID and schema.
26. Strict-tool request is rejected for model that does not advertise strict capability.
27. Parallel tool calls are independently authorized.
28. Streaming tool arguments cannot execute before complete/authorized.
29. Tool result can contain normalized text/file/image where supported.
30. Server tool and client tool lifecycle are distinguishable.
31. MCP-backed tool cannot bypass PAPER policy.
32. Structured output schema is represented and validated.
33. Reasoning effort can be requested only when advertised.
34. Hidden chain-of-thought is not required or exposed.
35. Cache read/write usage is separately accounted when model reports it.
36. Context compaction event is visible/traceable.
37. Finish reasons normalize tool use, refusal, context limit, cancellation and errors.
38. Multimodal input is capability-gated.
39. Citation/source output can be represented without inventing provider-specific wire fields.
40. Background/resumable state does not duplicate side effects.

## F. Public capacity/fairness

41. Five semantic work slots may multiplex over fewer network connections.
42. Multiple Harnesses for same account cannot race beyond authoritative capacity.
43. Heavy-context workload consumes appropriate capacity class.
44. High usage may queue/throttle without changing T&S state.
45. One account cannot starve the shared model pool under load.
46. Capacity authority outage respects bounded leases and does not create unmetered unlimited work.

## G. Account integrity/security

47. Single new IP/geolocation does not permanently ban.
48. Cryptographic credential replay can trigger immediate security containment.
49. Suspicious concurrent distant Harnesses can trigger step-up auth.
50. Platform attack state is separate from account-sharing state.
51. Security research prompt alone is not automatically an account violation.
52. Confirmed abusive/resale pattern has auditable enforcement reason.

## H. SRE

53. Operator can drill TTFT regression to model/package/endpoint/region.
54. GPU 99% with healthy queue/TPS does not alone page.
55. Queue + TTFT + decode degradation crosses configured alert.
56. SEV-1 alert reaches on-call/Slack/email.
57. Model pool failure produces user-visible structured service state.
58. Regional failure demonstrates capacity/failover behavior.

## I. Enterprise/Government regression

59. Existing org/project/repo policies still apply under PAPER.
60. Enterprise model catalog is filtered by organization/project/data policy.
61. Full provenance resolves Catalog Model to exact PMP/endpoint.
62. Communications/file transfer remain governed.
63. Government local catalog works with no public Internet.
64. Signed offline model/catalog update is accepted/rejected correctly.
65. Same core schemas work across profiles.

## J. Audit/admin

66. Sensitive admin content access is audited.
67. Subscription/risk/admin enforcement history is immutable through normal UI.
68. Catalog/model change has actor/diff/rollout evidence.
69. Capacity/fair-use decisions are reason-coded.
70. Metering reconciles Relay/PIA usage.

---

# 50. Product KPIs

KPIs are separated by profile and purpose.

## 50.1 Public growth/adoption

- registered accounts;
- active subscribers;
- trial→paid conversion;
- weekly/monthly active Harness users;
- registered Harnesses/account;
- first-session success;
- retention/renewal;
- sessions/user;
- model-selection mix.

## 50.2 Public reliability

- PAPER auth success;
- Working Session success;
- request success;
- queue p50/p95/p99;
- TTFT;
- output TPS;
- cancellation;
- resumption;
- catalog delivery/announcement lag;
- model endpoint availability;
- regional SLO.

## 50.3 Capacity/efficiency

- active agent slots;
- CLU/account cohort;
- input/output/cache token rates;
- GPU utilization;
- VRAM/KV;
- prefill/decode efficiency;
- cache hit;
- cost per active subscriber;
- cost per successful task proxy where methodology exists;
- capacity headroom.

## 50.4 Fairness

- percent immediately admitted;
- queue by cohort;
- starvation incidents;
- throttled users;
- heavy-user queue outcomes;
- burst utilization;
- per-account resource concentration.

## 50.5 Account integrity

- suspicious account rate;
- step-up auth success;
- confirmed sharing;
- false-positive appeal/reversal;
- compromised-account recoveries;
- credential replay;
- Harness churn.

## 50.6 Trust & Safety / security

- cases by category;
- time to review;
- enforcement;
- appeals;
- platform attacks;
- protocol violations;
- exploit attempts;
- critical incident containment.

Do not reward a safety system for high ban count.

## 50.7 Model/catalog

- model announcement success;
- upgrade-required rate;
- default model adoption;
- canary outcomes;
- capability conformance failures;
- model recall propagation;
- per-model reliability.

## 50.8 Enterprise

Retain v1:
- AI adoption;
- engineering outcome;
- quality/rework;
- security/governance;
- provenance coverage;
- communications;
- Work Intelligence;
- commercial expansion.

## 50.9 Business

Public:
- ARR/MRR;
- subscriber retention;
- plan margin/cohort GPU cost;
- support burden.

Enterprise:
- licensed vs active seats;
- expansion;
- model/GPU consumption;
- renewal;
- private deployments;
- advanced module attach.

## 50.10 Metric governance

No KPI silently becomes:
- an individual employee score;
- a permanent account-abuse determination;
- a public product claim without validated methodology.

---

# 51. Key Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Public scale overwhelms v1 CP | outages | Relay data plane, hot caches, sharded capacity authority |
| PCCP becomes four products | maintenance failure | kernel + DeploymentProfile + modules |
| Harness local model config survives | generic endpoint bypass/inconsistency | server catalog, remove base URL/provider path |
| PAPER lacks modern model features | Harness regressions | semantic IR + capability coverage/conformance |
| PAPER becomes vendor API clone | design/maintenance trap | provider-neutral semantics, own wire model |
| Catalog descriptor lies about model capability | runtime errors | conformance tests + adapter + eval-derived capability |
| Stale catalog uses recalled model | safety/quality incident | epoch validation + push withdrawal + server enforcement |
| Subscriber shares account | cost/margin | Harness registry + multi-signal integrity + capacity |
| Legit traveler falsely flagged | trust/support damage | no single-signal ban; step-up/human review |
| "Unlimited" causes unbounded GPU cost | margin/SLO | fair scheduler, semantic work slots, CLU, queueing |
| Aggressive fair-use harms power users | churn | generous burst, transparent queue, cohort analysis |
| Capacity state becomes punitive T&S | user harm | separate state machines |
| Account capacity races across Relays | overuse | account-sharded allocator + short leases |
| Hot caches accept stale revoked authority | abuse | bounded TTL + revocation epochs |
| Metering loss | billing/fairness failure | durable Relay/PIA events + reconciliation |
| Provider/base URL hidden escape hatch | policy failure | code/config audit + network tests + legacy removal |
| vLLM/SGLang adapter lag | model rollout delay | PIA adapter boundary, no permanent fork |
| Model capability fragmentation | client complexity | ModelDescriptor + version negotiation |
| Tool semantics permit authority escalation | security | tool proposal external authorization |
| Generic MCP bypass | exfiltration | MCP through Tool Broker/PAPER |
| Public provenance stores too much code | privacy/breach | operational trace profile |
| Enterprise admin privacy risk | employee/legal risk | purpose/role/audit controls |
| Work Intelligence surveillance | adoption/legal risk | retain v1 guardrails |
| Bifrost-inspired in-memory design loses state | inconsistency | signed source of truth + durable event journal + convergence tests |
| Too many in-process modules hurt reliability | crashes/latency | static hot kernel + service/async boundaries |
| QUIC blocked on some networks | connectivity | TLS/TCP PAPER fallback, no HTTP downgrade |
| Government diverges | product fork | same PAPER/catalog, local authorities/offline bundles |

---

# 52. Explicit Non-Goals for Initial v2 Releases

PCCP v2 will not:

- become an unrestricted multi-provider gateway for Patty Code;
- expose Public subscription as a generic API key;
- support user-configured OpenAI/Anthropic-compatible base URLs in official Harness service path;
- maintain a hard-coded Harness model catalog;
- invent new cryptographic primitives;
- require hardware attestation for ordinary Public enrollment;
- guarantee prevention of a fully host-compromised open-source Harness from being modified;
- infer account sharing from IP alone;
- treat heavy legitimate use as abuse by definition;
- make Trust & Safety judgments solely from raw token volume;
- guarantee "unlimited" means infinite concurrency/compute;
- copy OpenAI Responses or Anthropic Messages wire formats into PAPER;
- require every current/future provider-specific feature; only announced PAPER capabilities are supported;
- expose hidden model chain-of-thought;
- make MCP an alternate inference protocol;
- replace vLLM/SGLang with a custom inference engine;
- replace Git/CI/HR/SIEM systems where integration is sufficient;
- turn Public PCCP into full enterprise employee provenance;
- create a separate Government codebase;
- autonomously make employment decisions.

---

# 53. Open Product and Tuning Decisions

Architecture is locked by this PRD; the following remain tunable.

## Public commercial/operations
1. launch number of registered Harnesses (working assumption 3);
2. active Harness maximum;
3. agent/heavy/background slot counts;
4. CLU coefficients/windows;
5. queue priority by subscription;
6. fair-use public wording;
7. trial/grace behavior;
8. account-sharing enforcement thresholds;
9. default content retention.

## Catalog/model
10. public Catalog Model naming;
11. whether exact PMP is exposed in public diagnostics;
12. model default migration policy;
13. canary assignment strategy;
14. how much temporary capacity state appears in model selector;
15. default/fallback model semantics.

## PAPER
16. exact extension version for model catalog;
17. exact AI Semantic IR schema;
18. structured-output JSON Schema subset;
19. server-tool taxonomy;
20. context compaction primitive;
21. cache directive semantics;
22. opaque reasoning state representation;
23. live audio/realtime beyond asynchronous voice.

## Infrastructure
24. account capacity allocator technology;
25. Relay regional topology;
26. queue/scheduler implementation;
27. event-bus/OLAP choices;
28. Public SLO targets after benchmark;
29. GPU pool partitioning.

## Enterprise/Government
30. transcript default;
31. hardware attestation product tier;
32. Work Intelligence packaging;
33. managed Git timeline;
34. external chat bridges.

These decisions must not reopen:
- one-kernel architecture;
- PAPER-only official Harness service path;
- PCCP-authoritative model catalog;
- no arbitrary provider/base URL;
- PIA/PMP endpoint trust.

---

# 54. Definition of Done

PCCP v2 is complete when:

1. One PCCP kernel supports Public, Enterprise, and Sovereign profiles through module/deployment configuration rather than code forks.
2. Public subscriber can install Harness, OAuth sign in, verify subscription, enroll Harness, receive PAPER credential and begin work without API key.
3. Harness does not contain authoritative model list.
4. PCCP sends effective model catalog/capabilities over PAPER.
5. Online model add/default/deprecation/withdrawal works without ordinary Harness release.
6. Raw model ID/base URL/provider config cannot route subscriber traffic.
7. Official Harness has no supported OpenAI/Anthropic-compatible inference downgrade.
8. PAPER AI semantic layer supports modern coding-agent requirements: streaming, rich tools, approvals, parallel calls, structured output, multimodal capability negotiation, cache accounting, context management, normalized finish reasons and usage.
9. PIA remains the only approved bridge to local serving engine.
10. PCCP validates Catalog Model→PMP→Endpoint identity.
11. Public Account/Subscription/Harness/Session/Work-Slot authority is independently modeled.
12. Capacity uses semantic workload slots rather than socket count.
13. Account Capacity Leases prevent multi-Relay concurrency race.
14. Fair scheduler protects shared service from a small number of highly parallel accounts.
15. Heavy legitimate usage can be queued without being classified as abuse.
16. Account Integrity, Trust & Safety, Platform Security, and Capacity are separate states.
17. Public SRE console exposes tokens/cache/queues/TTFT/TPS/GPU/KV/endpoints and risk/service health.
18. SLO-based alerts reach Slack/email/on-call.
19. Public content retention defaults to operational/minimized profile.
20. Enterprise Governance/Security/Provenance/Comms/Work Intelligence features from v1 continue to pass regression.
21. Enterprise model availability is derived from organization/project/data/model policy.
22. Full enterprise provenance resolves user-visible Catalog Model to exact PMP/PIA endpoint.
23. Government deployment uses same PAPER/model catalog locally with no mandatory public Internet.
24. Legacy v1 generic Harness gateway path is removed from supported client surface.
25. Bifrost-inspired gateway optimizations are implemented as architecture patterns without adopting API-key/provider compatibility product semantics.
26. Model/capability conformance tests prevent advertising unsupported functionality.
27. Public and Enterprise metering reconcile from authenticated Relay/PIA events.
28. Account enforcement is explainable and auditable.
29. A modified/forked open-source client limitation is documented without false security claims.
30. The complete product can still be accurately summarized as: **PCCP is the source of authority for Patty Code identity, model capabilities, inference access, resource governance, and profile-specific control.**

---

# Appendix A. PAPER Model Routing and Endpoint Trust — Reference Design

This appendix supersedes the v1 Harness→HTTP Gateway request sequence.

## A.1 Components

- **Catalog Service** — user-visible ModelDescriptors and effective catalogs.
- **PCCP Policy/Entitlement** — determines whether model is eligible.
- **PAPER Relay** — inline governed data plane.
- **Model Registry** — PMP and approval.
- **Endpoint Registry** — PIA endpoints.
- **PIA** — PAPER inference peer.
- **Serving Engine** — local vLLM/SGLang/etc.
- **Model Artifact Store** — signed/encrypted PMP.
- **KMS/Key Broker** — optional.
- **Capacity/Scheduler** — selects eligible endpoint.

## A.2 Harness catalog sequence

```text
Harness                     PAPER Relay / PCCP
   │                              │
   │── PAPER auth/user bind ─────►│
   │                              │
   │◄─ MODEL_CATALOG_SNAPSHOT ────│
   │      catalog_epoch=184       │
   │      patty-code-standard     │
   │      patty-code-fast         │
   │                              │
   │── AI_OPEN ──────────────────►│
   │   model=patty-code-standard  │
   │   catalog_epoch=184          │
   │                              │
```

Harness never receives a serving URL.

## A.3 Relay resolution

```text
Catalog Model
     ↓ entitlement/policy
eligible PMP set
     ↓ health/canary/residency
Endpoint Lease set
     ↓ scheduler
selected PIA
```

## A.4 PIA sequence

```text
Relay                         PIA                    vLLM/SGLang
  │                            │                         │
  │── PAPER INFERENCE_REQUEST ►│                         │
  │     exact PMP + lease      │                         │
  │                            │── local adapter ───────►│
  │                            │◄── stream ──────────────│
  │◄════ PAPER token/tool ═════│                         │
```

## A.5 No name trust

The following are insufficient:
- hostname;
- IP;
- catalog name;
- `/models`;
- model self-report;
- output fingerprint.

Endpoint identity requires PIA credential + Endpoint Lease; model identity requires approved PMP.

## A.6 Endpoint enrollment

Retain v1 sequence:
1. deploy signed PIA;
2. obtain workload/peer identity;
3. verify PMP signature/digests;
4. verify serving configuration;
5. optionally collect host/GPU evidence;
6. enroll endpoint;
7. challenge/verify;
8. issue short-lived Endpoint Lease;
9. re-attest/revalidate on material changes.

## A.7 Assurance levels

- L1 Software Verified;
- L2 Host Attested;
- L3 Confidential/Hardware Attested.

Public Patty Cloud may use Patty-operated controls without exposing this complexity to user.

## A.8 Recall sequence

```text
Authorized model recall
   ↓
PMP suspended / Endpoint Leases invalidated
   ↓
Catalog Model status changed or underlying package replaced
   ↓
Relay stops new affected routes
   ↓
MODEL_WITHDRAW / MODEL_CAPABILITY_CHANGED sent to Harness
   ↓
existing exchanges handled by recall policy
```

## A.9 Legacy engine-compatible API

A local PIA adapter may use:
```text
localhost-only OpenAI-compatible endpoint
```
if the serving engine exposes it.

That endpoint:
- is not the PCCP public API;
- is not announced in PAPER catalog;
- has no subscriber API key;
- is unreachable from normal Harness network;
- cannot be configured by the subscriber.

## A.10 Air-gap

Local PCCP is catalog/endpoint authority. Catalog/PMP/PIA updates are delivered through signed offline bundles. No public Patty discovery endpoint is required.

---

# Appendix B. Provenance Data Model — Reference

## B.1 Provenance span

```yaml
provenance_span_id: psp_...
repository_id: repo_...
file_path: src/auth/AuthService.java
commit_sha: ...
symbol:
  language: java
  qualified_name: com.example.AuthService.login
location:
  start_line: 81
  end_line: 104
fingerprints:
  ast: ...
  semantic: ...
attribution:
  state: AI_THEN_HUMAN_EDITED
  confidence: 0.94
origin:
  session_id: ses_...
  user_id: usr_...
  harness_id: hrn_...
  model_package_id: pmp_...
  endpoint_id: ep_...
  change_set_id: chg_...
context_refs: [...]
tool_call_refs: [...]
policy_decision_refs: [...]
test_refs: [...]
review_refs: [...]
parent_span_refs: [...]
evidence_bundle_id: evb_...
```

## B.2 Provenance graph relationships

```text
User ─uses→ Harness ─creates→ Session
Session ─bound_to→ RepoBaseline
Session ─contains→ PromptExchange
PromptExchange ─uses→ ContextItem
PromptExchange ─routed_to→ ModelPackage/Endpoint
Session ─requests→ ToolCall
ToolCall ─produces→ ChangeSet
ChangeSet ─maps_to→ ProvenanceSpan
ProvenanceSpan ─included_in→ Commit
Commit ─reviewed_by→ Reviewer
Commit ─verified_by→ Test/Scan
All ─evidenced_by→ EvidenceBundle
```

---

---

# Appendix C. Work Intelligence Example Scorecard

> Example only. Customers must configure job-family-appropriate rubrics and review applicable employment/privacy requirements.

| Dimension | Weight | Example evidence | Do not substitute |
|---|---:|---|---|
| Delivery outcomes | 30% | completed accepted features/issues, cycle outcome, avoidable rework | LOC/prompts |
| Engineering quality | 25% | tests, defects/reverts, review outcome, maintainability | raw commit count |
| Security/governance | 20% | serious/repeat findings, approval adherence, secure dependencies | absence of any warning alone |
| AI effectiveness | 15% | task completion, context discipline, verification, rework, tool use | token count/AI acceptance alone |
| Collaboration/learning | 10% | reviews, handoffs, docs, improvement trend | chat volume |

### C.1 Data completeness indicator

Every scorecard should display:

- SCM integration coverage,
- issue/task integration coverage,
- CI/test coverage,
- incident integration coverage,
- time window,
- excluded repositories,
- incomplete/missing data warnings.

### C.2 Recommended scale

Prefer dimension ratings such as:

- `Insufficient Evidence`,
- `Needs Development`,
- `Meets Expectations`,
- `Strong`,
- `Exceptional`,

with evidence and manager rationale rather than a misleading `87.32/100` precision.

---

---

# Appendix D. Administrative Permission Matrix — Example

| Capability | Org Admin | Platform Ops | Security | Project Owner | HR/Manager | Auditor | GPU Ops |
|---|---:|---:|---:|---:|---:|---:|---:|
| Manage users/groups | ✓ |  |  |  |  | read |  |
| Manage Harness enrollment | ✓ | ✓ | revoke |  |  | read |  |
| View live metadata | ✓ | ✓ | ✓ | scoped | scoped | read | endpoint only |
| View prompt/source content | optional | no default | JIT/scoped | scoped | no default | JIT | no |
| Contain session | optional | ✓ | ✓ | scoped pause | no | no | endpoint drain only |
| Change policy | approve | deploy | author/approve | project overlay | no | read | no |
| View communications body | no default | no | investigation JIT | room member | no default | JIT | no |
| Broadcast | authorized role | ops | security | scoped | scoped | no | no |
| Work Intelligence individual | no default | no | security metrics only | scoped if manager | ✓ | read/JIT | no |
| Model approve | optional | deploy only | security review | no | no | read | deploy approved only |
| View GPU telemetry | summary | ✓ | summary | summary | no | read | ✓ |
| Export evidence | optional | ops | ✓ | scoped | eval only | ✓ | no |

Actual permissions are customer-defined; separation of duties should be enforceable.

---

---

# Appendix E. Profile and Module Matrix

| Capability | Patty Public Cloud | Enterprise | Government/Sovereign |
|---|---:|---:|---:|
| Patty Code Harness | ✓ | ✓ | ✓ |
| PAPER-only service channel | ✓ | ✓ | ✓ |
| Server-authoritative Model Catalog | ✓ | ✓ policy-filtered | ✓ local/offline |
| User OAuth | ✓ | optional | local/SSO/PKI |
| Enterprise SSO/SCIM | — | ✓ | ✓ |
| Harness enrollment | ✓ | ✓ | ✓ |
| Subscription entitlement | ✓ | ✓/commercial | offline/local |
| Account Capacity Lease | ✓ | optional/shared capacity | optional |
| Fair-use/account integrity | ✓ | customer policy if needed | usually not consumer-style |
| Trust & Safety | Patty service | customer governance | agency policy |
| Public SRE console | ✓ Patty internal | managed-service views | local ops |
| PAPER Relay | Patty | Patty/customer | local |
| PIA/model access | Patty cloud | cloud/private/hybrid | local |
| Generic provider/base URL in official Harness | **No** | **No** | **No** |
| Exact PMP endpoint trust | ✓ | ✓ | ✓ |
| Full Git provenance | minimal/personal if offered | ✓ | ✓ |
| Security/DLP/PII | service safety/minimal | full | full/strict |
| Enterprise communication | notices | ✓ | ✓ local |
| Broadcast | service notices | ✓ | ✓ |
| Work Intelligence | — | optional | optional/policy |
| Chargeback | internal Patty | ✓ | local |
| Air-gap | — | special | ✓ |
| Offline catalog/model update | — | optional | ✓ |
| Customer KMS/HSM | — | private | ✓ |
| Government policy packs | — | optional | ✓ |

---

# Appendix F. Relationship to the GongCode Master Plan

The GongCode master plan remains the security/government baseline. It already defines the product as more than a coding assistant: a governed engineering system combining a Harness, model/GPU platform, disposable execution, deterministic policy, real-time administration/security operations, line-level provenance, and evidence. It also establishes the important separation between **coding intelligence**, **execution authority**, and **governance evidence**.

Patty Code CP should reuse that architecture rather than create a separate enterprise stack. This PRD adds or materially expands:

- independent Harness identity and fleet lifecycle,
- Patty-only cryptographically attested inference endpoints,
- commercial entitlements/billing/chargeback,
- enterprise organization/affiliate/contractor hierarchy,
- contextual secure communications,
- file transfer and task handoff,
- broadcast/emergency messaging,
- Work Intelligence and evidence-backed evaluation workflows,
- executive/adoption analytics,
- Shadow AI discovery integration,
- enterprise release/fleet management,
- explicit deployment profile unification across Individual, Enterprise, and Government.

The existing GongCode plan's policy, sandbox, security, model registry, provenance, evidence, compliance, and air-gap work should be treated as shared core requirements, not independently reimplemented features.

---

---

# Appendix G. External Technical and Product Baseline for v2 Design Review

This appendix records the external systems studied for **capability coverage and architecture patterns**. It does not imply code copying or wire compatibility.

## G.1 OpenAI Responses and Models

Coverage reviewed:
- model list/discovery;
- text/image/file input;
- streaming output events;
- function/custom tool calling;
- tool choice;
- allowed tool subsets;
- parallel tool calls;
- MCP tools;
- shell/apply-patch style tool classes;
- structured output;
- reasoning controls;
- continuation;
- usage and cache fields.

Product decision:
- PAPER should represent comparable general semantic capabilities where relevant;
- official Harness does not speak OpenAI wire protocol;
- OpenAI model endpoint shapes are not copied.

## G.2 Anthropic Messages and Models

Coverage reviewed:
- model capability metadata including input/citation/structured output/thinking/context management;
- client tool use / tool result;
- server tools;
- pause/continuation;
- strict tool schema;
- structured outputs;
- streaming content/tool deltas;
- prompt caching/cache usage;
- stop reasons;
- citations.

Product decision:
- Anthropic's rich model capability object is a useful precedent for dynamic server-side model announcements;
- PCCP ModelDescriptor is Patty-specific and broader in entitlement/runtime policy.

## G.3 AI SDK

Coverage reviewed:
- provider-neutral abstraction;
- tool input schemas;
- strict mode;
- tool choice;
- active tools;
- multi-step loops;
- approvals;
- dynamic tools;
- MCP;
- multimodal tool results;
- normalized generation/stream usage.

Product decision:
- a provider-neutral semantic layer is feasible and desirable;
- PAPER AI IR serves this role at the protocol layer;
- AI SDK itself is not a protocol dependency.

## G.4 Bifrost

Repository studied: open-source Go Bifrost project.

Useful architecture patterns:
- high-throughput gateway focus;
- hot critical state in memory;
- hierarchical budgets/rate limits;
- governance-aware routing;
- adaptive load balancing;
- stable pre-request/per-attempt/post-response plugin phases;
- cluster state/counter synchronization;
- HA/health/capacity concerns.

Not adopted:
- Virtual Key as public user principal;
- multi-provider compatibility headers;
- user/provider API keys for Patty subscription;
- OpenAI/Anthropic-compatible client contract.

## G.5 Current subscription coding-service patterns

Industry services commonly:
- authenticate a user account/plan;
- apply plan/fair-use/concurrency constraints;
- allow multiple client devices within account policy;
- prohibit account sharing/resale;
- expose available models dynamically.

PCCP v2 uses these as product references while implementing its own identity/PAPER/capacity model.

## G.6 Korean governance baseline

Retain v1 requirements to validate current:
- 개인정보 보호법 and applicable employee/data rules;
- ISMS-P;
- KISA/MOIS secure development guidance;
- AI Basic Act/Enforcement Decree;
- KCMVP-aware profiles where applicable;
- eGovFrame/public-sector mappings for Government.

PCCP provides controls/evidence, not automatic legal certification.

## G.7 Source refresh

Because APIs and model capabilities evolve quickly:
- capability coverage review occurs at least quarterly and before major PAPER AI revisions;
- sources must be official/current;
- additions do not automatically become mandatory PAPER features;
- product team decides whether they represent a broadly useful semantic capability or vendor-specific behavior.

---

# Appendix H. Immediate PCCP v2 Modification Slice

Because PCCP is already built, the first demonstrable v2 milestone should prove the **new authority boundary**, not rebuild enterprise features.

## H.1 First modification slice

1. Add `CatalogModel`, `ModelDescriptor`, `CatalogEpoch`.
2. Map current production Patty model to one Catalog Model.
3. Implement `paper.models` snapshot.
4. Modify Harness model selector to render only snapshot.
5. Remove/disable user custom base URL/provider config for Patty service.
6. Modify `AI_OPEN` to send Catalog Model ID + epoch.
7. Relay validates catalog and maps to current PMP.
8. Use existing PIA/Endpoint Lease.
9. Add Public `Account`/Subscription/Entitlement if not already present.
10. Add three-Harness registration policy behind config.
11. Add five-work-slot account policy behind config.
12. Record normalized input/output/cache usage.
13. Add minimal Public operations page.
14. Run same Enterprise model request regression.

## H.2 First v2 demonstration

```text
A subscriber installs Patty Code on a new Mac.
They sign in through the browser.
PCCP recognizes an active subscription.
The Mac is enrolled as Harness #2.
PAPER authenticates.
PCCP sends a model catalog containing only models included in that account.
The user selects Patty Code Standard.
The Harness sends only the Catalog Model ID—not an endpoint.
Relay resolves the exact signed PMP and a healthy PIA endpoint.
The user launches several subagents within the account's work-slot policy.
PCCP meters input/output/cache usage and shows queue/TTFT/GPU health.
An operator withdraws the model.
The Harness receives a model withdrawal event and cannot open a new exchange against the stale model.
At no point does the user configure or receive an OpenAI/Anthropic-compatible base URL/API key.
```

This proves:
- Public profile;
- server-authoritative model discovery;
- PAPER-only path;
- subscription/Harness authority;
- PIA trust;
- semantic concurrency;
- live operations.

## H.3 Second slice

After that:
- expand PAPER AI tool/structured/multimodal semantic coverage;
- Account Capacity Lease;
- fair scheduler;
- account integrity;
- full SRE/alerting;
- migration of all Public traffic;
- Enterprise/Government regression.
