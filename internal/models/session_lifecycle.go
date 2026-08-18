package models

// Session lifecycle state machine (PAT-1496) — the canonical table for
// admin lifecycle actions on sessions. The Live view, session list, and
// this table agree on one predicate: only `active` is live.
//
// States: pending, active, idle, paused, closed, terminated.
// closed/terminated are terminal.
var sessionLifecycleTransitions = map[string][]string{
	"pending":    {"active", "paused", "closed", "terminated"},
	"active":     {"idle", "paused", "closed", "terminated"},
	"idle":       {"active", "paused", "closed", "terminated"},
	"paused":     {"active", "closed", "terminated"},
	"closed":     {},
	"terminated": {},
}

var sessionNonTerminalStatuses = []string{"pending", "active", "idle", "paused"}
var sessionTerminalStatuses = []string{"closed", "terminated"}

// SessionNonTerminalStatuses returns a copy of the canonical set used by
// lifecycle sweeps, scoped containment, and live-session projections.
func SessionNonTerminalStatuses() []string {
	return append([]string(nil), sessionNonTerminalStatuses...)
}

// SessionTerminalStatuses returns a copy of the canonical terminal set.
func SessionTerminalStatuses() []string {
	return append([]string(nil), sessionTerminalStatuses...)
}

// SessionTransitionSources returns the known source states that may move to
// target. Scoped transitions use this positive set so an ineligible row cannot
// make an otherwise successful bulk action partially fail after mutating its
// eligible neighbors.
func SessionTransitionSources(target string) []string {
	sources := make([]string, 0, len(sessionNonTerminalStatuses))
	for _, source := range sessionNonTerminalStatuses {
		if SessionTransitionAllowed(source, target) {
			sources = append(sources, source)
		}
	}
	return sources
}

func SessionIsTerminal(status string) bool {
	for _, terminal := range sessionTerminalStatuses {
		if status == terminal {
			return true
		}
	}
	return false
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
