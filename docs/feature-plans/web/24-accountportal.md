# 24 — 계정 포털 · AccountPortal (`web/src/pages/AccountPortal.tsx`)  *(Public)*

> Vertical read: component → `api.publicCreateAccount/publicCreateSub/publicLease` + `/api/public/accounts` → public-cloud handlers → `publiccloud` Account/Subscription/AccountCapacityLease (§6.6, §10C).

## What this page actually is
The **public user Account Portal** (§6.6) — self-service for public subscribers: create account, choose plan/subscription, view/issue capacity lease + work slots. The *user-facing* surface (profile `portal`), distinct from Patty Ops subscriber management.

## Current vertical (what exists)
- Lists public accounts; create account (email/display_name/plan); create subscription; issue capacity lease (returns slots/heavy/validity via `alert`).
- Backed by the real `publiccloud` service (Account, Subscription, AccountCapacityLease, slots).

## Gaps — grounded
**A. Only "create" works** — no management of an existing account (no harness list, revoke, sign-out-all, usage view, recovery, data/privacy, support) per §6.6.
**B. No payment/invoices** (§6.6) — `PaymentProvider` is a bare field; no provider integration; no billing history.
**C. No way back / no console switcher once inside** *(your complaint)* — the portal traps the user.
**D. Plan is a hardcoded `<select>`**; lease result shown via `alert` (no inline state).
**E. Must-not expose transferable API credentials** (§6.6) — verify none leak.

## UX improvements (grounded)
1. Only create works (A); no self-service management.
2. No invoices/payment (B).
3. Trapped — no console switcher/back (C).
4. Plan hardcoded select; lease via alert (D).
5. No usage/fair-use visualization.
6. No harness management (revoke/sign-out-all).
7. No security-events/active-sessions view.
8. No account recovery; no data/privacy settings; no support entry.
9. No responsive mobile; no empty-state; no favorites.
10. No animation/transitions; minimal form validation.

## Intended-features coverage (vs WEB_FEATURE_GAPS §19 — 10 features)
1. Invoices/payment method via provider (§6.6) → **B** ✅
2. Registered-harness management (revoke/sign-out-all) → **A** ✅
3. Plan usage/fair-use at-a-glance → **A** ✅
4. Security events visible to user → **B** (security-events) ✅
5. Active-session list + remote kill → **B** ✅
6. Account recovery flow → **A** ✅
7. Data/privacy settings + export/delete → **B** ✅
8. Support request submission → **B** ✅
9. Subscription upgrade/downgrade → **B** ✅
10. Must NOT expose transferable API credentials (§6.6) → **E** ✅

## Sequencing
Phase 1 (self-service): existing-account management (harness list/revoke, sign-out-all, usage, recovery), console switcher/back (C), inline lease state.
Phase 2 (commercial): payment provider + invoices/history (B), plan upgrade/downgrade.
Phase 3 (trust): security-events/active-sessions, data/privacy settings, support, no-credential-leak verification (E).
