package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/models"
)

func TestSandboxImageAllowlistCanonicalValidation(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		ok        bool
		errSubstr string
	}{
		{"good digest", `{"canonical":[{"repository":"patty/sandbox-base","digest_sha256":"` + strings.Repeat("a", 64) + `","signer":"patty-release","signature_ref":"sigstore://...","scan_status":"clean"}]}`, true, ""},
		{"bad digest length", `{"canonical":[{"repository":"r","digest_sha256":"deadbeef"}]}`, false, "64-char hex sha256"},
		{"bad digest non-hex", `{"canonical":[{"repository":"r","digest_sha256":"` + strings.Repeat("z", 64) + `"}]}`, false, "64-char hex sha256"},
		{"raw without expansion", `{"canonical":[{"repository":"r","is_raw":true,"original_ref":"r:*"}]}`, false, "is_raw entries require"},
		{"raw with expansion", `{"canonical":[{"repository":"r","is_raw":true,"original_ref":"r:*","expanded_digests":"[\"` + strings.Repeat("b", 64) + `\"]"}]}`, true, ""},
		{"raw expansion not hex", `{"canonical":[{"repository":"r","is_raw":true,"original_ref":"r:*","expanded_digests":"[\"not-hex\"]"}]}`, false, "must be 64-char hex sha256"},
		{"missing repository", `{"canonical":[{"digest_sha256":"` + strings.Repeat("c", 64) + `"}]}`, false, "repository required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req struct {
				Canonical []models.SandboxImage `json:"canonical"`
			}
			if err := json.Unmarshal([]byte(c.body), &req); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(req.Canonical) != 1 {
				t.Fatalf("expected 1 canonical entry, got %d", len(req.Canonical))
			}
			err := validateSandboxImage(&req.Canonical[0])
			if c.ok {
				if err != nil {
					t.Fatalf("expected valid, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if c.errSubstr != "" && !strings.Contains(err.Error(), c.errSubstr) {
					t.Fatalf("error %q should contain %q", err.Error(), c.errSubstr)
				}
			}
		})
	}
}

func TestSandboxImageAllowlistExpansionEnforced(t *testing.T) {
	// Confirms the runtime decision: a tag (non-digest) form passes only
	// when a raw entry with a matching tag in its expanded digests exists.
	srv, db := commsTestServer(t)
	org := models.Organization{Name: "o", Slug: "oimgexp", Status: "active"}
	db.Create(&org)
	goodDigest := strings.Repeat("d", 64)
	rec := doJSON(t, srv, "PUT", "/api/sandboxes/image-allowlist",
		`{"images":[],"canonical":[{"repository":"patty/sandbox-base","is_raw":true,"original_ref":"patty/sandbox-base:2026.08","expanded_digests":"[\"`+goodDigest+`\"]","scan_status":"clean","classification":"internal"}]}`, org.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("canonical PUT failed: %d %s", rec.Code, rec.Body.String())
	}
	// Debug: confirm the canonical row landed.
	var dbg []models.SandboxImage
	db.Where("organization_id = ?", org.ID).Find(&dbg)
	if len(dbg) != 1 {
		t.Fatalf("expected 1 canonical row, got %d (DB: %+v)", len(dbg), dbg)
	}
	// image matching the raw expansion's digest
	img := "patty/sandbox-base@" + goodDigest
	if !imageAllowlistedForOrg(org.ID, img, srv) {
		t.Fatalf("expected canonical raw expansion to permit the listed digest")
	}
	// image with the SAME repo+tag but a different digest must fail
	other := "patty/sandbox-base@" + strings.Repeat("e", 64)
	if imageAllowlistedForOrg(org.ID, other, srv) {
		t.Fatalf("canonical raw expansion must NOT permit unlisted digests")
	}
}

// imageAllowlistedForOrg is a thin wrapper that fetches the org's
// legacy text list + canonical table and returns whether the image
// passes either gate.
func imageAllowlistedForOrg(orgID, image string, srv *Server) bool {
	var setting models.OrgSetting
	srv.db.Where("organization_id = ? AND key = ?", orgID, "sandbox.image_allowlist").First(&setting)
	var legacy []string
	if setting.Value != "" {
		_ = json.Unmarshal([]byte(setting.Value), &legacy)
	}
	// Legacy list match: same repo + (empty tag / "*" / exact tag).
	imgRepo := image
	if at := strings.Index(image, "@"); at >= 0 {
		imgRepo = image[:at]
	} else if colon := strings.LastIndex(image, ":"); colon >= 0 {
		imgRepo = image[:colon]
	}
	imgTag := ""
	if at := strings.Index(image, "@"); at >= 0 {
		imgTag = ""
	} else if colon := strings.LastIndex(image, ":"); colon >= 0 {
		imgTag = image[colon+1:]
	}
	for _, entry := range legacy {
		eRepo := entry
		eTag := ""
		if at := strings.Index(entry, "@"); at >= 0 {
			eRepo = entry[:at]
		} else if colon := strings.LastIndex(entry, ":"); colon >= 0 {
			eTag = entry[colon+1:]
		}
		if eRepo != imgRepo {
			continue
		}
		if eTag == "" || eTag == "*" || eTag == imgTag {
			return true
		}
	}
	return canonicalImageAllows(srv, orgID, image)
}

func canonicalImageAllows(srv *Server, orgID, image string) bool {
	var entries []models.SandboxImage
	srv.db.Where("organization_id = ? AND status = 'approved'", orgID).Find(&entries)
	if len(entries) == 0 {
		return false
	}
	// Inline repo/tag parsing to avoid importing the sandbox package's
	// unexported helpers.
	imgRepo := image
	if at := strings.Index(image, "@"); at >= 0 {
		imgRepo = image[:at]
	} else if colon := strings.LastIndex(image, ":"); colon >= 0 {
		imgRepo = image[:colon]
	}
	imgDigest := ""
	if at := strings.Index(image, "@"); at >= 0 {
		imgDigest = strings.TrimPrefix(image[at+1:], "sha256:")
	}
	for _, e := range entries {
		if e.Repository != imgRepo {
			continue
		}
		if e.IsRaw {
			var digests []string
			_ = json.Unmarshal([]byte(e.ExpandedDigests), &digests)
			for _, d := range digests {
				if d == imgDigest {
					return true
				}
			}
			continue
		}
		if imgDigest != "" && e.DigestSHA256 == imgDigest {
			return true
		}
	}
	return false
}
