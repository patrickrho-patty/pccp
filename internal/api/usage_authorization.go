package api

import (
	"net/http"
	"strings"
)

type UsageAction string

const (
	UsageActionSummaryRead UsageAction = "summary.read"
	UsageActionLedgerRead  UsageAction = "ledger.read"
	UsageActionExport      UsageAction = "export"
)

func (action UsageAction) Permission() string { return "usage." + string(action) }

func usageReadAction(r *http.Request) UsageAction {
	// A continuation cursor always returns raw ledger rows. Callers cannot
	// downgrade that request to summary authorization with a query flag.
	if strings.TrimSpace(r.URL.Query().Get("cursor")) != "" {
		return UsageActionLedgerRead
	}
	if r.URL.Query().Get("summary_only") == "1" {
		return UsageActionSummaryRead
	}
	return UsageActionLedgerRead
}

func hasUsagePermission(r *http.Request, action UsageAction, resourceType, resourceID string) bool {
	claims, ok := claimsFromCtx(r.Context())
	if !ok {
		return false
	}
	switch claims.Role {
	case "owner", "admin", "super_admin", "billing_admin", "auditor", "security_admin":
		return true
	}
	permission := action.Permission()
	resourceType, resourceID = strings.TrimSpace(resourceType), strings.TrimSpace(resourceID)
	for _, grant := range claims.Permissions {
		if grant == permission || grant == "usage.*" {
			return true
		}
		if resourceType != "" && resourceID != "" && (grant == permission+":"+resourceType+":"+resourceID || grant == "usage.*:"+resourceType+":"+resourceID) {
			return true
		}
	}
	return false
}

func requireUsagePermission(w http.ResponseWriter, r *http.Request, action UsageAction, resourceType, resourceID string) bool {
	if hasUsagePermission(r, action, resourceType, resourceID) {
		return true
	}
	writeError(w, http.StatusForbidden, "사용량 및 비용 조회 권한이 없습니다")
	return false
}
