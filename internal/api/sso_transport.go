package api

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	ssoOIDCCookie = "pccp_oidc_transaction"
	ssoSAMLCookie = "pccp_saml_transaction"
)

func issueSSOTransactionCookie(w http.ResponseWriter, provider string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     ssoCookieName(provider),
		Value:    value,
		Path:     "/api/sso",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
	return value, nil
}

func readSSOTransactionCookie(r *http.Request, provider string) (string, error) {
	cookie, err := r.Cookie(ssoCookieName(provider))
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func clearSSOTransactionCookie(w http.ResponseWriter, provider string) {
	http.SetCookie(w, &http.Cookie{
		Name:     ssoCookieName(provider),
		Path:     "/api/sso",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
}

func ssoCookieName(provider string) string {
	if provider == "saml" {
		return ssoSAMLCookie
	}
	return ssoOIDCCookie
}

func publicSSORateKey(r *http.Request, endpoint string) string {
	host := r.RemoteAddr
	if parsed, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsed
	}
	// Do not include caller-controlled organization input in the key. An
	// attacker could otherwise rotate arbitrary organization strings to obtain a
	// fresh budget for every public SSO request.
	return endpoint + "|" + host
}
