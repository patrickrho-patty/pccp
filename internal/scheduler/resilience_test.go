package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestHealthProberMarksUnhealthy(t *testing.T) {
	p := NewHealthProber(DefaultHealthConfig())
	p.Register("w1", func(ctx context.Context) error { return nil })
	p.Register("w2", func(ctx context.Context) error { return errUnreachable{} })

	// Consecutive failures (threshold 3) flip w2 unhealthy; w1 stays
	// healthy throughout.
	for i := 0; i < 3; i++ {
		p.ProbeAll(context.Background())
	}
	if p.Healthy("w2") {
		t.Fatal("w2 must be unhealthy after consecutive failed probes")
	}
	if !p.Healthy("w1") {
		t.Fatal("w1 must stay healthy")
	}
}

func TestHealthProberNeedsConsecutiveFailures(t *testing.T) {
	// A single transient failure must not flip a healthy worker —
	// consecutive failures required (flap protection).
	p := NewHealthProber(DefaultHealthConfig())
	p.Register("w1", func(ctx context.Context) error { return errUnreachable{} })
	p.ProbeAll(context.Background())
	if !p.Healthy("w1") {
		t.Fatal("first failure must not mark unhealthy")
	}
	p.ProbeAll(context.Background())
	p.ProbeAll(context.Background())
	if p.Healthy("w1") {
		t.Fatal("third consecutive failure must mark unhealthy")
	}
}

func TestRequestMigration(t *testing.T) {
	// A failed worker's in-flight request migrates to a healthy one.
	m := NewMigrationManager(2) // 2 attempts budget
	ok := m.Migrate("req-1", "w-dead", "w-alive", "worker failed")
	if !ok {
		t.Fatal("migration within budget must succeed")
	}
	// Budget exhaustion: the third migration of the same request fails.
	m.Migrate("req-1", "w-alive", "w2", "failed again")
	if m.Migrate("req-1", "w2", "w3", "failed again") {
		t.Fatal("migration beyond budget must fail")
	}
}

func TestCancellationPropagation(t *testing.T) {
	c := NewCancellationHub()
	ctx, cancel := c.Register("req-1")
	select {
	case <-ctx.Done():
		t.Fatal("fresh request context must be live")
	default:
	}
	c.Cancel("req-1", "client disconnected")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel must propagate to the request context")
	}
	c.Cancel("req-1", "double cancel") // idempotent
	_ = cancel
	if c.Reason("req-1") == "" {
		t.Fatal("cancel reason must be recorded")
	}
}

func TestShadowFailover(t *testing.T) {
	// Shadow failover: a standby worker receives a copy of the request;
	// when the primary fails mid-flight, the shadow's result is used.
	s := NewShadowTracker()
	s.Begin("req-1", "primary", "shadow")
	p1, ok1 := s.Primary("req-1")
	p2, ok2 := s.Shadow("req-1")
	if !ok1 || p1 != "primary" || !ok2 || p2 != "shadow" {
		t.Fatal("shadow pairing not recorded")
	}
	// Primary fails → failover to shadow.
	if got, ok := s.Failover("req-1", "primary"); !ok || got != "shadow" {
		t.Fatalf("failover = %s,%v want shadow", got, ok)
	}
	// The shadow then completes; done.
	s.Complete("req-1")
	if _, ok := s.Primary("req-1"); ok {
		t.Fatal("completed shadow request must be cleaned up")
	}
}

type errUnreachable struct{}

func (errUnreachable) Error() string { return "unreachable" }
