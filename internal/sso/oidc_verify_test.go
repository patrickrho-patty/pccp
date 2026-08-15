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
	svc := New(db, "test-secret")
	svc.ConfigureOIDC("https://idp.example", "client", "secret")

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
	if err := svc.SetOIDCJWKS(jwks); err != nil {
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
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	// Valid ES256 token verifies.
	idt := mint(validClaims, "ES256", priv, "k1")
	info, err := svc.ParseOIDCIDToken(idt)
	if err != nil || info.Sub != "user-1" {
		t.Fatalf("valid token rejected: %v %+v", err, info)
	}

	// Wrong issuer rejected.
	badIss := mint(jwt.MapClaims{
		"iss": "https://evil.example", "sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, "ES256", priv, "k1")
	if _, err := svc.ParseOIDCIDToken(badIss); err == nil {
		t.Fatal("wrong issuer accepted")
	}

	// Expired rejected.
	expired := mint(jwt.MapClaims{
		"iss": "https://idp.example", "sub": "user-1",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}, "ES256", priv, "k1")
	if _, err := svc.ParseOIDCIDToken(expired); err == nil {
		t.Fatal("expired token accepted")
	}

	// Unknown kid rejected.
	unknownKid := mint(validClaims, "ES256", priv, "not-in-jwks")
	if _, err := svc.ParseOIDCIDToken(unknownKid); err == nil {
		t.Fatal("unknown kid accepted")
	}

	// No JWKS provisioned → refuse (never ParseUnverified).
	bare := New(db, "s")
	bare.ConfigureOIDC("https://idp.example", "c", "s")
	if _, err := bare.ParseOIDCIDToken(idt); err == nil {
		t.Fatal("unverified ID token accepted without JWKS")
	}

	// Garbage JWKS rejected at provisioning.
	if err := svc.SetOIDCJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty JWKS accepted")
	}
}
