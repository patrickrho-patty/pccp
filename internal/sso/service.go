package sso

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements SAML 2.0 and OIDC SSO integration (PRD 8.2, 32.1).
type Service struct {
	mu           sync.RWMutex
	oidcJWKS     map[string][]crypto.PublicKey
	db           *gorm.DB
	jwtSecret    []byte
	samlIDPID    string
	samlIDPURL   string
	samlSPURL    string
	oidcIssuer   string
	oidcClientID string
	oidcSecret   string
	scimToken    string
}

func New(db *gorm.DB, jwtSecret string) *Service {
	return &Service{db: db, jwtSecret: []byte(jwtSecret)}
}

func (s *Service) ConfigureSAML(idpEntityID, idpSSOURL, spURL string) {
	s.samlIDPID = idpEntityID
	s.samlIDPURL = idpSSOURL
	s.samlSPURL = spURL
}

func (s *Service) ConfigureOIDC(issuer, clientID, clientSecret string) {
	s.oidcIssuer = issuer
	s.oidcClientID = clientID
	s.oidcSecret = clientSecret
}

func (s *Service) GenerateSAMLRedirect(relayState string) (string, error) {
	if s.samlIDPURL == "" {
		return "", fmt.Errorf("sso: SAML not configured")
	}
	authReq := fmt.Sprintf(`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="%s" Version="2.0" IssueInstant="%s" Destination="%s"><saml:Issuer xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">%s</saml:Issuer></samlp:AuthnRequest>`,
		generateID(), time.Now().UTC().Format(time.RFC3339), s.samlIDPURL, s.samlSPURL)
	encoded := base64.StdEncoding.EncodeToString([]byte(authReq))
	params := url.Values{}
	params.Set("SAMLRequest", encoded)
	if relayState != "" {
		params.Set("RelayState", relayState)
	}
	return s.samlIDPURL + "?" + params.Encode(), nil
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

func (s *Service) HandleSAMLCallback(samlResponse string, relayState string) (*SAMLResponse, error) {
	data, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("sso: decode SAML response: %w", err)
	}

	var resp struct {
		XMLName    xml.Name `xml:"Response"`
		Assertions []struct {
			Subject struct {
				NameID string `xml:"NameID"`
			} `xml:"Subject"`
			Attributes []struct {
				Name  string `xml:"Name,attr"`
				Value string `xml:"AttributeValue"`
			} `xml:"AttributeStatement>Attribute"`
		} `xml:"Assertion"`
	}

	if err := xml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("sso: malformed SAML response: %w", err)
	}

	if len(resp.Assertions) == 0 || resp.Assertions[0].Subject.NameID == "" {
		return nil, fmt.Errorf("sso: SAML response carries no authenticated subject")
	}

	result := &SAMLResponse{Attributes: make(map[string]string)}
	result.UserID = resp.Assertions[0].Subject.NameID
	for _, attr := range resp.Assertions[0].Attributes {
		result.Attributes[attr.Name] = attr.Value
		switch attr.Name {
		case "email", "EmailAddress", "mail":
			result.Email = attr.Value
		case "name", "DisplayName", "cn":
			result.Name = attr.Value
		case "nameKo", "koreanName":
			result.NameKo = attr.Value
		}
	}
	return result, nil
}

func (s *Service) OIDCAuthURL(redirectURI, state string) (string, error) {
	if s.oidcIssuer == "" {
		return "", fmt.Errorf("sso: OIDC not configured")
	}
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", s.oidcClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "openid profile email")
	params.Set("state", state)
	return s.oidcIssuer + "/authorize?" + params.Encode(), nil
}

type OIDCTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func (s *Service) HandleOIDCCallback(code, redirectURI string) (*OIDCTokenResponse, error) {
	if s.oidcIssuer == "" {
		return nil, fmt.Errorf("sso: OIDC not configured")
	}
	// Real code exchange against the configured IdP token endpoint.
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", s.oidcClientID)
	form.Set("client_secret", s.oidcSecret)
	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(s.oidcIssuer, "/")+"/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("sso: token request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sso: token endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sso: token endpoint rejected the code (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var out OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("sso: decode token response: %w", err)
	}
	if out.IDToken == "" {
		return nil, fmt.Errorf("sso: token response carries no id_token")
	}
	return &out, nil
}

type OIDCUserInfo struct {
	Sub    string `json:"sub"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Locale string `json:"locale,omitempty"`
}

// SetOIDCJWKS installs the IdP's JWKS (discovered from the issuer's
// well-known endpoint or provisioned offline for sovereign deploys).
func (s *Service) SetOIDCJWKS(jwksJSON []byte) error {
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(jwksJSON, &jwks); err != nil {
		return fmt.Errorf("sso: decode JWKS: %w", err)
	}
	parsed := map[string][]crypto.PublicKey{}
	for _, k := range jwks.Keys {
		switch k.Kty {
		case "EC":
			if k.Crv == "P-256" {
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
		return fmt.Errorf("sso: JWKS carries no usable keys")
	}
	s.mu.Lock()
	s.oidcJWKS = parsed
	s.mu.Unlock()
	return nil
}

// DiscoverOIDCJWKS fetches the issuer's JWKS from the well-known
// endpoint (sovereign deployments provision it offline instead).
func (s *Service) DiscoverOIDCJWKS(ctx context.Context) error {
	if s.oidcIssuer == "" {
		return fmt.Errorf("sso: OIDC not configured")
	}
	wellKnown := strings.TrimSuffix(s.oidcIssuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sso: discover issuer: %w", err)
	}
	defer resp.Body.Close()
	var cfg struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil || cfg.JWKSURI == "" {
		return fmt.Errorf("sso: issuer discovery missing jwks_uri")
	}
	jwksResp, err := http.Get(cfg.JWKSURI)
	if err != nil {
		return fmt.Errorf("sso: fetch JWKS: %w", err)
	}
	defer jwksResp.Body.Close()
	jwksBytes, err := io.ReadAll(io.LimitReader(jwksResp.Body, 1<<20))
	if err != nil {
		return err
	}
	return s.SetOIDCJWKS(jwksBytes)
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

// ParseOIDCIDToken verifies the ID token signature against the
// provisioned JWKS, the issuer, and the expiry — ParseUnverified is
// never used for authentication.
func (s *Service) ParseOIDCIDToken(idToken string) (*OIDCUserInfo, error) {
	s.mu.RLock()
	jwks := s.oidcJWKS
	s.mu.RUnlock()
	if len(jwks) == 0 {
		return nil, fmt.Errorf("sso: no OIDC JWKS provisioned (refusing to trust an unverified ID token)")
	}
	// Verify by trying each JWKS candidate (x-only EC keys carry both
	// y roots; the signature discriminates them).
	popts := []jwt.ParserOption{jwt.WithValidMethods([]string{"ES256", "RS256"})}
	if s.oidcIssuer != "" {
		popts = append(popts, jwt.WithIssuer(s.oidcIssuer))
	}
	if s.oidcClientID != "" {
		popts = append(popts, jwt.WithAudience(s.oidcClientID))
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
	info := &OIDCUserInfo{
		Sub:    getStringClaim(claims, "sub"),
		Email:  getStringClaim(claims, "email"),
		Name:   getStringClaim(claims, "name"),
		Locale: getStringClaim(claims, "locale"),
	}
	if info.Sub == "" {
		return nil, fmt.Errorf("sso: ID token carries no subject")
	}
	if info.Locale == "" {
		info.Locale = "ko-KR"
	}
	return info, nil
}

func (s *Service) ProvisionUserFromSSO(orgID string, saml *SAMLResponse) (*models.User, error) {
	var user models.User
	err := s.db.Where("organization_id = ? AND external_id = ?", orgID, saml.UserID).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		user = models.User{
			AuditBase:  models.AuditBase{OrganizationID: orgID},
			Email:      saml.Email,
			Name:       saml.Name,
			NameKo:     saml.NameKo,
			Status:     "active",
			AuthMethod: "saml",
			ExternalID: saml.UserID,
			Locale:     "ko-KR",
			Timezone:   "Asia/Seoul",
		}
		if err := s.db.Create(&user).Error; err != nil {
			return nil, fmt.Errorf("sso: create user: %w", err)
		}
		return &user, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sso: lookup user: %w", err)
	}
	if saml.Name != "" {
		user.Name = saml.Name
	}
	if saml.NameKo != "" {
		user.NameKo = saml.NameKo
	}
	if saml.Email != "" {
		user.Email = saml.Email
	}
	now := time.Now().Format(time.RFC3339)
	user.LastLoginAt = &now
	s.db.Save(&user)
	return &user, nil
}

func (s *Service) GenerateSessionToken(userID, orgID, role string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID, "org_id": orgID, "role": role,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(), "iss": "pccp-sso",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

type SCIMUser struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	UserName    string   `json:"userName"`
	ExternalID  string   `json:"externalId"`
	Email       string   `json:"email"`
	Active      bool     `json:"active"`
	DisplayName string   `json:"displayName"`
}

func (s *Service) HandleSCIMRequest(w http.ResponseWriter, r *http.Request) {
	// SCIM is an ADMIN surface: a configured bearer token is REQUIRED
	// (fail closed when unset — the handler is never open).
	if s.scimToken == "" {
		http.Error(w, "SCIM not configured", http.StatusServiceUnavailable)
		return
	}
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+s.scimToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Org scope is mandatory — unscoped provisioning is refused.
	orgID := r.URL.Query().Get("org")
	if orgID == "" {
		http.Error(w, "org scope required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		var user SCIMUser
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		created, err := s.ProvisionUserFromSSO(orgID, &SAMLResponse{
			UserID: user.UserName,
			Email:  user.Email,
			Name:   user.DisplayName,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": created.ID, "email": created.Email})
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
		// Deletes are scoped to the caller's org.
		var user models.User
		if err := s.db.Where("id = ? AND organization_id = ?", userID, orgID).First(&user).Error; err != nil {
			http.Error(w, "user not found in org", http.StatusNotFound)
			return
		}
		if err := s.db.Model(&user).Update("status", "offboarded").Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "offboarded"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ConfigureSCIMToken sets the SCIM admin bearer token.
func (s *Service) ConfigureSCIMToken(token string) { s.scimToken = token }

func getStringClaim(claims jwt.MapClaims, key string) string {
	if v, ok := claims[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

var _ = ed25519.PublicKeySize

// candidateKeySets flattens the JWKS into per-key candidate sets: one
// set per (kid-slot) with each individual key, so x-only EC keys with
// two y roots are each tried.

// ProvisionOIDCUser finds or creates the console user for a verified
// OIDC identity (mirrors ProvisionUserFromSSO for the OIDC flow).
func (s *Service) ProvisionOIDCUser(orgID string, info *OIDCUserInfo) (*models.User, error) {
	externalID := info.Sub
	if externalID == "" {
		externalID = info.Email
	}
	if externalID == "" {
		return nil, fmt.Errorf("sso: oidc identity carries no sub or email")
	}
	var user models.User
	err := s.db.Where("organization_id = ? AND external_id = ?", orgID, externalID).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		user = models.User{
			AuditBase:  models.AuditBase{OrganizationID: orgID},
			Email:      info.Email,
			Name:       info.Name,
			Status:     "active",
			AuthMethod: "oidc",
			ExternalID: externalID,
			Locale:     "ko-KR",
			Timezone:   "Asia/Seoul",
		}
		if cerr := s.db.Create(&user).Error; cerr != nil {
			return nil, fmt.Errorf("sso: create user: %w", cerr)
		}
		return &user, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sso: lookup user: %w", err)
	}
	if info.Name != "" {
		user.Name = info.Name
	}
	if info.Email != "" {
		user.Email = info.Email
	}
	s.db.Save(&user)
	return &user, nil
}
