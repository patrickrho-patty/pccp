package api

import (
	"encoding/json"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
)

const (
	modelPolicyConfigured   = "configured"
	modelPolicyUnrestricted = "unrestricted"
	modelPolicyInvalid      = "invalid"
	defaultAllowedModelID   = "patty-code-standard"
	allowedModelLookupBatch = 200
)

type allowedModelItemView struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	State          string `json:"state"`
	EntityKind     string `json:"entity_kind"`
	CatalogModelID string `json:"catalog_model_id,omitempty"`
	PackageID      string `json:"package_id,omitempty"`
}

type allowedModelResolver struct {
	byID                  map[string]models.CatalogModel
	classMatches          map[string][]models.CatalogModel
	classLabels           map[string]string
	classLabelOrgSpecific map[string]bool
	restrictedKinds       map[string]string
	packages              map[string]models.ModelPackage
}

type projectView struct {
	models.Project
	AllowedModelClasses     []string               `json:"allowed_model_classes"`
	AllowedModelPolicyState string                 `json:"allowed_model_policy_state"`
	AllowedModelItems       []allowedModelItemView `json:"allowed_model_items"`
}

type projectListView struct {
	projectView
	MemberCount        int64 `json:"member_count"`
	RepositoryCount    int64 `json:"repository_count"`
	SessionCount       int64 `json:"session_count"`
	ActiveSessionCount int64 `json:"active_session_count"`
}

// parseModelClasses decodes the stored policy without letting malformed data
// masquerade as an intentionally unrestricted project.
func parseModelClasses(raw string) ([]string, string) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, modelPolicyUnrestricted
	}
	var parsed []string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed == nil {
		return []string{}, modelPolicyInvalid
	}
	classes := make([]string, 0, len(parsed))
	seen := make(map[string]bool, len(parsed))
	for _, class := range parsed {
		class = strings.TrimSpace(class)
		if class == "" || seen[class] {
			continue
		}
		seen[class] = true
		classes = append(classes, class)
	}
	if len(classes) == 0 {
		if len(parsed) == 0 {
			return classes, modelPolicyUnrestricted
		}
		return classes, modelPolicyInvalid
	}
	return classes, modelPolicyConfigured
}

func (s *Server) newAllowedModelResolver(orgID string, identifiers []string) (*allowedModelResolver, error) {
	resolver := &allowedModelResolver{
		byID:                  make(map[string]models.CatalogModel),
		classMatches:          make(map[string][]models.CatalogModel),
		classLabels:           make(map[string]string),
		classLabelOrgSpecific: make(map[string]bool),
		restrictedKinds:       make(map[string]string),
		packages:              make(map[string]models.ModelPackage),
	}
	if len(identifiers) == 0 {
		return resolver, nil
	}
	uniqueIdentifiers := make([]string, 0, len(identifiers))
	seenIdentifier := make(map[string]bool, len(identifiers))
	for _, identifier := range identifiers {
		if identifier != "" && !seenIdentifier[identifier] {
			seenIdentifier[identifier] = true
			uniqueIdentifiers = append(uniqueIdentifiers, identifier)
		}
	}
	if len(uniqueIdentifiers) == 0 {
		return resolver, nil
	}

	var scoped []models.CatalogModel
	for start := 0; start < len(uniqueIdentifiers); start += allowedModelLookupBatch {
		end := start + allowedModelLookupBatch
		if end > len(uniqueIdentifiers) {
			end = len(uniqueIdentifiers)
		}
		batch := uniqueIdentifiers[start:end]
		var rows []models.CatalogModel
		query := s.db.Where("(organization_id = ? OR organization_id = '')", orgID).
			Where("catalog_model_id IN ? OR family IN ? OR entitlement_class IN ?", batch, batch, batch)
		if err := query.Find(&rows).Error; err != nil {
			return nil, err
		}
		scoped = append(scoped, rows...)
	}
	packageIDs := make([]string, 0, len(scoped))
	seenPackage := make(map[string]bool, len(scoped))
	seenClassMatch := make(map[string]map[string]bool)
	for _, model := range scoped {
		resolver.byID[model.CatalogModelID] = model
		for _, class := range []string{model.Family, model.EntitlementClass} {
			if class != "" {
				if seenClassMatch[class] == nil {
					seenClassMatch[class] = make(map[string]bool)
				}
				if seenClassMatch[class][model.CatalogModelID] {
					continue
				}
				seenClassMatch[class][model.CatalogModelID] = true
				resolver.classMatches[class] = append(resolver.classMatches[class], model)
			}
		}
		if model.EntitlementClass != "" {
			label := model.EntitlementLabelKo
			if label == "" {
				label = model.EntitlementLabel
			}
			orgSpecific := model.OrganizationID == orgID
			current := resolver.classLabels[model.EntitlementClass]
			currentOrgSpecific := resolver.classLabelOrgSpecific[model.EntitlementClass]
			if label != "" && (current == "" || (orgSpecific && !currentOrgSpecific) || (orgSpecific == currentOrgSpecific && label < current)) {
				resolver.classLabels[model.EntitlementClass] = label
				resolver.classLabelOrgSpecific[model.EntitlementClass] = orgSpecific
			}
		}
		if model.ProductionPackageID != "" && !seenPackage[model.ProductionPackageID] {
			seenPackage[model.ProductionPackageID] = true
			packageIDs = append(packageIDs, model.ProductionPackageID)
		}
	}

	var allCandidates []models.CatalogModel
	for start := 0; start < len(uniqueIdentifiers); start += allowedModelLookupBatch {
		end := start + allowedModelLookupBatch
		if end > len(uniqueIdentifiers) {
			end = len(uniqueIdentifiers)
		}
		batch := uniqueIdentifiers[start:end]
		var rows []models.CatalogModel
		if err := s.db.Where("catalog_model_id IN ? OR family IN ? OR entitlement_class IN ?", batch, batch, batch).Find(&rows).Error; err != nil {
			return nil, err
		}
		allCandidates = append(allCandidates, rows...)
	}
	for _, model := range allCandidates {
		if seenIdentifier[model.CatalogModelID] {
			if _, allowed := resolver.byID[model.CatalogModelID]; !allowed {
				resolver.restrictedKinds[model.CatalogModelID] = "model"
			}
		}
		for _, class := range []string{model.Family, model.EntitlementClass} {
			if class != "" && seenIdentifier[class] && len(resolver.classMatches[class]) == 0 && resolver.restrictedKinds[class] == "" {
				resolver.restrictedKinds[class] = "class"
			}
		}
	}

	if len(packageIDs) > 0 {
		for start := 0; start < len(packageIDs); start += allowedModelLookupBatch {
			end := start + allowedModelLookupBatch
			if end > len(packageIDs) {
				end = len(packageIDs)
			}
			var packages []models.ModelPackage
			if err := s.db.Where("package_id IN ?", packageIDs[start:end]).Find(&packages).Error; err != nil {
				return nil, err
			}
			for _, pkg := range packages {
				resolver.packages[pkg.PackageID] = pkg
			}
		}
	}
	return resolver, nil
}

func (resolver *allowedModelResolver) resolve(identifier string) allowedModelItemView {
	if model, ok := resolver.byID[identifier]; ok {
		return resolver.catalogItem(identifier, model)
	}

	matches := resolver.classMatches[identifier]
	if len(matches) == 1 {
		return resolver.catalogItem(identifier, matches[0])
	}
	if len(matches) > 1 {
		label := resolver.classLabels[identifier]
		if label == "" {
			label = identifier
		}
		hasUsable := false
		hasUnavailable := false
		for _, model := range matches {
			switch resolver.catalogItem(identifier, model).State {
			case "single":
				hasUsable = true
			case "unavailable":
				hasUnavailable = true
			}
		}
		if hasUsable {
			return allowedModelItemView{ID: identifier, Label: label, State: "many", EntityKind: "class"}
		}
		if hasUnavailable {
			return allowedModelItemView{ID: identifier, Label: label, State: "unavailable", EntityKind: "class"}
		}
		return allowedModelItemView{ID: identifier, Label: label, State: "retired", EntityKind: "class"}
	}
	if kind := resolver.restrictedKinds[identifier]; kind != "" {
		return allowedModelItemView{ID: identifier, Label: identifier, State: "restricted", EntityKind: kind}
	}
	return allowedModelItemView{ID: identifier, Label: identifier, State: "unknown", EntityKind: "class"}
}

func (resolver *allowedModelResolver) catalogItem(identifier string, model models.CatalogModel) allowedModelItemView {
	label := model.DisplayNameKo
	if label == "" {
		label = model.DisplayName
	}
	if label == "" {
		label = identifier
	}
	item := allowedModelItemView{
		ID:             identifier,
		Label:          label,
		State:          "single",
		EntityKind:     "model",
		CatalogModelID: model.CatalogModelID,
	}
	pkg, found := resolver.packages[model.ProductionPackageID]
	if found && model.ProductionPackageID != "" {
		item.PackageID = model.ProductionPackageID
	}
	if model.Status != "active" || model.Availability == "withdrawn" {
		item.State = "retired"
		return item
	}
	if model.Availability != "available" && model.Availability != "degraded" {
		item.State = "unavailable"
		return item
	}
	if !found || model.ProductionPackageID == "" {
		item.State = "unavailable"
	} else if pkg.State == "deprecated" || pkg.State == "recalled" {
		item.State = "retired"
	} else if pkg.State != "published" {
		item.State = "unavailable"
	}
	return item
}

func (resolver *allowedModelResolver) resolvesRestricted(identifiers []string) bool {
	for _, identifier := range identifiers {
		if resolver.resolve(identifier).State == "restricted" {
			return true
		}
	}
	return false
}

func projectViewRow(proj models.Project, resolver *allowedModelResolver) projectView {
	classes, policyState := parseModelClasses(proj.AllowedModelClasses)
	items := make([]allowedModelItemView, 0, len(classes))
	for _, class := range classes {
		items = append(items, resolver.resolve(class))
	}
	return projectView{
		Project:                 proj,
		AllowedModelClasses:     classes,
		AllowedModelPolicyState: policyState,
		AllowedModelItems:       items,
	}
}

func (s *Server) projectViewRow(proj models.Project) (projectView, error) {
	classes, _ := parseModelClasses(proj.AllowedModelClasses)
	resolver, err := s.newAllowedModelResolver(proj.OrganizationID, classes)
	if err != nil {
		return projectView{}, err
	}
	return projectViewRow(proj, resolver), nil
}
