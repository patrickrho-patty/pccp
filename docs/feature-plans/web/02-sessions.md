# 02 — AI 세션 · Sessions (`web/src/pages/Sessions.tsx`)

> Vertical read: component → `api.ts` → `server.go` session handlers → `identity.OpenSession/CloseSession` → `models/project.go:59 Session`.

## What this page actually is
Admin-operated view of **governed AI coding sessions** — the `Session` entity that binds a developer (`user_id`) + harness (`harness_id`) + project/repo/branch + model to a PAPER working session (`session_id`). It's where an admin opens/pauses/resumes/closes sessions and inspects what happened (timeline, changesets, findings, conversation). Sessions are the spine of provenance (§19) and live ops (§14).

## Current vertical (what exists)
| Layer | Reality |
|---|---|
| Component | table + live-card view, create form, pause/resume/close, inspector modal (timeline/changesets/findings/conversation), provenance link, 10s auto-refresh |
| API (`api.ts:69`) | `GET/POST /api/sessions`, `POST /sessions/{id}/{close,pause,resume}`, `GET /sessions/{id}/{provenance,usage,timeline,exchanges}` |
| `Session` model (`project.go:59`) | has `PolicyEpochID`, `LeaseID`, `BaselineID`, `ProtectionProfile`(default P0), `SessionTTL`, `IdleTTL`, `ModelClass`, status `{pending,active,idle,closed,terminated}` |
| `identity.OpenSession` (273) | creates a row with generated `session_id`, status `active`, **hardcoded `ProtectionProfile="P0"`, hardcoded TTLs (8h/30m)**, **`PolicyEpochID`/`LeaseID`/`BaselineID` left empty** |
| `handleOpenSession` (frontend) | silently defaults `organization_id`/`harness_id` to `sessions[0]`/`harnesses[0]` — mis-binds if lists empty/reorder |
| Close/pause/resume | flip `Status`; **no propagation to the live harness/relay** — closing a session here doesn't kill the live PAPER session |
| Inspector fetches | usage/timeline/exchanges each fetched separately on expand — redundant; provenance/usage are empty until `RecordUsage`/evidence are wired |

## Gaps — grounded

### A. Modeled-but-unwired (wire the existing schema)
**A1. Policy epoch + lease not bound on open.** `Session.PolicyEpochID`/`LeaseID` exist but `OpenSession` never sets them. *Fix:* at open, resolve the org's active `PolicyEpoch` (`policy.GetActiveEpoch`) + issue/attach a `CapabilityLease` (`policy.IssueCapabilityLease`), store both on the session; refuse open if none/revoked. This is what makes a session "governed."
**A2. Baseline ignored.** `Session.BaselineID` + `RepoBaseline`/`CreateBaseline` exist (§18.3 immutable task baseline). *Fix:* require/record a baseline (commit SHA + tree digest) at open so provenance anchors to exact repo state.
**A3. Protection profile / TTLs hardcoded.** *Fix:* derive `ProtectionProfile` from repo sensitivity (§19) + policy; make TTLs per-org/per-project configurable.
**A4. Idle detection unused.** status has `idle` but nothing transitions active→idle. *Fix:* a job (or relay signal) marks idle past `IdleTTL`, auto-closes past `SessionTTL`.

### B. Genuinely missing
**B1. Live token-stream of an active session** (§14.1) — card view shows status text, not output; depends on PAPER `AI_TOKEN_CHUNK` streaming + SSE fan-out (MISSING_ITEMS X.1) with visibility-level gating (§27).
**B2. Per-exchange policy decision log** (§13) — exchanges carry a `verdict_result` badge but no *why*; needs the pipeline to emit `PolicyDecision` per exchange (MISSING_ITEMS 1.2/F2) once wired.
**B3. Close/pause/resume don't reach the live path** — they flip DB status only. *Fix:* Relay must honor session state (kill stream on close/terminate); today the relay checks nothing.
**B4. No server-side list query** — `handleListSessions` returns all, client slices. *Fix:* `?page=&status=&model=&user=&project=&range=`.
**B5. Inspector is a non-deep-linkable modal** — refresh loses it. *Fix:* `/sessions/:id/inspect` route.
**B6. Replay/fork** (§14.3) — not present; needs provenance reconstruction through the governed pipeline.
**B7. Cost/CLU breakdown** (§10C.4/§29) — tokens shown, no KRW/CLU; needs the rate card + `RecordUsage` wired.
**B8. Visibility-level indicator** (§27 A–D) — an admin can't tell what they're allowed to see; needs `authz` gating + a badge.

## UX improvements (grounded)
1. **Bug:** `<Fragment key={s.id || s.key || i}>` references non-existent `s.key` and an undeclared `i`; it only works because `s.id` short-circuits (`||`), so it would **throw on any id-less row** and is misleading. Use `key={s.id}` and drop the redundant inner `<tr key={s.id}>`.
2. **Bug:** `handleCreate` defaults org/harness to first list element — take from form/auth context, error if absent.
3. Model `<select>` hardcoded (patty-code-standard/pro) → from the active catalog epoch (§10A).
4. Server-side filter/pagination/sort (B4).
5. Deep-linkable inspector (B5).
6. Consolidate redundant expand/inspector/provenance fetches (one `/sessions/{id}/detail`).
7. Bulk close/pause/terminate with selection.
8. Reconcile card vs table (same filters/columns).
9. In-transcript search/filter.
10. Sort by duration/cost/tokens.
11. Patch-based auto-refresh (10s full-reload flickers) → SSE/diff.
12. Deep links to specific user/harness/repo (Users F8 etc.).
13. Favorites/recent + timeline scrubber for long sessions.
14. Status legend (🟢🟡⚪🔴✅).
15. Keyboard shortcuts (inspect/close).

## Sequencing
Phase 1 (correctness): bugs #1/#2, server query (B4), deep-link inspector (B5), consolidate fetches (#6).
Phase 2 (governance binding — only meaningful with the wired pipeline): A1 epoch/lease, A2 baseline, B2 decision log, B3 live propagation.
Phase 3 (live/advanced): B1 live stream, B6 replay/fork, B7 cost, B8 visibility.
