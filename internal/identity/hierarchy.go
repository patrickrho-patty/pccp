package identity

import (
	"errors"
	"fmt"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// hierarchy.go implements delegated administration over the Korean
// enterprise org hierarchy (PRD §12, master plan Task 16): an
// administrator at an ancestor organization may manage descendant
// organizations; a descendant may NEVER administer an ancestor or a
// sibling. Cycle-protected.

// ErrNotAuthorized marks an out-of-authority administration attempt.
var ErrNotAuthorized = errors.New("identity: not authorized for this organization")

// ErrHierarchyCycle marks a cyclic org tree.
var ErrHierarchyCycle = errors.New("identity: organization hierarchy contains a cycle")

// MaxHierarchyDepth bounds the ancestor walk.
const MaxHierarchyDepth = 32

// IsAncestor reports whether ancestorOrg administrates targetOrg: the
// target is the ancestor itself or any descendant of it.
func (s *Service) IsAncestor(ancestorOrgID, targetOrgID string) (bool, error) {
	if ancestorOrgID == targetOrgID {
		return true, nil
	}
	// Walk up from the target collecting the path. When the ancestor is
	// reached, CONFIRM the ancestor itself terminates (no parent loop
	// back into the walked path) — a cyclic hierarchy fails closed.
	cur := targetOrgID
	seen := map[string]bool{}
	for depth := 0; depth < MaxHierarchyDepth; depth++ {
		if seen[cur] {
			return false, ErrHierarchyCycle
		}
		seen[cur] = true
		var org models.Organization
		if err := s.db.Select("parent_org_id").Where("id = ?", cur).First(&org).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		if org.ParentOrgID == "" {
			return false, nil
		}
		if org.ParentOrgID == ancestorOrgID {
			// Confirm the ancestor terminates cleanly.
			return s.terminates(ancestorOrgID, seen)
		}
		cur = org.ParentOrgID
	}
	return false, ErrHierarchyCycle
}

// terminates walks up from orgID and reports whether the chain ends at
// a parentless root without revisiting any node on the walked path.
func (s *Service) terminates(orgID string, walked map[string]bool) (bool, error) {
	cur := orgID
	for depth := 0; depth < MaxHierarchyDepth; depth++ {
		if walked[cur] {
			return false, ErrHierarchyCycle
		}
		var org models.Organization
		if err := s.db.Select("parent_org_id").Where("id = ?", cur).First(&org).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return true, nil
			}
			return false, err
		}
		if org.ParentOrgID == "" {
			return true, nil
		}
		if _, seen := walked[org.ParentOrgID]; seen {
			return false, ErrHierarchyCycle
		}
		walked[cur] = true
		cur = org.ParentOrgID
	}
	return false, ErrHierarchyCycle
}

// AuthorizeAdmin enforces delegated administration: an admin of
// adminOrgID may act on targetOrgID only when adminOrgID is the target
// or an ancestor of it.
func (s *Service) AuthorizeAdmin(adminOrgID, targetOrgID, action string) error {
	ok, err := s.IsAncestor(adminOrgID, targetOrgID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: admin org %s cannot %s on org %s", ErrNotAuthorized, adminOrgID, action, targetOrgID)
	}
	return nil
}

// Descendants lists every organization under rootOrgID (inclusive).
func (s *Service) Descendants(rootOrgID string) ([]string, error) {
	var out []string
	var walk func(parent string, depth int) error
	seen := map[string]bool{}
	walk = func(parent string, depth int) error {
		if depth > MaxHierarchyDepth {
			return ErrHierarchyCycle
		}
		var children []models.Organization
		if err := s.db.Select("id").Where("parent_org_id = ?", parent).Find(&children).Error; err != nil {
			return err
		}
		for _, c := range children {
			if seen[c.ID] {
				return ErrHierarchyCycle
			}
			seen[c.ID] = true
			out = append(out, c.ID)
			if err := walk(c.ID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootOrgID, 0); err != nil {
		return nil, err
	}
	return append([]string{rootOrgID}, out...), nil
}
