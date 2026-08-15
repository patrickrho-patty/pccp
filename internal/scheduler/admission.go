package scheduler

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
)

// Outcome is the result of the admission ladder.
type Outcome string

const (
	OutcomeAdmitted    Outcome = "admitted"
	OutcomeQuarantined Outcome = "quarantined"
	OutcomeDenied      Outcome = "denied"
)

// AdmissionResult carries the ladder outcome plus the tenant binding that
// config authorization resolved (for registry/policy bookkeeping).
type AdmissionResult struct {
	Outcome  Outcome
	Reason   string
	TenantID string
}

// Trust is the scheduler's offline trust material: PPC issuer public keys and
// the CP config-signing key. The fleet keeps admitting during CP outages
// because verification is local.
type Trust struct {
	Issuers      map[string]ed25519.PublicKey // issuer ID → public key
	ConfigPubKey ed25519.PublicKey
	Now          func() time.Time
}

// PolicySource is the tenant policy gate (rung 5). Implementations back onto
// the Control Plane policy service.
type PolicySource interface {
	// MinReachability returns the tenant's minimum required backend grade
	// and whether a policy exists for the tenant.
	MinReachability(tenantID string) (string, bool)
}

// RevocationStore is a synced view of revoked PPC serials and peer IDs. The
// scheduler refreshes it from the CP revocation feed.
type RevocationStore struct {
	mu      sync.RWMutex
	serials map[string]bool
	peerIDs map[string]bool
}

// NewRevocationStore creates an empty revocation store.
func NewRevocationStore() *RevocationStore {
	return &RevocationStore{
		serials: make(map[string]bool),
		peerIDs: make(map[string]bool),
	}
}

// RevokeSerial marks a credential serial as revoked.
func (r *RevocationStore) RevokeSerial(serial string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serials[serial] = true
}

// RevokePeer marks a subject peer ID as revoked.
func (r *RevocationStore) RevokePeer(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peerIDs[peerID] = true
}

// Replace swaps the full revocation view (feed refresh).
func (r *RevocationStore) Replace(serials, peerIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serials = make(map[string]bool, len(serials))
	for _, s := range serials {
		r.serials[s] = true
	}
	r.peerIDs = make(map[string]bool, len(peerIDs))
	for _, p := range peerIDs {
		r.peerIDs[p] = true
	}
}

// IsRevoked reports whether a credential serial or subject peer is revoked.
func (r *RevocationStore) IsRevoked(serial, peerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serials[serial] || r.peerIDs[peerID]
}

// Admission implements the five-rung admission ladder (DARI scheduler §5).
// Rungs 1–4 fail = deny; rung 5 fails = quarantine.
type Admission struct {
	trust   Trust
	revoked *RevocationStore
	policy  PolicySource
}

// AdmissionRequest is one registration/heartbeat admission attempt.
type AdmissionRequest struct {
	Card   WorkerCard
	PPC    *dari.PeerCredential
	Config *SignedConfig
	Now    time.Time
}

type emptyPolicy struct{}

func (emptyPolicy) MinReachability(string) (string, bool) { return "", false }

// NewAdmission builds the ladder with the given trust material, revocation
// view, and policy source.
func NewAdmission(trust Trust, revoked *RevocationStore, policy PolicySource) *Admission {
	if trust.Now == nil {
		trust.Now = time.Now
	}
	if revoked == nil {
		revoked = NewRevocationStore()
	}
	if policy == nil {
		policy = emptyPolicy{}
	}
	return &Admission{trust: trust, revoked: revoked, policy: policy}
}

// Admit runs the ladder and returns the outcome.
func (a *Admission) Admit(req AdmissionRequest) AdmissionResult {
	now := req.Now
	if now.IsZero() {
		now = a.trust.Now()
	}
	deny := func(reason string) AdmissionResult {
		return AdmissionResult{Outcome: OutcomeDenied, Reason: reason}
	}

	// Rung 1–3: PPC chain and card binding.
	cred, err := a.verifyPPC(req.PPC, now)
	if err != nil {
		return deny("ppc: " + err.Error())
	}
	if a.revoked.IsRevoked(cred.Serial, cred.SubjectPeerID) {
		return deny("ppc: revoked")
	}
	if req.Card.WorkerID != cred.SubjectPeerID {
		return deny("card: worker_id does not match PPC subject")
	}
	if err := req.Card.Verify(ed25519.PublicKey(cred.PublicKey)); err != nil {
		return deny("card: " + err.Error())
	}

	// Rung 4: signed config authorizes the deployment.
	if req.Config == nil {
		return deny("config: missing")
	}
	if err := req.Config.Verify(a.trust.ConfigPubKey); err != nil {
		return deny("config: " + err.Error())
	}
	if err := req.Config.Authorizes(req.Card); err != nil {
		return deny("config: " + err.Error())
	}
	tenantID := req.Config.Config.TenantID

	// Rung 5: tenant policy gate → quarantine (admitted, visible, non-compliant).
	quarantine := func(reason string) AdmissionResult {
		return AdmissionResult{Outcome: OutcomeQuarantined, Reason: reason, TenantID: tenantID}
	}
	if required, ok := a.policy.MinReachability(tenantID); ok {
		if gradeStrength(req.Card.MeasuredGrade) < gradeStrength(required) {
			return quarantine(fmt.Sprintf("policy: measured grade %q below tenant minimum %q", req.Card.MeasuredGrade, required))
		}
	}
	if req.Card.MeasuredGrade != req.Card.ReachabilityMode {
		return quarantine(fmt.Sprintf("policy: measured grade %q does not match configured mode %q", req.Card.MeasuredGrade, req.Card.ReachabilityMode))
	}

	return AdmissionResult{Outcome: OutcomeAdmitted, TenantID: tenantID}
}

// verifyPPC checks presence, issuer trust, COSE signature, body binding, and
// the validity window of the presented peer credential.
func (a *Admission) verifyPPC(cred *dari.PeerCredential, now time.Time) (*dari.PeerCredential, error) {
	if cred == nil {
		return nil, errors.New("missing PPC")
	}
	if len(cred.SignedCredential) == 0 {
		return nil, errors.New("unsigned PPC")
	}
	issuerKey, ok := a.trust.Issuers[cred.Issuer]
	if !ok {
		return nil, fmt.Errorf("unknown issuer %q", cred.Issuer)
	}
	sign1, err := dari.DecodeCOSESign1(cred.SignedCredential)
	if err != nil {
		return nil, fmt.Errorf("decode credential: %w", err)
	}
	if err := dari.VerifyCOSESign1(sign1, issuerKey); err != nil {
		return nil, fmt.Errorf("credential signature: %w", err)
	}
	body, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		return nil, fmt.Errorf("credential body: %w", err)
	}
	if body.Serial != cred.Serial || body.SubjectPeerID != cred.SubjectPeerID {
		return nil, errors.New("credential body does not match presented credential")
	}
	if now.UnixMilli() < cred.NotBefore || now.UnixMilli() > cred.NotAfter {
		return nil, errors.New("credential outside validity window")
	}
	return cred, nil
}

// gradeStrength orders backend reachability grades. Weaker → lower.
func gradeStrength(grade string) int {
	switch grade {
	case "localhost":
		return 3
	case "mtls":
		return 2
	case "private":
		return 1
	default: // exposed, unknown, empty
		return 0
	}
}
