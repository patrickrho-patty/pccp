// Package workflow implements the T16 residual enterprise controls:
// the policy-exception workflow (request → review → decision with
// expiry and audit), onboarding/migration, and rollout/rollback
// controls with operator-visible status.
package korean

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// ExceptionRequest is a policy-exception ask (PRD §33.8).
type ExceptionRequest struct {
	OrganizationID string
	RequestedBy    string
	PolicyRule     string
	Reason         string
	ReasonKo       string
	Scope          string // repo / project / org
	ScopeID        string
	ExpiresAt      time.Time
}

// ErrExceptionDenied marks a refused exception.
var ErrExceptionDenied = errors.New("workflow: exception request denied")

// ErrExceptionExpired marks an approved exception past expiry.
var ErrExceptionExpired = errors.New("workflow: exception expired")

// RequestException opens the exception workflow (audit-logged).
func (s *Service) RequestException(req ExceptionRequest) (*models.AuditEvent, error) {
	if req.OrganizationID == "" || req.PolicyRule == "" || req.Reason == "" {
		return nil, errors.New("workflow: exception requires org, rule, and reason")
	}
	details, _ := json.Marshal(map[string]string{
		"policy_rule": req.PolicyRule, "scope": req.Scope, "scope_id": req.ScopeID,
		"reason": req.Reason, "reason_ko": req.ReasonKo,
		"expires_at": req.ExpiresAt.Format(time.RFC3339), "state": "requested",
	})
	ev := &models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      "cp.workflow.exception_requested",
		ActorID:        req.RequestedBy, ActorType: "user",
		Action:       "exception_request",
		ResourceType: "policy_rule", ResourceID: req.PolicyRule,
		Details:    string(details),
		Result:     "success",
		OccurredAt: time.Now().Format(time.RFC3339),
	}
	if err := s.db.Create(ev).Error; err != nil {
		return nil, fmt.Errorf("workflow: record exception request: %w", err)
	}
	return ev, nil
}

// DecideException approves or denies an open request (reviewer-gated).
func (s *Service) DecideException(orgID, eventID, reviewerID string, approve bool, note string) error {
	var ev models.AuditEvent
	if err := s.db.Where("id = ? AND organization_id = ?", eventID, orgID).First(&ev).Error; err != nil {
		return errors.New("workflow: exception request not found")
	}
	var details map[string]string
	if err := json.Unmarshal([]byte(ev.Details), &details); err != nil {
		return errors.New("workflow: malformed exception record")
	}
	if details["state"] != "requested" {
		return fmt.Errorf("workflow: exception is %s, not open", details["state"])
	}
	state := "denied"
	if approve {
		state = "approved"
	}
	details["state"] = state
	details["decided_by"] = reviewerID
	details["decision_note"] = note
	details["decided_at"] = time.Now().Format(time.RFC3339)
	updated, _ := json.Marshal(details)
	return s.db.Model(&models.AuditEvent{}).Where("id = ?", eventID).
		Update("details", string(updated)).Error
}

// ExceptionActive reports whether an approved exception covers the
// rule in scope today (expiry enforced).
func (s *Service) ExceptionActive(orgID, policyRule, scope, scopeID string, now time.Time) (bool, error) {
	var events []models.AuditEvent
	if err := s.db.Where("organization_id = ? AND event_type = ? AND resource_id = ?",
		orgID, "cp.workflow.exception_requested", policyRule).Find(&events).Error; err != nil {
		return false, err
	}
	for i := range events {
		var details map[string]string
		if err := json.Unmarshal([]byte(events[i].Details), &details); err != nil {
			continue
		}
		if details["state"] != "approved" {
			continue
		}
		if scope != "" && details["scope"] != scope {
			continue
		}
		if scopeID != "" && details["scope_id"] != scopeID {
			continue
		}
		exp, err := time.Parse(time.RFC3339, details["expires_at"])
		if err != nil {
			continue
		}
		if now.After(exp) {
			return false, ErrExceptionExpired
		}
		return true, nil
	}
	return false, nil
}

// OnboardingChecklist drives T16 onboarding: verifiable steps.
type OnboardingChecklist struct {
	OrganizationID string           `json:"organization_id"`
	Steps          []OnboardingStep `json:"steps"`
}

// OnboardingStep is one verifiable onboarding action.
type OnboardingStep struct {
	Key      string `json:"key"`
	LabelKo  string `json:"label_ko"`
	Done     bool   `json:"done"`
	Evidence string `json:"evidence,omitempty"`
}

// DefaultOnboardingChecklist is the standard enterprise onboarding
// sequence (Korean labels for the operator surface).
func DefaultOnboardingChecklist(orgID string) OnboardingChecklist {
	return OnboardingChecklist{OrganizationID: orgID, Steps: []OnboardingStep{
		{Key: "sso", LabelKo: "SSO 연결"},
		{Key: "policy_pack", LabelKo: "정책 팩 선택"},
		{Key: "enroll_harnesses", LabelKo: "하네스 등록"},
		{Key: "seats", LabelKo: "좌석 배정"},
		{Key: "regions", LabelKo: "데이터 영역 설정"},
		{Key: "first_session", LabelKo: "첫 거버넌스 세션"},
	}}
}

// RolloutControl is a rollout/rollback switch with operator-visible
// status (T16 Step 4).
type RolloutControl struct {
	state   string // rolling_out | rolled_back | paused
	Version string
	History []string
}

// NewRolloutControl builds a control for a rollout.
func NewRolloutControl(version string) *RolloutControl {
	return &RolloutControl{state: "rolling_out", Version: version, History: []string{"rolling_out:" + version}}
}

// Pause halts the rollout.
func (r *RolloutControl) Pause() {
	if r.state == "rolling_out" {
		r.state = "paused"
		r.History = append(r.History, "paused")
	}
}

// Resume continues a paused rollout.
func (r *RolloutControl) Resume() {
	if r.state == "paused" {
		r.state = "rolling_out"
		r.History = append(r.History, "resumed")
	}
}

// RollBack aborts and records the rollback.
func (r *RolloutControl) RollBack() {
	r.state = "rolled_back"
	r.History = append(r.History, "rolled_back")
}

// State reports the operator-visible status.
func (r *RolloutControl) State() string { return r.state }
