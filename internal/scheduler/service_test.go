package scheduler

import "testing"

func TestFairAdmission(t *testing.T) {
	s := New()

	// Account A: pro plan, weight 30
	s.UpdateAccount("acct-a", 30, 5, "INTERACTIVE")

	// Account B: developer plan, weight 10
	s.UpdateAccount("acct-b", 10, 3, "INTERACTIVE")

	// Both request admission
	s.Enqueue(QueuedRequest{AccountID: "acct-a", Class: "INTERACTIVE"})
	s.Enqueue(QueuedRequest{AccountID: "acct-b", Class: "INTERACTIVE"})

	// Admit first — should be acct-a (higher weight)
	r1 := s.Admit(100)
	if r1 == nil {
		t.Fatal("expected admission")
	}
	if r1.AccountID != "acct-a" {
		t.Fatalf("expected acct-a first (higher weight), got %s", r1.AccountID)
	}

	// Admit second
	r2 := s.Admit(100)
	if r2 == nil || r2.AccountID != "acct-b" {
		t.Fatal("expected acct-b second")
	}
}

func TestSlotLimit(t *testing.T) {
	s := New()
	s.UpdateAccount("acct-x", 10, 2, "INTERACTIVE")

	// Admit twice (fills 2 slots)
	s.Admit(100)
	s.Admit(100)

	// Third should fail (at capacity)
	r3 := s.Admit(100)
	if r3 != nil {
		t.Fatal("expected nil when account at capacity")
	}
}

func TestGlobalLimit(t *testing.T) {
	s := New()
	s.UpdateAccount("a", 10, 5, "INTERACTIVE")
	s.UpdateAccount("b", 10, 5, "INTERACTIVE")

	// Fill global to 2
	s.Admit(2)
	s.Admit(2)

	// Third should fail (global cap)
	r := s.Admit(2)
	if r != nil {
		t.Fatal("expected nil at global cap")
	}
}

func TestRelease(t *testing.T) {
	s := New()
	s.UpdateAccount("acct", 10, 1, "INTERACTIVE")
	s.Enqueue(QueuedRequest{AccountID: "acct", Class: "INTERACTIVE"})

	s.Admit(100)
	if s.ActiveSlots() != 1 {
		t.Fatal("expected 1 active slot")
	}

	s.Release("acct")
	if s.ActiveSlots() != 0 {
		t.Fatal("expected 0 after release")
	}
}

func TestStarvationPrevention(t *testing.T) {
	s := New()

	// Account with very low weight but long queue age
	s.UpdateAccount("low-priority", 1, 5, "BACKGROUND")
	s.UpdateAccount("high-priority", 100, 5, "INTERACTIVE")

	// Both enqueue
	s.Enqueue(QueuedRequest{AccountID: "low-priority", Class: "BACKGROUND"})
	s.Enqueue(QueuedRequest{AccountID: "high-priority", Class: "INTERACTIVE"})

	// High priority should win
	r := s.Admit(100)
	if r == nil {
		t.Fatal("expected admission")
	}
	// Note: in a real test we'd manipulate time to test starvation
	// For now just verify the scheduler doesn't crash
}

func TestQueueStats(t *testing.T) {
	s := New()
	s.UpdateAccount("acct", 30, 5, "INTERACTIVE")
	s.UpdateCLU("acct", 1500.0)
	s.Enqueue(QueuedRequest{AccountID: "acct", Class: "INTERACTIVE"})
	s.Admit(100)

	stats := s.GetStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 account, got %d", len(stats))
	}
	if stats[0].ActiveSlots != 1 {
		t.Fatal("expected 1 active slot")
	}
	if stats[0].CurrentCLU != 1500.0 {
		t.Fatal("expected CLU 1500")
	}
}
