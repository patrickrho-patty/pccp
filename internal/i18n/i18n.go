package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Locale identifies a language locale.
type Locale string

const (
	Korean  Locale = "ko-KR"
	English Locale = "en-US"
)

// Translator provides internationalization support.
type Translator struct {
	mu       sync.RWMutex
	locale   Locale
	messages map[Locale]map[string]string
}

// New creates a new translator with Korean as default.
func New(locale Locale) *Translator {
	t := &Translator{
		locale:   locale,
		messages: make(map[Locale]map[string]string),
	}
	t.loadDefaults()
	return t
}

// T translates a message key to the current locale.
func (t *Translator) T(key string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if msgs, ok := t.messages[t.locale]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	// Fall back to Korean
	if msgs, ok := t.messages[Korean]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	return key
}

// SetLocale changes the active locale.
func (t *Translator) SetLocale(locale Locale) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.locale = locale
}

// LoadFromFile loads translations from a JSON file.
func (t *Translator) LoadFromFile(path string, locale Locale) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages[locale] = msgs
	return nil
}

// loadDefaults loads the built-in Korean and English message sets.
func (t *Translator) loadDefaults() {
	t.messages[Korean] = map[string]string{
		// Navigation
		"nav.dashboard":      "대시보드",
		"nav.users":          "사용자",
		"nav.harnesses":      "하네스",
		"nav.projects":       "프로젝트",
		"nav.repositories":   "저장소",
		"nav.sessions":       "세션",
		"nav.models":         "모델",
		"nav.endpoints":      "엔드포인트",
		"nav.policy":         "정책",
		"nav.analytics":      "분석",
		"nav.communications": "커뮤니케이션",
		"nav.audit":          "감사 로그",
		"nav.fleet":          "플릿 관리",

		// Common actions
		"action.create":      "생성",
		"action.update":      "수정",
		"action.delete":      "삭제",
		"action.cancel":      "취소",
		"action.save":        "저장",
		"action.login":       "로그인",
		"action.logout":      "로그아웃",
		"action.enroll":      "등록",
		"action.publish":     "게시",
		"action.recall":      "리콜",
		"action.approve":     "승인",
		"action.reject":      "거부",
		"action.revoke":      "폐기",
		"action.view":        "보기",

		// Status
		"status.active":      "활성",
		"status.pending":     "대기중",
		"status.closed":      "종료",
		"status.revoked":     "폐기됨",
		"status.enrolled":    "등록됨",
		"status.published":   "게시됨",
		"status.draft":       "초안",
		"status.recalled":    "리콜됨",
		"status.terminated":  "강제종료",

		// Messages
		"msg.not_found":        "찾을 수 없습니다",
		"msg.login_failed":     "로그인 실패",
		"msg.bootstrap_success": "초기 설정 완료",
		"msg.no_data":          "데이터가 없습니다",
		"msg.confirm_delete":   "정말 삭제하시겠습니까?",

		// Security
		"security.pii_detected":    "개인정보가 감지되었습니다",
		"security.secret_detected": "시크릿이 감지되었습니다",
		"security.injection_detected": "프롬프트 인젝션이 감지되었습니다",
		"security.finding_critical": "치명적 보안 발견",
		"security.finding_high":     "높은 위험도 보안 발견",

		// Fleet
		"fleet.quarantine":      "격리",
		"fleet.terminate_session": "세션 강제종료",
		"fleet.revoke_cert":     "인증서 폐기",
		"fleet.emergency_lockdown": "긴급 잠금",

		// Impact
		"impact.risk_low":      "낮은 위험도",
		"impact.risk_medium":   "중간 위험도",
		"impact.risk_high":     "높은 위험도",
		"impact.risk_critical": "치명적 위험도",
	}

	t.messages[English] = map[string]string{
		// Navigation
		"nav.dashboard":      "Dashboard",
		"nav.users":          "Users",
		"nav.harnesses":      "Harnesses",
		"nav.projects":       "Projects",
		"nav.repositories":   "Repositories",
		"nav.sessions":       "Sessions",
		"nav.models":         "Models",
		"nav.endpoints":      "Endpoints",
		"nav.policy":         "Policy",
		"nav.analytics":      "Analytics",
		"nav.communications": "Communications",
		"nav.audit":          "Audit Log",
		"nav.fleet":          "Fleet Management",

		// Common actions
		"action.create":      "Create",
		"action.update":      "Update",
		"action.delete":      "Delete",
		"action.cancel":      "Cancel",
		"action.save":        "Save",
		"action.login":       "Login",
		"action.logout":      "Logout",
		"action.enroll":      "Enroll",
		"action.publish":     "Publish",
		"action.recall":      "Recall",
		"action.approve":     "Approve",
		"action.reject":      "Reject",
		"action.revoke":      "Revoke",
		"action.view":        "View",

		// Status
		"status.active":      "Active",
		"status.pending":     "Pending",
		"status.closed":      "Closed",
		"status.revoked":     "Revoked",
		"status.enrolled":    "Enrolled",
		"status.published":   "Published",
		"status.draft":       "Draft",
		"status.recalled":    "Recalled",
		"status.terminated":  "Terminated",

		// Messages
		"msg.not_found":        "Not found",
		"msg.login_failed":     "Login failed",
		"msg.bootstrap_success": "Bootstrap complete",
		"msg.no_data":          "No data available",
		"msg.confirm_delete":   "Are you sure you want to delete?",

		// Security
		"security.pii_detected":    "PII detected",
		"security.secret_detected": "Secret detected",
		"security.injection_detected": "Prompt injection detected",
		"security.finding_critical": "Critical security finding",
		"security.finding_high":     "High severity security finding",

		// Fleet
		"fleet.quarantine":      "Quarantine",
		"fleet.terminate_session": "Terminate Session",
		"fleet.revoke_cert":     "Revoke Certificate",
		"fleet.emergency_lockdown": "Emergency Lockdown",

		// Impact
		"impact.risk_low":      "Low Risk",
		"impact.risk_medium":   "Medium Risk",
		"impact.risk_high":     "High Risk",
		"impact.risk_critical": "Critical Risk",
	}
}

// Default returns the default Korean translator.
func Default() *Translator {
	return New(Korean)
}
