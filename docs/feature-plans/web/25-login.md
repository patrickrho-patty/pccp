# 25 — 로그인 · Login (`web/src/pages/Login.tsx`)  *(trivial auth surface)*

> Vertical read: component → `api.login` → `handleLogin` → `auth.Login` (checks `admin_credentials` only).

## What it is
The **console operator login** (email/password → JWT in localStorage). *Operators only* — managed developers (Users) never log in here.

## Current state
Single email/password form; error + loading states; stores `pccp_token`.

## Gaps — grounded
**A. No SSO** (§8.2) — OIDC/SAML exist in the `sso` package but no login buttons/routes (and SAML verify is insecure — MISSING_ITEMS). *Fix:* SSO buttons + routed callbacks + signature verification.
**B. No MFA** (§8.9) — `admin_credentials.mfa_enrolled` concept; no second factor.
**C. No forgot-password/recovery, no "remember device"/anomaly challenge (§8.8), no post-login profile/console selection.**

## UX improvements
1. No SSO option (A); no password-show toggle; minimal error styling.
2. No "remember me"; no rate-limit/captcha UX.
3. No localization switcher; no loading polish.
4. No deep-link return-to after login.

## Sequencing
Phase 1: SSO buttons + secure SAML/OIDC (A) [ties to the operators/authz plan].
Phase 2: MFA (B), recovery, remember-device, post-login console pick.
