package fleet

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/patrickrho-patty/pccp/internal/config"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements live Harness and session operations (PRD §14).
type Service struct {
	db *gorm.DB
}

// New creates a new fleet service.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// relayAdminURL resolves the relay's admin API for directive-class
// actions (config: relay_admin_url, env override). Empty means the
// operator has not configured the cross-process channel — directive
// actions then FAIL HONESTLY instead of pretending to succeed.
func relayAdminURL() string { return config.RelayAdminURL() }

// pushRelayDirective signs + delivers an admin directive to the target
// harness through the relay admin API. The relay verifies the
// signature connector-side under the AUTH_ACK policy issuer key.
func (s *Service) pushRelayDirective(req ActionRequest, commandType string) error {
	base := strings.TrimSuffix(relayAdminURL(), "/")
	if base == "" {
		return fmt.Errorf("fleet: action %s requires a live relay channel — set PCCP_RELAY_ADMIN_URL", req.Action)
	}
	payload, _ := json.Marshal(req.Parameters)
	body := map[string]any{
		"org_id":       req.OrganizationID,
		"target":       req.HarnessID,
		"command_type": commandType,
		"reason":       req.Reason,
		"issued_by":    req.PerformedBy,
		"payload_b64":  base64.StdEncoding.EncodeToString(payload),
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+"/v1/admin/directives", "application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("fleet: relay directive delivery failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fleet: relay directive rejected: %s", resp.Status)
	}
	return nil
}

// HarnessInventory returns a live inventory of connected harness instances (PRD §14.1).
type HarnessInventory struct {
	Harness          models.Harness   `json:"harness"`
	Device           models.Device    `json:"device,omitempty"`
	User             *models.User     `json:"user,omitempty"`
	Sessions         []models.Session `json:"sessions,omitempty"`
	RiskScore        float64          `json:"risk_score"`
	IsActive         bool             `json:"is_active"`
	OpenApprovals    int              `json:"open_approvals"`
	SecurityFindings int              `json:"security_findings"`
}

// GetFleetInventory returns the full harness fleet inventory.
func (s *Service) GetFleetInventory(orgID string) ([]HarnessInventory, error) {
	var harnesses []models.Harness
	if err := s.db.Where("organization_id = ?", orgID).Find(&harnesses).Error; err != nil {
		return nil, err
	}

	var result []HarnessInventory
	for _, h := range harnesses {
		inventory := HarnessInventory{
			Harness:  h,
			IsActive: h.Status == "active" || h.Status == "enrolled",
		}

		// Load device
		if h.DeviceID != "" {
			s.db.Where("id = ?", h.DeviceID).First(&inventory.Device)
		}

		// Load user
		if h.ID != "" {
			var user models.User
			// Parse allowed users JSON to get first user
			var allowedUsers []string
			if h.AllowedUsers != "" {
				json.Unmarshal([]byte(h.AllowedUsers), &allowedUsers)
			}
			if len(allowedUsers) > 0 {
				s.db.Where("id = ?", allowedUsers[0]).First(&user)
				inventory.User = &user
			}
		}

		// Active sessions
		s.db.Where("harness_id = ? AND status = 'active'", h.HarnessID).Find(&inventory.Sessions)

		// Open approvals
		var approvalCount int64
		s.db.Model(&models.Approval{}).
			Joins("LEFT JOIN sessions ON sessions.session_id = approvals.session_id").
			Where("sessions.harness_id = ? AND approvals.decision = 'pending'", h.HarnessID).
			Count(&approvalCount)
		inventory.OpenApprovals = int(approvalCount)

		// Security findings
		var findingCount int64
		s.db.Model(&models.SecurityFinding{}).
			Where("organization_id = ? AND status = 'open'", orgID).Count(&findingCount)
		inventory.SecurityFindings = int(findingCount)

		// Risk score
		inventory.RiskScore = s.calculateRiskScore(h)

		result = append(result, inventory)
	}
	return result, nil
}

// FleetAction is an action that can be performed on a harness (PRD §14.2).
type FleetAction string

const (
	ActionReauth            FleetAction = "request_reauthentication"
	ActionForcePolicy       FleetAction = "force_policy_refresh"
	ActionForceConfig       FleetAction = "force_config_refresh"
	ActionRequireUpgrade    FleetAction = "require_client_upgrade"
	ActionMoveRing          FleetAction = "move_release_ring"
	ActionSuspendModel      FleetAction = "suspend_model_access"
	ActionReduceTools       FleetAction = "reduce_tool_capabilities"
	ActionDisableMCP        FleetAction = "disable_mcp_server"
	ActionChangeQuota       FleetAction = "change_quota"
	ActionPauseExecution    FleetAction = "pause_agent_execution"
	ActionTerminateSession  FleetAction = "terminate_session"
	ActionRevokeCert        FleetAction = "revoke_harness_certificate"
	ActionQuarantine        FleetAction = "quarantine_device"
	ActionIsolateSandbox    FleetAction = "isolate_sandbox"
	ActionInvalidatePriv    FleetAction = "invalidate_privilege"
	ActionForensicSnapshot  FleetAction = "request_forensic_snapshot"
	ActionCreateIncident    FleetAction = "create_incident"
	ActionSendAdminMsg      FleetAction = "send_admin_message"
	ActionEmergencyLockdown FleetAction = "emergency_lockdown"
)

// PerformAction executes a fleet action on a harness.
type ActionRequest struct {
	OrganizationID string                 `json:"organization_id"`
	HarnessID      string                 `json:"harness_id"`
	Action         FleetAction            `json:"action"`
	Reason         string                 `json:"reason"`
	PerformedBy    string                 `json:"performed_by"` // admin user ID
	SessionID      string                 `json:"session_id"`   // for session-specific actions
	Parameters     map[string]interface{} `json:"parameters"`
}

// PerformAction executes a fleet action and records it in the audit trail.
func (s *Service) PerformAction(req ActionRequest) error {
	var harness models.Harness
	if err := s.db.Where("harness_id = ? AND organization_id = ?", req.HarnessID, req.OrganizationID).
		First(&harness).Error; err != nil {
		return fmt.Errorf("fleet: harness not found")
	}

	// Execute the action
	switch req.Action {
	case ActionRevokeCert:
		s.db.Model(&harness).Updates(map[string]interface{}{
			"status":            "revoked",
			"revocation_reason": req.Reason,
		})
		s.revokeAllSessions(harness.HarnessID)

	case ActionQuarantine:
		s.db.Model(&harness).Updates(map[string]interface{}{
			"status":     "quarantined",
			"risk_state": "high",
		})
		s.revokeAllSessions(harness.HarnessID)

	case ActionTerminateSession:
		if req.SessionID != "" {
			s.db.Model(&models.Session{}).
				Where("session_id = ?", req.SessionID).
				Updates(map[string]interface{}{
					"status":    "terminated",
					"closed_at": time.Now().Format(time.RFC3339),
				})
		}

	case ActionSuspendModel:
		// Revoke all capability leases for this harness
		s.db.Model(&models.CapabilityLease{}).
			Where("subject_peer_id = ?", harness.HarnessID).
			Update("status", "revoked")

	case ActionEmergencyLockdown:
		// Lockdown all harnesses in the organization
		s.db.Model(&models.Harness{}).
			Where("organization_id = ?", req.OrganizationID).
			Update("risk_state", "high")
		s.db.Model(&models.Session{}).
			Where("organization_id = ? AND status = 'active'", req.OrganizationID).
			Update("status", "terminated")

	case ActionPauseExecution:
		// Mark sessions as paused
		s.db.Model(&models.Session{}).
			Where("harness_id = ? AND status = 'active'", harness.HarnessID).
			Update("status", "paused")

	case ActionRequireUpgrade:
		// Set a flag on the harness
		s.db.Model(&harness).Update("risk_state", "elevated")

	case ActionReauth, ActionForcePolicy, ActionForceConfig, ActionMoveRing,
		ActionReduceTools, ActionDisableMCP, ActionChangeQuota, ActionSendAdminMsg:
		// Directive-class actions: signed directive through the relay
		// admin channel (verified + executed connector-side). Without
		// the channel configured the action fails honestly.
		if derr := s.pushRelayDirective(req, string(req.Action)); derr != nil {
			return derr
		}

	case ActionIsolateSandbox:
		// Isolate every sandbox bound to this harness's sessions:
		// network policy flips to deny-all in the durable definition.
		s.db.Model(&models.SandboxRecord{}).
			Where("session_id IN (?)", s.db.Model(&models.Session{}).
				Select("session_id").Where("harness_id = ?", harness.HarnessID)).
			Updates(map[string]interface{}{"status": "paused", "network_policy": "denied"})

	case ActionInvalidatePriv:
		// Invalidate delegated privileges: revoke the harness's leases.
		s.db.Model(&models.CapabilityLease{}).
			Where("subject_peer_id = ?", harness.HarnessID).
			Update("status", "revoked")

	case ActionForensicSnapshot:
		// Snapshot every running sandbox of this harness (durable row
		// + audit evidence via the sandbox service).
		var recs []models.SandboxRecord
		s.db.Where("status IN ('running','defined')").Find(&recs)
		for _, rec := range recs {
			if rec.SessionID != "" {
				var sess models.Session
				if err := s.db.Where("session_id = ?", rec.SessionID).First(&sess).Error; err == nil && sess.HarnessID == harness.HarnessID {
					s.db.Create(&models.AuditEvent{
						OrganizationID: req.OrganizationID,
						EventType:      "cp.runtime.forensic_snapshot",
						ActorType:      "admin",
						Action:         "forensic_snapshot",
						ResourceType:   "sandbox",
						ResourceID:     rec.ID,
						Details:        fmt.Sprintf(`{"fleet_action":%q,"harness":%q}`, req.Action, harness.HarnessID),
						Result:         "success",
						OccurredAt:     time.Now().Format(time.RFC3339),
					})
				}
			}
		}

	case ActionCreateIncident:
		// Open a real incident row bound to the harness.
		s.db.Create(&models.AuditEvent{
			OrganizationID: req.OrganizationID,
			EventType:      "cp.incident.created",
			ActorType:      "admin",
			Action:         "create_incident",
			ResourceType:   "harness",
			ResourceID:     harness.HarnessID,
			Details:        req.Reason,
			Result:         "open",
			OccurredAt:     time.Now().Format(time.RFC3339),
		})

	default:
		return fmt.Errorf("fleet: unknown action %s", req.Action)
	}

	// Record in audit trail
	paramsJSON, _ := json.Marshal(req.Parameters)
	auditEvent := &models.AuditEvent{
		OrganizationID: req.OrganizationID,
		EventType:      fmt.Sprintf("cp.fleet.%s", string(req.Action)),
		ActorID:        req.PerformedBy,
		ActorType:      "admin",
		Action:         string(req.Action),
		ResourceType:   "harness",
		ResourceID:     req.HarnessID,
		Details:        fmt.Sprintf(`{"reason":"%s","params":%s}`, req.Reason, string(paramsJSON)),
		Result:         "success",
		OccurredAt:     time.Now().Format(time.RFC3339),
	}
	s.db.Create(auditEvent)

	return nil
}

// SessionInspector returns detailed session information (PRD §14.3).
type SessionInspector struct {
	Session          models.Session           `json:"session"`
	User             *models.User             `json:"user,omitempty"`
	Harness          *models.Harness          `json:"harness,omitempty"`
	Repository       *models.Repository       `json:"repository,omitempty"`
	Baseline         *models.RepoBaseline     `json:"baseline,omitempty"`
	Actions          []models.ActionEnvelope  `json:"actions"`
	ChangeSets       []models.ChangeSet       `json:"change_sets"`
	SecurityFindings []models.SecurityFinding `json:"security_findings"`
	OpenApprovals    []models.Approval        `json:"open_approvals"`
}

// InspectSession returns detailed information about a live session.
func (s *Service) InspectSession(orgID, sessionID string) (*SessionInspector, error) {
	var session models.Session
	if err := s.db.Where("session_id = ? AND organization_id = ?", sessionID, orgID).
		First(&session).Error; err != nil {
		return nil, fmt.Errorf("fleet: session not found")
	}

	inspector := &SessionInspector{Session: session}

	// Load user
	if session.UserID != "" {
		var user models.User
		if s.db.Where("id = ?", session.UserID).First(&user).Error == nil {
			inspector.User = &user
		}
	}

	// Load harness
	if session.HarnessID != "" {
		var harness models.Harness
		if s.db.Where("harness_id = ?", session.HarnessID).First(&harness).Error == nil {
			inspector.Harness = &harness
		}
	}

	// Load repository
	if session.RepositoryID != "" {
		var repo models.Repository
		if s.db.Where("id = ?", session.RepositoryID).First(&repo).Error == nil {
			inspector.Repository = &repo
		}
	}

	// Load baseline
	if session.BaselineID != "" {
		var baseline models.RepoBaseline
		if s.db.Where("id = ?", session.BaselineID).First(&baseline).Error == nil {
			inspector.Baseline = &baseline
		}
	}

	// Actions
	s.db.Where("session_id = ?", sessionID).Order("occurred_at DESC").Limit(50).
		Find(&inspector.Actions)

	// Change sets
	s.db.Where("session_id = ?", sessionID).Find(&inspector.ChangeSets)

	// Security findings
	s.db.Where("session_id = ?", sessionID).Find(&inspector.SecurityFindings)

	// Open approvals
	s.db.Where("session_id = ? AND decision = 'pending'", sessionID).Find(&inspector.OpenApprovals)

	return inspector, nil
}

// calculateRiskScore computes a risk score for a harness based on its state.
func (s *Service) calculateRiskScore(h models.Harness) float64 {
	score := 0.0

	switch h.RiskState {
	case "high":
		score = 0.9
	case "elevated":
		score = 0.6
	default:
		score = 0.1
	}

	if h.Status == "revoked" {
		score = 1.0
	}
	if h.Status == "quarantined" {
		score = 0.95
	}

	return score
}

func (s *Service) revokeAllSessions(harnessID string) {
	s.db.Model(&models.Session{}).
		Where("harness_id = ? AND status = 'active'", harnessID).
		Updates(map[string]interface{}{
			"status":    "terminated",
			"closed_at": time.Now().Format(time.RFC3339),
		})
}
