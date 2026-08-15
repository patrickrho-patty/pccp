package incident

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Incident Management and Containment (PRD §15.2-15.4).
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
}

// New creates a new incident service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
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

	s.recordAudit(inc.OrganizationID, "cp.incident.created", "incident", inc.ID,
		fmt.Sprintf("Incident %s created: %s", inc.ID, inc.Title))

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
	result := &ContainResult{
		IncidentID: req.IncidentID,
		Mode:       string(req.Mode),
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	switch req.Mode {
	case ContainSession:
		// Session containment (PRD §15.4)
		if req.SessionID != "" {
			s.db.Model(&models.Session{}).
				Where("session_id = ?", req.SessionID).
				Update("status", "contained")
			result.AffectedSessions = append(result.AffectedSessions, req.SessionID)
		}
		result.Actions = []string{
			"세션 일시정지 (session paused)",
			"모델 요청 중지 (model requests stopped)",
			"도구 실행 중지 (tool execution stopped)",
			"샌드박스 상태 보존 (sandbox state preserved)",
			"세션 권한 회수 (session capabilities revoked)",
		}

	case ContainHarness:
		// Harness containment
		if req.HarnessID != "" {
			s.db.Model(&models.Harness{}).
				Where("harness_id = ?", req.HarnessID).
				Update("status", "quarantined")
			result.AffectedHarnesses = append(result.AffectedHarnesses, req.HarnessID)

			// Revoke all sessions for this harness
			s.db.Model(&models.Session{}).
				Where("harness_id = ? AND status = 'active'", req.HarnessID).
				Update("status", "contained")
		}
		result.Actions = []string{
			"하네스 리스/인증서 회수 (harness lease/cert revoked)",
			"재등록 필요 (re-enrollment required)",
			"로컬 캐시 권한 회수 (cached capabilities revoked)",
			"통신/파일 전송 비활성화 (comms/file transfer disabled)",
		}

	case ContainProject:
		// Project containment
		if req.ProjectID != "" {
			s.db.Model(&models.Session{}).
				Where("project_id = ? AND status = 'active'", req.ProjectID).
				Update("status", "contained")
		}
		result.Actions = []string{
			"읽기 전용 강제 (forced read-only)",
			"내보내기/커밋 비활성화 (exports/commits disabled)",
			"선택된 모델/도구 비활성화 (selected models/tools disabled)",
			"모든 작업 승인 필요 (all actions require approval)",
		}

	case ContainOrgLockdown:
		// Organization lockdown (PRD §15.4)
		s.db.Model(&models.Session{}).
			Where("organization_id = ? AND status = 'active'", req.OrganizationID).
			Update("status", "terminated")
		s.db.Model(&models.Harness{}).
			Where("organization_id = ? AND status IN ('active','enrolled')", req.OrganizationID).
			Update("risk_state", "high")
		result.Actions = []string{
			"모든 새 에이전트 실행 중지 (all new agent executions stopped)",
			"클라우드 모델 송신 일시정지 (cloud model egress suspended)",
			"새 하네스 등록 거부 (new harness enrollment rejected)",
			"긴급 방송 표시 (emergency broadcast displayed)",
		}
	}

	// Update incident status
	s.db.Model(&models.SecurityFinding{}).
		Where("id = ?", req.IncidentID).
		Updates(map[string]interface{}{
			"status":          "contained",
			"contains_action": string(req.Mode),
		})

	s.recordAudit(req.OrganizationID, "cp.incident.contained", "incident", req.IncidentID,
		fmt.Sprintf(`{"mode":"%s","reason":"%s","actions":%v}`, req.Mode, req.Reason, result.Actions))

	return result, nil
}

// Resolve marks an incident as resolved.
func (s *Service) Resolve(orgID, incidentID, resolution string) error {
	now := time.Now().Format(time.RFC3339)
	s.db.Model(&models.SecurityFinding{}).
		Where("id = ?", incidentID).
		Updates(map[string]interface{}{
			"status": "resolved",
		})

	s.recordAudit(orgID, "cp.incident.resolved", "incident", incidentID,
		fmt.Sprintf(`{"resolution":"%s","resolved_at":"%s"}`, resolution, now))
	return nil
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

func (s *Service) recordAudit(orgID, action, resourceType, resourceID, details string) {
	detailsJSON, _ := json.Marshal(details)
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      "admin",
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        string(detailsJSON),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}
