package relay

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/security"
)

type relayAlertDoer func(*http.Request) (*http.Response, error)

func (f relayAlertDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestRelayFindingUsesInjectedProviderAndDurableDeliveryWorker(t *testing.T) {
	database := setupGovernedTestDB(t)
	provider, err := keymgmt.NewLocalProvider([]byte("0123456789abcdef0123456789abcdef"), "relay-kek")
	if err != nil {
		t.Fatal(err)
	}
	endpointID := models.GenerateID("alert")
	encoded, kekID, credentialID, bindingVersion, err := keymgmt.SealAlertSecret(provider,
		"https://alerts.example/hook", keymgmt.AlertSecretContext{OrganizationID: "org-alert", EndpointID: endpointID, ProviderType: "webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.AlertEndpoint{
		Base: models.Base{ID: endpointID}, OrganizationID: "org-alert", Name: "oncall", Type: "webhook",
		TargetEnc: encoded, TargetKEKID: kekID, TargetBindingVersion: bindingVersion,
		CredentialID: credentialID, SeveritiesJSON: `[]`, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc, err := New(database, "", "relay-alert-test")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAlertKeyProvider(provider)
	called := 0
	svc.SetAlertHTTPClient(relayAlertDoer(func(req *http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}))
	if _, err := svc.security.RecordFinding("org-alert", "session-1", "exchange-1", security.SecurityFinding{
		Type: "secret", Severity: "critical", Title: "Secret", RuleID: "secret-test",
	}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("relay hot path performed synchronous webhook I/O")
	}
	processed, err := svc.ProcessAlertDeliveries(context.Background(), 10)
	if err != nil || processed != 1 || called != 1 {
		t.Fatalf("relay delivery path not wired: processed=%d called=%d err=%v", processed, called, err)
	}
	var job models.AlertDeliveryJob
	if err := database.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "delivered" || time.Since(job.UpdatedAt) > time.Minute {
		t.Fatalf("durable job not completed: %+v", job)
	}
}
