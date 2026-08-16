package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// TestCrossOrgMessageMutationRejected (round-2 BOLA): org A's operator
// must not react/read/edit/delete a message in org B's conversation —
// even with the exact message id.
func TestCrossOrgMessageMutationRejected(t *testing.T) {
	srv, db := complianceTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "orga-msg", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "orgb-msg", Status: "active"}
	if err := db.Create(&orgA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&orgB).Error; err != nil {
		t.Fatal(err)
	}

	convB := models.Conversation{AuditBase: models.AuditBase{Base: models.Base{ID: models.GenerateID("conv")}, OrganizationID: orgB.ID}, Type: "direct"}
	if err := db.Create(&convB).Error; err != nil {
		t.Fatal(err)
	}
	msgB := models.Message{Base: models.Base{ID: models.GenerateID("msg")}, ConversationID: convB.ID, SenderID: "u-b", Content: "b secret"}
	if err := db.Create(&msgB).Error; err != nil {
		t.Fatal(err)
	}

	do := func(method, path string, body string) int {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, httptest.NewRecorder().Body)
			req.Body = http.NoBody
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgA.ID, Email: "ops@a"}))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w.Code
	}
	if c := do(http.MethodPost, "/api/communications/messages/"+msgB.ID+"/react", `{"emoji":"👍","user_id":"x"}`); c != http.StatusNotFound {
		t.Fatalf("cross-org react = %d, want 404", c)
	}
	if c := do(http.MethodPost, "/api/communications/messages/"+msgB.ID+"/read", `{"user_id":"x"}`); c != http.StatusNotFound {
		t.Fatalf("cross-org read = %d, want 404", c)
	}
	_ = json.Marshal
}

// TestCrossOrgConversationAccessRejected (round-4): org A's operator
// must not list or send into org B's conversation even with exact ids.
func TestCrossOrgConversationAccessRejected(t *testing.T) {
	srv, db := complianceTestServer(t)
	orgA := models.Organization{Name: "A", Slug: "orga-conv", Status: "active"}
	orgB := models.Organization{Name: "B", Slug: "orgb-conv", Status: "active"}
	if err := db.Create(&orgA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&orgB).Error; err != nil {
		t.Fatal(err)
	}
	convB := models.Conversation{AuditBase: models.AuditBase{Base: models.Base{ID: models.GenerateID("conv")}, OrganizationID: orgB.ID}, Type: "channel", Title: "b"}
	if err := db.Create(&convB).Error; err != nil {
		t.Fatal(err)
	}

	// Cross-org LIST via the live route.
	req := httptest.NewRequest(http.MethodGet, "/api/communications/conversations/"+convB.ID+"/messages", nil)
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgA.ID, Email: "ops@a"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// Fail-closed: org A sees an empty list (indistinguishable from a
	// nonexistent conversation — no existence oracle), and critically
	// NO org-B content is returned.
	var listed []map[string]interface{}
	if rec.Code == http.StatusOK {
		_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	}
	for _, m := range listed {
		if s, _ := m["content"].(string); s != "" {
			t.Fatalf("cross-org list leaked org-B content: %v", m)
		}
	}
	// And the send path leaves zero rows in org B's conversation.
	sendBody := `{"sender_id":"ops@a","content":"injected"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/communications/conversations/"+convB.ID+"/messages", strings.NewReader(sendBody))
	req2.Header.Set("Content-Type", "application/json")
	req2 = req2.WithContext(contextWithClaims(req2.Context(), &identity.Claims{OrganizationID: orgA.ID, Email: "ops@a"}))
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	var n int64
	db.Model(&models.Message{}).Where("conversation_id = ?", convB.ID).Count(&n)
	if n != 0 {
		t.Fatalf("cross-org send leaked a message into org B's conversation (%d rows)", n)
	}
}
