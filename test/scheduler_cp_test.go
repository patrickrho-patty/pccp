package test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

func authedGet(ts *httptest.Server, path, token string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func TestCPConfigSigningEndpoint(t *testing.T) {
	ts, _ := setupAPITest(t)
	defer ts.Close()
	token := getTestToken(t, ts)

	// The config public key endpoint distributes the verification key.
	pubResp, err := authedGet(ts, "/api/scheduler/config-public-key", token)
	if err != nil {
		t.Fatal(err)
	}
	if pubResp.StatusCode != http.StatusOK {
		t.Fatalf("config-public-key status %d", pubResp.StatusCode)
	}
	var pubBody struct {
		PublicKeyHex string `json:"public_key_hex"`
	}
	json.NewDecoder(pubResp.Body).Decode(&pubBody)
	pubBytes, err := hex.DecodeString(pubBody.PublicKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		t.Fatalf("invalid public key: %q", pubBody.PublicKeyHex)
	}

	// Sign a worker config and verify the envelope with the distributed key.
	cfg := scheduler.PIAWorkerConfig{
		WorkerID:      "wkr-test-001",
		TenantID:      "tenant-a",
		BackendMode:   "localhost",
		EngineURL:     "http://127.0.0.1:8033/v1",
		AllowedModels: []string{"qwen3.6-27b"},
	}
	body, _ := json.Marshal(cfg)
	resp, err := authedPost(ts, "/api/scheduler/configs", token, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("configs status %d", resp.StatusCode)
	}
	var envelope scheduler.SignedConfig
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if err := envelope.Verify(ed25519.PublicKey(pubBytes)); err != nil {
		t.Fatalf("verify envelope: %v", err)
	}
	card := scheduler.WorkerCard{
		WorkerID:         cfg.WorkerID,
		ReachabilityMode: cfg.BackendMode,
		ModelName:        "qwen3.6-27b",
	}
	if err := envelope.Authorizes(card); err != nil {
		t.Fatalf("envelope should authorize card: %v", err)
	}

	// The key must be stable across requests (no rotation on every sign).
	pubResp2, err := authedGet(ts, "/api/scheduler/config-public-key", token)
	if err != nil {
		t.Fatal(err)
	}
	var pubBody2 struct {
		PublicKeyHex string `json:"public_key_hex"`
	}
	json.NewDecoder(pubResp2.Body).Decode(&pubBody2)
	if pubBody2.PublicKeyHex != pubBody.PublicKeyHex {
		t.Fatal("config key rotated between requests")
	}
}

func TestCPRevocationFeed(t *testing.T) {
	ts, db := setupAPITest(t)
	defer ts.Close()
	token := getTestToken(t, ts)

	// A revoked worker identity must appear in the scheduler feed.
	revoked := models.Harness{
		OrganizationID:   "org-1",
		HarnessID:        "wkr-revoked-001",
		Status:           "revoked",
		RevocationReason: "compromise",
	}
	if err := db.Create(&revoked).Error; err != nil {
		t.Fatal(err)
	}

	resp, err := authedGet(ts, "/api/scheduler/revocations", token)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revocations status %d", resp.StatusCode)
	}
	var feed struct {
		RevokedPeerIDs []string `json:"revoked_peer_ids"`
		RevokedSerials []string `json:"revoked_serials"`
		Epoch          uint64   `json:"epoch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		t.Fatalf("decode feed: %v", err)
	}
	found := false
	for _, id := range feed.RevokedPeerIDs {
		if id == "wkr-revoked-001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("revoked peer not in feed: %+v", feed.RevokedPeerIDs)
	}
}

type schedulerTestWorker struct {
	Trust       scheduler.Trust
	Card        scheduler.WorkerCard
	SubjectPub  ed25519.PublicKey
	EvidenceKey ed25519.PrivateKey
	Now         time.Time
}

func schedulerWorkerForTest(t *testing.T) schedulerTestWorker {
	t.Helper()
	ca, err := dari.NewPeerCredentialIssuer("test-ca")
	if err != nil {
		t.Fatal(err)
	}
	_, configPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	subjectPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := scheduler.Trust{
		Issuers:      map[string]ed25519.PublicKey{"test-ca": ca.PublicKey},
		ConfigPubKey: ed25519.PublicKey(configPriv.Public().(ed25519.PublicKey)),
		Now:          time.Now,
	}
	card := scheduler.WorkerCard{WorkerID: "wkr-readthrough-001", ModelName: "qwen3.6-27b"}
	return schedulerTestWorker{
		Trust:       trust,
		Card:        card,
		SubjectPub:  subjectPub,
		EvidenceKey: evidenceKey,
		Now:         time.Now(),
	}
}

func TestCPWorkersReadThrough(t *testing.T) {
	// Run a real scheduler with one registered worker.
	fx := schedulerWorkerForTest(t)
	sched := scheduler.NewScheduler(fx.Trust, nil, 30*time.Second, 60*time.Second, fx.EvidenceKey)
	if _, err := sched.Registry.Register(fx.Card, fx.SubjectPub, fx.Now); err != nil {
		t.Fatal(err)
	}
	schedHTTP := httptest.NewServer(scheduler.NewHTTPHandler(sched, ""))
	defer schedHTTP.Close()
	t.Setenv("PCCP_SCHED_HTTP_ADDR", schedHTTP.Listener.Addr().String())

	ts, _ := setupAPITest(t)
	defer ts.Close()
	token := getTestToken(t, ts)

	resp, err := authedGet(ts, "/api/scheduler/workers", token)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workers proxy status %d", resp.StatusCode)
	}
	var workers []scheduler.WorkerEntry
	if err := json.NewDecoder(resp.Body).Decode(&workers); err != nil {
		t.Fatalf("decode workers: %v", err)
	}
	if len(workers) != 1 || workers[0].Card.WorkerID != fx.Card.WorkerID {
		t.Fatalf("workers %+v", workers)
	}
}
