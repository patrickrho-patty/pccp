package sovereign

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOfflineEntitlementVerifiesScopeExpiryAndMonotonicSequence(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := New()
	if _, err := svc.ImportTrustBundle(TrustBundle{
		OrganizationID: "org-1", LocalCAPublicKey: hex.EncodeToString(pub), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.InstallEntitlementAuthority("org-1", hex.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	signed := SignedOfflineEntitlement{Entitlement: OfflineEntitlement{
		Version: 1, OrganizationID: "org-1", DeploymentID: "dep-seoul-1", Profile: "sovereign", Sequence: 7,
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Minute).Format(time.RFC3339),
		NotAfter: now.Add(24 * time.Hour).Format(time.RFC3339), MaxUserSeats: 500, MaxHarnessSeats: 800,
		Features: []string{"offline-inference"}, ModelClasses: []string{"internal"},
	}}
	signed.Signature = hex.EncodeToString(ed25519.Sign(priv, signed.Entitlement.SigningBytes()))
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-1", "dep-seoul-1", now); err != nil {
		t.Fatalf("valid entitlement: %v", err)
	}
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-2", "dep-seoul-1", now); err == nil {
		t.Fatal("cross-organization entitlement must fail")
	}
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-1", "dep-other", now); err == nil {
		t.Fatal("cross-deployment entitlement must fail")
	}
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-1", "dep-seoul-1", now.Add(25*time.Hour)); err == nil {
		t.Fatal("expired entitlement must fail")
	}
	signed.Entitlement.Sequence = 6
	signed.Signature = hex.EncodeToString(ed25519.Sign(priv, signed.Entitlement.SigningBytes()))
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-1", "dep-seoul-1", now); err == nil {
		t.Fatal("sequence rollback must fail")
	}
}

func TestOfflineEntitlementPersistsAndEnforcesAfterRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sovereign.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.OrgSetting{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "Agency", Slug: "agency-durable", Profile: "sovereign", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	service := New(db)
	if _, err := service.ImportTrustBundle(TrustBundle{
		OrganizationID: org.ID, LocalCAPublicKey: hex.EncodeToString(pub), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.InstallEntitlementAuthority(org.ID, hex.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	signed := SignedOfflineEntitlement{Entitlement: OfflineEntitlement{
		Version: 1, OrganizationID: org.ID, DeploymentID: "dep-durable", Profile: "sovereign", Sequence: 12,
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Minute).Format(time.RFC3339),
		NotAfter: now.Add(time.Hour).Format(time.RFC3339), MaxUserSeats: 30, MaxHarnessSeats: 40,
		Features: []string{"harness-enrollment", "offline-inference"}, ModelClasses: []string{"internal"},
	}}
	signed.Signature = hex.EncodeToString(ed25519.Sign(priv, signed.Entitlement.SigningBytes()))
	if _, err := service.ImportOfflineEntitlementAt(signed, org.ID, "dep-durable", now); err != nil {
		t.Fatal(err)
	}
	restarted := New(db)
	if got, err := restarted.GetOfflineEntitlement(org.ID, "dep-durable"); err != nil || got.Entitlement.Sequence != 12 {
		t.Fatalf("restarted entitlement = %+v, %v", got, err)
	}
	if _, err := restarted.ImportOfflineEntitlementAt(signed, org.ID, "dep-durable", now); err == nil {
		t.Fatal("restart lost entitlement sequence high-water")
	}
	if _, err := ValidateActiveEntitlementWithDB(db, org.ID, "dep-durable", "offline-inference", "internal", now); err != nil {
		t.Fatalf("active entitlement authorization failed: %v", err)
	}
	if _, err := ValidateActiveEntitlementWithDB(db, org.ID, "another-deployment", "offline-inference", "internal", now); err == nil {
		t.Fatal("artifact for another deployment authorized this installation")
	}
	if _, err := ValidateActiveEntitlementWithDB(db, org.ID, "dep-durable", "offline-inference", "restricted", now); err == nil {
		t.Fatal("disallowed model class was authorized")
	}
	if err := db.First(&org, "id = ?", org.ID).Error; err != nil || org.MaxUserSeats != 30 || org.MaxHarnessSeats != 40 {
		t.Fatalf("signed seat authority not applied: %+v, %v", org, err)
	}
}

func TestOfflineEntitlementSignatureBindsEveryAuthorityField(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().UTC().Truncate(time.Second)
	svc := New()
	_, _ = svc.ImportTrustBundle(TrustBundle{OrganizationID: "org-1", LocalCAPublicKey: hex.EncodeToString(pub), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339)})
	_ = svc.InstallEntitlementAuthority("org-1", hex.EncodeToString(pub))
	signed := SignedOfflineEntitlement{Entitlement: OfflineEntitlement{
		Version: 1, OrganizationID: "org-1", DeploymentID: "dep-1", Profile: "sovereign", Sequence: 1,
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Minute).Format(time.RFC3339),
		NotAfter: now.Add(time.Hour).Format(time.RFC3339), MaxHarnessSeats: 4,
	}}
	signed.Signature = hex.EncodeToString(ed25519.Sign(priv, signed.Entitlement.SigningBytes()))
	signed.Entitlement.MaxHarnessSeats = 400
	if _, err := svc.ImportOfflineEntitlementAt(signed, "org-1", "dep-1", now); err == nil {
		t.Fatal("tampered seat authority must fail")
	}
}

func TestInstalledEntitlementAuthorityIsPinned(t *testing.T) {
	firstPublicKey, _, _ := ed25519.GenerateKey(nil)
	secondPublicKey, _, _ := ed25519.GenerateKey(nil)
	service := New()
	if _, err := service.ImportTrustBundle(TrustBundle{
		OrganizationID: "org-pinned", LocalCAPublicKey: hex.EncodeToString(firstPublicKey), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.InstallEntitlementAuthority("org-pinned", hex.EncodeToString(firstPublicKey)); err != nil {
		t.Fatal(err)
	}
	if err := service.InstallEntitlementAuthority("org-pinned", hex.EncodeToString(firstPublicKey)); err != nil {
		t.Fatalf("idempotent authority install failed: %v", err)
	}
	if err := service.InstallEntitlementAuthority("org-pinned", hex.EncodeToString(secondPublicKey)); err == nil {
		t.Fatal("installed entitlement authority was silently rotated")
	}
	bundle, err := service.GetTrustBundle("org-pinned")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.EntitlementAuthorityPublicKey != hex.EncodeToString(firstPublicKey) {
		t.Fatal("failed rotation changed the pinned entitlement authority")
	}
}

func TestConfiguredEntitlementAuthorityPersistsAuditAndRejectsRotation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/authority.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.OrgSetting{}, &models.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "Agency", Slug: "authority-config", Profile: "sovereign", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	firstPublicKey, _, _ := ed25519.GenerateKey(nil)
	secondPublicKey, _, _ := ed25519.GenerateKey(nil)
	service := New(db)
	if _, err := service.ImportTrustBundle(TrustBundle{
		OrganizationID:   org.ID,
		LocalCAPublicKey: hex.EncodeToString(firstPublicKey),
		ExpiresAt:        time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	configured, _ := json.Marshal(map[string]string{org.ID: hex.EncodeToString(firstPublicKey)})
	if err := service.ConfigureEntitlementAuthoritiesJSON(string(configured)); err != nil {
		t.Fatalf("startup authority configuration failed: %v", err)
	}
	if err := service.ConfigureEntitlementAuthoritiesJSON(string(configured)); err != nil {
		t.Fatalf("idempotent startup authority configuration failed: %v", err)
	}
	var auditCount int64
	if err := db.Model(&models.AuditEvent{}).
		Where("organization_id = ? AND event_type = ?", org.ID, "cp.sovereign.entitlement_authority.installed").
		Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("authority installation audit count = %d, want 1", auditCount)
	}
	rotated, _ := json.Marshal(map[string]string{org.ID: hex.EncodeToString(secondPublicKey)})
	if err := service.ConfigureEntitlementAuthoritiesJSON(string(rotated)); err == nil {
		t.Fatal("startup configuration silently rotated the pinned authority")
	}
}

func TestOfflineEntitlementCrossRepositoryGolden(t *testing.T) {
	entitlement := OfflineEntitlement{
		Version: 1, OrganizationID: "org-golden", DeploymentID: "dep-golden", Profile: "sovereign", Sequence: 42,
		IssuedAt: "2026-08-22T10:00:00Z", NotBefore: "2026-08-22T11:00:00Z", NotAfter: "2026-09-22T11:00:00Z",
		MaxUserSeats: 12, MaxHarnessSeats: 34, Features: []string{"offline-inference", "harness-enrollment"},
		ModelClasses: []string{"internal", "restricted"},
	}
	wantBytes := "PCCP-OFFLINE-ENTITLEMENT-v1\x00{\"version\":1,\"organization_id\":\"org-golden\",\"deployment_id\":\"dep-golden\",\"profile\":\"sovereign\",\"sequence\":42,\"issued_at\":\"2026-08-22T10:00:00Z\",\"not_before\":\"2026-08-22T11:00:00Z\",\"not_after\":\"2026-09-22T11:00:00Z\",\"max_user_seats\":12,\"max_harness_seats\":34,\"features\":[\"offline-inference\",\"harness-enrollment\"],\"model_classes\":[\"internal\",\"restricted\"]}"
	if got := string(entitlement.SigningBytes()); got != wantBytes {
		t.Fatalf("cross-repository signing bytes drifted:\n%s", got)
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	gotSignature := hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), entitlement.SigningBytes()))
	const wantSignature = "83662daf8af6d68e436b63eaac75ac1ee24c4158dc9ec9d8af2f9ca158cceb1173ebdf251606b4ff3c585d6ca072d72ed2e91303d3e85d19e7c8a19582fe390b"
	if gotSignature != wantSignature {
		t.Fatalf("cross-repository signature drifted: %s", gotSignature)
	}
}
