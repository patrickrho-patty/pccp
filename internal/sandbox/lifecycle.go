package sandbox

import "fmt"

// Sandbox lifecycle state machine (PAT-1513) — the single source of truth
// for which operator actions are valid in which state. The web console
// mirrors this table in web/src/sandboxLifecycle.ts so list and detail
// surfaces cannot offer an action the service would reject.
//
// States (SandboxRecord.Status): pending, defined, running, paused,
// destroyed, failed.
const (
	ActionSnapshot = "snapshot"
	ActionDestroy  = "destroy"
	ActionRetry    = "retry"
)

// validActions maps each lifecycle state to the actions it admits:
//
//	snapshot — running only; a defined/pending sandbox has no live runtime
//	           state to capture and a destroyed one no longer exists.
//	destroy  — every non-terminal state; cleanup is always safe and is
//	           idempotent on the terminal destroyed state.
//	retry    — defined/failed only; re-attempts provisioning against a
//	           runtime that was unreachable or faulted.
var validActions = map[string][]string{
	"pending":   {ActionDestroy},
	"defined":   {ActionDestroy, ActionRetry},
	"running":   {ActionSnapshot, ActionDestroy},
	"paused":    {ActionSnapshot, ActionDestroy},
	"destroyed": {},
	"failed":    {ActionDestroy, ActionRetry},
}

// ValidActions returns the actions admissible in the given state.
// Unknown states admit nothing (fail-closed).
func ValidActions(status string) []string {
	actions, ok := validActions[status]
	if !ok {
		return []string{}
	}
	out := make([]string, len(actions))
	copy(out, actions)
	return out
}

// InvalidTransitionError marks a lifecycle action rejected by the state
// machine so API handlers can answer 409 instead of 500.
type InvalidTransitionError struct {
	Status string
	Action string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("sandbox: action %q not valid in state %q", e.Action, e.Status)
}

// requireAction returns an *InvalidTransitionError when action is not
// admissible in status.
func requireAction(status, action string) error {
	for _, a := range ValidActions(status) {
		if a == action {
			return nil
		}
	}
	return &InvalidTransitionError{Status: status, Action: action}
}
