package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements tool/runtime governance (PRD §17).
type Service struct {
	db *gorm.DB
}

// New creates a new tools service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// RegisterTool registers a development tool in the registry (PRD §17.1).
func (s *Service) RegisterTool(orgID, name, nameKo, category, toolClass string, dangerLevel string, requiresApproval bool) (*models.Tool, error) {
	// Dedup: check if tool already exists for this org
	var existing models.Tool
	if err := s.db.Where("organization_id = ? AND name = ?", orgID, name).First(&existing).Error; err == nil {
		return &existing, nil // already registered, return existing
	}

	tool := &models.Tool{
		AuditBase: models.AuditBase{
			OrganizationID: orgID,
			Classification: "internal",
		},
		Name:             name,
		NameKo:           nameKo,
		Category:         category, // read, write, execute, network
		ToolClass:        toolClass,
		DangerLevel:      dangerLevel,
		RequiresApproval: requiresApproval,
		Status:           "active",
	}
	if err := s.db.Create(tool).Error; err != nil {
		return nil, fmt.Errorf("tools: register tool: %w", err)
	}
	return tool, nil
}

// RequestApproval requests approval for a tool use within an exchange.
func (s *Service) RequestApproval(orgID, sessionID, exchangeID, actionID, approvalType, requestedBy string) (*models.Approval, error) {
	approval := &models.Approval{
		OrganizationID: orgID,
		SessionID:      sessionID,
		ExchangeID:     exchangeID,
		ActionID:       actionID,
		ApprovalType:   approvalType,
		RequestedBy:    requestedBy,
		Decision:       "pending",
		ExpiresAt:      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}
	if err := s.db.Create(approval).Error; err != nil {
		return nil, fmt.Errorf("tools: request approval: %w", err)
	}
	return approval, nil
}

// UpdateTool updates a registered tool's mutable fields (org-scoped).
// Nil pointers leave the corresponding field unchanged.
func (s *Service) UpdateTool(orgID, id string, name, nameKo, category, toolClass, dangerLevel *string, requiresApproval *bool, status *string) (*models.Tool, error) {
	var tool models.Tool
	if err := s.db.Where("organization_id = ? AND (id = ? OR name = ?)", orgID, id, id).First(&tool).Error; err != nil {
		return nil, fmt.Errorf("tools: tool not found: %w", err)
	}
	updates := map[string]interface{}{}
	if name != nil {
		updates["name"] = *name
	}
	if nameKo != nil {
		updates["name_ko"] = *nameKo
	}
	if category != nil {
		updates["category"] = *category
	}
	if toolClass != nil {
		updates["tool_class"] = *toolClass
	}
	if dangerLevel != nil {
		updates["danger_level"] = *dangerLevel
	}
	if requiresApproval != nil {
		updates["requires_approval"] = *requiresApproval
	}
	if status != nil {
		updates["status"] = *status
	}
	if len(updates) > 0 {
		if err := s.db.Model(&tool).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("tools: update tool: %w", err)
		}
	}
	return &tool, nil
}

// DeleteTool removes a tool from the registry. Un-registering a tool
// removes its authorization: CheckToolAuthorization reports "tool not
// registered in organization" afterwards.
func (s *Service) DeleteTool(orgID, id string) (*models.Tool, error) {
	var tool models.Tool
	if err := s.db.Where("organization_id = ? AND (id = ? OR name = ?)", orgID, id, id).First(&tool).Error; err != nil {
		return nil, fmt.Errorf("tools: tool not found: %w", err)
	}
	if err := s.db.Delete(&tool).Error; err != nil {
		return nil, fmt.Errorf("tools: delete tool: %w", err)
	}
	return &tool, nil
}

// DecideApproval records an approval decision and reflects it on the
// linked Tool row: approving clears the tool's requires_approval flag
// (the reviewer has granted it), any other decision keeps the flag so
// the tool remains gated. Audit recording happens at the API layer.
func (s *Service) DecideApproval(approvalID, reviewerID, decision, reason string) (*models.Approval, error) {
	var approval models.Approval
	if err := s.db.First(&approval, "id = ?", approvalID).Error; err != nil {
		return nil, fmt.Errorf("tools: approval not found: %w", err)
	}
	if err := s.db.Model(&models.Approval{}).Where("id = ?", approvalID).
		Updates(map[string]interface{}{
			"decision":        decision,
			"decision_reason": reason,
			"decided_by":      reviewerID,
			"decided_at":      time.Now().Format(time.RFC3339),
		}).Error; err != nil {
		return nil, fmt.Errorf("tools: decide approval: %w", err)
	}

	// Reflect the decision on the Tool row. Registry-fed approvals carry
	// the tool's ID in ActionID; legacy runtime rows encode the tool name
	// in ApprovalType ("<tool>_approval").
	toolRef := approval.ActionID
	if toolRef == "" {
		toolRef = strings.TrimSuffix(approval.ApprovalType, "_approval")
	}
	if toolRef != "" && decision == "approved" {
		if err := s.db.Model(&models.Tool{}).
			Where("organization_id = ? AND (id = ? OR name = ?)", approval.OrganizationID, toolRef, toolRef).
			Update("requires_approval", false).Error; err != nil {
			return nil, fmt.Errorf("tools: clear tool approval flag: %w", err)
		}
	}
	return &approval, nil
}

// CheckToolAuthorization determines if a tool use is allowed.
// Per DARI invariant 5: no tool proposal grants authority by itself.
type ToolAuthResult struct {
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requires_approval"`
	ApprovalID       string `json:"approval_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// CheckToolAuthorization checks whether a tool use is permitted.
func (s *Service) CheckToolAuthorization(orgID, toolName, sessionID, leaseID string) (*ToolAuthResult, error) {
	return s.CheckToolAuthorizationFull(orgID, toolName, sessionID, leaseID, "", "")
}

// CheckToolAuthorizationFull adds project allowlist + integrity-digest
// enforcement (web/14 features 7 + E).
func (s *Service) CheckToolAuthorizationFull(orgID, toolName, sessionID, leaseID, projectID, reportedDigest string) (*ToolAuthResult, error) {
	var tool models.Tool
	err := s.db.Where("organization_id = ? AND name = ?", orgID, toolName).First(&tool).Error
	if err != nil {
		return &ToolAuthResult{
			Allowed: false,
			Reason:  "tool not registered in organization",
		}, nil
	}

	if tool.Status != "active" {
		return &ToolAuthResult{
			Allowed: false,
			Reason:  "tool is not active",
		}, nil
	}

	// A tool proposal never grants authority by itself (DARI invariant 5).
	// The capability lease must explicitly allow the tool class.
	var lease models.CapabilityLease
	if err := s.db.Where("lease_id = ?", leaseID).First(&lease).Error; err != nil {
		return &ToolAuthResult{
			Allowed: false,
			Reason:  "capability lease not found",
		}, nil
	}

	var allowedTools []string
	json.Unmarshal([]byte(lease.ToolClasses), &allowedTools)

	toolClassAllowed := false
	for _, tc := range allowedTools {
		if tc == tool.ToolClass || tc == "*" {
			toolClassAllowed = true
			break
		}
	}

	if !toolClassAllowed {
		return &ToolAuthResult{
			Allowed: false,
			Reason:  fmt.Sprintf("tool class %s not permitted by capability lease", tool.ToolClass),
		}, nil
	}

	// Per-project allowlist (feature 7): when the project has one, the
	// tool must be on it.
	if projectID != "" {
		var pinned int64
		s.db.Model(&models.ProjectToolAllowlist{}).
			Where("organization_id = ? AND project_id = ?", orgID, projectID).Count(&pinned)
		if pinned > 0 {
			var hit int64
			s.db.Model(&models.ProjectToolAllowlist{}).
				Where("organization_id = ? AND project_id = ? AND tool_name = ?", orgID, projectID, toolName).Count(&hit)
			if hit == 0 {
				return &ToolAuthResult{Allowed: false, Reason: "tool not on the project allowlist"}, nil
			}
		}
	}

	// Integrity digest (E): a pinned tool must match its runtime digest.
	if ok, reason := s.VerifyToolDigest(tool, reportedDigest); !ok {
		return &ToolAuthResult{Allowed: false, Reason: "tool integrity: " + reason}, nil
	}

	// High-danger tools require approval
	if tool.RequiresApproval || tool.DangerLevel == "high" || tool.DangerLevel == "critical" {
		approval, _ := s.RequestApproval(orgID, sessionID, "", "", toolName+"_approval", "")
		return &ToolAuthResult{
			Allowed:          false,
			RequiresApproval: true,
			ApprovalID:       approval.ID,
			Reason:           "tool requires reviewer approval",
		}, nil
	}

	return &ToolAuthResult{
		Allowed: true,
	}, nil
}

// ListTools returns all registered tools for an organization.
func (s *Service) ListTools(orgID string) ([]models.Tool, error) {
	var tools []models.Tool
	s.db.Where("organization_id = ?", orgID).Find(&tools)
	return tools, nil
}

// ListPendingApprovals returns pending approvals. The reviewer queue is
// fed from the tool registry itself: every active tool still flagged
// requires_approval gets an idempotent pending Approval row, so the
// queue reflects reality even before a runtime caller requests one
// (CheckToolAuthorization remains the runtime entry point).
func (s *Service) ListPendingApprovals(orgID string) ([]models.Approval, error) {
	var gatedTools []models.Tool
	s.db.Where("organization_id = ? AND requires_approval = ? AND status = 'active'", orgID, true).Find(&gatedTools)
	for _, t := range gatedTools {
		var existing models.Approval
		err := s.db.Where("organization_id = ? AND action_id = ? AND decision = 'pending'", orgID, t.ID).
			First(&existing).Error
		if err != nil {
			s.db.Create(&models.Approval{
				OrganizationID: orgID,
				ActionID:       t.ID,
				ApprovalType:   "tool_use",
				Decision:       "pending",
				ExpiresAt:      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			})
		}
	}
	var approvals []models.Approval
	s.db.Where("organization_id = ? AND decision = 'pending'", orgID).
		Order("created_at DESC").Find(&approvals)
	return approvals, nil
}

// SeedDefaultTools registers the default tool set for an organization.
func (s *Service) SeedDefaultTools(orgID string) error {
	defaults := []struct {
		Name      string
		NameKo    string
		Category  string
		ToolClass string
		Danger    string
		NeedsAppr bool
	}{
		{"file.read", "파일 읽기", "read", "read", "low", false},
		{"file.write", "파일 쓰기", "write", "write", "medium", false},
		{"file.delete", "파일 삭제", "write", "delete", "high", true},
		{"shell.execute", "셸 명령 실행", "execute", "execute", "high", true},
		{"git.commit", "Git 커밋", "execute", "git", "medium", false},
		{"git.push", "Git 푸시", "execute", "git", "high", true},
		{"network.http", "HTTP 요청", "network", "network", "medium", true},
		{"search.code", "코드 검색", "read", "search", "low", false},
		{"test.run", "테스트 실행", "execute", "test", "low", false},
		{"package.install", "패키지 설치", "execute", "install", "critical", true},
	}

	for _, d := range defaults {
		s.RegisterTool(orgID, d.Name, d.NameKo, d.Category, d.ToolClass, d.Danger, d.NeedsAppr)
	}
	return nil
}

// GenerateApprovalID generates a unique approval ID.
func GenerateApprovalID() string {
	return dari.GenerateID("appr")
}
