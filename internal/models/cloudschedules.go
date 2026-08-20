package models

import (
	"gorm.io/gorm"
)

// Governed public cloud schedules (PAT-1437). Public build only; the
// capability must not appear in enterprise/sovereign compositions.
// PCCP is the scheduling authority: occurrences run server-side without
// the user's harness being online, against a frozen task/context
// snapshot with capability delegation — never the live transcript.

// CloudSchedule is one registered schedule with an immutable, versioned
// task specification frozen at registration.
type CloudSchedule struct {
	gorm.Model
	OwnerUserID string `gorm:"index" json:"owner_user_id"`
	// Frozen compiled task spec: objective, success criteria, context
	// references, delivery target. Later conversation messages never
	// silently alter this; edits create a new revision + snapshot.
	TaskSpecJSON        string `gorm:"type:text" json:"task_spec_json"`
	ContextSnapshotJSON string `gorm:"type:text" json:"context_snapshot_json"`
	// {"kind":"cron","expr":"0 8 * * 1-5","timezone":"Asia/Seoul"} or {"kind":"once","at":"..."}
	TriggerJSON             string `gorm:"type:text" json:"trigger_json"`
	State                   string `json:"state"` // draft|active|paused|authorization_required|restricted|completed|revoked|deleted
	Revision                int    `json:"revision"`
	NextOccurrenceAt        string `json:"next_occurrence_at"`
	LastOccurrenceAt        string `json:"last_occurrence_at"`
	Timezone                string `json:"timezone"`
	CreatedFromConversation string `json:"created_from_conversation"`
}

// ScheduleOccurrence is one intended run. The idempotency key
// (schedule revision + intended time) makes duplicate scheduler delivery
// unable to repeat an effect (PAT-1437).
type ScheduleOccurrence struct {
	gorm.Model
	ScheduleID     uint   `gorm:"index" json:"schedule_id"`
	Revision       int    `json:"revision"`
	IntendedAt     string `gorm:"index" json:"intended_at"`
	IdempotencyKey string `gorm:"uniqueIndex" json:"idempotency_key"`
	State          string `json:"state"` // pending|admitted|running|waiting_for_authorization|succeeded|failed|denied|expired|cancelled|coalesced
	// Frozen task/context snapshot copied at admission — an edit landing
	// mid-flight never changes what this run executes (PAT-1437).
	TaskSpecJSON           string `gorm:"type:text" json:"task_spec_json"`
	ContextSnapshotJSON    string `gorm:"type:text" json:"context_snapshot_json"`
	Attempts               int    `json:"attempts"`
	NextRetryAt            string `json:"next_retry_at"`
	RunSessionRef          string `json:"run_session_ref"`
	ResultSummaryKo        string `json:"result_summary_ko"`
	CostTokens             int    `json:"cost_tokens"`
	DenyReason             string `json:"deny_reason"`
	CredentialFingerprints string `json:"credential_fingerprints"`
	StartedAt              string `json:"started_at"`
	FinishedAt             string `json:"finished_at"`
}

// AccountCapability is the account-level capability matrix shared by
// the harness and web profile. The model sees metadata and connection
// state — never OAuth tokens or provider mechanics.
type AccountCapability struct {
	gorm.Model
	OwnerUserID     string `gorm:"index" json:"owner_user_id"`
	CapabilityID    string `gorm:"uniqueIndex" json:"capability_id"` // semantic id, e.g. "communication.email.read"
	Kind            string `json:"kind"`                             // mcp | tool | skill
	DisplayKo       string `json:"display_ko"`
	State           string `json:"state"` // available|authorization_required|insufficient_scope|expired|revoked|local_only|prohibited|unavailable
	CloudExecutable bool   `json:"cloud_executable"`
	ConnectionID    uint   `json:"connection_id"`
	Version         string `json:"version"`
}

// CapabilityConnection is the durable authorization broker record.
// Credential material lives under the KMS/HSM envelope seam — only an
// opaque encrypted reference is stored here.
type CapabilityConnection struct {
	gorm.Model
	OwnerUserID           string `gorm:"index" json:"owner_user_id"`
	CapabilityID          string `gorm:"index" json:"capability_id"`
	AuthorizationServer   string `json:"authorization_server"`
	ScopesJSON            string `json:"scopes_json"` // granted scopes
	CredentialEnvelopeRef string `json:"credential_envelope_ref"`
	State                 string `json:"state"`          // connected|expired|revoked|authorization_required
	InitiatedFrom         string `json:"initiated_from"` // harness | web — same account-level result
	Version               string `json:"version"`
}

// ScheduleDelegation records what a schedule may use: a subset of a
// connection's capabilities (narrow-only), plus any narrowly scoped
// standing authorization for unattended consequential actions.
type ScheduleDelegation struct {
	gorm.Model
	ScheduleID   uint   `gorm:"index" json:"schedule_id"`
	CapabilityID string `json:"capability_id"`
	ScopesJSON   string `json:"scopes_json"` // subset of the connection's scopes
	// Standing authorization for consequential unattended actions, with
	// the disclosed source/destination/data class recorded BEFORE grant.
	Consequential  bool   `json:"consequential"`
	DisclosureJSON string `json:"disclosure_json"` // {"source":"...","destination":"...","data_class":"...","operation":"..."}
	GrantedAt      string `json:"granted_at"`
	Revoked        bool   `json:"revoked"`
}
