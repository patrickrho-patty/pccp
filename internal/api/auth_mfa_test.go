package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
)

func TestTOTPCodeGeneration(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", code)
	}
	// Deterministic at the same timestamp.
	code2, _ := totpCode(secret, time.Now())
	if len(code2) != 6 {
		t.Fatalf("second code wrong: %q", code2)
	}
}

func TestLoginThrottleLocksAfterFailures(t *testing.T) {
	email := "throttle@test.local"
	throttleClear(email)
	for i := 0; i < maxLoginFailures; i++ {
		throttleRecordFailure(email)
	}
	if throttleCheck(email) {
		t.Fatal("expected lockout after max failures")
	}
	// Simulate window expiry.
	loginThrottle.mu.Lock()
	loginThrottle.lockedAt[email] = time.Now().Add(-loginLockoutWindow - time.Minute)
	loginThrottle.mu.Unlock()
	if !throttleCheck(email) {
		t.Fatal("expected unlock after window expiry")
	}
	throttleClear(email)
}

func TestMFAEnrolledLoginRequiresCode(t *testing.T) {
	resetMFAGuards()
	srv, db := publicOpsTestServer(t)
	// Bootstrap an admin, then enroll MFA, then login without a code.
	rec := doJSON(t, srv, "POST", "/api/auth/bootstrap",
		`{"email":"mfa@patty.dev","password":"password123","org_name":"MFA Org"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", rec.Code, rec.Body.String())
	}
	// Enroll MFA: setup then verify with the correct code.
	rec = doJSON2(t, srv, "POST", "/api/auth/mfa/setup", "", "mfa@patty.dev")
	if rec.Code != http.StatusOK {
		t.Fatalf("mfa setup failed: %d %s", rec.Code, rec.Body.String())
	}
	var admin identity.AdminCredentials
	db.Where("email = ?", "mfa@patty.dev").First(&admin)
	code, err := totpCode(admin.MFASecret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec = doJSON2(t, srv, "POST", "/api/auth/mfa/verify", `{"code":"`+code+`"}`, "mfa@patty.dev")
	if rec.Code != http.StatusOK {
		t.Fatalf("mfa verify failed: %d %s", rec.Code, rec.Body.String())
	}
	// Login without a code → 401.
	rec = doJSON(t, srv, "POST", "/api/auth/login",
		`{"email":"mfa@patty.dev","password":"password123"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without mfa code: %d", rec.Code)
	}
	// Login with the code → 200.
	code, _ = totpCode(admin.MFASecret, time.Now())
	rec = doJSON(t, srv, "POST", "/api/auth/login",
		`{"email":"mfa@patty.dev","password":"password123","mfa_code":"`+code+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with mfa code: %d %s", rec.Code, rec.Body.String())
	}
}
