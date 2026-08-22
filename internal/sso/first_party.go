package sso

import (
	"context"
	"crypto"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	firstPartyJWKSCacheTTL          = 15 * time.Minute
	firstPartyUnknownKidTTL         = 30 * time.Second
	firstPartyUnknownKidMax         = 128
	firstPartyForcedRefreshInterval = 30 * time.Second
)

// FirstPartyAccessClaims is the verified public-account identity used only to
// bootstrap Harness enrollment. It is never accepted by an inference route.
type FirstPartyAccessClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Scopes        []string
}

func (c *FirstPartyAccessClaims) HasScope(want string) bool {
	for _, scope := range c.Scopes {
		if scope == want {
			return true
		}
	}
	return false
}

// FirstPartyTokenVerifier validates Patty-operated Keycloak access tokens
// against a server-owned issuer, audience set, and rotating JWKS cache.
type FirstPartyTokenVerifier struct {
	issuer    string
	audiences []string
	jwksURL   string
	client    *http.Client

	mu              sync.RWMutex
	keys            map[string][]crypto.PublicKey
	fetchedAt       time.Time
	refresh         *firstPartyJWKSRefresh
	unknownKids     map[string]time.Time
	forcedRefreshAt time.Time
}

type firstPartyJWKSRefresh struct {
	done chan struct{}
	err  error
}

func NewFirstPartyTokenVerifier(issuer string, audiences []string, jwksURL string, client *http.Client) (*FirstPartyTokenVerifier, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return nil, fmt.Errorf("sso: first-party OIDC issuer is required")
	}
	parsedIssuer, err := url.Parse(issuer)
	if err != nil || !validSSOURL(parsedIssuer, true) {
		return nil, fmt.Errorf("sso: first-party OIDC issuer must be HTTPS")
	}
	cleanAudiences := make([]string, 0, len(audiences))
	for _, audience := range audiences {
		if audience = strings.TrimSpace(audience); audience != "" {
			cleanAudiences = append(cleanAudiences, audience)
		}
	}
	if len(cleanAudiences) == 0 {
		return nil, fmt.Errorf("sso: first-party OIDC audience is required")
	}
	if strings.TrimSpace(jwksURL) == "" {
		jwksURL = issuer + "/protocol/openid-connect/certs"
	}
	parsedJWKS, err := url.Parse(jwksURL)
	if err != nil || parsedJWKS.Hostname() != parsedIssuer.Hostname() || !validSSOURL(parsedJWKS, true) {
		return nil, fmt.Errorf("sso: first-party JWKS URL must use the configured issuer host")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &FirstPartyTokenVerifier{issuer: issuer, audiences: cleanAudiences, jwksURL: jwksURL, client: client, unknownKids: make(map[string]time.Time)}, nil
}

// NewFirstPartyTokenVerifierFromEnv loads the non-tenant-editable public issuer
// trust. An empty issuer deliberately leaves public enrollment unavailable.
func NewFirstPartyTokenVerifierFromEnv() (*FirstPartyTokenVerifier, error) {
	issuer := strings.TrimSpace(os.Getenv("PCCP_PUBLIC_OIDC_ISSUER"))
	if issuer == "" {
		return nil, nil
	}
	audiences := strings.Split(strings.TrimSpace(os.Getenv("PCCP_PUBLIC_OIDC_AUDIENCES")), ",")
	if len(audiences) == 1 && strings.TrimSpace(audiences[0]) == "" {
		audiences = []string{"patcode-pkce", "patcode-device"}
	}
	return NewFirstPartyTokenVerifier(issuer, audiences, os.Getenv("PCCP_PUBLIC_OIDC_JWKS_URL"), nil)
}

func (v *FirstPartyTokenVerifier) VerifyAccessToken(ctx context.Context, raw string) (*FirstPartyAccessClaims, error) {
	if v == nil {
		return nil, fmt.Errorf("sso: first-party OIDC trust is not configured")
	}
	keys, fetchedAt, err := v.loadJWKS(ctx, false, time.Time{})
	if err != nil {
		return nil, err
	}
	kid, err := firstPartyTokenKeyID(raw)
	if err != nil {
		return nil, err
	}
	if kid != "" && len(keys[kid]) == 0 {
		v.mu.RLock()
		blockedUntil := v.unknownKids[kid]
		v.mu.RUnlock()
		if time.Now().Before(blockedUntil) {
			return nil, fmt.Errorf("sso: first-party access token references an unknown key")
		}
		// Only an unknown rotated kid triggers an authoritative refresh. The
		// observed timestamp prevents concurrent callers from serially fetching
		// the same rotation after the first refresh completes.
		keys, _, err = v.loadJWKS(ctx, true, fetchedAt)
		if err != nil {
			return nil, err
		}
		if len(keys[kid]) == 0 {
			v.mu.Lock()
			if len(v.unknownKids) >= firstPartyUnknownKidMax {
				for cached := range v.unknownKids {
					delete(v.unknownKids, cached)
					break
				}
			}
			v.unknownKids[kid] = time.Now().Add(firstPartyUnknownKidTTL)
			v.mu.Unlock()
			return nil, fmt.Errorf("sso: first-party access token references an unknown key")
		}
	}
	return verifyFirstPartyAccessToken(raw, v.issuer, v.audiences, keys)
}

func firstPartyTokenKeyID(raw string) (string, error) {
	var claims jwt.MapClaims
	token, _, err := jwt.NewParser().ParseUnverified(raw, &claims)
	if err != nil {
		return "", fmt.Errorf("sso: first-party access token malformed: %w", err)
	}
	kid, _ := token.Header["kid"].(string)
	return strings.TrimSpace(kid), nil
}

func (v *FirstPartyTokenVerifier) loadJWKS(ctx context.Context, force bool, observed time.Time) (map[string][]crypto.PublicKey, time.Time, error) {
	for {
		v.mu.RLock()
		keys, fetchedAt, refresh, forcedRefreshAt := v.keys, v.fetchedAt, v.refresh, v.forcedRefreshAt
		fresh := len(keys) > 0 && time.Since(fetchedAt) < firstPartyJWKSCacheTTL
		newer := !observed.IsZero() && fetchedAt.After(observed)
		forceCooling := force && !forcedRefreshAt.IsZero() && time.Since(forcedRefreshAt) < firstPartyForcedRefreshInterval
		v.mu.RUnlock()
		if (!force && fresh) || (force && (newer || (forceCooling && refresh == nil))) {
			return keys, fetchedAt, nil
		}
		if refresh != nil {
			select {
			case <-ctx.Done():
				return nil, time.Time{}, ctx.Err()
			case <-refresh.done:
				if refresh.err != nil {
					return nil, time.Time{}, refresh.err
				}
				continue
			}
		}

		candidate := &firstPartyJWKSRefresh{done: make(chan struct{})}
		v.mu.Lock()
		if v.refresh != nil {
			v.mu.Unlock()
			continue
		}
		if force && !observed.IsZero() && v.fetchedAt.After(observed) {
			keys, fetchedAt = v.keys, v.fetchedAt
			v.mu.Unlock()
			return keys, fetchedAt, nil
		}
		if force && !v.forcedRefreshAt.IsZero() && time.Since(v.forcedRefreshAt) < firstPartyForcedRefreshInterval {
			keys, fetchedAt = v.keys, v.fetchedAt
			v.mu.Unlock()
			return keys, fetchedAt, nil
		}
		if force {
			v.forcedRefreshAt = time.Now()
		}
		v.refresh = candidate
		v.mu.Unlock()

		parsed, fetchErr := v.fetchJWKS(ctx)
		v.mu.Lock()
		if fetchErr == nil {
			v.keys, v.fetchedAt = parsed, time.Now()
			for kid := range parsed {
				delete(v.unknownKids, kid)
			}
		}
		candidate.err = fetchErr
		v.refresh = nil
		close(candidate.done)
		keys, fetchedAt = v.keys, v.fetchedAt
		v.mu.Unlock()
		if fetchErr != nil {
			return nil, time.Time{}, fetchErr
		}
		return keys, fetchedAt, nil
	}
}

func (v *FirstPartyTokenVerifier) fetchJWKS(ctx context.Context) (map[string][]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("sso: build first-party JWKS request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sso: first-party JWKS unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sso: first-party JWKS returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("sso: read first-party JWKS: %w", err)
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("sso: first-party JWKS exceeds 1 MiB")
	}
	return parseOIDCJWKS(raw)
}

func verifyFirstPartyAccessToken(raw, issuer string, audiences []string, keys map[string][]crypto.PublicKey) (*FirstPartyAccessClaims, error) {
	var info *OIDCUserInfo
	var err error
	for _, audience := range audiences {
		info, err = parseOIDCIDToken(raw, issuer, audience, "", keys)
		if err == nil {
			break
		}
	}
	if err != nil || info == nil {
		return nil, fmt.Errorf("sso: first-party access token verification failed: %w", err)
	}
	var tokenClaims jwt.MapClaims
	if _, _, err := jwt.NewParser().ParseUnverified(raw, &tokenClaims); err != nil {
		return nil, fmt.Errorf("sso: first-party access token claims malformed: %w", err)
	}
	if info.Email == "" || !info.EmailVerified {
		return nil, fmt.Errorf("sso: first-party access token carries no verified email")
	}
	scopes := stringSliceClaim(tokenClaims["scope"])
	if len(scopes) == 0 {
		scopes = stringSliceClaim(tokenClaims["scp"])
	}
	return &FirstPartyAccessClaims{
		Issuer: issuer, Subject: info.Sub, Email: info.Email, EmailVerified: info.EmailVerified, Scopes: scopes,
	}, nil
}

func stringSliceClaim(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		return strings.Fields(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if scope, ok := item.(string); ok && strings.TrimSpace(scope) != "" {
				out = append(out, strings.TrimSpace(scope))
			}
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return nil
	}
}
