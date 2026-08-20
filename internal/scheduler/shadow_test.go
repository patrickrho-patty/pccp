package scheduler

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

// stubRouter is a scripted Router double for shadow/replay tests.
type stubRouter struct {
	version  string
	decision RouteDecision
	err      error
	panics   bool
	calls    int
}

func (s *stubRouter) Version() string { return s.version }

func (s *stubRouter) Route(RouteRequest) (RouteDecision, error) {
	s.calls++
	if s.panics {
		panic("boom")
	}
	return s.decision, s.err
}

// shadowBaseline builds a one-worker baseline router with receipts.
func shadowBaseline(t *testing.T) (*CostRouter, *ReceiptStore) {
	t.Helper()
	r := NewCostRouter(DefaultRouterConfig())
	rs := NewReceiptStore(8)
	r.SetReceipts(rs)
	e := mkWorker("w1", "model-a", 4)
	r.UpsertWorker(e, RouterWorkerState{})
	return r, rs
}

func shadowRequest() RouteRequest {
	return RouteRequest{Model: "model-a", Namespace: "tenant-1", InputTokens: 100, ExpectedOutputTokens: 50}
}

func TestShadowAgreementAndVersions(t *testing.T) {
	r, rs := shadowBaseline(t)
	cand := &stubRouter{version: "candidate/v9", decision: RouteDecision{WorkerID: "w1", Cost: 42}}
	r.SetShadow(cand)

	dec, err := r.Route(shadowRequest())
	if err != nil {
		t.Fatal(err)
	}
	if dec.WorkerID != "w1" {
		t.Fatalf("baseline decision = %s, want w1", dec.WorkerID)
	}
	recs := rs.Recent()
	if len(recs) != 1 {
		t.Fatalf("receipts = %d, want 1", len(recs))
	}
	rec := recs[0]
	if rec.PolicyVersion != CostRouterVersion {
		t.Fatalf("policy version = %q, want %q", rec.PolicyVersion, CostRouterVersion)
	}
	if rec.Shadow == nil {
		t.Fatal("receipt carries no shadow record")
	}
	if rec.Shadow.CandidateVersion != "candidate/v9" {
		t.Fatalf("candidate version = %q", rec.Shadow.CandidateVersion)
	}
	if !rec.Shadow.Agree || rec.Shadow.WorkerID != "w1" || rec.Shadow.Cost != 42 {
		t.Fatalf("shadow = %+v, want agreement on w1", rec.Shadow)
	}
	if cand.calls != 1 {
		t.Fatalf("candidate calls = %d, want 1", cand.calls)
	}
}

func TestShadowDisagreementKeepsBaseline(t *testing.T) {
	r, rs := shadowBaseline(t)
	r.SetShadow(&stubRouter{version: "candidate/v2", decision: RouteDecision{WorkerID: "w2", Cost: 1}})

	dec, err := r.Route(shadowRequest())
	if err != nil {
		t.Fatal(err)
	}
	// Traffic follows the baseline even when the candidate disagrees.
	if dec.WorkerID != "w1" {
		t.Fatalf("routed worker = %s, want baseline w1", dec.WorkerID)
	}
	rec := rs.Recent()[0]
	if rec.Shadow.Agree || rec.Shadow.WorkerID != "w2" {
		t.Fatalf("shadow = %+v, want disagreement recorded as w2", rec.Shadow)
	}
}

func TestShadowErrorNeverFailsRouting(t *testing.T) {
	r, rs := shadowBaseline(t)
	r.SetShadow(&stubRouter{version: "candidate/v3", err: errors.New("no eligible candidate")})

	if _, err := r.Route(shadowRequest()); err != nil {
		t.Fatalf("candidate error leaked into routing: %v", err)
	}
	rec := rs.Recent()[0]
	if rec.Shadow.Err == "" || rec.Shadow.Agree {
		t.Fatalf("shadow = %+v, want recorded error without agreement", rec.Shadow)
	}
}

func TestShadowPanicNeverFailsRouting(t *testing.T) {
	r, rs := shadowBaseline(t)
	r.SetShadow(&stubRouter{version: "candidate/v4", panics: true})

	dec, err := r.Route(shadowRequest())
	if err != nil {
		t.Fatalf("candidate panic leaked into routing: %v", err)
	}
	if dec.WorkerID != "w1" {
		t.Fatalf("routed worker = %s, want w1", dec.WorkerID)
	}
	rec := rs.Recent()[0]
	if rec.Shadow == nil || rec.Shadow.Err == "" {
		t.Fatalf("shadow = %+v, want recovered panic recorded", rec.Shadow)
	}
}

func TestReceiptPredictorVersionStamped(t *testing.T) {
	r, rs := shadowBaseline(t)
	r.SetPredictor(NewLatencyPredictor(DefaultPredictorConfig()))

	if _, err := r.Route(shadowRequest()); err != nil {
		t.Fatal(err)
	}
	rec := rs.Recent()[0]
	if rec.PredictorVersion != PredictorVersion {
		t.Fatalf("predictor version = %q, want %q", rec.PredictorVersion, PredictorVersion)
	}
}

func TestReceiptVersionFieldsSigned(t *testing.T) {
	r, rs := shadowBaseline(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rs.SetSigningKey(key)
	r.SetShadow(&stubRouter{version: "candidate/v5", decision: RouteDecision{WorkerID: "w1"}})

	if _, err := r.Route(shadowRequest()); err != nil {
		t.Fatal(err)
	}
	rec := rs.Recent()[0]
	if rec.SignatureHex == "" {
		t.Fatal("receipt unsigned")
	}
	if !rec.Verify(rs.PublicKey()) {
		t.Fatal("receipt signature does not verify with version/shadow fields")
	}
}
