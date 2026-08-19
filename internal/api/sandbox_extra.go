package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

const sandboxImageAllowlistKey = "sandbox.image_allowlist"

var sha256DigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// sandbox_extra.go: web/15 — image allowlist (D). The allowlist is an
// org setting (JSON array of image refs); CreateSandbox enforces it
// fail-closed when configured. PAT-1514 adds canonical SandboxImage
// entries (digest-based, supply-chain evidence) alongside the legacy
// raw-string list.

// handleSandboxImageAllowlist GETs/replaces the org's image allowlist
// (legacy string list) AND returns the canonical SandboxImage entries.
func (s *Server) handleSandboxImageAllowlist(w http.ResponseWriter, r *http.Request) {
	orgID := getOrgID(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"images":     s.sandboxImageAllowlist(orgID),
			"canonical":  s.sandboxCanonicalImageEntries(orgID),
			"enforced":   len(s.sandboxImageAllowlist(orgID)) > 0 || len(s.sandboxCanonicalImageEntries(orgID)) > 0,
		})
	case http.MethodPut:
		var req struct {
			// Legacy: list of image refs (patty/sandbox-base:tag or digest)
			Images []string `json:"images"`
			// Canonical: list of digest-based entries (PAT-1514)
			Canonical []models.SandboxImage `json:"canonical"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		cleaned := make([]string, 0, len(req.Images))
		for _, img := range req.Images {
			if img = strings.TrimSpace(img); img != "" {
				cleaned = append(cleaned, img)
			}
		}
		raw, _ := json.Marshal(cleaned)
		// Validate every canonical entry BEFORE touching any rows so a
		// bad payload can't half-apply. Then replace (delete + insert)
		// plus the org-setting upsert and audit event inside ONE
		// transaction — a failure between delete and insert would
		// otherwise leave the org with zero entries (fail-closed with
		// no allowlist).
		for i := range req.Canonical {
			if err := validateSandboxImage(&req.Canonical[i]); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			req.Canonical[i].OrganizationID = orgID
			req.Canonical[i].Status = "approved"
			if req.Canonical[i].ApprovedAt == "" {
				req.Canonical[i].ApprovedAt = time.Now().UTC().Format(time.RFC3339)
			}
		}
		var setting models.OrgSetting
		settingExists := s.db.Where("organization_id = ? AND key = ?", orgID, sandboxImageAllowlistKey).First(&setting).Error == nil
		txnErr := s.db.Transaction(func(tx *gorm.DB) error {
			if settingExists {
				if err := tx.Model(&setting).Update("value", string(raw)).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Create(&models.OrgSetting{
					Base:           models.Base{ID: models.GenerateID("os")},
					OrganizationID: orgID, Key: sandboxImageAllowlistKey, Value: string(raw),
				}).Error; err != nil {
					return err
				}
			}
			if err := tx.Where("organization_id = ?", orgID).Delete(&models.SandboxImage{}).Error; err != nil {
				return err
			}
			if len(req.Canonical) > 0 {
				if err := tx.Create(&req.Canonical).Error; err != nil {
					return err
				}
			}
			return tx.Create(&models.AuditEvent{
				OrganizationID: orgID, EventType: "cp.sandbox.allowlist_updated", ActorType: "admin",
				Action: "update_image_allowlist", ResourceType: "organization", ResourceID: orgID,
				Details:    string(raw),
				Result:     "success",
				OccurredAt: time.Now().Format(time.RFC3339),
			}).Error
		})
		if txnErr != nil {
			writeError(w, http.StatusInternalServerError, txnErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"images": cleaned, "canonical": req.Canonical, "enforced": len(cleaned) > 0 || len(req.Canonical) > 0,
		})
	}
}

// validateSandboxImage enforces canonical-entry invariants (PAT-1514):
// digest entries must be sha256:hex; raw entries (tag/wildcard) must
// carry an explicit expanded_digests list — no silent tag/wildcard
// expansion at enforcement time.
func validateSandboxImage(e *models.SandboxImage) error {
	if e.Repository == "" {
		return errStr("repository required")
	}
	if e.IsRaw {
		if e.OriginalRef == "" {
			return errStr("is_raw entries require original_ref")
		}
		if len(e.ExpandedDigests) == 0 || !json.Valid([]byte(e.ExpandedDigests)) {
			return errStr("is_raw entries require expanded_digests (JSON array)")
		}
		var digests []string
		if err := json.Unmarshal([]byte(e.ExpandedDigests), &digests); err != nil || len(digests) == 0 {
			return errStr("is_raw entries require a non-empty expanded_digests array")
		}
		for _, d := range digests {
			if !sha256DigestPattern.MatchString(d) {
				return errStr("expanded_digests entries must be 64-char hex sha256")
			}
		}
		return nil
	}
	if e.DigestSHA256 == "" {
		return errStr("canonical entries require digest_sha256 (use is_raw + expanded_digests for tags/wildcards)")
	}
	if !sha256DigestPattern.MatchString(e.DigestSHA256) {
		return errStr("digest_sha256 must be 64-char hex sha256")
	}
	return nil
}

type stringError string

func (s stringError) Error() string { return string(s) }
func errStr(s string) error         { return stringError(s) }

func (s *Server) sandboxImageAllowlist(orgID string) []string {
	var setting models.OrgSetting
	if err := s.db.Where("organization_id = ? AND key = ?", orgID, sandboxImageAllowlistKey).First(&setting).Error; err != nil || setting.Value == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(setting.Value), &out)
	return out
}

func (s *Server) sandboxCanonicalImageEntries(orgID string) []models.SandboxImage {
	var entries []models.SandboxImage
	s.db.Where("organization_id = ? AND status = 'approved'", orgID).Order("created_at DESC").Find(&entries)
	return entries
}

func (s *Server) upsertOrgSetting(orgID, key, value string) {
	var existing models.OrgSetting
	err := s.db.Where("organization_id = ? AND key = ?", orgID, key).First(&existing).Error
	if err != nil {
		s.db.Create(&models.OrgSetting{
			Base:           models.Base{ID: models.GenerateID("os")},
			OrganizationID: orgID, Key: key, Value: value,
		})
		return
	}
	s.db.Model(&existing).Update("value", value)
}