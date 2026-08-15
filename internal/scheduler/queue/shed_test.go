package queue

import "testing"

func TestDropClassRemovesOnlySheddable(t *testing.T) {
	q := New(DefaultLimits())
	if err := q.Enqueue(req("b1", "t1", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("b2", "t2", ClassBatch, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(req("i1", "t3", ClassInteractivePaid, 10, 10)); err != nil {
		t.Fatal(err)
	}
	removed := q.DropClass(ClassBatch, ClassBackgroundAgent)
	if len(removed) != 2 {
		t.Fatalf("removed %d, want 2", len(removed))
	}
	if q.Pending() != 1 {
		t.Fatalf("pending after shed = %d, want 1 (interactive survives)", q.Pending())
	}
	got := mustNext(t, q)
	if got.ID != "i1" {
		t.Fatalf("survivor = %s, want i1", got.ID)
	}
}
