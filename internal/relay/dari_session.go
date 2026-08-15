package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/models"
	"github.com/patrickrho-patty/pccp/internal/policy"
	"github.com/patrickrho-patty/pccp/internal/provenance"
	"gorm.io/gorm"
)

// This file implements the session-governance and provenance flows of
// the DARI listener:
//
//   - setupSession: on SESSION_OPEN the relay binds the session to a
//     policy epoch, issues a signed capability lease, pushes the
//     effective model catalog, and grants the session (A3/A4/A5 e2e).
//   - evidence-receipt issuance after every governed exchange (B3 e2e).
//   - ingest* handlers: connector-pushed provenance envelopes recorded
//     through the provenance service (B1 e2e).

// authAckPayload builds the AUTH_ACK body. The connector reads the
// policy issuer's public key here so it can verify the lease the
// relay issues moments later — no side channel, no config file.
func (pl *DARIListener) authAckPayload() []byte {
	payload := map[string]string{
		"status":           "authenticated",
		"relay_id":         pl.svc.relayID,
		"policy_issuer":    "pccp-policy",
		"policy_issuer_pk": hex.EncodeToString(pl.svc.Policy().SigningPublicKey()),
		// Receipt signer (B3): the connector verifies pushed evidence
		// receipts under this key (persisted service identity).
		"receipt_signer_pk": hex.EncodeToString(pl.svc.Provenance().SigningPublicKey()),
	}
	data, _ := json.Marshal(payload)
	return data
}

// sessionOpenRequest is the connector's SESSION_OPEN body.
type sessionOpenRequest struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Model     string `json:"model"`
}

// setupSession issues the session's lease and pushes the governance
// baseline (epoch + catalog). Order matters: POLICY_EPOCH first (the
// catalog snapshot is epoch-bound), then CATALOG_SNAPSHOT, then
// LEASE_ISSUE, then SESSION_GRANT. The connector's connect() consumes
// exactly this sequence before its first AI_OPEN.
func (pl *DARIListener) setupSession(ctx context.Context, conn *dari.TransportConn, connID string, so sessionOpenRequest, harnessVersion string) {
	if so.SessionID == "" {
		pl.sendJSONError(conn, connID, "session_open missing session_id")
		return
	}
	orgID := pl.orgForPeer(connID)
	if orgID == "" {
		pl.sendJSONError(conn, connID, "authenticated peer has no organization")
		return
	}
	// D5: the org's forced harness version refuses sub-minimum
	// connectors at session setup (the audit trail carries the floor).
	if minV := pl.svc.ForcedHarnessVersion(orgID); minV != "" && harnessVersion != "" && versionBelow(harnessVersion, minV) {
		pl.sendJSONError(conn, connID, "harness version "+harnessVersion+" is below the organization minimum "+minV)
		return
	}
	if so.UserID == "" {
		so.UserID = "user-" + connID
	}

	// Model serving (Task 15/audit finding): a peer-supplied so.Model name
	// NEVER self-authorizes. Outside explicit dev bootstrap
	// (PCCP_DEV_BOOTSTRAP=1) the package, endpoint, endpoint-lease, and
	// a policy epoch allowing the package must ALREADY exist — the
	// session fails closed otherwise. Bootstrap mode exists for
	// first-run/e2e bring-up only.
	pkg, err := plEnsureModelServing(pl, orgID, so.Model)
	if err != nil {
		pl.sendJSONError(conn, connID, "so.Model serving unavailable: "+err.Error())
		return
	}
	epoch, err := pl.svc.Policy().GetActiveEpoch(orgID)
	if err != nil {
		if !devBootstrap() {
			pl.sendJSONError(conn, connID, "no active policy epoch for organization: "+err.Error())
			return
		}
		// Bootstrap: the FIRST epoch for a fresh organization allows the
		// bootstrapped package. Never widens an existing epoch.
		epoch, err = pl.svc.Policy().CreatePolicyEpoch(orgID, []string{pkg.PackageID}, "immediate")
		if err == nil {
			// A new epoch invalidates every cached governance snapshot
			// immediately (bounded staleness never outlives the epoch).
			pl.svc.hotState.InvalidateAll()
		}
		if err != nil {
			pl.sendJSONError(conn, connID, "bootstrap epoch creation failed: "+err.Error())
			return
		}
	} else if !epochAllowsPackage(epoch, pkg.PackageID) {
		pl.sendJSONError(conn, connID, "so.Model not allowed under the active policy epoch")
		return
	}

	// Push POLICY_EPOCH (0x0D10).
	wireEpoch, err := buildWirePolicyEpoch(epoch, pl.svc.Policy().SigningPublicKey())
	if err != nil {
		pl.sendJSONError(conn, connID, "epoch encode failed: "+err.Error())
		return
	}
	epochBody, err := encodeWire(wireEpoch)
	if err != nil {
		pl.sendJSONError(conn, connID, "epoch encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(dari.MsgPolicyEpochPush, nil, epochBody, 0, 1); err != nil {
		log.Printf("relay: policy-epoch push to %s failed: %v", connID, err)
		return
	}

	// Push CATALOG_SNAPSHOT (0x0D01) bound to the epoch. The snapshot
	// merges the org's CatalogModels with the ModelPackage registry so
	// a freshly bootstrapped relay advertises its governed models.
	descriptors, err := pl.svc.Catalog().GetEffectiveCatalog("", orgID, "")
	if err != nil {
		log.Printf("relay: catalog resolve for %s failed: %v", connID, err)
		descriptors = nil
	}
	if pkg != nil {
		descriptors = append(descriptors, models.ModelDescriptor{
			CatalogModelID: pkg.ModelID,
			DisplayName:    pkg.Name,
			ReleaseChannel: pkg.Version,
		})
	}
	thumb := policyKeyThumb(pl.svc)
	wireCatalog := buildWireCatalogSnapshot(epoch.EpochID, thumb, descriptors, time.Now())
	wireCatalog.Digest = wireCatalogDigest(wireCatalog)
	catalogBody, err := encodeWire(wireCatalog)
	if err != nil {
		pl.sendJSONError(conn, connID, "catalog encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(dari.MsgModelCatalogSnapshot, nil, catalogBody, 0, 2); err != nil {
		log.Printf("relay: catalog push to %s failed: %v", connID, err)
		return
	}

	// C1.3: push the org's DLP rule pack so the connector's scanner
	// runs the server-enforced lexicon (same bytes, same epoch).
	if secRules := pl.svc.securityRulesFor(orgID); len(secRules) > 0 {
		pack := BuildDLPRulePack(epoch.EpochID, orgID, secRules, time.Now())
		if body, derr := encodeWire(pack); derr == nil {
			if err := conn.SendMessage(dari.MsgDLPRulePack, nil, body, 0, 2); err != nil {
				log.Printf("relay: DLP rule pack push to %s failed: %v", connID, err)
			}
		}
	}

	// Production governance wiring (C3/C4/D1/D3-D6/E4): push the
	// org's governance-state snapshot so the connector's governed
	// gates fire on real tool calls.
	govView := pl.svc.GatherGovernanceState(orgID, "", so.Model)
	govSnap := BuildGovernanceState(govView)
	if govBody, gerr := encodeWire(govSnap); gerr == nil {
		if err := conn.SendMessage(dari.MsgGovernanceState, nil, govBody, 0, 2); err != nil {
			log.Printf("relay: governance-state push to %s failed: %v", connID, err)
		}
	}

	// Issue the capability lease (A3) and push LEASE_ISSUE (0x0210).
	lease, err := pl.svc.Policy().IssueCapabilityLease(pl.leaseRequest(connID, orgID, so.SessionID, so.UserID, epoch.EpochID, so.Model))
	if err != nil {
		pl.sendJSONError(conn, connID, "lease issuance failed: "+err.Error())
		return
	}
	wireLeaseObj, err := buildWireLease(lease, "pccp-policy")
	if err != nil {
		pl.sendJSONError(conn, connID, "lease encode failed: "+err.Error())
		return
	}
	leaseBody, err := encodeWire(wireLeaseObj)
	if err != nil {
		pl.sendJSONError(conn, connID, "lease encode failed: "+err.Error())
		return
	}
	if err := conn.SendMessage(dari.MsgLeaseIssue, nil, leaseBody, 0, 3); err != nil {
		log.Printf("relay: lease push to %s failed: %v", connID, err)
		return
	}

	// Issue the DARI Authorization Grant (Task 7): signed by the
	// policy issuer, bound to the harness's verified subject key.
	harnessPub := ed25519.PublicKey(nil)
	if cred := pl.credentialForConn(connID); cred != nil && len(cred.PublicKey) == ed25519.PublicKeySize {
		harnessPub = ed25519.PublicKey(cred.PublicKey)
	}
	if harnessPub == nil {
		pl.sendJSONError(conn, connID, "authenticated credential carries no subject key")
		return
	}
	grantEnv, err := pl.svc.IssueSessionGrant(lease, harnessPub)
	if err != nil {
		pl.sendJSONError(conn, connID, "authorization grant issuance failed: "+err.Error())
		return
	}

	// SESSION_GRANT (0x0201) closes the setup phase and carries the
	// signed grant on dari/1 connections.
	pl.mu.Lock()
	if state := pl.conns[connID]; state != nil {
		state.epoch = epoch.EpochID
		state.grant = grantEnv
		state.sessionID = so.SessionID
	}
	pl.mu.Unlock()
	grant, _ := json.Marshal(map[string]string{
		"session_id":   so.SessionID,
		"policy_epoch": epoch.EpochID,
		"lease_id":     lease.LeaseID,
		"organization": orgID,
		"grant_hex":    GrantHexForWire(grantEnv),
	})
	if err := conn.SendMessage(dari.MsgSessionGrant, nil, grant, 0, 4); err != nil {
		log.Printf("relay: session grant to %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: session %s granted for %s (epoch=%s lease=%s grant=%s)", so.SessionID, connID, epoch.EpochID, lease.LeaseID, grantEnv.Body.GrantID)
}

// leaseRequest builds the issuance request. The lease's allowed-models
// list carries MODEL IDs — the connector's AuthorizeExchange compares
// the requested model ID against exactly these.
func (pl *DARIListener) leaseRequest(connID, orgID, sessionID, userID, epochID, model string) policy.IssueLeaseRequest {
	allowed := []string{model}
	if model == "" && pl.svc != nil {
		allowed = nil // relay-side validation catches an unnamed model
	}
	peerID := pl.peerIDForConn(connID)
	return policy.IssueLeaseRequest{
		OrganizationID: orgID,
		SubjectPeerID:  peerID,
		UserID:         userID,
		SessionID:      sessionID,
		PolicyEpochID:  epochID,
		AllowedModels:  allowed,
		Validity:       8 * time.Hour,
	}
}

// devBootstrap reports whether explicit first-run bootstrap is on
// (PCCP_DEV_BOOTSTRAP=1). Bootstrap auto-registers unknown models and
// issues the first epoch; production NEVER runs in this mode — a
// peer-supplied so.Model cannot self-authorize.
func devBootstrap() bool { return os.Getenv("PCCP_DEV_BOOTSTRAP") == "1" }

// ensureModelServing resolves the governed chain a real exchange
// needs. In bootstrap mode missing rows are created (first-run); in
// production every row must already exist and the lookup fails closed.
func plEnsureModelServing(pl *DARIListener, orgID, model string) (*models.ModelPackage, error) {
	return ensureModelServingForDB(pl.svc.db, orgID, model)
}

func ensureModelServingForDB(db *gorm.DB, orgID, model string) (*models.ModelPackage, error) {
	if model == "" {
		return nil, fmt.Errorf("no model requested")
	}
	var pkg models.ModelPackage
	if err := db.Where("model_id = ?", model).First(&pkg).Error; err != nil {
		if !devBootstrap() {
			return nil, fmt.Errorf("model %s not registered (fail closed)", model)
		}
		provisioned, perr := provisionModelServingForDB(db, orgID, model)
		if perr != nil {
			return nil, perr
		}
		return provisioned, nil
	}
	if pkg.State != "published" {
		return nil, fmt.Errorf("model %s is %s (not published)", model, pkg.State)
	}
	return ensureServingChainForDB(db, orgID, &pkg)
}

// ensureServingChainForDB resolves or fail-closes the endpoint +
// endpoint-lease for an already-registered package.
func ensureServingChainForDB(db *gorm.DB, orgID string, pkg *models.ModelPackage) (*models.ModelPackage, error) {
	var endpoint models.InferenceEndpoint
	if err := db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		orgID, pkg.PackageID).First(&endpoint).Error; err != nil {
		return nil, fmt.Errorf("no active endpoint for %s (fail closed)", pkg.PackageID)
	}
	var epLease models.EndpointLease
	if err := db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		First(&epLease).Error; err != nil {
		return nil, fmt.Errorf("no valid endpoint lease for %s (fail closed)", endpoint.EndpointID)
	}
	return pkg, nil
}

// provisionModelServingForDB registers the FULL serving chain for a
// model (published package → org endpoint → endpoint lease) with no
// dev-bootstrap env: operators, the benchmark, and first-run bring-up
// call it explicitly through RegisterModelServing. Idempotent — every
// step reuses an existing row.
func provisionModelServingForDB(db *gorm.DB, orgID, model string) (*models.ModelPackage, error) {
	if model == "" {
		return nil, fmt.Errorf("no model requested")
	}
	var pkg models.ModelPackage
	if err := db.Where("model_id = ?", model).First(&pkg).Error; err != nil {
		pkg = models.ModelPackage{
			PackageID: "pmp-" + model,
			ModelID:   model,
			Name:      model,
			Family:    "general",
			State:     "published",
		}
		if err := db.Create(&pkg).Error; err != nil {
			return nil, fmt.Errorf("register model package: %w", err)
		}
	}
	var endpoint models.InferenceEndpoint
	if err := db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		orgID, pkg.PackageID).First(&endpoint).Error; err != nil {
		endpoint = models.InferenceEndpoint{
			OrganizationID: orgID,
			EndpointID:     "ep-" + pkg.PackageID,
			Name:           "primary PIA for " + model,
			PIAPeerID:      "pia-local",
			ModelPackageID: pkg.PackageID,
			ServingEngine:  "openai-compat",
			Status:         "active",
		}
		if err := db.Create(&endpoint).Error; err != nil {
			return nil, fmt.Errorf("register endpoint: %w", err)
		}
	}
	var epLease models.EndpointLease
	if err := db.Where("endpoint_id = ? AND status = 'active' AND not_after > ?",
		endpoint.EndpointID, time.Now().Format(time.RFC3339)).
		First(&epLease).Error; err != nil {
		now := time.Now()
		epLease = models.EndpointLease{
			EndpointID:     endpoint.EndpointID,
			OrganizationID: orgID,
			ModelPackageID: pkg.PackageID,
			LeaseID:        dari.GenerateID("epl"),
			PIAPeerID:      "pia-local",
			Status:         "active",
		}
		epLease.NotBefore = now.Format(time.RFC3339)
		epLease.NotAfter = now.Add(7 * 24 * time.Hour).Format(time.RFC3339)
		epLease.IssuedAt = now.Format(time.RFC3339)
		if err := db.Create(&epLease).Error; err != nil {
			return nil, fmt.Errorf("issue endpoint lease: %w", err)
		}
	}
	return &pkg, nil
}

// RegisterModelServing onboards a model's serving chain (published
// package → active org endpoint → active endpoint lease) WITHOUT the
// dev-bootstrap env. Operators, tests, and the benchmark call this
// explicitly; the session path stays fail-closed for unknown models.
func (s *Service) RegisterModelServing(orgID, model string) (*models.ModelPackage, error) {
	return provisionModelServingForDB(s.db, orgID, model)
}

// epochAllowsPackage reports whether the epoch's allow-list contains
// the package ID.
func epochAllowsPackage(epoch *models.PolicyEpoch, packageID string) bool {
	if epoch == nil {
		return false
	}
	var allowed []string
	json.Unmarshal([]byte(epoch.AllowedModelsJSON), &allowed)
	for _, m := range allowed {
		if m == packageID {
			return true
		}
	}
	return false
}

// orgForPeer resolves the authenticated credential's organization.
func (pl *DARIListener) orgForPeer(connID string) string {
	cred := pl.credentialForConn(connID)
	if cred == nil {
		return ""
	}
	return cred.Organization
}

// credentialForConn returns the credential captured at auth time.
func (pl *DARIListener) credentialForConn(connID string) *dari.PeerCredential {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if state := pl.conns[connID]; state != nil {
		return state.cred
	}
	return nil
}

// peerIDForConn returns the authenticated subject peer ID.
func (pl *DARIListener) peerIDForConn(connID string) string {
	if cred := pl.credentialForConn(connID); cred != nil {
		return cred.SubjectPeerID
	}
	return connID
}

func (pl *DARIListener) sendJSONError(conn *dari.TransportConn, connID, msg string) {
	log.Printf("relay: session setup error for %s: %s", connID, msg)
	payload, _ := json.Marshal(map[string]string{"error": msg})
	_ = conn.SendMessage(dari.MsgClose, nil, payload, 0, 1)
}

// ---------------------------------------------------------------------------
// Evidence receipts (B3): GovernInference issues the signed receipt for
// each governed exchange; the listener pushes it to the harness over
// EVIDENCE_RECEIPT (0x0307) and records the connector's ack.
// ---------------------------------------------------------------------------

func exchangeDigestHex(exchangeID string) string {
	h := sha256.Sum256([]byte("exchange|" + exchangeID))
	return hex.EncodeToString(h[:])
}

// sessionEpoch tracks the epoch bound at session setup so receipts
// carry it.
func (pl *DARIListener) epochForSession(connID string) string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if state := pl.conns[connID]; state != nil {
		return state.epoch
	}
	return ""
}

// ---------------------------------------------------------------------------
// Connector → relay provenance ingestion (B1).
// ---------------------------------------------------------------------------

func (pl *DARIListener) ingestChangeSet(conn *dari.TransportConn, connID, harnessID string, record *dari.Record) {
	var env wireChangeSetEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: changeset decode from %s failed: %v", connID, err)
		return
	}
	// Org attribution ALWAYS derives from the authenticated credential —
	// a client-supplied OrganizationID is never trusted (cross-tenant
	// provenance injection).
	orgID := pl.orgForPeer(connID)
	// D3 defense-in-depth: an active change freeze blocks AI writes.
	// The connector's dispatch gate already refuses them; a connector
	// that ignores the freeze has its changesets rejected here too.
	if frozen, reason, ferr := pl.svc.ActiveChangeFreeze(orgID); ferr == nil && frozen {
		errPayload, _ := json.Marshal(map[string]string{
			"error":         "change freeze active — AI changeset rejected",
			"freeze_reason": reason,
		})
		conn.SendMessage(dari.MsgChangeSetNack, nil, errPayload, record.LaneID, record.LaneSequence+1)
		return
	}
	_, err := pl.svc.provenance.CreateChangeSet(provenance.CreateChangeSetRequest{
		OrganizationID: orgID,
		SessionID:      env.SessionID,
		ExchangeID:     env.ExchangeID,
		RepositoryID:   env.RepositoryID,
		Branch:         env.Branch,
		UserID:         env.UserID,
		HarnessID:      harnessID,
		ModelPackageID: env.ModelPackageID,
		EndpointID:     env.EndpointID,
		FilesChanged:   env.FilesChanged,
		DiffSummary:    env.DiffSummary,
		LinesAdded:     env.LinesAdded,
		LinesRemoved:   env.LinesRemoved,
	})
	if err != nil {
		log.Printf("relay: changeset record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: changeset %s recorded from %s", env.ChangeSetID, connID)
}

func (pl *DARIListener) ingestSpan(conn *dari.TransportConn, connID, harnessID string, record *dari.Record) {
	var env wireSpanEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: span decode from %s failed: %v", connID, err)
		return
	}
	// Org attribution ALWAYS derives from the authenticated credential —
	// a client-supplied OrganizationID is never trusted (cross-tenant
	// provenance injection).
	orgID := pl.orgForPeer(connID)
	_, err := pl.svc.provenance.CreateProvenanceSpan(provenance.CreateSpanRequest{
		OrganizationID:   orgID,
		RepositoryID:     env.RepositoryID,
		ChangeSetID:      env.ChangeSetID,
		FilePath:         env.FilePath,
		CommitSHA:        env.CommitSHA,
		SymbolLang:       env.SymbolLang,
		SymbolName:       env.SymbolName,
		StartLine:        env.StartLine,
		EndLine:          env.EndLine,
		AttributionState: env.AttributionState,
		Confidence:       env.Confidence,
		SessionID:        env.SessionID,
		UserID:           env.UserID,
		HarnessID:        harnessID,
	})
	if err != nil {
		log.Printf("relay: span record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: span %s recorded from %s", env.SpanID, connID)
}

func (pl *DARIListener) ingestCommitBinding(conn *dari.TransportConn, connID, harnessID string, record *dari.Record) {
	var env wireCommitBindingEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: commit-binding decode from %s failed: %v", connID, err)
		return
	}
	// Org attribution ALWAYS derives from the authenticated credential —
	// a client-supplied OrganizationID is never trusted (cross-tenant
	// provenance injection).
	orgID := pl.orgForPeer(connID)
	if _, err := pl.svc.provenance.BindCommit(orgID, env.RepositoryID, env.CommitSHA, env.ChangeSetID, env.SessionID, env.Branch); err != nil {
		log.Printf("relay: commit-binding record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: commit binding %s recorded from %s", env.BindingID, connID)
}

func (pl *DARIListener) ingestActionEnvelope(conn *dari.TransportConn, connID, harnessID string, record *dari.Record) {
	var env wireActionEnvelope
	if err := decodeWire(record.Payload, &env); err != nil {
		log.Printf("relay: action decode from %s failed: %v", connID, err)
		return
	}
	// Org attribution ALWAYS derives from the authenticated credential —
	// a client-supplied OrganizationID is never trusted (cross-tenant
	// provenance injection).
	orgID := pl.orgForPeer(connID)
	_, err := pl.svc.provenance.RecordAction(provenance.RecordActionRequest{
		OrganizationID: orgID,
		SessionID:      env.SessionID,
		ExchangeID:     env.ExchangeID,
		UserID:         env.UserID,
		HarnessID:      harnessID,
		ModelPackageID: env.ModelPackageID,
		EndpointID:     env.EndpointID,
		ProjectID:      env.ProjectID,
		RepositoryID:   env.RepositoryID,
		Branch:         env.Branch,
		PolicyEpochID:  env.PolicyEpochID,
		LeaseID:        env.LeaseID,
		ActionType:     env.ActionType,
		ActionPayload:  env.ActionPayload,
		VerdictResult:  env.VerdictResult,
		Classification: env.Classification,
	})
	if err != nil {
		log.Printf("relay: action record from %s failed: %v", connID, err)
		return
	}
	log.Printf("relay: action %s recorded from %s", env.ActionID, connID)
}

func (pl *DARIListener) ingestReceiptAck(conn *dari.TransportConn, connID string, record *dari.Record) {
	var ack wireReceiptAck
	if err := decodeWire(record.Payload, &ack); err != nil {
		log.Printf("relay: receipt-ack decode from %s failed: %v", connID, err)
		return
	}
	if err := pl.svc.provenance.AckEvidenceReceipt(ack.ExchangeID); err != nil {
		log.Printf("relay: receipt ack for %s failed: %v", ack.ExchangeID, err)
		return
	}
	log.Printf("relay: receipt ack recorded for %s from %s", ack.ExchangeID, connID)
}

// policyKeyThumb is the single policy-key thumbprint derivation used
// by the epoch/catalog pushes.
func policyKeyThumb(svc *Service) [32]byte {
	return sha256.Sum256(svc.Policy().SigningPublicKey())
}
