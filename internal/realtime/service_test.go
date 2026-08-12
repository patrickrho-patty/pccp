package realtime

import "testing"

func TestBroadcast(t *testing.T) {
	svc := New()
	svc.Broadcast("test.event", map[string]string{"msg": "hello"})
	// No clients connected, should not panic
}

func TestNotifySession(t *testing.T) {
	svc := New()
	svc.NotifySessionUpdate("org-1", "ses-1", "active")
	svc.NotifySecurityFinding("org-1", "high", "테스트 보안 발견")
	svc.NotifyChatMessage("org-1", "conv-1", "김개발", "안녕하세요")
	svc.NotifyFleetAction("org-1", "quarantine", "hrn-1")
	// Should not panic with no clients
}

func TestConnectedClients(t *testing.T) {
	svc := New()
	if svc.ConnectedClients() != 0 {
		t.Fatal("expected 0 clients")
	}
}
