package models

import (
	"gorm.io/gorm"
)

// Governed browser control plane (PAT-1448) — PCCP half. The harness
// hosts the Chromium surface; PCCP owns the signed managed policy, the
// canonical action taxonomy, task capabilities, exact-effect approvals,
// and the structured evidence timeline.

// BrowserPolicy is one signed, versioned managed browser policy. The
// harness receives it and enforces destinations/actions/capture at the
// driver boundary; neither user nor model may expand it.
type BrowserPolicy struct {
	gorm.Model
	// (organization, version) is UNIQUE — concurrent versioning cannot
	// mint duplicates even under multi-writer databases.
	OrganizationID string `gorm:"uniqueIndex:idx_bgp_org_version" json:"organization_id"`
	Version        int    `gorm:"uniqueIndex:idx_bgp_org_version" json:"version"`
	// Canonical JSON: {"destinations":[{scheme,host,port,path_prefix,
	//   allow_redirect,expires_at}],"actions":{name:allowed|blocked|
	//   approval|takeover},"capture":{screenshot,dom,a11y,console,
	//   network,performance},"redaction":{mask_sensitive},
	//   "retention_days":30,"limits":{max_task_minutes,max_requests,
	//   max_concurrent_tabs},"layout":{tabs,side_by_side},
	//   "overrides":[{scope,kind,ref,policy}]}
	PolicyJSON string `gorm:"type:text" json:"policy_json"`
	Signature  string `json:"signature"`
	KeyID      string `json:"key_id"`
	CreatedBy  string `json:"created_by"`
	Active     bool   `json:"active"`
}

// BrowserTask is one delegated browser task: the initiating user,
// harness, bound tabs, the minimum issued capability, and the policy
// version it runs under (PAT-1448 agent task lifecycle).
type BrowserTask struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	TaskID         string `gorm:"uniqueIndex" json:"task_id"`
	UserID         string `json:"user_id"`
	HarnessID      string `json:"harness_id"`
	SessionID      string `json:"session_id"`
	// Explicitly attached tabs only — unrelated tabs are never implicit
	// task scope (PAT-1448 privacy boundary).
	TabsJSON      string `gorm:"type:text" json:"tabs_json"`
	GoalKo        string `json:"goal_ko"`
	LeaseID       string `json:"lease_id"`
	PolicyVersion int    `json:"policy_version"`
	State         string `json:"state"` // active|waiting_approval|completed|cancelled|failed|taken_over
	Outcome       string `json:"outcome"`
	CreatedAt2    string `json:"created_at_2"`
	ClosedAt      string `json:"closed_at"`
}

// BrowserApproval is bound to ONE exact proposed effect via its
// canonical digest. Any material change (price, quantity, seller,
// address…) changes the digest and invalidates the approval; use is
// single-use and expiring.
type BrowserApproval struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	TaskID         string `gorm:"index" json:"task_id"`
	EffectType     string `json:"effect_type"` // place_order|submit_form|upload_file|…
	EffectDigest   string `gorm:"index" json:"effect_digest"`
	// Human-readable before/after summary: for purchases the exact
	// product, seller, quantity, unit price, shipping, masked payment
	// source, total.
	DetailsJSON string `gorm:"type:text" json:"details_json"`
	State       string `json:"state"` // pending|approved|denied|expired|used
	Approver    string `json:"approver"`
	Reason      string `json:"reason"`
	ExpiresAt   string `json:"expires_at"`
	DecidedAt   string `json:"decided_at"`
	UsedAt      string `json:"used_at"`
}

// BrowserActionEvent is the structured evidence timeline entry —
// normalized action and target, redacted origin, policy/grant/approval
// binding, and effect operation id for idempotency (PAT-1448 evidence).
// No screenshots/payload bodies live here; artifacts are referenced by
// digest only.
type BrowserActionEvent struct {
	gorm.Model
	OrganizationID  string `gorm:"index" json:"organization_id"`
	TaskID          string `gorm:"index" json:"task_id"`
	Action          string `json:"action"`
	RiskClass       string `json:"risk_class"` // read_only|reversible|high_impact|mandatory_takeover
	TargetSummary   string `gorm:"type:varchar(255)" json:"target_summary"`
	Origin          string `gorm:"type:varchar(255)" json:"origin"` // redacted URL/origin
	Result          string `json:"result"`                          // ok|blocked|failed|denied|paused|taken_over
	PolicyVersion   int    `json:"policy_version"`
	GrantDigest     string `json:"grant_digest"`
	ApprovalID      uint   `json:"approval_id"`
	EffectOpID      string `json:"effect_op_id"` // idempotency key for effect checkpoints
	OccurredAt      string `gorm:"index" json:"occurred_at"`
	IntegrityDigest string `json:"integrity_digest"`
}
