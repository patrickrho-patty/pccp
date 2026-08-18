package models

const (
	UserStatusActive     = "active"
	UserStatusSuspended  = "suspended"
	UserStatusOffboarded = "offboarded"

	UserActionSuspend  = "suspend"
	UserActionResume   = "resume"
	UserActionOffboard = "offboard"
)

// User lifecycle state machine (PAT-1489) — the single canonical table
// every writer of users.status must honor: API lifecycle endpoints
// (internal/api/users_lifecycle.go), the contractor expiry sweep
// (internal/identity/entitlement.go), and SCIM/SSO provisioning.
//
// States: active, suspended, offboarded.
// offboarded is terminal: only read-only history remains.
// UserLifecycleEdge is the complete definition of one legal state change.
// Transition validation, API actions, and audit names all derive from this
// table so those three contracts cannot drift independently.
type UserLifecycleEdge struct {
	To          string
	Action      string
	EventType   string
	AuditAction string
}

var userLifecycleEdges = map[string][]UserLifecycleEdge{
	UserStatusActive: {
		{To: UserStatusSuspended, Action: UserActionSuspend, EventType: "cp.user.suspended", AuditAction: "suspend_user"},
		{To: UserStatusOffboarded, Action: UserActionOffboard, EventType: "cp.user.offboarded", AuditAction: "offboard_user"},
	},
	UserStatusSuspended: {
		{To: UserStatusActive, Action: UserActionResume, EventType: "cp.user.resumed", AuditAction: "resume_user"},
		{To: UserStatusOffboarded, Action: UserActionOffboard, EventType: "cp.user.offboarded", AuditAction: "offboard_user"},
	},
	UserStatusOffboarded: {},
}

// UserTransitionAllowed reports whether from → to is a legal lifecycle
// move under the canonical table.
func UserTransitionAllowed(from, to string) bool {
	_, ok := UserLifecycleEdgeForTransition(from, to)
	return ok
}

// UserLifecycleEdgeForTransition returns the canonical metadata for from → to.
func UserLifecycleEdgeForTransition(from, to string) (UserLifecycleEdge, bool) {
	for _, edge := range userLifecycleEdges[from] {
		if edge.To == to {
			return edge, true
		}
	}
	return UserLifecycleEdge{}, false
}

// UserLifecycleActions returns a copy of the valid action names for a state.
// API projections filter this canonical state result through operator RBAC and
// self-action rules before exposing it to list/detail clients.
func UserLifecycleActions(status string) []string {
	edges := userLifecycleEdges[status]
	actions := make([]string, 0, len(edges))
	for _, edge := range edges {
		actions = append(actions, edge.Action)
	}
	return actions
}
