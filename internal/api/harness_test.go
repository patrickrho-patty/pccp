package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/fleet"
	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/sovereign"
	"github.com/patrickrho-patty/pccp/internal/sso"
	"github.com/patrickrho-patty/pccp/internal/ssomigrate"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func signedBuildEvidence(t *testing.T, orgID, binaryHash string) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(priv, identity.BuildSigningBytes(orgID, binaryHash))
	return hex.EncodeToString(pub), hex.EncodeToString(signature)
}

func harnessTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/h.db?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.Organization{}, &models.User{}, &models.Harness{}, &models.Device{},
		&models.EnrollmentCode{}, &models.OrgSetting{}, &models.Session{},
		&models.Approval{}, &models.SecurityFinding{}, &models.AuditEvent{}, &models.CredentialRevocationRecord{},
		&models.ActionEnvelope{}, &models.ChangeSet{}, &models.PromptExchange{}, &models.ProvenanceSpan{},
		&models.SandboxRecord{},
		&models.ServiceSigningKey{}, &models.RealtimeEvent{}, &models.RealtimeSequence{}, &models.RealtimeStreamTicket{}, &models.RealtimeTransientEvent{},
		&models.FleetBulkOperation{},
		&models.FleetBulkTargetOutcome{},
		&models.FleetDesiredState{},
		&models.SecurityLockdown{},
		&identity.AdminCredentials{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := New(db, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return srv, db
}

func doJSON(t *testing.T, srv *Server, method, path, body string, orgID string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if path == "/api/auth/bootstrap" {
		req.Header.Set("X-PCCP-Bootstrap-Token", "test-bootstrap-token")
	}
	if orgID != "" {
		req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{OrganizationID: orgID}))
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestEnrollRejectsMissingOrganization(t *testing.T) {
	srv, _ := harnessTestServer(t)
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_test", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.0.0", "user_id": "u1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/harnesses/enroll", strings.NewReader(string(body)))
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without organization, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEnrollWithCodeBurnsCode(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "org", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	code, err := srv.identity.GenerateEnrollmentCode(org.ID, user.ID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_code", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID, "enrollment_code": code,
	})
	rec := doJSONAsRole(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var ec models.EnrollmentCode
	db.Where("code = ?", code).First(&ec)
	if !ec.Used || ec.UsedBy != "hrn_code" {
		t.Fatalf("code not burned: used=%v used_by=%q", ec.Used, ec.UsedBy)
	}
	var h models.Harness
	db.Where("harness_id = ?", "hrn_code").First(&h)
	if h.CredentialJSON == "" {
		t.Fatal("expected issued PPC on harness")
	}
	// The issued PPC must be a valid COSE-Sign1 signed by the CA.
	raw, err := hex.DecodeString(h.CredentialJSON)
	if err != nil {
		t.Fatal(err)
	}
	sign1, err := dari.DecodeCOSESign1(raw)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := dari.DecodePeerCredential(sign1.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cred.VerifySignature(srv.identity.CAPublicKeyRaw(), hex.EncodeToString(raw)); err != nil {
		t.Fatalf("issued PPC fails CA verification: %v", err)
	}
}

func TestDirectEnrollmentCannotTargetAnotherUserWithoutManagementPermission(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "tenant", Slug: "tenant-enroll-auth", Status: "active", MaxHarnessSeats: 10}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	member := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "member@corp.kr", Name: "Member", Status: models.UserStatusActive}
	victim := models.User{AuditBase: models.AuditBase{OrganizationID: org.ID}, Email: "victim@corp.kr", Name: "Victim", Status: models.UserStatusActive}
	db.Create(&member)
	db.Create(&victim)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn-impersonation", "public_key_hex": hex.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		"binary_version": "1.0.0", "user_id": victim.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/harnesses/enroll", strings.NewReader(string(body)))
	claims := &identity.Claims{OrganizationID: org.ID, Role: "member"}
	claims.Subject = member.ID
	req = req.WithContext(contextWithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member enrolling for another user = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	var count int64
	db.Model(&models.Harness{}).Where("organization_id = ?", org.ID).Count(&count)
	if count != 0 {
		t.Fatalf("denied enrollment persisted %d harnesses", count)
	}
}

func TestEnrollBlocksBelowForcedVersion(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "org", Slug: "org2", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev2@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	if err := srv.ext().Korean.SetForcedHarnessVersion(org.ID, "1.5.0", "stable", "", "security"); err != nil {
		t.Fatal(err)
	}
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_old", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID,
	})
	rec := doJSONAsRole(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID, "admin")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 below floor, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSovereignEnrollmentFailsClosedUntilEveryGatePasses(t *testing.T) {
	t.Setenv("PCCP_SOVEREIGN_DEPLOYMENT_ID", "dep-sovereign")
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "sovereign", Slug: "sovereign", Profile: "sovereign", Status: "active", MaxHarnessSeats: 4}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{Email: "pilot@agency.go.kr", Name: "pilot", Status: "active"}
	user.OrganizationID = org.ID
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	entitlementPublic, entitlementPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ext().Sovereign.ImportTrustBundle(sovereign.TrustBundle{
		OrganizationID: org.ID, LocalCAPublicKey: hex.EncodeToString(entitlementPublic), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.ext().Sovereign.InstallEntitlementAuthority(org.ID, hex.EncodeToString(entitlementPublic)); err != nil {
		t.Fatal(err)
	}
	entitlementNow := time.Now().UTC().Truncate(time.Second)
	signedEntitlement := sovereign.SignedOfflineEntitlement{Entitlement: sovereign.OfflineEntitlement{
		Version: 1, OrganizationID: org.ID, DeploymentID: "dep-sovereign", Profile: "sovereign", Sequence: 1,
		IssuedAt: entitlementNow.Add(-time.Hour).Format(time.RFC3339), NotBefore: entitlementNow.Add(-time.Minute).Format(time.RFC3339),
		NotAfter: entitlementNow.Add(time.Hour).Format(time.RFC3339), MaxUserSeats: 4, MaxHarnessSeats: 4,
		Features: []string{"harness-enrollment", "offline-inference"},
	}}
	signedEntitlement.Signature = hex.EncodeToString(ed25519.Sign(entitlementPrivate, signedEntitlement.Entitlement.SigningBytes()))
	if _, err := srv.ext().Sovereign.ImportOfflineEntitlementAt(signedEntitlement, org.ID, "dep-sovereign", entitlementNow); err != nil {
		t.Fatal(err)
	}
	buildKey, buildSignature := signedBuildEvidence(t, org.ID, "sha256:approved")
	attestationPublic, attestationPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(identity.EnrollmentPolicy{
		RequireAdminApproval:   true,
		RequireMDM:             true,
		RequiredMDMPosture:     []string{"disk_encryption", "screen_lock"},
		RequireAttestation:     true,
		AttestationPublicKeys:  []string{hex.EncodeToString(attestationPublic)},
		AllowedNetworkZones:    []string{"agency-vpn"},
		BuildSigningPublicKeys: []string{buildKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.OrgSetting{OrganizationID: org.ID, Key: identity.EnrollmentPolicySettingKey, Value: string(policy)}).Error; err != nil {
		t.Fatal(err)
	}

	pub := make([]byte, ed25519.PublicKeySize)
	attestedAt := time.Now().UTC().Format(time.RFC3339)
	base := map[string]interface{}{
		"harness_id": "hrn-sovereign", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.0.0", "binary_hash": "sha256:approved", "user_id": user.ID,
		"mdm_enrolled": true, "mdm_posture": `{"disk_encryption":true,"screen_lock":true}`,
		"network_zone": "agency-vpn", "attested_at": attestedAt, "build_signature": buildSignature,
	}
	attestationRequest := identity.EnrollHarnessRequest{
		OrganizationID: org.ID, UserID: user.ID, HarnessID: "hrn-sovereign", PublicKeyHex: hex.EncodeToString(pub),
		BinaryHash: "sha256:approved", MDMPosture: `{"disk_encryption":true,"screen_lock":true}`,
		NetworkZone: "agency-vpn", AttestedAt: attestedAt,
	}
	base["attestation"] = hex.EncodeToString(ed25519.Sign(attestationPrivate, identity.AttestationSigningBytes(attestationRequest)))
	assertDenied := func(field string) {
		t.Helper()
		copyBody := make(map[string]interface{}, len(base))
		for key, value := range base {
			copyBody[key] = value
		}
		delete(copyBody, field)
		encoded, _ := json.Marshal(copyBody)
		rec := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/enroll", string(encoded), org.ID, "admin")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("missing %s = %d, want 403: %s", field, rec.Code, rec.Body.String())
		}
	}
	for _, field := range []string{"enrollment_code", "mdm_posture", "attestation", "attested_at", "network_zone", "build_signature"} {
		assertDenied(field)
	}

	code, err := srv.identity.GenerateEnrollmentCode(org.ID, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	base["enrollment_code"] = code
	encoded, _ := json.Marshal(base)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/enroll", string(encoded), org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("all enrollment gates = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var device models.Device
	if err := db.Where("organization_id = ? AND user_id = ?", org.ID, user.ID).First(&device).Error; err != nil {
		t.Fatal(err)
	}
	if !device.MDMEnrolled || device.NetworkZone != "agency-vpn" || device.MDMPosture == "" {
		t.Fatalf("device proof was not persisted: %+v", device)
	}
	var harness models.Harness
	if err := db.Where("harness_id = ?", "hrn-sovereign").First(&harness).Error; err != nil {
		t.Fatal(err)
	}
	if harness.LastAttestation == "" || harness.NetworkZone != "agency-vpn" {
		t.Fatalf("harness proof was not persisted: %+v", harness)
	}
}

func TestEnterprisePilotSSOApprovalEnrollmentSeatAndSCIMDeprovision(t *testing.T) {
	srv, db := harnessTestServer(t)
	if err := db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{
		Name: "Enterprise Pilot", Slug: "enterprise-pilot", Profile: "enterprise", Status: "active",
		MaxUserSeats: 1, MaxHarnessSeats: 1,
	}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	srv.ext().SSO.ConfigureSCIMTokenForOrganization(org.ID, "pilot-scim-token")

	provision := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", strings.NewReader(`{
		"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],
		"externalId":"pilot-subject","userName":"pilot@corp.example",
		"email":"pilot@corp.example","displayName":"Pilot Developer"
	}`))
	provision.Header.Set("Authorization", "Bearer pilot-scim-token")
	provisionRec := httptest.NewRecorder()
	srv.ext().SSO.HandleSCIMRequest(provisionRec, provision)
	if provisionRec.Code != http.StatusCreated {
		t.Fatalf("SCIM provision = %d, want 201: %s", provisionRec.Code, provisionRec.Body.String())
	}
	var user models.User
	if err := db.Where("organization_id = ? AND external_id = ?", org.ID, "pilot-subject").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ssoMigrate.LinkIdentity(org.ID, ssomigrate.IdentityLinkRequest{
		LegacyIssuer: "https://idp.enterprise.example", LegacySubject: "pilot-subject", PattyUserID: user.ID,
		Note: "enterprise pilot explicit identity reconciliation",
	}, "pilot-governance-admin"); err != nil {
		t.Fatal(err)
	}
	resolved, err := srv.ext().SSO.ProvisionOIDCUser(org.ID, "https://idp.enterprise.example", &sso.OIDCUserInfo{
		Sub: "pilot-subject", Email: user.Email, EmailVerified: true, Name: user.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != user.ID {
		t.Fatalf("federated login created duplicate user %s, want %s", resolved.ID, user.ID)
	}

	buildKey, buildSignature := signedBuildEvidence(t, org.ID, "sha256:pilot-approved")
	policyBody, _ := json.Marshal(identity.EnrollmentPolicy{
		RequireAdminApproval: true, BuildSigningPublicKeys: []string{buildKey},
	})
	policyRec := doJSONAsRole(t, srv, http.MethodPut, "/api/harnesses/enrollment-policy", string(policyBody), org.ID, "admin")
	if policyRec.Code != http.StatusOK {
		t.Fatalf("policy enrollment configuration = %d: %s", policyRec.Code, policyRec.Body.String())
	}
	code, err := srv.identity.GenerateEnrollmentCode(org.ID, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enrollBody, _ := json.Marshal(map[string]string{
		"harness_id": "hrn-enterprise-pilot", "public_key_hex": hex.EncodeToString(publicKey),
		"binary_version": "1.0.0", "binary_hash": "sha256:pilot-approved", "build_signature": buildSignature,
		"user_id": user.ID, "enrollment_code": code,
	})
	enrollRec := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/enroll", string(enrollBody), org.ID, "admin")
	if enrollRec.Code != http.StatusCreated {
		t.Fatalf("approved enrollment = %d, want 201: %s", enrollRec.Code, enrollRec.Body.String())
	}
	var harness models.Harness
	if err := db.Where("organization_id = ? AND harness_id = ?", org.ID, "hrn-enterprise-pilot").First(&harness).Error; err != nil {
		t.Fatal(err)
	}
	if harness.CredentialJSON == "" {
		t.Fatal("approved enrollment did not issue a PPC")
	}

	secondCode, err := srv.identity.GenerateEnrollmentCode(org.ID, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, _ := json.Marshal(map[string]string{
		"harness_id": "hrn-over-seat-limit", "public_key_hex": hex.EncodeToString(publicKey),
		"binary_version": "1.0.0", "binary_hash": "sha256:pilot-approved", "build_signature": buildSignature,
		"user_id": user.ID, "enrollment_code": secondCode,
	})
	seatRec := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/enroll", string(secondBody), org.ID, "admin")
	if seatRec.Code != http.StatusPaymentRequired {
		t.Fatalf("second harness seat = %d, want 402: %s", seatRec.Code, seatRec.Body.String())
	}

	deprovision := httptest.NewRequest(http.MethodDelete, "/scim/v2/Users/"+user.ID, nil)
	deprovision.Header.Set("Authorization", "Bearer pilot-scim-token")
	deprovisionRec := httptest.NewRecorder()
	srv.ext().SSO.HandleSCIMRequest(deprovisionRec, deprovision)
	if deprovisionRec.Code != http.StatusOK {
		t.Fatalf("SCIM deprovision = %d, want 200: %s", deprovisionRec.Code, deprovisionRec.Body.String())
	}
	var result struct {
		Status          string `json:"status"`
		RemainingAccess int64  `json:"remaining_access"`
	}
	if err := json.Unmarshal(deprovisionRec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != models.UserStatusOffboarded || result.RemainingAccess != 0 {
		t.Fatalf("SCIM deprovision result = %+v", result)
	}
	if err := db.First(&harness, "id = ?", harness.ID).Error; err != nil {
		t.Fatal(err)
	}
	if harness.Status != "revoked" {
		t.Fatalf("offboarded user's harness status = %q, want revoked", harness.Status)
	}
	var auditEvents int64
	db.Model(&models.AuditEvent{}).Where("organization_id = ? AND event_type IN ?", org.ID, []string{
		"cp.harness.enrollment_policy_updated", "cp.harness.enrolled", "cp.user.offboarded",
	}).Count(&auditEvents)
	if auditEvents != 3 {
		t.Fatalf("pilot audit events = %d, want 3", auditEvents)
	}
}

func TestEnrollmentPolicyAdminEndpointPersistsAuditedTenantPolicy(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "enterprise", Slug: "enterprise-policy", Profile: "enterprise", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	publicKey, _ := signedBuildEvidence(t, org.ID, "sha256:any")
	body, _ := json.Marshal(identity.EnrollmentPolicy{RequireAdminApproval: true, BuildSigningPublicKeys: []string{publicKey}})
	denied := doJSONAsRole(t, srv, http.MethodPut, "/api/harnesses/enrollment-policy", string(body), org.ID, "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer policy update = %d, want 403", denied.Code)
	}
	rec := doJSONAsRole(t, srv, http.MethodPut, "/api/harnesses/enrollment-policy", string(body), org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin policy update = %d: %s", rec.Code, rec.Body.String())
	}
	var setting models.OrgSetting
	if err := db.Where("organization_id = ? AND key = ?", org.ID, identity.EnrollmentPolicySettingKey).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	var auditCount int64
	db.Model(&models.AuditEvent{}).Where("organization_id = ? AND event_type = ?", org.ID, "cp.harness.enrollment_policy_updated").Count(&auditCount)
	if auditCount != 1 {
		t.Fatalf("policy audit events = %d, want 1", auditCount)
	}
}

func TestSovereignEntitlementEndpointEnforcesAuthenticatedTenantScope(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "agency", Slug: "agency-entitlement", Profile: "sovereign", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := srv.ext().Sovereign.ImportTrustBundle(sovereign.TrustBundle{
		OrganizationID: org.ID, LocalCAPublicKey: hex.EncodeToString(publicKey), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.ext().Sovereign.InstallEntitlementAuthority(org.ID, hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	signed := sovereign.SignedOfflineEntitlement{Entitlement: sovereign.OfflineEntitlement{
		Version: 1, OrganizationID: org.ID, DeploymentID: "dep-1", Profile: "sovereign", Sequence: 1,
		IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Minute).Format(time.RFC3339),
		NotAfter: now.Add(time.Hour).Format(time.RFC3339), MaxHarnessSeats: 4,
	}}
	signed.Signature = hex.EncodeToString(ed25519.Sign(privateKey, signed.Entitlement.SigningBytes()))
	body, _ := json.Marshal(signed)
	rec := doJSONAsRole(t, srv, http.MethodPost, "/api/sovereign/entitlements/dep-1", string(body), org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("entitlement import = %d: %s", rec.Code, rec.Body.String())
	}
	crossTenant := doJSONAsRole(t, srv, http.MethodPost, "/api/sovereign/entitlements/dep-1", string(body), "org-other", "admin")
	if crossTenant.Code != http.StatusBadRequest {
		t.Fatalf("cross-tenant entitlement = %d, want 400: %s", crossTenant.Code, crossTenant.Body.String())
	}
}

func TestSovereignTrustBundleImportIsTenantAdminScopedAndCannotRotateEntitlementAuthority(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "agency", Slug: "agency-trust", Profile: "sovereign", Status: "active"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	pinnedKey, _, _ := ed25519.GenerateKey(rand.Reader)
	replacementKey, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := srv.ext().Sovereign.ImportTrustBundle(sovereign.TrustBundle{
		OrganizationID: org.ID, LocalCAPublicKey: hex.EncodeToString(pinnedKey), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.ext().Sovereign.InstallEntitlementAuthority(org.ID, hex.EncodeToString(pinnedKey)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(sovereign.TrustBundle{
		OrganizationID: org.ID, LocalCAPublicKey: hex.EncodeToString(replacementKey),
		EntitlementAuthorityPublicKey: hex.EncodeToString(replacementKey),
		ExpiresAt:                     time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	viewer := doJSONAsRole(t, srv, http.MethodPost, "/api/sovereign/trust-bundle", string(body), org.ID, "viewer")
	if viewer.Code != http.StatusForbidden {
		t.Fatalf("viewer trust import = %d, want 403", viewer.Code)
	}
	crossBody, _ := json.Marshal(sovereign.TrustBundle{
		OrganizationID: "org-other", LocalCAPublicKey: hex.EncodeToString(replacementKey), ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	crossTenant := doJSONAsRole(t, srv, http.MethodPost, "/api/sovereign/trust-bundle", string(crossBody), org.ID, "admin")
	if crossTenant.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant trust import = %d, want 403", crossTenant.Code)
	}
	admin := doJSONAsRole(t, srv, http.MethodPost, "/api/sovereign/trust-bundle", string(body), org.ID, "admin")
	if admin.Code != http.StatusCreated {
		t.Fatalf("admin trust import = %d: %s", admin.Code, admin.Body.String())
	}
	bundle, err := srv.ext().Sovereign.GetTrustBundle(org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.EntitlementAuthorityPublicKey != hex.EncodeToString(pinnedKey) {
		t.Fatal("tenant trust import rotated the offline entitlement authority")
	}
}

func TestRevokeTerminatesSessionsAndRecordsRevocation(t *testing.T) {
	srv, db := harnessTestServer(t)
	srv.fleet.SetRevocationSender(func(fleet.ActionRequest) error { return nil })
	org := models.Organization{Name: "org", Slug: "org3", Status: "active", MaxHarnessSeats: 100}
	db.Create(&org)
	user := models.User{Email: "dev3@corp.kr", Name: "dev", Status: "active"}
	user.OrganizationID = org.ID
	db.Create(&user)
	pub := make([]byte, 32)
	body, _ := json.Marshal(map[string]string{
		"harness_id": "hrn_revoke", "public_key_hex": hex.EncodeToString(pub),
		"binary_version": "1.2.0", "user_id": user.ID,
	})
	rec := doJSONAsRole(t, srv, "POST", "/api/harnesses/enroll", string(body), org.ID, "admin")
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll failed: %s", rec.Body.String())
	}
	var h models.Harness
	db.Where("harness_id = ?", "hrn_revoke").First(&h)
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, HarnessID: "hrn_revoke", UserID: user.ID, SessionID: "ses-1", Status: "active"})
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: org.ID}, HarnessID: "hrn_revoke", UserID: user.ID, SessionID: "ses-2", Status: "closed"})

	rec = doJSONAsRole(t, srv, "POST", "/api/harnesses/"+h.ID+"/revoke", `{"reason":"offboarded"}`, org.ID, "admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d %s", rec.Code, rec.Body.String())
	}
	var s1 models.Session
	db.Where("session_id = ?", "ses-1").First(&s1)
	if s1.Status != "terminated" {
		t.Fatalf("active session not terminated: %s", s1.Status)
	}
	var s2 models.Session
	db.Where("session_id = ?", "ses-2").First(&s2)
	if s2.Status != "closed" {
		t.Fatalf("closed session should be untouched: %s", s2.Status)
	}
	var revCount int64
	db.Model(&models.CredentialRevocationRecord{}).Count(&revCount)
	if revCount == 0 {
		t.Fatal("expected credential revocation record")
	}
}

// doJSON2 issues a request with an explicit operator email claim.
func doJSON2(t *testing.T, srv *Server, method, path, body string, email string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req = req.WithContext(contextWithClaims(req.Context(), &identity.Claims{Email: email, OrganizationID: "org-mfa"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestReactivateHarnessRequiresManagementPermissionAndTenantScope(t *testing.T) {
	srv, db := harnessTestServer(t)
	foreign := models.Harness{OrganizationID: "org-b", HarnessID: "foreign-harness", Status: "quarantined", RiskState: "high"}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	denied := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/harnesses/"+foreign.ID+"/reactivate", "", "org-a", "viewer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer reactivation = %d, want 403", denied.Code)
	}
	crossTenant := doSessionJSONWithPermissions(t, srv, http.MethodPost, "/api/harnesses/"+foreign.ID+"/reactivate", "", "org-a", "admin")
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant reactivation = %d, want 404: %s", crossTenant.Code, crossTenant.Body.String())
	}
	var got models.Harness
	db.First(&got, "id = ?", foreign.ID)
	if got.Status != "quarantined" || got.RiskState != "high" {
		t.Fatalf("foreign harness mutated: %+v", got)
	}
}

func TestHarnessHeartbeatRequiresTenantBoundSignatureAndTrustedAttestation(t *testing.T) {
	srv, db := harnessTestServer(t)
	org := models.Organization{Name: "tenant", Slug: "heartbeat-tenant", Status: "active", Profile: "enterprise"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatal(err)
	}
	device := models.Device{OrganizationID: org.ID, Hostname: "old-host", Status: "active"}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	harnessPub, harnessPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attestationPub, attestationPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policyJSON, err := json.Marshal(identity.EnrollmentPolicy{
		RequireAttestation:    true,
		AttestationPublicKeys: []string{hex.EncodeToString(attestationPub)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.OrgSetting{
		OrganizationID: org.ID,
		Key:            identity.EnrollmentPolicySettingKey,
		Value:          string(policyJSON),
	}).Error; err != nil {
		t.Fatal(err)
	}
	harness := models.Harness{
		OrganizationID: org.ID,
		DeviceID:       device.ID,
		HarnessID:      "hrn-signed-heartbeat",
		PublicKey:      hex.EncodeToString(harnessPub),
		Status:         "enrolled",
	}
	if err := db.Create(&harness).Error; err != nil {
		t.Fatal(err)
	}

	proof := identity.HarnessHeartbeatProof{
		OrganizationID:  org.ID,
		HarnessID:       harness.HarnessID,
		SignedAt:        time.Now().UTC().Truncate(time.Second).Add(123 * time.Nanosecond).Format(time.RFC3339Nano),
		BinaryVersion:   "2.1.0",
		DeviceHostname:  "trusted-host",
		DeviceOS:        "linux",
		DeviceOSVersion: "6.12",
		DeviceArch:      "arm64",
		IPAddress:       "10.0.0.8",
		AttestedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	unsignedBody, _ := json.Marshal(proof)
	unsigned := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(unsignedBody), org.ID, "admin")
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned heartbeat = %d, want 401: %s", unsigned.Code, unsigned.Body.String())
	}

	crossTenantProof := proof
	crossTenantProof.OrganizationID = "another-org"
	crossTenantBody, _ := json.Marshal(crossTenantProof)
	crossTenant := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(crossTenantBody), org.ID, "admin")
	if crossTenant.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant heartbeat = %d, want 403: %s", crossTenant.Code, crossTenant.Body.String())
	}

	proof.Attestation = hex.EncodeToString(ed25519.Sign(attestationPriv, proof.AttestationSigningBytes()))
	proof.Signature = hex.EncodeToString(ed25519.Sign(harnessPriv, proof.SigningBytes()))
	tampered := proof
	tampered.DeviceHostname = "attacker-host"
	tamperedBody, _ := json.Marshal(tampered)
	tamperedResponse := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(tamperedBody), org.ID, "admin")
	if tamperedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("tampered heartbeat = %d, want 401: %s", tamperedResponse.Code, tamperedResponse.Body.String())
	}

	validBody, _ := json.Marshal(proof)
	valid := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(validBody), org.ID, "admin")
	if valid.Code != http.StatusOK {
		t.Fatalf("valid heartbeat = %d, want 200: %s", valid.Code, valid.Body.String())
	}
	var updatedHarness models.Harness
	if err := db.First(&updatedHarness, "id = ?", harness.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedHarness.BinaryVersion != proof.BinaryVersion || updatedHarness.LastHeartbeat == "" || updatedHarness.LastAttestation == "" {
		t.Fatalf("signed harness facts not persisted: %+v", updatedHarness)
	}
	acceptedSignedAt, err := time.Parse(time.RFC3339Nano, proof.SignedAt)
	if err != nil {
		t.Fatal(err)
	}
	wantSignedAt := acceptedSignedAt.UTC().Format(heartbeatSignedAtLayout)
	if updatedHarness.LastHeartbeatSignedAt != wantSignedAt {
		t.Fatalf("accepted signed_at = %q, want %q", updatedHarness.LastHeartbeatSignedAt, wantSignedAt)
	}
	var updatedDevice models.Device
	if err := db.First(&updatedDevice, "id = ?", device.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedDevice.Hostname != proof.DeviceHostname || updatedDevice.OSVersion != proof.DeviceOSVersion || updatedDevice.IPAddress != proof.IPAddress {
		t.Fatalf("signed device facts not persisted: %+v", updatedDevice)
	}

	replay := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(validBody), org.ID, "admin")
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed heartbeat = %d, want 401: %s", replay.Code, replay.Body.String())
	}

	// Relay traffic updates server-observed liveness independently. A future
	// relay timestamp must neither advance nor poison the signed-client replay
	// watermark.
	relayObservedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if err := db.Model(&models.Harness{}).Where("id = ?", harness.ID).
		Update("last_heartbeat", relayObservedAt).Error; err != nil {
		t.Fatal(err)
	}
	nextSignedAt, err := time.Parse(time.RFC3339Nano, proof.SignedAt)
	if err != nil {
		t.Fatal(err)
	}
	nextProof := proof
	nextProof.SignedAt = nextSignedAt.Add(time.Second).Format(time.RFC3339)
	nextProof.Signature = hex.EncodeToString(ed25519.Sign(harnessPriv, nextProof.SigningBytes()))
	nextBody, _ := json.Marshal(nextProof)
	next := doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(nextBody), org.ID, "admin")
	if next.Code != http.StatusOK {
		t.Fatalf("heartbeat after relay liveness write = %d, want 200: %s", next.Code, next.Body.String())
	}

	// A valid timestamp inside the permitted future-skew window is accepted
	// once, then rejected on exact replay even though server liveness remains
	// behind the client-signed timestamp.
	futureProof := nextProof
	futureProof.SignedAt = time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339)
	futureProof.Signature = hex.EncodeToString(ed25519.Sign(harnessPriv, futureProof.SigningBytes()))
	futureBody, _ := json.Marshal(futureProof)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var requests sync.WaitGroup
	for range 2 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			<-start
			responses <- doJSONAsRole(t, srv, http.MethodPost, "/api/harnesses/heartbeat", string(futureBody), org.ID, "admin")
		}()
	}
	close(start)
	requests.Wait()
	close(responses)
	statusCounts := map[int]int{}
	for response := range responses {
		statusCounts[response.Code]++
	}
	if statusCounts[http.StatusOK] != 1 || statusCounts[http.StatusUnauthorized] != 1 {
		t.Fatalf("concurrent future-skew replay statuses = %#v, want one 200 and one 401", statusCounts)
	}
}
