package connectors

import "testing"

func TestConnectorRegisterAndGet(t *testing.T) {
	svc := New()

	conn, err := svc.Register(Connector{
		OrganizationID: "org-1",
		Type:           TypeJira,
		Name:           "Jira Enterprise",
		NameKo:         "지라 엔터프라이즈",
		BaseURL:        "https://jira.example.com",
		AuthType:       "api_key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.ID == "" {
		t.Fatal("expected connector ID")
	}

	// Get
	got, err := svc.Get(conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NameKo != "지라 엔터프라이즈" {
		t.Fatal("expected Korean name")
	}
}

func TestConnectorList(t *testing.T) {
	svc := New()
	svc.Register(Connector{OrganizationID: "org-1", Type: TypeGitHub, Name: "GitHub"})
	svc.Register(Connector{OrganizationID: "org-1", Type: TypeSlack, Name: "Slack"})
	svc.Register(Connector{OrganizationID: "org-2", Type: TypeJira, Name: "Jira"})

	connectors := svc.List("org-1")
	if len(connectors) != 2 {
		t.Fatalf("expected 2, got %d", len(connectors))
	}
}

func TestSupportedConnectorTypes(t *testing.T) {
	types := SupportedConnectorTypes()
	if len(types) < 8 {
		t.Fatalf("expected 8+ types, got %d", len(types))
	}

	// Check Korean names
	foundKakao := false
	for _, ct := range types {
		if ct.Type == TypeKakaoWork {
			foundKakao = true
			if ct.NameKo != "카카오워크 (Kakao Work)" {
				t.Fatal("expected Kakao Work Korean name")
			}
		}
	}
	if !foundKakao {
		t.Fatal("Kakao Work not found in supported types")
	}
}

func TestConnectorDisable(t *testing.T) {
	svc := New()
	conn, _ := svc.Register(Connector{OrganizationID: "org-1", Type: TypeGitHub})
	svc.Disable(conn.ID)

	got, _ := svc.Get(conn.ID)
	if got.Status != "disabled" {
		t.Fatal("expected disabled status")
	}
}
