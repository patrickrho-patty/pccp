package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	FleetStateRequireUpgrade = "require_client_upgrade"
	FleetStateMoveRing       = "move_release_ring"
	FleetStateSuspendModel   = "suspend_model_access"
	FleetStateReduceTools    = "reduce_tool_capabilities"
	FleetStateDisableMCP     = "disable_mcp_server"
	FleetStateChangeQuota    = "change_quota"
	FleetStatePauseExecution = "pause_agent_execution"
	FleetStateIsolateSandbox = "isolate_sandbox"
	FleetStateInvalidatePriv = "invalidate_privilege"
)

// FleetDesiredState is the durable per-harness policy established by a
// stateful fleet action. Relay directives are convergence notifications; this
// row remains the authority across reconnects and process restarts.
type FleetDesiredState struct {
	Base
	OrganizationID string     `gorm:"type:varchar(64);uniqueIndex:idx_fleet_desired_org_harness_action,priority:1;not null" json:"organization_id"`
	HarnessID      string     `gorm:"type:varchar(64);uniqueIndex:idx_fleet_desired_org_harness_action,priority:2;not null" json:"harness_id"`
	Action         string     `gorm:"type:varchar(64);uniqueIndex:idx_fleet_desired_org_harness_action,priority:3;not null" json:"action"`
	Status         string     `gorm:"type:varchar(16);index;not null" json:"status"`
	ParametersJSON string     `gorm:"type:text" json:"parameters,omitempty"`
	Reason         string     `gorm:"type:text" json:"reason"`
	SetBy          string     `gorm:"type:varchar(64)" json:"set_by"`
	SetAt          time.Time  `gorm:"not null" json:"set_at"`
	ReleasedBy     string     `gorm:"type:varchar(64)" json:"released_by,omitempty"`
	ReleasedAt     *time.Time `json:"released_at,omitempty"`
}

var fleetAdmissionBlockingActions = []string{
	FleetStateRequireUpgrade,
	FleetStateSuspendModel,
	FleetStatePauseExecution,
	FleetStateInvalidatePriv,
}

// ActiveFleetDesiredStates returns the authoritative active policy rows for a
// harness. Callers may pass an action subset; an empty subset returns all.
func ActiveFleetDesiredStates(db *gorm.DB, organizationID, harnessID string, actions ...string) ([]FleetDesiredState, error) {
	query := db.Where("organization_id = ? AND harness_id = ? AND status = ?", organizationID, harnessID, "active")
	if len(actions) > 0 {
		query = query.Where("action IN ?", actions)
	}
	var states []FleetDesiredState
	if err := query.Order("action ASC").Find(&states).Error; err != nil {
		return nil, err
	}
	return states, nil
}

// HarnessAdmissionRestriction returns the first durable fleet policy that
// blocks new sessions, lease issuance, or inference for this harness.
func HarnessAdmissionRestriction(db *gorm.DB, organizationID, harnessID string) (*FleetDesiredState, error) {
	states, err := ActiveFleetDesiredStates(db, organizationID, harnessID, fleetAdmissionBlockingActions...)
	if err != nil || len(states) == 0 {
		return nil, err
	}
	return &states[0], nil
}

func HarnessSandboxIsolationActive(db *gorm.DB, organizationID, harnessID string) (bool, error) {
	states, err := ActiveFleetDesiredStates(db, organizationID, harnessID, FleetStateIsolateSandbox)
	return len(states) > 0, err
}
