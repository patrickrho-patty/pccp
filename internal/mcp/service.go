package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"gorm.io/gorm"
)

// Service implements MCP (Model Context Protocol) server governance (PRD 17.2).
// An MCP server may never become a side door that bypasses File, Network,
// Secrets, PII, or Audit controls.
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
}

// New creates a new MCP governance service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// MCPServer represents a registered MCP server.
type MCPServer struct {
	ID                  string   `json:"id"`
	OrganizationID      string   `json:"organization_id"`
	Name                string   `json:"name"`
	ServerID            string   `json:"server_id"`
	Publisher           string   `json:"publisher"`
	Version             string   `json:"version"`
	PackageHash         string   `json:"package_hash"`
	Signature           string   `json:"signature"`
	SupportedOperations []string `json:"supported_operations"`
	RiskLevel           string   `json:"risk_level"`
	Status              string   `json:"status"`
	VersionPinned       bool     `json:"version_pinned"`
	CreatedAt           string   `json:"created_at"`
}

// ProjectMCPPolicy defines project-specific MCP configuration.
type ProjectMCPPolicy struct {
	ProjectID       string   `json:"project_id"`
	AllowList       []string `json:"allow_list"`
	DenyList        []string `json:"deny_list"`
	RequireApproval bool     `json:"require_approval"`
}

// ArgumentPolicy defines argument-level MCP governance.
type ArgumentPolicy struct {
	ServerID      string            `json:"server_id"`
	Operation     string            `json:"operation"`
	ArgumentRules map[string]string `json:"argument_rules"`
	Redact        []string          `json:"redact_args"`
}

// MCPPolicy defines organization-wide MCP allow/deny configuration.
type MCPPolicy struct {
	OrganizationID       string                     `json:"organization_id"`
	AllowList            []string                   `json:"allow_list"`
	DenyList             []string                   `json:"deny_list"`
	ProjectOverrides     map[string]ProjectMCPPolicy `json:"project_overrides"`
	ArgumentPolicies     []ArgumentPolicy           `json:"argument_policies"`
	ResultScanning       bool                       `json:"result_scanning"`
	TokenQuotaPerSession int                        `json:"token_quota_per_session"`
}

// MCPConnectionRequest is a request to connect an MCP server within a session.
type MCPConnectionRequest struct {
	OrganizationID string                 `json:"organization_id"`
	SessionID      string                 `json:"session_id"`
	HarnessID      string                 `json:"harness_id"`
	ServerID       string                 `json:"server_id"`
	RequestedOps   []string               `json:"requested_operations"`
	Arguments      map[string]interface{} `json:"arguments,omitempty"`
}

// MCPConnectionDecision is the governance decision for an MCP connection.
type MCPConnectionDecision struct {
	Allowed          bool                   `json:"allowed"`
	Reason           string                 `json:"reason"`
	AllowedOps       []string               `json:"allowed_operations"`
	DeniedOps        []string               `json:"denied_operations"`
	RequiresApproval bool                   `json:"requires_approval"`
	TokenBudget      int                    `json:"token_budget"`
	TransformedArgs  map[string]interface{} `json:"transformed_args,omitempty"`
	RuleIDs          []string               `json:"rule_ids"`
}

// RegisterServer registers a new MCP server in the managed registry.
func (s *Service) RegisterServer(server MCPServer) (*MCPServer, error) {
	if server.ID == "" {
		server.ID = dari.GenerateID("mcp")
	}
	if server.Status == "" {
		server.Status = "pending"
	}
	server.CreatedAt = time.Now().Format(time.RFC3339)

	tool := &models.Tool{
		AuditBase: models.AuditBase{
			OrganizationID: server.OrganizationID,
		},
		Name:             server.Name,
		Category:         "mcp",
		ToolClass:        "mcp:" + server.ServerID,
		RequiresApproval: server.RiskLevel == "high" || server.RiskLevel == "critical",
		DangerLevel:      server.RiskLevel,
		Status:           server.Status,
	}

	if err := s.db.Create(tool).Error; err != nil {
		return nil, fmt.Errorf("mcp: register server: %w", err)
	}

	s.recordAudit(server.OrganizationID, "mcp.server.registered", "mcp_server", server.ID,
		fmt.Sprintf("MCP server %s v%s registered", server.Name, server.Version))
	return &server, nil
}

// SetPolicy sets the organization-wide MCP policy.
func (s *Service) SetPolicy(orgID string, policy MCPPolicy) error {
	policyJSON, _ := json.Marshal(policy)
	pack := &models.PolicyPack{
		Base:            models.Base{ID: dari.GenerateID("mcp_policy")},
		OrganizationID:  orgID,
		Name:            "MCP Governance Policy",
		Version:         "1.0",
		Profile:         "enterprise",
		ToolPolicyJSON:  string(policyJSON),
		Status:          "active",
	}
	return s.db.Create(pack).Error
}

// GetPolicy retrieves the organization's MCP policy.
func (s *Service) GetPolicy(orgID string) (*MCPPolicy, error) {
	var pack models.PolicyPack
	if err := s.db.Where("organization_id = ? AND name = ? AND status = 'active'",
		orgID, "MCP Governance Policy").First(&pack).Error; err != nil {
		return &MCPPolicy{OrganizationID: orgID, ResultScanning: true}, nil
	}
	var policy MCPPolicy
	json.Unmarshal([]byte(pack.ToolPolicyJSON), &policy)
	return &policy, nil
}

// EvaluateConnection checks whether an MCP connection request is permitted.
func (s *Service) EvaluateConnection(req MCPConnectionRequest) (*MCPConnectionDecision, error) {
	decision := &MCPConnectionDecision{Allowed: true, AllowedOps: req.RequestedOps}

	policy, _ := s.GetPolicy(req.OrganizationID)

	for _, denied := range policy.DenyList {
		if denied == req.ServerID || denied == "*" {
			decision.Allowed = false
			decision.Reason = fmt.Sprintf("MCP server %s is on the deny list", req.ServerID)
			decision.RuleIDs = append(decision.RuleIDs, "mcp.deny_list")
			return decision, nil
		}
	}

	if len(policy.AllowList) > 0 {
		found := false
		for _, allowed := range policy.AllowList {
			if allowed == req.ServerID || allowed == "*" {
				found = true
				break
			}
		}
		if !found {
			decision.Allowed = false
			decision.Reason = fmt.Sprintf("MCP server %s is not on the allow list", req.ServerID)
			decision.RuleIDs = append(decision.RuleIDs, "mcp.allow_list")
			return decision, nil
		}
	}

	var server models.Tool
	if err := s.db.Where("organization_id = ? AND tool_class = ?",
		req.OrganizationID, "mcp:"+req.ServerID).First(&server).Error; err == nil {
		if server.Status != "active" {
			decision.Allowed = false
			decision.Reason = fmt.Sprintf("MCP server status is %s", server.Status)
			decision.RuleIDs = append(decision.RuleIDs, "mcp.server_status")
			return decision, nil
		}
		if server.RequiresApproval {
			decision.RequiresApproval = true
			decision.Reason = "high-risk MCP server requires approval"
			decision.RuleIDs = append(decision.RuleIDs, "mcp.high_risk")
		}
	}

	if policy.TokenQuotaPerSession > 0 {
		decision.TokenBudget = policy.TokenQuotaPerSession
	}

	if policy.ResultScanning {
		decision.RuleIDs = append(decision.RuleIDs, "mcp.result_scan_required")
	}

	s.recordAudit(req.OrganizationID, "mcp.connection.evaluated", "mcp_server", req.ServerID,
		fmt.Sprintf("Connection from session %s", req.SessionID))
	return decision, nil
}

// KillSwitch immediately disables an MCP server across the organization.
func (s *Service) KillSwitch(orgID, serverID, reason string) error {
	result := s.db.Model(&models.Tool{}).
		Where("organization_id = ? AND tool_class = ?", orgID, "mcp:"+serverID).
		Update("status", "disabled")
	if result.Error != nil {
		return result.Error
	}
	s.recordAudit(orgID, "mcp.kill_switch", "mcp_server", serverID, reason)
	return nil
}

// ListServers returns all registered MCP servers for an organization.
func (s *Service) ListServers(orgID string) ([]models.Tool, error) {
	var tools []models.Tool
	err := s.db.Where("organization_id = ? AND category = 'mcp'", orgID).Find(&tools).Error
	return tools, err
}

func (s *Service) recordAudit(orgID, action, resourceType, resourceID, details string) {
	event := &models.AuditEvent{
		OrganizationID: orgID,
		EventType:      action,
		ActorType:      "system",
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Details:        details,
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(event)
}
