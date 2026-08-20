package reference

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func refDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ref.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.ReferenceSource{}, &models.ReferenceChunk{}, &models.ReferencePackage{},
		&models.ReferenceCatalogState{}, &models.ReferenceAuditEvent{}, &models.User{}, &models.Organization{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func seedNCPSources(t *testing.T, svc *Service, orgID string) {
	t.Helper()
	for _, src := range []models.ReferenceSource{
		{SourceID: "ncp-maps", Name: "Naver Cloud Maps", NameKo: "네이버클라우드 지도", LibraryID: "ncp-maps", Tier: "tier1", Authority: "official", VersionScheme: "date"},
		{SourceID: "kakao-login", Name: "Kakao Login", NameKo: "카카오 로그인", LibraryID: "kakao-login", Tier: "tier1", Authority: "official", VersionScheme: "semver", Aliases: `["kakao login","로그인"]`},
		{SourceID: "toss-payments", Name: "Toss Payments", NameKo: "토스페이먼츠", LibraryID: "toss-payments", Tier: "tier1", Authority: "official", VersionScheme: "semver", Aliases: `["toss","결제"]`},
	} {
		if _, err := svc.RegisterSource(orgID, src); err != nil {
			t.Fatalf("register %s: %v", src.SourceID, err)
		}
	}
}

func seedChunk(t *testing.T, db *gorm.DB, orgID, sourceID, lib, version, body string) {
	t.Helper()
	row := models.ReferenceChunk{
		OrganizationID: orgID, SourceID: sourceID, LibraryID: lib, Version: version,
		ChunkID: "c-" + lib + "-" + version, DocPath: "docs/" + lib + ".md",
		TitleKo: "타이틀", Body: body, CodeLang: "javascript", Code: "const x = 1;",
		ChunkHash: chunkHash(sourceID, "docs/"+lib+".md", body, "const x = 1;"),
		Authority: "official", Tokens: tokenizeBody(body, "const x = 1;", "javascript"),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func TestResolveLibraryByAliasAndKoreanName(t *testing.T) {
	db := refDB(t)
	svc := New(db)
	seedNCPSources(t, svc, "org-r")
	// By English name.
	res, err := svc.ResolveLibrary("org-r", "Naver Cloud Maps", "", "")
	if err != nil || res.SourceID != "ncp-maps" {
		t.Fatalf("resolve english: %v %+v", err, res)
	}
	// By Korean name.
	res, err = svc.ResolveLibrary("org-r", "카카오 로그인", "", "")
	if err != nil || res.SourceID != "kakao-login" {
		t.Fatalf("resolve korean: %v %+v", err, res)
	}
	// By alias (lowercase).
	res, err = svc.ResolveLibrary("org-r", "toss", "", "")
	if err != nil || res.SourceID != "toss-payments" {
		t.Fatalf("resolve alias: %v %+v", err, res)
	}
}

// Version resolution: exact project evidence picks the exact chunk version and
// marks it; no-mismatch-note distinguishes a gap when exact is unavailable.
func TestResolveVersionExactVsGap(t *testing.T) {
	db := refDB(t)
	svc := New(db)
	seedNCPSources(t, svc, "org-r")
	seedChunk(t, db, "org-r", "toss-payments", "toss-payments", "1.4.0", "토스페이먼츠 결제 API: 결제 위젯을 생성합니다.")
	seedChunk(t, db, "org-r", "toss-payments", "toss-payments", "1.5.0", "토스페이먼츠 결제 API: 간편결제를 지원합니다.")
	// Exact version 1.4.0 available from project evidence.
	res, err := svc.ResolveLibrary("org-r", "toss-payments", "toss-payments: v1.4.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.DetectedVersion != "1.4.0" || res.VersionNote != "exact" {
		t.Fatalf("exact version not chosen: %+v", res)
	}
	// Requested version without project evidence also exact.
	res, _ = svc.ResolveLibrary("org-r", "toss-payments", "", "1.5.0")
	if res.ChosenVersion != "1.5.0" || res.VersionNote != "exact" {
		t.Fatalf("requested version not exact: %+v", res)
	}
	// No evidence → latest approved stable, disclosed as such.
	res, _ = svc.ResolveLibrary("org-r", "toss-payments", "", "")
	if res.VersionNote != "latest-approved (no exact project version detected)" {
		t.Fatalf("latest not disclosed: %+v", res)
	}
}

// Search ranks Korean/English mixed content and preserves code/hits citations.
func TestSearchKoreanAndCode(t *testing.T) {
	db := refDB(t)
	svc := New(db)
	seedNCPSources(t, svc, "org-r")
	seedChunk(t, db, "org-r", "kakao-login", "kakao-login", "2.0.0", "카카오 로그인은 사용자 정보 동의를 받은 뒤 인증 토큰을 발급합니다.")
	seedChunk(t, db, "org-r", "ncp-maps", "ncp-maps", "2026-06", "네이버클라우드 지도 SDK로 지도 마커를 렌더링합니다.")
	// Korean query.
	hits, err := svc.SearchDocs("org-r", "kakao-login", "카카오 로그인 인증 토큰", "", "ko", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatalf("no korean search hits")
	}
	// Version filter narrows.
	hits, _ = svc.SearchDocs("org-r", "ncp-maps", "지도 SDK 렌더링", "2026-06", "ko", 5)
	if len(hits) == 0 {
		t.Fatalf("version-filtered search missed")
	}
	for _, h := range hits {
		if h.Citation == "" || h.CodeLang == "" {
			t.Fatalf("citation/code missing: %+v", h)
		}
	}
}

// Package import validates, stages chunks, and activation atomically promotes
// (superseding prior). Rollback clears the marker.
func TestPackageImportStageActivateRollback(t *testing.T) {
	db := refDB(t)
	svc := New(db)
	seedNCPSources(t, svc, "org-r")
	man := Manifest{
		SchemaVersion: "1", CorpusID: "corpus-a", Publisher: "patty",
		Sources: []string{"kakao-login"},
		Chunks: []ChunkIn{
			{SourceID: "kakao-login", DocPath: "docs/login.md", Body: "카카오 로그인 첫 단계", Version: "2.0.0", LibraryID: "kakao-login", LineStart: 3, LineEnd: 6},
		},
	}
	mb, _ := json.Marshal(man)
	pkg, err := svc.ImportPackage("org-r", "patty", "detached-sig-123", "usr", mb)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.State != "staged" || pkg.ChunkCount != 1 {
		t.Fatalf("package stage wrong: %+v", pkg)
	}
	var chunks int64
	db.Model(&models.ReferenceChunk{}).Where("package_id = ?", pkg.ID).Count(&chunks)
	if chunks != 1 {
		t.Fatalf("expected 1 chunk, got %d", chunks)
	}

	// Activate (must be persisted package id, not package_id column).
	var row models.ReferencePackage
	if err := db.Where("organization_id = ? AND id = ?", "org-r", pkg.ID).First(&row).Error; err != nil {
		t.Fatalf("package persisted? %v", err)
	}
	if err := svc.ActivatePackage("org-r", row.ID, "usr", "activate"); err != nil {
		t.Fatal(err)
	}
	var state models.ReferenceCatalogState
	if err := db.Where("organization_id = ?", "org-r").First(&state).Error; err != nil || state.ActivePackageID != pkg.PackageID {
		t.Fatalf("catalog state not set: %+v err=%v", state, err)
	}
	if err := svc.RollbackPackage("org-r", row.ID, "usr"); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", row.ID).First(&row).Error; err != nil || row.State != "rolled_back" {
		t.Fatalf("rollback failed: %+v", row)
	}
}

// Path traversal and NUL-injected payloads are rejected at import.
func TestPackageRejectsUnsafeChunks(t *testing.T) {
	db := refDB(t)
	svc := New(db)
	seedNCPSources(t, svc, "org-r")
	man := Manifest{
		SchemaVersion: "1", CorpusID: "c", Publisher: "patty", Sources: []string{"kakao-login"},
		Chunks: []ChunkIn{
			{SourceID: "kakao-login", DocPath: "../etc/passwd", Body: "bad path", Version: "2.0.0"},
			{SourceID: "kakao-login", DocPath: "docs/ok.md", Body: "carries \x00 nul", Version: "2.0.0"},
			{SourceID: "kakao-login", DocPath: "docs/good.md", Body: "안전한 본문", Version: "2.0.0"},
		},
	}
	mb, _ := json.Marshal(man)
	pkg, err := svc.ImportPackage("org-r", "patty", "sig", "usr", mb)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []models.ReferenceChunk
	db.Where("package_id = ?", pkg.ID).Find(&chunks)
	if len(chunks) != 1 || chunks[0].DocPath != "docs/good.md" {
		t.Fatalf("unsafe chunks not rejected: %+v", chunks)
	}
}

func TestTokenizePreservesCodeIdentifiers(t *testing.T) {
	toks := tokenize("const mapToken = createMap(); 네이버 지도")
	joined := strings.Join(toks, " ")
	if !strings.Contains(joined, "maptoken") {
		t.Fatalf("code identifier lost: %v", toks)
	}
	if !strings.Contains(joined, "지도") {
		t.Fatalf("korean word lost: %v", toks)
	}
	_ = fmt.Sprintf
}
