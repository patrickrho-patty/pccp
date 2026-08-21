package models

import (
	"gorm.io/gorm"
)

// Patty Trails (PAT-1450): the canonical typed causal relationship index
// OVER existing immutable records (ActionEnvelope, ChangeSet,
// ProvenanceSpan, EvidenceReceipt, AuditEvent). No raw prompts, code,
// screenshots, or payloads are copied into this index — only bounded
// safe summaries, source references, and integrity digests.

// TrailNode is a critical-node milestone in the causal graph. It is a
// projection of a source record, identified by (source_type, source_id).
type TrailNode struct {
	gorm.Model
	OrganizationID string `gorm:"index:idx_trail_node_org_src" json:"organization_id"`
	SourceType     string `gorm:"index:idx_trail_node_org_src" json:"source_type"` // action|changeset|span|receipt|audit
	SourceID       string `gorm:"index" json:"source_id"`
	NodeType       string `json:"node_type"` // goal|execution|decision|change|effect|exception|outcome
	// Event-time scope snapshots — team attribution reflects membership
	// AT EVENT TIME, never rewritten by later reorganization.
	UserIDAtEvent    string `json:"user_id_at_event"`
	HarnessIDAtEvent string `json:"harness_id_at_event"`
	SessionID        string `json:"session_id"`
	ProjectID        string `json:"project_id"`
	RepositoryID     string `json:"repository_id"`
	// Bounded safe label (never raw content).
	LabelKo         string `gorm:"type:varchar(255)" json:"label_ko"`
	Status          string `json:"status"` // ok|denied|failed|rolled_back|degraded
	OccurredAt      string `gorm:"index" json:"occurred_at"`
	IntegrityDigest string `json:"integrity_digest"`
	GroupingKey     string `gorm:"index" json:"grouping_key"` // collapse repeated same-purpose ops
	CollapsedCount  int    `json:"collapsed_count"`
}

// TrailEdge is an explicit recorded causal relationship. Chronological
// adjacency alone NEVER creates an edge (PAT-1450 causality rule).
type TrailEdge struct {
	gorm.Model
	OrganizationID  string `gorm:"index:idx_trail_edge_org" json:"organization_id"`
	FromSourceType  string `gorm:"index:idx_trail_edge_from" json:"from_source_type"`
	FromSourceID    string `gorm:"index:idx_trail_edge_from" json:"from_source_id"`
	ToSourceType    string `gorm:"index:idx_trail_edge_to" json:"to_source_type"`
	ToSourceID      string `gorm:"index:idx_trail_edge_to" json:"to_source_id"`
	EdgeType        string `json:"edge_type"`       // initiated|delegated|authorized|blocked|produced|caused|rolled_back
	SourceEvidence  string `json:"source_evidence"` // the recorded field that proves this edge
	OccurredAt      string `json:"occurred_at"`
	IntegrityDigest string `json:"integrity_digest"`
}

// TrailViewerScope caches nothing sensitive: it records which
// organization + role dimensions a view was served under for
// invalidation on permission change.
type TrailViewerScope struct {
	gorm.Model
	OrganizationID string `gorm:"index" json:"organization_id"`
	ViewerEmail    string `json:"viewer_email"`
	Role           string `json:"role"`
	ScopeKind      string `json:"scope_kind"` // own|team|organization|harness|project|repository|session
	ScopeRef       string `json:"scope_ref"`
	PolicyEpoch    int    `json:"policy_epoch"`
	LastViewedAt   string `json:"last_viewed_at"`
}
