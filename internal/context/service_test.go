package context

import (
	"testing"

	"github.com/patrickrho-patty/pccp/internal/security"
)

func TestContextEvaluationClean(t *testing.T) {
	secSvc := security.New(nil)
	svc := New(nil, secSvc)

	manifest := &ContextManifest{
		Items: []ContextItem{
			{
				ID:         "item-1",
				Source:     "repository",
				Content:    "func main() { fmt.Println(\"hello\") }",
				TrustLabel: TrustRepository,
				Classification: "internal",
			},
		},
	}

	decisions := svc.EvaluateManifest("org-1", manifest)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Decision != "allow" {
		t.Fatalf("expected allow, got %s", decisions[0].Decision)
	}
}

func TestContextEvaluationPII(t *testing.T) {
	secSvc := security.New(nil)
	svc := New(nil, secSvc)

	manifest := &ContextManifest{
		Items: []ContextItem{
			{
				ID:         "item-1",
				Source:     "repository",
				Content:    "주민번호: 901225-1234567",
				TrustLabel: TrustRepository,
				Classification: "internal",
			},
		},
	}

	decisions := svc.EvaluateManifest("org-1", manifest)
	if decisions[0].Decision != "deny" {
		t.Fatalf("expected deny for PII, got %s", decisions[0].Decision)
	}
}

func TestContextEvaluationRestricted(t *testing.T) {
	secSvc := security.New(nil)
	svc := New(nil, secSvc)

	manifest := &ContextManifest{
		Items: []ContextItem{
			{
				ID:            "item-1",
				Content:       "clean code",
				TrustLabel:    TrustRepository,
				Classification: "restricted",
			},
		},
	}

	decisions := svc.EvaluateManifest("org-1", manifest)
	if decisions[0].Decision != "require_approval" {
		t.Fatalf("expected require_approval for restricted, got %s", decisions[0].Decision)
	}
}

func TestContextEvaluationUntrusted(t *testing.T) {
	secSvc := security.New(nil)
	svc := New(nil, secSvc)

	manifest := &ContextManifest{
		Items: []ContextItem{
			{
				ID:         "item-1",
				Content:    "external content",
				TrustLabel: TrustExternalUntrusted,
				Classification: "internal",
			},
		},
	}

	decisions := svc.EvaluateManifest("org-1", manifest)
	if decisions[0].Decision != "metadata_only" {
		t.Fatalf("expected metadata_only for untrusted, got %s", decisions[0].Decision)
	}
}

func TestEstimateTokens(t *testing.T) {
	// English text
	tokens := EstimateTokens("Hello world this is a test of token estimation")
	if tokens <= 0 {
		t.Fatal("expected positive token count")
	}

	// Korean text
	tokens = EstimateTokens("안녕하세요 이것은 토큰 추정 테스트입니다")
	if tokens <= 0 {
		t.Fatal("expected positive token count for Korean")
	}
}

func TestClassifyFile(t *testing.T) {
	trust, class := ClassifyFile("src/auth/login.go", "some content", "internal")
	if trust != TrustRepository {
		t.Fatal("expected TrustRepository")
	}
	if class != "internal" {
		t.Fatalf("expected internal, got %s", class)
	}

	trust, class = ClassifyFile(".env.production", "SECRET=abc", "confidential")
	if class != "restricted" {
		t.Fatalf("expected restricted for env file, got %s", class)
	}
	if trust != TrustAuthorized {
		t.Fatalf("expected TrustAuthorized for env file")
	}
}
