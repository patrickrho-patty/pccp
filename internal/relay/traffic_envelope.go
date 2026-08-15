package relay

import (
	"crypto/ed25519"
	"os"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

// traffic_envelope.go: the relay signs traffic-class envelopes for the
// scheduler gateway (spec §13.14). The class claim rides COSE-signed
// metadata — the scheduler verifies it against this relay's service key;
// harnesses can never self-assert priority.

var trafficKey struct {
	mu   sync.Mutex
	priv ed25519.PrivateKey
}

// trafficIssuerKey returns the persisted relay signing key for traffic
// envelopes (load-or-create, like the other service identities).
func (s *Service) trafficIssuerKey() (ed25519.PrivateKey, error) {
	trafficKey.mu.Lock()
	defer trafficKey.mu.Unlock()
	if trafficKey.priv != nil {
		return trafficKey.priv, nil
	}
	priv, err := keys.LoadOrCreate(s.db, "relay-traffic-issuer")
	if err != nil {
		return nil, err
	}
	trafficKey.priv = priv
	return priv, nil
}

// signTrafficEnvelope issues a short-lived traffic envelope binding the
// tenant to its authorized class. The default class for a tenant without
// an elevated entitlement is interactive-normal; batch/background are
// downgrades, interactive-paid requires an entitlement check upstream
// (capability lease) — the scheduler's resolver treats absence/invalid
// as batch, so mis-signing here only degrades, never elevates.
func (s *Service) signTrafficEnvelope(tenantID, userID, requestID string) (*scheduler.TrafficEnvelope, error) {
	priv, err := s.trafficIssuerKey()
	if err != nil {
		return nil, err
	}
	class := os.Getenv("PCCP_RELAY_TRAFFIC_CLASS")
	if class == "" {
		class = "interactive-normal"
	}
	// Developer-entitlement cap (web/01 B5): interactive-paid requires
	// the scoped entitlement on the live path — mis-signing here can
	// only degrade, never elevate (scheduler treats invalid as batch).
	if class == "interactive-paid" {
		if userID == "" {
			class = "interactive-normal"
		} else if ok, err := s.identity.EvaluateEntitlement(tenantID, userID, "class:interactive-paid"); err != nil || !ok {
			class = "interactive-normal"
		}
	}
	env := scheduler.NewTrafficEnvelope(requestID, tenantID, class, 2*time.Minute)
	if err := env.Sign(priv); err != nil {
		return nil, err
	}
	return env, nil
}

// TrafficIssuerPublicKey exposes the relay's traffic-issuer public key
// (the scheduler configures this to verify envelopes).
func (s *Service) TrafficIssuerPublicKey() (ed25519.PublicKey, error) {
	priv, err := s.trafficIssuerKey()
	if err != nil {
		return nil, err
	}
	return priv.Public().(ed25519.PublicKey), nil
}
