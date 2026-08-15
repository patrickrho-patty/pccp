package scheduler

import (
	"testing"
	"time"
)

func TestSchedulerUpdateRevocations(t *testing.T) {
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))

	res := s.Admit(AdmissionRequest{Card: fx.card, PPC: fx.cred, Config: fx.config, Now: time.Now()})
	if res.Outcome != OutcomeAdmitted {
		t.Fatalf("pre-revocation outcome %s (%s)", res.Outcome, res.Reason)
	}

	// Revocation feed sync must flip the same request to denied.
	s.UpdateRevocations([]string{fx.cred.Serial}, nil)
	res = s.Admit(AdmissionRequest{Card: fx.card, PPC: fx.cred, Config: fx.config, Now: time.Now()})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("post-revocation outcome %s, want denied (%s)", res.Outcome, res.Reason)
	}
}

func TestSchedulerUpdateRevocationsByPeerID(t *testing.T) {
	fx := newWorkerFixture(t)
	s := NewScheduler(fx.trust, nil, 30*time.Second, 60*time.Second, testEvidenceKey(t))

	s.UpdateRevocations(nil, []string{fx.cred.SubjectPeerID})
	res := s.Admit(AdmissionRequest{Card: fx.card, PPC: fx.cred, Config: fx.config, Now: time.Now()})
	if res.Outcome != OutcomeDenied {
		t.Fatalf("outcome %s, want denied", res.Outcome)
	}
}
