package keymgmt

import (
	"testing"
	"time"
)

func TestGetOrCreateKeyCreatesAndReturnsSame(t *testing.T) {
	s := New()

	first, err := s.GetOrCreateKey(DomainConfig, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("get-or-create: %v", err)
	}
	if first.Domain != DomainConfig {
		t.Fatalf("domain %s, want %s", first.Domain, DomainConfig)
	}
	if first.Status != "active" {
		t.Fatalf("status %s, want active", first.Status)
	}

	second, err := s.GetOrCreateKey(DomainConfig, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("second get-or-create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatal("get-or-create must return the existing key, not rotate")
	}

	if _, err := s.GetActiveKey(DomainConfig); err != nil {
		t.Fatalf("key not active: %v", err)
	}
}
