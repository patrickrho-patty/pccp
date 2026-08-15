package korean

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func workflowDB(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/w.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&models.AuditEvent{})
	return New(db)
}

// TestExceptionWorkflowLifecycle covers the T16 exception vectors:
// request → decide (approve/deny), scope + expiry enforcement,
// double-decision refusal, unknown-request refusal.
func TestExceptionWorkflowLifecycle(t *testing.T) {
	svc := workflowDB(t)
	ev, err := svc.RequestException(ExceptionRequest{
		OrganizationID: "o", RequestedBy: "dev-1", PolicyRule: "deny-prod-deploy",
		Reason: "incident hotfix", Scope: "repo", ScopeID: "repo-9",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Not active before approval.
	if ok, _ := svc.ExceptionActive("o", "deny-prod-deploy", "repo", "repo-9", time.Now()); ok {
		t.Fatal("unapproved exception active")
	}
	// Approve.
	if err := svc.DecideException("o", ev.ID, "admin-1", true, "granted for incident"); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.ExceptionActive("o", "deny-prod-deploy", "repo", "repo-9", time.Now()); err != nil || !ok {
		t.Fatalf("approved exception inactive: %v", err)
	}
	// Scope mismatch.
	if ok, _ := svc.ExceptionActive("o", "deny-prod-deploy", "repo", "OTHER", time.Now()); ok {
		t.Fatal("exception leaked across scopes")
	}
	// Expiry.
	if _, err := svc.ExceptionActive("o", "deny-prod-deploy", "repo", "repo-9", time.Now().Add(2*time.Hour)); !errors.Is(err, ErrExceptionExpired) {
		t.Fatalf("expected expiry error, got %v", err)
	}
	// Double decision refused.
	if err := svc.DecideException("o", ev.ID, "admin-1", false, "changed mind"); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatalf("expected closed-state refusal, got %v", err)
	}
	// Cross-org decision refused.
	ev2, _ := svc.RequestException(ExceptionRequest{OrganizationID: "other", RequestedBy: "d", PolicyRule: "r", Reason: "x", ExpiresAt: time.Now().Add(time.Hour)})
	if err := svc.DecideException("WRONG-ORG", ev2.ID, "a", true, ""); err == nil {
		t.Fatal("cross-org decision accepted")
	}
	// Denied exception never activates.
	ev3, _ := svc.RequestException(ExceptionRequest{OrganizationID: "o", RequestedBy: "d", PolicyRule: "r2", Reason: "x", ExpiresAt: time.Now().Add(time.Hour)})
	_ = svc.DecideException("o", ev3.ID, "a", false, "no")
	if ok, _ := svc.ExceptionActive("o", "r2", "", "", time.Now()); ok {
		t.Fatal("denied exception active")
	}
	// Validation.
	if _, err := svc.RequestException(ExceptionRequest{}); err == nil {
		t.Fatal("empty request accepted")
	}
}

func TestOnboardingChecklistAndRollout(t *testing.T) {
	cl := DefaultOnboardingChecklist("o-1")
	if len(cl.Steps) != 6 {
		t.Fatalf("steps = %d", len(cl.Steps))
	}
	for _, s := range cl.Steps {
		if s.LabelKo == "" {
			t.Fatalf("step %s missing Korean label", s.Key)
		}
	}

	rc := NewRolloutControl("v1.2.0")
	if rc.State() != "rolling_out" {
		t.Fatal("initial state")
	}
	rc.Pause()
	if rc.State() != "paused" {
		t.Fatal("pause")
	}
	rc.Pause() // idempotent
	if rc.State() != "paused" || len(rc.History) != 2 {
		t.Fatal("double pause recorded")
	}
	rc.Resume()
	rc.RollBack()
	if rc.State() != "rolled_back" {
		t.Fatal("rollback")
	}
	// History is the operator audit trail.
	if len(rc.History) != 4 {
		t.Fatalf("history = %v", rc.History)
	}
}
