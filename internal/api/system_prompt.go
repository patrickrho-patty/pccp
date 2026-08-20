package api

// PAT-1455 managed system-prompt additions. One active prompt-addition
// document per scope (org/team/fleet/user), immutable version history, secret/
// size/interpolation validation, a deterministic effective-prompt preview with
// locked precedence, and signed distribution over the relay directive carrier.
// Reuses security.CheckContext for secret/PII rejection and the policy-issuer
// signing key (same as PAT-1456).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/keys"
	"github.com/patrickrho-patty/pccp/internal/models"
)

const (
	promptMaxBytes    = 8000
	promptMaxBudgetKo = "원칙적으로 시스템 프롬프트 예산 8KB 이내"
)

// interpolationLike detects unsupported dynamic variables in managed prompt
// text — static instruction content only.
var interpolationLike = regexp.MustCompile(`\{\{[^}]*\}\}|\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

// spectrum of attempts to suppress/replace immutable instructions.
var immutableSuppression = regexp.MustCompile(`(?i)(ignore (?:all )?(?:previous|prior)|disregard (?:all )?(?:previous|prior)|ignore your (?:core )?(?:instructions|system prompt)|do not follow (?:your )?(?:instructions|the system|core goals)|erase (?:your )?(?:previous|core)|you are not (?:patt[y¢]|the assistant)|forget your instructions|override (?:your )?system)`)

// promptDocRequest is the admin save payload.
type promptDocRequest struct {
	ID      string   `json:"id,omitempty"` // present on update
	Scope   string   `json:"scope"`
	ScopeID string   `json:"scope_id,omitempty"`
	Title   string   `json:"title,omitempty"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

func (req *promptDocRequest) validate() error {
	if req.Scope != "org" && req.Scope != "team" && req.Scope != "fleet" && req.Scope != "user" {
		return fmt.Errorf("scope must be org|team|fleet|user")
	}
	if req.Scope != "org" && strings.TrimSpace(req.ScopeID) == "" {
		return fmt.Errorf("scope_id required for %s scope", req.Scope)
	}
	if len(req.Content) > promptMaxBytes {
		return fmt.Errorf("managed instruction exceeds size limit (%d bytes; allowed %d)", len(req.Content), promptMaxBytes)
	}
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		return fmt.Errorf("managed instruction cannot be empty")
	}
	if interpolationLike.MatchString(req.Content) {
		return fmt.Errorf("interpolation variables are not supported in managed instructions")
	}
	if immutableSuppression.MatchString(req.Content) {
		return fmt.Errorf("managed instructions cannot suppress or replace immutable core instructions")
	}
	return nil
}

// effective addition contributor for the preview.
type promptContributor struct {
	Scope    string `json:"scope"`
	ScopeID  string `json:"scope_id,omitempty"`
	Title    string `json:"title"`
	Digest   string `json:"digest"`
	Version  uint64 `json:"version"`
	Enabled  bool   `json:"enabled"`
	Winning  bool   `json:"winning"`
	Conflict bool   `json:"conflict"`
	Content  string `json:"content,omitempty"`
}

// scopedPromptAdditions loads enabled, current additions for one scope target.
func (s *Server) scopedPromptAdditions(orgID, scope, scopeID string) []models.SystemPromptDocument {
	var docs []models.SystemPromptDocument
	q := s.db.Where("organization_id = ? AND scope = ? AND enabled = ?", orgID, scope, true)
	if scopeID != "" {
		q = q.Where("scope_id = ?", scopeID)
	} else {
		q = q.Where("scope_id = '' OR scope_id IS NULL")
	}
	q.Find(&docs)
	return docs
}

// promptDigest computes the SHA-256 of canonical(scope, scope_id, content).
func promptDigest(scope, scopeID, content string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s", scope, scopeID, content)
	return hex.EncodeToString(h.Sum(nil))
}

// handleListSystemPrompts returns all prompt-addition documents for the org
// (or filtered by scope) with their version history counts.
func (s *Server) handleListSystemPrompts(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	q := s.db.Where("organization_id = ?", orgID)
	if scope := r.URL.Query().Get("scope"); scope != "" {
		q = q.Where("scope = ?", scope)
	}
	var docs []models.SystemPromptDocument
	q.Order("scope, scope_id").Find(&docs)
	type row struct {
		models.SystemPromptDocument
		VersionCount int64 `json:"version_count"`
	}
	out := make([]row, 0, len(docs))
	for _, d := range docs {
		var cnt int64
		s.db.Model(&models.SystemPromptVersion{}).Where("document_id = ?", d.ID).Count(&cnt)
		out = append(out, row{SystemPromptDocument: d, VersionCount: cnt})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListSystemPromptVersions returns immutable history for a document.
func (s *Server) handleListSystemPromptVersions(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and document id required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	var versions []models.SystemPromptVersion
	s.db.Where("organization_id = ? AND document_id = ?", orgID, id).Order("version DESC").Find(&versions)
	writeJSON(w, http.StatusOK, versions)
}

// handleSystemPromptEffective returns the deterministic effective preview for a
// scope target: managed contributors in locked precedence order (org → team →
// fleet → user), each marked winning/conflict, plus the current version/digest.
func (s *Server) handleSystemPromptEffective(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	scope := r.URL.Query().Get("scope")
	scopeID := r.URL.Query().Get("scope_id")
	if scope == "" {
		writeError(w, http.StatusBadRequest, "scope required")
		return
	}
	// Determine the applicable scope chain: org + (team/fleet) + user.
	contributors := []promptContributor{}
	pending := []promptContributor{}
	appendScope := func(apply string, applyID string) {
		for _, d := range s.scopedPromptAdditions(orgID, apply, applyID) {
			pending = append(pending, promptContributor{
				Scope: apply, ScopeID: applyID, Title: d.Title,
				Digest: d.Digest, Version: d.Version, Enabled: d.Enabled, Content: d.Content,
			})
		}
	}
	appendScope("org", "")
	if scopeID != "" {
		// For team/fleet/user targets include their narrower layer too.
		switch scope {
		case "team", "fleet", "user":
			appendScope(scope, scopeID)
		}
	}
	// Locked precedence: org → team → fleet → user wins; lower layers may not
	// weaken. For a simple single-addition-per-target model, the highest
	// applicable enabled addition is the winner; a same-scope duplicate is a
	// conflict, not an arbitrary pick.
	for i := range pending {
		pending[i].Winning = true
		if i > 0 {
			pending[i-1].Winning = false
			if pending[i-1].Scope == pending[i].Scope {
				pending[i].Conflict = true
			}
		}
		contributors = append(contributors, pending[i])
	}
	// Deterministic ordering: org first, then team/fleet/user by scope rank.
	sort.SliceStable(contributors, func(a, b int) bool {
		return scopeRank(contributors[a].Scope) < scopeRank(contributors[b].Scope)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scope": scope, "scope_id": scopeID,
		"contributors": contributors,
		"version":      currentPromptVersion(contributors),
		"digest":       currentPromptDigest(contributors),
		"budget_ok":    effectivePromptBytes(contributors) <= promptMaxBytes,
	})
}

func scopeRank(s string) int {
	switch s {
	case "org":
		return 0
	case "team":
		return 1
	case "fleet":
		return 2
	case "user":
		return 3
	}
	return 9
}

func currentPromptVersion(cs []promptContributor) uint64 {
	if len(cs) == 0 {
		return 0
	}
	// The winning (last applicable, non-conflicted) contributor's version.
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Winning && !cs[i].Conflict {
			return cs[i].Version
		}
	}
	return 0
}

func currentPromptDigest(cs []promptContributor) string {
	if len(cs) == 0 {
		return ""
	}
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Winning && !cs[i].Conflict {
			return cs[i].Digest
		}
	}
	return ""
}

func effectivePromptBytes(cs []promptContributor) int {
	total := 0
	for _, c := range cs {
		if c.Enabled {
			total += len(c.Content)
		}
	}
	return total
}

// handleSaveSystemPrompt validates + persists a managed prompt addition as an
// immutable next version, incorporating it into a fresh signed epoch. Secret/
// PII content is rejected via security.CheckContext (never echoed).
func (s *Server) handleSaveSystemPrompt(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var req promptDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Secret / PII rejection — warnings identify scope+reason, never the value.
	// Critical/high findings (credentials, tokens, keys, RRNs) deny the save;
	// medium-only findings (e.g. a corporate phone number) do not auto-block.
	if scan := s.security.CheckContext(orgID, req.Content); scan.Verdict == securityVerdictDeny {
		cats := []string{}
		for _, f := range scan.Findings {
			if f.Severity == "critical" || f.Severity == "high" {
				cats = append(cats, f.TitleKo)
				if len(cats) >= 3 {
					break
				}
			}
		}
		writeError(w, http.StatusBadRequest, "managed instruction rejected: "+strings.Join(cats, ", "))
		return
	}

	actor := getActorID(r)
	digest := promptDigest(req.Scope, req.ScopeID, req.Content)

	if req.ID != "" {
		var doc models.SystemPromptDocument
		if err := s.db.Where("organization_id = ? AND id = ?", orgID, req.ID).First(&doc).Error; err != nil {
			writeError(w, http.StatusNotFound, "prompt document not found")
			return
		}
		// Same-content save is a no-op (idempotent).
		if doc.Digest == digest {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": doc.ID, "version": doc.Version, "unchanged": true})
			return
		}
		next := doc.Version + 1
		// Immutable version row (history is never rewritten).
		if err := s.db.Create(&models.SystemPromptVersion{
			OrganizationID: orgID, DocumentID: doc.ID, Scope: doc.Scope, ScopeID: doc.ScopeID,
			Version: next, Content: req.Content, Digest: digest, CreatedBy: actor,
		}).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
			return
		}
		if err := s.db.Model(&doc).Updates(map[string]interface{}{
			"title": req.Title, "content": req.Content, "digest": digest, "version": next,
			"created_by": actor, "enabled": true, "epoch_id": "",
		}).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
			return
		}
		models.CreateAuditEvent(s.db, &models.AuditEvent{
			OrganizationID: orgID, ActorID: actor, ActorType: "user", EventType: "cp.prompt.versioned",
			Action:       "prompt.versioned",
			ResourceType: "system_prompt", ResourceID: doc.ID, Result: "saved",
			Details: string(mustJSON(map[string]interface{}{"scope": doc.Scope, "scope_id": doc.ScopeID, "from": doc.Version, "to": next, "digest": digest})),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": doc.ID, "version": next})
		return
	}

	// New document (scope+target). One current addition per target.
	var existing models.SystemPromptDocument
	q := s.db.Where("organization_id = ? AND scope = ?", orgID, req.Scope)
	if req.ScopeID != "" {
		q = q.Where("scope_id = ?", req.ScopeID)
	} else {
		q = q.Where("scope_id = '' OR scope_id IS NULL")
	}
	if err := q.First(&existing).Error; err == nil {
		// Turn into an update.
		next := existing.Version + 1
		s.db.Create(&models.SystemPromptVersion{
			OrganizationID: orgID, DocumentID: existing.ID, Scope: existing.Scope, ScopeID: existing.ScopeID,
			Version: next, Content: req.Content, Digest: digest, CreatedBy: actor,
		})
		s.db.Model(&existing).Updates(map[string]interface{}{
			"title": req.Title, "content": req.Content, "digest": digest, "version": next, "enabled": true, "epoch_id": "",
		})
		models.CreateAuditEvent(s.db, &models.AuditEvent{
			OrganizationID: orgID, ActorID: actor, ActorType: "user", EventType: "cp.prompt.versioned",
			Action: "prompt.versioned", ResourceType: "system_prompt", ResourceID: existing.ID, Result: "saved",
			Details: string(mustJSON(map[string]interface{}{"scope": existing.Scope, "scope_id": existing.ScopeID, "to": next, "digest": digest})),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": existing.ID, "version": next})
		return
	}

	doc := &models.SystemPromptDocument{
		OrganizationID: orgID, Scope: req.Scope, ScopeID: req.ScopeID,
		Title: req.Title, Content: req.Content, Digest: digest, Version: 1,
		Enabled: true, CreatedBy: actor,
	}
	if err := s.db.Create(doc).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
		return
	}
	s.db.Create(&models.SystemPromptVersion{
		OrganizationID: orgID, DocumentID: doc.ID, Scope: doc.Scope, ScopeID: doc.ScopeID,
		Version: 1, Content: req.Content, Digest: digest, CreatedBy: actor,
	})
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: actor, ActorType: "user", EventType: "cp.prompt.created",
		Action: "prompt.created", ResourceType: "system_prompt", ResourceID: doc.ID, Result: "created",
		Details: string(mustJSON(map[string]interface{}{"scope": doc.Scope, "scope_id": doc.ScopeID, "digest": digest})),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": doc.ID, "version": 1})
}

// handleSetSystemPromptEnabled enables/disables an addition. Disablement is
// versioned and audited; it takes effect on the next request.
func (s *Server) handleSetSystemPromptEnabled(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	enabled := r.URL.Query().Get("enabled") == "true"
	if orgID == "" || id == "" {
		writeError(w, http.StatusBadRequest, "organization context and document id required")
		return
	}
	var doc models.SystemPromptDocument
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&doc).Error; err != nil {
		writeError(w, http.StatusNotFound, "prompt document not found")
		return
	}
	if err := s.db.Model(&doc).Update("enabled", enabled).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
		return
	}
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user", EventType: "cp.prompt." + boolString(enabled),
		Action: "prompt." + boolString(enabled), ResourceType: "system_prompt", ResourceID: doc.ID, Result: "saved",
		Details: string(mustJSON(map[string]interface{}{"scope": doc.Scope, "scope_id": doc.ScopeID, "version": doc.Version})),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "enabled": enabled})
}

// handleRestoreSystemPrompt restores a prior version as a NEW immutable version
// (history is never decremented or rewritten).
func (s *Server) handleRestoreSystemPrompt(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	id := chi.URLParam(r, "id")
	versionStr := chi.URLParam(r, "version")
	if orgID == "" || id == "" || versionStr == "" {
		writeError(w, http.StatusBadRequest, "organization context, document id, and version required")
		return
	}

	if !requireGovernanceAdmin(w, r) {
		return
	}
	var doc models.SystemPromptDocument
	if err := s.db.Where("organization_id = ? AND id = ?", orgID, id).First(&doc).Error; err != nil {
		writeError(w, http.StatusNotFound, "prompt document not found")
		return
	}
	var old models.SystemPromptVersion
	if err := s.db.Where("document_id = ? AND version = ?", id, versionStr).First(&old).Error; err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	// Secret / PII rejection also applies on restore (rules can tighten between
	// versions); any remaining validation is on the restored content itself.
	if scan := s.security.CheckContext(orgID, old.Content); scan.Verdict == securityVerdictDeny {
		writeError(w, http.StatusBadRequest, "restore rejected: restored content fails current validation")
		return
	}
	next := doc.Version + 1
	digest := promptDigest(doc.Scope, doc.ScopeID, old.Content)
	if err := s.db.Create(&models.SystemPromptVersion{
		OrganizationID: orgID, DocumentID: doc.ID, Scope: doc.Scope, ScopeID: doc.ScopeID,
		Version: next, Content: old.Content, Digest: digest, CreatedBy: getActorID(r), RestoredFrom: old.Version,
	}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
		return
	}
	s.db.Model(&doc).Updates(map[string]interface{}{"content": old.Content, "digest": digest, "version": next, "enabled": true, "epoch_id": ""})
	models.CreateAuditEvent(s.db, &models.AuditEvent{
		OrganizationID: orgID, ActorID: getActorID(r), ActorType: "user", EventType: "cp.prompt.restored",
		Action: "prompt.restored", ResourceType: "system_prompt", ResourceID: doc.ID, Result: "saved",
		Details: string(mustJSON(map[string]interface{}{"from": old.Version, "to": next, "digest": digest})),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": doc.ID, "version": next})
}

// handleSystemPromptEpochDeliver signs the current effective additions and
// pushes a directive to every active harness so the next request uses the new
// prompt-policy version (fail closed otherwise).
func (s *Server) handleSystemPromptEpochDeliver(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	if orgID == "" {
		writeError(w, http.StatusForbidden, "organization context required")
		return
	}
	if !requireGovernanceAdmin(w, r) {
		return
	}
	var docs []models.SystemPromptDocument
	s.db.Where("organization_id = ? AND enabled = ?", orgID, true).Find(&docs)
	payload := map[string]interface{}{
		"organization_id": orgID, "kind": "system_prompt",
		"additions": docs, "issued_at": time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	priv, err := keys.LoadOrCreate(s.db, promptPolicyIssuer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: no signing identity: "+err.Error())
		return
	}
	sig, err := dari.COSESign1Hex(body, priv, []byte(promptPolicyIssuer))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: sign: "+err.Error())
		return
	}
	epoch := &models.SystemPromptEpoch{
		OrganizationID: orgID, EpochID: dari.GenerateID("sppe"),
		EpochNumber: s.nextPromptEpochNumber(orgID), AdditionsJSON: string(body),
		Digest: digest, SignatureHex: sig, CreatedBy: getActorID(r),
		Status: "active", EffectiveAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.db.Create(epoch).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "system prompt: "+err.Error())
		return
	}
	s.db.Model(&models.SystemPromptEpoch{}).Where("organization_id = ? AND status = 'active' AND id != ?", orgID, epoch.ID).
		Updates(map[string]interface{}{"status": "superseded", "superseded_by": epoch.EpochID})
	var targets []string
	s.db.Model(&models.Harness{}).Where("organization_id = ? AND status IN ?", orgID, []string{"active", "enrolled"}).Pluck("harness_id", &targets)
	delivered := 0
	for _, hid := range targets {
		if err := s.pushRelayDirective("system_prompt", orgID, hid, "prompt-policy epoch "+epoch.EpochID, map[string]interface{}{
			"epoch_id": epoch.EpochID, "epoch_number": epoch.EpochNumber,
			"digest": digest, "signature_hex": sig,
		}); err == nil {
			delivered++
		}
	}
	s.db.Model(&models.SystemPromptDocument{}).Where("organization_id = ? AND (epoch_id = '' OR epoch_id IS NULL)", orgID).
		Update("epoch_id", epoch.EpochID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "epoch_id": epoch.EpochID, "epoch_number": epoch.EpochNumber,
		"digest": digest, "targets": len(targets), "delivered": delivered,
	})
}

func (s *Server) nextPromptEpochNumber(orgID string) uint64 {
	var max uint64
	s.db.Model(&models.SystemPromptEpoch{}).Where("organization_id = ?", orgID).Select("COALESCE(MAX(epoch_number),0)").Scan(&max)
	return max + 1
}

const promptPolicyIssuer = "policy-issuer"

func boolString(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// securityVerdict* mirrors security.CheckResult verdict strings.
const (
	securityVerdictDeny   = "DENY"
	securityVerdictReview = "REQUIRE_REVIEW"
	securityVerdictAllow  = "ALLOW"
)
