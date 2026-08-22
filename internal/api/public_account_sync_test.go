package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func signedPublicAccountSyncRequest(t *testing.T, raw, token, signatureToken string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/internal/public-accounts/sync", strings.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	mac := hmac.New(sha256.New, []byte(signatureToken))
	_, _ = mac.Write([]byte(raw))
	req.Header.Set("X-Patty-Sync-Signature", hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestPublicAccountSyncIsSignedIdempotentAndBindsAllIdentities(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCP_ACCOUNTS_SERVICE_TOKEN", "sync-secret")
	t.Setenv("PCCP_SYNC_HMAC_KEY", "sync-secret")
	raw := `{"schema_version":"pccp.public-account-sync.v1","source_event_id":"billing:evt-1","account_id":"patty-account:1","revision":1,"profile":{"email":"owner@example.com","display_name":"Owner"},"identities":[{"issuer":"https://login.patty.io/realms/patty","subject":"sub-1"},{"issuer":"https://login.patty.io/realms/patty","subject":"sub-2"}],"subscription":{"plan":"pro","status":"active","expires_at":"2026-09-22T12:34:56.000Z"}}`
	for attempt := 0; attempt < 2; attempt++ {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, signedPublicAccountSyncRequest(t, raw, "sync-secret", "sync-secret"))
		if rec.Code != http.StatusOK {
			t.Fatalf("sync attempt %d = %d: %s", attempt+1, rec.Code, rec.Body.String())
		}
	}
	var account models.Account
	if err := db.Where("authority_id = ?", "patty-account:1").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	var identities int64
	if err := db.Model(&models.AccountExternalIdentity{}).Where("account_id = ?", account.ID).Count(&identities).Error; err != nil || identities != 2 {
		t.Fatalf("synced identities=%d err=%v", identities, err)
	}
	var subscriptions int64
	if err := db.Model(&models.Subscription{}).Where("account_id = ? AND payment_provider = ?", account.ID, "accounts-authority").Count(&subscriptions).Error; err != nil || subscriptions != 1 {
		t.Fatalf("authority subscriptions=%d err=%v", subscriptions, err)
	}

	bad := httptest.NewRecorder()
	srv.ServeHTTP(bad, signedPublicAccountSyncRequest(t, raw, "sync-secret", "wrong-secret"))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad sync signature = %d, want 401", bad.Code)
	}
}

func TestPublicAccountSyncRejectsMutatedReplayAndEqualRevision(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCP_ACCOUNTS_SERVICE_TOKEN", "sync-secret")
	t.Setenv("PCCP_SYNC_HMAC_KEY", "sync-secret")
	base := `{"schema_version":"pccp.public-account-sync.v1","source_event_id":"billing:evt-2","account_id":"patty-account:2","revision":1,"profile":{"email":"owner@example.com","display_name":"Owner"},"identities":[{"issuer":"https://login.patty.io/realms/patty","subject":"sub-1"}],"subscription":{"plan":"pro","status":"active","expires_at":null}}`
	for _, tc := range []struct {
		name, raw string
		want      int
	}{
		{"initial", base, http.StatusOK},
		{"exact replay", base, http.StatusOK},
		{"mutated event replay", strings.Replace(base, `"display_name":"Owner"`, `"display_name":"Changed"`, 1), http.StatusConflict},
		{"equal revision different event", strings.Replace(base, `"source_event_id":"billing:evt-2"`, `"source_event_id":"billing:evt-3"`, 1), http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, signedPublicAccountSyncRequest(t, tc.raw, "sync-secret", "sync-secret"))
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestValidatePublicAccountSyncRequiresCanonicalEnvelopeAndBounds(t *testing.T) {
	valid := publicAccountSyncRequest{SchemaVersion: publicAccountSyncSchemaVersion, SourceEventID: "identity:op", AccountID: "patty-account:42", Revision: 1}
	valid.Profile.Email, valid.Subscription.Plan, valid.Subscription.Status = "a@b.test", "pro", "canceling"
	valid.Identities = []publicAccountSyncIdentity{{Issuer: "https://issuer.test", Subject: "sub"}}
	if err := validatePublicAccountSync(valid); err != nil {
		t.Fatalf("valid contract: %v", err)
	}
	for _, mutate := range []func(*publicAccountSyncRequest){
		func(r *publicAccountSyncRequest) { r.SchemaVersion = "v2" },
		func(r *publicAccountSyncRequest) { r.AccountID = "42" },
		func(r *publicAccountSyncRequest) { r.Revision = 0 },
		func(r *publicAccountSyncRequest) {
			r.Identities = append(r.Identities, make([]publicAccountSyncIdentity, 20)...)
		},
		func(r *publicAccountSyncRequest) { r.Identities[0].Issuer = strings.Repeat("x", 513) },
	} {
		copy := valid
		copy.Identities = append([]publicAccountSyncIdentity(nil), valid.Identities...)
		mutate(&copy)
		if err := validatePublicAccountSync(copy); err == nil {
			t.Fatalf("invalid request accepted: %+v", copy)
		}
	}
}

func TestPublicAccountSyncV1WorkerGoldenFixtureConforms(t *testing.T) {
	raw, err := os.ReadFile("testdata/public_account_sync_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var req publicAccountSyncRequest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		t.Fatalf("decode Worker golden: %v", err)
	}
	if err := validatePublicAccountSync(req); err != nil {
		t.Fatalf("validate Worker golden: %v", err)
	}
	if req.AccountID != "patty-account:42" || req.Subscription.ExpiresAt == nil {
		t.Fatalf("unexpected golden DTO: %+v", req)
	}
}

func TestPublicAccountSyncV1EvaluationGoldenFixtureConforms(t *testing.T) {
	raw, err := os.ReadFile("testdata/public_account_sync_v1_evaluation_zero_identities.json")
	if err != nil {
		t.Fatal(err)
	}
	var req publicAccountSyncRequest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		t.Fatal(err)
	}
	if err := validatePublicAccountSync(req); err != nil {
		t.Fatal(err)
	}
	if req.Revision != 8 || req.Subscription.Plan != "free" || req.Subscription.Status != "lapsed" || req.Subscription.ExpiresAt != nil || len(req.Identities) != 0 {
		t.Fatalf("unexpected evaluation DTO: %+v", req)
	}
}

func TestPublicAccountSyncAcceptsFreeEvaluationAndEmptyIdentitySnapshotDeauthorizes(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PCCP_ACCOUNTS_SERVICE_TOKEN", "sync-secret")
	t.Setenv("PCCP_SYNC_HMAC_KEY", "sync-hmac")
	initial := `{"schema_version":"pccp.public-account-sync.v1","source_event_id":"identity:evaluation-1","account_id":"patty-account:700","revision":1,"profile":{"email":"evaluation@example.com","display_name":"Evaluation"},"identities":[{"issuer":"https://auth.patty.io/realms/patty","subject":"subject-700"}],"subscription":{"plan":"free","status":"active","expires_at":null}}`
	empty := `{"schema_version":"pccp.public-account-sync.v1","source_event_id":"identity:evaluation-2","account_id":"patty-account:700","revision":2,"profile":{"email":"evaluation@example.com","display_name":"Evaluation"},"identities":[],"subscription":{"plan":"free","status":"active","expires_at":null}}`
	for _, raw := range []string{initial, empty} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, signedPublicAccountSyncRequest(t, raw, "sync-secret", "sync-hmac"))
		if rec.Code != http.StatusOK {
			t.Fatalf("sync=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	var account models.Account
	if err := db.Where("authority_id = ?", "patty-account:700").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.SubscriptionPlan != "free" {
		t.Fatalf("plan=%q want free", account.SubscriptionPlan)
	}
	var identities int64
	if err := db.Model(&models.AccountExternalIdentity{}).Where("account_id = ?", account.ID).Count(&identities).Error; err != nil {
		t.Fatal(err)
	}
	if identities != 0 {
		t.Fatalf("empty authoritative snapshot retained %d identities", identities)
	}
	directEmpty := strings.ReplaceAll(strings.ReplaceAll(empty, "patty-account:700", "patty-account:701"), "identity:evaluation-2", "identity:evaluation-3")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, signedPublicAccountSyncRequest(t, directEmpty, "sync-secret", "sync-hmac"))
	if rec.Code != http.StatusOK {
		t.Fatalf("new evaluation account with empty identity snapshot=%d body=%s", rec.Code, rec.Body.String())
	}
}
