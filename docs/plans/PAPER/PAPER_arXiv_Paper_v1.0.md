# PAPER: Governance-Native Communication with Causal Provenance for Human–AI Software Engineering Systems

## Patty AI Provenance & Enforcement Relay

**Manuscript status:** Historical design and evaluation manuscript; canonical submission source is the DARI LaTeX edition
**Version:** 1.0  
**Date:** 2026-08-11  
**Authors:** Use the approved author list in the canonical DARI LaTeX edition before submission.
**Companion specification:** `PAPER Protocol Specification v1.0`  
**Open-source intent:** Protocol specification, schemas, reference implementations, conformance tests, test vectors, fuzzers, and benchmark harnesses are intended for public release.

---

## Abstract

AI coding systems are evolving from stateless assistants into long-running software-engineering agents that read repositories, select context, invoke models, call tools, modify files, collaborate with humans, and influence source-control history. The dominant communication interfaces used by these systems remain model-centric or capability-centric: they efficiently carry requests, responses, tools, tasks, and artifacts, while enterprise governance and provenance are commonly reconstructed by gateways, log pipelines, policy middleware, or post-hoc supply-chain attestations. This creates an architectural gap for regulated organizations that need to answer not only *what bytes crossed the network*, but *who was authorized to cause an AI action, under which policy, using which model and context, what enforcement occurred before execution, and how the result later affected source code and human decisions*.

We present **PAPER (Patty AI Provenance & Enforcement Relay)**, an open, stateful communication protocol for human–AI software-engineering systems. PAPER introduces a **Governed Exchange** as its principal unit of interaction. Each protected exchange binds an authenticated peer and user to a short-lived **Capability Lease**, an immutable **Policy Epoch**, inline **Relay Verdicts**, and a content-addressed **Provenance Spine** connecting intent, context disclosure, model invocation, tool execution, human edits, and resulting artifacts. PAPER separates an administrative Control Plane from horizontally scalable **PAPER Relays** in the data path and places a registered **Patty Inference Agent (PIA)** in front of model-serving engines. The same multiplexed protocol supports inference, tools, repository context, chat, asynchronous voice messages, governed file transfer, presence, broadcasts, telemetry, and administrative directives while keeping these communication classes under separate capabilities and stream contracts.

PAPER deliberately reuses established cryptographic and transport standards rather than introducing new cryptography: QUIC is the preferred binding, TLS 1.3/TCP is the fallback, deterministic CBOR and COSE provide canonical signed objects, and HPKE/MLS may provide application-level sealing where policy requires it. The research contribution is therefore not encrypted transport itself. It is a protocol model in which **authority, enforcement, and causal provenance are properties of the live AI exchange rather than metadata reconstructed after the fact**. This manuscript specifies the architecture, threat model, protocol invariants, and an evaluation methodology covering overhead, scale, adversarial behavior, interoperability, and provenance fidelity. We explicitly distinguish design claims from empirical results; no unperformed benchmark or formal-proof result is asserted.

---

## 1. Introduction

Large language models are increasingly embedded in software-development environments that can do more than produce text. Contemporary coding agents can inspect a repository, choose files to send as context, invoke compilers and test runners, install dependencies, edit multiple files, interact with Git, and continue over long sessions. Enterprises are beginning to place these systems inside development workflows that also contain confidential source code, personal information, production architecture, credentials, regulated data, internal communication, and change-control processes.

This transition changes the security question. A traditional model API can answer whether a request was authenticated and encrypted. An enterprise AI platform must additionally answer questions such as:

- Which employee and which enrolled Harness initiated the action?
- Which repository, branch, commit, project, and purpose bounded the session?
- Which model artifact—not merely which human-readable model name—processed the request?
- Which files and exact spans were disclosed as context, and why?
- Which policy version was in force before disclosure or execution?
- Which rule transformed, denied, quarantined, or required approval for an action?
- Did an LLM merely propose a tool call, or did an authorized runtime actually execute it?
- Which generated code survived later human edits, refactors, merges, and commits?
- Can an auditor independently verify evidence of the path from human intent to artifact?
- Can the same system support internal chat, files, voice messages, and emergency broadcasts without turning those channels into uncontrolled AI-context side doors?

The typical architecture answers these questions with several loosely coupled components: an HTTPS model API, an LLM gateway, an authorization layer, endpoint or sandbox tooling, a SIEM, an observability pipeline, source-control metadata, and perhaps an artifact-provenance system. This modularity is often useful, but it creates a semantic gap: the transport does not know that a token stream belongs to a particular authorized engineering purpose, while the governance system reconstructs relationships from logs that may have different identifiers, retention policies, trust boundaries, and failure modes.

PAPER explores a different design point: **make governed AI work itself a protocol primitive**.

Instead of modeling an AI interaction as only:

```text
request -> model -> response
```

PAPER models a protected interaction as:

```text
authenticated user + enrolled Harness
        |
        v
purpose-bound Working Session
        |
        v
Capability Lease + Policy Epoch
        |
        v
Governed Exchange
        |
        +--> authorized context disclosure
        +--> approved model package / endpoint
        +--> inline Relay Verdicts
        +--> model output
        +--> independently authorized tools/runtime
        +--> human approvals/edits
        |
        v
content-addressed causal provenance
        |
        v
Evidence Receipt / commit binding / audit
```

The design is motivated by enterprise and government deployments, especially organizations that operate private or sovereign GPU infrastructure, but PAPER is intended as an open protocol rather than a Korea-specific wire format. The Korean deployment requirements—centralized organizational control, on-premises and air-gapped operation, rich administrative visibility, contractor/SI support, and configurable cryptographic profiles—serve as demanding design cases rather than assumptions baked into the syntax.

### 1.1 Contributions

This paper makes five primary design contributions.

**C1. Governed Exchange as the communication primitive.** PAPER defines a protected exchange that is not executable unless it is bound to authenticated identities, a valid short-lived Capability Lease, an immutable Policy Epoch, and the required Relay decisions. This makes authorization state part of the live protocol lifecycle rather than merely a gateway configuration detail.

**C2. Inline enforcement separated from the administrative Control Plane.** PAPER separates policy authority from high-volume traffic forwarding. A horizontally scalable PAPER Relay fleet performs data-plane enforcement and evidence generation using signed/cacheable Control Plane state. The Control Plane need not proxy every token while remaining authoritative for identities, policy, model approval, revocation, and provenance state.

**C3. Causal, content-addressed AI engineering provenance.** PAPER defines a Provenance Spine that links human intent, context selection, policy decisions, model execution, tool proposals/results, human/AI edits, reviews, file artifacts, and source-control binding. The protocol also defines a per-exchange evidence chain and signed Evidence Receipt. We do not claim that provenance, DAGs, attestations, or signed receipts are themselves new; the contribution is their integration into the *live governed exchange* across Harness, Relay, PIA, runtime, and human actions.

**C4. Model-endpoint identity stronger than model-name routing.** PAPER introduces a registered PIA peer, a signed Patty Model Package (PMP), and expiring Endpoint Leases. A Relay routes to a cryptographically identified approved package/deployment instead of trusting a caller-supplied `model_name` string. Raw model-serving APIs are expected to remain local to PIA.

**C5. One capability-separated Harness substrate.** PAPER carries not only inference but also context, tools, chat, asynchronous voice, file transfer, presence, broadcasts, telemetry, and administrative controls. These channels share identity/session/provenance infrastructure but remain separate through extensions, peer profiles, Stream Contracts, protection modes, and capability scopes. Collaboration data does not become model context merely because it traverses the same protocol.

### 1.2 Non-contributions and claim discipline

PAPER does **not** claim to invent:

- secure transport;
- QUIC or TLS;
- public-key workload identity;
- capability security in general;
- provenance or signed attestations;
- Merkle structures or content addressing;
- tool-use protocols;
- agent-to-agent interoperability;
- source-control provenance;
- end-to-end group encryption;
- prompt-injection defenses.

PAPER intentionally composes mature standards for these lower-level building blocks. Its novelty claim is the *protocol-level composition and lifecycle semantics* around a specific boundary: **governed communication between a human-facing AI engineering Harness, inline enforcement Relay, controlled inference/runtime infrastructure, and the resulting software provenance**.

This distinction is important because current protocol research is moving quickly. If overlapping systems emerge before submission, the claim should be narrowed rather than defended through terminology changes.

### 1.3 Research questions

The design motivates the following research questions (RQs):

**RQ1 — Governance placement.** Can purpose-bound authorization and policy state be made intrinsic to live AI communication without coupling every token to a centralized database or policy service?

**RQ2 — Performance.** What latency, throughput, CPU, memory, and storage overhead results from capability validation, inline enforcement, governed multiplexing, and proof-carrying provenance relative to a conventional HTTPS/JSON streaming model API?

**RQ3 — Provenance fidelity.** How accurately can causal AI/human provenance remain attached to software through edits, file moves, rebases, merges, and multi-agent changes?

**RQ4 — Security.** Which classes of misconfiguration or attack are eliminated or made externally enforceable by removing generic model-protocol compatibility from the Harness and requiring enrolled peers, leases, policy epochs, PIA identity, and model-package authorization?

**RQ5 — Unified Harness transport.** Can inference and enterprise collaboration share a protocol without creating privilege confusion, implicit context disclosure, or unacceptable head-of-line/resource interference?

**RQ6 — Sovereign operation.** Can the same protocol operate in public cloud, enterprise private cloud, and fully disconnected government environments without altering its core trust model?

---

## 2. Background and Adjacent Systems

PAPER sits at the intersection of secure transport, agent communication, runtime governance, workload identity, and provenance. This section defines the boundary carefully.

### 2.1 Conventional model APIs and AI gateways

Most model-provider APIs expose request/response or streaming abstractions over HTTPS. Gateways commonly add API-key management, routing, quotas, retry, logging, and policy checks. These systems are valuable and operationally mature, but their typical abstraction remains model invocation: an authenticated application selects a model and sends a request.

PAPER deliberately rejects generic OpenAI-compatible or Anthropic-compatible networking at the Harness boundary. The point is not syntactic novelty. Removing compatibility means the official Harness cannot be configured to send governed enterprise traffic directly to an arbitrary model endpoint and still function normally. More importantly, PAPER's application objects include user/Harness identity, Capability Lease, Policy Epoch, Relay Verdicts, and causal provenance that a generic completion API does not require.

This is not a complete data-loss-prevention guarantee. A malicious employee can write separate HTTPS software unless operating-system or network egress controls prevent it. PAPER addresses the **authorized Harness path**, not arbitrary host networking.

### 2.2 Model Context Protocol

The Model Context Protocol (MCP) standardizes how AI applications interact with tools, resources, and related capabilities. The July 28, 2026 MCP specification explicitly moves the core toward stateless, self-describing requests over ordinary HTTP infrastructure and formalizes extensions and authorization hardening [14]. MCP solves an important interoperability boundary: exposing external capabilities and context to AI applications.

PAPER does not attempt to replace MCP tool semantics. A PAPER-governed Harness may call an MCP server through a controlled tool broker. PAPER's boundary is different: the authenticated Harness-to-Relay-to-inference/runtime communication path and the governance/evidence relationship around that path.

### 2.3 Agent2Agent

A2A 1.0 defines interoperability between independent and potentially opaque agents. Its canonical data model is expressed with Protocol Buffers and its specification provides JSON-RPC, gRPC, and HTTP/JSON protocol bindings [15]. A2A focuses on discovery, capabilities, messages, collaborative tasks, and artifacts between agent systems.

PAPER is not primarily an open agent-discovery or inter-vendor delegation protocol. Its primary peer is an enrolled enterprise Harness whose operations are bounded by organizational policy and whose AI actions must be tied to an evidence graph. A future PAPER agent extension could coexist with A2A rather than competing with it.

### 2.4 Runtime agent governance

Recent research argues that agentic systems require enforcement during execution rather than only pre-deployment governance. MI9, for example, proposes runtime governance components including continuous authorization monitoring and finite-state-machine conformance [20]. Security guidance for production agents emphasizes deterministic policy enforcement for high-consequence actions in addition to model/input defenses [21].

PAPER shares the principle that model reasoning must not be the final authority. It differs by standardizing the communication objects and peer transitions through which enforcement state is carried and evidenced.

### 2.5 Provenance-aware authorization

PACT (Provenance-Aware Capability Contracts) highlights an important granularity problem: invocation-level trust can be too coarse when untrusted content influences authority-bearing tool arguments. PACT tracks argument provenance and validates semantic role-specific trust contracts [19]. This is closely adjacent to PAPER and materially limits any claim that PAPER is the first system to join provenance and capability control.

The distinction is scope. PAPER uses purpose-bound Capability Leases for long-running Harness sessions and creates causal provenance across context, policy, model, runtime, collaboration references, human edits, and source-control artifacts. PAPER could additionally incorporate PACT-like argument-level taint/provenance in its Tool extension; the two ideas are complementary.

### 2.6 Software supply-chain provenance

in-toto records signed metadata about supply-chain steps and artifacts [17]. SLSA 1.2 defines provenance and source/build tracks for establishing where software and source revisions came from and which processes produced them [18]. These systems provide a mature vocabulary for verifiable artifact provenance.

PAPER targets an earlier and more interactive portion of the lifecycle. It records the causal graph *during* human-AI engineering activity, before a final source revision or build artifact necessarily exists. PAPER commit/evidence objects may later be exported into in-toto/SLSA-oriented attestations.

### 2.7 Transparency and receipts

The IETF SCITT architecture defines trustworthy and transparent digital-supply-chain services and uses COSE Receipts for transparency proofs [11, 12]. PAPER does not invent a public transparency ledger. It produces signed Evidence Receipts locally and may register selected receipts or statements into SCITT-compatible infrastructure when deployments require externally auditable transparency.

### 2.8 Workload identity

SPIFFE specifies portable identities for workloads across heterogeneous infrastructure and defines verifiable identity documents and a Workload API [16]. PAPER defines its own peer credential semantics because it must represent Harness enrollment, peer profiles, model endpoints, and protocol roles across unmanaged desktops as well as servers. Server-side deployments may nonetheless use SPIFFE/SPIRE to bootstrap Relay, PIA, runtime, or Control Plane workload identity.

### 2.9 Communication-protocol taxonomy

A recent taxonomy of LLM-agent communication protocols classifies protocols across counterparty, payload, state, discovery, and schema flexibility, and identifies privacy and policy enforcement as open research gaps [22]. PAPER can be viewed as exploring a particular point in that design space: stateful, authenticated, enterprise-controlled communication in which policy and causal provenance are mandatory aspects of protected exchanges rather than optional application metadata.

---

## 3. Requirements and Design Constraints

### 3.1 Product requirements that shape the protocol

The protocol was designed against the following product-level requirements.

1. The public Harness must be able to connect over ordinary Internet networks.
2. Enterprise and government Harnesses must use the same protocol rather than a separately forked government product.
3. QUIC should provide the preferred multiplexed transport, with TLS/TCP fallback for networks that block UDP.
4. The fallback must remain native PAPER—never an HTTP, WebSocket, or generic model-API downgrade.
5. A PAPER Control Plane must register Harness identities and bind them independently to users.
6. The Relay must be able to enforce policy inline before model disclosure or protected actions.
7. The Control Plane must remain authoritative without becoming the high-volume token-forwarding bottleneck.
8. The Relay must route only to approved PIA/model-package identities.
9. Human chat, voice messages, files, presence, and broadcasts must travel through the same authenticated infrastructure but may not become automatic model context.
10. The protocol must work in a fully local, air-gapped deployment.
11. The protocol specification and reference implementation must be open.
12. No security property may depend on the secrecy of the wire format.

### 3.2 Why statefulness is intentional

PAPER is stateful at the Working Session and lane level. This is not an assumption that all AI protocols should be stateful. The state exists because the system needs to represent:

- a human/Harness identity binding;
- expiring delegated authority;
- policy transitions during long-running work;
- ordered token/tool streams;
- resumable large transfers;
- causal provenance;
- administrative revocation;
- multi-lane fairness and flow control.

A PAPER Relay may still scale horizontally. Working Session state can be represented by signed leases, cached policy epochs, durable checkpoints, and shared/durable exchange state rather than process-local sticky-session assumptions.

### 3.3 Why a custom application protocol

The motivation for a custom protocol is not to make packet captures look unusual. It is to define application invariants that generic request/response APIs do not enforce:

- every protected action belongs to a Governed Exchange;
- every exchange is lease- and policy-bound;
- every lane declares a Stream Contract;
- every model endpoint is an authenticated protocol peer or authorized package endpoint;
- every protected completion produces evidence;
- collaboration-to-AI context conversion is explicit;
- administrative communication and administrative enforcement are distinct.

These invariants can in principle be implemented over HTTP, but a dedicated protocol makes them mandatory, versioned, conformance-testable, and non-optional for participating peers.

---

## 4. Threat Model

### 4.1 Protected assets

PAPER protects or provides evidence for:

- source code and repository metadata;
- prompts and model responses;
- internal documents and logs;
- personal information and secrets;
- tool and runtime authority;
- approved model identity;
- employee collaboration content;
- governance policy and approvals;
- provenance records;
- administrative directives;
- billing/metering evidence.

### 4.2 Adversaries

We consider:

**A1 — Unregistered external client.** The attacker understands the public specification and attempts to connect with a custom implementation but possesses no valid enrolled peer credential.

**A2 — Credential thief.** The attacker obtains a valid Harness credential/private key and attempts replay or cloning.

**A3 — Host-controlling user.** The attacker controls a user's machine, can modify the open-source Harness, and may be able to reuse local credentials.

**A4 — Malicious or compromised model.** The model produces unsafe instructions, attempts capability escalation, or follows prompt injection contained in context.

**A5 — Fake inference endpoint.** An operator exposes another model, possibly under a misleading model name, and attempts to receive traffic intended for approved Patty models.

**A6 — Malicious or compromised Relay.** A data-plane component with legitimate plaintext visibility abuses or tampers with content.

**A7 — Malicious administrator.** An authorized administrator abuses broad visibility or sends unauthorized high-impact controls.

**A8 — Network attacker.** The attacker can observe, replay, reorder, drop, or modify packets but cannot break accepted cryptographic primitives.

**A9 — Tenant adversary.** A legitimate user in one organization/project attempts cross-tenant or cross-repository access.

**A10 — Resource-exhaustion attacker.** The attacker attempts handshake, parser, lane, file, or inference resource exhaustion.

### 4.3 Explicit non-goals

Without hardware-rooted attestation, PAPER cannot prove that a peer key is currently held only by an unmodified official binary. If an attacker has full host control and extracts/reuses a legitimate key, protocol authentication proves possession of the enrolled identity, not binary integrity.

PAPER also cannot prevent a hostile user from using a completely separate networking program to send data to an external provider. Enterprise guarantees against arbitrary external AI egress require OS/network controls in addition to the Harness protocol.

These limitations are deliberately part of the model rather than hidden behind “zero trust” terminology.

### 4.4 Trust boundaries

The baseline architecture treats:

- model output as untrusted proposals;
- repository/document/web text as data, not authority;
- Harness requests as authenticated but still policy-constrained;
- Relay as a high-trust enforcement point for P0 plaintext traffic;
- PIA as trusted to bind authorized PAPER inference to the configured local serving engine;
- Control Plane signing authorities as high-value roots of trust;
- provenance/evidence as tamper-evident claims whose factual accuracy still depends on the integrity of the components producing them.

---

## 5. PAPER Architecture

### 5.1 Separation of control and data planes

A naïve centralized design would send every prompt, context block, token, file, and voice chunk through the Control Plane application itself. That maximizes observability but turns administrative services into a throughput bottleneck and a large plaintext processing surface.

PAPER instead separates:

```text
                    CONTROL PLANE
         identity / policy / model / approvals
        leases / revocation / provenance registry
                          |
                          | signed/cacheable state
                          v
                    PAPER RELAYS
             governed high-volume data plane
                 /                   \
                / PAPER               \ PAPER
               v                       v
           HARNESS                    PIA
                                         |
                                         v
                                  local model engine
                                         |
                                         v
                                        GPU
```

Relays scale independently. They validate signed state locally where safe and perform policy/security functions before protected data is forwarded. The Control Plane remains authoritative but does not need a synchronous database round trip per token.

### 5.2 Peer profiles

PAPER v1 defines six profiles:

- **HARNESS** — terminal/IDE client used by a human;
- **RELAY** — governed data-plane peer;
- **INFERENCE** — PIA/model-serving boundary;
- **RUNTIME** — sandbox/execution peer;
- **ADMIN_AGENT** — privileged automation;
- **CI_AGENT** — future CI/CD participant.

Profile is authenticated identity state, not merely a message field. A valid HARNESS credential cannot emit INFERENCE-only endpoint attestation or administrative directives.

### 5.3 Extensions

The protocol is divided into independently negotiated message families:

```text
paper.core/1
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

Support for an extension is distinct from authority to use it. A Harness can implement file transfer while the current Capability Lease prohibits it.

### 5.4 Transport bindings

PAPER uses QUIC as the preferred transport because concurrent inference, control, chat, file, voice, and telemetry lanes have different latency and flow-control requirements. TLS 1.3 over TCP is the native fallback for networks where UDP/QUIC is unavailable. Both bindings negotiate PAPER through ALPN; neither uses HTTP as a required application layer.

The protocol binds higher-level peer authentication to the negotiated TLS/QUIC channel using TLS exporter/channel-binding material. Thus an authentication proof captured on one secure connection cannot simply be replayed on another.

### 5.5 Structured representation

PAPER uses a small binary record prelude and deterministic CBOR for canonical structured objects. COSE is used for signed credentials, leases, and evidence objects. The choice is pragmatic rather than a novelty claim: canonical representation simplifies cross-language content addressing, signatures, offline verification, and compact binary operation.

---

## 6. Identity, Enrollment, and Bounded Authority

### 6.1 Separating user, client, and workload identity

A recurring source of ambiguity in AI infrastructure is the tendency to treat “the API key” as the identity of the entire interaction. PAPER deliberately distinguishes the user, Harness, Working Session, Relay, PIA, model package, and runtime. This distinction enables questions that would otherwise be conflated. A user may be authorized to a repository but use a revoked Harness; a Harness may be enrolled but bound to the wrong employee; a PIA may be trusted but serving a suspended model artifact; a session may be valid but its model lease may have expired.

Let:

- \(U\) denote a user identity;
- \(H\) an enrolled Harness identity;
- \(R\) a Relay identity;
- \(I\) an inference/PIA identity;
- \(S\) a Working Session;
- \(P\) a Policy Epoch;
- \(L\) a Capability Lease;
- \(M\) an approved model package.

A protected AI exchange is not authorized by any one of these values independently. The Relay evaluates a predicate of the form:

\[
\mathrm{Permit}(E) =
\mathrm{AuthPeer}(H) \land
\mathrm{BindUser}(U,H,S) \land
\mathrm{ValidLease}(L,S,U,H,P) \land
\mathrm{ScopeAllows}(L,E) \land
\mathrm{PolicyAllows}(P,E) \land
\mathrm{EndpointApproved}(I,M,E).
\]

Content-security checks and human approvals may introduce additional terms depending on the exchange class.

### 6.2 PAPER Peer Credential

A PAPER Peer Credential (PPC) binds a public key to a peer ID, organization/trust domain, and peer profile. The baseline specification represents the credential as a canonical CBOR object protected with COSE. This provides compact, offline-verifiable identity without requiring every Relay to synchronously query an identity database for every connection.

The credential carries validity and revocation information but is not a bearer token: possession of the serialized credential is insufficient. During authentication the peer signs a context that binds:

- client and server nonces;
- negotiated PAPER version and extension set;
- selected cryptographic profile;
- peer credential digest;
- TLS/QUIC channel-binding material;
- HELLO/HELLO_ACK transcript.

This produces downgrade resistance and prevents a proof captured on one secure connection from being replayed as a valid proof on another.

### 6.3 Harness enrollment

An ordinary public or enterprise Harness enrollment proceeds as follows:

1. Harness generates a local asymmetric key pair.
2. User authenticates to the applicable Control Plane.
3. Harness sends its public key and non-secret build metadata.
4. Organization enrollment policy evaluates user, limits, and optional administrative approval.
5. Control Plane issues a PPC with profile HARNESS.
6. Harness stores the private key using OS-protected storage where available.
7. User and Harness remain separate objects in the Control Plane.

Air-gapped environments may perform the same logical operation with signed offline enrollment bundles.

This design prioritizes deployability over an unattainable software-only attestation claim. The protocol can reject unregistered clients and can identify cloned or behaviorally anomalous credentials, but it does not claim to cryptographically distinguish an official open-source binary from a locally modified binary that possesses the same key on a hostile machine.

### 6.4 Capability Leases

Long-lived static credentials are a poor representation of agent authority because an agentic session changes over time. PAPER therefore delegates *session/action authority* through signed, expiring Capability Leases.

Conceptually:

\[
L = \mathrm{Sign}_{K_C}(H, U, S, P, \mathrm{scope}, t_{nbf}, t_{exp}, b, a)
\]

where \(K_C\) is an authorized Control Plane/lease-signing key, \(b\) is a resource budget, and \(a\) is an approval requirement set.

Lease scope can constrain:

- repository and branch;
- file read/write paths;
- model-package classes or exact model IDs;
- tool categories and arguments;
- network destinations and purposes;
- input/output token budgets;
- file/voice/collaboration capabilities;
- protection profile;
- runtime resources;
- required reviewers.

A lower-layer service may narrow a lease but cannot broaden it. Lease renewal itself becomes a provenance event, allowing later analysis to reconstruct exactly when authority changed during a long session.

### 6.5 Why leases rather than bearer API keys

A conventional API key typically grants service-level access and relies on server-side configuration to infer current policy. A PAPER lease answers a more granular question: *for this enrolled Harness and user, in this Working Session, under this exact policy version, what authority is delegated until what time?*

The design also supports bounded Control Plane outages. Relays can verify signed leases and cached policy state without a synchronous central lookup for every token, while new privileged authority can fail closed when central state is unavailable.

---

## 7. Policy Epochs and Governance-Native Exchanges

### 7.1 Policy Epoch

Enterprise policy is mutable. If an auditor later sees that an action was “allowed,” it is insufficient to know only the current rule set. PAPER therefore identifies the exact effective policy state with a **Policy Epoch**.

An epoch can incorporate digests or immutable references to:

- organization baseline;
- project/repository overlay;
- model-authorization policy;
- context/DLP profile;
- approval matrix;
- retention policy;
- policy-engine/schema version.

The epoch is immutable once issued. When a policy changes, the Control Plane issues a new epoch and distributes transition instructions. Security-critical revocations can invalidate current authority immediately; ordinary changes may take effect at lease renewal or after the current read-only operation, depending on policy.

This design addresses a common audit ambiguity: a Relay Verdict can always identify *which policy state* it evaluated, not simply “the policy service returned allow.”

### 7.2 Governed Exchange

The **Governed Exchange** is PAPER's central abstraction. It groups one logical action or interaction with its authority and evidence.

A conceptual exchange is:

\[
E = (id, kind, U, H, S, L, P, purpose, parents, streams, verdicts, artifacts, status).
\]

Examples include:

- one model inference;
- one context disclosure;
- one tool execution;
- one employee chat message;
- one voice transfer;
- one governed file transfer;
- one emergency broadcast;
- one administrative model recall;
- one code-provenance binding.

Not every exchange has the same cost or retention. Presence updates, for example, need lightweight ephemeral treatment. The unifying property is that protected exchanges are attributable and bounded by the applicable authority.

### 7.3 Exchange state machine

PAPER requires protected exchanges to traverse an explicit state machine:

```text
CREATED
  |
  v
AUTHORIZING ----deny----> DENIED
  |
  +----approval---------> WAITING_APPROVAL
  |                           |
  |                       approve/reject
  v
AUTHORIZED
  |
  v
ACTIVE ----revoke----> TERMINATED
  |
  +----failure-------> FAILED
  |
  v
FINALIZING
  |
  v
COMPLETED
```

The state machine is a security primitive because it gives conformance tests something concrete to reject. An implementation that accepts `RUNTIME_EXECUTE` before authorization is not merely “configured differently”; it violates the protocol state model.

### 7.4 Relay Verdicts

A Relay Verdict is a structured decision associated with an Exchange and Policy Epoch. Results include:

- allow;
- allow after transformation/redaction;
- allow with obligations;
- require user confirmation;
- require reviewer/security/dual approval;
- quarantine;
- deny;
- terminate session;
- isolate runtime;
- create incident.

The verdict includes reason/rule identifiers and any transformations or obligations. High-frequency allow events may be authenticated through the secure session and summarized in a signed exchange receipt, while high-impact denial, approval, quarantine, or termination decisions can be individually signed according to policy.

### 7.5 Governance Envelope

The Governance Envelope links the live payload to the validated authority context. It includes or references:

- lease;
- Policy Epoch;
- organization/user/Harness/session;
- project/repository/branch;
- classification;
- declared purpose;
- requested capability;
- model/tool authorization;
- protection profile;
- retention and approval requirements.

A key design rule is that Harness-supplied governance labels are *claims or requests*, not authoritative decisions. The Relay resolves them against signed Control Plane state. This prevents a modified client from changing `classification=public` or `model=approved` and expecting the server to trust the string.

### 7.6 Proof-carrying stream lifecycle

A PAPER data stream is opened with a **Stream Contract** that identifies its Exchange, extension, content class, peer profiles, lease, policy epoch, protection mode, maximum resource use, priority, ordering, acknowledgement, and resumability semantics.

The phrase “proof-carrying” here does not mean formal proof-carrying code. It means the stream carries verifiable references to the authority/evidence objects needed to determine whether it is valid. The receiver does not infer authorization merely from the fact that a TLS connection exists.

---

## 8. Multiplexed Communication Model

### 8.1 Why a single protocol carries multiple channel types

An enterprise coding Harness is becoming a workplace surface rather than merely a prompt box. The product requirements include model inference, tool execution, repository context, 1:1/group chat, asynchronous voice, file transfer, presence, emergency broadcasts, telemetry, and administrative controls.

Putting each feature behind unrelated protocols would multiply authentication, authorization, reconnect, observability, and provenance systems. PAPER instead shares a connection/session substrate while preventing semantic collapse through peer profiles, extension negotiation, lane types, capability scopes, and Stream Contracts.

### 8.2 Logical lanes

Representative lane classes are:

- control/authentication;
- critical broadcast;
- interactive AI;
- tool control;
- chat;
- presence;
- voice;
- file transfer;
- telemetry.

On QUIC, high-volume logical lanes map naturally to independent QUIC streams. On TLS/TCP, PAPER performs application-level multiplexing using a lane identifier and per-lane sequence. The semantics are identical even though TCP cannot eliminate transport-level head-of-line blocking.

### 8.3 Priority

PAPER defines application priorities so that a 2 GiB file transfer does not intentionally starve a token stream or emergency message. Control and critical broadcasts are highest priority, followed by interactive inference/tool control, chat/presence, then bulk voice/file/telemetry.

This priority is not a replacement for transport congestion control. It controls the sender/Relay's scheduling among application queues.

### 8.4 Chat is not implicit AI context

The unified substrate creates a dangerous temptation: if chat and files are already available to the Harness, automatically add them to model context. PAPER explicitly forbids that shortcut.

A chat or file object becomes model context only through a separate Context Exchange that re-evaluates:

- current user authorization;
- repository/project scope;
- classification;
- DLP/secrets/PII policy;
- prompt-injection trust label;
- context purpose;
- retention;
- provenance edge.

Thus a collaboration message may be visible to the user while still prohibited from disclosure to the model.

### 8.5 Voice

PAPER v1 focuses on asynchronous voice messages, not real-time telephony. Voice is chunked, resumable, content-digested, retention-classified, and separately governed. A speech-to-text transcription is itself an AI exchange with a model identity and provenance edge to the original voice digest. This avoids silently treating “voice transcription” as an untracked client-side convenience.

### 8.6 File transfer

File transfer uses an offer/decision/chunk/commit model. A Relay can enforce maximum size, classification, scanning, malware/quarantine, recipient scope, and full-content digest before committing the object. The file is not automatically context for an AI session.

### 8.7 Broadcasts versus directives

PAPER distinguishes *communication* from *authority*.

A critical broadcast can state “Model X has been recalled.” It does not itself revoke Model X. The Control Plane separately issues a signed administrative directive and invalidates model endpoint leases. This separation prevents a user who has broadcast permission from accidentally acquiring control-plane execution authority.

---

## 9. Inference Path and Model Identity

### 9.1 Why model names are insufficient

An enterprise may deploy Qwen, a Patty fine-tune, or another model behind vLLM/SGLang. If the Relay authorizes models using a string such as `model="patty-coder"`, an operator can trivially configure another serving process with the same name. Conversational “fingerprints” are probabilistic and spoofable. A raw serving URL plus bearer key therefore cannot establish model artifact identity.

### 9.2 Patty Model Package

PAPER defines a signed **Patty Model Package (PMP)** manifest. The manifest identifies:

- model family and package version;
- weight artifact digest(s);
- tokenizer digest;
- prompt/chat template digest;
- quantization profile;
- supported context;
- serving-engine compatibility;
- license/restrictions;
- evaluation references;
- publisher signature.

The package is the authorization object. A display name is only metadata.

### 9.3 Patty Inference Agent

PIA is a registered `INFERENCE` PAPER peer placed immediately in front of the local serving engine. It terminates PAPER, verifies or is provisioned with the expected PMP, and translates authorized inference into a local engine interface.

This avoids requiring a permanent fork of vLLM or SGLang as the principal trust boundary. Serving engines can be upgraded independently while the PIA adapter tracks their local interface. An engine plugin may improve performance or model-state measurement, but raw remote model APIs remain hidden behind PIA.

### 9.4 Endpoint Lease

The Control Plane associates PIA identity, PMP, deployment instance, classification, and approval state, then issues short-lived Endpoint Leases. A Relay routes only when the selected endpoint lease authorizes the current organization/classification/model package.

A model recall invalidates those leases. This provides a clear enforcement mechanism: no new exchange reaches a recalled package merely because the endpoint is still responding to TCP.

### 9.5 Residual trust

PMP plus PIA does not provide hardware remote attestation. A fully compromised PIA host may lie about local process/weight state unless an optional stronger attestation mechanism is integrated. PAPER therefore distinguishes:

- **package identity evidence** under the software trust model;
- **host/runtime attestation** as optional high-assurance hardening.

The specification should never label the first as equivalent to hardware-backed measured boot.

---

## 10. Context, Tool Authority, and Prompt Injection

### 10.1 Context is a governed resource

Repository access is not synonymous with model disclosure. A developer may be able to read a file locally while organizational policy prohibits sending it to a particular model or forbids including secrets/PII.

PAPER represents proposed context using manifests containing source identity, repository/commit, path/span/symbol, size/token estimate, classification, trust label, transformation state, and reason for inclusion. The Relay produces an item-level Context Decision: allow, metadata-only, transform, require approval, or deny.

### 10.2 Trust labels do not grant authority

PAPER can label content as trusted policy, trusted repository content, internal authorized content, user-supplied, external-untrusted, model-generated, or unknown. These labels guide policy and provenance. They do not allow retrieved text to modify leases or tool permissions.

This is particularly important for indirect prompt injection. A malicious README can instruct an LLM to upload credentials, but it cannot change the protocol's authenticated capability state.

### 10.3 Tool proposals versus tool execution

The model may propose:

```text
TOOL_PROPOSE {
    tool: shell,
    purpose: "run authentication unit tests",
    command: "./gradlew test --tests '*Auth*'"
}
```

The proposal is untrusted. Relay/runtime authorization normalizes its intended effect—executable, arguments, paths, environment, network, expected outputs—and tests it against the Capability Lease and Policy Epoch.

Only then can a scoped `RUNTIME_EXECUTE` be delivered to the runtime.

### 10.4 Argument-level provenance

PAPER's v1 core tracks causal parents at the message/object level. A stronger Tool extension can carry field/argument-level provenance, directly complementing PACT-like controls. For example, the destination URL argument of a network tool could state whether it originated from the user's explicit instruction, trusted policy, or untrusted web content. The design intentionally leaves room for this finer granularity rather than asserting that exchange-level provenance solves every prompt-injection problem.

### 10.5 Idempotency

Agent workflows frequently reconnect or retry. Tool actions cannot be naively replayed.

PAPER assigns operation classes such as:

- safe replay;
- same-idempotency-key only;
- query status before retry;
- never auto-retry.

A destructive command or database migration must not run twice merely because an inference connection was resumed.

---

## 11. Causal Provenance and Evidence

### 11.1 Why ordinary logs are insufficient

A distributed trace is useful for answering operational questions such as “which services handled request X?” It is not necessarily sufficient to answer “which human intent and context caused these surviving lines of code to exist, under what authorization and review process?” Likewise, a source-control commit records a resulting revision and authorship metadata but usually does not contain the full AI context, policy decisions, model identity, and runtime tool sequence.

PAPER therefore distinguishes three related data structures:

1. **operational traces** — implementation telemetry;
2. **Provenance Spine** — causal content-addressed graph;
3. **Evidence Receipt** — signed summary/root proving the Relay's recorded exchange evidence.

### 11.2 Provenance Spine

Let each provenance node \(N_i\) contain a node type, actor, exchange/session, policy epoch where applicable, object references, artifact references, and a set of causal parent digests \(Parents_i\).

A node digest is:

\[
d_i = H(\texttt{"PAPER-OBJ-v1"} \parallel type_i \parallel \mathrm{CanonicalCBOR}(N_i)).
\]

This creates a content-addressed DAG. Multiple parents represent causal convergence. For example, a generated patch may depend on a user instruction, three context disclosures, one previous tool result, and one policy-approved model invocation.

Representative node types include:

- user intent;
- context request/disclosure;
- policy decision;
- model invocation/output;
- tool proposal/decision/result;
- file read/write;
- patch creation;
- human edit;
- AI edit/refactor;
- review decision;
- commit binding;
- chat/voice/file references;
- security finding;
- administrative action;
- artifact export.

The DAG is intentionally causal rather than purely timestamp-ordered. Clocks remain useful metadata but cannot be the only ordering mechanism in a distributed system or an offline environment.

### 11.3 Exchange evidence chain

Within one Exchange, PAPER additionally maintains an ordered evidence chain. Let \(e_i\) be the digest of the \(i\)-th recorded evidence event. The chain is:

\[
r_0 = H(\texttt{"PAPER-EVIDENCE-START-v1"} \parallel d_{open})
\]

\[
r_i = H(\texttt{"PAPER-EVIDENCE-EVENT-v1"} \parallel r_{i-1} \parallel e_i).
\]

At finalization, the Relay signs an Evidence Receipt containing the final chain root, terminal status, policy epoch, lease digest, relevant PIA/model identity, and provenance terminal/root references.

This structure provides tamper evidence for the Relay's recorded history. It does not solve “lying sensor” problems: a compromised trusted component may emit a false event. PAPER therefore treats provenance integrity, provenance completeness, and claim accuracy as different evaluation dimensions.

### 11.4 Selective export and redaction

Enterprise evidence often contains sensitive content. PAPER permits an exported evidence package to omit raw prompts or source code while preserving digests, relationships, and a manifest of intentionally omitted fields/objects.

This distinction matters. A verifier must be able to tell the difference between:

- *the evidence system recorded an object but export policy withheld its plaintext*; and
- *the object was never recorded*.

Future integrations can use COSE Receipts or SCITT transparency infrastructure for independently verifiable inclusion/non-equivocation properties rather than inventing another global log.

### 11.5 Code attribution through software evolution

The most difficult provenance problem is not hashing a generated patch; it is preserving meaningful attribution after software changes.

Suppose AI generates function \(f_0\), a human changes two branches creating \(f_1\), another AI refactors it to \(f_2\), and a rebase moves the function into another file as \(f_3\). A static “AI lines = 23” annotation becomes inaccurate.

PAPER's source-control integration therefore treats line numbers as only one signal. An implementation can use:

- Git patch/hunk mapping;
- blob and file-rename information;
- AST node/symbol identity;
- normalized semantic fingerprints;
- token similarity;
- explicit edit events;
- merge/rebase parents;
- human confirmation when mapping is ambiguous.

The protocol records an *ambiguity state* instead of forcing false precision.

### 11.6 Relationship to SLSA and in-toto

PAPER's live provenance can feed later supply-chain attestations. For example, a protected source revision could include or reference:

- source-control review and branch policy;
- PAPER Evidence Receipt(s) for AI-assisted changes;
- model/artifact identities;
- human reviewer identities;
- test/security evidence.

SLSA Source provenance can then attest how the source revision was created under source-control policy, while PAPER provides richer causal detail about the human-AI session that contributed content. This is layering, not replacement.

---

## 12. Payload Confidentiality and Cryptographic Composition

### 12.1 Why PAPER does not invent encryption

Designing a custom encryption algorithm would add risk without research value. PAPER relies on TLS 1.3 and QUIC for transport confidentiality/integrity and uses standardized cryptographic containers for application objects.

The interesting question is **who is authorized to see content**, not whether PAPER can create a new cipher.

### 12.2 Protection profiles

PAPER defines four policy-selectable payload modes.

**P0 — Relay Inspectable.** The secure transport terminates at the Relay. The Relay can inspect/transform prompt, code, output, file, or collaboration content as policy permits. This is the ordinary mode for inline enterprise DLP, PII, secrets, malware, and prompt-injection controls.

**P1 — Service Sealed.** Payload content is additionally encrypted to a specific authorized processing service/key domain. Routing components may see metadata but not plaintext. HPKE is the preferred standardized construction.

**P2 — Endpoint Sealed.** Harness and PIA protect payload end to end so the Relay can see governance/routing metadata but cannot inspect the plaintext. The system must then honestly give up Relay-level content DLP for that object class.

**P3 — Group E2EE.** Collaboration groups may use MLS-based group encryption. This is optional because enterprise DLP/eDiscovery/legal-hold requirements may conflict with endpoint-only visibility.

### 12.3 Metadata remains sensitive

Application-layer sealing does not hide all metadata. Relays still need some combination of:

- organization/session;
- peer identities;
- lane class;
- message size/timing;
- model routing metadata;
- policy epoch;
- lease reference.

Traffic analysis remains possible. PAPER should not describe P2/P3 as metadata-private protocols unless future padding/oblivious-routing mechanisms are actually added and evaluated.

### 12.4 Key separation

The design uses separate key domains for:

- organization/peer issuance;
- Harness identity;
- Relay identity;
- PIA identity;
- Capability Lease signing;
- policy signing;
- model-package signing;
- evidence receipts;
- administrative directives;
- application payload sealing;
- offline updates.

This prevents a single compromised signing key from automatically authorizing all protocol operations.

### 12.5 Algorithm agility and Korean sovereign deployments

PAPER has an algorithm registry rather than hard-coding one long-term suite. A baseline profile ensures global interoperability; restricted government deployments can select approved cryptographic modules/suites appropriate to their accreditation environment.

This is particularly important for a protocol intended to operate both as public open source and in Korean government networks. The protocol must distinguish a *wire semantic requirement* from a *deployment crypto policy*.

### 12.6 Post-quantum transition

Current standards now provide frameworks for hybrid TLS 1.3 key exchange and COSE/JOSE representations for ML-DSA [13]. PAPER v1 makes post-quantum profiles optional rather than creating premature private algorithms. A future interoperable profile should specify exact standardized algorithm identifiers and publish cross-language test vectors.

---

## 13. Security Analysis

This section analyzes the intended effects of PAPER's design. It is not a formal security proof.

### 13.1 Unregistered protocol implementation

**Attack.** An attacker reads the open specification, implements a compatible client, and connects to an enterprise Relay.

**Expected result.** The implementation can perform syntax/version negotiation but cannot reach a protected Working Session without a valid enrolled peer credential, proof of its private key bound to the current TLS/QUIC channel, and user binding where required.

**Residual risk.** If the attacker obtains a legitimate credential/private key, the problem becomes credential theft rather than protocol reverse engineering.

### 13.2 Stolen Harness key

**Attack.** A valid Harness private key is copied to another process/device.

**Controls.** Credential revocation, key protection where available, concurrent-clone/anomaly detection, short credential/lease lifetimes, user re-binding, and connection-bound challenge proofs.

**Limitation.** Baseline PAPER cannot reliably distinguish a stolen key from the legitimate process if both satisfy ordinary software authentication. Optional hardware/device attestation can strengthen this but is not required by the product architecture.

### 13.3 Modified open-source Harness

**Attack.** User modifies Patty Code while retaining its enrolled key.

**Expected result.** The client may still authenticate as the enrolled Harness if it holds the key. It cannot, however, cause the Relay to broaden a Capability Lease or Policy Epoch merely by changing client-side fields. Server-side/profile/state validation remains authoritative.

**Limitation.** This is not binary attestation. PAPER's security goal is that a modified client remains constrained by server-side authorization, not that modification is cryptographically impossible.

### 13.4 Generic external model endpoint

**Attack.** User points the official Harness at `api.openai.com` or arbitrary OpenAI-compatible vLLM.

**Expected result.** The official Harness speaks PAPER, not a generic model API. The external endpoint cannot negotiate/authenticate as a PAPER Relay/PIA, and the request fails.

**Residual risk.** A user can run an independent HTTPS client if network policy allows it. PAPER must be combined with egress controls for a company-wide “no external models” guarantee.

### 13.5 Fake model name

**Attack.** An operator exposes Qwen under the human-readable name of an approved Patty model.

**Expected result.** Routing authorization refers to signed PMP digest and Endpoint Lease bound to PIA identity, not the display name. A name collision does not satisfy model-package authorization.

**Residual risk.** A compromised PIA host can misrepresent local runtime state absent remote attestation.

### 13.6 Replay and connection downgrade

**Attack.** Network adversary captures an authentication proof/lease and replays it on a new connection or forces QUIC failure to trigger TCP fallback.

**Expected result.** Authentication signatures bind the negotiated transcript and TLS exporter/channel binding; the proof is not portable. TCP fallback performs full PAPER authentication and preserves the same lease/policy semantics. Protected 0-RTT actions are forbidden in v1.

### 13.7 Prompt injection leading to tool escalation

**Attack.** A README or external document instructs the model to read secrets or contact an external endpoint.

**Expected result.** Retrieved content is untrusted data and cannot mutate the Capability Lease. A `TOOL_PROPOSE` request is re-authorized outside the model; file/network/tool scopes remain deterministic.

**Residual risk.** If policy already grants a broad tool capability, prompt injection can induce harmful behavior *within* that scope. More granular argument provenance and least privilege remain necessary.

### 13.8 Chat/file as a context bypass

**Attack.** User uploads a sensitive file through collaboration then expects the Harness to pass it to the model without repository/context policy.

**Expected result.** PAPER requires a separate Context Exchange to attach collaboration content to AI context. Authorization and DLP run at the point of disclosure.

### 13.9 Malicious Relay

**Attack.** Relay is compromised while handling P0 plaintext.

**Impact.** The Relay is intentionally a high-trust data-plane enforcement point and can misuse plaintext. Transport encryption does not protect against the legitimate termination point.

**Mitigations.** Service separation with P1 sealing, restricted administrative access, key separation, evidence/content digests, independent monitoring, hardened deployment, and optional endpoint-sealed modes.

**Limitation.** PAPER cannot make an authorized plaintext inspection point cryptographically unable to see the plaintext it is authorized to inspect.

### 13.10 Malicious administrator

Administrative read/control operations are explicit PAPER/Control Plane events and should be audited. High-impact directives can require two-person authorization. Yet organizational governance remains a trusted-policy problem: if the organization intentionally grants an administrator the right to read prompts, the protocol cannot call that action unauthorized.

### 13.11 Cross-tenant access

Every lease/session/exchange carries organization and scope. Relay policy must reject attempts to use context, chat, files, or model endpoints outside the authorized tenant/project. Vector/search systems behind PAPER still need tenant-safe access controls; protocol labels alone do not fix a broken storage backend.

### 13.12 Evidence tampering

Changing a canonical provenance node changes its digest and therefore descendant references. Removing/reordering an event changes the final evidence-chain root. A signed receipt detects post-signing modification.

A compromised trusted component can still originate false claims. Independent sensors, source-control state, PIA receipts, runtime receipts, and external transparency can reduce—but not eliminate—this trust.

### 13.13 Denial of service

Binary protocols can be attacked with oversized lengths, parser complexity, stream floods, authentication floods, and expensive model allocation. PAPER requires bounded parser depth/object sizes, cheap preface/syntax checks, per-peer/tenant/lane quotas, authentication before expensive work, and flow control.

The research evaluation must include stateful fuzzing and adversarial resource tests rather than treating memory safety as guaranteed by using Rust.

---

## 14. Korean Enterprise and Government Deployment

### 14.1 Korea as a demanding deployment case

PAPER is intended for global use, but Korean enterprise/government requirements influence the design in several useful ways:

- hierarchical organization and affiliate structure;
- centralized administrative policy;
- Korean PII/security classifications;
- internal contractors and SI partners;
- private GPU infrastructure;
- strong interest in on-premises deployment;
- disconnected/closed-network environments;
- detailed auditing and provenance;
- operational broadcasts and mandatory acknowledgements.

These are not protocol-localized strings; they imply scope, trust, and deployment semantics.

### 14.2 Organizational model

The identity/policy layer can represent:

- legal entity/affiliate;
- division/department/team;
- employee and contractor affiliation;
- `직급` and `직책` separately;
- project/repository membership;
- user/Harness relationship;
- display names in Korean and Romanized form.

PAPER wire objects remain Unicode/language neutral.

### 14.3 Korean PII

Korean enterprise deployments frequently need detection/control for resident registration numbers, foreigner registration numbers, passport/license numbers, phone/email/address, account/card data, employee identifiers, and contextual combinations that identify a person. PAPER does not define a national PII classifier; it defines how a classifier's decision, transformation, reason, and evidence become part of a Relay Verdict and Provenance Spine.

This separation allows classifiers and regulations to evolve without changing the transport protocol.

### 14.4 Contractor and SI access

A system integrator or contractor may be authorized to one project for a fixed period but must not inherit employee-wide collaboration or repository rights. PAPER Capability Leases can bind:

- sponsor organization;
- project/repository;
- start/expiry;
- allowed Harnesses;
- permitted chat groups;
- file/export restrictions;
- model classes;
- offboarding/revocation.

### 14.5 Air-gapped operation

Government Sovereign PAPER can operate without public Internet access:

```text
local Harnesses
      |
      v
local PAPER Relays ---- local Control Plane
      |
      v
local PIA/model registry
      |
      v
customer-owned GPUs
```

Enrollment, trust anchors, policy bundles, model packages, registry snapshots, protocol updates, and evidence verification can all be distributed through signed offline bundles.

The protocol must not require a vendor callback, cloud revocation service, or public certificate authority in this profile.

### 14.6 One protocol, not a government fork

The goal is not to create “PAPER Enterprise” and an incompatible “PAPER Government.” Government deployments select stricter cryptographic, retention, telemetry, model, and connectivity policies while preserving the same peer profiles and message semantics.

This improves both security review and interoperability: code paths used in sovereign deployments are exercised by ordinary enterprise use rather than becoming a rarely tested fork.

---

## 15. Implementation Architecture

### 15.1 Reference components

The reference project should include:

- Rust PAPER core/framing/state machine;
- QUIC binding;
- TLS/TCP binding;
- deterministic CBOR + COSE objects;
- Relay server;
- Harness client library;
- PIA;
- runtime adapter;
- Go interoperability implementation;
- vLLM and SGLang PIA adapters;
- CDDL schema repository;
- conformance runner;
- fuzz targets;
- golden byte vectors;
- benchmark harness.

### 15.2 Why Rust and Go

Rust is suitable for the parsing, client, Relay, and PIA core where memory safety and explicit ownership are valuable. Go provides a second independent implementation ecosystem and fits enterprise/cloud networking services.

Two languages also prevent accidental standardization of implementation quirks. Interoperability between independently written Rust and Go stacks should be a release gate.

### 15.3 Relay internals

Although PAPER treats Relay as one protocol peer, production implementation may decompose it into:

```text
connection terminator
       |
identity/lease validation
       |
policy coordinator
  /    |     \
DLP  context  security classifiers
  \    |     /
     verdict
       |
routing/admission
       |
PIA connection pool
       |
evidence/provenance writer
```

The decomposition must preserve ordering: content cannot reach PIA before all mandatory pre-inference gates that policy requires.

### 15.4 Control Plane state caching

Per-token synchronous calls to a centralized policy database would destroy performance and availability. Relays therefore cache signed/verifiable state:

- trust bundles;
- revocations;
- Policy Epochs;
- model/endpoint registry;
- lease issuer keys;
- group/role references;
- quotas;
- retention profiles.

The design must measure revocation propagation delay and define a bounded stale-state policy.

### 15.5 PIA adapters

PIA maps a normalized PAPER inference request into a local serving engine. For vLLM/SGLang, initial adapters may use local engine APIs. The security boundary is the PAPER-enrolled PIA plus model-package/endpoint authorization, not the provider-compatible API itself.

A future serving-engine plugin could expose stronger model-load measurements or direct token streaming without requiring a permanent fork.

### 15.6 Storage

PAPER itself does not mandate databases, but a reference enterprise deployment may use:

- relational DB for identity/session/control metadata;
- event bus for durable activity;
- columnar analytics for usage;
- object storage for evidence/files;
- graph/index services for provenance;
- Git/SCM for source lineage;
- standard observability backend for metrics/traces.

Protocol identifiers and schemas should remain stable regardless of backend choice.

---

## 16. Evaluation Methodology

This manuscript intentionally does not fabricate results. The following experiments are the required methodology for a later empirical revision.

### 16.1 Evaluation questions

The evaluation should determine:

- how much PAPER adds to connection establishment;
- how much pre-inference governance adds to TTFT;
- whether token forwarding remains near line-rate/model-rate;
- how Relay cost scales with concurrent Harnesses;
- how much provenance data is produced per exchange;
- whether reconnect/fallback duplicates side effects;
- which attacks are rejected by protocol invariants;
- how accurately code provenance survives realistic software evolution;
- whether independent implementations interoperate.

### 16.2 Baselines

Comparisons must be fair and goal-aware.

**B0 — Raw secure streaming transport.** A minimal TLS/QUIC binary echo/stream baseline to estimate pure PAPER framing overhead.

**B1 — Conventional HTTPS/JSON model API.** Bearer-authenticated HTTP(S) request with streaming response and no governance beyond basic authentication.

**B2 — Model API + conventional gateway.** HTTPS/JSON plus gateway auth, quota, logging, and comparable DLP/policy where implementable.

**B3 — PAPER with governance disabled/minimal.** Measures framing/session/identity cost.

**B4 — PAPER governed.** Full lease/policy/Relay verdict/provenance path.

MCP and A2A should be compared semantically and, where useful, with microbenchmarks for relevant message patterns, but the paper must not benchmark an MCP tool call against model token generation and call the result a protocol win. Their goals differ.

### 16.3 Hardware/software disclosure

Every benchmark publication should report:

- CPU model/core count;
- memory;
- NIC/link speed;
- client/Relay/PIA placement;
- RTT;
- OS/kernel;
- QUIC/TLS library and version;
- PAPER commit;
- inference engine/version;
- model and quantization;
- GPU type/count;
- policy modules enabled;
- payload protection mode;
- log/provenance backend configuration.

### 16.4 Connection setup benchmark

Measure p50/p95/p99 for:

1. TCP + TLS + PAPER authentication;
2. QUIC + PAPER authentication;
3. reconnect/resumption;
4. QUIC failure then TCP fallback;
5. public Cloud vs on-prem LAN.

Separate network/TLS handshake from PAPER HELLO/auth/user-binding/session-grant time.

### 16.5 Inference latency benchmark

Workloads:

- small prompt: 1 KiB input;
- coding prompt: 64 KiB input/context;
- repository context: 1 MiB;
- large context: 16 MiB or model-appropriate token equivalent;
- tool-use turn;
- multi-turn session.

Metrics:

- time to first Relay verdict;
- TTFT;
- inter-token latency;
- output tokens/sec;
- Relay-added p50/p95/p99 latency;
- bytes added by PAPER metadata;
- CPU cycles/user time per token forwarded;
- memory per active stream.

Policy configurations:

- identity/lease only;
- + deterministic path/model policy;
- + secrets/PII scan;
- + prompt-injection classifier;
- + response inspection;
- P0 vs P1 where applicable.

### 16.6 Scale benchmark

Progressive scale targets inherited from the product requirements:

| Profile | Connected Harnesses | Concurrent AI streams |
|---|---:|---:|
| Developer | 10 | 5 |
| SMB | 500 | 100 |
| Mid-market | 5,000 | 1,000 |
| Large enterprise | 50,000 | 10,000+ |
| Shared central service | 100,000+ | benchmark-defined |

At each stage measure:

- Relay CPU/memory;
- active QUIC/TCP connections;
- connection churn;
- policy-cache hit rate;
- evidence throughput;
- PIA admission queues;
- fairness between AI/chat/file lanes;
- control-message delivery under bulk traffic.

A 100,000-Harness result should be reported only if actually run; simulation/load-generator counts are not equivalent to real distributed clients and should be labeled accordingly.

### 16.7 Collaboration benchmark

Test concurrent:

- chat bursts;
- presence updates;
- 30-second and 5-minute Opus voice messages;
- 1 MiB, 100 MiB, and 1 GiB files;
- mandatory broadcasts;
- simultaneous AI token streams.

The primary question is whether bulk file/voice load materially degrades interactive AI/control lanes under QUIC and TCP bindings.

### 16.8 Provenance storage benchmark

Measure bytes and write amplification per:

- one inference;
- one tool call;
- one file edit;
- one commit;
- one 30-minute coding session;
- one collaboration-to-context attachment.

Compare:

- full raw-content logging;
- PAPER digest/reference model;
- checkpointed/aggregated evidence.

Report storage growth and verification time, not only generation overhead.

### 16.9 Provenance fidelity dataset

Construct repositories with controlled ground truth for:

1. AI generates new function.
2. Human changes 10%, 50%, 90%.
3. Function renamed.
4. File renamed.
5. Function moved across files/modules.
6. AI refactors human function.
7. Human resolves merge conflict.
8. Rebase changes surrounding code.
9. Two AI sessions edit same function.
10. Human copies generated code into another file.
11. Generated patch partially reverted.
12. Template-derived and AI-modified code mix.

Metrics:

- attribution precision/recall against ground truth;
- ambiguity rate;
- surviving-span accuracy;
- provenance graph completeness;
- time/storage overhead.

### 16.10 Security experiment suite

Protocol-level attacks:

- unknown credential;
- wrong issuer/profile;
- channel-binding replay;
- modified HELLO/version downgrade;
- expired/revoked lease;
- stale Policy Epoch;
- model-name spoof;
- invalid PMP digest;
- fake Endpoint Lease;
- unauthorized peer message family;
- malformed state order;
- duplicate side-effect retry;
- tampered evidence node;
- omitted evidence event;
- invalid file/voice digest;
- broadcast-to-admin privilege confusion.

Agent-security scenarios:

- malicious README asks to read `.env`;
- retrieved web content chooses unauthorized network destination;
- tool output attempts to alter system policy;
- model requests a broader tool scope;
- sensitive chat/file is attached as context without authorization;
- model tries to select unapproved external endpoint.

AgentDojo-style environments [23] and newer automated prompt-injection work can provide complementary adversarial workloads, while PAPER-specific tests focus on whether protocol-level authority boundaries remain intact even when the model is successfully manipulated.

### 16.11 Fuzzing

Fuzz targets include:

- fixed record parser;
- CBOR decoder/canonical checker;
- COSE credential/lease parser;
- state machine;
- file/voice resumability;
- provenance DAG decoder;
- negotiation;
- error handling.

Stateful fuzzing is essential because a syntactically valid sequence can still violate authorization transitions.

### 16.12 Interoperability

At least two implementations that do not share the full protocol stack must complete:

- QUIC authentication;
- TLS/TCP fallback;
- Working Session;
- governed inference;
- tool call;
- file transfer;
- evidence verification.

Golden byte vectors test deterministic encoding and signatures across languages.

### 16.13 Statistical reporting

Performance experiments should include repeated runs, confidence intervals or distribution percentiles as appropriate, warm/cold-cache separation, and raw benchmark data where disclosure permits. Security evaluation should report attack success/failure definitions and false-positive/utility costs rather than only “blocked attacks.”

---

## 17. Comparative Analysis and Novelty Boundary

### 17.1 Comparison dimensions

Table 1 summarizes the intended boundary. It is not a ranking: each system is optimized for different goals.

| System / pattern | Primary boundary | Typical transport/data model | Protocol-native authorization scope | Live inline policy decision object | Causal human/AI code provenance | Model artifact/endpoint identity | Collaboration substrate |
|---|---|---|---|---|---|---|---|
| Conventional model API | application ↔ model service | HTTPS + JSON/streaming | provider/app auth | generally gateway-specific | external | provider/model-name dependent | no |
| MCP 2026-07-28 | AI app ↔ tools/context | stateless JSON-RPC/HTTP core | MCP/extension authorization | not PAPER-style exchange verdict | not source-code lineage goal | no | not primary goal |
| A2A 1.0 | agent ↔ agent | canonical protobuf + JSON-RPC/gRPC/HTTP bindings | agent/service security | not enterprise Harness policy epoch model | not source-code lineage goal | agent identity/card, not model package goal | messages/tasks/artifacts |
| SPIFFE | workload ↔ workload identity | X.509/JWT/WIT identity framework | workload identity | no AI policy semantics | no | no | no |
| PACT research | agent tool-call arguments | runtime monitor | provenance-aware capability contracts | yes at argument/tool authority boundary | argument/cross-step provenance | no | no |
| MI9 research | runtime agent governance | framework instrumentation | continuous runtime governance | yes, framework-specific | telemetry/governance focus | not primary | no |
| in-toto/SLSA | software supply chain/source/build | signed attestations | supply-chain process policies | post-step/source/build evidence | source/build provenance | artifact/build identity | no |
| **PAPER** | Harness ↔ Relay ↔ PIA/runtime | native QUIC/TLS-TCP + deterministic CBOR | expiring Capability Lease + Policy Epoch | **Relay Verdict** | **live causal provenance to code/commit** | **PMP + Endpoint Lease** | **chat/voice/files/broadcast under separate capabilities** |

### 17.2 The claim that should survive peer review

The strongest defensible statement is not “PAPER is the first secure AI protocol.” It is:

> **PAPER proposes a governance-native communication model for human–AI software engineering in which purpose-bound authority, immutable policy state, inline enforcement decisions, and causal provenance are intrinsic to the lifecycle of multiplexed AI and collaboration exchanges.**

This claim can still be falsified by prior/parallel work, which is why the literature review must be repeated immediately before submission.

### 17.3 Why PAPER is more than “gRPC with extra fields”

A skeptical reviewer may argue that all PAPER semantics could be expressed as protobuf messages over gRPC. Technically, that is true: almost any application protocol can be re-encoded over another RPC system.

The research question is not whether bytes require a new framing format. PAPER's argument is that the following are **normative protocol invariants** rather than optional middleware conventions:

1. an authenticated connection is insufficient for protected action;
2. protected action requires a valid lease and Policy Epoch;
3. every logical data lane declares a Stream Contract;
4. model invocation requires approved PIA/model-package state;
5. tool proposals do not confer authority;
6. collaboration objects are not implicit AI context;
7. transport fallback preserves authorization semantics;
8. completed side effects are not automatically replayed;
9. protected completion generates verifiable evidence;
10. causal provenance is linkable to resulting software artifacts.

A conforming implementation can use different languages, databases, policy engines, or serving engines, but it cannot omit these behaviors while claiming the corresponding PAPER conformance level.

### 17.4 Why not simply extend MCP?

MCP's scope and 2026 stateless architecture deliberately optimize a different boundary. Extending MCP to make every coding session, token stream, runtime action, chat, voice message, file transfer, broadcast, model artifact, and provenance edge part of its tool/resource protocol would create an unusually large and mismatched extension.

PAPER instead permits a controlled Tool extension to speak MCP *behind* its authorization boundary. This layering uses MCP where MCP is strongest without forcing it to become an enterprise Harness transport.

### 17.5 Why not use A2A as the transport?

A2A is attractive for independent agent interoperability and already defines messages/tasks/artifacts and multiple bindings. PAPER's Harness is not primarily an autonomous agent advertising itself for arbitrary inter-agent delegation. It is an enrolled organizational endpoint whose model/context/tool authority is centrally bounded and whose resulting code must be attributable.

PAPER and A2A could interoperate through a future governed gateway: a PAPER Harness may authorize a particular A2A task delegation, and that external task becomes a provenance child rather than an untracked network call.

### 17.6 Relationship to PACT is particularly important

PACT shows that provenance must sometimes be finer than an exchange or tool invocation. A PAPER tool proposal with a safe-looking operation can still be dangerous if an authority-bearing argument derives from an untrusted source.

Therefore a future PAPER version should consider standardizing argument/value provenance within the Tool extension. Doing so would not make PACT redundant; rather, PACT provides a concrete model of semantic role and provenance-aware capability checking that PAPER could transport and evidence.

---

## 18. Discussion

### 18.1 Governance-native versus governance-aware

Many systems can be made “governance-aware” by attaching labels to ordinary requests. PAPER uses “governance-native” more narrowly: the receiver's state machine refuses protected operations that lack the required governance objects. Governance is therefore part of protocol validity, not an optional field used only if a gateway happens to be configured.

This distinction has operational consequences. Consider a model request copied from an enterprise environment to a raw public API. In a metadata-based design, the JSON may still be a perfectly valid inference request; the external service simply ignores the enterprise fields. In PAPER, the intended recipient must authenticate as the correct profile, validate the session/lease/policy, and participate in the required exchange lifecycle.

### 18.2 Protocol incompatibility as a safety property—within limits

Deliberately avoiding generic model-API compatibility reduces accidental bypass and narrows approved-client configuration. It is a useful *system property*, but it should not be overmarketed as cryptographic isolation. Open-source users can implement translators, and malicious employees can use other software if networking permits.

The security value comes from combining incompatibility with:

- enrolled peer identity;
- server-side lease/policy enforcement;
- model endpoint identity;
- egress controls where organizations require them;
- auditable provenance.

### 18.3 Rich administrative visibility versus privacy

The same provenance that enables security investigations can enable intrusive employee monitoring. PAPER makes admin access auditable and allows multiple payload-protection modes, but protocol design alone cannot determine an organization's legitimate purpose for monitoring.

A responsible deployment should separate:

- security/operational metadata;
- engineering-content visibility;
- collaboration-content visibility;
- HR/work-intelligence use.

In particular, presence duration, token volume, number of prompts, file count, or AI-acceptance rate should not be treated as standalone employee-performance metrics. Evidence of work is not the same as a fair evaluation of work.

### 18.4 Provenance completeness versus cost

Capturing every token as a signed object would be expensive and often unnecessary. PAPER instead favors:

- content digests;
- causal references;
- batched high-frequency events;
- signed checkpoints/receipts;
- selective retention of raw content.

This creates a trade-off: greater aggregation reduces storage/signature overhead but can reduce forensic granularity. The evaluation should quantify this frontier.

### 18.5 Relay as a choke point

Inline governance necessarily creates a data-path dependency. PAPER mitigates this by separating Relay from Control Plane and scaling Relays horizontally, but a Relay outage or expensive scanner can still increase TTFT or block protected work.

Possible mitigations include:

- local signed policy caches;
- parallel scanners;
- asynchronous post-checks only for low-risk non-disclosure events;
- classification-dependent inspection;
- per-tenant Relay pools;
- bounded degraded read-only modes.

The protocol should prefer truthful fail-closed behavior over silently bypassing missing mandatory scanners.

### 18.6 Stateful protocols and operations burden

PAPER's statefulness enables leases, causal lanes, resumability, and long-running actions, but increases implementation complexity compared with stateless HTTP requests. The state must be bounded, serializable/checkpointable, and recoverable after Relay failures.

This is an intentional trade-off. The empirical evaluation must show whether the resulting governance and resumability benefits justify the operational cost.

### 18.7 Binary protocol observability

A custom binary protocol can be harder to debug than JSON/HTTP. The open-source project therefore needs:

- a Wireshark/dissector or equivalent diagnostic tooling where feasible;
- a `paper inspect` tool;
- CDDL/schema-aware decoders;
- safe-redaction defaults;
- readable error codes;
- golden vectors.

Security through poor debuggability would be a failure.

### 18.8 Standardization strategy

PAPER should first prove interoperability and operational value as an open-source protocol. Premature standards submission can freeze poor design. A reasonable progression is:

1. public v0.x specification and implementations;
2. independent interoperability;
3. security review and fuzz maturity;
4. real enterprise/government pilots;
5. v1 stable registries;
6. only then evaluate IETF/other standards-track work and ALPN registration.

The project should not claim an IANA allocation before one exists.

---

## 19. Limitations

### 19.1 No mandatory hardware attestation

PAPER's baseline cannot prove an open-source Harness binary is unmodified on a fully hostile host. This limits the strength of “official client only” claims to enrolled credential possession plus server-side constraints.

### 19.2 Relay trust in inspectable mode

P0 makes Relay a plaintext trust point. Organizations requiring confidentiality from the Relay must use P1/P2/P3-like designs and accept reduced inspection capability.

### 19.3 PIA software measurement

A signed model package identifies what *should* be loaded. Without trusted execution or host attestation, a compromised server can lie about local state.

### 19.4 Provenance is not truth by magic

Cryptographic hashes and signatures detect later modification; they do not guarantee that a trusted producer recorded reality correctly. PAPER provenance quality depends on trustworthy collection points and corroboration.

### 19.5 Source-code attribution is probabilistic after complex evolution

Semantic lineage through arbitrary refactoring, copying, code generation, conflict resolution, and squash merges can become ambiguous. PAPER must represent uncertainty rather than publish misleading AI percentages.

### 19.6 Protocol scope is large

Inference plus collaboration creates implementation and attack-surface complexity. Extension separation and conformance levels mitigate this; the full enterprise implementation must implement core, governed inference, collaboration, and the applicable runtime profiles behind executable conformance gates.

### 19.7 Not a substitute for endpoint/network security

PAPER governs the authorized Harness path. It does not replace MDM, EDR, firewalls, DNS control, proxies, sandboxing, or secret-management systems.

### 19.8 No empirical results in this manuscript version

This version specifies the design and evaluation method. It does not claim measured latency, scale, security success rates, or provenance accuracy. Those claims require implementation and reproducible experiments.

---

## 20. Ethical, Privacy, and Governance Considerations

PAPER is intentionally capable of producing detailed evidence about employee interactions with AI and code. This capability can improve incident response, accountability, secure development, and audit, but can also enable disproportionate surveillance.

Recommended product/deployment safeguards include:

- purpose-specific administrator roles;
- explicit content-view permissions;
- audit of admin content access;
- configurable retention;
- visibility to users of effective policy where appropriate;
- appeal/correction mechanisms if provenance feeds employee evaluation;
- separation between security analytics and HR analytics;
- aggregate team views where individual detail is unnecessary;
- prohibition on covertly treating weak activity metrics as performance outcomes.

Government deployments should additionally ensure that protocol evidence and cryptographic controls are not marketed as legal or certification compliance by themselves. Compliance depends on system scope, operations, people, policy, and formal assessment.

---

## 21. Research Extensions Beyond the Current Release

### 21.1 Argument-level provenance

Integrate fine-grained provenance for authority-bearing tool arguments, potentially interoperating with PACT-like semantic role contracts.

### 21.2 Formal state-machine verification

Model critical PAPER invariants in TLA+, PlusCal, Alloy, or another formalism:

- no protected action before authorization;
- no stale policy epoch after critical revocation;
- no model route without endpoint lease;
- no duplicate non-idempotent action after resume;
- profile-message separation.

Formal model checking would strengthen claims but should be reported separately from implementation testing.

### 21.3 Hardware-backed optional attestation

Define optional peer/PIA attestation extensions for customers with TPM, Secure Enclave, confidential-computing, or GPU-attestation requirements without making such hardware mandatory for ordinary enterprise use.

### 21.4 Post-quantum profile

Once deployment-ready standardized suites and libraries are stable, define a tested PAPER post-quantum/hybrid cryptographic profile using registered TLS and COSE algorithms rather than private constructions.

### 21.5 External transparency

Map PAPER Evidence Receipts and provenance statements into SCITT transparency services for organizations that need third-party/verifiable append-only publication.

### 21.6 A2A/MCP bridges

Create explicit governed bridge specifications:

- PAPER Tool Exchange → MCP call;
- PAPER external-agent delegation → A2A task;
- provenance linkage back into the PAPER Exchange.

### 21.7 Privacy-preserving governance

Explore whether zero-knowledge proofs, private-set membership, trusted execution, or structured redaction can prove selected policy properties while exposing less source/prompt content to Relay/administrators. Such mechanisms are out of scope for v1.

### 21.8 Live voice and richer collaboration

The current release boundary treats voice/media as governed runtime profiles. Further work may evaluate real-time voice/video performance and interoperability while preserving enterprise authorization, recording policy, QoS, and provenance without turning PAPER into a general-purpose conferencing protocol.

### 21.9 Cross-organization federation

The DARI evolution plan defines federation as an executable profile with explicit trust negotiation, policy intersection, provenance disclosure rules, and model/data residency constraints. Further work is limited to deployment-scale interoperability and performance evaluation after those gates pass.

---

## 22. Reproducibility and Open-Source Plan

A research artifact release should contain:

```text
paper-protocol/
├── spec/
├── schema/cddl/
├── registry/
├── rust/
├── go/
├── reference/relay/
├── reference/pia/
├── adapters/vllm/
├── adapters/sglang/
├── conformance/
├── fuzz/
├── vectors/
├── benchmarks/
│   ├── connection/
│   ├── inference/
│   ├── scale/
│   ├── collaboration/
│   └── provenance/
└── datasets/
    └── provenance-evolution/
```

Each empirical paper revision should publish:

- exact commit hashes;
- build instructions;
- Docker/container manifests where licenses allow;
- benchmark configuration;
- synthetic/test data;
- raw results;
- analysis scripts;
- failures and excluded runs with rationale.

Model weights need not be redistributed if licensing prohibits it, but exact model IDs/digests, serving settings, and instructions for reproducing with an accessible substitute should be documented.

The conformance suite should be usable by independent implementations and should not require Patty cloud services.

---

## 23. Conclusion

AI engineering systems are acquiring authority that conventional model APIs were not designed to represent. A secure channel can protect bytes in transit, and an LLM gateway can route or inspect requests, but neither automatically establishes the causal relationship between a human's authorized intent, the policy state in force, the context disclosed to a model, the model artifact used, the tools that executed, and the code that ultimately survived into source control.

PAPER proposes that this relationship should be represented directly in the communication substrate.

The protocol's central unit is a **Governed Exchange**: an authenticated, purpose-bound interaction constrained by a short-lived **Capability Lease**, evaluated under an immutable **Policy Epoch**, mediated by inline **Relay Verdicts**, and linked into a content-addressed **Provenance Spine** with signed **Evidence Receipts**. A separate Relay data plane allows enforcement to scale without turning the Control Plane into the token proxy. A registered PIA and signed model-package/endpoint state make model authorization stronger than a model-name string. The same substrate can carry collaboration and administrative traffic while requiring explicit capability and context transitions so that chat, voice, files, and broadcasts do not become accidental privilege channels.

PAPER intentionally relies on standard cryptographic building blocks—TLS 1.3, QUIC, CBOR, COSE, HPKE, MLS, and existing transparency/provenance ecosystems—because the research question is not whether another encryption primitive can be invented. The question is whether **authority, governance, and causal provenance can become enforceable properties of live human–AI software-engineering communication with acceptable cost and operational complexity**.

The answer should ultimately be empirical. The next step is therefore not stronger marketing language but an interoperable open implementation, independent security review, rigorous stateful fuzzing, provenance-fidelity experiments, and measured comparison with conventional model/gateway architectures. If those results support the design, PAPER could provide a useful protocol layer between increasingly capable AI Harnesses and the enterprise or sovereign infrastructure expected to govern them.

---

# References

[1] E. Rescorla. **The Transport Layer Security (TLS) Protocol Version 1.3.** RFC 9846, IETF, July 2026. https://www.rfc-editor.org/info/rfc9846/

[2] J. Iyengar and M. Thomson. **QUIC: A UDP-Based Multiplexed and Secure Transport.** RFC 9000, IETF, May 2021. https://www.rfc-editor.org/info/rfc9000/

[3] M. Thomson and S. Turner. **Using TLS to Secure QUIC.** RFC 9001, IETF, May 2021. https://www.rfc-editor.org/info/rfc9001/

[4] S. Friedl, A. Popov, A. Langley, and E. Stephan. **Transport Layer Security (TLS) Application-Layer Protocol Negotiation Extension.** RFC 7301, IETF, July 2014. https://www.rfc-editor.org/info/rfc7301/

[5] S. Whited. **Channel Bindings for TLS 1.3.** RFC 9266, IETF, July 2022. https://www.rfc-editor.org/info/rfc9266/

[6] C. Bormann and P. Hoffman. **Concise Binary Object Representation (CBOR).** RFC 8949 / STD 94, IETF, December 2020. https://www.rfc-editor.org/info/rfc8949/

[7] J. Schaad. **CBOR Object Signing and Encryption (COSE): Structures and Process.** RFC 9052 / STD 96, IETF, August 2022. https://www.rfc-editor.org/info/rfc9052/

[8] R. Barnes, K. Bhargavan, and C. A. Wood. **Hybrid Public Key Encryption.** RFC 9180, IETF, February 2022. https://www.rfc-editor.org/info/rfc9180/

[9] R. Barnes, B. Beurdouche, R. Robert, J. Millican, E. Omara, and K. Cohn-Gordon. **The Messaging Layer Security (MLS) Protocol.** RFC 9420, IETF, July 2023. https://www.rfc-editor.org/info/rfc9420/

[10] O. Steele, H. Birkholz, A. Delignat-Lavaud, and C. Fournet. **CBOR Object Signing and Encryption (COSE) Receipts.** RFC 9942, IETF, June 2026. https://www.rfc-editor.org/info/rfc9942/

[11] H. Birkholz et al. **An Architecture for Trustworthy and Transparent Digital Supply Chains.** RFC 9943, IETF, 2026. https://www.rfc-editor.org/info/rfc9943/

[12] D. Stebila, S. Fluhrer, and S. Gueron. **Hybrid Key Exchange in TLS 1.3.** RFC 9954, IETF, July 2026. https://www.rfc-editor.org/info/rfc9954/

[13] IETF. **ML-DSA for JOSE and COSE.** RFC 9964, 2026. https://www.rfc-editor.org/info/rfc9964/

[14] Model Context Protocol Project. **The 2026-07-28 MCP Specification.** July 28, 2026. https://blog.modelcontextprotocol.io/posts/2026-07-28/

[15] A2A Project. **Agent2Agent (A2A) Protocol Specification, Version 1.0.0.** 2026. https://a2a-protocol.org/latest/specification/

[16] SPIFFE Project. **Secure Production Identity Framework for Everyone (SPIFFE) Standard.** https://spiffe.io/docs/latest/spiffe-specs/spiffe/

[17] in-toto Project. **in-toto Specification v1.0.** https://in-toto.io/docs/specs/

[18] SLSA Project. **SLSA Specification v1.2 and Provenance.** 2026. https://slsa.dev/spec/v1.2/

[19] L. Fan, Z. Li, Y. Tian, Y. Wang, R. Li, and X. Wang. **The Granularity Mismatch in Agent Security: Argument-Level Provenance Solves Enforcement and Isolates the LLM Reasoning Bottleneck.** arXiv:2605.11039, 2026. https://arxiv.org/abs/2605.11039

[20] C. L. Wang, T. Singhal, A. Kelkar, and J. Tuo. **MI9 — Agent Intelligence Protocol: Runtime Governance for Agentic AI Systems.** arXiv:2508.03858, 2025. https://arxiv.org/abs/2508.03858

[21] N. Li, K. Zhang, K. Polley, and J. Ma. **Security Considerations for Artificial Intelligence Agents.** arXiv:2603.12230, 2026. https://arxiv.org/abs/2603.12230

[22] L. Sander, H. K. Gidey, A. Lenz, and A. Knoll. **A Technical Taxonomy of LLM Agent Communication Protocols.** arXiv:2606.19135, 2026. https://arxiv.org/abs/2606.19135

[23] E. Debenedetti, J. Zhang, M. Balunović, L. Beurer-Kellner, M. Fischer, and F. Tramèr. **AgentDojo: A Dynamic Environment to Evaluate Prompt Injection Attacks and Defenses for LLM Agents.** arXiv:2406.13352, 2024. https://arxiv.org/abs/2406.13352

[24] D. Hofer, E. Debenedetti, and F. Tramèr. **Assessing Automated Prompt Injection Attacks in Agentic Environments.** arXiv:2606.10525, 2026. https://arxiv.org/abs/2606.10525

[25] S. Zhong, S. Noei, Y. Zou, and B. Adams. **Human-AI Synergy in Agentic Code Review.** arXiv:2603.15911, 2026. https://arxiv.org/abs/2603.15911

---

# Appendix A. PAPER Research Invariants

The following invariants are candidates for executable conformance tests and later model checking.

**I1 — Peer authentication.** A protected action cannot execute before the peer is authenticated for the message profile.

**I2 — User binding.** A human-scoped Harness action cannot execute before a valid user binding exists.

**I3 — Lease confinement.** An exchange cannot exceed the effective Capability Lease.

**I4 — Policy consistency.** A Relay Verdict identifies the exact Policy Epoch under which it was evaluated.

**I5 — Model binding.** Inference cannot route to a model package/endpoint without current approval and Endpoint Lease.

**I6 — Model non-authority.** Model output/tool proposal cannot modify a Capability Lease or administrative policy.

**I7 — Explicit context transition.** Chat/file/voice content cannot become AI context without a governed Context Exchange.

**I8 — Transport equivalence.** QUIC→TCP fallback does not broaden capabilities or drop evidence requirements.

**I9 — Side-effect safety.** Resume/retry cannot automatically duplicate a completed non-idempotent action.

**I10 — Evidence finality.** A protected successful completion must produce durable evidence or fail explicitly according to policy.

**I11 — Causal integrity.** Modifying a canonical provenance node changes its digest and invalidates descendant/receipt verification.

**I12 — Authority separation.** Broadcast permission does not grant administrative-directive permission.

---

# Appendix B. Example Governed Exchange

The following is a human-readable representation, not the normative wire encoding.

```yaml
exchange_id: ex_7f7e...
kind: ai_inference
session_id: ses_9c10...
organization: acme-kr
user: user-kim-minsu
harness: harness-42f1
purpose: "Refactor JWT validation and add expired-token tests"
policy_epoch: pe_192
capability_lease: lease_481a
repository:
  id: repo-auth
  branch: feature/jwt
  base_commit: 8fd21c7
requested_model:
  class: patty-coder-enterprise
protection: relay_inspectable
context:
  manifest: ctx_18
  items:
    - src/auth/JwtProvider.java
    - src/auth/AuthFilter.java
    - tests/auth/JwtProviderTest.java
verdicts:
  - rule: SECRET_CONTEXT
    result: allow_transform
  - rule: AUTH_CODE_REVIEW
    result: allow_with_obligation
    obligation: two_reviewer_before_merge
provenance_parents:
  - sha256:...
terminal_receipt: sha256:...
```

A generic model endpoint cannot treat this as an ordinary completion request without implementing the PAPER identity, state, lease, policy, and evidence model.

---

# Appendix C. Paper-to-Spec Traceability

| Research concept | Protocol-spec mechanism |
|---|---|
| Governed Exchange | `EXCHANGE_OPEN`, exchange state machine |
| Bounded authority | Capability Lease |
| Immutable policy state | Policy Epoch |
| Inline enforcement | Relay Verdict |
| Proof-carrying lane | Stream Contract + lease/epoch refs |
| Causal provenance | Provenance Spine |
| Ordered exchange evidence | Evidence hash chain |
| Verifiable completion | COSE Evidence Receipt |
| Model artifact identity | Patty Model Package |
| Endpoint authority | Endpoint Lease + PIA credential |
| Transport replay binding | RFC 9266 `tls-exporter` in PAPER auth context |
| Collaboration separation | explicit Chat/Voice/File extensions and Context Attach |
| Emergency control | Admin Directive distinct from Broadcast |
| Sovereign operation | local trust/CP/Relay/PIA + offline bundles |

---

# Appendix D. Pre-Submission Checklist

Before submitting this manuscript to arXiv:

1. Use the approved author list and affiliations in the canonical DARI LaTeX source; this historical Markdown copy is not the submission source.
2. Run a fresh literature and standards search for overlapping 2026 work.
3. Verify all RFC publication metadata and current errata.
4. Publish or link the exact PAPER specification revision discussed in the paper.
5. Mark which protocol components have actually been implemented.
6. If empirical results are added, publish complete methodology and raw data where possible.
7. Do not retain future-tense security claims in the Results section.
8. Clearly label optional versus implemented payload-protection profiles.
9. Have external security/protocol reviewers challenge the threat model and novelty statement.
10. Verify that the `PAPER`/expanded project name does not create an avoidable protocol/project naming collision.
11. Add figures generated from the exact architecture/state machines rather than marketing diagrams.
12. Convert reference URLs into the target LaTeX/BibTeX format for final arXiv submission.
13. Ensure open-source license and contributor/governance documents are published.
14. Add artifact DOI/archive link if a reproducibility artifact is released.
