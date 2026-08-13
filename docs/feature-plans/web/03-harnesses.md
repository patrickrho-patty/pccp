# 03 — 하네스 · Harnesses (`web/src/pages/Harnesses.tsx`)

> Vertical read: component → `api.ts:56` → `server.go` harness handlers → `identity.EnrollHarness/RevokeHarness` (95/192) → `models/identity.go` Harness/Device/EnrollmentCode. Cross-checked against the live auth path (`relay/paper_listener.go handleConn`, `patty-code-pccp/internal/provider/paper/paper.go`).

## What this page actually is
Admin management of **enrolled Patty Code Harness instances** (`Harness` entity) — the developer's coding agent as a governed PAPER peer. Each harness has a device, an ed25519 key pair, a signed PeerCredential (PPC), a binding to one or more developers (`AllowedUsers`), and a lifecycle (pending→enrolled→active→quarantined→revoked). Enrollment is the moment a developer's machine becomes a governed peer (§8.4).

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | list (client FilterBar + slice), enroll form (user, harness_id, public_key_hex, binary_version), expandable row; revoke/quarantine/reactivate actions |
| `Harness` model | `DeviceID`, `AllowedUsers` (JSON user-id array), `PublicKey`, `CredentialJSON` (the signed PPC), `BinaryHash`, `BuildChannel`, `PolicyProfile`, `LicenseState`, `RiskState`, `LastHeartbeat`, `LastAttestation`, `EnrollmentMode`. **No direct `user_id` field** — binding is `AllowedUsers` + `Device.UserID` |
| `EnrollHarness` (95) | **Real & substantial:** validates ed25519 pubkey (hex+size), creates `Device`, **issues a signed PPC via `s.ca.Issue` (90-day)**, creates `Harness` with `CredentialJSON`+`AllowedUsers=[userID]`, records audit. Returns `{harness, credential}` |
| `RevokeHarness` (192) | sets `status=revoked`+reason, **records audit**; **does NOT revoke the PPC in the CA revocation list, does NOT terminate sessions** |
| `handleQuarantineHarness` (1843) | sets `status=quarantined`+`risk_state=high`, **terminates active sessions** (DB-level); no live propagation |
| `handleReactivateHarness` (1860) | flips back to enrolled/normal; no PPC re-issue |
| Live auth (cross-ref) | harness `provider/paper` sends **hardcoded** `[]byte("patty-harness-credential")`/`Signature`; Relay `handleConn` **accepts any proof** ("Phase 0: accept any valid PPC format") |

## Gaps — grounded

### A. The core trust disconnect (highest severity)
**A1. Issued PPC is never presented or verified.** `EnrollHarness` mints a real signed credential stored in `harness.CredentialJSON`, but (a) the harness PAPER client sends hardcoded strings instead of the PPC, and (b) the Relay never verifies the proof. So enrollment is theater — any peer speaking PAPER framing authenticates. *Fix:* harness `provider/paper` must load + present the enrolled PPC (COSE-Sign1 over the HELLO/HELLO_ACK transcript); Relay must verify signature against the PCCP CA, check the CA revocation epoch, and reject on mismatch. Note: `identity.VerifyHarnessAuth` (signature-verify logic) already exists but the `relay` package doesn't import `identity`, so it's unwired — the fix is largely *wire existing logic*, not new crypto. Without this, §8.4/§9.6 and the entire identity thesis fail.

### B. Modeled-but-unwired
**B1. Revocation doesn't revoke the credential.** `RevokeHarness` flips status but never calls a CA revoke (no `s.ca.Revoke`) and doesn't terminate sessions (quarantine does, revoke doesn't — inconsistent). *Fix:* revoke = mark PPC revoked in CA revocation list + terminate sessions + propagate to relay (reject next connect).
**B2. `LastHeartbeat`/`LastAttestation` never updated.** Fields exist; nothing writes them (no heartbeat/attestation channel from harness). *Fix:* harness heartbeat over PAPER → updates + stale detection.
**B3. `EnrollmentCode` unused on this page.** `identity.GenerateEnrollmentCode` exists; admin-pastes-raw-pubkey is the only flow. *Fix:* issue a one-time enrollment code; the harness enrolls itself using it (no admin key-paste).
**B4. Seat enforcement.** `Organization.MaxHarnessSeats` exists; enrollment doesn't check it. *Fix:* block enroll at the limit.

### C. Genuinely missing
**C1. No harness detail page** (`handleGetHarness` exists at 667, unused) — `/harnesses/:id` with device, cred info (issuer/validity/revocation status), allowed users, sessions, attestation history, audit.
**C2. No forced-version / release-ring enforcement** (§33.10) — `BuildChannel` stored but not checked against a min version; vulnerable builds aren't blocked.
**C3. No live propagation** — revoke/quarantine change DB only; a live harness keeps its connection until the Relay enforces state (depends on A1 + the wired pipeline).
**C4. No server-side list query** — `handleListHarnesses` returns all, client slices.
**C5. No attestation view** — device posture/attestation (`Device.MDMPosture`, harness attestation) not surfaced.

## UX improvements (grounded in code)
1. **Bug:** `getUser(h.user_id || h.allowed_users)` — `user_id` doesn't exist on `Harness` and `allowed_users` is a JSON-array string, so lookup **always fails → user column always "-".** Parse `allowed_users` (or add a helper) to resolve the developer.
2. **Bug:** `<Fragment key={h.id || h.key || i}>` — `i` undefined → key warnings.
3. **Bug:** field-name mismatches in the expand panel — `h.build_hash` (model `binary_hash`) and `h.release_channel` (model `build_channel`) **always render "-"**.
4. **Bug:** `handleEnroll` silently defaults `organization_id` to `harnesses[0]`/`users[0]` — take from auth context; error if absent.
5. Enroll form requires pasting a raw ed25519 hex key (internal knowledge) → use enrollment-code flow (B3); validate key format with feedback.
6. Device fields hardcoded (`dev-machine`/`darwin`/`arm64`) — the harness should supply real device facts.
7. No detail page (C1); name/harness-id not deep-linkable.
8. Revoke vs quarantine inconsistency (B1) — revoke should also terminate sessions + show reason; require a reason modal.
9. Sessions column link goes to generic `/sessions`, not filtered to this harness.
10. No filter by version/ring/risk/user (after C1/B2).
11. No bulk revoke/quarantine.
12. No column sort/toggle/saved views; no favorites.
13. No skeleton/transition; no empty-state ("첫 하네스 등록" → enrollment code).
14. No left sub-menu (Harnesses / Devices / Enrollment / Attestation).
15. No stale-harness indicator (uses `LastHeartbeat` once B2 wired).
16. No responsive layout.

## Sequencing
Phase 1 (correctness): UX bugs #1–#4 (user column, keys, field names, org default) — the page is currently showing wrong/no data.
Phase 2 (trust): A1 (real PPC present+verify) — the product-defining fix; coordinate with the harness + relay plans.
Phase 3 (lifecycle enforcement): B1/B2 (cred revocation, heartbeat), C2 (forced version), C3 (live propagation).
Phase 4 (ops): C1 detail page, C4 server query, B3/B4 (enrollment codes, seats), C5 attestation.
