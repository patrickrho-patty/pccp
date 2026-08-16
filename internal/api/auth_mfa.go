package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// auth_mfa.go: web/25 B + web/26 C — TOTP second factor for console
// operators (RFC 6238, 30s step, 6 digits). Enrollment is per
// admin_credentials row; login requires the code once enrolled.

// adminMFA tracks the TOTP state on the admin row (mfa_secret hex,
// mfa_enrolled flag). Secrets live in the same protected DB as the
// password hash.

type mfaState struct {
	Secret   string `json:"-"`
	Enrolled bool
}

// loginThrottle is an in-memory failed-attempt limiter (5 attempts →
// 15-minute lockout per email). Honest brute-force protection on the
// operator login surface; a distributed deployment would move this to
// a shared store.
var loginThrottle struct {
	mu       sync.Mutex
	failures map[string]int
	lockedAt map[string]time.Time
}

func init() {
	loginThrottle.failures = map[string]int{}
	loginThrottle.lockedAt = map[string]time.Time{}
}

// resetMFAGuards clears the in-memory throttle/replay state (tests run
// with -count>1 in one process; production never calls this).
func resetMFAGuards() {
	loginThrottle.mu.Lock()
	loginThrottle.failures = map[string]int{}
	loginThrottle.lockedAt = map[string]time.Time{}
	loginThrottle.mu.Unlock()
	totpReplay.mu.Lock()
	totpReplay.byAcct = map[string]int64{}
	totpReplay.mu.Unlock()
}

const (
	maxLoginFailures   = 5
	loginLockoutWindow = 15 * time.Minute
)

func throttleCheck(email string) bool {
	loginThrottle.mu.Lock()
	defer loginThrottle.mu.Unlock()
	if at, ok := loginThrottle.lockedAt[email]; ok && time.Since(at) < loginLockoutWindow {
		return false
	}
	if time.Since(loginThrottle.lockedAt[email]) >= loginLockoutWindow {
		delete(loginThrottle.lockedAt, email)
		delete(loginThrottle.failures, email)
	}
	return true
}

func throttleRecordFailure(email string) {
	loginThrottle.mu.Lock()
	defer loginThrottle.mu.Unlock()
	// Bounded memory: distinct-email floods reset the table rather than
	// growing it without limit (in-memory limiter; honest best effort).
	if len(loginThrottle.failures)+len(loginThrottle.lockedAt) > 100_000 {
		loginThrottle.failures = map[string]int{}
		loginThrottle.lockedAt = map[string]time.Time{}
	}
	loginThrottle.failures[email]++
	if loginThrottle.failures[email] >= maxLoginFailures {
		loginThrottle.lockedAt[email] = time.Now()
		loginThrottle.failures[email] = 0
	}
}

func throttleClear(email string) {
	loginThrottle.mu.Lock()
	defer loginThrottle.mu.Unlock()
	delete(loginThrottle.failures, email)
	delete(loginThrottle.lockedAt, email)
}

// generateTOTPSecret returns a new base32 secret.
func generateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// totpReplay tracks the last accepted timestep per account so a code
// cannot be replayed within its ±30s validity window (RFC 6238 §5.2).
// Bounded like the login throttle maps.
var totpReplay struct {
	mu     sync.Mutex
	byAcct map[string]int64 // email → last accepted unix timestep
}

func init() {
	totpReplay.byAcct = map[string]int64{}
}

// verifyTOTPAcct is verifyTOTP plus replay protection: once a timestep
// has been accepted for the account, it (and anything earlier) is
// refused on subsequent use.
func verifyTOTPAcct(acct, secret, code string) bool {
	if len(code) != 6 {
		return false
	}
	now := time.Now()
	var acceptedStep int64 = -1
	for _, off := range []int64{-30, 0, 30} {
		want, err := totpCode(secret, now.Add(time.Duration(off)*time.Second))
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			acceptedStep = now.Unix()/30 + off/30
			break
		}
	}
	if acceptedStep < 0 {
		return false
	}
	totpReplay.mu.Lock()
	defer totpReplay.mu.Unlock()
	// Bounded memory WITHOUT the full-clear hole: pruning drops only
	// entries older than the validation window (2 timesteps = 90s), so
	// every account whose code could still verify keeps its guard.
	// A hard cap remains as the last resort for pathological churn.
	if len(totpReplay.byAcct) > 100_000 {
		minLive := now.Unix()/30 - 2
		for k, step := range totpReplay.byAcct {
			if step < minLive {
				delete(totpReplay.byAcct, k)
			}
		}
		if len(totpReplay.byAcct) > 100_000 { // still pathological: reset
			totpReplay.byAcct = map[string]int64{}
		}
	}
	if last, ok := totpReplay.byAcct[acct]; ok && acceptedStep <= last {
		return false
	}
	totpReplay.byAcct[acct] = acceptedStep
	return true
}

// verifyTOTP checks a code against the secret across the current step
// and its immediate neighbors (±30s skew per RFC 6238 verification
// practice). Constant-time comparison per candidate.
func verifyTOTP(secret, code string) bool {
	if len(code) != 6 {
		return false
	}
	now := time.Now()
	for _, off := range []int64{-30, 0, 30} {
		want, err := totpCode(secret, now.Add(time.Duration(off)*time.Second))
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

// totpCode computes the current RFC 6238 code for a secret.
func totpCode(secret string, at time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	counter := uint64(at.Unix() / 30)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// handleMFASetup issues a TOTP secret for the operator's account.
func (s *Server) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	email := getOperatorEmail(r)
	if email == "" || email == "unknown" {
		writeError(w, http.StatusUnauthorized, "operator identity required")
		return
	}
	var req struct {
		CurrentCode string `json:"current_code"`
	}
	_ = decodeJSON(r, &req) // optional body
	var admin identity.AdminCredentials
	if err := s.db.Where("email = ?", email).First(&admin).Error; err != nil {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	// Rotating an already-enrolled secret proves possession of the
	// current factor — a stolen session alone cannot silently replace
	// the operator's second factor.
	if admin.MFAEnrolled {
		if !verifyTOTPAcct(email, admin.MFASecret, req.CurrentCode) {
			writeError(w, http.StatusUnauthorized, "현재 코드 확인 필요 · current TOTP code required to rotate MFA")
			return
		}
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Store pending secret until verify.
	s.db.Model(&admin).Updates(map[string]interface{}{
		"mfa_secret": secret, "mfa_enrolled": false,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secret":  secret,
		"otpauth": fmt.Sprintf("otpauth://totp/PCCP:%s?secret=%s&issuer=PCCP", admin.Email, secret),
	})
}

// handleMFAVerify confirms a code and enrolls MFA.
func (s *Server) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	email := getOperatorEmail(r)
	if email == "" || email == "unknown" {
		writeError(w, http.StatusUnauthorized, "operator identity required")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var admin identity.AdminCredentials
	if err := s.db.Where("email = ?", email).First(&admin).Error; err != nil {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if admin.MFASecret == "" {
		writeError(w, http.StatusBadRequest, "setup first")
		return
	}
	if !verifyTOTP(admin.MFASecret, req.Code) {
		writeError(w, http.StatusUnauthorized, "코드 불일치 · code mismatch")
		return
	}
	s.db.Model(&admin).Update("mfa_enrolled", true)
	s.db.Create(&models.AuditEvent{
		OrganizationID: admin.OrganizationID, EventType: "cp.auth.mfa_enrolled", ActorType: "admin",
		Action: "enroll_mfa", ResourceType: "admin_credential", ResourceID: admin.Email,
		Result: "success", OccurredAt: time.Now().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "enrolled"})
}
