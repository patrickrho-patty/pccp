# PCCP v2 — Complete Feature Documentation
## Web Admin Console + Harness PAPER Integration

**Last Updated:** 2026-08-12
**Status:** Production-ready control plane with full PAPER protocol integration

---

## Table of Contents

1. [Three-Console Architecture](#three-console-architecture)
2. [Patty Operations Console (Public Cloud)](#patty-operations-console)
3. [Customer Control Console (Enterprise/Government)](#customer-control-console)
4. [Account Portal (Subscriber Self-Service)](#account-portal)
5. [Harness PAPER Protocol Integration](#harness-paper-protocol-integration)
6. [20 Enterprise Harness Features](#20-enterprise-harness-features)
7. [Backend API Reference](#backend-api-reference)
8. [Security Architecture](#security-architecture)
9. [Korean Compliance Frameworks](#korean-compliance-frameworks)

---

## Three-Console Architecture

PCCP uses a **profile-aware** UI that renders completely different navigation, 
pages, and data based on the user's role:

| Profile | Who | Theme | Purpose |
|---|---|---|---|
| **Patty Ops** | Patty SRE/T&S/Billing | 🔴 Red | Service health, capacity, accounts, risk |
| **Enterprise/Govt** | Customer IT/Security | 🔵 Blue | Their developers, projects, security, compliance |
| **Account Portal** | Individual subscriber | 🟢 Green | Subscription, devices, self-service |

A **profile switcher** in the sidebar footer allows switching between consoles
for development purposes.

**Navigation is clean with no overlapping pages:**
- Live View is merged INTO Sessions (card/table toggle)
- Model Catalog + Packages + Endpoints unified INTO Model Infrastructure
- Fleet is separate (harness lifecycle management only)

---

## Patty Operations Console

### Service Command Center (§7.1)
- **Subscription status breakdown**: 6 clickable cards (총계정, 활성, 미납, 연체, 취소, 만료)
- Click any card → filtered account list with risk states
- **Risk overview**: Account integrity, T&S cases, capacity flags, open findings
- **System health**: Control Plane, PAPER Relay, Event Spine, Metering, Catalog, PIA
- **Platform events**: Recent audit events (aggregate only, no session content per §1212)

### SRE Operations Console
- **4 tabs**: Overview, Accounts, Capacity, Risk
- Account management with integrity/trust-safety/capacity state tracking
- Capacity lease view
- Risk queue with graduated response

### Subscriber Management
- Full subscriber table with: email, plan, status, OAuth provider, locale
- **Clickable subscription cards** that filter the account list
- Detail panel with risk states, quotas, devices, usage
- Bulk actions: suspend, activate
- **Seat management widget**: User seats (used/max), Harness seats, plan tier

### Fleet Management (§14)
- **18 fleet actions** (PRD §14.2): re-auth, policy refresh, pause, suspend model,
  revoke cert, quarantine, emergency lockdown, etc.
- Harness detail panel with user, device, version, risk
- Session inspector with timeline, changesets, findings, approvals
- Action history from audit trail

### Model Infrastructure (§9.5)
Unified page with 3 tabs showing the **three separate model identities**:
- **Catalog Tab**: User-facing model descriptors with capabilities, epoch refresh
- **Packages Tab (PMP)**: Signed model artifacts with publish/recall lifecycle
- **Endpoints Tab (PIA)**: Deployed inference instances with health, TTFT, capacity

### Analytics
- Token usage bar charts (input/output/cached)
- Engineering metrics dashboard
- Security posture metrics
- **Cost breakdown** in KRW with per-model cost
- CSV export of all metrics

### Security Operations Center
- **8 AI-specific threat categories** (37 rules):
  1. Prompt Injection & Jailbreak
  2. Model Abuse & Extraction
  3. Account & Identity Abuse
  4. Data Exfiltration
  5. Supply Chain & Code Security
  6. Infrastructure Attacks
  7. Policy Evasion
  8. Capacity Abuse
- **Visual rule builder** with plain-language presets (no regex visible)
- **Finding detail modal**: type, severity, rule, evidence, related session/user/harness
- Finding status management: acknowledge, resolve, false positive
- Emergency lockdown

### Audit Trail
- Full event log with filtering by type, result, actor, date range
- Quick time presets (today, 7d, 30d)
- CSV export + PDF print
- Expandable detail rows

---

## Customer Control Console

### Dashboard
- **Seat management widget** with utilization bars (green/yellow/red)
- Clickable stat cards navigating to relevant pages
- Recent activity with icon classification
- Security summary with finding count
- Quick links grid
- **Demo data seed button** (shows when DB is empty)

### Users / User Management
- **Unified profile-aware page**: Different columns/forms for Patty Ops vs Enterprise
- Enterprise fields: 사번 (employee ID), 부서, 직책, SSO, MFA
- Full CRUD: create, edit, delete, suspend, offboard
- **Bulk actions**: Select multiple, bulk suspend, bulk offboard
- Department assignment (8 Korean departments)
- Expandable detail with sessions, harnesses, activity history
- Email uniqueness enforced (DB + app level)

### Harnesses
- Full harness registry with user column
- **Cross-navigation**: User name → users page, sessions → sessions page
- Expandable detail with user info, active sessions, security
- Actions: enroll, revoke, quarantine, reactivate
- Links to fleet management and audit

### Projects
- Full CRUD with card grid layout
- **Repository links** with SCM provider icons
- Session counts (active + total) with links
- Member counts with links to users
- Allowed models display
- Expandable detail with recent sessions

### Repositories
- Full CRUD with **project linking** dropdown
- SCM provider icons (GitHub, GitLab, Bitbucket, Gitea)
- **Branch protection** configuration (standard/protected/release/production/locked)
- Expandable detail with project info + security/provenance links

### AI Sessions (Merged with Live View)
- **View toggle**: Table (all sessions) vs Cards (live active)
- **Auto-refresh** every 10 seconds
- All session statuses visible: active, paused, closed, completed, terminated
- **Session Inspector modal**: 
  - Summary (user, model, status, duration)
  - Timeline (actions, tool calls with outcomes)
  - Change sets (AI/human attribution, diff stats)
  - Security findings
  - **Conversation history** (prompt/response pairs with tokens, latency, verdict)
- Cross-navigation to users, harnesses, fleet, audit
- Token usage per session

### Code Provenance Explorer
- **Real ChangeSet data** from session timelines (no mock data)
- AI/human attribution per code change
- Links each change to the session that produced it
- Cross-navigation to users, sessions, audit, security
- Diff stats and patch display

### Provenance Chain
- Per-session provenance chain
- **AI vs Human attribution chart** with percentage bar
- Evidence bundle section (§40.3)
- Cross-navigation to sessions, explorer, audit, compliance

### Governance Policy
- **6 policy domains** with 19 templates:
  - Model Access, Tool Permissions, Data Protection, Git/SCM, Network, Session
- **Visual policy builder** (no regex, plain Korean descriptions)
- Policy hierarchy visualization (Patty → Profile → Org → Dept → Project → Repo → Branch)
- Template gallery with one-click activation
- Epoch history

### Compliance & Certifications
- **5 Korean compliance frameworks** (43 controls):
  1. **CSAP** (클라우드보안인증) — 간편/일반 tier selection
  2. **ISMS-P** (정보보안관리체계) — 1-3급 level selection
  3. **개인정보보호법** (PIPA) — 민감정보/일반 scope
  4. **KISA 안전한 소프트웨어 개발** — secure coding guidelines
  5. **AI 가이던스 및 거버넌스** — 한국 AI 기본법 / ISO 42001
- Each control has: ID, requirement (Korean + English), status, evidence, PRD ref
- Remediation plan view for gaps
- Compliance score calculation

### Tools Registry
- **Visual rule builder** with preset patterns (no regex visible to admin)
- 6 threat categories: PII, Secret, Injection, Behavioral, Code, Infrastructure
- Full CRUD: create, edit, delete, toggle approval
- Stats: total, requiring approval, high-risk
- Seed dedup (no duplicates on repeated clicks)

### Communications Hub
- **Chat**: Create conversations with participant selection, real message send/receive
  with auto-refresh (3s), auto-scroll, message bubbles, AI session linking
- **Broadcast**: Create with severity (info/warning/critical/emergency), target scope,
  acknowledgement requirement; list all broadcasts
- **File Transfer**: Create with recipient + classification (public/internal/confidential/restricted)
- **Presence**: Online/away/offline status with activity info and harness links

### Sandboxes
- Full CRUD with runtime mode selection
- **Explanation card**: What a sandbox is, why it exists (PRD §31)
- Runtime modes: Docker, Firecracker, gVisor, Kata, None
- Network policies: restricted, egress-only, full, airgap
- Session linking
- Lifecycle: snapshot (forensic) + destroy

### Enterprise Harness Features (§33)
- **20 enterprise-only capabilities** with Korean names + PRD refs
- Categories: Governance, Security, Compliance, Identity, Audit
- Feature toggles: enable/enforce
- Violation tracking with severity and resolution
- All features seeded via one-click endpoint

---

## Account Portal

- Account overview with email, display name, locale, timezone
- Subscription management with plan selection
- Capacity lease view (work slots, heavy slots, background)
- Registered harnesses with first/last seen
- Self-service actions: revoke harness, sign out all

---

## Harness PAPER Protocol Integration

### Overview
The Patty Code Harness (`patcode`) now speaks PAPER protocol natively. 
There is no OpenAI/Anthropic fallback.

### New Harness Packages
1. **`internal/paperproto/`** — Vendored PAPER protocol library
   - `constants.go`: Message types matching PCCP exactly
   - `framing.go`: 32-byte binary record encode/decode  
   - `transport.go`: TLS/TCP dial, PAPER preface, Send/Recv records
   - `messages.go`: CBOR-encoded handshake + JSON AI payloads

2. **`internal/provider/paper/`** — PAPER provider
   - Registers as `kind = "paper"` via `init()`
   - Implements `provider.Provider` (Stream method)
   - Connects to PCCP PAPER Relay via TLS/TCP
   - Performs HELLO → HELLO_ACK → AUTH_CHALLENGE → AUTH_PROOF → AUTH_ACK
   - Sends AI_OPEN with Catalog Model ID (no base_url/api_key)
   - Receives AI_TOKEN_CHUNK streaming + AI_COMPLETE
   - Emits ChunkText + ChunkUsage + ChunkDone

### Protocol Blocking
- **Generic HTTP providers blocked by default** (PRD §0.2)
- `openai`, `anthropic`, `responses` kinds return error
- `PATTY_ALLOW_GENERIC=1` re-enables for development
- Official builds do NOT import openai/anthropic packages

### End-to-End Data Path
```
Harness (patcode)
  → PAPER TLS/TCP (CBOR framing, ALPN "paper/1")
  → PAPER Relay (:8444)
  → HTTP forward
  → PIA (:9090)
  → HTTP
  → Serving Engine (vLLM/SGLang)
  → Response back through same path
```

---

## 20 Enterprise Harness Features

| # | Feature (Korean) | Feature (English) | Category | PRD Ref | Enforced |
|---|---|---|---|---|---|
| 1 | 정책 기반 코드 리뷰 | Policy-Enforced Code Review | governance | §33.4 | ✅ |
| 2 | 의무 코드 서명 | Mandatory Code Signing | security | §18.6 | ✅ |
| 3 | 컴플라이언스 코딩 표준 | Compliance-Aware Coding Standards | compliance | §33.11 | |
| 4 | 감사 증거 내보내기 | Audit Trail Export | audit | §40.3 | ✅ |
| 5 | SSO/SCIM 신원 연결 | SSO/SCIM Identity Binding | identity | §32.1 | ✅ |
| 6 | 기기 보안 상태 증명 | Device Posture Attestation | security | §14.1 | ✅ |
| 7 | 의무 샌드박스 실행 | Mandatory Sandbox Execution | security | §31.2 | |
| 8 | 데이터 분류 태깅 | Data Classification Tagging | governance | §16 | ✅ |
| 9 | 공급망 검증 | Supply Chain Validation | security | §15.3 | ✅ |
| 10 | 네트워크 송신 제어 | Network Egress Control | security | §17.4 | ✅ |
| 11 | 키/비밀정보 브로커링 | Key/Secret Brokering | security | §17.5 | ✅ |
| 12 | 포렌식 스냅샷 | Forensic Snapshot | audit | §14.2 | |
| 13 | 정책 예외 워크플로 | Policy Exception Workflow | governance | §33.8 | |
| 14 | 의무 승인 확인 | Mandatory Acknowledgement | governance | §33.6 | ✅ |
| 15 | 변경 동결 모드 | Change-Freeze Mode | governance | §33.13 | |
| 16 | AI 코드 기여 추적 | AI Code Attribution | audit | §19 | ✅ |
| 17 | 명령어 인가 | Command Authorization | security | §17.3 | ✅ |
| 18 | MCP 서버 허용 목록 | MCP Server Allowlist | security | §17.2 | ✅ |
| 19 | 긴급 모델 리콜 | Emergency Model Recall | governance | §33.9 | ✅ |
| 20 | 프로젝트 오프보딩 | Project Offboarding | audit | §33.14 | |

---

## Backend API Reference

### Key Endpoints

#### Identity & Organization
- `POST /api/auth/login` — JWT login
- `POST /api/auth/bootstrap` — Initial setup
- `GET /api/organizations/seats` — Seat usage (users + harnesses)
- `CRUD /api/users` — User management with email dedup
- `CRUD /api/harnesses` — Harness enrollment + revoke/quarantine

#### Sessions
- `CRUD /api/sessions` — Session lifecycle
- `GET /api/sessions/{id}/usage` — Token usage per session
- `GET /api/sessions/{id}/timeline` — Full action/change/finding timeline
- `GET /api/sessions/{id}/exchanges` — Conversation history (prompts + responses)
- `POST /api/sessions/{id}/pause|close|resume`

#### Fleet (§14)
- `GET /api/fleet/inventory` — Harness inventory with risk scores
- `GET /api/fleet/sessions/{id}/inspect` — Session inspector
- `POST /api/fleet/actions` — 18 fleet actions with audit trail

#### Security
- `GET /api/security/findings` — Security findings list
- `GET /api/security/findings/{id}` — Finding detail with session/user enrichment
- `PUT /api/security/findings/{id}` — Update finding status
- `POST /api/security/check` — DLP/security scanner
- `POST /api/security/lockdown` — Emergency lockdown
- `GET/PUT /api/security/policy` — DLP rule management

#### Enterprise Features (§33)
- `GET /api/enterprise/features` — List 20 enterprise features
- `PUT /api/enterprise/features/{id}` — Toggle enable/enforce
- `GET /api/enterprise/violations` — Policy violations
- `POST /api/enterprise/features/seed` — Seed defaults
- `POST /api/enterprise/demo-seed` — Seed all demo data

#### Communications
- `CRUD /api/communications/conversations` — Chat conversations
- `POST /api/communications/conversations/{id}/messages` — Send message
- `GET/POST /api/communications/broadcasts` — Broadcast management
- `POST/GET /api/communications/file-transfers` — Secure file transfer
- `GET/POST /api/communications/presence` — User presence

#### Model Infrastructure
- `GET /api/catalog/models` — Catalog models with capabilities
- `GET /api/catalog/epoch` — Current catalog epoch
- `CRUD /api/models` — Model packages (PMP) with publish/recall
- `CRUD /api/endpoints` — PIA endpoints with lease/drain

#### Compliance
- `GET /api/compliance/certifications` — Certification status
- `POST /api/compliance/assess` — Run compliance assessment

#### Realtime
- `GET /api/realtime/ws` — WebSocket connection
- `GET /api/realtime/sse` — Server-Sent Events stream

---

## Build Statistics

- **Go packages**: 49+ internal packages
- **Go tests**: 159 passing
- **DB models**: 44 auto-migrated
- **API routes**: 90+ endpoints
- **React pages**: 30 (with profile-aware routing)
- **Harness**: PAPER provider + 4 paperproto packages
- **PAPER messages**: 50+ types (HELLO, AUTH, AI_OPEN, AI_COMPLETE, etc.)
