package incident

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
)

// Service implements Incident Management and Containment (PRD §15.2-15.4).
type Service struct {
	db        *gorm.DB
	mu        sync.RWMutex
	lifecycle *sessionlifecycle.Service
	fleet     *fleet.Service
}

func (s *Service) SetFleetService(service *fleet.Service) { s.fleet = service }

// New creates a new incident service.
func New(db *gorm.DB, lifecycles ...*sessionlifecycle.Service) *Service {
	lifecycle := sessionlifecycle.New(db)
	if len(lifecycles) > 0 && lifecycles[0] != nil {
		lifecycle = lifecycles[0]
	}
	return &Service{db: db, lifecycle: lifecycle}
}

// Incident represents a security incident (PRD §15.2).
type Incident struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Title          string `json:"title"`
	TitleKo        string `json:"title_ko"`
	Description    string `json:"description"`
	Severity       string `json:"severity"` // critical, high, medium, low, info
	Status         string `json:"status"`   // open, investigating, contained, resolved, closed
	// Affected entities
	UserIDs       []string `json:"user_ids,omitempty"`
	HarnessIDs    []string `json:"harness_ids,omitempty"`
	SessionIDs    []string `json:"session_ids,omitempty"`
	RepositoryIDs []string `json:"repository_ids,omitempty"`
	ModelIDs      []string `json:"model_ids,omitempty"`
	// Classification
	Category string `json:"category"` // credential_leak, pii_exposure, injection, etc.
	// Evidence
	FindingIDs    []string `json:"finding_ids,omitempty"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	PolicyRuleIDs []string `json:"policy_rule_ids,omitempty"`
	// Timeline
	FirstSeenAt string `json:"first_seen_at"`
	ContainedAt string `json:"contained_at,omitempty"`
	ResolvedAt  string `json:"resolved_at,omitempty"`
	// Recommended actions
	RecommendedActions []string `json:"recommended_actions"`
	// Containment
	ContainmentMode string `json:"containment_mode,omitempty"` // session, harness, project, org
	CreatedBy       string `json:"created_by"`
}

// CreateIncident creates a new security incident.
func (s *Service) CreateIncident(inc Incident) (*Incident, error) {
	if inc.ID == "" {
		inc.ID = dari.GenerateID("inc")
	}
	if inc.Status == "" {
		inc.Status = "open"
	}
	if inc.FirstSeenAt == "" {
		inc.FirstSeenAt = time.Now().Format(time.RFC3339)
	}

	// Store as a SecurityFinding
	finding := &models.SecurityFinding{
		Base:           models.Base{ID: inc.ID},
		OrganizationID: inc.OrganizationID,
		FindingType:    "incident_" + inc.Category,
		Severity:       inc.Severity,
		Title:          inc.Title,
		TitleKo:        inc.TitleKo,
		Description:    inc.Description,
		Status:         inc.Status,
		OccurredAt:     inc.FirstSeenAt,
	}
	if err := s.db.Create(finding).Error; err != nil {
		return nil, fmt.Errorf("incident: create: %w", err)
	}
	if err := s.recordAudit(inc.OrganizationID, "cp.incident.created", inc.CreatedBy, "incident", inc.ID,
		map[string]string{"title": inc.Title}); err != nil {
		_ = s.db.Delete(finding).Error
		return nil, fmt.Errorf("incident: create audit: %w", err)
	}

	return &inc, nil
}

// ContainmentMode defines the scope of containment (PRD §15.4).
type ContainmentMode string

const (
	ContainSession     ContainmentMode = "session"
	ContainHarness     ContainmentMode = "harness"
	ContainProject     ContainmentMode = "project"
	ContainOrgLockdown ContainmentMode = "org_lockdown"
)

// Contain applies a containment mode to affected entities.
type ContainRequest struct {
	OrganizationID string          `json:"organization_id"`
	IncidentID     string          `json:"incident_id"`
	Mode           ContainmentMode `json:"mode"`
	SessionID      string          `json:"session_id,omitempty"`
	HarnessID      string          `json:"harness_id,omitempty"`
	ProjectID      string          `json:"project_id,omitempty"`
	PerformedBy    string          `json:"performed_by"`
	Reason         string          `json:"reason"`
}

// ContainResult records what was contained.
type ContainResult struct {
	IncidentID        string   `json:"incident_id"`
	Mode              string   `json:"mode"`
	AffectedSessions  []string `json:"affected_sessions"`
	AffectedHarnesses []string `json:"affected_harnesses"`
	Actions           []string `json:"actions"`
	Timestamp         string   `json:"timestamp"`
}

// Contain applies containment measures (PRD §15.4).
func (s *Service) Contain(req ContainRequest) (*ContainResult, error) {
	var incidentFinding models.SecurityFinding
	if err := s.db.Where("organization_id = ? AND id = ? AND finding_type LIKE ?", req.OrganizationID, req.IncidentID, "incident_%").First(&incidentFinding).Error; err != nil {
		return nil, errors.New("incident: incident not found in organization")
	}
	result := &ContainResult{
		IncidentID: req.IncidentID,
		Mode:       string(req.Mode),
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	switch req.Mode {
	case ContainSession:
		// Session containment (PRD §15.4)
		if req.SessionID != "" {
			out := s.lifecycle.Transition(sessionlifecycle.Request{OrganizationID: req.OrganizationID, SessionRef: req.SessionID, Target: "paused", Action: "incident_containment", Reason: req.Reason, ActorID: req.PerformedBy})
			if out.Result != sessionlifecycle.ResultUpdated {
				return nil, fmt.Errorf("incident: session containment incomplete (%s): %s", out.Result, out.Error)
			}
			result.AffectedSessions = append(result.AffectedSessions, out.SessionID)
		} else {
			return nil, errors.New("incident: session_id is required")
		}
		result.Actions = []string{"세션 일시정지 (session paused)"}

	case ContainHarness:
		if req.HarnessID == "" && incidentFinding.SessionID != "" {
			var session models.Session
			if err := s.db.Where("organization_id = ? AND session_id = ?", req.OrganizationID, incidentFinding.SessionID).First(&session).Error; err == nil {
				req.HarnessID = session.HarnessID
			}
		}
		if req.HarnessID == "" {
			return nil, errors.New("incident: harness_id is required")
		}
		if s.fleet == nil {
			return nil, errors.New("incident: canonical fleet containment is not configured")
		}
		var sessions []string
		if err := s.db.Model(&models.Session{}).Where("organization_id = ? AND harness_id = ? AND status IN ?", req.OrganizationID, req.HarnessID, models.SessionNonTerminalStatuses()).Pluck("session_id", &sessions).Error; err != nil {
			return nil, err
		}
		if err := s.fleet.PerformAction(fleet.ActionRequest{OrganizationID: req.OrganizationID, HarnessID: req.HarnessID, Action: fleet.ActionQuarantine, Reason: req.Reason, PerformedBy: req.PerformedBy}); err != nil {
			return nil, err
		}
		result.AffectedHarnesses = append(result.AffectedHarnesses, req.HarnessID)
		result.AffectedSessions = sessions
		result.Actions = []string{"하네스 격리 및 진행 중 세션 종료 (harness quarantined and in-progress sessions terminated)"}

	case ContainProject:
		if req.ProjectID != "" {
			var project models.Project
			if err := s.db.Where("organization_id = ? AND id = ?", req.OrganizationID, req.ProjectID).First(&project).Error; err != nil {
				return nil, errors.New("incident: project not found in organization")
			}
			outcomes, err := s.lifecycle.TransitionScope(sessionlifecycle.Scope{OrganizationID: req.OrganizationID, ProjectID: req.ProjectID}, "paused", "incident_containment", req.Reason, req.PerformedBy)
			if err != nil {
				return nil, err
			}
			for _, outcome := range outcomes {
				if outcome.Result != sessionlifecycle.ResultUpdated {
					return nil, fmt.Errorf("incident: project session containment incomplete (%s): %s", outcome.Result, outcome.Error)
				}
				result.AffectedSessions = append(result.AffectedSessions, outcome.SessionID)
			}
		} else {
			return nil, errors.New("incident: project_id is required")
		}
		result.Actions = []string{"프로젝트 진행 중 세션 일시정지 (in-progress project sessions paused)"}

	case ContainOrgLockdown:
		return nil, errors.New("incident: organization lockdown must use the canonical /api/security/lockdown workflow")
	default:
		return nil, fmt.Errorf("incident: unsupported containment mode %q", req.Mode)
	}

	// Update incident status
	updated := s.db.Model(&models.SecurityFinding{}).
		Where("organization_id = ? AND id = ?", req.OrganizationID, req.IncidentID).
		Updates(map[string]interface{}{
			"status":          "contained",
			"contains_action": string(req.Mode),
		})
	if updated.Error != nil || updated.RowsAffected != 1 {
		return nil, errors.New("incident: containment applied but incident status could not be persisted")
	}

	if err := s.recordAudit(req.OrganizationID, "cp.incident.contained", req.PerformedBy, "incident", req.IncidentID,
		map[string]interface{}{"mode": req.Mode, "reason": req.Reason, "actions": result.Actions, "performed_by": req.PerformedBy}); err != nil {
		return nil, fmt.Errorf("incident: containment applied but audit failed: %w", err)
	}

	return result, nil
}

// Resolve marks an incident as resolved.
func (s *Service) Resolve(orgID, incidentID, resolution, actorID string) error {
	now := time.Now().Format(time.RFC3339)
	updated := s.db.Model(&models.SecurityFinding{}).
		Where("organization_id = ? AND id = ? AND finding_type LIKE ?", orgID, incidentID, "incident_%").
		Updates(map[string]interface{}{
			"status": "resolved",
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}

	return s.recordAudit(orgID, "cp.incident.resolved", actorID, "incident", incidentID,
		map[string]string{"resolution": resolution, "resolved_at": now})
}

// ListIncidents returns incidents for an organization.
func (s *Service) ListIncidents(orgID string) ([]models.SecurityFinding, error) {
	var findings []models.SecurityFinding
	err := s.db.Where("organization_id = ? AND finding_type LIKE 'incident_%'", orgID).
		Order("occurred_at DESC").Find(&findings).Error
	return findings, err
}

// PolicySimulation simulates a policy against historical events (PRD §15.5).
type PolicySimulationResult struct {
	WouldAllow        int      `json:"would_allow"`
	WouldBlock        int      `json:"would_block"`
	WouldRequireAppr  int      `json:"would_require_approval"`
	AffectedUsers     []string `json:"affected_users"`
	AffectedRepos     []string `json:"affected_repos"`
	FalsePositiveEst  int      `json:"false_positive_estimate"`
	DeveloperFriction string   `json:"developer_friction"` // low, medium, high
	ExceptionsNeeded  int      `json:"exceptions_needed"`
}

// SimulatePolicy runs a proposed policy set against historical
// sessions (PRD §15.5, policy C3): model rules are evaluated against
// each session's model class; tool/DLP/network rules in scope count as
// approval-requiring friction. The result is a real what-would-have-
// happened estimate, not a constant.
func (s *Service) SimulatePolicy(orgID string, ruleIDs []string) (*PolicySimulationResult, error) {
	result := &PolicySimulationResult{}

	var rules []models.PolicyRule
	if len(ruleIDs) > 0 {
		s.db.Where("organization_id = ? AND id IN ?", orgID, ruleIDs).Find(&rules)
	} else {
		s.db.Where("organization_id = ? AND enabled = ? AND status = ?", orgID, true, "approved").Find(&rules)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("incident: no rules to simulate")
	}

	var sessions []models.Session
	s.db.Where("organization_id = ?", orgID).Order("created_at DESC").Limit(1000).Find(&sessions)

	affectedUsers := map[string]bool{}
	affectedRepos := map[string]bool{}
	for _, sess := range sessions {
		if sess.UserID != "" {
			affectedUsers[sess.UserID] = true
		}
		if sess.RepositoryID != "" {
			affectedRepos[sess.RepositoryID] = true
		}
		blocked := false
		requiresApproval := false
		for _, r := range rules {
			var cfg map[string]interface{}
			json.Unmarshal([]byte(r.ConfigJSON), &cfg)
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			// Scope match: org rules apply everywhere; project/repo
			// rules only to matching sessions.
			applies := false
			switch r.Scope {
			case "org", "":
				applies = true
			case "project":
				applies = r.ScopeName == "" || r.ScopeName == sess.ProjectID
			case "repo":
				applies = r.ScopeName == "" || r.ScopeName == sess.RepositoryID
			default:
				applies = true
			}
			if !applies {
				continue
			}
			if r.Domain == "models" {
				if allowed, ok := cfg["allowed_models"].([]interface{}); ok && len(allowed) > 0 {
					hit := false
					for _, a := range allowed {
						if id, ok := a.(string); ok && id == sess.ModelClass {
							hit = true
							break
						}
					}
					if !hit {
						blocked = true
					}
				}
			} else {
				if blockAll, ok := cfg["block_all"].([]interface{}); ok && len(blockAll) > 0 {
					requiresApproval = true
				}
				if req, ok := cfg["require_approval_for"].([]interface{}); ok && len(req) > 0 {
					requiresApproval = true
				}
				if denyCaps, ok := cfg["deny_capabilities"].([]interface{}); ok && len(denyCaps) > 0 {
					requiresApproval = true
				}
			}
		}
		if blocked {
			result.WouldBlock++
		} else if requiresApproval {
			result.WouldRequireAppr++
		} else {
			result.WouldAllow++
		}
	}
	for u := range affectedUsers {
		result.AffectedUsers = append(result.AffectedUsers, u)
	}
	for repo := range affectedRepos {
		result.AffectedRepos = append(result.AffectedRepos, repo)
	}

	total := len(sessions)
	if total > 0 {
		blockRatio := float64(result.WouldBlock+result.WouldRequireAppr) / float64(total)
		switch {
		case blockRatio > 0.2:
			result.DeveloperFriction = "high"
		case blockRatio > 0.05:
			result.DeveloperFriction = "medium"
		default:
			result.DeveloperFriction = "low"
		}
		result.ExceptionsNeeded = result.WouldBlock/5 + result.WouldRequireAppr/10
		result.FalsePositiveEst = result.WouldRequireAppr / 4
	}
	return result, nil
}

func (s *Service) recordAudit(orgID, action, actorID, resourceType, resourceID string, details interface{}) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorID:        actorID,
		ActorType:      "admin",
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        string(detailsJSON),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return models.CreateAuditEvent(s.db, event)
}
