# Harness A — Trust & Transport (`patty-code-pccp`)

> Grounded in: `patty.toml` (default `kind="paper"` → Relay), `internal/provider/paper/paper.go` (connect handshake + Stream), `internal/paperproto/{messages,transport,framing,constants}.go`, `internal/workspacelease` (local file-lock, **not** a PAPER lease), and the PCCP side `internal/relay/paper_listener.go handleConn` + `internal/identity` (VerifyHarnessAuth, CA.Issue).

## What exists (honest baseline)
- The harness **defaults to PAPER** (`patty.toml`: `patty-paper`, `localhost:8444`) and ships a real `provider/paper` that does HELLO→HELLO_ACK→AUTH_CHALLENGE→AUTH_PROOF→AUTH_ACK, then `AI_OPEN`/`AI_TOKEN_CHUNK`/`AI_COMPLETE`.
- The PAPER transport library (CBOR/framing/TCP-TLS/QUIC) is real and vendored.
- PCCP mints a real signed PPC at enrollment (`identity.EnrollHarness` → `ca.Issue`, 90-day) and stores it in `harness.CredentialJSON`.

## The gaps (the trust thesis is unenforced)

### A1. The issued PPC is never presented or verified
- Harness `provider/paper` sends **hardcoded** `Credential: []byte("patty-harness-credential")`, `Signature: []byte("patty-harness-signature")` (`paper.go:148-153`, comment: *"simplified for Phase 1… in production, COSE-Sign1 signed PPC"*).
- Relay `handleConn` comment: *"For Phase 0: accept any valid PPC format"* → sends AUTH_ACK without verifying. `identity.VerifyHarnessAuth` exists but the relay doesn't import `identity`.
- **Any peer speaking PAPER framing authenticates.** *Fix (harness):* load the enrolled PPC (delivered via enrollment code flow), sign the HELLO/HELLO_ACK transcript (COSE-Sign1, EdDSA), present it in AUTH_PROOF. *Fix (PCCP):* relay verifies against PCCP CA + revocation epoch; reject on failure.

### A2. PAPER is not enforced as the sole transport
- `openai`/`anthropic` providers coexist; the resolver picks by config name — a user can add an OpenAI provider and bypass governance (§9.2). *Fix:* in enterprise/gov/public builds, refuse non-PAPER providers (build/deployment lock); keep OpenAI-compat as dev-only, feature-flagged.

### A3. No capability-lease lifecycle on the harness
- `workspacelease` is a **local workspace file-lock**, unrelated to PAPER capability leases. The harness has no concept of acquiring/presenting/renewing a PCCP `CapabilityLease`, and no lease/epoch/enroll message types in `paperproto`.
- *Fix:* add `paperproto` messages for ENROLL/LEASE/LEASE_RENEW; harness requests a lease at session open, presents it per exchange, fails closed on expiry/revocation (§8.4).

### A4. No policy-epoch binding
- 0 files. The harness doesn't fetch/bind the active `PolicyEpoch` and can't refuse on stale/revoked epoch (§13.1, §10A.5). *Fix:* harness receives the active epoch (via catalog push or a POLICY message), binds sessions to it, re-binds on change.

### A5. No server-authoritative catalog consumption
- The harness advertises `paper.models/1` in HELLO, but `paperproto` has **no catalog message types** and PCCP never pushes one — so the model list comes from local `patty.toml`, not the server (§10A, §0.1). *Fix:* define + handle `MODEL_CATALOG_SNAPSHOT`; harness uses the server catalog as the model source.

## Implementation notes
- The crypto substrate exists on both sides (ed25519, COSE, CBOR). The work is *wiring* (load PPC, sign transcript, verify, lease/epoch/catalog messages) not new crypto.
- Coordinate with `docs/feature-plans/web/03-harnesses.md` (A1) and the Domain 1–2 relay plan.

## Acceptance
- A harness without a valid, unrevoked PPC cannot complete AUTH; a revoked harness is rejected on next connect; non-PAPER providers are refused in non-dev profiles; sessions carry a bound epoch+lease.
