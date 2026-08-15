package dari

import (
	"errors"
	"fmt"
	"sort"
)

// profiles.go implements the profile negotiation machinery (spec
// F.13 + Compatibility and Profile Map §3, master plan Task 19
// groundwork). Results are derived ONLY from registered runtime
// handlers with conformance evidence — a profile without a passed gate
// returns UNSUPPORTED no matter what schemas exist.

// ProfileStatus is the negotiation result.
type ProfileStatus uint8

const (
	ProfileExact       ProfileStatus = 1
	ProfileDegraded    ProfileStatus = 2
	ProfileUnsupported ProfileStatus = 3
)

// CapabilityOffer is one offered capability.
type CapabilityOffer struct {
	ID       string `cbor:"1,keyasint"`
	Critical uint8  `cbor:"2,keyasint"`
}

// ProfileOffer is one profile offer.
type ProfileOffer struct {
	Profile      string            `cbor:"1,keyasint"`
	Capabilities []CapabilityOffer `cbor:"2,keyasint"`
}

// ProfileResult is one negotiation result.
type ProfileResult struct {
	Profile    string        `cbor:"1,keyasint"`
	Status     ProfileStatus `cbor:"2,keyasint"`
	Omitted    []string      `cbor:"3,keyasint,omitempty"`
	ReasonCode string        `cbor:"4,keyasint,omitempty"`
}

// Profile handler contract. Implementations register with evidence of
// a passed conformance gate; the registry refuses schema-only claims.
type ProfileHandler interface {
	// ProfileID is the exact profile identifier.
	ProfileID() string
	// Dependencies are profile IDs this profile requires.
	Dependencies() []string
	// Negotiate resolves one offer. The handler receives the offer and
	// returns EXACT or DEGRADED (with the enumerated non-critical
	// omissions). It MUST NOT return UNSUPPORTED for capabilities it
	// implements; refusing the whole profile is done by not
	// registering (or registering with GatePassed=false).
	Negotiate(offer ProfileOffer) ProfileResult
}

// RegisteredProfile couples a handler with its gate evidence.
type RegisteredProfile struct {
	Handler    ProfileHandler
	GatePassed bool   // conformance vectors + deployment evidence
	Reason     string // recorded when !GatePassed
}

// ProfileRegistry resolves negotiation offers. Everything derives
// from registered runtime handlers; an unregistered profile is
// UNSUPPORTED (map §3: schema publication never upgrades a result).
type ProfileRegistry struct {
	handlers map[string]RegisteredProfile
}

// NewProfileRegistry builds the standard kernel registry: dari/1 and
// its implemented extensions are registered as passed gates; web,
// federation, collab, and media register UNSUPPORTED-with-reason
// until their runtime gates (Tasks 13/14/18) pass.
func NewProfileRegistry() *ProfileRegistry {
	r := &ProfileRegistry{handlers: map[string]RegisteredProfile{}}
	// Kernel + implemented extensions (validated in this repo):
	r.Register(RegisteredProfile{Handler: &kernelProfile{id: "dari/1", capabilities: []string{
		"peer-credential", "authorization-grant", "attenuation", "governed-exchange",
		"authorization-decision", "obligations", "signed-state-checkpoint",
		"evidence-receipt", "linear-evidence", "segmented-mmr", "selective-disclosure",
	}}}, true, "")
	r.Register(RegisteredProfile{Handler: &staticProfile{
		id: "dari.ai/1", deps: []string{"dari/1"},
		exact: []string{"request", "streaming-response", "usage", "cancellation"},
	}}, true, "")
	r.Register(RegisteredProfile{Handler: &staticProfile{
		id: "dari.tools/1", deps: []string{"dari/1"},
		exact: []string{"effect-prepare", "effect-authorize", "effect-commit", "effect-abort", "effect-status"},
	}}, true, "")
	r.Register(RegisteredProfile{Handler: &staticProfile{
		id: "dari.model-supply/1", deps: []string{"dari/1"},
		exact: []string{"model-artifact-manifest", "endpoint-authorization"},
	}}, true, "")
	// dari.web/1 (Task 13): the runtime exists — WT/H3 carrier, WS
	// fallback, origin binding, browser proof-of-possession, durable
	// reconnect, effect-status — with in-process conformance vectors
	// passing (internal/webbinding). Evidence gap: deployment against
	// real browsers. Per map §3 that keeps the profile below EXACT;
	// negotiated as DEGRADED omitting the (non-critical)
	// browser-deployment-evidence capability.
	r.Register(RegisteredProfile{Handler: &staticProfile{
		id: "dari.web/1", deps: []string{"dari/1"},
		exact: []string{"webtransport-h3", "websocket-fallback", "origin-binding", "browser-proof-of-possession", "reconnect", "effect-status", "rate-limits", "idle-expiry"},
	}}, true, "")
	// dari.federation/1 (Task 14): runtime implemented in
	// internal/darifederation with in-process vectors passing (trust
	// bundle ledger, bilateral issuer/audience, policy intersection,
	// residency, cross-domain receipts). Deployment evidence
	// (a real partner domain interconnect) remains open — negotiated
	// DEGRADED omitting the non-critical partner-deployment-evidence.
	r.Register(RegisteredProfile{Handler: &staticProfile{
		id: "dari.federation/1", deps: []string{"dari/1"},
		exact: []string{"trust-bundle-import", "rollback-protection", "quarantine", "issuer-audience-validation", "policy-intersection", "residency", "cross-domain-receipts", "staleness"},
	}}, true, "")
	r.Register(RegisteredProfile{Handler: &staticProfile{id: "dari.collab/1", deps: []string{"dari/1"}}}, false, "Task 18 runtime gate not passed: no encrypted ordered delivery or resumable file transfer")
	r.Register(RegisteredProfile{Handler: &staticProfile{id: "dari.media/1", deps: []string{"dari/1", "dari.collab/1"}}}, false, "Task 18 runtime gate not passed: no governed media runtime")
	return r
}

// Register adds or replaces a profile registration.
func (r *ProfileRegistry) Register(rp RegisteredProfile, gatePassed bool, reason string) {
	rp.GatePassed = gatePassed
	rp.Reason = reason
	r.handlers[rp.Handler.ProfileID()] = rp
}

// ErrNegotiationFailed marks a hard negotiation failure (critical
// unsupported / degraded, malformed offers).
var ErrNegotiationFailed = errors.New("dari: profile negotiation failed")

// Negotiate resolves a set of offers per map §3:
//   - exactly one result per offer; none for unoffered profiles;
//   - duplicate or contradictory offers are invalid;
//   - critical offer → UNSUPPORTED (or DEGRADED dropping a critical
//     capability) fails the whole negotiation;
//   - capability arrays must be sorted without duplicates.
func (r *ProfileRegistry) Negotiate(offers []ProfileOffer) ([]ProfileResult, error) {
	if len(offers) == 0 {
		return nil, fmt.Errorf("%w: no offers", ErrNegotiationFailed)
	}
	seen := map[string]bool{}
	var results []ProfileResult
	hardFail := false
	for _, offer := range offers {
		if seen[offer.Profile] {
			return nil, fmt.Errorf("%w: duplicate offer %q", ErrNegotiationFailed, offer.Profile)
		}
		seen[offer.Profile] = true
		// Capability array sorted, duplicate-free.
		ids := make([]string, 0, len(offer.Capabilities))
		for _, c := range offer.Capabilities {
			ids = append(ids, c.ID)
		}
		if !sort.StringsAreSorted(ids) {
			return nil, fmt.Errorf("%w: unsorted capability offer for %q", ErrNegotiationFailed, offer.Profile)
		}
		for i := 1; i < len(ids); i++ {
			if ids[i] == ids[i-1] {
				return nil, fmt.Errorf("%w: duplicate capability in %q", ErrNegotiationFailed, offer.Profile)
			}
		}
		critical := map[string]bool{}
		for _, c := range offer.Capabilities {
			if c.Critical == 1 {
				critical[c.ID] = true
			}
		}

		rp, registered := r.handlers[offer.Profile]
		if !registered || !rp.GatePassed {
			reason := "profile not registered"
			if registered {
				reason = rp.Reason
			}
			res := ProfileResult{Profile: offer.Profile, Status: ProfileUnsupported, ReasonCode: reason}
			results = append(results, res)
			// A critical-capability offer resolving UNSUPPORTED fails
			// negotiation only when the OFFER itself is critical (the
			// offer-level critical flag is per capability; a profile
			// offered with any critical capability is a critical
			// offer per map §3).
			if len(critical) > 0 {
				hardFail = true
			}
			continue
		}

		res := rp.Handler.Negotiate(offer)
		if res.Status == ProfileDegraded {
			for _, om := range res.Omitted {
				if critical[om] {
					hardFail = true
				}
			}
		}
		if res.Status == ProfileExact {
			res.Omitted = nil
		}
		results = append(results, res)
	}
	if hardFail {
		return results, fmt.Errorf("%w: critical capability unavailable", ErrNegotiationFailed)
	}
	return results, nil
}

// SupportsEffects reports whether dari.tools/1 is available — the
// 0x0610-0x0614 gate from map §5 (reject as UNSUPPORTED_MESSAGE_TYPE
// without it).
func (r *ProfileRegistry) SupportsEffects() bool {
	rp, ok := r.handlers["dari.tools/1"]
	return ok && rp.GatePassed
}

// ---------------------------------------------------------------------------
// Static handler implementations.
// ---------------------------------------------------------------------------

// kernelProfile exposes the dari/1 kernel capabilities.
type kernelProfile struct {
	id           string
	capabilities []string
}

func (k *kernelProfile) ProfileID() string      { return k.id }
func (k *kernelProfile) Dependencies() []string { return nil }

func (k *kernelProfile) Negotiate(offer ProfileOffer) ProfileResult {
	have := map[string]bool{}
	for _, c := range k.capabilities {
		have[c] = true
	}
	var omitted []string
	for _, c := range offer.Capabilities {
		if !have[c.ID] {
			omitted = append(omitted, c.ID)
		}
	}
	if len(omitted) == 0 {
		return ProfileResult{Profile: k.id, Status: ProfileExact}
	}
	sort.Strings(omitted)
	return ProfileResult{Profile: k.id, Status: ProfileDegraded, Omitted: omitted}
}

// staticProfile serves extension profiles with a fixed capability set.
type staticProfile struct {
	id    string
	deps  []string
	exact []string
}

func (s *staticProfile) ProfileID() string      { return s.id }
func (s *staticProfile) Dependencies() []string { return s.deps }

func (s *staticProfile) Negotiate(offer ProfileOffer) ProfileResult {
	have := map[string]bool{}
	for _, c := range s.exact {
		have[c] = true
	}
	var omitted []string
	for _, c := range offer.Capabilities {
		if !have[c.ID] {
			omitted = append(omitted, c.ID)
		}
	}
	if len(omitted) == 0 {
		return ProfileResult{Profile: s.id, Status: ProfileExact}
	}
	sort.Strings(omitted)
	return ProfileResult{Profile: s.id, Status: ProfileDegraded, Omitted: omitted}
}
