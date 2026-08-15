package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func workflowsDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/s.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.SecurityFinding{}, &models.AlertEndpoint{}, &models.PIILexicon{},
		&models.PromptExchange{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return New(db), db
}

func TestSuppressAndSweep(t *testing.T) {
	svc, db := workflowsDB(t)
	f := models.SecurityFinding{OrganizationID: "org1", FindingType: "pii", Severity: "high", Title: "x", Status: "open", OccurredAt: "2026-01-01T00:00:00Z"}
	db.Create(&f)

	if err := svc.SuppressFinding("org1", f.ID, "test data", "admin", 30); err != nil {
		t.Fatal(err)
	}
	var stored models.SecurityFinding
	db.First(&stored, "id = ?", f.ID)
	if stored.Status != "suppressed" || !stored.Suppressed || stored.SuppressReason != "test data" {
		t.Fatalf("suppress not persisted: %+v", stored)
	}

	// Expire the suppression and sweep.
	db.Model(&stored).Update("suppress_expiry", time.Now().Add(-time.Hour).Format(time.RFC3339))
	if n := svc.SweepSuppressions(); n != 1 {
		t.Fatalf("expected 1 reopened, got %d", n)
	}
	db.First(&stored, "id = ?", f.ID)
	if stored.Status != "open" || stored.Suppressed {
		t.Fatalf("sweep should reopen: %+v", stored)
	}
}

func TestDispatchAlertsRoutesBySeverity(t *testing.T) {
	svc, db := workflowsDB(t)
	received := 0
	var gotPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Endpoint routes only critical findings.
	ep := models.AlertEndpoint{OrganizationID: "org1", Name: "oncall", Type: "webhook", Target: srv.URL, SeveritiesJSON: `["critical"]`, Enabled: true}
	db.Create(&ep)

	f := models.SecurityFinding{OrganizationID: "org1", FindingType: "secret", Severity: "high", Title: "t", Status: "open"}
	delivered := svc.DispatchAlerts("org1", f)
	if delivered != 0 {
		t.Fatalf("high should not route to critical-only endpoint, delivered=%d", delivered)
	}
	f.Severity = "critical"
	delivered = svc.DispatchAlerts("org1", f)
	if delivered != 1 || received != 1 {
		t.Fatalf("critical should route, delivered=%d received=%d", delivered, received)
	}
	if gotPayload == nil || gotPayload["severity"] != "critical" {
		t.Fatalf("payload mismatch: %v", gotPayload)
	}
}

func TestLexiconOverridesPIIPatterns(t *testing.T) {
	svc, db := workflowsDB(t)
	// Built-in RRN pattern matches 901225-1234567.
	if res := svc.CheckContext("org1", "주민 901225-1234567"); len(res.Findings) == 0 {
		t.Fatal("builtin lexicon should detect RRN")
	}
	// Publish an override that blocks bank accounts instead of RRNs:
	// the RRN rule now matches a custom pattern that won't hit.
	if _, err := svc.SetLexicon("org1", "2", "admin", map[string]string{
		"pii-kr-rrn": `\bXXXXX\d{2}-\d{7}\b`,
	}); err != nil {
		t.Fatal(err)
	}
	if res := svc.CheckContext("org1", "주민 901225-1234567"); len(res.Findings) != 0 {
		hasRRN := false
		for _, f := range res.Findings {
			if f.RuleID == "pii-kr-rrn" {
				hasRRN = true
			}
		}
		if hasRRN {
			t.Fatal("lexicon override should have replaced the RRN pattern")
		}
	}
	var lexicon models.PIILexicon
	db.First(&lexicon, "organization_id = ?", "org1")
	if lexicon.Version != "2" {
		t.Fatalf("lexicon version not persisted: %s", lexicon.Version)
	}
}

func TestScanSessionFindsBothSides(t *testing.T) {
	svc, db := workflowsDB(t)
	db.Create(&models.PromptExchange{SessionID: "ses-1", ExchangeID: "ex-1", PromptText: "aws key AKIAABCDEFGHIJKLMNOP here", ResponseText: "fine"})
	res, err := svc.ScanSession("org1", "ses-1")
	if err != nil {
		t.Fatal(err)
	}
	if res["total_findings"].(int) == 0 {
		t.Fatalf("expected findings from request scan: %v", res)
	}
	var count int64
	db.Model(&models.SecurityFinding{}).Where("session_id = ? AND direction = ?", "ses-1", "request").Count(&count)
	if count == 0 {
		t.Fatal("request-direction finding not persisted")
	}
}
