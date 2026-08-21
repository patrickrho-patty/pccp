package scheduler

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func roleWorker(id, model, engine, role string) FleetWorker {
	e := mkWorker(id, model, 8)
	e.Card.EngineKind = engine
	e.Card.PDRole = role
	return FleetWorker{Entry: e, State: RouterWorkerState{}}
}

func TestRoleActuatorSplitsHonoringFloor(t *testing.T) {
	pd := NewPDController(NewPDPlanner(), nil)
	pd.SetNow(time.Now)
	act := NewRoleActuator(pd)
	workers := []FleetWorker{
		roleWorker("w1", "model-a", "vllm", PDRoleAggregated),
		roleWorker("w2", "model-a", "vllm", PDRoleAggregated),
		roleWorker("w3", "model-a", "vllm", PDRoleAggregated),
	}

	// Not engaged: nothing to change.
	if got := act.Plan("model-a", workers); len(got) != 0 {
		t.Fatalf("unengaged plan = %+v", got)
	}

	pd.Evaluate("model-a", 0.9) // engage
	plan := act.Plan("model-a", workers)
	// floor=1 → 2 splittable, one prefill + one decode.
	if len(plan) != 2 {
		t.Fatalf("plan = %+v, want 2 directives", plan)
	}
	roles := map[string]int{}
	for _, d := range plan {
		roles[d.Role]++
	}
	if roles[PDRolePrefill] != 1 || roles[PDRoleDecode] != 1 {
		t.Fatalf("split = %v, want one prefill one decode", roles)
	}
	// w3 must remain aggregated (floor retained).
	for _, d := range plan {
		if d.WorkerID == "w3" {
			t.Fatal("floor worker got a split directive")
		}
	}
}

func TestRoleActuatorRevertsOnRelease(t *testing.T) {
	pd := NewPDController(NewPDPlanner(), nil)
	now := time.Now()
	pd.SetNow(func() time.Time { return now })
	act := NewRoleActuator(pd)
	workers := []FleetWorker{
		roleWorker("w1", "model-a", "vllm", PDRolePrefill),
		roleWorker("w2", "model-a", "vllm", PDRoleDecode),
		roleWorker("w3", "model-a", "vllm", PDRoleAggregated),
	}

	pd.Evaluate("model-a", 0.9)
	now = now.Add(3 * time.Minute) // past cooldown
	pd.Evaluate("model-a", 0.1)    // release
	plan := act.Plan("model-a", workers)
	if len(plan) != 2 {
		t.Fatalf("release plan = %+v, want 2 reverts", plan)
	}
	for _, d := range plan {
		if d.Role != PDRoleAggregated {
			t.Fatalf("release produced %s, want aggregated", d.Role)
		}
	}
}

func TestRoleActuatorNeverSplitsSGLang(t *testing.T) {
	pd := NewPDController(NewPDPlanner(), nil)
	pd.SetNow(time.Now)
	pd.Evaluate("model-a", 0.9)
	act := NewRoleActuator(pd)
	workers := []FleetWorker{
		roleWorker("w1", "model-a", "sglang", PDRoleAggregated),
		roleWorker("w2", "model-a", "sglang", PDRoleAggregated),
	}
	if got := act.Plan("model-a", workers); len(got) != 0 {
		t.Fatalf("sglang split = %+v — unsupported role changes must be rejected", got)
	}
}

func TestRoleDirectiveSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	d := RoleDirective{WorkerID: "w1", Model: "model-a", Role: PDRolePrefill, Reason: "test"}
	if err := d.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if !d.Verify(pub) {
		t.Fatal("signed directive must verify")
	}
	d.Role = PDRoleDecode // tamper
	if d.Verify(pub) {
		t.Fatal("tampered directive must not verify")
	}
}
