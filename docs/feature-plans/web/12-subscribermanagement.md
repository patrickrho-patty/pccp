# 12 — 구독자 관리 · SubscriberManagement (`web/src/pages/SubscriberManagement.tsx`)  *(Patty Ops)*

> Vertical read: component → `/api/public/accounts`, `/api/harnesses`, `/api/analytics/usage` → `publiccloud` Account (subscription state + 4 risk dimensions + slot limits) + Harness + Usage.

## What this page actually is
**Public subscriber management** (§6.1 Accounts, §10C) — the Patty Ops view of public-cloud subscribers: plan/subscription/risk/harness usage/tokens. Patty-internal trust/billing/support (not the user portal — that's AccountPortal).

## Current vertical (what exists)
- Subscriber table (subscriber/plan/status/tokens/device/risk/joined) + detail drawer; reads public accounts + harnesses + usage. Account model carries subscription state, 4 risk states, OAuth, slot limits.

## Gaps — grounded
**A1. No payment/subscription state from a real provider** (§29.9 — `PaymentProvider` bare field; SRE shows Payments `not_configured`). *Fix:* provider integration.
**A2. No harness registry + device/OS labels per subscriber** (§6.6). *Fix:* derive/show device/OS + harness binding.
**A3. No graduated-response actions** (§10C.10) — risk dimensions shown but not actionable; detection (which drives them) is dead. *Fix:* graduated-response workflow wired to detection.
**A4. No refund/credit workflow.** *Fix:* refund/credit + entitlement adjustment.
**A5. No fair-use / capacity-lease state per subscriber** (§10C.5). *Fix:* live lease/slot state.
**A6. No abuse case lifecycle** (§10C.11). *Fix:* abuse case queue.
**A7. No support-case linkage.** *Fix:* link support tickets to subscribers.
**A8. No segment/cohort tagging.** *Fix:* segment tagging for ops targeting.
**A9. No approximate geo/device (privacy-safe)** (§6.6). *Fix:* coarse geo/device.
**A10. No bulk comms/broadcast to a segment.** *Fix:* segment broadcast.
**A11. Email send is a stub** (`alert('이메일 발송 기능은…')`).
**A12. Usage from `/api/analytics/usage` — empty unless `RecordUsage` wired.**
**A13. Read-only; no plan edit.**

## UX improvements (grounded)
1. Email send is a stub alert (A11).
2. No edit of subscription/plan (A13); no refund/credit (A4).
3. Tokens/device/risk columns static; no drill-down to subscriber detail/sessions.
4. No filter by plan/status/risk; no search beyond listed.
5. No bulk actions (suspend/notify segment) (A8/A10).
6. No subscriber detail page; joined date raw.
7. No segment/cohort tagging UI (A8).
8. No approximate geo/device (A9).
9. No export; no favorites; no sub-menu (active/delinquent/abuse/support).
10. No empty-state; no responsive layout.
11. Plan change has no workflow.
12. No live-refresh indicator.
13. No favorites/pinning.
14. No loading skeleton.

## Sequencing
Phase 1 (usability): drill-down, filters/search, bulk notify, export, real email (A11), segment tagging (A8).
Phase 2 (commercial): A1 (payment provider), A4 (refund/credit), A2 (device/OS), A9 (geo).
Phase 3 (trust): A3 (graduated response), A5/A6/A7 (lease/abuse/support), A10 (segment broadcast), A12 (usage once metered).
