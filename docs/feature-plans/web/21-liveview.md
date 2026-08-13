# 21 — 실시간 뷰 · LiveView (`web/src/pages/LiveView.tsx`)

> Vertical read: component → `EventSource('/api/realtime/sse')` + polling `/api/sessions|users|harnesses` → **`/api/realtime/sse` is not mounted** → `internal/realtime` (HandleSSE/Broadcast/NotifySessionUpdate exist but unwired). Cross-checked PAPER streaming (Sessions F1).

## What this page actually is
The **live harness/session wall** (§14.1) — a grid of live terminal cards showing real-time model output per active session, clickable to a live inspector. The SOC/SRE "watch what's happening now" surface.

## Current vertical — wired to nothing
| Layer | Reality |
|---|---|
| Component | grid of session cards; opens `EventSource('/api/realtime/sse')` for `session.update`; polling fallback; `connected` indicator; expand → session inspector |
| `/api/realtime/sse` | **route NOT registered** (only `/realtime/status` is mounted) → SSE 404s → `connected=false` → falls back to polling session metadata |
| `realtime` hub | **exists & real** (`HandleSSE`, `HandleWebSocket`, `Broadcast`, `NotifySessionUpdate`, `NotifySecurityFinding`, …) but (a) not mounted at `/sse`, (b) **nothing on the live path calls `NotifySessionUpdate`** → even if mounted, it'd be silent |
| Live token output | absent — requires PAPER `AI_TOKEN_CHUNK` streaming (Sessions F1 / MISSING_ITEMS X.1) which doesn't exist |

➡️ The page shows **static session cards from the DB**, not live output. The "live" claim is unsupported.

## Gaps — grounded
**A. SSE route not mounted + no emitter.** *Fix:* mount `HandleSSE` at `/api/realtime/sse` (and/or WS); have the relay emit `NotifySessionUpdate` (and token chunks) on real session activity.
**B. No live token stream.** Even with SSE connected, there's no real per-token output feed (needs PAPER streaming, §10B.20). *Fix:* relay streams `AI_TOKEN_CHUNK` → SSE → terminal cards.
**C. No live risk/throughput overlay; no surveillance-boundary indicator (§14.4 — show what's visible vs not); no density/layout control; no filter by user/project/model/risk; no pause/follow; no deep-link to a live session.**

## UX improvements (grounded)
1. Cards show static metadata, not live output (A/B).
2. `connected=false` with no user-visible explanation.
3. No density/layout control (3/4/6 cols); no responsive reflow.
4. No filter/search; no favorites/pin.
5. No pause/follow; no timestamp correlation.
6. Back button only — no deep-link to a specific live session.
7. No color-coding by risk; no legend; no aggregate throughput overlay.
8. No empty-state when nothing active; no refresh/pause control.
9. No keyboard nav between cards; no export/snapshot of a stream.

## Intended-features coverage (vs WEB_FEATURE_GAPS §20 — 10 features)
1. Real live harness output → **A/B** ✅
2. Real-time token stream per card → **B** ✅
3. Risk indicator driven by live signals → **C** ✅
4. Grid auto-layout + density control → **C** ✅
5. Click card → live session inspector → **C** ✅
6. Pause/follow a stream → **C** ✅
7. Filter by user/project/model/risk → **C** ✅
8. Live surveillance-boundary indicator (§14.4) → **C** ✅
9. Aggregate throughput overlay → **C** ✅
10. Alert overlay when a session hits a policy → **add**; policy-hit alert overlay.

## Sequencing
Phase 1 (make it live): A (mount SSE + emit session events from relay) — minimal viable "live".
Phase 2 (real output): B (PAPER token streaming → SSE → terminals) — the actual live wall.
Phase 3 (ops): risk/throughput overlay, surveillance boundary, filters/layout, pause/follow, deep-links.
