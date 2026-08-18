package models

import "testing"

func TestUserLifecycleEdgesDriveTransitionsActionsAndAudit(t *testing.T) {
	tests := []struct {
		from, to, action, event, auditAction string
	}{
		{UserStatusActive, UserStatusSuspended, UserActionSuspend, "cp.user.suspended", "suspend_user"},
		{UserStatusActive, UserStatusOffboarded, UserActionOffboard, "cp.user.offboarded", "offboard_user"},
		{UserStatusSuspended, UserStatusActive, UserActionResume, "cp.user.resumed", "resume_user"},
		{UserStatusSuspended, UserStatusOffboarded, UserActionOffboard, "cp.user.offboarded", "offboard_user"},
	}
	for _, tt := range tests {
		if !UserTransitionAllowed(tt.from, tt.to) {
			t.Fatalf("expected %s -> %s to be allowed", tt.from, tt.to)
		}
		edge, ok := UserLifecycleEdgeForTransition(tt.from, tt.to)
		if !ok || edge.Action != tt.action || edge.EventType != tt.event || edge.AuditAction != tt.auditAction {
			t.Fatalf("wrong canonical edge for %s -> %s: %+v, %v", tt.from, tt.to, edge, ok)
		}
	}
	if _, ok := UserLifecycleEdgeForTransition(UserStatusOffboarded, UserStatusActive); ok {
		t.Fatal("terminal state unexpectedly has an outgoing edge")
	}
}
