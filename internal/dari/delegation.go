package dari

import (
	"bytes"
	"errors"
	"fmt"

	"crypto/ed25519"
)

// delegation.go implements delegated-authorization attenuation (spec
// Appendix F.4 algorithm, master plan Task 8). A delegation chain is
// validated root-to-leaf; ANY failed comparison rejects the entire
// chain with AUTHORITY_ESCALATION and no partial authority.

// Delegation errors map to the spec's conformance codes.
var (
	ErrInvalidGrantChain   = errors.New("INVALID_GRANT_CHAIN")
	ErrAuthorityEscalation = errors.New("AUTHORITY_ESCALATION")
	ErrGrantReplay         = errors.New("GRANT_REPLAY")
)

// MaxDelegationChainLen is the F.4 chain-length bound.
const MaxDelegationChainLen = 32

// ChainContext supplies live state to the chain validator. Every hook
// is optional; a nil hook skips its check (callers composing the
// validator for a live path MUST supply revocation and root-policy
// hooks — the pure object-level checks never substitute for them).
type ChainContext struct {
	// NowMs is the evaluation time (F.4 time containment + validity).
	NowMs int64
	// Revoked is consulted for grant digests and credential serials.
	Revoked func(digest Digest) bool
	// RootPolicy authorizes the root issuer for the root scope
	// (algorithm step 10's first clause). Return false to reject.
	RootPolicy func(root *GrantEnvelope) bool
	// SequenceSeen reports the previously recorded digest for an
	// (issuer, sequence) pair: "" when unseen, the recorded digest
	// otherwise. A same-digest reobservation is accepted; a different
	// digest for a seen sequence is GRANT_REPLAY.
	SequenceSeen func(issuer string, sequence uint64) Digest
	// RecordSequence records a newly accepted sequence (atomic in the
	// durable ledger; in-memory here).
	RecordSequence func(issuer string, sequence uint64, digest Digest)
}

// ValidateDelegationChain validates a root-to-leaf grant chain per the
// F.4 attenuation algorithm. envelopes[0] is the ROOT (no parent
// digest); the last is the leaf whose authority will be used.
func ValidateDelegationChain(chain []*GrantEnvelope, ctx ChainContext) error {
	if len(chain) == 0 || len(chain) > MaxDelegationChainLen {
		return fmt.Errorf("%w: chain length %d", ErrInvalidGrantChain, len(chain))
	}

	// Track repeated digests (rule 1).
	seen := map[Digest]bool{}

	// Step 2: per-grant validation, root-to-leaf.
	for i, env := range chain {
		if env == nil || env.Body == nil || env.COSE == nil {
			return fmt.Errorf("%w: grant %d missing envelope parts", ErrInvalidGrantChain, i)
		}
		if seen[env.SignedDigest] {
			return fmt.Errorf("%w: repeated grant digest at %d", ErrInvalidGrantChain, i)
		}
		seen[env.SignedDigest] = true

		b := env.Body
		canonical, err := EncodeGrantBody(b)
		if err != nil {
			return fmt.Errorf("%w: grant %d not canonicalizable: %v", ErrInvalidGrantChain, i, err)
		}
		// Body/payload equality + signature under the stated signer.
		if !bytes.Equal(canonical, env.COSE.Payload) {
			return fmt.Errorf("%w: grant %d payload is not its canonical body", ErrInvalidGrantChain, i)
		}
		if err := VerifyCOSESign1WithAAD(env.COSE, []byte(AuthorizationGrantAAD), canonical, env.SignerKey); err != nil {
			return fmt.Errorf("%w: grant %d signature: %v", ErrInvalidGrantChain, i, err)
		}
		// Validity window.
		if ctx.NowMs != 0 && (ctx.NowMs < b.NotBeforeMs || ctx.NowMs >= b.NotAfterMs) {
			return fmt.Errorf("%w: grant %d outside validity window", ErrInvalidGrantChain, i)
		}
		// Revocation.
		if ctx.Revoked != nil && ctx.Revoked(env.SignedDigest) {
			return fmt.Errorf("%w: grant %d revoked", ErrAuthorityEscalation, i)
		}
		// Issuer-sequence replay ledger (F.4 ledger semantics).
		if ctx.SequenceSeen != nil {
			if prev := ctx.SequenceSeen(b.Issuer, b.IssuerSequence); prev != (Digest{}) {
				if prev != env.SignedDigest {
					return fmt.Errorf("%w: issuer %s sequence %d reused for a different digest", ErrGrantReplay, b.Issuer, b.IssuerSequence)
				}
			} else if ctx.RecordSequence != nil {
				ctx.RecordSequence(b.Issuer, b.IssuerSequence, env.SignedDigest)
			}
		}

		// Root shape (rule 1): the first grant has no label 15.
		if i == 0 {
			if b.HasParentDigest() {
				return fmt.Errorf("%w: root grant carries a parent digest", ErrInvalidGrantChain)
			}
			if ctx.RootPolicy != nil && !ctx.RootPolicy(env) {
				return fmt.Errorf("%w: root issuer not authorized for the root scope", ErrAuthorityEscalation)
			}
		}
	}

	// Steps 1(residual), 3-9 over each parent/child pair.
	for i := 0; i+1 < len(chain); i++ {
		if err := validateDelegationPair(chain[i], chain[i+1]); err != nil {
			return fmt.Errorf("dari: chain pair %d: %w", i, err)
		}
	}
	return nil
}

// validateDelegationPair applies rules 1(residual), 3-9 to one
// parent/child pair. Shared by the chain walker and IssueChildGrant.
func validateDelegationPair(parentEnv, childEnv *GrantEnvelope) error {
	parent, child := parentEnv.Body, childEnv.Body
	// Rule 1: child's parent digest binds exactly to the parent's
	// signed-object digest.
	if !child.HasParentDigest() {
		return fmt.Errorf("%w: delegated grant missing parent digest", ErrInvalidGrantChain)
	}
	if *child.ParentGrantDigest != parentEnv.SignedDigest {
		return fmt.Errorf("%w: child parent digest mismatch", ErrInvalidGrantChain)
	}
	// Rule 3: delegation link — child issuer is the parent subject and
	// the child's signer key is the parent's subject key.
	if child.Issuer != parent.SubjectPeerID {
		return fmt.Errorf("%w: child issuer is not the parent subject", ErrAuthorityEscalation)
	}
	var childKeyThumb Digest
	if len(childEnv.SignerKey) == ed25519.PublicKeySize {
		childKeyThumb = SubjectKeyThumbprint(childEnv.SignerKey)
	}
	if childKeyThumb != parent.SubjectKeyThumbprint {
		return fmt.Errorf("%w: child signer key does not match parent subject-key thumbprint", ErrAuthorityEscalation)
	}
	// Rule 4: byte-for-byte equal binding fields.
	if child.OrganizationID != parent.OrganizationID ||
		child.UserID != parent.UserID ||
		child.SessionID != parent.SessionID ||
		child.PolicyEpochID != parent.PolicyEpochID ||
		!equalStringSets(child.Audience, parent.Audience) {
		return fmt.Errorf("%w: child changed organization/user/session/epoch/audience", ErrAuthorityEscalation)
	}
	// Rule 5: time containment.
	if child.NotBeforeMs < parent.NotBeforeMs || child.NotAfterMs > parent.NotAfterMs {
		return fmt.Errorf("%w: child extends the parent validity window", ErrAuthorityEscalation)
	}
	if child.NotAfterMs <= child.NotBeforeMs {
		return fmt.Errorf("%w: child has an empty validity window", ErrInvalidGrantChain)
	}
	// Rule 6: depth arithmetic; zero encoded by omission.
	if parent.DelegationDepth == 0 {
		return fmt.Errorf("%w: parent cannot delegate (depth absent or zero)", ErrAuthorityEscalation)
	}
	if child.DelegationDepth != parent.DelegationDepth-1 {
		return fmt.Errorf("%w: child depth is not parent minus one", ErrAuthorityEscalation)
	}
	// Rule 7/8/9: subset semantics.
	if err := validateScopeAttenuation(parent.Scope, child.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthorityEscalation, err)
	}
	return nil
}

// validateScopeAttenuation checks rule 7/8/9: child permissions must
// be a subset (or, for approvals, a superset) of the parent's.
func validateScopeAttenuation(parent, child AuthorizationScope) error {
	if !subsetStrings(parent.ActionClasses, child.ActionClasses) {
		return errors.New("action classes broadened")
	}
	if !subsetStrings(parent.Models, child.Models) {
		return errors.New("models broadened")
	}
	if !subsetStrings(parent.Tools, child.Tools) {
		return errors.New("tools broadened")
	}
	if !subsetStrings(parent.DataClassifications, child.DataClassifications) {
		return errors.New("data classifications broadened")
	}
	if !subsetStrings(parent.ProtectionProfiles, child.ProtectionProfiles) {
		return errors.New("protection profiles weakened")
	}
	// Rule 9: approvals may only be ADDED.
	if !subsetStrings(child.ApprovalClasses, parent.ApprovalClasses) {
		return errors.New("approval requirement removed")
	}
	if !coveredPaths(parent.ReadPaths, child.ReadPaths) {
		return errors.New("read path scope broadened")
	}
	if !coveredPaths(parent.WritePaths, child.WritePaths) {
		return errors.New("write path scope broadened")
	}
	if !coveredNetworks(parent.Networks, child.Networks) {
		return errors.New("network scope broadened")
	}
	// Rule 8: budgets — no new keys, each value ≤ parent.
	for k, v := range child.ResourceBudgets {
		pv, ok := parent.ResourceBudgets[k]
		if !ok {
			return fmt.Errorf("budget key %q introduced", k)
		}
		if v > pv {
			return fmt.Errorf("budget %q raised", k)
		}
	}
	return nil
}

func equalStringSets(a, b []string) bool {
	return len(a) == len(b) && subsetStrings(a, b) && subsetStrings(b, a)
}

// subsetStrings reports whether every element of sub occurs in super.
func subsetStrings(super, sub []string) bool {
	set := make(map[string]struct{}, len(super))
	for _, s := range super {
		set[s] = struct{}{}
	}
	for _, s := range sub {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// coveredPaths applies the F.4 path-coverage rule: a child rule is
// covered only when authority and revision equal the parent's, every
// child operation occurs in the parent operation set, and the child
// prefix equals or extends (with "/") the parent prefix.
func coveredPaths(parent, child []PathScope) bool {
	for _, c := range child {
		covered := false
		for _, p := range parent {
			if c.Authority != p.Authority || c.Revision != p.Revision {
				continue
			}
			if !subsetStrings(p.Operations, c.Operations) {
				continue
			}
			if c.Prefix == p.Prefix || (len(c.Prefix) > len(p.Prefix) && c.Prefix[:len(p.Prefix)] == p.Prefix && c.Prefix[len(p.Prefix)] == '/') {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

// coveredNetworks applies the F.4 network-coverage rule: scheme and
// host equal, child port interval contained by the parent interval,
// purposes a subset.
func coveredNetworks(parent, child []NetworkScope) bool {
	for _, c := range child {
		covered := false
		for _, p := range parent {
			if c.Scheme != p.Scheme || c.Host != p.Host {
				continue
			}
			if c.PortFirst < p.PortFirst || c.PortLast > p.PortLast {
				continue
			}
			if !subsetStrings(p.Purposes, c.Purposes) {
				continue
			}
			covered = true
			break
		}
		if !covered {
			return false
		}
	}
	return true
}

// IssueChildGrant derives a delegated child from a parent per the
// attenuation rules: the child MUST narrow (or keep) scope, shrink (or
// keep) the validity window, keep binding fields identical, decrement
// the remaining depth, and bind its parent digest. The child is signed
// by the PARENT'S SUBJECT key (the delegator) — the caller supplies
// that key; the child's SubjectKeyThumbprint identifies the delegate.
func IssueChildGrant(parent *GrantEnvelope, delegatePeerID string, delegateKeyThumb Digest, scope AuthorizationScope, notBeforeMs, notAfterMs int64, grantID string, sequence uint64, delegatorPriv ed25519.PrivateKey) (*GrantEnvelope, error) {
	pb := parent.Body
	if !pb.HasDelegationDepth(parent.COSE.Payload) || pb.DelegationDepth == 0 {
		return nil, fmt.Errorf("%w: parent cannot delegate", ErrAuthorityEscalation)
	}
	// The child must attenuate.
	if err := validateScopeAttenuation(pb.Scope, scope); err != nil {
		return nil, fmt.Errorf("%w: child scope: %v", ErrAuthorityEscalation, err)
	}
	if notBeforeMs < pb.NotBeforeMs || notAfterMs > pb.NotAfterMs || notAfterMs <= notBeforeMs {
		return nil, fmt.Errorf("%w: child window", ErrAuthorityEscalation)
	}
	d := parent.SignedDigest
	child := &AuthorizationGrantBody{
		Version:              pb.Version,
		GrantID:              grantID,
		Issuer:               pb.SubjectPeerID,
		SubjectPeerID:        delegatePeerID,
		SubjectKeyThumbprint: delegateKeyThumb,
		Audience:             append([]string(nil), pb.Audience...),
		OrganizationID:       pb.OrganizationID,
		UserID:               pb.UserID,
		SessionID:            pb.SessionID,
		PolicyEpochID:        pb.PolicyEpochID,
		Scope:                scope,
		NotBeforeMs:          notBeforeMs,
		NotAfterMs:           notAfterMs,
		IssuerSequence:       sequence,
		ParentGrantDigest:    &d,
	}
	if pb.DelegationDepth-1 > 0 {
		child.DelegationDepth = pb.DelegationDepth - 1
	}
	env, err := SignAuthorizationGrant(child, delegatorPriv)
	if err != nil {
		return nil, err
	}
	// Re-validate the new pair atomically before returning.
	if err := validateDelegationPair(parent, env); err != nil {
		return nil, err
	}
	return env, nil
}
