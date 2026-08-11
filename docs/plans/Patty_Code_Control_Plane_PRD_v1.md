# Patty Code Control Plane (CP)
## Enterprise & Government Product Requirements Document

**Document status:** Product Requirements Document (PRD) — Draft v1.0  
**Working product name:** **Patty Code Control Plane (CP)**  
**Primary market:** Republic of Korea — 중소기업, 중견기업, 대기업, public institutions and government  
**Secondary market:** International enterprise and regulated organizations  
**Primary language:** Korean-first; complete English support  
**Prepared:** 2026-08-11  
**Product family:** Patty Code Harness + Patty Code Control Plane + Patty Models  
**Architecture principle:** One product codebase, multiple deployment/security profiles — **not three independent products**

---

## Contents

- [0. Executive Decision Summary](#0-executive-decision-summary)
- [1. Product Family and Edition Strategy](#1-product-family-and-edition-strategy)
- [2. Vision, Positioning and Product Promise](#2-vision-positioning-and-product-promise)
- [3. Foundational Product Principles](#3-foundational-product-principles)
- [4. Goals and Non-Goals](#4-goals-and-non-goals)
- [5. Target Customers and Personas](#5-target-customers-and-personas)
- [6. Control Plane Information Architecture](#6-control-plane-information-architecture)
- [7. Overview Dashboard Requirements](#7-overview-dashboard-requirements)
- [8. Core Identity Model](#8-core-identity-model)
- [9. Trusted Model Endpoint Architecture](#9-trusted-model-endpoint-architecture--critical-requirement)
- [10. Gateway Requirements](#10-gateway-requirements)
- [11. Model Registry and Lifecycle](#11-model-registry-and-lifecycle)
- [12. Organization, Tenancy, and Korean Enterprise Hierarchy](#12-organization-tenancy-and-korean-enterprise-hierarchy)
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
- [24. Work Intelligence](#24-work-intelligence-engineering-and-ai-use-analytics)
- [25. Work Intelligence Rubric and Scorecards](#25-work-intelligence-rubric-and-scorecards)
- [26. Employment-Decision Guardrails and Review Workflow](#26-employment-decision-guardrails-and-review-workflow)
- [27. Privacy, Administrative Visibility, and Content Access](#27-privacy-administrative-visibility-and-content-access)
- [28. Engineering, Adoption, and Executive Analytics](#28-engineering-adoption-and-executive-analytics)
- [29. Usage, Entitlements, Billing, and Chargeback](#29-usage-entitlements-billing-and-chargeback)
- [30. Model and GPU Operations](#30-model-and-gpu-operations)
- [31. Runtime and Sandbox Control](#31-runtime-and-sandbox-control)
- [32. Enterprise Integration Requirements](#32-enterprise-integration-requirements)
- [33. Korean Enterprise-Specific Differentiators](#33-korean-enterprise-specific-differentiators)
- [34. Deployment Architecture and Editions](#34-deployment-architecture-and-editions)
- [35. Security Architecture and Threat Model](#35-security-architecture-and-threat-model)
- [36. Cryptography and Key Management](#36-cryptography-and-key-management)
- [37. Data Architecture](#37-data-architecture)
- [38. API Requirements](#38-api-requirements)
- [39. Event Model and Event Topics](#39-event-model-and-event-topics)
- [40. Audit, Evidence, Retention, and Legal Hold](#40-audit-evidence-retention-and-legal-hold)
- [41. Korean Governance and Compliance Packs](#41-korean-governance-and-compliance-packs)
- [42. Open Source Strategy and Trust Boundary](#42-open-source-strategy-and-trust-boundary)
- [43. Non-Functional Requirements](#43-non-functional-requirements)
- [44. Korean-First UX and Administration](#44-korean-first-ux-and-administration)
- [45. Reporting and Scheduled Outputs](#45-reporting-and-scheduled-outputs)
- [46. Product Administration and Change Management](#46-product-administration-configuration-and-change-management)
- [47. Onboarding and Customer Rollout](#47-onboarding-and-customer-rollout)
- [48. Implementation Roadmap](#48-implementation-roadmap)
- [49. Cross-Product Acceptance Criteria](#49-cross-product-acceptance-criteria)
- [50. Product KPIs](#50-product-kpis)
- [51. Key Risks and Mitigations](#51-key-risks-and-mitigations)
- [52. Explicit Non-Goals for Initial Releases](#52-explicit-non-goals-for-initial-releases)
- [53. Open Product Decisions](#53-open-product-decisions)
- [54. Definition of Done](#54-definition-of-done)
- [Appendix A. Model Endpoint Attestation Protocol](#appendix-a-model-endpoint-attestation-protocol--reference-design)
- [Appendix B. Provenance Data Model](#appendix-b-provenance-data-model--reference)
- [Appendix C. Work Intelligence Example Scorecard](#appendix-c-work-intelligence-example-scorecard)
- [Appendix D. Administrative Permission Matrix](#appendix-d-administrative-permission-matrix--example)
- [Appendix E. Edition Matrix](#appendix-e-edition-matrix)
- [Appendix F. Relationship to the GongCode Master Plan](#appendix-f-relationship-to-the-gongcode-master-plan)
- [Appendix G. External Technical and Regulatory Baseline](#appendix-g-external-technical-and-regulatory-baseline-for-design-review)
- [Appendix H. Immediate Product Decisions and First Build Slice](#appendix-h-immediate-product-decisions-and-first-build-slice)

---

# 0. Executive Decision Summary

Patty Code Control Plane (CP) is **not an LLM gateway product**. It contains a model gateway because model traffic must be authenticated, authorized, inspected, metered, routed, and attributed, but gateway functionality is infrastructure beneath the actual product value.

The product's primary purpose is to become the **control, governance, security, provenance, administration, communications, and engineering-intelligence plane for enterprise AI-assisted software development**.

The key value proposition is:

> **Every employee, harness, prompt, model request, tool action, file change, code span, commit, security decision, policy exception, message, model endpoint, and AI-assisted engineering outcome is authenticated, governed, observable, attributable, and reviewable from one control plane.**

CP should answer, in seconds:

- Who is connected right now?
- From which registered harness and device?
- What repository, branch, task, file, and code span are they working on?
- What did the human ask AI to do?
- What context was disclosed to the model?
- Which exact approved Patty model artifact handled the request?
- Where did the inference run?
- Which files did the model read or edit?
- Which lines were AI-authored, AI-modified, human-authored, or human-modified after AI generation?
- What commands, tools, MCP servers, APIs, packages, and network destinations were requested?
- What was allowed, transformed, approved, denied, or escalated?
- Were secrets, PII, source-code classifications, licenses, insecure patterns, or prompt-injection attempts detected?
- What Git commit, PR/MR, ticket, feature, test, build, or deployment resulted?
- What is the engineering impact of the change?
- How much AI was used, how effectively was it used, and what was the quality of the resulting work?
- What did the activity cost in tokens, GPU time, model capacity, or subscription entitlement?
- Can an authorized reviewer reproduce the evidence later?

CP is therefore best understood as six systems combined into one coherent product:

1. **AI Engineering Control Plane** — organization, users, harnesses, sessions, permissions, projects, repositories, models, tools and runtime control.
2. **AI Security & Governance Platform** — policies, DLP, PII/secrets, injection defense, tool/MCP/network control, approvals, incidents, compliance mappings.
3. **AI Provenance & Software Lineage Platform** — prompt-to-code-to-commit attribution and evidence.
4. **Enterprise Engineering Operations Console** — real-time operational visibility, GPU/model health, usage, budgets, reports, investigations and controls.
5. **Engineering Communications Hub** — presence, 1:1/group messaging, governed file transfer, broadcasts and emergency notices delivered directly into the coding harness.
6. **Engineering Work Intelligence Platform** — evidence-backed productivity, quality, security, AI-usage and contribution analytics, with strong controls when used for employment evaluation.

The gateway is a **required subsystem**, not the commercial identity of the product.

---

# 1. Product Family and Edition Strategy

## 1.1 One product, three consumption profiles

Patty should not build three unrelated products. The same Harness protocol, CP control model, provenance schema, policy engine, endpoint identity system, message model, event model and core backend services should serve all editions.

| Profile | Customer | What they receive | Model location | CP location | Internet dependency |
|---|---|---|---|---|---|
| **Individual / Public** | Individual developers | Patty Code Harness + Patty-hosted model access | Patty cloud | Patty service-internal CP; no customer admin console | Required for service use |
| **Enterprise** | 중소기업 / 중견기업 / 대기업 | Harness + customer-visible CP | Patty cloud, customer GPU, or hybrid | Patty-managed SaaS, customer private cloud, or on-prem | Optional depending deployment |
| **Government / Sovereign** | Government, public institutions, restricted environments | Same Harness + same CP core with strict security profile | Customer/government GPU | On-prem / closed network / air-gapped | Must support none |

The Government profile is an **edition/configuration and deployment profile**, not a forked product. Enterprise customers must also be able to purchase the same sovereign controls if they require them.

## 1.2 Profile differences

The differences between Enterprise and Government should primarily be **policy defaults and allowed deployment topologies**, not feature code.

### Enterprise defaults

- Patty cloud model subscription is allowed.
- On-prem/private GPU is allowed.
- Hybrid routing is allowed by data classification.
- Patty-managed CP SaaS is allowed.
- Vendor telemetry is opt-in/configurable.
- Online entitlement validation may be used.
- Online software/model updates may be used.
- Standard corporate SSO, MDM, SIEM, Git and collaboration integrations.
- Customer can choose visibility/retention levels.

### Government / Sovereign defaults

- Public Patty cloud inference is disabled unless a separately approved government cloud profile explicitly permits it.
- Customer/government-controlled model infrastructure is mandatory for restricted modes.
- All core services can run without public internet.
- Local identity, local PKI, local KMS/HSM and local audit stores are supported.
- Signed offline updates are mandatory for air-gap deployments.
- No required external telemetry, licensing callback or cloud control dependency.
- Stricter device, harness, endpoint and model attestation.
- Stronger network segmentation and fail-closed requirements.
- Local package/document/model mirrors.
- Higher default retention and evidence requirements where customer policy requires them.

## 1.3 Product modules

Customer-facing modules should remain parts of **Patty Code Control Plane**, not independent products unless commercial strategy later requires it.

- **Control** — unified admin console and API.
- **Identity** — users, service identities, harnesses, devices and workload identities.
- **Gateway** — governed inference path to approved Patty model endpoints.
- **Registry** — models, endpoints, harness builds, policy packs, tools, MCP servers and trusted artifacts.
- **Guard** — security enforcement and policy decision services.
- **Trace** — AI provenance and software lineage.
- **Git/SCM Connect** — Git and software-development-system integration.
- **Comms** — presence, chat, file transfer and broadcasts.
- **Work Intelligence** — engineering analytics and optional employee-evaluation support.
- **Evidence** — immutable/tamper-evident event and evidence storage.
- **Usage & Billing** — seats, entitlements, tokens, GPU usage, budgets and chargeback.
- **Runtime Control** — local/remote execution controls and sandbox orchestration where enabled.

---

# 2. Vision, Positioning and Product Promise

## 2.1 Vision

Make AI-assisted software development acceptable inside Korean organizations that want the productivity of modern coding agents without giving up organizational control.

## 2.2 Korean-market positioning

Suggested positioning language:

> **기업이 통제할 수 있는 AI 개발 환경**  
> 개발자는 AI로 더 빠르게 개발하고, 조직은 사용자·모델·소스코드·보안·비용·출처를 하나의 Control Plane에서 관리합니다.

Alternative enterprise-oriented message:

> **AI 개발의 모든 행위를 보이게 하고, 통제하고, 증명합니다.**

The product should feel designed for organizations that expect administrators to have deep operational control:

- organizational hierarchy,
- central policy,
- auditability,
- data residency,
- controlled model use,
- employee/device registration,
- project and repository permissions,
- approvals,
- reporting,
- broadcast communication,
- usage/cost accountability,
- security incident response,
- and engineering performance visibility.

## 2.3 Principal promise

> **Developers retain a fast terminal/IDE agent experience while the organization receives complete, policy-governed visibility from user identity to model execution to the surviving lines of code.**

## 2.4 What CP is not

CP is not primarily:

- a LiteLLM/Bifrost competitor;
- a general OpenAI-compatible multi-provider proxy;
- a generic observability dashboard;
- an employee surveillance product;
- a replacement for GitHub/GitLab by default;
- a replacement for Slack/Teams/Works by default;
- a SIEM;
- an EDR;
- a full HRIS;
- a general model marketplace;
- or an unrestricted proxy to arbitrary customer-specified inference URLs.

It may integrate with or provide narrow capabilities overlapping these categories where necessary to create one governed AI engineering system.

---

# 3. Foundational Product Principles

1. **The gateway is subordinate to governance.** Routing exists to serve authenticated Patty Code sessions and approved Patty model workloads.
2. **Identity exists at multiple layers.** A person, harness installation, device, CP workload, model endpoint and model artifact all have separate identities.
3. **No trust by model name.** A string such as `patty-coder-32b` proves nothing.
4. **No trust by conversational behavior.** Behavioral signatures are useful for monitoring, not identity.
5. **Cryptographic identity over obscurity.** The product may use a Patty-specific protocol, but security cannot depend on the protocol being secret.
6. **Model identity is artifact identity.** Model weights, adapters, tokenizer, configuration, quantization, serving image and runtime profile are identified by signed content digests.
7. **The CP routes only to attested endpoints.** An arbitrary IP/URL never becomes trusted merely because an administrator typed it into a form.
8. **Users and harnesses are independently revocable.** Disabling a user should not be the only way to disable a compromised machine, and vice versa.
9. **Git state is a first-class coordinate.** Repository, branch, commit, file, symbol and code span are embedded into session and provenance models.
10. **Every protected action becomes an event.** Events are correlated, signed where required and durable enough to produce evidence.
11. **Admin visibility is deep but authorized.** The product should expose extensive operational detail while enforcing role, purpose, scope and audit controls for sensitive content.
12. **Provenance must survive refactoring.** Line numbers alone are not sufficient.
13. **Human and AI authorship are separate dimensions.** A line can be AI-generated, human-modified, AI-refactored and human-reviewed over its lifetime.
14. **The model never grants itself authority.** File, command, network, package, MCP, secret and export permissions are enforced outside the model.
15. **Enterprise and Government share a core.** High-security deployment is a stricter profile of the same architecture.
16. **No customer code enters future training by default.** Training use requires a separate, explicit program and agreement.
17. **Korean is a first-class product language.** This includes admin labels, alert explanations, policy text, search, reports and operational terminology—not only translation.
18. **Open-source code cannot be the trust secret.** If CP/Harness are open source, model authorization must rely on keys, signatures, attestation, entitlements and artifact control.
19. **Evaluation metrics are evidence, not truth.** Employee/productivity analytics expose confidence, missing data and context.
20. **No consequential employment decision should default to fully autonomous execution.** Human review, explanation and contestation are product requirements when Work Intelligence is used for personnel decisions.

---

# 4. Goals and Non-Goals

## 4.1 Product goals

### G1 — Centralize AI engineering control
Provide a single administrative surface for every enterprise Patty Code user, harness, session, repository, model, endpoint, policy, security event, message, broadcast, cost and provenance record.

### G2 — Make AI code attributable
For protected repositories, an authorized reviewer must be able to click a code block and identify the complete relevant history from AI request through current code state.

### G3 — Restrict inference to trusted Patty model artifacts
The official CP build must not route a governed Patty Code session to an arbitrary OpenAI-compatible server simply because it responds correctly.

### G4 — Support every Korean enterprise size
The product must scale from a 20-person startup to large conglomerates and shared government environments without requiring a different product architecture.

### G5 — Make deployment sovereignty configurable
The same platform must support Patty cloud, customer private cloud, on-premises, hybrid, closed network and air-gapped operation.

### G6 — Create strong enterprise administration
Korean administrators should be able to control behavior at organization, affiliate, division, department, team, project, repository, branch, role, user, harness, model, tool and data-classification levels.

### G7 — Connect engineering work to business outcomes
Link AI sessions and code changes to tickets, features, incidents, reviews, tests and deployments so productivity analysis is based on outcomes, not raw activity counts.

### G8 — Make communication native to the development surface
Allow users to reach coworkers and receive project/security/management broadcasts without leaving the terminal or IDE.

### G9 — Generate continuous evidence
Security, compliance, audit and provenance evidence should be generated during work instead of reconstructed later.

### G10 — Preserve developer speed
Low-risk routine coding should not feel like operating a classified weapons system. Policy must distinguish routine from high-risk actions.

## 4.2 Security goals

- Compromise of an LLM must not automatically compromise the user device, repository, CP or enterprise network.
- Compromise of one harness must not grant another user's authority.
- A forged model name must not obtain trusted endpoint status.
- A generic vLLM/SGLang endpoint must not receive governed production traffic unless it has passed Patty endpoint enrollment and model-artifact validation.
- Model, tool and policy changes must be versioned and attributable.
- Security telemetry must remain available even if the local coding session is terminated.
- Protected actions must fail safely if required identity/policy/evidence services cannot function.

## 4.3 Commercial goals

- Offer one enterprise platform with deployment-based packaging.
- Monetize Patty model access, enterprise subscriptions, managed CP, sovereign deployments, support, policy packs, model operations and professional services.
- Support seat-based, usage-based and hybrid billing without redesigning identity or telemetry.
- Make Control Plane features a reason enterprises purchase Patty Code rather than merely adopting an unrestricted open-source coding agent.

## 4.4 Non-goals

- CP will not become an unrestricted third-party LLM router in its initial product direction.
- CP will not promise that AI-authorship percentages are mathematically perfect after arbitrary external edits; confidence and ambiguity must be represented.
- CP will not replace the customer's HR department or make fully automatic firing/promotion decisions.
- CP will not expose hidden model chain-of-thought as explainability.
- CP will not invent proprietary cryptography.
- CP will not require customers to migrate from existing Git systems.
- CP will not require all customers to expose raw prompts to all administrators.
- CP will not treat token consumption, lines changed, files touched or prompt count as standalone measures of employee quality.

---

# 5. Target Customers and Personas

## 5.1 Customer segments

### A. 중소기업
Typical needs:
- simple deployment,
- cloud subscription,
- basic SSO,
- cost visibility,
- security policy templates,
- GitHub/GitLab integration,
- lightweight admin,
- rapid onboarding,
- Korean support.

### B. 중견기업
Typical needs:
- hybrid/on-prem options,
- multiple departments,
- more granular policy,
- internal GitLab/Jenkins/Nexus,
- budget allocation,
- SIEM integration,
- approval workflows,
- IP/PII controls,
- executive reporting.

### C. 대기업 / 그룹사
Typical needs:
- affiliates and complex organizational hierarchy,
- delegated administration,
- tens of thousands of identities,
- multiple data centers and network zones,
- private GPU fleets,
- multiple SCM systems,
- strict audit and legal hold,
- security operations integration,
- project/repository classification,
- contractor/vendor separation,
- internal chargeback,
- disaster recovery,
- high availability,
- extensive reporting,
- HR/engineering effectiveness integration.

### D. Government / public institutions
Typical needs:
- closed network and air gap,
- local PKI/SSO,
- customer-controlled keys,
- local GPUs,
- offline updates,
- no vendor dependency at runtime,
- stricter model/harness/device attestation,
- stronger evidence,
- Korean public-sector policy packs.

## 5.2 Primary personas

| Persona | Primary needs |
|---|---|
| Developer | Fast coding, clear denials, chat, model availability, repository context |
| Tech Lead | Team activity, code quality, review, provenance, task outcomes |
| Engineering Manager | Project status, workload, AI effectiveness, risk and delivery signals |
| Project Manager / TPM | Feature/ticket progress, blockers, broadcasts, adoption and capacity |
| CISO / Security | Alerts, policy, investigations, containment, DLP, model/tool governance |
| AI Governance Officer | Model approval, AI use cases, transparency, audit and risk controls |
| Platform Engineer | CP, model endpoints, GPUs, integration, deployment and SLOs |
| GPU/ML Platform Operator | Model deployment, endpoint health, utilization, queues and capacity |
| Compliance / Privacy | Evidence, retention, PII, access history, policy mappings |
| HR / People Analytics | Optional evidence-backed work-intelligence reports with constrained access |
| Executive | Adoption, risk, delivery, cost, model capacity and high-level trends |
| Auditor | Read-only evidence, policy history and provenance |
| Contractor / Vendor | Narrow, time-bounded project access |
| CP Super Admin | Global configuration without ability to erase evidence |

---

# 6. Information Architecture — Admin Console

The CP web administration experience should be designed as an **enterprise command center**. Every top-level summary must drill down to the underlying evidence.

Recommended global navigation:

```text
Overview
Live Operations
Organization
  ├─ Affiliates / Business Units
  ├─ Users & Groups
  ├─ Harnesses & Devices
  ├─ Roles & Delegated Admin
  └─ Presence
Projects & Repositories
  ├─ Projects
  ├─ Repositories
  ├─ Branches
  ├─ Code Ownership
  └─ Integrations
AI Sessions
  ├─ Live Sessions
  ├─ Session History
  ├─ Prompts & Responses
  ├─ Tool / MCP Activity
  └─ Context
Provenance
  ├─ Code Explorer
  ├─ Commit Explorer
  ├─ Human / AI Attribution
  ├─ Context Graph
  └─ Replay
Security
  ├─ Alerts
  ├─ Incidents
  ├─ DLP / PII / Secrets
  ├─ Prompt Injection
  ├─ Runtime / Sandbox
  └─ Threat Hunt
Governance
  ├─ Policy Studio
  ├─ Approvals
  ├─ Exceptions
  ├─ Compliance Profiles
  ├─ Data Classification
  └─ Retention / Legal Hold
Models & Infrastructure
  ├─ Model Registry
  ├─ Trusted Endpoints
  ├─ Endpoint Attestation
  ├─ GPU / Capacity
  ├─ Routing
  └─ Evaluations
Communications
  ├─ Direct Messages
  ├─ Groups / Project Channels
  ├─ Broadcasts
  ├─ File Transfers
  └─ Retention
Work Intelligence
  ├─ Engineering Outcomes
  ├─ Team Analytics
  ├─ AI Effectiveness
  ├─ Security & Quality
  ├─ Evaluation Rubrics
  └─ Review Periods
Usage & Billing
  ├─ Seats
  ├─ Tokens / GPU
  ├─ Department Cost
  ├─ Budgets
  └─ Entitlements
Evidence & Audit
  ├─ Audit Search
  ├─ Evidence Bundles
  ├─ Admin Access Logs
  ├─ Reports
  └─ Exports
Integrations
System
```

## 6.1 Global search

Global search should support authorized cross-object lookup by:

- person,
- employee ID,
- harness ID,
- device ID,
- session ID,
- repository,
- branch,
- commit SHA,
- file path,
- symbol,
- prompt hash,
- model ID,
- endpoint,
- incident,
- IP address,
- policy decision,
- message/broadcast ID,
- ticket/feature ID,
- evidence bundle.

Search results must respect the caller's access rights while allowing highly privileged security/audit roles to search across organizational boundaries where policy permits.

---

# 7. Overview Dashboard Requirements

The Overview page must behave like a consolidated NOC/SOC/engineering command center, not a marketing dashboard.

## 7.1 Required summary cards

- Registered users
- Active users
- Registered harnesses
- Online harnesses
- Non-compliant harness versions
- Active sessions
- Sessions waiting for approval
- Active AI requests
- Input/output tokens
- Model queue depth
- Trusted model endpoints
- Unhealthy/suspended endpoints
- GPU utilization / VRAM / queue
- Security alerts by severity
- Policy blocks
- Prompt-injection events
- Secret detections
- PII detections
- Unauthorized repository/file attempts
- Tool/MCP denials
- Network denials
- Package/license violations
- Active incidents
- Messaging online users
- Unacknowledged critical broadcasts
- AI contribution to merged code
- AI-assisted PR/MR count
- Test/security pass rates for AI-assisted changes
- Estimated model/GPU spend
- Subscription/seat usage
- Evidence pipeline health

## 7.2 Live activity stream

Each live event may show, according to role:

```text
10:41:08  김민수  DESKTOP-184  repo=payments  branch=feature/KP-123
          Patty-Coder-35B-v4  read src/payment/Validator.java
          policy=ALLOW   trace=trc_8df...

10:41:09  이지현  MAC-221  repo=customer-api
          prompt-injection candidate in README.md
          action=QUARANTINE_CONTEXT  severity=HIGH

10:41:12  박서준  VSCODE-998  requested package left-pad@...
          license policy=DENY
```

## 7.3 Filters

Every dashboard metric must support filtering by:

- organization / affiliate,
- business unit / department / team,
- project,
- repository,
- branch,
- user,
- role,
- harness/device,
- network zone,
- model,
- endpoint,
- GPU pool,
- data classification,
- policy profile,
- severity,
- time range.

## 7.4 Wallboard mode

- Full-screen operations mode.
- Redacts prompt/message/file content by default.
- Shows security, capacity, service health, active users and broadcasts.
- Configurable TV rotation.
- High-contrast and Korean typography optimized.

---

# 8. Core Identity Model

CP must treat identity as a graph, not one `user_id` field.

## 8.1 Identity entities

- `Organization`
- `Affiliate`
- `BusinessUnit`
- `Department`
- `Team`
- `User`
- `EmploymentProfile`
- `Group`
- `Role`
- `Device`
- `HarnessInstance`
- `HarnessBuild`
- `Session`
- `ServiceIdentity`
- `ModelEndpointIdentity`
- `ModelArtifactIdentity`
- `SandboxIdentity`

## 8.2 User authentication

Enterprise:
- OIDC
- SAML 2.0
- LDAP / Active Directory
- Microsoft Entra ID
- SCIM provisioning where available
- MFA
- WebAuthn/passkeys where supported

Government/Sovereign:
- local directory,
- government/agency PKI,
- smart card/hardware token where applicable,
- offline-capable identity infrastructure.

## 8.3 Harness authentication is independent from user authentication

A user successfully authenticating does **not** automatically make a harness trusted.

Every harness installation receives a unique identity and lifecycle:

1. Signed Patty Harness binary/extension is installed.
2. Harness creates a device-bound key where the operating system supports it.
3. User authenticates.
4. Harness requests enrollment.
5. CP evaluates organization policy, user entitlement, binary version, signature, device posture and optional hardware attestation.
6. CP creates `HarnessInstance` and binds it to `Device` plus authorized users.
7. CP issues a short-lived harness credential/certificate.
8. Harness renews through heartbeat and posture checks.
9. Admin can revoke or quarantine the harness independently of the user.

## 8.4 Harness registration attributes

- Harness ID
- Device ID
- Organization
- Current user
- Allowed users
- OS and version
- CPU architecture
- Hostname
- Managed-device state
- MDM/EDR posture where integrated
- Harness binary version
- Binary signature/hash
- VS Code extension version
- CLI version
- Secure key-store status
- First/last seen
- IP/network zone
- CP endpoint
- Policy profile
- License/entitlement state
- Last attestation
- Risk state
- Revocation reason

## 8.5 Administrative controls

Administrators can:

- allow 1 or N devices per user;
- restrict OS versions;
- force minimum harness versions;
- require MDM enrollment;
- require hardware-backed device keys;
- restrict by network zone;
- restrict by geographic/corporate network policy where appropriate;
- quarantine outdated builds;
- revoke a single device;
- remotely invalidate a harness session;
- require re-enrollment;
- set contractor expiry;
- export device/harness inventory.


# 9. Trusted Model Endpoint Architecture — Critical Requirement

This is one of the most important differentiators from a generic LLM gateway.

## 9.1 Requirement

The official CP distribution must **not** trust a model endpoint because:

- an administrator entered an IP address;
- `/v1/models` returns an expected model name;
- an OpenAI-compatible endpoint claims a Patty model ID;
- the model responds to a behavioral challenge in a particular way;
- the endpoint presents a long-lived API key;
- or the hostname contains a Patty-approved string.

All of these are forgeable.

The trust target is not an IP. The trust target is:

> **an enrolled inference workload running an approved Patty-signed model artifact under an approved serving/runtime configuration on an approved node, holding a short-lived cryptographic workload identity.**

## 9.2 Recommended architecture: Patty Inference Agent, not a vLLM fork

Create a small security-critical component called the **Patty Inference Agent (PIA)**.

```text
Patty Code Harness
        │
        │ authenticated session protocol
        ▼
Patty Code CP Gateway
        │
        │ mTLS + signed ModelRequest envelope
        ▼
Patty Inference Agent (PIA)
        │
        │ localhost / Unix socket only
        ▼
   vLLM / SGLang
        │
        ▼
 Patty-signed model package
```

Rules:

- vLLM/SGLang is **not exposed directly** to the enterprise network.
- The only routable model-service endpoint is PIA.
- PIA owns the CP-facing workload certificate.
- CP only routes to PIA instances with a valid `EndpointLease`.
- PIA verifies the model package and serving configuration at startup.
- CP periodically challenges PIA to prove continued identity/state.
- Network policy prevents CP/Harness from bypassing PIA.

This architecture reduces dependency on a fast-moving inference engine. vLLM supports out-of-tree plugins, so a small plugin may be used to expose runtime measurements or enforce integration details, but the security boundary should remain outside vLLM when practical.

## 9.3 Patty Model Package (PMP)

A Patty model is not identified by a name. It is identified by a signed manifest plus content digests.

Example conceptual manifest:

```yaml
apiVersion: models.patty.dev/v1
kind: PattyModelPackage
metadata:
  modelId: patty-coder-35b-v4
  packageId: pmp_01J...
  release: 4.0.2
  createdAt: 2026-08-11T12:00:00Z
spec:
  family: coder
  baseArtifact:
    merkleRoot: sha256:...
    format: safetensors
  shards:
    - name: model-00001-of-00008.safetensors
      sha256: ...
  tokenizer:
    sha256: ...
  config:
    sha256: ...
  chatTemplate:
    sha256: ...
  adapters:
    - id: ko-enterprise-sft-v7
      sha256: ...
  quantization:
    type: fp8
    calibrationArtifactSha256: ...
  serving:
    allowedEngines:
      - engine: vllm
        minVersion: ...
      - engine: sglang
        minVersion: ...
    containerDigest: sha256:...
  capabilities:
    - code
    - tool_use
    - korean
  entitlementClass: enterprise-coder
  expiry: ...
  signature:
    keyId: patty-model-release-2026
    signature: ...
```

The manifest must include or reference:

- model weights/shards,
- adapter/LoRA artifacts,
- tokenizer,
- config,
- chat template,
- quantization artifact/configuration,
- custom model code where applicable,
- inference engine compatibility,
- serving container digest,
- model-card metadata,
- license,
- evaluation version,
- release/expiry state,
- Patty signature.

## 9.4 Endpoint enrollment flow

1. Operator deploys an approved PIA image.
2. PIA obtains a **node/workload identity** from CP or a configured workload-identity service.
3. PIA verifies Patty signatures on the model package.
4. PIA verifies the local artifact digests or trusted immutable image/volume measurements.
5. PIA verifies the serving process and engine configuration.
6. Where required, PIA collects TPM/measured-boot/VM/GPU attestation evidence.
7. PIA sends `EndpointEnrollmentRequest` to CP.
8. CP generates a nonce challenge.
9. PIA signs an attestation envelope containing the nonce, endpoint identity and current measurements.
10. CP validates the chain of trust and policy.
11. If successful, CP creates a `TrustedModelEndpoint`.
12. CP issues a short-lived `EndpointLease`.
13. Gateway is allowed to route only while the lease is valid.
14. PIA re-attests periodically and when any material runtime property changes.

## 9.5 Endpoint attestation envelope

Conceptual fields:

```json
{
  "endpoint_id": "ep_01J...",
  "nonce": "...",
  "organization_id": "org_...",
  "node_identity": "spiffe://.../node/...",
  "workload_identity": "spiffe://.../inference/pia/...",
  "pia_build_digest": "sha256:...",
  "serving_engine": "vllm",
  "serving_engine_build": "...",
  "serving_container_digest": "sha256:...",
  "model_package_id": "pmp_...",
  "model_manifest_digest": "sha256:...",
  "model_merkle_root": "sha256:...",
  "tokenizer_digest": "sha256:...",
  "adapter_digests": ["sha256:..."],
  "runtime_config_digest": "sha256:...",
  "node_attestation": "...",
  "gpu_attestation": "...",
  "timestamp": "...",
  "signature": "..."
}
```

## 9.6 Assurance levels

Not every customer has confidential-computing hardware. CP should expose explicit endpoint assurance levels rather than pretending all deployments have the same proof strength.

### Level 1 — Software Verified
Suitable for ordinary enterprise deployments.

- Signed PIA image.
- Signed Patty model package.
- Artifact digest verification.
- Read-only model mount.
- Workload identity.
- Network isolation.
- Short-lived endpoint lease.
- Signed/verified serving container.

**Threat limitation:** A customer/root administrator controlling the entire host can potentially subvert software-only checks.

### Level 2 — Host Attested
For high-control enterprise and many government deployments.

Adds:

- TPM-backed node identity where available.
- Measured/Secure Boot policy.
- Key sealed to approved measurements.
- Workload attestation.
- Signed operating image/deployment manifest.
- Optional dm-verity/fs-verity/immutable image mechanisms.

### Level 3 — Confidential / Hardware Attested
For highest-assurance compatible environments.

Adds where supported:

- Confidential VM / TEE.
- GPU confidential-computing mode.
- GPU attestation.
- Model decryption key release only after acceptable CPU/VM/GPU/workload attestation.
- Stronger resistance to privileged host tampering.

NVIDIA H100 and newer supported configurations provide GPU confidential-computing attestation capabilities; CP should integrate such evidence as an optional high-assurance profile rather than make it a universal dependency.

## 9.7 Air-gapped attestation

Government air-gap deployments cannot depend on an internet-hosted attestation service at runtime.

Requirements:

- local CP CA/trust domain,
- local verification of Patty signatures,
- locally imported trusted reference measurements,
- offline-imported revocation and signing-key bundles,
- local hardware evidence verification where supported,
- signed offline entitlement/model package,
- local clock/time-integrity strategy,
- no mandatory call to Patty cloud.

## 9.8 Model key release

For proprietary Patty model weights, support encrypted-at-rest model packages.

Model decryption keys may be released only when:

- endpoint identity is valid,
- endpoint entitlement is valid,
- required attestation level passes,
- model package signature is valid,
- organization/project is authorized,
- license period is valid.

Government air-gap deployment can use a customer-local HSM/KMS plus a Patty-signed offline entitlement.

## 9.9 Important open-source limitation

If the entire CP is open source and a customer has root/admin control of its deployment, **it is impossible to make the open-source CP code itself an unmodifiable enforcement authority**. A customer can patch a locally controlled fork to accept another model.

Therefore:

- Official Patty builds must enforce Patty-signed endpoints.
- Patty-hosted model services enforce authorization server-side.
- Proprietary Patty model packages should use signed/encrypted artifacts and controlled key release where commercial restrictions matter.
- High-assurance on-prem restrictions require hardware/host attestation if resistance to root-level modification is a requirement.
- Forked CP builds that remove endpoint restrictions must not be represented as official/trusted Patty deployments.

This limitation must be understood internally before making claims that CP can *absolutely* prevent a fully privileged owner of an open-source deployment from modifying the software.

## 9.10 Why not behavioral signatures

Behavioral fingerprints can help detect anomalies but cannot establish model identity. A different model can be fine-tuned or prompted to imitate responses.

Use behavioral tests for:

- drift detection,
- corruption detection,
- quality regression,
- accidental model misdeployment,
- red-team monitoring.

Do not use them as the root of trust.

## 9.11 Why not rely on a vLLM fork

A permanent vLLM fork creates:

- security patch lag,
- performance-feature lag,
- merge cost,
- compatibility risk,
- maintenance burden across CUDA/PyTorch/NVIDIA changes.

Preferred order:

1. External PIA/sidecar boundary.
2. Official vLLM plugin/extension points where needed.
3. Small, upstreamable patches.
4. A fork only when a required security measurement cannot be obtained otherwise.

The same PIA protocol should support SGLang and future serving engines.

---

# 10. Gateway Requirements

## 10.1 Gateway role

Gateway is the **model traffic enforcement and accounting path** inside CP.

It must:

- authenticate the harness/session,
- authorize the user/project/repository,
- authorize the selected model,
- enforce data-classification routing,
- validate endpoint lease,
- enforce prompt/context policy,
- route traffic,
- meter tokens/usage,
- correlate events,
- enforce concurrency/quotas,
- provide endpoint health/failover,
- return model output through response controls,
- record provenance metadata.

## 10.2 Gateway must not support arbitrary endpoint registration

The UI must not contain a generic production flow equivalent to:

```text
Add Provider
URL: http://10.0.0.42:8000/v1
Model: patty-coder
API Key: abc
```

Instead:

```text
Enroll Patty Inference Endpoint
  → verify PIA identity
  → verify model package
  → verify runtime
  → verify attestation level
  → approve endpoint
  → issue lease
```

An endpoint's IP address is routing metadata, not identity.

## 10.3 Gateway protocols

### Harness ↔ CP
Use standard cryptography and a Patty application protocol, e.g.:

- TLS 1.3
- mutual TLS for harness/service identity where applicable
- HTTP/2 gRPC or HTTP/3/QUIC where operationally justified
- signed request correlation envelope
- streaming token support

### CP ↔ PIA

- mTLS with short-lived workload certificates
- service/workload identity validation
- signed `ModelRequestEnvelope`
- endpoint lease verification
- nonce/replay protection
- structured stream frames

Do **not** invent a custom cipher.

## 10.4 Model request envelope

```json
{
  "request_id": "mrq_...",
  "session_id": "ses_...",
  "organization_id": "org_...",
  "user_id": "usr_...",
  "harness_id": "har_...",
  "project_id": "prj_...",
  "repository_id": "repo_...",
  "commit": "...",
  "classification": "internal",
  "model_artifact_id": "pmp_...",
  "endpoint_lease_id": "lease_...",
  "policy_bundle_hash": "sha256:...",
  "prompt_exchange_id": "px_...",
  "token_budget": 12000,
  "deadline_ms": 60000,
  "nonce": "..."
}
```

The payload may be encrypted in transit normally; sensitive fields at rest are separately governed.

## 10.5 Routing criteria

Routing can consider:

- required Patty model family/version,
- data classification,
- organization policy,
- project/repository policy,
- endpoint assurance level,
- region/site,
- context length,
- workload type,
- model capability,
- GPU availability,
- queue depth,
- estimated latency,
- priority,
- entitlement,
- reserved capacity,
- maintenance/canary status.

## 10.6 Fallback

Fallback is allowed only to another **explicitly approved Patty model artifact and endpoint assurance class**.

A fallback must never silently:

- downgrade from on-prem to cloud,
- downgrade data classification,
- downgrade endpoint attestation,
- send a government request to public cloud,
- switch to an unapproved generic model.

## 10.7 Caching

Prompt/prefix/result caching must be:

- tenant partitioned,
- project/classification aware,
- disabled where content policy prohibits it,
- provenance aware,
- encrypted,
- observable to administrators,
- purgeable by retention/deletion workflows.

Cross-customer cache reuse is prohibited for content-bearing entries unless the cached prefix is an immutable Patty-owned public/system artifact with explicit classification.

## 10.8 Capacity and admission control

Gateway must support:

- per-user limits,
- per-project limits,
- per-department limits,
- per-tenant limits,
- concurrency quotas,
- token rate limits,
- long-context budget,
- priority queues,
- executive/incident reserved capacity,
- burst policies,
- hard/soft budget thresholds,
- queue estimation,
- cancellation,
- model draining,
- canaries.

---

# 11. Model Registry and Lifecycle

## 11.1 Registry scope

Registry is the system of record for:

- Patty model packages,
- inference endpoints,
- serving builds,
- tokenizer/config artifacts,
- adapters,
- quantization variants,
- model evaluations,
- model approvals,
- endpoint assurance evidence,
- model licenses,
- entitlements,
- lifecycle and revocation.

## 11.2 Model states

- Draft
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

## 11.3 Endpoint states

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

## 11.4 Required admin view

For each endpoint show:

- endpoint identity,
- host/node,
- datacenter/zone,
- organization,
- model package,
- exact weight/package digest,
- serving engine/version,
- PIA version,
- endpoint assurance level,
- last attestation,
- lease expiry,
- GPU(s),
- GPU attestation status where applicable,
- active requests,
- queue,
- tokens/sec,
- TTFT,
- error rate,
- health,
- current users/projects,
- maintenance state,
- security findings.

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

# 29. Usage, Entitlements, Billing, and Chargeback

CP's identity graph must support both commercial subscription billing and customer-internal cost allocation.

## 29.1 Billable/measureable dimensions

- named users,
- active users,
- registered Harnesses,
- concurrently active Harnesses,
- model input/output tokens,
- model request count,
- cached tokens where billing model distinguishes them,
- model/GPU time,
- premium model usage,
- storage/evidence volume,
- optional managed services.

The commercial plan may choose only a subset of these; instrumentation should preserve optionality.

## 29.2 Entitlement object

Entitlements can constrain:

- allowed Patty model families,
- context limits,
- request/token quota,
- concurrent sessions,
- Harness count,
- Work Intelligence availability,
- retention duration,
- advanced security/governance features,
- cloud GPU pool access,
- support tier.

## 29.3 Enterprise subscription

Enterprise can use:

- Patty-hosted CP with Patty model cloud,
- customer-hosted CP with Patty cloud model service,
- customer-hosted CP and customer GPU running approved Patty model packages,
- hybrid combinations governed by data classification.

Subscription validation must not make protected production operations fail abruptly during a transient vendor outage. Use signed time-bounded entitlement leases with defined grace behavior.

## 29.4 Government/air-gap license

Government profile supports signed offline entitlement/license bundles with:

- organization/deployment identity,
- edition/features,
- model rights,
- Harness/user capacity where contracted,
- effective/expiry dates,
- signature and revocation process,
- no required online phone-home.

## 29.5 Internal chargeback/showback

Customers can allocate model/GPU cost by:

- affiliate,
- department,
- cost center,
- project,
- repository,
- user,
- model.

Reports should separate **technical consumption** from **commercial invoice price** so private-GPU customers can use their own cost model.


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

# 34. Deployment Architecture and Editions

## 34.1 Core principle

There is one product and one core set of APIs/schemas. Editions are deployment and policy profiles.

### Public / Individual

- Patty-hosted identity,
- Harness,
- Patty-hosted approved models,
- lightweight account/session/usage services,
- no enterprise Control admin console,
- no organization surveillance/evaluation features.

### Enterprise Cloud

- Harness + full CP,
- Patty-operated or customer-selected CP hosting,
- Patty model cloud available,
- enterprise SSO,
- governance/security/provenance/comms/Work Intelligence,
- optional customer model/GPU deployment only for approved Patty packages.

### Enterprise Private / On-Prem

- customer CP,
- customer data stores/keys,
- customer GPU or approved Patty model cloud depending on policy,
- private integrations.

### Government / Sovereign

- same Harness and CP codebase,
- no required Patty cloud dependency,
- on-prem/private/air-gap,
- offline license/update/attestation modes,
- stricter default policy,
- customer-controlled keys,
- sovereign model/GPU infrastructure.

## 34.2 Reference trust zones

```text
ZONE 1 — USER / ENDPOINT
  Patty Code Harness, VS Code extension, managed device
          │
          │ mTLS + user + Harness identity
          ▼
ZONE 2 — CONTROL PLANE
  Identity | Sessions | Policy | Admin | Comms | Work Intelligence
          │
  ┌───────┼────────────┬───────────────┐
  │       │            │               │
  ▼       ▼            ▼               ▼
EXECUTION MODEL      EVIDENCE        INTEGRATION
PLANE     PLANE      PLANE           PLANE
sandbox   PIA→vLLM   audit/trace     Git/CI/HR/SIEM
          /SGLang
```

## 34.3 Control/data-plane separation

The web admin console must never be able to execute generated code directly. UI invokes control APIs; execution happens in independently constrained workers.

The Gateway must never provide broad repository access. It receives only governed model request envelopes.

The Model Plane must not need Git credentials or employee chat access.

The Evidence Plane should be separately permissioned and tamper resistant.

---

# 35. Security Architecture and Threat Model

## 35.1 Principal threats

| Threat | Example | Required boundary |
|---|---|---|
| Malicious user | asks AI to read another project | identity/context/file policy |
| Compromised Harness | forged actions / replay | Harness cert, nonce, signed leases |
| Fake model server | Qwen endpoint claims Patty name | PIA identity + model package attestation |
| Compromised model | requests secrets/network | deterministic tool policy |
| Prompt injection | README says exfiltrate | untrusted-content model + capabilities external to model |
| Malicious MCP | exfiltrates data | registry, network, response/DLP controls |
| Dependency attack | install script steals data | package broker/sandbox/no broad egress |
| Admin abuse | reads employee content | role/purpose/JIT + admin audit |
| Audit tampering | delete evidence | append-only signed events/external store |
| Provenance forgery | fake AI/human history | signed event graph + Git/hash binding |
| Cross-tenant leakage | tenant A sees B data | tenant keys, ABAC, query isolation |
| Endpoint compromise | modified weights | signed package + periodic attestation |
| Communication exfiltration | source sent in chat | classification/DLP/recipient authorization |
| Evaluation misuse | bad automated employee decision | rubric transparency + human review workflow |

## 35.2 Zero-trust requirements

- authenticate user, Harness, service, workload, and model endpoint separately,
- short-lived credentials and leases,
- mTLS service-to-service,
- no implicit trust based only on IP/network,
- explicit policy per protected action,
- separate management/control/execution/model/evidence roles,
- signed artifacts and version inventory,
- auditable privilege elevation.

## 35.3 Fail-closed classes

Protected actions fail closed if:

- identity cannot be resolved,
- Harness identity/lease invalid,
- model endpoint attestation invalid/expired,
- policy service unavailable beyond approved cache,
- audit durability unavailable beyond signed buffer threshold,
- required DLP/PII/secrets scanner unavailable,
- required provenance writer unavailable for protected export,
- artifact/update signature fails.

A separately defined degraded read-only mode can remain available for low-risk explanation where customers permit it.

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

# 37. Data Architecture

## 37.1 Primary data domains

| Domain | Examples | Suggested store class |
|---|---|---|
| Core control | orgs, users, Harnesses, projects, sessions, policies | relational |
| Event stream | actions, decisions, model/tool/runtime events | durable event bus |
| Analytics | usage, latency, security, work metrics | columnar/OLAP |
| Search | alert/audit metadata, indexed events | search engine |
| Evidence | signed bundles, snapshots, scans | object/WORM-capable storage |
| Provenance | code spans, ancestry, context/action graph | relational + graph/index |
| Communication | conversations/messages metadata | relational + encrypted object/content store |
| File transfer | encrypted attachments | object storage |
| Registry | models/tools/MCP/policies/releases | relational + artifact store |
| Secrets | issuance/config | enterprise secret manager |
| Telemetry | service/system metrics/traces | OpenTelemetry-compatible pipeline |

Technology choices should be replaceable behind stable contracts for government deployments.

## 37.2 Core entities

- `Organization`
- `OrganizationUnit`
- `User`
- `EmploymentProfile`
- `Role`
- `Group`
- `Device`
- `Harness`
- `HarnessEnrollment`
- `HarnessLease`
- `Project`
- `Repository`
- `RepoBaseline`
- `Branch`
- `Session`
- `Task`
- `PromptExchange`
- `ContextItem`
- `Action`
- `PolicyDecision`
- `Approval`
- `Tool`
- `MCPServer`
- `ToolCall`
- `NetworkGrant`
- `Sandbox`
- `Model`
- `ModelPackage`
- `InferenceEndpoint`
- `EndpointAttestation`
- `EndpointLease`
- `ChangeSet`
- `ProvenanceSpan`
- `CommitBinding`
- `SecurityFinding`
- `Incident`
- `Conversation`
- `Message`
- `FileTransfer`
- `Broadcast`
- `Presence`
- `UsageRecord`
- `Entitlement`
- `EvaluationRubric`
- `WorkMetric`
- `EvaluationSnapshot`
- `EvidenceBundle`
- `AuditEvent`
- `PolicyPack`
- `Exception`

## 37.3 Universal object labels

Every relevant object carries:

- tenant/organization,
- project where applicable,
- classification,
- owner,
- retention profile,
- legal/hold state,
- access labels,
- region/zone restrictions,
- encryption-key reference,
- creation/source provenance,
- deletion/archive state.

---

# 38. API Requirements

## 38.1 External/client API families

- Authentication API
- Harness Enrollment API
- Harness Configuration API
- Session API
- Policy/Approval API
- Model Gateway API
- Registry API
- Endpoint Attestation API
- Repository/SCM API
- Provenance API
- Security/Incident API
- Communications API
- Broadcast API
- File Transfer API
- Presence API
- Usage/Entitlement API
- Work Intelligence API
- Evidence/Audit API
- Admin/Reporting API

## 38.2 Representative endpoints

```text
POST /v1/harnesses/enroll
POST /v1/harnesses/{id}/attest
POST /v1/harnesses/{id}/leases
POST /v1/sessions
POST /v1/sessions/{id}/actions
POST /v1/policies/evaluate
POST /v1/approvals
POST /v1/models/endpoints/enroll
POST /v1/models/endpoints/{id}/attest
POST /v1/gateway/responses
GET  /v1/provenance/repos/{repo}/commits/{sha}
GET  /v1/provenance/spans/{id}
GET  /v1/security/findings
POST /v1/incidents/{id}/contain
POST /v1/conversations
POST /v1/conversations/{id}/messages
POST /v1/transfers
POST /v1/broadcasts
GET  /v1/presence
GET  /v1/usage
GET  /v1/work-intelligence/teams/{id}
GET  /v1/work-intelligence/users/{id}
POST /v1/evidence/bundles
GET  /v1/audit/events
```

API authorization must not rely on the UI hiding a capability.

## 38.3 Protocol requirements

- versioned APIs,
- Protobuf/gRPC internally where appropriate,
- HTTP/JSON for admin/external interoperability,
- streaming/WebSocket or equivalent for live Harness communication,
- signed schemas for protected action envelopes,
- idempotency for write/retry operations,
- correlation IDs across every service.

---

# 39. Event Model and Event Topics

CP should be designed around a durable event spine so dashboard, audit, provenance, billing, security, and analytics derive from common events rather than duplicative logging.

## 39.1 Event envelope

```yaml
event_id: evt_...
event_type: patty.session.started
schema_version: 1
occurred_at: ...
received_at: ...
organization_id: org_...
actor:
  user_id: usr_...
  harness_id: hrn_...
session_id: ses_...
project_id: prj_...
repository_id: repo_...
trace_id: trc_...
classification: internal
payload_ref: ...
signature: ...
```

## 39.2 Core topics/classes

```text
cp.identity.lifecycle
cp.harness.lifecycle
cp.harness.attestation
cp.session.lifecycle
cp.prompt.exchange
cp.context.decision
cp.action.request
cp.policy.decision
cp.approval.lifecycle
cp.tool.request
cp.mcp.request
cp.network.grant
cp.runtime.event
cp.model.request
cp.model.endpoint.attestation
cp.model.lifecycle
cp.git.change
cp.provenance.span
cp.security.finding
cp.incident.lifecycle
cp.communication.message
cp.communication.presence
cp.file.transfer
cp.broadcast.lifecycle
cp.usage.record
cp.entitlement.lifecycle
cp.work.metric
cp.evaluation.snapshot
cp.evidence.bundle
cp.admin.action
cp.config.lifecycle
```

## 39.3 Event integrity

For security/provenance-critical events:

- monotonic/ordered sequence within a session where feasible,
- chained hashes or equivalent tamper evidence,
- source service/workload identity,
- schema validation,
- durable buffering,
- deduplication/idempotency,
- clock-drift monitoring,
- immutable retention according to policy.


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

# 43. Non-Functional Requirements

## 43.1 Availability targets

Suggested initial production objectives:

| Service | Target |
|---|---:|
| Core Control APIs | 99.9% monthly |
| Harness session/auth path | 99.9% |
| Model Gateway | 99.9% excluding approved maintenance |
| Policy evaluation within HA deployment | 99.99% |
| Communications | 99.9% |
| Audit signed buffering | no acknowledged protected action without durable path |
| Provenance for protected export | 100% required where policy enables merge/export gate |

Customer/air-gap targets may differ.

## 43.2 Performance targets

Initial product targets to validate with production-like loads:

- p95 cached policy evaluation < 20 ms,
- p95 remote policy evaluation < 100 ms inside deployment network,
- CP overhead before model dispatch should remain a small fraction of coding-request TTFT,
- live administrative event freshness < 3 seconds under normal load,
- Harness presence freshness < 15 seconds,
- message delivery p95 < 2 seconds inside healthy network,
- revocation/lockdown propagation < 10 seconds target,
- provenance query for one commit < 2 seconds p95 for ordinary repos,
- dashboard common query < 3 seconds p95.

Exact SLOs should be benchmarked and edition-specific.

## 43.3 Scale profiles

Design for:

### SME
- 10–200 developers,
- 10–500 Harnesses/devices,
- tens of repositories.

### Mid-market
- 200–2,000 developers,
- thousands of Harnesses,
- hundreds/thousands of repositories.

### Large enterprise/group
- 2,000–20,000+ developers/contractors,
- multiple affiliates,
- tens of thousands of Harness identities over lifecycle,
- thousands/tens of thousands of repos,
- large event/audit volume.

Architecture should scale horizontally without changing the user/identity/provenance model.

## 43.4 Resilience

- stateless services horizontally scalable where possible,
- event retry/idempotency,
- circuit breakers,
- model endpoint draining,
- policy cache with signed/versioned bundles,
- audit signed local buffer,
- database HA/backup,
- object-store durability,
- disaster-recovery procedures,
- offline government operation without Patty service dependency.

## 43.5 Observability

Platform operators need:

- metrics,
- distributed traces,
- structured logs,
- dependency health,
- queue/backpressure,
- DB/storage health,
- certificate/lease expiry,
- model endpoint health,
- event lag,
- evidence backlog,
- communication delivery failures.

System observability must avoid unnecessarily duplicating prompt/source/message content into telemetry systems.

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

# 46. Product Administration, Configuration, and Change Management

## 46.1 Configuration domains

- organization hierarchy,
- identities/roles,
- Harness enrollment,
- model/endpoints,
- projects/repos,
- policies,
- tools/MCP,
- runtime,
- communications,
- billing/entitlements,
- Work Intelligence,
- retention,
- integrations,
- keys/certificates,
- feature flags/release rings.

## 46.2 Configuration lifecycle

High-impact configuration uses:

1. draft,
2. validation,
3. conflict check,
4. optional historical simulation,
5. reviewer approval,
6. signed publication,
7. staged rollout,
8. observation,
9. full enforcement,
10. rollback/expiry.

## 46.3 Configuration drift

Detect:

- CP instance differs from intended profile,
- Harness is stale,
- endpoint PIA is stale,
- policy bundle out of sync,
- model package mismatch,
- SCM branch-protection drift,
- required integration unavailable,
- expired exception/certificate.

---

# 47. Onboarding and Customer Rollout

## 47.1 Enterprise pilot sequence

Recommended:

1. create organization and SSO,
2. import org units/groups,
3. onboard 1–3 repositories,
4. enroll pilot Harnesses,
5. integrate one SCM and CI,
6. deploy/enable one Patty model route,
7. enable baseline security/policy,
8. validate provenance,
9. train admins/security/developers,
10. run 2–4 week pilot,
11. review false positives/performance,
12. expand by team/repository ring.

## 47.2 Brownfield adoption

Customers can begin with **observe mode** for selected controls:

- log potential violation,
- show Harness warning,
- do not block,
- simulate policy outcome.

Move controls to enforce after tuning. Some mandatory Patty trust controls—identity, endpoint authorization, audit fundamentals—should not have an unsafe observe-only bypass in production profiles.

## 47.3 Developer migration

Provide:

- CLI installer/update channels,
- VS Code extension,
- SSO enrollment,
- repository auto-discovery,
- policy explanation,
- migration guidance from ordinary AI coding tools,
- optional import of non-sensitive preferences.

Do not import proprietary transcripts from third-party tools unless explicitly supported and authorized.

---

# 48. Implementation Roadmap

The CP scope should be implemented as a sequence of vertical slices rather than building every dashboard before enforcement is real.

## Phase 0 — Contracts and Trust Foundation (Weeks 0–6)

Deliver:

- organization/user/device/Harness schemas,
- user + Harness authentication,
- session/action/event contracts,
- core policy API,
- signed audit chain prototype,
- Model/ModelPackage/Endpoint schemas,
- Patty Inference Agent prototype,
- one vLLM/SGLang adapter without forking engine,
- one model package verification flow,
- initial Control shell/overview.

**Gate:** enrolled user + enrolled Harness can send one governed request only to an attested Patty endpoint, and the complete action is auditable.

## Phase 1 — Enterprise Harness Fleet + Gateway (Months 2–4)

Deliver:

- Live Harnesses,
- session inspector,
- entitlements/usage,
- endpoint registry/health/routing,
- SSO/SCIM,
- repository onboarding/Git baseline,
- basic policy and security alerts,
- cloud/on-prem deployment templates.

**Gate:** admins can enroll/revoke Harnesses, manage users, restrict models, and trace one session to repository state.

## Phase 2 — Provenance + Security Enforcement (Months 4–7)

Deliver:

- file/tool/command/network/context controls,
- secrets/PII/injection scanning,
- security operations,
- incident containment,
- change-set graph,
- line/span provenance MVP,
- PR/commit checks,
- evidence bundles.

**Gate:** click a code span and recover session/user/Harness/model/context/tools/policies/tests; critical incident can isolate session/Harness.

## Phase 3 — Communications + Enterprise Operations (Months 6–9)

Deliver:

- presence,
- 1:1/group/project chat,
- governed file transfer,
- session handoff,
- broadcasts,
- fleet release rings,
- organization/affiliate control tower,
- contractor mode.

**Gate:** enterprise can run CP as a central developer-AI operations hub without external chat for necessary engineering coordination.

## Phase 4 — Work Intelligence + Advanced Analytics (Months 8–12)

Deliver:

- engineering/adoption dashboards,
- Work Intelligence metrics,
- configurable rubrics,
- evaluation workflow,
- correction/appeal,
- executive reports,
- chargeback/showback,
- change-impact/risk scoring.

**Gate:** an evaluation scorecard can be explained entirely from underlying signed work evidence and requires human finalization.

## Phase 5 — Private/Sovereign Hardening (Months 10–15)

Deliver:

- HA/DR,
- signed offline licensing,
- offline update/attestation workflow,
- customer KMS/HSM,
- air-gap profile,
- advanced runtime/sandbox,
- SPIFFE/SPIRE or equivalent workload identity integration,
- hardware endpoint attestation profile where supported,
- full policy/compliance packs.

**Gate:** representative Government/Sovereign environment can install, license, attest endpoints, operate, audit, and update without public internet.

## Phase 6 — Scale and Ecosystem (Months 13–18)

Deliver:

- large-group tenancy,
- advanced SI mode,
- connector SDK,
- policy pack SDK,
- MCP registry/ecosystem,
- advanced provenance survival,
- enterprise-grade Work Intelligence integrations,
- certification-readiness work for separately scoped services.

---

# 49. Cross-Product Acceptance Criteria

The product is not ready merely because a Harness can call a model.

## Identity and access

1. A user without project access cannot make a Harness retrieve that project's data.
2. A valid user with an unregistered Harness cannot create a protected enterprise session.
3. Revoking a Harness prevents new work within the propagation SLO.
4. User termination through SCIM disables associated future sessions while preserving evidence.

## Model trust

5. A normal Qwen/vLLM endpoint with a Patty model name but no PIA attestation is rejected.
6. An endpoint with a valid PIA identity but wrong model package hash is rejected.
7. An expired endpoint lease stops new routing.
8. Suspending a Patty model package stops new requests across the organization.

## Security

9. Prompt injection in a README cannot expand tool permissions.
10. A secret in `.env` is prevented from entering model context according to policy.
11. Korean PII test fixtures trigger configured controls.
12. An unapproved MCP server cannot be invoked.
13. A sandbox cannot use broad public network access when deny-by-default is configured.
14. Critical incident containment pauses affected session execution.

## Provenance

15. An AI-created code block resolves to user, Harness, session, model artifact, endpoint, base commit, context, tools, policies, tests, and commit.
16. Provenance survives an ordinary file rename/refactor with a documented confidence result.
17. Human edits after AI generation are distinguishable from the initial AI patch.
18. Missing required provenance can block a protected merge/export.

## Communications

19. Unauthorized recipients cannot open repository-linked content merely because a user pasted a CP link.
20. File transfer is blocked/quarantined when DLP policy triggers.
21. A Critical broadcast reaches targeted online Harnesses and records acknowledgement where required.
22. Broadcast permission does not grant fleet-control permission.

## Work Intelligence

23. Raw prompt/token/LOC counts do not alone produce an employee performance outcome.
24. Every score displays its rubric, weights, data window, and evidence.
25. A manager can correct supported attribution with documented reason.
26. Final consequential evaluation requires a configured human finalization step by default.
27. Historical rubric changes are auditable.

## Audit/admin

28. An administrator cannot erase an existing protected audit event through normal product interfaces.
29. Opening sensitive prompt/message content records the admin access.
30. A policy change shows author, reviewer, diff, rollout, and resulting version.

## Deployment

31. Enterprise cloud operation supports subscription entitlements.
32. Sovereign operation supports an offline entitlement/update path.
33. Same core Harness/CP schemas work across Enterprise and Government profiles.

---

# 50. Product KPIs

## 50.1 Adoption

- eligible versus enrolled users,
- weekly/monthly active Harness users,
- active teams/projects,
- AI-assisted task rate,
- return usage,
- feature-mode adoption.

## 50.2 Engineering outcome

- task completion,
- accepted change rate,
- rework,
- tests/verification,
- defect/revert trends,
- review latency,
- cycle-time change.

## 50.3 Security/governance

- high/critical findings,
- containment time,
- repeat violation rate,
- provenance coverage,
- policy exception age,
- model approval/attestation compliance,
- unapproved tool/model attempts,
- evidence completeness.

## 50.4 Platform

- model request success,
- control-plane latency overhead,
- TTFT/model latency,
- concurrent sessions,
- Harness connectivity,
- event lag,
- admin query latency,
- communication delivery,
- endpoint attestation health.

## 50.5 Commercial

- licensed versus active seats,
- expansion by team/affiliate,
- model/GPU consumption,
- retention/renewal,
- private/on-prem deployments,
- attach rate of advanced Control/Work Intelligence capabilities.

KPIs must be used to improve product/customer operations and should not silently become individual employment metrics.

---

# 51. Key Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| CP becomes perceived as "just another LLM gateway" | weak differentiation | position Gateway as internal substrate; lead with Control, security, provenance, work intelligence |
| Customers bypass Patty-only endpoint restriction | commercial/security model weakens | PIA identity, signed model package, endpoint attestation, key release, official-build trust profile |
| vLLM/SGLang integration lags | maintenance burden | sidecar/plugin/adaptor first; fork only minimal unavoidable hooks |
| Admin visibility becomes privacy/employee-relations risk | adoption/legal risk | purpose/role separation, admin access audit, configurable notices/retention |
| Employee evaluation becomes simplistic surveillance | bad incentives, legal/reputational risk | outcome rubric, weak-signal exclusions, human review, correction/appeal, team-first analytics |
| Provenance attribution is inaccurate after refactors | loss of trust | multi-signal AST/Git/semantic lineage, confidence, ambiguity workflow |
| Communications expands scope into Slack replacement | development distraction | engineering-context messaging only; integrate external collaboration later |
| Messaging becomes exfiltration channel | data loss | DLP, classification, recipient re-auth, encrypted transfer, audit |
| Policy enforcement causes developer friction | bypass pressure | observe/simulate modes, risk tiers, clear Korean explanations, caching |
| Open source makes licensing restriction seem unenforceable | confusion | explicitly define official trust boundary; signatures/attestation rather than obscurity |
| Model/GPU cost surprises customers | renewal risk | showback/chargeback, quotas, capacity dashboards, model outcome comparisons |
| Work Intelligence data is incomplete | unfair conclusions | completeness indicators, integration caveats, human adjustment, prevent unsupported scoring |
| Government divergence creates a second product | maintenance failure | deployment profiles/policy packs only; common contracts and code |
| Air-gap updates become stale/cumbersome | security/operability risk | signed delta bundles, local validation, staged rings, offline evidence |
| CP stores too much sensitive content | breach impact | metadata-first storage, independent retention, encryption, content-access separation |

---

# 52. Explicit Non-Goals for Initial Releases

To keep the platform coherent, the first production releases should not attempt to:

- become a general-purpose Slack/Teams replacement,
- become a payroll/HRIS system,
- autonomously hire/fire/promote employees,
- support arbitrary external model providers through the official Patty Gateway,
- replace Git/CI/CD for customers that already have them,
- provide direct autonomous production deployment by default,
- guarantee legal/certification compliance merely because a policy pack exists,
- infer employee quality from token/line/activity volume,
- expose hidden model chain-of-thought as explainability,
- make an unmodifiable security guarantee against a customer root administrator who intentionally rebuilds all open-source components and controls all trust anchors.

---

# 53. Open Product Decisions

These should be explicitly resolved during design/implementation rather than hidden as assumptions.

1. **Product naming:** Patty Code Control Plane is temporary.
2. **Model packaging:** whole-artifact hash versus Merkle/chunk manifest for very large model packages.
3. **PIA architecture:** sidecar process, local reverse proxy, or engine plugin per serving environment.
4. **Hardware attestation scope:** required only for Government/high assurance, or available Enterprise premium profile.
5. **Model encryption/key release:** which Patty model distributions require encrypted-at-rest packages and attested key release.
6. **Default transcript retention:** metadata/redacted/full by enterprise profile.
7. **Local versus remote execution:** baseline Enterprise defaults.
8. **Messaging retention:** default and customer override model.
9. **File transfer maximum types/sizes:** initial conservative policy.
10. **Work Intelligence licensing:** core versus add-on.
11. **Employee scorecard default:** ship sample templates or require customer-created rubric.
12. **Managed Git:** whether to ship in first 12 months or rely on GitLab/GitHub/Gitea.
13. **Kakao Work/Teams/Slack bridges:** roadmap priority.
14. **Shadow AI Discovery:** built directly or connector-only.
15. **Open-source boundary:** exact repositories/licenses versus Patty commercial services/assets.
16. **Cloud CP tenancy:** shared multi-tenant SaaS versus dedicated enterprise control-plane option.
17. **Enterprise model route:** whether customers can use only Patty-produced model families or Patty-certified third-party models in the future.

---

# 54. Definition of Done

Patty Code Control Plane reaches its intended product definition when:

1. Enterprise admins can manage users, Harnesses, projects, models, policies, and entitlements from one Korean-first Control interface.
2. User identity and Harness identity are independently authenticated and correlated for every protected action.
3. The official Gateway will not route to an arbitrary vLLM/SGLang endpoint merely because it claims a Patty model name.
4. Patty model endpoints have verifiable endpoint identity and model-package evidence under the configured assurance profile.
5. Every protected session produces an attributable chain from user/Harness to prompt/context/model/tools/files/code/commit/outcome.
6. Administrators can inspect and contain live Harness sessions according to role and purpose.
7. Security controls can block secrets, Korean PII, unauthorized context, unsafe tools/commands/networks, and unapproved endpoints before protected actions occur.
8. A reviewer can click AI-assisted code and see grounded provenance that survives ordinary repository evolution.
9. Enterprise users can securely communicate, transfer approved files, hand off tasks, and receive targeted/critical broadcasts from within the Harness/IDE.
10. Work Intelligence can generate evidence-backed role-specific engineering/AI-use scorecards without reducing employee performance to raw activity volume or autonomously finalizing personnel decisions.
11. Enterprise subscription/cloud and Government on-prem/air-gap profiles use the same core schemas, Harness, Control Plane, policy engine, provenance model, and event contracts.
12. Government deployments can operate without a required external Patty cloud dependency.
13. Admin access and admin actions are themselves fully auditable.
14. The product can demonstrate meaningful differentiation even if all routing/Gateway functionality is described in one paragraph: **governance, security, provenance, operational control, communications, and work intelligence remain the product.**

---

# Appendix A. Model Endpoint Attestation Protocol — Reference Design

This appendix makes the Patty-only routing requirement concrete.

## A.1 Components

- **CP Model Registry** — approved logical Patty models and signed package manifests.
- **CP Endpoint Registry** — enrolled inference endpoints.
- **Attestation Service** — validates endpoint/host/GPU evidence according to profile.
- **Patty Inference Agent (PIA)** — signed edge process installed on model host/pod.
- **Serving Engine** — unmodified or lightly integrated vLLM/SGLang behind PIA.
- **Model Artifact Store** — signed/encrypted Patty model package.
- **KMS/Key Broker** — optional attestation-bound model decryption key release.
- **Gateway** — routes only to endpoints holding valid CP EndpointLease.

## A.2 Enrollment sequence

```text
PIA                     CP / Attestation             Artifact/KMS
 │                              │                         │
 │-- enroll(endpoint pubkey) -->│                         │
 │<--------- nonce -------------│                         │
 │                              │                         │
 │ measure host/PIA/model       │                         │
 │-- signed attestation ------->│                         │
 │                              │ verify trust            │
 │                              │ verify PIA build         │
 │                              │ verify model manifest    │
 │                              │ verify host/GPU if req.  │
 │                              │                         │
 │                              │------ key request ------>│ (optional)
 │                              │<----- wrapped key -------│
 │<-- endpoint cert + lease ----│                         │
```

## A.3 Request sequence

```text
Harness
   │ authenticated request
   ▼
CP Gateway
   │ resolve allowed logical Patty model
   │ select endpoint with valid lease
   │ mTLS + request envelope
   ▼
PIA
   │ verify lease/request audience
   │ forward only to local/private serving socket
   ▼
vLLM / SGLang
   │
   ▼
PIA response envelope
   │ endpoint/model/session correlation
   ▼
Gateway → Harness
```

## A.4 Model manifest example

```yaml
apiVersion: models.patty.ai/v1
kind: PattyModelPackage
metadata:
  id: pmp_kocoder_35b_2026_08_01
  name: patty-kocoder-35b
  version: 2026.08.01
artifacts:
  root_manifest_sha256: ...
  weights:
    - path: model-00001-of-00008.safetensors
      sha256: ...
  tokenizer:
    sha256: ...
  config:
    sha256: ...
  chat_template:
    sha256: ...
  adapters: []
serving:
  engines:
    - type: vllm
      versions: [">=0.x,<1.x"]
    - type: sglang
      versions: [">=0.x,<1.x"]
security:
  minimum_endpoint_assurance: L1
  allowed_data_classes: [internal, confidential]
signatures:
  - key_id: patty-model-release-2026
    signature: ...
```

## A.5 Endpoint lease

A lease should bind:

- endpoint ID,
- PIA public key,
- organization/deployment,
- model package ID/hash,
- host/node identity,
- optional GPU identities,
- serving-engine version,
- assurance level,
- issue/expiry,
- allowed routing zones/data classes,
- nonce/attestation reference,
- CP signature.

Gateway does not accept "endpoint URL + model name" as sufficient configuration.

## A.6 Periodic assurance

Re-attest on:

- startup,
- model load/change,
- PIA/engine upgrade,
- host reboot,
- certificate rotation,
- configured time interval,
- security incident,
- explicit admin request.

## A.7 Why not conversational fingerprinting

Behavioral/model-output fingerprints can be useful for anomaly detection but not as endpoint identity because they are probabilistic, may change under sampling/prompting, and can be imitated. They are a monitoring signal only.

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

# Appendix E. Edition Matrix

| Capability | Individual | Enterprise | Government/Sovereign |
|---|---:|---:|---:|
| Patty Code Harness | ✓ | ✓ | ✓ |
| Patty model access | Patty cloud | cloud and/or approved private | local/private only by default |
| Full CP admin | — | ✓ | ✓ |
| User + Harness identity | account/device lightweight | full | full/strict |
| Model endpoint attestation | Patty-managed | ✓ | ✓ strict/offline capable |
| Provenance | personal/basic | full | full |
| Security policy | basic | full | full/strict defaults |
| Communications | optional future personal | ✓ | ✓ local |
| Broadcast | — | ✓ | ✓ |
| Work Intelligence | — | ✓ optional | configurable; likely restricted by agency policy |
| Enterprise SSO/SCIM | — | ✓ | ✓ local directory/PKI |
| Cloud subscription | ✓ | ✓ | not required |
| On-prem CP | — | optional | required/typical |
| Air-gap | — | optional special | ✓ |
| Offline license/update | — | optional | ✓ |
| Customer-controlled KMS/HSM | — | private edition | ✓ |
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

# Appendix G. External Technical and Regulatory Baseline for Design Review

The implementation team should validate the current versions of these sources before shipping relevant controls:

### Inference/workload trust

- vLLM official plugin and OpenAI-compatible serving documentation.
- SPIFFE/SPIRE workload identity and attestation documentation.
- Sigstore/Cosign signing and attestation documentation.
- NVIDIA Confidential Computing / GPU attestation documentation for hardware-assisted profiles.

### Korean privacy and AI governance

- 개인정보 보호법 and current Enforcement Decree, including automated-decision provisions.
- Personal Information Protection Commission guidance/notifications relevant to automated decisions and employee data.
- 인공지능 발전과 신뢰 기반 조성 등에 관한 기본법 and current Enforcement Decree, including high-impact AI, transparency, human management/supervision, and impact-assessment provisions where applicable.
- Current ISMS-P criteria and KISA/PIPC guidance relevant to the deployment.

### Important product rule

These materials inform controls and evidence. CP must not claim legal compliance, certification, or a particular employment-law conclusion solely because the product provides a feature. Applicability depends on deployment, customer process, data, role, and current law.

---

# Appendix H. Immediate Product Decisions and First Build Slice

If work begins immediately, build this vertical slice first:

1. `Organization`, `User`, `Harness`, `Project`, `Repository`, `Session`, `Action` schemas.
2. User SSO + independent Harness enrollment/certificate.
3. Signed `ActionEnvelope` and audit stream.
4. `ModelPackage`, `InferenceEndpoint`, `EndpointAttestation`, `EndpointLease` schemas.
5. Patty Inference Agent in front of one vLLM deployment.
6. Signed Patty model manifest verification.
7. CP Gateway that rejects any endpoint without a valid EndpointLease.
8. One repository baseline and one model request correlated end-to-end.
9. One code patch written as `ChangeSet` with provenance to user/Harness/model.
10. Minimal Control UI showing user, Harness, session, repo/branch, model endpoint, request, and signed timeline.

**First demonstrable milestone:**

> An enterprise admin enrolls 김개발 and his managed Patty Code Harness, assigns him to Project A, permits only Patty-KoCoder-v1, starts a session on `repo/payment-service` branch `feature/refund`, sends one Korean coding request, routes it only to an attested Patty endpoint, records the resulting edit against exact Git state, and then opens Control to see the complete user → Harness → prompt → model → file → diff → policy → commit provenance chain.

That single flow proves the architecture. Everything else—security depth, communications, Work Intelligence, government air-gap, scale—can expand around the same IDs and contracts without redesigning the foundation.
