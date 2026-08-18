package models

// User lifecycle state machine (PAT-1489) — the single canonical table
// every writer of users.status must honor: API lifecycle endpoints
// (internal/api/users_lifecycle.go), the contractor expiry sweep
// (internal/identity/entitlement.go), and SCIM/SSO provisioning.
//
// States: active, suspended, offboarded.
// offboarded is terminal: only read-only history remains.
var userLifecycleTransitions = map[string][]string{
	"active":    {"suspended", "offboarded"},
	"suspended": {"active", "offboarded"},
}

// UserTransitionAllowed reports whether from → to is a legal lifecycle
// move under the canonical table.
func UserTransitionAllowed(from, to string) bool {
	for _, t := range userLifecycleTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}
