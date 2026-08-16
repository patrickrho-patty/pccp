package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/provenance"
)

// TestProvenanceReceiptsRealSignature (regression for the drift where the
// verifier re-derived an invented chain instead of checking the receipt's
// actual COSE-Sign1): a receipt issued by the real provenance service must
// verify as signature_verified, and a tampered ChainRoot must fail.
func TestProvenanceReceiptsRealSignature(t *testing.T) {
	srv, db := complianceTestServer(t)
	org := models.Organization{Name: "o", Slug: "provrec", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	prov, err := provenance.New(db, "relay-1")
	if err != nil {
		t.Fatal(err)
	}

	// Session + one good receipt + one tampered receipt.
	sess := models.Session{AuditBase: models.AuditBase{Base: models.Base{ID: "sess_r1"}, OrganizationID: org.ID}, SessionID: "sess_r1", UserID: "u1", Status: "closed"}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatal(err)
	}
	good, err := prov.IssueEvidenceReceipt(provenance.IssueReceiptRequest{
		OrganizationID: org.ID, ExchangeID: "exch_good", SessionID: "sess_r1",
		FinalState: "completed", LastEventSeq: 3, ChainRoot: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	tampered := *good
	tampered.Base = models.Base{ID: models.GenerateID("er")}
	tampered.ExchangeID = "exch_bad"
	tampered.ChainRoot = "tampered" // payload no longer matches signature
	if err := db.Create(&tampered).Error; err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/sessions/sess_r1/provenance/receipts", nil)
	r = r.WithContext(contextWithClaims(r.Context(), &identity.Claims{OrganizationID: org.ID}))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var rows []struct {
		ExchangeID   string `json:"exchange_id"`
		Verified     bool   `json:"verified"`
		Verification string `json:"verification"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]bool{}
	for _, row := range rows {
		byID[row.ExchangeID] = row.Verified
		if row.ExchangeID == "exch_good" && !row.Verified {
			t.Fatalf("good receipt must verify, got %q", row.Verification)
		}
		if row.ExchangeID == "exch_bad" && row.Verified {
			t.Fatalf("tampered receipt must NOT verify")
		}
	}
	if !byID["exch_good"] {
		t.Fatal("good receipt missing from response")
	}
}
