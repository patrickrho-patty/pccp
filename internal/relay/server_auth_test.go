package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelayAdminHTTPRequiresControlPlaneBearer(t *testing.T) {
	server := NewServer(nil)
	server.SetAdminToken("")

	req := httptest.NewRequest(http.MethodPost, "/v1/exchanges", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured admin surface status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	server.SetAdminToken("control-plane-test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/exchanges", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	// The matching bearer reaches the handler. The intentionally empty body is
	// rejected as malformed before the nil service is touched.
	req = httptest.NewRequest(http.MethodPost, "/v1/exchanges", nil)
	req.Header.Set("Authorization", "Bearer control-plane-test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("matching bearer did not reach handler: status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
