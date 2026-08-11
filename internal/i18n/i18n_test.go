package i18n

import "testing"

func TestKoreanDefault(t *testing.T) {
	tr := Default()
	if tr.T("nav.dashboard") != "대시보드" {
		t.Fatalf("expected Korean, got %s", tr.T("nav.dashboard"))
	}
}

func TestLocaleSwitch(t *testing.T) {
	tr := Default()
	tr.SetLocale(English)
	if tr.T("nav.dashboard") != "Dashboard" {
		t.Fatalf("expected English, got %s", tr.T("nav.dashboard"))
	}
	tr.SetLocale(Korean)
	if tr.T("nav.dashboard") != "대시보드" {
		t.Fatalf("expected Korean after switch, got %s", tr.T("nav.dashboard"))
	}
}

func TestMissingKeyFallback(t *testing.T) {
	tr := Default()
	if tr.T("nonexistent.key") != "nonexistent.key" {
		t.Fatal("missing key should return itself")
	}
}

func TestSecurityMessages(t *testing.T) {
	tr := Default()
	if tr.T("security.pii_detected") != "개인정보가 감지되었습니다" {
		t.Fatal("Korean PII message wrong")
	}
}
