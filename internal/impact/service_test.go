package impact

import (
	"testing"
)

func TestCalculateRiskScoreAuth(t *testing.T) {
	svc := New(nil)
	req := AnalyzeRequest{
		OrganizationID: "org-1",
		RepositoryID:   "repo-1",
		FilePath:       "src/auth/login.go",
		IsAuth:         true,
		SymbolsChanged: []string{"Login", "VerifyPassword"},
	}
	_, score, err := svc.AnalyzeChange(req)
	if err != nil {
		t.Fatal(err)
	}
	if score.Score < 25 {
		t.Fatalf("auth code should have high risk, got %.1f", score.Score)
	}
	if score.Level == "low" {
		t.Fatal("auth code risk level should not be low")
	}
}

func TestCalculateRiskScoreCrypto(t *testing.T) {
	svc := New(nil)
	req := AnalyzeRequest{
		OrganizationID: "org-1",
		FilePath:       "src/crypto/aes.go",
		IsCrypto:       true,
		SymbolsChanged: []string{"Encrypt"},
	}
	_, score, _ := svc.AnalyzeChange(req)
	if score.Score < 20 {
		t.Fatalf("crypto code should have high risk, got %.1f", score.Score)
	}
}

func TestCalculateRiskScoreLow(t *testing.T) {
	svc := New(nil)
	req := AnalyzeRequest{
		OrganizationID: "org-1",
		FilePath:       "src/utils/format.go",
		SymbolsChanged: []string{"FormatTime"},
	}
	_, score, _ := svc.AnalyzeChange(req)
	if score.Score > 10 {
		t.Fatalf("simple utility should have low risk, got %.1f", score.Score)
	}
	if score.Level != "low" {
		t.Fatalf("expected low level, got %s", score.Level)
	}
}

func TestDetectPathSensitivity(t *testing.T) {
	tests := []struct {
		path    string
		isAuth  bool
		isCrypto bool
		isDB    bool
		isAPI   bool
		isConfig bool
	}{
		{"src/auth/LoginService.java", true, false, false, false, false},
		{"lib/crypto/aes_cipher.py", false, true, false, false, false},
		{"migrations/001_add_users.sql", false, false, true, false, false},
		{"api/handlers/user_controller.go", false, false, false, true, false},
		{"config/production.yaml", false, false, false, false, true},
		{"src/utils/string_helper.ts", false, false, false, false, false},
	}

	for _, tt := range tests {
		result := DetectPathSensitivity(tt.path)
		if result.IsAuth != tt.isAuth {
			t.Errorf("%s: expected IsAuth=%v, got %v", tt.path, tt.isAuth, result.IsAuth)
		}
		if result.IsCrypto != tt.isCrypto {
			t.Errorf("%s: expected IsCrypto=%v, got %v", tt.path, tt.isCrypto, result.IsCrypto)
		}
	}
}

func TestCriticalRiskRequiresApproval(t *testing.T) {
	svc := New(nil)
	req := AnalyzeRequest{
		IsAuth:        true,
		IsCrypto:      true,
		IsDBMigration: true,
		IsAPIContract: true,
		IsConfig:      true,
		SymbolsChanged: []string{"a", "b", "c", "d", "e", "f"},
	}
	_, score, _ := svc.AnalyzeChange(req)
	if !score.RequiresApproval {
		t.Fatal("critical risk should require approval")
	}
}
