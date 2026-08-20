package models

import "time"

// WorkObjective is a predeclared unit of governed work for the evidence-backed
// leaderboard (PAT-1440). The objective receives its work type, size band,
// owner/attribution, and validation policy BEFORE meaningful work; the final
// model can never retroactively choose an easier class. Credit follows the
// recorded attribution and must total 100%.
type WorkObjective struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	ObjectiveID    string `gorm:"type:varchar(64);uniqueIndex;not null" json:"objective_id"`
	// Predeclared classification.
	WorkType  string `gorm:"type:varchar(32);not null" json:"work_type"` // defect|feature|refactor|security|documentation
	SizeBand  string `gorm:"type:varchar(16);not null" json:"size_band"` // small|medium|large
	OwnerID   string `gorm:"type:varchar(64);index;not null" json:"owner_id"`
	TeamID    string `gorm:"type:varchar(64);index" json:"team_id,omitempty"`
	ProjectID string `gorm:"type:varchar(64);index" json:"project_id,omitempty"`
	// Attribution JSON: [{user_id, fraction}] summing to 1 when shared.
	SharedAttribution string `gorm:"type:text" json:"shared_attribution,omitempty"`
	// Validation/acceptance policy snapshot used to judge first-pass quality.
	ValidationPolicy string `gorm:"type:text" json:"validation_policy,omitempty"`
	AcceptanceGate   string `gorm:"type:varchar(64)" json:"acceptance_gate,omitempty"`
	// Chronicled coalesce key: splits of the same governed objective collapse
	// to one accepted-delivery credit (anti-gaming).
	CoalesceKey string `gorm:"type:varchar(128);index" json:"coalesce_key,omitempty"`
	// Lifecycle.
	Status            string    `gorm:"type:varchar(24);index;not null" json:"status"` // open|accepted|rejected|reverted
	AcceptedAt        time.Time `json:"accepted_at,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	CompletedRev      string    `gorm:"type:varchar(128)" json:"completed_rev,omitempty"`
	RevertedAt        time.Time `json:"reverted_at,omitempty"`
	AcceptedChangeSet string    `gorm:"type:varchar(64)" json:"accepted_change_set,omitempty"`
	// First-pass quality gates (rate evidence).
	PassedFirstGate  bool   `json:"passed_first_gate"`            // validation + review gate passed
	AvoidedRework    bool   `json:"avoided_rework"`               // no substantial rework before acceptance
	NoRegression     bool   `json:"no_regression"`                // no rollback / confirmed attributable regression in window
	ModelTurns       int    `gorm:"default:0" json:"model_turns"` // model turns to acceptance
	ActiveElapsedMs  int64  `json:"active_elapsed_ms"`            // active authored/resolver time (excludes approval/queue/away/external wait)
	ApprovalWaitMs   int64  `json:"approval_wait_ms,omitempty"`   // recorded external wait, excluded from efficiency
	QueryQueueWaitMs int64  `json:"query_queue_wait_ms,omitempty"`
	UserAwayMs       int64  `json:"user_away_ms,omitempty"`
	SourcedBy        string `gorm:"type:varchar(32)" json:"sourced_by"` // parses from which event/changeset stream
}

func (WorkObjective) TableName() string { return "work_objectives" }

// ScorecardRubric is the Patty-defined property rubric (PAT-1440): exactly four
// scored properties with tenant-configurable weights within Patty-enforced
// guardrails. Rubrics FREEZE when their review period begins; weight or
// definition changes create a NEW version applied only prospectively.
type ScorecardRubric struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	RubricID       string `gorm:"type:varchar(64);uniqueIndex;not null" json:"rubric_id"`
	Version        uint64 `gorm:"index" json:"version"`
	Name           string `gorm:"type:varchar(255)" json:"name,omitempty"`
	NameKo         string `gorm:"type:varchar(255)" json:"name_ko,omitempty"`
	// Weights (percent, sum to 100).
	WeightDelivery   int `json:"weight_delivery"`
	WeightQuality    int `json:"weight_quality"`
	WeightSecurity   int `json:"weight_security"`
	WeightEfficiency int `json:"weight_efficiency"`
	// Critical violation score ceiling (Patty-enforced) — a confirmed
	// critical violation caps overall below a threshold regardless of weights.
	CriticalCeiling float64 `json:"critical_ceiling"` // 0-100
	// Minimum evidence for an individual rank.
	MinAcceptedOutcomes int `json:"min_accepted_outcomes"` // default 5
	MinGovernedActions  int `json:"min_governed_actions"`  // default 20
	// Lifecycle: draft|active|frozen
	Status      string `gorm:"type:varchar(16);default:'draft'" json:"status"`
	EffectiveAt string `gorm:"type:timestamp" json:"effective_at,omitempty"`
	FrozenAt    string `gorm:"type:timestamp" json:"frozen_at,omitempty"`
	CreatedBy   string `gorm:"type:varchar(64)" json:"created_by,omitempty"`
	Supersedes  uint64 `json:"supersedes,omitempty"` // prior version this replaces
}

func (ScorecardRubric) TableName() string { return "scorecard_rubrics" }

// ScorecardPeriod is a tenant review period (PAT-1440): rolling 90 days by
// default, or a fixed quarter. Snapshot FREEZES at finalization; nothing
// recomputes official scores after that (corrections create visible events).
type ScorecardPeriod struct {
	Base
	OrganizationID string    `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PeriodID       string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"period_id"`
	Name           string    `gorm:"type:varchar(255)" json:"name"`
	NameKo         string    `gorm:"type:varchar(255)" json:"name_ko"`
	PeriodType     string    `gorm:"type:varchar(24);not null" json:"period_type"` // rolling_90d|fixed_quarter
	StartAt        time.Time `gorm:"index;not null" json:"start_at"`
	EndAt          time.Time `gorm:"not null" json:"end_at"`
	RubricID       string    `gorm:"type:varchar(64);index" json:"rubric_id,omitempty"`
	Status         string    `gorm:"type:varchar(16);default:'running'" json:"status"` // running|frozen|finalized
	FinalizedBy    string    `gorm:"type:varchar(64)" json:"finalized_by,omitempty"`
	FinalizedAt    string    `gorm:"type:timestamp" json:"finalized_at,omitempty"`
}

func (ScorecardPeriod) TableName() string { return "scorecard_periods" }

// ScorecardSnapshot is one computed, frozen-in-time leaderboard row (PAT-1440).
// Provisional scores refresh daily; the period snapshot freezes at finalization.
// State: provisional|finalized|corrected|disputed.
type ScorecardSnapshot struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PeriodID       string `gorm:"type:varchar(64);index;not null" json:"period_id"`
	RubricID       string `gorm:"type:varchar(64);index;not null" json:"rubric_id"`
	RubricVersion  uint64 `json:"rubric_version"`
	SubjectType    string `gorm:"type:varchar(16);index;not null" json:"subject_type"` // individual|team|project
	SubjectID      string `gorm:"type:varchar(64);index;not null" json:"subject_id"`
	Cohort         string `gorm:"type:varchar(128)" json:"cohort,omitempty"` // role/level/work-family cohort
	// Property scores (0-100).
	DeliveryScore   float64 `json:"delivery_score"`
	QualityScore    float64 `json:"quality_score"`
	SecurityScore   float64 `json:"security_score"`
	EfficiencyScore float64 `json:"efficiency_score"`
	OverallScore    float64 `json:"overall_score"`
	Percentile      float64 `json:"percentile,omitempty"` // 0-100 within cohort
	Rank            int     `json:"rank"`                 // 0 = unranked
	EvidenceCount   int     `json:"evidence_count"`
	Confidence      string  `gorm:"type:varchar(16)" json:"confidence"` // sufficient|insufficient|partial
	// Aggregated evidence counts (context, never score inputs by themselves).
	AcceptedOutcomes    int   `json:"accepted_outcomes"`
	GovernedActions     int   `json:"governed_actions"`
	ConfirmedViolations int   `json:"confirmed_violations"`
	TotalSessions       int   `json:"total_sessions"`
	TotalModelTurns     int   `json:"total_model_turns"`
	TokensUsed          int64 `json:"tokens_used,omitempty"`
	// Explanation snapshot.
	Explanation string `gorm:"type:text" json:"explanation,omitempty"`
	// State machine.
	State      string `gorm:"type:varchar(16);default:'provisional'" json:"state"`
	ComputedAt string `gorm:"type:timestamp;not null" json:"computed_at"`
}

func (ScorecardSnapshot) TableName() string { return "scorecard_snapshots" }

// ScorecardCorrection is a correction/dispute/appeal record (PAT-1440). Late
// verified evidence recomputes the score with a visible correction event; a
// dispute marks evidence without silently deleting it.
type ScorecardCorrection struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PeriodID       string `gorm:"type:varchar(64);index;not null" json:"period_id"`
	SubjectID      string `gorm:"type:varchar(64);index;not null" json:"subject_id"`
	Kind           string `gorm:"type:varchar(24);not null" json:"kind"` // correction|dispute|appeal
	Reason         string `gorm:"type:text" json:"reason"`
	EvidenceRef    string `gorm:"type:varchar(128)" json:"evidence_ref,omitempty"`
	Status         string `gorm:"type:varchar(16);default:'open'" json:"status"` // open|accepted|rejected
	ByUserID       string `gorm:"type:varchar(64)" json:"by_user_id,omitempty"`
	DecidedBy      string `gorm:"type:varchar(64)" json:"decided_by,omitempty"`
	DecidedAt      string `gorm:"type:timestamp" json:"decided_at,omitempty"`
	DecisionNote   string `gorm:"type:text" json:"decision_note,omitempty"`
}

func (ScorecardCorrection) TableName() string { return "scorecard_corrections" }

// ScorecardReview is a human finalization record: the reviewer records their
// decision and independent rationale SEPARATELY from any score. PCCP never
// emits an automatic promote/do-not-promote recommendation.
type ScorecardReview struct {
	Base
	OrganizationID string `gorm:"type:varchar(64);index;not null" json:"organization_id"`
	PeriodID       string `gorm:"type:varchar(64);index;not null" json:"period_id"`
	SubjectID      string `gorm:"type:varchar(64);index;not null" json:"subject_id"`
	ReviewerID     string `gorm:"type:varchar(64)" json:"reviewer_id"`
	Decision       string `gorm:"type:varchar(32)" json:"decision"` // promote_review|retain|documented; never auto "promote"
	Rationale      string `gorm:"type:text" json:"rationale"`
	ReviewedAt     string `gorm:"type:timestamp" json:"reviewed_at"`
}

func (ScorecardReview) TableName() string { return "scorecard_reviews" }
