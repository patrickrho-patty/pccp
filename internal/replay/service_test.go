package replay

import "testing"

func TestSafeReplay(t *testing.T) {
	p := New(0)
	seen1, _, err := p.Check("key1", "ses1", "exch1", ClassSafeReplay)
	if seen1 || err != nil {
		t.Fatal("first check should not be seen")
	}
	seen2, _, err := p.Check("key1", "ses1", "exch1", ClassSafeReplay)
	if seen2 || err != nil {
		t.Fatal("safe replay should not block")
	}
}

func TestSameKeyOnly(t *testing.T) {
	p := New(0)
	seen1, _, _ := p.Check("key2", "ses1", "exch1", ClassSameKeyOnly)
	if seen1 {
		t.Fatal("first check should not be seen")
	}
	p.Record("key2", map[string]string{"result": "ok"})
	seen2, entry, _ := p.Check("key2", "ses1", "exch1", ClassSameKeyOnly)
	if !seen2 {
		t.Fatal("second check should be seen")
	}
	if entry.Result == nil {
		t.Fatal("expected recorded result")
	}
}

func TestNeverAutoRetry(t *testing.T) {
	p := New(0)
	p.Check("key3", "ses1", "exch1", ClassNeverAutoRetry)
	_, _, err := p.Check("key3", "ses1", "exch1", ClassNeverAutoRetry)
	if err == nil {
		t.Fatal("never auto-retry should return error")
	}
}

func TestQueryBeforeRetry(t *testing.T) {
	p := New(0)
	p.Check("key4", "ses1", "exch1", ClassQueryBeforeRetry)
	_, _, err := p.Check("key4", "ses1", "exch1", ClassQueryBeforeRetry)
	if err == nil {
		t.Fatal("query before retry should return error")
	}
}

func TestOperationClass(t *testing.T) {
	tests := []struct {
		op     string
		expect IdempotencyClass
	}{
		{"presence.update", ClassSafeReplay},
		{"model.request", ClassSameKeyOnly},
		{"shell.command", ClassQueryBeforeRetry},
		{"runtime.destructive", ClassNeverAutoRetry},
		{"session.open", ClassSameKeyOnly},
	}
	for _, tt := range tests {
		if got := OperationClass(tt.op); got != tt.expect {
			t.Errorf("%s: expected %s, got %s", tt.op, tt.expect, got)
		}
	}
}

func TestSize(t *testing.T) {
	p := New(0)
	p.Check("a", "s", "e", ClassSafeReplay)
	p.Check("b", "s", "e", ClassSafeReplay)
	if p.Size() != 2 {
		t.Fatalf("expected 2, got %d", p.Size())
	}
	p.Clear("a")
	if p.Size() != 1 {
		t.Fatalf("expected 1 after clear, got %d", p.Size())
	}
}
