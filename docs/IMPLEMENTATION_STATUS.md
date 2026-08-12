# PCCP v2 Implementation Progress

**Last updated:** Admin UX improvements round 2

## Page-by-Page Status (Admin Perspective)

### Dashboard (§7)
- ✅ Clickable stat cards (navigate to entity pages)
- ✅ Severity icons on recent activity (🔴🟢🟡🔵)
- ✅ Relative time display ("3분 전")
- ✅ Quick action buttons with navigation
- ✅ "전체 보기" links on sections
- ✅ Governance brief with compliance status
- ✅ Active sessions with model + time
- ✅ Clickable activity rows

### Users (§8)
- ✅ CRUD: Create/Edit/Suspend/Activate/Offboard
- ✅ Auth method dropdown (local/OIDC/SAML/LDAP/SCIM)
- ✅ FilterBar: auth method + status dropdowns
- ✅ Search by name/email/title
- ✅ Pagination (25/page)
- ✅ **Last login date column**
- ❌ Bulk actions (select multiple)
- ❌ Department/business unit assignment
- ❌ User detail view with activity history

### Sessions (§14.3)
- ✅ CRUD: Create/Pause/Resume/Close
- ✅ FilterBar: status + model dropdowns
- ✅ Expandable detail with provenance preview
- ✅ User name resolution
- ✅ **Duration display (경과)**
- ❌ Token usage per session
- ❌ Live monitoring

### Harnesses (§14)
- ✅ CRUD: Enroll/Quarantine/Reactivate/Revoke
- ✅ FilterBar: status + risk level dropdowns
- ✅ Expandable detail with device/heartbeat/attestation
- ✅ Risk state display
- ❌ Relative last-seen time ("3분 전")
- ❌ Session count per harness

### Projects (§12)
- ✅ CRUD: Create/Edit/Archive
- ✅ Expandable card detail
- ❌ Member count
- ❌ Session count
- ❌ Model access summary

### Repositories (§18)
- ✅ CRUD: Register/Edit/Unregister
- ✅ FilterBar: sensitivity + status
- ✅ Expandable detail
- ❌ Branch protection status
- ❌ Last activity indicator

### Models (§11)
- ✅ CRUD: Create/Edit/Publish/Recall
- ✅ FilterBar: state + family
- ✅ Expandable detail with manifest/signature info
- ❌ Endpoint count per model
- ❌ Capability badges in list

### Endpoints (§9)
- ✅ CRUD: Enroll/Drain/Lease
- ✅ FilterBar: status + assurance
- ✅ Expandable detail
- ✅ **Health indicator dot (green/yellow/red)**
- ✅ **Model package column**
- ❌ Latency/TTFT metrics

### Security (§15-16)
- ✅ SOC with 4 tabs (Dashboard/Findings/Rules/Scanner)
- ✅ DLP rule toggles
- ✅ Scanner with live detection
- ✅ Incident response panel
- ❌ Finding count badge
- ❌ Rule persistence to backend
- ❌ Export findings report

### Compliance (§41)
- ✅ Framework overview with 5 certifications
- ✅ Assessment with control details
- ✅ Evidence tab
- ❌ Last assessment date
- ❌ Remediation tracking

### Audit (§40)
- ✅ FilterBar: date range + type + result + actor
- ✅ **Quick time presets (오늘/어제/7일/30일/전체)**
- ✅ Stats summary
- ✅ CSV export
- ✅ Pagination (50/page)
- ❌ PDF export

### Analytics (§28)
- ✅ Token usage with progress bars
- ✅ Engineering metrics
- ✅ Security posture
- ✅ Executive governance brief
- ❌ Charts/visualizations
- ❌ Cost breakdown by user/project

### Communications (§21-23)
- ✅ Functional chat interface
- ✅ Broadcast creation
- ✅ File transfer placeholder
- ❌ Real-time WebSocket updates
- ❌ Presence indicators

### Model Catalog (§10A)
- ✅ Server-authoritative model display
- ✅ Capability indicators
- ✅ Withdraw/Announce
- ❌ Epoch refresh

### SRE Console (§10C/§7.1)
- ✅ Overview with system health
- ✅ Accounts with risk domains
- ✅ Capacity concepts
- ✅ Risk domain separation
- ✅ Graduated response ladder

### Account Portal (§6.6)
- ✅ Public account creation
- ✅ Subscription plans
- ✅ Slot policy display
- ✅ Lease issuance
