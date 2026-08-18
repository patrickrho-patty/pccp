package api

import (
	"encoding/json"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/models"
)

// parseModelClasses decodes the stored allowed-model JSON into a typed,
// trimmed, duplicate-free string slice (PAT-1491). A malformed or empty
// payload yields an empty slice — serialized database values never reach
// presentation code or a URL path segment.
func parseModelClasses(raw string) []string {
	parsed := parseAllowedUsers(raw) // shared JSON-string-array decoder
	classes := make([]string, 0, len(parsed))
	seen := make(map[string]bool, len(parsed))
	for _, c := range parsed {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		classes = append(classes, c)
	}
	return classes
}

// projectViewRow renders a project as a JSON-ready map with
// allowed_model_classes parsed into a real array (PAT-1491). All project
// read and mutation surfaces — list, get, detail, create, update — go
// through this so no consumer can receive the raw serialized string.
func projectViewRow(proj models.Project) map[string]interface{} {
	row := map[string]interface{}{}
	b, _ := json.Marshal(proj)
	json.Unmarshal(b, &row)
	row["allowed_model_classes"] = parseModelClasses(proj.AllowedModelClasses)
	return row
}
