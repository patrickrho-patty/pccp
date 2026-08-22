package sso

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func firstPartyJWKS(t *testing.T, kid string, key *rsa.PrivateKey) []byte {
	t.Helper()
	e := big.NewInt(int64(key.PublicKey.E)).Bytes()
	raw, err := json.Marshal(map[string]interface{}{"keys": []map[string]string{{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFirstPartyVerifierDoesNotRefreshKnownKeyForInvalidToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = w.Write(firstPartyJWKS(t, "key", key))
	}))
	defer server.Close()
	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	invalid := firstPartyToken(t, wrongKey, "key", server.URL, "patcode-device", true)
	for i := 0; i < 3; i++ {
		if _, err := verifier.VerifyAccessToken(context.Background(), invalid); err == nil {
			t.Fatal("expected invalid signature to be rejected")
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("known-kid invalid tokens caused %d JWKS fetches, want 1", fetches.Load())
	}
}

func TestFirstPartyVerifierCoalescesConcurrentUnknownKeyRefresh(t *testing.T) {
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)
	var generation atomic.Int32
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		if generation.Load() == 1 {
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write(firstPartyJWKS(t, "key-2", key2))
			return
		}
		_, _ = w.Write(firstPartyJWKS(t, "key-1", key1))
	}))
	defer server.Close()
	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.VerifyAccessToken(context.Background(), firstPartyToken(t, key1, "key-1", server.URL, "patcode-device", true)); err != nil {
		t.Fatal(err)
	}
	generation.Store(1)
	rotated := firstPartyToken(t, key2, "key-2", server.URL, "patcode-device", true)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := verifier.VerifyAccessToken(context.Background(), rotated)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent rotated token failed: %v", err)
		}
	}
	if fetches.Load() != 2 {
		t.Fatalf("concurrent rotation caused %d JWKS fetches, want 2 total", fetches.Load())
	}
}

func TestFirstPartyVerifierNegativeCachesSequentialUnknownKey(t *testing.T) {
	known, _ := rsa.GenerateKey(rand.Reader, 2048)
	unknown, _ := rsa.GenerateKey(rand.Reader, 2048)
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = w.Write(firstPartyJWKS(t, "known", known))
	}))
	defer server.Close()
	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	unknownToken := firstPartyToken(t, unknown, "never-published", server.URL, "patcode-device", true)
	for i := 0; i < 8; i++ {
		if _, err := verifier.VerifyAccessToken(context.Background(), unknownToken); err == nil {
			t.Fatal("unknown kid accepted")
		}
	}
	if fetches.Load() != 2 {
		t.Fatalf("sequential unknown kid caused %d JWKS fetches, want initial + one authoritative refresh", fetches.Load())
	}
}

func TestFirstPartyVerifierGloballyCoolsDistinctUnknownKidsAndAcceptsRotationAfterCooldown(t *testing.T) {
	known, _ := rsa.GenerateKey(rand.Reader, 2048)
	rotated, _ := rsa.GenerateKey(rand.Reader, 2048)
	var publishRotated atomic.Bool
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		if publishRotated.Load() {
			_, _ = w.Write(firstPartyJWKS(t, "rotated", rotated))
			return
		}
		_, _ = w.Write(firstPartyJWKS(t, "known", known))
	}))
	defer server.Close()
	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		attackerKey, _ := rsa.GenerateKey(rand.Reader, 1024)
		token := firstPartyToken(t, attackerKey, fmt.Sprintf("unknown-%d", i), server.URL, "patcode-device", true)
		if _, err := verifier.VerifyAccessToken(context.Background(), token); err == nil {
			t.Fatal("unknown kid accepted")
		}
	}
	if fetches.Load() != 2 {
		t.Fatalf("distinct unknown kids caused %d fetches, want initial + one forced refresh", fetches.Load())
	}
	publishRotated.Store(true)
	verifier.mu.Lock()
	verifier.forcedRefreshAt = time.Now().Add(-firstPartyForcedRefreshInterval - time.Second)
	verifier.mu.Unlock()
	if _, err := verifier.VerifyAccessToken(context.Background(), firstPartyToken(t, rotated, "rotated", server.URL, "patcode-device", true)); err != nil {
		t.Fatalf("real rotation after cooldown: %v", err)
	}
	if fetches.Load() != 3 {
		t.Fatalf("rotation fetches=%d want 3", fetches.Load())
	}
}

func firstPartyToken(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience string, verified bool) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": issuer, "aud": []string{audience}, "sub": "account-subject", "email": "owner@example.com",
		"email_verified": verified, "scope": "openid profile harness-enroll", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFirstPartyVerifierValidatesIdentityScopeAndRotatedKey(t *testing.T) {
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)
	var generation atomic.Int32
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if generation.Load() == 1 {
			_, _ = w.Write(firstPartyJWKS(t, "key-2", key2))
			return
		}
		_, _ = w.Write(firstPartyJWKS(t, "key-1", key1))
	}))
	defer server.Close()

	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.VerifyAccessToken(context.Background(), firstPartyToken(t, key1, "key-1", server.URL, "patcode-device", true))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "account-subject" || claims.Email != "owner@example.com" || !claims.HasScope("harness-enroll") {
		t.Fatalf("unexpected verified claims: %+v", claims)
	}
	generation.Store(1)
	if _, err := verifier.VerifyAccessToken(context.Background(), firstPartyToken(t, key2, "key-2", server.URL, "patcode-device", true)); err != nil {
		t.Fatalf("rotated key was not recovered by authoritative refresh: %v", err)
	}
	if fetches.Load() != 2 {
		t.Fatalf("expected cached JWKS plus one rotation refresh, got %d fetches", fetches.Load())
	}
}

func TestFirstPartyVerifierRejectsUnverifiedEmailAndUnsafeLoopbackScheme(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(firstPartyJWKS(t, "key", key))
	}))
	defer server.Close()
	verifier, err := NewFirstPartyTokenVerifier(server.URL, []string{"patcode-device"}, server.URL+"/certs", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.VerifyAccessToken(context.Background(), firstPartyToken(t, key, "key", server.URL, "patcode-device", false)); err == nil {
		t.Fatal("expected unverified email to be rejected")
	}
	if _, err := NewFirstPartyTokenVerifier("ftp://localhost/realms/patty", []string{"aud"}, "ftp://localhost/certs", nil); err == nil {
		t.Fatal("expected non-HTTP loopback issuer to be rejected")
	}
}
