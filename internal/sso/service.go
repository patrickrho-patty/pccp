package sso

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sessionlifecycle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service implements SAML 2.0 and OIDC SSO integration (PRD 8.2, 32.1).
type Service struct {
	mu          sync.RWMutex
	db          *gorm.DB
	httpClient  *http.Client
	scimTokens  map[[32]byte]string // bearer digest -> immutable organization
	lifecycle   *sessionlifecycle.Service
	publicSSO   map[string]ssoAttemptWindow
	publicSweep time.Time
	publicGate  chan struct{}
	keyProvider keymgmt.KeyProvider
}

func (s *Service) SetKeyProvider(provider keymgmt.KeyProvider) {
	s.mu.Lock()
	s.keyProvider = provider
	s.mu.Unlock()
}

type ssoAttemptWindow struct {
	Started time.Time
	Count   int
}

func (s *Service) SetSessionLifecycle(lifecycle *sessionlifecycle.Service) {
	s.lifecycle = lifecycle
}

func New(db *gorm.DB, _ string) *Service {
	return &Service{
		db: db,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		scimTokens: make(map[[32]byte]string),
		publicSSO:  make(map[string]ssoAttemptWindow),
		publicGate: make(chan struct{}, 16),
	}
}

// BeginPublicRequest applies both a concurrency ceiling and a fixed-window
// request budget to unauthenticated SSO endpoints. The returned function must
// be deferred when allowed is true.
func (s *Service) BeginPublicRequest(key string) (release func(), allowed bool) {
	select {
	case s.publicGate <- struct{}{}:
	default:
		return nil, false
	}
	release = func() { <-s.publicGate }
	now := time.Now().UTC()
	s.mu.Lock()
	if s.publicSweep.IsZero() || now.Sub(s.publicSweep) >= 10*time.Second {
		for candidate, record := range s.publicSSO {
			if now.Sub(record.Started) >= 2*time.Minute {
				delete(s.publicSSO, candidate)
			}
		}
		s.publicSweep = now
	}
	if _, exists := s.publicSSO[key]; !exists && len(s.publicSSO) >= 4096 {
		s.mu.Unlock()
		release()
		return nil, false
	}
	window := s.publicSSO[key]
	if window.Started.IsZero() || now.Sub(window.Started) >= time.Minute {
		window = ssoAttemptWindow{Started: now}
	}
	if window.Count >= 30 {
		s.mu.Unlock()
		release()
		return nil, false
	}
	window.Count++
	s.publicSSO[key] = window
	s.mu.Unlock()
	return release, true
}

type SAMLResponse struct {
	UserID       string            `json:"user_id"`
	Email        string            `json:"email"`
	Name         string            `json:"name"`
	NameKo       string            `json:"name_ko"`
	Attributes   map[string]string `json:"attributes"`
	Issuer       string            `json:"issuer"`
	NotOnOrAfter time.Time         `json:"not_on_or_after"`
}

type OIDCTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type OIDCUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Locale        string `json:"locale,omitempty"`
}

func parseOIDCJWKS(jwksJSON []byte) (map[string][]crypto.PublicKey, error) {
	var jwks struct {
		Keys []struct {
			Kid    string   `json:"kid"`
			Kty    string   `json:"kty"`
			Crv    string   `json:"crv"`
			X      string   `json:"x"`
			Y      string   `json:"y"`
			N      string   `json:"n"`
			E      string   `json:"e"`
			Use    string   `json:"use"`
			Alg    string   `json:"alg"`
			KeyOps []string `json:"key_ops"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(jwksJSON, &jwks); err != nil {
		return nil, fmt.Errorf("sso: decode JWKS: %w", err)
	}
	parsed := map[string][]crypto.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if len(k.KeyOps) > 0 && !slices.Contains(k.KeyOps, "verify") {
			continue
		}
		switch k.Kty {
		case "EC":
			if k.Crv == "P-256" && (k.Alg == "" || k.Alg == "ES256") {
				x, xerr := base64.RawURLEncoding.DecodeString(k.X)
				if xerr != nil || len(x) != 32 {
					continue
				}
				xInt := new(big.Int).SetBytes(x)
				var candidates []*ecdsa.PublicKey
				if y, yerr := base64.RawURLEncoding.DecodeString(k.Y); yerr == nil && len(y) == 32 {
					// Full (x, y) published — the normal case.
					yInt := new(big.Int).SetBytes(y)
					if elliptic.P256().IsOnCurve(xInt, yInt) {
						candidates = append(candidates, &ecdsa.PublicKey{Curve: elliptic.P256(), X: xInt, Y: yInt})
					}
				} else {
					// x-only: BOTH y roots are valid candidates (parity
					// is not carried); each is on-curve by construction.
					if yInt, ok := decompressP256(xInt); ok {
						candidates = append(candidates, &ecdsa.PublicKey{Curve: elliptic.P256(), X: xInt, Y: yInt})
						pMinusY := new(big.Int).Sub(elliptic.P256().Params().P, yInt)
						candidates = append(candidates, &ecdsa.PublicKey{Curve: elliptic.P256(), X: xInt, Y: pMinusY})
					}
				}
				if len(candidates) == 0 {
					continue
				}
				kid := k.Kid
				if kid == "" {
					kid = "default"
				}
				for _, pub := range candidates {
					parsed[kid] = append(parsed[kid], pub)
				}
			}
		case "RSA":
			if k.Alg != "" && k.Alg != "RS256" {
				continue
			}
			n, errN := base64.RawURLEncoding.DecodeString(k.N)
			e, errE := base64.RawURLEncoding.DecodeString(k.E)
			if errN != nil || errE != nil || len(e) < 1 || len(e) > 4 {
				continue
			}
			exp := 0
			for _, b := range e {
				exp = exp<<8 | int(b)
			}
			pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}
			kid := k.Kid
			if kid == "" {
				kid = "default"
			}
			parsed[kid] = append(parsed[kid], pub)
		}
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("sso: JWKS carries no usable keys")
	}
	return parsed, nil
}

// decompressP256 recovers the even-parity y for x on P-256.
func decompressP256(x *big.Int) (*big.Int, bool) {
	// y² = x³ - 3x + b (mod p)
	p := elliptic.P256().Params().P
	b := elliptic.P256().Params().B
	rhs := new(big.Int).Exp(x, big.NewInt(3), p)
	rhs.Sub(rhs, new(big.Int).Mul(big.NewInt(3), x))
	rhs.Add(rhs, b)
	rhs.Mod(rhs, p)
	// p ≡ 3 (mod 4) for P-256 → y = rhs^((p+1)/4).
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Div(exp, big.NewInt(4))
	y := new(big.Int).Exp(rhs, exp, p)
	// Verify y² == rhs.
	check := new(big.Int).Mul(y, y)
	check.Mod(check, p)
	if check.Cmp(rhs) != 0 {
		return nil, false
	}
	return y, true
}

func parseOIDCIDToken(idToken, issuer, clientID, expectedNonce string, jwks map[string][]crypto.PublicKey) (*OIDCUserInfo, error) {
	if len(jwks) == 0 {
		return nil, fmt.Errorf("sso: no OIDC JWKS provisioned (refusing to trust an unverified ID token)")
	}
	// Verify by trying each JWKS candidate (x-only EC keys carry both
	// y roots; the signature discriminates them).
	popts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"ES256", "RS256"}),
		jwt.WithExpirationRequired(),
	}
	if issuer != "" {
		popts = append(popts, jwt.WithIssuer(issuer))
	}
	if clientID != "" {
		popts = append(popts, jwt.WithAudience(clientID))
	}
	kidHint := ""
	if parts := strings.Split(idToken, "."); len(parts) == 3 {
		var hdr struct {
			Kid string `json:"kid"`
		}
		if raw, derr := base64.RawURLEncoding.DecodeString(parts[0]); derr == nil {
			_ = json.Unmarshal(raw, &hdr)
			kidHint = hdr.Kid
		}
	}
	// Kid resolution: a PRESENT kid must exist in the JWKS (unknown
	// kid = reject). Only an absent kid falls back to the default
	// slot; the full set is never tried when a kid was named.
	var candidates []crypto.PublicKey
	if kidHint != "" {
		candidates = jwks[kidHint]
	} else {
		candidates = jwks["default"]
		if len(candidates) == 0 {
			for _, ks := range jwks {
				candidates = append(candidates, ks...)
			}
		}
	}
	var token *jwt.Token
	var err error
	for _, key := range candidates {
		k := key
		p := jwt.NewParser(popts...)
		token, err = p.Parse(idToken, func(*jwt.Token) (interface{}, error) { return k, nil })
		if err == nil && token != nil && token.Valid {
			break
		}
	}
	if err != nil || token == nil || !token.Valid {
		return nil, fmt.Errorf("sso: ID token verification failed: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("sso: ID token claims malformed")
	}
	if expectedNonce != "" && getStringClaim(claims, "nonce") != expectedNonce {
		return nil, fmt.Errorf("sso: ID token nonce mismatch")
	}
	if clientID != "" {
		audiences, audienceErr := claims.GetAudience()
		if audienceErr != nil || len(audiences) == 0 {
			return nil, fmt.Errorf("sso: ID token audience is invalid")
		}
		authorizedParty := getStringClaim(claims, "azp")
		if (len(audiences) > 1 && authorizedParty != clientID) || (authorizedParty != "" && authorizedParty != clientID) {
			return nil, fmt.Errorf("sso: ID token authorized party mismatch")
		}
	}
	issuedAt, issuedAtErr := claims.GetIssuedAt()
	if issuedAtErr != nil || issuedAt == nil || issuedAt.Time.After(time.Now().UTC().Add(2*time.Minute)) {
		return nil, fmt.Errorf("sso: ID token issued-at time is invalid")
	}
	info := &OIDCUserInfo{
		Sub:           getStringClaim(claims, "sub"),
		Email:         getStringClaim(claims, "email"),
		EmailVerified: claims["email_verified"] == true,
		Name:          getStringClaim(claims, "name"),
		Locale:        getStringClaim(claims, "locale"),
	}
	if info.Sub == "" {
		return nil, fmt.Errorf("sso: ID token carries no subject")
	}
	if expectedNonce != "" {
		verified, ok := claims["email_verified"].(bool)
		if info.Email == "" || !ok || !verified {
			return nil, fmt.Errorf("sso: ID token carries no verified email")
		}
	}
	if info.Locale == "" {
		info.Locale = "ko-KR"
	}
	return info, nil
}

func (s *Service) ProvisionUserFromSSO(orgID, issuer string, saml *SAMLResponse) (*models.User, error) {
	external := identity.NormalizeExternalIdentity(issuer, saml.UserID)
	issuer, saml.UserID = external.Issuer, external.Subject
	if issuer == "" || saml.UserID == "" {
		return nil, fmt.Errorf("sso: SAML identity carries no immutable issuer and subject")
	}
	user, err := s.findExternalUser(orgID, "saml", issuer, saml.UserID)
	if err == gorm.ErrRecordNotFound {
		created := models.User{
			AuditBase:              models.AuditBase{OrganizationID: orgID},
			Email:                  saml.Email,
			Name:                   saml.Name,
			NameKo:                 saml.NameKo,
			Status:                 "active",
			AuthMethod:             "saml",
			ExternalID:             saml.UserID,
			ExternalIssuer:         issuer,
			ExternalIssuerVerified: true,
			Locale:                 "ko-KR",
			Timezone:               "Asia/Seoul",
		}
		var createdNow bool
		var concurrent *models.User
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			var admitErr error
			concurrent, createdNow, admitErr = identity.AdmitExternalUserWithDB(tx, &created, true)
			return admitErr
		}); err != nil {
			return nil, fmt.Errorf("sso: create user: %w", err)
		}
		if concurrent != nil {
			user, err = concurrent, nil
		} else if createdNow {
			return &created, nil
		} else {
			user, err = s.findExternalUser(orgID, "saml", issuer, saml.UserID)
			if err != nil {
				return nil, fmt.Errorf("sso: external identity conflicts with an existing account")
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("sso: lookup user: %w", err)
	}
	// Lifecycle enforcement: only active users may obtain a console
	// session (canonical state machine — suspended/offboarded may not).
	if user.Status != "active" {
		return nil, fmt.Errorf("sso: account is %s", user.Status)
	}
	updates := map[string]interface{}{}
	if saml.Name != "" {
		updates["name"] = saml.Name
	}
	if saml.NameKo != "" {
		updates["name_ko"] = saml.NameKo
	}
	if saml.Email != "" {
		updates["email"] = saml.Email
	}
	now := time.Now().Format(time.RFC3339)
	updates["last_login_at"] = now
	if err := s.updateActiveSSOProfile(user, updates); err != nil {
		return nil, err
	}
	return user, nil
}

// updateActiveSSOProfile updates only IdP-owned profile columns and keeps the
// active-state predicate in the write itself. A lifecycle transition that wins
// after the initial lookup therefore cannot be overwritten by stale user data.
func (s *Service) updateActiveSSOProfile(user *models.User, updates map[string]interface{}) error {
	return s.updateLifecycleBoundProfile(user, updates, true)
}

// updateLifecycleBoundProfile keeps an IdP-owned email change and the console
// credential keyed by that email in one lifecycle-locked transaction. Without
// this, an old credential could become detached from the managed user and
// survive suspension/offboarding as an apparently standalone operator.
func (s *Service) updateLifecycleBoundProfile(user *models.User, updates map[string]interface{}, activeOnly bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var locked *models.User
		var err error
		if activeOnly {
			locked, err = identity.LockActiveUser(tx, user.OrganizationID, user.ID)
		} else {
			locked, err = identity.LockMutableUser(tx, user.OrganizationID, user.ID)
		}
		if err != nil {
			return fmt.Errorf("sso: account state changed during profile update: %w", err)
		}
		if nextEmail, ok := updates["email"].(string); ok && nextEmail != "" &&
			!strings.EqualFold(strings.TrimSpace(locked.Email), strings.TrimSpace(nextEmail)) {
			nextEmail = identity.NormalizeEmail(nextEmail)
			updates["email"] = nextEmail
			if err := tx.Model(&identity.AdminCredentials{}).
				Where("organization_id = ? AND user_id = ?", user.OrganizationID, user.ID).
				Update("email", nextEmail).Error; err != nil {
				return fmt.Errorf("sso: update linked console identity: %w", err)
			}
		}
		res := tx.Model(&models.User{}).
			Where("id = ? AND organization_id = ? AND status = ?", user.ID, user.OrganizationID, locked.Status).
			Updates(updates)
		if res.Error != nil {
			return fmt.Errorf("sso: update lifecycle-bound profile: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("sso: account state changed during profile update")
		}
		if err := tx.Where("id = ? AND organization_id = ?", user.ID, user.OrganizationID).First(user).Error; err != nil {
			return fmt.Errorf("sso: reload updated user: %w", err)
		}
		if activeOnly && user.Status != models.UserStatusActive {
			return fmt.Errorf("sso: account is %s", user.Status)
		}
		return nil
	})
}

type SCIMUser struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	UserName    string   `json:"userName"`
	ExternalID  string   `json:"externalId"`
	Email       string   `json:"email"`
	Active      *bool    `json:"active,omitempty"`
	DisplayName string   `json:"displayName"`
}

func (s *Service) HandleSCIMRequest(w http.ResponseWriter, r *http.Request) {
	// SCIM is an ADMIN surface: a configured bearer token is REQUIRED
	// (fail closed when unset — the handler is never open).
	s.mu.RLock()
	hasTokens := len(s.scimTokens) > 0
	s.mu.RUnlock()
	if !hasTokens {
		http.Error(w, "SCIM not configured", http.StatusServiceUnavailable)
		return
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	tokenDigest := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
	s.mu.RLock()
	orgID, configured := s.scimTokens[tokenDigest]
	s.mu.RUnlock()
	if !configured {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// The credential determines the tenant. A caller-supplied org can only
	// narrow/assert that binding; it can never select another tenant.
	if requestedOrg := strings.TrimSpace(r.URL.Query().Get("org")); requestedOrg != "" && requestedOrg != orgID {
		http.Error(w, "SCIM credential is not authorized for requested organization", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		var requestUser SCIMUser
		if err := json.NewDecoder(r.Body).Decode(&requestUser); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		provisioned, err := s.provisionSCIMUser(orgID, &requestUser)
		if err != nil {
			http.Error(w, err.Error(), scimLifecycleHTTPStatus(err))
			return
		}
		if requestUser.Active != nil {
			to := models.UserStatusActive
			reason := "SCIM activation"
			if !*requestUser.Active {
				to = models.UserStatusOffboarded
				reason = "SCIM deprovisioning"
			}
			if provisioned.Status != to {
				result, transitionErr := identity.TransitionUserLifecycle(s.db, identity.UserLifecycleMutation{
					OrganizationID:   orgID,
					UserID:           provisioned.ID,
					To:               to,
					Reason:           reason,
					ActorID:          "scim",
					ActorType:        "system",
					Idempotent:       true,
					SessionLifecycle: s.lifecycle,
				})
				if transitionErr != nil {
					if result != nil && errors.Is(transitionErr, identity.ErrLifecycleCleanup) {
						provisioned = &result.User
					} else {
						status := http.StatusConflict
						if errors.Is(transitionErr, identity.ErrLifecycleUserNotFound) {
							status = http.StatusNotFound
						}
						http.Error(w, transitionErr.Error(), status)
						return
					}
				} else {
					provisioned = &result.User
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": provisioned.ID, "email": provisioned.Email, "active": provisioned.Status == models.UserStatusActive})
	case http.MethodDelete:
		userID := chi.URLParam(r, "userID")
		if userID == "" {
			userID = r.URL.Query().Get("userID")
		}
		if userID == "" {
			// Public /scim/v2 mount: /Users/{id} is the trailing segment.
			segs := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(segs) >= 2 && segs[len(segs)-2] == "Users" {
				userID = segs[len(segs)-1]
			}
		}
		if userID == "" {
			http.Error(w, "userID required", http.StatusBadRequest)
			return
		}
		result, err := identity.TransitionUserLifecycle(s.db, identity.UserLifecycleMutation{
			OrganizationID:   orgID,
			UserID:           userID,
			To:               models.UserStatusOffboarded,
			Reason:           "SCIM deprovisioning",
			ActorID:          "scim",
			ActorType:        "system",
			Idempotent:       true,
			SessionLifecycle: s.lifecycle,
		})
		if err != nil {
			if result == nil || !errors.Is(err, identity.ErrLifecycleCleanup) {
				http.Error(w, err.Error(), scimLifecycleHTTPStatus(err))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": result.User.Status, "remaining_access": result.RemainingAccess,
			"cleanup_failures": result.SessionCleanupFailures,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) provisionSCIMUser(orgID string, scimUser *SCIMUser) (*models.User, error) {
	externalID := scimUser.ExternalID
	if externalID == "" {
		externalID = scimUser.UserName
	}
	if externalID == "" {
		return nil, fmt.Errorf("sso: SCIM userName or externalId is required")
	}
	user, err := s.findExternalUser(orgID, "scim", "scim", externalID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		initialStatus := models.UserStatusActive
		if scimUser.Active != nil && !*scimUser.Active {
			initialStatus = models.UserStatusOffboarded
		}
		created := models.User{
			AuditBase:              models.AuditBase{OrganizationID: orgID},
			Email:                  identity.NormalizeEmail(scimUser.Email),
			Name:                   scimUser.DisplayName,
			Status:                 initialStatus,
			AuthMethod:             "scim",
			ExternalID:             externalID,
			ExternalIssuer:         "scim",
			ExternalIssuerVerified: true,
			Locale:                 "ko-KR",
			Timezone:               "Asia/Seoul",
		}
		err := s.db.Transaction(func(tx *gorm.DB) error {
			admitted, _, err := identity.AdmitExternalUserWithDB(tx, &created, false)
			if err != nil {
				return err
			}
			created = *admitted
			if initialStatus != models.UserStatusOffboarded {
				return nil
			}
			today := time.Now().UTC().Format("2006-01-02")
			created.OffboardingDate = &today
			if err := tx.Model(&models.User{}).Where("id = ? AND organization_id = ?", created.ID, orgID).
				Update("offboarding_date", today).Error; err != nil {
				return err
			}
			details, _ := json.Marshal(map[string]interface{}{"from": "provisioning", "to": models.UserStatusOffboarded, "reason": "SCIM deprovisioning"})
			return tx.Create(&models.AuditEvent{
				OrganizationID: orgID, EventType: "cp.user.offboarded", ActorID: "scim", ActorType: "system",
				Action: "offboard_user", ResourceType: "user", ResourceID: created.ID,
				Details: string(details), Result: "success", OccurredAt: time.Now().UTC().Format(time.RFC3339),
			}).Error
		})
		if err != nil {
			return nil, fmt.Errorf("sso: create SCIM user: %w", err)
		}
		return &created, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sso: lookup SCIM user: %w", err)
	}
	if user.Status == models.UserStatusOffboarded {
		if scimUser.Active != nil && !*scimUser.Active {
			return user, nil
		}
		return nil, fmt.Errorf("sso: offboarded SCIM account is terminal")
	}
	updates := map[string]interface{}{"auth_method": "scim", "external_issuer_verified": true}
	if scimUser.Email != "" {
		updates["email"] = identity.NormalizeEmail(scimUser.Email)
	}
	if scimUser.DisplayName != "" {
		updates["name"] = scimUser.DisplayName
	}
	if err := s.updateLifecycleBoundProfile(user, updates, false); err != nil {
		return nil, err
	}
	return user, nil
}

// ConfigureSCIMTokenForOrganization installs one tenant-bound provisioning
// credential. Only its digest is retained in memory.
func (s *Service) ConfigureSCIMTokenForOrganization(orgID, token string) {
	orgID, token = strings.TrimSpace(orgID), strings.TrimSpace(token)
	if orgID == "" || token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scimTokens[sha256.Sum256([]byte(token))] = orgID
}

// ConfigureSCIMTokensJSON accepts a JSON object of organization IDs to bearer
// tokens. It is intended for production secret injection at process startup.
func (s *Service) ConfigureSCIMTokensJSON(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var configured map[string]string
	if err := json.Unmarshal([]byte(raw), &configured); err != nil {
		return fmt.Errorf("sso: invalid tenant-bound SCIM token configuration: %w", err)
	}
	for orgID, token := range configured {
		s.ConfigureSCIMTokenForOrganization(orgID, token)
	}
	return nil
}

func scimLifecycleHTTPStatus(err error) int {
	switch {
	case errors.Is(err, identity.ErrLifecycleUserNotFound):
		return http.StatusNotFound
	case errors.Is(err, identity.ErrLifecycleInvalid), errors.Is(err, identity.ErrLifecycleStateChanged),
		errors.Is(err, identity.ErrLifecycleLastAdmin), errors.Is(err, identity.ErrLifecycleAccessRemain),
		errors.Is(err, identity.ErrUserReadOnly), errors.Is(err, identity.ErrUserSeatLimit):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func getStringClaim(claims jwt.MapClaims, key string) string {
	if v, ok := claims[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// candidateKeySets flattens the JWKS into per-key candidate sets: one
// set per (kid-slot) with each individual key, so x-only EC keys with
// two y roots are each tried.

// ProvisionOIDCUser finds or creates the console user for a verified
// OIDC identity (mirrors ProvisionUserFromSSO for the OIDC flow).
func (s *Service) ProvisionOIDCUser(orgID, issuer string, info *OIDCUserInfo) (*models.User, error) {
	external := identity.NormalizeExternalIdentity(issuer, info.Sub)
	issuer, externalID := external.Issuer, external.Subject
	if externalID == "" || issuer == "" {
		return nil, fmt.Errorf("sso: oidc identity carries no immutable subject")
	}
	user, err := s.findExternalUser(orgID, "oidc", issuer, externalID)
	if err == gorm.ErrRecordNotFound {
		user = nil
		// A previously resolved OIDC identity may point at a SCIM-owned user,
		// whose canonical provisioning identity remains auth_method=scim.
		_, linkedUser, linkErr := identity.ResolveLinkedSourceIdentity(s.db, orgID, external)
		if linkErr == nil {
			user = linkedUser
		}
		if linkErr != nil && !errors.Is(linkErr, identity.ErrExternalIdentityUnlinked) {
			return nil, fmt.Errorf("sso: external identity requires administrator resolution: %w", linkErr)
		}

		if user == nil && errors.Is(linkErr, identity.ErrExternalIdentityUnlinked) {
			var enrolled models.User
			qerr := s.db.Select("id").Where("organization_id = ? AND auth_method = ?", orgID, "scim").Limit(1).Take(&enrolled).Error
			if qerr != nil && !errors.Is(qerr, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("sso: inspect SCIM enrollment: %w", qerr)
			}
			if qerr == nil {
				resolution := models.SSOIdentityLink{
					OrganizationID: orgID, LegacyIssuer: issuer, LegacySubject: externalID,
					Status:         models.SSOLinkStatusUnlinked,
					ResolutionNote: "SCIM-managed organization requires an explicit issuer+subject identity link",
				}
				createErr := s.db.Transaction(func(tx *gorm.DB) error {
					var organization models.Organization
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&organization, "id = ?", orgID).Error; err != nil {
						return err
					}
					return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&resolution).Error
				})
				if createErr != nil {
					return nil, fmt.Errorf("sso: record identity resolution: %w", createErr)
				}
				return nil, fmt.Errorf("sso: external identity requires administrator resolution")
			}
		}

		if user != nil {
			err = nil
		} else {
			created := models.User{
				AuditBase:              models.AuditBase{OrganizationID: orgID},
				Email:                  info.Email,
				Name:                   info.Name,
				Status:                 "active",
				AuthMethod:             "oidc",
				ExternalID:             externalID,
				ExternalIssuer:         issuer,
				ExternalIssuerVerified: true,
				Locale:                 "ko-KR",
				Timezone:               "Asia/Seoul",
			}
			var createdNow bool
			var concurrent *models.User
			if err := s.db.Transaction(func(tx *gorm.DB) error {
				var admitErr error
				concurrent, createdNow, admitErr = identity.AdmitExternalUserWithDB(tx, &created, true)
				return admitErr
			}); err != nil {
				return nil, fmt.Errorf("sso: create user: %w", err)
			}
			if concurrent != nil {
				user, err = concurrent, nil
			} else if createdNow {
				return &created, nil
			} else {
				user, err = s.findExternalUser(orgID, "oidc", issuer, externalID)
				if err != nil {
					return nil, fmt.Errorf("sso: external identity conflicts with an existing account")
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("sso: lookup user: %w", err)
	}
	// Lifecycle enforcement: only active users may obtain a console
	// session (canonical state machine — suspended/offboarded may not).
	if user.Status != "active" {
		return nil, fmt.Errorf("sso: account is %s", user.Status)
	}
	updates := map[string]interface{}{}
	if info.Name != "" {
		updates["name"] = info.Name
	}
	if info.Email != "" {
		updates["email"] = info.Email
	}
	updates["last_login_at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.updateActiveSSOProfile(user, updates); err != nil {
		return nil, err
	}
	return user, nil
}

// findExternalUser resolves a subject only in its verified provider namespace.
// Issuer-less legacy rows are migrated from the organization's authoritative
// configuration; a runtime login may never claim them using a new issuer.
func (s *Service) findExternalUser(orgID, authMethod, issuer, externalID string) (*models.User, error) {
	return identity.FindUserByExternalIdentity(s.db, orgID, authMethod, identity.NormalizeExternalIdentity(issuer, externalID))
}
