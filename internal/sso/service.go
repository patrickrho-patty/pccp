package sso

import (
	"context"
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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements SAML 2.0 and OIDC SSO integration (PRD 8.2, 32.1).
type Service struct {
	oidcJWKS     map[string]interface{}
	db           *gorm.DB
	jwtSecret    []byte
	samlIDPID    string
	samlIDPURL   string
	samlSPURL    string
	oidcIssuer   string
	oidcClientID string
	oidcSecret   string
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
	UserID       string            `json:"user_i_d"`
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
	s.oidcJWKS = map[string]interface{}{}
	for _, k := range jwks.Keys {
		switch k.Kty {
		case "EC":
			if k.Crv == "P-256" {
				x, xerr := base64.RawURLEncoding.DecodeString(k.X)
				y, yerr := base64.RawURLEncoding.DecodeString(k.Y)
				if xerr != nil || len(x) != 32 {
					continue
				}
				xInt := new(big.Int).SetBytes(x)
				var yInt *big.Int
				if yerr == nil && len(y) == 32 {
					yInt = new(big.Int).SetBytes(y)
				} else {
					// Recover y from the curve equation (both
					// candidates; verify the point is on the curve).
					yy, ok := decompressP256(xInt)
					if !ok {
						continue
					}
					yInt = yy
				}
				if !elliptic.P256().IsOnCurve(xInt, yInt) {
					continue
				}
				pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: xInt, Y: yInt}
				if k.Kid != "" {
					s.oidcJWKS[k.Kid] = pub
				} else {
					s.oidcJWKS["default"] = pub
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
			if k.Kid != "" {
				s.oidcJWKS[k.Kid] = pub
			} else {
				s.oidcJWKS["default"] = pub
			}
		}
	}
	if len(s.oidcJWKS) == 0 {
		return fmt.Errorf("sso: JWKS carries no usable keys")
	}
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
	if len(s.oidcJWKS) == 0 {
		return nil, fmt.Errorf("sso: no OIDC JWKS provisioned (refusing to trust an unverified ID token)")
	}
	keyfunc := func(t *jwt.Token) (interface{}, error) {
		switch t.Method.Alg() {
		case "ES256", "RS256":
		default:
			return nil, fmt.Errorf("sso: unsupported ID token algorithm %q", t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		if kid != "" {
			if key, ok := s.oidcJWKS[kid]; ok {
				return key, nil
			}
		}
		if key, ok := s.oidcJWKS["default"]; ok {
			return key, nil
		}
		return nil, fmt.Errorf("sso: ID token kid not in JWKS")
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"ES256", "RS256"}))
	if s.oidcIssuer != "" {
		parser = jwt.NewParser(jwt.WithValidMethods([]string{"ES256", "RS256"}), jwt.WithIssuer(s.oidcIssuer))
	}
	token, err := parser.Parse(idToken, keyfunc)
	if err != nil || !token.Valid {
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
	Active      bool     `json:"active"`
	DisplayName string   `json:"displayName"`
}

func (s *Service) HandleSCIMRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var scimUser SCIMUser
		if err := json.NewDecoder(r.Body).Decode(&scimUser); err != nil {
			http.Error(w, "invalid SCIM request", http.StatusBadRequest)
			return
		}
		orgID := r.URL.Query().Get("org")
		if orgID == "" {
			orgID = "default"
		}
		user := models.User{
			AuditBase:  models.AuditBase{OrganizationID: orgID},
			Name:       scimUser.DisplayName,
			NameKo:     scimUser.DisplayName,
			Status:     "active",
			AuthMethod: "scim",
			ExternalID: scimUser.UserName,
			Locale:     "ko-KR",
			Timezone:   "Asia/Seoul",
		}
		if !scimUser.Active {
			user.Status = "suspended"
		}
		if err := s.db.Create(&user).Error; err != nil {
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}
		scimUser.ID = user.ID
		w.Header().Set("Content-Type", "application/scim+json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(scimUser)
	case http.MethodDelete:
		userID := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		s.db.Model(&models.User{}).Where("id = ?", userID).Update("status", "offboarded")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) mockSAMLResponse(raw string) *SAMLResponse {
	return &SAMLResponse{
		UserID: "mock-user-001",
		Email:  "kim@patty.dev",
		Name:   "Kim Gaebal",
		NameKo: "김개발",
		Attributes: map[string]string{
			"email":      "kim@patty.dev",
			"name":       "Kim Gaebal",
			"nameKo":     "김개발",
			"department": "개발팀",
		},
		Issuer:       s.samlIDPID,
		NotOnOrAfter: time.Now().Add(8 * time.Hour),
	}
}

func (s *Service) generateMockIDToken() string {
	claims := jwt.MapClaims{
		"sub": "mock-user-001", "email": "kim@patty.dev",
		"name": "김개발", "locale": "ko-KR",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString(s.jwtSecret)
	return signed
}

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
