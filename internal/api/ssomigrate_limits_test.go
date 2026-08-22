package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSSOMigrationWaveRequestBoundsAppsAndStrings(t *testing.T) {
	if err := validateSSOMigrationWaveRequest("manifest", "wave", "owner", []string{"app"}); err != nil {
		t.Fatalf("valid wave rejected: %v", err)
	}
	tooMany := make([]string, ssoMigrationWaveMaxApps+1)
	for i := range tooMany {
		tooMany[i] = "app"
	}
	for name, err := range map[string]error{
		"too many apps": validateSSOMigrationWaveRequest("manifest", "wave", "owner", tooMany),
		"oversize name": validateSSOMigrationWaveRequest("manifest", strings.Repeat("x", ssoMigrationWaveStringLimit+1), "owner", []string{"app"}),
		"oversize app":  validateSSOMigrationWaveRequest("manifest", "wave", "owner", []string{strings.Repeat("x", ssoMigrationWaveStringLimit+1)}),
	} {
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestSSOMigrationPageLimitIsBounded(t *testing.T) {
	for raw, want := range map[string]int{"": 50, "?limit=10": 10, "?limit=9999": 200, "?limit=-1": 50, "?limit=nope": 50} {
		req := httptest.NewRequest("GET", "/api/sso-migrate/links"+raw, nil)
		if got := ssoMigrationPageLimit(req); got != want {
			t.Fatalf("%q limit=%d want=%d", raw, got, want)
		}
	}
}
