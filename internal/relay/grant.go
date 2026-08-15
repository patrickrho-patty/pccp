package relay

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
)

// grant.go wires the DARI Authorization Grant into the live governed
// path (master plan Task 7 steps 3-4). The relay issues a signed root
// grant at session setup, carries it on SESSION_GRANT (dari/1), and
// GovernInference validates the presented grant — falling back to the
// explicit legacy-lease adapter during the compatibility window.

// grantModelWildcard authorizes any model in the session grant when
// the lease's allow-list carried "*". A named model is always
// narrower, so the wildcard root still attenuates correctly.
const grantModelWildcard = "*"

// IssueSessionGrant builds and signs the session's ROOT Authorization
// Grant from an issued capability lease. The policy signing key is the
// issuer; the harness's verified credential key is the subject.
func (s *Service) IssueSessionGrant(lease *models.CapabilityLease, harnessPub ed25519.PublicKey) (*dari.GrantEnvelope, error) {
	if lease == nil {
		return nil, errors.New("relay: nil lease for grant issuance")
	}
	subject := lease.SubjectPeerID
	if subject == "" {
		return nil, errors.New("relay: lease carries no subject peer")
	}

	scope, err := parseLeaseScope(lease)
	if err != nil {
		return nil, err
	}
	allowedModels := scope.AllowedModels
	if len(allowedModels) == 0 {
		return nil, errors.New("relay: lease carries no allowed models — refusing to issue a wildcard grant")
	}
	tools := scope.Tools
	repoScope := scope.RepoScope
	filePathScope := struct {
		Read  []string
		Write []string
	}{scope.ReadPaths, scope.WritePaths}

	grantScope := dari.AuthorizationScope{
		ActionClasses:   []string{"ai.inference"},
		Models:          allowedModels,
		Tools:           tools,
		ApprovalClasses: []string{"lease.standard"},
	}
	for _, rs := range repoScope {
		grantScope.ReadPaths = append(grantScope.ReadPaths, dari.PathScope{
			Authority:  rs["repo"],
			Revision:   rs["branch"],
			Prefix:     "src",
			Operations: []string{"read"},
		})
	}
	for _, p := range filePathScope.Read {
		if prefix, err := dari.NormalizePathPrefix(p); err == nil {
			grantScope.ReadPaths = append(grantScope.ReadPaths, dari.PathScope{Authority: "repo", Revision: "main", Prefix: prefix, Operations: []string{"read"}})
		}
	}
	for _, p := range filePathScope.Write {
		if prefix, err := dari.NormalizePathPrefix(p); err == nil {
			grantScope.WritePaths = append(grantScope.WritePaths, dari.PathScope{Authority: "repo", Revision: "main", Prefix: prefix, Operations: []string{"write"}})
		}
	}
	if lease.TokenBudget > 0 {
		grantScope.ResourceBudgets = map[string]uint64{"tokens": uint64(lease.TokenBudget)}
	}
	grantScope, nerr := dari.NormalizeScope(grantScope)
	if nerr != nil {
		return nil, fmt.Errorf("relay: normalize grant scope: %w", nerr)
	}

	nb, _ := time.Parse(time.RFC3339, lease.NotBefore)
	na, _ := time.Parse(time.RFC3339, lease.NotAfter)
	body := &dari.AuthorizationGrantBody{
		Version:              1,
		GrantID:              "grant-" + lease.LeaseID,
		Issuer:               "pccp-policy",
		SubjectPeerID:        subject,
		SubjectKeyThumbprint: dari.SubjectKeyThumbprint(harnessPub),
		Audience:             []string{s.relayID},
		OrganizationID:       lease.OrganizationID,
		UserID:               lease.UserID,
		SessionID:            lease.SessionID,
		PolicyEpochID:        lease.PolicyEpochID,
		Scope:                grantScope,
		NotBeforeMs:          nb.UnixMilli(),
		NotAfterMs:           na.UnixMilli(),
		IssuerSequence:       lease.LeaseSequence,
		// Session grants delegate to sub-agents in governed flows.
		DelegationDepth: 3,
	}
	return dari.SignAuthorizationGrant(body, s.policy.SigningPrivateKey())
}

// DecodeLegacyCapabilityLease adapts a persisted legacy lease row into
// a NON-DELEGABLE DARI grant view (compat map §7): depth zero, exact
// audience/session/epoch, no authority beyond what the legacy bytes
// covered. Callers MUST revalidate scope fields from authoritative
// state and record the conversion in evidence when re-signing.
func DecodeLegacyCapabilityLease(lease *models.CapabilityLease) (*dari.AuthorizationGrantBody, error) {
	if lease == nil {
		return nil, errors.New("relay: nil legacy lease")
	}
	var allowedModels []string
	if err := json.Unmarshal([]byte(lease.AllowedModelPackages), &allowedModels); err != nil {
		return nil, errors.New("relay: lease allowed-models column unparseable — refusing a grant view")
	}
	// An empty legacy allow-list yields an EMPTY grant view (grants
	// nothing) — never a wildcard.
	nb, nbe := time.Parse(time.RFC3339, lease.NotBefore)
	na, nae := time.Parse(time.RFC3339, lease.NotAfter)
	if nbe != nil || nae != nil {
		return nil, errors.New("relay: legacy lease has malformed validity window")
	}
	scope, err := dari.NormalizeScope(dari.AuthorizationScope{
		ActionClasses:   []string{"ai.inference"},
		Models:          allowedModels,
		ApprovalClasses: []string{"lease.legacy"},
	})
	if err != nil {
		return nil, err
	}
	return &dari.AuthorizationGrantBody{
		Version:         1,
		GrantID:         "grant-legacy-" + lease.LeaseID,
		Issuer:          "pccp-policy",
		SubjectPeerID:   lease.SubjectPeerID,
		Audience:        []string{"legacy"},
		OrganizationID:  lease.OrganizationID,
		UserID:          lease.UserID,
		SessionID:       lease.SessionID,
		PolicyEpochID:   lease.PolicyEpochID,
		Scope:           scope,
		NotBeforeMs:     nb.UnixMilli(),
		NotAfterMs:      na.UnixMilli(),
		IssuerSequence:  lease.LeaseSequence,
		DelegationDepth: 0, // non-delegable view (spec §7 table)
	}, nil
}

// revokedGrants is the revoked-grant-digest registry backing
// VerifySessionGrant. A grant revocation terminates the grant and
// every descendant (F.4).
type revokedGrants struct {
	mu sync.Mutex
	m  map[dari.Digest]bool
}

func (r *revokedGrants) add(d dari.Digest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[d] = true
}

func (r *revokedGrants) has(d dari.Digest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[d]
}

// policyAuthority resolves the policy issuer's verification key.
type policyAuthority struct{ key ed25519.PublicKey }

func (p policyAuthority) IssuerKey(string) (ed25519.PublicKey, bool) { return p.key, true }

// VerifySessionGrant validates a presented grant for a governed
// exchange: signature + validity window under the policy issuer
// (dari.VerifyGrantAuthority), subject/session/audience binding, model
// authorization (either the model_id or the resolved package id may
// appear in scope), and chain validation with the live revocation
// registry. Fail-closed at every step.
func (s *Service) VerifySessionGrant(env *dari.GrantEnvelope, harnessID, sessionID string, model string, nowMs int64) error {
	return s.VerifySessionGrantFor(env, harnessID, sessionID, []string{model}, nowMs)
}

// VerifySessionGrantFor is VerifySessionGrant accepting the set of
// authorized identifiers (model_id and/or package_id) so the grant is
// crypto-verified exactly once per exchange.
func (s *Service) VerifySessionGrantFor(env *dari.GrantEnvelope, harnessID, sessionID string, authorizedNames []string, nowMs int64) error {
	if env == nil || env.Body == nil {
		return errors.New("relay: no authorization grant presented")
	}
	// Signature + validity window under the policy key (fresh decode —
	// the caller's envelope is never trusted as-is).
	if err := dari.VerifyGrantAuthority(env, policyAuthority{key: s.policy.SigningPublicKey()}, nowMs); err != nil {
		return fmt.Errorf("relay: grant authority: %w", err)
	}
	b := env.Body
	if b.SubjectPeerID != harnessID {
		return fmt.Errorf("relay: grant subject %q does not match harness %q", b.SubjectPeerID, harnessID)
	}
	if sessionID != "" && b.SessionID == "" {
		return errors.New("relay: grant carries no session binding")
	}
	if b.SessionID != "" && sessionID != "" && b.SessionID != sessionID {
		return fmt.Errorf("relay: grant session %q does not match exchange session %q", b.SessionID, sessionID)
	}
	// Audience: this relay must be an intended audience (F.4 rule 10).
	audienceOK := false
	for _, a := range b.Audience {
		if a == s.relayID {
			audienceOK = true
			break
		}
	}
	if !audienceOK {
		return fmt.Errorf("relay: grant audience does not include this relay (%s)", s.relayID)
	}
	allowed := false
	for _, name := range authorizedNames {
		if name == "" {
			continue
		}
		for _, m := range b.Scope.Models {
			if m == name || m == grantModelWildcard {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return fmt.Errorf("relay: model %v not authorized by the grant", authorizedNames)
	}
	// Chain validation with the live revocation registry.
	if s.grantRevocations.has(env.SignedDigest) {
		return errors.New("relay: authorization grant revoked")
	}
	ctx := dari.ChainContext{
		NowMs: nowMs,
		Revoked: func(d dari.Digest) bool {
			return s.grantRevocations.has(d)
		},
		RootPolicy: func(root *dari.GrantEnvelope) bool {
			return root.Body.Issuer == "pccp-policy"
		},
	}
	if err := dari.ValidateDelegationChain([]*dari.GrantEnvelope{env}, ctx); err != nil {
		return fmt.Errorf("relay: grant chain: %w", err)
	}
	return nil
}

// RevokeGrant revokes a session grant by signed-object digest (and,
// per F.4, every descendant bound through it).
func (s *Service) RevokeGrant(digest dari.Digest) {
	s.grantRevocations.add(digest)
}

// GrantHexForWire renders a grant envelope for a JSON message field.
func GrantHexForWire(env *dari.GrantEnvelope) string {
	if env == nil {
		return ""
	}
	return hex.EncodeToString(env.COSEBytes)
}

// issueDecision signs and attaches the F.6 Authorization Decision for
// an exchange outcome (Task 9). The decision is immutable once signed;
// denial carries a stable reason code and no obligations.
func (s *Service) issueDecision(ex *Exchange, req OpenExchangeRequest, outcome dari.DecisionOutcome, reason string) {
	actionBody, _ := dari.MarshalCBOR(struct {
		ExchangeID string `cbor:"1,keyasint"`
		Model      string `cbor:"2,keyasint"`
	}{ex.ID, req.ModelPackageID})
	actionDigest := dari.KernelObjectDigestRaw("DARI-ACTION-v1\x00", actionBody)
	body := &dari.AuthorizationDecisionBody{
		Version:                1,
		DecisionID:             "dec-" + ex.ID,
		ExchangeID:             ex.ID,
		GovernedExchangeDigest: exchangeBindingDigest(ex),
		ActionDigest:           actionDigest,
		LeafGrantDigest:        ex.GrantDigest,
		PolicyCheckpointDigest: policyEpochBindingDigest(ex.PolicyEpochID),
		EvaluatorPeerID:        s.relayID,
		Outcome:                outcome,
		IssuedAtMs:             time.Now().UnixMilli(),
		ExpiresAtMs:            time.Now().Add(time.Hour).UnixMilli(),
	}
	if reason != "" {
		body.ReasonCodes = []string{reason}
	}
	env, err := dari.SignAuthorizationDecision(body, s.policy.SigningPrivateKey())
	if err != nil {
		log.Printf("relay: warning: sign decision for %s: %v", ex.ID, err)
		return
	}
	ex.Decision = env
	s.mu.Lock()
	s.decisionLog = append(s.decisionLog, env)
	if len(s.decisionLog) > 256 {
		s.decisionLog = s.decisionLog[len(s.decisionLog)-256:]
	}
	s.mu.Unlock()
}

// RecentDecisions returns the bounded log of signed decisions.
func (s *Service) RecentDecisions() []*dari.DecisionEnvelope {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*dari.DecisionEnvelope(nil), s.decisionLog...)
}

// denyWithoutExchange signs a DENY decision for a refusal that occurs
// before a governed exchange exists (unknown model, closed session).
// The stub exchange carries the request's binding fields so the
// decision is still attributable and evidenced (F.6).
func (s *Service) denyWithoutExchange(req GovernRequest, orgID, reason string) {
	stub := &Exchange{
		ID: dari.GenerateID("exch"), SessionID: req.SessionID,
		OrganizationID: orgID, HarnessID: req.HarnessID,
		State: dari.ExchangeDenied, Verdict: dari.VerdictDeny,
	}
	s.issueDecision(stub, OpenExchangeRequest{
		OrganizationID: orgID, SessionID: req.SessionID, HarnessID: req.HarnessID,
	}, dari.DecisionDeny, reason)
}

// exchangeBindingDigest digests the governed-exchange binding body
// (F.5) rather than a bare ID string.
func exchangeBindingDigest(ex *Exchange) dari.Digest {
	body, _ := dari.MarshalCBOR(struct {
		ExchangeID string `cbor:"1,keyasint"`
		SessionID  string `cbor:"2,keyasint"`
	}{ex.ID, ex.SessionID})
	return dari.KernelObjectDigest(dari.ObjTypeGovernedExchange, body)
}

// policyEpochBindingDigest digests the epoch binding body.
func policyEpochBindingDigest(epochID string) dari.Digest {
	body, _ := dari.MarshalCBOR(struct {
		EpochID string `cbor:"1,keyasint"`
	}{epochID})
	return dari.KernelObjectDigestRaw("DARI-POLICY-CHECKPOINT-v1\x00", body)
}
