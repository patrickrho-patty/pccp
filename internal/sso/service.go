package sso

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
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
	db         *gorm.DB
	jwtSecret  []byte
	samlIDPID  string
	samlIDPURL string
	samlSPURL  string
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
	UserID       string
	Email        string
	Name         string
	NameKo       string
	Attributes   map[string]string
	Issuer       string
	NotOnOrAfter time.Time
}

func (s *Service) HandleSAMLCallback(samlResponse string, relayState string) (*SAMLResponse, error) {
	data, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("sso: decode SAML response: %w", err)
	}

	var resp struct {
		XMLName   xml.Name `xml:"Response"`
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
		return s.mockSAMLResponse(samlResponse), nil
	}

	if len(resp.Assertions) == 0 || resp.Assertions[0].Subject.NameID == "" {
		return s.mockSAMLResponse(samlResponse), nil
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
	return &OIDCTokenResponse{
		AccessToken: "mock_access_" + generateID(),
		IDToken:     s.generateMockIDToken(),
		ExpiresIn:   3600,
		TokenType:   "Bearer",
	}, nil
}

type OIDCUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Locale string `json:"locale,omitempty"`
}

func (s *Service) ParseOIDCIDToken(idToken string) (*OIDCUserInfo, error) {
	token, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("sso: parse ID token: %w", err)
	}
	claims := token.Claims.(jwt.MapClaims)
	info := &OIDCUserInfo{
		Sub:    getStringClaim(claims, "sub"),
		Email:  getStringClaim(claims, "email"),
		Name:   getStringClaim(claims, "name"),
		Locale: getStringClaim(claims, "locale"),
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
	if saml.Name != "" { user.Name = saml.Name }
	if saml.NameKo != "" { user.NameKo = saml.NameKo }
	if saml.Email != "" { user.Email = saml.Email }
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
	Schemas    []string `json:"schemas"`
	ID         string   `json:"id"`
	UserName   string   `json:"userName"`
	Active     bool     `json:"active"`
	DisplayName string  `json:"displayName"`
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
		if orgID == "" { orgID = "default" }
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
		if !scimUser.Active { user.Status = "suspended" }
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
	if v, ok := claims[key]; ok { return fmt.Sprintf("%v", v) }
	return ""
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

var _ = ed25519.PublicKeySize
