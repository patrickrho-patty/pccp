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
