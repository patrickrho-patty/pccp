# DARI Compatibility and Profile Map

**Status:** Normative Phase 2 contract
**Protocol:** DARI — Delegated Authorization and Receipts for Inference
**Companion specification:** `PAPER_Protocol_Specification_v1.0.md`, Appendix F
**Implementation status:** Runtime gates specified; this document does not claim that the current checkout has passed them or provide a deployment measurement

## 1. Purpose and precedence

This document fixes the boundary between the bounded `paper/1` compatibility profile, the active `dari/1` kernel, and DARI extension profiles. It is normative for profile negotiation and compatibility. Appendix F of the protocol specification is normative for schemas, signatures, validation algorithms, evidence, and effects. If older PAPER prose conflicts with either document for a DARI connection, Appendix F and this map take precedence.

An implementation MUST select exactly one kernel profile for a connection: `paper/1` or `dari/1`. The two kernels MUST NOT be blended object by object. Every `dari.*` extension depends on `dari/1` and MUST NOT be negotiated with `paper/1`.

## 2. Exact profile registry

Profile identifiers are case-sensitive ASCII strings.

| Profile | Status | Normative boundary | Dependencies |
|---|---|---|---|
| `paper/1` | Bounded legacy compatibility | Frozen PAPER v1 preface, record/message encoding, numeric message labels, and explicitly documented legacy objects | None |
| `dari/1` | Active kernel | Peer Credential, Authorization Grant, Governed Exchange, Authorization Decision, Signed State Checkpoint, Evidence Receipt, validation order, and failure semantics | None |
| `dari.ai/1` | Active extension contract | Provider-neutral inference request/response streaming, usage, cancellation, and model/endpoint binding | `dari/1`; `dari.model-supply/1` when a model or endpoint is selected |
| `dari.tools/1` | Active extension contract | Transactional effects and tool bridges, including prepare/authorize/commit/abort/status | `dari/1` |
| `dari.model-supply/1` | Active extension contract | Model Artifact Manifest and Endpoint Authorization | `dari/1` |
| `dari.web/1` | Runtime profile | WebTransport/HTTP/3 browser binding, constrained WebSocket fallback, origin/proof binding, reconnect, and unchanged DARI authorization and receipt semantics | `dari/1`; implementation gate Task 13 |
| `dari.federation/1` | Runtime profile | Issuer, audience, trust domain, policy intersection, residency constraints, trust-bundle freshness, and receipt keys | `dari/1`; implementation gate Task 14 |
| `dari.collab/1` | Runtime profile | Governed chat, presence, broadcasts, encrypted delivery, and resumable file transfer | `dari/1`; implementation gate Task 18 |
| `dari.media/1` | Runtime profile | Governed live-media/voice streams with explicit authorization, cancellation, usage, and receipts | `dari/1`; implementation gate Task 18 |

“Active extension contract” means the normative behavior is frozen for implementation and testing. It does not mean that the current runtime already implements or conforms to it.

## 3. Negotiation

Each profile offer is the tuple `(profile ID, critical flag, capability set)`. Each result is exactly one of:

- `EXACT` — every mandatory behavior and every offered critical capability is implemented and conformant.
- `DEGRADED` — the profile is usable only after the result enumerates a non-empty set of omitted optional capabilities. The omitted set MUST contain no mandatory or critical capability.
- `UNSUPPORTED` — the runtime cannot provide the profile's mandatory behavior.

The responder MUST return one result for every offer and MUST NOT return a result for an unoffered profile. Duplicate or contradictory offers are invalid. A critical offer that resolves to `UNSUPPORTED`, or to `DEGRADED` with any critical capability omitted, MUST fail negotiation before authentication or protected data transfer. A non-critical unsupported offer MAY be omitted only after returning `UNSUPPORTED` explicitly.

`DEGRADED` MUST NOT weaken authentication, subject-key binding, grant attenuation, deny-overrides, state freshness or rollback, required obligations, receipt truthfulness, attestation scope, evidence verification, or transactional-effect replay protection. A transport change MUST NOT change those semantics.

Until the runtime behavior, deployment evidence, and conformance suite for a profile exist, that profile MUST produce `UNSUPPORTED`. Schema parsing, generated types, documentation, a feature flag, or an experimental adapter is not sufficient to return `EXACT` or `DEGRADED`. The implementation plan assigns those runtime gates to Tasks 13, 14, and 18; this document does not permit a schema-only release.

## 4. Kernel and extension ownership

| Object or behavior | Owning profile | Other profiles may reference it? |
|---|---|---|
| Peer Credential and transcript proof of possession | `dari/1` | Yes; MUST NOT redefine it |
| Authorization Grant and attenuation | `dari/1` | Yes; MUST NOT broaden it |
| Governed Exchange | `dari/1` | Yes; extensions bind their actions to it |
| Authorization Decision and obligations | `dari/1` | Yes; extensions may register obligation kinds |
| Signed State Checkpoint | `dari/1` | Yes; extensions may register state classes |
| Evidence Receipt, Receipt Attestation, and selective disclosure | `dari/1` | Yes; extensions may register evidence event and claim classes |
| Inference request/stream/result/usage/cancel | `dari.ai/1` | Kernel validates the enclosing authority and evidence |
| Effect Prepare, Effect Authorization, Effect Result, and status | `dari.tools/1` | Kernel validates decisions and records evidence |
| Model Artifact Manifest and Endpoint Authorization | `dari.model-supply/1` | `dari.ai/1` binds selected artifacts/endpoints by digest |
| Browser origin and reconnect binding | `dari.web/1` | MUST preserve kernel semantics; runtime uses WebTransport/HTTP/3 with a constrained WebSocket fallback |
| Cross-domain trust and policy-intersection binding | `dari.federation/1` | MUST preserve kernel semantics; runtime validates bilateral trust, residency, and receipts |
| Collaboration and encrypted delivery | `dari.collab/1` | MUST preserve kernel semantics; runtime emits ordered evidence |
| Live media and voice streams | `dari.media/1` | MUST preserve kernel semantics; runtime reports usage, cancellation, and terminal receipts |

An extension MAY add only labels inside the kernel object's extension map and MUST mark any security-critical extension label as critical. An extension MUST NOT reinterpret a registered core label, outcome, state, signature input, digest, or message ID.

## 5. Stable wire and object allocations

Message-type and object-type registries are independent. Existing legacy message types MUST NOT be renumbered. New DARI transactional-effect messages use the unused `0x0610` subfamily:

| Message constant | Value | Profile |
|---|---:|---|
| `EFFECT_PREPARE` | `0x0610` | `dari.tools/1` |
| `EFFECT_AUTHORIZE` | `0x0611` | `dari.tools/1` |
| `EFFECT_COMMIT` | `0x0612` | `dari.tools/1` |
| `EFFECT_ABORT` | `0x0613` | `dari.tools/1` |
| `EFFECT_STATUS` | `0x0614` | `dari.tools/1` |

Values `0x0604` through `0x0606` MUST NOT be allocated: the legacy specification already claims them. Values `0x0610` through `0x0614` MUST be rejected as unsupported unless `dari.tools/1` is negotiated.

The DARI object-type allocation is:

| Object | Value |
|---|---:|
| Peer Credential | `0x0100` |
| Authorization Grant | `0x0202` |
| Evidence Receipt body | `0x0302` |
| Governed Exchange | `0x0303` |
| Authorization Decision | `0x0304` |
| Signed State Checkpoint | `0x0305` |
| Effect Prepare | `0x0610` |
| Effect Authorization | `0x0611` |
| Effect Result | `0x0612` |
| Effect Status response | `0x0613` |
| Receipt Attestation | `0x0703` |
| Selective Disclosure Proof | `0x0704` |

The shared numeric values in the two registries do not imply the same encoded type. Implementations MUST select the registry from the field being decoded and MUST NOT infer it from the number alone.

## 6. `paper/1` bounded legacy compatibility

`paper/1` preserves the existing PAPER v1 preface, record major, message type, integer-key CBOR payload layouts, and frozen cryptographic byte inputs. It exists to keep deployed peers interoperable during migration. It is not an alias for `dari/1`.

The bounded legacy contract has these rules:

1. A `paper/1` sender MUST use the legacy preface `50 41 50 45 52 00 01 0a`, record major `1`, and existing message numbers. A receiver MUST NOT renumber a legacy message according to an older prose table when the deployed registry and byte fixtures use another number.
2. Task 5 golden vectors for the root protocol package are the byte-level compatibility oracle for preface, framing, legacy map-form COSE, Peer Credential signing bytes, Capability Lease signing bytes, object-digest quirks, and Evidence Receipt signing bytes. A future implementation change MUST reproduce those vectors in `paper/1` or reject the legacy object explicitly.
3. The canonical repository message constants are the allocation source for existing root messages; `registry/messages.csv` corroborates them except where the two are explicitly known to drift. The older Appendix 76 table is descriptive where it conflicts; it MUST NOT renumber deployed values. Task 5 MUST freeze ambiguous legacy names or omissions by numeric value and fixture. The additive DARI allocations in Section 5 are normative.
4. A decoder MUST select `paper/1` before parsing a legacy object. A `dari/1` parse or validation failure MUST NOT cause a retry through the legacy decoder.
5. A legacy object MUST retain its original bytes for signature and digest verification. An adapter MAY construct a separate DARI in-memory view, but it MUST NOT present that view as the original signed object.
6. Successful `paper/1` verification proves only the properties actually covered by the legacy bytes. It MUST NOT be reported as proof of DARI delegation, full-scope signing, state freshness, rollback protection, multi-party attestation, selective disclosure, or exactly-once effects.
7. A security check that does not alter legacy bytes—such as comparing a verified COSE payload with the presented model or rejecting a revoked key—MUST still be enforced. Compatibility does not require preserving an acceptance bug.

The existing semantic carriers remain stable during migration:

| Meaning | Existing message | Value | DARI use |
|---|---|---:|---|
| Grant presented to a session | `SESSION_GRANT` | `0x0201` | Carries a DARI Authorization Grant only on a `dari/1` connection |
| Grant issued | `LEASE_ISSUE` | `0x0210` | Carries a DARI Authorization Grant only on a `dari/1` connection |
| Authorization Decision | `RELAY_VERDICT` | `0x0304` | DARI body/signature rules replace legacy verdict semantics |
| Evidence Receipt | `EVIDENCE_RECEIPT` | `0x0307` | DARI receipt body/attestation rules replace the legacy shape |

Using a shared carrier number does not make the payloads wire-identical. The negotiated kernel determines the payload schema.

## 7. Explicit legacy adapters

An implementation MAY expose explicit one-way adapters at the `paper/1` boundary. Every adapter MUST fail closed when a required DARI property cannot be derived.

| Legacy input | Permitted internal mapping | Mandatory restriction |
|---|---|---|
| Legacy Peer Credential | Peer identity and confirmation key | Verify issuer signature, exact signed-payload equality, time, revocation, and transport proof of possession; retain original signed bytes |
| Capability Lease | Non-delegable Authorization Grant view | Signatures and scope fields not covered by legacy bytes MUST be revalidated from authoritative state; depth is zero; audience/session/epoch are exact; no new authority may be inferred |
| Relay verdict `ALLOW` / `DENY` | Matching DARI outcome | Bind exchange, action, grant, policy checkpoint, evaluator, and expiry or reject |
| Legacy transform or approval verdict | `ALLOW_WITH_OBLIGATIONS` | Encode every condition as a pending/satisfied obligation with evidence; otherwise map to `DENY` |
| Legacy quarantine, terminate, isolate, or incident verdict | `DENY` plus evidence | MUST NOT be translated to unqualified `ALLOW` |
| Legacy Evidence Receipt | Verifiable legacy artifact | MUST NOT be relabeled as a DARI Evidence Receipt or multi-party attestation |
| Legacy tool proposal/execute/result | Legacy operation only | MUST NOT be relabeled as a DARI transactional effect or exactly-once result |
| Unsigned or stale epoch/catalog state | No DARI state | Protected DARI transition MUST fail until a valid Signed State Checkpoint exists |

An adapter that creates a new signed DARI object acts as a new issuer and MUST record the legacy source digest and conversion policy in evidence. It MUST NOT claim that the legacy signer signed the new fields.

## 8. Provider-neutral inference and model supply

`dari.ai/1` defines inference in terms of request digest, ordered input, streaming output, usage, cancellation, selected model-artifact digest, selected endpoint-authorization digest, Authorization Grant, Authorization Decision, and Evidence Receipt. It MUST NOT require a named model vendor, serving engine, SDK, or product component. An adapter MAY translate these objects to a local provider API, but provider-specific fields belong in a non-critical extension unless policy makes them critical.

When a request selects a model or endpoint, `dari.ai/1` MUST also negotiate `dari.model-supply/1`. The selected Model Artifact Manifest and Endpoint Authorization MUST be covered by fresh Signed State Checkpoints and committed by digest in the decision and receipt. A model name, endpoint URL, or configured database row without those signed bindings is insufficient.

## 9. Tool bridges and effects

`dari.tools/1` owns the five message allocations in Section 5 and the schemas/state machine in Appendix F. A bridge MAY expose a provider-specific tool or runtime, but it MUST bind the neutral operation ID, nonce, grant digest, input digest, executor, retry owner, decision digest, result digest, and evidence before returning a terminal result.

Legacy propose/authorize/execute/result messages remain `paper/1` operations and do not satisfy `dari.tools/1`. A receiver MUST reject a DARI effect message sent without `dari.tools/1`, and MUST reject a legacy tool message presented as a DARI Effect Prepare.

## 10. Web runtime profile

`dari.web/1` defines the executable browser binding for:

- exact web origin and top-level site binding;
- proof of possession for the browser-held subject key;
- authenticated channel/exporter or equivalent session binding;
- reconnect and status-query behavior that cannot duplicate effects;
- the same Authorization Grant, decision, obligation, freshness, receipt, and attestation semantics as `dari/1`.
- WebTransport over HTTP/3 as the primary carrier, with a deployment-controlled WebSocket fallback carrying the identical canonical DARI envelope;
- durable browser-session state containing origin, subject-key thumbprint, last sequence, grant digest, and effect operation IDs;
- bounded queues, per-origin rate limits, idle expiry, health/readiness, and metrics for proof failure, reconnect conflict, and backpressure.

A web transport MUST NOT treat cookies, origin headers, bearer tokens, or reconnect identifiers alone as proof of possession. A runtime claiming `EXACT` MUST pass the Task 13 browser SDK, transport parity, origin, reconnect, and effect-status vectors. A deployment that has not passed those gates returns `UNSUPPORTED` and records the reason in its capability manifest.

## 11. Federation runtime profile

`dari.federation/1` defines the executable cross-domain binding for:

- credential and grant issuer identity;
- exact audience and trust-domain binding;
- deterministic intersection of local and remote policy, where denial and the narrower authority win;
- residency and routing constraints that cannot be weakened in transit;
- receipt-verification keys and their signed freshness/rollback state.
- signed trust-bundle discovery/import over configured HTTPS/mTLS or sovereign offline media;
- monotonic trust-bundle high-water marks, predecessor checks, maximum staleness, and emergency domain quarantine;
- local and remote decision intersection before forwarding, with the narrower authority and denial winning;
- cross-domain provenance roots and scoped receipt attestations that remain independently verifiable.

Federation MUST NOT treat a remote signature as local authorization. Each trust domain MUST validate the full chain and apply its local policy; the resulting authority is the intersection of all valid grants and policies. Missing trust, state, residency, or receipt-key material fails closed. A runtime claiming `EXACT` MUST pass the Task 14 trust-bundle, issuer/audience, policy-intersection, residency, rollback, offline, and receipt vectors. A deployment that has not passed those gates returns `UNSUPPORTED` and records the reason in its capability manifest.

## 12. Named Patty reference and legacy profile

This section is an informative product mapping and is the only place this map assigns Patty product names. It does not change core DARI terminology or wire semantics.

| Patty component or term | Neutral DARI role/object |
|---|---|
| Patty Code Harness | Application peer using a Peer Credential and Authorization Grant |
| PAPER Relay | Governance Relay |
| Patty Inference Agent (PIA) | Inference Peer |
| Patty Model Package (PMP) | Predecessor/adaptor input to a Model Artifact Manifest |
| Patty runtime/tool service | Effect Executor |
| Patty evidence/provenance verifier | Evidence Verifier |
| vLLM or SGLang adapter | Local inference-provider adapter outside the kernel |

The duplicated Patty client protocol package has known constant and encoder drift from the root protocol package. In particular, close/drain and model-catalog numbers collide, and its CBOR encoder is not the root deterministic mode. Those bytes MUST NOT be auto-detected or silently normalized. Task 5 MUST freeze any supported legacy variant with a named fixture and explicit adapter; an unrecognized variant is `UNSUPPORTED`, not generic `paper/1`.

## 13. Black-box compatibility requirements

A compatibility suite MUST demonstrate all of the following:

1. Frozen `paper/1` golden frames, legacy signatures, and legacy digests remain byte-for-byte stable after DARI code is added.
2. Existing legacy message numbers are unchanged; message values `0x0610` through `0x0614` are unique within the message-type registry and unavailable without `dari.tools/1`; `0x0604` through `0x0606` are not reused.
3. A valid legacy signature over different presented fields is rejected even though the original signed bytes remain unchanged.
4. A converted Capability Lease cannot delegate, widen scope, change audience/session/epoch, or acquire an unsigned budget.
5. A legacy receipt is reported as legacy evidence and never as a DARI multi-party receipt.
6. A DARI object is never accepted after fallback to a legacy decoder.
7. A critical unsupported profile fails negotiation; a non-critical unsupported profile is reported explicitly.
8. `dari.web/1`, `dari.federation/1`, `dari.collab/1`, and `dari.media/1` return `UNSUPPORTED` until their named runtime and conformance gates pass; a schema-only implementation never reports `EXACT` or `DEGRADED`.
9. No product/provider name is required to encode or validate a `dari/1`, `dari.ai/1`, `dari.tools/1`, `dari.model-supply/1`, `dari.web/1`, `dari.federation/1`, `dari.collab/1`, or `dari.media/1` object.

Passing these cases demonstrates compatibility behavior only. Runtime conformance for the DARI kernel additionally requires every negative and positive vector in Appendix F.14.
