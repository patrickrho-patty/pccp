package fleet

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeliverRelayDirectiveAuthenticatesAndRequiresDelivery(t *testing.T) {
	t.Setenv("PCCP_CP_TOKEN", "relay-secret")
	delivered := 0
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer relay-secret" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"delivered":%d}`, delivered)
	}))
	defer relay.Close()
	t.Setenv("PCCP_RELAY_ADMIN_URL", relay.URL)
	directive := RelayDirective{OrganizationID: "org", HarnessID: "harness", CommandType: "pause"}
	if err := DeliverRelayDirective(directive); err == nil {
		t.Fatal("zero live deliveries reported success")
	}
	delivered = 1
	if err := DeliverRelayDirective(directive); err != nil {
		t.Fatalf("positive delivery failed: %v", err)
	}
}

func TestTerminateSessionRequiresExplicitSession(t *testing.T) {
	db := fleetLifecycleDB(t)
	db.Create(&models.Harness{OrganizationID: "org", HarnessID: "harness", Status: "active"})
	svc := New(db, sessionlifecycle.New(db))
	svc.SetDirectiveSender(func(ActionRequest, string) error { return nil })
	if err := svc.PerformAction(ActionRequest{OrganizationID: "org", HarnessID: "harness", Action: ActionTerminateSession, Reason: "operator request"}); err == nil {
		t.Fatal("terminate_session without session_id succeeded")
	}
	if IsBulkHarnessScopedAction(ActionTerminateSession) {
		t.Fatal("terminate_session exposed as a bulk harness action")
	}
}

func fleetLifecycleDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/fleet.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Harness{}, &models.Session{}, &models.AuditEvent{}, &models.FleetDesiredState{}, &models.CapabilityLease{}, &models.SandboxRecord{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStatefulActionPersistsAdmissionPolicyUntilExplicitClear(t *testing.T) {
	db := fleetLifecycleDB(t)
	db.Create(&models.Harness{OrganizationID: "org", HarnessID: "harness", Status: "active"})
	svc := New(db, sessionlifecycle.New(db))
	svc.SetDirectiveSender(func(ActionRequest, string) error { return nil })
	if err := svc.PerformAction(ActionRequest{OrganizationID: "org", HarnessID: "harness", Action: ActionSuspendModel, Reason: "incident", PerformedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	restriction, err := models.HarnessAdmissionRestriction(db, "org", "harness")
	if err != nil || restriction == nil || restriction.Action != string(ActionSuspendModel) {
		t.Fatalf("restriction = %+v err=%v", restriction, err)
	}
	if err := svc.PerformAction(ActionRequest{
		OrganizationID: "org", HarnessID: "harness", Action: ActionClearDesiredState,
		Reason: "incident resolved", PerformedBy: "admin", Parameters: map[string]interface{}{"action": string(ActionSuspendModel)},
	}); err != nil {
		t.Fatal(err)
	}
	restriction, err = models.HarnessAdmissionRestriction(db, "org", "harness")
	if err != nil || restriction != nil {
		t.Fatalf("released restriction = %+v err=%v", restriction, err)
	}
}

func TestRevokedHarnessCannotBeQuarantinedOrResurrected(t *testing.T) {
	db := fleetLifecycleDB(t)
	db.Create(&models.Harness{OrganizationID: "org", HarnessID: "harness", Status: "revoked"})
	svc := New(db, sessionlifecycle.New(db))
	svc.SetDirectiveSender(func(ActionRequest, string) error { return nil })
	if err := svc.PerformAction(ActionRequest{OrganizationID: "org", HarnessID: "harness", Action: ActionQuarantine, Reason: "test"}); err == nil {
		t.Fatal("revoked harness was quarantined")
	}
	var harness models.Harness
	db.Where("organization_id = ? AND harness_id = ?", "org", "harness").First(&harness)
	if harness.Status != "revoked" {
		t.Fatalf("status = %q, want revoked", harness.Status)
	}
}

func TestEmergencyLockdownCannotBypassCanonicalSecurityWorkflow(t *testing.T) {
	db := fleetLifecycleDB(t)
	for _, orgID := range []string{"org-a", "org-b"} {
		db.Create(&models.Harness{OrganizationID: orgID, HarnessID: "harness-" + orgID, Status: "enrolled"})
	}
	for _, status := range []string{"active", "idle", "paused"} {
		db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-a"}, SessionID: "org-a-" + status, HarnessID: "harness-org-a", UserID: "user", Status: status})
	}
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-b"}, SessionID: "org-b-active", HarnessID: "harness-org-b", UserID: "user", Status: "active"})

	lifecycle := sessionlifecycle.New(db)
	svc := New(db, lifecycle)
	svc.SetDirectiveSender(func(ActionRequest, string) error { return nil })
	if err := svc.PerformAction(ActionRequest{OrganizationID: "org-a", Action: ActionEmergencyLockdown, Reason: "incident", PerformedBy: "admin"}); err == nil {
		t.Fatal("generic fleet service accepted organization-wide lockdown")
	}
	var ownActive, foreignActive int64
	db.Model(&models.Session{}).Where("organization_id = ? AND status IN ?", "org-a", models.SessionNonTerminalStatuses()).Count(&ownActive)
	db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", "org-b", "active").Count(&foreignActive)
	if ownActive != 3 || foreignActive != 1 {
		t.Fatalf("generic lockdown mutated sessions: own=%d foreign=%d", ownActive, foreignActive)
	}
}

func TestPauseExecutionCannotAffectAnotherOrganization(t *testing.T) {
	db := fleetLifecycleDB(t)
	db.Create(&models.Harness{OrganizationID: "org-a", HarnessID: "shared", Status: "enrolled"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-a"}, SessionID: "own", HarnessID: "shared", UserID: "user", Status: "active"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org-b"}, SessionID: "foreign", HarnessID: "shared", UserID: "user", Status: "active"})
	svc := New(db, sessionlifecycle.New(db))
	svc.SetDirectiveSender(func(ActionRequest, string) error { return nil })
	if err := svc.PerformAction(ActionRequest{OrganizationID: "org-a", HarnessID: "shared", Action: ActionPauseExecution, Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	var own, foreign models.Session
	db.First(&own, "session_id = ?", "own")
	db.First(&foreign, "session_id = ?", "foreign")
	if own.Status != "paused" || foreign.Status != "active" {
		t.Fatalf("own=%q foreign=%q", own.Status, foreign.Status)
	}
}
