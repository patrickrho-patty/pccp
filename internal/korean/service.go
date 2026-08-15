package korean

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Korean Enterprise-Specific Differentiators (PRD §33).
type Service struct {
	db *gorm.DB
}

// New creates a new Korean enterprise service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// GroupAffiliate represents a Korean 대기업 group/affiliate structure (PRD §33.1).
type GroupAffiliate struct {
	GroupID         string `json:"group_id"`
	GroupName       string `json:"group_name"`
	GroupNameKo     string `json:"group_name_ko"`
	AffiliateID     string `json:"affiliate_id"`
	AffiliateName   string `json:"affiliate_name"`
	AffiliateNameKo string `json:"affiliate_name_ko"`
	IsCore          bool   `json:"is_core"`
}

// SIContractorMode manages SI/outsourced developer mode (PRD §33.2).
type SIContractor struct {
	OrganizationID   string `json:"organization_id"`
	ContractorOrgID  string `json:"contractor_org_id"`
	ContractorName   string `json:"contractor_name"`
	ContractorNameKo string `json:"contractor_name_ko"`
	StartDate        string `json:"start_date"`
	EndDate          string `json:"end_date"`
	ProjectScope     string `json:"project_scope"`
	DataAccessLevel  string `json:"data_access_level"` // limited, standard, elevated
	RequiresEscort   bool   `json:"requires_escort"`   // supervised access
}

// ShadowAIDiscovery detects unauthorized AI tool usage (PRD §33.3).
type ShadowAIFinding struct {
	ID              string `json:"id"`
	OrganizationID  string `json:"organization_id"`
	UserID          string `json:"user_id"`
	ToolType        string `json:"tool_type"` // copilot, chatgpt, claude, generic_llm
	DetectedAt      string `json:"detected_at"`
	Evidence        string `json:"evidence"`
	NetworkEndpoint string `json:"network_endpoint"`
	Severity        string `json:"severity"`
	Status          string `json:"status"` // detected, investigated, resolved, false_positive
}

// ChangeFreezeMode implements critical period mode (PRD §33.13).
type ChangeFreeze struct {
	OrganizationID string   `json:"organization_id"`
	FreezeReason   string   `json:"freeze_reason"`
	FreezeReasonKo string   `json:"freeze_reason_ko"`
	StartedAt      string   `json:"started_at"`
	EndsAt         string   `json:"ends_at,omitempty"`
	AffectedRepos  []string `json:"affected_repos"`
	AllowedActions []string `json:"allowed_actions"` // e.g. ["hotfix", "security_patch"]
	InitiatedBy    string   `json:"initiated_by"`
	IsActive       bool     `json:"is_active"`
}

// EmergencyModelRecall implements emergency model recall (PRD §33.9, DARI §65).
type ModelRecall struct {
	ModelPackageID string   `json:"model_package_id"`
	Reason         string   `json:"reason"`
	ReasonKo       string   `json:"reason_ko"`
	Severity       string   `json:"severity"` // security, compliance, quality
	InitiatedBy    string   `json:"initiated_by"`
	InitiatedAt    string   `json:"initiated_at"`
	AffectedOrgs   []string `json:"affected_organizations"`
}

// DetectShadowAI checks for patterns indicating unauthorized AI tool usage.
func (s *Service) DetectShadowAI(orgID string) ([]ShadowAIFinding, error) {
	// In a real implementation, this would check network logs, DNS queries,
	// browser history, extension installations, etc.
	// For now, we return findings from recorded security events.
	var findings []models.SecurityFinding
	s.db.Where("organization_id = ? AND finding_type LIKE '%shadow_ai%'", orgID).Find(&findings)

	var result []ShadowAIFinding
	for _, f := range findings {
		result = append(result, ShadowAIFinding{
			ID:             f.ID,
			OrganizationID: f.OrganizationID,
			ToolType:       f.RuleID,
			DetectedAt:     f.OccurredAt,
			Evidence:       f.EvidenceJSON,
			Severity:       f.Severity,
			Status:         f.Status,
		})
	}
	return result, nil
}

// InitiateChangeFreeze starts a change-freezing/critical period mode.
func (s *Service) InitiateChangeFreeze(orgID, reason, reasonKo string, affectedRepos []string, allowedActions []string, initiatedBy string) (*ChangeFreeze, error) {
	freeze := &ChangeFreeze{
		OrganizationID: orgID,
		FreezeReason:   reason,
		FreezeReasonKo: reasonKo,
		StartedAt:      time.Now().Format(time.RFC3339),
		AffectedRepos:  affectedRepos,
		AllowedActions: allowedActions,
		InitiatedBy:    initiatedBy,
		IsActive:       true,
	}

	// Record in audit
	details, _ := json.Marshal(freeze)
	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.korean.change_freeze_started",
		ActorID:        initiatedBy,
		ActorType:      "admin",
		Action:         "initiate_change_freeze",
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(audit)

	return freeze, nil
}

// IsChangeFrozen checks if a change-freeze is active for a repository.
func (s *Service) IsChangeFrozen(orgID, repoID string) (bool, *ChangeFreeze, error) {
	// Check audit events for active freezes
	var events []models.AuditEvent
	s.db.Where("organization_id = ? AND event_type IN ('cp.korean.change_freeze_started', 'cp.korean.change_freeze_ended')", orgID).
		Order("occurred_at DESC").Find(&events)

	for _, e := range events {
		var freeze ChangeFreeze
		json.Unmarshal([]byte(e.Details), &freeze)
		if e.EventType == "cp.korean.change_freeze_started" {
			// Check if this repo is affected
			for _, r := range freeze.AffectedRepos {
				if r == repoID || r == "*" {
					return true, &freeze, nil
				}
			}
		}
		if e.EventType == "cp.korean.change_freeze_ended" {
			break // most recent action was ending a freeze
		}
	}

	return false, nil, nil
}

// EmergencyModelRecall triggers a model recall across organizations (PRD §33.9).
func (s *Service) EmergencyModelRecall(modelPackageID, reason, reasonKo, severity, initiatedBy string, affectedOrgs []string) (*ModelRecall, error) {
	recall := &ModelRecall{
		ModelPackageID: modelPackageID,
		Reason:         reason,
		ReasonKo:       reasonKo,
		Severity:       severity,
		InitiatedBy:    initiatedBy,
		InitiatedAt:    time.Now().Format(time.RFC3339),
		AffectedOrgs:   affectedOrgs,
	}

	// Update model package state
	s.db.Model(&models.ModelPackage{}).
		Where("package_id = ?", modelPackageID).
		Update("state", "recalled")

	// Invalidate all endpoint leases
	s.db.Model(&models.EndpointLease{}).
		Where("model_package_id = ? AND status = 'active'", modelPackageID).
		Update("status", "revoked")

	// Record audit
	for _, orgID := range affectedOrgs {
		details, _ := json.Marshal(recall)
		audit := &models.AuditEvent{
			OrganizationID: orgID,
			EventType:      "cp.korean.emergency_model_recall",
			ActorID:        initiatedBy,
			ActorType:      "admin",
			Action:         "emergency_model_recall",
			ResourceType:   "model_package",
			ResourceID:     modelPackageID,
			Details:        string(details),
			Result:         "success",
			OccurredAt:     time.Now().Format(time.RFC3339),
		}
		s.db.Create(audit)
	}

	return recall, nil
}

// SetForcedHarnessVersion forces a minimum harness version (PRD §33.10).
type ForcedVersion struct {
	OrganizationID string `json:"organization_id"`
	MinVersion     string `json:"min_version"`
	ReleaseRing    string `json:"release_ring"` // stable, beta, canary
	Deadline       string `json:"deadline"`
	Reason         string `json:"reason"`
}

// SetForcedHarnessVersion sets a minimum harness version requirement.
func (s *Service) SetForcedHarnessVersion(orgID, minVersion, releaseRing, deadline, reason string) error {
	details, _ := json.Marshal(ForcedVersion{
		OrganizationID: orgID,
		MinVersion:     minVersion,
		ReleaseRing:    releaseRing,
		Deadline:       deadline,
		Reason:         reason,
	})

	audit := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      "cp.korean.forced_harness_version",
		ActorType:      "admin",
		Action:         "set_forced_version",
		Details:        string(details),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	return s.db.Create(audit).Error
}

// GetAISkillsMatrix returns the AI skills matrix for the organization (PRD §33.7).
type SkillMatrixEntry struct {
	UserID       string   `json:"user_id"`
	UserName     string   `json:"user_name"`
	UserNameKo   string   `json:"user_name_ko"`
	Department   string   `json:"department"`
	AIUsageScore float64  `json:"ai_usage_score"`
	ToolClasses  []string `json:"tool_classes_used"`
	Sessions     int      `json:"sessions"`
	LastActive   string   `json:"last_active"`
}

// GetAISkillsMatrix returns the organization's AI skills matrix.
func (s *Service) GetAISkillsMatrix(orgID string) ([]SkillMatrixEntry, error) {
	var users []models.User
	s.db.Where("organization_id = ?", orgID).Find(&users)

	var result []SkillMatrixEntry
	for _, user := range users {
		entry := SkillMatrixEntry{
			UserID:     user.ID,
			UserName:   user.Name,
			UserNameKo: user.NameKo,
			Department: user.BusinessUnitID,
		}

		var sessionCount int64
		s.db.Model(&models.Session{}).
			Where("user_id = ? AND status = 'active'", user.ID).Count(&sessionCount)
		entry.Sessions = int(sessionCount)

		result = append(result, entry)
	}
	return result, nil
}

// GenerateGovernanceBrief creates an executive weekly AI governance brief (PRD §33.12).
type GovernanceBrief struct {
	OrganizationID   string   `json:"organization_id"`
	WeekOf           string   `json:"week_of"`
	TotalSessions    int      `json:"total_sessions"`
	ActiveHarnesses  int      `json:"active_harnesses"`
	SecurityFindings int      `json:"security_findings"`
	ModelInvocations int      `json:"model_invocations"`
	CodeChanges      int      `json:"code_changes"`
	ApprovalRate     float64  `json:"approval_rate"`
	ComplianceStatus string   `json:"compliance_status"`
	Recommendations  []string `json:"recommendations"`
}

// GenerateGovernanceBrief creates the executive brief.
func (s *Service) GenerateGovernanceBrief(orgID string) (*GovernanceBrief, error) {
	brief := &GovernanceBrief{
		OrganizationID: orgID,
		WeekOf:         time.Now().Format("2006-01-02"),
	}

	var sessionCount, harnessCount, findingCount, actionCount int64
	s.db.Model(&models.Session{}).Where("organization_id = ?", orgID).Count(&sessionCount)
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ('active','enrolled')", orgID).Count(&harnessCount)
	s.db.Model(&models.SecurityFinding{}).Where("organization_id = ?", orgID).Count(&findingCount)
	s.db.Model(&models.ActionEnvelope{}).Where("organization_id = ? AND action_type = 'ai.inference'", orgID).Count(&actionCount)

	brief.TotalSessions = int(sessionCount)
	brief.ActiveHarnesses = int(harnessCount)
	brief.SecurityFindings = int(findingCount)
	brief.ModelInvocations = int(actionCount)

	// Compliance status assessment
	if findingCount > 10 {
		brief.ComplianceStatus = "주의 필요" // attention needed
	} else if findingCount > 0 {
		brief.ComplianceStatus = "모니터링" // monitoring
	} else {
		brief.ComplianceStatus = "양호" // good
	}

	// Recommendations
	brief.Recommendations = []string{
		"정기적인 보안 교육 실施 권장",
		"AI 사용 가이드라인 업데이트 검토",
	}
	if findingCount > 5 {
		brief.Recommendations = append(brief.Recommendations, "보안 발견 건수가 높음 - 즉시 조치 필요")
	}

	return brief, nil
}

// Ensure fmt import used
var _ = fmt.Sprintf
