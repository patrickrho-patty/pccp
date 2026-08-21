package scheduler

import "testing"

// migrationFixture builds a directory with one hot extent on w1 and a
// fleet with w1/w2 (healthy) / w3 (saturated).
func migrationFixture() (*KVDirectory, *WorkerFleet, *StaticTopologyOracle) {
	dir := NewKVDirectory()
	dir.SetNow(func() int64 { return 0 })
	dir.Add("w1", L1GPU, dirBlock("tenant-a", "viral", 1024), testIdentity, true)
	for i := 0; i < 5; i++ {
		dir.Hit("tenant-a", "viral", testIdentity)
	}

	fleet := NewWorkerFleet()
	fleet.Upsert(mkWorker("w1", "model-a", 8), RouterWorkerState{Load: WorkerLoad{MaxConcurrent: 8}})
	fleet.Upsert(mkWorker("w2", "model-a", 8), RouterWorkerState{Load: WorkerLoad{MaxConcurrent: 8}})
	fleet.Upsert(mkWorker("w3", "model-a", 8), RouterWorkerState{Load: WorkerLoad{MaxConcurrent: 8, Active: 8}})

	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddWorker("w1", "n1")
	inv.AddWorker("w2", "n1")
	inv.AddWorker("w3", "n1")
	return dir, fleet, NewStaticTopologyOracle(inv)
}

func TestMigrationReplicatesHotPrefix(t *testing.T) {
	dir, fleet, oracle := migrationFixture()
	m := NewMigrationCoordinator(dir, oracle, fleet)

	res := m.ReplicateOnce()
	if res.Candidates != 1 || res.Placed != 1 || res.Skipped != 0 {
		t.Fatalf("result = %+v", res)
	}
	// w2 is the target (w3 saturated): placed UNVERIFIED (no router
	// credit until the target confirms).
	locs := dir.Locations("tenant-a", "viral", testIdentity)
	if len(locs) != 1 || locs[0].WorkerID != "w1" {
		t.Fatalf("usable locations = %+v — unverified replica must earn no credit yet", locs)
	}
	if !dir.VerifyLocation("w2", "tenant-a", "viral", testIdentity) {
		t.Fatal("verify failed")
	}
	locs = dir.Locations("tenant-a", "viral", testIdentity)
	if len(locs) != 2 {
		t.Fatalf("verified locations = %+v, want 2", locs)
	}
	// Second pass: at max replicas now — nothing placed.
	res = m.ReplicateOnce()
	if res.Placed != 0 {
		t.Fatalf("replicated past max replicas: %+v", res)
	}
}

func TestMigrationNeverOverloadsForAHit(t *testing.T) {
	dir, fleet, oracle := migrationFixture()
	// Only the saturated worker is available as a target.
	fleet.Remove("w2")
	m := NewMigrationCoordinator(dir, oracle, fleet)
	res := m.ReplicateOnce()
	if res.Placed != 0 || res.Skipped != 1 {
		t.Fatalf("result = %+v — must skip rather than overload", res)
	}
}

func TestMigrationSkipsUnpriceableAndModelMismatch(t *testing.T) {
	dir, fleet, _ := migrationFixture()
	// No topology for w2: transfer unpriceable → skip.
	inv := NewTopologyInventory()
	inv.AddNode("n1", TopologyNode{Zone: "z1", Rack: "r1"})
	inv.AddWorker("w1", "n1")
	m := NewMigrationCoordinator(dir, NewStaticTopologyOracle(inv), fleet)
	if res := m.ReplicateOnce(); res.Placed != 0 {
		t.Fatalf("unpriceable transfer placed: %+v", res)
	}

	// Model mismatch: w2 serves a different model → not a replica target.
	dir2, fleet2, oracle2 := migrationFixture()
	fleet2.Mutate("w2", func(w *FleetWorker) { w.Entry.Card.ModelName = "model-b" })
	m2 := NewMigrationCoordinator(dir2, oracle2, fleet2)
	res := m2.ReplicateOnce()
	if res.Placed != 0 {
		t.Fatalf("model-mismatched replica placed: %+v", res)
	}

	// Prefix confusion: a "model-ab" worker must not receive "model-a"
	// package replicas (the match is on the model@ boundary, not a raw
	// string prefix).
	dir3, fleet3, oracle3 := migrationFixture()
	fleet3.Mutate("w2", func(w *FleetWorker) { w.Entry.Card.ModelName = "model-ab" })
	m3 := NewMigrationCoordinator(dir3, oracle3, fleet3)
	if res := m3.ReplicateOnce(); res.Placed != 0 {
		t.Fatalf("model-ab worker received model-a replica: %+v", res)
	}
	// Exact package match still replicates.
	dir4, fleet4, oracle4 := migrationFixture()
	fleet4.Mutate("w2", func(w *FleetWorker) { w.Entry.Card.ModelName = "model-ab" })
	dir4.InvalidateIf(func(id CacheIdentity) bool { return true })
	samePkg := testIdentity
	samePkg.ModelPackage = "model-ab@2.0"
	dir4.Add("w1", L1GPU, dirBlock("tenant-a", "viral", 1024), samePkg, true)
	for i := 0; i < 5; i++ {
		dir4.Hit("tenant-a", "viral", samePkg)
	}
	m4 := NewMigrationCoordinator(dir4, oracle4, fleet4)
	if res := m4.ReplicateOnce(); res.Placed != 1 {
		t.Fatalf("exact package match did not replicate: %+v", res)
	}
}
