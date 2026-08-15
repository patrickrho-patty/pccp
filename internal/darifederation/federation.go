// Package darifederation implements the dari.federation/1 runtime
// profile (spec Appendix F.13 §11, master plan Task 14): signed
// trust-bundle discovery/import with freshness and rollback
// protection, bilateral issuer/audience validation, deterministic
// policy intersection (denial and the narrower authority win),
// residency constraints, and cross-domain receipt verification.
//
// Federation never treats a remote signature as local authorization:
// each trust domain validates the full chain and applies local policy;
// the resulting authority is the intersection of all valid grants and
// policies. Missing trust, state, residency, or receipt-key material
// fails closed.
package darifederation

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// ---------------------------------------------------------------------------
// Trust bundle (spec F.13 federation-trust-bundle-body).
// ---------------------------------------------------------------------------

// TrustBundleBody is the signed federation trust bundle.
type TrustBundleBody struct {
	Version          uint16        `cbor:"1,keyasint"`
	TrustDomain      string        `cbor:"2,keyasint"`
	Issuers          []string      `cbor:"3,keyasint"`
	IssuerKeyDigests []dari.Digest `cbor:"4,keyasint"`
	Audiences        []string      `cbor:"5,keyasint"`
	Sequence         uint64        `cbor:"6,keyasint"`
	IssuedAtMs       int64         `cbor:"7,keyasint"`
	ExpiresAtMs      int64         `cbor:"8,keyasint"`
}

// TrustBundleAAD is the signing domain.
const TrustBundleAAD = "DARI-FEDERATION-TRUST-BUNDLE-v1\x00"

// IssuerKeyDigest computes the trust-bundle issuer-key digest form:
// SHA-256 over the domain-separated public key bytes.
func IssuerKeyDigest(pub ed25519.PublicKey) dari.Digest {
	h := sha256.New()
	h.Write([]byte("DARI-FEDERATION-ISSUER-KEY-v1\x00"))
	h.Write([]byte(pub))
	var d dari.Digest
	copy(d[:], h.Sum(nil))
	return d
}

// TrustBundleEnvelope is a signed bundle.
type TrustBundleEnvelope struct {
	Body      *TrustBundleBody
	COSE      *dari.COSESign1
	COSEBytes []byte
	Digest    dari.Digest // signed-object digest
}

// SignTrustBundle signs the canonical bundle body.
func SignTrustBundle(b *TrustBundleBody, priv ed25519.PrivateKey) (*TrustBundleEnvelope, error) {
	if b.ExpiresAtMs <= b.IssuedAtMs {
		return nil, errors.New("federation: bundle expiry must exceed issuance")
	}
	sort.Strings(b.Issuers)
	sort.Strings(b.Audiences)
	payload, err := dari.MarshalCBOR(b)
	if err != nil {
		return nil, err
	}
	kid := dari.SubjectKeyThumbprint(priv.Public().(ed25519.PublicKey))
	sign1, err := dari.CreateCOSESign1WithAAD(payload, []byte(TrustBundleAAD), priv, kid[:])
	if err != nil {
		return nil, err
	}
	coseBytes, err := dari.MarshalCBOR(sign1)
	if err != nil {
		return nil, err
	}
	var dec dari.Digest
	copy(dec[:], coseBytes)
	return &TrustBundleEnvelope{Body: b, COSE: sign1, COSEBytes: coseBytes, Digest: dec}, nil
}

// VerifyTrustBundle verifies a bundle under the bootstrap key set.
func VerifyTrustBundle(env *TrustBundleEnvelope, bootstrapKeys []ed25519.PublicKey) error {
	if env == nil || env.COSE == nil {
		return errors.New("federation: nil trust bundle")
	}
	var lastErr error
	for _, k := range bootstrapKeys {
		if err := dari.VerifyCOSESign1WithAAD(env.COSE, []byte(TrustBundleAAD), env.COSE.Payload, k); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("federation: no bootstrap keys")
	}
	return fmt.Errorf("federation: bundle signature: %w", lastErr)
}

// ---------------------------------------------------------------------------
// Trust store: monotonic high-water + rollback + staleness + quarantine.
// ---------------------------------------------------------------------------

// ErrStateRollback mirrors the kernel checkpoint rollback code.
var ErrStateRollback = errors.New("FEDERATION_STATE_ROLLBACK")

// ErrQuarantined marks an emergency-quarantined domain.
var ErrQuarantined = errors.New("FEDERATION_DOMAIN_QUARANTINED")

// ErrStaleBundle marks exceeded maximum staleness.
var ErrStaleBundle = errors.New("FEDERATION_BUNDLE_STALE")

// MaxStaleness is the configured upper bound on bundle age.
var MaxStaleness = 24 * time.Hour

// TrustStore is the per-(trust-domain) bundle ledger.
type TrustStore struct {
	mu         sync.Mutex
	highWater  map[string]uint64
	highDigest map[string]dari.Digest
	quarantine map[string]bool
	maxStale   time.Duration
}

// NewTrustStore builds a store.
func NewTrustStore() *TrustStore {
	return &TrustStore{
		highWater:  map[string]uint64{},
		highDigest: map[string]dari.Digest{},
		quarantine: map[string]bool{},
		maxStale:   MaxStaleness,
	}
}

// Quarantine emergency-quarantines a trust domain (map: emergency
// domain quarantine). All subsequent imports/validations fail closed.
func (t *TrustStore) Quarantine(domain string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.quarantine[domain] = true
}

// Import accepts a verified bundle into the store with the F.7-style
// rules: strictly increasing sequence per domain; equal sequence is an
// idempotent replay only for identical bytes; rollback is rejected;
// stale bundles rejected; quarantined domains rejected.
func (t *TrustStore) Import(env *TrustBundleEnvelope, nowMs int64) error {
	if env == nil || env.Body == nil {
		return errors.New("federation: nil bundle")
	}
	domain := env.Body.TrustDomain
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantine[domain] {
		return fmt.Errorf("%w: domain %s", ErrQuarantined, domain)
	}
	// Freshness: expires, and age within the staleness bound.
	if nowMs >= env.Body.ExpiresAtMs {
		return fmt.Errorf("%w: bundle expired", ErrStaleBundle)
	}
	if age := nowMs - env.Body.IssuedAtMs; age < 0 || uint64(age) > uint64(t.maxStale.Milliseconds()) {
		return fmt.Errorf("%w: bundle age %dms exceeds bound %dms", ErrStaleBundle, age, t.maxStale.Milliseconds())
	}
	cur, seen := t.highWater[domain]
	if !seen {
		t.highWater[domain] = env.Body.Sequence
		t.highDigest[domain] = env.Digest
		return nil
	}
	switch {
	case env.Body.Sequence < cur:
		return fmt.Errorf("%w: sequence %d below high-water %d", ErrStateRollback, env.Body.Sequence, cur)
	case env.Body.Sequence == cur:
		if env.Digest == t.highDigest[domain] {
			return nil // idempotent replay
		}
		return fmt.Errorf("%w: sequence %d forked", ErrStateRollback, env.Body.Sequence)
	default:
		t.highWater[domain] = env.Body.Sequence
		t.highDigest[domain] = env.Digest
		return nil
	}
}

// HighWater reports the current sequence for a domain.
func (t *TrustStore) HighWater(domain string) (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	seq, ok := t.highWater[domain]
	return seq, ok
}

// ---------------------------------------------------------------------------
// Bilateral issuer/audience validation.
// ---------------------------------------------------------------------------

// ErrUntrustedIssuer marks an issuer outside the bundle.
var ErrUntrustedIssuer = errors.New("FEDERATION_UNTRUSTED_ISSUER")

// ErrAudienceMismatch marks an object issued for another audience.
var ErrAudienceMismatch = errors.New("FEDERATION_AUDIENCE_MISMATCH")

// ValidateBilateral checks a grant against a trust bundle: the grant's
// issuer must be in the bundle's issuer set (by key digest), the
// audience must include the LOCAL domain, and the bundle fresh.
func ValidateBilateral(bundle *TrustBundleEnvelope, grant *dari.GrantEnvelope, localDomain string, nowMs int64) error {
	if bundle == nil || bundle.Body == nil {
		return errors.New("federation: no trust bundle")
	}
	if nowMs >= bundle.Body.ExpiresAtMs {
		return fmt.Errorf("%w: bundle expired", ErrStaleBundle)
	}
	// Audience: the local trust domain must be an allowed audience.
	allowed := false
	for _, a := range bundle.Body.Audiences {
		if a == localDomain {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: local domain %q not in bundle audiences", ErrAudienceMismatch, localDomain)
	}
	// Issuer membership by the bundle's issuer-key digest form.
	thumb := IssuerKeyDigest(grant.SignerKey)
	trusted := false
	for _, d := range bundle.Body.IssuerKeyDigests {
		if d == thumb {
			trusted = true
			break
		}
	}
	if !trusted {
		return fmt.Errorf("%w: signer key not in bundle issuer set", ErrUntrustedIssuer)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deterministic policy intersection.
// ---------------------------------------------------------------------------

// PolicyIntersect computes the intersection of local and remote
// authorization scopes: denial and the narrower authority win
// (map §11). The result is the maximal scope permitted by BOTH.
func PolicyIntersect(local, remote dari.AuthorizationScope) dari.AuthorizationScope {
	return dari.AuthorizationScope{
		ActionClasses:       intersectStrings(local.ActionClasses, remote.ActionClasses),
		Models:              intersectStrings(local.Models, remote.Models),
		ReadPaths:           intersectPaths(local.ReadPaths, remote.ReadPaths),
		WritePaths:          intersectPaths(local.WritePaths, remote.WritePaths),
		Tools:               intersectStrings(local.Tools, remote.Tools),
		Networks:            intersectNetworks(local.Networks, remote.Networks),
		DataClassifications: intersectStrings(local.DataClassifications, remote.DataClassifications),
		ResourceBudgets:     intersectBudgets(local.ResourceBudgets, remote.ResourceBudgets),
		ProtectionProfiles:  intersectStrings(local.ProtectionProfiles, remote.ProtectionProfiles),
		// Approvals UNION: requiring more approvals is always safe.
		ApprovalClasses: unionStrings(local.ApprovalClasses, remote.ApprovalClasses),
	}
}

func intersectStrings(a, b []string) []string {
	set := map[string]struct{}{}
	for _, s := range b {
		set[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := set[s]; ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func unionStrings(a, b []string) []string {
	set := map[string]struct{}{}
	var out []string
	for _, group := range [][]string{a, b} {
		for _, s := range group {
			if _, ok := set[s]; !ok {
				set[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func intersectBudgets(a, b map[string]uint64) map[string]uint64 {
	out := map[string]uint64{}
	for k, v := range a {
		if bv, ok := b[k]; ok {
			if bv < v {
				out[k] = bv
			} else {
				out[k] = v
			}
		}
	}
	return out
}

// intersectPaths narrows to rules covered by both sides: same
// authority/revision, the NARROWER prefix when one covers the other
// (or their common segment boundary otherwise), intersected
// operations. A rule not covered on both sides is dropped — the
// intersection never widens.
func intersectPaths(a, b []dari.PathScope) []dari.PathScope {
	var out []dari.PathScope
	for _, ra := range a {
		for _, rb := range b {
			if ra.Authority != rb.Authority || ra.Revision != rb.Revision {
				continue
			}
			ops := intersectStrings(ra.Operations, rb.Operations)
			if len(ops) == 0 {
				continue
			}
			prefix := narrowerPrefix(ra.Prefix, rb.Prefix)
			if prefix == "" {
				continue
			}
			out = append(out, dari.PathScope{
				Authority: ra.Authority, Revision: ra.Revision,
				Prefix: prefix, Operations: ops,
			})
		}
	}
	return out
}

// narrowerPrefix returns the narrower (longer) prefix when one covers
// the other, else the longest common segment boundary — never wider
// than both inputs.
func narrowerPrefix(a, b string) string {
	if len(a) > len(b) {
		a, b = b, a
	}
	// a is the shorter. If a is a prefix-ancestor of b, b is narrower.
	if a == b || (len(b) > len(a) && b[:len(a)] == a && b[len(a)] == '/') {
		return b
	}
	// Otherwise the longest common segment boundary.
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	p := a[:n]
	for len(p) > 0 && p[len(p)-1] != '/' {
		p = p[:len(p)-1]
	}
	if len(p) > 0 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	if p == "" || p == "." {
		return ""
	}
	return p
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func intersectNetworks(a, b []dari.NetworkScope) []dari.NetworkScope {
	var out []dari.NetworkScope
	for _, na := range a {
		for _, nb := range b {
			if na.Scheme != nb.Scheme || na.Host != nb.Host {
				continue
			}
			first, last := na.PortFirst, na.PortLast
			if nb.PortFirst > first {
				first = nb.PortFirst
			}
			if nb.PortLast < last {
				last = nb.PortLast
			}
			if first > last {
				continue
			}
			purposes := intersectStrings(na.Purposes, nb.Purposes)
			if len(purposes) == 0 {
				continue
			}
			out = append(out, dari.NetworkScope{
				Scheme: na.Scheme, Host: na.Host,
				PortFirst: first, PortLast: last, Purposes: purposes,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Residency constraints.
// ---------------------------------------------------------------------------

// ResidencyPolicy constrains where governed data may flow.
type ResidencyPolicy struct {
	// AllowedRegions are the ISO region codes data may transit/be
	// processed in. Empty = no restriction configured (fail-closed at
	// the caller's discretion).
	AllowedRegions []string
	// ForbiddenDomains are trust domains explicitly excluded.
	ForbiddenDomains []string
}

// ErrResidency marks a residency violation.
var ErrResidency = errors.New("FEDERATION_RESIDENCY_VIOLATION")

// CheckResidency validates a cross-domain routing decision: the
// destination domain must not be forbidden and its region must be
// allowed. Missing region information fails closed.
func CheckResidency(policy ResidencyPolicy, destDomain string, destRegion string) error {
	for _, d := range policy.ForbiddenDomains {
		if d == destDomain {
			return fmt.Errorf("%w: domain %q forbidden", ErrResidency, destDomain)
		}
	}
	if len(policy.AllowedRegions) > 0 {
		if destRegion == "" {
			return fmt.Errorf("%w: destination region unknown", ErrResidency)
		}
		allowed := false
		for _, r := range policy.AllowedRegions {
			if r == destRegion {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: region %q not allowed", ErrResidency, destRegion)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cross-domain receipt verification.
// ---------------------------------------------------------------------------

// ErrReceiptDomain marks a receipt signed outside the trust bundle.
var ErrReceiptDomain = errors.New("FEDERATION_RECEIPT_DOMAIN")

// VerifyCrossDomainReceipt verifies a receipt attestation produced by
// a remote trust domain: the attestation signature must verify under a
// key digest in the (fresh) bundle, the role/claims must satisfy the
// kernel scope rules, and the attested receipt body digest must match.
func VerifyCrossDomainReceipt(bundle *TrustBundleEnvelope, receiptBodyDigest dari.Digest, att *dari.ReceiptAttestationBody, attCOSE []byte, attSigner ed25519.PublicKey, nowMs int64) error {
	if bundle == nil || bundle.Body == nil {
		return errors.New("federation: no trust bundle")
	}
	if nowMs >= bundle.Body.ExpiresAtMs {
		return fmt.Errorf("%w: bundle expired", ErrStaleBundle)
	}
	if att.ReceiptBodyDigest != receiptBodyDigest {
		return fmt.Errorf("%w: attestation binds a different receipt body", ErrReceiptDomain)
	}
	// Signer key must be in the bundle's issuer-key set.
	thumb := IssuerKeyDigest(attSigner)
	trusted := false
	for _, d := range bundle.Body.IssuerKeyDigests {
		if d == thumb {
			trusted = true
			break
		}
	}
	if !trusted {
		return fmt.Errorf("%w: receipt signer not trusted by the bundle", ErrReceiptDomain)
	}
	// Kernel scope rules still apply cross-domain.
	if err := dari.ValidateAttestationScope(att); err != nil {
		return fmt.Errorf("%w: %v", ErrReceiptDomain, err)
	}
	// Signature over the attestation envelope.
	var sign1 dari.COSESign1
	if err := dari.UnmarshalCBOR(attCOSE, &sign1); err != nil {
		return fmt.Errorf("%w: decode attestation: %v", ErrReceiptDomain, err)
	}
	payload, err := dari.MarshalCBOR(att)
	if err != nil {
		return err
	}
	if !bytes.Equal(payload, sign1.Payload) {
		return fmt.Errorf("%w: attestation body is not canonical", ErrReceiptDomain)
	}
	if err := dari.VerifyCOSESign1WithAAD(&sign1, []byte(dari.ReceiptAttestationAAD), payload, attSigner); err != nil {
		return fmt.Errorf("%w: attestation signature: %v", ErrReceiptDomain, err)
	}
	return nil
}
