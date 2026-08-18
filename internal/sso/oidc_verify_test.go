package sso

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestOIDCIDTokenVerification covers Task 16's OIDC hardening: the ID
// token is verified against a provisioned JWKS (issuer + expiry +
// algorithm allowlist); ParseUnverified is never used.
func TestOIDCIDTokenVerification(t *testing.T) {
	db := setupDB(t)
	_ = New(db, "test-secret")

	// EC P-256 IdP key + JWKS provisioning.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	xBytes := priv.PublicKey.X.FillBytes(make([]byte, 32))
	jwks, _ := json.Marshal(map[string]any{
		"keys": []map[string]any{{
			"kty": "EC", "crv": "P-256", "kid": "k1",
			"x": base64.RawURLEncoding.EncodeToString(xBytes),
		}},
	})
	keys, err := parseOIDCJWKS(jwks)
	if err != nil {
		t.Fatal(err)
	}

	mint := func(claims jwt.MapClaims, alg string, key interface{}, kid string) string {
		tok := jwt.New(jwt.GetSigningMethod(alg))
		tok.Header["kid"] = kid
		tok.Claims = claims
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	validClaims := jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1", "email": "u@example.com",
		"aud": "client",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	// Valid ES256 token verifies.
	idt := mint(validClaims, "ES256", priv, "k1")
	info, err := parseOIDCIDToken(idt, "https://idp.example", "client", "", keys)
	if err != nil || info.Sub != "user-1" {
		t.Fatalf("valid token rejected: %v %+v", err, info)
	}

	// Wrong issuer rejected.
	badIss := mint(jwt.MapClaims{
		"iss": "https://evil.example", "sub": "user-1", "aud": "client",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}, "ES256", priv, "k1")
	if _, err := parseOIDCIDToken(badIss, "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("wrong issuer accepted")
	}

	// Expired rejected.
	expired := mint(jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1", "aud": "client",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(-time.Hour).Unix(),
	}, "ES256", priv, "k1")
	if _, err := parseOIDCIDToken(expired, "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("expired token accepted")
	}

	// Unknown kid rejected.
	unknownKid := mint(validClaims, "ES256", priv, "not-in-jwks")
	if _, err := parseOIDCIDToken(unknownKid, "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("unknown kid accepted")
	}

	// No JWKS provisioned → refuse (never ParseUnverified).
	if _, err := parseOIDCIDToken(idt, "https://idp.example", "client", "", nil); err == nil {
		t.Fatal("unverified ID token accepted without JWKS")
	}

	// Token-substitution: token for a different client rejected.
	substituted := mint(jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1", "aud": "other-client",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}, "ES256", priv, "k1")
	if _, err := parseOIDCIDToken(substituted, "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("ID token for a different audience accepted")
	}

	// Multi-audience tokens must identify this client as the authorized party.
	multiAudience := jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1", "aud": []string{"client", "other-client"},
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	if _, err := parseOIDCIDToken(mint(multiAudience, "ES256", priv, "k1"), "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("multi-audience ID token without azp was accepted")
	}
	multiAudience["azp"] = "other-client"
	if _, err := parseOIDCIDToken(mint(multiAudience, "ES256", priv, "k1"), "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("multi-audience ID token with mismatched azp was accepted")
	}
	multiAudience["azp"] = "client"
	if _, err := parseOIDCIDToken(mint(multiAudience, "ES256", priv, "k1"), "https://idp.example", "client", "", keys); err != nil {
		t.Fatalf("multi-audience ID token with matching azp was rejected: %v", err)
	}
	singleAudienceWrongAZP := jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1", "aud": "client", "azp": "other-client",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	if _, err := parseOIDCIDToken(mint(singleAudienceWrongAZP, "ES256", priv, "k1"), "https://idp.example", "client", "", keys); err == nil {
		t.Fatal("single-audience ID token with mismatched azp was accepted")
	}

	// Garbage JWKS rejected at provisioning.
	if _, err := parseOIDCJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty JWKS accepted")
	}
}

// Token-substitution attack: an ID token minted for a DIFFERENT
// client must be rejected by the audience check.
