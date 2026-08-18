package sessionlifecycle

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func lifecycleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/lifecycle.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Session{}, &models.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func lifecycleSession(t *testing.T, db *gorm.DB, orgID, sessionID, status string) models.Session {
	t.Helper()
	sess := models.Session{AuditBase: models.AuditBase{OrganizationID: orgID}, SessionID: sessionID, UserID: "user", HarnessID: "harness", Status: status, OpenedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestTransitionIsOrgScopedTerminalAndRunsHooks(t *testing.T) {
	db := lifecycleTestDB(t)
	sess := lifecycleSession(t, db, "org-a", "ses-a", "idle")
	cleaned, notified := 0, 0
	svc := New(db)
	svc.SetCleanup(func(orgID, sessionID string) []string {
		if orgID != "org-a" || sessionID != "ses-a" {
			t.Fatalf("cleanup scope = %s/%s", orgID, sessionID)
		}
		cleaned++
		return nil
	})
	svc.SetNotifier(func(orgID, sessionID, status string) {
		notified++
	})

	foreign := svc.Transition(Request{OrganizationID: "org-b", SessionRef: sess.ID, Target: "terminated", Action: "test", Reason: "reason"})
	if foreign.Result != ResultNotFound {
		t.Fatalf("cross-org result = %q", foreign.Result)
	}
	out := svc.Transition(Request{OrganizationID: "org-a", SessionRef: sess.ID, Target: "terminated", Action: "test", Reason: "reason"})
	if out.Result != ResultUpdated || cleaned != 1 || notified != 1 {
		t.Fatalf("outcome=%+v cleaned=%d notified=%d", out, cleaned, notified)
	}
	var got models.Session
	if err := db.First(&got, "id = ?", sess.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "terminated" || got.ClosedAt == "" {
		t.Fatalf("stored session = status %q closed_at %q", got.Status, got.ClosedAt)
	}
	again := svc.Transition(Request{OrganizationID: "org-a", SessionRef: sess.ID, Target: "active", Action: "test", Reason: "reason"})
	if again.Result != ResultInvalidTransition || got.Status == "active" {
		t.Fatalf("terminal resurrection outcome = %+v", again)
	}
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).Where("organization_id = ? AND resource_id = ?", "org-a", sess.ID).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("durable outcomes = %d, want 2", auditCount)
	}
}

func TestTransitionScopeCoversEveryNonTerminalState(t *testing.T) {
	db := lifecycleTestDB(t)
	for _, status := range []string{"pending", "active", "idle", "paused"} {
		lifecycleSession(t, db, "org-a", "ses-"+status, status)
	}
	lifecycleSession(t, db, "org-a", "ses-closed", "closed")
	lifecycleSession(t, db, "org-b", "ses-foreign", "active")

	outcomes, err := New(db).TransitionScope(Scope{OrganizationID: "org-a"}, "terminated", "lockdown", "security response", "actor")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 4 {
		t.Fatalf("outcomes = %d, want 4", len(outcomes))
	}
	var orgATerminated, orgBActive int64
	db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", "org-a", "terminated").Count(&orgATerminated)
	db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", "org-b", "active").Count(&orgBActive)
	if orgATerminated != 4 || orgBActive != 1 {
		t.Fatalf("scoped results terminated=%d foreign_active=%d", orgATerminated, orgBActive)
	}
}

func TestTransitionScopeSelectsOnlyCanonicalSourcesAndPreservesActorType(t *testing.T) {
	db := lifecycleTestDB(t)
	for _, status := range []string{"pending", "active", "idle", "paused"} {
		lifecycleSession(t, db, "org-a", "ses-"+status, status)
	}

	outcomes, err := New(db).TransitionScope(
		Scope{OrganizationID: "org-a", ActorType: "system"},
		"paused", "incident_containment", "automated containment", "scim-worker",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("pause outcomes = %d, want pending, active, and idle: %+v", len(outcomes), outcomes)
	}
	for _, outcome := range outcomes {
		if outcome.Result != ResultUpdated || (outcome.From != "pending" && outcome.From != "active" && outcome.From != "idle") {
			t.Fatalf("unexpected pause outcome: %+v", outcome)
		}
	}
	var audits []models.AuditEvent
	if err := db.Where("organization_id = ? AND event_type = ?", "org-a", "cp.session.incident_containment").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 3 || audits[0].ActorType != "system" || audits[1].ActorType != "system" || audits[2].ActorType != "system" {
		t.Fatalf("scope audit attribution = %+v", audits)
	}
	var pending, paused int64
	db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", "org-a", "pending").Count(&pending)
	db.Model(&models.Session{}).Where("organization_id = ? AND status = ?", "org-a", "paused").Count(&paused)
	if pending != 0 || paused != 4 {
		t.Fatalf("state counts pending=%d paused=%d", pending, paused)
	}
}

func TestTransactionalScopeBatchesAuditAndCoalescesNotifications(t *testing.T) {
	db := lifecycleTestDB(t)
	for i := 0; i < 201; i++ {
		lifecycleSession(t, db, "org-a", models.GenerateID("ses"), "active")
	}
	if err := db.Model(&models.Session{}).Where("organization_id = ?", "org-a").Update("project_id", "project-a").Error; err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	individualNotifications := 0
	scopeNotifications := 0
	svc.SetNotifier(func(string, string, string) { individualNotifications++ })
	svc.SetScopeNotifier(func(orgID, status string, count int) {
		if orgID != "org-a" || status != "paused" || count != 201 {
			t.Fatalf("scope notification = %s/%s/%d", orgID, status, count)
		}
		scopeNotifications++
	})

	var outcomes []Outcome
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		outcomes, err = svc.TransitionScopeInTransaction(
			tx, Scope{OrganizationID: "org-a", ProjectID: "project-a", ActorType: "admin"},
			"paused", "project_archived", "project archived", "admin-a",
		)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 201 {
		t.Fatalf("outcomes = %d, want 201", len(outcomes))
	}
	if _, err := svc.FinalizeTransitions("org-a", outcomes, "paused", "project_archived", "project archived", "admin-a", "admin"); err != nil {
		t.Fatal(err)
	}
	if individualNotifications != 0 || scopeNotifications != 1 {
		t.Fatalf("notifications individual=%d scope=%d", individualNotifications, scopeNotifications)
	}
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND event_type = ? AND resource_type = ?", "org-a", "cp.session.project_archived", "session_scope").
		Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("batched lifecycle audits = %d, want 2", auditCount)
	}
}

func TestTransitionRollsBackWhenOutcomeCannotBeRecorded(t *testing.T) {
	db := lifecycleTestDB(t)
	sess := lifecycleSession(t, db, "org-a", "ses-a", "active")
	if err := db.Migrator().DropTable(&models.AuditEvent{}); err != nil {
		t.Fatal(err)
	}

	out := New(db).Transition(Request{
		OrganizationID: "org-a",
		SessionRef:     sess.ID,
		Target:         "terminated",
		Action:         "lockdown",
		Reason:         "security response",
		ActorID:        "admin",
	})
	if out.Result != ResultFailed {
		t.Fatalf("outcome = %+v, want failed", out)
	}

	var got models.Session
	if err := db.First(&got, "id = ?", sess.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" {
		t.Fatalf("session changed without a durable outcome: status=%q", got.Status)
	}
}
