package scheduler

import (
	"crypto/ed25519"
	"time"
)

// Scheduler is the composition root of the pccp-scheduler process: registry,
// admission ladder, and evidence log. The DARI listener feeds it; the HTTP
// API reads it (S1.5); the router (S3) will consume the registry.
type Scheduler struct {
	Registry  *Registry
	Admission *Admission
	Evidence  *EvidenceLog
}

// NewScheduler assembles the S1 scheduler with the given trust material,
// policy source, lease parameters, and evidence signing key.
func NewScheduler(trust Trust, policy PolicySource, ttl, grace time.Duration, evidenceKey ed25519.PrivateKey) *Scheduler {
	return &Scheduler{
		Registry:  NewRegistry(ttl, grace),
		Admission: NewAdmission(trust, NewRevocationStore(), policy),
		Evidence:  NewEvidenceLog(evidenceKey),
	}
}

// Admit runs the admission ladder for a registration/heartbeat request.
func (s *Scheduler) Admit(req AdmissionRequest) AdmissionResult {
	return s.Admission.Admit(req)
}

// UpdateRevocations applies a revocation-feed refresh (serials and peer IDs)
// from the Control Plane.
func (s *Scheduler) UpdateRevocations(serials, peerIDs []string) {
	s.Admission.revoked.Replace(serials, peerIDs)
}

// Sweep evicts expired workers and emits evidence for each eviction.
func (s *Scheduler) Sweep(now time.Time) []string {
	evicted := s.Registry.Sweep(now)
	for _, id := range evicted {
		s.Evidence.Emit(EventWorkerEvict, id, "lease expired")
	}
	return evicted
}
