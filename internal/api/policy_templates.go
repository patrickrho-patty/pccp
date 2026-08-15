package api

import (
	"encoding/json"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// policyTemplateSeed is the server-side six-domain template catalog
// (policy UX2) — the same catalog the console previously hardcoded,
// now seeded per org and editable/versioned through
// /api/policy/templates.
func policyTemplateSeed() []models.PolicyTemplate {
	type tpl struct {
		id, domain, name, nameEn, desc string
		config                         map[string]interface{}
	}
	raw := []tpl{
		{"restrict-models", "models", "모델 제한", "Restrict Models", "특정 모델만 허용 (예: Standard만)", map[string]interface{}{"allowed_models": []string{"patty-code-standard"}, "denied_models": []string{}}},
		{"production-models", "models", "프로덕션 전용", "Production Only", "프로덕션 환경에서 검증된 모델만", map[string]interface{}{"allowed_models": []string{"patty-code-standard"}, "denied_models": []string{"patty-code-fast"}}},
		{"no-vision", "models", "비전 모델 차단", "No Vision Models", "이미지 입력 지원 모델 비활성화", map[string]interface{}{"deny_capabilities": []string{"image"}}},
		{"strict-tools", "tools", "엄격 모드", "Strict Mode", "모든 쓰기/실행 도구 승인 필요", map[string]interface{}{"require_approval_for": []string{"file.write", "file.delete", "shell.execute"}, "auto_approve": []string{"file.read", "search.code"}}},
		{"read-only", "tools", "읽기 전용", "Read Only", "쓰기/실행 완전 차단, 읽기/검색만", map[string]interface{}{"block_all": []string{"file.write", "file.delete", "shell.execute", "git.push", "package.install"}, "allow": []string{"file.read", "search.code", "test.run"}}},
		{"no-network", "tools", "네트워크 차단", "No Network", "외부 HTTP 요청 차단", map[string]interface{}{"block_all": []string{"network.http"}, "reason": "보안: 외부 데이터 유출 방지"}},
		{"no-mcp", "tools", "MCP 서버 제한", "Restrict MCP", "승인되지 않은 MCP 서버 차단", map[string]interface{}{"require_mcp_allowlist": true}},
		{"kr-pii", "data", "한국 PII 보호", "Korean PII Protection", "주민번호, 사업자번호 자동 마스킹", map[string]interface{}{"dlp_rules": []string{"pii-kr-rrn", "pii-kr-business", "pii-kr-phone"}, "action": "mask"}},
		{"secrets-block", "data", "비밀정보 차단", "Block Secrets", "API 키, 토큰, 개인키 모델 입력 차단", map[string]interface{}{"dlp_rules": []string{"secret-aws", "secret-jwt", "secret-private-key", "secret-github"}, "action": "block"}},
		{"context-firewall", "data", "컨텍스트 방화벽", "Context Firewall", "외부 컨텐츠에서 인젝션 감지", map[string]interface{}{"scan_context": true, "block_injection": true}},
		{"protected-main", "scm", "메인 브랜치 보호", "Protected Main", "main/release/prod 직접 푸시 금지", map[string]interface{}{"protected_branches": []string{"main", "release", "prod"}, "require_pr": true}},
		{"require-approval", "scm", "AI 변경 승인", "AI Change Approval", "AI가 생성한 모든 커밋은 인간 승인 필요", map[string]interface{}{"require_human_review": true, "block_ai_direct_push": true}},
		{"no-force-push", "scm", "강제 푸시 금지", "No Force Push", "모든 브랜치 force push 차단", map[string]interface{}{"block_force_push": true}},
		{"allowlist", "network", "접속 허용 목록", "Allowlist", "지정된 도메인만 접속 허용", map[string]interface{}{"mode": "allowlist", "allowed": []string{"npmjs.org", "pypi.org", "github.com"}}},
		{"block-exfil", "network", "데이터 유출 방지", "Anti-Exfiltration", "대용량 업로드 차단", map[string]interface{}{"max_upload_mb": 10, "block_unknown": true}},
		{"vpn-only", "network", "VPN 전용", "VPN Only", "VPN 네트워크 대역에서만 접속", map[string]interface{}{"require_vpn": true, "allowed_zones": []string{"corp-vpn"}}},
		{"max-duration", "session", "최대 세션 시간", "Max Duration", "세션 최대 4시간 후 자동 종료", map[string]interface{}{"max_duration_minutes": 240}},
		{"idle-timeout", "session", "유휴 종료", "Idle Timeout", "30분 미사용 시 자동 종료", map[string]interface{}{"idle_timeout_minutes": 30}},
		{"auto-evidence", "session", "자동 증거 수집", "Auto Evidence", "세션 종료 시 자동으로 증거 번들 생성", map[string]interface{}{"auto_evidence": true, "retain_days": 90}},
	}
	out := make([]models.PolicyTemplate, 0, len(raw))
	for _, t := range raw {
		cfgJSON, _ := json.Marshal(t.config)
		out = append(out, models.PolicyTemplate{
			TemplateID: t.id, Domain: t.domain, Name: t.name, NameEn: t.nameEn,
			Description: t.desc, ConfigJSON: string(cfgJSON), Version: "1", Enabled: true,
		})
	}
	return out
}
