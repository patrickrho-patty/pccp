// Package audit verifies the per-org audit-event hash chain (web/17 B).
// Every AuditEvent row carries a content digest and its predecessor's
// digest; VerifyChain recomputes the walk and reports the first break,
// if any — tamper-evidence for the compliance trail.
package audit

import (
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// ChainReport is the verification outcome for one organization.
type ChainReport struct {
	OrganizationID string `json:"organization_id"`
	Events         int64  `json:"events"`
	Verified       bool   `json:"verified"`
	FirstBreakID   string `json:"first_break_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// VerifyChain walks the org's audit events in insertion order,
// recomputes each digest, and checks the chain linkage. An empty chain
// verifies trivially.
func VerifyChain(db *gorm.DB, orgID string) (*ChainReport, error) {
	var events []models.AuditEvent
	if err := db.Where("organization_id = ?", orgID).Order("chain_seq ASC").Find(&events).Error; err != nil {
		return nil, fmt.Errorf("audit: load chain: %w", err)
	}
	report := &ChainReport{OrganizationID: orgID, Events: int64(len(events)), Verified: true}
	prev := ""
	for i := range events {
		e := &events[i]
		if want := e.ComputeAuditDigest(); want != e.EventDigest {
			report.Verified = false
			report.FirstBreakID = e.ID
			report.Reason = fmt.Sprintf("event %s digest mismatch (content changed after write)", e.ID)
			return report, nil
		}
		if i == 0 {
			if e.PrevEventDigest != "" {
				report.Verified = false
				report.FirstBreakID = e.ID
				report.Reason = "first event carries a predecessor digest"
				return report, nil
			}
		} else if e.PrevEventDigest != prev {
			report.Verified = false
			report.FirstBreakID = e.ID
			report.Reason = fmt.Sprintf("event %s does not link to its predecessor (inserted or removed row)", e.ID)
			return report, nil
		}
		prev = e.EventDigest
	}
	return report, nil
}
