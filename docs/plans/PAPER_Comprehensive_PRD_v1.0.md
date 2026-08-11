# PAPER Protocol
## Patty AI Provenance & Enforcement Relay
### Comprehensive Product Requirements Document (PRD) v1.0

**Document status:** Design baseline for open-source implementation and protocol specification  
**Protocol name:** **PAPER — Patty AI Provenance & Enforcement Relay**  
**Primary product:** Patty Code communication protocol and governed AI data plane  
**Primary market:** Republic of Korea — 중소기업, 중견기업, 대기업, public institutions, and government  
**Secondary market:** Global enterprises, regulated organizations, and open-source adopters  
**Prepared:** 2026-08-11  
**Primary language:** Korean-first product experience; protocol specification and source code in English with Korean documentation  
**Open-source posture:** Public specification, public reference implementation, public conformance suite  
**Core transport decision:** QUIC first; native TLS 1.3/TCP fallback; no HTTP/REST/WebSocket compatibility mode  
**Architecture decision:** All governed AI traffic traverses a horizontally scalable PAPER Relay data plane controlled by Patty Code Control Plane (CP)

---

# Table of Contents

1. Executive Summary
2. Problem Statement
3. Product Thesis and Novelty Boundary
4. Frozen Architectural Decisions
5. Product Goals
6. Explicit Non-Goals
7. Target Users and Deployment Profiles
8. Terminology
9. System Context
10. Trust Model and Security Boundary
11. Protocol Layering
12. Transport Bindings
13. Peer Profiles
14. Identity and Enrollment
15. Credential Lifecycle
16. Connection and Session Lifecycle
17. PAPER Exchange Model
18. Capability Leases
19. Governance Envelope
20. Policy Epochs and Policy Binding
21. PAPER Provenance Spine
22. Content Addressing and Evidence Receipts
23. PAPER Relay Architecture
24. Relay Enforcement Pipeline
25. Harness-to-Relay AI Flow
26. Relay-to-PIA Inference Flow
27. Patty Inference Agent
28. Approved Model Identity and Model Packages
29. Stream and Lane Architecture
30. AI Inference Message Family
31. Context and Repository Message Family
32. Tool and Runtime Message Family
33. Collaboration Message Family
34. Voice Messaging
35. Managed File Transfer
36. Presence and Availability
37. Broadcast and Emergency Messaging
38. Administrative Control Messaging
39. Telemetry and Usage Signals
40. Payload Protection Profiles
41. Cryptography and Key Management
42. Replay Protection, Idempotency, and Resumption
43. Versioning and Extensibility
44. Error and Failure Model
45. Resource, Flow-Control, and DoS Requirements
46. Security Threat Model
47. Privacy and Administrative Visibility
48. Korean Enterprise Requirements
49. Government and Air-Gapped Requirements
50. Public, Enterprise, and Government Product Profiles
51. Control Plane Integration
52. Git/SCM and Code Provenance Integration
53. Billing, Metering, and Entitlements
54. Observability and Operations
55. Reliability and Availability
56. Performance and Scale Requirements
57. Open-Source Governance
58. Protocol Registry and Extension Governance
59. Conformance, Fuzzing, and Interoperability
60. Reference Implementations
61. Rollout and Implementation Phases
62. Product KPIs
63. Risks and Mitigations
64. Cross-Product Acceptance Criteria
65. Definition of Done
66. Research and Publication Requirements
67. Appendix A — Initial Message Registry
68. Appendix B — State Machines
69. Appendix C — Representative Exchange Flows
70. Appendix D — Conceptual Schemas
71. Appendix E — Security Profiles
72. Appendix F — Novelty and Adjacent Protocol Boundary
73. Appendix G — Test Matrix
74. Appendix H — Standards Baseline

---

# 1. Executive Summary

PAPER is Patty's open communication protocol for AI engineering systems. It is designed to connect the Patty Code Harness, Patty Code Control Plane, PAPER Relay, Patty Inference Agent (PIA), runtime services, and approved GPU/model infrastructure through one stateful, authenticated, governance-native protocol.

PAPER is deliberately **not** an OpenAI-compatible API with different endpoint names. It does not use `/v1/chat/completions`, `/v1/responses`, Anthropic-style `/messages`, generic HTTP POST endpoints, JSON-RPC, WebSocket, or SSE as its normal remote communication model. It uses QUIC as the preferred transport and a native TLS 1.3/TCP binding as a compatibility fallback for networks that block QUIC. Both bindings carry the same PAPER application protocol.

The defining product idea is that AI communication should no longer be represented as a bare request and response:

```text
prompt -> model -> response
```

Instead, PAPER treats every meaningful AI operation as a **Governed Exchange**:

```text
actor identity
   +
harness identity
   +
organization / project / repository context
   +
policy and capability lease
   +
classified context
   +
approved model identity
   +
AI request and response
   +
tool/runtime actions
   +
artifacts and code changes
   +
relay verdicts
   +
cryptographically linked provenance
```

This shifts governance and provenance from external logging systems into the communication lifecycle itself.

PAPER is also the **complete communication substrate of the Harness**, not only the inference path. A connected enterprise Harness uses the same authenticated PAPER connection for:

- AI inference and token streaming,
- context disclosure and repository references,
- tool and runtime requests,
- provenance signals,
- policy updates and approval requests,
- 1:1 and group chat with other employees,
- asynchronous voice messages,
- managed file transfer,
- presence and availability,
- project/repository channels,
- administrative broadcasts,
- security warnings and emergency alerts,
- telemetry, usage, entitlements, and session health.

PAPER therefore becomes the trusted communications fabric between the developer surface, organizational governance, and AI infrastructure.

The enterprise architecture separates **Control Plane** and **Data Plane**:

```text
                         PATTY CODE CONTROL PLANE
                  identity / policy / registry / audit
                          / provenance / admin
                                  |
                                  | signed state,
                                  | policy epochs,
                                  | capability leases
                                  v
+----------------+       +--------------------+       +----------------+
| Patty Code     | PAPER | PAPER Relay        | PAPER | PIA            |
| Harness        |<=====>| Governed Data Plane|<=====>| Inference Peer |
+----------------+       +--------------------+       +----------------+
                                  |                         |
                                  |                         v
                                  |                   vLLM / SGLang
                                  |                   Patty Model
                                  v
                         evidence / telemetry
```

The Relay remains physically inline for governed AI exchanges so it can enforce policy before sensitive context reaches a model, before tool actions execute, and before model outputs reach the Harness. The Relay is horizontally scalable and is not the same process as the CP web application or administrative API.

PAPER uses standard, reviewed cryptography. The project will not invent its own cipher, KEM, signature primitive, or secure transport. Novelty belongs in the **protocol semantics and security architecture**, including:

1. **Governed Exchanges** — identity, intent, policy, authorization, and provenance are intrinsic to the exchange object.
2. **Capability Leases** — short-lived, purpose-bound authority issued by CP and bound to actor, Harness, project/repository scope, model, tool classes, data classification, and transport channel.
3. **Relay Verdicts** — signed, explicit enforcement outcomes generated at defined checkpoints in the exchange lifecycle.
4. **PAPER Provenance Spine** — content-addressed, cryptographically linked lineage connecting user intent, prompts, context, models, tools, generated artifacts, human edits, commits, and approvals.
5. **Proof-Carrying Stream Lifecycle** — streams begin with an intent/authorization manifest and end with a completion/evidence receipt; long streams produce resumable checkpoints.
6. **Policy Epoch Binding** — every exchange is bound to the exact effective policy state under which it was allowed.
7. **Unified Harness Communication** — AI, collaboration, broadcast, file, voice, security, and telemetry use one authenticated protocol family with strict capability namespaces.

The target is not merely to make Patty Code harder to redirect to OpenAI or arbitrary endpoints. The target is to define a protocol that enterprises and governments can audit, deploy, and potentially adopt beyond Patty's own products.

---

# 2. Problem Statement

AI coding Harnesses increasingly operate with authority that traditional chat clients never had. They may read large repositories, infer architecture, modify source code, invoke shells, call MCP tools, install packages, connect to services, alter configuration, generate tests, and prepare commits. At the same time, enterprises want to know exactly what information left the developer environment, which model handled it, what policies were applied, what AI changed, and what human ultimately approved.

Most model APIs are fundamentally model-access APIs. Their abstraction begins at the application boundary and generally assumes that authentication, governance, provenance, repository scope, human approvals, and enterprise messaging live elsewhere. Even when a gateway adds logging or DLP, those controls are usually layered around a request format that has no intrinsic concept of a repository, branch, code span, employee, policy epoch, approved tool capability, or resulting commit.

For Korean enterprises and government, the product requirements are stricter:

- centralized administrative control is expected,
- on-premises and air-gapped deployments must be possible,
- employee and Harness identities must be independently controllable,
- all model usage should be visible and governable,
- model endpoints must be centrally approved,
- sensitive Korean PII and corporate data must be controllable before disclosure,
- source-code provenance must survive normal Git workflows,
- administrators need a consolidated real-time view,
- employee collaboration must continue even in restricted environments,
- emergency notices and security commands must reach connected developers quickly,
- evidence must be reproducible for internal audit and government review.

PAPER addresses these requirements by treating communication itself as an enterprise control surface.

---

# 3. Product Thesis and Novelty Boundary

## 3.1 Product thesis

The central thesis is:

> **Secure AI transport is necessary but insufficient. Trustworthy enterprise AI requires exchanges that carry authority and accountability together with content.**

PAPER therefore does not claim novelty for encrypted transport, multiplexing, signatures, public-key encryption, or group key establishment. These are mature areas and should use existing standards.

PAPER's product novelty is the composition and semantics of a communication protocol specifically designed for governed AI engineering.

## 3.2 What PAPER intentionally reuses

PAPER should reuse established standards where appropriate:

- QUIC for secure multiplexed transport,
- TLS 1.3 for secure TCP fallback and the security foundation used by QUIC,
- ALPN for application-protocol negotiation,
- standard channel binding/exporter mechanisms for binding higher-level credentials to a secure channel,
- standard AEAD/signature/hash algorithms,
- HPKE where recipient-sealed payloads are needed,
- MLS where true endpoint-only group-messaging encryption is explicitly selected,
- standard audio codecs such as Opus for voice messages,
- existing SCM formats and Git hashes rather than inventing a version-control system.

## 3.3 What must be genuinely PAPER-specific

PAPER must introduce its own coherent model for:

- peer profiles and profile-bound authority,
- Harness enrollment and organization binding,
- governed AI exchange lifecycle,
- capability leases,
- policy epochs,
- context authorization manifests,
- Relay verdict checkpoints,
- provenance spine objects,
- AI/tool/artifact causality,
- stream contracts,
- multi-plane Harness communications,
- collaboration objects linked directly to code/provenance entities,
- evidence receipts,
- protocol-level model identity and authorization,
- state and replay semantics suitable for long-running agentic sessions.

The open-source repository must clearly identify which mechanisms are PAPER inventions and which are standards reused for safety/interoperability.

---

# 4. Frozen Architectural Decisions

The following decisions are considered approved for v1 design unless a future design review explicitly reopens them.

| Decision | v1 position |
|---|---|
| Protocol name | PAPER — Patty AI Provenance & Enforcement Relay |
| Open source | Yes: protocol, schemas, reference implementation, test suites |
| Specification | Public |
| Scope | Complete Harness communication protocol, not inference only |
| Preferred transport | QUIC |
| Compatibility transport | Native TLS 1.3 over TCP |
| HTTP/REST fallback | No |
| WebSocket/SSE fallback | No |
| OpenAI compatibility | Not exposed to Harness |
| Anthropic compatibility | Not exposed to Harness |
| JSON-RPC | Not used as core wire model |
| Data plane | PAPER Relay, horizontally scalable |
| CP placement | Authoritative control plane; not bulk token forwarding process |
| AI path | Harness -> Relay -> PIA -> approved model |
| Relay position | Inline for governed AI traffic |
| Peer authentication | Mandatory |
| Harness enrollment | Mandatory for enterprise/government CP |
| Device/TPM/MDM requirement | Not mandatory |
| Hardware attestation | Optional higher-assurance profile, not baseline |
| Model endpoints | PIA-mediated; raw vLLM/SGLang not a Harness-facing interface |
| Collaboration | Built into PAPER |
| Voice messages | Built into PAPER |
| File transfer | Built into PAPER and governed |
| Broadcasts | Built into PAPER |
| Provenance | First-class protocol concern |
| Cryptography | Standard primitives only; no proprietary cipher |

---

# 5. Product Goals

## G1 — Make governed AI communication native

Every protected AI action must carry enough information for the organization to decide whether the action is permitted before it occurs.

## G2 — Make provenance reconstructable

An authorized reviewer must be able to reconstruct the path from human intent to model invocation, tool use, generated artifact, human modification, and Git outcome.

## G3 — Restrict enterprise Harness communication to the PAPER network

The official Patty Code enterprise Harness should communicate with organizational AI services through PAPER rather than generic model APIs. A user should not be able to configure `OPENAI_BASE_URL` or an Anthropic endpoint and transparently substitute it for CP.

This requirement does **not** claim that software alone can prevent a user with full control of a workstation from writing a separate network client. Endpoint egress controls are required for that stronger property.

## G4 — Authenticate Harness separately from user

A valid employee login must not automatically make any program an authorized Harness. CP must know the user and the enrolled Harness instance independently.

## G5 — Support public, enterprise, private, and air-gapped deployments

One protocol must operate over Patty's public service, enterprise SaaS, customer private cloud, on-premises GPU infrastructure, and disconnected government environments.

## G6 — Keep CP scalable

Bulk inference traffic must not require the CP administrative application itself to process every byte. Relays scale separately and consume signed policy/identity state from CP.

## G7 — Support rich enterprise collaboration

Employees must be able to communicate directly within the Harness through 1:1 chat, group channels, voice messages, file transfer, presence, and code/session links.

## G8 — Support authoritative broadcasts

Organizations must be able to deliver informational, warning, critical, and emergency messages to specific users, projects, repositories, teams, models, or all connected Harnesses.

## G9 — Make Korean requirements first-class

The protocol and product must support Korean names, organization hierarchies, Korean PII classifications, KST, Korean-language reason codes, government offline operations, and customer-controlled cryptographic profiles.

## G10 — Be independently auditable

Security teams should be able to inspect the specification and source without trusting Patty's marketing claims.

## G11 — Enable global adoption

Although optimized initially for Patty Code and Korean organizations, the protocol abstractions must avoid hard-coding Patty-specific product assumptions where generic peer/profile semantics are feasible.

## G12 — Remain usable under policy

Security must not make normal coding intolerably slow. Low-risk actions should stay low-friction; policy should become progressively more explicit as risk increases.

---

# 6. Explicit Non-Goals

PAPER v1 is not intended to:

- replace TCP, QUIC, TLS, HPKE, MLS, or standard cryptography,
- provide arbitrary Internet agent-to-agent discovery,
- become a generic consumer messaging protocol,
- replace Git, GitHub, GitLab, or SCM systems,
- replace enterprise identity providers,
- replace SIEM/SOAR systems,
- expose a drop-in OpenAI/Anthropic-compatible endpoint to the Harness,
- prove an executable is unmodified on a hostile device without a hardware root of trust,
- allow anonymous enterprise/government usage,
- make hidden model chain-of-thought an audit artifact,
- promise legal/regulatory compliance by protocol use alone,
- make raw network traffic the source of truth for employee performance,
- make direct production changes the default agent behavior,
- invent cryptographic primitives for differentiation.

---

# 7. Target Users and Deployment Profiles

## 7.1 Individual developer

Uses Patty Code against Patty-hosted models. The public Harness still speaks PAPER, but Patty operates the Relay/PIA/CP services. The individual user does not receive the enterprise administrative console.

## 7.2 Enterprise developer

Uses Patty Code under company identity, policies, model entitlements, repository rules, collaboration, and audit.

## 7.3 Team lead / project manager

Needs broadcasts, team channels, repository/session visibility, approvals, engineering outcomes, and collaboration.

## 7.4 Security operator

Needs inline findings, incident controls, live session isolation, DLP/PII/prompt-injection visibility, and immutable evidence.

## 7.5 AI governance administrator

Needs model registry, endpoint approval, policy epochs, model usage, prompt/context controls, and audit.

## 7.6 Platform/GPU operator

Needs PIA fleet status, endpoint leases, model replicas, queue depth, capacity, and health without automatically receiving source-code or prompt permissions.

## 7.7 Auditor / compliance reviewer

Needs historical verification, evidence receipts, provenance reconstruction, policy versioning, access history, and exports.

## 7.8 Korean government administrator

Needs the same core capabilities with local-only dependencies, offline trust-anchor management, signed updates, restricted cryptographic profiles, and no required Patty cloud connection.

---

# 8. Terminology

| Term | Definition |
|---|---|
| **PAPER** | Patty AI Provenance & Enforcement Relay protocol family. |
| **Harness** | Patty Code CLI/TUI/IDE client used by a developer. |
| **CP** | Patty Code Control Plane; authoritative identity, policy, registry, governance, provenance, and admin system. |
| **Relay** | Horizontally scalable PAPER data-plane service that terminates/forwards PAPER streams and performs inline enforcement. |
| **PIA** | Patty Inference Agent; authenticated peer adjacent to an approved inference engine/model. |
| **Peer Profile** | Authenticated role that determines which protocol capabilities/messages a peer may use. |
| **Connection** | One QUIC or TLS/TCP secure transport connection carrying PAPER. |
| **Session** | Identity/policy-bound Harness working lifecycle, often associated with user, project, repository, and task. |
| **Exchange** | Causally meaningful protocol object representing an AI, tool, collaboration, control, or evidence interaction. |
| **Governed Exchange** | Exchange containing mandatory identity, scope, policy, and authorization bindings. |
| **Capability Lease** | Short-lived signed authority defining what a peer/session may do. |
| **Policy Epoch** | Immutable identifier for the exact effective policy state used to authorize an exchange. |
| **Relay Verdict** | Signed result of a required Relay/assurance evaluation. |
| **Provenance Spine** | Content-addressed causal graph connecting exchanges, decisions, artifacts, and human actions. |
| **Evidence Receipt** | Signed, independently verifiable summary of a completed/checkpointed exchange. |
| **Stream Contract** | Negotiated declaration of stream purpose, peer roles, capability, content class, protection mode, and limits. |
| **Content Class** | Classification of payload such as prompt, source code, chat, voice, file, telemetry, or policy. |
| **PMP** | Patty Model Package; signed manifest and artifacts representing an approved model release. |

---

# 9. System Context

PAPER exists inside a larger Patty Code platform but must remain a separable open-source project.

```text
                          +-----------------------------+
                          | Patty Code Control Plane    |
                          |-----------------------------|
                          | Org/User/Harness Registry   |
                          | Policy Engine               |
                          | Model Registry              |
                          | Provenance Store            |
                          | Audit/Evidence              |
                          | Billing/Entitlements        |
                          | Admin Console               |
                          +--------------+--------------+
                                         |
                              signed state / leases
                                         |
                                         v
+------------------+     PAPER     +------+-------+     PAPER     +------------------+
| Patty Code       |<=============>| PAPER Relay  |<=============>| PIA              |
| Harness          |               | Data Plane   |               | Inference Agent  |
+--------+---------+               +------+-------+               +--------+---------+
         |                                |                                |
         |                                |                                v
         |                                |                          vLLM / SGLang
         |                                |                          Patty Model
         |                                |
         +-- local repo/tools             +-- DLP/policy/security
         +-- TUI/VS Code                  +-- provenance events
         +-- collaboration                +-- rate/admission
```

The public service uses the same protocol:

```text
Public Harness -> Patty Cloud Relay -> Patty Cloud PIA -> Patty GPU
```

Enterprise on-premises:

```text
Enterprise Harness -> Customer PAPER Relay -> Customer PIA -> Customer GPU
                       ^
                       |
                    Local CP
```

Air-gapped government:

```text
Harness -> Local Relay -> Local PIA -> Local GPU
              ^              ^
              |              |
       air-gapped CP     local registry
```

---

# 10. Trust Model and Security Boundary

PAPER must distinguish identities that traditional APIs often collapse.

```text
User identity        != Harness identity
Harness identity     != Session identity
Session identity     != Relay identity
Relay identity       != PIA identity
PIA identity         != Model artifact identity
Model artifact       != Model serving endpoint
```

A valid user may operate multiple Harness installations. A valid Harness may be revoked independently from the user. A valid PIA may host multiple approved model artifacts. A model artifact may be approved for one classification and prohibited for another.

## 10.1 Baseline trust assumptions

PAPER v1 assumes:

- the cryptographic implementation is correct,
- trusted CP signing keys are protected,
- a registered Harness private key has not been stolen,
- Relay and PIA private keys are protected according to deployment profile,
- policy state distributed to Relay is authentic,
- the user's local OS may be imperfect but is not assumed to provide hardware attestation.

## 10.2 Explicit limitation: unmodified Harness proof

Without hardware-backed remote attestation, CP cannot mathematically prove that an open-source Harness binary on a machine controlled by an attacker is byte-for-byte unmodified. PAPER can strongly authenticate an **enrolled Harness identity**, detect revoked/duplicated/anomalous identities, require signed official build metadata, and bind credentials to protocol state; it cannot turn software-only self-reporting into a hardware root of trust.

This limitation must be documented rather than hidden.

Organizations requiring stronger executable integrity may enable an optional higher-assurance profile using platform attestation/MDM/TPM/Secure Enclave or equivalent controls, but this is not a general PAPER requirement.

---
# 11. Protocol Layering

PAPER has a layered design so transport, identity, protocol semantics, and application capabilities can evolve independently.

```text
+---------------------------------------------------------------+
| Harness / Relay / PIA / Runtime Application                   |
+---------------------------------------------------------------+
| PAPER Capability Extensions                                  |
| AI | Context | Tools | Provenance | Policy | Chat | Voice ... |
+---------------------------------------------------------------+
| PAPER Core                                                    |
| Peer identity | Session | Stream Contract | Exchange | Error   |
+---------------------------------------------------------------+
| PAPER Secure-Channel Binding                                  |
| connection binding | credential proof | lease binding         |
+---------------------------------------------------------------+
| QUIC/TLS                         | TLS 1.3/TCP                  |
+---------------------------------------------------------------+
| UDP                              | TCP                          |
+---------------------------------------------------------------+
```

The protocol must never depend on HTTP semantics. There is no URL routing model, status-code dependency, cookie model, `Authorization: Bearer`, HTTP content negotiation, or browser-origin assumption at the PAPER layer.

PAPER implementations may expose separate local administrative HTTP APIs for unrelated operational purposes, but those interfaces are not PAPER and must not become a hidden compatibility path for the Harness.

---

# 12. Transport Bindings

## 12.1 QUIC binding — preferred

QUIC is the preferred transport because PAPER needs multiple concurrent streams with different lifetimes and flow-control characteristics: inference token streams, chat, large file transfers, broadcasts, control messages, and telemetry should not be serialized behind one application-level TCP stream.

Requirements:

- QUIC v1 or a later explicitly approved version.
- TLS security as defined for the selected QUIC version.
- ALPN identifying PAPER, initially using a project-controlled experimental/private identifier; pursue formal registration when the protocol stabilizes.
- UDP/443 default for public/enterprise compatibility, configurable for private deployments.
- QUIC connection migration may be supported for mobile/network transitions but must not bypass PAPER peer/session policy.
- 0-RTT application data is **disabled by default** for PAPER v1 because replayable early data is inappropriate for policy-changing, tool, file, chat, or inference actions. Future use may be restricted to explicitly idempotent messages.

## 12.2 TLS/TCP binding — mandatory fallback

If QUIC cannot be established because of enterprise firewall/VPN/UDP restrictions, the client falls back to:

```text
TCP -> TLS 1.3 -> ALPN paper/* -> PAPER Core
```

Requirements:

- same peer identities,
- same session semantics,
- same message schemas,
- same capability leases,
- same policy requirements,
- same application-level stream IDs,
- no behavioral downgrade.

The TCP binding requires a PAPER multiplexing layer because TCP provides one ordered byte stream. A blocked file transfer must not indefinitely block a control/broadcast stream.

## 12.3 Transport selection

Harness behavior:

1. Resolve configured PAPER service.
2. Attempt QUIC unless organization policy disables it.
3. If QUIC fails under defined network errors/time budget, attempt TLS/TCP.
4. Never silently fall back to HTTP, WebSocket, an OpenAI endpoint, or an ungoverned direct model path.
5. Record selected transport in session provenance.

## 12.4 Transport parity requirement

An action allowed over QUIC must not become more privileged over TCP. Transport selection is not an authorization attribute except where an organization explicitly restricts a deployment to one binding.

---

# 13. Peer Profiles

PAPER is one protocol family with authenticated peer profiles. Profiles are not cosmetic labels; the credential and protocol state machine bind the peer to allowed message families.

Initial profiles:

| Profile | Purpose | Typical connection |
|---|---|---|
| `HARNESS` | Developer-facing Patty Code client | Harness ↔ Relay |
| `RELAY` | Governed data-plane enforcement service | Relay ↔ Harness / Relay ↔ PIA |
| `INFERENCE` | PIA/model-serving boundary | PIA ↔ Relay |
| `RUNTIME` | Controlled sandbox/runtime agent | Runtime ↔ Relay/CP |
| `ADMIN_AGENT` | Approved enterprise operational agent | Agent ↔ Relay/CP |
| `CI_AGENT` | Non-interactive CI/CD integration | CI ↔ Relay |

## 13.1 Profile isolation

A `HARNESS` credential MUST NOT open an `INFERENCE` registration stream. An `INFERENCE` peer MUST NOT send employee chat messages. A `RUNTIME` peer MUST NOT request model-registry administration.

Profile misuse produces a protocol-level authorization error, security event, and—depending on policy—connection termination or credential suspension.

## 13.2 Extension negotiation

Within a profile, features are negotiated independently. Example Harness capability set:

```text
paper.core/1
paper.ai/1
paper.context/1
paper.tools/1
paper.provenance/1
paper.policy/1
paper.chat/1
paper.voice/1
paper.file/1
paper.presence/1
paper.broadcast/1
paper.telemetry/1
```

An individual public Harness may not negotiate enterprise chat or broadcast. A government Harness may disable cloud-specific extensions while retaining the same core protocol.

---

# 14. Identity and Enrollment

## 14.1 Identity hierarchy

PAPER must support at least:

- organization identity,
- user identity,
- Harness installation identity,
- peer/service identity,
- session identity,
- inference endpoint identity,
- model artifact identity.

## 14.2 Harness enrollment

An enterprise Harness cannot participate merely by knowing a CP address. It must be enrolled.

Reference flow:

```text
User installs signed Patty Code
        |
        v
Harness creates local key pair
        |
        v
User authenticates to organization
        |
        v
CP evaluates entitlement + enrollment policy
        |
        v
CP registers Harness public key and installation record
        |
        v
CP issues Harness Credential
        |
        v
Harness may establish PAPER connections
```

Enrollment record SHOULD include:

- `harness_id`,
- public key / certificate identity,
- organization,
- primary user or permitted user set,
- initial registration time,
- last credential rotation,
- Harness release channel,
- reported build/version,
- platform family,
- revocation status,
- last-seen network metadata,
- risk state,
- allowed deployment(s),
- enterprise policy profile.

## 14.3 Enrollment modes

Support:

- user self-enrollment with organizational SSO,
- invitation/enrollment code,
- admin-provisioned installation,
- offline government enrollment bundle,
- mass enterprise enrollment through deployment tooling,
- ephemeral CI agent enrollment.

## 14.4 User binding

User authentication and Harness authentication are evaluated separately. A Harness credential proves the installation identity, not the human.

A session becomes usable for developer activity only when the organization can bind:

```text
User + Harness + Organization + Entitlement + Session
```

Support user switching only where organization policy permits it. A shared jump-box/server Harness may bind to multiple users sequentially, but each session must preserve the actual user identity.

---

# 15. Credential Lifecycle

## 15.1 Credential classes

PAPER should distinguish:

- long-lived organizational trust anchors,
- medium-lived enrollment credentials,
- short-lived connection credentials,
- short-lived capability leases,
- one-time enrollment/recovery tokens,
- artifact signing identities.

## 15.2 Harness private key

The Harness enrollment private key should be generated locally and stored using OS-provided secure credential/key storage where available. Export should be disabled by default.

Because hardware backing is not required, security claims must say **non-exportable where platform facilities permit**, not imply impossible extraction resistance on every device.

## 15.3 Rotation

The system must allow seamless key rotation:

1. authenticated Harness requests rotation,
2. old key signs rotation intent where possible,
3. CP issues challenge,
4. new key is registered,
5. overlap period permits active sessions to complete,
6. old credential is revoked,
7. historical receipts continue to verify against time-bounded trust metadata.

## 15.4 Revocation

Revocation reasons include:

- user terminated,
- device lost,
- suspected private-key compromise,
- duplicate/clone anomaly,
- unsupported Harness version,
- policy violation,
- administrative action,
- organization offboarding.

Relays must receive revocation information rapidly. Government disconnected environments use locally distributed signed revocation sets.

---

# 16. Connection and Session Lifecycle

A transport connection is not automatically an AI session.

## 16.1 Connection phases

```text
TRANSPORT_CONNECTING
        |
TRANSPORT_SECURE
        |
PAPER_NEGOTIATING
        |
PEER_AUTHENTICATING
        |
PEER_AUTHENTICATED
        |
CAPABILITIES_NEGOTIATED
        |
CONNECTION_ACTIVE
        |
DRAINING
        |
CLOSED
```

## 16.2 Initial connection handshake

Conceptual PAPER exchange after TLS/QUIC security establishment:

1. `CORE_HELLO`
2. `CORE_SERVER_CHALLENGE`
3. `CORE_PEER_PROOF`
4. `CORE_PROFILE_ACCEPT`
5. `CORE_CAPABILITIES_OFFER`
6. `CORE_CAPABILITIES_ACCEPT`
7. `CORE_CONNECTION_BIND`
8. connection becomes active.

The exact wire names belong to the formal specification, but the PRD requires these semantics.

## 16.3 Channel binding

Higher-level peer authentication/session credentials should be bound to the current secure transport using an exporter/channel-binding mechanism so that an authorization artifact captured from one transport instance cannot simply be replayed on another.

## 16.4 Harness working session

A developer session adds organizational context:

- user,
- organization,
- project,
- repository,
- branch/worktree,
- policy epoch,
- entitlements,
- allowed models,
- allowed tools,
- retention profile,
- collaboration context.

Session states:

```text
REQUESTED
  -> AUTHORIZED
  -> ACTIVE
  -> DEGRADED | PAUSED | APPROVAL_WAIT
  -> CLOSING
  -> CLOSED
  -> EVIDENCE_FINALIZED
```

A revoked/suspended session cannot open new protected exchanges.

---

# 17. PAPER Exchange Model

The **Exchange** is the central application object of PAPER.

## 17.1 Exchange philosophy

Traditional RPC often asks: “What method is being called?” PAPER additionally asks:

- Who is acting?
- For what purpose?
- Under which organizational scope?
- What authority allows it?
- What data classification is involved?
- Which prior event caused it?
- What evidence must be produced?

## 17.2 Exchange classes

Initial exchange classes:

- `AI_INFERENCE`,
- `CONTEXT_DISCLOSURE`,
- `TOOL_ACTION`,
- `RUNTIME_ACTION`,
- `ARTIFACT_TRANSFER`,
- `CHAT`,
- `VOICE`,
- `PRESENCE`,
- `BROADCAST`,
- `ADMIN_CONTROL`,
- `POLICY`,
- `PROVENANCE`,
- `TELEMETRY`.

## 17.3 Exchange identity

Every protected exchange has:

- globally unique exchange ID,
- parent/causal references,
- session ID,
- peer identities,
- policy epoch,
- capability lease reference,
- creation and completion timestamps,
- exchange class,
- content classification,
- provenance state,
- completion status.

## 17.4 Causal relationships

PAPER must represent more than chronological order. An exchange may explicitly state:

- caused by user instruction,
- derived from context request,
- spawned tool invocation,
- produced artifact,
- superseded earlier output,
- reviewed by human,
- merged into commit,
- retried after failure.

This causal graph becomes part of the Provenance Spine.

---

# 18. Capability Leases

Capability Leases are a central PAPER-specific security primitive at the application layer.

A Capability Lease is a short-lived, signed statement from CP authorizing a constrained set of actions. It is **not** a bearer API key with broad standing authority.

## 18.1 Lease binding dimensions

A lease may bind:

- organization,
- user,
- Harness,
- session,
- peer profile,
- project,
- repository,
- branch,
- allowed path patterns,
- data classifications,
- model IDs/versions,
- tool categories,
- network destinations,
- maximum tokens/bytes,
- concurrency,
- time window,
- policy epoch,
- current secure-channel binding or connection generation,
- approval requirement.

## 18.2 Lease examples

A normal coding lease might permit:

```text
read: repo src/**, tests/**
write: repo src/payment/**
model: patty-coder-v4
commands: test, lint, formatter
network: none
tool calls: git/read, shell/test
expires: 30 minutes
```

A file-transfer lease may authorize one sender, one recipient group, one content class, 25 MB maximum, and 10-minute expiry.

## 18.3 Lease enforcement

Relays and downstream peers verify leases independently. The Harness cannot enlarge a lease by modifying message metadata. If an operation falls outside lease scope, Relay returns a deny/approval-required verdict and emits evidence.

## 18.4 Lease renewal

Long-running sessions renew leases without losing provenance continuity. Renewal must produce a new lease ID and preserve causal relationship to the prior authorization.

---

# 19. Governance Envelope

Every protected exchange carries or references a **Governance Envelope**.

Required conceptual contents:

```yaml
organization_id: org_...
actor:
  user_id: usr_...
  harness_id: hrn_...
  session_id: ses_...
scope:
  project_id: prj_...
  repository_id: repo_...
  branch: feature/payment
classification: confidential
policy_epoch: pol_epoch_...
capability_lease_id: lease_...
purpose: implement-payment-retry
requested_effects:
  - model.invoke
  - context.read
retention_profile: ret_...
required_evidence:
  - prompt_hash
  - model_identity
  - code_provenance
```

The envelope is not necessarily repeated byte-for-byte on every token chunk. A stream may establish an immutable governance context once and reference it by ID/hash for subsequent frames.

## 19.1 Enforcement results

Policy decisions include:

- allow,
- allow with transformation,
- allow with obligations,
- require user confirmation,
- require reviewer approval,
- require security approval,
- throttle,
- quarantine,
- deny,
- terminate exchange,
- terminate session,
- create incident.

## 19.2 Reason codes

Every non-trivial policy decision must use machine-readable reason codes plus localized human explanations. Korean enterprise UI must not reduce denials to a generic “Forbidden.”

---

# 20. Policy Epochs and Policy Binding

A Policy Epoch identifies the exact effective policy state that governs an exchange.

## 20.1 Why epochs are required

Enterprise policies change during the day. An audit record saying “the request followed current policy” is insufficient if the current policy differs from the policy at the time of execution.

Each epoch references:

- mandatory Patty baseline version,
- organization policy version,
- project/repository overlays,
- model approval state,
- data-classification rules,
- exception set,
- effective time,
- policy bundle hash/signature.

## 20.2 Mid-session changes

When policy changes while a session is active:

- changes that only relax policy must not automatically expand existing leases;
- changes that tighten policy must be evaluated against active exchanges;
- emergency revocations may terminate affected streams immediately;
- new protected actions bind to the new epoch;
- history preserves the epoch used for each prior decision.

---

# 21. PAPER Provenance Spine

The Provenance Spine is the protocol-level causal/evidence graph that makes PAPER materially different from a model API plus logs.

## 21.1 Provenance nodes

Possible node classes:

- human intent,
- prompt,
- context item/span,
- policy decision,
- model invocation,
- model output segment,
- tool request,
- tool result,
- file read/write,
- generated patch,
- human edit,
- test execution,
- security finding,
- approval,
- artifact export,
- Git commit/PR,
- deployment/release reference,
- chat/file/voice object when used as engineering context.

## 21.2 Provenance edges

Examples:

- `DERIVED_FROM`,
- `AUTHORIZED_BY`,
- `INFLUENCED_BY`,
- `EXECUTED_BY`,
- `PRODUCED`,
- `MODIFIED`,
- `REVIEWED_BY`,
- `SUPERSEDES`,
- `COMMITTED_AS`,
- `TRANSFERRED_TO`.

## 21.3 Distributed authorship

Different peers contribute evidence:

- Harness: user intent, local context selection, human edits.
- Relay: policy/verdicts, transformations, security findings, routing.
- PIA: exact model endpoint, model artifact, inference lifecycle.
- Runtime: commands, environment, outputs.
- CP/Git connector: commit/PR/approval relationships.

No single peer should be allowed to silently rewrite the entire history.

---

# 22. Content Addressing and Evidence Receipts

## 22.1 Content-addressed records

Hash-critical provenance objects should have deterministic canonical representation and content digests. The protocol must support an approved hash-suite registry; SHA-256 is the conservative baseline for interoperability and Korean government profiles unless a deployment mandates another approved algorithm.

A digest is calculated over canonical object content, not over unstable JSON serialization or UI text.

## 22.2 Evidence receipts

Important lifecycle checkpoints create signed receipts. Examples:

- exchange accepted,
- context disclosure approved,
- inference started,
- model response completed,
- tool action executed,
- artifact exported,
- session closed,
- provenance finalized.

A receipt contains at minimum:

- receipt ID,
- exchange/session references,
- hashes of relevant manifests/results,
- peer identity,
- policy epoch,
- verdict/result,
- timestamp/time-source metadata,
- signature and algorithm ID.

## 22.3 Proof-carrying streams

Long-lived streams use:

1. **Intent Manifest** — establishes identity, authority, purpose, content class, and limits.
2. **Checkpoint Receipts** — cryptographically summarize accepted stream progress.
3. **Completion Receipt** — commits final outcome and provenance references.

For token streaming, PAPER must not require a heavyweight signature for each token. Implementations may group chunks into incremental hash chains or Merkle-style checkpoints while preserving the ability to detect omission/reordering within the evidence policy.

## 22.4 Independent verification

A exported evidence package should be verifiable without trusting the live CP database, provided the verifier has appropriate historical trust anchors, signatures, manifests, and redacted/hashed content references.

---

# 23. PAPER Relay Architecture

The Relay is the central governed data-plane component.

## 23.1 Responsibilities

Relay responsibilities include:

- authenticate PAPER peers,
- validate profile/capabilities,
- validate sessions and leases,
- enforce policy epoch,
- inspect/transform permitted content,
- apply DLP/PII/secrets controls,
- detect prompt-injection indicators,
- enforce model authorization,
- route to approved PIA endpoints,
- enforce rate/concurrency/token budgets,
- inspect model responses,
- emit Relay Verdicts,
- produce provenance/evidence events,
- deliver broadcasts/control messages,
- broker collaboration/file traffic according to policy.

## 23.2 Responsibilities Relay should not own

Relay should not become:

- the system of record for users,
- the primary admin UI,
- long-term evidence storage,
- a Git server,
- a model registry database,
- an HR evaluation system,
- a general enterprise message archive.

Those responsibilities live in CP or integrated systems.

## 23.3 Stateless versus sticky processing

Relay nodes should be horizontally scalable. Durable state belongs in CP/event/provenance systems. A Relay may hold connection/session/stream state and short-lived policy caches. QUIC connections naturally have a node affinity; failover uses PAPER resumption rather than pretending connections are stateless HTTP requests.

## 23.4 Regional/local Relays

Support:

- Patty Cloud Relay,
- customer private Relay,
- customer data-center Relay,
- air-gapped government Relay,
- regional Relay groups for data residency,
- dedicated high-sensitivity Relay pools.

---

# 24. Relay Enforcement Pipeline

An inference request passes defined checkpoints.

```text
Harness
  |
  | Exchange Intent
  v
[1] Identity + Lease Validation
  |
[2] Policy Epoch Resolution
  |
[3] Context Authorization
  |
[4] Secrets / PII / Classification
  |
[5] Injection / Content Trust Analysis
  |
[6] Model Authorization + Routing
  |
  | ---- PRE-INFERENCE VERDICT ----
  v
PIA / Model
  |
  v
[7] Response Inspection
  |
[8] Tool/Action Extraction Governance
  |
[9] Output Classification / DLP
  |
  | ---- POST-INFERENCE VERDICT ----
  v
Harness
```

## 24.1 Relay Verdict

Each configured checkpoint can emit a signed `RelayVerdict` containing:

- exchange ID,
- checkpoint class,
- policy epoch,
- decision,
- transformations,
- reason codes,
- obligations,
- scanner/evaluator versions,
- content digest references,
- timestamp,
- Relay identity/signature.

The protocol specification must define which verdicts are mandatory by exchange class.

## 24.2 Transformations

Examples:

- redact secret,
- tokenize PII,
- remove unauthorized context span,
- lower maximum output tokens,
- replace requested model with an approved equivalent only when policy explicitly allows such substitution,
- strip unsafe file attachment metadata,
- quarantine generated binary.

Transformation is evidence, not invisible middleware behavior.

---

# 25. Harness-to-Relay AI Flow

Reference flow:

1. Harness opens or resumes developer session.
2. CP/Relay confirms policy epoch and effective capability lease.
3. User enters task.
4. Harness creates `AI_INFERENCE` intent referencing project/repository/branch.
5. Harness proposes context references and local content digests.
6. Relay evaluates the Governance Envelope.
7. Relay requests missing classification/provenance information if necessary.
8. Approved context is transferred/streamed.
9. Relay transforms/redacts as required.
10. Relay emits pre-inference verdict.
11. Relay selects approved PIA endpoint.
12. Relay opens downstream PAPER inference stream.
13. Model response streams back through Relay.
14. Relay performs response checks and provenance correlation.
15. Response chunks reach Harness.
16. If model proposes tool actions, separate governed tool exchanges are created.
17. Exchange completes with receipt/evidence references.

The Harness UI should be capable of showing important governance facts without exposing protocol complexity:

```text
Model: Patty Coder 35B v4
Policy: Enterprise Secure Development v12
Context: 14 files / 21,842 tokens
Secrets removed: 2
PII removed: 0
Network: disabled
Trace: enabled
```

---

# 26. Relay-to-PIA Inference Flow

Relay must never forward AI traffic to an arbitrary host solely because an administrator typed an IP address and a model name.

Reference flow:

1. PIA enrolls with CP/model registry.
2. PIA establishes PAPER connection as `INFERENCE` profile.
3. PIA presents endpoint identity and registered model inventory.
4. Relay verifies active endpoint lease and model authorization.
5. Relay opens a model-scoped stream contract.
6. PIA confirms the exact model package/version it will serve.
7. Relay sends governed inference payload.
8. PIA translates internally to the serving engine API/IPC as needed.
9. PIA streams model output back over PAPER.
10. PIA reports model/inference metadata required for provenance.
11. Relay completes post-response controls.

Raw vLLM/SGLang endpoints should normally be network-restricted so only PIA can reach them.

---

# 27. Patty Inference Agent

PIA is the security/protocol adapter adjacent to model serving.

## 27.1 Why PIA exists

Without PIA, CP would have to trust arbitrary OpenAI-compatible serving endpoints. A user could deploy Qwen, rename it to a Patty model identifier, and claim it is approved.

PIA provides:

- PAPER peer identity,
- endpoint enrollment,
- model package verification,
- serving-engine isolation,
- model metadata/provenance,
- health/capacity reporting,
- policy-relevant inference settings,
- protocol translation to vLLM/SGLang local interfaces.

## 27.2 PIA implementation model

PIA should be deployable as:

- sidecar on the model-serving host,
- daemon on the inference node,
- dedicated proxy in the model network,
- Kubernetes sidecar/gateway with strict network policy.

Do not require a permanent fork of vLLM/SGLang unless a future capability truly cannot be implemented externally. Staying outside the serving engine reduces upgrade lag and makes serving-engine support extensible.

## 27.3 PIA authority

PIA cannot alter CP policy or invent model approvals. It proves its identity, verifies/declares serving state, and executes only Relay-authorized inference.

---

# 28. Approved Model Identity and Model Packages

## 28.1 Patty Model Package (PMP)

A PMP is a signed manifest representing an approved deployable model artifact.

Conceptual fields:

- package ID/version,
- model family,
- base/fine-tune lineage,
- weight artifact digest(s),
- tokenizer digest,
- chat/template digest,
- quantization profile,
- inference-engine compatibility,
- context limit,
- license/use restrictions,
- expected evaluation bundle,
- creation/release metadata,
- Patty or authorized publisher signature.

## 28.2 Endpoint registration

An endpoint is approved only when CP associates:

```text
PIA identity
 +
PMP identity
 +
deployment profile
 +
endpoint/network identity
 +
approval state
```

## 28.3 Software-only assurance limitation

A PIA running on a host fully controlled by a malicious root administrator can theoretically falsify software-only measurements. PAPER baseline therefore promises authenticated PIA/model package workflow and operational enforcement, not impossible remote proof without a hardware root of trust.

Optional government/high-assurance deployments may add host/TEE/GPU attestation, but the open protocol remains usable without it.

## 28.4 Endpoint lease

Relays route only to endpoints with current signed endpoint leases. Suspension/revocation must stop new inference quickly.

---

# 29. Stream and Lane Architecture

PAPER is not one giant ordered stream.

## 29.1 Logical lanes

A connection may carry logical lanes such as:

```text
CONTROL
AI
TOOLS
CONTEXT
PROVENANCE
CHAT
VOICE
FILE
PRESENCE
BROADCAST
TELEMETRY
```

On QUIC, lanes map to one or more QUIC streams. On TCP, PAPER multiplexes them using application stream IDs and independent flow-control/priority semantics.

## 29.2 Stream Contract

Before sending protected payloads, a peer establishes a Stream Contract containing:

- stream type,
- exchange class,
- source/destination peer profiles,
- capability lease,
- policy epoch,
- content class,
- confidentiality/protection mode,
- maximum bytes/messages,
- ordering requirement,
- resumability requirement,
- retention/evidence requirement,
- priority.

## 29.3 Priorities

Recommended priority order:

1. emergency/admin security control,
2. connection/session control,
3. policy/approval,
4. interactive inference token stream,
5. tool/runtime results,
6. chat/presence,
7. provenance/evidence,
8. file transfer/voice upload,
9. bulk telemetry.

A multi-gigabyte file transfer must not delay an emergency broadcast.

---

# 30. AI Inference Message Family

PAPER AI messages represent an agentic inference lifecycle, not merely a text completion.

Required semantic operations include:

- inference intent/open,
- model requirement/preferences,
- context manifest,
- context chunk/reference,
- inference accepted/rejected,
- input payload,
- output token/text delta,
- structured output delta,
- tool proposal,
- usage/capacity checkpoint,
- model metadata,
- stop/cancel,
- completion,
- error,
- provenance checkpoint.

## 30.1 Model selection

Harness may express requirements, but CP/Relay remains authoritative. Examples:

- `coding_general`,
- `coding_high_reasoning`,
- `review_security`,
- `embedding`,
- exact approved model when policy permits user selection.

## 30.2 Streaming

Streaming must support:

- partial text/token delivery,
- structured/tool-call deltas,
- cancellation,
- backpressure,
- output size limits,
- checkpoints for resumption/evidence.

## 30.3 Tool proposals

A model's request to execute a tool is never authority. It becomes a new governed `TOOL_ACTION` exchange evaluated under the current capability lease/policy.

## 30.4 Inference metadata

Provenance metadata should include:

- exact model package,
- PIA/endpoint identity,
- serving engine and approved relevant configuration,
- sampling/inference parameters where policy records them,
- token counts,
- start/end time,
- termination reason,
- retry/fallback lineage.

---
# 31. Context and Repository Message Family

Context is a governed resource, not an unstructured blob appended to a prompt.

## 31.1 Context object types

PAPER should support references/content for:

- repository file,
- file span,
- symbol/AST node,
- directory tree,
- Git diff,
- commit,
- PR/MR,
- issue/ticket,
- build/test log,
- database/schema description,
- API specification,
- document/wiki page,
- user-provided attachment,
- prior AI exchange,
- chat/voice/file object explicitly shared into engineering context.

## 31.2 Context manifest

Before bulk content is transmitted, Harness sends a manifest describing proposed items:

```yaml
items:
  - context_id: ctx_1
    type: repo_span
    repo: repo_123
    commit: a92c...
    path: src/payments/retry.ts
    range: [120, 198]
    digest: sha256:...
    classification: internal
    reason: required_by_symbol_dependency
```

Relay can allow, deny, request transformation, or restrict a span before content crosses the boundary.

## 31.3 Context minimization

Policy may impose:

- maximum files,
- maximum bytes/tokens,
- path allow/deny patterns,
- source-code classifications,
- secrets/PII removal,
- no full-repository upload,
- metadata-only mode for protected files.

## 31.4 Context trust labels

Retrieved content can contain instructions. PAPER context objects carry trust attributes such as:

- authoritative policy/system,
- trusted organization documentation,
- user-authored instruction,
- repository content,
- external/untrusted content,
- generated content.

Trust labels never grant tool authority; they inform model prompting and injection defenses.

## 31.5 Repository binding

For coding sessions, repository context must bind to a concrete base state (commit/tree hash or equivalent) so later provenance can distinguish what the model actually saw from current repository state.

---

# 32. Tool and Runtime Message Family

PAPER must represent agent tool activity as governed exchanges.

## 32.1 Tool classes

- file read/write/search,
- shell/build/test,
- Git operations,
- package/dependency management,
- MCP/server tool invocation,
- network/API access,
- database/test service,
- artifact generation,
- runtime/sandbox management.

## 32.2 Tool intent

A tool request includes:

- proposing peer/model,
- human/session context,
- tool identity/version,
- arguments in structured form where possible,
- predicted effects,
- required capabilities,
- target resources,
- risk classification,
- parent inference exchange.

## 32.3 Deterministic enforcement

Relay/CP policy, not the model, decides whether a tool is available. A model cannot elevate capabilities by text instructions.

## 32.4 Command model

Shell commands should be transmitted as structured command objects where practical:

- executable,
- argv,
- working directory,
- environment references,
- expected file effects,
- network requirement,
- timeout,
- resource budget.

Raw shell text can be supported but receives stricter policy/inspection.

## 32.5 Result model

Tool results include:

- exit/result code,
- stdout/stderr or structured result,
- created/modified artifact references,
- resource usage,
- policy transformations,
- runtime identity,
- execution digest,
- provenance references.

## 32.6 Runtime separation

PAPER does not assume tools run on the developer host. Tool exchange can target a `RUNTIME` peer representing a local governed runner, remote sandbox, CI runner, or government microVM pool.

---

# 33. Collaboration Message Family

PAPER provides contextual engineering collaboration inside the Harness.

## 33.1 Goals

- allow employees to communicate without leaving the coding surface,
- work in restricted/closed-network environments,
- link conversation directly to engineering objects,
- preserve organization policy/retention requirements,
- avoid turning CP into a generic social network.

## 33.2 Conversation types

- 1:1 direct message,
- group conversation,
- project channel,
- repository channel,
- incident channel,
- temporary session/handoff channel,
- administrative/system conversation.

## 33.3 Message types

- text,
- formatted/code block,
- source reference,
- provenance reference,
- commit/PR reference,
- session reference,
- file attachment,
- voice message,
- system event,
- acknowledgement.

## 33.4 Message object requirements

Each message carries:

- message and conversation IDs,
- sender user and optional Harness identity,
- organization/tenant,
- classification,
- body/content reference,
- mentions,
- linked engineering objects,
- created/edited/deleted state,
- retention policy,
- encryption/protection mode,
- provenance/evidence policy.

## 33.5 Access re-evaluation

Sharing a link to a repository file, prompt trace, security finding, or session does not grant access. Recipient authorization is checked when opening the linked object.

## 33.6 Message editing/deletion

Organizations configure whether users can edit/delete messages and for how long. Audit metadata must distinguish:

- content removed from ordinary user view,
- content retained under legal/audit policy,
- content cryptographically unavailable because endpoint-only encryption was selected.

---

# 34. Voice Messaging

Initial PAPER voice support focuses on **asynchronous voice messages**, not full real-time calling.

## 34.1 User experience

Harness TUI/IDE may let a user record a voice note, attach it to a conversation/project/session, and send it to authorized recipients.

## 34.2 Voice transfer

Voice messages are streamed/chunked as a specific content class with:

- voice message ID,
- sender/conversation,
- codec and sample metadata,
- duration,
- chunk sequence,
- content digest,
- transcript reference if generated,
- classification,
- retention/protection profile.

Opus should be the preferred baseline codec unless environment constraints require another open codec.

## 34.3 Transcription

If organization policy enables transcription:

- transcription uses an approved local/Patty model,
- the voice object remains the source artifact,
- transcript records model/version and confidence metadata,
- sensitive-data policy applies before any cloud transcription,
- transcript may become searchable but follows the original message's access controls.

## 34.4 Provenance links

A voice message explicitly used as an AI task input can be linked into the Provenance Spine as human intent/context rather than becoming an untracked side channel.

## 34.5 Future live audio

Live voice/calling is not required for v1. If added, it should use a separate PAPER real-time media extension rather than overloading asynchronous voice-message semantics.

---

# 35. Managed File Transfer

Files are a high-risk exfiltration path and therefore a first-class governed exchange.

## 35.1 Supported flows

- employee-to-employee,
- project/group attachment,
- Harness session handoff,
- logs/test reports,
- patch/diff,
- architecture/document artifact,
- incident evidence,
- approved binary archive.

## 35.2 Transfer lifecycle

```text
OFFER
 -> METADATA_POLICY
 -> CONTENT_POLICY / SCANNING
 -> RECIPIENT_AUTHORIZATION
 -> ACCEPT
 -> CHUNK_TRANSFER
 -> VERIFY
 -> STORE/DELIVER
 -> RECEIPT
```

## 35.3 Chunking and resumption

Large files use content-addressed chunks with:

- deterministic chunk IDs/digests,
- independent flow control,
- resumable offsets/chunk maps,
- final object digest verification,
- duplicate-content optimization only within authorized tenant/security scope.

## 35.4 Security gates

Configurable gates:

- classification,
- secret/PII scanning,
- malware scanning,
- archive inspection,
- extension/MIME restrictions,
- max size,
- sender/recipient relationship,
- cross-affiliate boundary,
- contractor restrictions,
- retention/expiry,
- download count,
- watermarking for supported documents.

## 35.5 No implicit AI context

Receiving a file in chat does not automatically send the file to a model. A separate `CONTEXT_DISCLOSURE` exchange is required when a file is introduced into AI context.

---

# 36. Presence and Availability

Presence is operationally useful but must remain low-risk metadata.

States may include:

- online,
- available,
- busy,
- away,
- do not disturb,
- offline,
- active coding session,
- incident-response mode,
- custom status.

Distinguish:

- user presence,
- Harness connectivity,
- active AI session,
- current project/repository visibility.

Organizations control which details are visible to peers. Presence data should have short retention by default and must not be treated as a primary employee productivity metric.

---

# 37. Broadcast and Emergency Messaging

Broadcasts use PAPER but have stronger delivery and acknowledgement semantics than ordinary chat.

## 37.1 Severities

| Level | Example | Default UX |
|---|---|---|
| `INFO` | policy/news/maintenance | notification center |
| `ADVISORY` | upcoming model migration | persistent banner |
| `WARNING` | degraded service / risky dependency | prominent banner |
| `CRITICAL` | active credential/security incident | interruptive alert + optional ack |
| `EMERGENCY` | stop work / containment | blocking alert; may link enforcement action |

## 37.2 Targeting

Target expressions can include:

- whole organization,
- affiliate/business unit,
- department/team,
- named group/users,
- project/repository,
- active Harnesses,
- users of model/version/tool,
- network zone/location,
- Harness release ring,
- sessions matching security criteria.

## 37.3 Broadcast object

Includes:

- sender/authority,
- severity,
- localized title/body,
- target expression,
- start/expiry,
- acknowledgement requirement/deadline,
- linked incident/runbook/policy,
- action buttons,
- optional enforcement action reference,
- delivery/read/ack statistics,
- signed audit evidence.

## 37.4 Message versus enforcement

A critical design rule:

> A broadcast message does not itself change policy.

“Stop using model X” and “suspend model X” are separate objects. They can be linked, but the latter requires an authorized administrative-control exchange.

---

# 38. Administrative Control Messaging

PAPER can deliver control-plane actions to connected Harness/Relay/PIA peers.

Examples:

- require session reauthentication,
- refresh policy epoch,
- revoke capability lease,
- pause new inference,
- terminate session,
- quarantine Harness,
- force model drain,
- request diagnostic bundle,
- require Harness upgrade by deadline,
- enter maintenance mode,
- initiate emergency lockdown profile.

## 38.1 Authorization

Admin-control messages require:

- privileged user/service identity,
- explicit authority scope,
- policy decision,
- optional second approver,
- signed control object,
- target specificity,
- expiry/replay protection,
- audit evidence.

## 38.2 Harness transparency

When appropriate, users should see why a session was paused/terminated rather than receiving a generic network error. Security policy may limit details during an active incident.

---

# 39. Telemetry and Usage Signals

PAPER carries operational telemetry but telemetry should not compete with interactive traffic.

## 39.1 Telemetry classes

- connection health,
- Harness version/feature state,
- session metrics,
- inference token usage,
- model latency,
- Relay enforcement counts,
- tool/runtime duration,
- file-transfer metrics,
- message delivery state,
- error/retry statistics,
- capacity/queue signals,
- security events.

## 39.2 Privacy

Telemetry is metadata-first. Raw prompts, code, chat, or files should not be copied into telemetry events unless the event type and retention policy explicitly require content evidence.

## 39.3 Metering integrity

Billing/chargeback metrics should be correlated with signed exchange/PIA receipts where feasible so customer-facing usage is reconcilable rather than based on unverified client counters.

---

# 40. Payload Protection Profiles

Transport encryption protects data between immediate peers. PAPER additionally needs explicit semantics for **who is permitted to access plaintext**.

## 40.1 Protection modes

### Mode P0 — Relay-Inspectable

- TLS/QUIC protects transport.
- Relay decrypts content and may inspect/transform according to policy.
- Default for governed AI prompt/context/response traffic where inline DLP/security is required.

### Mode P1 — Service-Sealed

- Payload is additionally sealed to a named authorized processing service/key domain.
- Intermediate Relay routing components may handle metadata without plaintext.
- Useful when Relay is decomposed into routing and inspection services.

### Mode P2 — Endpoint-Sealed

- Payload is end-to-end protected between designated endpoints, such as Harness ↔ PIA.
- Relay sees governance/routing metadata but cannot inspect content.
- This mode **cannot** simultaneously claim inline content DLP at Relay; organizations must accept that tradeoff.
- Disabled by default for protected enterprise AI inference.

### Mode P3 — Group E2EE

- For collaboration channels explicitly requiring endpoint-only group confidentiality.
- Use a standard group key protocol such as MLS rather than inventing a PAPER group-crypto algorithm.
- Server-side content inspection, eDiscovery, and retention may be limited/incompatible unless organization deploys a separately defined compliant archival mechanism.

## 40.2 Policy binding

The chosen protection mode is part of the Stream Contract and Governance Envelope. A peer cannot downgrade the protection requirement silently.

## 40.3 Metadata minimization

Even when payload is endpoint-sealed, PAPER should minimize exposed metadata and clearly document what remains visible: peer identity, sizes, timing, policy labels, routing, and exchange references may still reveal information.

---

# 41. Cryptography and Key Management

PAPER does not define proprietary cryptographic primitives.

## 41.1 Cryptographic domains

Separate keys/trust domains for:

- organization/CP trust,
- peer enrollment credentials,
- Relay service identity,
- PIA identity,
- session/capability lease signing,
- model-package signing,
- evidence receipt signing,
- payload sealing,
- tenant data at rest,
- offline update signing.

Avoid a single root key whose compromise invalidates every trust domain.

## 41.2 Algorithm agility

PAPER defines algorithm identifiers/registries so deployments can meet different cryptographic requirements.

Global baseline may support modern common suites; Korean government profiles may restrict selection to algorithms/modules accepted in the deployment's applicable KCMVP/organizational policy. The protocol semantics must not depend on one signature/hash family.

## 41.3 Payload sealing

Where application-layer recipient sealing is needed, use standardized constructions such as HPKE rather than designing custom hybrid encryption.

## 41.4 Group messaging

Where endpoint-only group encryption is selected, use MLS or another formally approved standard rather than custom group-key rotation.

## 41.5 Key lifecycle

All key types need:

- creation,
- activation,
- rotation,
- overlap/grace,
- revocation,
- compromise handling,
- historical verification metadata,
- destruction/expiry.

## 41.6 Cryptographic profile negotiation

Peers advertise supported suites; the authoritative policy selects from the intersection. A peer must fail closed rather than negotiate below organization minimum.

---

# 42. Replay Protection, Idempotency, and Resumption

Agent sessions can last minutes or hours. Networks fail. PAPER must resume safely without replaying side effects.

## 42.1 Layered replay defenses

Use:

- transport packet/record protections,
- connection/session nonces,
- monotonically ordered stream sequences,
- unique exchange IDs,
- capability lease expiry,
- idempotency keys for side-effecting operations,
- checkpoint receipts.

## 42.2 0-RTT

PAPER v1 must not execute protected application actions from QUIC 0-RTT early data. Future versions may permit narrowly defined idempotent messages after explicit analysis.

## 42.3 Exchange retry rules

Each operation class declares retry behavior:

- safe to replay,
- retry only with same idempotency key,
- requires status query before retry,
- never automatic.

Tool execution, file finalization, broadcasts, and administrative actions require particularly careful idempotency semantics.

## 42.4 Session resumption

A reconnecting Harness presents a session resumption proof/token and last verified checkpoints. Relay/CP determines:

- whether session still valid,
- current policy epoch,
- lease renewal requirement,
- which streams are resumable,
- last acknowledged sequence/chunk,
- whether in-flight inference can be reattached or must terminate/restart.

## 42.5 Inference disconnect

If Harness disconnects but model inference continues:

- organization policy decides cancel vs short grace period,
- Relay buffers only within defined memory/retention limits,
- reconnect may resume from checkpoint if the response is still available,
- provenance records disconnect and continuation.

---

# 43. Versioning and Extensibility

PAPER must be usable by enterprises that update slowly and government systems that may remain offline for extended periods.

## 43.1 Version layers

- transport binding version,
- PAPER core major version,
- capability extension versions,
- schema/object versions,
- policy/evidence schema versions.

## 43.2 Core version negotiation

Major incompatible core versions negotiate during connection establishment. Unsupported major versions fail explicitly.

## 43.3 Extensions

Extensions use stable identifiers such as conceptual:

```text
paper.ai/1
paper.provenance/1
paper.chat/1
```

The final public namespace may change before protocol registration.

## 43.4 Critical versus optional fields

Unknown optional fields are ignored/preserved according to schema rules. Unknown critical fields/extensions cause the affected message/stream to fail rather than silently dropping security semantics.

## 43.5 Deprecation

Every deprecated capability needs:

- deprecation version/date,
- security rationale if applicable,
- supported replacement,
- enterprise grace policy,
- government/offline migration guidance.

---

# 44. Error and Failure Model

PAPER errors must be machine-actionable and human-explainable.

## 44.1 Error domains

- transport,
- protocol framing,
- peer authentication,
- profile/capability,
- session,
- lease,
- policy,
- context,
- security scanning,
- inference/model,
- runtime/tool,
- collaboration,
- file transfer,
- quota/rate,
- provenance/evidence,
- administrative.

## 44.2 Error object

Conceptually includes:

- stable error code,
- severity,
- retry class,
- affected exchange/stream,
- human message key/localized detail,
- policy reason codes,
- incident/reference ID where applicable,
- safe diagnostic metadata.

## 44.3 Fail closed

Protected operations fail closed when required identity, policy, model authorization, DLP/security, or evidence services are unavailable beyond configured safe buffering/caching thresholds.

## 44.4 Degraded mode

Organizations may define a read-only degraded mode that allows local explanation/search but prohibits model disclosure, tool execution, file export, or other protected actions.

---

# 45. Resource, Flow-Control, and DoS Requirements

## 45.1 Connection limits

Relays enforce:

- connections per Harness/user/IP/organization,
- authentication attempt rate,
- streams per connection,
- pending exchanges,
- bytes/token limits,
- file/voice sizes,
- malformed-frame thresholds.

## 45.2 Authenticate before expensive work

Unauthenticated peers must not trigger model allocations, large memory buffers, database-heavy queries, or expensive scanners.

## 45.3 Stream-level backpressure

Flow control must preserve priority. Telemetry/file uploads back off before interactive token streams and control messages.

## 45.4 Maximum object sizes

The protocol defines bounded lengths for:

- core frames,
- manifest objects,
- single metadata fields,
- chat messages,
- chunks,
- pending reassembly buffers.

Large content is streamed/chunked rather than encoded into one giant object.

## 45.5 Abuse response

Progressive responses:

- throttle,
- stream reset,
- connection drain,
- connection close,
- Harness risk flag,
- credential temporary suspension,
- incident creation.

---

# 46. Security Threat Model

PAPER's security design must be tested against explicit adversaries.

| Threat | Example | Required mitigation |
|---|---|---|
| Unregistered client | custom client connects to CP | peer enrollment + cryptographic proof |
| Stolen Harness key | attacker copies credential | short-lived creds, anomaly detection, revoke/rotate, channel binding |
| Modified Harness | open-source binary changed | enrollment identity + signed-release policy; document software-only attestation limit |
| Protocol reverse engineering | attacker implements PAPER | safe by design; open spec; auth/policy remains boundary |
| Replay | captured message repeated | exchange IDs, sequences, expiry, channel binding, idempotency |
| Downgrade | force TCP/old extension | transport parity, minimum-version policy, signed negotiation context |
| Fake PIA | arbitrary server claims model | enrolled PIA identity + endpoint lease + PMP verification |
| Fake model name | Qwen renamed Patty | package digest/signature registry, not string name |
| Malicious Relay | modifies exchange | end-to-end receipts/digests where applicable, signed Relay verdicts, evidence verification |
| Compromised model | asks for secret/network | capability/tool authority external to model |
| Prompt injection | README changes agent behavior | context trust labels + deterministic permissions + injection controls |
| Data exfiltration | prompt/file sent externally | inline Relay DLP + network restrictions + destination/model policy |
| Cross-tenant leak | wrong org data routed | tenant-bound identities/leases/keys + tests |
| Admin abuse | admin reads unnecessary content | granular roles, purpose-bound access, JIT, audit |
| File malware | employee shares executable | transfer scanning + quarantine + policy |
| Broadcast abuse | compromised manager sends emergency | strong authorization, optional dual approval, signed audit |
| DoS | connection/stream flood | quotas, stateless protections, early auth, flow control |
| Evidence tamper | delete/alter history | signed/content-addressed receipts + external durable storage |
| Clock manipulation | forged event ordering | monotonic sequence + trusted time metadata + causal graph |

## 46.1 Egress limitation

PAPER makes the official Harness non-compatible with generic external model endpoints. Preventing a malicious employee from writing a separate HTTPS client to OpenAI requires OS/network egress controls outside the protocol. CP should integrate with enterprise network/security controls, but PAPER must not overclaim this guarantee.

## 46.2 Relay compromise modes

For high-assurance deployments, consider splitting Relay functions:

- edge connection terminator,
- policy authorizer,
- content inspector,
- routing worker,
- evidence signer.

Service-sealed payloads can reduce plaintext exposure to components that do not require inspection.

---

# 47. Privacy and Administrative Visibility

PAPER supports strong administrative visibility but does not treat administrator access as inherently unregulated.

## 47.1 Visibility levels

### Operational metadata

- user/Harness/session,
- repository/branch,
- model,
- token usage,
- message/file counts,
- policy outcomes,
- alert counts.

### Engineering content

- prompts/responses,
- context spans,
- diffs,
- tool outputs.

### Collaboration content

- chat bodies,
- voice messages/transcripts,
- attachments.

### Security/evidence content

- DLP findings,
- incident snapshots,
- evidence exports.

## 47.2 Access policy

Organizations can configure roles such as:

- platform admin,
- security operator,
- AI governance,
- engineering manager,
- auditor,
- HR/work-intelligence reviewer.

Viewing sensitive content itself is an audited action. Search/query interfaces should preserve purpose and authorization context.

## 47.3 User awareness

Enterprise deployments should clearly document organizational monitoring/retention policies. The Harness can display effective policy summaries so users understand what is retained/visible.

---

# 48. Korean Enterprise Requirements

PAPER is globally implementable but Korean enterprise behavior is a first-class product input.

## 48.1 Korean identity and organization

Support:

- Korean names and Romanized names,
- hierarchical org/affiliate structures,
- 직급/직책 as distinct optional attributes,
- teams, departments, 본부, 법인/계열사,
- contractor/SI identities,
- KST display and reporting.

## 48.2 Korean PII/security classification

Relay policy integrations should recognize Korean-sensitive patterns/data classes including resident registration numbers, foreigner registration numbers, phone/email/address, financial identifiers, employee identifiers, and organization-defined sensitive terms.

PAPER itself carries classification/result metadata; detection engines are replaceable components.

## 48.3 Centralized administration

Korean enterprise CP deployments should support detailed real-time views by:

- user,
- Harness,
- team,
- affiliate,
- project,
- repository,
- model,
- policy,
- security finding,
- time range.

PAPER must supply sufficient signed/structured events to power those views.

## 48.4 SI/contractor mode

A contractor may be authorized for one project/repository/time period with:

- separate Harness registration,
- stricter file/chat policies,
- no cross-project visibility,
- explicit offboarding expiry,
- export/evidence handoff.

## 48.5 Enterprise emergency controls

Korean enterprises frequently require centralized rapid control. PAPER should make it possible to:

- recall a model,
- disable an extension/tool,
- force policy refresh,
- pause high-risk sessions,
- issue mandatory acknowledgement,
- enforce a change freeze.

---

# 49. Government and Air-Gapped Requirements

Government uses the **same PAPER protocol**, not a separate fork.

## 49.1 No cloud dependency

Air-gapped operation must support local:

- CP,
- Relay,
- PIA,
- model registry,
- identity integration,
- trust anchors,
- revocation lists,
- provenance/evidence stores,
- collaboration data,
- update bundles.

## 49.2 Offline enrollment

Support signed enrollment bundles and local organization CAs/trust anchors. No online Patty authorization server may be required for routine operation.

## 49.3 Offline updates

Protocol/schema/implementation updates arrive as signed offline bundles containing:

- binaries,
- schemas,
- trust metadata,
- model/package updates,
- policy packs,
- migration tools,
- release notes,
- rollback artifacts.

## 49.4 Cryptographic restrictions

Government profile supports deployment-specific allowed suites and KCMVP-aware module requirements where applicable. PAPER's wire semantics remain algorithm-agile.

## 49.5 Evidence export

Audit/evidence packages must be verifiable offline and allow selective redaction without falsely representing omitted content as present.

## 49.6 Collaboration in closed networks

Chat/voice/file/broadcast remain fully functional inside the closed PAPER deployment, which is an important differentiator versus cloud-dependent collaboration tools.

---

# 50. Public, Enterprise, and Government Product Profiles

| Capability | Public Individual | Enterprise | Government/Sovereign |
|---|---:|---:|---:|
| PAPER core | Yes | Yes | Yes |
| QUIC | Yes | Yes | Configurable |
| TLS/TCP fallback | Yes | Yes | Yes |
| Patty-hosted Relay | Yes | Optional | No required |
| Customer Relay | No | Yes | Yes |
| PIA | Patty-managed | Patty/customer | Customer/local |
| AI inference | Yes | Yes | Yes |
| Provenance | service-level | full | full/local |
| Policy leases | service defaults | full | full/strict |
| Enterprise chat | No/optional future | Yes | Yes |
| Voice messages | No/optional | Yes | Yes |
| File transfer | limited | Yes | Yes |
| Broadcasts | service notices | Yes | Yes |
| Offline operation | No | optional private | Yes |
| Customer keys | No | optional | expected |
| Admin CP | No | Yes | Yes |
| Air-gap update | No | optional | Yes |

The core protocol implementation should avoid unnecessary edition branches. Product entitlement controls capability availability.

---
# 51. Control Plane Integration

PAPER is a protocol project, but CP is its authoritative enterprise control system.

## 51.1 CP responsibilities consumed by PAPER

- organization/user/Harness registry,
- peer/service identities,
- capability lease issuance,
- policy epoch publication,
- model/package/endpoint registry,
- approval service,
- broadcast authority,
- provenance persistence,
- evidence verification,
- collaboration directory,
- entitlements/billing,
- admin actions,
- revocation.

## 51.2 CP-to-Relay state distribution

Relay needs low-latency access to signed/cacheable state. Do not perform a synchronous database/HTTP round trip to CP for every token.

Use a model such as:

- signed policy snapshots,
- lease objects,
- revocation feed,
- endpoint inventory feed,
- incremental configuration/event distribution,
- bounded caches with expiry/fail-closed behavior.

The internal distribution implementation is not necessarily PAPER v1, though using an `ADMIN_AGENT`/service PAPER profile is preferred if it remains clean.

## 51.3 Administrative actions

CP admin UI creates signed control objects which PAPER delivers to the relevant Relay/Harness/PIA. PAPER receipts return execution/acknowledgement state.

---

# 52. Git/SCM and Code Provenance Integration

PAPER's engineering provenance becomes valuable only if it maps to source-control reality.

## 52.1 Repository identity

A repository object includes:

- organization/project,
- canonical SCM remote or internal repo ID,
- repository fingerprint/UUID,
- default branch,
- protection rules,
- code-owner metadata,
- sensitivity/classification.

## 52.2 Session baseline

At task/session start, establish:

- repository,
- base commit/tree,
- branch/worktree identity,
- dirty-state digest if local changes exist,
- Harness user identity.

## 52.3 File provenance

File-change provenance needs more than line numbers. Track:

- patch hunks,
- content digests,
- symbol/AST identity where available,
- rename/move lineage,
- semantic fingerprints,
- human/AI modification sequence.

Attribution states can include:

- human original,
- AI generated,
- AI modified human code,
- human modified AI code,
- AI refactored AI code,
- copied template/generated artifact,
- ambiguous/mixed.

## 52.4 Commit binding

When code reaches Git, connector/Harness emits a provenance binding:

```text
PAPER session/exchanges
    -> candidate diff
    -> human edits
    -> commit SHA
    -> PR/MR
    -> review/merge
```

Git commit trailers/notes/status checks may contain references, but the authoritative detailed evidence should remain outside Git to avoid exposing prompts/sensitive content and to survive history operations.

## 52.5 Export formats

PAPER provenance should be exportable or translatable to established software-provenance/attestation ecosystems where practical, while preserving PAPER-specific AI/governance fields.

---

# 53. Billing, Metering, and Entitlements

PAPER carries trusted usage signals needed by public and enterprise product models.

## 53.1 Metered dimensions

- active users,
- enrolled Harnesses,
- concurrent Harness sessions,
- inference requests,
- input/output tokens,
- model tier,
- GPU/inference time,
- tool/runtime minutes,
- file storage/transfer,
- collaboration storage if commercially relevant.

## 53.2 Authority

Billing should prefer Relay/PIA/CP evidence over client self-reported counters.

## 53.3 Entitlements

Capability leases should reflect entitlement constraints:

- organization plan,
- user seat,
- model availability,
- monthly/annual quotas,
- concurrency limits,
- feature extensions,
- dedicated GPU allocations.

## 53.4 Government licensing

Air-gapped licensing must not require online per-request authorization. Use signed offline entitlements with expiry/renewal procedures appropriate to procurement contracts.

---

# 54. Observability and Operations

PAPER implementations must be operationally transparent without leaking unnecessary payload content.

## 54.1 Metrics

Transport:

- QUIC/TCP connection success,
- fallback rate,
- handshake latency,
- retransmission/loss,
- connection migration,
- stream reset rates.

Protocol:

- active sessions,
- streams by family,
- exchange latency,
- lease validation latency,
- policy verdict distribution,
- resumption success,
- protocol violations.

AI:

- TTFT,
- input/output token rate,
- active inference,
- model/endpoint errors,
- cancellation,
- queue/admission delay.

Collaboration:

- message delivery latency,
- voice/file transfer success,
- broadcast acknowledgement.

Security:

- authentication failures,
- replay attempts,
- invalid profile messages,
- DLP/PII/secrets findings,
- denied endpoint/model attempts.

## 54.2 Tracing

Internal distributed traces correlate using PAPER connection/session/exchange IDs. Payload data should not automatically be copied into tracing tags.

## 54.3 Diagnostics

Harness can generate a support bundle containing:

- build/version,
- negotiated transport/capabilities,
- safe connection diagnostics,
- recent error codes,
- timing/packet summaries,
- no source code/prompts by default.

---

# 55. Reliability and Availability

## 55.1 SLO targets

Initial enterprise targets:

- PAPER Relay service availability: 99.95% per region/cluster target,
- policy/lease validation availability: 99.99% within deployment cluster,
- message/broadcast durable acceptance: 99.99% where persistence enabled,
- provenance event loss: no acknowledged protected completion without durable/signed buffering,
- connection establishment p95 under normal conditions: product benchmark target established by deployment,
- transparent QUIC→TCP fallback success when TCP path is available.

These are product targets, not protocol guarantees.

## 55.2 Relay HA

- multiple Relay nodes,
- load-balanced connection assignment,
- readiness/health checks,
- graceful drain,
- resumption after node failure,
- replicated/cacheable policy state,
- no single global Relay dependency for private deployments.

## 55.3 CP outage

Relays may continue already-authorized low-risk activity for a bounded cached-policy/lease interval if policy allows. New privileged capabilities, model approvals, or admin changes fail closed.

## 55.4 PIA/model outage

Relay may route to another **approved** endpoint/model according to deterministic fallback policy. It must never downgrade to an unapproved external model merely for availability.

---

# 56. Performance and Scale Requirements

PAPER's additional governance must be measurable and optimized.

## 56.1 Scale targets for design/testing

The reference architecture should be tested at progressively larger profiles:

| Profile | Connected Harnesses | Concurrent AI streams | Purpose |
|---|---:|---:|---|
| Developer | 10 | 5 | local/dev |
| SMB | 500 | 100 | 중소기업 |
| Mid-market | 5,000 | 1,000 | 중견기업 |
| Large enterprise | 50,000 | 10,000+ | 대기업/그룹 |
| Central service | 100,000+ | benchmark-defined | multi-org/shared service |

## 56.2 Latency budgets

Governance checks should run in parallel when safe. Define budgets separately for:

- connection authentication,
- lease validation,
- context scanning,
- pre-inference Relay overhead,
- token forwarding overhead,
- post-response scanning,
- tool authorization.

The system should publish measured overhead rather than claiming “zero latency.”

## 56.3 Token streaming

Relay should forward incremental model output with minimal buffering unless output policy requires semantic/windowed inspection. If a scanner needs chunks, the UI may receive bounded delayed windows and should expose “governed streaming” behavior honestly.

## 56.4 Provenance overhead

Provenance metadata must not duplicate entire payloads. Use digests/references and separate evidence storage. Benchmark storage overhead per:

- inference exchange,
- tool action,
- file change,
- code commit.

---

# 57. Open-Source Governance

PAPER must be credible as an open protocol, not merely “source visible.”

## 57.1 Repository contents

Recommended public structure:

```text
paper/
  spec/
    core/
    transport/
    identity/
    exchange/
    provenance/
    extensions/
    security/
  schema/
  registry/
  reference/
    rust/
    go/
  conformance/
  fuzz/
  test-vectors/
  examples/
  rfcs-or-proposals/
  governance/
```

## 57.2 Licensing

Protocol specification should use a permissive/community-friendly specification license reviewed for standards adoption. Reference implementations should use a permissive OSI-approved license compatible with broad enterprise/government use.

## 57.3 Transparent changes

Protocol changes use public proposals including:

- problem,
- design,
- security considerations,
- compatibility,
- implementation evidence,
- test vectors,
- migration.

## 57.4 Security disclosure

Provide:

- security contact,
- coordinated disclosure policy,
- signed advisories,
- CVE process for implementation defects,
- protocol-level security errata.

## 57.5 Trademark versus implementation freedom

If Patty owns the PAPER trademark, define fair rules allowing conforming implementations to state compatibility without implying Patty endorsement.

---

# 58. Protocol Registry and Extension Governance

PAPER should maintain machine-readable registries for:

- peer profiles,
- capability extensions,
- message types,
- exchange classes,
- error codes,
- verdict reason codes,
- content classes,
- protection modes,
- signature/hash/encryption suites,
- compression codecs,
- voice codecs,
- provenance edge types.

## 58.1 Allocation policy

Reserve ranges for:

- core standard,
- experimental,
- vendor/private,
- future standards.

Experimental/private extensions must never collide with standard identifiers.

## 58.2 Extension requirements

A proposed public extension must document:

- semantics,
- peer profiles,
- security/privacy considerations,
- state machine,
- compatibility,
- message schemas,
- limits,
- test cases.

## 58.3 IANA path

When PAPER stabilizes and adoption justifies it, pursue appropriate public registration such as an ALPN protocol identifier and any other relevant registries rather than assuming an unregistered value forever.

---

# 59. Conformance, Fuzzing, and Interoperability

Open protocols fail when every implementation interprets edge cases differently.

## 59.1 Conformance levels

- Core Transport Conformant,
- Harness Profile Conformant,
- Relay Profile Conformant,
- Inference Profile Conformant,
- extension-specific conformance.

## 59.2 Test categories

- valid message/state cases,
- invalid ordering,
- unknown critical fields,
- duplicate sequence/exchange,
- replay,
- expired lease,
- revoked credential,
- profile misuse,
- malformed lengths/varints,
- truncated streams,
- resumption,
- QUIC/TCP parity,
- policy-epoch mismatch,
- content digest mismatch,
- receipt signature verification.

## 59.3 Fuzzing

Maintain continuous fuzzing targets for:

- frame decoder,
- schema parser,
- state machine,
- stream reassembly,
- canonicalization/hash logic,
- file chunk handling,
- extension negotiation.

Fuzzers must include stateful sequences, not only random isolated frames.

## 59.4 Test vectors

Publish deterministic vectors for:

- canonical exchange object encoding,
- digest computation,
- signed receipt verification,
- capability lease verification,
- channel-binding input,
- version negotiation,
- provenance-spine linking.

## 59.5 Interop events

Before 1.0, run interoperability tests between at least two independently implemented peers for Harness/Relay/Inference roles. Patty's own Rust and Go code alone should not be treated as independent interpretations if they share generated libraries.

---

# 60. Reference Implementations

## 60.1 Rust reference

Recommended for:

- Harness core networking,
- high-performance Relay,
- PIA agent,
- FFI/library use.

Rust provides memory-safety advantages for an Internet-facing binary protocol implementation.

## 60.2 Go reference

Recommended second implementation for:

- enterprise services,
- conformance diversity,
- easy deployment,
- independent interop validation.

## 60.3 Shared schema concern

Generated schemas may be shared, but state machine/security logic should still be independently testable.

## 60.4 Libraries

Potential SDK layers:

- `paper-core`,
- `paper-transport-quic`,
- `paper-transport-tls`,
- `paper-harness`,
- `paper-relay`,
- `paper-inference`,
- `paper-provenance`,
- `paper-conformance`.

## 60.5 Serving-engine adapters

PIA adapters for:

- vLLM,
- SGLang,
- future approved engines.

Adapters remain behind PIA; they are not PAPER protocol endpoints exposed to Harnesses.

---

# 61. Rollout and Implementation Phases

## Phase 0 — Protocol model and threat model

Deliver:

- terminology,
- peer/profile model,
- transport bindings,
- identity/enrollment model,
- Exchange/Lease/Epoch/Receipt schemas,
- threat model,
- protocol registries,
- first conformance vectors.

Gate:

Two test peers can authenticate over QUIC and TCP, negotiate identical PAPER core semantics, and verify a signed Capability Lease/channel binding.

## Phase 1 — Harness ↔ Relay ↔ PIA inference vertical slice

Deliver:

- `HARNESS`, `RELAY`, `INFERENCE` profiles,
- QUIC/TCP transport,
- session lifecycle,
- AI inference streaming,
- PIA vLLM adapter,
- model package registry prototype,
- pre/post Relay Verdicts,
- basic provenance receipts.

Gate:

A registered Harness can perform governed streaming inference only through an approved Relay/PIA endpoint, while a fake/unregistered peer and expired/revoked lease are rejected.

## Phase 2 — Context/tools/provenance

Deliver:

- context manifests,
- tool exchanges,
- runtime profile,
- policy epochs,
- provenance spine,
- Git binding,
- content-addressed evidence,
- resumption/checkpoints.

Gate:

A code change can be traced from user instruction through model/tool activity to file diff and commit candidate.

## Phase 3 — Collaboration

Deliver:

- directory/presence,
- 1:1/group/project chat,
- voice notes,
- managed file transfer,
- broadcasts,
- acknowledgements,
- code/provenance links.

Gate:

Enterprise employees can collaborate entirely inside a private PAPER deployment with policy-controlled attachments and emergency broadcast delivery.

## Phase 4 — Enterprise/government hardening

Deliver:

- air-gap enrollment/update,
- algorithm profiles,
- HA Relay,
- large-scale tests,
- SIEM/evidence export,
- independent security review,
- formal conformance suite.

Gate:

A disconnected deployment can install, operate, update, restore, verify evidence, and run collaboration/inference without public Internet dependency.

## Phase 5 — Open standard maturity

Deliver:

- public 1.0 specification,
- multiple reference implementations,
- independent interoperability report,
- arXiv systems/security paper,
- public benchmarks,
- ALPN registration proposal if appropriate,
- external contributor governance.

---

# 62. Product KPIs

## Security

- unauthorized peer rejection rate,
- credential/replay attack detection,
- percentage protected exchanges with valid lease/policy binding,
- security finding containment rate,
- evidence verification success.

## Provenance

- percentage AI exchanges represented in Provenance Spine,
- percentage exported code changes bound to provenance,
- trace survival after rename/refactor,
- missing/ambiguous attribution rate.

## Reliability

- connection establishment success,
- QUIC success and TCP fallback success,
- resumption success,
- stream error rate,
- message/file/broadcast delivery success.

## Performance

- Relay-added TTFT latency,
- token forwarding overhead,
- policy evaluation p50/p95/p99,
- file throughput,
- message delivery latency,
- CPU/memory per concurrent connection.

## Adoption

- active Harnesses,
- enterprise deployments,
- non-Patty implementations,
- third-party extensions,
- external contributors,
- interoperability participants.

---

# 63. Risks and Mitigations

| Risk | Consequence | Mitigation |
|---|---|---|
| Scope too broad | protocol never stabilizes | layered extensions; freeze Core small |
| Reinvented crypto | catastrophic security risk | standards only; external cryptographic review |
| Relay latency | poor coding UX | parallel checks, streaming, caches, benchmarks |
| Relay compromise | plaintext/evidence risk | service separation, signed receipts, optional sealing |
| Software-only Harness proof overclaimed | false security | explicit limitation; optional hardware profile |
| Protocol becomes Patty-only | no external adoption | generic schemas/profiles; public governance |
| Open-source clone connects | customer fear | security based on enrollment credentials/leases, not secrecy |
| TCP fallback diverges | inconsistent behavior | conformance parity tests |
| Provenance storage explodes | cost/performance | digests/references, checkpoint aggregation, retention |
| Content-addressing leaks info | correlation risk | tenant scoping, salted/opaque references where needed |
| Chat creates surveillance concern | adoption/legal risk | content access controls, retention modes, transparent policy |
| E2EE conflicts with DLP | contradictory claims | explicit payload protection modes/tradeoffs |
| Endpoint/model spoofing | wrong model receives code | PIA identity + signed PMP + endpoint leases |
| OpenAI-compatible bypass by separate client | data leakage | endpoint/network egress controls outside PAPER |
| Government crypto requirements vary | deployment block | algorithm agility + local approved profiles |
| Standardization collision | naming/technical overlap | ongoing related-work review and public proposals |
| Message taxonomy becomes bloated | complex implementations | capability extensions and registries |
| Policy changes break sessions | instability | policy epochs, lease rules, safe tightening semantics |

---

# 64. Cross-Product Acceptance Criteria

The following are required before PAPER 1.0:

1. An enrolled Harness successfully authenticates over QUIC.
2. The same Harness successfully uses native TLS/TCP fallback with identical authorization semantics.
3. No HTTP/OpenAI/Anthropic endpoint is required by the Harness.
4. An unregistered peer implementing the public PAPER spec cannot enter an authenticated enterprise session.
5. A valid user with an unregistered Harness cannot enter an enterprise session.
6. A revoked Harness credential is rejected within the target revocation propagation window.
7. A captured Capability Lease cannot be replayed on an unrelated connection/session.
8. A `HARNESS` peer cannot invoke `INFERENCE`-profile-only protocol operations.
9. An expired lease cannot invoke a model.
10. A session cannot use a model outside the effective policy epoch.
11. An arbitrary vLLM endpoint cannot be registered as an approved inference peer merely by setting a model name.
12. PIA must present registered identity and approved model package/endpoint lease.
13. Raw serving endpoints are unnecessary for Harness operation.
14. Context outside repository/path authorization is denied before model disclosure.
15. Secret/PII transformations are represented in Relay Verdict/evidence.
16. A model-requested tool action receives an independent authorization decision.
17. A prompt injection embedded in repository content cannot expand tool capability.
18. An inference exchange produces verifiable provenance links to user, Harness, policy epoch, Relay, PIA, and model package.
19. Provenance can link generated code to a Git commit candidate.
20. Provenance survives a representative file rename/refactor with documented confidence/ambiguity.
21. Chat messages can link to source/provenance objects without granting unauthorized access.
22. Voice messages can be sent/received, chunk-verified, and optionally transcribed under policy.
23. File transfer is resumable and final digest mismatch is detected.
24. A file denied by DLP/malware policy cannot be delivered.
25. A critical broadcast reaches targeted online Harnesses with acknowledgement state.
26. A broadcast cannot itself modify policy unless linked to a separately authorized control action.
27. Emergency admin control can revoke a session/lease and produce audit evidence.
28. Presence/telemetry cannot starve interactive AI/control streams.
29. QUIC 0-RTT cannot cause side-effecting PAPER actions in v1.
30. Connection loss and resume do not duplicate a tool/file/admin side effect.
31. Unknown critical extensions fail safely.
32. Public conformance tests run against at least two independent implementations.
33. Stateful fuzzing finds no unresolved critical memory-safety/parser issue in release candidate.
34. Evidence receipts verify offline against historical trust metadata.
35. Air-gapped deployment performs enrollment, inference, collaboration, provenance, and audit without Patty cloud.
36. Security documentation clearly states the non-hardware modified-client limitation.
37. Performance benchmarks publish Relay overhead under representative concurrency.
38. The protocol source/spec permits an independent engineer to implement a conforming peer without private Patty information.

---

# 65. Definition of Done

PAPER reaches v1.0 product readiness when:

1. **Protocol completeness:** Core, transport bindings, Harness/Relay/Inference profiles, governance, provenance, AI, chat, voice, file, broadcast, and required security semantics have stable specifications.
2. **Secure participation:** Enterprise CP only accepts authenticated enrolled peers with appropriate profiles, leases, and policy bindings.
3. **Governed data path:** Protected AI traffic passes through Relay enforcement before reaching approved inference infrastructure.
4. **Trusted endpoint workflow:** Relay routes only to approved PIA/model package/endpoint combinations according to CP state.
5. **No generic model API dependency:** Harness operation does not require REST/OpenAI/Anthropic compatibility.
6. **Provenance:** A reviewer can trace an AI-assisted code change from human intent to model/tool/artifact/commit with verifiable receipts.
7. **Collaboration:** Enterprise users can chat, send voice messages/files, see presence, and receive broadcasts inside PAPER.
8. **Resilience:** QUIC/TCP fallback and session resumption work without duplicate side effects.
9. **Government operation:** a local disconnected deployment works without public Internet or Patty cloud dependencies.
10. **Open implementation:** protocol, schemas, reference code, fuzzers, test vectors, security model, and conformance suite are public.
11. **Independent interoperability:** at least one implementation path not sharing Patty's complete protocol stack interoperates successfully.
12. **Independent security review:** high/critical issues from a release-target security assessment are remediated or explicitly blocked from release.
13. **Measured claims:** published security/performance claims map to tests and benchmarks.
14. **Novelty clarity:** documentation distinguishes PAPER's novel governance/provenance semantics from reused Internet/cryptographic standards.

---

# 66. Research and Publication Requirements

PAPER is intended for open-source publication and an arXiv systems/security paper. Product development should capture experimental evidence from the start.

## 66.1 Core research questions

1. Can governance be made a first-class property of AI communication rather than middleware metadata?
2. Can a capability-lease model constrain long-running agentic sessions without unacceptable latency?
3. Can distributed Relay/PIA/Harness receipts create useful, independently verifiable AI provenance?
4. What is the overhead of proof-carrying governed streams relative to a conventional HTTPS model API?
5. How well does the Provenance Spine survive real software evolution such as refactoring, rebasing, and human/AI co-editing?
6. Can one protocol cleanly support AI inference and enterprise collaboration without weakening security-domain separation?
7. What security benefits arise from eliminating generic model API compatibility at the Harness boundary?

## 66.2 Required benchmark comparisons

Compare PAPER to representative conventional patterns, not to strawmen:

- HTTPS/JSON model API with bearer authentication,
- OpenAI-style streaming API pattern,
- MCP transport model for tool/context interoperability,
- A2A transport/task model for agent interoperability,
- conventional enterprise gateway + external logging/provenance,
- software-supply-chain provenance systems such as in-toto/SLSA for post-build artifact provenance.

The paper must not claim MCP/A2A/in-toto/SLSA are inferior at goals they do not attempt. The contribution is that PAPER targets a different boundary: **governed Harness-to-AI infrastructure communication with live policy and causal provenance**.

## 66.3 Experiments

Performance:

- connection setup QUIC/TCP,
- TTFT overhead,
- sustained token throughput,
- concurrent streams,
- Relay CPU/memory,
- file/voice throughput,
- provenance storage overhead.

Security:

- fake peer,
- stolen/replayed lease,
- protocol downgrade,
- invalid profile,
- malformed state sequences,
- fake inference endpoint,
- model-name spoof,
- prompt injection requesting capability escalation,
- evidence tampering,
- connection-resume side-effect duplication.

Provenance:

- generated function then human-edited,
- file rename,
- function move,
- rebase/merge,
- multi-agent modification,
- partial AI patch survival.

## 66.4 Publication discipline

The arXiv paper must:

- avoid claiming formal security proofs unless actually performed,
- report limitations,
- publish benchmark methodology and hardware/software versions,
- cite protocols from their official specifications,
- publish source/test vectors needed to reproduce results where possible,
- distinguish product requirements from implemented/evaluated capabilities.

---
# 67. Appendix A — Initial Message Registry

The names below are **PRD-level semantic names**, not yet final wire opcode names. The formal protocol specification may compact/rename them while preserving behavior.

## A.1 Core

| Semantic message | Direction | Purpose |
|---|---|---|
| `CORE_HELLO` | any → peer | declare core versions/profile intent |
| `CORE_CHALLENGE` | verifier → peer | fresh authentication challenge |
| `CORE_PEER_PROOF` | peer → verifier | credential/channel-bound proof |
| `CORE_PROFILE_ACCEPT` | verifier → peer | accept authenticated profile |
| `CORE_CAPABILITIES_OFFER` | either | extensions/features supported |
| `CORE_CAPABILITIES_ACCEPT` | either | selected extensions/limits |
| `CORE_CONNECTION_BIND` | both | bind PAPER identity state to secure channel |
| `CORE_PING/PONG` | both | liveness |
| `CORE_DRAIN` | either | graceful shutdown/no new streams |
| `CORE_ERROR` | either | protocol/application error |

## A.2 Sessions / Leases

| Message | Purpose |
|---|---|
| `SESSION_REQUEST` | request developer/work session |
| `SESSION_ACCEPT` | return session identity/effective state |
| `SESSION_RESUME` | reconnect to prior session |
| `SESSION_STATE` | update pause/degraded/approval state |
| `SESSION_CLOSE` | request closure |
| `SESSION_RECEIPT` | final closure/evidence result |
| `LEASE_PRESENT` | provide capability lease |
| `LEASE_RENEW` | request/return renewed authority |
| `LEASE_REVOKE` | immediate revocation notice |

## A.3 Governance

| Message | Purpose |
|---|---|
| `POLICY_EPOCH_BIND` | bind stream/session to policy epoch |
| `POLICY_REFRESH` | notify newer effective policy |
| `GOVERNANCE_MANIFEST` | exchange governance context |
| `RELAY_VERDICT` | enforcement result |
| `APPROVAL_REQUIRED` | suspend action pending approval |
| `APPROVAL_DECISION` | authorized approval/rejection |
| `TRANSFORMATION_NOTICE` | disclose redaction/tokenization/etc. |

## A.4 AI

| Message | Purpose |
|---|---|
| `AI_OPEN` | begin inference exchange |
| `AI_MODEL_REQUIREMENT` | task/model requirements |
| `AI_INPUT` | prompt/instruction content |
| `AI_ACCEPT` | downstream accepted inference |
| `AI_OUTPUT_DELTA` | streamed text/structured output |
| `AI_TOOL_PROPOSAL` | model proposes tool action |
| `AI_USAGE_CHECKPOINT` | token/resource usage |
| `AI_CANCEL` | cancel inference |
| `AI_COMPLETE` | completion reason/metadata |

## A.5 Context

| Message | Purpose |
|---|---|
| `CONTEXT_MANIFEST` | proposed context list/digests |
| `CONTEXT_DECISION` | allow/deny/transform per item |
| `CONTEXT_OPEN` | begin item transfer |
| `CONTEXT_CHUNK` | content chunk |
| `CONTEXT_REFERENCE` | content-addressed/existing object reference |
| `CONTEXT_COMPLETE` | verify completed item |

## A.6 Tools / Runtime

| Message | Purpose |
|---|---|
| `TOOL_INTENT` | proposed tool operation |
| `TOOL_DECISION` | policy result |
| `TOOL_EXECUTE` | authorized execution request |
| `TOOL_PROGRESS` | progress/events |
| `TOOL_RESULT` | structured result |
| `TOOL_CANCEL` | cancel where supported |
| `RUNTIME_STATE` | sandbox/runtime status |
| `RUNTIME_ARTIFACT` | produced artifact reference |

## A.7 Provenance

| Message | Purpose |
|---|---|
| `PROV_NODE` | add/announce provenance node |
| `PROV_EDGE` | establish causal relation |
| `PROV_CHECKPOINT` | aggregate stream/event digest |
| `PROV_ARTIFACT_BIND` | bind result artifact |
| `PROV_CODE_SPAN_BIND` | bind code span/symbol/diff |
| `PROV_COMMIT_BIND` | connect to SCM commit/PR |
| `EVIDENCE_RECEIPT` | signed checkpoint/completion receipt |

## A.8 Chat / Presence

| Message | Purpose |
|---|---|
| `CHAT_SEND` | send message |
| `CHAT_DELIVER` | server/Relay delivery |
| `CHAT_ACK` | delivery/read acknowledgement as policy allows |
| `CHAT_EDIT` | edit existing message |
| `CHAT_DELETE` | deletion/tombstone request |
| `PRESENCE_SET` | user presence update |
| `PRESENCE_EVENT` | presence change delivery |
| `DIRECTORY_DELTA` | authorized directory/member updates |

## A.9 Voice

| Message | Purpose |
|---|---|
| `VOICE_OFFER` | voice object metadata |
| `VOICE_ACCEPT` | recipient/service accepts upload |
| `VOICE_CHUNK` | encoded audio chunk |
| `VOICE_COMPLETE` | final digest/duration |
| `VOICE_TRANSCRIPT` | authorized transcript object/reference |

## A.10 Files

| Message | Purpose |
|---|---|
| `FILE_OFFER` | metadata/digest/recipient/purpose |
| `FILE_POLICY` | allow/deny/scan requirements |
| `FILE_ACCEPT` | initiate transfer |
| `FILE_CHUNK` | content-addressed chunk |
| `FILE_RESUME_MAP` | identify missing chunks |
| `FILE_COMPLETE` | final object verification |
| `FILE_DELIVERY_RECEIPT` | recipient/download evidence |

## A.11 Broadcast / Admin

| Message | Purpose |
|---|---|
| `BROADCAST_PUSH` | targeted announcement |
| `BROADCAST_DELIVERED` | delivery status |
| `BROADCAST_ACK` | required acknowledgement |
| `ADMIN_ACTION` | privileged control request |
| `ADMIN_ACTION_RESULT` | action status/evidence |
| `SECURITY_LOCKDOWN` | high-priority emergency action profile |

## A.12 Telemetry

| Message | Purpose |
|---|---|
| `TELEMETRY_BATCH` | bounded operational metrics/events |
| `USAGE_CHECKPOINT` | metering data |
| `HEALTH_STATE` | peer health |
| `SECURITY_EVENT` | security signal/reference |

---

# 68. Appendix B — State Machines

## B.1 Harness peer

```text
UNENROLLED
   |
   | enrollment
   v
ENROLLED
   |
   | transport + peer proof
   v
CONNECTED
   |
   | user auth/session request
   v
SESSION_ACTIVE
   |       |         |
   |       |         +--> APPROVAL_WAIT
   |       +------------> PAUSED
   +--------------------> DEGRADED
   |
   v
SESSION_CLOSING
   |
   v
CONNECTED
   |
   v
DISCONNECTED

Any state -> REVOKED (administrative/security event)
```

## B.2 AI Exchange

```text
CREATED
  -> GOVERNANCE_PENDING
  -> CONTEXT_PENDING
  -> PRE_VERDICT_ALLOWED
  -> ROUTING
  -> INFERENCE_ACTIVE
  -> RESPONSE_GOVERNANCE
  -> COMPLETED
  -> EVIDENCE_FINALIZED
```

Alternative terminal states:

```text
DENIED
CANCELLED
FAILED
QUARANTINED
EXPIRED
```

## B.3 File transfer

```text
OFFERED
 -> POLICY_PENDING
 -> ACCEPTED
 -> TRANSFERRING
 -> VERIFYING
 -> STORED/DELIVERED
 -> RECEIPTED
```

Terminal exceptions:

```text
DENIED
QUARANTINED
EXPIRED
CANCELLED
DIGEST_MISMATCH
```

## B.4 PIA endpoint

```text
UNREGISTERED
 -> ENROLLED
 -> CONNECTED
 -> INVENTORY_VERIFIED
 -> AVAILABLE
 -> DRAINING
 -> OFFLINE
```

Administrative states:

```text
SUSPENDED
REVOKED
MODEL_MISMATCH
```

---

# 69. Appendix C — Representative Exchange Flows

## C.1 Normal coding inference

```text
Harness                    Relay                      PIA
   |                         |                         |
   |-- SESSION/LEASE ------->|                         |
   |<-- ACCEPT --------------|                         |
   |                         |                         |
   |-- AI_OPEN ------------->|                         |
   |-- CONTEXT_MANIFEST ---->|                         |
   |                         |-- policy/DLP ----------> internal
   |<-- CONTEXT_DECISION ----|                         |
   |-- CONTEXT_CHUNKS ------>|                         |
   |                         |                         |
   |<-- PRE VERDICT ---------|                         |
   |                         |-- AI_OPEN ------------->|
   |                         |-- INPUT/CONTEXT ------->|
   |                         |<-- OUTPUT_DELTA --------|
   |<-- OUTPUT_DELTA --------|                         |
   |                         |<-- COMPLETE ------------|
   |<-- COMPLETE ------------|                         |
   |<-- EVIDENCE RECEIPT ----|                         |
```

## C.2 Model proposes shell command

```text
Model -> PIA -> Relay -> Harness: AI_TOOL_PROPOSAL
Harness/Orchestrator -> Relay: TOOL_INTENT
Relay:
  verify lease
  parse command
  evaluate file/network/package impact
  require approval if policy says
Relay -> Runtime: TOOL_EXECUTE
Runtime -> Relay: TOOL_RESULT + artifact digests
Relay -> Harness/Model: governed result
Provenance Spine links tool action to parent inference.
```

## C.3 Chat file used as AI context

```text
User A -- FILE_TRANSFER --> User B
                         (not AI context)

User B selects file for task
   |
   +--> CONTEXT_DISCLOSURE exchange
        -> recipient access re-check
        -> repository/project/policy classification
        -> scanning/transformation
        -> approved AI context
```

## C.4 Emergency model recall

```text
Security Admin -> CP: suspend PMP model-v4
CP:
  revoke endpoint/model authorization
  publish new policy epoch/revocation
  create emergency broadcast
Relay:
  stop new streams to model-v4
  optionally terminate active streams by policy
Harness:
  receives CRITICAL/EMERGENCY notice
PIA:
  receives drain/suspend control
Evidence:
  records admin, approval, affected sessions, acknowledgements
```

## C.5 TCP fallback

```text
Harness attempts QUIC UDP/443
  -> network blocks UDP
  -> bounded failure
Harness opens TCP/443 + TLS 1.3
  -> ALPN PAPER
  -> same peer proof
  -> same extensions
  -> same lease/policy semantics
  -> PAPER multiplexing maps logical streams
```

---

# 70. Appendix D — Conceptual Schemas

These schemas are illustrative PRD objects, not the final canonical wire definition.

## D.1 Exchange

```yaml
exchange:
  id: ex_01...
  version: 1
  class: AI_INFERENCE
  session_id: ses_...
  parent_ids: [ex_parent]
  actor:
    organization_id: org_...
    user_id: usr_...
    harness_id: hrn_...
  peer:
    source_profile: HARNESS
    destination_profile: RELAY
  governance:
    policy_epoch: pe_...
    capability_lease: lease_...
    classification: confidential
    purpose: implement-retry
  timestamps:
    created: ...
    completed: ...
  status: completed
  provenance_root: sha256:...
```

## D.2 Capability Lease

```yaml
capability_lease:
  id: lease_...
  issuer: cp://org/...
  subject:
    user_id: usr_...
    harness_id: hrn_...
    session_id: ses_...
  scope:
    project: prj_...
    repository: repo_...
    branch: feature/retry
    file_read: ["src/**", "tests/**"]
    file_write: ["src/payments/**"]
    models: ["pmp:patty-coder-v4"]
    tools: ["file", "git-read", "test"]
    network: []
  budgets:
    input_tokens: 100000
    output_tokens: 20000
  policy_epoch: pe_...
  not_before: ...
  expires: ...
  channel_binding_required: true
  signature: ...
```

## D.3 Relay Verdict

```yaml
relay_verdict:
  id: verd_...
  exchange_id: ex_...
  checkpoint: PRE_INFERENCE
  result: ALLOW_WITH_TRANSFORM
  policy_epoch: pe_...
  transformations:
    - type: SECRET_TOKENIZE
      object: ctx_7
  reasons:
    - PATH_ALLOWED
    - SECRET_DETECTED_AND_TOKENIZED
  obligations:
    - RECORD_PROVENANCE
    - REQUIRE_TESTS_BEFORE_EXPORT
  evaluator_versions:
    pii: ko-pii-3.1
    secrets: secret-scan-5.0
  input_manifest_digest: sha256:...
  output_manifest_digest: sha256:...
  signer: relay_...
  signature: ...
```

## D.4 Provenance node

```yaml
provenance_node:
  digest: sha256:...
  type: MODEL_INVOCATION
  exchange_id: ex_...
  actor_or_peer: pia_...
  properties:
    model_package: pmp:patty-coder-v4
    endpoint: inf_...
  parents:
    - sha256:prompt-node
    - sha256:context-manifest
    - sha256:pre-verdict
```

---

# 71. Appendix E — Security Profiles

## E.1 Public Cloud Profile

- QUIC preferred/TCP fallback,
- Patty-managed trust and Relay/PIA,
- service-defined model registry,
- user/Harness auth,
- standard modern global crypto suite,
- service provenance and abuse controls.

## E.2 Enterprise Managed Profile

- organization Harness enrollment,
- SSO user binding,
- full leases/policy epochs,
- customer-visible provenance/audit,
- collaboration/broadcast,
- Patty or customer Relay/PIA,
- customer key options,
- admin content-access policy.

## E.3 Enterprise Restricted Profile

Adds:

- customer-hosted Relay,
- customer-hosted PIA/model,
- no public inference route,
- strict DLP/PII,
- no endpoint-sealed AI traffic if inline inspection mandatory,
- restricted extensions/tools,
- local evidence storage.

## E.4 Government Sovereign Profile

Adds:

- no required public Internet,
- offline enrollment/update/revocation,
- customer/local trust anchors,
- local Relay/PIA/GPU,
- government-approved cryptographic module/profile where applicable,
- signed evidence export,
- strict role separation,
- optional hardware attestation as deployment-specific hardening.

---

# 72. Appendix F — Novelty and Adjacent Protocol Boundary

PAPER must be designed with awareness of adjacent open protocols while avoiding false claims of uniqueness.

## F.1 Conventional model APIs

OpenAI/Anthropic-style APIs are optimized around remote model interaction and streaming. PAPER intentionally differs by making organization identity, policy epochs, capability leases, Relay enforcement, and causal provenance intrinsic protocol objects and by refusing generic model API compatibility at the Harness boundary.

## F.2 Model Context Protocol (MCP)

MCP is designed to connect AI applications to tools/context/resources. Its standardized remote transport uses JSON-RPC and Streamable HTTP (with stdio also defined). PAPER is not intended to replace MCP tool semantics in the ecosystem. Patty Code may still use MCP **inside a governed tool model**, while PAPER governs the Harness/Relay/inference and enterprise communication boundary.

PAPER novelty claim must therefore not be “first protocol to let AI use tools.”

## F.3 Agent2Agent (A2A)

A2A targets interoperability between independent agent systems, including capability discovery, tasks, messages, and artifacts, with HTTP(S)/JSON-RPC-based bindings in its specification. PAPER does not primarily target open agent discovery/inter-vendor task delegation. PAPER targets authenticated enterprise Harness participation, inline governance, inference infrastructure, and provenance.

PAPER novelty claim must therefore not be “first agent communication protocol.”

## F.4 in-toto / SLSA

in-toto and SLSA provide important software-supply-chain provenance/attestation concepts. PAPER should interoperate/export where useful rather than pretending provenance is new.

The distinction is timing and boundary:

- in-toto/SLSA focus on verifiable software supply-chain steps/build artifacts,
- PAPER generates causal provenance during live human-AI engineering exchanges, including prompts, context, policy verdicts, model identity, tools, human edits, and collaboration references before final build provenance exists.

PAPER novelty claim must therefore not be “first signed software provenance.”

## F.5 TLS/QUIC/HPKE/MLS

These are security/transport building blocks. PAPER's novelty is not their cryptographic properties.

## F.6 Candidate research contribution wording

A defensible formulation is:

> **PAPER proposes a governance-native communication model for human-AI software engineering in which purpose-bound authority, inline enforcement decisions, and causal provenance are intrinsic to the lifecycle of multiplexed AI and collaboration exchanges.**

This should be tested against ongoing literature before publication; the arXiv paper must narrow claims if overlapping systems appear.

---

# 73. Appendix G — Test Matrix

## G.1 Authentication

- valid enrolled Harness,
- invalid signature,
- unknown Harness,
- wrong organization,
- revoked credential,
- expired credential,
- copied credential concurrent use,
- wrong peer profile,
- stale channel-binding proof.

## G.2 Negotiation

- QUIC success,
- QUIC blocked → TCP success,
- unsupported core major,
- optional extension unknown,
- critical extension unknown,
- crypto suite no overlap,
- protection profile downgrade attempt.

## G.3 Sessions/leases

- valid lease,
- expired lease,
- wrong repository,
- wrong model,
- exceeded token budget,
- lease renewal,
- policy epoch tightened mid-session,
- emergency lease revoke.

## G.4 Inference

- normal stream,
- cancel,
- Relay restart/resume,
- PIA disconnect,
- model endpoint suspended,
- fake model name,
- wrong PMP digest,
- fallback only to approved endpoint.

## G.5 Context

- allowed file,
- denied path,
- metadata-only protected file,
- secret tokenization,
- Korean PII masking,
- injection marker,
- digest mismatch,
- changed repository baseline.

## G.6 Tools

- read file,
- write file,
- test command,
- unauthorized shell,
- unauthorized network,
- model attempts capability escalation,
- duplicate retry does not rerun side effect.

## G.7 Collaboration

- DM,
- group message,
- edit/delete,
- unauthorized repository link,
- voice upload resume,
- transcript policy,
- file quarantine,
- recipient revoked mid-transfer,
- E2EE group mode compatibility behavior.

## G.8 Broadcast/admin

- info delivery,
- mandatory ack,
- expired broadcast,
- unauthorized sender,
- two-person critical approval,
- emergency session revoke,
- control action vs message distinction.

## G.9 Provenance

- receipt verification,
- node/edge digest mismatch,
- omitted checkpoint,
- commit binding,
- file rename,
- human edit after AI,
- multi-agent edit,
- offline evidence verification.

## G.10 Abuse/fuzz

- oversized lengths,
- recursive/hostile metadata,
- invalid UTF-8 where text required,
- compression bomb where compression enabled,
- billions of tiny streams,
- sequence wrap/edge cases,
- invalid resumption map,
- deliberate CPU-expensive authentication flood.

---

# 74. Appendix H — Standards Baseline

PAPER v1 design should reference the current official specification for each reused standard at implementation/release time. As of this PRD baseline, important standards/specifications include:

- **RFC 9000** — QUIC: A UDP-Based Multiplexed and Secure Transport.
- **RFC 9001** — Using TLS to Secure QUIC.
- **RFC 9846** — The Transport Layer Security (TLS) Protocol Version 1.3; published in 2026 and obsoleting RFC 8446.
- **RFC 7301** — Application-Layer Protocol Negotiation (ALPN).
- **RFC 9266** — TLS 1.3 channel binding using `tls-exporter`.
- **RFC 9180** — Hybrid Public Key Encryption (HPKE), if PAPER payload sealing uses it.
- **RFC 9420** — Messaging Layer Security (MLS), if an endpoint-only encrypted group-chat profile is implemented.
- **Model Context Protocol specification** — adjacent tool/context protocol; remote transport currently based on JSON-RPC/Streamable HTTP.
- **Agent2Agent (A2A) Protocol specification** — adjacent agent interoperability protocol using HTTP/JSON-RPC bindings.
- **in-toto specification** — software supply-chain integrity/provenance framework.
- **SLSA specification** — software supply-chain security and provenance/attestation ecosystem.

The formal PAPER specification must use normative references only where PAPER actually depends on a standard. Related-work references belong in informative sections and the research paper.

---

# Final Product Statement

PAPER is successful if an enterprise or government organization can make the following statement and prove it with the protocol's own evidence:

> **Every Patty Code Harness that participates in our AI engineering environment is explicitly enrolled; every protected AI action carries bounded authority; every model request passes through our governed Relay to an approved inference identity; every material policy decision is attributable; every resulting tool action and code change can be traced through a cryptographically linked provenance spine; and the same secure communication fabric provides the collaboration and operational messaging developers need without creating an ungoverned side channel.**

That—not custom encryption or different endpoint names—is the product PAPER is intended to deliver.
