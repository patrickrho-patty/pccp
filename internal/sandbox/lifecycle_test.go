package sandbox

import (
	"errors"
	"testing"
)

// lifecycle_test.go pins the sandbox lifecycle state machine (PAT-1513):
// every state admits exactly its documented action set, unknown states
// fail closed, and the service enforces the table on destroy/snapshot.

func TestValidActionsPerState(t *testing.T) {
	want := map[string][]string{
		"pending":   {ActionDestroy},
		"defined":   {ActionDestroy, ActionRetry},
		"running":   {ActionSnapshot, ActionDestroy},
		"paused":    {ActionSnapshot, ActionDestroy},
		"destroyed": {},
		"failed":    {ActionDestroy, ActionRetry},
	}
	for status, actions := range want {
		got := ValidActions(status)
		if len(got) != len(actions) {
			t.Fatalf("state %s: got %v, want %v", status, got, actions)
		}
		for i, a := range actions {
			if got[i] != a {
				t.Fatalf("state %s: got %v, want %v", status, got, actions)
			}
		}
	}
}

func TestValidActionsFailClosedOnUnknownState(t *testing.T) {
	for _, status := range []string{"", "running-ish", "DESTROYED"} {
		if got := ValidActions(status); len(got) != 0 {
			t.Fatalf("unknown state %q must admit no actions, got %v", status, got)
		}
	}
}

func TestRequireActionReportsInvalidTransition(t *testing.T) {
	if err := requireAction("defined", ActionSnapshot); err == nil {
		t.Fatal("snapshot on a defined sandbox must be rejected")
	} else {
		var inv *InvalidTransitionError
		if !errors.As(err, &inv) {
			t.Fatalf("error must be InvalidTransitionError, got %T", err)
		}
		if inv.Status != "defined" || inv.Action != ActionSnapshot {
			t.Fatalf("wrong transition detail: %+v", inv)
		}
	}
	if err := requireAction("running", ActionSnapshot); err != nil {
		t.Fatalf("snapshot on a running sandbox must be admitted: %v", err)
	}
	if err := requireAction("failed", ActionRetry); err != nil {
		t.Fatalf("retry on a failed sandbox must be admitted: %v", err)
	}
	if err := requireAction("running", ActionRetry); err == nil {
		t.Fatal("retry on a running sandbox must be rejected")
	}
}
