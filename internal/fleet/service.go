package fleet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service implements live Harness and session operations (PRD §14).
type Service struct {
	db            *gorm.DB
	lifecycle     *sessionlifecycle.Service
	revokeHarness func(orgID, harnessID, reason, actorID string) error
	directive     func(ActionRequest, string) error
	revokeRelay   func(ActionRequest) error
}

func (s *Service) SetHarnessRevoker(revoke func(orgID, harnessID, reason, actorID string) error) {
	s.revokeHarness = revoke
}

func (s *Service) SetDirectiveSender(sender func(ActionRequest, string) error) {
	if sender != nil {
		s.directive = sender
	}
}

func (s *Service) SetRevocationSender(sender func(ActionRequest) error) {
	if sender != nil {
		s.revokeRelay = sender
	}
}

// New creates a new fleet service.
func New(db *gorm.DB, lifecycles ...*sessionlifecycle.Service) *Service {
	lifecycle := sessionlifecycle.New(db)
	if len(lifecycles) > 0 && lifecycles[0] != nil {
		lifecycle = lifecycles[0]
	}
	service := &Service{db: db, lifecycle: lifecycle}
	service.directive = service.pushRelayDirective
	service.revokeRelay = service.pushRelayRevocation
	return service
}

// relayAdminURL resolves the relay's admin API for directive-class
// actions (config: relay_admin_url, env override). Empty means the
// operator has not configured the cross-process channel — directive
// actions then FAIL HONESTLY instead of pretending to succeed.
func relayAdminURL() string { return config.RelayAdminURL() }

// RelayDirective is the authenticated control-plane command accepted by the
// relay admin API. Delivery succeeds only when at least one live harness
// receives the command; a 2xx response with delivered=0 is not an execution.
type RelayDirective struct {
	Context        context.Context
	OrganizationID string
	HarnessID      string
	CommandType    string
	Reason         string
	IssuedBy       string
	Parameters     map[string]interface{}
}

func DeliverRelayDirective(d RelayDirective) error {
	base := strings.TrimSuffix(relayAdminURL(), "/")
	if base == "" {
		return fmt.Errorf("relay directive requires a live relay channel — set PCCP_RELAY_ADMIN_URL")
	}
	token := strings.TrimSpace(config.LoadRelayFromEnv().ControlPlaneToken)
	if token == "" {
		return fmt.Errorf("relay directive requires PCCP_CP_TOKEN")
	}
	payload, err := json.Marshal(d.Parameters)
	if err != nil {
		return fmt.Errorf("relay directive payload: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"org_id": d.OrganizationID, "target": d.HarnessID,
		"command_type": d.CommandType, "reason": d.Reason,
		"issued_by":   d.IssuedBy,
		"payload_b64": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		return fmt.Errorf("relay directive request: %w", err)
	}
	ctx := d.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/admin/directives", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("relay directive request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("relay directive delivery failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay directive rejected: %s", resp.Status)
	}
	var result struct {
		Delivered int  `json:"delivered"`
		Queued    bool `json:"queued"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("relay directive invalid response: %w", err)
	}
	if result.Delivered < 1 && !result.Queued {
		return errors.New("relay directive was not delivered or durably queued")
	}
	return nil
}

// pushRelayDirective signs + delivers an admin directive to the target
// harness through the relay admin API. The relay verifies the
// signature connector-side under the AUTH_ACK policy issuer key.
func (s *Service) pushRelayDirective(req ActionRequest, commandType string) error {
	if err := DeliverRelayDirective(RelayDirective{
		Context:        req.Context,
		OrganizationID: req.OrganizationID,
		HarnessID:      req.HarnessID,
		CommandType:    commandType,
		Reason:         req.Reason,
		IssuedBy:       req.PerformedBy,
		Parameters:     req.Parameters,
	}); err != nil {
		return fmt.Errorf("fleet: action %s: %w", req.Action, err)
	}
	return nil
}

func (s *Service) pushRelayRevocation(action ActionRequest) error {
	base := strings.TrimSuffix(relayAdminURL(), "/")
	if base == "" {
		return errors.New("relay revocation requires a live relay channel — set PCCP_RELAY_ADMIN_URL")
	}
	token := strings.TrimSpace(config.LoadRelayFromEnv().ControlPlaneToken)
	if token == "" {
		return errors.New("relay revocation requires PCCP_CP_TOKEN")
	}
	body, err := json.Marshal(map[string]string{"organization_id": action.OrganizationID, "harness_id": action.HarnessID, "reason": action.Reason})
	if err != nil {
		return err
	}
	ctx := action.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/harnesses/revoke", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("relay revocation delivery failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay revocation rejected: %s", resp.Status)
	}
	return nil
}

// HarnessInventory returns a live inventory of connected harness instances (PRD §14.1).
type HarnessInventory struct {
	Harness          models.Harness   `json:"harness"`
	Device           models.Device    `json:"device,omitempty"`
	User             *models.User     `json:"user,omitempty"`
	Sessions         []models.Session `json:"sessions,omitempty"`
	ActiveSessions   int              `json:"active_sessions"`
	RiskScore        float64          `json:"risk_score"`
	IsActive         bool             `json:"is_active"`
	OpenApprovals    int              `json:"open_approvals"`
	SecurityFindings int              `json:"security_findings"`
}

type InventoryQuery struct {
	Status    string
	Risk      string
	Version   string
	HarnessID string
	Search    string
	Offset    int
	Limit     int
}

func (s *Service) GetFleetInventoryPage(orgID string, filter InventoryQuery) ([]HarnessInventory, int64, error) {
	query := s.db.Model(&models.Harness{}).Where("organization_id = ?", orgID)
	if filter.HarnessID != "" {
		query = query.Where("harness_id = ?", filter.HarnessID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Risk != "" {
		query = query.Where("risk_state = ?", filter.Risk)
	}
	if filter.Version == "stale" {
		query = query.Where("binary_version = '' OR last_heartbeat = ''")
	} else if filter.Version != "" {
		query = query.Where("binary_version = ?", filter.Version)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		var userIDs []string
		if err := s.db.Model(&models.User{}).
			Where("organization_id = ? AND (LOWER(email) LIKE ? OR LOWER(name) LIKE ?)", orgID, pattern, pattern).
			Limit(200).Pluck("id", &userIDs).Error; err != nil {
			return nil, 0, err
		}
		searchQuery := s.db.Where("LOWER(name) LIKE ? OR LOWER(harness_id) LIKE ?", pattern, pattern)
		for _, userID := range userIDs {
			searchQuery = searchQuery.Or("allowed_users LIKE ?", "%\""+userID+"\"%")
		}
		query = query.Where(searchQuery)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var harnesses []models.Harness
	query = query.Order("updated_at DESC, id ASC")
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if err := query.Find(&harnesses).Error; err != nil {
		return nil, 0, err
	}
	if len(harnesses) == 0 {
		return []HarnessInventory{}, total, nil
	}

	harnessIDs := make([]string, 0, len(harnesses))
	deviceIDs := make([]string, 0, len(harnesses))
	userIDs := make([]string, 0, len(harnesses))
	for _, harness := range harnesses {
		harnessIDs = append(harnessIDs, harness.HarnessID)
		if harness.DeviceID != "" {
			deviceIDs = append(deviceIDs, harness.DeviceID)
		}
		var allowed []string
		if json.Unmarshal([]byte(harness.AllowedUsers), &allowed) == nil && len(allowed) > 0 {
			userIDs = append(userIDs, allowed[0])
		}
	}
	var devices []models.Device
	if len(deviceIDs) > 0 {
		if err := s.db.Where("organization_id = ? AND id IN ?", orgID, deviceIDs).Find(&devices).Error; err != nil {
			return nil, 0, err
		}
	}
	devicesByID := make(map[string]models.Device, len(devices))
	for _, device := range devices {
		devicesByID[device.ID] = device
	}
	var users []models.User
	if len(userIDs) > 0 {
		if err := s.db.Where("organization_id = ? AND id IN ?", orgID, userIDs).Find(&users).Error; err != nil {
			return nil, 0, err
		}
	}
	usersByID := make(map[string]models.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	type groupedCount struct {
		HarnessID string
		Count     int64
	}
	var sessionCounts []groupedCount
	if err := s.db.Model(&models.Session{}).
		Select("harness_id, COUNT(*) AS count").
		Where("organization_id = ? AND harness_id IN ? AND status = ?", orgID, harnessIDs, "active").
		Group("harness_id").Scan(&sessionCounts).Error; err != nil {
		return nil, 0, err
	}
	sessionsByHarness := make(map[string]int, len(sessionCounts))
	for _, count := range sessionCounts {
		sessionsByHarness[count.HarnessID] = int(count.Count)
	}
	var approvalCounts []groupedCount
	if err := s.db.Model(&models.Approval{}).
		Select("sessions.harness_id, COUNT(*) AS count").
		Joins("JOIN sessions ON sessions.session_id = approvals.session_id").
		Where("sessions.organization_id = ? AND sessions.harness_id IN ? AND approvals.decision = ?", orgID, harnessIDs, "pending").
		Group("sessions.harness_id").Scan(&approvalCounts).Error; err != nil {
		return nil, 0, err
	}
	approvalsByHarness := make(map[string]int, len(approvalCounts))
	for _, count := range approvalCounts {
		approvalsByHarness[count.HarnessID] = int(count.Count)
	}
	var findingCounts []groupedCount
	if err := s.db.Model(&models.SecurityFinding{}).
		Select("sessions.harness_id, COUNT(*) AS count").
		Joins("JOIN sessions ON sessions.session_id = security_findings.session_id AND sessions.organization_id = security_findings.organization_id").
		Where("security_findings.organization_id = ? AND security_findings.status = ? AND sessions.harness_id IN ?", orgID, "open", harnessIDs).
		Group("sessions.harness_id").Scan(&findingCounts).Error; err != nil {
		return nil, 0, err
	}
	findingsByHarness := make(map[string]int, len(findingCounts))
	for _, count := range findingCounts {
		findingsByHarness[count.HarnessID] = int(count.Count)
	}

	result := make([]HarnessInventory, 0, len(harnesses))
	for _, h := range harnesses {
		inventory := HarnessInventory{
			Harness:          h,
			Device:           devicesByID[h.DeviceID],
			ActiveSessions:   sessionsByHarness[h.HarnessID],
			IsActive:         models.HarnessStatusPermitted(h.Status),
			OpenApprovals:    approvalsByHarness[h.HarnessID],
			SecurityFindings: findingsByHarness[h.HarnessID],
			RiskScore:        s.calculateRiskScore(h),
		}
		var allowed []string
		if json.Unmarshal([]byte(h.AllowedUsers), &allowed) == nil && len(allowed) > 0 {
			if user, ok := usersByID[allowed[0]]; ok {
				userCopy := user
				inventory.User = &userCopy
			}
		}
		result = append(result, inventory)
	}
	return result, total, nil
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
	ActionClearDesiredState FleetAction = "clear_desired_state"
)

// IsHarnessScopedAction is the external fleet-action allowlist. Organization-
// wide containment intentionally uses the dedicated, two-step security
// lockdown endpoint instead of this per-harness action surface.
func IsHarnessScopedAction(action FleetAction) bool {
	switch action {
	case ActionReauth, ActionForcePolicy, ActionForceConfig, ActionRequireUpgrade,
		ActionMoveRing, ActionSuspendModel, ActionReduceTools, ActionDisableMCP,
		ActionChangeQuota, ActionPauseExecution, ActionTerminateSession,
		ActionRevokeCert, ActionQuarantine, ActionIsolateSandbox,
		ActionInvalidatePriv, ActionForensicSnapshot, ActionCreateIncident,
		ActionSendAdminMsg, ActionClearDesiredState:
		return true
	default:
		return false
	}
}

// IsBulkHarnessScopedAction excludes actions that require per-request data.
// In particular, terminate_session must identify one session and cannot be
// safely inferred from a harness-wide selection.
func IsBulkHarnessScopedAction(action FleetAction) bool {
	return IsHarnessScopedAction(action) && action != ActionTerminateSession && action != ActionClearDesiredState
}

func isStatefulAction(action FleetAction) bool {
	switch action {
	case ActionRequireUpgrade, ActionMoveRing, ActionSuspendModel, ActionReduceTools,
		ActionDisableMCP, ActionChangeQuota, ActionPauseExecution,
		ActionIsolateSandbox, ActionInvalidatePriv:
		return true
	default:
		return false
	}
}

func (s *Service) setDesiredState(req ActionRequest, harnessID string) error {
	parameters, err := json.Marshal(req.Parameters)
	if err != nil {
		return fmt.Errorf("fleet: encode desired state: %w", err)
	}
	now := time.Now().UTC()
	state := models.FleetDesiredState{
		OrganizationID: req.OrganizationID, HarnessID: harnessID, Action: string(req.Action),
		Status: "active", ParametersJSON: string(parameters), Reason: req.Reason,
		SetBy: req.PerformedBy, SetAt: now,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "organization_id"}, {Name: "harness_id"}, {Name: "action"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status": "active", "parameters_json": string(parameters), "reason": req.Reason,
			"set_by": req.PerformedBy, "set_at": now, "released_by": "", "released_at": nil,
		}),
	}).Create(&state).Error
}

func (s *Service) clearDesiredState(req ActionRequest, harnessID string) error {
	target, _ := req.Parameters["action"].(string)
	if !isStatefulAction(FleetAction(target)) {
		return errors.New("fleet: clear_desired_state requires a valid stateful parameters.action")
	}
	now := time.Now().UTC()
	result := s.db.Model(&models.FleetDesiredState{}).
		Where("organization_id = ? AND harness_id = ? AND action = ? AND status = ?", req.OrganizationID, harnessID, target, "active").
		Updates(map[string]interface{}{"status": "released", "released_by": req.PerformedBy, "released_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("fleet: no active desired state for %s", target)
	}
	return nil
}

// PerformAction executes a fleet action on a harness.
type ActionRequest struct {
	Context        context.Context        `json:"-"`
	OrganizationID string                 `json:"organization_id"`
	HarnessID      string                 `json:"harness_id"`
	Action         FleetAction            `json:"action"`
	Reason         string                 `json:"reason"`
	PerformedBy    string                 `json:"performed_by"` // admin user ID
	SessionID      string                 `json:"session_id"`   // for session-specific actions
	Parameters     map[string]interface{} `json:"parameters"`
}

// ActionExecutionError reports that a fleet request did not complete while
// preserving whether durable local enforcement or relay delivery already
// happened. Callers must not present these outcomes as an ordinary failure
// that is safe to retry blindly.
type ActionExecutionError struct {
	Action         FleetAction
	LocalApplied   bool
	RelayDelivered bool
	Cause          error
}

func (e *ActionExecutionError) Error() string { return e.Cause.Error() }
func (e *ActionExecutionError) Unwrap() error { return e.Cause }

// PerformAction executes a fleet action and records it in the audit trail.
func (s *Service) PerformAction(req ActionRequest) (resultErr error) {
	localApplied := false
	relayDelivered := false
	defer func() {
		result := "success"
		if resultErr != nil {
			result = "failure"
			if localApplied || relayDelivered {
				result = "partial"
			}
		}
		details := map[string]interface{}{
			"reason": req.Reason, "params": req.Parameters,
			"local_state_applied": localApplied, "relay_delivered": relayDelivered,
		}
		if resultErr != nil {
			details["error"] = resultErr.Error()
		}
		detailsJSON, marshalErr := json.Marshal(details)
		if marshalErr == nil {
			auditErr := models.CreateAuditEvent(s.db, &models.AuditEvent{
				OrganizationID: req.OrganizationID,
				EventType:      fmt.Sprintf("cp.fleet.%s", string(req.Action)),
				ActorID:        req.PerformedBy,
				ActorType:      "admin",
				Action:         string(req.Action),
				ResourceType:   "harness",
				ResourceID:     req.HarnessID,
				Details:        string(detailsJSON),
				Result:         result,
				OccurredAt:     time.Now().UTC().Format(time.RFC3339Nano),
			})
			if auditErr != nil {
				marshalErr = fmt.Errorf("fleet: record action audit: %w", auditErr)
			}
		}
		if marshalErr != nil {
			if resultErr == nil {
				resultErr = marshalErr
			} else {
				resultErr = errors.Join(resultErr, marshalErr)
			}
		}
		if resultErr != nil {
			resultErr = &ActionExecutionError{Action: req.Action, LocalApplied: localApplied, RelayDelivered: relayDelivered, Cause: resultErr}
		}
	}()
	var harness models.Harness
	if req.Action != ActionEmergencyLockdown {
		if err := s.db.Where("harness_id = ? AND organization_id = ?", req.HarnessID, req.OrganizationID).
			First(&harness).Error; err != nil {
			return fmt.Errorf("fleet: harness not found")
		}
	}
	if isStatefulAction(req.Action) {
		if err := s.setDesiredState(req, harness.HarnessID); err != nil {
			return fmt.Errorf("fleet: persist desired state: %w", err)
		}
		localApplied = true
	}

	// Execute the action
	switch req.Action {
	case ActionRevokeCert:
		if s.revokeHarness == nil {
			return errors.New("fleet: canonical harness revocation is not configured")
		}
		if err := s.revokeHarness(req.OrganizationID, harness.HarnessID, req.Reason, req.PerformedBy); err != nil {
			var appliedErr *identity.RevocationAppliedError
			if errors.As(err, &appliedErr) {
				localApplied = true
			}
			return err
		}
		localApplied = true
		if err := s.revokeRelay(req); err != nil {
			return err
		}
		relayDelivered = true

	case ActionQuarantine:
		var outcomes []sessionlifecycle.Outcome
		err := s.db.Transaction(func(tx *gorm.DB) error {
			var locked models.Harness
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND organization_id = ?", harness.ID, req.OrganizationID).First(&locked).Error; err != nil {
				return err
			}
			if locked.Status != "quarantined" && !models.HarnessStatusPermitted(locked.Status) {
				return fmt.Errorf("fleet: harness status %s cannot transition to quarantined", locked.Status)
			}
			if locked.Status != "quarantined" {
				updated := tx.Model(&models.Harness{}).
					Where("id = ? AND organization_id = ? AND status = ?", locked.ID, req.OrganizationID, locked.Status).
					Updates(map[string]interface{}{"status": "quarantined", "risk_state": "high"})
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return errors.New("fleet: harness standing changed before quarantine")
				}
			}
			var transitionErr error
			outcomes, transitionErr = s.lifecycle.TransitionScopeInTransaction(tx, sessionlifecycle.Scope{
				OrganizationID: req.OrganizationID, HarnessID: locked.HarnessID, ForceTerminal: true, ActorType: "admin",
			}, "terminated", string(req.Action), req.Reason, req.PerformedBy)
			if transitionErr != nil {
				return transitionErr
			}
			return requireSuccessfulTransitions(outcomes)
		})
		if err != nil {
			return err
		}
		localApplied = true
		if _, err := s.lifecycle.FinalizeTransitions(req.OrganizationID, outcomes, "terminated", string(req.Action), req.Reason, req.PerformedBy, "admin"); err != nil {
			return err
		}
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionTerminateSession:
		if strings.TrimSpace(req.SessionID) == "" {
			return errors.New("fleet: terminate_session requires session_id")
		}
		outcomes, err := s.lifecycle.TransitionScope(sessionlifecycle.Scope{OrganizationID: req.OrganizationID, HarnessID: harness.HarnessID, SessionRefs: []string{req.SessionID}, ForceTerminal: true}, "terminated", string(req.Action), req.Reason, req.PerformedBy)
		if err != nil || len(outcomes) != 1 || outcomes[0].Result != sessionlifecycle.ResultUpdated {
			if err != nil {
				return err
			}
			return fmt.Errorf("fleet: session not found or not transitionable")
		}
		localApplied = true
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionSuspendModel:
		// Revoke all capability leases for this harness
		if err := s.db.Model(&models.CapabilityLease{}).
			Where("organization_id = ? AND subject_peer_id = ?", req.OrganizationID, harness.HarnessID).
			Update("status", "revoked").Error; err != nil {
			return fmt.Errorf("fleet: revoke model leases: %w", err)
		}
		localApplied = true
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionEmergencyLockdown:
		return errors.New("fleet: emergency lockdown requires the canonical security lockdown workflow")

	case ActionPauseExecution:
		// Mark sessions as paused
		if err := s.transitionHarnessSessions(req, harness.HarnessID, "paused"); err != nil {
			return err
		}
		localApplied = true
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionRequireUpgrade:
		// FleetDesiredState is the authority. Risk remains an independently
		// managed signal, so releasing this policy cannot erase another cause.
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionMoveRing, ActionReduceTools, ActionDisableMCP, ActionChangeQuota:
		if derr := s.directive(req, string(req.Action)); derr != nil {
			return derr
		}
		relayDelivered = true

	case ActionReauth, ActionForcePolicy, ActionForceConfig, ActionSendAdminMsg:
		// Directive-class actions: signed directive through the relay
		// admin channel (verified + executed connector-side). Without
		// the channel configured the action fails honestly.
		if derr := s.directive(req, string(req.Action)); derr != nil {
			return derr
		}
		relayDelivered = true

	case ActionIsolateSandbox:
		// Isolate every sandbox bound to this harness's sessions:
		// network policy flips to deny-all in the durable definition.
		if err := s.db.Model(&models.SandboxRecord{}).
			Where("organization_id = ? AND session_id IN (?)", req.OrganizationID, s.db.Model(&models.Session{}).
				Select("session_id").Where("organization_id = ? AND harness_id = ?", req.OrganizationID, harness.HarnessID)).
			Updates(map[string]interface{}{"status": "paused", "network_policy": "denied"}).Error; err != nil {
			return fmt.Errorf("fleet: isolate sandbox: %w", err)
		}
		localApplied = true
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionInvalidatePriv:
		// Invalidate delegated privileges: revoke the harness's leases.
		if err := s.db.Model(&models.CapabilityLease{}).
			Where("organization_id = ? AND subject_peer_id = ?", req.OrganizationID, harness.HarnessID).
			Update("status", "revoked").Error; err != nil {
			return fmt.Errorf("fleet: invalidate privileges: %w", err)
		}
		localApplied = true
		if err := s.directive(req, string(req.Action)); err != nil {
			return err
		}
		relayDelivered = true

	case ActionClearDesiredState:
		// Clear releases future admission/reconciliation policy only. Existing
		// paused sessions, revoked leases, and isolated sandboxes are not
		// silently resumed; their canonical recovery workflows remain explicit.
		if err := s.clearDesiredState(req, harness.HarnessID); err != nil {
			return err
		}
		localApplied = true
		target, _ := req.Parameters["action"].(string)
		if err := s.directive(req, "release_"+target); err != nil {
			return err
		}
		relayDelivered = true

	case ActionForensicSnapshot:
		// Snapshot running sandboxes for this harness only. A bounded query
		// avoids scanning every organization sandbox and then issuing one
		// session lookup per row.
		var recs []models.SandboxRecord
		sandboxQuery := s.db.Model(&models.SandboxRecord{}).
			Where("organization_id = ? AND status IN ('running','defined') AND session_id IN (?)", req.OrganizationID,
				s.db.Model(&models.Session{}).Select("session_id").Where("organization_id = ? AND harness_id = ?", req.OrganizationID, harness.HarnessID))
		var sandboxCount int64
		if err := sandboxQuery.Count(&sandboxCount).Error; err != nil {
			return err
		}
		if sandboxCount > 500 {
			return fmt.Errorf("fleet: forensic snapshot contains %d sandboxes; submit as a bounded background job", sandboxCount)
		}
		if err := sandboxQuery.Order("id ASC").Limit(500).Find(&recs).Error; err != nil {
			return err
		}
		for _, rec := range recs {
			snapshotDetails, err := json.Marshal(map[string]interface{}{"fleet_action": req.Action, "harness": harness.HarnessID})
			if err != nil {
				return fmt.Errorf("fleet: encode forensic snapshot audit: %w", err)
			}
			if err := models.CreateAuditEvent(s.db, &models.AuditEvent{
				OrganizationID: req.OrganizationID,
				EventType:      "cp.runtime.forensic_snapshot",
				ActorID:        req.PerformedBy,
				ActorType:      "admin",
				Action:         "forensic_snapshot",
				ResourceType:   "sandbox",
				ResourceID:     rec.ID,
				Details:        string(snapshotDetails),
				Result:         "success",
				OccurredAt:     time.Now().Format(time.RFC3339),
			}); err != nil {
				return err
			}
		}
		localApplied = true

	case ActionCreateIncident:
		incidentDetails, err := json.Marshal(map[string]string{"reason": req.Reason, "harness_id": harness.HarnessID})
		if err != nil {
			return fmt.Errorf("fleet: encode incident details: %w", err)
		}
		finding := &models.SecurityFinding{
			Base:           models.Base{ID: models.GenerateID("inc")},
			OrganizationID: req.OrganizationID,
			FindingType:    "incident_fleet_action",
			Severity:       "high",
			Title:          "Fleet incident for " + harness.Name,
			TitleKo:        harness.Name + " 하네스 보안 인시던트",
			Description:    req.Reason,
			EvidenceJSON:   string(incidentDetails),
			Status:         "open",
			ContainsAction: string(ActionCreateIncident),
			Direction:      "request",
			OccurredAt:     time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := s.db.Create(finding).Error; err != nil {
			return fmt.Errorf("fleet: create incident: %w", err)
		}
		localApplied = true
		if err := models.CreateAuditEvent(s.db, &models.AuditEvent{
			OrganizationID: req.OrganizationID,
			EventType:      "cp.incident.created",
			ActorID:        req.PerformedBy,
			ActorType:      "admin",
			Action:         "create_incident",
			ResourceType:   "incident",
			ResourceID:     finding.ID,
			Details:        string(incidentDetails),
			Result:         "success",
			OccurredAt:     time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("fleet: record incident audit: %w", err)
		}

	default:
		return fmt.Errorf("fleet: unknown action %s", req.Action)
	}

	return nil
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

func (s *Service) transitionHarnessSessions(req ActionRequest, harnessID, target string) error {
	outcomes, err := s.lifecycle.TransitionScope(sessionlifecycle.Scope{OrganizationID: req.OrganizationID, HarnessID: harnessID, ForceTerminal: models.SessionIsTerminal(target)}, target, string(req.Action), req.Reason, req.PerformedBy)
	if err != nil {
		return err
	}
	return requireSuccessfulTransitions(outcomes)
}

func requireSuccessfulTransitions(outcomes []sessionlifecycle.Outcome) error {
	for _, outcome := range outcomes {
		if outcome.Result != sessionlifecycle.ResultUpdated || len(outcome.CleanupFailures) > 0 {
			return fmt.Errorf("fleet: session %s transition incomplete (%s): %s", outcome.RequestedID, outcome.Result, outcome.Error)
		}
	}
	return nil
}
