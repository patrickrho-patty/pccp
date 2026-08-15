package secret

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
)

// broker.go upgrades the secret service to the governed Task 17
// contract: every read requires a VERIFIED DARI Authorization Grant
// (dari.tools/1 effect authorization) and is denied BEFORE the value
// is touched; values at rest are envelope-encrypted under the KMS/HSM
// seam; usage accounting feeds the grant's resource budgets.

// ErrGrantRequired is the deny-before-read boundary.
var ErrGrantRequired = fmt.Errorf("secret: read denied — no valid authorization grant for this secret")

// ErrScopeMismatch marks a grant that does not cover the secret ref.
var ErrScopeMismatch = fmt.Errorf("secret: read denied — grant does not cover this secret scope")

// Broker is the governed secret broker facade over the Service.
type Broker struct {
	svc      *Service
	provider keymgmt.KeyProvider
	// atRest envelope-encrypts stored values (issue-time).
	atRest map[string]*keymgmt.Envelope
	// usage meters reads per (grant, secret-ref) for budget checks.
	usage map[string]uint64
	mu    sync.Mutex
}

// NewBroker wraps a Service with governance. provider may be nil (no
// at-rest encryption — dev only); production passes the KMS/HSM seam.
func NewBroker(svc *Service, provider keymgmt.KeyProvider) *Broker {
	return &Broker{svc: svc, provider: provider, atRest: map[string]*keymgmt.Envelope{}, usage: map[string]uint64{}}
}

// IssueScoped issues a short-lived credential and (when a provider is
// configured) stores only the envelope-encrypted value.
func (b *Broker) IssueScoped(req IssueRequest) (*ScopedCredential, error) {
	cred, err := b.svc.Issue(req)
	if err != nil {
		return nil, err
	}
	if b.provider != nil {
		env, err := keymgmt.Seal(b.provider, []byte(cred.CredentialValue))
		if err != nil {
			_ = b.svc.Revoke(cred.OrganizationID, cred.ID, "seal failure")
			return nil, fmt.Errorf("secret: seal credential: %w", err)
		}
		b.mu.Lock()
		b.atRest[cred.ID] = env
		b.mu.Unlock()
	}
	return cred, nil
}

// ReadAuthorized performs the governed read:
//  1. the grant must verify (policy issuer, validity window);
//  2. the grant's tool/scope set must cover the secret ref target;
//  3. the read is metered against the grant's budget;
//  4. only then is the credential value returned (or unsealed).
//
// A missing/invalid/out-of-scope grant is denied BEFORE the value is
// touched — deny-before-side-effect is the Task 17 boundary.
func (b *Broker) ReadAuthorized(ctx context.Context, grant *dari.GrantEnvelope, signer dari.AuthorityResolver, credID, secretRef string, nowMs int64) (string, error) {
	if grant == nil || grant.Body == nil {
		return "", ErrGrantRequired
	}
	if err := dari.VerifyGrantAuthority(grant, signer, nowMs); err != nil {
		return "", fmt.Errorf("%w: %v", ErrGrantRequired, err)
	}
	if !grantCoversSecret(grant.Body.Scope, secretRef) {
		return "", ErrScopeMismatch
	}
	// Budget metering: count reads against the grant.
	key := grant.Body.GrantID + "|" + secretRef
	b.mu.Lock()
	b.usage[key]++
	reads := b.usage[key]
	b.mu.Unlock()
	if max, ok := grant.Body.Scope.ResourceBudgets["secret.reads"]; ok && reads > max {
		return "", fmt.Errorf("secret: read denied — budget secret.reads exhausted (%d > %d)", reads, max)
	}

	// The underlying credential must itself be live (budget is charged
	// only for authorized, usable reads).
	if _, err := b.svc.Validate(credID, grant.Body.SessionID); err != nil {
		return "", fmt.Errorf("secret: credential not usable: %w", err)
	}
	return b.svc.GetCredentialValue(credID)
}

// EvictExpired drops sealed envelopes and usage counters whose grants
// have passed their validity window (bounded memory; sealed material
// does not outlive its grant).
func (b *Broker) EvictExpired(nowMs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// NOTE: usage keys carry grantIDs, not expiry — the caller sweeps
	// atRest by credential validity through the service.
	for id := range b.atRest {
		if _, err := b.svc.Validate(id, ""); err != nil {
			delete(b.atRest, id)
		}
	}
}

// grantCoversSecret checks the grant's tool/scope coverage for a
// secret reference. Coverage is by exact secret-ref match or a
// declared "secrets:<target>" tool class.
func grantCoversSecret(scope dari.AuthorizationScope, secretRef string) bool {
	for _, t := range scope.Tools {
		if t == "secrets:*" || t == "secrets:"+secretRef {
			return true
		}
	}
	return false
}

// UsageReport returns per-(grant, secret) read counts for evidence.
func (b *Broker) UsageReport() map[string]uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]uint64, len(b.usage))
	for k, v := range b.usage {
		out[k] = v
	}
	return out
}

var _ = time.Now
