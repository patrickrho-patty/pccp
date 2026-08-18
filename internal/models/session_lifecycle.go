package models

// Session lifecycle state machine (PAT-1496) — the canonical table for
// admin lifecycle actions on sessions. The Live view, session list, and
// this table agree on one predicate: only `active` is live.
//
// States: pending, active, idle, paused, closed, terminated.
// closed/terminated are terminal.
var sessionLifecycleTransitions = map[string][]string{
	"pending":    {"active", "closed", "terminated"},
	"active":     {"idle", "paused", "closed", "terminated"},
	"idle":       {"active", "paused", "closed", "terminated"},
	"paused":     {"active", "closed", "terminated"},
	"closed":     {},
	"terminated": {},
}

// SessionTransitionAllowed reports whether from → to is a legal session
// lifecycle move under the canonical table.
func SessionTransitionAllowed(from, to string) bool {
	for _, t := range sessionLifecycleTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// SessionIsLive reports the canonical live predicate: only an active
// session is live. Paused and idle sessions are in-progress but NOT
// live — surfaces must label them separately (PAT-1496).
func SessionIsLive(status string) bool {
	return status == "active"
}
