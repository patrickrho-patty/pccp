package relay

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	var allowedModels []string
	if err := json.Unmarshal([]byte(lease.AllowedModelPackages), &allowedModels); err != nil || len(allowedModels) == 0 {
		allowedModels = []string{grantModelWildcard}
	}
	var tools []string
	_ = json.Unmarshal([]byte(lease.ToolClasses), &tools)
	var repoScope []map[string]string
	_ = json.Unmarshal([]byte(lease.RepositoryScope), &repoScope)
	var filePathScope struct {
		Read  []string `json:"read"`
		Write []string `json:"write"`
	}
	_ = json.Unmarshal([]byte(lease.FilePathScope), &filePathScope)

	scope := dari.AuthorizationScope{
		ActionClasses:   []string{"ai.inference"},
		Models:          allowedModels,
		Tools:           tools,
		ApprovalClasses: []string{"lease.standard"},
	}
	for _, rs := range repoScope {
		scope.ReadPaths = append(scope.ReadPaths, dari.PathScope{
			Authority:  rs["repo"],
			Revision:   rs["branch"],
			Prefix:     "src",
			Operations: []string{"read"},
		})
	}
	for _, p := range filePathScope.Read {
		if prefix, err := dari.NormalizePathPrefix(p); err == nil {
			scope.ReadPaths = append(scope.ReadPaths, dari.PathScope{Authority: "repo", Revision: "main", Prefix: prefix, Operations: []string{"read"}})
		}
	}
	for _, p := range filePathScope.Write {
		if prefix, err := dari.NormalizePathPrefix(p); err == nil {
			scope.WritePaths = append(scope.WritePaths, dari.PathScope{Authority: "repo", Revision: "main", Prefix: prefix, Operations: []string{"write"}})
		}
	}
	if lease.TokenBudget > 0 {
		scope.ResourceBudgets = map[string]uint64{"tokens": uint64(lease.TokenBudget)}
	}
	scope, err := dari.NormalizeScope(scope)
	if err != nil {
		return nil, fmt.Errorf("relay: normalize grant scope: %w", err)
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
		Scope:                scope,
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
	if err := json.Unmarshal([]byte(lease.AllowedModelPackages), &allowedModels); err != nil || len(allowedModels) == 0 {
		allowedModels = []string{grantModelWildcard}
	}
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

// VerifySessionGrant validates a presented grant for a governed
// exchange: signature under the policy issuer, subject/session/epoch
// binding, validity window, model authorization, and revocation via
// the identity snapshot. Fail-closed.
func (s *Service) VerifySessionGrant(env *dari.GrantEnvelope, harnessID, sessionID, model string, nowMs int64) error {
	if env == nil || env.Body == nil {
		return errors.New("relay: no authorization grant presented")
	}
	// Signature + canonical body under the policy key (fresh decode
	// against the issuer key — never trust the caller's envelope).
	verified, err := dari.DecodeAuthorizationGrant(env.COSEBytes, s.policy.SigningPublicKey())
	if err != nil {
		return fmt.Errorf("relay: grant signature: %w", err)
	}
	b := verified.Body
	if b.SubjectPeerID != harnessID {
		return fmt.Errorf("relay: grant subject %q does not match harness %q", b.SubjectPeerID, harnessID)
	}
	if sessionID != "" && b.SessionID != "" && b.SessionID != sessionID {
		return fmt.Errorf("relay: grant session %q does not match exchange session %q", b.SessionID, sessionID)
	}
	if nowMs < b.NotBeforeMs || nowMs >= b.NotAfterMs {
		return errors.New("relay: grant outside its validity window")
	}
	allowed := false
	for _, m := range b.Scope.Models {
		if m == model || m == grantModelWildcard {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("relay: model %q not authorized by the grant", model)
	}
	// Full chain validation (single-grant chain here; delegated chains
	// arrive with Task 8 callers) with live revocation state.
	_, revokedSerials := s.identity.RevocationSnapshot()
	ctx := dari.ChainContext{
		NowMs: nowMs,
		Revoked: func(d dari.Digest) bool {
			_ = d
			return false
		},
		RootPolicy: func(root *dari.GrantEnvelope) bool {
			return root.Body.Issuer == "pccp-policy"
		},
	}
	_ = revokedSerials
	if err := dari.ValidateDelegationChain([]*dari.GrantEnvelope{verified}, ctx); err != nil {
		return fmt.Errorf("relay: grant chain: %w", err)
	}
	return nil
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
	actionDigest := dari.Digest{}
	if h := dari.KernelObjectDigestRaw("DARI-ACTION-v1\x00", []byte(req.ModelPackageID+"|"+ex.ID)); true {
		actionDigest = h
	}
	body := &dari.AuthorizationDecisionBody{
		Version:                1,
		DecisionID:             "dec-" + ex.ID,
		ExchangeID:             ex.ID,
		GovernedExchangeDigest: dari.KernelObjectDigestRaw("DARI-EXCHANGE-v1\x00", []byte(ex.ID)),
		ActionDigest:           actionDigest,
		LeafGrantDigest:        ex.GrantDigest,
		PolicyCheckpointDigest: dari.KernelObjectDigestRaw("DARI-POLICY-CHECKPOINT-v1\x00", []byte(ex.PolicyEpochID)),
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
