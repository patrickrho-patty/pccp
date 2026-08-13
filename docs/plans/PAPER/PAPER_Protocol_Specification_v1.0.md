# PAPER Protocol Specification v1.0

## Patty AI Provenance & Enforcement Relay

**Status:** Open protocol specification — implementation baseline  
**Date:** 2026-08-11  
**Protocol family:** PAPER  
**Primary transports:** QUIC; TLS 1.3/TCP fallback  
**Canonical structured encoding:** Deterministic CBOR  
**Protocol receipts and credentials:** COSE  
**Primary deployment profiles:** Public Cloud, Enterprise Managed, Enterprise Restricted, Government Sovereign  
**Reference architecture:** Harness ↔ PAPER Relay ↔ Patty Inference Agent (PIA) ↔ inference engine/GPU  
**License intent:** Open specification suitable for an open-source reference implementation and independent conforming implementations

---

## Abstract

PAPER — **Patty AI Provenance & Enforcement Relay** — is an open, stateful application protocol for communication between AI coding harnesses, governance relays, inference infrastructure, controlled runtimes, and human collaboration services.

PAPER is designed for a boundary that conventional model APIs, generic RPC frameworks, agent-to-agent protocols, and post-build provenance systems do not directly address: **live communication between an AI engineering client and AI infrastructure where authority, policy enforcement, causal provenance, and evidence are part of the exchange itself**.

A PAPER deployment separates the administrative **Control Plane** from a horizontally scalable **PAPER Relay data plane**. Harnesses do not call raw model endpoints. A Harness establishes PAPER with an enrolled Relay. The Relay authenticates the Harness and user, validates a time-bounded **Capability Lease**, binds the exchange to a **Policy Epoch**, performs required security and governance controls, and forwards authorized AI traffic to an enrolled **Patty Inference Agent (PIA)**. PIA is the only component permitted to translate PAPER inference messages into a local serving-engine interface such as vLLM or SGLang.

PAPER also serves as the complete communication substrate of an enterprise Harness. The same protocol carries coding sessions, context manifests, tool proposals and results, provenance events, employee chat, voice messages, governed file transfers, presence, administrative notices, broadcasts, telemetry, and metering. Capabilities are separated by peer profile, extension, Stream Contract, and lease rather than by assuming all authenticated peers may use all features.

PAPER deliberately does **not** define new cryptographic primitives. It composes standardized secure transport and cryptographic building blocks, including TLS 1.3, QUIC, ALPN, TLS channel binding/exporters, CBOR, COSE, and, where enabled, HPKE or MLS. PAPER's protocol-specific contribution is the semantics and lifecycle of governed exchanges, capability-bound streams, inline Relay verdicts, and causal provenance.

---

# 1. Conformance and Normative Language

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and **OPTIONAL** in this document are to be interpreted as described by BCP 14 when, and only when, they appear in all capitals.

A conforming PAPER implementation implements one or more peer profiles defined by this specification and satisfies the mandatory behavior for every enabled extension.

Conformance is divided into four levels:

| Level | Meaning |
|---|---|
| **Core** | Framing, transport binding, identity proof, version negotiation, capabilities, error model |
| **Governed** | Core + Capability Leases + Policy Epochs + Relay Verdicts + evidence receipts |
| **AI** | Governed + AI, Context, Tool/Runtime, PIA/model identity, provenance |
| **Enterprise** | AI + collaboration, files, voice, presence, broadcast/admin, enterprise policy requirements |

A Government Sovereign implementation is an Enterprise implementation with additional deployment and cryptographic-profile requirements; it is not a separate protocol.

---

# 2. Scope

PAPER specifies communication for:

1. an official or conforming AI Harness communicating with an authorized PAPER Relay;
2. a PAPER Relay communicating with a registered PIA;
3. a Relay communicating with controlled runtime agents;
4. a Harness exchanging enterprise collaboration messages through PAPER infrastructure;
5. CP services distributing signed policy, lease, endpoint, revocation, and administration state to Relays;
6. participants generating and verifying causal provenance and protocol evidence.

PAPER does not standardize:

- an LLM architecture;
- model weights;
- a specific serving engine;
- a sandbox implementation;
- an enterprise identity provider;
- an HR system;
- a SIEM;
- Git itself;
- a new cryptographic algorithm;
- a generic Internet browser API.

PAPER is not OpenAI-compatible, Anthropic-compatible, or an HTTP REST API.

---

# 3. Design Principles

## 3.1 Governance is not logging

An authorized AI action is represented by a **Governed Exchange**. A Relay MUST evaluate authority before protected content or side effects are forwarded. Recording a denial after execution is not equivalent to governance.

## 3.2 Provenance is causal, not merely chronological

PAPER records explicit parent relationships between user intent, context, policy decisions, model actions, tool calls, file changes, reviews, and artifacts. Wall-clock ordering is supplemental evidence; it is not the sole provenance model.

## 3.3 Identity is multi-actor

The following identities are distinct:

- Organization
- User
- Harness
- Relay
- Session
- Runtime
- PIA
- Inference Endpoint
- Model Package
- Administrator

Authenticating one MUST NOT silently authenticate the others.

## 3.4 Authority is bounded and expiring

A long-running coding session MUST NOT receive a timeless bearer capability. Protected actions are constrained by signed **Capability Leases** with scope, policy epoch, and expiry.

## 3.5 A model has no independent authority

Model text and tool proposals are untrusted proposals. A model cannot grant itself file access, network access, model access, runtime privilege, or administrative capability.

## 3.6 Open specification, closed participation

The wire format and reference implementation may be fully public. Security MUST remain valid if an attacker knows every protocol detail. Participation in a controlled PAPER deployment depends on enrolled identity and authorization.

## 3.7 Transport parity

The QUIC and TLS/TCP bindings MUST preserve the same PAPER semantics. A TCP fallback MUST NOT silently weaken authentication, policy, provenance, or payload protection.

---

# 4. Reference Architecture

```text
                    ┌────────────────────────────────────┐
                    │       Patty Control Plane          │
                    │                                    │
                    │ users / harness registry           │
                    │ policy / approvals / model registry│
                    │ endpoint leases / provenance store │
                    │ revocation / billing / admin       │
                    └──────────────┬─────────────────────┘
                                   │ signed state
                                   │
                    ┌──────────────▼─────────────────────┐
                    │          PAPER Relay               │
                    │       governed data plane          │
                    │                                    │
Harness ══ PAPER ══►│ auth / lease / policy / DLP        │══ PAPER ══► PIA
                    │ routing / verdict / evidence       │             │
                    └────────────────────────────────────┘             ▼
                                                               vLLM/SGLang/
                                                               local engine
                                                                    │
                                                                    ▼
                                                                   GPU
```

The Control Plane is authoritative for identities, policy state, approvals, model inventory, endpoint authorization, and durable provenance. It is not required to proxy every token.

PAPER Relay is the inline data-plane enforcement point. It MAY be horizontally replicated. It MAY be decomposed into connection, inspection, routing, and evidence services as long as externally observable PAPER behavior remains conformant.

PIA is the protocol termination point immediately in front of inference serving. Raw serving-engine ports SHOULD be unreachable from Harness networks.

---

# 5. Terminology

**Peer** — an authenticated PAPER participant.

**Peer Profile** — a constrained role defining which message families and state transitions a peer may use.

**Connection** — one QUIC or TLS/TCP transport instance carrying PAPER.

**Working Session** — a longer-lived user/Harness/project interaction that may survive transport reconnects.

**Lane** — a logical ordered PAPER stream. A lane is transport-independent and may map to a QUIC stream or to a multiplexed TCP sequence.

**Stream Contract** — the declaration and authorization context governing a lane.

**Governed Exchange** — a causally bounded operation or interaction under an explicit lease and policy epoch.

**Capability Lease** — a signed, expiring authorization object constraining what a peer/session may do.

**Policy Epoch** — an immutable identifier and digest for the effective policy state used to evaluate an exchange.

**Relay Verdict** — the structured decision produced by a Relay enforcement step.

**Evidence Receipt** — a signed statement summarizing an exchange's authenticated evidence chain.

**Provenance Spine** — the content-addressed DAG linking intent, context, decisions, execution, human actions, and artifacts.

**PMP** — Patty Model Package, a signed manifest representing an approved model artifact and serving configuration.

**PIA** — Patty Inference Agent, an enrolled PAPER INFERENCE peer that binds a serving endpoint to a PMP and executes Relay-authorized inference.

---

# 6. Peer Profiles

The initial registry defines:

| Profile ID | Name | Primary function |
|---:|---|---|
| 1 | `HARNESS` | Developer-facing terminal/IDE Harness |
| 2 | `RELAY` | Governance and routing data plane |
| 3 | `INFERENCE` | PIA / approved model endpoint |
| 4 | `RUNTIME` | Controlled execution/sandbox agent |
| 5 | `ADMIN_AGENT` | Operational or administrative automation |
| 6 | `CI_AGENT` | Future controlled CI/CD participant |

A credential declares exactly one primary profile. Multi-profile services MUST possess separately identifiable credentials or a credential explicitly authorizing the combined profile set.

A peer MUST reject a message whose message-family/profile matrix does not permit the sender profile.

Example:

- `HARNESS` MAY send `AI_OPEN`;
- `HARNESS` MUST NOT send `ENDPOINT_ATTEST`;
- `INFERENCE` MAY send `MODEL_READY`;
- `INFERENCE` MUST NOT send `USER_CHAT_MESSAGE`;
- `RELAY` MAY send `RELAY_VERDICT`;
- an ordinary `HARNESS` MUST NOT send `ADMIN_DIRECTIVE`.

Profile violation is a protocol-security event.

---

# 7. Transport Bindings

## 7.1 QUIC binding

QUIC is the preferred transport.

A conforming public or enterprise Relay SHOULD accept QUIC on UDP/443 unless deployment policy selects another port.

The implementation:

- MUST use a QUIC version whose security properties meet deployment policy;
- MUST negotiate PAPER with ALPN;
- MUST authenticate the Relay using the configured TLS trust model;
- MUST NOT send protected PAPER application actions as 0-RTT early data in version 1;
- MUST apply QUIC transport flow control in addition to PAPER lane limits.

The provisional ALPN identifier is:

```text
paper/1
```

Before a final standards-track registration, public implementations SHOULD provide a configuration mechanism in case the identifier changes during registration.

## 7.2 TLS/TCP binding

TLS 1.3 over TCP is the mandatory fallback for public and Enterprise Managed clients unless a deployment explicitly forbids fallback.

A TCP PAPER server SHOULD accept TCP/443 where practical.

The TLS/TCP binding:

- MUST negotiate `paper/1` with ALPN;
- MUST use TLS 1.3;
- MUST reject a connection that negotiates another application protocol;
- MUST NOT encapsulate PAPER inside HTTP, WebSocket, SSE, gRPC, or CONNECT as the native fallback;
- MUST implement PAPER logical-lane multiplexing.

## 7.3 Fallback behavior

A Harness may attempt QUIC first and TCP second.

Fallback is not an authorization downgrade. The Harness sends the same identity proof, capability negotiation, and user binding over either transport. The Relay records the selected binding in the Working Session.

Enterprise Restricted or Government Sovereign policy MAY require QUIC only, TCP only, or specific network zones.

A peer MUST NOT fall back from PAPER to an OpenAI-compatible, Anthropic-compatible, or other generic model protocol.

## 7.4 Channel binding

PAPER authentication proofs are bound to the secure transport with exported keying material compatible with TLS 1.3 channel binding.

PAPER uses the TLS 1.3 `tls-exporter` channel-binding value defined by RFC 9266. The value is 32 bytes of Exported Keying Material obtained with the exact RFC 9266 inputs:

```text
label   = "EXPORTER-Channel-Binding"
context = zero-length string
length  = 32 bytes
```

PAPER includes this `tls-exporter` value in the higher-level peer-authentication context together with the PAPER HELLO/HELLO_ACK transcript and nonces. PAPER does not redefine the channel-binding derivation or introduce a private exporter label.

A credential proof captured on one transport connection MUST NOT be valid on another connection.

## 7.5 QUIC migration

QUIC connection migration MAY be enabled in Public Cloud and Enterprise Managed profiles. Enterprise Restricted and Government Sovereign deployments MAY disable it.

Migration does not create a new PAPER Working Session, but the Relay SHOULD generate a network-path-change telemetry event.

---

# 8. Connection Preface

## 8.1 QUIC

After QUIC establishment and ALPN negotiation, the client opens the first client-initiated bidirectional stream as the PAPER Control Lane and sends `HELLO` as its first PAPER record.

## 8.2 TLS/TCP

Immediately after TLS and ALPN negotiation, each side expects the following eight-octet connection preface before ordinary PAPER records:

```text
50 41 50 45 52 00 01 0A
 P  A  P  E  R  NUL 1 LF
```

The preface identifies the protocol family and framing generation; it is not a secret and is not an authentication mechanism.

A malformed preface causes immediate connection close with no expensive processing.

---

# 9. Record Framing

PAPER uses a fixed 32-octet binary prelude followed by a deterministic-CBOR header and optional payload bytes.

All fixed-width integers are network byte order.

```text
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------+---------------+-------------------------------+
| Version Major |  Record Kind  |             Flags             |
+---------------+---------------+-------------------------------+
|         Message Type          |          Header Length         |
+---------------------------------------------------------------+
|                         Payload Length                         |
+---------------------------------------------------------------+
|                           Lane ID                             |
|                                                               |
+---------------------------------------------------------------+
|                        Lane Sequence                           |
|                                                               |
+---------------------------------------------------------------+
|                           Reserved                            |
+---------------------------------------------------------------+
```

Fields:

- `version_major` — 1 octet; value `1` for this specification.
- `record_kind` — 1 octet.
- `flags` — 16-bit bit field.
- `message_type` — 16-bit registry value.
- `header_length` — length of deterministic-CBOR header.
- `payload_length` — length of following payload.
- `lane_id` — unsigned 64-bit logical lane identifier.
- `lane_sequence` — unsigned 64-bit sequence number within lane.
- `reserved` — MUST be zero when sent; non-zero values are invalid until assigned by a later core version.

Record kinds:

| Value | Name | Meaning |
|---:|---|---|
| 0 | `CONTROL` | Connection/session control |
| 1 | `MESSAGE` | Structured application message |
| 2 | `DATA` | Streaming or chunk payload |
| 3 | `ACK` | Explicit PAPER-level acknowledgement |
| 4 | `RESET` | Lane or operation reset |
| 5 | `RECEIPT` | Evidence/provenance receipt |
| 6 | `ERROR` | Structured error |
| 7 | `PING` | PAPER-level liveness/latency probe |

Transport integrity protects the record bytes. PAPER does not add an unauthenticated CRC.

Limits for baseline conformance:

- CBOR header: maximum 16 KiB;
- one `DATA` payload: maximum 1 MiB;
- recommended token/context data chunk: <= 64 KiB;
- implementation-configured object limits MUST be advertised or discoverable.

---

# 10. Canonical Structured Encoding

## 10.1 CBOR

Structured PAPER objects use CBOR.

Objects that are hashed, signed, or content-addressed MUST use the deterministic encoding profile required by PAPER. Implementations MUST reject non-canonical encodings when an object's hash/signature semantics depend on canonical bytes.

## 10.2 Numeric field labels

Normative wire schemas use unsigned integer map keys. Human-facing documentation MAY display symbolic names.

Numeric labels reduce overhead and prevent ambiguity around spelling/case.

## 10.3 Unknown fields

Objects distinguish **optional** and **critical** extensions.

A receiver:

- MAY ignore an unknown non-critical field while preserving it where forwarding rules require;
- MUST reject an object containing an unknown critical field whose semantics affect authorization, confidentiality, integrity, or routing.

## 10.4 Payloads

Large or streaming content is not embedded inside large CBOR maps. Structured metadata references payload chunks carried in `DATA` records.

---

# 11. Identifiers

PAPER opaque identifiers are 128-bit values unless a schema specifies otherwise.

Examples:

- `connection_instance_id`
- `working_session_id`
- `exchange_id`
- `message_id`
- `lease_id`
- `transfer_id`
- `broadcast_id`
- `approval_id`

Implementations SHOULD generate identifiers using a cryptographically secure random source. Time-sortable identifiers MAY be used if their construction does not reveal prohibited information or weaken unpredictability.

Identifiers are not authorization tokens.

Content digests use the selected cryptographic hash profile and are distinct from opaque IDs.

---

# 12. Common Header Map

The deterministic-CBOR header MAY contain the following common labels:

| Key | Name | Type | Description |
|---:|---|---|---|
| 1 | `exchange_id` | bstr(16) | Governed Exchange |
| 2 | `session_id` | bstr(16) | Working Session |
| 3 | `message_id` | bstr(16) | Unique message |
| 4 | `parent_ids` | array | Causal parent messages |
| 5 | `created_at_ms` | uint | Advisory UTC epoch milliseconds |
| 6 | `organization_id` | tstr/bstr | Tenant/organization |
| 7 | `peer_id` | tstr/bstr | Sender peer |
| 8 | `lease_id` | bstr(16) | Capability Lease |
| 9 | `policy_epoch` | uint/bstr | Effective policy epoch |
| 10 | `provenance_parents` | array(bstr) | Parent provenance node digests |
| 11 | `content_type` | uint/tstr | Payload type |
| 12 | `protection_profile` | uint | P0-P3 |
| 13 | `idempotency_key` | bstr | Side-effect idempotency |
| 14 | `classification` | uint/tstr | Data classification |
| 15 | `critical_fields` | array(uint) | Field keys receiver must understand |

`created_at_ms` MUST NOT be used alone to decide causal order or replay safety.

---

# 13. Message Type Registry

The initial allocation ranges are:

| Range | Family |
|---|---|
| `0x0000–0x00FF` | Core / connection |
| `0x0100–0x01FF` | Authentication / identity |
| `0x0200–0x02FF` | Sessions / capability leases |
| `0x0300–0x03FF` | Governance / approvals |
| `0x0400–0x04FF` | AI inference |
| `0x0500–0x05FF` | Context / repository |
| `0x0600–0x06FF` | Tools / runtime |
| `0x0700–0x07FF` | Provenance / evidence |
| `0x0800–0x08FF` | Chat / presence |
| `0x0900–0x09FF` | Voice |
| `0x0A00–0x0AFF` | Files |
| `0x0B00–0x0BFF` | Broadcast / administration |
| `0x0C00–0x0CFF` | Telemetry / metering |
| `0x8000–0xBFFF` | Registered extensions |
| `0xC000–0xFFFF` | Private/experimental |

A private extension MUST NOT be required for baseline interoperability unless both peers explicitly negotiate it.

---

# 14. Core Connection State Machine

```text
NEW
 │
 │ secure transport + ALPN
 ▼
TRANSPORT_READY
 │
 │ HELLO / HELLO_ACK
 ▼
NEGOTIATED
 │
 │ AUTH_CHALLENGE / AUTH_PROOF
 ▼
PEER_AUTHENTICATED
 │
 │ USER_BIND where required
 ▼
IDENTITY_BOUND
 │
 │ capability / extension negotiation
 ▼
READY
 │
 ├── open/restore Working Sessions
 │
 └── connection-level control
 ▼
DRAINING
 │
 ▼
CLOSED
```

A peer MUST NOT accept application `MESSAGE` or `DATA` records before the connection reaches `READY`, except messages explicitly permitted by the authentication state machine.

---

# 15. HELLO and Version Negotiation

## 15.1 HELLO

The initiating peer sends:

- supported PAPER core major/minor revisions;
- peer profile;
- supported transport-binding features;
- supported extensions and versions;
- supported deterministic-encoding profile;
- supported cryptographic profiles;
- random `client_nonce` of at least 256 bits;
- credential identifier or credential digest hint;
- implementation name/version as non-security metadata.

## 15.2 HELLO_ACK

Relay replies with:

- selected core version;
- selected extension versions;
- selected cryptographic profile;
- random `server_nonce` of at least 256 bits;
- Relay peer credential;
- authentication challenge;
- minimum Harness version/build policy metadata if applicable;
- connection resource limits.

The transcript of `HELLO` and `HELLO_ACK` is hashed and bound into authentication.

## 15.3 Downgrade resistance

The authentication proof includes the negotiated versions, extensions, selected crypto profile, transport binding, and channel binding.

A man-in-the-middle cannot safely modify negotiation without invalidating authentication.

Organization policy MAY reject a mutually supported but deprecated version.

---

# 16. PAPER Peer Credentials

A **PAPER Peer Credential (PPC)** is a CP or organization-authority-signed credential binding a peer ID and profile to a public key.

The baseline credential encoding is a COSE-signed canonical CBOR object.

Required claims:

- credential version;
- issuer;
- subject peer ID;
- organization/trust domain;
- peer profile;
- public-key thumbprint or embedded public key;
- not-before;
- not-after;
- credential serial;
- revocation authority;
- allowed protocol major versions;
- optional allowed build channel/version policy;
- optional deployment zone.

A credential MUST NOT contain a reusable bearer secret.

Public Patty Cloud may use Patty-operated credential issuers. Enterprise and Government Sovereign deployments MAY operate organization-local issuers.

---

# 17. Harness Enrollment

## 17.1 Baseline enrollment

1. User installs Harness.
2. Harness generates its long-lived enrollment key pair locally.
3. User authenticates to CP using the deployment's user-identity method.
4. Harness submits an enrollment request containing its public key and non-secret build metadata.
5. CP evaluates organization policy and enrollment limits.
6. CP issues a PPC with profile `HARNESS`.
7. Harness stores private key and credential in OS-appropriate secure storage where available.
8. CP records Harness/user/organization relationships independently.

## 17.2 Enrollment modes

Supported operational modes include:

- self-service public;
- enterprise SSO enrollment;
- administrator pre-approval;
- one-time enrollment code;
- offline signed enrollment bundle for air-gapped systems.

## 17.3 Security limitation

PAPER's baseline enrollment is software-only. If a user fully controls the host and can extract or reuse the Harness private key, the protocol cannot mathematically prove that the executable presenting that key is the original unmodified binary.

PAPER therefore guarantees **enrolled peer identity and protocol authorization**, not remote binary attestation, unless an optional hardware-attestation extension is deployed.

A server MAY enforce signed release/build policy and behavioral anomaly detection, but these do not replace a hardware root of trust.

---

# 18. Peer Authentication

## 18.1 Challenge

Relay sends `AUTH_CHALLENGE` containing:

- `server_nonce`;
- challenge ID;
- credential issuer expectations;
- current revocation epoch;
- authentication deadline.

## 18.2 Proof

The peer computes:

```text
auth_context =
  HASH(
    "PAPER-AUTH-v1" ||
    canonical(HELLO) ||
    canonical(HELLO_ACK) ||
    client_nonce ||
    server_nonce ||
    channel_binding ||
    peer_credential_digest
  )
```

The peer signs `auth_context` using the private key bound by the PPC.

`AUTH_PROOF` contains:

- credential;
- signature;
- key/algorithm identifier;
- challenge ID;
- optional stapled revocation/status evidence.

Relay verifies:

1. credential signature and issuer;
2. validity window;
3. revocation;
4. profile;
5. signature over exact bound transcript;
6. selected crypto/profile policy;
7. duplicate/cloning policy signals.

## 18.3 Relay authentication

Harness MUST also validate Relay's transport certificate and PAPER peer credential. A generic TLS endpoint possessing an unrelated Web PKI certificate is not sufficient if enterprise policy requires organization-managed Relay identity.

---

# 19. User Binding

Harness identity and user identity are independent.

After peer authentication, an enterprise Harness performs `USER_BIND`.

The message contains:

- user authentication assertion or opaque reference;
- organization;
- claimed user ID;
- Harness ID;
- requested persona/role context if applicable;
- authentication assurance metadata.

Relay/CP validates the assertion through the deployment's identity provider.

Accepted binding produces `USER_BIND_ACK` with:

- canonical user ID;
- organization;
- effective group/role references;
- binding expiry;
- user-policy epoch;
- optional interactive re-authentication deadline.

A Harness used by another person MUST perform a new user binding.

Government deployments MAY use local PKI, SSO, smart-card, or air-gapped identity systems without changing PAPER wire semantics.

---

# 20. Capability Negotiation

Capabilities are negotiated in two layers:

1. **implementation capability** — what the software can speak;
2. **authority capability** — what policy currently permits.

Supporting `paper.voice/1` does not mean the current user is authorized to use voice.

The `CAPABILITIES` object lists extensions such as:

```text
paper.ai/1
paper.context/1
paper.tools/1
paper.provenance/1
paper.chat/1
paper.voice/1
paper.file/1
paper.broadcast/1
paper.telemetry/1
```

Unknown optional extensions are ignored. Unknown critical extensions fail negotiation.

---

# 21. Working Sessions

A Working Session is a policy-bound lifecycle that can outlive one transport connection.

`SESSION_OPEN` requests:

- organization;
- user binding;
- project;
- repository and branch where applicable;
- task purpose;
- requested extensions;
- requested model class;
- requested tool classes;
- retention profile;
- client session nonce.

Relay/CP returns `SESSION_GRANT` or denial.

`SESSION_GRANT` contains:

- session ID;
- effective Policy Epoch;
- initial Capability Lease;
- retention/classification summary;
- allowed model classes;
- protection profile;
- session TTL;
- idle TTL;
- resumption policy.

A Working Session cannot outlive its maximum organization policy or all valid Capability Leases.

---

# 22. Capability Leases

A Capability Lease is a signed authorization object.

It is not a bearer token by itself; use requires a bound authenticated peer/session.

Required fields:

- lease ID;
- issuer;
- subject Harness peer ID;
- user ID;
- organization;
- Working Session;
- not-before / expiry;
- Policy Epoch;
- allowed peer counterpart profiles;
- allowed extensions;
- allowed model package classes or exact IDs;
- repository/branch scope;
- file path/read/write scope;
- tool classes and constraints;
- network destinations/purposes where applicable;
- token/context/resource budgets;
- protection profile;
- required approvals;
- lease sequence/revision.

Lower-layer components MUST NOT broaden a lease.

A Relay MUST validate lease signature, expiry, peer/session binding, Policy Epoch, and action scope before protected operations.

Short lease durations are RECOMMENDED for high-risk sessions. Renewal produces a new lease ID or revision and is itself a provenance event.

---

# 23. Policy Epochs

A Policy Epoch identifies the exact effective policy used for governance decisions.

An epoch includes or references:

- organization policy digest;
- project/repository overlay digest;
- model policy digest;
- DLP/security profile digest;
- approval matrix digest;
- retention policy digest;
- policy engine/schema version.

An epoch identifier MUST be immutable once issued.

If policy changes during a Working Session, CP distributes a new epoch. Relays then apply configured transition semantics:

- immediate invalidate;
- finish current read-only exchange then renew;
- allow until lease expiry;
- explicit user/reviewer acknowledgement.

Security-critical revocations override ordinary transition grace.

Every Relay Verdict and Evidence Receipt identifies the Policy Epoch used.

---

# 24. Governed Exchange

A Governed Exchange is PAPER's central application primitive.

An exchange has:

- opaque exchange ID;
- exchange class;
- initiator identity;
- Working Session;
- Capability Lease;
- Policy Epoch;
- purpose;
- causal parent references;
- one or more lanes;
- zero or more Relay Verdicts;
- zero or more artifacts;
- provenance nodes;
- terminal status;
- Evidence Receipt.

Exchange classes include:

- AI inference;
- context disclosure;
- tool execution;
- runtime action;
- chat message;
- voice transfer;
- file transfer;
- broadcast;
- administrative directive;
- provenance binding;
- metering event.

A protected exchange MUST NOT transition to execution until its required governance preconditions are satisfied.

---

# 25. Exchange State Machine

```text
CREATED
  │
  │ EXCHANGE_OPEN
  ▼
AUTHORIZING
  │
  ├── DENY ───────────────► DENIED
  ├── QUARANTINE ─────────► QUARANTINED
  ├── approval needed ────► WAITING_APPROVAL
  │                            │
  │                            ├── reject -> DENIED
  │                            └── approve
  ▼
AUTHORIZED
  │
  │ one or more streams/actions
  ▼
ACTIVE
  │
  ├── policy revoke -> TERMINATED
  ├── failure ------> FAILED
  └── normal close
  ▼
FINALIZING
  │
  │ provenance/evidence receipt
  ▼
COMPLETED
```

Terminal states are immutable; correction is represented by a new linked exchange/event, not mutation of signed history.

---

# 26. Governance Envelope

Every protected exchange carries a Governance Envelope.

Conceptual fields:

```text
governance = {
  lease_id,
  policy_epoch,
  organization,
  user_id,
  harness_id,
  session_id,
  project_id?,
  repository_id?,
  branch?,
  classification,
  purpose,
  requested_capabilities,
  model_authorization?,
  tool_authorization?,
  protection_profile,
  approval_requirements[],
  retention_profile
}
```

The Relay treats Harness-supplied policy values as requests or context, not authoritative policy. Authoritative values come from validated lease/CP state.

---

# 27. Relay Verdict

A Relay enforcement step produces a structured verdict.

Results:

- `ALLOW`
- `ALLOW_TRANSFORM`
- `ALLOW_WITH_OBLIGATION`
- `REQUIRE_USER_CONFIRMATION`
- `REQUIRE_REVIEWER_APPROVAL`
- `REQUIRE_SECURITY_APPROVAL`
- `REQUIRE_DUAL_APPROVAL`
- `QUARANTINE`
- `DENY`
- `TERMINATE_SESSION`
- `ISOLATE_RUNTIME`
- `CREATE_INCIDENT`

A verdict includes:

- verdict ID;
- exchange ID;
- Relay ID;
- Policy Epoch;
- rule IDs and reason codes;
- transformations;
- obligations;
- time;
- evidence node digest;
- authentication mode.

For high-frequency events, an inline verdict MAY use a connection-bound authenticated tag and be included in the final signed Evidence Receipt. Security-significant verdicts such as denial, policy transformation, approval, quarantine, session termination, or model recall SHOULD also be emitted as individually signed evidence objects according to deployment policy.

---

# 28. Stream Contracts

Every non-control lane begins with `STREAM_OPEN` containing a Stream Contract.

Required fields:

- lane ID;
- stream class;
- extension/version;
- initiator profile;
- intended counterparty profile;
- Working Session;
- Exchange ID;
- Capability Lease;
- Policy Epoch;
- protection profile;
- content class/type;
- maximum bytes/messages;
- priority;
- ordering semantics;
- acknowledgement semantics;
- resumability class;
- retention class;
- idempotency class.

The Relay verifies the contract before accepting substantive data.

A sender MUST NOT repurpose a lane for another content class.

---

# 29. Lane Classes and Priority

Baseline lanes:

| Class | Typical use | Priority |
|---|---|---:|
| `CONTROL` | auth/session/policy | 0 |
| `BROADCAST_CRITICAL` | emergency notices | 1 |
| `AI_INTERACTIVE` | token streaming | 2 |
| `TOOL_CONTROL` | tool auth/results | 2 |
| `CHAT_INTERACTIVE` | employee chat | 3 |
| `PRESENCE` | ephemeral status | 4 |
| `VOICE` | voice messages | 5 |
| `FILE` | transfers | 6 |
| `TELEMETRY` | metrics/usage | 7 |

Lower numeric value means higher scheduling priority.

A large file transfer MUST NOT starve an interactive model response or emergency administrative message.

---

# 30. QUIC Lane Mapping

A QUIC implementation SHOULD map each long-lived or high-volume logical lane to a dedicated QUIC stream.

The `lane_id` remains present in PAPER records even when QUIC already identifies the transport stream because:

- a logical lane may be reattached after reconnect;
- the TCP binding uses the same identifier;
- provenance references the logical lane independent of transport.

A QUIC stream may carry only one PAPER lane unless an extension explicitly defines aggregation.

---

# 31. TCP Multiplexing

Under TLS/TCP, all lanes share one byte stream.

The 32-byte PAPER prelude identifies the lane and sequence.

Receivers MUST maintain independent:

- per-lane ordering;
- flow-control windows;
- reset state;
- acknowledgement state.

A stalled low-priority lane MUST NOT cause the sender to intentionally withhold higher-priority records, subject to unavoidable TCP head-of-line behavior.

Implementations SHOULD use bounded per-lane buffers and priority-aware write scheduling.

---

# 32. Evidence Hashing and Domain Separation

PAPER content-addressed objects use an algorithm selected by the active cryptographic profile.

Baseline interoperable hash: SHA-256.

For an object `O` of registered type `T`:

```text
object_digest =
  HASH(
    "PAPER-OBJ-v1\0" ||
    uint16(T) ||
    deterministic_cbor(O_without_digest)
  )
```

For a payload chunk:

```text
chunk_digest =
  HASH(
    "PAPER-CHUNK-v1\0" ||
    exchange_id ||
    lane_id ||
    lane_sequence ||
    payload_bytes
  )
```

Domain-separation strings are ASCII exactly as shown.

A deployment MAY select a stronger or nationally required hash through a cryptographic profile, but all participants in an exchange MUST agree on one digest profile.

---

# 33. Provenance Spine

The Provenance Spine is a content-addressed directed acyclic graph.

Each node contains:

- node type;
- actor/peer identity;
- Working Session;
- Exchange ID;
- Policy Epoch where relevant;
- object/artifact references;
- causal parent node digests;
- result/status;
- optional human-readable summary;
- node digest.

Baseline node types:

- `USER_INTENT`
- `CONTEXT_REQUEST`
- `CONTEXT_DISCLOSURE`
- `POLICY_DECISION`
- `MODEL_INVOCATION`
- `MODEL_OUTPUT`
- `TOOL_PROPOSAL`
- `TOOL_DECISION`
- `TOOL_RESULT`
- `FILE_READ`
- `FILE_WRITE`
- `PATCH_CREATED`
- `HUMAN_EDIT`
- `AI_EDIT`
- `REVIEW_DECISION`
- `COMMIT_BIND`
- `CHAT_MESSAGE`
- `VOICE_MESSAGE`
- `FILE_TRANSFER`
- `BROADCAST`
- `ADMIN_ACTION`
- `SECURITY_FINDING`
- `ARTIFACT_EXPORT`

A node may have multiple parents. This represents causal convergence such as a code change based on several context files and a reviewer instruction.

Absence of a parent does not imply independence if the implementation failed to record a required relationship; conformance tests define mandatory edges for protected workflows.

---

# 34. Exchange Evidence Chain

Within one exchange, Relay maintains an ordered evidence chain:

```text
R0 = HASH("PAPER-EVIDENCE-START-v1\0" || exchange_open_digest)

Ri = HASH(
       "PAPER-EVIDENCE-EVENT-v1\0" ||
       R(i-1) ||
       event_digest
     )
```

At finalization, the Relay generates an Evidence Receipt containing:

- exchange ID;
- final state;
- first/last event sequence;
- final chain root `Rn`;
- Provenance Spine root/terminal node references;
- Policy Epoch;
- lease digest;
- Relay identity;
- model/PIA identity where applicable;
- key algorithm;
- signature;
- redaction/omission manifest if exporting a subset.

The receipt is encoded as COSE-signed CBOR.

The evidence chain proves the Relay's recorded order and final root. It does not by itself prove that every underlying claim is factually true. Trust in actors and collection components remains part of the threat model.

---

# 35. External Transparency

PAPER does not create its own global transparency ledger.

Deployments MAY submit PAPER Evidence Receipts or selected provenance statements to external transparency infrastructure.

SCITT-compatible export is a recommended integration path where organizations require independently auditable signed-statement transparency.

PAPER conformance does not require a public transparency service, which preserves air-gapped operation.

---

# 36. Payload Protection Profiles

## 36.1 P0 — Relay Inspectable

Transport provides hop security. Relay receives plaintext content and may inspect/transform according to policy.

Typical use:

- enterprise AI prompts;
- code/context;
- responses requiring DLP;
- malware scanning;
- compliance inspection.

P0 is the default for governed AI traffic.

## 36.2 P1 — Service Sealed

Payload is additionally encrypted to a named authorized processing service/key domain using a standardized recipient-sealing construction.

Routing components may see metadata but cannot see plaintext.

This supports decomposed Relay architectures where only an inspection service should decrypt.

## 36.3 P2 — Endpoint Sealed

Content is encrypted between designated endpoints, for example Harness ↔ PIA. Relay sees required routing/governance metadata but cannot inspect content.

A deployment MUST NOT claim inline Relay content DLP for P2 payloads.

P2 is optional and disabled by default for high-governance inference.

## 36.4 P3 — Group E2EE

Used by collaboration channels requiring group endpoint confidentiality.

If enabled, the implementation SHOULD use MLS rather than a PAPER-defined group key protocol.

Organization policy MUST explicitly address the impact on:

- DLP;
- eDiscovery;
- legal hold;
- moderation;
- archival;
- administrative content visibility.

---

# 37. Cryptographic Profiles

PAPER is algorithm-agile.

## 37.1 PAPER-BASE-1

Every interoperable implementation MUST support a profile with:

- TLS 1.3;
- QUIC security for QUIC binding;
- SHA-256 content digests;
- COSE;
- ECDSA P-256/SHA-256 for PAPER credential/evidence signatures;
- AES-GCM-capable TLS suites accepted by deployment policy.

Implementations MAY additionally support Ed25519, stronger hash profiles, or other registered algorithms.

## 37.2 Application sealing

Where P1/P2 is enabled, HPKE is RECOMMENDED.

A baseline interoperable HPKE suite SHOULD use widely supported standardized P-256/HKDF-SHA-256/AES-GCM components. Deployments MAY negotiate X25519 or other registered suites.

## 37.3 Group encryption

P3 implementations SHOULD use MLS and an organization-approved cipher suite.

## 37.4 Post-quantum profiles

PAPER's registries allow hybrid TLS key exchange and post-quantum signatures where standardized and deployment-approved.

Post-quantum support is optional in PAPER v1 and MUST NOT be advertised unless interoperable test vectors pass.

Government deployments may constrain algorithms to cryptographic modules and profiles accepted by their procurement/accreditation environment.

---

# 38. Key Domains

Deployments SHOULD separate:

- root/organization issuance key;
- Harness credential issuance;
- Relay identity;
- PIA identity;
- Capability Lease signing;
- Policy bundle signing;
- model-package signing;
- evidence-receipt signing;
- admin-directive signing;
- application sealing keys;
- offline update signing.

One compromised key SHOULD NOT automatically authorize every protocol function.

Keys have:

- activation time;
- expiry;
- rotation overlap;
- revocation status;
- algorithm;
- key ID;
- historical verification record.

---

# 39. AI Inference Extension (`paper.ai/1`)

## 39.1 AI_OPEN

Harness requests an AI inference exchange with:

- task/intention reference;
- requested model class or exact approved model;
- inference mode;
- requested maximum input/output tokens;
- context-manifest reference;
- response format;
- permitted tool classes;
- model parameter request;
- provenance parents.

The Harness MUST NOT address a raw serving-engine URL.

## 39.2 Relay authorization

Relay:

1. verifies lease;
2. resolves approved model;
3. applies prompt/context security;
4. applies quotas;
5. resolves a PIA endpoint lease;
6. emits Relay Verdict;
7. opens downstream PAPER inference lane to PIA.

## 39.3 INFERENCE_REQUEST

Relay → PIA includes:

- Exchange ID;
- endpoint lease;
- exact PMP/model artifact ID;
- sanitized prompt/context payload or refs;
- inference parameters within policy;
- response/tool schema;
- deadline and cancellation semantics;
- provenance/evidence linkage.

## 39.4 Token streaming

PIA emits `AI_TOKEN_CHUNK` records.

Each chunk includes:

- output sequence;
- token/text bytes;
- finish metadata if terminal;
- optional per-chunk usage counters.

Relays MAY coalesce token chunks for efficiency but MUST preserve logical ordering and final provenance linkage.

## 39.5 Completion

`AI_COMPLETE` includes:

- completion reason;
- input/output token accounting;
- model package;
- inference endpoint;
- engine metadata allowed by policy;
- generated-output digest;
- tool proposals;
- error/finish state.

---

# 40. PIA and Model Identity

## 40.1 PIA enrollment

PIA is an `INFERENCE` peer with its own PPC.

PIA authenticates to Relay exactly as a first-class PAPER peer.

## 40.2 Patty Model Package

A PMP is a signed manifest containing:

- package ID/version;
- model family;
- base/fine-tune lineage where known;
- weight artifact digests;
- tokenizer digest;
- template digest;
- quantization profile;
- supported context limit;
- inference-engine compatibility;
- license/use restrictions;
- evaluation bundle references;
- publisher;
- signature.

Model authorization MUST compare cryptographic package identity, not a user-controlled model name string.

## 40.3 Endpoint registration

CP associates:

```text
PIA identity
+ PMP identity
+ serving deployment profile
+ endpoint identity
+ approval state
```

## 40.4 Endpoint Lease

Relays route only to an endpoint with a current signed Endpoint Lease.

Endpoint Lease fields include:

- PIA peer ID;
- PMP ID/digest;
- serving instance;
- permitted organizations/classifications;
- valid time;
- capacity class;
- revocation epoch.

A model recall invalidates endpoint leases.

## 40.5 Serving-engine isolation

The raw vLLM/SGLang/other engine interface SHOULD be bound to loopback or an isolated local network reachable only by PIA.

PIA MAY translate PAPER to a serving engine's local native or compatibility API internally. That translation is not a PAPER network binding and MUST NOT be exposed to Harnesses as an alternative route.

---

# 41. Context Extension (`paper.context/1`)

Context is a governed resource.

## 41.1 Context object types

- repository file/span;
- symbol/AST node;
- Git diff;
- issue;
- document;
- schema/API;
- log;
- terminal output;
- build/test output;
- chat message explicitly attached as context;
- organization knowledge object.

## 41.2 CONTEXT_MANIFEST

Before large disclosure, Harness/Relay SHOULD create a manifest with:

- context item ID;
- source;
- repository/commit;
- path/symbol/span;
- byte/token estimate;
- classification;
- trust label;
- reason for inclusion;
- transformations;
- provenance digest.

## 41.3 Trust labels

Baseline labels:

- `TRUSTED_POLICY`
- `TRUSTED_REPOSITORY`
- `AUTHORIZED_INTERNAL`
- `USER_SUPPLIED`
- `EXTERNAL_UNTRUSTED`
- `MODEL_GENERATED`
- `UNKNOWN`

Trust label is not authority. Untrusted text cannot change a lease.

## 41.4 CONTEXT_DECISION

Relay records per-item:

- allow;
- metadata only;
- allow transformed/redacted;
- require approval;
- deny.

Secrets/PII/injection systems are implementation components; PAPER carries their structured decisions.

---

# 42. Tool and Runtime Extension (`paper.tools/1`)

## 42.1 Tool proposal

A model may emit `TOOL_PROPOSE` but cannot execute directly.

The proposal includes:

- tool ID/class;
- semantic purpose;
- arguments;
- affected resources;
- requested authority;
- estimated side effects;
- provenance parents.

## 42.2 Tool authorization

Relay and Runtime policy evaluate the proposal against the Capability Lease.

High-risk classes may require human approval.

## 42.3 Tool intent normalization

Implementations SHOULD normalize shell/filesystem/network operations into structured intent before authorization.

For a command:

- executable;
- arguments;
- working directory;
- environment references;
- read/write paths;
- network implications;
- expected outputs;
- risk class.

Opaque shell strings MAY be retained for compatibility but MUST NOT bypass structured policy.

## 42.4 RUNTIME_EXECUTE

Relay sends an authorized action with:

- runtime peer;
- scoped action token;
- exact operation;
- resource budget;
- expiry;
- evidence linkage.

## 42.5 Result

Runtime emits:

- exit/result state;
- stdout/stderr references;
- modified files;
- produced artifacts;
- network/package events;
- resource usage;
- result digest.

The result becomes provenance context for subsequent model turns.

---

# 43. Code Provenance

PAPER integrates with Git/SCM but does not replace it.

A coding Working Session binds to:

- repository identity;
- canonical remote where applicable;
- baseline commit;
- branch;
- worktree/sandbox ID.

File-change events identify:

- path;
- base blob digest;
- result blob digest;
- patch hunks;
- AST/symbol identifiers where available;
- generating Exchange;
- human/AI attribution class;
- subsequent edit parents.

At commit or PR creation, `COMMIT_BIND` links:

- commit/PR ID;
- parent commit(s);
- provenance node set/root;
- review approvals;
- tests/scans;
- Evidence Receipt.

PAPER allows attribution states such as:

- AI-generated;
- human-generated;
- AI-generated then human-edited;
- human-generated then AI-refactored;
- mixed/ambiguous;
- template-derived.

A line number alone is insufficient. Implementations SHOULD preserve lineage across rename/refactor using patch mapping, symbols/AST fingerprints, semantic matching, and explicit ambiguity state.

---

# 44. Chat Extension (`paper.chat/1`)

PAPER collaboration is not an ungoverned side channel.

Conversation classes:

- 1:1;
- group;
- project;
- incident;
- temporary session handoff.

A chat message contains:

- conversation ID;
- sender user/Harness;
- recipients or group;
- message ID;
- causal reply parent;
- content;
- classification;
- retention profile;
- optional attachment references;
- protection profile.

Default enterprise mode is P0 or P1 according to organization policy.

P3 group E2EE is optional.

A chat message does not automatically become model context. A user or authorized workflow must create a separate `CONTEXT_ATTACH` exchange, which is governed and provenance-linked.

Edits and deletions are represented as new events. Durable audit policy may retain the original even when the user-facing view displays the edited/deleted state.

---

# 45. Presence Extension

Presence is low-value ephemeral state and SHOULD NOT create heavyweight signed evidence for every heartbeat.

Fields may include:

- user ID;
- Harness ID;
- available/away/do-not-disturb/offline;
- current project indicator if policy permits;
- last activity bucket;
- expiry.

Presence has short TTL and is automatically expired.

Administrative monitoring MUST NOT infer performance solely from presence duration.

---

# 46. Voice Extension (`paper.voice/1`)

Version 1 standardizes asynchronous voice messages, not live telephony.

Flow:

1. `VOICE_OPEN`
2. `VOICE_METADATA`
3. one or more `VOICE_CHUNK`
4. optional server-side malware/content processing
5. `VOICE_COMMIT`
6. recipient acknowledgement

Metadata:

- message/conversation;
- codec/container;
- duration;
- size;
- chunk size;
- full-content digest;
- classification;
- retention;
- protection profile.

Opus is RECOMMENDED as an interoperable voice codec, but codec negotiation is extensible.

Transcription is a separate governed AI exchange. A transcript MUST reference the exact voice-message digest and transcription model/provenance.

A voice message is not AI context unless explicitly attached.

---

# 47. File Transfer Extension (`paper.file/1`)

PAPER-managed file transfer is resumable and auditable.

## 47.1 FILE_OFFER

Contains:

- transfer ID;
- sender/recipient/conversation;
- filename;
- MIME/media type;
- size;
- full file digest;
- classification;
- purpose;
- retention;
- protection profile;
- optional repository/artifact linkage.

## 47.2 Authorization and scanning

Relay policy may:

- allow;
- require scan;
- quarantine;
- require approval;
- deny by type/size/classification.

Executable or archive content MAY receive stricter controls.

## 47.3 Chunks

`FILE_CHUNK` identifies:

- transfer ID;
- chunk index/offset;
- bytes;
- chunk digest.

The receiver/Relay may acknowledge ranges.

## 47.4 Commit

`FILE_COMMIT` succeeds only when full-content digest matches and required security gates complete.

A transferred file does not become model context until a distinct governed Context Exchange authorizes that use.

---

# 48. Broadcast Extension (`paper.broadcast/1`)

Broadcast severities:

- `INFO`
- `ADVISORY`
- `WARNING`
- `CRITICAL`
- `EMERGENCY`

A broadcast includes:

- sender authority;
- target selector;
- severity;
- title/body;
- creation/expiry;
- acknowledgement requirement;
- display behavior;
- related incident/maintenance/policy refs;
- signature/evidence.

Targets may be:

- organization;
- affiliate;
- department;
- group;
- project;
- repository;
- Harness cohort;
- active session cohort;
- model users;
- network zone.

Broadcasting a message is **not** an execution-control primitive.

For example, "Model X is recalled" as text does not disable Model X. A separate authenticated administrative directive must update policy/endpoint leases.

---

# 49. Administrative Directives

`ADMIN_DIRECTIVE` is a high-authority exchange family.

Examples:

- revoke Harness;
- revoke session;
- force policy refresh;
- recall model/PMP;
- disable extension;
- pause high-risk sessions;
- isolate runtime;
- enable change freeze;
- require mandatory acknowledgment;
- drain Relay/PIA endpoint.

Requirements:

- sender must possess administrative authority for exact scope;
- directive carries Policy Epoch and idempotency key;
- critical directives SHOULD require dual authorization when organization policy says so;
- Relay/Harness displays an auditable reason;
- execution and acknowledgement generate signed evidence.

An ordinary chat/broadcast sender cannot construct an effective directive.

---

# 50. Telemetry and Metering

Telemetry classes:

- connection health;
- Harness health/version;
- session counts;
- latency;
- tokens;
- model usage;
- Relay resource use;
- PIA capacity;
- security counters;
- collaboration counts;
- transfer bytes;
- error rates.

Metering events used for billing or internal chargeback MUST be tied to authenticated Exchange/Session IDs and cannot rely solely on client self-report.

Prompt content is not telemetry.

Organizations configure telemetry retention separately from engineering content.

---

# 51. Replay Protection

Replay protections exist at multiple layers:

- TLS/QUIC record protection;
- random connection nonces;
- channel-bound auth proof;
- unique Working Session/Exchange IDs;
- per-lane monotonic sequence;
- lease expiry;
- Policy Epoch;
- idempotency keys;
- action/result receipts.

A receiver MUST maintain a replay window sufficient for active sessions and configured reconnect duration.

A stale lease or completed side-effecting idempotency key MUST NOT execute again.

---

# 52. Idempotency Classes

Every side-effecting message class declares one:

- `SAFE_REPLAY`
- `SAME_KEY_ONLY`
- `QUERY_BEFORE_RETRY`
- `NEVER_AUTORETRY`

Examples:

| Operation | Class |
|---|---|
| Presence update | SAFE_REPLAY |
| Context read | SAME_KEY_ONLY |
| Model request before tool side effect | SAME_KEY_ONLY |
| Shell command | QUERY_BEFORE_RETRY |
| File finalization | SAME_KEY_ONLY |
| Broadcast | SAME_KEY_ONLY |
| Admin model recall | SAME_KEY_ONLY |
| Destructive runtime action | NEVER_AUTORETRY unless status known |

---

# 53. Session Resumption

A disconnected Harness may request resumption with:

- Working Session ID;
- resumption credential/token;
- fresh authenticated connection;
- last acknowledged lane sequence;
- last Evidence Receipt/checkpoint;
- active Exchange IDs.

Relay validates:

- session validity;
- peer/user binding;
- Policy Epoch;
- lease freshness;
- stream resumability;
- retained server state.

Each lane declares whether it is:

- non-resumable;
- resume from acknowledged record;
- resume from chunk offset;
- query-status then resume;
- restart exchange.

Transport switch QUIC→TCP may resume the same Working Session after full PAPER authentication.

---

# 54. Inference Disconnect Semantics

If the Harness disconnects while inference is running, policy chooses:

- immediate cancel;
- short grace;
- continue and retain response;
- continue only for background task class.

Relay MUST bound buffering.

On reconnect, the Harness queries exchange status. If output remains available, it may resume from a checkpoint. Otherwise the exchange ends with explicit partial/canceled status.

PAPER MUST NOT silently re-run a completed tool side effect while reconstructing an inference conversation.

---

# 55. Error Model

Error domains:

- `TRANSPORT`
- `FRAMING`
- `VERSION`
- `AUTH`
- `PROFILE`
- `CAPABILITY`
- `SESSION`
- `LEASE`
- `POLICY`
- `APPROVAL`
- `CONTEXT`
- `SECURITY`
- `MODEL`
- `INFERENCE`
- `RUNTIME`
- `CHAT`
- `VOICE`
- `FILE`
- `BROADCAST`
- `QUOTA`
- `PROVENANCE`
- `EVIDENCE`

An error object contains:

- stable numeric error code;
- domain;
- severity;
- retry class;
- message localization key;
- safe human detail;
- affected Exchange/Lane;
- rule/reason codes if authorized;
- incident/reference ID;
- retry-after where relevant.

Sensitive resource existence MUST NOT be leaked through differential errors to unauthorized peers.

---

# 56. Failure Policy

Mandatory identity, lease, model authorization, or integrity failures are fail-closed.

For content-protected enterprise operations, required DLP/security/evidence services also fail closed after bounded signed-buffer thresholds.

An organization MAY define a degraded mode that allows local/read-only Harness functions while prohibiting:

- model disclosure;
- new tool execution;
- artifact export;
- file transfer;
- privileged administration.

A degraded state is visible to the user and recorded.

---

# 57. Flow Control and Resource Limits

Relays enforce limits by:

- source IP/network;
- peer;
- user;
- organization;
- Working Session;
- lane;
- Exchange;
- model;
- file/voice type.

Authenticate before allocating expensive scanners, database work, model capacity, or large buffers.

Recommended implementation controls:

- maximum concurrent connections;
- maximum lanes per connection;
- maximum pending exchanges;
- per-lane byte window;
- per-session context/token budget;
- maximum file/voice size;
- malformed-record threshold;
- auth-failure backoff;
- per-tenant model concurrency.

PAPER application priority complements transport flow control; it does not override QUIC congestion control.

---

# 58. DoS and Abuse Resistance

A Relay SHOULD:

- perform cheap syntax/preface checks first;
- rate-limit HELLO and failed authentication;
- cap credential object sizes;
- verify basic credential structure before expensive external lookups;
- use bounded CBOR parser depth and collection lengths;
- never preallocate payload_length without limits;
- cancel streams exceeding declared Stream Contract;
- isolate repeated profile/state violations;
- support organization-level circuit breakers;
- preserve control/broadcast capacity under load.

Large language-model allocation happens only after peer/session/lease validation.

---

# 59. Privacy and Administrative Visibility

PAPER provides structured visibility; it does not mandate that every administrator may read every payload.

Data categories include:

1. operational metadata;
2. engineering content;
3. collaboration content;
4. security/evidence content.

Organization policy defines roles and purposes for access.

Reading protected prompt, chat, voice, attachment, incident, or provenance content SHOULD itself produce an administrative audit event.

Protection modes P2/P3 may make content technically unavailable to Relay/admin services; interfaces must not pretend inspection occurred.

---

# 60. Data Retention

Retention classes are referenced in the Governance Envelope.

Separate policies SHOULD exist for:

- raw prompt;
- redacted prompt;
- model response;
- context;
- tool output;
- code provenance;
- chat;
- voice;
- file transfer;
- telemetry;
- security findings;
- Evidence Receipts.

Deletion of content MAY preserve non-content cryptographic evidence where legally/organizationally required.

An exported evidence subset must include an omission/redaction manifest so a verifier can distinguish "not included" from "never existed."

---

# 61. Korean Enterprise Profile

PAPER remains language-neutral on the wire, while the product profile supports Korean enterprise requirements.

Implementations SHOULD support organization attributes for:

- Korean and Romanized display names;
- team/department/division;
- legal entity/affiliate;
- 직급 and 직책 as distinct fields;
- contractor/SI affiliation;
- KST display.

Relay policy metadata SHOULD support Korean PII classifications and organization-defined sensitive terminology.

PAPER does not hard-code a Korean PII detector; it standardizes the decision/result metadata emitted by such detectors.

Central administrators can scope policy by affiliate, department, project, repository, user, Harness, or classification.

---

# 62. Contractor / SI Profile

A contractor identity SHOULD support:

- explicit sponsoring organization;
- project/repository scope;
- start/expiry;
- restricted chat/file groups;
- stricter export;
- separate Harness enrollment;
- mandatory offboarding/revocation;
- evidence handoff.

A contractor's Capability Lease MUST NOT implicitly inherit unrelated employee access.

---

# 63. Government Sovereign Profile

Government Sovereign deployments use the same PAPER protocol.

Requirements:

- no mandatory public Internet service;
- local CP/Relay/PIA;
- local identity/trust anchors;
- offline enrollment option;
- offline revocation/update distribution;
- local collaboration;
- local evidence verification;
- algorithm-profile restriction;
- configurable no-telemetry-to-vendor policy.

Protocol update bundles SHOULD include schemas, registry snapshots, test vectors, migration instructions, signatures, and rollback material.

A Government deployment MAY operate entirely with organization-owned GPUs and model packages.

---

# 64. Control Plane to Relay State

CP distributes signed state to Relays:

- trust bundles;
- revocation state;
- Policy Epochs;
- Capability Lease issuer keys;
- model/PMP registry;
- Endpoint Leases;
- approval state;
- user/group references;
- quotas;
- retention profiles;
- broadcast/admin authority keys.

Relays SHOULD cache enough signed state for bounded continuity during short CP outage.

Security-critical revocation latency targets are deployment policy.

Relay MUST NOT fabricate a new policy epoch while disconnected.

---

# 65. Model Recall

Model recall flow:

1. authorized admin/automation creates recall decision;
2. CP suspends PMP and invalidates Endpoint Leases;
3. CP distributes signed recall/admin directive;
4. Relays stop opening new inference;
5. policy determines whether in-flight inference is canceled;
6. PIA receives drain/suspend;
7. Evidence Receipts reference recall state;
8. Harness receives user-visible operational notice where appropriate.

A text broadcast alone is not sufficient.

---

# 66. Observability

PAPER implementations SHOULD export internal metrics through deployment-standard observability systems.

Useful metrics:

- handshake success/failure;
- QUIC/TCP ratio;
- connection setup latency;
- session resumption;
- active lanes;
- Relay verdict counts;
- approval waits;
- token TTFT;
- stream token rate;
- PIA queue;
- provenance events/sec;
- Evidence Receipt latency;
- file/voice throughput;
- malformed frames;
- profile violations;
- revocation propagation.

Distributed tracing IDs may be carried as non-authoritative metadata. Trace IDs are not provenance IDs.

---

# 67. Protocol Extension Governance

An extension specification must define:

- identifier/version;
- peer profiles;
- message type allocations;
- schemas;
- state machine;
- capability negotiation;
- Stream Contracts;
- authority requirements;
- idempotency;
- security considerations;
- privacy considerations;
- test vectors;
- conformance scenarios;
- backward-compatibility behavior.

Security-critical semantics MUST NOT be introduced through undocumented implementation-specific fields.

---

# 68. Versioning

PAPER versioning has:

- transport binding behavior;
- core major version;
- core minor revision;
- extension versions;
- object-schema versions;
- cryptographic profiles.

Core major incompatibility is negotiated at HELLO.

Minor revisions MUST remain backward compatible within the major version or be exposed as capability extensions.

Unknown security-critical fields cause failure.

Deprecation metadata SHOULD include:

- first deprecated version;
- removal-not-before date;
- replacement;
- security rationale;
- offline/government migration guidance.

---

# 69. Registry Management

Until formal standards governance exists, the open-source PAPER project maintains public registries for:

- message types;
- field labels;
- peer profiles;
- extensions;
- error codes;
- content types;
- reason codes;
- cryptographic profiles;
- protection profiles.

Allocation policy:

- core assignments require specification review;
- registered extension space requires public documentation and tests;
- experimental space may be used without central assignment but cannot be required for baseline interoperability.

---

# 70. Conformance Suite

A conforming implementation ships or passes tests for:

## 70.1 Framing

- short read/write;
- split records;
- concatenated records;
- invalid lengths;
- non-zero reserved;
- unknown optional type;
- invalid critical field;
- oversized CBOR;
- nesting bombs.

## 70.2 Authentication

- valid PPC;
- expired PPC;
- revoked PPC;
- wrong issuer;
- wrong profile;
- proof from another TLS connection;
- modified HELLO;
- nonce replay;
- cloned concurrent credential policy.

## 70.3 Leases

- valid action;
- expired lease;
- wrong Harness;
- wrong user;
- wrong repository;
- wrong branch;
- model outside scope;
- tool outside scope;
- stale Policy Epoch.

## 70.4 AI/PIA

- valid PMP;
- model-name spoof with wrong digest;
- invalid Endpoint Lease;
- recalled model;
- PIA wrong profile;
- interrupted token stream;
- cancellation.

## 70.5 Tools

- prompt injection attempts escalation;
- duplicated side-effect idempotency;
- unapproved command;
- denied network;
- runtime result mismatch.

## 70.6 Collaboration

- unauthorized group;
- chat edit/delete;
- chat-to-context requires explicit attachment;
- voice chunk resumption;
- file digest mismatch;
- quarantine;
- broadcast target/ack;
- admin-directive privilege separation.

## 70.7 Provenance

- node hash verification;
- causal parents;
- evidence chain;
- signed receipt;
- omitted-content manifest;
- code rename/refactor lineage scenario.

---

# 71. Fuzzing Requirements

Reference implementations MUST expose fuzz targets for:

- record parser;
- deterministic-CBOR decoder;
- credential parser;
- Capability Lease;
- Governance Envelope;
- Stream Contract;
- Relay Verdict;
- provenance node;
- file/voice chunk state machine;
- version/extension negotiation.

Stateful fuzzing SHOULD generate illegal message sequences, not only malformed bytes.

Security release gates SHOULD include cross-version corpus replay.

---

# 72. Reference Implementation Requirements

The project SHOULD provide:

- Rust implementation focused on Harness/Relay/PIA and safe parsing;
- Go implementation for interoperability and enterprise/cloud services;
- language-neutral CDDL schemas;
- golden byte vectors;
- CLI protocol inspector that redacts protected content by default;
- local test Relay;
- PIA adapter examples for vLLM and SGLang;
- mock CP state distributor;
- conformance runner.

Reference code does not define the protocol when it conflicts with this specification. The specification and registered schemas are authoritative.

---

# 73. Security Considerations

## 73.1 Protocol knowledge

PAPER is open. Reverse engineering is not a threat to protocol confidentiality because no security property depends on secrecy of message definitions.

## 73.2 Modified Harness

Without hardware-rooted attestation, an attacker controlling the machine may modify the Harness and potentially reuse its enrolled key. PAPER cannot provide impossible binary-integrity guarantees under that threat.

Mitigations include:

- protected local key storage;
- short-lived peer credentials or rotations;
- revocation;
- release/build policy;
- behavioral anomaly detection;
- concurrent-clone detection;
- optional attestation extensions.

## 73.3 External model egress

An official Harness has no generic model-protocol fallback. This prevents accidental or simple configuration-based redirection to generic OpenAI/Anthropic endpoints.

A malicious user can write another HTTPS program unless endpoint/network controls prevent it. Strong enterprise "no external model" policy requires operating-system/network egress controls in addition to PAPER.

## 73.4 Malicious model

A malicious or prompt-injected model remains unable to expand authority because tool/file/network authorization is checked outside the model.

## 73.5 Malicious Relay

A Relay sees plaintext in P0 and is security-critical. P1 decomposition can reduce plaintext exposure. Evidence receipts and end-to-end content digests make some Relay modifications detectable, but a compromised Relay authorized to inspect/transform content can still misuse that authority.

High-assurance deployments SHOULD separate duties and keys.

## 73.6 Malicious PIA/host

Software-only PMP measurement is not remote attestation. A fully compromised PIA host can lie about local state unless a stronger attestation mechanism is configured.

## 73.7 Traffic analysis

TLS/QUIC hides payload content, not necessarily peer IPs, timing, sizes, or connection patterns. P2/P3 do not eliminate metadata leakage.

## 73.8 Clock

Wall-clock time is useful for records but is not a sole security ordering primitive. PAPER relies on nonces, sequence numbers, policy/lease validity, causal hashes, and signed receipts.

---

# 74. Privacy Considerations

PAPER can expose unusually rich enterprise activity data. Implementers must avoid assuming that administrator role implies unlimited content access.

Privacy-sensitive design points:

- separate operational from content permissions;
- audit content viewing;
- minimize metadata in sealed modes;
- explicit retention;
- deletion/hold semantics;
- employee notice through effective-policy UI;
- avoid using token count, presence, message volume, or files touched as sole performance metrics.

The protocol provides evidence. Organizational use of that evidence remains subject to policy and applicable law.

---

# 75. Interoperability with Existing Standards

PAPER may coexist with:

- MCP inside a Harness for tool/context interoperability;
- A2A at an agent-to-agent boundary;
- SPIFFE/SPIRE for workload identity in data centers;
- in-toto/SLSA for build/release provenance;
- SCITT for external signed-statement transparency;
- OpenTelemetry for operations telemetry.

These systems solve adjacent problems. PAPER does not require reimplementation of them.

Example:

```text
PAPER Harness session
  ├─ model inference via PAPER
  ├─ tool may internally speak MCP
  ├─ PIA workload may receive SPIFFE identity
  ├─ commit provenance exported to in-toto/SLSA
  └─ PAPER Evidence Receipt optionally registered in SCITT
```

---

# 76. Initial Message Registry

## 76.1 Core / authentication

| ID | Name | Profiles |
|---:|---|---|
| `0x0001` | `HELLO` | any initiator |
| `0x0002` | `HELLO_ACK` | RELAY |
| `0x0003` | `CAPABILITIES` | any |
| `0x0004` | `PING` | any |
| `0x0005` | `PONG` | any |
| `0x0006` | `GOAWAY` | any |
| `0x0101` | `AUTH_CHALLENGE` | RELAY |
| `0x0102` | `AUTH_PROOF` | any |
| `0x0103` | `AUTH_RESULT` | RELAY |
| `0x0104` | `USER_BIND` | HARNESS |
| `0x0105` | `USER_BIND_ACK` | RELAY |
| `0x0106` | `CREDENTIAL_STATUS` | RELAY |

## 76.2 Session / lease

| ID | Name |
|---:|---|
| `0x0201` | `SESSION_OPEN` |
| `0x0202` | `SESSION_GRANT` |
| `0x0203` | `SESSION_STATUS` |
| `0x0204` | `SESSION_RESUME` |
| `0x0205` | `SESSION_CLOSE` |
| `0x0210` | `LEASE_GRANT` |
| `0x0211` | `LEASE_RENEW` |
| `0x0212` | `LEASE_REVOKE` |
| `0x0213` | `POLICY_EPOCH_NOTICE` |

## 76.3 Governance

| ID | Name |
|---:|---|
| `0x0301` | `EXCHANGE_OPEN` |
| `0x0302` | `STREAM_OPEN` |
| `0x0303` | `RELAY_VERDICT` |
| `0x0304` | `APPROVAL_REQUEST` |
| `0x0305` | `APPROVAL_RESULT` |
| `0x0306` | `EXCHANGE_CANCEL` |
| `0x0307` | `EXCHANGE_COMPLETE` |
| `0x0308` | `SECURITY_FINDING` |

## 76.4 AI

| ID | Name |
|---:|---|
| `0x0401` | `AI_OPEN` |
| `0x0402` | `INFERENCE_REQUEST` |
| `0x0403` | `AI_INPUT_CHUNK` |
| `0x0404` | `AI_TOKEN_CHUNK` |
| `0x0405` | `AI_USAGE` |
| `0x0406` | `AI_COMPLETE` |
| `0x0407` | `AI_CANCEL` |
| `0x0410` | `MODEL_READY` |
| `0x0411` | `MODEL_STATUS` |
| `0x0412` | `ENDPOINT_LEASE_STATUS` |

## 76.5 Context

| ID | Name |
|---:|---|
| `0x0501` | `CONTEXT_MANIFEST` |
| `0x0502` | `CONTEXT_ITEM` |
| `0x0503` | `CONTEXT_DECISION` |
| `0x0504` | `CONTEXT_CHUNK` |
| `0x0505` | `CONTEXT_ATTACH` |

## 76.6 Tools/runtime

| ID | Name |
|---:|---|
| `0x0601` | `TOOL_PROPOSE` |
| `0x0602` | `TOOL_DECISION` |
| `0x0603` | `RUNTIME_EXECUTE` |
| `0x0604` | `RUNTIME_OUTPUT` |
| `0x0605` | `RUNTIME_RESULT` |
| `0x0606` | `RUNTIME_CANCEL` |

## 76.7 Provenance/evidence

| ID | Name |
|---:|---|
| `0x0701` | `PROVENANCE_NODE` |
| `0x0702` | `ARTIFACT_BIND` |
| `0x0703` | `CODE_SPAN_BIND` |
| `0x0704` | `COMMIT_BIND` |
| `0x0705` | `EVIDENCE_CHECKPOINT` |
| `0x0706` | `EVIDENCE_RECEIPT` |

## 76.8 Chat/presence

| ID | Name |
|---:|---|
| `0x0801` | `CHAT_MESSAGE` |
| `0x0802` | `CHAT_EDIT` |
| `0x0803` | `CHAT_DELETE` |
| `0x0804` | `CHAT_ACK` |
| `0x0810` | `PRESENCE_UPDATE` |
| `0x0811` | `PRESENCE_SNAPSHOT` |

## 76.9 Voice

| ID | Name |
|---:|---|
| `0x0901` | `VOICE_OPEN` |
| `0x0902` | `VOICE_CHUNK` |
| `0x0903` | `VOICE_COMMIT` |
| `0x0904` | `VOICE_ACK` |

## 76.10 Files

| ID | Name |
|---:|---|
| `0x0A01` | `FILE_OFFER` |
| `0x0A02` | `FILE_DECISION` |
| `0x0A03` | `FILE_CHUNK` |
| `0x0A04` | `FILE_ACK` |
| `0x0A05` | `FILE_COMMIT` |
| `0x0A06` | `FILE_RESULT` |

## 76.11 Broadcast/admin

| ID | Name |
|---:|---|
| `0x0B01` | `BROADCAST` |
| `0x0B02` | `BROADCAST_ACK` |
| `0x0B10` | `ADMIN_DIRECTIVE` |
| `0x0B11` | `ADMIN_DIRECTIVE_RESULT` |
| `0x0B12` | `POLICY_REFRESH_REQUIRED` |

## 76.12 Telemetry

| ID | Name |
|---:|---|
| `0x0C01` | `TELEMETRY_BATCH` |
| `0x0C02` | `USAGE_EVENT` |
| `0x0C03` | `HEALTH_STATUS` |
| `0x0C04` | `CAPACITY_STATUS` |

---

# 77. Conceptual CDDL — Peer Credential

The exact CDDL repository file is normative when published. The following representation documents the v1 model:

```cddl
paper-peer-credential = {
  1: uint,                    ; credential_version
  2: tstr,                    ; issuer
  3: tstr,                    ; peer_id
  4: tstr,                    ; organization_id
  5: uint,                    ; peer_profile
  6: bstr,                    ; public_key_thumbprint
  7: uint,                    ; not_before_ms
  8: uint,                    ; not_after_ms
  9: bstr,                    ; serial
  10: [* uint],               ; allowed_core_majors
  ? 11: tstr,                 ; build_channel
  ? 12: tstr,                 ; deployment_zone
  ? 13: { * int => any }      ; non-critical extensions
}
```

The object is carried as a COSE signed object.

---

# 78. Conceptual CDDL — Capability Lease

```cddl
capability-lease = {
  1: bstr .size 16,           ; lease_id
  2: tstr,                    ; issuer
  3: tstr,                    ; harness_peer_id
  4: tstr,                    ; user_id
  5: tstr,                    ; organization_id
  6: bstr .size 16,           ; session_id
  7: uint,                    ; not_before_ms
  8: uint,                    ; expires_ms
  9: any,                     ; policy_epoch
  10: [* tstr],               ; extensions
  ? 11: tstr,                 ; project
  ? 12: tstr,                 ; repository
  ? 13: tstr,                 ; branch
  ? 14: [* tstr],             ; model package ids/classes
  ? 15: [* tstr],             ; allowed tool classes
  ? 16: { * tstr => any },    ; file scopes
  ? 17: { * tstr => any },    ; network scopes
  ? 18: { * tstr => uint },   ; resource budgets
  19: uint,                   ; protection profile
  ? 20: [* any],              ; approval requirements
  ? 21: { * int => any }      ; extensions
}
```

---

# 79. Conceptual CDDL — Provenance Node

```cddl
provenance-node = {
  1: uint,                    ; node_type
  2: bstr .size 16,           ; exchange_id
  3: bstr .size 16,           ; session_id
  4: tstr,                    ; actor_id
  ? 5: any,                   ; policy_epoch
  6: [* bstr],                ; causal_parent_digests
  ? 7: [* any],               ; object refs
  ? 8: [* any],               ; artifact refs
  9: uint,                    ; result/status
  ? 10: tstr,                 ; summary
  ? 11: { * int => any }      ; extensions
}
```

Its digest is computed from the deterministic encoding without an embedded digest field.

---

# 80. Representative Flow — Coding Inference

```text
Harness              Relay / CP                PIA                 Model
  │                      │                       │                    │
  │── HELLO/AUTH ───────►│                       │                    │
  │── USER_BIND ────────►│                       │                    │
  │── SESSION_OPEN ─────►│                       │                    │
  │◄─ SESSION_GRANT ─────│                       │                    │
  │                      │                       │                    │
  │── EXCHANGE_OPEN ────►│                       │                    │
  │── CONTEXT_MANIFEST ─►│                       │                    │
  │                      │ policy/DLP            │                    │
  │◄─ CONTEXT_DECISION ──│                       │                    │
  │── AI_OPEN ──────────►│                       │                    │
  │                      │ resolve endpoint      │                    │
  │                      │── INFERENCE_REQUEST ─►│                    │
  │                      │                       │── local invoke ───►│
  │                      │                       │◄─ token stream ────│
  │◄════ AI_TOKEN_CHUNK ═╪═══════════════════════│                    │
  │                      │                       │                    │
  │                      │◄── AI_COMPLETE ───────│                    │
  │◄─ AI_COMPLETE ───────│                       │                    │
  │◄─ EVIDENCE_RECEIPT ──│                       │                    │
```

Every context disclosure, Relay decision, model invocation, and completion is represented in the Provenance Spine.

---

# 81. Representative Flow — Model Tool Proposal

```text
PIA -> Relay: TOOL_PROPOSE("run tests")
Relay:
  validate Capability Lease
  normalize command intent
  evaluate Command/File/Network policy
  generate Relay Verdict

if approval required:
  Relay -> Harness: APPROVAL_REQUEST
  Harness -> Relay: APPROVAL_RESULT

Relay -> Runtime: RUNTIME_EXECUTE(scoped action)
Runtime -> Relay: RUNTIME_RESULT
Relay -> PIA: TOOL_RESULT
```

The PIA/model never receives a reusable credential that bypasses Relay/Runtime policy.

---

# 82. Representative Flow — Chat Attachment to AI Context

```text
User A -> PAPER Chat -> User B
            │
            └─ FILE_TRANSFER attachment

Later User B chooses "Use in AI context"

Harness -> Relay: CONTEXT_ATTACH(chat/file refs)
Relay:
  re-check current user authorization
  re-check file classification
  scan/redact according to current policy
  create provenance edge CHAT_MESSAGE/FILE_TRANSFER -> CONTEXT_DISCLOSURE

Only then is the content sent toward PIA.
```

This separation prevents collaboration channels from becoming implicit model-input bypasses.

---

# 83. Representative Flow — Emergency Model Recall

```text
Admin -> CP: recall PMP X
CP:
  suspend PMP X
  revoke endpoint leases
  increment/issue policy epoch
  sign ADMIN_DIRECTIVE

CP -> Relays: signed recall state
Relays:
  stop new AI_OPEN for X
  cancel/drain in-flight sessions per policy
  emit evidence

CP/Relay -> Harnesses: BROADCAST(CRITICAL)
CP/Relay -> PIAs: ADMIN_DIRECTIVE(DRAIN_MODEL)
```

The broadcast communicates; the directive enforces.

---

# 84. Representative Flow — QUIC Failure and TCP Resume

```text
Harness -- QUIC PAPER --> Relay
       network path blocks UDP
       connection lost

Harness -- TLS/TCP PAPER --> Relay
       HELLO/AUTH again
       SESSION_RESUME(session_id, checkpoints)
Relay:
       validates user/Harness
       validates policy epoch
       renews lease if needed
       reattaches resumable lanes
```

No HTTP or generic model-protocol downgrade occurs.

---

# 85. IANA Considerations

A future standards submission should request, where appropriate:

- ALPN protocol identifier for PAPER;
- media types for exported PAPER evidence objects if required;
- registries or registry policies for protocol extension identifiers if moved under standards governance.

Until then, the open-source project maintains provisional registries.

This document does not claim an existing IANA allocation for `paper/1`.

---

# 86. Standards Baseline

PAPER intentionally builds on established standards rather than replacing them.

At publication time, the implementation baseline should consult the current versions and errata for:

- TLS 1.3 — RFC 9846;
- QUIC Transport — RFC 9000;
- Using TLS to Secure QUIC — RFC 9001;
- ALPN — RFC 7301;
- TLS 1.3 channel bindings — RFC 9266;
- CBOR — RFC 8949 / STD 94;
- COSE structures and algorithms — RFC 9052 / RFC 9053 / STD 96;
- HPKE — RFC 9180;
- MLS — RFC 9420;
- COSE Receipts — RFC 9942;
- SCITT architecture — RFC 9943;
- Hybrid TLS key exchange framework — RFC 9954;
- ML-DSA for COSE/JOSE — RFC 9964 where a post-quantum signature profile is enabled.

Standards references define cryptographic/encoding primitives; PAPER defines the application-level authority and provenance semantics.

---

# 87. Definition of Protocol Completion

PAPER v1 is implementation-ready when all of the following are true:

1. Rust and Go implementations establish interoperable QUIC and TLS/TCP connections.
2. Authentication proofs cannot be replayed across connections.
3. Unregistered peers cannot open application exchanges.
4. Profile violations are rejected deterministically.
5. Capability Lease and Policy Epoch enforcement passes conformance tests.
6. Harness can perform a governed inference against PIA without exposing a generic model endpoint.
7. A fake model name with a non-approved package digest is rejected.
8. Model recall stops new routing.
9. Tool proposals cannot expand model authority.
10. Chat, voice, files, presence, and broadcasts use independent governed lane classes.
11. Chat/file content cannot enter AI context without a separate Context Exchange.
12. QUIC/TCP fallback preserves the same security semantics.
13. Interrupted side-effecting exchanges do not duplicate execution.
14. Provenance Spine and Evidence Receipt verify independently.
15. A representative code change can be traced from user intent through model/context/tool actions to commit binding.
16. Government Sovereign deployment functions with no public Internet dependency.
17. Conformance/fuzz suites are public and reproducible.
18. Security limitations around modified clients, malicious hosts, Relay visibility, and external egress are documented without overclaiming.

---

# Appendix A. Error Code Families

Recommended initial numeric families:

```text
0x0000 Core
0x1000 Authentication
0x2000 Session/Lease
0x3000 Governance
0x4000 AI/Model
0x5000 Context
0x6000 Tool/Runtime
0x7000 Provenance/Evidence
0x8000 Collaboration
0x9000 File/Voice
0xA000 Admin/Broadcast
0xB000 Quota/Resource
```

Representative codes:

| Code | Symbol |
|---:|---|
| `0x1001` | `AUTH_INVALID_CREDENTIAL` |
| `0x1002` | `AUTH_REVOKED_CREDENTIAL` |
| `0x1003` | `AUTH_CHANNEL_BINDING_FAILED` |
| `0x1004` | `AUTH_PROFILE_MISMATCH` |
| `0x2001` | `LEASE_EXPIRED` |
| `0x2002` | `LEASE_SCOPE_VIOLATION` |
| `0x2003` | `POLICY_EPOCH_STALE` |
| `0x3001` | `POLICY_DENY` |
| `0x3002` | `APPROVAL_REQUIRED` |
| `0x4001` | `MODEL_NOT_APPROVED` |
| `0x4002` | `ENDPOINT_LEASE_INVALID` |
| `0x4003` | `MODEL_RECALLED` |
| `0x5001` | `CONTEXT_NOT_AUTHORIZED` |
| `0x6001` | `TOOL_NOT_AUTHORIZED` |
| `0x7001` | `PROVENANCE_REQUIRED` |
| `0x8001` | `CONVERSATION_NOT_AUTHORIZED` |
| `0x9001` | `TRANSFER_DIGEST_MISMATCH` |
| `0xA001` | `ADMIN_AUTHORITY_REQUIRED` |
| `0xB001` | `QUOTA_EXCEEDED` |

---

# Appendix B. Security Profile Summary

| Requirement | Public | Enterprise Managed | Enterprise Restricted | Government Sovereign |
|---|---|---|---|---|
| QUIC | Preferred | Preferred | Configurable/required | Configurable |
| TLS/TCP fallback | Required | Required | Policy | Policy |
| Harness enrollment | Patty account | Org SSO | Org SSO/admin | Local/offline |
| Relay content inspection | Typical | Typical | Required for selected classes | Local policy |
| P2 endpoint sealing | Optional | Optional | Restricted | Policy |
| P3 group E2EE | Optional | Policy | Often disabled | Policy |
| Local CP/Relay | No | Optional | Common | Required |
| Local PIA/GPU | Patty cloud | Optional | Common | Required |
| External telemetry | Product policy | Org policy | Minimized | Optional/none |
| Offline evidence verify | Supported | Supported | Required | Required |
| Crypto restrictions | Baseline | Org | Org/security | Deployment/KCMVP-aware |

---

# Appendix C. Implementation Guidance for vLLM/SGLang

PAPER does not require a permanent fork of a serving engine.

Preferred integration:

```text
Relay <--PAPER--> PIA <--local adapter--> serving engine
```

PIA owns:

- PAPER identity;
- PMP verification;
- endpoint lease;
- request authorization;
- mapping into serving engine;
- normalized token/tool output;
- inference telemetry.

The serving engine owns:

- model loading;
- batching;
- KV cache;
- token generation;
- GPU execution.

The adapter can evolve independently. A serving-engine plugin may improve integration but is not the root trust boundary.

---

# Appendix D. Open-Source Repository Layout

A recommended project layout:

```text
paper-protocol/
├── spec/
│   ├── core.md
│   ├── transport-quic.md
│   ├── transport-tcp.md
│   ├── identity.md
│   ├── governance.md
│   ├── provenance.md
│   ├── security.md
│   └── extensions/
├── registry/
│   ├── messages.csv
│   ├── fields.csv
│   ├── errors.csv
│   ├── profiles.csv
│   └── crypto.csv
├── schema/
│   └── cddl/
├── crates/
│   ├── paper-core/
│   ├── paper-quic/
│   ├── paper-tcp/
│   ├── paper-cose/
│   ├── paper-provenance/
│   └── paper-conformance/
├── go/
├── reference/
│   ├── relay/
│   ├── pia/
│   └── harness-client/
├── adapters/
│   ├── vllm/
│   └── sglang/
├── fuzz/
├── vectors/
└── docs/
```

---

# Appendix E. Protocol Invariants

The following invariants are useful implementation and formal-analysis targets:

1. **No protected action without authenticated peer.**
2. **No protected action outside a valid Capability Lease.**
3. **No protected action evaluated under an unknown Policy Epoch.**
4. **No model invocation against an unapproved PMP/Endpoint Lease.**
5. **No tool proposal grants authority by itself.**
6. **No collaboration payload becomes model context implicitly.**
7. **No transport fallback changes authorization semantics.**
8. **No completed side effect is automatically duplicated after reconnect.**
9. **Every protected exchange terminates with verifiable evidence or explicit evidence failure.**
10. **Every provenance node digest changes if any canonicalized causal content changes.**
11. **A peer profile cannot emit privileged messages assigned to another profile.**
12. **Administrative communication and administrative enforcement are separate message classes.**

These invariants should be expressed in executable conformance scenarios and, for critical subsets, model-checked state machines where practical.

---

# Appendix F. Normative DARI v1 Contract (Phase 2 Freeze)

This appendix freezes the Phase 2 wire and semantic contract for **DARI — Delegated Authorization and Receipts for Inference**. It is normative for `dari/1` and every `dari.*` profile. Earlier PAPER sections remain the bounded `paper/1` legacy specification. Where an earlier PAPER rule conflicts with this appendix for a negotiated DARI profile, this appendix takes precedence. This appendix does not assert that the new behavior is implemented or measured.

## F.1 Scope, roles, and conformance

DARI is an application- and vendor-neutral kernel. Its protocol roles are:

- **Governance Relay** — authenticates peers, evaluates authority and policy, and assembles evidence.
- **Inference Peer** — performs an authorized inference operation and attests only what it observed.
- **Effect Executor** — performs a transactional external effect under an authorization.
- **Evidence Verifier** — validates credentials, grants, decisions, state, receipts, attestations, and disclosure proofs without trusting the producer.

A deployment MAY combine roles, but it MUST apply the validation and attestation rules independently for each role. Possession of one role MUST NOT imply another role or any authority not present in an Authorization Grant.

The five protected kernel objects are **Peer Credential**, **Authorization Grant**, **Governed Exchange**, **Authorization Decision**, and **Evidence Receipt**. Signed State Checkpoints, Receipt Attestations, selective-disclosure proofs, and transactional effect objects support those kernel objects. The word “Credential” in DARI prose is shorthand for Peer Credential; it does not define a second credential type.

An implementation claiming `dari/1` conformance MUST implement Sections F.2 through F.9, F.11, the kernel portions of F.12, and the applicable negative cases in F.14. Section F.10 and its message allocations are mandatory only when `dari.tools/1` is negotiated. An extension-profile implementation MUST also satisfy the dependency and runtime rules in F.13. Schema recognition alone is not runtime conformance.

## F.2 Encoding, hashing, COSE, and extension rules

All DARI structured values MUST use deterministic CBOR as defined by RFC 8949 Core Deterministic Encoding. Maps MUST have unique keys, indefinite-length items and floating-point numbers MUST NOT appear in a DARI object, and text MUST be valid UTF-8. A receiver MUST parse with finite depth, item-count, and byte limits before allocating application resources. After parsing, the receiver MUST deterministically re-encode the item and compare the result byte-for-byte with the received encoding. A mismatch is `NON_CANONICAL`.

The following common CDDL is used by the remainder of this appendix:

```cddl
dari-version = 1
uint16 = 0..65535
uint8 = 0..255
uint32 = 0..4294967295
uint64 = 0..18446744073709551615
time-ms = -9223372036854775808..9223372036854775807
digest32 = bstr .size 32
nonce32 = bstr .size 32
identifier = tstr .size (1..255)
uri = tstr .size (1..2048)
critical-fields = [0*32 uint]
extensions = { * (256..65535) => any }

; RFC 9052 COSE_Sign1 array. A CBOR tag is forbidden in dari/1.
cose-sign1 = [
  protected : bstr,
  unprotected : {},
  payload : bstr,
  signature : bstr
]
```

Every DARI schema uses integer labels. Label `254`, when present, lists extension-map labels that are critical. Label `255`, when present, contains the extension map. Every label named in `254` MUST occur in `255`. A receiver MUST reject an unknown critical label with `UNSUPPORTED_CRITICAL_FIELD`; it MAY ignore an unknown non-critical extension after retaining its encoded bytes for signature and digest verification. Unknown top-level labels are forbidden.

The baseline content hash is SHA-256. The DARI object digest is:

```text
object_digest(T, body) =
  SHA-256("DARI-OBJ-v1\0" || uint16_be(T) || deterministic_cbor(body))
```

The type value `T` comes from F.12. The encoded body MUST omit any field that would contain its own digest. A digest of a signed envelope is explicitly named `signed_object_digest` and is:

```text
signed_object_digest(T, cose) =
  SHA-256("DARI-SIGNED-OBJ-v1\0" || uint16_be(T) || deterministic_cbor(cose))
```

A DARI signature uses an attached-payload, untagged RFC 9052 `COSE_Sign1`. The protected header bytes MUST be the deterministic encoding of a map containing `alg` (label `1`) and `kid` (label `4`); both MUST be protected. The unprotected map MUST be empty. The payload MUST equal the deterministic encoding of the parsed body. The signing input is:

```text
Sig_structure = deterministic_cbor([
  "Signature1",
  protected,
  external_aad,
  payload
])
```

`external_aad` is the exact ASCII byte string specified for each object below, including its terminal NUL byte. A verifier MUST verify the signature over this `Sig_structure`, parse the attached payload, deterministically re-encode the parsed body, and require exact byte equality with the attached payload. Verifying the COSE signature without establishing this body-to-payload equality is non-conforming.

The baseline signature algorithm is Ed25519 (`alg = -8`). Another algorithm MAY be negotiated by a future profile, but a receiver MUST NOT substitute an unnegotiated algorithm. A `kid` identifies a key; it is not a trust decision. The verifier MUST resolve the key through a valid Peer Credential or configured trust anchor and then perform the object-specific authorization checks.

`paper/1` preserves its frozen legacy signing and digest bytes, including any documented compatibility quirk. A receiver MUST NOT apply `paper/1` map-form COSE or legacy object-digest bytes to a `dari/1` object, and MUST NOT silently rewrite one form into the other.

## F.3 Peer Credential and proof-of-possession linkage

```cddl
peer-credential-body = {
  1 => dari-version,                 ; version
  2 => identifier,                   ; issuer
  3 => identifier,                   ; subject peer ID
  4 => identifier,                   ; trust domain / organization
  5 => identifier,                   ; authorized peer profile
  6 => bstr,                         ; deterministic CBOR COSE_Key
  7 => time-ms,                      ; not before
  8 => time-ms,                      ; not after
  9 => identifier,                   ; serial
  10 => identifier,                  ; revocation authority
  11 => [1*16 uint16],               ; protocol versions
  ? 12 => identifier,                ; build channel
  ? 13 => identifier,                ; deployment zone
  ? 254 => critical-fields,
  ? 255 => extensions
}

peer-credential = cose-sign1
```

The Peer Credential external AAD is `DARI-PEER-CREDENTIAL-v1\0`. Its COSE payload is exactly `deterministic_cbor(peer-credential-body)`. The issuer MUST be authorized by the configured trust domain to issue the stated peer profile. `not_after` MUST be greater than `not_before`; the credential is valid only at times `not_before <= now < not_after`, subject to the negotiated clock-skew bound.

The subject-key thumbprint used by grants and handshakes is:

```text
subject_key_thumbprint =
  SHA-256("DARI-SUBJECT-KEY-v1\0" || deterministic_cbor(subject COSE_Key))
```

During authentication, the subject MUST prove possession of the private key corresponding to label `6` by signing the transport transcript, both nonces, the challenge identifier, the negotiated profile set, the channel binding, and the Peer Credential signed-object digest. The authentication proof MUST bind all of those values in one signed structure. A credential is not authenticated merely because its issuer signature verifies.

```cddl
auth-proof-body = {
  1 => dari-version,
  2 => digest32,                     ; transcript hash
  3 => nonce32,                      ; client nonce
  4 => nonce32,                      ; server nonce
  5 => identifier,                   ; challenge ID
  6 => [1*16 identifier],            ; negotiated profile set
  7 => bstr .size (1..255),          ; channel binding
  8 => digest32,                     ; Peer Credential signed-object digest
  9 => identifier                    ; subject peer ID
}

auth-proof = cose-sign1
```

Let `hello` and `hello_ack` be the exact canonical DARI payload bytes accepted by each peer, excluding record headers. The transcript hash is `SHA-256("DARI-TRANSCRIPT-v1\0" || uint32_be(len(hello)) || hello || uint32_be(len(hello_ack)) || hello_ack)`. The negotiated profile set MUST be sorted by encoded byte order with no duplicates. The `auth-proof` external AAD is `DARI-AUTH-PROOF-v1\0`; it MUST be signed by the subject key, and its protected `kid` MUST equal the subject-key thumbprint. The explicit nonce, profile, challenge, and channel-binding fields MUST equal the values authenticated by the transcript and connection. A verification API MUST receive this complete negotiated authentication context; an opaque transcript parameter is conforming only if it contains every value above without permitting caller substitution.

A verifier MUST, in order: validate canonical encodings and critical fields; validate protected headers and require body/payload equality; resolve and validate the issuer authority and trust domain; validate the issuer signature; validate time; validate current revocation state from a fresh Signed State Checkpoint; validate the negotiated profile against label `5` and `11`; validate the transcript signature and subject-key thumbprint; and only then bind the authenticated peer ID to the connection. Failure at any step is `AUTHENTICATION_FAILED`; an implementation MAY expose a more specific code only to an authorized diagnostic principal.

## F.4 Authorization Grant and attenuation

```cddl
path-scope = {
  1 => identifier,                   ; resource authority/repository
  2 => identifier,                   ; revision/branch namespace
  3 => tstr,                         ; normalized slash-delimited prefix
  4 => [1*16 identifier]             ; permitted operations
}

network-scope = {
  1 => identifier,                   ; scheme
  2 => identifier,                   ; exact DNS name or address
  3 => uint16,                       ; first port
  4 => uint16,                       ; last port
  5 => [1*16 identifier]             ; permitted purposes
}

authorization-scope = {
  1 => [0*64 identifier],            ; action classes
  2 => [0*64 identifier],            ; model identifiers
  3 => [0*128 path-scope],            ; readable context
  4 => [0*128 path-scope],            ; writable context
  5 => [0*64 identifier],            ; tools/effect kinds
  6 => [0*64 network-scope],         ; network destinations
  7 => [0*64 identifier],            ; data classifications
  8 => { * identifier => uint64 },   ; resource-budget maxima
  9 => [0*16 identifier],            ; allowed protection profiles
  10 => [0*32 identifier]            ; required approval classes
}

authorization-grant-body = {
  1 => dari-version,                 ; version
  2 => identifier,                   ; grant ID
  3 => identifier,                   ; issuer peer ID
  4 => identifier,                   ; subject peer ID
  5 => digest32,                     ; subject-key thumbprint
  6 => [1*16 identifier],            ; audience
  7 => identifier,                   ; organization/trust domain
  8 => identifier,                   ; user ID
  9 => identifier,                   ; session ID
  10 => identifier,                  ; policy epoch ID
  11 => authorization-scope,
  12 => time-ms,                     ; not before
  13 => time-ms,                     ; not after
  14 => uint64,                      ; issuer sequence
  ? 15 => digest32,                  ; parent signed-object digest
  ? 16 => uint8,                     ; remaining delegation depth
  ? 254 => critical-fields,
  ? 255 => extensions
}

authorization-grant = cose-sign1
```

The Authorization Grant external AAD is `DARI-AUTHORIZATION-GRANT-v1\0`. The grant digest used by a child, decision, exchange, or effect is `signed_object_digest(0x0202, authorization-grant)`.

All scope arrays are mathematical sets encoded as arrays sorted by the deterministic encoding of each element, with no duplicates. An empty permission set grants no permission; omission is not a wildcard. Path prefixes MUST be normalized UTF-8 segments, MUST NOT contain `.` or `..` segments, and MUST use `/` as the separator. A child path rule is covered only when authority and revision equal the parent, every child operation occurs in the parent operation set, and the child prefix equals the parent prefix or begins with the parent prefix followed by `/`. Network hosts are exact values; suffix wildcards are not defined. A child network rule is covered only when scheme and host equal the parent, its port interval is contained by the parent interval, and its purpose set is a subset.

A root grant has no label `15`. Omitted label `16` means zero remaining delegation depth. A delegated grant MUST contain label `15`. The complete attenuation algorithm is:

1. Resolve the parent by label `15` and require the digest to equal the parent Authorization Grant signed-object digest. Reject a missing parent, a repeated digest, or a chain longer than 32 with `INVALID_GRANT_CHAIN`.
2. Validate every grant in root-to-leaf order: canonical CBOR, critical fields, COSE headers, body/payload equality, signature, issuer credential, issuer proof-of-possession binding, time, revocation, and issuer-sequence replay state. The validator MUST retain each original signed envelope, its signed-object digest, and its validated signer context; body-only values are insufficient.
3. For each parent/child pair, require the child issuer peer ID to equal the parent subject peer ID and require the child signing-key thumbprint to equal the parent subject-key thumbprint. The child subject peer ID and subject-key thumbprint identify the delegate and MAY differ from the parent subject.
4. Require organization, user, session, policy epoch, and audience to be byte-for-byte equal. A changed audience is invalid; narrowing an audience requires a new root grant from an authorized issuer.
5. Require `child.not_before >= parent.not_before` and `child.not_after <= parent.not_after`. Require `child.not_after > child.not_before`.
6. Require the parent remaining delegation depth to be present and greater than zero. Require the child remaining depth to equal the parent depth minus one; zero MUST be encoded by omission.
7. For action classes, models, tools, data classifications, and allowed protection profiles, require every child element to occur in the parent set. For every child path and network rule, require coverage as defined above.
8. For each child resource-budget entry, require the same parent key and a value less than or equal to the parent maximum. A child MUST NOT introduce a budget key. Budget consumption is cumulative across descendants and MUST be charged against every grant in the chain.
9. Require the child's approval-class set to be a superset of the parent's set. Adding an approval requirement is attenuation; removing one is escalation.
10. Require the root issuer to be authorized for the full root scope by fresh policy state. Validate the leaf subject, subject-key thumbprint, audience, organization, user, session, policy epoch, and requested action against live connection and exchange bindings.

Any failed comparison rejects the entire chain with `AUTHORITY_ESCALATION` and no partial authority. A verifier MUST NOT choose a more permissive ancestor, ignore a malformed descendant, or fall back to a legacy lease. Revocation of any credential or grant in the chain revokes all descendants. A later expiry, added model, wider path, added tool, new network destination, higher budget, weaker protection set, removed approval, changed audience, changed organization/user/session/epoch, incorrect parent digest, skipped depth, or replayed issuer sequence is a required negative conformance case.

Issuer-sequence state is a durable ledger keyed by `(issuer peer ID, organization, session ID)`. It maps every accepted sequence to exactly one signed-object digest and retains a high-water sequence. An already-recorded sequence is accepted only for the identical digest; an unseen sequence below or equal to the high-water mark is `GRANT_REPLAY`; a higher sequence is recorded with an atomic compare-and-set. Historical parent envelopes already present in the ledger remain valid subject to time, revocation, and budget state. Before a protected action, budget reservation MUST atomically debit the requested amount from the leaf and every ancestor ledger entry or debit none of them. Concurrent descendants MUST NOT each observe and consume the same remaining budget.

## F.5 Governed Exchange

```cddl
governed-exchange-body = {
  1 => dari-version,
  2 => identifier,                   ; exchange ID
  3 => identifier,                   ; session ID
  4 => identifier,                   ; exchange class
  5 => identifier,                   ; initiating peer ID
  6 => digest32,                     ; leaf Authorization Grant digest
  7 => digest32,                     ; policy checkpoint digest
  8 => [1*16 identifier],            ; required profiles
  9 => digest32,                     ; canonical request/action digest
  10 => time-ms,                     ; opened at
  ? 11 => [1*16 identifier],         ; causal parent exchange IDs
  ? 12 => identifier,                ; declared purpose
  ? 254 => critical-fields,
  ? 255 => extensions
}
```

The Governed Exchange digest is `object_digest(0x0303, governed-exchange-body)`. A Governance Relay MUST create it only after authentication, state, grant, audience, and session validation succeeds. The exchange binds all subsequent decisions, evidence events, inference operations, and effects through its exchange ID and digest. Reuse of an exchange ID with any different body is `REPLAY_CONFLICT`.

An exchange state is `OPEN`, `AUTHORIZED`, `RUNNING`, `COMPLETED`, `DENIED`, `ABORTED`, or `EVIDENCE_FAILURE`. A terminal state MUST NOT transition. `COMPLETED` is permitted only after every required obligation is satisfied, every transactional effect is terminal, all required evidence is durably committed, the Evidence Receipt body is finalized, and every required Receipt Attestation is present and valid. A protected operation MUST NOT be forwarded before the exchange reaches `AUTHORIZED`.

## F.6 Authorization Decision and obligation lifecycle

```cddl
decision-outcome = 1 / 2 / 3          ; ALLOW / DENY / ALLOW_WITH_OBLIGATIONS
obligation-phase = 1 / 2              ; PRE_ACTION / POST_ACTION
obligation-state = 1 / 2 / 3          ; PENDING / SATISFIED / FAILED

obligation = {
  1 => identifier,                   ; obligation ID
  2 => identifier,                   ; kind
  3 => digest32,                     ; canonical parameter digest
  4 => obligation-phase,
  5 => obligation-state,
  6 => identifier,                   ; responsible peer ID
  ? 7 => time-ms,                    ; deadline
  ? 8 => digest32                    ; satisfaction/failure evidence digest
}

authorization-decision-body = {
  1 => dari-version,
  2 => identifier,                   ; decision ID
  3 => identifier,                   ; exchange ID
  4 => digest32,                     ; governed-exchange digest
  5 => digest32,                     ; request/action digest
  6 => digest32,                     ; leaf grant digest
  7 => digest32,                     ; policy checkpoint digest
  8 => identifier,                   ; evaluator peer ID
  9 => decision-outcome,
  10 => [0*64 obligation],
  11 => [0*32 identifier],           ; stable reason codes
  12 => time-ms,                     ; issued at
  13 => time-ms,                     ; expires at
  ? 14 => [1*16 digest32],           ; supporting evidence digests
  ? 254 => critical-fields,
  ? 255 => extensions
}

authorization-decision = cose-sign1
```

The Authorization Decision external AAD is `DARI-AUTHORIZATION-DECISION-v1\0`; its digest is `signed_object_digest(0x0304, authorization-decision)`. `expires_at` MUST be greater than `issued_at`. The evaluator MUST be authorized by the fresh policy checkpoint and trust domain to decide the bound action. A `DENY` decision MUST contain no obligations. An `ALLOW` decision MUST contain no obligations. `ALLOW_WITH_OBLIGATIONS` MUST contain at least one obligation, and every newly issued obligation MUST be `PENDING` unless its evidence digest proves a satisfaction that preceded and is explicitly bound into the decision.

When multiple required decisions apply, the deterministic aggregate is:

1. A missing, invalid, stale, or expired required decision produces `DENY`.
2. Any valid `DENY` overrides every `ALLOW` and `ALLOW_WITH_OBLIGATIONS`.
3. Otherwise, union obligations by obligation ID. Two different encodings for the same ID are `DECISION_CONFLICT` and produce `DENY`.
4. A non-empty union produces `ALLOW_WITH_OBLIGATIONS`; an empty union produces `ALLOW`.

A signed decision is immutable. Every obligation carried by a decision is required; optional advisory notices are evidence events, not obligations. Obligation state is maintained as an append-only state machine keyed by decision digest and obligation ID. The only transitions are `PENDING -> SATISFIED` and `PENDING -> FAILED`; both require canonical evidence from the responsible peer. Terminal obligation states MUST NOT change. An expired deadline changes a pending obligation to `FAILED`. Every `PRE_ACTION` obligation MUST be satisfied before a protected action or effect starts. Every `POST_ACTION` obligation MUST be satisfied before the exchange becomes `COMPLETED`. A failed required obligation denies an unstarted action or aborts/fails an active exchange. An implementation MUST NOT represent a transform, approval, quarantine, or other condition as an unqualified `ALLOW`; it MUST encode the condition as an obligation or return `DENY`.

## F.7 Signed State Checkpoint, freshness, and rollback

```cddl
state-class = 1 / 2 / 3 / 4 / 5 / 255
; 1 revocation, 2 issuer keys, 3 policy epochs,
; 4 model manifests, 5 endpoint authorizations, 255 extension state

freshness-class = 1 / 2              ; INTEGRITY / LOW_RISK_READONLY

signed-state-checkpoint-body = {
  1 => dari-version,
  2 => identifier,                   ; checkpoint ID
  3 => identifier,                   ; issuer
  4 => identifier,                   ; trust domain
  5 => state-class,
  6 => uint64,                       ; strictly increasing sequence
  7 => time-ms,                      ; issued at
  8 => time-ms,                      ; expires at
  9 => uint64,                       ; maximum staleness in milliseconds
  10 => state-content-ref,
  ? 11 => digest32,                  ; previous checkpoint signed-object digest
  ? 12 => [1*16 identifier],         ; audience restriction
  13 => freshness-class,
  ? 254 => critical-fields,
  ? 255 => extensions
}

state-content-kind = 1 / 2           ; SNAPSHOT / DELTA
state-content-ref = {
  1 => identifier,                   ; registered content type
  2 => state-content-kind,
  3 => digest32,                     ; content digest
  ? 4 => uri                         ; authenticated retrieval location
}

signed-state-checkpoint = cose-sign1
```

The Signed State Checkpoint external AAD is `DARI-SIGNED-STATE-CHECKPOINT-v1\0`; its digest is `signed_object_digest(0x0305, signed-state-checkpoint)`. The signer MUST be authorized for the state class and trust domain. `expires_at` MUST be greater than `issued_at`, and `maximum staleness` MUST be nonzero and within the implementation's configured upper bound. The content resolver obtains canonical CBOR bytes by the content digest, optionally using label `4`, and MUST verify `SHA-256("DARI-STATE-CONTENT-v1\0" || deterministic_cbor([content type, content kind, content bytes]))` equals label `3` before using the state. A delta MUST identify its base inside the registered content schema, and that base MUST equal the preceding checkpoint's resolved state digest.

Absence of label `12` means the checkpoint applies to the complete trust domain; when it is present, the authenticated relying peer MUST occur in the audience set. The audience set MUST be sorted by encoded byte order with no duplicates. A verifier MUST keep one durable high-water mark `(sequence, checkpoint digest)` for each `(issuer, trust domain, state class)` stream; an audience restriction does not create another sequence stream. The first accepted checkpoint MUST either be an explicitly provisioned trust baseline or have sequence `0` and no previous digest. For a later checkpoint:

- a lower sequence is `STATE_ROLLBACK`;
- an equal sequence is accepted only when the signed-object digest is identical, as an idempotent replay;
- a higher sequence MUST contain label `11` equal to the current high-water digest.

A fork, missing predecessor, or reset not authorized by an out-of-band trust-baseline operation is `STATE_ROLLBACK`. Sequence/predecessor validation and the high-water update MUST be one durable atomic compare-and-set so concurrent children of one predecessor cannot both succeed. Rollback MUST fail closed for every freshness class. A checkpoint MAY be delivered inline as a signed object in a state-bearing carrier or resolved by its signed-object digest through the configured state service; transport does not change these validation rules and Phase 2 allocates no standalone checkpoint message.

At evaluation time `now`, allowing negotiated clock skew, a checkpoint is fresh only if `issued_at <= now < expires_at` and `now - issued_at <= maximum staleness`. Revocation, issuer-key, policy-epoch, model-manifest, and endpoint-authorization state are integrity state and MUST use `INTEGRITY`; stale or unavailable integrity state MUST fail closed before a protected transition. `LOW_RISK_READONLY` MAY be used only by an extension for an action that cannot disclose protected data, allocate a model, mutate state, consume a delegated budget, or cause an external effect, and only when fresh policy explicitly authorizes degraded evaluation. Such degradation MUST be reported and evidenced; it MUST NOT be inferred from transport failure.

The required validation order is: canonical form and critical fields; COSE headers and body/payload equality; signature and signer authority; trust domain and audience; time bounds; high-water sequence and predecessor; state digest resolution; then the protected object's use of the state. A failure MUST leave the previous high-water mark unchanged.

## F.8 Evidence Receipt and scoped multi-party attestations

```cddl
receipt-final-state = 1 / 2 / 3 / 4
; COMPLETED / DENIED / ABORTED / EVIDENCE_FAILURE

evidence-receipt-body = {
  1 => dari-version,
  2 => identifier,                   ; receipt ID
  3 => identifier,                   ; exchange ID
  4 => digest32,                     ; governed-exchange digest
  5 => identifier,                   ; session ID
  6 => identifier,                   ; organization/trust domain
  7 => receipt-final-state,
  8 => uint64,                       ; first event sequence
  9 => uint64,                       ; last event sequence
  10 => uint64,                      ; event count
  11 => digest32,                    ; linear evidence-chain root
  12 => digest32,                    ; segmented MMR root
  13 => [1*32 digest32],             ; grant digests
  14 => [0*64 digest32],             ; decision digests
  15 => [1*32 digest32],             ; state-checkpoint digests
  16 => [0*64 digest32],             ; effect-result digests
  ? 17 => digest32,                  ; model-manifest digest
  ? 18 => digest32,                  ; endpoint-authorization digest
  ? 19 => digest32,                  ; provenance root
  ? 20 => digest32,                  ; omission-manifest digest
  21 => time-ms,                     ; issued at
  22 => [1*3 receipt-attestation-role], ; required attestation roles
  ? 23 => identifier,                ; retention/export profile
  ? 254 => critical-fields,
  ? 255 => extensions
}

attestation-claim = {
  1 => uint,                         ; registered claim class
  2 => [1*64 digest32],              ; objects actually observed
  ? 3 => uint64,                     ; first observed event sequence
  ? 4 => uint64                      ; last observed event sequence
}

receipt-attestation-role = 1 / 2 / 3
; Governance Relay / Inference Peer / Effect Executor

receipt-attestation-body = {
  1 => dari-version,
  2 => digest32,                     ; Evidence Receipt body digest
  3 => digest32,                     ; signer Peer Credential digest
  4 => receipt-attestation-role,
  5 => [1*16 attestation-claim],
  6 => time-ms,
  ? 254 => critical-fields,
  ? 255 => extensions
}

receipt-attestation = cose-sign1

evidence-receipt = {
  1 => evidence-receipt-body,
  2 => [1*16 receipt-attestation]
}
```

The Evidence Receipt body digest is `object_digest(0x0302, evidence-receipt-body)`. The Receipt Attestation external AAD is `DARI-RECEIPT-ATTESTATION-v1\0`; its digest is `signed_object_digest(0x0703, receipt-attestation)`. Receipt label `22` MUST be a sorted set of valid `receipt-attestation-role` values. The Evidence Receipt is complete only when its body is canonical and every role listed at label `22` has at least one valid, in-scope attestation. The receipt MUST satisfy `last_sequence - first_sequence + 1 = event_count`, without integer overflow, and `event_count` MUST be nonzero.

A Governance Relay MAY attest decisions it evaluated or relayed, routing it performed, state it validated, and the evidence roots it assembled. An Inference Peer MAY attest inference inputs, model/endpoint identity, and inference outputs it directly observed. An Effect Executor MAY attest prepares, authorizations, and results it directly processed. A signer MUST NOT attest another role's observation. If inference occurred, each inference result committed by the receipt MUST be claimed by its producing Inference Peer; if effects occurred, every effect-result digest at label `16` MUST be claimed by the Effect Executor named by that result. The verifier MUST validate the signer's current Peer Credential, role, signature, body-digest binding, event range, and every claimed object against the committed evidence. An out-of-scope claim is `ATTESTATION_SCOPE_VIOLATION` and invalidates that attestation.

The producer MUST derive every receipt field from durably committed protocol objects and events. It MUST NOT populate a digest from requested, expected, configured, randomly generated, or merely logged data. It MUST NOT infer an Inference Peer or Effect Executor attestation from Relay observation. Missing required evidence yields `EVIDENCE_FAILURE`; it MUST NOT be replaced by an empty, placeholder, synthetic, or unsigned receipt. In particular, a receipt MUST NOT claim `COMPLETED` while a required obligation is pending or failed, an effect is non-terminal, a required state checkpoint is missing/stale, an evidence append failed, or a required attestation is absent.

## F.9 Ordered evidence and selective disclosure

For a Governed Exchange with digest `X`, the linear evidence chain is:

```text
R0 = SHA-256("DARI-EVIDENCE-START-v1\0" || X)
Ri = SHA-256(
  "DARI-EVIDENCE-EVENT-v1\0" || uint64_be(sequence_i) ||
  R(i-1) || event_digest_i
)
```

Sequences MUST be contiguous and strictly increasing. The receipt `first event sequence`, `last event sequence`, and `event count` MUST agree with the committed sequence. The linear root proves ordering; the segmented commitment below enables selective disclosure.

The `dari/1` baseline segment size is 1024 events. Another power of two from 16 through 65536 MAY be negotiated before the exchange opens and MUST be bound into its extension map. It MUST NOT change within an exchange.

For zero-based segment index `s` and leaf position `p`, compute:

```text
event_digest = object_digest(event_type, event_body)
leaf = SHA-256(
  "DARI-EVIDENCE-LEAF-v1\0" || uint64_be(sequence) || event_digest
)
empty = SHA-256(
  "DARI-EVIDENCE-EMPTY-v1\0" || uint64_be(s) || uint32_be(p)
)
node = SHA-256("DARI-EVIDENCE-NODE-v1\0" || left || right)
```

The final partial segment MUST be padded with the position-specific `empty` leaves. Folding adjacent pairs left-to-right produces the segment root. Commit each completed or final partial segment as an MMR leaf:

```text
segment_leaf = SHA-256(
  "DARI-EVIDENCE-SEGMENT-v1\0" || uint64_be(s) ||
  uint64_be(first_sequence) || uint32_be(actual_count) || segment_root
)
```

Segment leaves are appended to a binary Merkle Mountain Range by binary carry: equal-height adjacent peaks combine as `SHA-256("DARI-EVIDENCE-MMR-NODE-v1\0" || left || right)`. List the remaining peaks from highest covered range to lowest covered range. Bag them right-to-left with `SHA-256("DARI-EVIDENCE-MMR-BAG-v1\0" || left_peak || accumulator)`. The receipt root is `SHA-256("DARI-EVIDENCE-MMR-ROOT-v1\0" || uint64_be(segment_count) || bagged_peaks)`. For one segment, `bagged_peaks` is its segment leaf. An Evidence Receipt MUST contain at least one event and one segment.

```cddl
proof-step = [0 / 1, digest32]        ; sibling is left / right
mmr-peak = [uint, digest32]           ; height, digest

event-disclosure = {
  1 => uint16,                       ; registered event object type
  2 => bstr,                         ; canonical event body bytes
  3 => uint64,                       ; event sequence
  4 => uint64,                       ; segment index
  5 => uint32,                       ; position in segment
  6 => uint32,                       ; actual count in segment
  7 => [* proof-step],               ; leaf-to-segment-root path
  8 => [* proof-step],               ; segment-leaf-to-peak path
  9 => uint                          ; target peak height
}

omitted-range = {
  1 => uint64,
  2 => uint64,
  3 => identifier,                   ; policy reason, not existence claim
  ? 4 => digest32                    ; commitment to separately held material
}

omission-manifest = [1*256 omitted-range]

selective-disclosure-proof = {
  1 => dari-version,
  2 => digest32,                     ; Evidence Receipt body digest
  3 => uint32,                       ; segment size
  4 => uint64,                       ; segment count
  5 => [1*256 event-disclosure],
  6 => [1*64 mmr-peak],              ; complete canonical peak list
  ? 7 => bstr,                       ; canonical omission-manifest bytes
  ? 254 => critical-fields,
  ? 255 => extensions
}
```

Peak heights and covered ranges are determined uniquely by the binary decomposition of `segment_count`; label `6` MUST contain that exact peak list in left-to-right range order, with strictly descending heights, no duplicate height, and no extra peak. For each disclosure, a verifier MUST: validate canonical event bytes; recompute the event digest and leaf; require the segment index and position implied by the receipt's first sequence; require the exact `log2(segment_size)` segment steps, including deterministic empty padding; reconstruct the segment leaf; require exactly `target peak height` MMR steps and reconstruct its unique MMR peak; replace exactly one matching peak in label `6`; bag all peaks; and compare the resulting root to the receipt. A duplicated sequence, inconsistent path direction, wrong height/count, uncommitted event, extra peak, or altered event MUST fail.

If label `7` is present, its canonical digest MUST equal receipt label `20`; if receipt label `20` is present, label `7` is required when verifying disclosure completeness. Omitted ranges MUST be ordered, non-overlapping, within receipt sequence bounds, and disjoint from disclosed events. An omission manifest states only that a committed range was withheld for the named policy reason. It MUST NOT state or imply that undisclosed content did not exist, was harmless, or was observed by a party that did not attest it. A selective proof proves inclusion and receipt consistency, not the truth of undisclosed content.

## F.10 Transactional effects

```cddl
effect-prepare-body = {
  1 => dari-version,
  2 => identifier,                   ; operation ID
  3 => identifier,                   ; exchange ID
  4 => nonce32,                      ; operation nonce
  5 => digest32,                     ; leaf grant digest
  6 => digest32,                     ; canonical input digest
  7 => identifier,                   ; effect kind/tool ID
  8 => identifier,                   ; executor peer ID
  9 => identifier,                   ; retry-owner peer ID
  10 => time-ms,                     ; expires at
  ? 11 => uri,                       ; opaque input reference
  ? 254 => critical-fields,
  ? 255 => extensions
}

effect-authorization-body = {
  1 => dari-version,
  2 => identifier,                   ; operation ID
  3 => digest32,                     ; Effect Prepare signed-object digest
  4 => digest32,                     ; Authorization Decision digest
  5 => identifier,                   ; authorizing Relay peer ID
  6 => time-ms,                      ; issued at
  7 => time-ms,                      ; expires at
  ? 254 => critical-fields,
  ? 255 => extensions
}

effect-terminal-state = 1 / 2        ; COMMITTED / ABORTED

effect-result-body = {
  1 => dari-version,
  2 => identifier,                   ; operation ID
  3 => digest32,                     ; Effect Prepare digest
  4 => digest32,                     ; Effect Authorization digest
  5 => identifier,                   ; executor peer ID
  6 => effect-terminal-state,
  7 => digest32,                     ; input digest repeated for binding
  ? 8 => digest32,                   ; result digest
  ? 9 => uri,                        ; opaque result reference
  10 => time-ms,                     ; terminal time
  ? 254 => critical-fields,
  ? 255 => extensions
}

effect-state = 1 / 2 / 3 / 4 / 5
; PREPARED / AUTHORIZED / EXECUTING / COMMITTED / ABORTED

effect-status-body = {
  1 => dari-version,
  2 => identifier,                   ; operation ID
  3 => nonce32,                      ; request correlation nonce
  ? 4 => effect-state,
  ? 5 => digest32,                   ; Effect Prepare digest
  ? 6 => digest32,                   ; terminal Effect Result digest
  ? 7 => identifier,                 ; retry-owner peer ID
  ? 254 => critical-fields,
  ? 255 => extensions
}
```

Effect Prepare, Effect Authorization, Effect Result, and a status response each use an attached COSE signature. Their external AAD values are respectively `DARI-EFFECT-PREPARE-v1\0`, `DARI-EFFECT-AUTHORIZATION-v1\0`, `DARI-EFFECT-RESULT-v1\0`, and `DARI-EFFECT-STATUS-v1\0`. Their signed-object types are `0x0610`, `0x0611`, `0x0612`, and `0x0613`. A status request MAY be authenticated by its enclosing DARI connection rather than signed; a status response carrying state or a result MUST be signed by the executor.

The executor MUST persist the operation ID, nonce, prepare digest, grant digest, input digest, executor, retry owner, and state before acknowledging `PREPARED`. It MUST persist the authorization digest before acknowledging `AUTHORIZED`, and MUST atomically persist the terminal state, result digest/reference, and Effect Result before sending `COMMIT` or `ABORT`.

The only effect transitions are:

```text
ABSENT -> PREPARED -> AUTHORIZED -> EXECUTING -> COMMITTED
                                           \-> ABORTED
```

The executor MUST validate the complete Peer Credential, state, grant, decision, and pre-action obligation chain before `AUTHORIZED`, and MUST use an atomic compare-and-set before entering `EXECUTING`. A terminal state MUST NOT transition. Only the named retry owner MAY reconcile an operation left in `PREPARED`, `AUTHORIZED`, or `EXECUTING` after disconnect.

The operation ID is the idempotency key. Repeating an operation ID with the same nonce, prepare digest, grant digest, input digest, executor, and retry owner MUST return the durable current status or terminal result without executing again. Repeating it with any different binding is `REPLAY_CONFLICT`. After an uncertain disconnect, a caller MUST issue `EFFECT_STATUS`; it MUST NOT automatically submit another prepare or execute. An operation stranded in `EXECUTING` MUST remain there until the retry owner obtains authoritative proof from an idempotent/transactional external system or aborts without claiming an unproved outcome. If the external system cannot make the effect and durable result atomic or idempotently reconcilable, the implementation MUST NOT claim exactly-once completion.

An effect's `COMMITTED` result becomes eligible for an Evidence Receipt only after all post-action obligations are satisfied and the result event is durably appended. A failure to append evidence yields `EVIDENCE_FAILURE`; it does not erase or repeat an already committed external effect.

## F.11 Validation order and failure semantics

For every protected inbound transition, a conforming receiver MUST apply this order and stop at the first failure:

1. Validate record framing, declared size, resource bounds, deterministic CBOR, duplicate keys, exact schema, version, and critical fields.
2. Validate that the negotiated profile, message type, sender role, stream contract, and current connection/exchange state permit the object.
3. Validate COSE structure, protected headers, algorithm, `kid`, and attached payload equality; resolve the referenced signer credential without yet granting it authority.
4. Validate the signer's Peer Credential chain, trust domain, time, revocation, transcript proof-of-possession, and connection binding, and obtain the authorized verification key.
5. Validate the object signature with that key and the object-specific external AAD.
6. Validate object identifiers and digest bindings to session, exchange, request/action, subject key, audience, organization, user, and policy epoch.
7. Validate Signed State Checkpoint authority, freshness, high-water sequence, predecessor, and referenced state.
8. Validate the complete Authorization Grant chain, attenuation, replay sequence, scope, budget availability, and requested action.
9. Validate and aggregate Authorization Decisions; enforce pre-action obligations.
10. Validate action-specific model, endpoint, context, inference, or effect constraints.
11. Perform the durable replay/idempotency and state transition.
12. Append the canonical evidence event durably before forwarding protected data, acknowledging a protected transition, or declaring completion.

Later success MUST NOT override an earlier failure. Caches MAY accelerate a step only when their cache key covers every normative input and their validity is bounded by the earliest credential, grant, decision, or checkpoint expiry and by revocation high-water state.

On any failure, the receiver MUST NOT forward protected content, allocate inference, consume an effect, or advance to a more privileged state. Malformed framing, authentication-integrity failure, or repeated hostile input SHOULD close the connection. An object-scoped policy, authority, freshness, obligation, or replay failure SHOULD deny or abort the affected exchange without disrupting unrelated exchanges when isolation is safe. The receiver SHOULD append a denial/failure event when doing so does not trust unvalidated attacker-controlled claims. A failure to durably append required evidence MUST produce `EVIDENCE_FAILURE`, never `COMPLETED`.

Profile negotiation failure uses `UNSUPPORTED`. A critical requested profile with `UNSUPPORTED` MUST terminate negotiation. A non-critical `DEGRADED` result MUST enumerate every omitted capability and MUST NOT weaken the `dari/1` authorization, receipt, freshness, rollback, or effect semantics. A receiver MUST NOT silently fall back from a DARI object to a `paper/1` parser after any DARI validation failure.

## F.12 Stable allocations

Object-type numbers and message-type numbers are independent registries. Existing `paper/1` numbers are not renumbered. This appendix reserves these object types for DARI signed-object domain separation:

| Object | Object type |
|---|---:|
| Peer Credential | `0x0100` (existing) |
| Authorization Grant | `0x0202` |
| Evidence Receipt body | `0x0302` (existing) |
| Governed Exchange | `0x0303` |
| Authorization Decision | `0x0304` |
| Signed State Checkpoint | `0x0305` |
| Effect Prepare | `0x0610` |
| Effect Authorization | `0x0611` |
| Effect Result | `0x0612` |
| Effect Status response | `0x0613` |
| Receipt Attestation | `0x0703` |
| Selective Disclosure Proof | `0x0704` |

Authorization Grants use existing message carriers `SESSION_GRANT` (`0x0201`) and `LEASE_ISSUE` (`0x0210`) according to exchange phase. Authorization Decisions use `RELAY_VERDICT` (`0x0304`). Evidence Receipts use `EVIDENCE_RECEIPT` (`0x0307`). A Signed State Checkpoint is carried by the state-bearing message that references it or by a profile extension; no new core message type is allocated for it in Phase 2.

The only new message allocations are the transactional-effect subfamily:

| Constant | Message type | Payload |
|---|---:|---|
| `EFFECT_PREPARE` | `0x0610` | signed Effect Prepare |
| `EFFECT_AUTHORIZE` | `0x0611` | signed Effect Authorization |
| `EFFECT_COMMIT` | `0x0612` | signed Effect Result with `COMMITTED` |
| `EFFECT_ABORT` | `0x0613` | signed Effect Result with `ABORTED` |
| `EFFECT_STATUS` | `0x0614` | Effect Status request or signed response |

Values `0x0604` through `0x0606` are not available: the legacy specification already assigns them even though current source registries are inconsistent. The message-type allocation `0x0610` through `0x0614` MUST NOT be reused by `paper/1` or another extension. A profile that does not negotiate `dari.tools/1` MUST report these messages as `UNSUPPORTED_MESSAGE_TYPE` without attempting a legacy interpretation.

## F.13 Protocol and extension profiles

The normative profile and compatibility table is `DARI_COMPATIBILITY_AND_PROFILE_MAP.md`. The following profile identifiers are exact and case-sensitive:

- `paper/1` is bounded legacy compatibility for the frozen preface, record/message encoding, and documented legacy objects. It is not the active DARI kernel and confers no DARI conformance.
- `dari/1` is the active, application-neutral DARI kernel defined by this appendix.
- `dari.ai/1` is provider-neutral inference request, streaming response, usage, cancellation, and model/endpoint binding.
- `dari.tools/1` is the transactional effects and tool-bridge profile defined by F.10 and F.12.
- `dari.model-supply/1` is model-artifact manifest and endpoint-authorization binding.
- `dari.web/1` is the executable browser profile for WebTransport/HTTP/3 origin binding, browser proof of possession, constrained WebSocket fallback, durable reconnect/status behavior, and unchanged DARI authorization/receipt semantics. A runtime claiming `EXACT` MUST pass the browser transport, origin, proof, reconnect, effect-status, and deployment vectors defined by the implementation plan; otherwise it returns `UNSUPPORTED` and records the reason in its capability manifest.
- `dari.federation/1` is the executable cross-domain profile for issuer, audience, trust domain, bilateral trust-bundle freshness, policy intersection, residency constraints, and receipt keys. A runtime claiming `EXACT` MUST pass the trust-bundle, issuer/audience, intersection, residency, rollback, offline, and receipt vectors defined by the implementation plan; otherwise it returns `UNSUPPORTED` and records the reason in its capability manifest.
- `dari.collab/1` is the executable governed-collaboration profile for ordered chat, presence, broadcasts, encrypted delivery, and resumable file transfer. It depends on `dari/1` and returns `EXACT`, `DEGRADED`, or `UNSUPPORTED` only from executable handlers and conformance evidence.
- `dari.media/1` is the executable voice/live-media profile for authorized media chunks, cancellation, usage, retention, and terminal receipts. It depends on `dari/1` and `dari.collab/1` and MUST NOT create an ungoverned media side channel.

All `dari.*` profiles depend on `dari/1`. An implementation MUST negotiate each requested profile as `EXACT`, `DEGRADED`, or `UNSUPPORTED`. `EXACT` means every required behavior is implemented, deployed, observable, and conformant. `DEGRADED` is permitted only for explicitly optional capabilities and MUST enumerate them; it MUST NOT hide a missing transport, trust, authorization, evidence, or effect guarantee. A profile whose runtime or conformance gate has not passed MUST return `UNSUPPORTED`, even when its schemas or generated types are present.

## F.14 Required black-box negative conformance cases

A conformance suite MUST exercise at least these cases and observe no protected side effect on rejection:

1. Non-canonical CBOR, duplicate map key, indefinite length, unknown top-level label, and unknown critical extension.
2. COSE signature valid over a payload that differs from the decoded object presented to the application.
3. Wrong external AAD, unprotected `alg`/`kid`, unnegotiated algorithm, unknown key, expired/revoked credential, altered transcript, wrong challenge, wrong channel binding, or missing subject proof of possession.
4. Every attenuation failure enumerated in F.4, including changed audience and parent-digest substitution.
5. Missing, invalid, stale, forked, replayed-lower-sequence, or wrong-audience Signed State Checkpoint.
6. Missing decision, deny-overrides-allow, conflicting obligation ID, expired decision, pending pre-action obligation, failed obligation, and completion with a pending post-action obligation.
7. Receipt field not derived from committed evidence, random/placeholder root, sequence gap, absent required signer, signer-role overclaim, changed body after attestation, and claimed completion after evidence append failure.
8. Altered disclosed event, wrong leaf position, bad empty padding, wrong segment count, malformed MMR peak list, altered omission manifest, overlapping omission range, or proof for an event outside receipt bounds.
9. Duplicate operation with identical binding returns the stored state/result without another execution; duplicate operation with changed nonce, input, grant, executor, or retry owner returns `REPLAY_CONFLICT`; reconnect queries status rather than re-executing; unauthorized retry owner is rejected; crash in `EXECUTING` never fabricates `COMMITTED`.
10. Critical `dari.web/1` or `dari.federation/1` negotiation fails with `UNSUPPORTED`; a non-critical request is explicitly omitted and never reported as `EXACT` or `DEGRADED`.
11. A DARI validation error never triggers silent parsing as `paper/1`; a legacy `paper/1` object never acquires delegation, fresh-state, multi-party-attestation, selective-disclosure, or exactly-once claims that its bytes do not prove.

Positive conformance vectors MUST include byte-exact deterministic CBOR, protected-header bytes, `Sig_structure`, signature, object digest, grant-chain digest, checkpoint high-water transition, linear evidence root, segment root, MMR root, disclosure proof, and duplicate-effect result. These are required contract tests for implementation work; this specification does not claim they exist yet.
