package config

import (
	"strings"
	"testing"
)

func TestProductionJWTSecretReadinessFailsClosed(t *testing.T) {
	for _, secret := range []string{"", InsecureDevelopmentJWTSecret, "too-short"} {
		if err := ValidateJWTSecret(secret, false); err == nil {
			t.Fatalf("production accepted insecure JWT secret %q", secret)
		}
	}
	if err := ValidateJWTSecret(strings.Repeat("x", 32), false); err != nil {
		t.Fatalf("production rejected strong JWT secret: %v", err)
	}
	if err := ValidateJWTSecret(InsecureDevelopmentJWTSecret, true); err != nil {
		t.Fatalf("explicit development mode rejected development secret: %v", err)
	}
}
