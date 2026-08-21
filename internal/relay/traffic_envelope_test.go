package relay

import (
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

func TestSignTrafficEnvelopeBindsProgramMetadata(t *testing.T) {
	db := setupGovernedTestDB(t)
	s, err := New(db, "http://cp.invalid", "relay-test")
	if err != nil {
		t.Fatal(err)
	}
	meta := &scheduler.ProgramMeta{
		ProgramID:  "prog-7",
		TurnSeq:    4,
		ParentID:   "prog-1",
		ToolPaused: true,
		TaskSLOMs:  30000,
	}
	env, err := s.signTrafficEnvelope("tenant-1", "user-1", "req-1", meta)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := s.TrafficIssuerPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Verify(pub); err != nil {
		t.Fatalf("signed envelope must verify: %v", err)
	}
	if env.Program == nil || env.Program.ProgramID != "prog-7" || env.Program.TurnSeq != 4 || !env.Program.ToolPaused {
		t.Fatalf("program metadata = %+v", env.Program)
	}

	// Over-long identifiers are dropped, never signed — the envelope
	// still issues (degrade metadata, never traffic).
	bad := &scheduler.ProgramMeta{ProgramID: strings.Repeat("x", 200)}
	env2, err := s.signTrafficEnvelope("tenant-1", "user-1", "req-2", bad)
	if err != nil {
		t.Fatal(err)
	}
	if env2.Program != nil {
		t.Fatalf("over-long program metadata must be dropped, got %+v", env2.Program)
	}
	if err := env2.Verify(pub); err != nil {
		t.Fatalf("degraded envelope must still verify: %v", err)
	}
}
