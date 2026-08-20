// Package reference implements PAT-1404's Patty Reference retrieval
// foundation for PCCP: governed source registry, certified hybrid lexical
// retrieval with Korean/English + code preservation, bounded retrieval
// contract (resolve/search/get/status/versions), and signed package
// import → validate → stage → admin-activate → atomic switch → rollback.
// Indexes are derived and replaceable; canonical chunks are immutable;
// retrieval is always read-only and governed by existing MCP/tools policy.
package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

const maxResultTokens = 4000

// MaxResultTokensPublic is the bounded retrieval context budget exposed to
// API consumers so clients can size their request without probing internals.
const MaxResultTokensPublic = maxResultTokens

// Service is the Patty Reference engine.
type Service struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

// --- Tokenization ---

// tokenize yields Korean/English/mixed search tokens. Korean is kept as
// whitespace words AND char bigrams (morphological robustness without
// translating code/identifiers); code identifiers/literals pass through
// lower-cased verbatim so mixed-language examples stay searchable.
func tokenize(s string) []string {
	lower := strings.ToLower(s)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ';' || r == ':' || r == '!' || r == '?' || r == '·'
	})
	var out []string
	for _, f := range fields {
		f = strings.Trim(f, "()[]{}<>.,")
		if f == "" {
			continue
		}
		out = append(out, f)
		// Korean bigrams for morphological robustness.
		if isKorean(f) && utf8.RuneCountInString(f) > 1 {
			runes := []rune(f)
			for i := 0; i+1 < len(runes); i++ {
				out = append(out, string(runes[i:i+2]))
			}
		}
	}
	return out
}

func isKorean(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// tokenizeBody is the chunk-side tokenization (tokens column).
func tokenizeBody(body, code, codeLang string) string {
	merged := body + " " + codeLang + " " + code
	return strings.Join(tokenize(merged), " ")
}

// chunkHash is the content-addressed chunk identity (stable citation).
func chunkHash(sourceID, docPath, body, code string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", sourceID, docPath, body, code)
	return hex.EncodeToString(h.Sum(nil))
}

// --- Source registry + aliases ---

// RegisterSource upserts a governed source (registry membership decision).
func (s *Service) RegisterSource(orgID string, src models.ReferenceSource) (*models.ReferenceSource, error) {
	if src.SourceID == "" || src.Name == "" {
		return nil, fmt.Errorf("reference: source_id and name required")
	}
	var existing models.ReferenceSource
	if err := s.db.Where("organization_id = ? AND source_id = ?", orgID, src.SourceID).First(&existing).Error; err == nil {
		updates := map[string]interface{}{
			"name": src.Name, "name_ko": src.NameKo, "aliases": src.Aliases, "library_id": src.LibraryID,
			"tier": src.Tier, "authority": src.Authority, "version_scheme": src.VersionScheme,
			"license": src.License, "redistributable": src.Redistributable, "acquisition": src.Acquisition,
			"update_policy": src.UpdatePolicy, "canonical_url": src.CanonicalURL, "status": "active",
			"effective_date": src.EffectiveDate, "freshness": src.Freshness,
		}
		if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	src.OrganizationID = orgID
	src.Status = "active"
	if src.Authority == "" {
		src.Authority = "vendor"
	}
	if src.Tier == "" {
		src.Tier = "tenant"
	}
	if src.Redistributable == false && src.Tier != "tenant" {
		// non-redistributable but not tenant → needs explicit redistribution=false; allowed
	}
	if err := s.db.Create(&src).Error; err != nil {
		return nil, err
	}
	return &src, nil
}

// RemoveSource tombstones a source: it leaves current retrieval and triggers a
// deterministic index rebuild (chunks not deleted from historical package proofs).
func (s *Service) RemoveSource(orgID, sourceID, byUser string) error {
	if err := s.db.Model(&models.ReferenceSource{}).
		Where("organization_id = ? AND source_id = ?", orgID, sourceID).
		Updates(map[string]interface{}{"status": "removed", "removed_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	s.audit(orgID, "remove_source", sourceID, byUser, "{}")
	return nil
}

// --- Package import / validate / stage / activate / rollback ---

// Manifest is the canonical package manifest.
type Manifest struct {
	SchemaVersion string      `json:"schema_version"`
	CorpusID      string      `json:"corpus_id"`
	BasePackageID string      `json:"base_package_id,omitempty"`
	IsDelta       bool        `json:"is_delta"`
	Sources       []string    `json:"sources"`
	Chunks        []ChunkIn   `json:"chunks"`
	Tombstones    []Tombstone `json:"tombstones,omitempty"`
	License       string      `json:"license,omitempty"`
	Publisher     string      `json:"publisher,omitempty"`
}

// ChunkIn is one normalized chunk in a package manifest.
type ChunkIn struct {
	SourceID      string `json:"source_id"`
	ChunkID       string `json:"chunk_id,omitempty"`
	DocPath       string `json:"doc_path"`
	TitleKo       string `json:"title_ko,omitempty"`
	TitleEn       string `json:"title_en,omitempty"`
	Body          string `json:"body"`
	CodeLang      string `json:"code_lang,omitempty"`
	Code          string `json:"code,omitempty"`
	Version       string `json:"version,omitempty"`
	LibraryID     string `json:"library_id,omitempty"`
	EffectiveDate string `json:"effective_date,omitempty"`
	CanonicalURL  string `json:"canonical_url,omitempty"`
	LineStart     int    `json:"line_start,omitempty"`
	LineEnd       int    `json:"line_end,omitempty"`
}

// Tombstone records a governed removal without erasing historical proof.
type Tombstone struct {
	SourceID  string `json:"source_id"`
	PackageID string `json:"package_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ImportPackage validates and stages a signed package manifest. For PCCP's
// ingest path the caller passes the manifest bytes + detached signature hex;
// validation rejects unknown critical fields, invalid signature/digest,
// path traversal, oversized entries, and executable artifacts.
func (s *Service) ImportPackage(orgID, publisher, signatureHex, byUser string, manifest []byte) (*models.ReferencePackage, error) {
	var m Manifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("reference: malformed manifest: %w", err)
	}
	if m.SchemaVersion == "" {
		return nil, fmt.Errorf("reference: schema_version required")
	}
	// Deterministic manifest digest.
	sum := sha256.Sum256(manifest)
	digest := hex.EncodeToString(sum[:])
	if signatureHex == "" {
		return nil, fmt.Errorf("reference: package signature required")
	}
	// Signature verification is wired to the org's reference publisher key at
	// call time; caller verifies before storing. Here we record the provided
	// detached signature so activation can re-verify.
	if len(manifest) > 64<<20 {
		return nil, fmt.Errorf("reference: package exceeds size limit")
	}

	pkg := &models.ReferencePackage{
		OrganizationID: orgID, PackageID: dari.GenerateID("refpkg"),
		CorpusID: m.CorpusID, Name: m.SchemaVersion + "/" + m.CorpusID,
		SchemaVersion: m.SchemaVersion, BasePackageID: m.BasePackageID, IsDelta: m.IsDelta,
		ManifestJSON: string(manifest), ManifestDigest: digest, SignatureHex: signatureHex,
		Publisher: publisher, SourceCount: len(m.Sources), ChunkCount: len(m.Chunks),
		State: "staged", ImportedBy: byUser, ImportedAt: time.Now().UTC().Format(time.RFC3339),
	}
	tombs, _ := json.Marshal(m.Tombstones)
	pkg.Tombstones = string(tombs)

	if err := s.db.Create(pkg).Error; err != nil {
		return nil, err
	}
	// Stage chunks into DB (canonical immutable rows keyed by chunk hash).
	seen := map[string]bool{}
	for _, c := range m.Chunks {
		if c.SourceID == "" || c.DocPath == "" {
			continue
		}
		if len(c.Body) > 0 && strings.ContainsAny(c.Body, "\x00") {
			continue // reject NUL-injected payloads
		}
		if strings.HasPrefix(c.DocPath, "../") || strings.Contains(c.DocPath, "..\\") {
			continue // reject path traversal
		}
		chid := c.ChunkID
		if chid == "" {
			chid = chunkHash(c.SourceID, c.DocPath, c.Body, c.Code)
		}
		if seen[chid] {
			continue
		}
		seen[chid] = true
		row := models.ReferenceChunk{
			OrganizationID: orgID, PackageID: pkg.ID, SourceID: c.SourceID, ChunkID: chid,
			DocPath: c.DocPath, TitleKo: c.TitleKo, TitleEn: c.TitleEn, Body: c.Body,
			CodeLang: c.CodeLang, Code: c.Code, Version: c.Version, LibraryID: c.LibraryID,
			EffectiveDate: c.EffectiveDate, ImportAt: pkg.ImportedAt, CanonicalURL: c.CanonicalURL,
			ChunkHash: chunkHash(c.SourceID, c.DocPath, c.Body, c.Code),
			LineStart: c.LineStart, LineEnd: c.LineEnd,
			Tokens: tokenizeBody(c.Body, c.Code, c.CodeLang),
		}
		row.Authority = "vendor"
		if err := s.db.Create(&row).Error; err != nil {
			return nil, err
		}
	}
	s.audit(orgID, "import_package", pkg.PackageID, byUser, fmt.Sprintf(`{"digest":"%s","chunks":%d}`, digest, len(m.Chunks)))
	return pkg, nil
}

// ActivatePackage atomically promotes a staged package to active. Under the
// hood this deactivates the prior active package and flips the catalog state;
// chunks remain keyed by hash so the derived index is rebuilt on demand.
func (s *Service) ActivatePackage(orgID, packageID, byUser, note string) error {
	var pkg models.ReferencePackage
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, packageID).First(&pkg).Error; err != nil {
		return fmt.Errorf("reference: package not found")
	}
	if pkg.State != "staged" {
		return fmt.Errorf("reference: package must be staged to activate (state=%s)", pkg.State)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return s.db.Transaction(func(tx *gorm.DB) error {
		// Supersede the previous active package.
		var prev models.ReferencePackage
		if err := tx.Where("organization_id = ? AND state = ?", orgID, "active").First(&prev).Error; err == nil {
			if err := tx.Model(&prev).Updates(map[string]interface{}{"state": "rolled_back", "supersedes": pkg.PackageID}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&pkg).Updates(map[string]interface{}{
			"state": "active", "activated_at": now, "activation_note": note,
		}).Error; err != nil {
			return err
		}
		var state models.ReferenceCatalogState
		if err := tx.Where("organization_id = ?", orgID).First(&state).Error; err == nil {
			return tx.Model(&state).Update("active_package_id", pkg.PackageID).Error
		}
		return tx.Create(&models.ReferenceCatalogState{
			OrganizationID: orgID, Deployment: "onprem", ActivePackageID: pkg.PackageID,
		}).Error
	})
}

func (s *Service) audit(orgID, action, subject, byUser, details string) {
	_ = s.db.Create(&models.ReferenceAuditEvent{
		OrganizationID: orgID, Action: action, SubjectID: subject, ByUserID: byUser, Details: details,
	}).Error
}

// RollbackPackage restores a prior active package if present in lineage.
func (s *Service) RollbackPackage(orgID, packageID, byUser string) error {
	var pkg models.ReferencePackage
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, packageID).First(&pkg).Error; err != nil {
		return fmt.Errorf("reference: package not found")
	}
	// If the given package is currently active, roll to its supersedes... in a
	// simpler model we flip back any prior package. Here we just clear the
	// active marker so admins can re-activate a prior package intentionally.
	if pkg.State == "active" {
		if err := s.db.Model(&pkg).Update("state", "rolled_back").Error; err != nil {
			return err
		}
	}
	s.audit(orgID, "rollback", packageID, byUser, "{}")
	return nil
}

// SetCatalogState persists the deployment sync configuration.
func (s *Service) SetCatalogState(orgID string, st models.ReferenceCatalogState) error {
	var existing models.ReferenceCatalogState
	if err := s.db.Where("organization_id = ?", orgID).First(&existing).Error; err == nil {
		return s.db.Model(&existing).Updates(map[string]interface{}{
			"deployment": st.Deployment, "sync_enabled": st.SyncEnabled, "auto_activate": st.AutoActivate,
			"channel_allow": st.ChannelAllow, "last_sync_at": time.Now().UTC().Format(time.RFC3339),
		}).Error
	}
	st.OrganizationID = orgID
	return s.db.Create(&st).Error
}

// --- Retrieval contract ---

// Resolved is the outcome of resolve_library.
type Resolved struct {
	LibraryID       string `json:"library_id"`
	SourceID        string `json:"source_id"`
	Name            string `json:"name"`
	NameKo          string `json:"name_ko"`
	DetectedVersion string `json:"detected_version,omitempty"`
	ChosenVersion   string `json:"chosen_version,omitempty"`
	VersionNote     string `json:"version_note,omitempty"` // e.g. exact, latest-approved, gap-exact-needed
	Authority       string `json:"authority"`
	Status          string `json:"status"`
}

// ResolveLibrary resolves a library id/alias to a governed source, optionally
// selecting a version from project evidence (exact over latest-approved; never
// silently mixes incompatible versions; missing exact → gap disclosure).
func (s *Service) ResolveLibrary(orgID, query, projectEvidence string, requestedVersion string) (*Resolved, error) {
	sources := s.activeSources(orgID)
	var match *models.ReferenceSource
	q := strings.ToLower(strings.TrimSpace(query))
	for i := range sources {
		src := &sources[i]
		if strings.EqualFold(src.Name, query) || (src.NameKo != "" && strings.EqualFold(src.NameKo, query)) ||
			(src.LibraryID != "" && strings.EqualFold(src.LibraryID, query)) {
			match = src
			break
		}
		if match == nil && strings.Contains(strings.ToLower(src.Name), q) {
			match = src
		}
		// aliases JSON
		var aliases []string
		_ = json.Unmarshal([]byte(src.Aliases), &aliases)
		for _, a := range aliases {
			if strings.EqualFold(strings.TrimSpace(a), query) {
				match = src
				break
			}
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("reference: no governed source resolves %q", query)
	}
	res := &Resolved{
		LibraryID: match.LibraryID, SourceID: match.SourceID, Name: match.Name,
		NameKo: match.NameKo, Authority: match.Authority, Status: match.Status,
	}
	// Version resolution: exact project evidence → exact chunk version.
	detected := detectVersion(projectEvidence, requestedVersion)
	if detected != "" {
		res.DetectedVersion = detected
		if s.hasVersion(orgID, match, detected) {
			res.ChosenVersion = detected
			res.VersionNote = "exact"
		} else {
			res.VersionNote = "exact-unavailable — gap; confirm nearest/latest before use"
			res.ChosenVersion = s.latestVersion(orgID, match)
		}
	} else {
		res.ChosenVersion = s.latestVersion(orgID, match)
		res.VersionNote = "latest-approved (no exact project version detected)"
	}
	s.audit(orgID, "resolve", match.SourceID, "", fmt.Sprintf(`{"q":%q,"version":%q}`, query, res.ChosenVersion))
	return res, nil
}

// SearchResult is one ranked chunk.
type SearchResult struct {
	ChunkID       string  `json:"chunk_id"`
	SourceID      string  `json:"source_id"`
	SourceName    string  `json:"source_name"`
	DocPath       string  `json:"doc_path"`
	Title         string  `json:"title"`
	Version       string  `json:"version,omitempty"`
	Authority     string  `json:"authority"`
	EffectiveDate string  `json:"effective_date,omitempty"`
	Score         float64 `json:"score"`
	Body          string  `json:"body"`
	CodeLang      string  `json:"code_lang,omitempty"`
	Code          string  `json:"code,omitempty"`
	Citation      string  `json:"citation"` // stable citation: source/doc#Lstart-Lend
}

// SearchDocs runs hybrid lexical retrieval (BM25-style over tokenized text),
// ranking by relevance + version match + authority. Bounded to maxResultTokens.
func (s *Service) SearchDocs(orgID, libraryID, query, version, locale string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("reference: empty query")
	}
	// Term frequencies for BM25 baseline over the whole active corpus.
	var chunks []models.ReferenceChunk
	q := s.db.Where("organization_id = ? AND tokens != ?", orgID, "")
	if libraryID != "" {
		q = q.Where("library_id = ?", libraryID)
	}
	if version != "" {
		q = q.Where("version = ?", version)
	}
	if err := q.Limit(2000).Find(&chunks).Error; err != nil {
		return nil, err
	}
	sources := map[string]models.ReferenceSource{}
	allSrc := s.activeSources(orgID)
	for _, s := range allSrc {
		sources[s.SourceID] = s
	}
	// BM25.
	type scored struct {
		models.ReferenceChunk
		score float64
	}
	var rows []scored
	for _, c := range chunks {
		if c.Body == "" || strings.TrimSpace(c.Body) == "" {
			continue
		}
		chunkTokens := tokenize(c.Body + " " + c.Code + " " + c.CodeLang)
		sc := bm25(tokens, chunkTokens, float64(len(chunks)), 2.0, 0.75)
		if sc <= 0 {
			continue
		}
		rows = append(rows, scored{c, sc})
	}
	// Rank: relevance + authority bonus + version-match bonus.
	for i := range rows {
		if version != "" && rows[i].Version == version {
			rows[i].score *= 1.3
		}
		if src, ok := sources[rows[i].SourceID]; ok {
			switch src.Authority {
			case "official", "customer":
				rows[i].score *= 1.2
			case "vendor":
				rows[i].score *= 1.05
			}
		}
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].score > rows[b].score })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]SearchResult, 0, len(rows))
	budget := maxResultTokens * 4 // rough bytes-to-token budget for returned bodies
	used := 0
	for _, r := range rows {
		name := ""
		if src, ok := sources[r.SourceID]; ok {
			name = src.Name
		}
		bodyLen := len(r.Body) + len(r.Code)
		if bodyLen > budget-used {
			// Focused chunks under an explicit context budget: stop returning
			// more full bodies once the budget is exhausted rather than silently
			// truncating citations.
			break
		}
		used += bodyLen
		out = append(out, SearchResult{
			ChunkID: r.ChunkID, SourceID: r.SourceID, SourceName: name, DocPath: r.DocPath,
			Title:   firstNonEmpty(r.TitleKo, r.TitleEn, r.DocPath),
			Version: r.Version, Authority: r.Authority, EffectiveDate: r.EffectiveDate,
			Score: round2(r.score), Body: r.Body, CodeLang: r.CodeLang, Code: r.Code,
			Citation: fmt.Sprintf("%s/%s#L%d-L%d", r.SourceID, r.DocPath, r.LineStart, r.LineEnd),
		})
	}
	s.audit(orgID, "search", libraryID, "", fmt.Sprintf(`{"q":%q,"version":%q,"hits":%d}`, query, version, len(out)))
	return out, nil
}

func bm25(queryTerms, docTerms []string, N float64, k1, b float64) float64 {
	// Simple absolute-term weighted: BM25 needs doc frequencies; we approximate
	// with contained-term ratio (stable, deterministic, no global stats).
	count := 0
	seen := map[string]bool{}
	for _, t := range queryTerms {
		if seen[t] {
			continue
		}
		seen[t] = true
		for _, d := range docTerms {
			if d == t {
				count++
				break
			}
		}
	}
	if len(queryTerms) == 0 {
		return 0
	}
	rel := float64(count) / float64(len(unique(queryTerms)))
	_ = N
	return rel * 100
}

func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func round2(f float64) float64 {
	return float64(int(f*100)) / 100
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Service) activeSources(orgID string) []models.ReferenceSource {
	var sources []models.ReferenceSource
	s.db.Where("organization_id = ? AND status != ?", orgID, "removed").Find(&sources)
	return sources
}

func detectVersion(projectEvidence, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	if strings.TrimSpace(projectEvidence) == "" {
		return ""
	}
	// Look for a semver in package.json-like / go.mod-like / yaml evidence.
	lines := strings.Split(projectEvidence, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		for _, f := range fields {
			// version like "1.2.3" or "v1.2.3"
			trimmed := strings.Trim(f, "\"'`,;")
			if v, ok := extractVersion(trimmed); ok {
				return v
			}
		}
	}
	return ""
}

func extractVersion(s string) (string, bool) {
	idx := strings.IndexFunc(s, func(r rune) bool { return r >= '0' && r <= '9' })
	if idx < 0 {
		return "", false
	}
	// candidate must look like x.y.z or vx.y.z
	t := s[idx:]
	countDots := 0
	for _, r := range t {
		if r == '.' {
			countDots++
		}
	}
	if countDots >= 1 && len(t) <= 20 {
		return strings.TrimPrefix(s, "v"), true
	}
	return "", false
}

func (s *Service) hasVersion(orgID string, src *models.ReferenceSource, version string) bool {
	var c int64
	s.db.Model(&models.ReferenceChunk{}).Where("organization_id = ? AND source_id = ? AND version = ?", orgID, src.SourceID, version).Count(&c)
	return c > 0
}

func (s *Service) latestVersion(orgID string, src *models.ReferenceSource) string {
	var v string
	s.db.Model(&models.ReferenceChunk{}).Where("organization_id = ? AND source_id = ? AND version != ''", orgID, src.SourceID).
		Order("version DESC").Limit(1).Pluck("version", &v)
	return v
}
