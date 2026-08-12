package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/billing"
	"github.com/patrickrho-patty/pccp/internal/command"
	"github.com/patrickrho-patty/pccp/internal/incident"
	"github.com/patrickrho-patty/pccp/internal/korean"
	"github.com/patrickrho-patty/pccp/internal/mcp"
	"github.com/patrickrho-patty/pccp/internal/network"
	"github.com/patrickrho-patty/pccp/internal/privacy"
	"github.com/patrickrho-patty/pccp/internal/reporting"
	"github.com/patrickrho-patty/pccp/internal/secret"
	"github.com/patrickrho-patty/pccp/internal/telemetry"
	"github.com/patrickrho-patty/pccp/internal/tools"
	"gorm.io/gorm"
)

// ext holds additional services, initialized lazily.
var extMap = make(map[*Server]*AdditionalServices)

// AdditionalServices holds services not in the main Server struct.
// These are initialized in ExtendedInit and accessed via the Server's
// extension field.
type AdditionalServices struct {
	Billing   *billing.Service
	Command   *command.Service
	Incident  *incident.Service
	Korean    *korean.Service
	MCP       *mcp.Service
	Network   *network.Service
	Privacy   *privacy.Service
	Reporting *reporting.Service
	Secret    *secret.Service
	Telemetry *telemetry.Service
	Tools     *tools.Service
}

// ext gets the additional services for this server, initializing if needed.
func (s *Server) ext() *AdditionalServices {
	if e, ok := extMap[s]; ok {
		return e
	}
	e := s.initAdditional()
	extMap[s] = e
	return e
}

// initAdditional creates services that require additional wiring.
func (s *Server) initAdditional() *AdditionalServices {
	ext := &AdditionalServices{
		Billing:   mustBilling(s.db),
		Command:   command.New(),
		Incident:  incident.New(s.db),
		Korean:    korean.New(s.db),
		MCP:       mcp.New(s.db),
		Network:   network.New(s.db),
		Privacy:   privacy.New(s.db),
		Reporting: reporting.New(s.db),
		Secret:    secret.New(s.db),
		Telemetry: telemetry.New(s.db),
		Tools:     tools.New(s.db),
	}
	return ext
}

func mustBilling(db interface{}) *billing.Service {
	svc, err := billing.New(db.(*gorm.DB))
	if err != nil {
		panic(fmt.Sprintf("api: init billing: %v", err))
	}
	return svc
}

// setupAdditionalRoutes adds routes for the additional services.
func (s *Server) setupAdditionalRoutes(r chi.Router, ext *AdditionalServices) {
	// MCP Governance
	r.Route("/mcp", func(r chi.Router) {
		r.Get("/servers", s.wrapMCPList(ext))
		r.Post("/servers", s.wrapMCPRegister(ext))
		r.Post("/evaluate", s.wrapMCPEvaluate(ext))
		r.Post("/kill-switch", s.wrapMCPKill(ext))
		r.Get("/policy", s.wrapMCPGetPolicy(ext))
		r.Post("/policy", s.wrapMCPSetPolicy(ext))
	})

	// Network Broker
	r.Route("/network", func(r chi.Router) {
		r.Post("/evaluate", s.wrapNetworkEvaluate(ext))
		r.Post("/grants", s.wrapNetworkGrant(ext))
		r.Delete("/grants/{id}", s.wrapNetworkRevoke(ext))
	})

	// Secret Broker
	r.Route("/secrets", func(r chi.Router) {
		r.Post("/issue", s.wrapSecretIssue(ext))
		r.Post("/revoke", s.wrapSecretRevoke(ext))
	})

	// Command Authorization
	r.Route("/commands", func(r chi.Router) {
		r.Post("/evaluate", s.wrapCommandEvaluate(ext))
		r.Get("/policy", s.wrapCommandGetPolicy(ext))
		r.Post("/policy", s.wrapCommandSetPolicy(ext))
	})

	// Billing / Entitlements
	r.Route("/billing", func(r chi.Router) {
		r.Get("/entitlement", s.wrapBillingGetEntitlement(ext))
		r.Post("/entitlement", s.wrapBillingSetEntitlement(ext))
		r.Get("/quota-check", s.wrapBillingQuotaCheck(ext))
		r.Get("/chargeback", s.wrapBillingChargeback(ext))
	})

	// Incident Management
	r.Route("/incidents", func(r chi.Router) {
		r.Get("/", s.wrapIncidentList(ext))
		r.Post("/", s.wrapIncidentCreate(ext))
		r.Post("/contain", s.wrapIncidentContain(ext))
		r.Post("/{id}/resolve", s.wrapIncidentResolve(ext))
		r.Post("/simulate-policy", s.wrapIncidentSimulate(ext))
	})

	// Korean Enterprise Features
	r.Route("/korean", func(r chi.Router) {
		r.Get("/skills-matrix", s.wrapKoreanSkills(ext))
		r.Get("/governance-brief", s.wrapKoreanBrief(ext))
		r.Post("/change-freeze", s.wrapKoreanFreeze(ext))
		r.Post("/model-recall", s.wrapKoreanRecall(ext))
	})

	// Privacy & Access Control
	r.Route("/privacy", func(r chi.Router) {
		r.Post("/evaluate-access", s.wrapPrivacyEvaluate(ext))
		r.Get("/legal-hold", s.wrapPrivacyLegalHold(ext))
		r.Post("/legal-hold", s.wrapPrivacySetHold(ext))
		r.Delete("/legal-hold", s.wrapPrivacyReleaseHold(ext))
	})

	// Reporting
	r.Route("/reports", func(r chi.Router) {
		r.Post("/generate", s.wrapReportGenerate(ext))
	})

	// Telemetry
	r.Route("/telemetry", func(r chi.Router) {
		r.Get("/snapshot", s.wrapTelemetrySnapshot(ext))
	})

	// Tools Governance
	r.Route("/tools", func(r chi.Router) {
		r.Get("/", s.wrapToolsList(ext))
		r.Post("/", s.wrapToolsRegister(ext))
		r.Post("/seed-defaults", s.wrapToolsSeed(ext))
		r.Get("/approvals", s.wrapToolsPendingApprovals(ext))
		r.Post("/approvals/{id}/decide", s.wrapToolsDecideApproval(ext))
	})
}

// --- Handler wrappers ---

func writeJSON2(w http.ResponseWriter, status int, v interface{}) {
	writeJSON(w, status, v)
}

func decodeJSON2(r *http.Request, v interface{}) error {
	return decodeJSON(r, v)
}

// MCP handlers
func (s *Server) wrapMCPList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		servers, _ := ext.MCP.ListServers(orgID)
		writeJSON(w, http.StatusOK, servers)
	}
}

func (s *Server) wrapMCPRegister(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var server mcp.MCPServer
		if err := decodeJSON(r, &server); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.MCP.RegisterServer(server)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapMCPEvaluate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req mcp.MCPConnectionRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		decision, err := ext.MCP.EvaluateConnection(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, decision)
	}
}

func (s *Server) wrapMCPKill(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			ServerID       string `json:"server_id"`
			Reason         string `json:"reason"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ext.MCP.KillSwitch(req.OrganizationID, req.ServerID, req.Reason)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
	}
}

func (s *Server) wrapMCPGetPolicy(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		policy, _ := ext.MCP.GetPolicy(orgID)
		writeJSON(w, http.StatusOK, policy)
	}
}

func (s *Server) wrapMCPSetPolicy(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var policy mcp.MCPPolicy
		if err := decodeJSON(r, &policy); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ext.MCP.SetPolicy(policy.OrganizationID, policy)
		writeJSON(w, http.StatusOK, map[string]string{"status": "set"})
	}
}

// Network handlers
func (s *Server) wrapNetworkEvaluate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req network.ConnectionRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		decision, _ := ext.Network.EvaluateConnection(req)
		writeJSON(w, http.StatusOK, decision)
	}
}

func (s *Server) wrapNetworkGrant(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var grant network.NetworkGrant
		if err := decodeJSON(r, &grant); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Network.Grant(grant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapNetworkRevoke(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grantID := chi.URLParam(r, "id")
		orgID := getOrgID(r)
		ext.Network.RevokeGrant(orgID, grantID, "revoked via API")
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	}
}

// Secret handlers
func (s *Server) wrapSecretIssue(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req secret.IssueRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		cred, err := ext.Secret.Issue(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, cred)
	}
}

func (s *Server) wrapSecretRevoke(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			CredentialID   string `json:"credential_id"`
			Reason         string `json:"reason"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ext.Secret.Revoke(req.OrganizationID, req.CredentialID, req.Reason)
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	}
}

// Command handlers
func (s *Server) wrapCommandEvaluate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req command.CommandRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		decision := ext.Command.Evaluate(req)
		writeJSON(w, http.StatusOK, decision)
	}
}

func (s *Server) wrapCommandGetPolicy(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		policy := ext.Command.GetPolicy(orgID)
		writeJSON(w, http.StatusOK, policy)
	}
}

func (s *Server) wrapCommandSetPolicy(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var policy command.CommandPolicy
		if err := decodeJSON(r, &policy); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ext.Command.SetPolicy(policy.OrganizationID, policy)
		writeJSON(w, http.StatusOK, map[string]string{"status": "set"})
	}
}

// Billing handlers
func (s *Server) wrapBillingGetEntitlement(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		ent := ext.Billing.GetEntitlement(orgID)
		writeJSON(w, http.StatusOK, ent)
	}
}

func (s *Server) wrapBillingSetEntitlement(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ent billing.Entitlement
		if err := decodeJSON(r, &ent); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Billing.SetEntitlement(ent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapBillingQuotaCheck(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		check := ext.Billing.CheckRequestQuota(orgID, 0)
		writeJSON(w, http.StatusOK, check)
	}
}

func (s *Server) wrapBillingChargeback(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		period := time.Now().Format("2006-01")
		report, _ := ext.Billing.GenerateChargebackReport(orgID, period)
		writeJSON(w, http.StatusOK, report)
	}
}

// Incident handlers
func (s *Server) wrapIncidentList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		incidents, _ := ext.Incident.ListIncidents(orgID)
		writeJSON(w, http.StatusOK, incidents)
	}
}

func (s *Server) wrapIncidentCreate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var inc incident.Incident
		if err := decodeJSON(r, &inc); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Incident.CreateIncident(inc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapIncidentContain(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req incident.ContainRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Incident.Contain(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) wrapIncidentResolve(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		orgID := getOrgID(r)
		var req struct {
			Resolution string `json:"resolution"`
		}
		decodeJSON(r, &req)
		ext.Incident.Resolve(orgID, id, req.Resolution)
		writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
	}
}

func (s *Server) wrapIncidentSimulate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			RuleIDs []string `json:"rule_ids"`
		}
		decodeJSON(r, &req)
		result, _ := ext.Incident.SimulatePolicy(orgID, req.RuleIDs)
		writeJSON(w, http.StatusOK, result)
	}
}

// Korean handlers
func (s *Server) wrapKoreanSkills(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		matrix, _ := ext.Korean.GetAISkillsMatrix(orgID)
		writeJSON(w, http.StatusOK, matrix)
	}
}

func (s *Server) wrapKoreanBrief(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		brief, _ := ext.Korean.GenerateGovernanceBrief(orgID)
		writeJSON(w, http.StatusOK, brief)
	}
}

func (s *Server) wrapKoreanFreeze(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string   `json:"organization_id"`
			Reason         string   `json:"reason"`
			ReasonKo       string   `json:"reason_ko"`
			Repos          []string `json:"repos"`
			AllowedActions []string `json:"allowed_actions"`
			InitiatedBy    string   `json:"initiated_by"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		freeze, err := ext.Korean.InitiateChangeFreeze(req.OrganizationID, req.Reason, req.ReasonKo,
			req.Repos, req.AllowedActions, req.InitiatedBy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, freeze)
	}
}

func (s *Server) wrapKoreanRecall(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ModelPackageID string   `json:"model_package_id"`
			Reason         string   `json:"reason"`
			ReasonKo       string   `json:"reason_ko"`
			Severity       string   `json:"severity"`
			InitiatedBy    string   `json:"initiated_by"`
			AffectedOrgs   []string `json:"affected_orgs"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		recall, err := ext.Korean.EmergencyModelRecall(req.ModelPackageID, req.Reason, req.ReasonKo,
			req.Severity, req.InitiatedBy, req.AffectedOrgs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, recall)
	}
}

// Privacy handlers
func (s *Server) wrapPrivacyEvaluate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req privacy.AccessRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		decision := ext.Privacy.EvaluateAccess(req)
		writeJSON(w, http.StatusOK, decision)
	}
}

func (s *Server) wrapPrivacyLegalHold(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		hold, _ := ext.Privacy.CheckLegalHold(orgID, "")
		writeJSON(w, http.StatusOK, map[string]bool{"legal_hold": hold})
	}
}

func (s *Server) wrapPrivacySetHold(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			Reason   string `json:"reason"`
			PlacedBy string `json:"placed_by"`
		}
		decodeJSON(r, &req)
		ext.Privacy.SetLegalHold(orgID, req.Reason, req.PlacedBy)
		writeJSON(w, http.StatusOK, map[string]string{"status": "hold_activated"})
	}
}

func (s *Server) wrapPrivacyReleaseHold(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			Reason     string `json:"reason"`
			ReleasedBy string `json:"released_by"`
		}
		decodeJSON(r, &req)
		ext.Privacy.ReleaseLegalHold(orgID, req.Reason, req.ReleasedBy)
		writeJSON(w, http.StatusOK, map[string]string{"status": "hold_released"})
	}
}

// Reporting handlers
func (s *Server) wrapReportGenerate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			Type        string `json:"type"`
			Period      string `json:"period"`
			GeneratedBy string `json:"generated_by"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Period == "" {
			req.Period = time.Now().Format("2006-01")
		}
		report, err := ext.Reporting.GenerateReport(orgID, reporting.ReportType(req.Type), req.Period, req.GeneratedBy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// Telemetry handlers
func (s *Server) wrapTelemetrySnapshot(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := ext.Telemetry.Snapshot()
		writeJSON(w, http.StatusOK, snap)
	}
}

// Tools handlers
func (s *Server) wrapToolsList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		tools, _ := ext.Tools.ListTools(orgID)
		writeJSON(w, http.StatusOK, tools)
	}
}

func (s *Server) wrapToolsRegister(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID   string `json:"organization_id"`
			Name             string `json:"name"`
			NameKo           string `json:"name_ko"`
			Category         string `json:"category"`
			ToolClass        string `json:"tool_class"`
			DangerLevel      string `json:"danger_level"`
			RequiresApproval bool   `json:"requires_approval"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		orgID := req.OrganizationID
		if orgID == "" {
			orgID = getOrgID(r)
		}
		tool, err := ext.Tools.RegisterTool(orgID, req.Name, req.NameKo, req.Category,
			req.ToolClass, req.DangerLevel, req.RequiresApproval)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, tool)
	}
}

func (s *Server) wrapToolsSeed(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		ext.Tools.SeedDefaultTools(orgID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "seeded"})
	}
}

func (s *Server) wrapToolsPendingApprovals(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		approvals, _ := ext.Tools.ListPendingApprovals(orgID)
		writeJSON(w, http.StatusOK, approvals)
	}
}

func (s *Server) wrapToolsDecideApproval(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			ReviewerID string `json:"reviewer_id"`
			Decision   string `json:"decision"`
			Reason     string `json:"reason"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ext.Tools.DecideApproval(id, req.ReviewerID, req.Decision, req.Reason)
		writeJSON(w, http.StatusOK, map[string]string{"status": "decided"})
	}
}

// Ensure imports used
var _ = fmt.Sprintf
