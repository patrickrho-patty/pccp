package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/patrickrho-patty/pccp/internal/attestation"
	"github.com/patrickrho-patty/pccp/internal/catalog"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/publiccloud"
	"github.com/patrickrho-patty/pccp/internal/billing"
	"github.com/patrickrho-patty/pccp/internal/command"
	"github.com/patrickrho-patty/pccp/internal/compliance"
	"github.com/patrickrho-patty/pccp/internal/configmgmt"
	"github.com/patrickrho-patty/pccp/internal/connectors"
	"github.com/patrickrho-patty/pccp/internal/gpuops"
	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"github.com/patrickrho-patty/pccp/internal/mcpmarket"
	"github.com/patrickrho-patty/pccp/internal/realtime"
	"github.com/patrickrho-patty/pccp/internal/sovereign"
	"github.com/patrickrho-patty/pccp/internal/sso"
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
 	Billing   *billing.Service `json:"billing"`
 	Command   *command.Service `json:"command"`
 	Incident  *incident.Service `json:"incident"`
 	Korean    *korean.Service `json:"korean"`
 	MCP       *mcp.Service `json:"m_c_p"`
 	Network   *network.Service `json:"network"`
 	Privacy   *privacy.Service `json:"privacy"`
 	Reporting *reporting.Service `json:"reporting"`
 	Secret    *secret.Service `json:"secret"`
 	Telemetry *telemetry.Service `json:"telemetry"`
 	Tools      *tools.Service `json:"tools"`
 	Attestation *attestation.Service `json:"attestation"`
 	Compliance  *compliance.Service `json:"compliance"`
 	ConfigMgmt  *configmgmt.Service `json:"config_mgmt"`
 	Connectors  *connectors.Service `json:"connectors"`
 	GPUOps      *gpuops.Service `json:"g_p_u_ops"`
 	KeyMgmt     *keymgmt.Service `json:"key_mgmt"`
 	MCPMarket   *mcpmarket.Service `json:"m_c_p_market"`
 	Realtime    *realtime.Service `json:"realtime"`
 	Sovereign   *sovereign.Service `json:"sovereign"`
 	SSO         *sso.Service `json:"s_s_o"`
	Catalog      *catalog.Service `json:"catalog"`
	PublicCloud  *publiccloud.Service `json:"public_cloud"`
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
		Attestation: attestation.New(),
		Billing:     mustBilling(s.db),
		Command:   command.New(),
		Incident:  incident.New(s.db),
		Korean:    korean.New(s.db),
		MCP:       mcp.New(s.db),
		Network:   network.New(s.db),
		Privacy:   privacy.New(s.db),
		Reporting: reporting.New(s.db),
		Secret:    secret.New(s.db),
		Telemetry: telemetry.New(s.db),
		Tools:      tools.New(s.db),
		Compliance:  compliance.New(s.db),
		ConfigMgmt:  configmgmt.New(s.db),
		Connectors:  connectors.New(),
		GPUOps:      gpuops.New(s.db),
		KeyMgmt:     keymgmt.New(),
		MCPMarket:   mcpmarket.New(),
		Realtime:    realtime.New(),
		Sovereign:   sovereign.New(),
		SSO:         sso.New(s.db, "pccp-sso-secret"),
		Catalog:     mustCatalog(s.db),
		PublicCloud: mustPublicCloud(s.db),
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

	// Attestation
	r.Route("/attestation", func(r chi.Router) {
		r.Post("/collect", s.wrapAttestCollect(ext))
		r.Post("/verify", s.wrapAttestVerify(ext))
		r.Get("/levels/{level}", s.wrapAttestLevels(ext))
		r.Post("/key-release", s.wrapAttestKeyRelease(ext))
	})

	// Compliance
	r.Route("/compliance", func(r chi.Router) {
		r.Get("/certifications", s.wrapComplianceList(ext))
		r.Get("/packs/{cert}", s.wrapCompliancePack(ext))
		r.Post("/assess", s.wrapComplianceAssess(ext))
	})

	// Config Management
	r.Route("/config-changes", func(r chi.Router) {
		r.Post("/", s.wrapConfigCreate(ext))
		r.Get("/", s.wrapConfigList(ext))
		r.Post("/{id}/validate", s.wrapConfigValidate(ext))
		r.Post("/{id}/approve", s.wrapConfigApprove(ext))
		r.Post("/{id}/publish", s.wrapConfigPublish(ext))
		r.Post("/{id}/rollback", s.wrapConfigRollback(ext))
		r.Get("/drift", s.wrapConfigDrift(ext))
	})

	// Connectors
	r.Route("/connectors", func(r chi.Router) {
		r.Get("/", s.wrapConnectorsList(ext))
		r.Post("/", s.wrapConnectorsRegister(ext))
		r.Delete("/{id}", s.wrapConnectorsDisable(ext))
		r.Get("/types", s.wrapConnectorsTypes(ext))
	})

	// GPU Operations
	r.Route("/gpu", func(r chi.Router) {
		r.Get("/endpoints", s.wrapGPUEndpoints(ext))
		r.Get("/gpus", s.wrapGPUs(ext))
		r.Get("/models", s.wrapGPUModels(ext))
		r.Post("/route", s.wrapGPURoute(ext))
	})

	// Key Management
	r.Route("/keys", func(r chi.Router) {
		r.Post("/generate", s.wrapKeyGenerate(ext))
		r.Get("/{domain}", s.wrapKeyList(ext))
		r.Post("/{id}/rotate", s.wrapKeyRotate(ext))
		r.Post("/{id}/revoke", s.wrapKeyRevoke(ext))
	})

	// MCP Marketplace
	r.Route("/mcp-market", func(r chi.Router) {
		r.Get("/", s.wrapMarketSearch(ext))
		r.Post("/", s.wrapMarketPublish(ext))
		r.Get("/{id}", s.wrapMarketGet(ext))
		r.Get("/categories", s.wrapMarketCategories(ext))
		r.Post("/seed", s.wrapMarketSeed(ext))
	})

	// SSO
	r.Route("/sso", func(r chi.Router) {
		r.Post("/saml/redirect", s.wrapSSOSAMLRedirect(ext))
		r.Post("/saml/callback", s.wrapSSOSAMLCallback(ext))
		r.Get("/oidc/auth-url", s.wrapSSOOIDCAuthURL(ext))
		r.Post("/oidc/callback", s.wrapSSOOIDCCallback(ext))
		r.Post("/scim", s.wrapSSOSCIM(ext))
	})

	// Sovereign
	r.Route("/sovereign", func(r chi.Router) {
		r.Post("/trust-bundle", s.wrapSovImportBundle(ext))
		r.Post("/updates", s.wrapSovImportUpdate(ext))
		r.Post("/updates/{id}/apply", s.wrapSovApplyUpdate(ext))
		r.Get("/updates/pending", s.wrapSovPendingUpdates(ext))
		r.Post("/time-proof", s.wrapSovTimeProof(ext))
	})

	// Realtime
	r.Get("/realtime/status", s.wrapRealtimeStatus(ext))

	// v2 Model Catalog (§10A)
	r.Route("/catalog", func(r chi.Router) {
		r.Get("/models", s.wrapCatalogModels(ext))
		r.Post("/models", s.wrapCatalogRegister(ext))
		r.Get("/epoch", s.wrapCatalogEpoch(ext))
		r.Post("/seed", s.wrapCatalogSeed(ext))
		r.Post("/{id}/withdraw", s.wrapCatalogWithdraw(ext))
		r.Post("/{id}/announce", s.wrapCatalogAnnounce(ext))
	})

	// v2 Public Cloud (§10C)
	r.Route("/public", func(r chi.Router) {
		r.Post("/accounts", s.wrapPublicCreateAccount(ext))
		r.Get("/accounts", s.wrapPublicAccounts(ext))
		r.Get("/accounts/{id}", s.wrapPublicGetAccount(ext))
		r.Post("/accounts/{id}/subscription", s.wrapPublicCreateSub(ext))
		r.Get("/accounts/{id}/lease", s.wrapPublicLease(ext))
		r.Get("/accounts/{id}/slots", s.wrapPublicSlots(ext))
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

// --- Attestation Handlers ---

func (s *Server) wrapAttestCollect(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req attestation.CollectRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		evidence, err := ext.Attestation.CollectEvidence(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, evidence)
	}
}

func (s *Server) wrapAttestVerify(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Evidence        attestation.AttestationEvidence `json:"evidence"`
			ReferenceValues map[string]string               `json:"reference_values"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := ext.Attestation.VerifyEvidence(&req.Evidence, req.ReferenceValues); err != nil {
			writeJSON(w, http.StatusOK, req.Evidence)
			return
		}
		writeJSON(w, http.StatusOK, req.Evidence)
	}
}

func (s *Server) wrapAttestLevels(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		level := attestation.AssuranceLevel(chi.URLParam(r, "level"))
		reqs := attestation.AssuranceLevelRequirements(level)
		writeJSON(w, http.StatusOK, map[string]interface{}{"level": level, "requirements": reqs})
	}
}

func (s *Server) wrapAttestKeyRelease(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req attestation.ModelKeyReleaseRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result := ext.Attestation.EvaluateKeyRelease(req)
		writeJSON(w, http.StatusOK, result)
	}
}

// --- Compliance Handlers ---

func (s *Server) wrapComplianceList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ext.Compliance.ListCertifications())
	}
}

func (s *Server) wrapCompliancePack(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cert := compliance.CertificationType(chi.URLParam(r, "cert"))
		pack, err := ext.Compliance.GetCertificationPack(cert)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, pack)
	}
}

func (s *Server) wrapComplianceAssess(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			Certification string `json:"certification"`
		}
		decodeJSON(r, &req)
		assessment, err := ext.Compliance.AssessCompliance(orgID, compliance.CertificationType(req.Certification))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, assessment)
	}
}

// --- Config Management Handlers ---

func (s *Server) wrapConfigCreate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var change configmgmt.ConfigChange
		if err := decodeJSON(r, &change); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.ConfigMgmt.CreateChange(change)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapConfigList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		writeJSON(w, http.StatusOK, ext.ConfigMgmt.GetPendingChanges(orgID))
	}
}

func (s *Server) wrapConfigValidate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := ext.ConfigMgmt.ValidateChange(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "validated"})
	}
}

func (s *Server) wrapConfigApprove(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct{ ApprovedBy string `json:"approved_by"` }
		decodeJSON(r, &req)
		if err := ext.ConfigMgmt.ApproveChange(id, req.ApprovedBy); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	}
}

func (s *Server) wrapConfigPublish(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := ext.ConfigMgmt.PublishChange(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
	}
}

func (s *Server) wrapConfigRollback(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct{ Reason string `json:"reason"` }
		decodeJSON(r, &req)
		ext.ConfigMgmt.RollbackChange(id, req.Reason)
		writeJSON(w, http.StatusOK, map[string]string{"status": "rolled_back"})
	}
}

func (s *Server) wrapConfigDrift(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		drift, _ := ext.ConfigMgmt.DetectDrift(orgID)
		writeJSON(w, http.StatusOK, drift)
	}
}

// --- Connectors Handlers ---

func (s *Server) wrapConnectorsList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		writeJSON(w, http.StatusOK, ext.Connectors.List(orgID))
	}
}

func (s *Server) wrapConnectorsRegister(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var conn connectors.Connector
		if err := decodeJSON(r, &conn); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Connectors.Register(conn)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapConnectorsDisable(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ext.Connectors.Disable(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
	}
}

func (s *Server) wrapConnectorsTypes(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, connectors.SupportedConnectorTypes())
	}
}

// --- GPU Operations Handlers ---

func (s *Server) wrapGPUEndpoints(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ext.GPUOps.GetAllEndpointMetrics())
	}
}

func (s *Server) wrapGPUs(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ext.GPUOps.GetAllGPUMetrics())
	}
}

func (s *Server) wrapGPUModels(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		report, _ := ext.GPUOps.GetModelOperationsReport(orgID)
		writeJSON(w, http.StatusOK, report)
	}
}

func (s *Server) wrapGPURoute(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		var req struct {
			ModelPackageID string `json:"model_package_id"`
			DataResidency  string `json:"data_residency"`
		}
		decodeJSON(r, &req)
		decision, err := ext.GPUOps.RouteRequest(orgID, req.ModelPackageID, req.DataResidency)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, decision)
	}
}

// --- Key Management Handlers ---

func (s *Server) wrapKeyGenerate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Domain    string `json:"domain"`
			ValidityH int    `json:"validity_hours"`
		}
		decodeJSON(r, &req)
		validity := time.Duration(req.ValidityH) * time.Hour
		if validity == 0 {
			validity = 90 * 24 * time.Hour
		}
		entry, err := ext.KeyMgmt.GenerateKey(keymgmt.KeyDomain(req.Domain), validity)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"id": entry.ID, "domain": entry.Domain, "algorithm": entry.Algorithm,
			"status": entry.Status,
		})
	}
}

func (s *Server) wrapKeyList(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain := keymgmt.KeyDomain(chi.URLParam(r, "domain"))
		writeJSON(w, http.StatusOK, ext.KeyMgmt.ListKeys(domain))
	}
}

func (s *Server) wrapKeyRotate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		// Extract domain from the key entry
		entry, err := ext.KeyMgmt.GetKey(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		newEntry, err := ext.KeyMgmt.RotateKey(entry.Domain, 90*24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"new_key_id": newEntry.ID})
	}
}

func (s *Server) wrapKeyRevoke(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ext.KeyMgmt.RevokeKey(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	}
}

// --- MCP Marketplace Handlers ---

func (s *Server) wrapMarketSearch(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		cat := r.URL.Query().Get("category")
		writeJSON(w, http.StatusOK, ext.MCPMarket.SearchListings(q, cat))
	}
}

func (s *Server) wrapMarketPublish(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var listing mcpmarket.MCPListing
		if err := decodeJSON(r, &listing); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.MCPMarket.PublishListing(listing)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapMarketGet(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		listing, err := ext.MCPMarket.GetListing(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, listing)
	}
}

func (s *Server) wrapMarketCategories(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, mcpmarket.Categories())
	}
}

func (s *Server) wrapMarketSeed(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ext.MCPMarket.SeedDefaultListings()
		writeJSON(w, http.StatusOK, map[string]string{"status": "seeded"})
	}
}

// --- SSO Handlers ---

func (s *Server) wrapSSOSAMLRedirect(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IdpEntityID string `json:"idp_entity_id"`
			IdpSSOURL   string `json:"idp_sso_url"`
			RelayState  string `json:"relay_state"`
		}
		decodeJSON(r, &req)
		ext.SSO.ConfigureSAML(req.IdpEntityID, req.IdpSSOURL, "")
		url, err := ext.SSO.GenerateSAMLRedirect(req.RelayState)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"redirect_url": url})
	}
}

func (s *Server) wrapSSOSAMLCallback(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SAMLResponse string `json:"saml_response"`
			RelayState   string `json:"relay_state"`
		}
		decodeJSON(r, &req)
		resp, err := ext.SSO.HandleSAMLCallback(req.SAMLResponse, req.RelayState)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func (s *Server) wrapSSOOIDCAuthURL(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")
		issuer := r.URL.Query().Get("issuer")
		clientID := r.URL.Query().Get("client_id")
		ext.SSO.ConfigureOIDC(issuer, clientID, "")
		url, err := ext.SSO.OIDCAuthURL(redirectURI, state)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"auth_url": url})
	}
}

func (s *Server) wrapSSOOIDCCallback(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code        string `json:"code"`
			RedirectURI string `json:"redirect_uri"`
		}
		decodeJSON(r, &req)
		resp, err := ext.SSO.HandleOIDCCallback(req.Code, req.RedirectURI)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func (s *Server) wrapSSOSCIM(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ext.SSO.HandleSCIMRequest(w, r)
	}
}

// --- Sovereign Handlers ---

func (s *Server) wrapSovImportBundle(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var bundle sovereign.TrustBundle
		if err := decodeJSON(r, &bundle); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Sovereign.ImportTrustBundle(bundle)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapSovImportUpdate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var update sovereign.OfflineUpdate
		if err := decodeJSON(r, &update); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		result, err := ext.Sovereign.ImportUpdate(update)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) wrapSovApplyUpdate(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := ext.Sovereign.ApplyUpdate(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
	}
}

func (s *Server) wrapSovPendingUpdates(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ext.Sovereign.ListPendingUpdates())
	}
}

func (s *Server) wrapSovTimeProof(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		proof := ext.Sovereign.GenerateTimeProof(orgID)
		writeJSON(w, http.StatusOK, proof)
	}
}

// --- Realtime Handler ---

func (s *Server) wrapRealtimeStatus(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"connected_clients": ext.Realtime.ConnectedClients(),
		})
	}
}


// --- Catalog Handlers ---

func (s *Server) wrapCatalogModels(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := getOrgID(r)
		descs, err := ext.Catalog.GetEffectiveCatalog("", orgID, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, descs)
	}
}

func (s *Server) wrapCatalogRegister(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cm models.CatalogModel
		if err := decodeJSON(r, &cm); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := ext.Catalog.RegisterCatalogModel(&cm); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, cm)
	}
}

func (s *Server) wrapCatalogEpoch(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := r.URL.Query().Get("account_id")
		orgID := getOrgID(r)
		epoch, err := ext.Catalog.GenerateCatalogEpoch(accountID, orgID, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, epoch)
	}
}

func (s *Server) wrapCatalogSeed(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := ext.Catalog.SeedDefaultCatalog(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "seeded"})
	}
}

func (s *Server) wrapCatalogWithdraw(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ext.Catalog.WithdrawModel(id, "manual withdraw")
		writeJSON(w, http.StatusOK, map[string]string{"status": "withdrawn"})
	}
}

func (s *Server) wrapCatalogAnnounce(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ext.Catalog.AnnounceModel(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "announced"})
	}
}

// --- Public Cloud Handlers ---

func (s *Server) wrapPublicCreateAccount(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email        string `json:"email"`
			DisplayName  string `json:"display_name"`
			DisplayNameKo string `json:"display_name_ko"`
			Plan         string `json:"plan"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		acct, err := ext.PublicCloud.CreateAccount(req.Email, req.DisplayName, req.DisplayNameKo, req.Plan)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, acct)
	}
}

func (s *Server) wrapPublicAccounts(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var accounts []models.Account
		s.db.Find(&accounts)
		writeJSON(w, http.StatusOK, accounts)
	}
}

func (s *Server) wrapPublicGetAccount(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		acct, err := ext.PublicCloud.GetAccount(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, acct)
	}
}

func (s *Server) wrapPublicCreateSub(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct{ Plan string `json:"plan"` }
		decodeJSON(r, &req)
		sub, err := ext.PublicCloud.CreateSubscription(id, req.Plan)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, sub)
	}
}

func (s *Server) wrapPublicLease(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		lease, err := ext.PublicCloud.IssueCapacityLease(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, lease)
	}
}

func (s *Server) wrapPublicSlots(ext *AdditionalServices) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		acct, _ := ext.PublicCloud.GetAccount(id)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"max_harnesses":         acct.MaxHarnesses,
			"max_active_harnesses":  acct.MaxActiveHarnesses,
			"normal_work_slots":     acct.NormalWorkSlots,
			"heavy_work_slots":      acct.HeavyWorkSlots,
			"background_slots":      acct.BackgroundSlots,
		})
	}
}


func mustCatalog(db *gorm.DB) *catalog.Service {
	svc, err := catalog.New(db)
	if err != nil {
		panic(fmt.Sprintf("api: init catalog: %v", err))
	}
	return svc
}

func mustPublicCloud(db *gorm.DB) *publiccloud.Service {
	svc, err := publiccloud.New(db)
	if err != nil {
		panic(fmt.Sprintf("api: init publiccloud: %v", err))
	}
	return svc
}

// Ensure imports used
var _ = fmt.Sprintf
