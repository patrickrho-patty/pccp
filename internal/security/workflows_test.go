package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type workflowDoerFunc func(*http.Request) (*http.Response, error)

func (f workflowDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func workflowsDB(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/s.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.SecurityFinding{}, &models.AlertEndpoint{}, &models.AlertDeliveryJob{}, &models.PIILexicon{},
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
	svc.SetAlertHTTPClient(workflowDoerFunc(func(r *http.Request) (*http.Response, error) {
		received++
		json.NewDecoder(r.Body).Decode(&gotPayload)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	}))

	// Endpoint routes only critical findings.
	ep := models.AlertEndpoint{OrganizationID: "org1", Name: "oncall", Type: "webhook", Target: "https://alerts.example/hook", SeveritiesJSON: `["critical"]`, Enabled: true}
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

func TestSeverityRoutingEmptyMeansAllMalformedFailsClosed(t *testing.T) {
	if !severityRouted("", "critical") || !severityRouted("[]", "low") || !severityRouted("null", "high") {
		t.Fatal("unset and explicitly empty severity filters must route all severities")
	}
	if severityRouted("not-json", "critical") {
		t.Fatal("malformed stored severity filter must fail closed")
	}
}

func TestRotationRequiredEndpointCannotDeliver(t *testing.T) {
	svc, db := workflowsDB(t)
	called := 0
	svc.SetAlertHTTPClient(workflowDoerFunc(func(*http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	}))
	db.Create(&models.AlertEndpoint{OrganizationID: "org1", Name: "quarantined", Type: "webhook", Target: "https://example.com/hook", Enabled: true, RotationRequired: true})
	if delivered := svc.DispatchAlerts("org1", models.SecurityFinding{OrganizationID: "org1", Severity: "critical"}); delivered != 0 || called != 0 {
		t.Fatalf("rotation-required endpoint delivered: delivered=%d called=%d", delivered, called)
	}
}

func TestAlertProviderReadinessFailsForEncryptedRowsWithoutProvider(t *testing.T) {
	_, db := workflowsDB(t)
	db.Create(&models.AlertEndpoint{OrganizationID: "org1", Name: "encrypted", Type: "webhook", TargetEnc: "opaque"})
	if err := ValidateAlertProviderReadiness(db, nil); err == nil {
		t.Fatal("process must not become ready with encrypted endpoints and no provider")
	}
}

func TestAlertProviderReadinessRejectsLegacyAndAuthenticatesEveryEnvelope(t *testing.T) {
	_, db := workflowsDB(t)
	provider, _ := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "ready")
	legacy := models.AlertEndpoint{Base: models.Base{ID: "legacy-ready"}, OrganizationID: "org1", Name: "legacy", Type: "webhook", Target: "https://example.com/legacy"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateAlertProviderReadiness(db, provider); err == nil {
		t.Fatal("readiness must reject plaintext rows")
	}
	db.Delete(&legacy)
	ctx := keymgmt.AlertSecretContext{OrganizationID: "org1", EndpointID: "encrypted-ready", ProviderType: "webhook"}
	encoded, kekID, credentialID, bindingVersion, err := keymgmt.SealAlertSecret(provider, "https://example.com/hook", ctx)
	if err != nil {
		t.Fatal(err)
	}
	ep := models.AlertEndpoint{Base: models.Base{ID: ctx.EndpointID}, OrganizationID: ctx.OrganizationID, Name: "encrypted", Type: ctx.ProviderType,
		TargetEnc: encoded, TargetKEKID: kekID, TargetBindingVersion: bindingVersion, CredentialID: credentialID}
	if err := db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	wrongProvider, _ := keymgmt.NewLocalProvider([]byte("fedcba9876543210fedcba9876543210"), "ready")
	if err := ValidateAlertProviderReadiness(db, wrongProvider); err == nil {
		t.Fatal("readiness must authenticate envelopes, not only match KEK ids")
	}
	if err := ValidateAlertProviderReadiness(db, provider); err != nil {
		t.Fatalf("valid provider should pass readiness: %v", err)
	}
}

func TestDispatchAlertLogDoesNotContainTargetOrRawTransportError(t *testing.T) {
	svc, db := workflowsDB(t)
	target := "https://hooks.slack.com/services/A/B/never-log"
	svc.SetAlertHTTPClient(workflowDoerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed for " + target)
	}))
	db.Create(&models.AlertEndpoint{OrganizationID: "org1", Name: target, Type: "slack", Target: target, CredentialID: "cred-safe", Enabled: true})

	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	_ = svc.DispatchAlerts("org1", models.SecurityFinding{OrganizationID: "org1", Severity: "critical", Title: "x"})
	if strings.Contains(logs.String(), target) || strings.Contains(logs.String(), "never-log") || strings.Contains(logs.String(), "dial failed") {
		t.Fatalf("delivery log leaked credential/error detail: %s", logs.String())
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
