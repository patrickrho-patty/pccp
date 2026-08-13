# 26 — 부트스트랩 · Bootstrap (`web/src/pages/Bootstrap.tsx`)  *(trivial setup surface)*

> Vertical read: component → `api.bootstrap` → `handleBootstrap` (creates org + `BootstrapAdmin`). Note: `BootstrapAdmin` is idempotent (no-op if an admin already exists).

## What it is
First-run **org + admin creation** for the console. Creates an Organization + the first `admin_credentials` admin.

## Current state
Single form (org name, admin email, password); defaults `admin@patty.dev` / `Patty Enterprise`.

## Gaps — grounded
**A. No deployment-profile choice** (§34) — enterprise/public/sovereign determines defaults, policy pack, trust sources; not selected here. *Fix:* profile picker driving defaults.
**B. No initial policy-pack / compliance-framework choice** (§41); no explicit demo-data toggle (vs the always-on seeds elsewhere).
**C. No admin MFA setup at bootstrap** (§8.9); no validation/progress steps; success flow minimal.

## UX improvements
1. Single flat form — no steps/guidance.
2. Defaults pre-filled (`admin@patty.dev`) — fine for dev, risky if shipped.
3. No profile selection (A); no validation feedback; no localization.
4. Success redirect not polished.

## Sequencing
Phase 1: profile picker (A), step-by-step wizard, validation.
Phase 2: policy-pack/compliance choice (B), admin MFA setup, explicit demo-data toggle.
