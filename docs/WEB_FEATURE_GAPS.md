# PCCP Web — Per-Page Critical Features & UX Improvements

**Generated:** 2026-08-13
**Method:** each item is grounded in the actual current page code (`web/src/pages/*.tsx`) and the v2 PRD — not generic. "Improvements" are strictly web UI/UX. Trivial auth pages (Login/Bootstrap) get a focused short list rather than padded filler.

Legend: features = new critical/near-critical capabilities; improvements = concrete UI/UX fixes tied to what the page renders today.

---

## 1. 사용자 · Users (`Users.tsx`)
**Critical features (PRD §8, §12, §13, §27, §33)**
1. RBAC role assignment per org/project (§13) — no role editor exists at all.
2. Delegated / affiliate-local admin (§12.3, §33.1) — flat org only.
3. Group → Affiliate → Division assignment (§12.1) — missing.
4. Access-label / clearance-level editor (model has `access_labels`, unused) (§27).
5. MFA/WebAuthn enrollment + admin reset (`web_authn_cred_id` field exists, unused).
6. Contractor/SI sponsor + contract expiry + auto-disable (§12.4, §33.2).
7. SCIM/CSV bulk import + sync-status indicator (§32.1).
8. Deep-linkable user detail page `/users/:id` (only inline expand today).
9. Per-user token usage / cost / quota view (§29.14).
10. Mandatory policy-acknowledgement tracking (§33.6).
11. Offboarding workflow w/ evidence handoff + access removal confirmation (§33.14).
12. Admin-action audit trail scoped per user (§40).

**UX improvements**
1. **Department is a hardcoded `<select>`** — make departments a managed entity (create/edit/delete/rename). *(your earlier complaint)*
2. Name cell isn't a link to a detail page.
3. Auth-method cell is a static badge — should show SSO connection status / jump to config.
4. Suspend/offboard capture **no reason** and write no audit rationale.
5. Bulk actions missing "activate", "assign role/dept", "export selected".
6. No CSV/Excel import or export.
7. Expand row loads **all** sessions+harnesses client-side (won't scale) — lazy-load per user.
8. "퇴사" and "삭제" both call `deleteUser` — irreversible, no archive/confirmation of revocations.
9. No avatar/initials, no contractor/role visual cue.
10. No login-anomaly or stale-account indicator.
11. FilterBar missing **department** and **role** filters (only auth+status).
12. No column sorting, column toggle, or saved views.
13. No favorites / pin frequent users.
14. Pagination is client-side slicing — should be server-side.
15. No skeleton/transition on expand; no empty-state onboarding hint.
16. No left sub-menu (users / groups / roles / contractors as siblings).

---

## 2. AI 세션 · Sessions (`Sessions.tsx`)
**Critical features (§14, §19, §27)**
1. Live token-stream view of an active session (real, not card status).
2. Per-session policy decision log (allow/deny per exchange) drill-down (§13).
3. Replay an exchange / re-run with a different model (§14.3).
4. Fork/branch a session into a new one (carry context).
5. Session cost + CLU (compute load units) breakdown (§10C.4, §29).
6. Privacy visibility-level indicator per session (§27) — who can read its content.
7. Evidence-receipt viewer per session (§40.3).
8. Session comparison (diff two sessions' changes).
9. Export session transcript (redaction-aware) for review/audit.
10. Real-time tool-call sequence diagram (which tools ran, approved?).

**UX improvements**
1. **Bug:** `<Fragment key={s.id || s.key || i}>` — `i` is undefined → React key warning/collision.
2. `handleCreate` defaults to `sessions[0]?.organization_id` / `harnesses[0]?.harness_id` — silently mis-binds if lists reorder/empty.
3. Model `<select>` is hardcoded (patty-code-standard/pro) — should come from the catalog epoch.
4. No server-side filter; client-side slice of all sessions.
5. Inspector modal is huge but **non-deep-linkable** — refresh loses it; no shareable URL.
6. No bulk close/pause/terminate of selected sessions.
7. Card view and table view drift (card shows active only; inconsistent columns).
8. Conversation history has no search/filter within the transcript.
9. No keyboard shortcut to inspect/close.
10. "상세 검사" + "프로바이던스" + expand all do overlapping fetches — consolidate.
11. Status legend not shown; emojis in dropdown options (inconsistent with rest).
12. Duration shown but no sort by duration/cost/tokens.
13. No "jump to user/harness/repo" deep links from the row (only generic `/users`).
14. Auto-refresh every 10s causes full reload flicker — should patch/diff.
15. No favorites/recent sessions.
16. No timeline scrubber for long sessions.

---

## 3. 하네스 · Harnesses (`Harnesses.tsx`)
**Critical features (§8.4, §8.5, §14, §33.10)**
1. Enrollment certificate viewer + revocation reason trail (§8.4).
2. Per-harness device attestation status (§9.6) — currently absent.
3. Forced-version / release-ring enforcement UI (§33.10) — set min version, block builds.
4. Harness sharing/multi-user binding editor (§8.7).
5. Per-harness policy pack assignment (§13).
6. Quarantine playbook (auto-revoke leases, notify owner) as one action.
7. Heartbeat/last-seen anomaly + stale-harness detection.
8. Harness-to-session-to-commit full lineage view.
9. Bulk revoke/quarantine by filter (e.g., all <v1.2).
10. Enrollment QR/command snippet for the developer (copy-paste enroll command).

**UX improvements**
1. Enroll form fields (공개키, 바이너리 버전) require internal knowledge — should validate/format and explain.
2. Harness ID is long/mono — no copy-to-clipboard, no truncation tooltip.
3. No detail page `/harnesses/:id` — only inline.
4. "재활성화"/"등록" actions lack confirmation modals for destructive ops.
5. Risk column is a static label — no score breakdown or trend.
6. Session count is a number, not clickable to filtered session list.
7. No filter by version/ring/last-seen/risk.
8. Links go to generic `/fleet` not to this specific harness.
9. No bulk selection.
10. No empty-state enroll guidance.
11. Registration date shown raw — no relative time.
12. No favorites.
13. No table ↔ card toggle.
14. Pubkey field has no format check → easy to save garbage.

---

## 4. 프로젝트 · Projects (`Projects.tsx`)
**Critical features (§12, §18, §29, §33.4)**
1. Project budget/quota + chargeback (§29.12).
2. Member roster + role per project (§12.3).
3. AI Change Control Board queue per project (§33.4).
4. Project-level policy override / exception (§33.8).
5. Repository sensitivity inheritance (§33.5) at project scope.
6. Project lifecycle (active/archived/frozen) with freeze mode (§33.13).
7. Project KPI/scorecard rollup (§25).
8. Default model + allowed-model policy editor (real, not free-text).
9. Project offboarding/evidence-handoff action (§33.14).
10. Cross-project analytics comparison.

**UX improvements**
1. "허용 모델" is free-text — needs a multi-select from catalog.
2. No project detail page — expand only.
3. Member count/session count not clickable.
4. Slug field requires manual entry — should auto-generate from name with edit.
5. "+ 저장소 연결" — no way to unlink or see which repos are linked clearly.
6. No archiving/restore flow.
7. No favorites.
8. No status filter / lifecycle state.
9. No bulk archive.
10. No sub-menu (active / archived / templates).
11. Cards lack hover affordance to open detail.
12. No description rendering (markdown).
13. No recent-activity preview.
14. No empty-state.
15. No responsive grid behavior documented.

---

## 5. 저장소 · Repositories (`Repositories.tsx`)
**Critical features (§18, §19, §33.5)**
1. Real SCM integration (GitHub/GitLab clone + webhooks) — currently just a URL field (your earlier complaint).
2. File browser (browse files/branches) (§18.1).
3. Repository sensitivity heatmap editor (§33.5).
4. Immutable task-baseline tagging (§18.3).
5. Change-set graph / branch-aware governance view (§18.4–§18.5).
6. Branch-protection rule editor (real rules, not a flag).
7. Commit/merge integration + provenance binding (§18.6, §19).
8. Default-branch + protected-branches list per repo.
9. Repo health (last sync, divergence, stale branches).
10. Per-repo AI-policy pack.

**UX improvements**
1. SCM provider is a hardcoded `<select>` — should be configurable connectors.
2. Clone URL is plain text — no validation, no auth-credential picker.
3. No repo detail page / file tree.
4. "브랜치 보호" is a flag with no rule detail.
5. ID shown raw, not copyable.
6. No sync/pull status indicator.
7. No link to the actual repo (external).
8. No filter by project/SCM/sensitivity.
9. No bulk unregister.
10. No favorites.
11. No commit count / last-commit time.
12. Project column not clickable.
13. No empty-state guidance for connecting first repo.
14. No webhook/secret config UI.

---

## 6. 보안 운영 센터 · Security (`Security.tsx`)
**Critical features (§15, §16, §35)**
1. Findings are a **static catalog**, not live — wire real detection output into the findings list.
2. Incident lifecycle (open → triage → contain → resolve → postmortem) (§15.4).
3. Containment action that actually propagates to the relay (lockdown today is DB-only).
4. DLP rule editor with test-against-sample (simulator) (§15.5).
5. Korean PII lexicon versioning + per-org overrides (§16.3).
6. Per-finding evidence chain (which session/exchange triggered it).
7. Suppress / accept-risk workflow with approver + expiry.
8. Security posture trend over time (not just counts).
9. Alert routing (Slack/email/on-call) (§10C.14) — currently no delivery.
10. SIEM export / forwarding (§32.4).

**UX improvements**
1. Four sub-areas (dashboard/findings/rules/scanner) but **no tabs** — unclear ordering; user doesn't know where to start.
2. Rule form (위협 카테고리/탐지 대상/심각도/조치) needs guided templates, not free-text regex.
3. Regex patterns require expert knowledge — add a tester with match highlighting.
4. No severity filter / status filter on findings.
5. Findings not clickable to the offending session.
6. No bulk resolve/suppress.
7. CSV export exists but no date-range scoping on findings.
8. No diff view when editing a rule.
9. Counts (critical/high) not clickable to filtered lists.
10. No favorites/pinned rules.
11. No animation/transitions; static tables feel dated.
12. Scanner output has no progress/affordance.
13. No sub-menu for rule families (injection / PII / secrets / network).
14. Action ("검사 실행") lacks confirmation for heavy scans.
15. No empty-state guidance.

---

## 7. 거버넌스 정책 · Policy (`Policy.tsx`)
**Critical features (§13, §33.6, §33.8)**
1. Policy pack is **not persisted** (api/server.go:1927 logs to audit only) — make it a real record.
2. ABAC attribute editor (§13.2) — missing.
3. Policy-epoch diff/version history with rollback (§13.1).
4. Mandatory acknowledgement campaign (§33.6).
5. Exception marketplace templates (§33.8).
6. Policy simulation against historical sessions (§15.5).
7. Per-scope (org/project/repo) effective-policy resolver.
8. Approval workflow for policy changes (§46.2).
9. Conflict detection between overlapping epochs.
10. Export/import policy packs.

**UX improvements**
1. Domain/template/scope are hardcoded `<select>`s (4 arrays) — make configurable.
2. "허용 모델" free-text — multi-select from catalog.
3. No policy detail / version page.
4. Activation has no preview of affected users/harnesses.
5. No diff between epochs.
6. Epoch table not filterable/sortable.
7. No draft → review → publish staging UI (state machine hidden).
8. Delete has no impact warning.
9. No favorites.
10. No sub-menu by domain (model / network / tool / data).
11. No search across policy contents.
12. No empty-state.
13. No bulk enable/disable.
14. Timestamps raw, no relative time.

---

## 8. 컴플라이언스 · Compliance (`Compliance.tsx`)
**Critical features (§41, §33)**
1. Assessment is a **static template** — make it evaluate real org state (your earlier complaint).
2. CSAP 간편/일반/인증 대상 selection; ISMS-P level selection (you explicitly asked).
3. Evidence vault — attach/query evidence per control (§40.3).
4. Control-mapping to PCCP features (auto-derive status).
5. Gap → remediation task tracking with owner + due date.
6. Audit-ready export pack (ISMS-P/CSAP matrix).
7. Continuous compliance re-assessment schedule.
8. Government overlay (§41.2) toggle.
9. Policy-source model (§41.3) — which policy satisfies which control.
10. Remediation SLA / overdue alerts.

**UX improvements**
1. **No API call** — entire page is 46 inline literals; wire to backend.
2. Read-only — no CRUD on controls/evidence/gaps.
3. "해결 계획" is a single text field — no structured task.
4. No tabs (overview / controls / evidence / gaps) — all stacked.
5. Summary counts (compliant/partial) not clickable.
6. No filter by certification/severity/status.
7. No per-control drill-down page.
8. No upload for evidence files.
9. PRD ref column not linkable to spec.
10. No favorites.
11. No progress-over-time view.
12. No export.
13. No empty-state.
14. No responsive layout.

---

## 9. 플릿 관리 · Fleet (`Fleet.tsx`)
**Critical features (§14.2, §15.4, §33.13)**
1. Fleet actions must **propagate** to the live relay (today they're DB-only).
2. Live containment verification (confirm harness actually dropped).
3. Change-freeze activation per repo/branch (§33.13).
4. Force-harness-version block (§33.10).
5. Mass-action targeting (by risk/version/affiliation).
6. Action history per harness with revert where possible.
7. Forensic snapshot download (evidence bundle) (§40.3).
8. Quarantine network isolation confirmation.
9. Approvals queue integration.
10. Broadcast-to-affected-users on lockdown (§22).

**UX improvements**
1. "관리 ▾" dropdown actions need confirmation modals (destructive).
2. Reason field required only for some actions — inconsistent.
3. No filter by risk/status/version/user.
4. Sessions/approvals/findings columns are counts, not clickable.
5. Harness cell not deep-linkable.
6. No bulk select for mass actions.
7. No live refresh indicator.
8. "🔴 비상 잠금" too easy to misclick — needs 2-step confirm + scope picker.
9. No favorites/pinned harnesses.
10. No sub-menu (live / quarantined / history).
11. No animation on state change.
12. No empty-state.
13. Action result not shown inline (success/fail toast).
14. No audit trail inline per row.

---

## 10. SRE 운영 콘솔 · SREConsole (`SREConsole.tsx`)
**Critical features (§7.1, §10C, §43)**
1. Real SLO burn-rate + error-budget tracking (§43) — currently static.
2. Live queue depth / admission / TTFT from the scheduler (§10C.7) — scheduler is dead code.
3. Account integrity vs T&S vs capacity **separate** panels (§8.9) — conflated today.
4. Graduated-response ladder with live state (§10C.10).
5. Capacity forecast (§10C.15).
6. Incident timeline + postmortem.
7. Regional health / dependency status (§7.1).
8. Alert routing config (Slack/email/on-call) (§10C.14) — missing.
9. GPU fleet utilization live (§30.3).
10. On-call handoff view.

**UX improvements**
1. Read-only dashboard — no drill-down to the underlying account/queue.
2. 16 inline literals — verify which are live vs canned.
3. Plan/slots table has repeated "플랜/하네스 Max" headers — layout confusion.
4. No time-range selector.
5. No wallboard/kiosk mode (§7.5).
6. No auto-refresh toggle/indicator.
7. No favorites/pinned accounts.
8. No export.
9. No sub-menu (capacity / risk / reliability / support).
10. Numbers not clickable to filtered lists.
11. No alert/silence controls.
12. No historical comparison (§7.6).
13. No color-blind-safe legend.
14. No empty-state.

---

## 11. 커뮤니케이션 허브 · Communications (`Communications.tsx`)
**Critical features (§21–23, §6.5)**
1. Real-time delivery to harness (WebSocket/DARI) — today CP-side only.
2. E2E encryption indicator + key management (§21.5).
3. File transfer **actual storage** — today metadata only.
4. Context-linking messages to AI sessions (§21.6) — make first-class.
5. Presence that reflects real harness state (§21.3).
6. Broadcast targeting by role/group/affiliate (§22.2) + acknowledgement tracking.
7. Admin command vs message distinction (§22.4).
8. Secure handoff between shifts (§23.4).
9. Privacy-aware separation (comms content not visible to platform admin) (§6.5).
10. Message search + retention controls (§40).

**UX improvements**
1. Email send is a stub: `alert('이메일 발송 기능은...')`.
2. No tabs (chat / files / broadcast / presence) — all stacked, confusing order.
3. Severity/target are hardcoded `<select>`s (7 arrays).
4. No 1:1 chat launch from a user search (your example) — comms is disconnected from user lookup.
5. No unread indicators / mention notifications.
6. No message threading/replies.
7. File transfer has no progress/preview/scan-result.
8. Broadcast has no recipient preview or ack dashboard.
9. No keyboard shortcuts.
10. No favorites/pinned conversations.
11. No sub-menu (channels / DMs / broadcasts / transfers).
12. No markdown/rich text in messages.
13. No empty-state.
14. No responsive mobile layout.
15. Presence dots are static, no "last active" detail.

---

## 12. 도구 관리 · Tools (`Tools.tsx`)
**Critical features (§17, §10B)**
1. Tool/MCP governance enforced at request time — today admin-only.
2. MCP server registry with approval workflow (§17.2).
3. Command-authorization policy editor (§17.3).
4. Network-broker allow/deny rules (§17.4).
5. Secret-broker scope management (§17.5).
6. Tool risk classification + approval SLA.
7. Per-project tool allowlist.
8. Tool-call audit (which session called which tool).
9. Custom tool registration (your earlier question: "is there a way to register a custom one?").
10. Tool capability/compatibility matrix vs models (§10B.4).

**UX improvements**
1. **"기본 도구 등록" adds duplicates on repeat click** (your earlier complaint) — guard against dupes.
2. 11 hardcoded option arrays — make tool classes/categories configurable.
3. Danger level is a label — no guidance on what each level means.
4. No tool detail page.
5. Approval column not actionable (can't approve/deny inline).
6. No filter by class/risk/approval-state.
7. No bulk approve.
8. No sub-menu (tools / MCP / commands / secrets / networks).
9. No search by Korean name only — inconsistent.
10. No favorites.
11. No empty-state.
12. No export.
13. No versioning of tool definitions.
14. Risk cell not color-explained.
15. Form fields require internal knowledge (tool class strings).

---

## 13. 샌드박스 · Sandboxes (`Sandboxes.tsx`)
**Critical features (§31)**
1. Real sandbox runtime — today `sandbox` is "record definition / reconstruct from audit" (fake).
2. Runtime-mode enforcement (local/remote/container) (§31.1).
3. Network-policy actual enforcement (§31.1, §17.4).
4. Resource limits (CPU/mem) actual enforcement.
5. Snapshot/restore real (today "📸 스냅샷" is cosmetic).
6. Session ↔ sandbox lifecycle binding.
7. Image allowlist + signing verification.
8. Sandbox fleet health.
9. Per-sandbox evidence/provenance.
10. Auto-teardown on session close.

**UX improvements**
1. Runtime mode/base image/network policy are hardcoded `<select>`s (3 arrays).
2. Form requires infra knowledge (image names) — needs an image picker/allowlist.
3. No sandbox detail / terminal view.
4. "세션 →" link only, no live state.
5. Snapshot button non-functional feedback.
6. No filter by mode/status/session.
7. No bulk destroy.
8. No resource-usage live gauge.
9. No favorites.
10. No empty-state.
11. No sub-menu (active / snapshots / images).
12. CPU/mem are free number inputs — no validation/slider.
13. No confirmation on destroy.
14. No export.

---

## 14. 분석 · Analytics (`Analytics.tsx`)
**Critical features (§28, §29, §50)**
1. Numbers (token usage, cost) **not clickable** to the underlying records (your example) — make every stat drill-down.
2. Cohort analysis (§28.2).
3. Cost breakdown by model/project/user with chargeback export (§29.12).
4. Anomaly detection on usage spikes.
5. NL analytics / "ask a question" (§28.4).
6. Saved custom dashboards per role.
7. Scheduled report delivery (§45.2).
8. Export to BI (CSV/JSON connector).
9. KPI scorecards vs targets (§50).
10. Trend + forecast.

**UX improvements**
1. CSS bar charts — no interactivity (hover detail, zoom, filter).
2. No time-range selector / granularity (day/week/month).
3. No comparison periods.
4. No drill-through anywhere.
5. No favorites/saved views.
6. No sub-menu (usage / cost / engineering / adoption).
7. CSV export only — no PDF/scheduled.
8. No loading skeleton.
9. No empty-state.
10. No responsive chart sizing.
11. No legend toggle.
12. Numbers not localized consistently.
13. No tooltips on metrics.
14. No shareable link to a view.
15. No annotation of incidents on the timeline.

---

## 15. 감사 로그 · Audit (`Audit.tsx`)
**Critical features (§40, §6.5)**
1. Legal-hold flagging per event (§40.5).
2. Retention-class display + purge schedule (§40.4).
3. Tamper-evidence (hash chain / signature verification UI) (§39.6).
4. Admin-action audit specifically (§6.5, DoD #13).
5. Evidence-bundle assembly from selected events (§40.3).
6. Saved searches / watchlists.
7. Streaming tail (live audit).
8. SIEM forwarding (§32.4).
9. Actor correlation graph (who/what/when across events).
10. Compliance-scoped views (per certification).

**UX improvements**
1. Filter presets exist (오늘/어제/…) but actor/resource not searchable by partial.
2. Rows not expandable to full event payload.
3. No JSON/raw view toggle.
4. Export works but no column selection.
5. No live-refresh toggle.
6. No favorites/saved queries.
7. Stats (total/success) not clickable to filtered list.
8. No sub-menu (admin / model / security / system).
9. Pagination client-side (50/page slice).
10. Result column color but no legend.
11. No relative-time column (only absolute).
12. No keyboard nav.
13. No empty-state guidance.
14. No diff view for config-change events.

---

## 16. 모델 인프라 · ModelInfra (`ModelInfra.tsx`)
**Critical features (§9, §10A, §11, §30)**
1. PMP signature/attestation viewer (§9.4) — today just IDs.
2. Endpoint assurance level editor + verification (§9.6).
3. Catalog-epoch publish/recall with affected-endpoint preview (§10A.8).
4. Model recall impact analysis (which sessions/commits used it) (§33.9).
5. Routing-policy editor (§10.6, §30.4).
6. GPU/replica capacity planner (§30.3).
7. Endpoint drain/cordon with live in-flight request count.
8. Canary/rollout ring assignment (§33.10).
9. Data-residency router config (§30.5).
10. KV/cache utilization per endpoint (§10.10).

**UX improvements**
1. Package/endpoint tables mixed on one page — split with tabs (packages / endpoints / catalog / routing).
2. "기본 모델 등록" free-text IDs — should pick from catalog.
3. No model/endpoint detail page.
4. TTFT/active-requests not sortable.
5. Status badges but no health drill-down.
6. No filter by family/state/assurance.
7. Epoch refresh has no confirmation or diff.
8. No favorites.
9. No bulk drain.
10. No export.
11. Artifact field raw, not copyable.
12. No empty-state.
13. No sub-menu.
14. Publish/recall lack impact preview.

---

## 17. 서비스 커맨드 센터 · ServiceCommandCenter (`ServiceCommandCenter.tsx`, Patty Ops)
**Critical features (§6.1, §7.1, §10C)**
1. Live traffic view (active accounts/harnesses/sessions/slots/queues/exchanges) — currently counts only.
2. Account-integrity / T&S / capacity live state (§8.9).
3. Work-slot & queue health from the (currently dead) scheduler.
4. Capacity-lease issuer/validator live.
5. Support timeline per account (§6.1 Support).
6. Refund/entitlement escalation (§6.1).
7. Regional health (§7.1).
8. Incident/command panel.
9. Trust & Safety case queue (§10C.11) — separated from integrity.
10. Abuse signal feed (§10C.9).

**UX improvements**
1. Read-only; nothing drill-downs to the account.
2. Subscriber table cells (integrity/T&S/capacity) not clickable.
3. 6 inline literals — verify live vs canned.
4. No time-range.
5. No wallboard mode (§7.5).
6. No auto-refresh indicator.
7. No favorites.
8. No sub-menu matching §6.1 nav tree.
9. No export.
10. No alerts panel.
11. No empty-state.
12. No search across accounts.
13. No responsive layout.
14. Plan/sub status not filterable.

---

## 18. 구독자 관리 · SubscriberManagement (`SubscriberManagement.tsx`, Patty Ops)
**Critical features (§8.6, §10C, §29)**
1. Payment/subscription state from a real provider (§29.9) — field only.
2. Harness registry + device/OS labels per subscriber (§6.6).
3. Account-integrity vs T&S graduated-response actions (§10C.10).
4. Refund/credit workflow.
5. Fair-use / capacity-lease state per subscriber (§10C.5).
6. Abuse case lifecycle (§10C.11).
7. Support-case linkage.
8. Segment/cohort tagging for ops.
9. Approximate geo/device (privacy-safe) (§6.6).
10. Bulk comms/broadcast to a segment.

**UX improvements**
1. Email send is a stub alert.
2. Read-only mostly — no edit of subscription/plan.
3. Tokens/device/risk columns are static labels.
4. No filter by plan/status/risk.
5. No subscriber detail page.
6. No search beyond listed.
7. No favorites.
8. No export.
9. No sub-menu (active / delinquent / abuse / support).
10. Joined date raw.
11. No empty-state.
12. No bulk actions.
13. No responsive layout.
14. Plan change has no workflow.

---

## 19. 계정 포털 · AccountPortal (`AccountPortal.tsx`, public user)
**Critical features (§6.6, §29)**
1. Invoices / payment method via provider (§6.6) — missing.
2. Registered-harness management (revoke, sign-out-all) (§6.6).
3. Plan usage / fair-use state at-a-glance (§6.6).
4. Security events visible to the user.
5. Active-session list + remote kill.
6. Account recovery flow.
7. Data/privacy settings + export/delete (GDPR/PIPC).
8. Support request submission.
9. Subscription upgrade/downgrade.
10. **Must NOT expose transferable API credentials** (§6.6) — verify none leak.

**UX improvements**
1. Plan is a hardcoded `<select>` (5 arrays).
2. Only "create account" works — no management of existing.
3. No way back out / no console switcher once inside (your earlier complaint).
4. No invoices/billing history.
5. No harness list for the account.
6. No usage visualization.
7. No responsive mobile.
8. No empty-state.
9. No favorites/recent.
10. No animation/transitions.
11. No security-settings tab.
12. No support entry.
13. Form validation minimal.
14. No localization switcher.

---

## 20. 실시간 뷰 · LiveView (`LiveView.tsx`)
**Critical features (§14.1, §14.4)**
1. **Real** live harness output (today scripted/simulated — your "fakery" complaint).
2. Real-time token stream per terminal card.
3. Risk indicator driven by live signals.
4. Grid auto-layout by screen (3/4/6 cols) + density control.
5. Click card → live session inspector.
6. Pause/follow a specific stream.
7. Filter by user/project/model/risk.
8. Live surveillance-boundary indicator (§14.4) — show what's visible vs not.
9. Aggregate throughput overlay.
10. Alert overlay when a session hits a policy.

**UX improvements**
1. Cards are fake scrolling text — needs real feed.
2. No density/layout control.
3. No filter/search.
4. No favorites/pin a session to top.
5. No pause/follow.
6. No timestamp correlation.
7. Back button only — no deep-link to a specific live session.
8. No empty-state when nothing active.
9. No refresh/pause control.
10. No responsive reflow.
11. No color coding by risk.
12. No legend.
13. No export/snapshot of a stream.
14. No keyboard nav between cards.

---

## 21. 코드 탐색기 · CodeExplorer (`CodeExplorer.tsx`)
**Critical features (§19, §20)**
1. Real provenance data — today empty until live sessions feed it (wire the path).
2. Line-level attribution hover → session/user/model (§19).
3. AI-vs-human diff with surviving-code metrics (§19.4).
4. Change-impact intelligence (§20) — blast radius of a span.
5. Click a code block → harness replay (§19.1).
6. Repo/file tree (real git browse).
7. Attribution confidence + override.
8. Commit-binding navigation (§18.6).
9. Export attribution report.
10. Filter by attribution/model/author/confidence.

**UX improvements**
1. Tabs (changesets/spans/files) but no guidance on order.
2. No real file browser.
3. Spans table not filterable/sortable.
4. Confidence shown as number, no visual.
5. No deep-link to span.
6. Heatmap lacks legend/interactivity.
7. No favorites.
8. No search across files.
9. No empty-state guidance ("run a session to see data").
10. Stats not clickable.
11. No diff view.
12. No responsive layout.
13. No export.
14. Session links generic.

---

## 22. 프로바이던스 체인 · Provenance (`Provenance.tsx`)
**Critical features (§19, §40)**
1. Full user→harness→prompt→model→file→diff→policy→commit chain (§19.5) — verify completeness.
2. Evidence-receipt verification (signature) (§40.3).
3. Export chain as signed bundle.
4. Replay any node.
5. Cross-session provenance search.
6. Privacy-level gating on displayed content (§27).
7. Survives-rename/repo-evolution test view (§19).
8. Timeline scrubber.
9. Policy-decision annotation per action.
10. Link to compliance evidence.

**UX improvements**
1. Minimal page — back button + lists.
2. No search/filter.
3. No export.
4. No drill-down on action payload.
5. No visual graph.
6. No favorites.
7. No empty-state.
8. No shareable link.
9. Timestamps raw.
10. No sub-menu.
11. No responsive.
12. No legend.
13. No copy of IDs.
14. No diff.

---

## 23. 엔터프라이즈 하네스 기능 · EnterpriseFeatures (`EnterpriseFeatures.tsx`)
**Critical features (harness→CP business features, §33)**
1. These "20 features" are seeded defaults — make them real, configurable, enforced.
2. Per-feature enable/disable by org/affiliate.
3. Severity → routing → owner workflow.
4. Tie each feature to a harness capability + DARI message.
5. Compliance/evidence per feature.
6. Audit of feature toggles.
7. Rollout rings (§33.10).
8. Feature dependency graph.
9. Bulk import/export feature packs.
10. Health/usage of each feature.

**UX improvements**
1. "20개 기본 기능 등록" seeds dupes on repeat (like Tools).
2. Read-mostly — no edit of feature config.
3. No filter by severity/status/harness.
4. No detail page.
5. "해결" action has no workflow.
6. No favorites.
7. No sub-menu by category.
8. No export.
9. No empty-state.
10. PRD ref not linkable.
11. No search.
12. No bulk actions.
13. Timestamps raw.
14. No responsive.

---

## 24. 대시보드 · Dashboard (`Dashboard.tsx`)
**Critical features (§7.3, §6.3)**
1. Stat cards drill-down (your example — numbers must be clickable).
2. Role/persona-scoped dashboard (§5, §6.5).
3. Governance brief widget (§33.12).
4. Active incidents / open gaps widget.
5. Quick-action wizards (enroll harness, create project).
6. Recently-viewed / favorites.
7. Cross-object navigation (§6.3).
8. Notification center.
9. Onboarding checklist for new admins.
10. KPI vs target.

**UX improvements**
1. "📊 데모 데이터 생성" button ships in the UI — remove in non-dev.
2. Stat cards not clickable.
3. "전체 보기 →" links generic.
4. No time-range.
5. No favorites/pinning.
6. No animation/transitions.
7. No sub-menu.
8. No empty-state.
9. No responsive reflow.
10. No loading skeleton.
11. No notification center.
12. No recent-activity personalization.
13. No theme/density toggle.
14. Cards not reorderable.

---

## 25. 로그인 · Login (`Login.tsx`)  *(trivial page — focused list)*
**Critical features**
1. OIDC/SAML SSO buttons (§8.2) — today local only.
2. MFA second factor (§8.9).
3. Account-recovery / forgot-password.
4. "Remember this device" + anomaly challenge (§8.8).
5. Profile/console selection post-login.

**UX improvements**
1. No SSO option visible.
2. No error-state styling guidance.
3. No password-show toggle.
4. No loading state.
5. No localization switcher.
6. No "remember me".
7. No captcha/rate-limit UX.

---

## 26. 부트스트랩 · Bootstrap (`Bootstrap.tsx`)  *(trivial page — focused list)*
**Critical features**
1. Org profile selection at bootstrap (enterprise/public/sovereign) (§34).
2. Seed demo-data toggle (explicit, not in prod).
3. Initial policy-pack choice (§41).
4. Admin MFA setup at bootstrap (§8.9).

**UX improvements**
1. Single form, no progress steps.
2. No validation feedback.
3. No profile guidance.
4. No success redirect polish.
5. No localization.

---

## Cross-cutting web improvements (apply to every page)
1. **No favorites/pinning** anywhere.
2. **No left sub-menus** — nav is flat; group related pages.
3. **No animations/transitions** beyond a pulse dot — add smooth, buttery transitions (page enter, modal, row expand, toasts).
4. **Server-side pagination/filtering** is absent everywhere (client slices full lists).
5. **Numbers/stats are not clickable** to filtered lists anywhere (your explicit example).
6. **Hardcoded `<select>`s** pervade (departments, models, plans, SCM, runtime, severity, auth) — none are admin-configurable entities.
7. **No detail pages** for any entity — everything is inline expand; breaks deep-linking/sharing.
8. **No global command palette / ⌘K**.
9. **No dark/light + density toggles.**
10. **Cancel/OK/Submit button consistency** — many destructive actions lack confirmation modals; modal responsiveness (max-height, sticky header) is inconsistent.
11. **Search is per-page fetch-and-filter**, not unified; no user→1:1-chat launch (your example).
12. **No empty-state guidance / onboarding** on any page.
13. **No keyboard navigation / shortcuts.**
14. **Responsive/mobile layouts** largely absent.
